package pg

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// Native NEC burn reads and writes.
//
// Structural changes from the MongoDB implementation, all of them fixes rather than
// preferences:
//
//   - The write is reorg-aware. Each delivery carries a block's ENTIRE burn, so a second
//     delivery for a block is either a verbatim re-delivery or a reorg -- never a partial
//     addition. The stored amount is REPLACED with the delivered one and the running total
//     is moved by the difference, so a re-delivery moves it by zero and a reorg reconciles
//     it exactly. The Mongo path (and the first PostgreSQL version) ADDED every delivery and
//     used the transaction-hash set to dedup, which could not distinguish a reorg -- whose
//     transactions are all new -- from a brand-new burn, so a reorged block stacked a second
//     burn on top of the first and inflated the global total permanently. (The Mongo writer
//     never even got that far: it decoded the existing document into the wrong variable, so
//     the accumulation, dedup and partial guards were all unreachable and the final UpdateOne
//     matched nothing -- the whole path was a no-op that reported success.)
//
//   - Amounts are stored as exact wei. Mongo divided by BurnDecimalsCorrection before
//     storing and then summed the truncated values, losing up to 1e10 wei per block.
//     The truncation now happens once, at read, in BurnTotal -- so the totals this
//     reports are slightly HIGHER than Mongo's, and correct.
//
//   - Dedup is structural. The included transaction hashes live in burn_tx under a
//     composite primary key, so a hash cannot be recorded twice for a block. On a reorg the
//     recorded set is replaced, not merged, so it reflects the block that actually won.
//
// The running total lives in meta_counter('burn_total_wei') and is moved inside the same
// transaction as the burn row. A sum() over `burn` would be a full-table numeric aggregate
// over one-row-per-block for a figure that changes once per block.
//
// burn and burn_tx are deliberately NOT in the reorg purge set: this write corrects a
// block's burn in place rather than relying on delete-and-rebuild.

// StoreBurn records the native NEC burn for a block, replacing any burn already stored for
// that block and reconciling the running total by the difference.
//
// Each delivery carries the block's whole burn: the dispatcher accumulates every transaction
// of a block in memory and calls this once, when the block boundary is crossed. A second
// call for the same block is therefore a re-delivery (after a restart) or a reorg that
// changed the block's contents; both are handled by replacing the amount and moving the
// total by (new - old).
func (s *Store) StoreBurn(ctx context.Context, burn *types.NecBurn) error {
	// A nil burn is not an error, it is nothing to do.
	if burn == nil {
		return nil
	}

	amount, err := Wei(burn.Amount.ToInt())
	if err != nil {
		return fmt.Errorf("burn at #%d: %w", uint64(burn.BlockNumber), err)
	}

	blockNumber := int64(burn.BlockNumber)

	// De-duplicate the hash list in Go so tx_count equals the number of burn_tx rows even
	// if a caller repeats a hash within one delivery.
	seen := make(map[string]struct{}, len(burn.TxList))
	hashes := make([][]byte, 0, len(burn.TxList))
	for i := range burn.TxList {
		hb := HashVal(burn.TxList[i])
		k := string(hb)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		hashes = append(hashes, hb)
	}
	txCount := len(hashes)

	return s.pool.InTx(ctx, func(ctx context.Context, tx Tx) error {
		// Upsert the block's burn to the delivered value and move the running total by the
		// delta, atomically. `prev` reads the pre-write amount under the statement snapshot
		// (a data-modifying WITH cannot observe its siblings' effects), `up` writes the new
		// amount and returns it, and the outer UPDATE applies (new - old) -- which is
		// negative when a reorg reduced the block's burn, and SQL handles that. A missing
		// block row surfaces here as a 23503 foreign-key violation, annotated below.
		ct, err := tx.Exec(ctx, `
			WITH prev AS (
			    SELECT amount_wei AS old FROM burn WHERE block_number = $1
			),
			up AS (
			    INSERT INTO burn (block_number, ts, amount_wei, tx_count)
			    VALUES ($1, $2, $3::NUMERIC, $4)
			    ON CONFLICT (block_number) DO UPDATE
			       SET amount_wei = EXCLUDED.amount_wei,
			           tx_count   = EXCLUDED.tx_count,
			           ts         = EXCLUDED.ts
			    RETURNING amount_wei AS new
			)
			UPDATE meta_counter
			SET    value = value + ((SELECT new FROM up) - COALESCE((SELECT old FROM prev), 0)),
			       updated_at = now()
			WHERE  key = 'burn_total_wei'`,
			blockNumber, burn.BlkTimeStamp.UTC(), amount, txCount)
		if err != nil {
			return burnWriteError(blockNumber, err)
		}
		if ct.RowsAffected() == 0 {
			// Migration 00001 seeds this row. Its absence means the running total has been
			// silently not-maintained, so refuse the write rather than record a burn the
			// total will never include. The burn row and the counter move together or not
			// at all: this whole function is one transaction.
			return fmt.Errorf("burn total counter row is missing; burn at #%d not stored", blockNumber)
		}

		// Replace the recorded transaction set for the block. On a reorg the old set names
		// the losing block's transactions, so it is cleared rather than merged. burn_tx is
		// write-only (a record, never read for display), so replacing it has no read effect.
		if _, err := tx.Exec(ctx, `DELETE FROM burn_tx WHERE block_number = $1`, blockNumber); err != nil {
			return burnWriteError(blockNumber, err)
		}
		if len(hashes) > 0 {
			if _, err := tx.Exec(ctx, `
				INSERT INTO burn_tx (block_number, tx_hash)
				SELECT $1, unnest($2::BYTEA[])
				ON CONFLICT (block_number, tx_hash) DO NOTHING`,
				blockNumber, hashes); err != nil {
				return burnWriteError(blockNumber, err)
			}
		}
		return nil
	})
}

