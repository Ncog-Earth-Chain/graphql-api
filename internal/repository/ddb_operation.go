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
//
// The METADATA is the explorer's to serve; the PAYLOAD is not. Every row's operation JSON goes
// back through the node's read verdict before it leaves here (SEC-07c, ddb_verdict.go), because
// the payload of a DDB commit IS the tenant's data -- the procedure arguments of a CallProcedure
// op are the row values -- and the index holds rows captured before the node withheld any of it.
func (p *proxy) DdbOperations(ctx context.Context, c pg.DdbOpCriteria, cursor *string, count int32) ([]*types.DdbOperation, error) {
	ops, err := p.pg.DdbOperations(ctx, c, derefCursor(cursor), count)
	if err != nil {
		return nil, err
	}
	p.ddbFilterOperations(ctx, ops)
	return ops, nil
}

// DdbOperationAt returns the DDB operation committed at a block position, with its payload filtered
// through the node's read verdict exactly as the list is.
func (p *proxy) DdbOperationAt(ctx context.Context, blockNumber, txIndex uint64) (*types.DdbOperation, error) {
	op, err := p.pg.DdbOperationAt(ctx, blockNumber, txIndex)
	if err != nil || op == nil {
		return op, err
	}
	p.ddbFilterOperations(ctx, []*types.DdbOperation{op})
	return op, nil
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
