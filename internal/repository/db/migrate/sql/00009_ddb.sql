-- +goose Up
-- +goose StatementBegin

-- DDB visibility.
--
-- The node exposes NO RPC for DDB history: ddb_getEndorsementStatus and
-- ddb_getConsensusStats are in-memory and reset on restart, ddb_getSchema returns only the
-- current materialized state, and there is no per-block or per-contract operation feed. So
-- the commit-transaction stream is the ONLY chain-derived, historical, reorg-safe source of
-- DDB activity, and anything not captured at ingest cannot be asked for later.
--
-- The decoder already exists and is golden-vector tested against the node's own encoder
-- (rpc/ddb_commit.go). Until now its output was decoded and discarded -- only the contract
-- address was kept. These tables are where it lands.

-- ddb_operation: one row per DDB operation committed on chain.
--
-- Block-keyed, so the reorg purge covers it: a reorged-away commit transaction must take
-- its operation with it, or the explorer reports an operation the chain no longer contains.
CREATE TABLE ddb_operation (
    block_number  BIGINT      NOT NULL,
    tx_index      INT         NOT NULL,
    tx_hash       hash32      NOT NULL,

    -- request_id is the operation's identity in the endorsement protocol. NOT the primary
    -- key: the same request could in principle be committed at two heights across a reorg,
    -- and the position is what makes a row unique on this chain.
    request_id    hash32      NOT NULL,
    requester     address     NOT NULL,

    -- op_type mirrors inter.DdbOperationType (0..9: CreateSchema, UpdateSchema,
    -- DeleteSchema, CreateTable, InsertData, UpdateData, DeleteData, CallProcedure,
    -- GrantRole, RevokeRole). Stored as the numeric code the wire carries.
    op_type       SMALLINT    NOT NULL,

    -- schema_name as the operation declares it. May be empty for operations scoped only by
    -- contract address.
    schema_name   TEXT        NOT NULL DEFAULT '',

    -- contract_address is the data contract this operation targets, when it names one.
    -- Nullable: a call or grant scoped only by schema name has none, and the zero address
    -- is NOT a substitute -- the node always serializes the field even when unset, so the
    -- decoder maps zero to absent deliberately.
    contract_addr address,

    -- contract_name and version come from the operation payload. Version is TEXT because
    -- inter.DdbOperation.Version is a string ("1", "1.2.0"), not an integer.
    contract_name TEXT,
    version       TEXT,

    -- author is the operation's declared author; distinct from requester, which is who
    -- submitted it to the endorsement protocol.
    author        address,

    -- payload is the operation JSON verbatim, exactly as it rode the wire. Kept whole
    -- rather than shredded into columns because the operation shape is user-defined:
    -- a CreateSchema carries table and column definitions, an InsertData carries rows.
    -- JSONB so it is queryable without being schema-bound.
    payload       JSONB       NOT NULL,

    ts            TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (block_number, tx_index)
);

-- the contract activity feed: "every operation on this data contract, newest first"
CREATE INDEX ddb_op_contract_idx ON ddb_operation (contract_addr, block_number DESC, tx_index DESC)
    WHERE contract_addr IS NOT NULL;

-- the same by schema name, for operations that name one instead
CREATE INDEX ddb_op_schema_idx ON ddb_operation (schema_name, block_number DESC, tx_index DESC)
    WHERE schema_name <> '';

-- "what has this account been doing to the database"
CREATE INDEX ddb_op_requester_idx ON ddb_operation (requester, block_number DESC, tx_index DESC);

-- filter the global feed by operation kind
CREATE INDEX ddb_op_type_idx ON ddb_operation (op_type, block_number DESC, tx_index DESC);

-- lookup by protocol request id
CREATE INDEX ddb_op_request_idx ON ddb_operation (request_id);

-- the unfiltered chronological feed
CREATE INDEX ddb_op_recent_idx ON ddb_operation (block_number DESC, tx_index DESC);

