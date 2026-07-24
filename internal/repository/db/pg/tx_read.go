package pg

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Transaction reads.
//
// Ordering and pagination use (block_number, tx_index) directly, which is the change
// that retires the packed ordinal.
//
// The MongoDB schema ordered on a single uint64 `orx` computed as
// (block_number << 14) | (index & 0x3fff). Two things were wrong with it. The index was
// masked to 14 bits, so transaction 16384 in a block collided with transaction 0 -- and
// because `orx` carried a UNIQUE index, the collision was a write FAILURE that dropped
// the transaction rather than merely mis-ordering it. And the high mask was a no-op
// through operator precedence, so a sufficiently large block number would corrupt the
// ordering silently.
//
// A composite key has no such ceiling, and the row-value comparison in keyset.go turns
// it into a single index seek -- verified as `Index Only Scan Backward using
// tx_position_uq` with the comparison as an Index Cond rather than a filter.

// txKeyset is the total ordering for transaction lists: newest first, with tx_index
// breaking ties inside a block. The pair is unique, so pagination cannot repeat or skip
// a row at a page boundary.
var txKeyset = Keyset{Columns: []KeyColumn{
	{Name: "block_number", Dir: Desc},
	{Name: "tx_index", Dir: Desc},
}}

// txColumns is the projection every transaction read shares, so a column added in one
// place cannot be forgotten in another.
const txColumns = `
	hash, block_number, tx_index, block_hash, from_addr, to_addr,
	value_wei, nonce, gas_limit, gas_used, gas_cumulative, gas_price_wei,
	input, chain_id, sig_version, status, created_contract, ts`

// Transaction loads one transaction by hash.
//
// Returns (nil, nil) when it does not exist. A missing transaction is an ordinary
// outcome -- a client can ask about any hash -- so it is not an error.
func (s *Store) Transaction(ctx context.Context, hash *common.Hash) (*types.Transaction, error) {
	if hash == nil {
		return nil, fmt.Errorf("no transaction hash given")
	}

	row := s.pool.QueryRow(ctx, `SELECT `+txColumns+` FROM tx WHERE hash = $1`, HashVal(*hash))

	trx, err := scanTransaction(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("can not load transaction %s: %w", hash.String(), err)
	}
	return trx, nil
}

// TransactionsCount returns the total number of stored transactions.
//
// This is an estimate read from the planner's statistics, not a count. An exact
// count(*) on the largest table in the database walks every row, and this value feeds a
// headline counter where being current to the last few hundred rows does not matter.
// Exact counts are still used where a filter makes them cheap.
func (s *Store) TransactionsCount(ctx context.Context) (uint64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `
		SELECT GREATEST(reltuples::BIGINT, 0) FROM pg_class WHERE relname = 'tx'`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("can not estimate transaction count: %w", err)
	}
	return uint64(n), nil
}

// TransactionsByAccount lists transactions touching an address, newest first.
//
// Served by the tx_account edge table rather than by filtering tx with
// (from_addr = $1 OR to_addr = $1). No single index can serve that disjunction, so
// PostgreSQL would either scan the table or combine two index scans and sort -- on the
// single most requested page in an explorer. The edge table makes it one index scan on
// (address, block_number, tx_index), which is exactly the primary key.
func (s *Store) TransactionsByAccount(ctx context.Context, addr *common.Address, cursor string, count int32) ([]*types.Transaction, error) {
	if addr == nil {
		return nil, fmt.Errorf("no account address given")
	}

	page := NewPage(count, maxListLimit)

	cur, err := DecodeCursor(cursor, 2)
	if err != nil {
		return nil, err
	}

	args := []any{AddrVal(*addr)}
	where := "e.address = $1"

	if len(cur) == 2 {
		pred, curArgs, err := keysetOn("e", txKeyset).After(
			[]any{cur[0], int32(cur[1])}, page.Reverse, len(args))
		if err != nil {
			return nil, err
		}
		where += " AND " + pred
		args = append(args, curArgs...)
	}

	sql := `
		SELECT ` + prefixColumns("t", txColumns) + `
		FROM   tx_account e
		JOIN   tx t ON t.block_number = e.block_number AND t.tx_index = e.tx_index
		WHERE  ` + where + `
		` + keysetOn("e", txKeyset).OrderBy(page.Reverse) + `
		LIMIT $` + itoa(len(args)+1)

	// Fetch one row past the page so buildTransactionList can tell "exactly full" from "more
	// exist" without a second query; the probe row is trimmed before the page is returned.
	args = append(args, page.Limit+1)

	return s.queryTransactions(ctx, sql, args...)
}

// TransactionsInBlock lists a block's transactions in position order.
func (s *Store) TransactionsInBlock(ctx context.Context, number uint64) ([]*types.Transaction, error) {
	return s.queryTransactions(ctx, `
		SELECT `+txColumns+` FROM tx WHERE block_number = $1 ORDER BY tx_index ASC`,
		int64(number))
}

// TransactionList lists transactions across the chain, newest first.
func (s *Store) TransactionList(ctx context.Context, cursor string, count int32) ([]*types.Transaction, error) {
	page := NewPage(count, maxListLimit)

	cur, err := DecodeCursor(cursor, 2)
	if err != nil {
		return nil, err
	}

	var where string
	var args []any

	if len(cur) == 2 {
		pred, curArgs, err := txKeyset.After([]any{cur[0], int32(cur[1])}, page.Reverse, 0)
		if err != nil {
			return nil, err
		}
		where = "WHERE " + pred
		args = append(args, curArgs...)
	}

	sql := `SELECT ` + txColumns + ` FROM tx ` + where + ` ` +
		txKeyset.OrderBy(page.Reverse) + ` LIMIT $` + itoa(len(args)+1)
	// One row past the page: buildTransactionList uses it to set hasNextPage correctly on an
	// exactly-full final page, then discards it.
	args = append(args, page.Limit+1)

	return s.queryTransactions(ctx, sql, args...)
}

