package pg

import (
	"context"
	"math/big"
	"ncogearthchain-api-graphql/internal/types"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	retypes "github.com/ethereum/go-ethereum/core/types"
)

// Ingest tests against a real PostgreSQL.
//
// These verify the claims that motivated the rewrite. Each one corresponds to a way the
// MongoDB ingest lost data:
//
//   - a partially-written block must not be recorded at all (it was, and silently)
//   - the watermark must never pass a gap (it did, permanently)
//   - re-ingest must be a clean replace (there was no replace; a reorg orphaned rows)
//
// A claim like "this fixes permanent gaps" is worth nothing unless the gap is actually
// constructed and the watermark observed refusing to cross it. That is what these do.

func testStore(t *testing.T) *Store {
	t.Helper()

	pool := testPool(t)
	return NewStore(&Pool{Pool: pool, log: testLogger{}}, testLogger{})
}

// testLogger discards output; the tests assert on database state, not on logs.
type testLogger struct{}

func (testLogger) Fatal(...interface{})             {}
func (testLogger) Fatalf(string, ...interface{})    {}
func (testLogger) Panic(...interface{})             {}
func (testLogger) Panicf(string, ...interface{})    {}
func (testLogger) Critical(...interface{})          {}
func (testLogger) Criticalf(string, ...interface{}) {}
func (testLogger) Error(...interface{})             {}
func (testLogger) Errorf(string, ...interface{})    {}
func (testLogger) Warning(...interface{})           {}
func (testLogger) Warningf(string, ...interface{})  {}
func (testLogger) Notice(...interface{})            {}
func (testLogger) Noticef(string, ...interface{})   {}
func (testLogger) Info(...interface{})              {}
func (testLogger) Infof(string, ...interface{})     {}
func (testLogger) Debug(...interface{})             {}
func (testLogger) Debugf(string, ...interface{})    {}
func (testLogger) Printf(string, ...interface{})    {}

// cleanDB empties every table the ingest path writes, so tests do not see each other.
func cleanDB(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	for _, q := range []string{
		"DELETE FROM tx_log", "DELETE FROM tx_account", "DELETE FROM tx",
		"DELETE FROM ddb_operation", "DELETE FROM ddb_endorsement", "DELETE FROM ddb_contract",
		"DELETE FROM block",
		"UPDATE meta_counter SET value = 0 WHERE key = 'contiguous_head'",
	} {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			t.Fatalf("clean: %v", err)
		}
	}
}

// mkBlock builds a deterministic block at a height.
func mkBlock(n uint64) *types.Block {
	var h, ph, sr common.Hash
	h[0], h[31] = 0x00, byte(n) // first 4 bytes are the epoch; keep it 0 here
	ph[31] = byte(n - 1)
	sr[31] = 0xAA
	return &types.Block{
		Number:     hexutil.Uint64(n),
		Hash:       h,
		ParentHash: ph,
		Miner:      common.HexToAddress("0x1111111111111111111111111111111111111111"),
		StateRoot:  sr,
		GasLimit:   hexutil.Uint64(20500000),
		GasUsed:    hexutil.Uint64(21000),
		Size:       hexutil.Uint64(1024),
		TimeStamp:  hexutil.Uint64(time.Now().Unix()),
	}
}

// mkTx builds a transaction at a position in a block.
func mkTx(blockNum uint64, idx uint64, from, to common.Address, value *big.Int) *types.Transaction {
	var h common.Hash
	h[0] = byte(blockNum)
	h[1] = byte(idx)

	bn := hexutil.Uint64(blockNum)
	ix := hexutil.Uint64(idx)
	status := hexutil.Uint64(1)
	bh := mkBlock(blockNum).Hash

	return &types.Transaction{
		Hash:        h,
		BlockNumber: &bn,
		BlockHash:   &bh,
		Index:       &ix,
		From:        from,
		To:          &to,
		Value:       hexutil.Big(*value),
		GasPrice:    hexutil.Big(*big.NewInt(1000000000)),
		Gas:         hexutil.Uint64(21000),
		Nonce:       hexutil.Uint64(idx),
		Status:      &status,
		TimeStamp:   time.Now().UTC(),
		InputData:   hexutil.Bytes{},
	}
}

