package pg

import (
	"context"
	"errors"
	"fmt"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// SFC delegation reads and writes.
//
// Ordering changes here, and the change is the point of the port. MongoDB ordered
// delegations on a packed `orx` ordinal built from 40 bits of creation time, 12 bits of
// validator id and 12 bits of the creation transaction hash. Those 12-bit fields are far
// too narrow -- validator 4097 collides with validator 1, and the hash contributes a
// 4096-way tie-break -- so two delegations created in the same second could not be
// ordered against each other at all, and pagination on that key could repeat or skip
// rows. (block_number, tx_index) is the position the chain itself assigns, is unique,
// and both delegation indexes carry it, so a page is one index seek with no sort node.
//
// The delegation table has no ordinal column by design; cursors are the opaque keyset
// tokens from keyset.go.

// ErrUnknownDelegation is returned when an operation names a delegation that is not
// stored.
//
// It is a sentinel because it is load-bearing control flow rather than a failure: the
// SFC balance handler treats it as "this delegation has not been indexed yet" and runs a
// back-fill from the chain. Collapsing it into a generic error, or into a (nil, nil)
// not-found, would silently disable that recovery path and leave delegations permanently
// missing.
//
// NOTE FOR CALLERS: the MongoDB bridge exported this same sentinel as
// db.ErrUnknownDelegation, and repository/sfc_delegation.go compares against it with
// `==`. Callers moving to this store must compare against pg.ErrUnknownDelegation
// instead -- an equality test against the old variable compiles but never matches, which
// is a silent loss of the back-fill.
var ErrUnknownDelegation = errors.New("unknown delegation")

// delegationKeyset is the total ordering for delegation lists: newest first. The pair is
// unique per row, so a page boundary can neither repeat nor drop a delegation.
var delegationKeyset = Keyset{Columns: []KeyColumn{
	{Name: "block_number", Dir: Desc},
	{Name: "tx_index", Dir: Desc},
}}

// delegationColumns is the projection every delegation read shares.
//
// created_at is converted at the database rather than scanned as a timestamp and
// converted in Go: EXTRACT(EPOCH FROM timestamptz) is an absolute instant and does not
// depend on the session time zone, so the round-trip through the domain type's unix
// seconds is exact regardless of how the connection is configured.
const delegationColumns = `
	delegator, validator_id, validator_addr, created_tx, block_number, tx_index,
	EXTRACT(EPOCH FROM created_at)::BIGINT, amount_staked, amount_delegated`

// Delegation returns details of a delegation from an address to a validator ID.
//
// Returns ErrUnknownDelegation when the pair is not stored. This is the one lookup in
// this package that reports not-found as an error rather than (nil, nil), and
// deliberately so: see ErrUnknownDelegation. The caller also dereferences the result
// immediately, so a (nil, nil) return would be a nil dereference rather than a miss.
func (s *Store) Delegation(ctx context.Context, addr *common.Address, valID *hexutil.Big) (*types.Delegation, error) {
	if addr == nil {
		return nil, fmt.Errorf("no delegator address given")
	}
	if valID == nil {
		return nil, fmt.Errorf("no validator id given")
	}

	id, err := Wei(valID.ToInt())
	if err != nil {
		return nil, fmt.Errorf("invalid validator id: %w", err)
	}

	row := s.pool.QueryRow(ctx, `SELECT `+delegationColumns+`
		FROM delegation WHERE delegator = $1 AND validator_id = $2`,
		AddrVal(*addr), id)

	dlg, err := scanDelegation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		// Not logged. A miss here is ordinary control flow -- the caller's whole
		// recovery path begins with it -- and the MongoDB version logged it at error
		// level, which meant a healthy back-fill filled the log with false alarms.
		return nil, ErrUnknownDelegation
	}
	if err != nil {
		return nil, fmt.Errorf("can not load delegation %s to #%s: %w",
			addr.String(), valID.ToInt().String(), err)
	}
	return dlg, nil
}

