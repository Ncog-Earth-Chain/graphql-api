package repository

import (
	"context"
	"ncogearthchain-api-graphql/internal/repository/db/pg"
	"ncogearthchain-api-graphql/internal/types"
)

// Adapters between the Repository interface, which the GraphQL layer shapes, and the
// PostgreSQL store, which the schema shapes.
//
// These exist because the two disagree in small ways that are each deliberate on their
// own side, and translating in one place beats bending either to match the other:
//
//   - the interface passes cursors as *string ("no cursor" as nil); the store takes a
//     string, since an empty cursor already means the first page
//   - the interface returns list wrappers carrying pagination state; the store returns
//     rows and an exact count, leaving presentation to the caller

// derefCursor converts an optional cursor into the store's plain-string form. A nil
// cursor and an empty cursor both mean "start at the beginning".
func derefCursor(c *string) string {
	if c == nil {
		return ""
	}
	return *c
}

// buildTransactionList wraps a page of transactions with the pagination state the
// GraphQL layer expects.
//
// TotalIsExact is true because the count comes from tx_account, keyed by address, which
// makes it an index-only scan. That is the concrete payoff of the edge table: MongoDB
// counted an $or over the whole transaction collection with a 500 ms budget, and on
// timeout reported the CHAIN's total as the account's.
func buildTransactionList(rows []*types.Transaction, total uint64, count int32, cursor string) *types.TransactionList {
	list := &types.TransactionList{
		Total:        total,
		TotalIsExact: true,
	}

	// The store fetches one row beyond the page so "a further page exists" can be answered
	// without a second query and WITHOUT the exactly-full-page ambiguity. Deriving the
	// direction-end flag from a short page reported hasNextPage=true whenever the remaining
	// rows were an exact multiple of the page size (the last full page claimed a next page
	// that is actually empty). Detect the extra row, then discard it. This mirrors the trick
	// every sibling list store (withdrawal/token_tx/epoch/...) already uses.
	limit := int(abs32(count))
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	list.Collection = rows

	if len(rows) == 0 {
		list.IsStart = true
		list.IsEnd = true
		return list
	}

	// `more` fixes the leading boundary (the direction of travel); whether a cursor was
	// supplied fixes the trailing one -- paging with no cursor starts at the natural edge
	// (forward -> the top, so IsStart; backward -> the bottom, so IsEnd). Setting only the
	// short-page flag left the trailing flag false, which the resolver mapped to
	// hasPreviousPage=true even on the very first page.
	atBoundary := cursor == ""
	if count >= 0 {
		list.IsEnd = !more
		list.IsStart = atBoundary
	} else {
		list.IsStart = !more
		list.IsEnd = atBoundary
	}
	return list
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	if v == 0 {
		return 25
	}
	return v
}

// compile-time assurance that the store satisfies what the adapters assume.
var _ = pg.TokenTxCriteria{}

// burnTotal adapts the store's BurnTotal to the cache's expected signature, which
// predates contexts.
func (p *proxy) burnTotal(ctx context.Context) (int64, error) {
	return p.pg.BurnTotal(ctx)
}

// buildContractList wraps a page of contracts with pagination state.
//
// The count is exact when filtered to verified contracts, because contract_verified_idx
// is PARTIAL on that predicate and so touches only verified rows; unfiltered it is an
// estimate, matching what the MongoDB path actually did.
func buildContractList(rows []*types.Contract, total uint64, count int32, cursor string) *types.ContractList {
	list := &types.ContractList{
		Total:        total,
		TotalIsExact: true,
	}

	// See buildTransactionList: the store fetches one extra row so `more` answers the leading
	// boundary without the exactly-full-page ambiguity; the presence of a cursor fixes the
	// trailing one. Discard the probe row before returning the page.
	limit := int(abs32(count))
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	list.Collection = rows

	if len(rows) == 0 {
		list.IsStart = true
		list.IsEnd = true
		return list
	}

	atBoundary := cursor == ""
	if count >= 0 {
		list.IsEnd = !more
		list.IsStart = atBoundary
	} else {
		list.IsStart = !more
		list.IsEnd = atBoundary
	}
	return list
}
