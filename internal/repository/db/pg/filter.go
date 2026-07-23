package pg

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

// Typed query filters.
//
// These replace the bson.D filter documents that the repository layer used to build and
// hand down to the storage layer. That arrangement had two problems worth naming, since
// they explain why this is a type rather than a string:
//
//  1. It put MongoDB's query language ABOVE the storage seam, so the storage layer was
//     not swappable -- which is exactly the wall this migration hit.
//  2. It encoded domain meaning in encoding-level detail. The clearest example:
//     "a withdrawal that has not been finalized" was written as
//     {fin_trx: {$type: 10}} -- BSON type code 10, meaning NULL. Reading that requires
//     knowing the BSON type table, and nothing about it says "not finalized".
//
// Filters here carry domain meaning and render themselves to parameterised SQL. Values
// are always bound, never interpolated, so a filter cannot become an injection vector.

// Cond is one rendered predicate plus its bound arguments.
type Cond struct {
	SQL  string
	Args []any
}

// Filter accumulates conditions and renders a WHERE clause.
//
// Placeholder numbering is assigned at render time from a single counter, so callers
// never hand-number $1/$2 and cannot misalign them against the argument slice -- a
// mistake that binds a value to the wrong column and silently filters on the wrong
// thing.
type Filter struct {
	conds []func(next func() string) Cond
	args  []any
}

// NewFilter creates an empty filter, which renders to no WHERE clause at all.
func NewFilter() *Filter { return &Filter{} }

// Eq adds `col = value`.
func (f *Filter) Eq(col string, value any) *Filter {
	return f.binary(col, "=", value)
}

// Lt adds `col < value`.
func (f *Filter) Lt(col string, value any) *Filter { return f.binary(col, "<", value) }

// Lte adds `col <= value`.
func (f *Filter) Lte(col string, value any) *Filter { return f.binary(col, "<=", value) }

// Gt adds `col > value`.
func (f *Filter) Gt(col string, value any) *Filter { return f.binary(col, ">", value) }

// Gte adds `col >= value`.
func (f *Filter) Gte(col string, value any) *Filter { return f.binary(col, ">=", value) }

func (f *Filter) binary(col, op string, value any) *Filter {
	f.conds = append(f.conds, func(next func() string) Cond {
		return Cond{SQL: col + " " + op + " " + next(), Args: []any{value}}
	})
	return f
}

// IsNull adds `col IS NULL`.
//
// This is the honest spelling of what the Mongo code wrote as {$type: 10}. Call sites
// should wrap it in a domain-named helper (NotFinalized, and so on) so the meaning is
// visible at the point of use.
func (f *Filter) IsNull(col string) *Filter {
	f.conds = append(f.conds, func(func() string) Cond {
		return Cond{SQL: col + " IS NULL"}
	})
	return f
}

// IsNotNull adds `col IS NOT NULL`.
func (f *Filter) IsNotNull(col string) *Filter {
	f.conds = append(f.conds, func(func() string) Cond {
		return Cond{SQL: col + " IS NOT NULL"}
	})
	return f
}

// EitherAddr adds `(a = $n OR b = $n)` binding the address ONCE.
//
// This is the account-page predicate: transactions where an address is either sender or
// recipient. Binding the value once matters -- PostgreSQL can then recognise both sides
// as the same parameter, and it removes any chance of the two sides drifting apart.
//
// Note that on the transaction table this filter is usually the wrong tool: the tx_account
// edge table exists precisely so an account's transactions are one index scan rather than
// an OR across two indexes. Use this only where no edge table exists.
func (f *Filter) EitherAddr(colA, colB string, addr *common.Address) *Filter {
	if addr == nil {
		return f
	}
	v := AddrVal(*addr)
	f.conds = append(f.conds, func(next func() string) Cond {
		p := next()
		return Cond{SQL: "(" + colA + " = " + p + " OR " + colB + " = " + p + ")", Args: []any{v}}
	})
	return f
}

// InInts adds `col = ANY($n)`.
//
// ANY over an array beats an IN list with one placeholder per element: the query text is
// stable regardless of how many values are supplied, so PostgreSQL can reuse the plan
// instead of treating every distinct list length as a new statement.
func (f *Filter) InInts(col string, values []int32) *Filter {
	if len(values) == 0 {
		return f
	}
	f.conds = append(f.conds, func(next func() string) Cond {
		return Cond{SQL: col + " = ANY(" + next() + ")", Args: []any{values}}
	})
	return f
}

// Raw adds a pre-written predicate with its own bound arguments.
//
// The SQL must use %s in place of each placeholder, in argument order; render fills them
// in with correctly numbered parameters. This exists for shapes the helpers above do not
// cover -- it is not an escape hatch for interpolating values, which must still be bound.
func (f *Filter) Raw(sqlWithPlaceholders string, args ...any) *Filter {
	f.conds = append(f.conds, func(next func() string) Cond {
		holders := make([]any, len(args))
		for i := range args {
			holders[i] = next()
		}
		return Cond{SQL: fmt.Sprintf(sqlWithPlaceholders, holders...), Args: args}
	})
	return f
}

// Empty reports whether the filter would render no conditions.
func (f *Filter) Empty() bool { return f == nil || len(f.conds) == 0 }

// Render produces the conditions and their arguments, numbering placeholders from
// argOffset+1.
//
// Returns the bare conjunction without a leading WHERE, so callers can compose it with
// other predicates (a keyset cursor, most often).
func (f *Filter) Render(argOffset int) (string, []any) {
	if f.Empty() {
		return "", nil
	}

	n := argOffset
	next := func() string {
		n++
		return "$" + strconv.Itoa(n)
	}

	parts := make([]string, 0, len(f.conds))
	args := make([]any, 0, len(f.conds))
	for _, c := range f.conds {
		cond := c(next)
		parts = append(parts, cond.SQL)
		args = append(args, cond.Args...)
	}

	return strings.Join(parts, " AND "), args
}

// Where renders a complete WHERE clause, or the empty string when there are no
// conditions.
func (f *Filter) Where(argOffset int) (string, []any) {
	sql, args := f.Render(argOffset)
	if sql == "" {
		return "", nil
	}
	return "WHERE " + sql, args
}
