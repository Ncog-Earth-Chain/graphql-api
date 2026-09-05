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

// Healthy reports API readiness: PostgreSQL is reachable, the node RPC answers, and the
// ingest watermark is queryable. It returns the contiguous head (the highest block below
// which nothing is missing) so a probe can also observe ingest progress. A load balancer
// should cut over on this rather than issuing a real GraphQL query.
//
// THE NODE PROBE IS NOT OPTIONAL, and leaving it out was measured to be actively
// misleading. rpc.Dial over http:// is LAZY -- it connects on first use -- so an apiserver
// pointed at a dead chain starts cleanly, logs "node connection open" against a black hole,
// and this function returned 200 while every resolver answered "internal server error".
// Everything downstream inherited that lie: the container reported healthy, `compose up
// --wait` returned 0 in 13s, and the systemd unit that argues --wait "is the part that
// makes this unit honest" would report active on a stack that can serve nothing. A load
// balancer cutting over on /health -- which is exactly what the packaging tells operators
// to do -- would route traffic straight to it.
func (p *proxy) Healthy(ctx context.Context) (uint64, error) {
	if err := p.pg.Ping(ctx); err != nil {
		return 0, fmt.Errorf("database unreachable: %w", err)
	}
	// Bounded by the caller's context, which the health handler already gives a short
	// deadline. BlockHeight is the cheapest call that proves the node is really answering.
	if _, err := p.rpc.BlockHeight(); err != nil {
		return 0, fmt.Errorf("node RPC unreachable: %w", err)
	}
	return p.pg.ContiguousHead(ctx)
}

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

	// Report what the runway actually looks like afterwards.
	//
	// partition_health() shipped with the partition machinery in 00004 and nothing ever
	// called it, so the condition this whole job exists to prevent -- rows landing in the
	// catch-all DEFAULT partition because the runway ran out -- was visible only to someone
	// running psql. That is how a wedged runway could sit behind an error line repeated
	// every 12 hours and be missed. A failure to READ health must not fail the maintenance
	// pass that just succeeded, so it is logged and swallowed.
	health, err := p.pg.PartitionHealth(ctx)
	if err != nil {
		p.log.Errorf("can not read partition health: %s", err.Error())
		return nil
	}
	for _, h := range health {
		if h.DefaultRows > 0 {
			// Not Noticef: rows in the default partition are correct and queryable but
			// unprunable and unindexed by range, and they are the precursor to the wedge.
			p.log.Warningf("%s has %d row(s) in its DEFAULT partition (%d partition(s) of runway ahead); "+
				"range-filtered queries scan all of them",
				h.Table, h.DefaultRows, h.PartitionsAhead)
		}
		if h.PartitionsAhead <= 0 {
			p.log.Errorf("%s has NO partition runway ahead; further rows will land in its default partition", h.Table)
		}
		if h.OrphanAttached > 0 || h.OrphanDetached > 0 {
			p.log.Warningf("%s has %d attached and %d detached partition(s) missing from partition_registry",
				h.Table, h.OrphanAttached, h.OrphanDetached)
		}
	}
	return nil
}