// TransactionCursor renders the pagination cursor for a transaction.
func TransactionCursor(t *types.Transaction) string {
	if t == nil || t.BlockNumber == nil || t.Index == nil {
		return ""
	}
	return EncodeCursor([]int64{int64(*t.BlockNumber), int64(*t.Index)})
}

// maxListLimit caps any single page. A client must not be able to choose how much work
// the database does.
const maxListLimit = 500

// queryTransactions runs a transaction query and scans the result.
func (s *Store) queryTransactions(ctx context.Context, sql string, args ...any) ([]*types.Transaction, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("transaction query failed: %w", err)
	}
	defer rows.Close()

	out := make([]*types.Transaction, 0, 32)
	for rows.Next() {
		trx, err := scanTransaction(rows)
		if err != nil {
			return nil, fmt.Errorf("can not scan transaction: %w", err)
		}
		out = append(out, trx)
	}
	return out, rows.Err()
}

// rowScanner is satisfied by both pgx.Row and pgx.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanTransaction maps one row onto the domain type.
func scanTransaction(row rowScanner) (*types.Transaction, error) {
	var (
		hash, blockHash, fromAddr []byte
		toAddr, createdContract   []byte
		blockNumber               int64
		txIndex                   int32
		valueWei, gasPriceWei     pgtype.Numeric
		nonce, gasLimit           int64
		gasUsed, gasCumulative    *int64
		input                     []byte
		chainID                   *int64
		sigVersion                *int16
		status                    int16
		ts                        pgtype.Timestamptz
	)

	if err := row.Scan(&hash, &blockNumber, &txIndex, &blockHash, &fromAddr, &toAddr,
		&valueWei, &nonce, &gasLimit, &gasUsed, &gasCumulative, &gasPriceWei,
		&input, &chainID, &sigVersion, &status, &createdContract, &ts); err != nil {
		return nil, err
	}

	h, err := ToHash(hash)
	if err != nil {
		return nil, err
	}
	bh, err := ToHash(blockHash)
	if err != nil {
		return nil, err
	}
	from, err := ToAddr(fromAddr)
	if err != nil {
		return nil, err
	}
	to, err := ToAddr(toAddr)
	if err != nil {
		return nil, err
	}
	created, err := ToAddr(createdContract)
	if err != nil {
		return nil, err
	}

	value, err := FromWei(valueWei)
	if err != nil {
		return nil, err
	}
	gasPrice, err := FromWei(gasPriceWei)
	if err != nil {
		return nil, err
	}

	bn := hexutil.Uint64(blockNumber)
	ix := hexutil.Uint64(txIndex)

	trx := &types.Transaction{
		Hash:        *h,
		BlockNumber: &bn,
		BlockHash:   bh,
		Index:       &ix,
		From:        *from,
		To:          to,
		Value:       hexutil.Big(*value),
		GasPrice:    hexutil.Big(*gasPrice),
		Gas:         hexutil.Uint64(gasLimit),
		Nonce:       hexutil.Uint64(nonce),
		InputData:   input,
		TimeStamp:   ts.Time,

		// ContractAddress is the contract this transaction CREATED, which is why it is
		// read from created_contract rather than from `to`.
		ContractAddress: created,
	}

	if chainID != nil {
		v := (hexutil.Big)(*new(big.Int).SetInt64(*chainID))
		trx.ChainID = &v
	}
	if sigVersion != nil {
		v := hexutil.Uint64(*sigVersion)
		trx.SigVersion = &v
	}

	if gasUsed != nil {
		v := hexutil.Uint64(*gasUsed)
		trx.GasUsed = &v
	}
	if gasCumulative != nil {
		v := hexutil.Uint64(*gasCumulative)
		trx.CumulativeGasUsed = &v
	}

	// statusUnknown means no receipt has been seen, which must stay distinct from a
	// receipt reporting failure. Collapsing them would report an unconfirmed
	// transaction as failed.
	if status != statusUnknown {
		v := hexutil.Uint64(status)
		trx.Status = &v
	}

	return trx, nil
}

// keysetOn qualifies a keyset's columns with a table alias, for queries that join.
func keysetOn(alias string, k Keyset) Keyset {
	cols := make([]KeyColumn, len(k.Columns))
	for i, c := range k.Columns {
		cols[i] = KeyColumn{Name: alias + "." + c.Name, Dir: c.Dir}
	}
	return Keyset{Columns: cols}
}

// prefixColumns qualifies a column list with a table alias.
func prefixColumns(alias, cols string) string {
	out := make([]byte, 0, len(cols)*2)
	field := make([]byte, 0, 32)

	flush := func() {
		if len(field) == 0 {
			return
		}
		out = append(out, alias...)
		out = append(out, '.')
		out = append(out, field...)
		field = field[:0]
	}

	for i := 0; i < len(cols); i++ {
		c := cols[i]
		switch {
		case c == ',':
			flush()
			out = append(out, ',', ' ')
		case c == ' ' || c == '\n' || c == '\t':
			// separators between fields are collapsed by flush
		default:
			field = append(field, c)
		}
	}
	flush()
	return string(out)
}

// itoa renders a small non-negative int without pulling in strconv at call sites.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
