package pg

import (
	"context"
	"math/big"
	"testing"
	"time"

	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	retypes "github.com/ethereum/go-ethereum/core/types"
)

// Ingest throughput.
//
// A benchmark rather than a test, so `go test ./...` never pays for it; run it with
//
//	go test ./internal/repository/db/pg/ -bench StoreBlock -benchtime 20x
//
// with EXPLORER_TEST_DSN pointing at a scratch database.
//
// What it measures is the DB write path, not the scanner: the block is built once, up
// front, so only StoreBlock is timed. That distinction matters, because end-to-end scan
// time against a remote node is dominated by RPC latency and would hide a change here
// entirely.
//
// Measured on PostgreSQL 16 in Docker, 150 transactions per block with two logs each,
// three runs of 6,000 transactions:
//
//	row-at-a-time   296-301 tx/s   (~20.1 s)
//	batched         4839-4928 tx/s (~1.23 s)
//
// The gap is round trips, not PostgreSQL. Row-at-a-time cost one statement for the
// transaction plus one per account edge plus a COPY for its logs -- four per transaction
// here -- where the batched path pipelines the whole block into a constant few.

// benchTx builds one synthetic transaction at a position in a block.
func benchTx(blockNum, idx uint64, logsPer int) *types.Transaction {
	var h common.Hash
	// Unique across the whole run: block in the high bytes, index in the low. mkTx packs
	// both into single bytes, which collides past 256 and is fine for its callers but not
	// at benchmark volumes.
	h[0] = byte(blockNum >> 24)
	h[1] = byte(blockNum >> 16)
	h[2] = byte(blockNum >> 8)
	h[3] = byte(blockNum)
	h[4] = byte(idx >> 8)
	h[5] = byte(idx)

	bn := hexutil.Uint64(blockNum)
	ix := hexutil.Uint64(idx)
	status := hexutil.Uint64(1)
	bh := mkBlock(blockNum).Hash

	// Spread over many distinct addresses so the account-edge index is exercised rather
	// than a single hot key repeatedly updated in place.
	from := common.BigToAddress(big.NewInt(int64(idx%512 + 1)))
	to := common.BigToAddress(big.NewInt(int64(idx%997 + 10000)))

	logs := make([]retypes.Log, 0, logsPer)
	for i := 0; i < logsPer; i++ {
		var t0 common.Hash
		t0[31] = byte(i)
		logs = append(logs, retypes.Log{
			// log_index is unique within the BLOCK, not within the transaction:
			// tx_log's primary key is (block_number, log_index).
			Index:   uint(idx)*uint(logsPer) + uint(i),
			Address: to,
			Topics:  []common.Hash{t0},
			Data:    []byte{0xde, 0xad, 0xbe, 0xef},
		})
	}

	return &types.Transaction{
		Hash:        h,
		BlockNumber: &bn,
		BlockHash:   &bh,
		Index:       &ix,
		From:        from,
		To:          &to,
		Value:       hexutil.Big(*big.NewInt(1)),
		GasPrice:    hexutil.Big(*big.NewInt(1000000000)),
		Gas:         hexutil.Uint64(21000),
		Nonce:       hexutil.Uint64(idx),
		Status:      &status,
		TimeStamp:   time.Now().UTC(),
		InputData:   hexutil.Bytes{},
		Logs:        logs,
	}
}

// BenchmarkStoreBlock writes one full block per iteration.
//
// Re-storing the SAME height each time is deliberate and costs nothing in realism:
// StoreBlock purges the block before inserting it, so every iteration does the same purge
// and the same writes, and the database does not grow without bound across b.N.
func BenchmarkStoreBlock(b *testing.B) {
	const (
		perBlock = 150
		logsPer  = 2
	)

	s := testStore(b)
	ctx := context.Background()

	txs := make([]*types.Transaction, 0, perBlock)
	for i := uint64(0); i < perBlock; i++ {
		txs = append(txs, benchTx(1, i, logsPer))
	}
	data := &BlockData{Block: mkBlock(1), Transactions: txs}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.StoreBlock(ctx, data); err != nil {
			b.Fatalf("store block: %v", err)
		}
	}
	b.StopTimer()

	// Transactions per second is the number this benchmark exists to report; ns/op on a
	// whole block is hard to compare against anything.
	elapsed := b.Elapsed().Seconds()
	if elapsed > 0 {
		b.ReportMetric(float64(b.N*perBlock)/elapsed, "tx/s")
	}
}
