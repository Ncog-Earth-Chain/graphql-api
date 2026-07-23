package pg

import (
	"context"
	"errors"
	"fmt"
	"math"
	"ncogearthchain-api-graphql/internal/types"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/jackc/pgx/v5/pgtype"
)

// Epoch reads and writes.
//
// Epochs are keyed, filtered, ordered and paginated on `id` alone. The MongoDB version
// range-filtered on `_id` but sorted on `end` -- two different orderings -- so whenever
// the end-time order disagreed with the id order (a clock-skewed seal, two epochs sealing
// in the same second) pages silently repeated and skipped rows. `id` is the primary key,
// monotonic by construction, so one index scan serves both the range and the order with
// no sort node.

// ErrNoEpochs reports an empty epoch table from LastKnownEpoch.
//
// It is an error rather than (0, nil) deliberately. The caller
// (internal/svc/scan_epochs.go) resumes scanning at LastKnownEpoch and treats any error
// as "start at epoch 1"; MongoDB returned mongo.ErrNoDocuments on an empty collection,
// which is what made a fresh database start at 1. Returning (0, nil) would make the
// scanner start at epoch 0, which does not exist, and repo.Epoch(0) would then fail on
// every tick forever. Callers that can distinguish should use errors.Is on this sentinel
// instead of treating it as a failure.
var ErrNoEpochs = errors.New("no epoch stored yet")

// epochKeyset is the total ordering for epoch lists: newest (highest id) first. The id is
// the primary key, so the ordering is total and pagination cannot repeat or skip a row.
var epochKeyset = Keyset{Columns: []KeyColumn{{Name: "id", Dir: Desc}}}

// epochColumns is the projection every epoch read shares.
//
// end_time is a TIMESTAMPTZ but the domain type carries unix seconds, so it is extracted
// rather than scanned as a timestamp. MongoDB stored the same instant twice -- `et` as an
// int64 and `end` as a date -- which could and did disagree; there is one column here.
const epochColumns = `
	id, EXTRACT(EPOCH FROM end_time)::BIGINT, fee, base_reward_weight,
	tx_reward_weight, reward, stake, total_supply`

// AddEpoch stores an epoch if it is not already known.
//
// Idempotent by ON CONFLICT rather than by a preceding existence check. The scanner
// replays epochs after a restart, so a re-add must be a silent no-op -- but the MongoDB
// read-then-insert was also a TOCTOU race: two scanner goroutines could both observe the
// epoch as absent and the loser's InsertOne then failed on the duplicate key. One
// statement removes both the race and the extra round trip.
//
// DO NOTHING, not DO UPDATE: an epoch is sealed on-chain and immutable, so a differing
// re-read is a bug upstream and must not overwrite what was recorded first.
func (s *Store) AddEpoch(ctx context.Context, e *types.Epoch) error {
	if e == nil {
		return fmt.Errorf("empty epoch received")
	}
	if e.EndTime == 0 {
		return fmt.Errorf("epoch #%d has no end time", uint64(e.Id))
	}

	// The MongoDB guard also rejected StakeTotalAmount <= 0 as an "empty epoch". That is
	// not reproduced: a zero-stake epoch is legal, and rejecting it created a PERMANENT
	// hole -- the scanner had already advanced its cursor before the store failed, and it
	// only logs the failure, so the epoch was never retried.

	fee, err := Wei(e.EpochFee.ToInt())
	if err != nil {
		return fmt.Errorf("epoch #%d fee: %w", uint64(e.Id), err)
	}
	baseWeight, err := Wei(e.TotalBaseRewardWeight.ToInt())
	if err != nil {
		return fmt.Errorf("epoch #%d base reward weight: %w", uint64(e.Id), err)
	}
	txWeight, err := Wei(e.TotalTxRewardWeight.ToInt())
	if err != nil {
		return fmt.Errorf("epoch #%d tx reward weight: %w", uint64(e.Id), err)
	}
	reward, err := Wei(e.BaseRewardPerSecond.ToInt())
	if err != nil {
		return fmt.Errorf("epoch #%d base reward per second: %w", uint64(e.Id), err)
	}
	stake, err := Wei(e.StakeTotalAmount.ToInt())
	if err != nil {
		return fmt.Errorf("epoch #%d stake: %w", uint64(e.Id), err)
	}
	supply, err := Wei(e.TotalSupply.ToInt())
	if err != nil {
		return fmt.Errorf("epoch #%d total supply: %w", uint64(e.Id), err)
	}

	// Column order below is NOT the struct order. `reward` holds BaseRewardPerSecond and
	// `fee` holds EpochFee; the BSON tags additionally disagreed with the JSON tags on
	// which of brw/trw was the base weight. Mapped by Go field, not by tag name.
	_, err = s.pool.Exec(ctx, `
		INSERT INTO epoch (id, end_time, fee, base_reward_weight,
		                   tx_reward_weight, reward, stake, total_supply)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO NOTHING`,
		int64(e.Id), time.Unix(int64(e.EndTime), 0).UTC(),
		fee, baseWeight, txWeight, reward, stake, supply)
	if err != nil {
		return fmt.Errorf("can not store epoch #%d: %w", uint64(e.Id), err)
	}
	return nil
}