// ClearBurn removes any recorded burn for a block and reconciles the running total, returning the
// amount that was cleared (nil if the block had no burn recorded).
//
// StoreBurn is driven by the transaction fan-out and is called once per block when the block
// boundary is crossed; it never fires for a block with no transactions. So when a reorg re-ingests
// a block that previously had transactions (and thus a burn row and a contribution to
// burn_total_wei) as a block with ZERO transactions, nothing corrects the old burn: it is not in
// the reorg purge set (this package corrects burns in place, see the file header) and the
// dispatcher never delivers the emptied block. This is the zero-transaction counterpart: it DELETEs
// the block's burn row and moves the running total down by exactly that amount, in one transaction,
// so the stale row and its inflated total do not survive the reorg.
//
// A block that never had a burn recorded is a no-op (nil, nil) -- we must not write a phantom
// zero-amount row, which would show up in BurnList.
func (s *Store) ClearBurn(ctx context.Context, blockNumber uint64) (*big.Int, error) {
	bn := int64(blockNumber)

	var cleared *big.Int
	err := s.pool.InTx(ctx, func(ctx context.Context, tx Tx) error {
		// Delete the burn row and read the amount it held in the same statement. No row means the
		// block never burned anything -- nothing to reconcile, and no phantom row to create.
		var old pgtype.Numeric
		err := tx.QueryRow(ctx,
			`DELETE FROM burn WHERE block_number = $1 RETURNING amount_wei`, bn).Scan(&old)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return burnWriteError(bn, err)
		}

		// A burn row existed; move the running total down by exactly what it contributed. The row
		// and the counter move together or not at all -- this whole function is one transaction,
		// mirroring StoreBurn.
		ct, err := tx.Exec(ctx, `
			UPDATE meta_counter
			SET    value = value - $1::NUMERIC, updated_at = now()
			WHERE  key = 'burn_total_wei'`, old)
		if err != nil {
			return burnWriteError(bn, err)
		}
		if ct.RowsAffected() == 0 {
			return fmt.Errorf("burn total counter row is missing; burn at #%d not cleared", bn)
		}

		// Drop the recorded transaction set for the block as well; the block it belonged to lost.
		if _, err := tx.Exec(ctx, `DELETE FROM burn_tx WHERE block_number = $1`, bn); err != nil {
			return burnWriteError(bn, err)
		}

		cleared, err = FromWei(old)
		if err != nil {
			return fmt.Errorf("burn at #%d: %w", bn, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return cleared, nil
}

// burnWriteError annotates a burn write failure, calling out the one failure mode that is
// an ordering problem rather than a data problem.
//
// burn.block_number references block(number), which MongoDB had no equivalent of. The
// dispatcher flushes block N on first seeing a transaction of block N+1, so the block row
// is normally already committed -- but a burn that arrives ahead of its block now fails
// loudly with 23503 instead of silently creating an orphan nobody can join to.
func burnWriteError(blockNumber int64, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return fmt.Errorf("burn at #%d arrived before block #%d was stored: %w", blockNumber, blockNumber, err)
	}
	return fmt.Errorf("can not store burn at #%d: %w", blockNumber, err)
}

// BurnTotal reports the total burned amount, in units of wei / BurnDecimalsCorrection.
//
// The unit is NOT wei and must not become wei: both resolvers scale this value back up --
// necBurnedTotal multiplies by BurnDecimalsCorrection to get wei, necBurnedTotalAmount
// divides by BurnNECDecimalsCorrection to get NEC. Returning raw wei here would inflate
// both by 1e10.
//
// The divisor is bound from types.BurnDecimalsCorrection rather than written into the SQL
// so the two cannot drift apart. div() truncates toward zero, matching the big.Int Div the
// Mongo path used; a plain ::BIGINT cast would round instead.
//
// This is an O(1) counter read. Mongo ran a $group/$sum over the whole collection.
func (s *Store) BurnTotal(ctx context.Context) (int64, error) {
	var total int64

	// An out-of-range result raises here rather than wrapping silently. int64 in these
	// units overflows at roughly 9.2e10 NEC; failing loudly is the correct outcome.
	err := s.pool.QueryRow(ctx, `
		SELECT div(value, $1::NUMERIC)::BIGINT
		FROM   meta_counter
		WHERE  key = 'burn_total_wei'`, MustWei(types.BurnDecimalsCorrection)).Scan(&total)

	if errors.Is(err, pgx.ErrNoRows) {
		// Migration 00001 seeds this row at 0, so "no row" is a broken install, not an
		// empty chain -- and it must not be reported as a total of zero. The cache
		// snapshots this value once and then only adds per-block deltas for the next 1200
		// blocks, so a fabricated zero would persist long after the underlying problem.
		return 0, fmt.Errorf("burned fee total is missing from meta_counter")
	}
	if err != nil {
		return 0, fmt.Errorf("can not read the burned fee total: %w", err)
	}
	return total, nil
}

// BurnList returns the most recent block burns, newest first.
//
// Served by burn_pkey as a backward index scan, so there is no sort step.
func (s *Store) BurnList(ctx context.Context, count int32) ([]types.NecBurn, error) {
	// The resolver already clamps to 1..50, but the store must not depend on a caller to
	// bound the work it does. Page.Reverse is unused: this is a top-N list with no cursor,
	// so there is no position to walk backwards from and the order is always descending.
	page := NewPage(count, maxListLimit)

	rows, err := s.pool.Query(ctx, `
		SELECT block_number, ts, amount_wei
		FROM   burn
		ORDER  BY block_number DESC
		LIMIT  $1`, page.Limit)
	if err != nil {
		return nil, fmt.Errorf("can not list burns: %w", err)
	}
	defer rows.Close()

	// Non-nil: the GraphQL field is [NecBlockBurn!]!.
	list := make([]types.NecBurn, 0, page.Limit)

	for rows.Next() {
		var (
			blockNumber int64
			ts          pgtype.Timestamptz
			amountWei   pgtype.Numeric
		)
		if err := rows.Scan(&blockNumber, &ts, &amountWei); err != nil {
			// Mongo logged a decode failure and skipped the row, quietly returning a short
			// list. A row that will not scan means the column no longer holds what this
			// code expects, and hiding that produces a burn list missing arbitrary blocks.
			return nil, fmt.Errorf("can not scan burn: %w", err)
		}

		amount, err := FromWei(amountWei)
		if err != nil {
			return nil, fmt.Errorf("burn at #%d: %w", blockNumber, err)
		}
		if amount == nil {
			// amount_wei is NOT NULL, so this is unreachable through the schema.
			return nil, fmt.Errorf("burn at #%d has no amount", blockNumber)
		}

		list = append(list, types.NecBurn{
			BlockNumber:  hexutil.Uint64(blockNumber),
			BlkTimeStamp: ts.Time,

			// Full-precision wei, matching what UnmarshalBSON reconstructed from the
			// `value` string -- not the lossy `amount` field that BurnTotal summed.
			Amount: hexutil.Big(*new(big.Int).Set(amount)),

			// TxList is deliberately nil, not empty. NecBlockBurn exposes only
			// blockNumber, timestamp, amount and necValue, so joining burn_tx would cost a
			// row per included transaction for a field no client can read. nil says "not
			// loaded"; an empty slice would claim the block burned no transactions.
			TxList: nil,
		})
	}
	return list, rows.Err()
}
