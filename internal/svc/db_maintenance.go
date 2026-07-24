// Package svc implements blockchain data processing services.
package svc

import (
	"fmt"
	"time"
)

const (
	// accountStatRefreshPeriod is how often the account_stat materialized view is rebuilt.
	// It backs account transaction counts and the "most active" token lists, which stay
	// frozen at migration until this runs, so it is refreshed frequently. The refresh is
	// CONCURRENT and so never blocks readers.
	accountStatRefreshPeriod = 5 * time.Minute

	// partitionMaintenancePeriod is how often the partition runway is extended and history
	// past retention is pruned. The runway is months wide, so a daily pass is ample; it
	// also runs once at startup so a long-idle deployment recovers immediately.
	partitionMaintenancePeriod = 12 * time.Hour
)

// dbMaintenance runs the recurring PostgreSQL housekeeping the schema needs but does not
// perform on its own: refreshing the account statistics view and keeping the time- and
// block-range partitions ahead of the chain head (and pruning them past retention).
//
// None of this ran before -- the routines existed on the store with no caller -- so an
// unattended deployment silently reported zero account transactions and, roughly a year
// in, began failing every gas-price insert as the seeded partition runway ran out.
type dbMaintenance struct {
	service
	statTicker      *time.Ticker
	partitionTicker *time.Ticker
}

// name returns a human-readable name of the service used by the manager.
func (dbm *dbMaintenance) name() string {
	return "db maintenance"
}

// run starts the database maintenance loop.
func (dbm *dbMaintenance) run() {
	if dbm.mgr == nil {
		panic(fmt.Errorf("no svc manager set on %s", dbm.name()))
	}
	dbm.mgr.started(dbm)
	go dbm.execute()
}

// close terminates the database maintenance loop.
func (dbm *dbMaintenance) close() {
	if dbm.statTicker != nil {
		dbm.statTicker.Stop()
		dbm.partitionTicker.Stop()
	}
	if dbm.sigStop != nil {
		dbm.sigStop <- true
	}
}

// execute performs regular ticker-based database maintenance.
func (dbm *dbMaintenance) execute() {
	defer func() {
		close(dbm.sigStop)
		dbm.mgr.finished(dbm)
	}()

	// run partition maintenance once up front so a fresh or long-idle deployment does not
	// wait a full interval for its first pass.
	go dbm.maintainPartitions()

	dbm.statTicker = time.NewTicker(accountStatRefreshPeriod)
	dbm.partitionTicker = time.NewTicker(partitionMaintenancePeriod)

	for {
		select {
		case <-dbm.sigStop:
			return
		case <-dbm.statTicker.C:
			go dbm.refreshAccountStats()
		case <-dbm.partitionTicker.C:
			go dbm.maintainPartitions()
		}
	}
}

// refreshAccountStats rebuilds the account statistics materialized view.
func (dbm *dbMaintenance) refreshAccountStats() {
	if err := repo.RefreshAccountStats(bgCtx()); err != nil {
		log.Errorf("can not refresh account statistics; %s", err.Error())
	}
}

// maintainPartitions extends the partition runway ahead of the current head and prunes
// history past retention.
func (dbm *dbMaintenance) maintainPartitions() {
	ctx := bgCtx()

	// the head the store has actually ingested; partitions are extended ahead of it.
	head, err := repo.LastKnownBlock(ctx)
	if err != nil {
		log.Errorf("can not read last known block for partition maintenance; %s", err.Error())
		return
	}

	if err := repo.MaintainPartitions(ctx, head); err != nil {
		log.Errorf("partition maintenance failed; %s", err.Error())
	}
}
