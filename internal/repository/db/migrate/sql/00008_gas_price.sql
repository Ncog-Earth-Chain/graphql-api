-- +goose Up
-- +goose StatementBegin

-- Gas price samples.
--
-- MongoDB expressed retention here as a TTL index, which PostgreSQL has no equivalent
-- for. The naive replacement -- a periodic DELETE -- would churn roughly 4.3 million
-- rows a year (one per poll interval), producing dead tuples, autovacuum load and
-- permanent index bloat for data that expires on a strict time boundary. Monthly range
-- partitions make expiry a DROP TABLE: constant time, no dead tuples, no bloat.
CREATE TABLE gas_price_tick (
    ts_from     TIMESTAMPTZ NOT NULL,
    ts_to       TIMESTAMPTZ NOT NULL,
    period_type SMALLINT    NOT NULL DEFAULT 0,

    -- uint256, not int64. Gas price is a wei quantity like every other value on this
    -- chain; the MongoDB schema stored it as a signed 64-bit integer in one place and a
    -- float64 in another.
    open_wei    uint256 NOT NULL,
    close_wei   uint256 NOT NULL,
    min_wei     uint256 NOT NULL,
    max_wei     uint256 NOT NULL,
    avg_wei     uint256 NOT NULL,

    tick_ns     BIGINT  NOT NULL,

    PRIMARY KEY (ts_from, period_type)
) PARTITION BY RANGE (ts_from);

CREATE INDEX gas_tick_range_idx ON gas_price_tick (ts_from, ts_to);

-- +goose StatementEnd

-- +goose StatementBegin
-- Creates monthly partitions covering [from_month, from_month + months). Idempotent.
CREATE OR REPLACE FUNCTION ensure_gas_price_partitions(from_ts TIMESTAMPTZ, months INT)
RETURNS INT
LANGUAGE plpgsql
AS $$
DECLARE
    start_m TIMESTAMPTZ := date_trunc('month', from_ts);
    i       INT;
    lo      TIMESTAMPTZ;
    hi      TIMESTAMPTZ;
    part    TEXT;
    made    INT := 0;
BEGIN
    FOR i IN 0 .. GREATEST(months - 1, 0) LOOP
        lo   := start_m + (i || ' months')::INTERVAL;
        hi   := lo + INTERVAL '1 month';
        part := format('gas_price_tick_%s', to_char(lo, 'YYYYMM'));

        IF to_regclass(format('%I', part)) IS NULL THEN
            EXECUTE format(
                'CREATE TABLE %I PARTITION OF gas_price_tick FOR VALUES FROM (%L) TO (%L)',
                part, lo, hi);
            made := made + 1;
        END IF;
    END LOOP;

    RETURN made;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- Drops whole monthly partitions strictly older than the retention window.
--
-- Returns the partitions it dropped so the caller can log exactly what was removed --
-- silent deletion of data is not an acceptable outcome for a retention job.
CREATE OR REPLACE FUNCTION prune_gas_price_partitions(retain_months INT)
RETURNS TABLE (dropped TEXT)
LANGUAGE plpgsql
AS $$
DECLARE
    cutoff TIMESTAMPTZ := date_trunc('month', now()) - (retain_months || ' months')::INTERVAL;
    r      RECORD;
BEGIN
    IF retain_months < 1 THEN
        RAISE EXCEPTION 'retain_months must be >= 1, got %', retain_months;
    END IF;

    FOR r IN
        SELECT c.relname, pg_get_expr(c.relpartbound, c.oid) AS bound
        FROM   pg_class c
        JOIN   pg_inherits i ON i.inhrelid = c.oid
        JOIN   pg_class p    ON p.oid = i.inhparent
        WHERE  p.relname = 'gas_price_tick'
    LOOP
        -- Only drop a partition whose UPPER bound is at or before the cutoff, so a
        -- partition straddling the boundary is always kept. Erring towards keeping
        -- data is the correct direction for a destructive job.
        IF (regexp_match(r.bound, 'TO \(''([^'']+)''\)'))[1]::TIMESTAMPTZ <= cutoff THEN
            EXECUTE format('DROP TABLE %I', r.relname);
            dropped := r.relname;
            RETURN NEXT;
        END IF;
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- Cover the current month and a year ahead so a fresh deployment can ingest immediately.
SELECT ensure_gas_price_partitions(now(), 13);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP FUNCTION IF EXISTS prune_gas_price_partitions(INT);
DROP FUNCTION IF EXISTS ensure_gas_price_partitions(TIMESTAMPTZ, INT);
DROP TABLE IF EXISTS gas_price_tick;
-- +goose StatementEnd
