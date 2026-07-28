package pg

import (
	"context"
	"math/big"
	"ncogearthchain-api-graphql/internal/types"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// seedBlocks stores blocks 1..n, each carrying `txs` transactions.
func seedBlocks(t *testing.T, s *Store, n uint64, txs int) {
	t.Helper()
	ctx := context.Background()

	alice := common.HexToAddress("0xaaaa000000000000000000000000000000000001")
	bob := common.HexToAddress("0xbbbb000000000000000000000000000000000002")

	for i := uint64(1); i <= n; i++ {
		list := make([]*types.Transaction, 0, txs)
		for j := 0; j < txs; j++ {
			list = append(list, mkTx(i, uint64(j), alice, bob, big.NewInt(int64(j+1))))
		}
		if err := s.StoreBlock(ctx, &BlockData{Block: mkBlock(i), Transactions: list}); err != nil {
			t.Fatalf("store block %d: %v", i, err)
		}
	}
}

func numbers(blocks []*types.Block) []uint64 {
	out := make([]uint64, len(blocks))
	for i, b := range blocks {
		out[i] = uint64(b.Number)
	}
	return out
}

func sameNumbers(got []*types.Block, want ...uint64) bool {
	g := numbers(got)
	if len(g) != len(want) {
		return false
	}
	for i := range g {
		if g[i] != want[i] {
			return false
		}
	}
	return true
}

// TestBlockListPagesLikeTheNodeWalk pins the semantics the removed per-block RPC walk had.
//
// These flags drive hasNextPage/hasPreviousPage on the block list, so getting them wrong is
// visible to every paging client -- and the cursor being EXCLUSIVE is what stops a page from
// repeating the block the previous page ended on.
func TestBlockListPagesLikeTheNodeWalk(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	seedBlocks(t, s, 10, 0)
	ctx := context.Background()

	// Newest first, no cursor. The probe row means 11 blocks exist behind a 3-block page.
	got, err := s.BlockList(ctx, nil, 3)
	if err != nil {
		t.Fatalf("head page: %v", err)
	}
	if !sameNumbers(got, 10, 9, 8, 7) {
		t.Errorf("head page = %v, want 10,9,8,7 (three requested plus the probe row)", numbers(got))
	}

	// Cursor is EXCLUSIVE: paging on from block 8 must not repeat it.
	from := uint64(8)
	got, err = s.BlockList(ctx, &from, 3)
	if err != nil {
		t.Fatalf("cursored page: %v", err)
	}
	if !sameNumbers(got, 7, 6, 5, 4) {
		t.Errorf("page after 8 = %v, want 7,6,5,4 -- the cursor block must be excluded", numbers(got))
	}

	// Negative count scans upward from the cursor, also exclusive.
	got, err = s.BlockList(ctx, &from, -3)
	if err != nil {
		t.Fatalf("upward page: %v", err)
	}
	if !sameNumbers(got, 9, 10) {
		t.Errorf("upward page from 8 = %v, want 9,10 (only two exist above)", numbers(got))
	}

	// At the end of the chain there is no probe row, which is how the caller learns it is
	// the last page rather than reporting "more" forever.
	last := uint64(3)
	got, err = s.BlockList(ctx, &last, 5)
	if err != nil {
		t.Fatalf("tail page: %v", err)
	}
	if !sameNumbers(got, 2, 1) {
		t.Errorf("tail page = %v, want 2,1 with no probe row", numbers(got))
	}
}

// TestBlockReadsCarryTheirTransactions is the guard for the defect that made pg.BlockList
// unusable while it sat unwired.
//
// Block.transactionCount, Block.txHashList and Block.txList are ALL derived from
// types.Block.Txs. A block read from the index without it does not merely lack its
// transactions -- it reports, with no error, that it has none. That is worse than the N+1
// RPC walk it replaces, because it is silently wrong rather than merely slow.
func TestBlockReadsCarryTheirTransactions(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	seedBlocks(t, s, 4, 3)
	ctx := context.Background()

	t.Run("list", func(t *testing.T) {
		got, err := s.BlockList(ctx, nil, 4)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, b := range got {
			if len(b.Txs) != 3 {
				t.Errorf("block %d carries %d transaction hashes, want 3", uint64(b.Number), len(b.Txs))
			}
		}
	})

	t.Run("by number", func(t *testing.T) {
		b, err := s.Block(ctx, 2)
		if err != nil {
			t.Fatalf("by number: %v", err)
		}
		if b == nil || len(b.Txs) != 3 {
			t.Fatalf("block 2 carries %v transaction hashes, want 3", b)
		}
	})

	t.Run("by hash", func(t *testing.T) {
		b, err := s.BlockByHash(ctx, &mkBlock(2).Hash)
		if err != nil {
			t.Fatalf("by hash: %v", err)
		}
		if b == nil || len(b.Txs) != 3 {
			t.Fatalf("block by hash carries %v transaction hashes, want 3", b)
		}
	})

	t.Run("empty block reports an empty list, not null", func(t *testing.T) {
		cleanDB(t, s)
		seedBlocks(t, s, 1, 0)
		b, err := s.Block(ctx, 1)
		if err != nil {
			t.Fatalf("empty block: %v", err)
		}
		if b == nil || b.Txs == nil {
			t.Fatal("an empty block must carry an empty transaction list, not nil")
		}
		if len(b.Txs) != 0 {
			t.Errorf("empty block carries %d hashes, want 0", len(b.Txs))
		}
	})
}

// TestBlockTransactionsKeepBlockOrder: txHashList promises block position order, and one
// query serving a whole page must not let one block's hashes land on another.
func TestBlockTransactionsKeepBlockOrder(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	seedBlocks(t, s, 3, 4)
	ctx := context.Background()

	got, err := s.BlockList(ctx, nil, 3)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	for _, b := range got {
		want := make([]common.Hash, 0, 4)
		for j := uint64(0); j < 4; j++ {
			want = append(want, mkTx(uint64(b.Number), j, common.Address{}, common.Address{}, big.NewInt(1)).Hash)
		}
		if len(b.Txs) != len(want) {
			t.Fatalf("block %d has %d hashes, want %d", uint64(b.Number), len(b.Txs), len(want))
		}
		for i := range want {
			if *b.Txs[i] != want[i] {
				t.Errorf("block %d hash[%d] = %s, want %s (position order, and the right block's rows)",
					uint64(b.Number), i, b.Txs[i].String(), want[i].String())
			}
		}
	}
}
