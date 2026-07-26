package svc

import "testing"

// TestIdleStallDetection covers the guard that stops the block scanner from silently freezing the
// index when the new-heads subscription dies.
//
// The scanner goes idle once it reaches the head and then depends on that subscription to dispatch
// new blocks; blsReScanHysteresis (100 blocks) is only meant as a large-gap safety net. Against an
// endpoint that cannot deliver notifications, nothing advances the dispatcher and the index can sit
// frozen for hours while node-backed queries still look healthy. idleStallDetected is what breaks
// that: outstanding blocks + no dispatch progress for blsIdleStallLimit observations => resume.
func TestIdleStallDetection(t *testing.T) {
	t.Run("caught up with the head never stalls", func(t *testing.T) {
		bls := &blkScanner{done: 100}
		for i := 0; i < blsIdleStallLimit*3; i++ {
			if bls.idleStallDetected(100) {
				t.Fatalf("reported a stall while at the head (observation %d)", i)
			}
		}
	})

	t.Run("outstanding blocks with no progress trips after the limit", func(t *testing.T) {
		bls := &blkScanner{done: 509}

		// The FIRST observation only establishes the baseline: with no previous `done` recorded we
		// cannot tell "just went idle" from "stalled", so it must not trip. After that, each
		// no-progress observation counts, and the trip lands on baseline + blsIdleStallLimit.
		for i := 0; i < blsIdleStallLimit; i++ {
			if bls.idleStallDetected(514) {
				t.Fatalf("tripped early at observation %d (limit is %d)", i+1, blsIdleStallLimit)
			}
		}
		if !bls.idleStallDetected(514) {
			t.Fatalf("did not trip on observation %d with 5 blocks outstanding", blsIdleStallLimit+1)
		}
	})

	t.Run("dispatch progress resets the counter", func(t *testing.T) {
		bls := &blkScanner{done: 509}

		// one observation with no progress...
		if bls.idleStallDetected(514) {
			t.Fatal("tripped on the first observation")
		}
		// ...then the subscription delivers a block, so the counter must restart
		bls.done = 510
		if bls.idleStallDetected(514) {
			t.Fatal("tripped despite dispatch progress")
		}
		if bls.idleStalls != 0 {
			t.Fatalf("stall counter = %d after progress, want 0", bls.idleStalls)
		}

		// a healthy feed that keeps advancing must never trip, however long it runs
		for i := 0; i < blsIdleStallLimit*3; i++ {
			bls.done++
			if bls.idleStallDetected(bls.done + 2) {
				t.Fatalf("tripped on a feed that is still delivering (observation %d)", i)
			}
		}
	})

	t.Run("counter resets after tripping so it can trip again", func(t *testing.T) {
		bls := &blkScanner{done: 509}
		for i := 0; i < blsIdleStallLimit; i++ {
			bls.idleStallDetected(514)
		}
		if !bls.idleStallDetected(514) {
			t.Fatal("first trip did not fire")
		}
		if bls.idleStalls != 0 {
			t.Fatalf("stall counter = %d right after tripping, want 0", bls.idleStalls)
		}
		// and it must be able to fire again on a subsequent stall
		for i := 1; i < blsIdleStallLimit; i++ {
			if bls.idleStallDetected(514) {
				t.Fatalf("second cycle tripped early at %d", i)
			}
		}
		if !bls.idleStallDetected(514) {
			t.Fatal("did not trip again on a second stall")
		}
	})
}
