package pg

import (
	"strings"
	"testing"
)

// TestKeysetOrderAndPredicateAgree is the core invariant.
//
// Keyset pagination is correct only when the WHERE predicate and the ORDER BY agree on
// column order and direction. When they are written separately they drift, and the
// symptom is rows silently skipped or repeated at page boundaries -- which reads as
// missing chain data rather than as a pagination bug. Both are generated from one
// column list here; this asserts they actually match.
func TestKeysetOrderAndPredicateAgree(t *testing.T) {
	ks := Keyset{Columns: []KeyColumn{
		{Name: "block_number", Dir: Desc},
		{Name: "tx_index", Dir: Desc},
	}}

	for _, reverse := range []bool{false, true} {
		order := ks.OrderBy(reverse)
		pred, _, err := ks.After([]any{int64(100), int64(2)}, reverse, 0)
		if err != nil {
			t.Fatalf("After: %v", err)
		}

		// descending order pages toward smaller values, ascending toward larger
		wantOp := "<"
		wantDir := "DESC"
		if reverse {
			wantOp = ">"
			wantDir = "ASC"
		}

		if !strings.Contains(pred, ") "+wantOp+" (") {
			t.Errorf("reverse=%v: predicate %q does not use %q", reverse, pred, wantOp)
		}
		if !strings.Contains(order, "block_number "+wantDir) {
			t.Errorf("reverse=%v: order %q does not sort %s", reverse, order, wantDir)
		}

		// both must name the columns in the same sequence
		if strings.Index(pred, "block_number") > strings.Index(pred, "tx_index") {
			t.Errorf("predicate column order differs from the keyset: %q", pred)
		}
		if strings.Index(order, "block_number") > strings.Index(order, "tx_index") {
			t.Errorf("order column order differs from the keyset: %q", order)
		}
	}
}

// TestKeysetUsesRowValueComparison pins the generated form.
//
// A row-value comparison lets PostgreSQL drive a multi-column index scan directly. The
// expanded disjunction -- a < $1 OR (a = $1 AND b < $2) -- is logically equivalent but
// often degrades into a bitmap OR or a filter, and is easy to get subtly wrong by hand.
func TestKeysetUsesRowValueComparison(t *testing.T) {
	ks := Keyset{Columns: []KeyColumn{
		{Name: "block_number", Dir: Desc},
		{Name: "tx_index", Dir: Desc},
	}}

	pred, args, err := ks.After([]any{int64(100), int64(2)}, false, 0)
	if err != nil {
		t.Fatalf("After: %v", err)
	}

	if want := "(block_number, tx_index) < ($1, $2)"; pred != want {
		t.Errorf("predicate = %q, want %q", pred, want)
	}
	if len(args) != 2 {
		t.Errorf("expected 2 bind arguments, got %d", len(args))
	}
	if strings.Contains(pred, " OR ") {
		t.Error("predicate expanded into a disjunction instead of a row-value comparison")
	}
}

// TestKeysetRejectsMixedDirections: row-value comparison requires a single direction.
// A mixed keyset would produce a predicate that quietly disagrees with its ORDER BY and
// drop rows, so it must be an error rather than a plausible-looking query.
func TestKeysetRejectsMixedDirections(t *testing.T) {
	ks := Keyset{Columns: []KeyColumn{
		{Name: "a", Dir: Desc},
		{Name: "b", Dir: Asc},
	}}

	if _, _, err := ks.After([]any{int64(1), int64(2)}, false, 0); err == nil {
		t.Error("accepted a mixed-direction keyset, which cannot be expressed as a row-value comparison")
	}
}

// TestKeysetArgOffset checks placeholder numbering when the cursor follows other bound
// parameters. Getting this wrong binds the cursor to the wrong parameter and silently
// filters on the wrong value.
func TestKeysetArgOffset(t *testing.T) {
	ks := Keyset{Columns: []KeyColumn{{Name: "ordinal", Dir: Desc}}}

	pred, _, err := ks.After([]any{int64(7)}, false, 3)
	if err != nil {
		t.Fatalf("After: %v", err)
	}
	if want := "(ordinal) < ($4)"; pred != want {
		t.Errorf("predicate = %q, want %q", pred, want)
	}
}

// TestKeysetRejectsWrongCursorArity: a cursor whose component count does not match the
// keyset would bind the wrong number of parameters.
func TestKeysetRejectsWrongCursorArity(t *testing.T) {
	ks := Keyset{Columns: []KeyColumn{
		{Name: "block_number", Dir: Desc},
		{Name: "tx_index", Dir: Desc},
	}}

	if _, _, err := ks.After([]any{int64(1)}, false, 0); err == nil {
		t.Error("accepted a 1-component cursor for a 2-column keyset")
	}
}

// TestCursorRoundTrip covers the opaque cursor encoding.
func TestCursorRoundTrip(t *testing.T) {
	in := []int64{424242, 7}

	got, err := DecodeCursor(EncodeCursor(in), 2)
	if err != nil {
		t.Fatalf("DecodeCursor: %v", err)
	}
	if len(got) != 2 || got[0] != in[0] || got[1] != in[1] {
		t.Errorf("round-trip changed the cursor: %v -> %v", in, got)
	}
}

// TestCursorIsOpaque: the encoding must not be plain readable digits, so clients cannot
// construct cursors or come to depend on the ordering key. The old cursors were bare
// hex ordinals, which had effectively made a defective ordinal scheme public API.
func TestCursorIsOpaque(t *testing.T) {
	if c := EncodeCursor([]int64{424242}); strings.Contains(c, "424242") {
		t.Errorf("cursor %q exposes its underlying value", c)
	}
}

// TestDecodeCursorRejectsMalformed: a cursor that silently parsed from the wrong
// position would look like missing chain data, so every malformed shape must error.
func TestDecodeCursorRejectsMalformed(t *testing.T) {
	cases := []struct {
		name   string
		cursor string
		want   int
	}{
		{"not base64", "!!!not-base64!!!", 1},
		{"wrong component count", EncodeCursor([]int64{1, 2, 3}), 2},
		{"non-numeric component", EncodeCursor([]int64{1}) + "XX", 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := DecodeCursor(c.cursor, c.want); err == nil {
				t.Errorf("accepted malformed cursor %q", c.cursor)
			}
		})
	}

	// an empty cursor means "first page" and is not an error
	got, err := DecodeCursor("", 2)
	if err != nil || got != nil {
		t.Errorf("empty cursor should mean the first page, got %v, %v", got, err)
	}
}

// TestNewPageClampsAndReadsSign covers this API's convention of encoding pagination
// direction in the SIGN of count, and the clamp that keeps a client from choosing how
// much work the database does.
func TestNewPageClampsAndReadsSign(t *testing.T) {
	cases := []struct {
		count       int32
		max         int
		wantLimit   int
		wantReverse bool
	}{
		{25, 100, 25, false},
		{-25, 100, 25, true},
		{5000, 100, 100, false}, // clamped
		{-5000, 100, 100, true}, // clamped, direction preserved
		{0, 100, 25, false},     // default
	}

	for _, c := range cases {
		got := NewPage(c.count, c.max)
		if got.Limit != c.wantLimit || got.Reverse != c.wantReverse {
			t.Errorf("NewPage(%d, %d) = {%d, %v}, want {%d, %v}",
				c.count, c.max, got.Limit, got.Reverse, c.wantLimit, c.wantReverse)
		}
	}
}
