package pg

import (
	"context"
	"fmt"
	"math/big"
	"ncogearthchain-api-graphql/internal/types"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// Daily transaction flow: the trx_volume aggregate and the two speed gauges.
//
// trx_volume is a pure cache over `tx` -- every row can be recomputed from the
// transaction table with one INSERT ... SELECT -- which is why the update path here is a
// full replace of the affected days rather than an accumulation.
//
// The one substantive change from the MongoDB version is the unit of the stored amount.
// Mongo summed `amo`, a per-transaction int64 of value_wei/1e9 truncated at write time,
// and the resolver multiplied the sum back by 1e9. That round trip is not the identity:
// every transaction lost its sub-gwei remainder before it was ever added up, so the
// daily total was short by up to (1e9 - 1) wei per transaction. Here the sum is taken
// over value_wei itself, in uint256, and is exact.

// trxVolumeColumns is the projection shared by every trx_volume read.
const trxVolumeColumns = `day, tx_count, volume, gas_used`

// maxDailyFlowDays caps a daily-flow list. Carried over from the MongoDB limit of 365.
const maxDailyFlowDays = 365

// TrxDailyFlowList loads daily transaction volumes in the given closed date range.
//
// Both bounds are inclusive and both are optional, matching the Mongo filter which
// simply omitted the missing side. The result is ascending by day and never nil: an
// empty range is an empty slice with a nil error, not a not-found.
//
// The LIMIT selects the NEWEST 365 days of the requested window, not the oldest. Mongo
// ordered ascending and truncated, so a caller asking for five years silently received
// the first year and a chart that appeared to stop in the distant past -- the truncation
// was invisible and the visible part was the least interesting. Ordering descending and
// reversing in Go keeps the same row budget while anchoring the window to `to`, which is
// the bound the caller actually chose.
func (s *Store) TrxDailyFlowList(ctx context.Context, from *time.Time, to *time.Time) ([]*types.DailyTrxVolume, error) {
	// Day bucketing is UTC everywhere in this file, so the bounds are normalised to UTC
	// in Go rather than cast in SQL. A bare `$1::date` would be resolved against the
	// server's TimeZone setting, which makes the same query return different days on
	// two machines.
	rows, err := s.pool.Query(ctx, `
		SELECT `+trxVolumeColumns+`
		FROM   trx_volume
		WHERE  ($1::date IS NULL OR day >= $1::date)
		  AND  ($2::date IS NULL OR day <= $2::date)
		ORDER  BY day DESC
		LIMIT  `+itoa(maxDailyFlowDays),
		dayBound(from), dayBound(to))
	if err != nil {
		return nil, fmt.Errorf("can not load daily transaction flow: %w", err)
	}
	defer rows.Close()

	list := make([]*types.DailyTrxVolume, 0, 64)
	for rows.Next() {
		v, err := scanDailyTrxVolume(rows)
		if err != nil {
			return nil, fmt.Errorf("can not scan daily transaction volume: %w", err)
		}
		list = append(list, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("can not load daily transaction flow: %w", err)
	}

	// back to ascending, the order the API has always returned
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	return list, nil
}

// TrxGasSpeed provides the amount of gas consumed by transactions per second over the
// given time range.
//
// Served by tx_ts_brin: an unordered aggregate over a wide contiguous window is exactly
// the shape a BRIN index is for.
func (s *Store) TrxGasSpeed(ctx context.Context, from *time.Time, to *time.Time) (float64, error) {
	if from == nil || to == nil {
		return 0.0, fmt.Errorf("both ends of the time range are required")
	}

	// Guard kept from the Mongo version: it is what stops the division below from being
	// a divide by zero (equal bounds) or from returning a negative rate (inverted).
	if !from.Before(*to) {
		return 0.0, fmt.Errorf("invalid time range requested")
	}

	where, args := NewFilter().
		Gte("ts", from.UTC()).
		Lte("ts", to.UTC()).
		Where(0)

	// COALESCE, not SUM(COALESCE(gas_used, 0)): the difference is the empty window. A
	// quiet stretch of chain with no transactions in it is a rate of zero, not a
	// failure -- the Mongo path returned "gas speed aggregation failure" there, because
	// $group produced no document and the cursor was empty. Per-row the two spellings
	// agree, and NULL gas_used (no receipt seen) must stay skipped rather than counted
	// as zero gas.
	var gas int64
	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(gas_used), 0)::BIGINT FROM tx `+where, args...).Scan(&gas); err != nil {
		return 0.0, fmt.Errorf("can not collect gas speed: %w", err)
	}

	// The sum stays an exact integer until the single conversion here.
	return float64(gas) / to.Sub(*from).Seconds(), nil
}

// TrxRecentTrxSpeed provides the number of transactions per second over the last `sec`
// seconds.
//
// The window is bounded at BOTH ends -- ts >= from AND ts <= now -- so a block carrying a
// timestamp in the future can no longer inflate the rate until the wall clock catches up.
//
// Known, preserved quirk: the divisor is always `sec`, even when the chain is younger than
// that, so the figure understates during the first minutes after genesis. Cosmetic.
func (s *Store) TrxRecentTrxSpeed(ctx context.Context, sec int32) (float64, error) {
	if sec < 60 {
		sec = 60
	}

	// Computed in Go, not as now() in SQL, so both boundaries are the same values the caller
	// can log and so the statement stays parameterised and plan-cacheable.
	now := time.Now().UTC()
	from := now.Add(time.Duration(-sec) * time.Second)

	var total int64
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM tx WHERE ts >= $1 AND ts <= $2`, from, now).Scan(&total); err != nil {
		return 0, fmt.Errorf("can not count recent transactions: %w", err)
	}
	if total == 0 {
		return 0, nil
	}
	return float64(total) / float64(sec), nil
}

