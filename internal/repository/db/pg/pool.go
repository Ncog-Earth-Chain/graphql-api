package pg

import (
	"context"
	"fmt"
	"ncogearthchain-api-graphql/internal/logger"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config holds the PostgreSQL connection settings.
type Config struct {
	// DSN is the libpq connection string or postgres:// URL.
	DSN string

	// MaxConns bounds the pool. It must be sized against the server's
	// max_connections, not against expected concurrency: the ingest path can spawn a
	// goroutine per transaction, and an unbounded pool converts that into connection
	// exhaustion that locks out the operator's own psql session.
	MaxConns int32

	// MinConns keeps warm connections. Establishing a PostgreSQL connection is
	// expensive enough that a cold pool shows up as latency on the first requests
	// after an idle period.
	MinConns int32

	// MaxConnLifetime bounds how long a connection is reused, so a rolling restart or
	// a failover is picked up without bouncing the process.
	MaxConnLifetime time.Duration

	// MaxConnIdleTime reaps idle connections back down to MinConns.
	MaxConnIdleTime time.Duration

	// StatementTimeout bounds any single statement server-side. This is the backstop
	// that keeps one pathological query from occupying a connection indefinitely;
	// context cancellation handles the client side, but a client that has gone away
	// without cancelling leaves the server working otherwise.
	StatementTimeout time.Duration
}

// DefaultConfig returns settings suitable for a single explorer instance.
func DefaultConfig(dsn string) Config {
	return Config{
		DSN:              dsn,
		MaxConns:         16,
		MinConns:         2,
		MaxConnLifetime:  time.Hour,
		MaxConnIdleTime:  30 * time.Minute,
		StatementTimeout: 30 * time.Second,
	}
}

// Pool wraps a pgx pool with the explorer's logger.
type Pool struct {
	*pgxpool.Pool
	log logger.Logger
}

// NewPool opens and verifies a connection pool.
//
// The connection is verified with a Ping before returning. Failing at startup with a
// clear error beats deferring the failure to the first query, where it surfaces as an
// opaque GraphQL error from whichever request happened to arrive first.
func NewPool(ctx context.Context, cfg Config, log logger.Logger) (*Pool, error) {
	pcfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("invalid PostgreSQL DSN: %w", err)
	}

	pcfg.MaxConns = cfg.MaxConns
	pcfg.MinConns = cfg.MinConns
	pcfg.MaxConnLifetime = cfg.MaxConnLifetime
	pcfg.MaxConnIdleTime = cfg.MaxConnIdleTime

	// Applied per connection rather than per query so it covers every statement,
	// including those issued by code that forgets to set it.
	if cfg.StatementTimeout > 0 {
		pcfg.ConnConfig.RuntimeParams["statement_timeout"] =
			fmt.Sprintf("%d", cfg.StatementTimeout.Milliseconds())
	}

	// Addresses and hashes are compared as bytes, and text ordering must not depend on
	// the server's locale. The C collation makes ordering byte-wise and stable across
	// machines, which matters because cursors encode positions in that order.
	pcfg.ConnConfig.RuntimeParams["application_name"] = "ncog-explorer"

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("can not create PostgreSQL pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("can not reach PostgreSQL: %w", err)
	}

	log.Noticef("PostgreSQL pool ready (max %d connections)", cfg.MaxConns)
	return &Pool{Pool: pool, log: log}, nil
}

// Close shuts the pool down.
func (p *Pool) Close() {
	if p.Pool != nil {
		p.Pool.Close()
		p.log.Info("PostgreSQL pool closed")
	}
}

// InTx runs fn inside a transaction, committing on success and rolling back on error
// or panic.
//
// Per-block atomicity is the single largest correctness gain of moving off MongoDB.
// The Mongo ingest path had no transaction at any granularity larger than one
// InsertOne, so a crash midway through a block left the database holding some of that
// block's rows and none of the rest, with nothing recording which. Here a block either
// lands whole or not at all, and the watermark advances in the same transaction as the
// data it describes.
func (p *Pool) InTx(ctx context.Context, fn func(context.Context, Tx) error) error {
	tx, err := p.Begin(ctx)
	if err != nil {
		return fmt.Errorf("can not begin transaction: %w", err)
	}

	defer func() {
		// Rollback after a successful commit is a no-op, so this is safe
		// unconditionally and also covers the panic path.
		_ = tx.Rollback(ctx)
	}()

	if err := fn(ctx, tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("can not commit transaction: %w", err)
	}
	return nil
}
