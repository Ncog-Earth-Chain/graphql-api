-- +goose Up
-- +goose StatementBegin

-- SEC-07c (NEC-174): what ingest records for a DDB commit the node would not serve.
--
-- Since SEC-07 the node's chain-history RPCs serve a restricted DDB commit with `input` empty and an
-- `inputRedacted` descriptor {reason, schema, size, keccak256} in its place. Before this migration
-- ingest saw an empty input, decoded nothing, and wrote no DDB row at all -- so a commit the node
-- withheld for a moment (its block above the node's applied frontier, a Postgres hiccup) was lost to
-- the index for good, and one it withheld for a reason was indistinguishable from one it never had.
--
-- The node's reasons fall in two classes, and they need different tables because they need
-- different futures:
--
--   * FINAL -- `reader-role`, `tombstoned`, `undecodable`. A verdict on the contract as the node holds
--     it. Recorded here as withheld, and NEVER decoded from another source: there is no other source
--     the explorer may use, because the whole point is that the bytes are not public.
--   * TRANSIENT -- `undecidable`. A statement about the NODE, not the contract: every commit above its
--     contiguous applied frontier, every pending commit, a freshly deployed public contract it has not
--     applied yet, everything a DDB-off or Postgres-down node returns. The same commit is served
--     verbatim once applied, so it is queued for a re-fetch and must never be recorded as final.
--
-- Both are keyed by position like every other per-transaction table, and both are in the ingest
-- purge order, so a reorg or a re-scan of the block replaces them exactly as it replaces
-- ddb_operation.

CREATE TABLE ddb_withheld_commit (
    block_number  BIGINT      NOT NULL,
    tx_index      INT         NOT NULL,
    tx_hash       hash32      NOT NULL,

    -- Only the final reasons. `undecidable` here would be the defect this table exists to prevent:
    -- a transient refusal remembered as a verdict.
    reason        TEXT        NOT NULL
        CHECK (reason IN ('reader-role', 'tombstoned', 'undecodable')),

    -- The contract the node judged (its derived db_name). Empty when the node named none.
    schema_name   TEXT        NOT NULL DEFAULT '',

    -- Size and keccak256 of the withheld calldata, as the node states them, so a party who obtains
    -- the bytes through an authorized read can bind them to the chain.
    size          BIGINT      NOT NULL,
    keccak256     hash32      NOT NULL,

    ts            TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (block_number, tx_index)
);

CREATE TABLE ddb_verdict_pending (
    block_number  BIGINT      NOT NULL,
    tx_index      INT         NOT NULL,
    tx_hash       hash32      NOT NULL,

    -- The block's timestamp, so the operation can be written with it once the node serves the commit
    -- (the re-fetch reads the transaction, not its block).
    ts            TIMESTAMPTZ NOT NULL,

    attempts      INT         NOT NULL DEFAULT 0,
    next_try      TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (block_number, tx_index)
);

CREATE INDEX ddb_verdict_pending_next_idx ON ddb_verdict_pending (next_try);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS ddb_verdict_pending;
DROP TABLE IF EXISTS ddb_withheld_commit;
-- +goose StatementEnd