// TestStoreBlockIsAtomic: a block whose transactions cannot all be written must leave NO
// trace, not a partial one.
//
// Under MongoDB there was no transaction larger than a single InsertOne, so a failure
// midway left some rows present and the rest missing with nothing recording which.
func TestStoreBlockIsAtomic(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	from := common.HexToAddress("0xaaaa000000000000000000000000000000000001")
	to := common.HexToAddress("0xbbbb000000000000000000000000000000000002")

	// a nil transaction stands for one the loader could not fetch
	err := s.StoreBlock(ctx, &BlockData{
		Block: mkBlock(1),
		Transactions: []*types.Transaction{
			mkTx(1, 0, from, to, big.NewInt(100)),
			nil,
		},
	})
	if err == nil {
		t.Fatal("StoreBlock accepted a block with an unloadable transaction")
	}

	// nothing at all may have landed
	var blocks, txs int
	if err := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM block), (SELECT count(*) FROM tx)").
		Scan(&blocks, &txs); err != nil {
		t.Fatalf("count: %v", err)
	}
	if blocks != 0 || txs != 0 {
		t.Errorf("a failed block left %d block rows and %d transaction rows behind", blocks, txs)
	}
}

// TestWatermarkNeverCrossesAGap is the central claim of the rewrite.
//
// Blocks 1,2,3 then 5 are stored. The watermark must report 3 -- not 5 -- because block
// 4 is missing. The MongoDB watermark was written by a detached goroutine after storing a
// transaction, so it advanced past holes and the scanner never came back: the fixed
// rescan depth could not reach an old gap, and afterwards nothing distinguished the hole
// from a range that genuinely had no transactions.
func TestWatermarkNeverCrossesAGap(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	for _, n := range []uint64{1, 2, 3} {
		if err := s.StoreBlock(ctx, &BlockData{Block: mkBlock(n)}); err != nil {
			t.Fatalf("store block %d: %v", n, err)
		}
	}

	head, err := s.ContiguousHead(ctx)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if head != 3 {
		t.Fatalf("after blocks 1-3 the watermark is %d, want 3", head)
	}

	// skip 4
	if err := s.StoreBlock(ctx, &BlockData{Block: mkBlock(5)}); err != nil {
		t.Fatalf("store block 5: %v", err)
	}

	head, err = s.ContiguousHead(ctx)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if head != 3 {
		t.Errorf("the watermark advanced to %d across the missing block 4; it must stay at 3", head)
	}

	// and the gap must be enumerable, so it can be healed
	missing, err := s.MissingBlocks(ctx, 1, 5, 10)
	if err != nil {
		t.Fatalf("MissingBlocks: %v", err)
	}
	if len(missing) != 1 || missing[0] != 4 {
		t.Errorf("MissingBlocks = %v, want [4]", missing)
	}

	// filling it must let the watermark move past
	if err := s.StoreBlock(ctx, &BlockData{Block: mkBlock(4)}); err != nil {
		t.Fatalf("store block 4: %v", err)
	}
	head, err = s.ContiguousHead(ctx)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if head != 5 {
		t.Errorf("after healing the gap the watermark is %d, want 5", head)
	}
}

