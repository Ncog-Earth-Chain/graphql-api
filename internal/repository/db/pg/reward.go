package pg

import (
	"context"
	"fmt"
	"math/big"
	"ncogearthchain-api-graphql/internal/types"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/jackc/pgx/v5/pgtype"
)

// SFC reward claims.
//
// Three MongoDB defects are fixed rather than carried over, and each one changes what the
// explorer reports, so they are named here.
//
//  1. Identity. The Mongo document was keyed on the claim TRANSACTION hash alone
//     (RewardClaim.Pk()), and the writer probed for that key before inserting. One
//     transaction can emit two reward logs -- two SFC handlers fire for the same
//     transaction, and any batching contract claiming from two validators does the same --
//     so the second claim was silently discarded and the delegator's reward total was
//     permanently understated. The row is keyed on (block_number, log_index) here, which
//     is the position of the log that produced it.
//
//  2. Amount fidelity. The aggregatable column in Mongo was `value`: Amount / 1e9,
//     truncated into a uint64. Every claim lost its sub-gwei remainder, any single claim
//     above ~1.8e28 wei wrapped, and the sum was multiplied back by 1e9 before being
//     presented -- a fabricated round number offered as an exact total. Here `amount` is
//     the exact wei value and SUM() is over that, so types.RewardDecimalsCorrection has no
//     counterpart in this file.
//
//  3. Ordering. Mongo sorted on a synthetic ordinal, (timestamp & 0x7FFFFFFFFF) << 24 |
//     (3 bytes of the transaction hash). Claims within the same second ordered by hash
//     bytes, i.e. arbitrarily, and the 3-byte tail is not unique -- so the ordering was not
//     total and pagination could repeat or skip rows at a page boundary. Ordering is on
//     (block_number, log_index), which is unique by construction.

// rewardKeyset is the total ordering for reward claim lists: newest first. The pair is the
// primary key, so it is unique and pagination cannot repeat or skip a row.
var rewardKeyset = Keyset{Columns: []KeyColumn{
	{Name: "block_number", Dir: Desc},
	{Name: "log_index", Dir: Desc},
}}

// rewardColumns is the projection every reward claim read shares, so a column added in one
// place cannot be forgotten in another.
const rewardColumns = `
	block_number, log_index, claim_tx, tx_index, delegator,
	validator_id, claimed_at, amount, is_restake`

// AddRewardClaim stores a reward claim.
//
// Idempotent: re-ingesting the same log is a no-op rather than an error, because a rescan
// after a restart or a reorg replays logs that are already stored.
//
// This replaces a read-then-write pair whose read returned "not known" on a genuine driver
// error, into a collection carrying no unique index -- so a transient failure produced a
// duplicate document that then double-counted in every reward total. ON CONFLICT makes
// that outcome unrepresentable regardless of what the caller does.
func (s *Store) AddRewardClaim(ctx context.Context, rc *types.RewardClaim) error {
	return s.writeRewardClaim(ctx, s.pool, rc)
}

// writeRewardClaim stores a reward claim through any querier.
//
// Taking a Querier rather than reaching for the pool is what lets this INSERT be attached
// to the per-block ingest transaction, so a block that fails to land leaves no orphan
// claim behind. The Mongo write was a separate round trip with no such option.
func (s *Store) writeRewardClaim(ctx context.Context, q Querier, rc *types.RewardClaim) error {
	if rc == nil {
		return fmt.Errorf("can not store an empty reward claim")
	}

	// validator_id is a uint256 column, so it goes through the same gate as money: it
	// arrives from an event topic and is attacker-influenced in principle.
	validator, err := Wei(rc.ToValidatorId.ToInt())
	if err != nil {
		return fmt.Errorf("reward claim %s validator id: %w", rc.ClaimTrx.String(), err)
	}
	if !validator.Valid {
		return fmt.Errorf("reward claim %s has no validator id", rc.ClaimTrx.String())
	}

	amount, err := Wei(rc.Amount.ToInt())
	if err != nil {
		return fmt.Errorf("reward claim %s amount: %w", rc.ClaimTrx.String(), err)
	}
	if !amount.Valid {
		return fmt.Errorf("reward claim %s has no amount", rc.ClaimTrx.String())
	}

	_, err = q.Exec(ctx, `
		INSERT INTO reward_claim (block_number, log_index, claim_tx, tx_index,
		                          delegator, validator_id, claimed_at, amount, is_restake)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (block_number, log_index) DO NOTHING`,
		int64(rc.BlockNumber),
		int32(rc.LogIndex),
		HashVal(rc.ClaimTrx),
		int32(rc.TxIndex),
		AddrVal(rc.Delegator),
		validator,
		time.Unix(int64(rc.Claimed), 0).UTC(),
		amount,
		rc.IsDelegated,
	)
	if err != nil {
		return fmt.Errorf("can not store reward claim %s: %w", rc.ClaimTrx.String(), err)
	}
	return nil
}

