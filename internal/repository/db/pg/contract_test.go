package pg

import (
	"context"
	"math/big"
	"ncogearthchain-api-graphql/internal/types"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// seedContractTx stores a block with one transaction, so a contract has a deploy
// transaction to derive its position from.
func seedContractTx(t *testing.T, s *Store, block, idx uint64) *types.Transaction {
	t.Helper()
	ctx := context.Background()

	from := common.HexToAddress("0xaaaa000000000000000000000000000000000001")
	to := common.HexToAddress("0xbbbb000000000000000000000000000000000002")
	trx := mkTx(block, idx, from, to, big.NewInt(0))

	if err := s.StoreBlock(ctx, &BlockData{
		Block:        mkBlock(block),
		Transactions: []*types.Transaction{trx},
	}); err != nil {
		t.Fatalf("seed block: %v", err)
	}
	return trx
}

// TestContractPositionComesFromItsDeployTransaction: a contract's chain position is the
// position of the transaction that created it. Deriving it means the two can never
// disagree, and it replaces a MongoDB ordinal that was a wall-clock timestamp with a
// 24-bit hash tiebreaker -- an ordering that disagreed with every other list in the API.
func TestContractPositionComesFromItsDeployTransaction(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	trx := seedContractTx(t, s, 7, 3)
	addr := common.HexToAddress("0xcccc000000000000000000000000000000000010")

	if err := s.AddContract(ctx, &types.Contract{
		Address:         addr,
		Type:            types.AccountTypeContract,
		TransactionHash: trx.Hash,
		TimeStamp:       hexutil.Uint64(1700000000),
	}); err != nil {
		t.Fatalf("add contract: %v", err)
	}

	var blockNumber int64
	var txIndex int32
	if err := s.pool.QueryRow(ctx,
		`SELECT block_number, tx_index FROM contract WHERE address = $1`, AddrVal(addr)).
		Scan(&blockNumber, &txIndex); err != nil {
		t.Fatalf("read position: %v", err)
	}
	if blockNumber != 7 || txIndex != 3 {
		t.Errorf("contract position = (%d, %d), want (7, 3) from its deploy transaction",
			blockNumber, txIndex)
	}
}

// TestAddContractRefusesAnUnstoredDeployTx: writing nothing silently would leave a
// contract the explorer has seen but cannot list.
func TestAddContractRefusesAnUnstoredDeployTx(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)

	err := s.AddContract(context.Background(), &types.Contract{
		Address:         common.HexToAddress("0xcccc000000000000000000000000000000000011"),
		Type:            types.AccountTypeContract,
		TransactionHash: common.HexToHash("0xdeadbeef"),
		TimeStamp:       hexutil.Uint64(1700000000),
	})
	if err == nil {
		t.Error("AddContract accepted a contract whose deploy transaction is not stored")
	}
}

// TestVerificationSurvivesReDeployObservation is the point of splitting the tables.
//
// Verification is user-submitted and NOT rebuildable from the chain. A re-scan that
// re-observes the deployment must never disturb it.
func TestVerificationSurvivesReDeployObservation(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	trx := seedContractTx(t, s, 1, 0)
	addr := common.HexToAddress("0xcccc000000000000000000000000000000000012")

	base := &types.Contract{
		Address:         addr,
		Type:            types.AccountTypeContract,
		TransactionHash: trx.Hash,
		TimeStamp:       hexutil.Uint64(1700000000),
	}
	if err := s.AddContract(ctx, base); err != nil {
		t.Fatalf("add: %v", err)
	}

	validated := hexutil.Uint64(1700000123)
	if err := s.UpdateContractValidation(ctx, &types.Contract{
		Address:    addr,
		Name:       "MyToken",
		SourceCode: "contract MyToken {}",
		Abi:        `[{"type":"function","name":"transfer"}]`,
		Validated:  &validated,
	}); err != nil {
		t.Fatalf("validate: %v", err)
	}

	// a re-scan re-observes the deployment
	if err := s.AddContract(ctx, base); err != nil {
		t.Fatalf("re-add: %v", err)
	}

	got, err := s.Contract(ctx, &addr)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got == nil {
		t.Fatal("contract vanished")
	}
	if got.SourceCode != "contract MyToken {}" {
		t.Errorf("source code lost on re-scan: %q", got.SourceCode)
	}
	if got.Name != "MyToken" {
		t.Errorf("verification name lost on re-scan: %q", got.Name)
	}
	if got.Validated == nil {
		t.Error("validation timestamp lost on re-scan")
	}
}

// TestUnverifiedContractReadsWithoutPanicking pins the LEFT join: every verification
// column is NULL for an unverified contract, and the scanner must not dereference them.
func TestUnverifiedContractReadsWithoutPanicking(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	trx := seedContractTx(t, s, 1, 0)
	addr := common.HexToAddress("0xcccc000000000000000000000000000000000013")

	if err := s.AddContract(ctx, &types.Contract{
		Address:         addr,
		Type:            types.AccountTypeContract,
		TransactionHash: trx.Hash,
		TimeStamp:       hexutil.Uint64(1700000000),
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	got, err := s.Contract(ctx, &addr)
	if err != nil {
		t.Fatalf("load an unverified contract: %v", err)
	}
	if got == nil {
		t.Fatal("unverified contract not found")
	}
	if got.Validated != nil {
		t.Errorf("unverified contract reports a validation timestamp: %v", got.Validated)
	}
	if got.SourceCode != "" {
		t.Errorf("unverified contract reports source code: %q", got.SourceCode)
	}
}

// TestLastKnownBlockIsTheContiguousHead: resuming from the height would step over a gap
// forever, which is the MongoDB failure this replaces.
func TestLastKnownBlockIsTheContiguousHead(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	for _, n := range []uint64{1, 2, 3, 5} { // 4 missing
		if err := s.StoreBlock(ctx, &BlockData{Block: mkBlock(n)}); err != nil {
			t.Fatalf("store %d: %v", n, err)
		}
	}

	height, err := s.BlockHeight(ctx)
	if err != nil {
		t.Fatalf("height: %v", err)
	}
	resume, err := s.LastKnownBlock(ctx)
	if err != nil {
		t.Fatalf("resume point: %v", err)
	}

	if height != 5 {
		t.Errorf("height = %d, want 5", height)
	}
	if resume != 3 {
		t.Errorf("resume point = %d, want 3 -- resuming from the height would skip block 4 forever", resume)
	}
}