// AddDelegation stores a delegation, replacing any existing row for the same
// (delegator, validator) pair.
//
// One statement, no preceding existence check. The MongoDB version read the row, then
// branched into an insert or an update; two ingest workers handling the same delegation
// could both observe "absent" and both insert, and the unique index turned the loser
// into a failed block. ON CONFLICT resolves that inside the database.
//
// blockNumber and txIndex are parameters because types.Delegation carries no chain
// position -- it only had the packed ordinal, which is exactly what this schema retires.
// Both columns are NOT NULL and they are the sort key, so the caller (the SFC log
// handler) must supply them from the log it is decoding.
func (s *Store) AddDelegation(ctx context.Context, dl *types.Delegation, blockNumber uint64, txIndex uint32) error {
	return s.upsertDelegation(ctx, dl, blockNumber, txIndex)
}

// UpdateDelegation writes the given delegation, inserting it if it is not yet stored.
//
// Identical to AddDelegation: under MongoDB the two differed only in that the update
// path was an upsert whose $set omitted `crt` and `amo`, so an update that landed on the
// insert branch produced a document with no creation time and no staked amount. Reading
// that document then panicked inside the BSON decoder and was recovered as "can not
// decode BIG number" -- the row was permanently unreadable. Writing the full column set
// makes that unrepresentable, and PostgreSQL's NOT NULL on amount_staked would refuse it
// anyway.
func (s *Store) UpdateDelegation(ctx context.Context, dl *types.Delegation, blockNumber uint64, txIndex uint32) error {
	return s.upsertDelegation(ctx, dl, blockNumber, txIndex)
}

