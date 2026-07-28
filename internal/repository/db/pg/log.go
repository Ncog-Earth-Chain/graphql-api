package pg

import (
	"context"
	"fmt"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/jackc/pgx/v5/pgtype"
)

// Event log queries.
//
// MongoDB persisted logs INSIDE the transaction document and indexed nothing about them,
// so the explorer paid full write amplification on its largest collection and could not
// answer a single log query -- no filter by contract, no filter by event signature,
// nothing. This is the query that was impossible.
//
// The four topics are discrete columns rather than an array because EVM logs carry at
// most four, so the shape is fixed and plain btree indexes serve the equality lookups
// every real log query performs. An array would need GIN, which is larger and slower for
// exactly this access pattern.

// logKeyset orders logs newest-first. (block_number, log_index) is the primary key, so
// the ordering is total and pagination cannot repeat or skip a row.
var logKeyset = Keyset{Columns: []KeyColumn{
	{Name: "block_number", Dir: Desc},
	{Name: "log_index", Dir: Desc},
}}

const logColumns = `
	block_number, log_index, tx_hash, tx_index, address,
	topic0, topic1, topic2, topic3, topic_count, data, removed, ts`

// LogCriteria narrows a log query.
//
// Deliberately NOT a free-form filter. Every combination here is served by one of the
// table's indexes; an arbitrary predicate builder would let a client compose a query no
// index covers, which on the largest table in the database is a denial of service with
// extra steps.
type LogCriteria struct {
	// Address restricts to logs emitted by one contract. Served by tx_log_addr_idx, or
	// tx_log_addr_t0_idx when combined with Topic0.
	Address *common.Address

	// Topic0 is the event signature -- keccak256 of the event's canonical declaration.
	// This is the field that answers "all Transfer events", and pairing it with Address
	// is the single most useful log query there is.
	Topic0 *common.Hash

	// Topic1 and Topic2 are indexed event parameters. On an ERC-20 Transfer they are the
	// sender and recipient, which is what makes "every transfer involving this account"
	// answerable. Served by partial indexes that skip logs where they are absent.
	Topic1 *common.Hash
	Topic2 *common.Hash

	// FromBlock and ToBlock bound the range. Partition pruning uses these, so a bounded
	// query touches only the partitions it needs rather than every one.
	FromBlock *uint64
	ToBlock   *uint64
}

// Logs returns event logs matching the criteria, newest first.
func (s *Store) Logs(ctx context.Context, c LogCriteria, cursor string, count int32) ([]*types.Log, error) {
	page := NewPage(count, maxLogLimit)

	f := NewFilter()
	if c.Address != nil {
		f.Eq("address", AddrVal(*c.Address))
	}
	if c.Topic0 != nil {
		f.Eq("topic0", HashVal(*c.Topic0))
	}
	if c.Topic1 != nil {
		f.Eq("topic1", HashVal(*c.Topic1))
	}
	if c.Topic2 != nil {
		f.Eq("topic2", HashVal(*c.Topic2))
	}
	if c.FromBlock != nil {
		f.Gte("block_number", int64(*c.FromBlock))
	}
	if c.ToBlock != nil {
		f.Lte("block_number", int64(*c.ToBlock))
	}

	where, args := f.Render(0)

	cur, err := DecodeCursor(cursor, 2)
	if err != nil {
		return nil, err
	}
	if len(cur) == 2 {
		pred, curArgs, err := logKeyset.After([]any{cur[0], int32(cur[1])}, page.Reverse, len(args))
		if err != nil {
			return nil, err
		}
		if where != "" {
			where += " AND "
		}
		where += pred
		args = append(args, curArgs...)
	}

	sql := `SELECT ` + logColumns + ` FROM tx_log`
	if where != "" {
		sql += ` WHERE ` + where
	}
	sql += ` ` + logKeyset.OrderBy(page.Reverse) + ` LIMIT $` + itoa(len(args)+1)
	// One row PAST the page, as every sibling list store does. Without it a caller can
	// only guess "is there more" from whether the page came back short, which is wrong
	// for an exactly-full final page -- and every full page is exactly full here, since
	// the API's per-request cap is below this store's own maximum.
	args = append(args, page.Limit+1)

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("log query failed: %w", err)
	}
	defer rows.Close()

	out := make([]*types.Log, 0, page.Limit+1)
	for rows.Next() {
		lr, err := scanLog(rows)
		if err != nil {
			return nil, fmt.Errorf("can not scan log: %w", err)
		}
		out = append(out, lr)
	}
	return out, rows.Err()
}

// LogsByTransaction returns every log a transaction emitted, in emission order.
func (s *Store) LogsByTransaction(ctx context.Context, txHash *common.Hash) ([]*types.Log, error) {
	if txHash == nil {
		return nil, fmt.Errorf("no transaction hash given")
	}

	rows, err := s.pool.Query(ctx,
		`SELECT `+logColumns+` FROM tx_log WHERE tx_hash = $1 ORDER BY log_index ASC`,
		HashVal(*txHash))
	if err != nil {
		return nil, fmt.Errorf("transaction log query failed: %w", err)
	}
	defer rows.Close()

	out := make([]*types.Log, 0, 8)
	for rows.Next() {
		lr, err := scanLog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, lr)
	}
	return out, rows.Err()
}

// maxLogLimit caps a log page. Lower than the general list cap: a log carries arbitrary
// data bytes, so a page of logs is far larger than a page of transactions.
const maxLogLimit = 200

// LogCursor renders the pagination cursor for a log record.
func LogCursor(blockNumber uint64, logIndex uint) string {
	return EncodeCursor([]int64{int64(blockNumber), int64(logIndex)})
}

// scanLog maps one row onto the domain type.
func scanLog(row rowScanner) (*types.Log, error) {
	var (
		blockNumber       int64
		logIndex, txIndex int32
		txHash, address   []byte
		t0, t1, t2, t3    []byte
		topicCount        int16
		data              []byte
		removed           bool
		ts                pgtype.Timestamptz
	)

	if err := row.Scan(&blockNumber, &logIndex, &txHash, &txIndex, &address,
		&t0, &t1, &t2, &t3, &topicCount, &data, &removed, &ts); err != nil {
		return nil, err
	}

	addr, err := ToAddr(address)
	if err != nil {
		return nil, err
	}
	th, err := ToHash(txHash)
	if err != nil {
		return nil, err
	}

	// Reassemble only the topics the log actually had. topic_count is stored precisely
	// because a NULL topic and an absent topic are different: an anonymous event has no
	// signature topic at all, and padding it back to four would invent topics it never
	// emitted.
	topics := make([]common.Hash, 0, topicCount)
	for i, raw := range [][]byte{t0, t1, t2, t3} {
		if int16(i) >= topicCount {
			break
		}
		h, err := ToHash(raw)
		if err != nil {
			return nil, err
		}
		if h == nil {
			break
		}
		topics = append(topics, *h)
	}

	return &types.Log{
		Address:     *addr,
		Topics:      topics,
		Data:        data,
		BlockNumber: hexutil.Uint64(blockNumber),
		TxHash:      *th,
		TxIndex:     hexutil.Uint64(txIndex),
		Index:       hexutil.Uint64(logIndex),
		Removed:     removed,
		TimeStamp:   hexutil.Uint64(ts.Time.Unix()),
	}, nil
}
