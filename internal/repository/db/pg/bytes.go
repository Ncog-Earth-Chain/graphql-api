package pg

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

// Addresses and hashes are stored as BYTEA, not as hex text.
//
// The MongoDB schema stored them as checksummed hex strings produced by .String().
// That makes equality depend on rendering: a writer that emits lowercase and a reader
// that queries checksummed silently never match, and the row is simply not found. It
// also costs about 60% more index space on the largest tables in the database.
//
// The functions below are the only conversion points, so the representation cannot
// diverge between call sites.

// Addr converts an address for storage. A nil pointer becomes SQL NULL, preserving the
// difference between "no recipient" (contract creation) and "the zero address", which
// are distinct facts about a transaction.
func Addr(a *common.Address) []byte {
	if a == nil {
		return nil
	}
	b := make([]byte, common.AddressLength)
	copy(b, a[:])
	return b
}

// AddrVal converts a non-pointer address for storage.
func AddrVal(a common.Address) []byte {
	b := make([]byte, common.AddressLength)
	copy(b, a[:])
	return b
}

// Hash converts a hash for storage. A nil pointer becomes SQL NULL.
func Hash(h *common.Hash) []byte {
	if h == nil {
		return nil
	}
	b := make([]byte, common.HashLength)
	copy(b, h[:])
	return b
}

// HashVal converts a non-pointer hash for storage.
func HashVal(h common.Hash) []byte {
	b := make([]byte, common.HashLength)
	copy(b, h[:])
	return b
}

// ToAddr converts a stored value back into an address.
//
// Length is checked rather than assumed: the `address` domain enforces 20 bytes at the
// database, so a wrong length here means the value arrived by some route that bypassed
// it. Silently truncating or zero-padding would turn that into a plausible-looking but
// wrong address.
func ToAddr(b []byte) (*common.Address, error) {
	if b == nil {
		return nil, nil
	}
	if len(b) != common.AddressLength {
		return nil, fmt.Errorf("address column holds %d bytes, want %d", len(b), common.AddressLength)
	}
	var a common.Address
	copy(a[:], b)
	return &a, nil
}

// ToHash converts a stored value back into a hash.
func ToHash(b []byte) (*common.Hash, error) {
	if b == nil {
		return nil, nil
	}
	if len(b) != common.HashLength {
		return nil, fmt.Errorf("hash column holds %d bytes, want %d", len(b), common.HashLength)
	}
	var h common.Hash
	copy(h[:], b)
	return &h, nil
}
