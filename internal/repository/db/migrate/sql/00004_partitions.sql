-- +goose Up
-- +goose StatementBegin

-- Partition management for block-range-partitioned tables.
--
-- The failure this exists to prevent: PostgreSQL raises
--   ERROR: no partition of relation "tx_log" found for row
-- when a row falls outside every partition. On the ingest path that error aborts the
-- block transaction, and since the indexer retries the same block it wedges permanently
-- at that height. Partition creation is therefore a liveness requirement, not
-- housekeeping.
--
-- Two independent defences:
--   1. ensure_block_partitions() pre-creates partitions ahead of the write head, called
--      by the application at startup and periodically thereafter.
--   2. A DEFAULT partition catches anything that still falls through, so the indexer
--      degrades to "rows landed in the wrong place" instead of stopping. Rows in the
--      default partition are a monitored condition, not a normal state.
--
-- The default partition has a cost worth stating plainly: while it holds rows in a
-- range, creating a real partition for that range must scan it to prove no conflict,
-- taking an ACCESS EXCLUSIVE lock for the duration. That is acceptable precisely
-- because the default is expected to stay empty; partition_health() below is what
-- makes "expected" observable.

CREATE TABLE partition_config (
    table_name   TEXT    PRIMARY KEY,
    block_width  BIGINT  NOT NULL,
    ahead_target INT     NOT NULL DEFAULT 4,   -- partitions to keep ahead of the head
    CHECK (block_width > 0),
    CHECK (ahead_target >= 1)
);

-- 1,000,000 blocks per partition is a starting point, not a derived constant. The
-- correct width depends on the chain's real block rate, which has not been measured on
-- a production NCOG network -- the emitter's floor is a 110 ms interval, so the rate
-- could plausibly differ from 1/s by an order of magnitude. Width is stored in a table
-- rather than compiled in so it can be retuned for future partitions without a
-- migration; existing partitions keep their original bounds.
INSERT INTO partition_config (table_name, block_width) VALUES ('tx_log', 1000000);

-- +goose StatementEnd

-- +goose StatementBegin
-- Creates any missing partitions for `tbl` covering blocks up to
-- (head + ahead_target * width). Idempotent: existing partitions are skipped.
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
    made       INT := 0;
BEGIN
    SELECT * INTO cfg FROM partition_config WHERE table_name = tbl;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'no partition_config row for table %', tbl;
    END IF;

    target_max := head + (cfg.ahead_target::BIGINT * cfg.block_width);

    -- Always start from partition 0 so a fresh database is fully covered from genesis.
    idx := 0;
    WHILE idx * cfg.block_width <= target_max LOOP
        lo   := idx * cfg.block_width;
        hi   := lo + cfg.block_width;
        part := format('%s_p%s', tbl, idx);

        IF to_regclass(format('%I', part)) IS NULL THEN
            EXECUTE format(
                'CREATE TABLE %I PARTITION OF %I FOR VALUES FROM (%s) TO (%s)',
                part, tbl, lo, hi);
            made := made + 1;
        END IF;

        idx := idx + 1;
    END LOOP;

    RETURN made;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- Reports, per partitioned table, how many partitions exist beyond the current head and
-- how many rows have landed in the DEFAULT partition.
--
-- Both are alerting inputs:
--   partitions_ahead = 0  -> the indexer is about to run out of partitions
--   default_rows    > 0  -> partition creation already failed and rows are misplaced
CREATE OR REPLACE FUNCTION partition_health()
RETURNS TABLE (table_name TEXT, partitions_ahead BIGINT, default_rows BIGINT)
LANGUAGE plpgsql
AS $$
DECLARE
    cfg  partition_config%ROWTYPE;
    head BIGINT;
    cnt  BIGINT;
    dflt BIGINT;
BEGIN
    SELECT value::BIGINT INTO head FROM meta_counter WHERE key = 'contiguous_head';
    head := COALESCE(head, 0);

    FOR cfg IN SELECT * FROM partition_config LOOP
        -- Partitions whose lower bound sits above the current head.
        SELECT count(*) INTO cnt
        FROM   pg_class c
        JOIN   pg_inherits i ON i.inhrelid = c.oid
        JOIN   pg_class p    ON p.oid = i.inhparent
        WHERE  p.relname = cfg.table_name
          AND  c.relname <> cfg.table_name || '_default'
          AND  (regexp_replace(c.relname, '^.*_p', ''))::BIGINT * cfg.block_width > head;

        EXECUTE format('SELECT count(*) FROM %I', cfg.table_name || '_default') INTO dflt;

        table_name       := cfg.table_name;
        partitions_ahead := cnt;
        default_rows     := dflt;
        RETURN NEXT;
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- Genesis coverage plus the safety net. ensure_block_partitions(0) creates partitions
-- 0..ahead_target so a brand-new database can ingest immediately.
CREATE TABLE tx_log_default PARTITION OF tx_log DEFAULT;
SELECT ensure_block_partitions('tx_log', 0);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP FUNCTION IF EXISTS partition_health();
DROP FUNCTION IF EXISTS ensure_block_partitions(TEXT, BIGINT);
DROP TABLE IF EXISTS partition_config;
-- +goose StatementEnd
