// Package resolvers implements GraphQL resolvers to incoming API requests.
package resolvers

import (
	"context"
	"fmt"
	"math/big"
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
func (rs *rootResolver) Transaction(ctx context.Context, args *struct{ Hash common.Hash }) (tx *Transaction, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Criticalf("transaction loader crashed on %s", args.Hash.String())
			err = fmt.Errorf("failed to load transaction %s", args.Hash.String())
			tx = nil
		}
	}()

	// get the transaction from repository
	trx, err := repository.R().IndexedTransaction(ctx, &args.Hash)
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
func (trx *Transaction) Sender(ctx context.Context) (*Account, error) {
	// get the sender by address
	acc, err := repository.R().Account(ctx, &trx.From)
	if err != nil {
		return nil, err
	}

	return NewAccount(acc), nil
}

// Recipient resolves recipient's account of the transaction.
func (trx *Transaction) Recipient(ctx context.Context) (*Account, error) {
	// no recipient available
	if trx.To == nil {
		return nil, nil
	}

	// get the recipient by address
	acc, err := repository.R().Account(ctx, trx.To)
	if err != nil {
		return nil, err
	}

	return NewAccount(acc), nil
}

// Block resolves block the transaction is bundled in, nil if it's pending and not added to a block yet.
// Served from the index. This is a per-EDGE resolver on every transaction list, so it fired
// once per transaction on the page, each time an eth_getBlockByNumber for a row the block
// table holds on its primary key.
func (trx *Transaction) Block(ctx context.Context) (*Block, error) {
	// no recipient available
	if trx.BlockNumber == nil {
		return nil, nil
	}

	// get the sender by address
	blk, err := repository.R().IndexedBlockByNumber(ctx, trx.BlockNumber)
	if err != nil {
		return nil, err
	}

	return NewBlock(blk), nil
}

// tokenTransactions loads list of all token transaction related to this transaction call.
func (trx *Transaction) tokenTransactions(ctx context.Context) ([]*types.TokenTransaction, error) {
	// call for it only once
	val, err, _ := trx.cg.Do("erc", func() (interface{}, error) {
		log.Noticef("Loading ERC list for %s", trx.Hash.String())
		return repository.R().TokenTransactionsByCall(ctx, &trx.Hash)
	})
	if err != nil {
		return nil, err
	}
	return val.([]*types.TokenTransaction), nil
}

// TokenTransactions resolves list of all generic token transactions involved
// with the base transaction call.
func (trx *Transaction) TokenTransactions(ctx context.Context) ([]*TokenTransaction, error) {
	// get all the transaction
	tl, err := trx.tokenTransactions(ctx)
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
func (trx *Transaction) Erc20Transactions(ctx context.Context) ([]*ERC20Transaction, error) {
	// get all the transaction
	tl, err := trx.tokenTransactions(ctx)
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
func (trx *Transaction) Erc721Transactions(ctx context.Context) ([]*ERC721Transaction, error) {
	// get all the transaction
	tl, err := trx.tokenTransactions(ctx)
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
func (trx *Transaction) Erc1155Transactions(ctx context.Context) ([]*ERC1155Transaction, error) {
	// get all the transaction
	tl, err := trx.tokenTransactions(ctx)
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

// trace runs debug_traceTransaction for this transaction, at most once per resolved transaction
// however many fields ask for it. A nil result with a nil error means the connected node does not
// serve the optional `debug` namespace, so nothing about internal calls can be determined.
func (trx *Transaction) trace(ctx context.Context) (interface{}, error) {
	val, err, _ := trx.cg.Do("trace", func() (interface{}, error) {
		return repository.R().TraceTransaction(ctx, trx.Hash, map[string]interface{}{
			"tracer": "callTracer",
		})
	})
	if err != nil {
		return nil, err
	}
	return val, nil
}

// TracingAvailable reports whether the connected node could trace this transaction at all.
//
// This exists because an empty internalTransactions list is ambiguous: it means either "this
// transaction genuinely made no internal calls" or "nobody could tell". graphql-go v1.4 renders a
// nil Go slice and an empty one identically as [], so the distinction cannot live in that field --
// read this flag alongside it. False => internalTransactions is [] for lack of a tracer, and must
// NOT be presented as "no internal transactions".
func (trx *Transaction) TracingAvailable(ctx context.Context) (bool, error) {
	result, err := trx.trace(ctx)
	if err != nil {
		return false, err
	}
	return result != nil, nil
}

// InternalTransactions resolves the list of internal transactions for this transaction.
//
// Always read together with tracingAvailable -- see the note there on why [] is ambiguous.
func (trx *Transaction) InternalTransactions(ctx context.Context) ([]*InternalTransaction, error) {
	result, err := trx.trace(ctx)
	if err != nil {
		return nil, err
	}
	// The node does not serve `debug`. Nothing is determinable; tracingAvailable reports false.
	if result == nil {
		return []*InternalTransaction{}, nil
	}

	// Geth callTracer output: top-level map with "calls"
	if v, ok := result.(map[string]interface{}); ok {
		if calls, ok := v["calls"].([]interface{}); ok {
			var internalTxs []*InternalTransaction
			for i, c := range calls {
				if callMap, ok := c.(map[string]interface{}); ok {
					idx := hexutil.Big(*big.NewInt(int64(i)))
					internalTxs = append(internalTxs, extractInternalTxs(callMap, []hexutil.Big{idx})...)
				}
			}
			return internalTxs, nil
		}
	}

	// Fallback to OpenEthereum/Parity style (flat result)
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
			// structLogs carries no call frames, so nothing can be determined from it.
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
					bi := big.NewInt(int64(i))
					itx.TraceAddress = append(itx.TraceAddress, hexutil.Big(*bi))
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

// extractInternalTxs recursively extracts internal transactions from a callTracer call tree.
func extractInternalTxs(trace map[string]interface{}, parentTraceAddress []hexutil.Big) []*InternalTransaction {
	var internalTxs []*InternalTransaction

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
	// Compose trace address
	if idx, ok := trace["traceAddress"].([]interface{}); ok {
		for _, i := range idx {
			if n, ok := i.(float64); ok {
				bi := big.NewInt(int64(n))
				itx.TraceAddress = append(itx.TraceAddress, hexutil.Big(*bi))
			}
		}
	} else if len(parentTraceAddress) > 0 {
		itx.TraceAddress = append([]hexutil.Big{}, parentTraceAddress...)
	}
	if errStr, ok := trace["error"].(string); ok {
		itx.Error = &errStr
	}
	internalTxs = append(internalTxs, NewInternalTransaction(itx))

	// Recursively process child calls
	if calls, ok := trace["calls"].([]interface{}); ok {
		for i, c := range calls {
			if callMap, ok := c.(map[string]interface{}); ok {
				idx := hexutil.Big(*big.NewInt(int64(i)))
				childTraceAddress := append(append([]hexutil.Big{}, itx.TraceAddress...), idx)
				internalTxs = append(internalTxs, extractInternalTxs(callMap, childTraceAddress)...)
			}
		}
	}
	return internalTxs
}