// TestReIngestIsIdempotent: scanning the same block twice must not duplicate rows. This
// is what makes re-scanning a safe repair operation rather than a corruption risk.
func TestReIngestIsIdempotent(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	from := common.HexToAddress("0xaaaa000000000000000000000000000000000001")
	to := common.HexToAddress("0xbbbb000000000000000000000000000000000002")

	data := &BlockData{
		Block:        mkBlock(1),
		Transactions: []*types.Transaction{mkTx(1, 0, from, to, big.NewInt(100))},
	}

	for i := 0; i < 3; i++ {
		if err := s.StoreBlock(ctx, data); err != nil {
			t.Fatalf("store pass %d: %v", i, err)
		}
	}

	var blocks, txs, edges int
	if err := s.pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM block), (SELECT count(*) FROM tx), (SELECT count(*) FROM tx_account)`).
		Scan(&blocks, &txs, &edges); err != nil {
		t.Fatalf("count: %v", err)
	}
	if blocks != 1 || txs != 1 {
		t.Errorf("after 3 identical ingests: %d blocks, %d transactions; want 1 and 1", blocks, txs)
	}
	if edges != 2 {
		t.Errorf("expected 2 account edges (sender + recipient), got %d", edges)
	}
}

// TestReorgReplacesBlockContents: re-ingesting a height with DIFFERENT transactions must
// leave only the new ones.
//
// ON CONFLICT alone cannot do this -- it updates rows that still exist and orphans those
// that no longer do, so a transaction dropped by a reorg would linger forever, attached
// to a block that no longer contains it.
func TestReorgReplacesBlockContents(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	a := common.HexToAddress("0xaaaa000000000000000000000000000000000001")
	b := common.HexToAddress("0xbbbb000000000000000000000000000000000002")

	// first version: two transactions
	if err := s.StoreBlock(ctx, &BlockData{
		Block: mkBlock(1),
		Transactions: []*types.Transaction{
			mkTx(1, 0, a, b, big.NewInt(100)),
			mkTx(1, 1, a, b, big.NewInt(200)),
		},
	}); err != nil {
		t.Fatalf("first ingest: %v", err)
	}

	// reorg: the same height now carries only one
	if err := s.StoreBlock(ctx, &BlockData{
		Block:        mkBlock(1),
		Transactions: []*types.Transaction{mkTx(1, 0, a, b, big.NewInt(100))},
	}); err != nil {
		t.Fatalf("reorg ingest: %v", err)
	}

	var txs int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM tx WHERE block_number = 1").Scan(&txs); err != nil {
		t.Fatalf("count: %v", err)
	}
	if txs != 1 {
		t.Errorf("after a reorg the block holds %d transactions; the orphaned one was not removed", txs)
	}
}

// TestSelfTransferProducesOneEdge: an address that is both sender and recipient must
// appear ONCE in its own history, with both role bits set -- not twice.
func TestSelfTransferProducesOneEdge(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	self := common.HexToAddress("0xcccc000000000000000000000000000000000003")

	if err := s.StoreBlock(ctx, &BlockData{
		Block:        mkBlock(1),
		Transactions: []*types.Transaction{mkTx(1, 0, self, self, big.NewInt(1))},
	}); err != nil {
		t.Fatalf("store: %v", err)
	}

	var rows int
	var roles int16
	if err := s.pool.QueryRow(ctx,
		"SELECT count(*), COALESCE(max(roles),0) FROM tx_account WHERE address = $1", AddrVal(self)).
		Scan(&rows, &roles); err != nil {
		t.Fatalf("query: %v", err)
	}
	if rows != 1 {
		t.Errorf("a self-transfer produced %d edge rows, want 1", rows)
	}
	if roles != roleSender|roleRecipient {
		t.Errorf("roles = %d, want both sender and recipient bits set (%d)", roles, roleSender|roleRecipient)
	}
}

// TestLogsAreStoredAndQueryable: logs were persisted under MongoDB but embedded in the
// transaction document and indexed by nothing, so no log query was possible at all.
func TestLogsAreStoredAndQueryable(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	from := common.HexToAddress("0xaaaa000000000000000000000000000000000001")
	to := common.HexToAddress("0xbbbb000000000000000000000000000000000002")
	emitter := common.HexToAddress("0xdddd000000000000000000000000000000000004")

	// the ERC-20 Transfer signature
	sig := common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")

	trx := mkTx(1, 0, from, to, big.NewInt(0))
	trx.Logs = []retypes.Log{{
		Address: emitter,
		Topics:  []common.Hash{sig, {}, {}},
		Data:    []byte{0x01, 0x02},
		Index:   0,
	}}

	if err := s.StoreBlock(ctx, &BlockData{
		Block:        mkBlock(1),
		Transactions: []*types.Transaction{trx},
	}); err != nil {
		t.Fatalf("store: %v", err)
	}

	// the query the MongoDB schema could not answer at all
	var count int
	var topicCount int16
	if err := s.pool.QueryRow(ctx,
		"SELECT count(*), COALESCE(max(topic_count),0) FROM tx_log WHERE address = $1 AND topic0 = $2",
		AddrVal(emitter), HashVal(sig)).Scan(&count, &topicCount); err != nil {
		t.Fatalf("log query: %v", err)
	}
	if count != 1 {
		t.Errorf("log lookup by address+topic0 returned %d rows, want 1", count)
	}
	if topicCount != 3 {
		t.Errorf("topic_count = %d, want 3", topicCount)
	}
}

// TestPurgeCoversEveryBlockKeyedTable is a structural guard, not a behavioural one.
//
// purgeBlockRows must delete from every table keyed by block_number. A table left out
// survives a reorg at its old position, and because re-ingest is ON CONFLICT DO NOTHING
// those rows are never overwritten -- producing phantom transfers and permanently wrong
// reward totals. That is exactly the failure the purge exists to prevent, and it
// reappears one table at a time as new tables are added.
//
// This asks the database which tables have a block_number column and fails if the purge
// does not mention one, so adding a table without updating the purge breaks a test rather
// than corrupting data after the next reorg.
func TestPurgeCoversEveryBlockKeyedTable(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	rows, err := s.pool.Query(ctx, `
		SELECT c.relname
		FROM   information_schema.columns col
		JOIN   pg_class c ON c.relname = col.table_name
		WHERE  col.table_schema = 'public'
		  AND  col.column_name = 'block_number'
		  AND  c.relkind = 'r'
		  AND  c.relispartition = false
		GROUP  BY c.relname
		ORDER  BY c.relname`)
	if err != nil {
		t.Fatalf("introspect: %v", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		tables = append(tables, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(tables) == 0 {
		t.Fatal("no block-keyed tables found; the introspection query is wrong")
	}

	// Tables the purge deliberately does not touch. Each entry is a decision, not an
	// oversight -- adding one of these to the purge would DELETE VALID DATA.
	exempt := map[string]string{
		// Upserted by writeBlock. Deleting it would cascade to everything else
		// mid-transaction.
		"block": "upserted by writeBlock, not deleted",

		// Accumulated per block with its own reconciliation, and burn_tx is a child of
		// burn rather than of a block's logs.
		"burn":    "accumulated separately",
		"burn_tx": "child of burn, not of a block's logs",

		// Partition bookkeeping, not chain data.
		"partition_registry": "partition metadata",

		// Keyed by ADDRESS, not by block position, and contract_verification hangs off
		// it holding user-submitted source code and validation results -- data that is
		// NOT derivable from the chain and cannot be rebuilt by re-scanning. Purging a
		// contract on reorg would destroy it permanently. block_number here records
		// where the contract was deployed; it is not an ownership key.
		"contract": "address-keyed; holds non-rebuildable user-submitted verification",

		// Uniswap, scheduled for removal (see doc/defi-removal-notes.md).
		"swap": "part of the Uniswap module being removed",

		// DOMAIN-KEYED, and this is a known limitation rather than a clean exemption.
		//
		// delegation is keyed (delegator, validator_id) and withdrawal is keyed
		// (delegator, validator_id, request_id, request_tx). Their block_number records
		// the LAST event that touched the row, not the row's identity. Deleting by block
		// would remove a delegation that is still live merely because its most recent
		// update happened in the reorged block.
		//
		// Rolling these back correctly needs the prior state, which is not stored --
		// they are current-state rows built by applying events, with no event log to
		// replay. So a reorg that removes an SFC event can leave these slightly stale
		// until the next event for the same key overwrites them.
		//
		// Recorded here rather than papered over: the fix is to make them event-sourced,
		// which is a schema change and a separate decision.
		"delegation": "domain-keyed; needs event-sourced rollback (known limitation)",
		"withdrawal": "domain-keyed; needs event-sourced rollback (known limitation)",

		// Also domain-keyed, but unlike the two above this one is SAFE to leave stale:
		// it is a materialized fold of ddb_operation, which IS purged and IS the
		// authoritative history. A rebuild recomputes it exactly, so a reorg can leave it
		// briefly ahead of the operations it summarises without losing anything.
		"ddb_contract": "materialized fold of ddb_operation, which is purged and authoritative",
	}

	purged := purgedTables()

	for _, tbl := range tables {
		if _, ok := exempt[tbl]; ok {
			continue
		}
		if !purged[tbl] {
			t.Errorf("table %q has a block_number column but purgeBlockRows does not delete from it; "+
				"a reorg would leave its rows behind at the old block position", tbl)
		}
	}
}

// TestIncompleteBlockLeavesNoTraceAndDoesNotAdvance is the end-to-end statement of what
// the scanner rewiring buys.
//
// The scenario is the one the MongoDB pipeline mishandled: block 4's content cannot be
// fully loaded. Under the old flow the block was announced as dispatched BEFORE anything
// was loaded (dispatch_blk.go marked it first), the unloadable transaction was silently
// skipped, and the watermark -- written per transaction by a detached goroutine -- moved
// past it. The scanner's fixed rescan depth then meant the hole could never be revisited.
//
// Here the block must leave nothing behind, the watermark must stop below it, and a later
// successful re-ingest must heal it completely.
func TestIncompleteBlockLeavesNoTraceAndDoesNotAdvance(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	from := common.HexToAddress("0xaaaa000000000000000000000000000000000001")
	to := common.HexToAddress("0xbbbb000000000000000000000000000000000002")

	for _, n := range []uint64{1, 2, 3} {
		if err := s.StoreBlock(ctx, &BlockData{
			Block:        mkBlock(n),
			Transactions: []*types.Transaction{mkTx(n, 0, from, to, big.NewInt(1))},
		}); err != nil {
			t.Fatalf("store block %d: %v", n, err)
		}
	}

	// block 4: one transaction loads, one does not
	err := s.StoreBlock(ctx, &BlockData{
		Block: mkBlock(4),
		Transactions: []*types.Transaction{
			mkTx(4, 0, from, to, big.NewInt(1)),
			nil,
		},
	})
	if err == nil {
		t.Fatal("an incomplete block was accepted")
	}

	// nothing from block 4 may exist
	var blk4, tx4 int
	if err := s.pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM block WHERE number = 4),
		       (SELECT count(*) FROM tx WHERE block_number = 4)`).Scan(&blk4, &tx4); err != nil {
		t.Fatalf("count block 4: %v", err)
	}
	if blk4 != 0 || tx4 != 0 {
		t.Errorf("the failed block left %d block rows and %d transactions behind", blk4, tx4)
	}

	// the scanner's resume point must stop below the hole
	resume, err := s.LastKnownBlock(ctx)
	if err != nil {
		t.Fatalf("resume point: %v", err)
	}
	if resume != 3 {
		t.Errorf("resume point = %d, want 3 -- resuming above the hole is how gaps became permanent", resume)
	}

	// block 5 arriving later must NOT move the resume point past the hole
	if err := s.StoreBlock(ctx, &BlockData{
		Block:        mkBlock(5),
		Transactions: []*types.Transaction{mkTx(5, 0, from, to, big.NewInt(1))},
	}); err != nil {
		t.Fatalf("store block 5: %v", err)
	}
	resume, _ = s.LastKnownBlock(ctx)
	if resume != 3 {
		t.Errorf("resume point advanced to %d across the hole at 4", resume)
	}

	// and the hole must be enumerable so the scanner knows where to go back to
	missing, err := s.MissingBlocks(ctx, 1, 5, 10)
	if err != nil {
		t.Fatalf("MissingBlocks: %v", err)
	}
	if len(missing) != 1 || missing[0] != 4 {
		t.Errorf("MissingBlocks = %v, want [4]", missing)
	}

	// the retry succeeds and heals everything
	if err := s.StoreBlock(ctx, &BlockData{
		Block: mkBlock(4),
		Transactions: []*types.Transaction{
			mkTx(4, 0, from, to, big.NewInt(1)),
			mkTx(4, 1, from, to, big.NewInt(2)),
		},
	}); err != nil {
		t.Fatalf("retry block 4: %v", err)
	}

	resume, _ = s.LastKnownBlock(ctx)
	if resume != 5 {
		t.Errorf("after healing the gap the resume point is %d, want 5", resume)
	}

	var healed int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM tx WHERE block_number = 4").Scan(&healed); err != nil {
		t.Fatalf("count healed: %v", err)
	}
	if healed != 2 {
		t.Errorf("healed block 4 holds %d transactions, want 2", healed)
	}
}
