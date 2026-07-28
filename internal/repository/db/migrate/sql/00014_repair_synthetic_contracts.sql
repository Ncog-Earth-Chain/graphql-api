-- +goose Up
-- +goose StatementBegin

-- Remove contract rows that were written for addresses which are not contracts.
--
-- WHAT WENT WRONG. The account dispatcher decided "this account is a contract" from a
-- property of the TRANSACTION rather than of the account:
--
--     if acc.trx.ContractAddress != nil { processContract(acc) }
--
-- pushAccounts queues the sender, the recipient and the created contract of one
-- transaction, and all three carry that same *types.Transaction. So on every
-- contract-creating transaction ALL THREE took the contract branch, and each got a row
-- written into `contract` -- including the sender, which is an ordinary wallet.
--
-- It surfaced as isDDB being true for a plain EOA. For a DDB commit the recipient is the
-- DDB sentinel address and ContractAddress is the data contract, so detectContract found no
-- ERC interface on the sender's code-less address, fell through to the generic contract
-- path, and set IsDDB from trx.To -- tagging the SENDER as a DDB contract. The read path
-- has no way to tell such a row from a real one.
--
-- The ingest now gates on identity (*acc.trx.ContractAddress == *acc.addr), so no new rows
-- of this shape appear. This migration repairs the ones already written, which the ingest
-- cannot do on its own: it upserts with ON CONFLICT (address) DO UPDATE, which rewrites a
-- bogus row but never removes it.
--
-- THE INVARIANT. A contract row's address must equal its own deploy transaction's
-- created_contract. That is precisely what the buggy branch failed to check, so rows
-- violating it are exactly the rows it produced.
--
-- WHAT IS DELIBERATELY LEFT ALONE:
--
--   * Rows whose deploy_tx is not in the index at all. Genesis and precompiled contracts
--     (the SFC among them) were never created by an indexed transaction, so the join finds
--     nothing and they survive. An INNER join, not an anti-join, is what makes that true.
--   * Verified contracts. is_verified is set only by the verification mutation and carries
--     human-supplied source; contract_verification cascades on delete, so removing one
--     would destroy that source. A verified row that also violates the invariant is
--     strange enough to deserve a human, so it is reported rather than deleted.

DO $$
DECLARE
    bogus     BIGINT;
    protected BIGINT;
BEGIN
    SELECT count(*) INTO protected
    FROM   contract c
    JOIN   tx t ON t.hash = c.deploy_tx
    WHERE  c.is_verified
      AND  (t.created_contract IS NULL OR t.created_contract <> c.address);

    IF protected > 0 THEN
        RAISE WARNING
            'contract repair: % VERIFIED row(s) do not match their deploy transaction and were left in place; review them by hand',
            protected;
    END IF;

    WITH removed AS (
        DELETE FROM contract c
        USING  tx t
        WHERE  t.hash = c.deploy_tx
          AND  NOT c.is_verified
          AND  (t.created_contract IS NULL OR t.created_contract <> c.address)
        RETURNING 1
    )
    SELECT count(*) INTO bogus FROM removed;

    RAISE NOTICE 'contract repair: removed % row(s) written for addresses that are not contracts', bogus;
END
$$;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Irreversible by design: the deleted rows were wrong, and the data needed to recreate them
-- (which addresses were mistakenly recorded) is exactly what was removed. Re-running the
-- scanner over the affected blocks rebuilds `contract` correctly from the chain.
SELECT 1;

-- +goose StatementEnd
