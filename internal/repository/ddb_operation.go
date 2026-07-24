package repository

import (
	"context"

	"ncogearthchain-api-graphql/internal/repository/db/pg"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
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
func (p *proxy) DdbContracts(ctx context.Context, cursor *string, count int32) ([]*types.DdbContract, error) {
	return p.pg.DdbContracts(ctx, derefCursor(cursor), count)
}

// DdbContract returns a single data contract by address, or nil if unknown.
func (p *proxy) DdbContract(ctx context.Context, addr common.Address) (*types.DdbContract, error) {
	return p.pg.DdbContract(ctx, addr)
}

// DdbStateHashChainValid reports whether a contract's per-operation state-hash chain is
// continuous (each operation's priorPostStateHash equals the previous op's postStateHash).
func (p *proxy) DdbStateHashChainValid(ctx context.Context, addr common.Address) (bool, error) {
	return p.pg.DdbStateHashChainValid(ctx, addr)
}
