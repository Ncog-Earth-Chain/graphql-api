// Package resolvers implements GraphQL resolvers to incoming API requests.
package resolvers

import (
	"context"
	"math/big"
	"ncogearthchain-api-graphql/internal/repository"
	"ncogearthchain-api-graphql/internal/repository/db/pg"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common/hexutil"
)

// TransactionList represents resolvable list of blockchain transaction edges structure.
type TransactionList struct {
	types.TransactionList
}

// TransactionListEdge represents a single edge of a transaction list structure.
type TransactionListEdge struct {
	Transaction *Transaction
	Cursor      Cursor
}

// NewTransactionList builds new resolvable list of transactions.
func NewTransactionList(txs *types.TransactionList) *TransactionList {
	return &TransactionList{
		TransactionList: *txs,
	}
}

// Transactions resolves list of blockchain transactions encapsulated in a listable structure.
func (rs *rootResolver) Transactions(ctx context.Context, args *struct {
	Cursor *Cursor
	Count  int32
}) (*TransactionList, error) {
	// limit query size; the count can be either positive or negative
	// this controls the loading direction
	args.Count = listLimitCount(args.Count, listMaxEdgesPerRequest)

	// get the transaction hash list from repository
	txs, err := repository.R().Transactions(ctx, (*string)(args.Cursor), args.Count)
	if err != nil {
		log.Errorf("can not get transactions list; %s", err.Error())
		return nil, err
	}
	return NewTransactionList(txs), nil
}

// TotalCount resolves the total number of transactions in the list.
//
// Read this together with TotalCountIsExact: when that is false, this value is a lower
// bound rather than a total.
func (tl *TransactionList) TotalCount() hexutil.Big {
	val := (*hexutil.Big)(big.NewInt(int64(tl.Total)))
	return *val
}

// TotalCountIsExact resolves whether TotalCount is an exact count or a lower bound.
func (tl *TransactionList) TotalCountIsExact() bool {
	return tl.TotalIsExact
}

// PageInfo resolves the current page information for the transaction list.
func (tl *TransactionList) PageInfo() (*ListPageInfo, error) {
	// do we have any items?
	if tl.Collection == nil || len(tl.Collection) == 0 {
		return NewListPageInfo(nil, nil, false, false)
	}

	// get the first and last elements
	//
	// The cursor is the opaque keyset token (base64 of block_number:tx_index) the store's
	// TransactionList/TransactionsByAccount decode with DecodeCursor(_, 2) -- NOT the tx hash.
	// Emitting the hash here made every page after the first fail with "malformed cursor".
	first := Cursor(pg.TransactionCursor(tl.Collection[0]))
	last := Cursor(pg.TransactionCursor(tl.Collection[len(tl.Collection)-1]))
	return NewListPageInfo(&first, &last, !tl.IsEnd, !tl.IsStart)
}

// Edges resolves list of transaction list edges for the linked transaction list.
func (tl *TransactionList) Edges() []*TransactionListEdge {
	// do we have any items? return empty list if not
	if tl.Collection == nil || len(tl.Collection) == 0 {
		return make([]*TransactionListEdge, 0)
	}

	// make the list
	edges := make([]*TransactionListEdge, len(tl.Collection))
	for i, t := range tl.Collection {
		// make the element
		edges[i] = &TransactionListEdge{
			Transaction: NewTransaction(t),
			Cursor:      Cursor(pg.TransactionCursor(t)),
		}
	}
	return edges
}
