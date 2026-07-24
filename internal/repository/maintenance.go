package repository

import (
	"context"
	"fmt"
	"time"
)

// Database maintenance policy.
//
// These are the recurring housekeeping jobs the PostgreSQL schema needs but does not run
// on its own. The migrations seed a fixed partition runway once; nothing else advances it,
// so an unattended deployment eventually fails gas-price inserts (that table has no default
// partition) and writes tx_log rows into an unprunable default partition. The values below
// are deliberately generous and one-line tunable.
const (
	// gasPricePartitionRunwayMonths is how many months of gas-price partitions to keep
	// ahead of now. Must comfortably exceed the maintenance interval.
	gasPricePartitionRunwayMonths = 12

	// gasPriceRetentionMonths is how long gas-price history is kept before whole monthly
	// partitions are dropped. This replaces the old MongoDB TTL index; pruning is
	// destructive, so the window is intentionally long and every drop is logged.
	gasPriceRetentionMonths = 24

	// txLogPartitionTable is the block-range partitioned table whose runway we extend.
	txLogPartitionTable = "tx_log"
)

// RefreshAccountStats rebuilds the account_stat materialized view.
func (p *proxy) RefreshAccountStats(ctx context.Context) error {
	return p.pg.RefreshAccountStats(ctx)
}

// MaintainPartitions extends the partition runway ahead of head and prunes history past
// retention. Idempotent; safe to call on a schedule and once at startup.
func (p *proxy) MaintainPartitions(ctx context.Context, head uint64) error {
	// Gas-price time-range partitions are correctness-critical: the table has no default
	// partition, so a single missing month fails every gas-price insert.
	if made, err := p.pg.EnsureGasPricePartitions(ctx, time.Now(), gasPricePartitionRunwayMonths); err != nil {
		return fmt.Errorf("gas price partition runway: %w", err)
	} else if made > 0 {
		p.log.Noticef("created %d gas-price partition(s)", made)
	}

	// tx_log block-range partitions are a performance concern: rows past the runway land
	// in the default partition, which cannot be pruned and defeats the partition indexes.
	if made, err := p.pg.EnsureBlockPartitions(ctx, txLogPartitionTable, head); err != nil {
		return fmt.Errorf("tx_log partition runway: %w", err)
	} else if made > 0 {
		p.log.Noticef("created %d tx_log partition(s) ahead of block %d", made, head)
	}

	// Prune gas-price history past retention. The names come back so we can log them:
	// silently destroying data is not an acceptable outcome for a retention job.
	dropped, err := p.pg.PruneGasPricePartitions(ctx, gasPriceRetentionMonths)
	if err != nil {
		return fmt.Errorf("gas price retention: %w", err)
	}
	for _, name := range dropped {
		p.log.Noticef("pruned gas-price partition past %d-month retention: %s", gasPriceRetentionMonths, name)
	}
	return nil
}