// TrxDailyFlowUpdate recomputes the daily aggregate for every day from `from` onwards.
//
// The caller runs this every few minutes over a rolling two-day window, so the write
// must be a REPLACE and never an accumulation -- `+= EXCLUDED` would multiply each day's
// totals by the number of ticks that have run since midnight.
func (s *Store) TrxDailyFlowUpdate(ctx context.Context, from time.Time) error {
	s.log.Debugf("updating trx flow after %s", from.String())
	return s.rebuildDailyFlow(ctx, from)
}

// TrxDailyFlowRebuild recomputes the whole aggregate from genesis.
//
// The two-day trailing window is a caller policy, not a property of the data: trx_volume
// is derived entirely from `tx`, so a full rebuild is the same statement with an open
// left bound. It exists because the Mongo rows were built from the lossy per-transaction
// amount and cannot be corrected in place -- they have to be recomputed from value_wei.
func (s *Store) TrxDailyFlowRebuild(ctx context.Context) error {
	s.log.Noticef("rebuilding the whole daily trx flow aggregate")
	return s.rebuildDailyFlow(ctx, time.Time{})
}

// rebuildDailyFlow replaces the aggregate for all days at or after `from`.
func (s *Store) rebuildDailyFlow(ctx context.Context, from time.Time) error {
	// Snap the bound down to the UTC day boundary. Both statements below have to agree
	// on which days they touch: if `from` fell mid-day, the DELETE would remove that
	// whole day while the INSERT would rebuild it from only the transactions after
	// `from`, and the day would be left permanently undercounted. The scheduled caller
	// already passes a midnight, but the invariant belongs here, not in the caller.
	//
	// Truncate works on the absolute instant, and the Unix epoch is itself a UTC
	// midnight, so a 24h truncation lands on a UTC midnight.
	fromDay := from.UTC().Truncate(24 * time.Hour)

	return s.pool.InTx(ctx, func(ctx context.Context, tx Tx) error {
		// Delete first so a day that lost all of its transactions -- a reorg emptied it,
		// or the window was rebuilt after a purge -- disappears from the aggregate. A
		// bare upsert produces no group for such a day and therefore leaves the old
		// total standing forever. Mongo's $merge had exactly this hole.
		if _, err := tx.Exec(ctx,
			`DELETE FROM trx_volume WHERE day >= $1::date`,
			fromDay.Format(time.DateOnly)); err != nil {
			return fmt.Errorf("can not clear daily trx flow from %s: %w", fromDay, err)
		}

		// ON CONFLICT is redundant after the DELETE within this transaction and is kept
		// only so two concurrent updaters cannot turn an overlap into a hard failure.
		//
		// COALESCE on gas_used is mandatory, not defensive: tx.gas_used is nullable
		// (no receipt yet) while trx_volume.gas_used is NOT NULL, so a single receiptless
		// transaction in the window would abort the whole statement.
		ct, err := tx.Exec(ctx, `
			INSERT INTO trx_volume (day, tx_count, volume, gas_used)
			SELECT (ts AT TIME ZONE 'UTC')::date,
			       count(*),
			       SUM(value_wei),
			       COALESCE(SUM(gas_used), 0)
			FROM   tx
			WHERE  ts >= $1
			GROUP  BY 1
			ON CONFLICT (day) DO UPDATE SET
			       tx_count = EXCLUDED.tx_count,
			       volume   = EXCLUDED.volume,
			       gas_used = EXCLUDED.gas_used`, fromDay)
		if err != nil {
			return fmt.Errorf("can not update daily trx flow from %s: %w", fromDay, err)
		}

		s.log.Debugf("daily trx flow updated for %d day(s)", ct.RowsAffected())
		return nil
	})
}

