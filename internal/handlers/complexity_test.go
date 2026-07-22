package handlers

import "testing"

// TestEstimateCostOrdersQueriesCorrectly checks the property that actually matters: an
// amplifying query must cost more than a modest one, by a wide margin.
func TestEstimateCostOrdersQueriesCorrectly(t *testing.T) {
	cheap := `{ block(number: 100) { hash number timestamp } }`
	wide := `{ blocks(count: 25) { edges { block { txList { hash } } } } }`
	abusive := `{ transactions(count: 250) { edges { transaction { internalTransactions { from to value } } } } }`

	cCheap := EstimateCost(cheap)
	cWide := EstimateCost(wide)
	cAbusive := EstimateCost(abusive)

	t.Logf("cheap=%d wide=%d abusive=%d", cCheap, cWide, cAbusive)

	if !(cCheap < cWide && cWide < cAbusive) {
		t.Errorf("expected cheap < wide < abusive, got %d, %d, %d", cCheap, cWide, cAbusive)
	}
	if cCheap > 100 {
		t.Errorf("a single-block query should be cheap, got %d", cCheap)
	}
	if cAbusive <= 25000 {
		t.Errorf("250 transactions x internalTransactions should exceed the default budget, got %d", cAbusive)
	}
}

// TestEstimateCostCountsNestedListsMultiplicatively is the core of the model: nesting a
// list inside a list multiplies, it does not add.
func TestEstimateCostCountsNestedListsMultiplicatively(t *testing.T) {
	flat := `{ a(count: 10) { x } b(count: 10) { y } }`
	nested := `{ a(count: 10) { b(count: 10) { y } } }`

	if EstimateCost(nested) <= EstimateCost(flat) {
		t.Errorf("nested lists (%d) must cost more than sibling lists (%d)",
			EstimateCost(nested), EstimateCost(flat))
	}
}

// TestEstimateCostIgnoresBracesInStrings guards the tokenizer.
//
// If a `{` inside a string literal were treated as a selection set, the multiplier stack
// would unbalance and a crafted query could under-report its own cost -- which is exactly
// how a limit like this gets bypassed.
func TestEstimateCostIgnoresBracesInStrings(t *testing.T) {
	honest := `{ contracts(count: 100) { edges { contract { address } } } }`
	sneaky := `{ contracts(count: 100, cursor: "}}}}}}}}") { edges { contract { address } } } }`

	if EstimateCost(sneaky) < EstimateCost(honest) {
		t.Errorf("braces inside a string literal changed the estimate: honest=%d sneaky=%d",
			EstimateCost(honest), EstimateCost(sneaky))
	}
}

// TestEstimateCostHandlesCommentsAndBlockStrings makes sure the tokenizer does not
// mis-parse valid GraphQL syntax.
func TestEstimateCostHandlesCommentsAndBlockStrings(t *testing.T) {
	q := `{
		# this comment has { braces } and "quotes"
		contracts(count: 5) {
			edges { contract { address } }
		}
	}`
	if got := EstimateCost(q); got <= 0 {
		t.Errorf("a valid commented query should have positive cost, got %d", got)
	}
}

// TestEstimateCostChargesNegativeCountByMagnitude covers this API's convention of
// encoding pagination direction in the SIGN of `count`. A backwards page costs the same
// as a forwards one.
func TestEstimateCostChargesNegativeCountByMagnitude(t *testing.T) {
	fwd := `{ transactions(count: 100) { edges { transaction { hash } } } }`
	back := `{ transactions(count: -100) { edges { transaction { hash } } } }`

	if EstimateCost(fwd) != EstimateCost(back) {
		t.Errorf("direction should not change cost: forward=%d backward=%d",
			EstimateCost(fwd), EstimateCost(back))
	}
}

// TestEstimateCostChargesVariablesDefensively: a count supplied via a GraphQL variable
// cannot be resolved by a pre-execution pass, so it must not be charged as 1.
func TestEstimateCostChargesVariablesDefensively(t *testing.T) {
	variable := `query($n: Int) { transactions(count: $n) { edges { transaction { hash } } } }`
	literalOne := `{ transactions(count: 1) { edges { transaction { hash } } } }`

	if EstimateCost(variable) <= EstimateCost(literalOne) {
		t.Errorf("a variable count must be charged defensively: variable=%d literal1=%d",
			EstimateCost(variable), EstimateCost(literalOne))
	}
}

// TestEstimateCostDoesNotOverflow: a hostile document must not drive the estimate
// negative, which would make it compare as under budget.
func TestEstimateCostDoesNotOverflow(t *testing.T) {
	q := "{"
	for i := 0; i < 200; i++ {
		q += ` a(count: 1000) {`
	}
	q += ` x `
	for i := 0; i < 201; i++ {
		q += `}`
	}

	if got := EstimateCost(q); got < 0 {
		t.Fatalf("cost overflowed to a negative value: %d", got)
	}
}

// TestExtractQueriesHandlesBatches: a batched request must have every operation weighed,
// not just the first.
func TestExtractQueriesHandlesBatches(t *testing.T) {
	body := []byte(`[{"query":"{ a }"},{"query":"{ b }"}]`)
	got := extractQueries(body)
	if len(got) != 2 {
		t.Fatalf("expected 2 queries from a batch, got %d: %v", len(got), got)
	}
}

func TestExtractQueriesHandlesSingle(t *testing.T) {
	body := []byte(`{"query":"{ a }","variables":{}}`)
	got := extractQueries(body)
	if len(got) != 1 || got[0] != "{ a }" {
		t.Fatalf("unexpected single-query extraction: %v", got)
	}
}
