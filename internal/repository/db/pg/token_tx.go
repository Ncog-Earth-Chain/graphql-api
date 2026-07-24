package pg

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"ncogearthchain-api-graphql/internal/types"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/jackc/pgx/v5/pgtype"
)

// Decoded token transfers (ERC-20 / ERC-721 / ERC-1155).
//
// The single largest change here is the ordering key. MongoDB sorted and paginated this
// collection on `orx`, a uint64 built from a 40-bit block TIMESTAMP whose low three bytes
// were XOR-salted with trxHash[0:3], the log index and the sequence number. Three things
// were wrong with it, and all three are user-visible:
//
//   - transfers sharing a block second ordered by a hash XOR, i.e. arbitrarily, not by
//     execution order;
//   - the salt collides, so two distinct transfers could share an ordinal -- and keyset
//     pagination over a non-unique key silently skips or repeats rows at page boundaries;
//   - it is not monotonic in block number, so "newest first" was only approximately true.
//
// (block_number, log_index, seq) is the true execution order of a transfer on this chain,
// it is unique, and it is already the primary key. Ordering and paging on it is both
// correct and a straight index seek.

// tokenTxKeyset is the total ordering for token transfer lists: newest first, with the
// log position and the intra-log sequence breaking ties. The triple is the primary key,
// so the ordering is total and pagination can neither repeat nor skip a row.
var tokenTxKeyset = Keyset{Columns: []KeyColumn{
	{Name: "block_number", Dir: Desc},
	{Name: "log_index", Dir: Desc},
	{Name: "seq", Dir: Desc},
}}

// tokenTxColumns is the projection every token transfer read shares, so a column added in
// one place cannot be forgotten in another.
const tokenTxColumns = `
	block_number, log_index, seq, call_tx_hash, tx_index, token, std,
	event_type, from_addr, to_addr, amount, token_id, ts`

// maxCallTransfers bounds the transfers returned for one blockchain transaction.
//
// The MongoDB query was unbounded. A single batch-transfer call can emit thousands of
// ERC-1155 transfers, and an unbounded read of them is a client-triggered way to make the
// server materialise an arbitrarily large result.
const maxCallTransfers = 1000

// token standard codes. These are the SAME numbers as the account type codes in
// account.go by deliberate choice -- a token contract's `account.acct_type` and its
// transfers' `token_tx.std` describe the same fact, and keeping the encodings identical
// means a join between them never needs a translation table.
//
// The mapping is TOTAL in both directions and unknown values are rejected. Defaulting an
// unrecognised standard to ERC-20 would file NFT trades into the fungible-token list.

// tokenStdCode maps the domain token type onto its stored code.
//
// Non-token account types (wallet, contract, SFC) are rejected rather than accepted and
// stored: a transfer belongs to a token standard, and a `wallet` standard is a bug in the
// caller, not a value to persist.
func tokenStdCode(s string) (int16, error) {
	switch s {
	case types.AccountTypeERC20Token:
		return acctERC20, nil
	case types.AccountTypeERC721Contract:
		return acctERC721, nil
	case types.AccountTypeERC1155Contract:
		return acctERC1155, nil
	default:
		return 0, fmt.Errorf("unknown token standard %q", s)
	}
}

// tokenStdName maps a stored code back onto the domain token type.
func tokenStdName(c int16) (string, error) {
	switch c {
	case acctERC20:
		return types.AccountTypeERC20Token, nil
	case acctERC721:
		return types.AccountTypeERC721Contract, nil
	case acctERC1155:
		return types.AccountTypeERC1155Contract, nil
	default:
		return "", fmt.Errorf("unknown token standard code %d", c)
	}
}

// TokenTxCriteria describes which token transfers a list or count covers.
//
// This replaces the bson.D the repository layer used to assemble and hand down. That
// arrangement put MongoDB's query language above the storage seam; here the caller states
// what it wants in domain terms and this file owns the column names.
type TokenTxCriteria struct {
	// TokenType is the token standard and is REQUIRED. Every list endpoint the API
	// exposes is standard-specific, and every index on the table leads with `std`, so a
	// query without it is both meaningless and unservable.
	TokenType string

	// Token restricts to one contract.
	Token *common.Address

	// TokenId restricts to one token of a multi-token contract. Nil means "any",
	// which is NOT the same as token_id IS NULL.
	TokenId *big.Int

	// Account matches an address in either role, sender or recipient.
	Account *common.Address

	// EventTypes restricts to particular transfer events (transfer, approval, mint,
	// burn, approval-for-all). Empty means all of them.
	EventTypes []int32
}

