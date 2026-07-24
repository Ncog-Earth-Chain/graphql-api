package pg

import (
	"context"
	"fmt"
	"math/big"
	"ncogearthchain-api-graphql/internal/types"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// Gas price ticks.
//
// One row per sampling period (10 minutes by default, svc/scan_gas.go), read back only
// over a recent time range. Two things change relative to MongoDB.
//
// Retention. Mongo expressed it as a TTL index, and the declaration was broken: the same
// ascending index on `to` was requested twice with different options, which Mongo rejects
// as IndexOptionsConflict, and since all index models went in one CreateMany the failure
// took every index with it. The error was logged and discarded, so production ran with no
// indexes and no expiry at all. Here retention is a partition DROP driven by
// PruneGasPricePartitions, which is constant time and leaves no dead tuples behind.
//
// Money. The amounts are uint256 columns, not int64, so they go through Wei/FromWei like
// every other value in this package. types.GasPricePeriod still carries int64 fields, so
// the read path narrows explicitly and fails loudly rather than wrapping.

// gasTickColumns is the projection every gas tick read shares, in struct-scan order.
const gasTickColumns = `
	ts_from, ts_to, period_type,
	open_wei, close_wei, min_wei, max_wei, avg_wei, tick_ns`

// maxGasPriceTicks caps a single range read.
//
// The Mongo query had no limit at all while being exported on repository.Interface, so
// any caller outside the resolver could ask for the whole table. The resolver's own
// window is 90 days, which at the 10 minute period interval is ~13k rows; this leaves
// room for a period interval four times shorter before the cap can bite, and the cap
// being hit is logged rather than silently truncating a chart.
const maxGasPriceTicks = 50000

// GasPriceRetentionMonths mirrors the 365 day retention the Mongo TTL intended. Whole
// months, because the unit of expiry is now a partition.
//
// Exported so the scheduled maintenance job in internal/svc uses the same window the
// schema was designed around instead of picking its own.
const GasPriceRetentionMonths = 12

// GasPricePartitionRunway is how many months ahead partitions are created. Inserts fail
// outright once they run past the last partition, so this is a liveness setting, not a
// tuning knob.
const GasPricePartitionRunway = 13

// AddGasPricePeriod stores one closed gas price sampling period.
//
// ON CONFLICT DO NOTHING makes a re-send idempotent. The primary key (ts_from,
// period_type) is new -- Mongo happily accepted unlimited duplicates for the same instant
// -- and the writer is a one-shot flush at the end of a period, so a second insert for the
// same start instant is a retry, not new data. DO UPDATE would let a retry overwrite a
// period that had already been aggregated into a chart.
func (s *Store) AddGasPricePeriod(ctx context.Context, gp *types.GasPricePeriod) error {
	if gp == nil {
		return fmt.Errorf("no value to store")
	}

	// The zero time.Time falls outside every partition, so an unset From would surface as
	// an opaque "no partition of relation" error attributed to the insert rather than to
	// the value. Mongo accepted it and stored a document nothing could ever find.
	if gp.From.IsZero() || gp.To.IsZero() {
		return fmt.Errorf("gas price period has no time range")
	}

	open, err := gasPriceWei("open", gp.Open)
	if err != nil {
		return err
	}
	closed, err := gasPriceWei("close", gp.Close)
	if err != nil {
		return err
	}
	min, err := gasPriceWei("min", gp.Min)
	if err != nil {
		return err
	}
	max, err := gasPriceWei("max", gp.Max)
	if err != nil {
		return err
	}
	avg, err := gasPriceWei("avg", gp.Avg)
	if err != nil {
		return err
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO gas_price_tick (ts_from, ts_to, period_type,
		                            open_wei, close_wei, min_wei, max_wei, avg_wei, tick_ns)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (ts_from, period_type) DO NOTHING`,
		gp.From.UTC(), gp.To.UTC(), int16(gp.Type),
		open, closed, min, max, avg, gp.Tick)
	if err != nil {
		// Logged as well as returned, matching the Mongo bridge: the caller
		// (repository/transaction.go) propagates it, but the gas monitor's own caller
		// only logs, so losing the message here would make a stalled ticker invisible.
		s.log.Errorf("can not store gas price value; %s", err)
		return fmt.Errorf("can not store gas price period starting %s: %w", gp.From.UTC(), err)
	}
	return nil
}

// GasPricePeriodCount returns an approximate number of stored gas price periods.
//
// Approximate by design, matching EstimatedDocumentCount, which read collection metadata
// rather than counting. An exact count(*) here would walk every monthly partition of the
// most frequently written table in the database to produce a headline figure.
//
// Summed across partitions because the parent of a partitioned table holds no rows and so
// carries reltuples = 0 itself.
func (s *Store) GasPricePeriodCount(ctx context.Context) (uint64, error) {
	var n int64
	// reltuples is -1 before a partition's first ANALYZE on PostgreSQL 14+, hence the
	// per-partition clamp; summing the raw value would subtract one per fresh partition.
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(GREATEST(c.reltuples, 0)), 0)::BIGINT
		FROM   pg_class c
		JOIN   pg_inherits i ON i.inhrelid = c.oid
		WHERE  i.inhparent = 'gas_price_tick'::regclass`).Scan(&n); err != nil {
		return 0, fmt.Errorf("can not estimate gas price period count: %w", err)
	}
	return uint64(n), nil
}

// GasPriceTicks lists gas price periods inside a time range, oldest first.
//
// Ordering is load-bearing rather than cosmetic: aggregateGasPriceTicks (resolvers/gas.go)
// walks the slice assuming From increases monotonically and folds ticks into buckets as it
// goes, so out-of-order rows produce silently wrong aggregates instead of an error.
//
// Not-found is an empty non-nil slice and a nil error, never (nil, nil) -- the resolver
// checks len() on the result.
//
// The predicate is an OVERLAP test, ts_from < to AND ts_to > from, so a period that starts
// inside the window but ends after `to` -- the newest, still-open period -- is included.
// The earlier port kept Mongo's asymmetric ts_from >= from AND ts_to <= to, which dropped
// that newest period from every "up to now" query. That was a defect, now fixed.
func (s *Store) GasPriceTicks(ctx context.Context, from *time.Time, to *time.Time) ([]types.GasPricePeriod, error) {
	// Mongo encoded a nil *time.Time as BSON null and the comparison quietly matched
	// nothing; in PostgreSQL a NULL bind makes every comparison NULL, which is also zero
	// rows and also silent. An empty chart with no explanation is the worst outcome.
	if from == nil || to == nil {
		return nil, fmt.Errorf("gas price tick range needs both a start and an end time")
	}

	// period_type is not filtered -- only GasPricePeriodTypeSuggestion exists today and
	// Mongo filtered on the time range alone -- but it is selected so the struct round
	// trips and a second sampler's rows would be visibly mixed in rather than silently
	// mislabelled.
	//
	// ORDER BY (ts_from, period_type) rather than ts_from alone: the pair is the primary
	// key, so the ordering is total and the same range always returns the same sequence.
	//
	// ts_from is the PARTITION KEY, so `ts_from < $2` lets the planner prune every future
	// partition instead of appending a scan of each -- there are 13 today and the count
	// grows with every maintenance run. The lower bound is on ts_to, which is not the
	// partition key, so past partitions are not pruned; they are finite and the LIMIT
	// bounds the scan.
	rows, err := s.pool.Query(ctx, `
		SELECT `+gasTickColumns+`
		FROM   gas_price_tick
		WHERE  ts_from < $2 AND ts_to > $1
		ORDER  BY ts_from ASC, period_type ASC
		LIMIT  $3`,
		from.UTC(), to.UTC(), maxGasPriceTicks)
	if err != nil {
		s.log.Errorf("can not pull gas price ticks; %s", err.Error())
		return nil, fmt.Errorf("can not load gas price ticks: %w", err)
	}
	defer rows.Close()

	list := make([]types.GasPricePeriod, 0, 256)
	for rows.Next() {
		gp, err := scanGasPricePeriod(rows)
		if err != nil {
			s.log.Errorf("could not decode gas price tick; %s", err.Error())
			return nil, fmt.Errorf("can not scan gas price tick: %w", err)
		}
		list = append(list, *gp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("can not read gas price ticks: %w", err)
	}

	if len(list) == maxGasPriceTicks {
		s.log.Warningf("gas price tick range %s..%s hit the %d row cap; the result is truncated",
			from.UTC(), to.UTC(), maxGasPriceTicks)
	}
	return list, nil
}

// EnsureGasPricePartitions creates the monthly partitions covering [from, from+months)
// and reports how many were new. Idempotent.
//
// This has to be called on a schedule. The migration seeds 13 months of runway once, and
// nothing else creates partitions, so an unattended deployment starts failing every insert
// with "no partition of relation gas_price_tick found" roughly a year after deploy.
func (s *Store) EnsureGasPricePartitions(ctx context.Context, from time.Time, months int) (int, error) {
	if months < 1 {
		return 0, fmt.Errorf("partition runway must be at least one month, got %d", months)
	}

	var made int
	if err := s.pool.QueryRow(ctx,
		`SELECT ensure_gas_price_partitions($1, $2)`, from.UTC(), months).Scan(&made); err != nil {
		return 0, fmt.Errorf("can not create gas price partitions: %w", err)
	}
	return made, nil
}

// PruneGasPricePartitions drops whole partitions older than the retention window and
// returns the names it dropped.
//
// This is the replacement for the Mongo TTL index. The names come back rather than a count
// because silently destroying data is not an acceptable outcome for a retention job -- the
// caller is expected to log them.
func (s *Store) PruneGasPricePartitions(ctx context.Context, retainMonths int) ([]string, error) {
	// The SQL function raises on a value below 1, but catching it here keeps a caller
	// bug from arriving as a database exception with no context about who asked.
	if retainMonths < 1 {
		return nil, fmt.Errorf("gas price retention must be at least one month, got %d", retainMonths)
	}

	rows, err := s.pool.Query(ctx, `SELECT dropped FROM prune_gas_price_partitions($1)`, retainMonths)
	if err != nil {
		return nil, fmt.Errorf("can not prune gas price partitions: %w", err)
	}
	defer rows.Close()

	var dropped []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		dropped = append(dropped, name)
	}
	return dropped, rows.Err()
}

// gasPriceWei encodes one int64 amount for a uint256 column.
//
// types.GasPricePeriod's fields are signed, the column is not. A negative value means the
// monitor computed nonsense (its min starts at math.MaxInt64 and an empty period would
// leave it there), and it must be rejected with the field named rather than arriving as an
// anonymous domain violation on a nine-column insert.
func gasPriceWei(field string, v int64) (pgtype.Numeric, error) {
	n, err := Wei(big.NewInt(v))
	if err != nil {
		return pgtype.Numeric{}, fmt.Errorf("gas price %s: %w", field, err)
	}
	return n, nil
}

// gasPriceAmount decodes a uint256 column back into the int64 the domain type carries.
//
// The column is wider than the field, so the narrowing is checked. A value above MaxInt64
// would otherwise wrap to a negative price, which the resolver would publish as an
// enormous hexutil.Uint64.
func gasPriceAmount(field string, n pgtype.Numeric) (int64, error) {
	v, err := FromWei(n)
	if err != nil {
		return 0, fmt.Errorf("gas price %s: %w", field, err)
	}
	if v == nil {
		return 0, fmt.Errorf("gas price %s is NULL, but the column is NOT NULL", field)
	}
	if !v.IsInt64() {
		return 0, fmt.Errorf("gas price %s is %s, which does not fit the int64 the API exposes", field, v)
	}
	return v.Int64(), nil
}

// scanGasPricePeriod maps one row onto the domain type.
func scanGasPricePeriod(row rowScanner) (*types.GasPricePeriod, error) {
	var (
		tsFrom, tsTo                    pgtype.Timestamptz
		periodType                      int16
		open, closed, min, max, avg     pgtype.Numeric
		tickNS                          int64
		openV, closeV, minV, maxV, avgV int64
	)

	if err := row.Scan(&tsFrom, &tsTo, &periodType,
		&open, &closed, &min, &max, &avg, &tickNS); err != nil {
		return nil, err
	}

	// period_type is SMALLINT so the schema admits values the int8 field cannot hold.
	// Truncating would silently relabel one sampler's data as another's.
	if periodType < -128 || periodType > 127 {
		return nil, fmt.Errorf("gas price period type %d does not fit the int8 the API exposes", periodType)
	}

	var err error
	if openV, err = gasPriceAmount("open", open); err != nil {
		return nil, err
	}
	if closeV, err = gasPriceAmount("close", closed); err != nil {
		return nil, err
	}
	if minV, err = gasPriceAmount("min", min); err != nil {
		return nil, err
	}
	if maxV, err = gasPriceAmount("max", max); err != nil {
		return nil, err
	}
	if avgV, err = gasPriceAmount("avg", avg); err != nil {
		return nil, err
	}

	return &types.GasPricePeriod{
		Type:  int8(periodType),
		Open:  openV,
		Close: closeV,
		Min:   minV,
		Max:   maxV,
		Avg:   avgV,
		From:  tsFrom.Time,
		To:    tsTo.Time,
		Tick:  tickNS,
	}, nil
}