// upsertDelegation writes the complete delegation row.
func (s *Store) upsertDelegation(ctx context.Context, dl *types.Delegation, blockNumber uint64, txIndex uint32) error {
	if dl == nil {
		return fmt.Errorf("can not store an empty delegation")
	}
	if dl.ToStakerId == nil {
		return fmt.Errorf("delegation of %s has no validator id", dl.Address.String())
	}

	// Checked here rather than left to the NOT NULL constraint so the error names the
	// field. A constraint violation from inside a block ingest reports only the column
	// and the table, which is not enough to find the log handler that dropped the value.
	if dl.AmountStaked == nil {
		return fmt.Errorf("delegation %s to #%s has no staked amount",
			dl.Address.String(), dl.ToStakerId.ToInt().String())
	}
	if dl.AmountDelegated == nil {
		return fmt.Errorf("delegation %s to #%s has no delegated amount",
			dl.Address.String(), dl.ToStakerId.ToInt().String())
	}

	valID, err := Wei(dl.ToStakerId.ToInt())
	if err != nil {
		return fmt.Errorf("invalid validator id on delegation of %s: %w", dl.Address.String(), err)
	}
	staked, err := Wei(dl.AmountStaked.ToInt())
	if err != nil {
		return fmt.Errorf("invalid staked amount on delegation of %s: %w", dl.Address.String(), err)
	}
	delegated, err := Wei(dl.AmountDelegated.ToInt())
	if err != nil {
		return fmt.Errorf("invalid delegated amount on delegation of %s: %w", dl.Address.String(), err)
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO delegation (
			delegator, validator_id, validator_addr, created_tx,
			block_number, tx_index, created_at, amount_staked, amount_delegated)
		VALUES ($1, $2, $3, $4, $5, $6, to_timestamp($7), $8, $9)
		ON CONFLICT (delegator, validator_id) DO UPDATE SET
			-- COALESCE, not a plain assignment: the validator address is resolved
			-- asynchronously and is NULL until it is known. A later re-delivery that has
			-- not resolved it yet would otherwise overwrite a good address with NULL,
			-- losing information the database already held.
			validator_addr   = COALESCE(EXCLUDED.validator_addr, delegation.validator_addr),
			created_tx       = EXCLUDED.created_tx,
			block_number     = EXCLUDED.block_number,
			tx_index         = EXCLUDED.tx_index,
			created_at       = EXCLUDED.created_at,
			amount_staked    = EXCLUDED.amount_staked,
			amount_delegated = EXCLUDED.amount_delegated`,
		AddrVal(dl.Address), valID, delegationValidatorAddr(dl.ToStakerAddress),
		HashVal(dl.Transaction), int64(blockNumber), int32(txIndex),
		int64(dl.CreatedTime), staked, delegated)
	if err != nil {
		return fmt.Errorf("can not store delegation %s to #%s: %w",
			dl.Address.String(), dl.ToStakerId.ToInt().String(), err)
	}
	return nil
}

// UpdateDelegationBalance sets the active delegated amount of an existing delegation.
//
// Deliberately NOT an upsert, matching the MongoDB behaviour, and the ErrUnknownDelegation
// on zero rows is the contract the caller depends on: it is the signal that starts the
// back-fill of a delegation the indexer has not seen yet. An upsert here would create a
// row with a fabricated creation transaction and timestamp, and the back-fill would then
// never run because the delegation now "exists".
//
// The MongoDB version also maintained a second, scaled copy of the amount
// (amountDelegated / 1e9, truncated into a uint64). That copy is dropped. It was the
// column IsDelegating tested, so every delegation below one gwei was reported as not
// delegating at all, and anything above ~1.8e10 NEC wrapped. The exact uint256 column is
// the only amount now.
func (s *Store) UpdateDelegationBalance(ctx context.Context, addr *common.Address, valID *hexutil.Big, amo *hexutil.Big) error {
	if addr == nil {
		return fmt.Errorf("no delegator address given")
	}
	if valID == nil {
		return fmt.Errorf("no validator id given")
	}
	if amo == nil {
		return fmt.Errorf("no delegated amount given")
	}

	id, err := Wei(valID.ToInt())
	if err != nil {
		return fmt.Errorf("invalid validator id: %w", err)
	}
	amount, err := Wei(amo.ToInt())
	if err != nil {
		return fmt.Errorf("invalid delegated amount for %s: %w", addr.String(), err)
	}

	tag, err := s.pool.Exec(ctx, `
		UPDATE delegation SET amount_delegated = $3
		WHERE delegator = $1 AND validator_id = $2`,
		AddrVal(*addr), id, amount)
	if err != nil {
		return fmt.Errorf("can not update delegation balance of %s to #%s: %w",
			addr.String(), valID.ToInt().String(), err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUnknownDelegation
	}
	return nil
}

// IsDelegating reports whether an address holds any delegation with a non-zero active
// amount.
//
// Replaces DelegationsCountFiltered, whose only caller counted matching rows and then
// compared the count against zero. EXISTS stops at the first match; the partial index
// delegation_active_idx covers exactly this predicate, so the check is one index probe
// regardless of how many delegations the address holds.
func (s *Store) IsDelegating(ctx context.Context, addr *common.Address) (bool, error) {
	if addr == nil {
		return false, fmt.Errorf("no delegator address given")
	}

	var exists bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM delegation WHERE delegator = $1 AND amount_delegated > 0)`,
		AddrVal(*addr)).Scan(&exists); err != nil {
		return false, fmt.Errorf("can not check delegations of %s: %w", addr.String(), err)
	}
	return exists, nil
}

// DelegationsByDelegator lists the delegations of one delegator, newest first.
func (s *Store) DelegationsByDelegator(ctx context.Context, addr *common.Address, cursor string, count int32) (*types.DelegationList, error) {
	if addr == nil {
		return nil, fmt.Errorf("no delegator address given")
	}
	return s.delegationList(ctx, "delegator", AddrVal(*addr), cursor, count)
}

// DelegationsByValidator lists the delegations received by one validator, newest first.
func (s *Store) DelegationsByValidator(ctx context.Context, valID *hexutil.Big, cursor string, count int32) (*types.DelegationList, error) {
	if valID == nil {
		return nil, fmt.Errorf("no validator id given")
	}
	id, err := Wei(valID.ToInt())
	if err != nil {
		return nil, fmt.Errorf("invalid validator id: %w", err)
	}
	return s.delegationList(ctx, "validator_id", id, cursor, count)
}

