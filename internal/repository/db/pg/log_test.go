package pg

import (
	"context"
	"math/big"
	"ncogearthchain-api-graphql/internal/types"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	retypes "github.com/ethereum/go-ethereum/core/types"
)

// erc20Transfer is the canonical ERC-20 Transfer event signature.
var erc20Transfer = common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")

// seedLogs stores a block whose transaction emits several logs from two contracts.
func seedLogs(t *testing.T, s *Store) (tokenA, tokenB common.Address, alice common.Hash) {
	t.Helper()
	ctx := context.Background()

	tokenA = common.HexToAddress("0xaaaa000000000000000000000000000000000aaa")
	tokenB = common.HexToAddress("0xbbbb000000000000000000000000000000000bbb")
	alice = common.HexToHash("0x000000000000000000000000aaaa000000000000000000000000000000000001")
	bob := common.HexToHash("0x000000000000000000000000bbbb000000000000000000000000000000000002")

	from := common.HexToAddress("0xaaaa000000000000000000000000000000000001")
	to := common.HexToAddress("0xbbbb000000000000000000000000000000000002")

	trx := mkTx(1, 0, from, to, big.NewInt(0))
	trx.Logs = []retypes.Log{
		{Address: tokenA, Topics: []common.Hash{erc20Transfer, alice, bob}, Data: []byte{0x01}, Index: 0},
		{Address: tokenA, Topics: []common.Hash{erc20Transfer, bob, alice}, Data: []byte{0x02}, Index: 1},
		{Address: tokenB, Topics: []common.Hash{erc20Transfer, alice, bob}, Data: []byte{0x03}, Index: 2},
		// an anonymous event: no signature topic at all
		{Address: tokenB, Topics: []common.Hash{}, Data: []byte{0x04}, Index: 3},
	}

	if err := s.StoreBlock(ctx, &BlockData{
		Block:        mkBlock(1),
		Transactions: []*types.Transaction{trx},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return tokenA, tokenB, alice
}

// TestLogsByAddressAndTopic is the query MongoDB could not answer at all.
func TestLogsByAddressAndTopic(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	tokenA, tokenB, _ := seedLogs(t, s)

	// every log from one contract
	got, err := s.Logs(ctx, LogCriteria{Address: &tokenA}, "", 25)
	if err != nil {
		t.Fatalf("by address: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("tokenA emitted %d logs, want 2", len(got))
	}

	// narrowed to an event signature
	got, err = s.Logs(ctx, LogCriteria{Address: &tokenB, Topic0: &erc20Transfer}, "", 25)
	if err != nil {
		t.Fatalf("by address+topic0: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("tokenB Transfer logs = %d, want 1 (the anonymous event must not match)", len(got))
	}
}

// TestLogsByIndexedParameter covers the "every transfer involving this account" shape.
func TestLogsByIndexedParameter(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	_, _, alice := seedLogs(t, s)

	// alice as the sender (topic1)
	got, err := s.Logs(ctx, LogCriteria{Topic1: &alice}, "", 25)
	if err != nil {
		t.Fatalf("by topic1: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("alice sent %d transfers, want 2", len(got))
	}
}

// TestAnonymousEventKeepsNoTopics pins the topic_count decision.
//
// A NULL topic and an ABSENT topic are different: an anonymous event has no signature
// topic at all. Padding the slice back to four would invent topics that were never
// emitted, and a client filtering on topics[0] would see one that does not exist.
func TestAnonymousEventKeepsNoTopics(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	_, tokenB, _ := seedLogs(t, s)

	got, err := s.Logs(ctx, LogCriteria{Address: &tokenB}, "", 25)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var anonymous int
	for _, l := range got {
		if len(l.Topics) == 0 {
			anonymous++
		}
	}
	if anonymous != 1 {
		t.Errorf("found %d topic-less logs, want exactly 1 (the anonymous event)", anonymous)
	}

	for _, l := range got {
		if len(l.Topics) > 0 && l.Topics[0] == (common.Hash{}) {
			t.Error("a log came back with a zero-hash topic; the slice was padded rather than truncated")
		}
	}
}

// TestLogsPaginateExactlyOnce: the same integrity property as transactions.
func TestLogsPaginateExactlyOnce(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	seedLogs(t, s)

	seen := map[string]int{}
	cursor := ""
	for page := 0; page < 10; page++ {
		got, err := s.Logs(ctx, LogCriteria{}, cursor, 2)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		if len(got) == 0 {
			break
		}
		for _, l := range got {
			seen[l.TxHash.String()+":"+string(rune(l.Index))]++
		}
		last := got[len(got)-1]
		cursor = LogCursor(uint64(last.BlockNumber), uint(last.Index))
	}

	if len(seen) != 4 {
		t.Errorf("paging visited %d distinct logs, want 4", len(seen))
	}
	for k, n := range seen {
		if n != 1 {
			t.Errorf("log %s returned %d times", k, n)
		}
	}
}
