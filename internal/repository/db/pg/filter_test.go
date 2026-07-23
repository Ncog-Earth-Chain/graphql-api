package pg

import (
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// TestFilterNumbersPlaceholdersFromOffset is the property that makes hand-numbering
// unnecessary: placeholders come from one counter, so they cannot drift out of step with
// the argument slice. A misnumbered placeholder binds a value to the wrong column and
// silently filters on the wrong thing -- a bug that returns plausible rows.
func TestFilterNumbersPlaceholdersFromOffset(t *testing.T) {
	f := NewFilter().Eq("a", 1).Eq("b", 2).Eq("c", 3)

	sql, args := f.Render(0)
	if want := "a = $1 AND b = $2 AND c = $3"; sql != want {
		t.Errorf("Render(0) = %q, want %q", sql, want)
	}
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(args))
	}

	// with an offset, as when a cursor's parameters come first
	sql, _ = f.Render(2)
	if want := "a = $3 AND b = $4 AND c = $5"; sql != want {
		t.Errorf("Render(2) = %q, want %q", sql, want)
	}
}

// TestFilterArgOrderMatchesPlaceholderOrder: the nth placeholder must bind the nth
// argument. This is the invariant a hand-numbered query gets wrong.
func TestFilterArgOrderMatchesPlaceholderOrder(t *testing.T) {
	f := NewFilter().Eq("first", "A").Eq("second", "B").Eq("third", "C")

	sql, args := f.Render(0)

	for i, want := range []string{"A", "B", "C"} {
		if args[i] != want {
			t.Errorf("arg %d = %v, want %v (sql: %s)", i+1, args[i], want, sql)
		}
	}
}

// TestEitherAddrBindsOnce covers the account-page predicate. Binding the address once
// keeps both sides provably identical and lets PostgreSQL see them as the same
// parameter.
func TestEitherAddrBindsOnce(t *testing.T) {
	addr := common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")

	sql, args := NewFilter().EitherAddr("from_addr", "to_addr", &addr).Render(0)

	if want := "(from_addr = $1 OR to_addr = $1)"; sql != want {
		t.Errorf("sql = %q, want %q", sql, want)
	}
	if len(args) != 1 {
		t.Errorf("expected the address bound once, got %d args", len(args))
	}
}

// TestIsNullReplacesTheBsonTypeCode documents what this replaces.
//
// "A withdrawal that has not been finalized" was written as {fin_trx: {$type: 10}} --
// BSON type code 10, meaning NULL. Reading it required knowing the BSON type table, and
// nothing in it said "not finalized".
func TestIsNullReplacesTheBsonTypeCode(t *testing.T) {
	sql, args := NewFilter().IsNull("fin_trx").Render(0)

	if want := "fin_trx IS NULL"; sql != want {
		t.Errorf("sql = %q, want %q", sql, want)
	}
	if len(args) != 0 {
		t.Errorf("IS NULL should bind no arguments, got %d", len(args))
	}
}

// TestEmptyFilterRendersNothing: a filter with no conditions must produce no WHERE
// clause, not "WHERE" or "WHERE TRUE".
func TestEmptyFilterRendersNothing(t *testing.T) {
	f := NewFilter()

	if !f.Empty() {
		t.Error("a fresh filter should be empty")
	}
	if sql, args := f.Where(0); sql != "" || args != nil {
		t.Errorf("empty filter rendered %q with %v", sql, args)
	}
}

// TestNilAddressIsIgnored: an absent optional filter must not add a condition matching
// the zero address, which would silently return the wrong rows.
func TestNilAddressIsIgnored(t *testing.T) {
	if sql, _ := NewFilter().EitherAddr("a", "b", nil).Render(0); sql != "" {
		t.Errorf("nil address produced a condition: %q", sql)
	}
}

// TestInIntsUsesAnyArray: ANY over an array keeps the query text stable regardless of
// how many values are supplied, so the plan is reusable. An IN list with one placeholder
// per element makes every distinct list length a different statement.
func TestInIntsUsesAnyArray(t *testing.T) {
	sql, args := NewFilter().InInts("tx_type", []int32{1, 2, 3}).Render(0)

	if want := "tx_type = ANY($1)"; sql != want {
		t.Errorf("sql = %q, want %q", sql, want)
	}
	if len(args) != 1 {
		t.Errorf("expected the slice bound as one argument, got %d", len(args))
	}

	// an empty set must add no condition rather than one that matches nothing
	if sql, _ := NewFilter().InInts("tx_type", nil).Render(0); sql != "" {
		t.Errorf("empty set produced a condition: %q", sql)
	}
}

// TestRawNumbersItsOwnPlaceholders checks that Raw participates in the shared counter
// rather than restarting at $1.
func TestRawNumbersItsOwnPlaceholders(t *testing.T) {
	f := NewFilter().
		Eq("a", 1).
		Raw("ts BETWEEN %s AND %s", "start", "end").
		Eq("b", 2)

	sql, args := f.Render(0)

	if want := "a = $1 AND ts BETWEEN $2 AND $3 AND b = $4"; sql != want {
		t.Errorf("sql = %q, want %q", sql, want)
	}
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d", len(args))
	}
	if args[1] != "start" || args[2] != "end" || args[3] != 2 {
		t.Errorf("argument order does not follow placeholder order: %v", args)
	}
}

// TestWherePrefixesOnlyWhenNonEmpty
func TestWherePrefixesOnlyWhenNonEmpty(t *testing.T) {
	sql, _ := NewFilter().Eq("a", 1).Where(0)
	if !strings.HasPrefix(sql, "WHERE ") {
		t.Errorf("Where did not prefix: %q", sql)
	}
}
