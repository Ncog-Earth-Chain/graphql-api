package pg

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"ncogearthchain-api-graphql/internal/types"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Withdrawal request reads and writes.
//
// Three MongoDB behaviours are deliberately not carried over, because each of them is a
// defect that costs money rather than a convention:
//
//  1. (delegator, validator, request id) was treated as unique, and a repeat was handled
//     by REWRITING the older settled row's request id to a value derived from its
//     transaction hash. The PostgreSQL primary key carries request_tx, so repeats coexist
//     natively and no history has to be falsified to make room.
//
//  2. The amount was stored twice -- once exact as a string, once as amount/1e9 truncated
//     into a uint64 -- and only the lossy copy could be aggregated. Every pending-total
//     lost up to 1 Gwei per request and wrapped silently past 2^64 wei (about 18.4 NEC).
//     Here `amount` is uint256 and is summed directly.
//
//  3. The withdrawal transaction HASH was written into the penalty field and read back as
//     a big integer, so the API presented a hash-derived 256-bit number to users as a
//     slashing penalty. penalty is written only from the real penalty, or NULL.

// ErrWithdrawalNotFound reports a withdraw request that is not in the database.
//
// This is the one single-item lookup in this package that does NOT return (nil, nil) on a
// miss. Its only caller -- the SFC "Withdrawn" log handler -- branches on err != nil and
// then dereferences the result unconditionally, so a (nil, nil) miss would panic the log
// processing goroutine. A withdraw request being finalized must exist; its absence is a
// genuine fault, not an ordinary lookup outcome.
var ErrWithdrawalNotFound = errors.New("withdraw request not found in database")

// wrKeyset is the total ordering for withdrawal lists: newest first.
//
// request_id is the uniqueness tie-break. (block_number, tx_index) alone is NOT unique
// here -- a single transaction can open several withdraw requests -- and keyset pagination
// over a non-unique key repeats and skips rows at page boundaries. It replaces the packed
// `orx` ordinal, whose 12-bit transaction discriminator collided by birthday at roughly 77
// requests in one second.
var wrKeyset = Keyset{Columns: []KeyColumn{
	{Name: "block_number", Dir: Desc},
	{Name: "tx_index", Dir: Desc},
	{Name: "request_id", Dir: Desc},
}}

// wrColumns is the projection every withdrawal read shares.
const wrColumns = `
	delegator, validator_id, request_id, request_tx, block_number, tx_index,
	created_at, amount, penalty, finalized_tx, finalized_at, req_type`

// Withdrawal returns one withdraw request identified by delegator, validator and request
// ID.
//
// The ORDER BY is load-bearing and is a change from MongoDB. Mongo's key made this triple
// unique, so it returned "the" row; under the PostgreSQL key the triple can match several
// rows, one per request transaction. This method exists to find the request that is about
// to be finalized, so the open row -- there is at most one, enforced by withdrawal_open_uq
// -- must win over settled history.
func (s *Store) Withdrawal(ctx context.Context, addr *common.Address, valID *hexutil.Big, reqID *hexutil.Big) (*types.WithdrawRequest, error) {
	if addr == nil || valID == nil || reqID == nil {
		return nil, fmt.Errorf("withdraw request lookup needs a delegator, a validator and a request id")
	}

	val, err := Wei(valID.ToInt())
	if err != nil {
		return nil, fmt.Errorf("validator id: %w", err)
	}
	req, err := Wei(reqID.ToInt())
	if err != nil {
		return nil, fmt.Errorf("withdraw request id: %w", err)
	}

	row := s.pool.QueryRow(ctx, `
		SELECT `+wrColumns+`
		FROM   withdrawal
		WHERE  delegator = $1 AND validator_id = $2 AND request_id = $3
		ORDER  BY (finalized_tx IS NULL) DESC, block_number DESC, tx_index DESC
		LIMIT  1`, AddrVal(*addr), val, req)

	wr, err := scanWithdrawal(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s of %s to #%s",
			ErrWithdrawalNotFound, reqID.String(), addr.String(), valID.ToInt().String())
	}
	if err != nil {
		return nil, fmt.Errorf("can not load withdraw request %s of %s: %w", reqID.String(), addr.String(), err)
	}
	return wr, nil
}

