package repository

import (
	"context"

	"ncogearthchain-api-graphql/internal/repository/db/pg"
	"ncogearthchain-api-graphql/internal/types"
)

// DdbOperations returns the on-chain DDB operation history.
//
// The node serves no DDB history RPC, so this is the only place it exists: the records
// were captured from the commit-transaction stream at ingest and cannot be recovered
// afterwards.
func (p *proxy) DdbOperations(ctx context.Context, c pg.DdbOpCriteria, cursor *string, count int32) ([]*types.DdbOperation, error) {
	return p.pg.DdbOperations(ctx, c, derefCursor(cursor), count)
}

// DdbOperationAt returns the DDB operation committed at a block position.
func (p *proxy) DdbOperationAt(ctx context.Context, blockNumber, txIndex uint64) (*types.DdbOperation, error) {
	return p.pg.DdbOperationAt(ctx, blockNumber, txIndex)
}

// DdbContracts returns the known data contracts, most recently active first.
func (p *proxy) DdbContracts(ctx context.Context, count int32) ([]*types.DdbContract, error) {
	return p.pg.DdbContracts(ctx, count)
}
