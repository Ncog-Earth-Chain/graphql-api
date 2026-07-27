-- +goose Up
-- +goose StatementBegin

-- op_count stops being recomputed and starts being maintained, without reintroducing the
-- drift that made a maintained counter wrong the first time.
--
-- WHAT IT COST. The ingest fold set
--     op_count = (SELECT count(*) FROM ddb_operation WHERE contract_addr = ...)
-- once in the INSERT's VALUES list and AGAIN in its ON CONFLICT DO UPDATE, because
-- PostgreSQL builds the proposed tuple -- evaluating the subquery inside it -- before it can
-- detect the conflict. Every DDB commit therefore counted the contract's ENTIRE history
-- TWICE. Measured on PostgreSQL 16 with one contract holding 1,000,000 operations: 371 ms
-- for that single statement, 208 ms + 162 ms of it in the two counts. Per-commit cost is
-- O(M), so ingesting M operations on a contract costs O(M^2) -- and it is paid inside the
-- block transaction, which also carries the 30 s statement_timeout.
--
-- An index does not rescue it. ddb_op_contract_idx already leads with contract_addr;
-- forcing an index-only scan measured 178 ms against the sequential scan's 175 ms, because
-- the work is per tuple and there are a million of them either way.
--
-- WHY A TRIGGER RATHER THAN A COUNTER IN THE WRITER. `op_count + 1` in the fold was the
-- original form and it double-counted on re-ingest -- which the scanner does routinely, and
-- which is the entire repair mechanism for a gap. TestDdbContractCountSurvivesReScan pins
-- that. It drifted because "the writer believes one more operation happened" and "how many
-- rows are in ddb_operation" are two independent facts with nothing tying them together.
--
-- A trigger ties them together by construction: op_count moves if and only if a
-- ddb_operation row appears, disappears, or changes contract. Re-ingest is
-- DELETE-then-INSERT (purgeBlockRows runs before the operation is written and ddb_operation
-- is in purgeOrder), so the decrement and the increment cancel exactly and the count stands
-- still.
--
-- It also FIXES a case the derived count got wrong. The fold ran only when a DDB commit
-- arrived, so a reorg that replaced a commit with an ordinary transfer deleted the operation
-- row and left op_count still counting it -- stale until some later commit for the SAME
-- contract happened to trigger a recount, possibly never. The trigger decrements on the
-- purge itself.
CREATE OR REPLACE FUNCTION ddb_contract_op_count_apply() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.contract_addr IS NOT NULL THEN
            UPDATE ddb_contract SET op_count = op_count + 1
            WHERE  contract_addr = NEW.contract_addr;
        END IF;

    ELSIF TG_OP = 'DELETE' THEN
        -- GREATEST(...,0) because op_count is read back through hexutil.Uint64
        -- (ddb_read.go scanDdbContract), so -1 would be served to clients as
        -- 18446744073709551615. The floor cannot hide sustained drift -- the reconcile
        -- statement at the bottom of this migration is the repair -- but it stops one lost
        -- decrement from rendering as 1.8e19 operations.
        IF OLD.contract_addr IS NOT NULL THEN
            UPDATE ddb_contract SET op_count = GREATEST(op_count - 1, 0)
            WHERE  contract_addr = OLD.contract_addr;
        END IF;

    ELSIF OLD.contract_addr IS DISTINCT FROM NEW.contract_addr THEN
        -- An operation re-observed at the same (block_number, tx_index) but naming a
        -- different data contract. The StoreTransaction path takes the ON CONFLICT DO UPDATE
        -- branch without purging first, so the row moves between contracts in place. Both
        -- counters have to move, or the abandoned contract keeps an operation it no longer
        -- owns -- which is exactly what the derived count did, since it only ever recounted
        -- the contract named by the INCOMING operation.
        IF OLD.contract_addr IS NOT NULL THEN
            UPDATE ddb_contract SET op_count = GREATEST(op_count - 1, 0)
            WHERE  contract_addr = OLD.contract_addr;
        END IF;
        IF NEW.contract_addr IS NOT NULL THEN
            UPDATE ddb_contract SET op_count = op_count + 1
            WHERE  contract_addr = NEW.contract_addr;
        END IF;
    END IF;

    -- AFTER trigger: the return value is discarded. NULL rather than NEW/OLD so nothing
    -- reads it as an attempt to alter the row.
    RETURN NULL;
END;
$$;

-- +goose StatementEnd
-- +goose StatementBegin

-- FOR EACH ROW, not a statement trigger with transition tables. A DDB commit inserts exactly
-- one operation and the reorg purge deletes the handful a block carried, so there is no batch
-- worth materialising a tuplestore for -- and a row trigger costs nothing at all on the
-- overwhelmingly common case, a purge of a block that carried no DDB rows.
--
-- UPDATE OF contract_addr, so the routine re-upsert of an unchanged operation (the
-- StoreTransaction path, which does not purge and therefore always takes the ON CONFLICT DO
-- UPDATE branch) enters the function and leaves on the IS DISTINCT FROM guard without moving
-- a counter that must not move.
CREATE TRIGGER ddb_operation_op_count
AFTER INSERT OR DELETE OR UPDATE OF contract_addr ON ddb_operation
FOR EACH ROW EXECUTE FUNCTION ddb_contract_op_count_apply();

-- +goose StatementEnd
-- +goose StatementBegin

-- Seed the counter from the truth, and THE REPAIR STATEMENT for any future drift.
--
-- Re-run this verbatim if the trigger is ever bypassed: TRUNCATE fires no row triggers,
-- and neither does `pg_restore --disable-triggers`, `ALTER TABLE ... DISABLE TRIGGER`, or a
-- logical-replication apply worker with session_replication_role = replica. Those are the
-- only ways op_count can disagree with ddb_operation, and none of them occur on the ingest
-- path -- which is why this is a documented repair rather than another trigger.
UPDATE ddb_contract c
SET    op_count = COALESCE(o.n, 0)
FROM  (SELECT contract_addr, count(*) AS n FROM ddb_operation GROUP BY contract_addr) o
WHERE  o.contract_addr = c.contract_addr
   AND c.op_count IS DISTINCT FROM o.n;

-- A contract whose operations have all been purged has no row in the aggregate above, so the
-- join leaves it untouched. Zero it separately.
UPDATE ddb_contract c
SET    op_count = 0
WHERE  c.op_count <> 0
  AND  NOT EXISTS (SELECT 1 FROM ddb_operation o WHERE o.contract_addr = c.contract_addr);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TRIGGER IF EXISTS ddb_operation_op_count ON ddb_operation;
DROP FUNCTION IF EXISTS ddb_contract_op_count_apply();

-- +goose StatementEnd