-- +goose StatementEnd
-- +goose StatementBegin

-- ddb_endorsement: the quorum proof that authorised an operation.
--
-- This is the dual-consensus story the explorer could not tell at all. It shares the
-- operation's primary key because a commit transaction carries exactly one proof for
-- exactly one operation.
CREATE TABLE ddb_endorsement (
    block_number    BIGINT      NOT NULL,
    tx_index        INT         NOT NULL,

    operation_hash  hash32      NOT NULL,
    data_hash       hash32      NOT NULL,

    -- state_hash is the PRIOR per-contract state hash the quorum signed against. Every
    -- validator signature commits to it, which is what makes the per-contract hash chain
    -- verifiable rather than merely asserted.
    state_hash      hash32      NOT NULL,

    -- The state-hash chain links. BYTEA and NULLABLE, deliberately, and this is the
    -- correction that matters most in this migration:
    --
    -- they are variable-length []byte with omitempty on the wire and are EMPTY for a
    -- contract's FIRST operation and for non-contract operations -- the node itself guards
    -- on len(...)==0. Declaring them hash32 NOT NULL would fail the domain's
    -- octet_length = 32 check and ABORT THE BLOCK for every contract's genesis operation,
    -- which is to say every contract would break the indexer at the moment it was created.
    prior_post_state_hash BYTEA,
    post_state_hash       BYTEA,

    -- The endorsing committee's epoch (the grace window). This is the field whose absence
    -- from the explorer's mirrored struct made every DDB commit transaction fail to decode.
    epoch           BIGINT      NOT NULL DEFAULT 0,

    -- validator_count is the size of the committee that endorsed; signature_count is how
    -- many actually signed. Both are stored because the RATIO is the interesting number --
    -- a quorum that barely cleared threshold reads differently from a unanimous one.
    validator_count INT         NOT NULL,
    signature_count INT         NOT NULL,

    validators      BYTEA[]     NOT NULL,
    ts              TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (block_number, tx_index)
);

-- "which operations did this validator endorse" -- validator participation over time
CREATE INDEX ddb_endorsement_epoch_idx ON ddb_endorsement (epoch, block_number DESC);

-- +goose StatementEnd
-- +goose StatementBegin

-- ddb_contract: the current view of each data contract, folded from its operations.
--
-- DOMAIN-KEYED, not block-keyed, and therefore NOT purged on reorg -- the same limitation
-- as delegation and withdrawal. Its block_number records the last operation that touched
-- it, not its identity, so deleting by block would remove a live contract merely because
-- its most recent update landed in a reorged block.
--
-- It is safe to leave slightly stale: it is a materialized convenience, and ddb_operation
-- is the authoritative history it is folded from. A rebuild can always recompute it.
CREATE TABLE ddb_contract (
    contract_addr  address     PRIMARY KEY,

    -- db_name is DERIVED, not carried on the wire: the node computes
    -- lower(contract_name) + '_' + last-6-of-address. Stored because it is how the DDB's
    -- Postgres schema is actually named, which is what an operator needs to find it.
    db_name        TEXT,

    contract_name  TEXT,

    -- author, not owner. There is no owner field anywhere in the operation payload; the
    -- node's local metadata has one, but it is not chain-derived and therefore not
    -- something this explorer can honestly claim.
    author         address,

    latest_version TEXT,

    -- the position of the operation that created it, and of the most recent one
    first_block    BIGINT      NOT NULL,
    last_block     BIGINT      NOT NULL,
    last_tx_index  INT         NOT NULL,

    op_count       BIGINT      NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL
);

CREATE INDEX ddb_contract_recent_idx ON ddb_contract (last_block DESC, last_tx_index DESC);
CREATE INDEX ddb_contract_name_idx   ON ddb_contract (contract_name) WHERE contract_name IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS ddb_contract;
DROP TABLE IF EXISTS ddb_endorsement;
DROP TABLE IF EXISTS ddb_operation;
-- +goose StatementEnd
