-- +goose Up
-- +goose StatementBegin

-- Domains for the three value shapes that appear on nearly every table.
--
-- Addresses and hashes are BYTEA, not CHAR(42)/TEXT. They are binary values that
-- someone chose to render as hex; storing the rendering means a writer that disagrees
-- about checksum casing produces a row that never matches. 20 bytes occupies 24 bytes
-- aligned in the heap and 32 in a btree tuple, against 44/52 for CHAR(42) -- roughly
-- 60% larger address indexes on the largest tables in the database.
CREATE DOMAIN address AS BYTEA CHECK (octet_length(VALUE) = 20);
CREATE DOMAIN hash32  AS BYTEA CHECK (octet_length(VALUE) = 32);

-- uint256 money.
--
-- The upper bound is load-bearing and is NOT just an overflow guard:
--
--   (a) NaN. In PostgreSQL, NaN sorts ABOVE every other numeric value, so
--       'NaN'::numeric >= 0 evaluates TRUE. A lower-bound-only CHECK therefore
--       admits NaN, and a single NaN row turns every sum() over the column into
--       NaN permanently and silently -- no error, no log line, just a wrong total
--       forever. The upper comparison evaluates FALSE for NaN, so this one clause
--       rejects it.
--
--   (b) NUMERIC(78,0) holds up to 10^78-1, which is ~8.6x larger than 2^256-1
--       (1.1579e77). Without the bound, values in that band are accepted and no
--       longer represent anything the chain can produce.
CREATE DOMAIN uint256 AS NUMERIC(78, 0) CHECK (
    VALUE >= 0 AND
    VALUE < 115792089237316195423570985008687907853269984665640564039457584007913129639936
);

-- Small key/value counters that must be updated transactionally with the data they
-- describe. contiguous_head is the ingest watermark and is the single most important
-- row in the database: it is advanced inside the same transaction that writes a
-- block, so it can never claim progress the data does not back.
CREATE TABLE meta_counter (
    key        TEXT PRIMARY KEY,
    value      NUMERIC(78, 0) NOT NULL,
    updated_at TIMESTAMPTZ    NOT NULL DEFAULT now()
);

INSERT INTO meta_counter (key, value) VALUES
    ('contiguous_head', 0),   -- highest block N such that 1..N are all present
    ('last_swap_block', 0),   -- was a stray sentinel document inside the swap collection
    ('burn_total_wei',  0);   -- running total; the alternative is a full-table sum() per request

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS meta_counter;
DROP DOMAIN IF EXISTS uint256;
DROP DOMAIN IF EXISTS hash32;
DROP DOMAIN IF EXISTS address;
-- +goose StatementEnd