// dayBound renders an optional time bound as an optional YYYY-MM-DD date string.
//
// Returns a nil *string for a nil bound, which the query reads as "no bound on this
// side" -- NULL and a date are different things here, and the IS NULL test in the SQL is
// what keeps an absent bound from becoming an impossible one.
func dayBound(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.DateOnly)
	return &s
}

// scanDailyTrxVolume maps one trx_volume row onto the domain type.
func scanDailyTrxVolume(row rowScanner) (*types.DailyTrxVolume, error) {
	var (
		day     pgtype.Date
		count   int64
		volume  pgtype.Numeric
		gasUsed int64
	)

	if err := row.Scan(&day, &count, &volume, &gasUsed); err != nil {
		return nil, err
	}
	if !day.Valid {
		// day is the primary key, so this cannot happen through any supported path.
		return nil, fmt.Errorf("daily trx volume row carries no day")
	}

	amount, err := FromWei(volume)
	if err != nil {
		return nil, err
	}
	if amount == nil {
		// volume is NOT NULL in the schema; a NULL here means the row arrived by some
		// route other than this package.
		return nil, fmt.Errorf("daily trx volume for %s carries no amount", day.Time.Format(time.DateOnly))
	}

	v := &types.DailyTrxVolume{
		Day: day.Time.Format(time.DateOnly),

		// Stamp has no column of its own -- it was Mongo's $toDate of the string key --
		// so it is derived back from the day. It is unreachable from the GraphQL schema
		// and is populated only to keep the struct self-consistent.
		Stamp:   day.Time.UTC(),
		Counter: count,
		Gas:     gasUsed,
		Amount:  amount,
	}

	// AmountAdjusted is the legacy gwei-scaled field the Mongo schema stored, kept
	// populated so the MongoDB-era resolver path keeps working unchanged. Amount is the
	// authoritative value; this one is truncated to gwei and is left at zero rather than
	// wrapped if a day's total ever exceeds what an int64 of gwei can hold.
	gwei := new(big.Int).Div(amount, types.TransactionDecimalsCorrection)
	if gwei.IsInt64() {
		v.AmountAdjusted = gwei.Int64()
	}

	return v, nil
}
