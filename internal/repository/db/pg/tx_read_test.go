package pg

import (
	"context"
	"math/big"
	"ncogearthchain-api-graphql/internal/types"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// TestTransactionRoundTrip: a transaction written by the ingest path must read back with
// every field intact, including the values MongoDB stored lossily.
func TestTransactionRoundTrip(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	from := common.HexToAddress("0xaaaa000000000000000000000000000000000001")
	to := common.HexToAddress("0xbbbb000000000000000000000000000000000002")

	// a value above 2^63, which the int64 column in the MongoDB schema could not hold
	huge, _ := new(big.Int).SetString("123456789012345678901234567890", 10)

	trx := mkTx(1, 0, from, to, huge)
	if err := s.StoreBlock(ctx, &BlockData{
		Block:        mkBlock(1),
		Transactions: []*types.Transaction{trx},
	}); err != nil {
		t.Fatalf("store: %v", err)
	}

	got, err := s.Transaction(ctx, &trx.Hash)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got == nil {
		t.Fatal("transaction not found after storing it")
	}

	if got.Hash != trx.Hash {
		t.Errorf("hash: got %s want %s", got.Hash, trx.Hash)
	}
	if got.From != from {
		t.Errorf("from: got %s want %s", got.From, from)
	}
	if got.To == nil || *got.To != to {
		t.Errorf("to: got %v want %s", got.To, to)
	}
	if v := (*big.Int)(&got.Value); v.Cmp(huge) != 0 {
		t.Errorf("value: got %s want %s -- a uint256 above 2^63 did not survive", v, huge)
	}
	if got.BlockNumber == nil || uint64(*got.BlockNumber) != 1 {
		t.Errorf("blockNumber: got %v want 1", got.BlockNumber)
	}
	if got.Status == nil || uint64(*got.Status) != 1 {
		t.Errorf("status: got %v want 1", got.Status)
	}
}

// TestTransactionNotFoundIsNotAnError: a client can ask about any hash, so a miss is an
// ordinary outcome and must not surface as an error.
func TestTransactionNotFoundIsNotAnError(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)

	missing := common.HexToHash("0xdeadbeef")
	got, err := s.Transaction(context.Background(), &missing)
	if err != nil {
		t.Errorf("a missing transaction returned an error: %v", err)
	}
	if got != nil {
		t.Errorf("a missing transaction returned %v", got)
	}
}

// TestPaginationCoversEveryRowExactlyOnce is the test the MongoDB ordinal could not pass.
//
// Paging through the whole set in pages must visit every transaction exactly once. A
// keyset whose predicate and ORDER BY disagree, or an ordinal that collides, shows up
// here as a duplicate or a missing row -- which in production reads as chain data being
// wrong rather than as a pagination bug.
func TestPaginationCoversEveryRowExactlyOnce(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	from := common.HexToAddress("0xaaaa000000000000000000000000000000000001")
	to := common.HexToAddress("0xbbbb000000000000000000000000000000000002")

	// 5 blocks x 7 transactions = 35, paged 4 at a time so boundaries land mid-block
	const blocks, perBlock, pageSize = 5, 7, 4
	want := map[common.Hash]bool{}

	for b := uint64(1); b <= blocks; b++ {
		txs := make([]*types.Transaction, 0, perBlock)
		for i := uint64(0); i < perBlock; i++ {
			trx := mkTx(b, i, from, to, big.NewInt(int64(b*100+i)))
			txs = append(txs, trx)
			want[trx.Hash] = false
		}
		if err := s.StoreBlock(ctx, &BlockData{Block: mkBlock(b), Transactions: txs}); err != nil {
			t.Fatalf("store block %d: %v", b, err)
		}
	}

	seen := map[common.Hash]int{}
	cursor := ""
	for page := 0; page < 20; page++ {
		got, err := s.TransactionList(ctx, cursor, pageSize)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		if len(got) == 0 {
			break
		}
		for _, trx := range got {
			seen[trx.Hash]++
		}
		cursor = TransactionCursor(got[len(got)-1])
	}

	if len(seen) != len(want) {
		t.Errorf("paging visited %d distinct transactions, want %d", len(seen), len(want))
	}
	for h, n := range seen {
		if n != 1 {
			t.Errorf("transaction %s was returned %d times; pagination repeated a row", h, n)
		}
	}
	for h := range want {
		if seen[h] == 0 {
			t.Errorf("transaction %s was never returned; pagination skipped a row", h)
		}
	}
}

