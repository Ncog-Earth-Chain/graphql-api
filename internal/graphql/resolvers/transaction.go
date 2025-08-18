// Package resolvers implements GraphQL resolvers to incoming API requests.
package resolvers

import (
	"fmt"
	"ncogearthchain-api-graphql/internal/repository"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"golang.org/x/sync/singleflight"
)

// Transaction represents resolvable blockchain transaction structure.
type Transaction struct {
	types.Transaction
	cg *singleflight.Group
}

// NewTransaction builds new resolvable transaction structure.
func NewTransaction(trx *types.Transaction) *Transaction {
	return &Transaction{
		Transaction: *trx,
		cg:          new(singleflight.Group),
	}
}

// Transaction resolves blockchain transaction by transaction hash.
func (rs *rootResolver) Transaction(args *struct{ Hash common.Hash }) (tx *Transaction, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Criticalf("transaction loader crashed on %s", args.Hash.String())
			err = fmt.Errorf("failed to load transaction %s", args.Hash.String())
			tx = nil
		}
	}()

	// get the transaction from repository
	trx, err := repository.R().Transaction(&args.Hash)
	if err != nil {
		log.Warningf("can not get transaction %s", args.Hash)
		return nil, err
	}

	// transaction not found, yet no error?
	if trx == nil {
		log.Errorf("transaction %s not found", args.Hash.String())
		return nil, fmt.Errorf("transaction %s not found", args.Hash.String())
	}
	return NewTransaction(trx), nil
}

// SendTransaction sends raw signed and RLP encoded transaction to the blockchain.
func (rs *rootResolver) SendTransaction(args *struct{ Tx hexutil.Bytes }) (*Transaction, error) {
	// get the transaction from repository
	trx, err := repository.R().SendTransaction(args.Tx)
	if err != nil {
		log.Warningf("can not send transaction; %s", err.Error())
		return nil, err
	}

	return NewTransaction(trx), nil
}

// Sender resolves sender's account of the transaction.
func (trx *Transaction) Sender() (*Account, error) {
	// get the sender by address
	acc, err := repository.R().Account(&trx.From)
	if err != nil {
		return nil, err
	}

	return NewAccount(acc), nil
}

// Recipient resolves recipient's account of the transaction.
func (trx *Transaction) Recipient() (*Account, error) {
	// no recipient available
	if trx.To == nil {
		return nil, nil
	}

	// get the recipient by address
	acc, err := repository.R().Account(trx.To)
	if err != nil {
		return nil, err
	}

	return NewAccount(acc), nil
}

// Block resolves block the transaction is bundled in, nil if it's pending and not added to a block yet.
func (trx *Transaction) Block() (*Block, error) {
	// no recipient available
	if trx.BlockNumber == nil {
		return nil, nil
	}

	// get the sender by address
	blk, err := repository.R().BlockByNumber(trx.BlockNumber)
	if err != nil {
		return nil, err
	}

	return NewBlock(blk), nil
}

// tokenTransactions loads list of all token transaction related to this transaction call.
func (trx *Transaction) tokenTransactions() ([]*types.TokenTransaction, error) {
	// call for it only once
	val, err, _ := trx.cg.Do("erc", func() (interface{}, error) {
		log.Noticef("Loading ERC list for %s", trx.Hash.String())
		return repository.R().TokenTransactionsByCall(&trx.Hash)
	})
	if err != nil {
		return nil, err
	}
	return val.([]*types.TokenTransaction), nil
}

// TokenTransactions resolves list of all generic token transactions involved
// with the base transaction call.
func (trx *Transaction) TokenTransactions() ([]*TokenTransaction, error) {
	// get all the transaction
	tl, err := trx.tokenTransactions()
	if err != nil {
		return nil, err
	}

	// convert to resolvable
	list := make([]*TokenTransaction, len(tl))
	for i, tx := range tl {
		list[i] = NewTokenTransaction(tx)
	}
	return list, nil
}

// Erc20Transactions resolves list of ERC-20 transactions executed in the scope
// of this general transaction function call.
func (trx *Transaction) Erc20Transactions() ([]*ERC20Transaction, error) {
	// get all the transaction
	tl, err := trx.tokenTransactions()
	if err != nil {
		return nil, err
	}

	list := make([]*ERC20Transaction, 0)
	for _, tx := range tl {
		if tx.TokenType == types.AccountTypeERC20Token {
			list = append(list, NewErc20Transaction(tx))
		}
	}
	return list, nil
}

