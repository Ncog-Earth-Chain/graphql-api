-- +goose Up
-- +goose StatementBegin

-- Decoded token transfers (ERC-20 / 721 / 1155).
CREATE TABLE token_tx (
    block_number BIGINT   NOT NULL,
    log_index    INT      NOT NULL,

    -- INT, not SMALLINT. The source sequence value is a uint16, and the driver rejects
    -- anything above 32767 rather than wrapping -- which would abort the whole block.
    seq          INT      NOT NULL,

    call_tx_hash hash32   NOT NULL,
    tx_index     INT      NOT NULL,
    token        address  NOT NULL,
    std          SMALLINT NOT NULL,   -- 3 = ERC20, 4 = ERC721, 5 = ERC1155
    event_type   SMALLINT NOT NULL,
    from_addr    address  NOT NULL,
    to_addr      address  NOT NULL,

    -- One exact column. The MongoDB schema stored every amount TWICE -- an exact hex
    -- string for display and a lossy int64 for arithmetic -- and only the lossy copy
    -- was aggregatable. For ERC-721 and ERC-1155 that copy was unconditionally wrong,
    -- because it put a raw uint256 token amount through an int64 conversion.
    amount       uint256  NOT NULL,
    token_id     uint256,

    ts           TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (block_number, log_index, seq)
);

-- Every account-page index leads with (std, address).
--
-- The token standard is ALWAYS part of the filter -- the API has separate erc20/721/1155
-- list endpoints -- so an index that does not lead with it is a regression against even
-- the MongoDB compounds. Without `std` leading, an address with 400,000 ERC-20 transfers
-- and 12 NFT trades scans all 400,012 index entries and heap-fetches each one to render
-- a single page of 25 NFT trades.
CREATE INDEX token_tx_std_from_idx ON token_tx (std, from_addr, block_number DESC, log_index DESC, seq DESC);
CREATE INDEX token_tx_std_to_idx   ON token_tx (std, to_addr,   block_number DESC, log_index DESC, seq DESC);
CREATE INDEX token_tx_std_tok_idx  ON token_tx (std, token,     block_number DESC, log_index DESC, seq DESC);

-- token and token_id are INDEPENDENT optional arguments on the NFT endpoints, so a
-- (token, token_id) index is unusable when only token_id is supplied.
CREATE INDEX token_tx_std_tid_idx ON token_tx (std, token_id, block_number DESC) WHERE token_id IS NOT NULL;

CREATE INDEX token_tx_assets_idx ON token_tx (to_addr, token);          -- distinct held assets
CREATE INDEX token_tx_call_idx   ON token_tx (call_tx_hash, log_index); -- transfers of one tx
CREATE INDEX token_tx_blk_idx    ON token_tx (block_number);            -- per-block purge


CREATE TABLE delegation (
    delegator        address     NOT NULL,
    validator_id     uint256     NOT NULL,
    validator_addr   address,
    created_tx       hash32      NOT NULL,
    block_number     BIGINT      NOT NULL,
    tx_index         INT         NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL,
    amount_staked    uint256     NOT NULL,
    amount_delegated uint256     NOT NULL,
    PRIMARY KEY (delegator, validator_id)
);

CREATE INDEX delegation_delegator_idx ON delegation (delegator, block_number DESC, tx_index DESC);
CREATE INDEX delegation_validator_idx ON delegation (validator_id, block_number DESC, tx_index DESC);
CREATE INDEX delegation_active_idx    ON delegation (delegator) WHERE amount_delegated > 0;
CREATE INDEX delegation_blk_idx       ON delegation (block_number);


CREATE TABLE withdrawal (
    delegator    address     NOT NULL,
    validator_id uint256     NOT NULL,
    request_id   uint256     NOT NULL,

    -- request_tx is IN THE PRIMARY KEY, and that is not redundancy.
    --
    -- (delegator, validator_id, request_id) is NOT unique over time. The MongoDB code
    -- carries a whole function whose only purpose is to handle a repeat -- it rewrites
    -- the older finalized row's request id to a transaction-derived value "to preserve
    -- requests history", and refuses when the older row is not yet finalized. Under a
    -- three-column key a reused request id would either abort the block or silently
    -- OVERWRITE a settled withdrawal, under-reporting withdrawal totals by that amount.
    request_tx   hash32      NOT NULL,

    block_number BIGINT      NOT NULL,
    tx_index     INT         NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL,
    amount       uint256     NOT NULL,

    -- Genuinely a penalty. The MongoDB writer put the withdrawal TRANSACTION HASH into
    -- this field and the reader parsed it back as a big integer, presenting a
    -- hash-derived 256-bit number to users as a slashing penalty.
    penalty      uint256,

    finalized_tx hash32,
    finalized_at TIMESTAMPTZ,

    -- No CHECK constraint. A new SFC event type must not halt ingest.
    req_type     TEXT        NOT NULL,

    PRIMARY KEY (delegator, validator_id, request_id, request_tx)
);

-- At most one OPEN request per (delegator, validator, request id); finalized ones may
-- repeat. This is the invariant the MongoDB code was hand-maintaining.
CREATE UNIQUE INDEX withdrawal_open_uq ON withdrawal (delegator, validator_id, request_id)
    WHERE finalized_tx IS NULL;

CREATE INDEX withdrawal_delegator_idx ON withdrawal (delegator, block_number DESC, tx_index DESC);
CREATE INDEX withdrawal_pending_idx   ON withdrawal (delegator, validator_id)
    INCLUDE (amount) WHERE finalized_tx IS NULL;
CREATE INDEX withdrawal_blk_idx       ON withdrawal (block_number);


CREATE TABLE reward_claim (
    -- Keyed on (block, log index), NOT on the claim transaction.
    --
    -- Multiple reward claims per transaction are reachable on this chain today: two
    -- different SFC handlers can fire for the same transaction, and any batching
    -- contract claiming from two validators does the same. Keying on the transaction
    -- hash would silently drop the second claim and under-report reward totals
    -- permanently and deterministically.
    block_number BIGINT      NOT NULL,
    log_index    INT         NOT NULL,
    claim_tx     hash32      NOT NULL,
    tx_index     INT         NOT NULL,
    delegator    address     NOT NULL,
    validator_id uint256     NOT NULL,
    claimed_at   TIMESTAMPTZ NOT NULL,
    amount       uint256     NOT NULL,
    is_restake   BOOLEAN     NOT NULL DEFAULT false,
    PRIMARY KEY (block_number, log_index)
);

CREATE INDEX reward_delegator_idx ON reward_claim (delegator, block_number DESC, log_index DESC);
CREATE INDEX reward_validator_idx ON reward_claim (validator_id, block_number DESC, log_index DESC);

-- Time-bounded reward queries. The MongoDB version appended the timestamp filter twice
-- when both bounds were given, and Mongo kept only the last -- silently discarding the
-- lower bound. Expressed as a BETWEEN in SQL, that class of bug cannot recur.
CREATE INDEX reward_time_idx ON reward_claim (delegator, claimed_at) INCLUDE (amount);
CREATE INDEX reward_tx_idx   ON reward_claim (claim_tx);
CREATE INDEX reward_blk_idx  ON reward_claim (block_number);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS reward_claim;
DROP TABLE IF EXISTS withdrawal;
DROP TABLE IF EXISTS delegation;
DROP TABLE IF EXISTS token_tx;
-- +goose StatementEnd