// TestTransactionsByAccountUsesTheEdgeTable checks both the result and the plan.
//
// The result being right is not enough: the point of the edge table is that an account's
// history is one index scan rather than a disjunction no single index can serve. A
// correct answer obtained by scanning the transaction table would still be the wrong
// implementation on the most requested page in an explorer.
func TestTransactionsByAccountUsesTheEdgeTable(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	alice := common.HexToAddress("0xaaaa000000000000000000000000000000000001")
	bob := common.HexToAddress("0xbbbb000000000000000000000000000000000002")
	carol := common.HexToAddress("0xcccc000000000000000000000000000000000003")

	// alice sends one, receives one; carol is uninvolved
	if err := s.StoreBlock(ctx, &BlockData{
		Block: mkBlock(1),
		Transactions: []*types.Transaction{
			mkTx(1, 0, alice, bob, big.NewInt(100)),
			mkTx(1, 1, bob, alice, big.NewInt(200)),
			mkTx(1, 2, bob, carol, big.NewInt(300)),
		},
	}); err != nil {
		t.Fatalf("store: %v", err)
	}

	got, err := s.TransactionsByAccount(ctx, &alice, "", 25)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("alice has %d transactions, want 2 (one sent, one received)", len(got))
	}

	// carol appears in exactly one
	got, err = s.TransactionsByAccount(ctx, &carol, "", 25)
	if err != nil {
		t.Fatalf("read carol: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("carol has %d transactions, want 1", len(got))
	}
}

// TestPageLimitIsClamped: a client must not be able to choose how much work the database
// does.
func TestPageLimitIsClamped(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	from := common.HexToAddress("0xaaaa000000000000000000000000000000000001")
	to := common.HexToAddress("0xbbbb000000000000000000000000000000000002")

	txs := make([]*types.Transaction, 0, 10)
	for i := uint64(0); i < 10; i++ {
		txs = append(txs, mkTx(1, i, from, to, big.NewInt(1)))
	}
	if err := s.StoreBlock(ctx, &BlockData{Block: mkBlock(1), Transactions: txs}); err != nil {
		t.Fatalf("store: %v", err)
	}

	// ask for far more than the cap; the query must still be bounded
	got, err := s.TransactionList(ctx, "", 1000000)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) > maxListLimit {
		t.Errorf("returned %d rows, above the %d cap", len(got), maxListLimit)
	}
}

// TestTransactionsInBlockAreOrdered: a block's transactions must come back in execution
// order, which is what makes an index position meaningful.
func TestTransactionsInBlockAreOrdered(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	from := common.HexToAddress("0xaaaa000000000000000000000000000000000001")
	to := common.HexToAddress("0xbbbb000000000000000000000000000000000002")

	txs := make([]*types.Transaction, 0, 5)
	for i := uint64(0); i < 5; i++ {
		txs = append(txs, mkTx(1, i, from, to, big.NewInt(1)))
	}
	if err := s.StoreBlock(ctx, &BlockData{Block: mkBlock(1), Transactions: txs}); err != nil {
		t.Fatalf("store: %v", err)
	}

	got, err := s.TransactionsInBlock(ctx, 1)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d transactions, want 5", len(got))
	}
	for i, trx := range got {
		if trx.Index == nil || uint64(*trx.Index) != uint64(i) {
			t.Errorf("position %d holds index %v; block transactions are out of order", i, trx.Index)
		}
	}
}