// delegationList serves one page of delegations filtered by a single indexed column.
//
// The column name is chosen from this file's own two call sites and never from caller
// input; the filter VALUE is always bound. Both supported columns lead an index that
// continues with the full sort key, so the whole page is one index scan.
//
// This replaces the MongoDB "border PK" dance -- a separate FindOne to discover the
// ordinal at the edge of the page, whose error was swallowed by a shadowed variable, so
// a cursor pointing at a row that no longer existed silently served page one as though
// it were the requested page.
func (s *Store) delegationList(ctx context.Context, column string, value any, cursor string, count int32) (*types.DelegationList, error) {
	// Preserved from the MongoDB version: direction is carried in the sign of count, so
	// zero has no direction and cannot be interpreted.
	if count == 0 {
		return nil, fmt.Errorf("nothing to do, zero delegations requested")
	}

	page := NewPage(count, maxListLimit)

	cur, err := DecodeCursor(cursor, 2)
	if err != nil {
		return nil, err
	}

	// Exact, not estimated: the filter makes it an index-only scan of one delegator's
	// or one validator's entries, which is bounded by that party's delegation count
	// rather than by the size of the table.
	var total int64
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM delegation WHERE `+column+` = $1`, value).Scan(&total); err != nil {
		return nil, fmt.Errorf("can not count delegations: %w", err)
	}

	list := &types.DelegationList{
		// Never nil. The GraphQL layer marshals a nil slice as null, and the schema
		// declares a non-null list of edges.
		Collection: make([]*types.Delegation, 0),
		Total:      uint64(total),
		IsStart:    total == 0,
		IsEnd:      total == 0,
	}
	if total == 0 {
		return list, nil
	}

	args := []any{value}
	where := column + " = $1"

	if len(cur) == 2 {
		pred, curArgs, err := delegationKeyset.After([]any{cur[0], int32(cur[1])}, page.Reverse, len(args))
		if err != nil {
			return nil, err
		}
		where += " AND " + pred
		args = append(args, curArgs...)
	}

	// One row beyond the page, to learn whether the list continues past it without a
	// second query. It is trimmed below and never reaches the caller.
	sql := `SELECT ` + delegationColumns + ` FROM delegation WHERE ` + where + ` ` +
		delegationKeyset.OrderBy(page.Reverse) + ` LIMIT $` + itoa(len(args)+1)
	args = append(args, page.Limit+1)

	rows, err := s.queryDelegations(ctx, sql, args...)
	if err != nil {
		return nil, err
	}

	hasMore := len(rows) > page.Limit
	if hasMore {
		// Trimmed BEFORE any reversal. The MongoDB version reversed first and then cut
		// the tail, which after reversal is the newest row on the page -- so a backward
		// page dropped a row the client should have seen and kept the probe row it
		// should not have.
		rows = rows[:page.Limit]
	}
	if page.Reverse {
		// Rows arrive oldest-first when paging backwards; the list is always presented
		// newest-first.
		for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
			rows[i], rows[j] = rows[j], rows[i]
		}
	}

	list.Collection = rows

	// Boundary marks, matching the MongoDB semantics: an absent cursor means the page
	// starts at whichever end the direction implies, and not filling the page means the
	// other end was reached.
	if page.Reverse {
		list.IsEnd = cursor == ""
		list.IsStart = !hasMore
	} else {
		list.IsStart = cursor == ""
		list.IsEnd = !hasMore
	}
	return list, nil
}

// DelegationsByDelegatorAll lists a delegator's delegations without paging, newest
// first.
//
// Bounded, unlike the MongoDB version, which fetched every matching document with no
// limit at all -- an address with a large number of delegations was a remotely
// triggerable way to make the API materialise an unbounded result set.
//
// Ordering also changes from the creation timestamp to (block_number, tx_index). Many
// delegations share a block timestamp, so the timestamp alone is not a total order and
// the same request could return the same rows in a different sequence each time.
func (s *Store) DelegationsByDelegatorAll(ctx context.Context, addr *common.Address, limit int32) ([]*types.Delegation, error) {
	if addr == nil {
		return nil, fmt.Errorf("no delegator address given")
	}

	page := NewPage(limit, maxListLimit)

	return s.queryDelegations(ctx, `SELECT `+delegationColumns+`
		FROM delegation WHERE delegator = $1
		`+delegationKeyset.OrderBy(false)+` LIMIT $2`,
		AddrVal(*addr), page.Limit)
}

// DelegationCursor renders the pagination cursor for a delegation.
//
// The delegation carries it in ID, which is where the GraphQL edge resolver reads its
// cursor from; under MongoDB that field held the document's _id.
func DelegationCursor(blockNumber int64, txIndex int32) string {
	return EncodeCursor([]int64{blockNumber, int64(txIndex)})
}

// delegationValidatorAddr renders the validator address for storage.
//
// The zero address is stored as NULL rather than as twenty zero bytes. Mongo wrote the
// zero address when the validator's address was not yet known, which is a claim that the
// validator IS address 0x00..00 rather than that it is unknown. The read path maps NULL
// back to the zero address, so the domain type sees no change; the important part is
// that only ONE representation of "unknown" is ever written, because a mix of NULL and
// zero would make an equality filter on the column miss rows.
func delegationValidatorAddr(a common.Address) []byte {
	if a == (common.Address{}) {
		return nil
	}
	return AddrVal(a)
}

// queryDelegations runs a delegation query and scans the result.
func (s *Store) queryDelegations(ctx context.Context, sql string, args ...any) ([]*types.Delegation, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("delegation query failed: %w", err)
	}
	defer rows.Close()

	out := make([]*types.Delegation, 0, 32)
	for rows.Next() {
		dlg, err := scanDelegation(rows)
		if err != nil {
			return nil, fmt.Errorf("can not scan delegation: %w", err)
		}
		out = append(out, dlg)
	}
	return out, rows.Err()
}

// scanDelegation maps one row onto the domain type.
func scanDelegation(row rowScanner) (*types.Delegation, error) {
	var (
		delegator, validatorAddr, createdTx []byte
		validatorID, staked, delegated      pgtype.Numeric
		blockNumber                         int64
		txIndex                             int32
		createdAt                           int64
	)

	if err := row.Scan(&delegator, &validatorID, &validatorAddr, &createdTx,
		&blockNumber, &txIndex, &createdAt, &staked, &delegated); err != nil {
		return nil, err
	}

	adr, err := ToAddr(delegator)
	if err != nil {
		return nil, err
	}
	trx, err := ToHash(createdTx)
	if err != nil {
		return nil, err
	}
	val, err := ToAddr(validatorAddr)
	if err != nil {
		return nil, err
	}

	id, err := FromWei(validatorID)
	if err != nil {
		return nil, err
	}
	amoStaked, err := FromWei(staked)
	if err != nil {
		return nil, err
	}
	amoDelegated, err := FromWei(delegated)
	if err != nil {
		return nil, err
	}

	dlg := &types.Delegation{
		// ID is the pagination cursor, which is what the GraphQL edge resolver returns
		// from it. Deriving it from the row means a client never sees the ordering key
		// itself and cannot pin the API to it, which is how the old packed ordinal
		// became impossible to change.
		ID:              DelegationCursor(blockNumber, txIndex),
		Transaction:     *trx,
		Address:         *adr,
		ToStakerId:      (*hexutil.Big)(id),
		CreatedTime:     hexutil.Uint64(createdAt),
		AmountStaked:    (*hexutil.Big)(amoStaked),
		AmountDelegated: (*hexutil.Big)(amoDelegated),
	}

	// NULL means the validator's address was never resolved; the domain type has no way
	// to express that, so it reads back as the zero address exactly as MongoDB stored
	// it. See delegationValidatorAddr for why only one of the two is ever written.
	if val != nil {
		dlg.ToStakerAddress = *val
	}

	// types.Delegation.Index is left at zero. It held the packed ordinal, which has no
	// column here and no reader -- it is not exposed in the GraphQL schema, and the
	// cursor it used to serve is now ID.

	return dlg, nil
}
