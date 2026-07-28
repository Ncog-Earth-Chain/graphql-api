package resolvers

import (
	"context"

	"ncogearthchain-api-graphql/internal/repository"
	"ncogearthchain-api-graphql/internal/repository/db/pg"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Log resolves an EVM event log.
type Log struct{ types.Log }

// LogList resolves a page of logs.
type LogList struct {
	list []*types.Log

	// asked is the page size the client requested.
	asked int32

	// Page boundaries, settled once at construction by trimProbePage. They used to be
	// inferred here from a short page, which cannot distinguish an exactly-full last page
	// from a full one with more behind it -- and counting logs matching a filter, the
	// alternative, would scan every matching row on the largest table in the database.
	// The store now fetches one row past the page instead, which answers it exactly.
	hasNext bool
	hasPrev bool
}

// LogListEdge resolves one entry with its cursor.
type LogListEdge struct {
	Log *Log
}

// Logs resolves event logs matching the given filters.
//
// This query did not exist. Logs were persisted inside the transaction document and
// indexed by nothing, so the explorer paid the write cost of storing them and could not
// answer a single question about them.
func (rs *rootResolver) Logs(ctx context.Context, args struct {
	Address   *common.Address
	Topic0    *common.Hash
	Topic1    *common.Hash
	Topic2    *common.Hash
	FromBlock *hexutil.Uint64
	ToBlock   *hexutil.Uint64
	Cursor    *Cursor
	Count     int32
}) (*LogList, error) {
	args.Count = listLimitCount(args.Count, listMaxEdgesPerRequest)

	c := pg.LogCriteria{
		Address: args.Address,
		Topic0:  args.Topic0,
		Topic1:  args.Topic1,
		Topic2:  args.Topic2,
	}
	if args.FromBlock != nil {
		v := uint64(*args.FromBlock)
		c.FromBlock = &v
	}
	if args.ToBlock != nil {
		v := uint64(*args.ToBlock)
		c.ToBlock = &v
	}

	rows, err := repository.R().Logs(ctx, c, (*string)(args.Cursor), args.Count)
	if err != nil {
		log.Errorf("can not get logs; %s", err.Error())
		return nil, err
	}
	page, hasNext, hasPrev := trimProbePage(rows, args.Count, args.Cursor != nil)
	return &LogList{list: page, asked: args.Count, hasNext: hasNext, hasPrev: hasPrev}, nil
}

// Edges resolves the page entries.
func (ll *LogList) Edges() []LogListEdge {
	out := make([]LogListEdge, len(ll.list))
	for i, l := range ll.list {
		out[i] = LogListEdge{Log: &Log{Log: *l}}
	}
	return out
}

// PageInfo resolves the page boundaries.
//
// The flags come from the probe row the store fetches past the page, not from a count:
// counting logs matching a filter would scan every matching row on the largest table in the
// database, and the probe answers the only question a paging client actually has for the
// cost of one extra row.
func (ll *LogList) PageInfo() (*ListPageInfo, error) {
	if len(ll.list) == 0 {
		return NewListPageInfo(nil, nil, false, false)
	}

	first := Cursor(pg.LogCursor(uint64(ll.list[0].BlockNumber), uint(ll.list[0].Index)))
	last := ll.list[len(ll.list)-1]
	lastCur := Cursor(pg.LogCursor(uint64(last.BlockNumber), uint(last.Index)))

	return NewListPageInfo(&first, &lastCur, ll.hasNext, ll.hasPrev)
}

// Cursor resolves the pagination cursor of a log.
func (e LogListEdge) Cursor() Cursor {
	return Cursor(pg.LogCursor(uint64(e.Log.BlockNumber), uint(e.Log.Index)))
}

// Topics resolves the indexed event parameters.
func (l *Log) Topics() []common.Hash { return l.Log.Topics }

// Data resolves the ABI-encoded non-indexed parameters.
func (l *Log) Data() hexutil.Bytes { return l.Log.Data }

// TransactionHash resolves the emitting transaction's hash.
func (l *Log) TransactionHash() common.Hash { return l.Log.TxHash }

// TransactionIndex resolves the emitting transaction's position in its block.
func (l *Log) TransactionIndex() hexutil.Uint64 { return l.Log.TxIndex }

// LogIndex resolves the log's position within its block.
func (l *Log) LogIndex() hexutil.Uint64 { return l.Log.Index }

// Timestamp resolves the emitting block's timestamp.
func (l *Log) Timestamp() hexutil.Uint64 { return l.Log.TimeStamp }

// Transaction resolves the transaction that emitted this log.
func (l *Log) Transaction(ctx context.Context) (*Transaction, error) {
	trx, err := repository.R().IndexedTransaction(ctx, &l.Log.TxHash)
	if err != nil || trx == nil {
		return nil, err
	}
	return NewTransaction(trx), nil
}

// absCount returns the magnitude of a signed page size. Direction lives in the sign; a
// zero means the schema default.
func absCount(v int32) int32 {
	if v < 0 {
		return -v
	}
	if v == 0 {
		return 25
	}
	return v
}
