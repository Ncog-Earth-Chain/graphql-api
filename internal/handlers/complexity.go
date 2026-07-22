package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"ncogearthchain-api-graphql/internal/logger"
	"net/http"
	"strconv"
	"strings"
)

// ComplexityGuard rejects GraphQL queries whose estimated cost exceeds a budget,
// before any resolver runs.
//
// Why this exists on top of graphql.MaxDepth: depth bounds nesting, not width. A
// query only three levels deep can still ask for 250 transactions and then a
// per-transaction field that costs a node round-trip each, which is 250 trace
// executions from one HTTP POST. Cost has to be modelled as the product of list
// sizes along a path, not as the length of the path.
//
// The estimate is deliberately structural and conservative: it never executes
// anything and never consults the schema, so it cannot be tricked into doing work
// while deciding whether to do work. It over-estimates in some shapes (a field
// that is not a list but carries a `count` argument) and that is the safe
// direction to be wrong in.
type ComplexityGuard struct {
	max int
	log logger.Logger
}

// NewComplexityGuard creates a guard enforcing the given maximum estimated cost.
// A max of zero or less disables the check.
func NewComplexityGuard(max int, log logger.Logger) *ComplexityGuard {
	return &ComplexityGuard{max: max, log: log}
}

// fieldWeights assigns extra cost to fields known to be far more expensive than a
// single field resolution. Each of these costs at least one JSON-RPC round-trip to
// the node, and the trace family re-executes transactions on the node.
var fieldWeights = map[string]int{
	// re-executes the transaction on the node
	"internalTransactions": 100,

	// one uncached node round-trip per item (two, where a receipt is also needed)
	"txList":       2,
	"transaction":  2,
	"block":        2,
	"parent":       2,
	"contract":     2,
	"erc20TokenList": 2,

	// live contract calls, one per asset
	"tokenSummaries": 20,
	"balanceOf":      5,

	// node-local Postgres reads
	"ddbSelect": 10,
	"ddbQuery":  10,
}

// listArgs are the argument names that set how many items a field returns. The
// API encodes pagination direction in the SIGN of `count`, so magnitude is what
// matters here, not value.
var listArgs = map[string]bool{
	"count": true,
	"limit": true,
	"first": true,
	"last":  true,
}

// Handler wraps the next handler, rejecting over-budget queries with 400.
func (g *ComplexityGuard) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// nothing to weigh on a GET, and a disabled budget means pass-through
		if g.max <= 0 || r.Method != http.MethodPost || r.Body == nil {
			next.ServeHTTP(w, r)
			return
		}

		// Read the body so we can inspect it, then hand an identical copy onward.
		// MaxBytesReader upstream already bounds how much this can be.
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			http.Error(w, `{"errors":[{"message":"request body too large or unreadable"}]}`, http.StatusBadRequest)
			return
		}
		r.Body = ioutil.NopCloser(bytes.NewReader(body))

		// A malformed body is not our problem to report; let the GraphQL handler
		// produce its own well-formed error for it.
		for _, q := range extractQueries(body) {
			cost := EstimateCost(q)
			if cost > g.max {
				g.log.Warningf("rejected query with estimated cost %d (budget %d) from %s", cost, g.max, r.RemoteAddr)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"errors": []map[string]interface{}{{
						"message": fmt.Sprintf(
							"query is too expensive: estimated cost %d exceeds the limit of %d; request fewer items per list or fewer nested lists",
							cost, g.max),
						"extensions": map[string]interface{}{
							"code":          "QUERY_TOO_COMPLEX",
							"estimatedCost": cost,
							"maxCost":       g.max,
						},
					}},
				})
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// extractQueries pulls the query strings out of a GraphQL request body, handling
// both the single-operation and batched-array forms.
func extractQueries(body []byte) []string {
	type gqlRequest struct {
		Query string `json:"query"`
	}

	trimmed := bytes.TrimLeft(body, " \t\r\n")
	if len(trimmed) == 0 {
		return nil
	}

	if trimmed[0] == '[' {
		var batch []gqlRequest
		if err := json.Unmarshal(trimmed, &batch); err != nil {
			return nil
		}
		out := make([]string, 0, len(batch))
		for _, b := range batch {
			if b.Query != "" {
				out = append(out, b.Query)
			}
		}
		return out
	}

	var single gqlRequest
	if err := json.Unmarshal(trimmed, &single); err != nil || single.Query == "" {
		return nil
	}
	return []string{single.Query}
}

// EstimateCost returns a structural cost estimate for a GraphQL query document.
//
// The model: every selection set inherits a multiplier from its parent, scaled by
// the list size requested on the field that opened it. Each field costs its
// multiplier, times its weight if it is a known-expensive field. The total is the
// sum over all fields.
//
// So `blocks(count:25){edges{block{txList{hash}}}}` costs roughly
// 25 x (txList weight) x (per-item fields), which is the amplification that
// matters, while `block{hash}` costs almost nothing.
func EstimateCost(query string) int {
	toks := tokenize(query)

	total := 0

	// multipliers[i] is the multiplier in effect inside the i-th open brace
	multipliers := []int{1}
	current := func() int { return multipliers[len(multipliers)-1] }

	// list size carried by the most recently seen argument list, applied to the
	// selection set that opens right after it
	pendingListSize := 1

	// weight of the most recently seen field name, likewise pending
	pendingWeight := 1

	for i := 0; i < len(toks); i++ {
		switch t := toks[i]; t.kind {
		case tokName:
			// A name is a field (or an alias, fragment keyword, type condition...).
			// Charging for the extras is harmless: they are bounded by document
			// size, which MaxBytesReader already limits.
			w := fieldWeights[t.text]
			if w == 0 {
				w = 1
			}
			pendingWeight = w
			total += current() * w

			// guard against a pathological document driving this to overflow
			if total < 0 || total > costCeiling {
				return costCeiling
			}

		case tokArgs:
			pendingListSize = listSizeOf(t.text)

		case tokBraceOpen:
			next := current() * pendingListSize
			if pendingWeight > 1 {
				// an expensive field's children are expensive too
				next *= pendingWeight
			}
			if next < 1 || next > costCeiling {
				next = costCeiling
			}
			multipliers = append(multipliers, next)
			pendingListSize = 1
			pendingWeight = 1

		case tokBraceClose:
			if len(multipliers) > 1 {
				multipliers = multipliers[:len(multipliers)-1]
			}
			pendingListSize = 1
			pendingWeight = 1
		}
	}

	return total
}

