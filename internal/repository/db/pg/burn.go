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
// Three structural changes from the MongoDB implementation, all of them fixes rather
// than preferences:
//
//   - Accumulation actually happens. The Mongo writer decoded the existing document into
//     the wrong variable (`sr.Decode(&sr)`), so the "existing" burn was always a zero
//     value: the new amount replaced rather than extended the block's total, the dedup
//     and partial-burn guards were unreachable, and the final UpdateOne filtered on
//     block 0 and therefore matched nothing at all. The whole update path was a no-op
//     that reported success. Here the accumulation is done by the database, in SQL,
//     against the row itself.
//
//   - Amounts are stored as exact wei. Mongo divided by BurnDecimalsCorrection before
//     storing and then summed the truncated values, losing up to 1e10 wei per block.
//     The truncation now happens once, at read, in BurnTotal -- so the totals this
//     reports are slightly HIGHER than Mongo's, and correct.
//
//   - Dedup is structural. The included transaction hashes live in burn_tx under a
//     composite primary key, so a hash cannot be recorded twice for a block no matter
//     how often the burn is re-delivered. Mongo appended unconditionally into an array
//     that grew without bound.
//
// The running total lives in meta_counter('burn_total_wei') and is moved inside the same
// transaction as the burn row. A sum() over `burn` would be a full-table numeric
// aggregate over one-row-per-block for a figure that changes once per block.

// StoreBurn records a native NEC burn for a block, accumulating into any burn already
// stored for that block.
//
// The transaction hash list is the idempotency oracle:
//
//   - every incoming hash already recorded  -> re-delivery, no amounts move
//   - no incoming hash recorded             -> a genuinely new burn, amounts accumulate
//   - some but not all recorded             -> rejected, exactly as the Mongo writer
//     intended to (it could not, see above). A partial overlap means the caller has
//     merged two different views of the block and there is no correct amount to add.
//
// A burn carrying no hashes at all has no such signal and is therefore always added,
// which matches the Mongo behaviour. Callers that can re-deliver must send the hashes.
func (s *Store) StoreBurn(ctx context.Context, burn *types.NecBurn) error {
	// Mirrors the Mongo guard: a nil burn is not an error, it is nothing to do.
	if burn == nil {
		return nil
	}

	amount, err := Wei(burn.Amount.ToInt())
	if err != nil {
		return fmt.Errorf("burn at #%d: %w", uint64(burn.BlockNumber), err)
	}

	blockNumber := int64(burn.BlockNumber)
	hashes := make([][]byte, 0, len(burn.TxList))
	for i := range burn.TxList {
		hashes = append(hashes, HashVal(burn.TxList[i]))
	}

	return s.pool.InTx(ctx, func(ctx context.Context, tx Tx) error {
		// The burn row must exist before burn_tx can reference it. Amount and count start
		// at zero and are moved by the UPDATE below, so the insert and the accumulate path
		// are the same code -- a first burn and a later addition differ only in whether
		// this insert did anything.
		if _, err := tx.Exec(ctx, `
			INSERT INTO burn (block_number, ts, amount_wei, tx_count)
			VALUES ($1, $2, 0, 0)
			ON CONFLICT (block_number) DO NOTHING`,
			blockNumber, burn.BlkTimeStamp.UTC()); err != nil {
			return burnWriteError(blockNumber, err)
		}

		newHashes := 0
		if len(hashes) > 0 {
			inserted, total, err := claimBurnTxHashes(ctx, tx, blockNumber, hashes)
			if err != nil {
				return burnWriteError(blockNumber, err)
			}

			// Full replay. Commit -- the hash rows are unchanged and the amounts must not
			// move a second time.
			if inserted == 0 {
				return nil
			}
			if inserted < total {
				s.log.Criticalf("invalid partial burn received at #%d: %d of %d transactions already recorded",
					blockNumber, total-inserted, total)
				return fmt.Errorf("partial burn update rejected at #%d", blockNumber)
			}
			newHashes = inserted
		}

		// tx_count counts hashes actually claimed, not hashes offered, so it stays equal to
		// the number of burn_tx rows even if a caller repeats a hash within one call.
		if _, err := tx.Exec(ctx, `
			UPDATE burn
			SET    amount_wei = amount_wei + $2::NUMERIC,
			       tx_count   = tx_count + $3,
			       ts         = $4
			WHERE  block_number = $1`,
			blockNumber, amount, newHashes, burn.BlkTimeStamp.UTC()); err != nil {
			return burnWriteError(blockNumber, err)
		}

		// Same transaction as the row above, deliberately: a total maintained separately
		// would drift permanently on any crash between the two, and nothing recomputes it.
		ct, err := tx.Exec(ctx, `
			UPDATE meta_counter
			SET    value = value + $1::NUMERIC, updated_at = now()
			WHERE  key = 'burn_total_wei'`, amount)
		if err != nil {
			return fmt.Errorf("can not update the burned total for #%d: %w", blockNumber, err)
		}
		if ct.RowsAffected() == 0 {
			// Migration 00001 seeds this row. Its absence means the running total has been
			// silently not-maintained, so refuse the write rather than record a burn that
			// the total will never include.
			return fmt.Errorf("burn total counter row is missing; burn at #%d not stored", blockNumber)
		}
		return nil
	})
}

// claimBurnTxHashes inserts the transaction hashes for a block and reports how many were
// new against how many were offered.
//
// One statement so the claim and the count cannot disagree, and so the composite primary
// key on burn_tx serves as both the conflict target and the dedup probe -- there is no
// separate SELECT that a concurrent writer could slip between.
//
// The incoming list is made DISTINCT first: a caller repeating a hash within a single call
// would otherwise inflate `total` and make its own delivery look partial.
func claimBurnTxHashes(ctx context.Context, q Querier, blockNumber int64, hashes [][]byte) (inserted, total int, err error) {
	err = q.QueryRow(ctx, `
		WITH incoming(tx_hash) AS (
		    SELECT DISTINCT unnest($2::BYTEA[])
		),
		ins AS (
		    INSERT INTO burn_tx (block_number, tx_hash)
		    SELECT $1, tx_hash FROM incoming
		    ON CONFLICT (block_number, tx_hash) DO NOTHING
		    RETURNING 1
		)
		SELECT (SELECT count(*) FROM ins)::INT,
		       (SELECT count(*) FROM incoming)::INT`,
		blockNumber, hashes).Scan(&inserted, &total)
	if err != nil {
		return 0, 0, fmt.Errorf("can not record burn transactions: %w", err)
	}
	return inserted, total, nil
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
