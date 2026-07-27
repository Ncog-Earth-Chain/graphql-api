-- +goose Up
-- +goose StatementBegin

-- The DEFAULT partition was a one-way door. This adds the way out.
--
-- 00004 gave tx_log a DEFAULT partition so a row past the runway would land somewhere
-- recoverable instead of aborting the ingest block transaction. That part was right and
-- stays. What was missing is that PostgreSQL validates every new partition bound against
-- the default partition's contents: once a single row sits in tx_log_default inside a
-- range the maintainer later tries to cover,
--
--   CREATE TABLE tx_log_pN PARTITION OF tx_log FOR VALUES FROM (lo) TO (hi)
--
-- fails with "updated partition constraint for default partition would be violated by
-- some row" -- and keeps failing. Reproduced on postgres:16.14 with one row at block 87:
-- the call that should have created p5, p6, p7 and p8 left ZERO partitions behind,
-- because the old function created up to ahead_target partitions inside ONE transaction
-- and the poisoned range rolled its three clean siblings back with it.
--
-- ensure_block_partitions has no EXCEPTION block, so the error reaches
-- internal/svc/db_maintenance.go, which only LOGS it and retries the identical doomed
-- statement every 12 hours, forever. The gas-price retention prune is the step after the
-- failing one, so it stops running too. The safety net had become the wedge.
--
-- Two changes make it self-healing.
--
-- 1. CREATE DETACHED, EVACUATE, THEN ATTACH. The rows blocking the bound are moved out of
--    the default and into the new partition in the same transaction, so the validation
--    scan sees them as already gone. This is the only way to un-poison a range without
--    dropping data or dropping the default partition.
--
-- 2. ONE PARTITION PER CALL instead of ahead_target. The Go caller already loops until
--    this returns 0 and each call is its own transaction, so a range that cannot be
--    created now leaves every range below it committed rather than rolling them back.
--
-- ON LOCKING, PRECISELY -- an earlier draft of this comment claimed the reclaim path is
-- strictly less blocking than what it replaces, and that is NOT true in general. It is
-- true for the parent: CREATE TABLE ... PARTITION OF took ACCESS EXCLUSIVE on tx_log
-- itself, blocking every reader and writer of the table, whereas ATTACH PARTITION takes
-- only SHARE UPDATE EXCLUSIVE there. But this function also takes ACCESS EXCLUSIVE on the
-- DEFAULT partition before evacuating it, and an ingest transaction that is concurrently
-- inserting a log row routed to the default will block on that and can be chosen as a
-- deadlock victim. That is a real cost and it is accepted deliberately: rows only route to
-- the default when the runway has already been exhausted, which is precisely the wedged
-- state this exists to clear, and one aborted block is retried by the scanner on its next
-- pass. In the healthy case the default is empty, nothing routes to it, and the lock is
-- uncontended.
--
-- This is a NEW migration, not an edit to 00004. 00004 has already run on real databases
-- and goose records it by version, so editing it in place would change nothing on any
-- deployed database while silently diverging every freshly-created one from them. The only
-- objects that change are the function body (CREATE OR REPLACE, which 00004 itself used)
-- and one added column, so it re-applies identically to a fresh and to an already-wedged
-- database. A wedged database heals on its next maintenance pass -- which internal/svc
-- runs once at startup -- with no operator action.

-- Metadata-only on PostgreSQL 11+: a NOT NULL column with a constant default does not
-- rewrite the table.
ALTER TABLE partition_registry
    ADD COLUMN IF NOT EXISTS reclaimed_rows BIGINT NOT NULL DEFAULT 0;

-- +goose StatementEnd
-- +goose StatementBegin

-- Creates ONE missing partition for `tbl` per call, at the lowest uncovered index at or
-- above min_index and at or below (head + ahead_target * width). Returns 1 if it created
-- one, 0 when the runway is fully covered; the caller re-invokes until it returns 0.
--
-- One per call is deliberate: it is the transaction boundary. A failure on one block range
-- must not take the ranges already created below it, which is exactly what the
-- ahead_target-sized batch did when the default partition poisoned a bound.
CREATE OR REPLACE FUNCTION ensure_block_partitions(tbl TEXT, head BIGINT)
RETURNS INT
LANGUAGE plpgsql
AS $$
DECLARE
    cfg        partition_config%ROWTYPE;
    target_max BIGINT;
    idx        BIGINT;
    lo         BIGINT;
    hi         BIGINT;
    part       TEXT;
    dflt       TEXT;
    keycol     TEXT;
    cols       TEXT;
    moved      BIGINT;
