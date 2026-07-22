-- +goose Up
-- +goose StatementBegin

CREATE TABLE epoch (
    id                 BIGINT      PRIMARY KEY,
    end_time           TIMESTAMPTZ NOT NULL,
    fee                uint256     NOT NULL,
    base_reward_weight uint256     NOT NULL,
    tx_reward_weight   uint256     NOT NULL,
    reward             uint256     NOT NULL,
    stake              uint256     NOT NULL,
    total_supply       uint256     NOT NULL
);

-- NOT unique on end_time. The MongoDB schema put a UNIQUE index on the epoch end while
-- keying the document on the epoch id, so two epochs ending within the same second --
-- entirely legal on a sub-second-block chain -- made the second one un-insertable.
--
-- This also fixes a structural defect in the old pagination: it range-filtered on the
-- epoch id but sorted on the end time, with an index covering only the latter, so one
-- of the two always cost a scan or a blocking sort.
CREATE INDEX epoch_end_idx ON epoch (end_time DESC);


CREATE TABLE burn (
    block_number BIGINT      PRIMARY KEY REFERENCES block(number) ON DELETE CASCADE,
    ts           TIMESTAMPTZ NOT NULL,
    amount_wei   uint256     NOT NULL,
    tx_count     INT         NOT NULL DEFAULT 0
);

CREATE TABLE burn_tx (
    block_number BIGINT NOT NULL REFERENCES burn(block_number) ON DELETE CASCADE,
    tx_hash      hash32 NOT NULL,
    PRIMARY KEY (block_number, tx_hash)
);

-- The running total lives in meta_counter, not in a sum() over this table. `burn` holds
-- one row per BLOCK, so a whole-table aggregate would be a hundred-million-row numeric
-- addition on every request for a figure that changes once per block.


CREATE TABLE swap (
    block_number BIGINT   NOT NULL,
    log_index    INT      NOT NULL,
    tx_hash      hash32   NOT NULL,
    tx_index     INT      NOT NULL,
    pair         address  NOT NULL,
    sender       address  NOT NULL,

    -- A bare SMALLINT with no label table and no foreign key, deliberately.
    --
    -- The upstream handler currently writes the "mint" enum value for actual TRADES --
    -- byte-identical to the mint handler -- and the enum has no distinct swap value at
    -- all. Attaching a human-readable label and a foreign key to data that is
    -- mislabelled at the source would make a known-wrong value look authoritative.
    -- Fix the handler first, then add the label.
    swap_type    SMALLINT NOT NULL,

    amount0_in   uint256  NOT NULL,
    amount0_out  uint256  NOT NULL,
    amount1_in   uint256  NOT NULL,
    amount1_out  uint256  NOT NULL,
    reserve0     uint256  NOT NULL,
    reserve1     uint256  NOT NULL,

    -- The MongoDB struct tagged this field one way and the writer emitted another, so
    -- the timestamp never round-tripped.
    ts           TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (block_number, log_index)
);

CREATE INDEX swap_pair_ts_idx  ON swap (pair, ts);
CREATE INDEX swap_pair_typ_idx ON swap (pair, swap_type, block_number DESC, log_index DESC);
CREATE INDEX swap_blk_idx      ON swap (block_number);


-- Daily aggregates. Derived entirely from `tx`, so this is a cache rather than a
-- source: it can be rebuilt at any time with a single INSERT ... SELECT.
--
-- Note the MongoDB version only ever maintained a two-day trailing window, so a
-- from-genesis rebuild there produced today and yesterday and nothing else. Backfilling
-- the full history from `tx` is both correct and strictly more accurate, because the
-- old rows summed the lossy integer amount column.
CREATE TABLE trx_volume (
    day      DATE    PRIMARY KEY,
    tx_count BIGINT  NOT NULL,
    volume   uint256 NOT NULL,
    gas_used BIGINT  NOT NULL
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS trx_volume;
DROP TABLE IF EXISTS swap;
DROP TABLE IF EXISTS burn_tx;
DROP TABLE IF EXISTS burn;
DROP TABLE IF EXISTS epoch;
-- +goose StatementEnd
