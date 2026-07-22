-- +goose Up
-- +goose StatementBegin

-- Blocks were not stored at all under MongoDB -- block lists were served by N
-- sequential RPC calls, one per block. Storing them also gives the ingest watermark
-- something exact to stand on: a block row exists only if its whole transaction set
-- was committed in the same transaction.
CREATE TABLE block (
    number      BIGINT      PRIMARY KEY,
    hash        hash32      NOT NULL UNIQUE,
    parent_hash hash32      NOT NULL,
    miner       address     NOT NULL,
    state_root  hash32      NOT NULL,
    gas_limit   BIGINT      NOT NULL,
    gas_used    BIGINT      NOT NULL,
    size_bytes  BIGINT      NOT NULL,
    ts          TIMESTAMPTZ NOT NULL,
    tx_count    INT         NOT NULL DEFAULT 0,
    epoch       BIGINT,
    ingested_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX block_ts_idx ON block (ts DESC);

-- Partial index: epoch is nullable until the epoch feed is wired, and a per-epoch
-- block listing is only meaningful for rows that have one.
CREATE INDEX block_epoch_idx ON block (epoch, number DESC) WHERE epoch IS NOT NULL;

CREATE TABLE tx (
    hash            hash32   PRIMARY KEY,

    -- NOT NULL, deliberately. Pending transactions are unreachable in this codebase:
    -- the single call site always supplies a block and the writer rejects a nil one.
    -- Modelling pending as a nullable block number would reintroduce NULL-blind
    -- keyset comparisons and NULLS-FIRST ordering for a state that never occurs.
    block_number    BIGINT   NOT NULL REFERENCES block(number) ON DELETE CASCADE,
    tx_index        INT      NOT NULL,
    block_hash      hash32   NOT NULL,

    from_addr       address  NOT NULL,
    to_addr         address,                  -- NULL means contract creation
    value_wei       uint256  NOT NULL,
    nonce           BIGINT   NOT NULL,

    gas_limit       BIGINT   NOT NULL,
    gas_used        BIGINT,
    gas_cumulative  BIGINT,
    gas_price_wei   uint256  NOT NULL,
    max_fee_per_gas          uint256,
    max_priority_fee_per_gas uint256,

    input           BYTEA    NOT NULL DEFAULT '',
    tx_type         SMALLINT NOT NULL DEFAULT 0,

    -- Post-quantum wire fields. None of these existed under MongoDB, which is why the
    -- explorer could not show which chain or signature scheme a transaction used.
    -- sig_version 2 = ML-DSA-87, 3 = ML-DSA-87 after key rotation.
    chain_id        BIGINT,
    sig_version     SMALLINT,

    status          SMALLINT NOT NULL,        -- 0 reverted, 1 success
    created_contract address,
    ts              TIMESTAMPTZ NOT NULL,

    is_ddb          BOOLEAN  NOT NULL DEFAULT false,
    ddb_contract    address,

    -- The real ordering key. The MongoDB schema packed (block, index) into one 64-bit
    -- ordinal with the index masked to 14 bits, which collided silently past 16384
    -- transactions in a block -- against a UNIQUE index, so the colliding transaction
    -- was not merely mis-sorted but REJECTED and lost. PostgreSQL compares row values
    -- natively, so the tuple is the key and no packing is needed.
    CONSTRAINT tx_position_uq UNIQUE (block_number, tx_index)
);

-- Serves ORDER BY (block_number, tx_index) DESC keyset pagination directly.
CREATE INDEX tx_block_idx ON tx (block_number DESC, tx_index DESC);

-- Sender feed. Recipient is served through tx_account (see 00003) rather than a
-- second single-column index, because the account page needs sender OR recipient in
-- one ordered scan.
CREATE INDEX tx_from_idx ON tx (from_addr, block_number DESC, tx_index DESC);

-- Two-party filter (transactions from A to B).
CREATE INDEX tx_from_to_idx ON tx (from_addr, to_addr, block_number DESC, tx_index DESC);

-- DDB transaction feed. Partial, because DDB transactions are a small fraction of all
-- transactions and a full index would be mostly dead weight.
CREATE INDEX tx_ddb_idx ON tx (block_number DESC, tx_index DESC) WHERE is_ddb;

-- BRIN, not btree, for the time dimension.
--
-- The only consumers of a time range over transactions are unordered aggregates over
-- wide contiguous windows -- daily volume, transaction speed, gas speed. Timestamps
-- are physically correlated with insertion order on an append-mostly table, which is
-- exactly the correlation BRIN exploits. A btree here would cost roughly 40 bytes per
-- row (about 4 GB at 100M transactions) to serve three background jobs; the BRIN is
-- kilobytes.
--
-- autosummarize is load-bearing: the "recent transaction speed" query reads the newest
-- data, which is precisely the range that would otherwise sit unsummarized and degrade
-- to a sequential scan.
CREATE INDEX tx_ts_brin ON tx USING brin (ts) WITH (pages_per_range = 32, autosummarize = on);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS tx;
DROP TABLE IF EXISTS block;
-- +goose StatementEnd