// costCeiling caps the arithmetic so a hostile document cannot overflow the
// estimate into a negative number and pass the budget check.
const costCeiling = 1 << 30

// listSizeOf extracts the requested list size from an argument list body, e.g.
// `count: 250, cursor: "0x1"` yields 250. Direction is encoded in the sign of
// `count`, so the magnitude is what is charged. A variable reference cannot be
// resolved here, so it is charged at a defensive default.
func listSizeOf(args string) int {
	size := 1

	for _, part := range splitArgs(args) {
		colon := strings.IndexByte(part, ':')
		if colon < 0 {
			continue
		}
		name := strings.TrimSpace(part[:colon])
		if !listArgs[name] {
			continue
		}

		val := strings.TrimSpace(part[colon+1:])

		// `count: $n` — the value lives in the variables map, which we do not
		// resolve. Charge a defensive default rather than 1.
		if strings.HasPrefix(val, "$") {
			if defaultVariableListSize > size {
				size = defaultVariableListSize
			}
			continue
		}

		n, err := strconv.Atoi(val)
		if err != nil {
			continue
		}
		if n < 0 {
			n = -n
		}
		if n > size {
			size = n
		}
	}

	return size
}

// defaultVariableListSize is what a `count: $var` is charged, since the guard
// does not resolve variables. It matches the API's own list cap so ordinary
// paginated clients are unaffected.
const defaultVariableListSize = 50

// splitArgs splits an argument list on commas and whitespace that sit at the top
// level, i.e. not inside a nested object, list or string.
func splitArgs(s string) []string {
	var parts []string
	var buf strings.Builder
	depth := 0
	inStr := false

	for i := 0; i < len(s); i++ {
		c := s[i]

		if inStr {
			buf.WriteByte(c)
			if c == '\\' && i+1 < len(s) {
				i++
				buf.WriteByte(s[i])
			} else if c == '"' {
				inStr = false
			}
			continue
		}

		switch c {
		case '"':
			inStr = true
			buf.WriteByte(c)
		case '{', '[':
			depth++
			buf.WriteByte(c)
		case '}', ']':
			depth--
			buf.WriteByte(c)
		case ',':
			if depth == 0 {
				parts = append(parts, buf.String())
				buf.Reset()
			} else {
				buf.WriteByte(c)
			}
		default:
			buf.WriteByte(c)
		}
	}
	if buf.Len() > 0 {
		parts = append(parts, buf.String())
	}
	return parts
}

// token kinds recognised by the cost estimator
const (
	tokName = iota
	tokArgs
	tokBraceOpen
	tokBraceClose
)

type token struct {
	kind int
	text string
}

// tokenize reduces a GraphQL document to the few token kinds the cost model needs.
//
// It must handle strings, block strings and comments properly: a `{` inside a
// string literal is not a selection set, and treating it as one would let a
// crafted query unbalance the multiplier stack and under-estimate its own cost.
func tokenize(q string) []token {
	var out []token

	for i := 0; i < len(q); i++ {
		c := q[i]

		switch {
		case c == '#':
			// comment runs to end of line
			for i < len(q) && q[i] != '\n' {
				i++
			}

		case strings.HasPrefix(q[i:], `"""`):
			// block string
			end := strings.Index(q[i+3:], `"""`)
			if end < 0 {
				return out
			}
			i += 3 + end + 2

		case c == '"':
			// ordinary string, honouring escapes
			i++
			for i < len(q) && q[i] != '"' {
				if q[i] == '\\' {
					i++
				}
				i++
			}

		case c == '(':
			// argument list, tracking nesting and strings so a ')' inside either
			// does not close it early
			depth := 1
			start := i + 1
			i++
			for i < len(q) && depth > 0 {
				switch q[i] {
				case '"':
					i++
					for i < len(q) && q[i] != '"' {
						if q[i] == '\\' {
							i++
						}
						i++
					}
				case '(':
					depth++
				case ')':
					depth--
				}
				i++
			}
			end := i - 1
			if end < start {
				end = start
			}
			out = append(out, token{kind: tokArgs, text: q[start:end]})
			i--

		case c == '{':
			out = append(out, token{kind: tokBraceOpen})

		case c == '}':
			out = append(out, token{kind: tokBraceClose})

		case isNameStart(c):
			start := i
			for i < len(q) && isNameChar(q[i]) {
				i++
			}
			out = append(out, token{kind: tokName, text: q[start:i]})
			i--
		}
	}

	return out
}

func isNameStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isNameChar(c byte) bool {
	return isNameStart(c) || (c >= '0' && c <= '9')
}
