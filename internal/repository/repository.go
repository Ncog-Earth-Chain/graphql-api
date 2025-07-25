/*
Package repository implements repository for handling fast and efficient access to data required
by the resolvers of the API server.

Internally it utilizes RPC to access Ncogearthchain/Forest full node for blockchain interaction. Mongo database
for fast, robust and scalable off-chain data storage, especially for aggregated and pre-calculated data mining
results. BigCache for in-memory object storage to speed up loading of frequently accessed entities.
*/
package repository

import (
	"fmt"
	"math/big"
	"ncogearthchain-api-graphql/internal/config"
	"ncogearthchain-api-graphql/internal/logger"
	"ncogearthchain-api-graphql/internal/repository/cache"
	"ncogearthchain-api-graphql/internal/repository/db"
	"ncogearthchain-api-graphql/internal/repository/rpc"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"golang.org/x/sync/singleflight"
)

// repo represents an instance of the Repository manager.
var repo Repository

// onceRepo is the sync object used to make sure the Repository
// is instantiated only once on the first demand.
var onceRepo sync.Once

// config represents the configuration setup used by the repository
// to establish and maintain required connectivity to external services
// as needed.
var cfg *config.Config

// log represents the logger to be used by the repository.
var log logger.Logger

// SetConfig sets the repository configuration to be used to establish
// and maintain external repository connections.
func SetConfig(c *config.Config) {
	cfg = c
}

// SetLogger sets the repository logger to be used to collect logging info.
func SetLogger(l logger.Logger) {
	log = l
}

// R provides access to the singleton instance of the Repository.
func R() Repository {
	// make sure to instantiate the Repository only once
	onceRepo.Do(func() {
		repo = newRepository()
	})
	return repo
}

// Proxy represents Repository interface implementation and controls access to data
// trough several low level bridges.
type proxy struct {
	cache *cache.MemBridge
	db    *db.MongoDbBridge
	rpc   *rpc.NecBridge
	log   logger.Logger
	cfg   *config.Config

	// transaction estimator counter
	txCount uint64

	// we need a Group to use single flight to control price pulls
	apiRequestGroup singleflight.Group

	// governance contracts reference
	govContracts map[string]*config.GovernanceContract

	// smart contract compilers
	solCompiler string
}

// newRepository creates new instance of Repository implementation, namely proxy structure.
func newRepository() Repository {
	if cfg == nil {
		panic(fmt.Errorf("missing configuration"))
	}
	if log == nil {
		panic(fmt.Errorf("missing logger"))
	}

	// create connections
	caBridge, dbBridge, rpcBridge, err := connect(cfg, log)
	if err != nil {
		log.Fatal("repository init failed")
		return nil
	}

	// construct the proxy instance
	p := proxy{
		cache: caBridge,
		db:    dbBridge,
		rpc:   rpcBridge,
		log:   log,
		cfg:   cfg,

		// get the map of governance contracts
		govContracts: governanceContractsMap(&cfg.Governance),

		// keep reference to the SOL compiler
		solCompiler: cfg.Compiler.DefaultSolCompilerPath,
	}

	// return the proxy
	return &p
}

// governanceContractsMap creates map of governance contracts keyed
// by the contract address.
func governanceContractsMap(cfg *config.Governance) map[string]*config.GovernanceContract {
	// prep the result set
	res := make(map[string]*config.GovernanceContract)

	// collect all the configured governance contracts into the map
	for _, gv := range cfg.Contracts {
		res[gv.Address.String()] = &gv
	}
	return res
}

// connect opens connections to the external sources we need.
func connect(cfg *config.Config, log logger.Logger) (*cache.MemBridge, *db.MongoDbBridge, *rpc.NecBridge, error) {
	// create new in-memory cache bridge
	caBridge, err := cache.New(cfg, log)
	if err != nil {
		log.Criticalf("can not create in-memory cache bridge, %s", err.Error())
		return nil, nil, nil, err
	}

	// create new database connection bridge
	dbBridge, err := db.New(cfg, log)
	if err != nil {
		log.Criticalf("can not connect backend persistent storage, %s", err.Error())
		return nil, nil, nil, err
	}

	// create new Forest RPC bridge
	rpcBridge, err := rpc.New(cfg, log)
	if err != nil {
		log.Criticalf("can not connect Forest RPC interface, %s", err.Error())
		return nil, nil, nil, err
	}
	return caBridge, dbBridge, rpcBridge, nil
}

// Close with close all connections and clean up the pending work for graceful termination.
func (p *proxy) Close() {
	// inform about actions
	p.log.Notice("repository is closing")

	// close connections
	p.db.Close()
	p.rpc.Close()

	// inform about actions
	p.log.Notice("repository done")
}

// Erc721Assets returns all ERC721 contracts where the owner has a balance > 0.
func (p *proxy) Erc721Assets(owner common.Address, count int32) ([]common.Address, error) {
	contracts, err := p.Erc721ContractsList(count)
	if err != nil {
		return nil, err
	}
	var result []common.Address
	for _, contract := range contracts {
		balance, err := p.Erc721BalanceOf(&contract, &owner)
		if err != nil {
			p.log.Errorf("Erc721BalanceOf error for %s: %v", contract.Hex(), err)
			continue
		}
		if balance.ToInt().Cmp(big.NewInt(0)) > 0 {
			result = append(result, contract)
		}
	}
	return result, nil
}

