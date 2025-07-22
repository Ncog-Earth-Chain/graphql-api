package resolvers

import (
	"ncogearthchain-api-graphql/internal/repository"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// TraceBlock returns the tracing of a full block by hash.
func (rs *rootResolver) TraceBlock(args struct{ Hash common.Hash }) (*types.TraceBlockResponse, error) {
	return repository.R().TraceBlock(args.Hash)
}

// TraceBlockByNumber returns the tracing of a block by its number.
func (rs *rootResolver) TraceBlockByNumber(args struct{ Number hexutil.Uint64 }) (*types.TraceBlockResponse, error) {
	return repository.R().TraceBlockByNumber(args.Number)
}

// TraceBlockByHash is identical to TraceBlock (kept for symmetry).
func (rs *rootResolver) TraceBlockByHash(args struct{ Hash common.Hash }) (*types.TraceBlockResponse, error) {
	return repository.R().TraceBlockByHash(args.Hash)
}

// TraceTransaction returns the tracing of a single transaction.
func (rs *rootResolver) TraceTransaction(args struct{ Hash common.Hash }) (*types.TraceTransactionResponse, error) {
	return repository.R().TraceTransaction(args.Hash)
}
