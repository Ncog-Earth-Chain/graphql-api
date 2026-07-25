-- +goose Up
-- +goose StatementBegin
-- Stake DELEGATION is not offered on this chain. The delegation query surface (delegation,
-- delegationsOf, delegationsByAddress, Account.delegations, Staker.delegations), its resolvers and
-- domain types, the repository read/write path, and the SFC ingest handler that populated this
-- table have all been removed. Nothing reads or writes `delegation` any longer. It is a leaf table
-- (no foreign key references it) and was never in the block purge set, so the drop is safe; its
-- indexes are removed with it.
DROP TABLE IF EXISTS delegation;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Intentional no-op: the delegation feature was removed wholesale (schema, resolvers, ingest, types),
-- so recreating an empty table on rollback would only reintroduce a data model nothing maintains.
-- When stake delegation is reintroduced it will define its own schema.
SELECT 1;
-- +goose StatementEnd