// Erc721Transactions resolves list of ERC-721 transactions executed in the scope
// of this general transaction function call.
func (trx *Transaction) Erc721Transactions() ([]*ERC721Transaction, error) {
	// get all the transaction
	tl, err := trx.tokenTransactions()
	if err != nil {
		return nil, err
	}

	list := make([]*ERC721Transaction, 0)
	for _, tx := range tl {
		if tx.TokenType == types.AccountTypeERC721Contract {
			list = append(list, NewErc721Transaction(tx))
		}
	}
	return list, nil
}

// Erc1155Transactions resolves list of ERC-155 transactions executed in the scope
// of this general transaction function call.
func (trx *Transaction) Erc1155Transactions() ([]*ERC1155Transaction, error) {
	// get all the transaction
	tl, err := trx.tokenTransactions()
	if err != nil {
		return nil, err
	}

	list := make([]*ERC1155Transaction, 0)
	for _, tx := range tl {
		if tx.TokenType == types.AccountTypeERC1155Contract {
			list = append(list, NewErc1155Transaction(tx))
		}
	}
	return list, nil
}

// InternalTransaction represents a resolvable internal transaction structure.
type InternalTransaction struct {
	types.InternalTransaction
}

// NewInternalTransaction builds a new resolvable internal transaction structure.
func NewInternalTransaction(itx *types.InternalTransaction) *InternalTransaction {
	return &InternalTransaction{*itx}
}

// InternalTransactions resolves the list of internal transactions for this transaction.
func (trx *Transaction) InternalTransactions() ([]*InternalTransaction, error) {
	// Use debug_traceTransaction to get internal transactions
	result, err := repository.R().TraceTransaction(trx.Hash, nil)
	if err != nil {
		return nil, err
	}

	// The result is expected to be a map with a "structLogs" or "result" key, or a list of traces (depending on node config)
	// For OpenEthereum/Parity style, it's a list of traces under "result"
	// For Geth, it's a map with "structLogs" (not suitable for internal txs)
	// We'll expect the "result" key as a list of traces
	var traces []map[string]interface{}
	switch v := result.(type) {
	case map[string]interface{}:
		if arr, ok := v["result"].([]interface{}); ok {
			for _, item := range arr {
				if trace, ok := item.(map[string]interface{}); ok {
					traces = append(traces, trace)
				}
			}
		} else if _, ok := v["structLogs"].([]interface{}); ok {
			// Not supported for internal txs, return empty
			return []*InternalTransaction{}, nil
		}
	case []interface{}:
		for _, item := range v {
			if trace, ok := item.(map[string]interface{}); ok {
				traces = append(traces, trace)
			}
		}
	}

	var internalTxs []*InternalTransaction
	for _, trace := range traces {
		itx := &types.InternalTransaction{}
		if from, ok := trace["from"].(string); ok {
			itx.From = common.HexToAddress(from)
		}
		if to, ok := trace["to"].(string); ok {
			addr := common.HexToAddress(to)
			itx.To = &addr
		}
		if value, ok := trace["value"].(string); ok {
			itx.Value = (hexutil.Big)(*hexutil.MustDecodeBig(value))
		}
		if gas, ok := trace["gas"].(string); ok {
			gasVal := hexutil.MustDecodeUint64(gas)
			itx.Gas = hexutil.Uint64(gasVal)
		}
		if gasUsed, ok := trace["gasUsed"].(string); ok {
			guVal := hexutil.MustDecodeUint64(gasUsed)
			gu := hexutil.Uint64(guVal)
			itx.GasUsed = &gu
		}
		if input, ok := trace["input"].(string); ok {
			itx.Input = common.FromHex(input)
		}
		if typ, ok := trace["type"].(string); ok {
			itx.Type = typ
		}
		if traceAddr, ok := trace["traceAddress"].([]interface{}); ok {
			for _, idx := range traceAddr {
				if i, ok := idx.(float64); ok {
					itx.TraceAddress = append(itx.TraceAddress, int(i))
				}
			}
		}
		if errStr, ok := trace["error"].(string); ok {
			itx.Error = &errStr
		}
		internalTxs = append(internalTxs, NewInternalTransaction(itx))
	}
	return internalTxs, nil
}
