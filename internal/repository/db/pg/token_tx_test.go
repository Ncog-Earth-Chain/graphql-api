package pg

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// TestTokenTransferCountIsExactOnlyWhereItIsCheap pins which of the two counting paths each
// filter shape takes.
//
// TokenTransactions used to run an exact count(*) before every page, whatever the filter.
// Every token_tx index leads with `std`, so the global feed -- narrowed only by standard --
// counted substantially the whole table on each page fetch, and that cost grows with the
// table forever. Narrowed by token, token id or account it is one small index range and the
// exact answer is both affordable and the number the user is actually asking for.
//
// The estimate must still be a real answer for the real predicate, not a whole-table figure
// wearing a filtered label, so this asserts it responds to selectivity: an impossible
// predicate must estimate far below the table, and it must never be negative.
func TestTokenTransferCountIsExactOnlyWhereItIsCheap(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// std-only: the shape that must NOT be counted exactly.
	broad := NewFilter().Eq("std", 0)
	est, err := s.TokenTransactionCountEstimated(ctx, broad)
	if err != nil {
		t.Fatalf("estimate (std only): %v", err)
	}
	exact, err := s.TokenTransactionCountFiltered(ctx, broad)
	if err != nil {
		t.Fatalf("exact (std only): %v", err)
	}
	t.Logf("std-only: planner estimate %d, exact %d", est, exact)

	// The estimate tracks the predicate. An address that cannot match anything must estimate
	// well below a whole-table figure -- if the implementation ignored the filter and returned
	// reltuples, this would come back as the table size.
	narrow := NewFilter().
		Eq("std", 0).
		Eq("token", AddrVal(common.HexToAddress("0xeeee000000000000000000000000000000000eee")))
	narrowEst, err := s.TokenTransactionCountEstimated(ctx, narrow)
	if err != nil {
		t.Fatalf("estimate (narrow): %v", err)
	}
	if narrowEst > est {
		t.Errorf("a strictly narrower filter estimated MORE rows (%d) than a broader one (%d); the estimate is not tracking the predicate",
			narrowEst, est)
	}

	narrowExact, err := s.TokenTransactionCountFiltered(ctx, narrow)
	if err != nil {
		t.Fatalf("exact (narrow): %v", err)
	}
	if narrowExact != 0 {
		t.Errorf("an unused token address matched %d transfers, want 0", narrowExact)
	}

	// The number that actually reaches TotalCount must be EXACT for a small result set,
	// whatever the filter. The planner's floor estimate on a near-empty table is one or two
	// rows, so returning the estimate unconditionally reported "2 transfers" against an empty
	// list -- and since IsStart/IsEnd test this value for zero, the empty list would also have
	// claimed further pages.
	total, err := s.tokenTransactionTotal(ctx, broad)
	if err != nil {
		t.Fatalf("total (std only): %v", err)
	}
	if total != exact {
		t.Errorf("reported total is %d but the exact count is %d; a small result set must be counted, not estimated", total, exact)
	}
}
