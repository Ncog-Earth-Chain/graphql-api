package repository

import (
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Tracer RPC methods
func (p *proxy) TraceBlock(hash common.Hash) (*types.TraceBlockResponse, error) {
	return p.rpc.TraceBlock(hash)
}

func (p *proxy) TraceBlockByNumber(number hexutil.Uint64) (*types.TraceBlockResponse, error) {
	return p.rpc.TraceBlockByNumber(number)
}

func (p *proxy) TraceBlockByHash(hash common.Hash) (*types.TraceBlockResponse, error) {
	return p.rpc.TraceBlockByHash(hash)
}

func (p *proxy) TraceTransaction(hash common.Hash) (*types.TraceBlockResponse, error) {
	return p.rpc.TraceTransaction(hash)
}