// RewardClaims lists reward claims for a delegator and/or a validator, newest first.
//
// The filter is two typed arguments rather than a query document. Both are optional and
// the common call passes both (the delegation page); each is emitted as a bound predicate
// only when present.
//
// count carries the direction in its sign, as before: positive pages forward from the
// cursor toward older claims, negative pages backward toward newer ones. Its magnitude is
// clamped -- nothing bounded it previously, so a client chose how much work the database
// did.
func (s *Store) RewardClaims(ctx context.Context, adr *common.Address, valID *big.Int, cursor *string, count int32) (*types.RewardClaimsList, error) {
	// preserved verbatim: a zero count is a caller bug, not an empty page
	if count == 0 {
		return nil, fmt.Errorf("nothing to do, zero reward claims requested")
	}

	filter, err := rewardFilter(adr, valID)
	if err != nil {
		return nil, err
	}

	total, err := s.rewardClaimsCount(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Collection is a non-nil empty slice even when there is nothing to return; the
	// resolver ranges over it directly.
	list := &types.RewardClaimsList{
		Collection: make([]*types.RewardClaim, 0),
		Total:      total,
		IsStart:    total == 0,
		IsEnd:      total == 0,
	}
	if total == 0 {
		return list, nil
	}

	page := NewPage(count, maxListLimit)

	where, args := filter.Render(0)

	// A cursor is a position in THIS ordering, and the filter is re-applied around it.
	// The Mongo version looked the cursor up by document id alone, so a cursor belonging
	// to another delegator paginated from that foreign position without complaint.
	cur, err := DecodeCursor(cursorValue(cursor), 2)
	if err != nil {
		return nil, err
	}
	if len(cur) == 2 {
		pred, curArgs, err := rewardKeyset.After([]any{cur[0], int32(cur[1])}, page.Reverse, len(args))
		if err != nil {
			return nil, err
		}
		if where != "" {
			where += " AND "
		}
		where += pred
		args = append(args, curArgs...)
	}
	if where != "" {
		where = "WHERE " + where
	}

	// One row beyond the page is read to learn whether another page exists. It is trimmed
	// before returning, so the extra row is never visible to the caller.
	sql := `SELECT ` + rewardColumns + ` FROM reward_claim ` + where + ` ` +
		rewardKeyset.OrderBy(page.Reverse) + ` LIMIT $` + itoa(len(args)+1)
	args = append(args, page.Limit+1)

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("reward claim query failed: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		rc, err := scanRewardClaim(rows)
		if err != nil {
			return nil, fmt.Errorf("can not scan reward claim: %w", err)
		}
		list.Collection = append(list.Collection, rc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reward claim query failed: %w", err)
	}

	hasMore := len(list.Collection) > page.Limit
	if hasMore {
		list.Collection = list.Collection[:page.Limit]
	}

	// Boundary marks. Paging forward, the first page starts at the newest claim, and the
	// oldest is reached when no probe row came back; paging backward the two swap. This is
	// the same rule the Mongo loader applied, expressed against the probe row instead of
	// against a count comparison that the clamp above would have made wrong.
	atCursorStart := len(cur) == 0
	if page.Reverse {
		list.IsEnd = atCursorStart
		list.IsStart = !hasMore
		// backward pages arrive oldest-first; flip so the newest claim is at the top
		list.Reverse()
	} else {
		list.IsStart = atCursorStart
		list.IsEnd = !hasMore
	}

	// First and Last are left at zero. They held the synthetic Mongo ordinal, which no
	// longer exists -- and Last was never assigned there either, so nothing can have
	// depended on it. Page boundaries are expressed as opaque cursors; see
	// RewardClaimCursor.
	return list, nil
}

// RewardsClaimed sums claimed rewards, optionally bounded by delegator, validator and
// time.
//
// Named after the repository method it serves; the Mongo layer called it RewardsSumValue.
//
// Both time bounds are honoured. The Mongo caller appended its timestamp condition to the
// same document twice, and only the last survived -- so asking for a window silently
// returned everything up to `until`, with no lower bound. Two separate SQL predicates
// cannot collapse that way.
//
// Bounds are inclusive at both ends, matching the $gte/$lte they replace.
func (s *Store) RewardsClaimed(ctx context.Context, adr *common.Address, valID *big.Int, since *int64, until *int64) (*big.Int, error) {
	filter, err := rewardFilter(adr, valID)
	if err != nil {
		return nil, err
	}
	if since != nil {
		filter.Gte("claimed_at", time.Unix(*since, 0).UTC())
	}
	if until != nil {
		filter.Lte("claimed_at", time.Unix(*until, 0).UTC())
	}

	where, args := filter.Where(0)

	// COALESCE is load-bearing: SUM over no rows is NULL, and the callers dereference the
	// result. "No claims" must read as zero, never as nil and never as an error.
	var sum pgtype.Numeric
	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount), 0) FROM reward_claim `+where, args...).Scan(&sum); err != nil {
		return nil, fmt.Errorf("can not sum claimed rewards: %w", err)
	}

	value, err := FromWei(sum)
	if err != nil {
		return nil, fmt.Errorf("can not read claimed rewards total: %w", err)
	}
	if value == nil {
		return new(big.Int), nil
	}
	return value, nil
}

// RewardClaimCursor renders the pagination cursor for a reward claim.
//
// Opaque, so the ordering key stays an implementation detail. The old cursor was the claim
// transaction hash, which is not unique per claim and is therefore not a position at all --
// two claims in one transaction shared it.
func RewardClaimCursor(rc *types.RewardClaim) string {
	if rc == nil {
		return ""
	}
	return EncodeCursor([]int64{int64(rc.BlockNumber), int64(rc.LogIndex)})
}

// rewardClaimsCount counts the claims matching a filter.
//
// Exact rather than estimated, and recomputed per page as it was before. Both supported
// filter shapes are index-served, so this is an index-only scan rather than a table walk.
func (s *Store) rewardClaimsCount(ctx context.Context, filter *Filter) (uint64, error) {
	where, args := filter.Where(0)

	var n int64
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM reward_claim `+where, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("can not count reward claims: %w", err)
	}
	return uint64(n), nil
}

