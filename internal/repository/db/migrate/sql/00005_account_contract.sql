-- +goose Up
-- +goose StatementBegin

CREATE TABLE account (
    address     address     PRIMARY KEY,
    acct_type   SMALLINT    NOT NULL,
    sc_tx_hash  hash32,
    first_block BIGINT,
    first_seen  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX account_type_idx ON account (acct_type);

-- Transaction count and last-activity are deliberately NOT columns on `account`.
--
-- Two reasons, either sufficient on its own:
--
--   1. Correctness. The MongoDB version incremented a counter per observation while
--      the scanner deliberately re-scans the most recent blocks on every startup, so
--      the value was wrong by construction -- and it was the SORT KEY for the
--      front-page token lists. A "high-water block" guard does not fix it either,
--      because ingest is not monotone: head-push interleaves with backfill and the
--      recent-block cache replays newest-first, so one head block at a high height
--      makes every subsequent backfill block below it contribute nothing.
--
--   2. Write amplification. The updated columns would BE the index keys, which makes
--      heap-only-tuple updates structurally impossible: every counter bump rewrites
--      the row and every index entry pointing at it, and rewrites the front-page sort
--      index continuously.
--
-- tx_account is the ground truth. Derive the statistics from it.
CREATE MATERIALIZED VIEW account_stat AS
SELECT e.address,
       count(*)  AS tx_count,
       max(b.ts) AS last_activity
FROM   tx_account e
JOIN   block b ON b.number = e.block_number
GROUP  BY e.address;

-- REFRESH MATERIALIZED VIEW CONCURRENTLY requires a unique index.
CREATE UNIQUE INDEX account_stat_pk ON account_stat (address);

-- Serves the three token lists: filter by account type, order by activity.
CREATE INDEX account_stat_rank_idx ON account_stat (tx_count DESC, last_activity DESC);


CREATE TABLE contract (
    address      address     PRIMARY KEY,
    acct_type    SMALLINT    NOT NULL,
    deploy_tx    hash32      NOT NULL,
    block_number BIGINT      NOT NULL,
    tx_index     INT         NOT NULL,

    -- Guards a case that is latent rather than active. Today a contract is recorded
    -- from the receipt's single top-level created address, so (block, tx_index) is
    -- 1:1 with a contract. The moment internal-transaction tracing is added, a factory
    -- deploying two children in one transaction collides. The column costs nothing now
    -- and would be a table rewrite later.
    deploy_seq   SMALLINT    NOT NULL DEFAULT 0,

    ts           TIMESTAMPTZ NOT NULL,
    is_ddb       BOOLEAN     NOT NULL DEFAULT false,

    -- Denormalized, written ONLY by the verification mutation and never by the scanner.
    -- Without it, "list verified contracts" is a highly selective predicate living in a
    -- DIFFERENT table from the ORDER BY, so the planner either nested-loops thousands
    -- of random probes to fill one page or hash-joins and sorts both tables on every
    -- request and every count.
    is_verified  BOOLEAN     NOT NULL DEFAULT false,

    UNIQUE (block_number, tx_index, deploy_seq)
);

CREATE INDEX contract_recent_idx   ON contract (block_number DESC, tx_index DESC, deploy_seq DESC);
CREATE INDEX contract_ddb_idx      ON contract (block_number DESC, tx_index DESC) WHERE is_ddb;
CREATE INDEX contract_verified_idx ON contract (block_number DESC, tx_index DESC) WHERE is_verified;


-- USER-SUBMITTED AND NOT REBUILDABLE.
--
-- This is the one table in the database that a from-genesis re-scan cannot reproduce:
-- source code, ABI and compiler metadata arrive through a GraphQL mutation, not from
-- the chain. It lives in its own table specifically so that the indexer's
-- INSERT ... ON CONFLICT on `contract` physically cannot blank it.
--
-- The hazard being closed: under MongoDB, adding a contract that already existed fell
-- through to an update that $set the WHOLE document from a freshly-built struct with
-- every verification field empty. It survived only because an earlier existence check
-- short-circuited first -- meaning what protected irreplaceable user data was an
-- unrelated cache lookup, not the writer.
CREATE TABLE contract_verification (
    address          address     PRIMARY KEY REFERENCES contract(address) ON DELETE CASCADE,
    validated_at     TIMESTAMPTZ,

    name             TEXT,

    -- Two columns, not one. The MongoDB marshaller destructively overwrote the user's
    -- contract version with the compiler version at write time, so the original value
    -- is unrecoverable from both the database AND the chain. Mapping that single field
    -- into both columns would make every migrated row wrong in one of them; keep the
    -- ambiguous legacy value separate and disambiguate offline.
    version_legacy   TEXT,
    contract_version TEXT,
    compiler_version TEXT,

    support_contact  TEXT,
    license          TEXT,
    compiler         TEXT,
    evm_version      TEXT,
    via_ir           BOOLEAN NOT NULL DEFAULT false,
    is_optimized     BOOLEAN NOT NULL DEFAULT false,
    optimize_runs    INT     NOT NULL DEFAULT 0,

    source_code      TEXT,
    source_hash      hash32,

    -- TEXT, not JSONB, for both abi and metadata.
    --
    -- The compiler metadata is hashed into the deployed bytecode's metadata trailer, so
    -- byte-exactness is what makes verification verifiable. JSONB normalizes key order,
    -- whitespace and duplicate keys, destroying that at write time and unrecoverably.
    -- abi_json below is a queryable projection, populated by a separate row-tolerant
    -- pass so that one malformed user-pasted ABI cannot abort an import of the only
    -- irreplaceable table in the system.
    abi              TEXT,
    abi_json         JSONB,
    metadata         TEXT,

    creation_bytecode BYTEA,
    runtime_bytecode  BYTEA,
    creation_link_refs JSONB,
    runtime_link_refs  JSONB,

    -- Proxy detection runs only inside the verification mutation and requires an
    -- already-verified implementation, so it belongs here rather than on the
    -- scanner-owned table. No CHECK on proxy_type: the set of proxy standards grows,
    -- and a rejected value here would abort a user's verification request.
    is_proxy      BOOLEAN NOT NULL DEFAULT false,
    proxy_type    TEXT,
    impl_address  address
);

CREATE INDEX cv_validated_idx ON contract_verification (validated_at DESC) WHERE validated_at IS NOT NULL;
CREATE INDEX cv_impl_idx      ON contract_verification (impl_address) WHERE impl_address IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS contract_verification;
DROP TABLE IF EXISTS contract;
DROP MATERIALIZED VIEW IF EXISTS account_stat;
DROP TABLE IF EXISTS account;
-- +goose StatementEnd
