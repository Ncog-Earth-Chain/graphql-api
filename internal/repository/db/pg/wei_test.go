package pg

import (
	"math/big"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// TestWeiRoundTripsExactly is the property that matters: a 256-bit value must survive
// storage and retrieval with every bit intact.
//
// This is the defect the MongoDB schema had. Amounts were stored twice -- an exact hex
// string for display and an int64 for arithmetic -- and only the lossy copy was
// aggregatable, so every sum over a value above 2^63 was silently wrong. big.Int.Int64()
// on a larger value returns garbage rather than failing.
func TestWeiRoundTripsExactly(t *testing.T) {
	cases := []string{
		"0",
		"1",
		"1000000000000000000",  // 1 NEC
		"9223372036854775807",  // MaxInt64
		"9223372036854775808",  // MaxInt64 + 1: the float64/int64 cliff
		"18446744073709551615", // MaxUint64
		"18446744073709551616", // MaxUint64 + 1
		"115792089237316195423570985008687907853269984665640564039457584007913129639935", // 2^256-1
	}

	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			in, ok := new(big.Int).SetString(s, 10)
			if !ok {
				t.Fatalf("bad fixture %q", s)
			}

			n, err := Wei(in)
			if err != nil {
				t.Fatalf("Wei(%s): %v", s, err)
			}

			out, err := FromWei(n)
			if err != nil {
				t.Fatalf("FromWei: %v", err)
			}
			if out.Cmp(in) != 0 {
				t.Errorf("round-trip changed the value: %s -> %s", in, out)
			}
			if out.String() != s {
				t.Errorf("round-trip changed the decimal form: %s -> %s", s, out)
			}
		})
	}
}

// TestWeiRejectsOutOfRange covers the bounds the uint256 domain enforces in SQL. The Go
// side checks them too so the error names the offending value rather than surfacing as
// an opaque constraint violation from somewhere inside a batch.
func TestWeiRejectsOutOfRange(t *testing.T) {
	// exactly 2^256 -- one past the largest representable amount
	tooBig := new(big.Int).Lsh(big.NewInt(1), 256)
	if _, err := Wei(tooBig); err == nil {
		t.Error("accepted 2^256, which no on-chain amount can be")
	}

	if _, err := Wei(big.NewInt(-1)); err == nil {
		t.Error("accepted a negative amount; on-chain values are unsigned")
	}

	// the largest legal value must still be accepted -- an off-by-one here would
	// reject real transactions
	max := new(big.Int).Sub(tooBig, big.NewInt(1))
	if _, err := Wei(max); err != nil {
		t.Errorf("rejected 2^256-1, which is a legal amount: %v", err)
	}
}

// TestWeiNilIsNullNotZero pins the distinction MongoDB lost: "no value recorded" and
// "the value was zero" are different claims about the chain, and conflating them makes
// a missing amount look like a zero-value transaction.
func TestWeiNilIsNullNotZero(t *testing.T) {
	n, err := Wei(nil)
	if err != nil {
		t.Fatalf("Wei(nil): %v", err)
	}
	if n.Valid {
		t.Error("nil became a non-NULL value")
	}

	back, err := FromWei(n)
	if err != nil {
		t.Fatalf("FromWei(NULL): %v", err)
	}
	if back != nil {
		t.Errorf("NULL came back as %v, want nil", back)
	}

	// and zero must remain distinguishable from it
	z, err := Wei(big.NewInt(0))
	if err != nil {
		t.Fatalf("Wei(0): %v", err)
	}
	if !z.Valid {
		t.Error("zero became NULL")
	}
}

// TestWeiCopiesInput guards a real aliasing hazard: pgtype.Numeric retains the pointer
// it is given, and callers reuse big.Int values (hexutil.Big fields are mutated in
// place while decoding). Without a copy, mutating the source after queueing a row would
// rewrite a value already sitting in a batch.
func TestWeiCopiesInput(t *testing.T) {
	src := big.NewInt(1000)

	n, err := Wei(src)
	if err != nil {
		t.Fatalf("Wei: %v", err)
	}

	src.SetInt64(9999) // simulate the caller reusing the value

	out, err := FromWei(n)
	if err != nil {
		t.Fatalf("FromWei: %v", err)
	}
	if out.Int64() != 1000 {
		t.Errorf("mutating the source changed the stored value: got %s, want 1000", out)
	}
}

// TestFromWeiRejectsNaN documents the belt-and-braces case.
//
// NaN is the reason the uint256 domain has an upper bound rather than only VALUE >= 0:
// in PostgreSQL NaN sorts ABOVE every numeric, so 'NaN'::numeric >= 0 is TRUE and a
// lower-bound-only CHECK admits it. One NaN row turns every sum() over that column into
// NaN permanently and silently. The domain makes it unreachable; this makes it loud if
// it ever arrives by another route.
func TestFromWeiRejectsNaN(t *testing.T) {
	if _, err := FromWei(pgtype.Numeric{NaN: true, Valid: true}); err == nil {
		t.Error("FromWei accepted NaN")
	}
}

// TestFromWeiHandlesExponent covers values carrying a scale factor, which a column
// written by hand or by a migration can hold. Returning a number wrong by a power of
// ten would be worse than failing.
func TestFromWeiHandlesExponent(t *testing.T) {
	// 15 * 10^2 = 1500
	got, err := FromWei(pgtype.Numeric{Int: big.NewInt(15), Exp: 2, Valid: true})
	if err != nil {
		t.Fatalf("positive exponent: %v", err)
	}
	if got.Int64() != 1500 {
		t.Errorf("15e2 = %s, want 1500", got)
	}

	// 1500 * 10^-2 = 15, exact
	got, err = FromWei(pgtype.Numeric{Int: big.NewInt(1500), Exp: -2, Valid: true})
	if err != nil {
		t.Fatalf("negative exponent: %v", err)
	}
	if got.Int64() != 15 {
		t.Errorf("1500e-2 = %s, want 15", got)
	}

	// 15 * 10^-1 = 1.5, which is not an integer number of wei
	if _, err := FromWei(pgtype.Numeric{Int: big.NewInt(15), Exp: -1, Valid: true}); err == nil {
		t.Error("accepted a fractional wei value")
	}
}
