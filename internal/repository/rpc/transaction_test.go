package rpc

import "testing"

// TestTransactionsChunksByBatchLimit pins the chunking, because the failure it prevents is
// silent and remote.
//
// go-ethereum's RPC server caps both the number of requests in a batch and the total
// response size, and neither cap is advertised to the client. A block with thousands of
// transactions sent as one batch would be rejected or truncated by the node -- and a
// truncated batch is far worse than a slow one, because a short response would look like
// transactions that do not exist.
func TestTransactionsChunksByBatchLimit(t *testing.T) {
	if txBatchChunk <= 0 {
		t.Fatalf("txBatchChunk is %d; a non-positive chunk size would loop forever", txBatchChunk)
	}
	if txBatchChunk > 1000 {
		t.Errorf("txBatchChunk is %d, which exceeds go-ethereum's default batch request limit of 1000",
			txBatchChunk)
	}

	// The loop in Transactions must cover every hash exactly once, with no gap at the
	// boundary and no final chunk running past the end of the slice.
	for _, n := range []int{0, 1, txBatchChunk - 1, txBatchChunk, txBatchChunk + 1, 3*txBatchChunk + 7} {
		covered := 0
		for start := 0; start < n; start += txBatchChunk {
			end := start + txBatchChunk
			if end > n {
				end = n
			}
			if end <= start {
				t.Fatalf("n=%d produced an empty or negative chunk [%d,%d)", n, start, end)
			}
			covered += end - start
		}
		if covered != n {
			t.Errorf("n=%d: chunks covered %d hashes, want %d", n, covered, n)
		}
	}
}