// filter renders the criteria as a typed, fully bound Filter.
func (c TokenTxCriteria) filter() (*Filter, error) {
	std, err := tokenStdCode(c.TokenType)
	if err != nil {
		return nil, err
	}

	f := NewFilter().Eq("std", std)

	if c.Token != nil {
		f.Eq("token", AddrVal(*c.Token))
	}

	if c.TokenId != nil {
		id, err := Wei(c.TokenId)
		if err != nil {
			return nil, fmt.Errorf("token id filter: %w", err)
		}
		f.Eq("token_id", id)
	}

	// No tx_account-style edge table exists for token transfers, so the sender/recipient
	// disjunction is expressed directly and PostgreSQL serves it as a bitmap OR of
	// token_tx_std_from_idx and token_tx_std_to_idx.
	f.EitherAddr("from_addr", "to_addr", c.Account)

	f.InInts("event_type", c.EventTypes)

	return f, nil
}

// StoreTokenTransaction stores one decoded token transfer.
//
// Standalone variant for callers outside the block-ingest transaction. Prefer
// writeTokenTransaction with the ingest transaction's Querier where one exists, so a
// transfer cannot be committed independently of the block it belongs to.
func (s *Store) StoreTokenTransaction(ctx context.Context, trx *types.TokenTransaction) error {
	return s.writeTokenTransaction(ctx, s.pool, trx)
}

// writeTokenTransaction stores one decoded token transfer through the given Querier.
//
// Idempotent by ON CONFLICT DO NOTHING rather than by a preceding existence check. The
// MongoDB version did FindOne-then-InsertOne, which was wrong twice over: the check
// returned "not known" on a genuine database fault (so a transient error produced a
// duplicate insert attempt), and the check/insert pair is a TOCTOU race between two
// ingest goroutines. A single statement has neither problem, and re-ingesting a block
// stays a no-op.
func (s *Store) writeTokenTransaction(ctx context.Context, q Querier, trx *types.TokenTransaction) error {
	if trx == nil {
		return fmt.Errorf("can not store an empty token transaction")
	}

	std, err := tokenStdCode(trx.TokenType)
	if err != nil {
		return fmt.Errorf("token transfer in %s: %w", trx.Transaction.String(), err)
	}

	// The domain type carries the position in wider Go types than the columns hold.
	// Converting without a check would wrap a large value into a small negative one and
	// file the transfer at a position that is not merely wrong but ORDERED wrong, which
	// then corrupts pagination for every reader. None of these are reachable on a real
	// chain; that is exactly why an unchecked conversion would never be noticed.
	if trx.BlockNumber > math.MaxInt64 {
		return fmt.Errorf("token transfer block number %d exceeds the storable range", trx.BlockNumber)
	}
	if uint64(trx.LogIndex) > math.MaxInt32 {
		return fmt.Errorf("token transfer log index %d exceeds the storable range", trx.LogIndex)
	}
	if uint64(trx.TrxIndex) > math.MaxInt32 {
		return fmt.Errorf("token transfer transaction index %d exceeds the storable range", uint64(trx.TrxIndex))
	}
	if trx.Type < math.MinInt16 || trx.Type > math.MaxInt16 {
		return fmt.Errorf("token transfer event type %d exceeds the storable range", trx.Type)
	}

	amount, err := Wei(trx.Amount.ToInt())
	if err != nil {
		return fmt.Errorf("token transfer amount in %s: %w", trx.Transaction.String(), err)
	}

	tokenID, err := tokenIDValue(std, trx)
	if err != nil {
		return fmt.Errorf("token transfer in %s: %w", trx.Transaction.String(), err)
	}

	_, err = q.Exec(ctx, `
		INSERT INTO token_tx (block_number, log_index, seq, call_tx_hash, tx_index,
		                      token, std, event_type, from_addr, to_addr,
		                      amount, token_id, ts)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (block_number, log_index, seq) DO NOTHING`,
		int64(trx.BlockNumber),
		int32(trx.LogIndex),
		int32(trx.Seq),
		HashVal(trx.Transaction),
		int32(trx.TrxIndex),
		AddrVal(trx.TokenAddress),
		std,
		int16(trx.Type),
		AddrVal(trx.Sender),
		AddrVal(trx.Recipient),
		amount,
		tokenID,
		time.Unix(int64(trx.TimeStamp), 0).UTC(),
	)
	if err != nil {
		return fmt.Errorf("can not store token transfer %s: %w", trx.Pk(), err)
	}
	return nil
}

