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
		"DELETE FROM tx_log", "DELETE FROM tx_account", "DELETE FROM tx", "DELETE FROM block",
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
