package pg

import (
	"context"
	"math/big"
	"testing"
	"time"

	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Burn writes must be reorg-safe. Each delivery carries a block's ENTIRE burn, so a second
// delivery for the same block is either a verbatim re-delivery or a reorg -- never a partial
// addition. StoreBurn therefore REPLACES the stored amount and moves the running total by
// (new - old): a re-delivery moves it by zero, a reorg reconciles it exactly.
//
// The bug this guards against: the earlier implementation ADDED every delivery and used the
// transaction-hash set to dedup, which could not tell a reorg (all-new hashes) from a
// brand-new burn -- so a reorged block stacked a second burn on top of the first and
// inflated the global total permanently.

// cleanBurn empties the burn tables and resets the running total. It runs before cleanDB so
// the burn rows referencing block(number) are gone before the block rows are deleted.
func cleanBurn(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	for _, q := range []string{
		"DELETE FROM burn_tx",
		"DELETE FROM burn",
		"UPDATE meta_counter SET value = 0 WHERE key = 'burn_total_wei'",
	} {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			t.Fatalf("clean burn: %v", err)
		}
	}
}

// mkHashes builds n deterministic, distinct transaction hashes seeded by tag.
func mkHashes(tag byte, n int) []common.Hash {
	out := make([]common.Hash, n)
	for i := 0; i < n; i++ {
		out[i][0] = tag
		out[i][31] = byte(i + 1)
	}
	return out
}

// mkBurn builds a block burn carrying the whole block's amount and its transaction hashes.
func mkBurn(block uint64, wei *big.Int, hashes []common.Hash) *types.NecBurn {
	return &types.NecBurn{
		BlockNumber:  hexutil.Uint64(block),
		BlkTimeStamp: time.Unix(1_700_000_000, 0).UTC(),
		Amount:       hexutil.Big(*wei),
		TxList:       hashes,
	}
}

// burnTotalWei reads the exact running total (not the BurnTotal read, which divides by the
// decimals correction) so the delta arithmetic can be asserted precisely.
func burnTotalWei(t *testing.T, s *Store) *big.Int {
	t.Helper()
	var text string
	if err := s.pool.QueryRow(context.Background(),
		`SELECT value::text FROM meta_counter WHERE key = 'burn_total_wei'`).Scan(&text); err != nil {
		t.Fatalf("read burn total counter: %v", err)
	}
	v, ok := new(big.Int).SetString(text, 10)
	if !ok {
		t.Fatalf("burn total counter is not an integer: %q", text)
	}
	return v
}

// blockBurn reads a block's stored amount and transaction count.
func blockBurn(t *testing.T, s *Store, block uint64) (*big.Int, int) {
	t.Helper()
	var amountText string
	var count int
	err := s.pool.QueryRow(context.Background(),
		`SELECT amount_wei::text, tx_count FROM burn WHERE block_number = $1`, int64(block)).
		Scan(&amountText, &count)
	if err != nil {
		t.Fatalf("read burn row for #%d: %v", block, err)
	}
	v, ok := new(big.Int).SetString(amountText, 10)
	if !ok {
		t.Fatalf("burn amount is not an integer: %q", amountText)
	}
	return v, count
}

