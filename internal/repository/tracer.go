package repository

import (
	"context"
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
	return p.rpc.TraceBlockByNumber(ctx, number, params)
}

func (p *proxy) TraceBlockByHash(ctx context.Context, hash common.Hash, params map[string]interface{}) (interface{}, error) {
	return p.rpc.TraceBlockByHash(ctx, hash, params)
}

func (p *proxy) TraceTransaction(ctx context.Context, hash common.Hash, params map[string]interface{}) (interface{}, error) {
	return p.rpc.TraceTransaction(ctx, hash, params)
}