// rewardFilter builds the shared delegator/validator predicate.
//
// A nil argument means "unfiltered on this column", so it emits no predicate -- as opposed
// to matching the zero address or validator zero, which are different questions.
func rewardFilter(adr *common.Address, valID *big.Int) (*Filter, error) {
	f := NewFilter()
	if adr != nil {
		f.Eq("delegator", AddrVal(*adr))
	}
	if valID != nil {
		v, err := Wei(valID)
		if err != nil {
			return nil, fmt.Errorf("validator id filter: %w", err)
		}
		f.Eq("validator_id", v)
	}
	return f, nil
}

// cursorValue reads an optional cursor. A nil pointer and an empty string both mean the
// first page.
func cursorValue(c *string) string {
	if c == nil {
		return ""
	}
	return *c
}

// scanRewardClaim maps one row onto the domain type.
func scanRewardClaim(row rowScanner) (*types.RewardClaim, error) {
	var (
		blockNumber         int64
		logIndex, txIndex   int32
		claimTx, delegator  []byte
		validatorID, amount pgtype.Numeric
		claimedAt           pgtype.Timestamptz
		isRestake           bool
	)

	if err := row.Scan(&blockNumber, &logIndex, &claimTx, &txIndex, &delegator,
		&validatorID, &claimedAt, &amount, &isRestake); err != nil {
		return nil, err
	}

	tx, err := ToHash(claimTx)
	if err != nil {
		return nil, err
	}
	adr, err := ToAddr(delegator)
	if err != nil {
		return nil, err
	}

	validator, err := FromWei(validatorID)
	if err != nil {
		return nil, err
	}
	value, err := FromWei(amount)
	if err != nil {
		return nil, err
	}

	// Both columns are NOT NULL, so a nil here means the row arrived by some route that
	// bypassed the schema. Reporting a claim of zero would be a false statement about the
	// chain, so it is an error instead.
	if validator == nil || value == nil {
		return nil, fmt.Errorf("reward claim at block %d log %d has a NULL uint256 column",
			blockNumber, logIndex)
	}

	return &types.RewardClaim{
		Delegator:     *adr,
		ToValidatorId: (hexutil.Big)(*validator),
		Claimed:       hexutil.Uint64(claimedAt.Time.Unix()),
		ClaimTrx:      *tx,
		Amount:        (hexutil.Big)(*value),
		IsDelegated:   isRestake,
		BlockNumber:   uint64(blockNumber),
		LogIndex:      uint(logIndex),
		TxIndex:       uint(txIndex),
	}, nil
}