// AddWithdrawal stores a newly observed withdraw request.
//
// One idempotent statement replaces MongoDB's read-probe / shift / insert-or-update dance.
// The probe was a TOCTOU race against concurrent block workers -- two workers could both
// observe "not known" and both insert -- and the shift falsified settled rows. ON CONFLICT
// is atomic and needs neither.
//
// penalty, finalized_tx and finalized_at are absent from both the insert and the update
// list on purpose: re-ingesting a request log during a re-scan or a reorg must never
// un-finalize a settled withdrawal.
func (s *Store) AddWithdrawal(ctx context.Context, wr *types.WithdrawRequest) error {
	if wr == nil {
		return fmt.Errorf("can not store an empty withdraw request")
	}
	if wr.StakerID == nil || wr.WithdrawRequestID == nil || wr.Amount == nil {
		return fmt.Errorf("withdraw request %s is missing validator, request id or amount", wr.RequestTrx.String())
	}
	if wr.BlockNumber == nil || wr.TxIndex == nil {
		// The columns are NOT NULL and the position is the ordering key for every
		// withdrawal list, so a request without one cannot be stored at all. Failing here
		// names the missing input; letting it through would surface as a constraint
		// violation attributed to the statement rather than to the caller.
		return fmt.Errorf("withdraw request %s has no block position; it can not be ordered or paginated", wr.RequestTrx.String())
	}

	val, err := Wei(wr.StakerID.ToInt())
	if err != nil {
		return fmt.Errorf("withdraw request %s validator id: %w", wr.RequestTrx.String(), err)
	}
	req, err := Wei(wr.WithdrawRequestID.ToInt())
	if err != nil {
		return fmt.Errorf("withdraw request %s request id: %w", wr.RequestTrx.String(), err)
	}
	amount, err := Wei(wr.Amount.ToInt())
	if err != nil {
		return fmt.Errorf("withdraw request %s amount: %w", wr.RequestTrx.String(), err)
	}

	// A conflict on withdrawal_open_uq rather than on the primary key means a SECOND open
	// request for the same (delegator, validator, request id) arrived while the first is
	// unsettled. That is the invariant the MongoDB code hand-maintained; here it surfaces
	// as an error instead of silently overwriting a live request.
	_, err = s.pool.Exec(ctx, `
		INSERT INTO withdrawal (delegator, validator_id, request_id, request_tx,
		                        block_number, tx_index, created_at, amount, req_type)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (delegator, validator_id, request_id, request_tx) DO UPDATE SET
		    block_number = EXCLUDED.block_number,
		    tx_index     = EXCLUDED.tx_index,
		    created_at   = EXCLUDED.created_at,
		    amount       = EXCLUDED.amount,
		    -- Same hazard as delegation.validator_addr: req_type is NOT NULL, so a
		    -- re-delivery carrying an empty type would write '' over a known value.
		    -- UpdateWithdrawal already guards this; the insert path must agree.
		    req_type     = COALESCE(NULLIF(EXCLUDED.req_type, ''), withdrawal.req_type)`,
		AddrVal(wr.Address),
		val,
		req,
		HashVal(wr.RequestTrx),
		int64(*wr.BlockNumber),
		int32(*wr.TxIndex),
		time.Unix(int64(wr.CreatedTime), 0).UTC(),
		amount,
		wr.Type,
	)
	if err != nil {
		return fmt.Errorf("can not store withdraw request %s of %s to #%s: %w",
			wr.RequestTrx.String(), wr.Address.String(), wr.StakerID.ToInt().String(), err)
	}
	return nil
}

