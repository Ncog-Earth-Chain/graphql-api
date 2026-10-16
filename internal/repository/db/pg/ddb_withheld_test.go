package pg

import (
	"context"
	"math/big"
	"ncogearthchain-api-graphql/internal/types"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// mkRedactedDdbTx builds a DDB commit transaction as the node serves it when it withholds the
// calldata: `input` empty and an `inputRedacted` descriptor in its place -- so nothing decodes and
// t.DDB stays nil, exactly as on the wire.
func mkRedactedDdbTx(block, idx uint64, reason string) *types.Transaction {
	from := common.HexToAddress("0xaaaa000000000000000000000000000000000001")
	to := types.DDBContractAddress
	trx := mkTx(block, idx, from, to, big.NewInt(0))
	trx.InputData = hexutil.Bytes{} // "0x" on the wire, as the node serves a withheld commit
	trx.InputRedacted = &types.DdbWithheld{
		Reason:    reason,
		Schema:    "c_cccc000000000000000000000000000000000abc",
		Size:      hexutil.Uint64(512),
		Keccak256: common.HexToHash("0x5555"),
	}
	return trx
}

func countRows(t *testing.T, s *Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// TestDdbFinalWithholdIsRecordedNotDecoded: a commit the node withholds for a reason about the
// CONTRACT is recorded as withheld, and nothing about its operation is indexed.
func TestDdbFinalWithholdIsRecordedNotDecoded(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	for i, reason := range []string{types.DdbWithheldReaderRole, types.DdbWithheldTombstoned, types.DdbWithheldUndecodable} {
		blk := uint64(10 + i)
		if err := s.StoreBlock(ctx, &BlockData{
			Block:        mkBlock(blk),
			Transactions: []*types.Transaction{mkRedactedDdbTx(blk, 0, reason)},
		}); err != nil {
			t.Fatalf("%s: store: %v", reason, err)
		}
		if n := countRows(t, s, `SELECT count(*) FROM ddb_withheld_commit WHERE block_number = $1 AND reason = $2`, int64(blk), reason); n != 1 {
			t.Errorf("%s: %d withheld rows, want 1 -- a final refusal must be recorded as withheld", reason, n)
		}
		if n := countRows(t, s, `SELECT count(*) FROM ddb_verdict_pending WHERE block_number = $1`, int64(blk)); n != 0 {
			t.Errorf("%s: queued for a re-fetch; a final verdict is not transient", reason)
		}
	}
	if n := countRows(t, s, `SELECT count(*) FROM ddb_operation`); n != 0 {
		t.Errorf("%d ddb_operation rows for withheld commits, want 0", n)
	}
}

// TestDdbUndecidableIsQueuedNeverFinal: `undecidable` is a statement about the node, not the
// contract. It is queued for a re-fetch and never recorded as a final withhold.
func TestDdbUndecidableIsQueuedNeverFinal(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	if err := s.StoreBlock(ctx, &BlockData{
		Block:        mkBlock(20),
		Transactions: []*types.Transaction{mkRedactedDdbTx(20, 0, types.DdbWithheldUndecidable)},
	}); err != nil {
		t.Fatalf("store: %v", err)
	}
	if n := countRows(t, s, `SELECT count(*) FROM ddb_withheld_commit`); n != 0 {
		t.Errorf("an undecidable commit was persisted as final (%d rows)", n)
	}
	pending, err := s.DdbPendingVerdicts(ctx, time.Now().Add(time.Second), 10)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 1 || pending[0].BlockNumber != 20 || pending[0].TxIndex != 0 {
		t.Fatalf("pending = %+v, want the one commit at 20/0", pending)
	}
}

// TestDdbUndecidableReIngestedOnceAppliedIsIndexed: the node serves the same commit verbatim once it
// has applied its block. The re-fetch then indexes the operation and clears the queue entry.
func TestDdbUndecidableReIngestedOnceAppliedIsIndexed(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	contract := common.HexToAddress("0xcccc000000000000000000000000000000000abc")
	redacted := mkRedactedDdbTx(30, 0, types.DdbWithheldUndecidable)
	if err := s.StoreBlock(ctx, &BlockData{Block: mkBlock(30), Transactions: []*types.Transaction{redacted}}); err != nil {
		t.Fatalf("store: %v", err)
	}
	pending, err := s.DdbPendingVerdicts(ctx, time.Now().Add(time.Second), 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending = %+v, %v; want one", pending, err)
	}

	// Still undecidable: stays queued, backed off.
	if err := s.ResolveDdbPending(ctx, pending[0], redacted); err != nil {
		t.Fatalf("resolve (still undecidable): %v", err)
	}
	if again, _ := s.DdbPendingVerdicts(ctx, time.Now().Add(time.Second), 10); len(again) != 0 {
		t.Errorf("an undecidable re-fetch was retried immediately; it must back off")
	}
	if n := countRows(t, s, `SELECT count(*) FROM ddb_verdict_pending WHERE attempts = 1`); n != 1 {
		t.Errorf("the still-undecidable commit left the queue or did not count the attempt")
	}

	// Applied now: the node serves it.
	served := mkDdbTx(30, 0, &contract, nil)
	served.Hash = redacted.Hash
	served.InputData = hexutil.Bytes("DDBR-served")
	if err := s.ResolveDdbPending(ctx, pending[0], served); err != nil {
		t.Fatalf("resolve (served): %v", err)
	}
	if n := countRows(t, s, `SELECT count(*) FROM ddb_operation WHERE block_number = 30 AND tx_index = 0`); n != 1 {
		t.Errorf("the served commit was not indexed (%d rows)", n)
	}
	if n := countRows(t, s, `SELECT count(*) FROM tx WHERE block_number = 30 AND is_ddb`); n != 1 {
		t.Errorf("the served commit's transaction is not marked is_ddb")
	}
	if n := countRows(t, s, `SELECT count(*) FROM ddb_verdict_pending`); n != 0 {
		t.Errorf("the served commit is still queued")
	}
	if n := countRows(t, s, `SELECT count(*) FROM ddb_withheld_commit`); n != 0 {
		t.Errorf("a served commit left a withheld record")
	}
}

// TestDdbPendingThatTurnsFinalIsRecordedWithheld: a re-fetch can also come back with a final
// reason (a reader role added in the block the node has now applied). It is recorded withheld and
// leaves the queue.
func TestDdbPendingThatTurnsFinalIsRecordedWithheld(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	redacted := mkRedactedDdbTx(40, 0, types.DdbWithheldUndecidable)
	if err := s.StoreBlock(ctx, &BlockData{Block: mkBlock(40), Transactions: []*types.Transaction{redacted}}); err != nil {
		t.Fatalf("store: %v", err)
	}
	pending, _ := s.DdbPendingVerdicts(ctx, time.Now().Add(time.Second), 10)
	if len(pending) != 1 {
		t.Fatalf("pending = %+v; want one", pending)
	}
	final := mkRedactedDdbTx(40, 0, types.DdbWithheldReaderRole)
	final.Hash = redacted.Hash
	if err := s.ResolveDdbPending(ctx, pending[0], final); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if n := countRows(t, s, `SELECT count(*) FROM ddb_withheld_commit WHERE reason = 'reader-role'`); n != 1 {
		t.Errorf("the final verdict was not recorded")
	}
	if n := countRows(t, s, `SELECT count(*) FROM ddb_verdict_pending`); n != 0 {
		t.Errorf("a final verdict is still queued")
	}
	if n := countRows(t, s, `SELECT count(*) FROM ddb_operation`); n != 0 {
		t.Errorf("a withheld commit was indexed")
	}
}

// TestDdbWithheldRowsPurgedOnReorg: both tables follow the block like ddb_operation does.
func TestDdbWithheldRowsPurgedOnReorg(t *testing.T) {
	for _, tbl := range []string{"ddb_withheld_commit", "ddb_verdict_pending"} {
		if !purgedTables()[tbl] {
			t.Errorf("%s is not in the ingest purge order; a reorg would leave its rows behind", tbl)
		}
	}
}
