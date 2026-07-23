package pg

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Querier is the query surface shared by the pool and a transaction.
//
// Helpers take a Querier so the same function works standalone or composed into a
// block-level transaction. Under MongoDB every write was its own round trip and code
// needing several writes to land together had no way to express it, so this is what
// makes per-block atomicity expressible without duplicating each helper.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	CopyFrom(ctx context.Context, table pgx.Identifier, cols []string, src pgx.CopyFromSource) (int64, error)
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}

// Tx is a Querier that can also be committed or rolled back.
type Tx interface {
	Querier
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// compile-time proof that both concrete types satisfy the interfaces, so a pgx
// signature change is a build failure here rather than at every call site.
var (
	_ Querier = (*Pool)(nil)
	_ Tx      = (pgx.Tx)(nil)
)