// tokenIDValue renders the token id, or SQL NULL where the event has none.
//
// ERC-20 transfers have no token id at all. MongoDB stored the string "0x0" for them,
// which is a different claim -- "token number zero" is a real and distinct NFT. It also
// makes the partial index on (std, token_id) WHERE token_id IS NOT NULL cover every row
// in the table instead of only the rows that can match, which is the whole point of it.
func tokenIDValue(std int16, trx *types.TokenTransaction) (any, error) {
	if std == acctERC20 {
		return nil, nil
	}
	n, err := Wei(trx.TokenId.ToInt())
	if err != nil {
		// Propagate rather than substituting NULL. NULL here means "this transfer has no
		// token id", which for an ERC-721/1155 transfer is false -- it collapses a bad
		// value into a different and equally wrong claim, and does it silently. The
		// caller can decide; this function must not decide for it.
		return nil, fmt.Errorf("token id: %w", err)
	}
	return n, nil
}

// TokenTransactionCount returns an approximate number of stored token transfers.
//
// Approximate by design, matching the MongoDB behaviour: it fed EstimatedDocumentCount,
// not a count. This is a headline figure on the largest secondary table in the database,
// where being current to the last few hundred rows does not matter; exact counts are used
// where a filter makes them cheap.
func (s *Store) TokenTransactionCount(ctx context.Context) (uint64, error) {
	var n int64
	// reltuples is -1 before the first ANALYZE on PostgreSQL 14+, hence the clamp.
	if err := s.pool.QueryRow(ctx, `
		SELECT GREATEST(reltuples, 0)::BIGINT FROM pg_class WHERE oid = 'token_tx'::regclass`).
		Scan(&n); err != nil {
		return 0, fmt.Errorf("can not estimate token transfer count: %w", err)
	}
	return uint64(n), nil
}

