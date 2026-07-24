package pg

import (
	"context"
	"testing"
)

// ForkedPredecessors is what turns a chain reorg into a self-repairing one: it names the
// stored blocks whose hash no longer matches the parent_hash of the block above them, so the
// orchestrator can re-fetch the canonical block. A false negative would leave a reorged block
// permanently wrong; a false positive would churn re-fetches. mkBlock builds a continuous
// chain (block n's parent_hash equals block n-1's hash), so a deliberate hash perturbation is
// the only divergence.
func TestForkedPredecessorsDetectsReorg(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	for _, n := range []uint64{1, 2, 3} {
		if err := s.StoreBlock(ctx, &BlockData{Block: mkBlock(n)}); err != nil {
			t.Fatalf("store block %d: %v", n, err)
		}
	}

	// A continuous chain has no forked predecessors.
	forked, err := s.ForkedPredecessors(ctx, 0, 10)
	if err != nil {
		t.Fatalf("ForkedPredecessors on a continuous chain: %v", err)
	}
	if len(forked) != 0 {
		t.Fatalf("continuous chain reported %d forked block(s): %v", len(forked), forked)
	}

	// Reorg height 2: store a DIFFERENT block at height 2 (perturbed hash). Block 3's
	// parent_hash still points at the original block-2 hash, so block 2 is now on an
	// abandoned fork relative to its successor.
	fork2 := mkBlock(2)
	fork2.Hash[1] = 0xFF // diverge from the original mkBlock(2).Hash
	if err := s.StoreBlock(ctx, &BlockData{Block: fork2}); err != nil {
		t.Fatalf("store fork block 2: %v", err)
	}

	forked, err = s.ForkedPredecessors(ctx, 0, 10)
	if err != nil {
		t.Fatalf("ForkedPredecessors after reorg: %v", err)
	}
	if len(forked) != 1 || forked[0] != 2 {
		t.Fatalf("expected forked predecessor [2] after a height-2 reorg, got %v", forked)
	}

	// The window bound excludes blocks at or below aboveBlock: with aboveBlock=2 only block 3
	// (number > 2) is inspected, and block 3's own predecessor (2) is the divergence, so it is
	// still found; with aboveBlock=3 nothing above it exists, so the scan is empty.
	forked, err = s.ForkedPredecessors(ctx, 3, 10)
	if err != nil {
		t.Fatalf("ForkedPredecessors with aboveBlock=3: %v", err)
	}
	if len(forked) != 0 {
		t.Fatalf("aboveBlock=3 should inspect nothing, got %v", forked)
	}
}