// UpdateWithdrawal records the finalization of a withdraw request.
//
// A plain UPDATE, never an upsert. MongoDB used SetUpsert(true) and then reported failure
// when MatchedCount was 0 -- which is exactly what an upsert-insert reports while HAVING
// written a row. Worse, the upserted document was built from the $set list alone, which
// omits the amount, so the next read of that row ran MustDecodeBig("") and panicked into a
// swallowed "can not decode BIG number". Finalizing a request that does not exist is an
// error, and nothing is written.
//
// The `finalized_tx IS NULL` predicate targets the open row through withdrawal_open_uq, so
// a repeat of the same request id cannot re-finalize settled history.
func (s *Store) UpdateWithdrawal(ctx context.Context, wr *types.WithdrawRequest) error {
	if wr == nil {
		return fmt.Errorf("can not update an empty withdraw request")
	}
	if wr.StakerID == nil || wr.WithdrawRequestID == nil {
		return fmt.Errorf("withdraw request %s is missing validator or request id", wr.RequestTrx.String())
	}

	val, err := Wei(wr.StakerID.ToInt())
	if err != nil {
		return fmt.Errorf("withdraw request validator id: %w", err)
	}
	req, err := Wei(wr.WithdrawRequestID.ToInt())
	if err != nil {
		return fmt.Errorf("withdraw request id: %w", err)
	}

	// nil penalty stays SQL NULL. NULL and zero are different claims: "no penalty was
	// recorded" against "the penalty was zero", and the pending-total query reads the
	// finalization columns to decide what is still outstanding.
	var penalty pgtype.Numeric
	if wr.Penalty != nil {
		penalty, err = Wei(wr.Penalty.ToInt())
		if err != nil {
			return fmt.Errorf("withdraw request penalty: %w", err)
		}
	}

	var finalizedAt any
	if wr.WithdrawTime != nil {
		finalizedAt = time.Unix(int64(*wr.WithdrawTime), 0).UTC()
	}

	tag, err := s.pool.Exec(ctx, `
		UPDATE withdrawal SET
		    finalized_tx = $4,
		    finalized_at = $5,
		    penalty      = $6,
		    -- NULLIF guards against a caller that built the struct without a type:
		    -- blanking req_type would lose which SFC event opened the request.
		    req_type     = COALESCE(NULLIF($7, ''), req_type)
		WHERE  delegator = $1 AND validator_id = $2 AND request_id = $3
		  AND  finalized_tx IS NULL`,
		AddrVal(wr.Address), val, req,
		Hash(wr.WithdrawTrx), finalizedAt, penalty, wr.Type)
	if err != nil {
		return fmt.Errorf("can not update withdraw request %s of %s: %w",
			wr.WithdrawRequestID.String(), wr.Address.String(), err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("can not update, the withdraw request not found in database")
	}
	return nil
}

// WithdrawalCountFiltered returns the exact number of withdraw requests matching a filter.
//
// Exact, unlike WithdrawalsCount: a delegator-scoped filter is served by
// withdrawal_delegator_idx, so the count touches only that delegator's rows.
func (s *Store) WithdrawalCountFiltered(ctx context.Context, f *Filter) (uint64, error) {
	where, args := f.Where(0)

	var n int64
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM withdrawal `+where, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("can not count withdraw requests: %w", err)
	}
	return uint64(n), nil
}

// WithdrawalsCount returns an approximate total number of withdraw requests.
//
// Approximate by design, matching the MongoDB behaviour it replaces (EstimateCount, which
// read collection metadata). It feeds a headline figure, and an exact count(*) here would
// be a sequential scan of the largest staking table on every request.
func (s *Store) WithdrawalsCount(ctx context.Context) (uint64, error) {
	var n int64
	// reltuples is -1 before the first ANALYZE on PostgreSQL 14+, hence the clamp.
	if err := s.pool.QueryRow(ctx, `
		SELECT GREATEST(reltuples, 0)::BIGINT FROM pg_class WHERE oid = 'withdrawal'::regclass`).
		Scan(&n); err != nil {
		return 0, fmt.Errorf("can not estimate withdraw request count: %w", err)
	}
	return uint64(n), nil
}

// Withdrawals lists withdraw requests from a cursor position.
//
// Two of MongoDB's three round trips are gone. It ran an exact count, then a separate
// FindOne to resolve a boundary ordinal, then the page query; the keyset predicate carries
// the boundary itself, so only the count and the page remain.
//
// The filter is rendered per call and never mutated. The MongoDB version appended its
// ordinal bound onto the CALLER's filter document, so a filter reused across calls
// accumulated contradictory range clauses and progressively returned nothing.
func (s *Store) Withdrawals(ctx context.Context, cursor string, count int32, f *Filter) (*types.WithdrawRequestList, error) {
	if count == 0 {
		return nil, fmt.Errorf("nothing to do, zero withdrawals requested")
	}

	total, err := s.WithdrawalCountFiltered(ctx, f)
	if err != nil {
		return nil, err
	}

	list := types.WithdrawRequestList{
		Collection: make([]*types.WithdrawRequest, 0),
		Total:      total,
		IsStart:    total == 0,
		IsEnd:      total == 0,
	}
	if total == 0 {
		return &list, nil
	}

	page := NewPage(count, maxListLimit)

	where, args := f.Render(0)

	pos, err := decodeWithdrawalCursor(cursor)
	if err != nil {
		return nil, err
	}
	if pos != nil {
		reqID, err := Wei(pos.requestID)
		if err != nil {
			return nil, fmt.Errorf("cursor request id: %w", err)
		}
		pred, curArgs, err := wrKeyset.After(
			[]any{pos.blockNumber, pos.txIndex, reqID}, page.Reverse, len(args))
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

	// One row beyond the page, used only to learn whether a further page exists and then
	// discarded -- the same trick the MongoDB code used, kept because it answers IsEnd
	// without a second query.
	sql := `SELECT ` + wrColumns + ` FROM withdrawal ` + where + ` ` +
		wrKeyset.OrderBy(page.Reverse) + ` LIMIT $` + itoa(len(args)+1)
	args = append(args, page.Limit+1)

	rows, err := s.queryWithdrawals(ctx, sql, args...)
	if err != nil {
		return nil, err
	}

	more := len(rows) > page.Limit
	if more {
		rows = rows[:page.Limit]
	}

	// Backwards paging walks oldest-first through the index, so the page arrives in
	// reverse display order; flip it so the newest request stays on top either way.
	if page.Reverse {
		for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
			rows[i], rows[j] = rows[j], rows[i]
		}
	}

	list.Collection = rows

	// Boundary marks, preserving the MongoDB semantics exactly: paging forward from no
	// cursor means we are at the top of the list, paging backward from no cursor means we
	// are at the bottom, and running out of rows means we reached the far end.
	if page.Reverse {
		list.IsEnd = cursor == ""
		list.IsStart = !more
	} else {
		list.IsStart = cursor == ""
		list.IsEnd = !more
	}
	return &list, nil
}

// WithdrawalsSumValue totals the amounts of the withdraw requests matching a filter.
//
// Sums the exact uint256 `amount`. MongoDB summed the lossy amount/1e9 copy and multiplied
// the total back by 1e9, so every row contributed a truncation error and the running total
// could wrap.
//
// No matching rows is zero, not an error and not nil -- the caller renders it as an
// outstanding balance.
func (s *Store) WithdrawalsSumValue(ctx context.Context, f *Filter) (*big.Int, error) {
	where, args := f.Where(0)

	var sum pgtype.Numeric
	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(sum(amount), 0) FROM withdrawal `+where, args...).Scan(&sum); err != nil {
		return nil, fmt.Errorf("can not sum withdraw request amounts: %w", err)
	}

	v, err := FromWei(sum)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return new(big.Int), nil
	}
	return v, nil
}