// LastKnownEpoch returns the id of the highest stored epoch.
//
// MAX(id), not the id of the latest-ending epoch. MongoDB sorted on `end` and returned
// that document's `_id`; with a clock-skewed or otherwise out-of-order end time the
// scanner resumed from the wrong epoch and permanently skipped everything between.
//
// Returns ErrNoEpochs when the table is empty -- see the sentinel's own note.
func (s *Store) LastKnownEpoch(ctx context.Context) (uint64, error) {
	// MAX over the primary key is an index-only scan of a single leaf entry, so this is
	// not a table scan even though it has no WHERE clause.
	var id *int64
	if err := s.pool.QueryRow(ctx, `SELECT MAX(id) FROM epoch`).Scan(&id); err != nil {
		return 0, fmt.Errorf("can not read the last known epoch: %w", err)
	}
	if id == nil {
		return 0, ErrNoEpochs
	}
	if *id < 0 {
		return 0, fmt.Errorf("stored epoch id %d is negative", *id)
	}
	return uint64(*id), nil
}

// EpochsCount returns the exact number of stored epochs.
//
// Exact, unlike the reltuples estimate used for blocks and transactions. MongoDB used
// EstimatedDocumentCount here, but the epoch table holds one row per epoch -- thousands,
// not hundreds of millions -- so count(*) is an index-only scan that costs less than the
// estimate is worth.
func (s *Store) EpochsCount(ctx context.Context) (uint64, error) {
	var n int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM epoch`).Scan(&n); err != nil {
		return 0, fmt.Errorf("can not count epochs: %w", err)
	}
	return uint64(n), nil
}

// Epochs lists epochs from the given cursor.
//
// count carries both the page size and the direction: positive walks toward older epochs
// with the newest first, negative walks toward newer epochs. Its magnitude is clamped --
// the caller must not decide how much work the database does.
//
// The cursor is the epoch id as hex, matching what the GraphQL resolver emits
// (Epoch.Id.String()). It is deliberately NOT the opaque token of keyset.go: adopting
// that here would require editing PageInfo in resolvers/sfc_epochs.go in the same change,
// and a hex cursor fed to DecodeCursor fails as malformed. Whoever makes the cursor
// opaque must change both.
func (s *Store) Epochs(ctx context.Context, cursor *string, count int32) (*types.EpochList, error) {
	if count == 0 {
		return nil, fmt.Errorf("nothing to do, zero epochs requested")
	}

	total, err := s.EpochsCount(ctx)
	if err != nil {
		return nil, err
	}

	// Collection is a non-nil empty slice, never nil: the resolver's PageInfo branches on
	// len(Collection) == 0 and Edges() on the same, so a nil slice would be indistinguishable
	// here but is one dereference away from mattering.
	list := &types.EpochList{
		Collection: make([]*types.Epoch, 0),
		Total:      total,
		IsStart:    total == 0,
		IsEnd:      total == 0,
	}
	if total == 0 {
		return list, nil
	}

	page := NewPage(count, maxListLimit)

	var where string
	var args []any

	if cursor != nil {
		id, err := hexutil.DecodeUint64(*cursor)
		if err != nil {
			return nil, fmt.Errorf("invalid epoch cursor %q: %w", *cursor, err)
		}
		if id > math.MaxInt64 {
			return nil, fmt.Errorf("epoch cursor %q is out of range", *cursor)
		}

		// Strictly after the cursor, so the row the cursor names is not repeated on the
		// next page. Predicate and ORDER BY are generated from the same Keyset, which is
		// the only way they cannot drift apart.
		pred, curArgs, err := epochKeyset.After([]any{int64(id)}, page.Reverse, 0)
		if err != nil {
			return nil, err
		}
		where = "WHERE " + pred
		args = append(args, curArgs...)
	}
	// With no cursor there is no predicate at all. MongoDB looked up the boundary id and
	// filtered id <= MAX(id) / id >= MIN(id), which is a tautology over the whole table
	// and cost an extra round trip to construct.

	// One row beyond the page, used only to decide whether a further page exists.
	args = append(args, page.Limit+1)

	sql := `SELECT ` + epochColumns + ` FROM epoch ` + where + ` ` +
		epochKeyset.OrderBy(page.Reverse) + ` LIMIT $` + itoa(len(args))

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("epoch query failed: %w", err)
	}
	defer rows.Close()

	fetched := make([]*types.Epoch, 0, page.Limit+1)
	for rows.Next() {
		e, err := scanEpoch(rows)
		if err != nil {
			return nil, fmt.Errorf("can not scan epoch: %w", err)
		}
		fetched = append(fetched, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("epoch query failed: %w", err)
	}

	hasMore := len(fetched) > page.Limit

	// Boundary marks, kept as MongoDB computed them: a request with no cursor is by
	// definition sitting on the end it started from, and a short page means the other end
	// was reached.
	list.IsEnd = (cursor == nil && page.Reverse) || (!page.Reverse && !hasMore)
	list.IsStart = (cursor == nil && !page.Reverse) || (page.Reverse && !hasMore)

	// Trim the probe row BEFORE reversing. MongoDB reversed first and then dropped the
	// last element, which on a backward page discarded the OLDEST epoch rather than the
	// probe -- so `epochs(count: -n)` with no cursor could never return epoch 1 at all,
	// while still reporting IsEnd.
	if hasMore {
		fetched = fetched[:page.Limit]
	}
	list.Collection = fetched

	// A backward page is read ascending by the database; flip it so the newest is first,
	// which is the order the API presents everywhere.
	if page.Reverse {
		list.Reverse()
	}

	// First and Last describe the page actually returned. MongoDB set First to the cursor
	// or boundary id -- seeded, wrongly, from an end-time sort while being used as an id
	// bound -- and never assigned Last at all, so it read 0 on every list ever produced.
	if n := len(list.Collection); n > 0 {
		list.First = uint64(list.Collection[0].Id)
		list.Last = uint64(list.Collection[n-1].Id)
	}
	return list, nil
}

// scanEpoch maps one row onto the domain type.
func scanEpoch(row rowScanner) (*types.Epoch, error) {
	var (
		id, endTime               int64
		fee, baseWeight, txWeight pgtype.Numeric
		reward, stake, supply     pgtype.Numeric
		e                         types.Epoch
		convErr                   error
	)

	if err := row.Scan(&id, &endTime, &fee, &baseWeight, &txWeight,
		&reward, &stake, &supply); err != nil {
		return nil, err
	}

	e.Id = hexutil.Uint64(id)
	e.EndTime = hexutil.Uint64(endTime)

	// Errors returned, never a panic. The MongoDB decoder used hexutil.MustDecodeBig and
	// caught the panic with a recover() in UnmarshalBSON, which turned any malformed
	// stored amount into a generic "can not decode stored epoch" with no indication of
	// which field was bad.
	if e.EpochFee, convErr = epochWei(fee, id, "fee"); convErr != nil {
		return nil, convErr
	}
	if e.TotalBaseRewardWeight, convErr = epochWei(baseWeight, id, "base_reward_weight"); convErr != nil {
		return nil, convErr
	}
	if e.TotalTxRewardWeight, convErr = epochWei(txWeight, id, "tx_reward_weight"); convErr != nil {
		return nil, convErr
	}
	if e.BaseRewardPerSecond, convErr = epochWei(reward, id, "reward"); convErr != nil {
		return nil, convErr
	}
	if e.StakeTotalAmount, convErr = epochWei(stake, id, "stake"); convErr != nil {
		return nil, convErr
	}
	if e.TotalSupply, convErr = epochWei(supply, id, "total_supply"); convErr != nil {
		return nil, convErr
	}
	return &e, nil
}

// epochWei converts one money column into the domain's hexutil.Big.
//
// types.Epoch holds hexutil.Big by value and so has no way to express "not recorded";
// every epoch money column is NOT NULL, which is what makes that safe. A NULL therefore
// means the schema no longer matches this code, and it is reported rather than quietly
// rendered as zero -- zero is a claim about the chain, absent is not.
func epochWei(n pgtype.Numeric, id int64, column string) (hexutil.Big, error) {
	v, err := FromWei(n)
	if err != nil {
		return hexutil.Big{}, fmt.Errorf("epoch #%d column %s: %w", id, column, err)
	}
	if v == nil {
		return hexutil.Big{}, fmt.Errorf("epoch #%d column %s is NULL but the column is NOT NULL", id, column)
	}
	return hexutil.Big(*v), nil
}

// EpochCursor renders the pagination cursor for an epoch.
//
// Hex, matching the resolver and the cursor Epochs accepts. See the note there.
func EpochCursor(e *types.Epoch) string {
	if e == nil {
		return ""
	}
	return e.Id.String()
}
