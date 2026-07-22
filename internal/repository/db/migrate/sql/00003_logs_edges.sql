-- +goose Up
-- +goose StatementBegin

-- Event logs, promoted to a first-class table.
--
-- Under MongoDB these were an array embedded inside each transaction document, with no
-- index touching them and no query anywhere that read them. The explorer paid the full
-- write cost of the largest field in the largest collection and got zero query
-- capability for it. This is the single biggest capability gain of the migration.
CREATE TABLE tx_log (
    block_number BIGINT   NOT NULL,
    log_index    INT      NOT NULL,
    tx_hash      hash32   NOT NULL,
    tx_index     INT      NOT NULL,
    address      address  NOT NULL,

    -- Four fixed topic columns, NOT a bytea[] with a GIN index.
    --
    -- eth_getLogs topic matching is POSITIONAL: topic[0] is the event signature,
    -- topic[1] the first indexed parameter, and so on. GIN's containment operator is
    -- position-blind, so an array design needs a post-filter to re-impose position.
    -- GIN also returns an unordered bitmap, never sorted output, so every
    -- "ORDER BY block DESC LIMIT n" page would have to sort the entire match set.
    -- And only btree supports index-only scans, which is what makes a topic filter
    -- over a large range affordable.
    topic0 hash32,
    topic1 hash32,
    topic2 hash32,
    topic3 hash32,
    topic_count SMALLINT NOT NULL DEFAULT 0,

    data    BYTEA       NOT NULL DEFAULT '',
    removed BOOLEAN     NOT NULL DEFAULT false,
    ts      TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (block_number, log_index)
) PARTITION BY RANGE (block_number);

-- The canonical eth_getLogs shape: an address plus an event signature over a block
-- range, newest first.
CREATE INDEX tx_log_addr_t0_idx ON tx_log (address, topic0, block_number DESC, log_index DESC);

-- Address-only feed (all events emitted by one contract).
CREATE INDEX tx_log_addr_idx ON tx_log (address, block_number DESC, log_index DESC);

-- Receipt reconstruction: all logs of one transaction, in order.
CREATE INDEX tx_log_tx_idx ON tx_log (tx_hash, log_index);

-- Indexed-parameter lookups (e.g. "all transfers TO this address"). Partial, because
-- most logs carry fewer than three topics and a full index would store mostly NULLs.
CREATE INDEX tx_log_t1_idx ON tx_log (topic1, block_number DESC) WHERE topic1 IS NOT NULL;
CREATE INDEX tx_log_t2_idx ON tx_log (topic2, block_number DESC) WHERE topic2 IS NOT NULL;

-- Deliberately NOT indexed: topic0 alone.
--
-- topic0 is the event signature, and Transfer(address,address,uint256) is 70-90% of all
-- logs on any EVM chain. A standalone topic0 index is enormous and, for the query it
-- would exist to serve, is a near-full scan anyway. It answers nothing that
-- tx_log_addr_t0_idx or a block-range scan does not, and a global "every Transfer" feed
-- is served from token_tx, which has already decoded exactly that.


-- The account edge table.
--
-- The account transaction page asks for "transactions where this address is sender OR
-- recipient, newest first, with an exact total". MongoDB could not serve that from one
-- index -- it index-unions and merge-sorts, which is why the count carried a 500 ms
-- budget and an inexact-total escape hatch. A naive SQL OR has the same problem, and a
-- UNION ALL has three of its own: self-transfers appear twice, the exact count stays
-- unbounded, and it does not survive partitioning.
--
-- One row per (address, transaction) makes the page a single index-only range scan and
-- the count an index-only aggregate.
CREATE TABLE tx_account (
    address      address  NOT NULL,
    block_number BIGINT   NOT NULL,
    tx_index     INT      NOT NULL,

    -- A BITMASK, not a key column. Putting the role in the primary key emits two rows
    -- when sender == recipient (a self-transfer), and N rows once log-participant
    -- edges are added -- which would inflate the "exact" count, render a transaction
    -- more than once on the page, and break the LIMIT n+1 has-more probe.
    --   1 = sender, 2 = recipient, 4 = log participant
    roles        SMALLINT NOT NULL,

    PRIMARY KEY (address, block_number, tx_index)
);

-- No second DESC index is needed: with `address` fixed by equality, the primary key
-- index is scanned backwards to satisfy ORDER BY block_number DESC, tx_index DESC.

-- Supports the per-block purge that makes re-ingesting a block idempotent. Without a
-- block_number-leading index this DELETE would be a sequential scan of the whole table
-- on every re-ingested block.
CREATE INDEX tx_account_blk_idx ON tx_account (block_number);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS tx_account;
DROP TABLE IF EXISTS tx_log;
-- +goose StatementEnd