// TokenTransactionCountFiltered returns the exact number of token transfers matching a
// filter. A nil or empty filter counts the whole table.
func (s *Store) TokenTransactionCountFiltered(ctx context.Context, f *Filter) (uint64, error) {
	where, args := f.Where(0)

	var n int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM token_tx `+where, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("can not count token transfers: %w", err)
	}
	return uint64(n), nil
}

// TokenTransactions pages through token transfers matching the criteria, newest first.
//
// Replaces Erc20Transactions and the whole ercTrxListInit / CollectRangeMarks / BorderPk /
// ListFilter / ListOptions / ListLoad chain, which existed only to locate a starting
// ordinal and then range-scan from it. A keyset comparison does that in the query.
//
// Renamed from Erc20Transactions because it has always served ERC-721 and ERC-1155 too;
// TokenTransactions is also the name the repository layer already exposes for it.
func (s *Store) TokenTransactions(ctx context.Context, c TokenTxCriteria, cursor string, count int32) (*types.TokenTransactionList, error) {
	// Preserved: a zero count is rejected rather than silently defaulted. The sign of
	// count carries the paging DIRECTION, so zero has no direction to interpret.
	if count == 0 {
		return nil, fmt.Errorf("nothing to do, zero erc transactions requested")
	}

	f, err := c.filter()
	if err != nil {
		return nil, err
	}

	// Validate the cursor before spending a count on the query. The MongoDB version
	// looked the cursor up as a document id WITHOUT re-applying the base filter, so a
	// cursor minted under a different filter was accepted and paged from a position
	// outside the result set; a stale one surfaced as a raw ErrNoDocuments failure of the
	// whole request. Here the cursor is only a position in this query's own ordering.
	cur, err := DecodeCursor(cursor, 3)
	if err != nil {
		return nil, err
	}

	total, err := s.TokenTransactionCountFiltered(ctx, f)
	if err != nil {
		return nil, err
	}

	list := &types.TokenTransactionList{
		// Never nil. The resolvers index into this slice and a nil one would read as
		// "no page information available" rather than "no results".
		Collection: make([]*types.TokenTransaction, 0),
		Total:      total,
		IsStart:    total == 0,
		IsEnd:      total == 0,

		// First/Last are deliberately left zero. They held `orx` ordinal positions, and
		// that key no longer exists; inventing a block number for them would be a
		// different quantity wearing the same name. Callers should use
		// TokenTransactionCursor on the first and last elements instead -- which is what
		// the GraphQL PageInfo resolvers already do.
		//
		// The MongoDB code never assigned Last either, so Reverse() swapped a real First
		// with a zero Last on every backwards page.
	}

	if total == 0 {
		return list, nil
	}

	page := NewPage(count, maxListLimit)

	where, args := f.Render(0)

	if len(cur) == 3 {
		pred, curArgs, err := tokenTxKeyset.After(
			[]any{cur[0], int32(cur[1]), int32(cur[2])}, page.Reverse, len(args))
		if err != nil {
			return nil, err
		}
		where += " AND " + pred
		args = append(args, curArgs...)
	}

	// One row beyond the page is fetched purely to answer "is there more?" without a
	// second query, and is dropped before the page is returned.
	sql := `SELECT ` + tokenTxColumns + `
		FROM token_tx
		WHERE ` + where + `
		` + tokenTxKeyset.OrderBy(page.Reverse) + `
		LIMIT $` + itoa(len(args)+1)
	args = append(args, page.Limit+1)

	rows, err := s.queryTokenTransactions(ctx, sql, args...)
	if err != nil {
		return nil, err
	}

	hasMore := len(rows) > page.Limit
	if hasMore {
		rows = rows[:page.Limit]
	}

	// A boundary is reached when the query started at one end of the ordering, or when
	// the far end ran out of rows. Reading in reverse walks toward newer transfers, so
	// the two ends swap roles.
	if page.Reverse {
		list.IsEnd = cursor == ""
		list.IsStart = !hasMore
	} else {
		list.IsStart = cursor == ""
		list.IsEnd = !hasMore
	}

	// A backwards page is read oldest-first by the database; flip it so the newest
	// transfer is on top, as it is for a forwards page.
	if page.Reverse {
		for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
			rows[i], rows[j] = rows[j], rows[i]
		}
	}

	list.Collection = rows
	return list, nil
}

// TokenTransactionsByCall lists the token transfers emitted by one blockchain
// transaction, in execution order.
//
// Two fixes over the MongoDB version. It sorted by the salted ordinal, so transfers
// inside a single call came back in hash order rather than log order -- visibly wrong on
// a batch transfer. And it assigned the Find error to a variable it only inspected inside
// the decode loop, so a failed query left a nil cursor that the deferred close and the
// loop condition both dereferenced.
func (s *Store) TokenTransactionsByCall(ctx context.Context, trxHash *common.Hash) ([]*types.TokenTransaction, error) {
	if trxHash == nil {
		return nil, fmt.Errorf("no transaction hash given")
	}

	return s.queryTokenTransactions(ctx, `
		SELECT `+tokenTxColumns+`
		FROM token_tx
		WHERE call_tx_hash = $1
		ORDER BY log_index ASC, seq ASC
		LIMIT $2`, HashVal(*trxHash), maxCallTransfers)
}

// TokenAssetsByOwner lists the distinct token contracts an address has received tokens
// from, for one token standard.
//
// Replaces Erc20Assets, which was wrong in three ways at once: it rejected a legitimate
// count of 1 (`count <= 1`), it then never applied count at all -- an unbounded Distinct
// over an address's entire history -- and it filtered on neither standard nor event type,
// so an "ERC-20 assets" call returned ERC-721 and ERC-1155 contracts, and included tokens
// where the address was merely an Approval SPENDER. Being named as a spender is not
// ownership, and listing it as a held asset is a false statement about the address.
func (s *Store) TokenAssetsByOwner(ctx context.Context, owner *common.Address, tokenType string, count int32) ([]common.Address, error) {
	if owner == nil {
		return nil, fmt.Errorf("no owner address given")
	}

	std, err := tokenStdCode(tokenType)
	if err != nil {
		return nil, err
	}

	page := NewPage(count, maxListLimit)

	// Only events that actually move tokens INTO the address count as acquiring it:
	// a transfer in, or a mint. Approvals and burns do not.
	acquiring := []int32{types.TokenTrxTypeTransfer, types.TokenTrxTypeMint}

	// ORDER BY is load-bearing, not cosmetic: LIMIT over an unordered DISTINCT returns
	// an arbitrary subset, so the same owner could get a different asset list on every
	// call. token_tx_assets_idx is (to_addr, token), so this ordering is free.
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT token
		FROM token_tx
		WHERE to_addr = $1 AND std = $2 AND event_type = ANY($3)
		ORDER BY token
		LIMIT $4`, AddrVal(*owner), std, acquiring, page.Limit)
	if err != nil {
		return nil, fmt.Errorf("can not list %s assets of %s: %w", tokenType, owner.String(), err)
	}
	defer rows.Close()

	out := make([]common.Address, 0, page.Limit)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		a, err := ToAddr(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// TokenTransactionCursor renders the pagination cursor for a token transfer.
//
// The old cursor was the bare 14-byte hex primary key, which clients could read and
// construct -- so the ordering key had become public API. This one is opaque and encodes
// the position in the query's ordering, nothing more.
func TokenTransactionCursor(t *types.TokenTransaction) string {
	if t == nil {
		return ""
	}
	return EncodeCursor([]int64{int64(t.BlockNumber), int64(t.LogIndex), int64(t.Seq)})
}

// queryTokenTransactions runs a token transfer query and scans the result.
func (s *Store) queryTokenTransactions(ctx context.Context, sql string, args ...any) ([]*types.TokenTransaction, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("token transfer query failed: %w", err)
	}
	defer rows.Close()

	out := make([]*types.TokenTransaction, 0, 32)
	for rows.Next() {
		trx, err := scanTokenTransaction(rows)
		if err != nil {
			return nil, fmt.Errorf("can not scan token transfer: %w", err)
		}
		out = append(out, trx)
	}
	return out, rows.Err()
}

