package resolvers

import (
	"strconv"
	"testing"

	"ncogearthchain-api-graphql/internal/repository/db/pg"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// These lock in the pagination-cursor fix. Every list resolver must emit the OPAQUE keyset cursor
// the PostgreSQL store can DecodeCursor -- not the legacy Mongo cursor (tx hash / claim-tx hash /
// Uid decimal) that made every page after the first fail with "malformed cursor". They are pure
// unit tests: Edges()/PageInfo() operate on an already-loaded collection, so no repository is
// needed, and the store-level round-trip helpers (pg.*Cursor) are the exact thing the resolver
// must now agree with.

func TestTransactionListEmitsKeysetCursor(t *testing.T) {
	bn := hexutil.Uint64(9)
	idx := hexutil.Uint64(4)
	tx := &types.Transaction{
		Hash:        common.HexToHash("0x1234000000000000000000000000000000000000000000000000000000005678"),
		BlockNumber: &bn,
		Index:       &idx,
	}
	tl := NewTransactionList(&types.TransactionList{Collection: []*types.Transaction{tx}})

	want := pg.TransactionCursor(tx)
	if want == "" {
		t.Fatal("TransactionCursor produced an empty cursor for a positioned transaction")
	}

	edges := tl.Edges()
	if len(edges) != 1 {
		t.Fatalf("want 1 edge, got %d", len(edges))
	}
	if got := string(edges[0].Cursor); got != want {
		t.Errorf("edge cursor = %q, want the keyset cursor %q", got, want)
	}
	if string(edges[0].Cursor) == tx.Hash.String() {
		t.Error("edge cursor is still the tx hash -- the store cannot decode it")
	}

	pi, err := tl.PageInfo()
	if err != nil {
		t.Fatalf("PageInfo: %v", err)
	}
	if pi.First == nil || string(*pi.First) != want || pi.Last == nil || string(*pi.Last) != want {
		t.Errorf("PageInfo cursors = (%v, %v), want both %q", pi.First, pi.Last, want)
	}
}

func TestRewardClaimListEmitsKeysetCursor(t *testing.T) {
	claim := &types.RewardClaim{
		ClaimTrx:    common.HexToHash("0xabcd0000000000000000000000000000000000000000000000000000000000ef"),
		BlockNumber: 12,
		LogIndex:    2,
	}
	rl := NewRewardClaimList(&types.RewardClaimsList{Collection: []*types.RewardClaim{claim}})

	want := pg.RewardClaimCursor(claim)
	if want == "" {
		t.Fatal("RewardClaimCursor produced an empty cursor")
	}

	edges := rl.Edges()
	if got := string(edges[0].Cursor()); got != want {
		t.Errorf("edge cursor = %q, want %q", got, want)
	}
	if string(edges[0].Cursor()) == claim.ClaimTrx.String() {
		t.Error("edge cursor is still the claim tx hash")
	}

	pi, err := rl.PageInfo()
	if err != nil {
		t.Fatalf("PageInfo: %v", err)
	}
	if pi.First == nil || string(*pi.First) != want {
		t.Errorf("PageInfo start cursor = %v, want %q", pi.First, want)
	}
}

func TestContractListEmitsKeysetCursor(t *testing.T) {
	c := &types.Contract{
		TransactionHash: common.HexToHash("0x9999000000000000000000000000000000000000000000000000000000001111"),
		TimeStamp:       hexutil.Uint64(1_700_000_000),
		// scanContract sets this from the row's (block_number, tx_index, deploy_seq).
		Cursor: pg.ContractCursor(7, 1, 0),
	}
	cl := NewContractList(&types.ContractList{Collection: []*types.Contract{c}})

	want := c.Cursor

	edges := cl.Edges()
	if got := string(edges[0].Cursor); got != want {
		t.Errorf("edge cursor = %q, want the keyset cursor %q", got, want)
	}
	if string(edges[0].Cursor) == strconv.FormatUint(c.Uid(), 10) {
		t.Error("edge cursor is still the legacy Uid() decimal")
	}

	pi, err := cl.PageInfo()
	if err != nil {
		t.Fatalf("PageInfo: %v", err)
	}
	// The old bug: PageInfo start/end cursors were always the literal "0" because
	// buildContractList never populated First/Last. Assert they now carry the row cursor.
	if pi.First == nil || string(*pi.First) != want {
		t.Errorf("PageInfo start cursor = %v, want %q", pi.First, want)
	}
	if pi.First != nil && string(*pi.First) == "0" {
		t.Error(`PageInfo start cursor is still the constant "0"`)
	}
}
