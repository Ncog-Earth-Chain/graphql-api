package pg

import (
	"context"
	"testing"
)

// TestBlockPartitionCreationReclaimsPoisonedDefault pins the exit from a one-way door.
//
// PostgreSQL validates every new partition bound against the DEFAULT partition's contents,
// so a single row sitting in tx_log_default inside a range the maintainer later tries to
// cover makes
//
//	CREATE TABLE tx_log_pN PARTITION OF tx_log FOR VALUES FROM (lo) TO (hi)
//
// fail with "updated partition constraint for default partition would be violated by some
// row" -- permanently. ensure_block_partitions has no EXCEPTION block, so the error reached
// svc/db_maintenance.go, which only logs it and retries the identical doomed statement every
// 12 hours forever; the gas-price retention prune is the step after it and stopped running
// too. The safety net had become the wedge.
//
// Worse, the old function created up to ahead_target partitions inside ONE transaction, so
// the poisoned range rolled its CLEAN siblings back with it. Reproduced against the original
// 00004 body with one row at block 87: the call that should have created p5..p8 left zero
// behind, and the table still had only p0..p4 plus the default.
//
// Since 00013 the function creates one partition per call and, when a range is poisoned,
// moves the offending rows out of the default and into the new partition before attaching
// it. This asserts all three properties that matters: the runway gets built, the default is
// drained, and no row is lost.
//
// The width is narrowed for the test so the trap is reachable without generating a million
// blocks; the existing partitions are dropped first so the narrower ranges cannot overlap
// them.
func TestBlockPartitionCreationReclaimsPoisonedDefault(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	// Narrow tx_log to 10-block partitions. EVERY existing partition has to go first, and
	// they are enumerated from the catalog rather than named: a surviving p0 spanning
	// 0..1,000,000 makes the narrow attach fail with "would overlap partition tx_log_p0",
	// and a surviving p8 would already cover the range this test needs to leave uncovered,
	// silently disarming the trap instead of failing.
	if _, err := s.pool.Exec(ctx, `
		DO $$
		DECLARE p TEXT;
		BEGIN
		    FOR p IN
		        SELECT c.relname FROM pg_class c
		        JOIN   pg_inherits i ON i.inhrelid = c.oid
		        WHERE  i.inhparent = 'tx_log'::regclass
		          AND  c.relname <> 'tx_log_default'
		    LOOP
		        EXECUTE format('DROP TABLE %I', p);
		    END LOOP;
		END $$`); err != nil {
		t.Fatalf("drop existing tx_log partitions: %v", err)
	}
	for _, q := range []string{
		`DELETE FROM tx_log_default`,
		`DELETE FROM partition_registry WHERE table_name = 'tx_log'`,
		`UPDATE partition_config SET block_width = 10, ahead_target = 4 WHERE table_name = 'tx_log'`,
	} {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			t.Fatalf("narrow tx_log partitions (%s): %v", q, err)
		}
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(),
			`UPDATE partition_config SET block_width = 1000000, ahead_target = 4 WHERE table_name = 'tx_log'`)
	})

	// POISON: a log row at block 87, with no partition covering 80..90, lands in the default.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO tx_log (block_number, log_index, tx_index, tx_hash, address,
		                    topic0, topic_count, data, removed, ts)
		VALUES (87, 0, 0, decode(lpad('57', 64, '0'), 'hex'), decode(lpad('01', 40, '0'), 'hex'),
		        decode(lpad('09', 64, '0'), 'hex'), 1, ''::bytea, false, now())`); err != nil {
		t.Fatalf("insert the poisoning log row: %v", err)
	}

	var inDefault int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM tx_log_default`).Scan(&inDefault); err != nil {
		t.Fatalf("count default: %v", err)
	}
	if inDefault != 1 {
		t.Fatalf("the fixture put %d row(s) in tx_log_default, want 1 -- the trap is not set up", inDefault)
	}

	// head 60 with ahead_target 4 at width 10 => cover up to block 100, i.e. across the
	// poisoned 80..90 range.
	made, err := s.EnsureBlockPartitions(ctx, "tx_log", 60)
	if err != nil {
		t.Fatalf("EnsureBlockPartitions over a poisoned range: %v", err)
	}
	if made == 0 {
		t.Fatalf("no partitions were created at all")
	}

	// 1. The poisoned range is now covered.
	var covered bool
	if err := s.pool.QueryRow(ctx,
		`SELECT to_regclass('tx_log_p8') IS NOT NULL`).Scan(&covered); err != nil {
		t.Fatalf("check p8: %v", err)
	}
	if !covered {
		t.Errorf("tx_log_p8 (blocks 80-90) was not created; the default partition is still a one-way door")
	}

	// 2. The default is drained -- otherwise the next range to overlap it wedges again.
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM tx_log_default`).Scan(&inDefault); err != nil {
		t.Fatalf("recount default: %v", err)
	}
	if inDefault != 0 {
		t.Errorf("tx_log_default still holds %d row(s) after the range covering them was created", inDefault)
	}

	// 3. NO ROW WAS LOST. Draining the default by deleting from it would satisfy the two
	//    assertions above and destroy data.
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM tx_log WHERE block_number = 87`).Scan(&total); err != nil {
		t.Fatalf("count the reclaimed row: %v", err)
	}
	if total != 1 {
		t.Errorf("the log row at block 87 is gone: found %d, want 1 -- rows were dropped, not moved", total)
	}

	// 4. The move is recorded rather than silent.
	var reclaimed int64
	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(sum(reclaimed_rows), 0) FROM partition_registry WHERE table_name = 'tx_log'`).
		Scan(&reclaimed); err != nil {
		t.Fatalf("read reclaimed_rows: %v", err)
	}
	if reclaimed != 1 {
		t.Errorf("partition_registry recorded %d reclaimed row(s), want 1", reclaimed)
	}
}

// TestPartitionHealthIsReadable covers the accessor that partition_health() never had.
//
// The function shipped in 00004 with no Go caller, so the condition the maintenance job
// exists to prevent -- rows accumulating in the catch-all DEFAULT partition -- was
// observable only by hand. MaintainPartitions now logs it; this asserts the read works and
// reports the tables it should.
func TestPartitionHealthIsReadable(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	health, err := s.PartitionHealth(ctx)
	if err != nil {
		t.Fatalf("PartitionHealth: %v", err)
	}
	if len(health) == 0 {
		t.Fatal("partition_health() reported no partitioned tables at all")
	}

	seen := map[string]bool{}
	for _, h := range health {
		seen[h.Table] = true
		if h.DefaultRows < 0 || h.PartitionsAhead < 0 {
			t.Errorf("%s reported negative counts: %+v", h.Table, h)
		}
	}
	if !seen["tx_log"] {
		t.Errorf("tx_log is missing from partition health; tables reported: %v", seen)
	}
}
