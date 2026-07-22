-- +goose Up
-- +goose StatementBegin

-- Partition management for block-range-partitioned tables.
--
-- The failure this exists to prevent: PostgreSQL raises
--   ERROR: no partition of relation "tx_log" found for row
-- when a row falls outside every partition. On the ingest path that aborts the block
-- transaction, and because the indexer retries the same block it wedges permanently at
-- that height. Partition creation is a liveness requirement, not housekeeping.
--
-- Bounds are recorded in partition_registry rather than parsed back out of relation
-- names. Deriving a partition's range from its name means any partition created by
-- another tool, restored under a different name, or named for an archive tier raises
-- invalid_text_representation and takes the whole health check down.

CREATE TABLE partition_config (
    table_name   TEXT   PRIMARY KEY,
    block_width  BIGINT NOT NULL,
    ahead_target INT    NOT NULL DEFAULT 4,   -- partitions to keep ahead of the head

    -- The floor the creator starts from. Without it, a table whose old partitions have
    -- been pruned would have every dropped partition RECREATED on the next tick --
    -- forever: create, prune, recreate, with an ACCESS EXCLUSIVE lock on the parent
    -- each time and a partition count that never falls.
    min_index    BIGINT NOT NULL DEFAULT 0,

    -- Whether this table has a DEFAULT partition. A pruned table must not: a default
    -- partition accumulates rows from arbitrary ranges and can never itself be dropped,
    -- silently turning an O(1)-prunable table into an unprunable one.
    has_default  BOOLEAN NOT NULL DEFAULT false,

    CHECK (block_width > 0),
    CHECK (ahead_target >= 1),
    CHECK (min_index >= 0)
);

-- Authoritative partition bounds and lifecycle state.
CREATE TABLE partition_registry (
    part_name   TEXT   PRIMARY KEY,
    table_name  TEXT   NOT NULL,
    part_index  BIGINT NOT NULL,
    lo_block    BIGINT NOT NULL,
    hi_block    BIGINT NOT NULL,          -- EXCLUSIVE; byte-identical to the DDL bound
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    detached_at TIMESTAMPTZ,
    dropped_at  TIMESTAMPTZ,
    UNIQUE (table_name, part_index)
);

CREATE INDEX partition_registry_tbl_idx ON partition_registry (table_name, part_index);

-- 1,000,000 blocks per partition is a starting point, not a derived constant. The right
-- width depends on the chain's real block rate, which has not been measured on a
-- production NCOG network -- the emitter's floor is a 110 ms interval, so the rate could
-- differ from 1/s by an order of magnitude. Width lives in a table so it can be retuned
-- for future partitions without a migration; existing partitions keep their bounds.
INSERT INTO partition_config (table_name, block_width, has_default)
VALUES ('tx_log', 1000000, true);

-- +goose StatementEnd

-- +goose StatementBegin
-- Creates missing partitions for `tbl` up to (head + ahead_target * width), starting at
-- the configured min_index. Idempotent.
--
-- Returns after creating at most ahead_target partitions per call. Each
-- CREATE TABLE ... PARTITION OF takes an ACCESS EXCLUSIVE lock on the PARENT and holds
-- it until the surrounding transaction ends, so an unbounded loop would block every
-- reader and writer of the table for the whole run. The caller re-invokes until this
-- returns 0.
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

    idx := cfg.min_index;
    WHILE idx * cfg.block_width <= target_max LOOP
        lo   := idx * cfg.block_width;
        hi   := lo + cfg.block_width;
        part := format('%s_p%s', tbl, idx);

        IF to_regclass(format('%I', part)) IS NULL THEN
            EXECUTE format(
                'CREATE TABLE %I PARTITION OF %I FOR VALUES FROM (%s) TO (%s)',
                part, tbl, lo, hi);

            INSERT INTO partition_registry (part_name, table_name, part_index, lo_block, hi_block)
            VALUES (part, tbl, idx, lo, hi)
            ON CONFLICT (part_name) DO NOTHING;

            made := made + 1;

            -- bound the ACCESS EXCLUSIVE hold; caller loops until this returns 0
            IF made >= cfg.ahead_target THEN
                RETURN made;
            END IF;
        END IF;

        idx := idx + 1;
    END LOOP;

    RETURN made;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- Per-table partition health.
