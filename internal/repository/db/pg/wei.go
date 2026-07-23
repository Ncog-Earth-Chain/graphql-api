// Package pg holds the PostgreSQL persistence layer for the explorer.
package pg

import (
	"fmt"
	"math/big"

	"github.com/jackc/pgx/v5/pgtype"
)

// maxUint256 is 2^256, the exclusive upper bound for any on-chain money value.
// It matches the CHECK on the uint256 domain in 00001_domains_meta.sql exactly.
var maxUint256 = new(big.Int).Lsh(big.NewInt(1), 256)

// Wei converts a big.Int money value into a pgtype.Numeric suitable for a uint256
// column.
//
// This is the single write-side gate on money. Every path that stores an amount goes
// through it, because the failure it prevents is silent: the uint256 domain rejects
// NaN and out-of-range values at the database, but a Go-side bug that produced them
// would surface as an opaque constraint violation deep inside a batch, attributed to
// whichever row happened to carry it. Checking here attributes the error to the value.
//
// It also refuses to guess. A nil *big.Int is NULL, not zero -- under MongoDB the
// distinction was lost, and "no value recorded" became "the value was zero", which is a
// different and wrong claim about the chain.
//
// Precision: pgtype.Numeric holds an arbitrary-precision Int with an exponent, so a
// 256-bit integer round-trips exactly. This never goes through float64, which carries
// 53 bits of mantissa and would silently corrupt any value above ~9e15 -- the exact
// defect the MongoDB schema had, where amounts were stored twice and only the lossy
// copy was aggregatable.
func Wei(v *big.Int) (pgtype.Numeric, error) {
	if v == nil {
		return pgtype.Numeric{Valid: false}, nil
	}
	if v.Sign() < 0 {
		return pgtype.Numeric{}, fmt.Errorf("negative money value %s: on-chain amounts are unsigned", v)
	}
	if v.Cmp(maxUint256) >= 0 {
		return pgtype.Numeric{}, fmt.Errorf("money value %s exceeds 2^256-1 and cannot be an on-chain amount", v)
	}

	// Copy: pgtype.Numeric retains the pointer, and callers routinely reuse the
	// big.Int they pass (hexutil.Big fields are mutated in place during decoding).
	// Without this, a later mutation would rewrite a value already queued in a batch.
	return pgtype.Numeric{Int: new(big.Int).Set(v), Exp: 0, Valid: true}, nil
}

// MustWei is Wei for values already known to be in range, such as constants and
// counters computed locally. It panics rather than returning an error.
//
// Do NOT use it on chain-derived input.
func MustWei(v *big.Int) pgtype.Numeric {
	n, err := Wei(v)
	if err != nil {
		panic(fmt.Sprintf("MustWei: %v", err))
	}
	return n
}

// FromWei converts a uint256 column value back into a *big.Int.
//
// Returns nil for SQL NULL, preserving the "not recorded" vs "zero" distinction on the
// way out as well as in.
func FromWei(n pgtype.Numeric) (*big.Int, error) {
	if !n.Valid {
		return nil, nil
	}
	if n.NaN {
		// Unreachable through this package's write path and rejected by the domain
		// CHECK, so reaching it means data arrived by some other route.
		return nil, fmt.Errorf("money column holds NaN; the uint256 domain CHECK should have made this impossible")
	}
	if n.Int == nil {
		return nil, fmt.Errorf("money column is valid but carries no integer")
	}

	// Exp is 0 for anything this package wrote, but a value loaded from a column
	// written by hand, or by a migration, can carry one. Scaling explicitly beats
	// returning a number that is wrong by a factor of ten.
	switch {
	case n.Exp == 0:
		return new(big.Int).Set(n.Int), nil
	case n.Exp > 0:
		mul := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n.Exp)), nil)
		return new(big.Int).Mul(n.Int, mul), nil
	default:
		div := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-n.Exp)), nil)
		q, r := new(big.Int).QuoRem(n.Int, div, new(big.Int))
		if r.Sign() != 0 {
			return nil, fmt.Errorf("money column holds a fractional value (%v); wei is an integer", n)
		}
		return q, nil
	}
}