BEGIN
    SELECT * INTO cfg FROM partition_config WHERE table_name = tbl;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'no partition_config row for table %', tbl;
    END IF;

    -- The partition key is read from the catalog rather than hard-coded to 'block_number'.
    -- The evacuation DELETE has to name the key column, and a wrong guess would either
    -- error or -- far worse -- delete by the wrong predicate. partattrs is an int2vector
    -- and 0-subscripted; partnatts = 1 and partstrat = 'r' reject a multi-column or
    -- expression key, whose bounds this function's lo/hi arithmetic cannot express anyway.
    SELECT a.attname INTO keycol
    FROM   pg_partitioned_table p
    JOIN   pg_attribute a ON a.attrelid = p.partrelid AND a.attnum = p.partattrs[0]
    WHERE  p.partrelid = format('%I', tbl)::regclass
      AND  p.partstrat = 'r'
      AND  p.partnatts = 1;
    IF keycol IS NULL THEN
        RAISE EXCEPTION '% is not range-partitioned on exactly one plain column', tbl;
    END IF;

    -- CREATE TABLE ... (LIKE ...) does not reproduce identity or generated columns, and a
    -- partition silently missing one would diverge from its siblings in a way nothing here
    -- would notice. tx_log has neither today; fail loudly if that ever changes rather than
    -- attach a subtly wrong partition.
    IF EXISTS (
        SELECT 1 FROM pg_attribute a
        WHERE  a.attrelid = format('%I', tbl)::regclass
          AND  a.attnum > 0 AND NOT a.attisdropped
          AND  (a.attidentity <> '' OR a.attgenerated <> ''))
    THEN
        RAISE EXCEPTION '% has an identity or generated column; LIKE does not copy those, '
                        'so the reclaim path would attach a partition differing from its siblings', tbl;
    END IF;

    -- The evacuation INSERT names its columns instead of relying on SELECT *. A parent that
    -- has ever had a column dropped leaves a hole in attnum, and positional column matching
    -- between two relations is exactly how rows get written into the wrong columns.
    SELECT string_agg(quote_ident(a.attname), ', ' ORDER BY a.attnum) INTO cols
    FROM   pg_attribute a
    WHERE  a.attrelid = format('%I', tbl)::regclass
      AND  a.attnum > 0 AND NOT a.attisdropped;

    dflt       := tbl || '_default';
    target_max := head + (cfg.ahead_target::BIGINT * cfg.block_width);

    idx := cfg.min_index;
    WHILE idx * cfg.block_width <= target_max LOOP
        lo   := idx * cfg.block_width;
        hi   := lo + cfg.block_width;
        part := format('%s_p%s', tbl, idx);

        IF to_regclass(format('%I', part)) IS NULL THEN
            moved := 0;

            -- Built standalone, then attached. Indexes are NOT copied here on purpose:
            -- ATTACH PARTITION clones every partitioned index from the parent onto the new
            -- partition, including partial predicates.
            EXECUTE format(
                'CREATE TABLE %I (LIKE %I INCLUDING DEFAULTS INCLUDING CONSTRAINTS INCLUDING STORAGE)',
                part, tbl);

            IF cfg.has_default AND to_regclass(format('%I', dflt)) IS NOT NULL THEN
                -- Taken before the DELETE rather than left to ATTACH. ATTACH takes ACCESS
                -- EXCLUSIVE on the default anyway; taking it up front closes the window in
                -- which a concurrent insert could add a row to this very range after we
                -- deleted it, which would fail the ATTACH validation scan and cost another
                -- 12 hours. See the locking note in the migration header for what this
                -- costs a concurrent ingest.
                EXECUTE format('LOCK TABLE %I IN ACCESS EXCLUSIVE MODE', dflt);

                EXECUTE format(
                    'WITH reclaimed AS ('
                    '  DELETE FROM %I WHERE %I >= %s AND %I < %s RETURNING %s'
                    ') INSERT INTO %I (%s) SELECT %s FROM reclaimed',
                    dflt, keycol, lo, keycol, hi, cols, part, cols, cols);
                GET DIAGNOSTICS moved = ROW_COUNT;
            END IF;

            EXECUTE format('ALTER TABLE %I ATTACH PARTITION %I FOR VALUES FROM (%s) TO (%s)',
                           tbl, part, lo, hi);

            INSERT INTO partition_registry
                   (part_name, table_name, part_index, lo_block, hi_block, reclaimed_rows)
            VALUES (part, tbl, idx, lo, hi, moved)
            ON CONFLICT (part_name) DO NOTHING;

            -- Row movement is recorded, not silent: reclaimed_rows is the durable count and
            -- this lands in the server log.
            IF moved > 0 THEN
                RAISE WARNING 'partition %: reclaimed % row(s) from %', part, moved, dflt;
            END IF;

            RETURN 1;
        END IF;

        idx := idx + 1;
    END LOOP;

    RETURN 0;

-- Two API replicas each run this at startup and every 12 h, so both can see the same index
-- uncovered and both issue the CREATE. One wins; the loser must not turn a benign race into
-- the every-12-hours error loop this migration exists to remove. Report 0 -- "nothing left
-- for me to do" -- and let the caller's loop re-evaluate against the winner's committed
-- work on its next round.
EXCEPTION
    WHEN duplicate_table OR unique_violation THEN
        RETURN 0;
END;
$$;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Intentional no-op, matching 00010 and 00011.
--
-- Restoring the 00004 body would reinstate the exact statement that wedges partition
-- creation permanently, and dropping reclaimed_rows would erase the only record of which
-- rows were moved out of a default partition and into which one. The new function is a
-- strict superset of the old behaviour and is compatible with the old Go caller -- that
-- loop already runs until the function reports 0 -- so a code rollback needs no schema
-- rollback.
SELECT 1;
-- +goose StatementEnd