// scanTokenTransaction maps one row onto the domain type.
func scanTokenTransaction(row rowScanner) (*types.TokenTransaction, error) {
	var (
		blockNumber      int64
		logIndex, seq    int32
		callTxHash       []byte
		txIndex          int32
		token            []byte
		std, eventType   int16
		fromAddr, toAddr []byte
		amount, tokenID  pgtype.Numeric
		ts               pgtype.Timestamptz
	)

	if err := row.Scan(&blockNumber, &logIndex, &seq, &callTxHash, &txIndex, &token, &std,
		&eventType, &fromAddr, &toAddr, &amount, &tokenID, &ts); err != nil {
		return nil, err
	}

	call, err := ToHash(callTxHash)
	if err != nil {
		return nil, err
	}
	tok, err := ToAddr(token)
	if err != nil {
		return nil, err
	}
	sender, err := ToAddr(fromAddr)
	if err != nil {
		return nil, err
	}
	recipient, err := ToAddr(toAddr)
	if err != nil {
		return nil, err
	}
	stdName, err := tokenStdName(std)
	if err != nil {
		return nil, err
	}

	// Read through FromWei with a real error return. The MongoDB decoder parsed both
	// amounts with hexutil.MustDecodeBig, which PANICS on an empty or malformed string;
	// its deferred recover then called err.Error() while err was still nil, so a bad row
	// produced a second nil dereference inside the handler that was meant to contain the
	// first.
	amt, err := FromWei(amount)
	if err != nil {
		return nil, fmt.Errorf("token transfer amount: %w", err)
	}
	tid, err := FromWei(tokenID)
	if err != nil {
		return nil, fmt.Errorf("token transfer token id: %w", err)
	}

	// FromWei returns (nil, nil) for SQL NULL, and the domain type stores the amount by
	// VALUE -- so dereferencing a NULL here panics rather than erroring. A transfer with
	// no amount is a broken row, not a zero-value transfer, so say that instead of
	// inventing a zero.
	if amt == nil {
		return nil, fmt.Errorf("token transfer at block %d log %d has a NULL amount", blockNumber, logIndex)
	}

	trx := &types.TokenTransaction{
		Transaction:  *call,
		TrxIndex:     hexutil.Uint64(txIndex),
		TokenAddress: *tok,
		TokenType:    stdName,
		Type:         int32(eventType),
		Sender:       *sender,
		Recipient:    *recipient,
		Amount:       (hexutil.Big)(*amt),
		TimeStamp:    hexutil.Uint64(ts.Time.Unix()),
		BlockNumber:  uint64(blockNumber),
		LogIndex:     uint(logIndex),
		Seq:          uint16(seq),
	}

	// TokenId is a value, not a pointer, so the domain type cannot represent "no token
	// id" separately from "token id zero". A NULL column is therefore rendered as zero
	// here and the distinction survives only in the database. TokenType is the reliable
	// discriminator above this layer: an ERC-20 transfer never has one.
	if tid != nil {
		trx.TokenId = (hexutil.Big)(*tid)
	}

	// ID is not stored. It carries the opaque keyset cursor (base64 of block_number:log_index:seq)
	// that the GraphQL list resolvers emit and that TokenTransactions decodes with DecodeCursor(_, 3).
	// It was previously the bare 14-byte packed Pk() hex, which DecodeCursor rejected -- so every
	// token-transfer page after the first errored with "malformed cursor". ID is consumed only as the
	// cursor (no schema field exposes it), so encoding it as the cursor here is the single-point fix
	// for all three (ERC-20/721/1155) list resolvers.
	trx.ID = TokenTransactionCursor(trx)

	return trx, nil
}