// WithdrawalsOf builds the list filter for a delegator, optionally narrowed to one
// validator. A nil validator means "to any validator".
func WithdrawalsOf(addr *common.Address, valID *hexutil.Big) (*Filter, error) {
	f := NewFilter()
	if addr != nil {
		f.Eq("delegator", AddrVal(*addr))
	}
	if valID != nil {
		// Through Wei(), not a decimal string spliced into the SQL. validator_id is a
		// uint256 column, and Wei is the single place that rejects a negative or
		// out-of-range value with the offending number named. Rendering it as text here
		// would bypass that gate -- bound and un-injectable, but unchecked, and the
		// whole point of having one codec is that there is no second path.
		v, err := Wei(valID.ToInt())
		if err != nil {
			return nil, fmt.Errorf("validator id: %w", err)
		}
		f.Eq("validator_id", v)
	}
	return f, nil
}

// PendingWithdrawalsOf builds the filter for a delegator's OPEN withdraw requests.
//
// "Pending" is `finalized_tx IS NULL`. MongoDB spelled it {fin_trx: {$type: 10}} -- BSON
// type code 10 -- which additionally failed to match documents where the field was absent
// rather than null, so requests written by the $set-only upsert path were left out of
// pending totals entirely. IS NULL covers both.
//
// Served index-only by withdrawal_pending_idx, which is partial on exactly this predicate
// and INCLUDEs amount.
func PendingWithdrawalsOf(addr *common.Address, valID *hexutil.Big) (*Filter, error) {
	f, err := WithdrawalsOf(addr, valID)
	if err != nil {
		return nil, err
	}
	return f.IsNull("finalized_tx"), nil
}

// WithdrawalCursor renders the pagination cursor for a withdraw request.
//
// It does not use EncodeCursor because request_id is a uint256 and the shared cursor codec
// carries int64 components. Truncating a 256-bit request id into 64 bits would collide,
// and a colliding cursor silently paginates from the wrong row.
//
// This REPLACES the old cursor, which was the request transaction hash. A transaction can
// open more than one withdraw request, so the hash was never a unique position -- the
// GraphQL layer's WithdrawRequest.Id() must be switched to this before cutover.
func WithdrawalCursor(wr *types.WithdrawRequest) string {
	if wr == nil || wr.BlockNumber == nil || wr.TxIndex == nil || wr.WithdrawRequestID == nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join([]string{
		strconv.FormatUint(uint64(*wr.BlockNumber), 10),
		strconv.FormatUint(uint64(*wr.TxIndex), 10),
		wr.WithdrawRequestID.ToInt().String(),
	}, ":")))
}

