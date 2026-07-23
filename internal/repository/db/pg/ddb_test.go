package pg

import (
	"context"
	"encoding/json"
	"math/big"
	"ncogearthchain-api-graphql/internal/types"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// mkDdbTx builds a DDB commit transaction carrying a decoded dual-consensus record.
func mkDdbTx(block, idx uint64, contract *common.Address, priorState []byte) *types.Transaction {
	from := common.HexToAddress("0xaaaa000000000000000000000000000000000001")
	to := common.HexToAddress("0x0000000000000000000000000000000000000DDB")
	trx := mkTx(block, idx, from, to, big.NewInt(0))

	op := map[string]any{
		"type":          0, // CreateSchema
		"schema_name":   "users",
		"contract_name": "UserRegistry",
		"version":       "1",
		"author":        from.Hex(),
	}
	payload, _ := json.Marshal(op)

	trx.DDB = &types.DdbCommit{
		Operation:       payload,
		OpType:          0,
		SchemaName:      "users",
		ContractAddress: contract,
		ContractName:    "UserRegistry",
		Version:         "1",
		Author:          &from,
		RequestID:       common.HexToHash("0x1111"),
		Requester:       from,
		OperationHash:   common.HexToHash("0x2222"),
		DataHash:        common.HexToHash("0x3333"),
		StateHash:       common.HexToHash("0x4444"),
		Epoch:           hexutil.Uint64(7),
		// nil for a contract's FIRST operation -- the case the original design would have
		// aborted the block on
		PriorPostStateHash: priorState,
		PostStateHash:      []byte{0xaa, 0xbb},
		ValidatorSet: []common.Address{
			common.HexToAddress("0xv1"), common.HexToAddress("0xv2"), common.HexToAddress("0xv3"),
		},
		Signatures: 3,
	}
	return trx
}

// TestDdbGenesisOperationStores is the regression test for the defect that would have
// broken every data contract at the moment it was created.
//
// A contract's FIRST operation has NO prior state hash -- the field is variable-length
// with omitempty and the node itself guards on len()==0. A hash32 NOT NULL column would
// fail its octet_length = 32 check and abort the whole block, so the very act of creating
// a data contract would have wedged the indexer at that height.
func TestDdbGenesisOperationStores(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	contract := common.HexToAddress("0xcccc000000000000000000000000000000000abc")

	// nil prior state: the genesis case
	if err := s.StoreBlock(ctx, &BlockData{
		Block:        mkBlock(1),
		Transactions: []*types.Transaction{mkDdbTx(1, 0, &contract, nil)},
	}); err != nil {
		t.Fatalf("a contract's first DDB operation failed to store: %v", err)
	}

	var priorNull bool
	var epoch int64
	if err := s.pool.QueryRow(ctx, `
		SELECT prior_post_state_hash IS NULL, epoch FROM ddb_endorsement
		WHERE block_number = 1 AND tx_index = 0`).Scan(&priorNull, &epoch); err != nil {
		t.Fatalf("read endorsement: %v", err)
	}
	if !priorNull {
		t.Error("the genesis operation's prior state hash should be NULL, not empty bytes")
	}
	if epoch != 7 {
		t.Errorf("epoch = %d, want 7 -- the field whose absence broke every DDB decode", epoch)
	}
}

// TestDdbOperationIsQueryable covers the history the node cannot serve at all: it exposes
// no RPC for DDB operations, so this data exists only because ingest captured it.
func TestDdbOperationIsQueryable(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	contract := common.HexToAddress("0xcccc000000000000000000000000000000000abc")

	for _, n := range []uint64{1, 2, 3} {
		if err := s.StoreBlock(ctx, &BlockData{
			Block:        mkBlock(n),
			Transactions: []*types.Transaction{mkDdbTx(n, 0, &contract, []byte{0x01})},
		}); err != nil {
			t.Fatalf("store block %d: %v", n, err)
		}
	}

	var ops int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM ddb_operation WHERE contract_addr = $1`, AddrVal(contract)).
		Scan(&ops); err != nil {
		t.Fatalf("count: %v", err)
	}
	if ops != 3 {
		t.Errorf("contract has %d operations, want 3", ops)
	}

	// the folded contract view
	var opCount int64
	var name, dbName, version *string
	if err := s.pool.QueryRow(ctx,
		`SELECT op_count, contract_name, db_name, latest_version FROM ddb_contract WHERE contract_addr = $1`,
		AddrVal(contract)).Scan(&opCount, &name, &dbName, &version); err != nil {
		t.Fatalf("read folded contract: %v", err)
	}
	if opCount != 3 {
		t.Errorf("op_count = %d, want 3", opCount)
	}
	if name == nil || *name != "UserRegistry" {
		t.Errorf("contract_name = %v, want UserRegistry", name)
	}
	// db_name is DERIVED, not carried on the wire: lower(name) + '_' + last 6 hex of address
	if dbName == nil || *dbName != "userregistry_000abc" {
		t.Errorf("db_name = %v, want userregistry_000abc", dbName)
	}
	if version == nil || *version != "1" {
		t.Errorf("version = %v, want \"1\" (a STRING on the wire, not an integer)", version)
	}
}

// TestDdbRowsPurgedOnReorg: a reorged-away commit must take its operation and endorsement
// with it, or the explorer reports a DDB operation the chain no longer contains.
func TestDdbRowsPurgedOnReorg(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	contract := common.HexToAddress("0xcccc000000000000000000000000000000000abc")

	if err := s.StoreBlock(ctx, &BlockData{
		Block:        mkBlock(1),
		Transactions: []*types.Transaction{mkDdbTx(1, 0, &contract, nil)},
	}); err != nil {
		t.Fatalf("first ingest: %v", err)
	}

	// the same height, reorged to contain a plain transfer instead
	from := common.HexToAddress("0xaaaa000000000000000000000000000000000001")
	to := common.HexToAddress("0xbbbb000000000000000000000000000000000002")
	if err := s.StoreBlock(ctx, &BlockData{
		Block:        mkBlock(1),
		Transactions: []*types.Transaction{mkTx(1, 0, from, to, big.NewInt(1))},
	}); err != nil {
		t.Fatalf("reorg ingest: %v", err)
	}

	var ops, endorsements int
	if err := s.pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM ddb_operation   WHERE block_number = 1),
		       (SELECT count(*) FROM ddb_endorsement WHERE block_number = 1)`).
		Scan(&ops, &endorsements); err != nil {
		t.Fatalf("count: %v", err)
	}
	if ops != 0 || endorsements != 0 {
		t.Errorf("after the reorg %d operations and %d endorsements survived; both must be 0",
			ops, endorsements)
	}
}