// burnTxCount reports how many transaction hashes are recorded for a block.
func burnTxCount(t *testing.T, s *Store, block uint64) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM burn_tx WHERE block_number = $1`, int64(block)).Scan(&n); err != nil {
		t.Fatalf("count burn_tx for #%d: %v", block, err)
	}
	return n
}

// TestBurnIsReorgSafe walks a block's burn through a first delivery, a verbatim
// re-delivery, a reorg that raises the amount, and a reorg that lowers it, asserting the
// per-block row and the global running total after each.
func TestBurnIsReorgSafe(t *testing.T) {
	s := testStore(t)
	cleanBurn(t, s)
	cleanDB(t, s)
	ctx := context.Background()

	if err := s.StoreBlock(ctx, &BlockData{Block: mkBlock(1)}); err != nil {
		t.Fatalf("store block 1: %v", err)
	}

	first := big.NewInt(1_000_000_000_000) // 1e12 wei
	hashesA := mkHashes(0xA1, 2)

	// First delivery: the block's whole burn lands and the total picks it up.
	if err := s.StoreBurn(ctx, mkBurn(1, first, hashesA)); err != nil {
		t.Fatalf("first burn: %v", err)
	}
	if amt, cnt := blockBurn(t, s, 1); amt.Cmp(first) != 0 || cnt != 2 {
		t.Fatalf("after first burn: amount=%s count=%d, want %s and 2", amt, cnt, first)
	}
	if got := burnTotalWei(t, s); got.Cmp(first) != 0 {
		t.Fatalf("total after first burn = %s, want %s", got, first)
	}

	// Verbatim re-delivery (e.g. after a restart re-scans the block): nothing moves.
	if err := s.StoreBurn(ctx, mkBurn(1, first, hashesA)); err != nil {
		t.Fatalf("re-delivery: %v", err)
	}
	if amt, cnt := blockBurn(t, s, 1); amt.Cmp(first) != 0 || cnt != 2 {
		t.Fatalf("after re-delivery: amount=%s count=%d, want %s and 2", amt, cnt, first)
	}
	if got := burnTotalWei(t, s); got.Cmp(first) != 0 {
		t.Errorf("re-delivery double-counted: total = %s, want %s", got, first)
	}

	// Reorg that raises the burn and replaces the transaction set entirely. The total must
	// reflect the NEW amount, not first+higher.
	higher := big.NewInt(3_000_000_000_000)
	hashesB := mkHashes(0xB2, 3)
	if err := s.StoreBurn(ctx, mkBurn(1, higher, hashesB)); err != nil {
		t.Fatalf("reorg (raise): %v", err)
	}
	if amt, cnt := blockBurn(t, s, 1); amt.Cmp(higher) != 0 || cnt != 3 {
		t.Fatalf("after reorg-raise: amount=%s count=%d, want %s and 3", amt, cnt, higher)
	}
	if n := burnTxCount(t, s, 1); n != 3 {
		t.Errorf("reorg did not replace the transaction set: %d burn_tx rows, want 3", n)
	}
	if got := burnTotalWei(t, s); got.Cmp(higher) != 0 {
		t.Errorf("reorg-raise total = %s, want %s (stacked instead of replaced?)", got, higher)
	}

	// Reorg that lowers the burn: the delta is negative and the total must come back down.
	lower := big.NewInt(500_000_000_000)
	hashesC := mkHashes(0xC3, 1)
	if err := s.StoreBurn(ctx, mkBurn(1, lower, hashesC)); err != nil {
		t.Fatalf("reorg (lower): %v", err)
	}
	if amt, cnt := blockBurn(t, s, 1); amt.Cmp(lower) != 0 || cnt != 1 {
		t.Fatalf("after reorg-lower: amount=%s count=%d, want %s and 1", amt, cnt, lower)
	}
	if got := burnTotalWei(t, s); got.Cmp(lower) != 0 {
		t.Errorf("reorg-lower total = %s, want %s", got, lower)
	}
}

// TestBurnTotalsAcrossBlocks confirms the running total is the sum of independent blocks and
// that a reorg of one block does not disturb another.
func TestBurnTotalsAcrossBlocks(t *testing.T) {
	s := testStore(t)
	cleanBurn(t, s)
	cleanDB(t, s)
	ctx := context.Background()

	for _, n := range []uint64{1, 2} {
		if err := s.StoreBlock(ctx, &BlockData{Block: mkBlock(n)}); err != nil {
			t.Fatalf("store block %d: %v", n, err)
		}
	}

	b1 := big.NewInt(1_000_000_000_000)
	b2 := big.NewInt(2_000_000_000_000)
	if err := s.StoreBurn(ctx, mkBurn(1, b1, mkHashes(0x11, 1))); err != nil {
		t.Fatalf("burn block 1: %v", err)
	}
	if err := s.StoreBurn(ctx, mkBurn(2, b2, mkHashes(0x22, 1))); err != nil {
		t.Fatalf("burn block 2: %v", err)
	}

	want := new(big.Int).Add(b1, b2)
	if got := burnTotalWei(t, s); got.Cmp(want) != 0 {
		t.Fatalf("total across two blocks = %s, want %s", got, want)
	}

	// Reorg block 1 only; block 2 must be untouched and the total must move by block 1's
	// delta alone.
	b1new := big.NewInt(1_500_000_000_000)
	if err := s.StoreBurn(ctx, mkBurn(1, b1new, mkHashes(0x13, 1))); err != nil {
		t.Fatalf("reorg block 1: %v", err)
	}
	if amt, _ := blockBurn(t, s, 2); amt.Cmp(b2) != 0 {
		t.Errorf("block 2 burn changed to %s during a block 1 reorg, want %s", amt, b2)
	}
	want = new(big.Int).Add(b1new, b2)
	if got := burnTotalWei(t, s); got.Cmp(want) != 0 {
		t.Errorf("total after block 1 reorg = %s, want %s", got, want)
	}
}