--
-- Every value comes from the catalog or the registry; nothing is parsed out of a
-- relation name and no table is assumed to have a DEFAULT partition. Both assumptions
-- were present in the first version of this function and both were fatal: because the
-- function loops over every registered table, a single table lacking a default
-- partition, or a single partition named by any other scheme, aborted the health check
-- for ALL tables -- so the monitoring that exists to detect data loss would itself go
-- silent at exactly the moment a second partitioned table was introduced.
--
-- orphan_attached / orphan_detached surface registry drift in both directions, so a
-- partition created or dropped outside this machinery is visible rather than invisible.
CREATE OR REPLACE FUNCTION partition_health()
RETURNS TABLE (
    table_name       TEXT,
    partitions_ahead BIGINT,
    default_rows     BIGINT,
    orphan_attached  BIGINT,
    orphan_detached  BIGINT
)
LANGUAGE plpgsql
AS $$
DECLARE
    cfg  partition_config%ROWTYPE;
    head BIGINT;
BEGIN
    SELECT COALESCE(max(value)::BIGINT, 0) INTO head
    FROM meta_counter WHERE key = 'contiguous_head';

    FOR cfg IN SELECT * FROM partition_config ORDER BY partition_config.table_name LOOP
        table_name := cfg.table_name;

        -- Partitions whose lower bound is beyond the current head. Read from the
        -- registry, joined to the catalog so a registry row for a partition that no
        -- longer exists is not counted as headroom.
        SELECT count(*) INTO partitions_ahead
        FROM   partition_registry r
        WHERE  r.table_name = cfg.table_name
          AND  r.dropped_at IS NULL
          AND  r.lo_block > head
          AND  to_regclass(format('%I', r.part_name)) IS NOT NULL;

        -- Only consult a default partition if this table is configured to have one.
        default_rows := 0;
        IF cfg.has_default AND to_regclass(format('%I', cfg.table_name || '_default')) IS NOT NULL THEN
            EXECUTE format('SELECT count(*) FROM %I', cfg.table_name || '_default')
            INTO default_rows;
        END IF;

        -- Attached in the catalog but absent from (or already marked dropped in) the
        -- registry.
        SELECT count(*) INTO orphan_attached
        FROM   pg_class c
        JOIN   pg_inherits i ON i.inhrelid = c.oid
        JOIN   pg_class p    ON p.oid = i.inhparent
        WHERE  p.relname = cfg.table_name
          AND  c.relname <> cfg.table_name || '_default'
          AND  NOT EXISTS (
                 SELECT 1 FROM partition_registry r
                 WHERE r.part_name = c.relname AND r.dropped_at IS NULL);

        -- Registered as live but no longer attached.
        SELECT count(*) INTO orphan_detached
        FROM   partition_registry r
        WHERE  r.table_name = cfg.table_name
          AND  r.dropped_at IS NULL
          AND  r.detached_at IS NULL
          AND  NOT EXISTS (
                 SELECT 1
                 FROM   pg_class c
                 JOIN   pg_inherits i ON i.inhrelid = c.oid
                 JOIN   pg_class p    ON p.oid = i.inhparent
                 WHERE  p.relname = cfg.table_name AND c.relname = r.part_name);

        RETURN NEXT;
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- Genesis coverage plus the safety net for tx_log.
--
-- tx_log keeps a DEFAULT partition because it is never pruned: if partition creation
-- ever falls behind, rows land somewhere recoverable instead of wedging the indexer.
-- Pruned tables (tx_credential, pubkey) deliberately get no default -- see 00009.
CREATE TABLE tx_log_default PARTITION OF tx_log DEFAULT;

-- Called repeatedly because each call creates at most ahead_target partitions.
DO $$
DECLARE made INT;
BEGIN
    LOOP
        made := ensure_block_partitions('tx_log', 0);
        EXIT WHEN made = 0;
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP FUNCTION IF EXISTS partition_health();
DROP FUNCTION IF EXISTS ensure_block_partitions(TEXT, BIGINT);
DROP TABLE IF EXISTS partition_registry;
DROP TABLE IF EXISTS partition_config;
-- +goose StatementEnd