// TestDdbContractCountSurvivesReScan is the regression test for a maintained-counter bug I
// wrote and then removed.
//
// op_count was `op_count + 1` on conflict, so re-ingesting a block -- which the scanner
// does routinely, and which is the whole repair mechanism for a gap -- incremented it
// again. The count would drift upward forever with nothing to detect it. It is now derived
// by counting ddb_operation, so it is a function of the stored operations and cannot
// disagree with them.
func TestDdbContractCountSurvivesReScan(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	contract := common.HexToAddress("0xcccc000000000000000000000000000000000abc")
	data := &BlockData{
		Block:        mkBlock(1),
		Transactions: []*types.Transaction{mkDdbTx(1, 0, &contract, nil)},
	}

	// ingest the same block three times, as a re-scan would
	for i := 0; i < 3; i++ {
		if err := s.StoreBlock(ctx, data); err != nil {
			t.Fatalf("ingest pass %d: %v", i, err)
		}
	}

	var opCount int64
	var rows int
	if err := s.pool.QueryRow(ctx, `
		SELECT (SELECT op_count FROM ddb_contract WHERE contract_addr = $1),
		       (SELECT count(*) FROM ddb_operation WHERE contract_addr = $1::address)`,
		AddrVal(contract)).Scan(&opCount, &rows); err != nil {
		t.Fatalf("read: %v", err)
	}

	if rows != 1 {
		t.Errorf("three ingests produced %d operation rows, want 1", rows)
	}
	if opCount != 1 {
		t.Errorf("op_count = %d after three ingests, want 1 -- a maintained counter would read 3", opCount)
	}
}