// wrCursorPos is a decoded cursor position.
type wrCursorPos struct {
	blockNumber int64
	txIndex     int32
	requestID   *big.Int
}

// decodeWithdrawalCursor parses a withdrawal cursor, returning nil for an empty one.
//
// A malformed cursor is an error rather than a best-effort parse: paginating from a
// silently wrong position looks like missing chain data, not like a bad request.
func decodeWithdrawalCursor(s string) (*wrCursorPos, error) {
	if s == "" {
		return nil, nil
	}

	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("malformed withdrawal cursor")
	}

	parts := strings.Split(string(raw), ":")
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed withdrawal cursor: expected 3 components, found %d", len(parts))
	}

	bn, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("malformed withdrawal cursor: block number is not a number")
	}
	ix, err := strconv.ParseInt(parts[1], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("malformed withdrawal cursor: transaction index is not a number")
	}
	req, ok := new(big.Int).SetString(parts[2], 10)
	if !ok || req.Sign() < 0 {
		return nil, fmt.Errorf("malformed withdrawal cursor: request id is not a number")
	}
	return &wrCursorPos{blockNumber: bn, txIndex: int32(ix), requestID: req}, nil
}

// queryWithdrawals runs a withdrawal query and scans the result.
func (s *Store) queryWithdrawals(ctx context.Context, sql string, args ...any) ([]*types.WithdrawRequest, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("withdraw request query failed: %w", err)
	}
	defer rows.Close()

	out := make([]*types.WithdrawRequest, 0, 32)
	for rows.Next() {
		wr, err := scanWithdrawal(rows)
		if err != nil {
			return nil, fmt.Errorf("can not scan withdraw request: %w", err)
		}
		out = append(out, wr)
	}
	return out, rows.Err()
}

// scanWithdrawal maps one row onto the domain type.
func scanWithdrawal(row rowScanner) (*types.WithdrawRequest, error) {
	var (
		delegator, requestTx   []byte
		finalizedTx            []byte
		validatorID, requestID pgtype.Numeric
		blockNumber            int64
		txIndex                int32
		createdAt              time.Time
		amount, penalty        pgtype.Numeric
		finalizedAt            pgtype.Timestamptz
		reqType                string
	)

	if err := row.Scan(&delegator, &validatorID, &requestID, &requestTx, &blockNumber, &txIndex,
		&createdAt, &amount, &penalty, &finalizedTx, &finalizedAt, &reqType); err != nil {
		return nil, err
	}

	addr, err := ToAddr(delegator)
	if err != nil {
		return nil, err
	}
	reqTx, err := ToHash(requestTx)
	if err != nil {
		return nil, err
	}
	finTx, err := ToHash(finalizedTx)
	if err != nil {
		return nil, err
	}

	val, err := FromWei(validatorID)
	if err != nil {
		return nil, err
	}
	req, err := FromWei(requestID)
	if err != nil {
		return nil, err
	}
	amo, err := FromWei(amount)
	if err != nil {
		return nil, err
	}

	bn := hexutil.Uint64(blockNumber)
	ix := hexutil.Uint64(txIndex)

	wr := &types.WithdrawRequest{
		RequestTrx:        *reqTx,
		WithdrawRequestID: (*hexutil.Big)(req),
		Address:           *addr,
		StakerID:          (*hexutil.Big)(val),
		CreatedTime:       hexutil.Uint64(createdAt.Unix()),
		Amount:            (*hexutil.Big)(amo),
		Type:              reqType,
		BlockNumber:       &bn,
		TxIndex:           &ix,
		WithdrawTrx:       finTx,
	}

	// The finalization fields stay nil when the column is NULL. They are the marker for
	// "still pending", so collapsing NULL to a zero value would report every open request
	// as settled at time zero with a zero penalty.
	if finalizedAt.Valid {
		t := hexutil.Uint64(finalizedAt.Time.Unix())
		wr.WithdrawTime = &t
	}
	if penalty.Valid {
		p, err := FromWei(penalty)
		if err != nil {
			return nil, err
		}
		wr.Penalty = (*hexutil.Big)(p)
	}

	return wr, nil
}
