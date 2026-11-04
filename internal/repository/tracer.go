package repository

import (
	"context"
	"errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Tracer RPC methods.
//
// These take a caller context because they are the most expensive thing this API can ask
// the node to do -- each one re-executes transactions. A client that disconnects mid-trace
// must stop that work rather than leave the node computing a result nobody will read; with
// context.Background() it could not.
func (p *proxy) TraceBlockByNumber(ctx context.Context, number hexutil.Uint64, params map[string]interface{}) (interface{}, error) {
	block, err := p.traceBlockForPolicy(ctx, "eth_getBlockByNumber", number)
	if err != nil {
		return nil, err
	}
	if *block.Number != number {
		return nil, errors.New("trace block number differs from the requested block")
	}
	if err := p.requireTraceReadable(ctx, *block.Transactions); err != nil {
		return nil, err
	}
	// Trace the block whose members were checked, even if a number lookup would
	// now resolve differently. Never fall back to the mutable number.
	return p.rpc.TraceBlockByHash(ctx, *block.Hash, params)
}

func (p *proxy) TraceBlockByHash(ctx context.Context, hash common.Hash, params map[string]interface{}) (interface{}, error) {
	block, err := p.traceBlockForPolicy(ctx, "eth_getBlockByHash", hash)
	if err != nil {
		return nil, err
	}
	if *block.Hash != hash {
		return nil, errors.New("trace block hash differs from the requested block")
	}
	if err := p.requireTraceReadable(ctx, *block.Transactions); err != nil {
		return nil, err
	}
	return p.rpc.TraceBlockByHash(ctx, hash, params)
}

func (p *proxy) TraceTransaction(ctx context.Context, hash common.Hash, params map[string]interface{}) (interface{}, error) {
	if err := p.requireTraceReadable(ctx, []common.Hash{hash}); err != nil {
		return nil, err
	}
	return p.rpc.TraceTransaction(ctx, hash, params)
}

// InternalTransactionTrace is exclusively the source for the typed internal-call
// view and its availability flag. Its caller MUST extract the permitted metadata
// and apply Transaction.redactInternalInputs; this result must never be returned
// as JSONAny or used by a raw trace root. Keeping this path separate preserves the
// existing safe internal view when a raw trace must be refused.
func (p *proxy) InternalTransactionTrace(ctx context.Context, hash common.Hash) (interface{}, error) {
	return p.rpc.TraceTransaction(ctx, hash, map[string]interface{}{"tracer": "callTracer"})
}
