-- +goose Up
-- +goose StatementBegin
-- The `swap` table and the `last_swap_block` meta counter are Uniswap-module leftovers. The
-- Uniswap DEX module was removed from the explorer, so nothing writes swap rows and nothing
-- reads them; the table has always been empty. Drop both so the schema stops carrying a data
-- set the explorer no longer maintains. `swap` is a leaf table (no foreign key references it),
-- so the drop is safe.
DROP TABLE IF EXISTS swap;
-- +goose StatementEnd

-- +goose StatementBegin
DELETE FROM meta_counter WHERE key = 'last_swap_block';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Intentional no-op: the dropped table carried no data worth restoring, and recreating an
-- empty Uniswap-era table (plus its dead counter) on rollback would only re-introduce the
-- dead schema this migration exists to remove.
SELECT 1;
-- +goose StatementEnd
