package pg

import (
	"context"
	"fmt"
	"ncogearthchain-api-graphql/internal/logger"
)

// Store is the PostgreSQL implementation of the explorer's persistence layer.
//
// It replaces db.MongoDbBridge. The method set is kept deliberately close to the
// bridge's so the repository layer above changes as little as possible, with two
// intentional differences:
//
//   - every method takes a context.Context. The MongoDB layer used
//     context.Background() in 123 places, so no query was cancellable and a client that
//     disconnected left the database working on a result nobody would read.
//   - filters are typed (see filter.go) rather than bson.D documents built by the
//     caller. That is what makes the storage layer swappable at all; with query
//     documents built above the seam, it was not.
type Store struct {
	pool *Pool
	log  logger.Logger
}

// NewStore creates the PostgreSQL store over an existing pool.
func NewStore(pool *Pool, log logger.Logger) *Store {
	return &Store{pool: pool, log: log}
}

// Pool exposes the underlying pool for callers that need a transaction, notably the
// per-block ingest path.
func (s *Store) Pool() *Pool { return s.pool }

// Close releases the pool.
func (s *Store) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

// Ping verifies the database connection is alive. It backs the /health probe, which needs a
// cheap readiness signal that does not run a real query.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// counter reads a meta_counter value.
//
// meta_counter holds the small scalars that must move in step with the data they
// describe -- most importantly contiguous_head, the ingest watermark, which is advanced
// inside the same transaction that writes a block so it can never claim progress the
// data does not back.
func (s *Store) counter(ctx context.Context, q Querier, key string) (int64, error) {
	var v int64
	err := q.QueryRow(ctx, `SELECT value::BIGINT FROM meta_counter WHERE key = $1`, key).Scan(&v)
	if err != nil {
		return 0, fmt.Errorf("can not read counter %q: %w", key, err)
	}
	return v, nil
}

// setCounter writes a meta_counter value.
func (s *Store) setCounter(ctx context.Context, q Querier, key string, value int64) error {
	_, err := q.Exec(ctx, `
		INSERT INTO meta_counter (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
		key, value)
	if err != nil {
		return fmt.Errorf("can not write counter %q: %w", key, err)
	}
	return nil
}
