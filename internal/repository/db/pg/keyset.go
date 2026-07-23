package pg

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// Keyset pagination.
//
// The existing API paginates by ordinal range rather than OFFSET, which is the right
// choice and is preserved here: OFFSET makes the database walk and discard every
// skipped row, so page 10,000 costs 10,000 pages of work. Keyset seeks straight to the
// cursor position via the index.
//
// Two things are done differently from the MongoDB implementation.
//
// First, the comparison and the ordering are generated from ONE column list. Keyset
// pagination is only correct when the WHERE predicate and the ORDER BY agree exactly on
// column order and direction; when they are written out separately they drift, and the
// symptom is subtle -- rows silently skipped or repeated at page boundaries, which
// looks like chain data being wrong rather than like a pagination bug.
//
// Second, cursors are opaque. The old cursors were bare hex ordinals that clients could
// read, construct and depend on, which freezes the ordering key as public API forever.

// SortDir is a per-column sort direction.
type SortDir bool

const (
	Asc  SortDir = false
	Desc SortDir = true
)

func (d SortDir) String() string {
	if d == Desc {
		return "DESC"
	}
	return "ASC"
}

// cmp returns the comparison operator that means "after this position" for the
// direction. Descending order advances toward smaller values.
func (d SortDir) cmp() string {
	if d == Desc {
		return "<"
	}
	return ">"
}

// KeyColumn is one column of a keyset ordering.
type KeyColumn struct {
	Name string
	Dir  SortDir
}

// Keyset describes a total ordering used for pagination.
//
// The final column must be unique, or the ordering is not total and pagination can
// repeat or skip rows at page boundaries. For transactions that is (block_number,
// tx_index); for lists keyed by a single ordinal it is that ordinal alone.
type Keyset struct {
	Columns []KeyColumn
}

// OrderBy renders the ORDER BY clause.
func (k Keyset) OrderBy(reverse bool) string {
	parts := make([]string, len(k.Columns))
	for i, c := range k.Columns {
		d := c.Dir
		if reverse {
			d = !d
		}
		parts[i] = c.Name + " " + d.String()
	}
	return "ORDER BY " + strings.Join(parts, ", ")
}

// After renders a row-value comparison selecting rows strictly after the cursor
// position, together with the arguments to bind.
//
// A row-value comparison -- (a, b) < ($1, $2) -- is used rather than the expanded
// disjunction a < $1 OR (a = $1 AND b < $2). Both are correct, but PostgreSQL can drive
// a multi-column index scan directly from the row-value form, while the disjunction
// often degrades into a bitmap OR or a filter. It is also impossible to get subtly
// wrong by hand, which the expanded form very much is.
//
// Row-value comparison requires every column to share a direction. Mixed-direction
// keysets are rejected rather than silently mis-paginated; nothing in this schema needs
// one, and an ordering that appears to work while dropping rows is worse than an error.
func (k Keyset) After(cursor []any, reverse bool, argOffset int) (string, []any, error) {
	if len(cursor) == 0 {
		return "", nil, nil
	}
	if len(cursor) != len(k.Columns) {
		return "", nil, fmt.Errorf("cursor has %d values, keyset has %d columns", len(cursor), len(k.Columns))
	}

	dir := k.Columns[0].Dir
	for _, c := range k.Columns[1:] {
		if c.Dir != dir {
			return "", nil, fmt.Errorf(
				"keyset %v mixes sort directions; row-value comparison requires a single direction", k.Columns)
		}
	}
	if reverse {
		dir = !dir
	}

	names := make([]string, len(k.Columns))
	holders := make([]string, len(k.Columns))
	for i, c := range k.Columns {
		names[i] = c.Name
		holders[i] = "$" + strconv.Itoa(argOffset+i+1)
	}

	return fmt.Sprintf("(%s) %s (%s)",
		strings.Join(names, ", "), dir.cmp(), strings.Join(holders, ", ")), cursor, nil
}

// EncodeCursor renders a cursor position as an opaque token.
//
// Opaque means clients cannot construct one or depend on its contents, which keeps the
// ordering key an implementation detail. The old cursors were bare hex ordinals, so the
// 14-bit-packed ordinal -- a defect -- had effectively become public API.
func EncodeCursor(values []int64) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = strconv.FormatInt(v, 10)
	}
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(parts, ":")))
}

// DecodeCursor parses a cursor token back into its values.
//
// want is the expected number of components. A cursor of the wrong shape is an error
// rather than a best-effort parse: a truncated cursor that silently paginated from the
// wrong position would look like missing chain data.
func DecodeCursor(s string, want int) ([]int64, error) {
	if s == "" {
		return nil, nil
	}

	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("malformed cursor")
	}

	parts := strings.Split(string(raw), ":")
	if len(parts) != want {
		return nil, fmt.Errorf("malformed cursor: expected %d components, found %d", want, len(parts))
	}

	out := make([]int64, len(parts))
	for i, p := range parts {
		v, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("malformed cursor: component %d is not a number", i)
		}
		out[i] = v
	}
	return out, nil
}

// Page bounds a requested page size.
//
// The API encodes direction in the SIGN of count, which is preserved. The magnitude is
// clamped: an unbounded count is a denial-of-service vector, and it is the caller's
// count that decides how much work the database does.
type Page struct {
	Limit   int
	Reverse bool
}

// NewPage interprets a signed count against a maximum.
func NewPage(count int32, max int) Page {
	reverse := count < 0
	n := int(count)
	if reverse {
		n = -n
	}
	if n <= 0 {
		n = 25
	}
	if n > max {
		n = max
	}
	return Page{Limit: n, Reverse: reverse}
}
