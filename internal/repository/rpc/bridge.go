/*
Package rpc implements bridge to Forest full node API interface.

We recommend using local IPC for fast and the most efficient inter-process communication between the API server
and an Ncogearthchain/Forest node. Any remote RPC connection will work, but the performance may be significantly degraded
by extra networking overhead of remote RPC calls.

You should also consider security implications of opening Forest RPC interface for remote access.
If you considering it as your deployment strategy, you should establish encrypted channel between the API server
and Forest RPC interface with connection limited to specified endpoints.

We strongly discourage opening Forest RPC interface for unrestricted Internet access.
*/
package rpc

import (
	"context"
	"ncogearthchain-api-graphql/internal/config"
	"ncogearthchain-api-graphql/internal/logger"
	"ncogearthchain-api-graphql/internal/repository/rpc/contracts"
	"ncogearthchain-api-graphql/internal/types"
	"ncogearthchain-api-graphql/internal/util"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	etc "github.com/ethereum/go-ethereum/core/types"
	eth "github.com/ethereum/go-ethereum/ethclient"
	nec "github.com/ethereum/go-ethereum/rpc"
	"golang.org/x/sync/singleflight"
)

// rpcHeadProxyChannelCapacity represents the capacity of the new received blocks proxy channel.
const rpcHeadProxyChannelCapacity = 10000

// NecBridge represents Forest RPC abstraction layer.
type NecBridge struct {
	rpc *nec.Client
	eth *eth.Client
	log logger.Logger
	cg  *singleflight.Group

	// fMintCfg represents the configuration of the fMint protocol
	sigConfig     *config.ServerSignature
	sfcConfig     *config.Staking
	uniswapConfig *config.DeFiUniswap

	// extended minter config
	fMintCfg fMintConfig
	fLendCfg fLendConfig

	// common contracts
	sfcAbi      *abi.ABI
	sfcContract *contracts.SfcContract

	// received blocks proxy
	wg       *sync.WaitGroup
	sigClose chan bool
	headers  chan *etc.Header
}

// New creates new Forest RPC connection bridge.
func New(cfg *config.Config, log logger.Logger) (*NecBridge, error) {
	cli, con, err := connect(cfg, log)
	if err != nil {
		log.Criticalf("can not open connection; %s", err.Error())
		return nil, err
	}

	// build the bridge structure using the con we have
	br := &NecBridge{
		rpc: cli,
		eth: con,
		log: log,
		cg:  new(singleflight.Group),

		// special configuration options below this line
		sigConfig:     &cfg.MySignature,
		sfcConfig:     &cfg.Staking,
		uniswapConfig: &cfg.DeFi.Uniswap,
		fMintCfg: fMintConfig{
			addressProvider: cfg.DeFi.FMint.AddressProvider,
		},
		fLendCfg: fLendConfig{lendigPoolAddress: cfg.DeFi.FLend.LendingPool},

		// configure block observation loop
		wg:       new(sync.WaitGroup),
		sigClose: make(chan bool, 1),
		headers:  make(chan *etc.Header, rpcHeadProxyChannelCapacity),
	}

	// inform about the local address of the API node
	log.Noticef("using signature address %s", br.sigConfig.Address.String())

	// add the bridge ref to the fMintCfg and return the instance
	br.fMintCfg.bridge = br
	br.run()
	return br, nil
}

// connect opens connections we need to communicate with the blockchain node.
func connect(cfg *config.Config, log logger.Logger) (*nec.Client, *eth.Client, error) {
	// log what we do
	log.Debugf("connecting blockchain node at %s", cfg.Forest.Url)

	// try to establish a connection
	client, err := nec.Dial(cfg.Forest.Url)
	if err != nil {
		log.Critical(err)
		return nil, nil, err
	}

	// try to establish a for smart contract interaction
	con, err := eth.Dial(cfg.Forest.Url)
	if err != nil {
		log.Critical(err)
		return nil, nil, err
	}

	// log
	log.Notice("node connection open")
	return client, con, nil
}

// run starts the bridge threads required to collect blockchain data.
func (nec *NecBridge) run() {
	nec.wg.Add(1)
	go nec.observeBlocks()
}

// terminate kills the bridge threads to end the bridge gracefully.
func (nec *NecBridge) terminate() {
	nec.sigClose <- true
	nec.wg.Wait()
	nec.log.Noticef("rpc threads terminated")
}

// Close will finish all pending operations and terminate the Forest RPC connection
func (nec *NecBridge) Close() {
	// terminate threads before we close connections
	nec.terminate()

	// do we have a connection?
	if nec.rpc != nil {
		nec.rpc.Close()
		nec.eth.Close()
		nec.log.Info("blockchain connections are closed")
	}
}

// Connection returns open Ncogearthchain/Forest connection.
func (nec *NecBridge) Connection() *nec.Client {
	return nec.rpc
}

