package repository

import (
	"context"

	"ncogearthchain-api-graphql/internal/repository/db/pg"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
)

// Logs returns event logs matching the criteria, newest first.
//
// The criteria are a typed struct rather than a free-form filter deliberately: every
// combination it can express is served by an index on tx_log, so a client cannot compose
// a query that scans the largest table in the database.
func (p *proxy) Logs(ctx context.Context, c pg.LogCriteria, cursor *string, count int32) ([]*types.Log, error) {
	return p.pg.Logs(ctx, c, derefCursor(cursor), count)
}

// LogsByTransaction returns every log a transaction emitted, in emission order.
func (p *proxy) LogsByTransaction(ctx context.Context, txHash *common.Hash) ([]*types.Log, error) {
	return p.pg.LogsByTransaction(ctx, txHash)
}
