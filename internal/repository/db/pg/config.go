package pg

import (
	"context"
	"fmt"
)

// Scanner restart state.
//
// This is the whole of the indexer's durable position, and it is worth being precise
// about which of the two numbers below means what, because conflating them is what let
// gaps become permanent under MongoDB.
//
//	BlockHeight()     -- the highest block stored. How far the scanner has REACHED.
//	ContiguousHead()  -- the highest block N such that 1..N are all present. How far it
//	                     has reached with NOTHING MISSING BEHIND IT.
//
// MongoDB stored a single value, written by a detached per-transaction goroutine after a
// transaction landed. It therefore behaved like the height while being used as the
// watermark: it advanced past a block whose contents had failed to load, and the scanner
// resumed above the hole. Since the scanner only ever rewinds a fixed rescan depth, a gap
// older than that window could never be revisited.
//
// LastKnownBlock returns the CONTIGUOUS head, so resuming from it re-scans anything
// incomplete instead of stepping over it.

// LastKnownBlock returns the block the scanner should resume from.
//
// Deliberately the contiguous head rather than the height: resuming from the height would
// skip a gap forever, which is precisely the MongoDB failure. Re-scanning a block already
// stored is cheap and idempotent, so erring toward re-work is the correct direction.
func (s *Store) LastKnownBlock(ctx context.Context) (uint64, error) {
	return s.ContiguousHead(ctx)
}

// UpdateLastKnownBlock exists for interface compatibility and deliberately does nothing.
//
// The watermark is DERIVED from the block table inside the same transaction that writes a
// block (advanceContiguousHead), so it cannot be set from outside. Accepting a value here
// would let a caller assert progress the data does not support -- which is exactly how the
// MongoDB watermark came to overstate what had been indexed.
//
// It returns nil rather than an error so an existing caller keeps working; the write it
// intended has already happened, correctly, at ingest time.
func (s *Store) UpdateLastKnownBlock(ctx context.Context, blockNo uint64) error {
	return nil
}

// LastKnownEpochBlock returns the highest block recorded for an epoch.
//
// Used to resume epoch scanning independently of the block watermark, since epochs are
// sealed behind the head.
func (s *Store) LastKnownEpochBlock(ctx context.Context) (uint64, error) {
	var n *int64
	if err := s.pool.QueryRow(ctx,
		`SELECT max(number) FROM block WHERE epoch IS NOT NULL`).Scan(&n); err != nil {
		return 0, fmt.Errorf("can not read the last epoch-tagged block: %w", err)
	}
	if n == nil {
		return 0, nil
	}
	return uint64(*n), nil
}