// DefaultCallOpts creates a default record for call options.
func (nec *NecBridge) DefaultCallOpts() *bind.CallOpts {
	// get the default call opts only once if called in parallel
	co, _, _ := nec.cg.Do("default-call-opts", func() (interface{}, error) {
		return &bind.CallOpts{
			Pending:     false,
			From:        nec.sigConfig.Address,
			BlockNumber: nil,
			Context:     context.Background(),
		}, nil
	})
	return co.(*bind.CallOpts)
}

// SfcContract returns instance of SFC contract for interaction.
func (nec *NecBridge) SfcContract() *contracts.SfcContract {
	// lazy create SFC contract instance
	if nil == nec.sfcContract {
		// instantiate the contract and display its name
		var err error
		nec.sfcContract, err = contracts.NewSfcContract(nec.sfcConfig.SFCContract, nec.eth)
		if err != nil {
			nec.log.Criticalf("failed to instantiate SFC contract; %s", err.Error())
			panic(err)
		}
	}
	return nec.sfcContract
}

// SfcAbi returns a parse ABI of the AFC contract.
func (nec *NecBridge) SfcAbi() *abi.ABI {
	if nil == nec.sfcAbi {
		ab, err := abi.JSON(strings.NewReader(contracts.SfcContractABI))
		if err != nil {
			nec.log.Criticalf("failed to parse SFC contract ABI; %s", err.Error())
			panic(err)
		}
		nec.sfcAbi = &ab
	}
	return nec.sfcAbi
}

// ObservedBlockProxy provides a channel fed with new headers observed
// by the connected blockchain node.
func (nec *NecBridge) ObservedBlockProxy() chan *etc.Header {
	return nec.headers
}

func (br *NecBridge) TraceBlock(hash common.Hash) (*types.TraceBlockResponse, error) {
	var raw []types.RPCTraceBlock
	if err := br.rpc.CallContext(context.Background(), &raw, "debug_traceBlock", hash); err != nil {
		return nil, err
	}

	// build a slice of *TraceBlockResult
	out := make([]*types.TraceBlockResult, len(raw))
	for i, elt := range raw {
		out[i] = elt.Inner
		if out[i] != nil && out[i].ReturnValue != nil {
			decoded, err := util.DecodeReturnNoABI(*out[i].ReturnValue)
			if err == nil {
				out[i].ReturnValueDecoded = decoded
			}
		}
	}
	// pass &out so Result is *[]*TraceBlockResult
	return &types.TraceBlockResponse{Result: &out}, nil
}

func (br *NecBridge) TraceBlockByNumber(number hexutil.Uint64) (*types.TraceBlockResponse, error) {
	var raw []types.RPCTraceBlock
	if err := br.rpc.CallContext(context.Background(), &raw, "debug_traceBlockByNumber", number); err != nil {
		return nil, err
	}

	out := make([]*types.TraceBlockResult, len(raw))
	for i, elt := range raw {
		out[i] = elt.Inner
		if out[i] != nil && out[i].ReturnValue != nil {
			decoded, err := util.DecodeReturnNoABI(*out[i].ReturnValue)
			if err == nil {
				out[i].ReturnValueDecoded = decoded
			}
		}
	}
	return &types.TraceBlockResponse{Result: &out}, nil
}

func (br *NecBridge) TraceBlockByHash(hash common.Hash) (*types.TraceBlockResponse, error) {
	var raw []types.RPCTraceBlock
	if err := br.rpc.CallContext(context.Background(), &raw, "debug_traceBlockByHash", hash); err != nil {
		return nil, err
	}

	out := make([]*types.TraceBlockResult, len(raw))
	for i, elt := range raw {
		out[i] = elt.Inner
		if out[i] != nil && out[i].ReturnValue != nil {
			decoded, err := util.DecodeReturnNoABI(*out[i].ReturnValue)
			if err == nil {
				out[i].ReturnValueDecoded = decoded
			}
		}
	}
	return &types.TraceBlockResponse{Result: &out}, nil
}

// TraceTransaction fetches the execution-trace for the given transaction hash.
func (br *NecBridge) TraceTransaction(txHash common.Hash) (*types.TraceTransactionResponse, error) {
	// debug_traceTransaction returns a single object, not an array
	var raw types.TraceBlockResult
	if err := br.rpc.CallContext(context.Background(), &raw, "debug_traceTransaction", txHash); err != nil {
		return nil, err
	}

	// wrap the single result in a slice
	if raw.ReturnValue != nil {
		decoded, err := util.DecodeReturnNoABI(*raw.ReturnValue)
		if err == nil {
			raw.ReturnValueDecoded = decoded
		}
	}
	slice := []*types.TraceBlockResult{&raw}
	return &types.TraceTransactionResponse{Result: &slice}, nil
}