// TokenSummary represents a summary of any token type for a wallet address.
type TokenSummary struct {
	TokenAddress  common.Address `json:"tokenAddress"`
	TokenName     string         `json:"tokenName"`
	TokenSymbol   string         `json:"tokenSymbol"`
	TokenType     string         `json:"tokenType"`
	TokenDecimals int32          `json:"tokenDecimals"`
	Type          string         `json:"type"`
	Amount        hexutil.Big    `json:"amount"`
}

// TokenSummariesByAddress aggregates all token types for a wallet address.
func (p *proxy) TokenSummariesByAddress(addr common.Address, count int32) ([]TokenSummary, error) {
	var summaries []TokenSummary

	// ERC20 tokens
	erc20Tokens, err := p.Erc20Assets(addr, count)
	if err != nil {
		p.log.Errorf("Erc20Assets error for %s: %v", addr.Hex(), err)
		return summaries, err
	}
	for _, tokenAddr := range erc20Tokens {
		token, err := p.Erc20Token(&tokenAddr)
		if err != nil || token == nil {
			p.log.Errorf("Erc20Token error for %s: %v", tokenAddr.Hex(), err)
			continue
		}
		balance, err := p.Erc20BalanceOf(&tokenAddr, &addr)
		if err != nil {
			p.log.Errorf("Erc20BalanceOf error for %s: %v", tokenAddr.Hex(), err)
			continue
		}
		summaries = append(summaries, TokenSummary{
			TokenAddress:  tokenAddr,
			TokenName:     token.Name,
			TokenSymbol:   token.Symbol,
			TokenType:     "ERC20",
			TokenDecimals: token.Decimals,
			Type:          "BALANCE",
			Amount:        balance,
		})
	}

	// fMint Collateral
	fmintAcc, err := p.FMintAccount(addr)
	if err == nil && fmintAcc != nil {
		for _, tokenAddr := range fmintAcc.CollateralList {
			token, err := p.Erc20Token(&tokenAddr)
			if err != nil || token == nil {
				p.log.Errorf("Erc20Token error for fMint collateral %s: %v", tokenAddr.Hex(), err)
				continue
			}
			amount, err := p.FMintTokenBalance(&addr, &tokenAddr, "COLLATERAL")
			if err != nil {
				p.log.Errorf("FMintTokenBalance error for collateral %s: %v", tokenAddr.Hex(), err)
				continue
			}
			summaries = append(summaries, TokenSummary{
				TokenAddress:  tokenAddr,
				TokenName:     token.Name,
				TokenSymbol:   token.Symbol,
				TokenType:     "FMINT_COLLATERAL",
				TokenDecimals: token.Decimals,
				Type:          "DEPOSIT",
				Amount:        amount,
			})
		}
		// fMint Debt
		for _, tokenAddr := range fmintAcc.DebtList {
			token, err := p.Erc20Token(&tokenAddr)
			if err != nil || token == nil {
				p.log.Errorf("Erc20Token error for fMint debt %s: %v", tokenAddr.Hex(), err)
				continue
			}
			amount, err := p.FMintTokenBalance(&addr, &tokenAddr, "DEBT")
			if err != nil {
				p.log.Errorf("FMintTokenBalance error for debt %s: %v", tokenAddr.Hex(), err)
				continue
			}
			summaries = append(summaries, TokenSummary{
				TokenAddress:  tokenAddr,
				TokenName:     token.Name,
				TokenSymbol:   token.Symbol,
				TokenType:     "FMINT_DEBT",
				TokenDecimals: token.Decimals,
				Type:          "DEBT",
				Amount:        amount,
			})
		}
	} else if err != nil {
		p.log.Errorf("FMintAccount error for %s: %v", addr.Hex(), err)
	}

	// ERC721 tokens (NFTs)
	erc721Tokens, err := p.Erc721Assets(addr, count)
	if err == nil {
		for _, tokenAddr := range erc721Tokens {
			name, _ := p.Erc721Name(&tokenAddr)
			symbol, _ := p.Erc721Symbol(&tokenAddr)
			ownedCount, err := p.Erc721BalanceOf(&tokenAddr, &addr)
			if err != nil {
				p.log.Errorf("Erc721BalanceOf error for %s: %v", tokenAddr.Hex(), err)
				continue
			}
			summaries = append(summaries, TokenSummary{
				TokenAddress:  tokenAddr,
				TokenName:     name,
				TokenSymbol:   symbol,
				TokenType:     "ERC721",
				TokenDecimals: 0,
				Type:          "OWNED",
				Amount:        ownedCount,
			})
		}
	} else {
		p.log.Errorf("Erc721Assets error for %s: %v", addr.Hex(), err)
	}

	return summaries, nil
}
