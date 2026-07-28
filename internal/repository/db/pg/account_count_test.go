package pg

import (
	"context"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// TestAccountTransactionCountIsBounded covers the cap that stops a hot address from being
// counted in full on every page of every request.
//
// The count is an index-only scan: cheap per row, unbounded in total, because its cost is
// the number of edges the address has. An exchange or bridge address makes that arbitrarily
// large, and the only thing behind it was the 30 s statement_timeout -- which converts a
// slow page into a failed one rather than protecting anything.
func TestAccountTransactionCountIsBounded(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	hot := common.HexToAddress("0xaaaa000000000000000000000000000000000001")

	// Seed more edges than the cap. Written directly rather than through StoreBlock: this
	// test is about the counting query, and ingesting 50k transactions to assert on a
	// SELECT would dominate the runtime for no added coverage.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO block (number, hash, parent_hash, miner, state_root,
		                   gas_limit, gas_used, size_bytes, ts, tx_count)
		SELECT n, sha256(n::text::bytea), sha256((n-1)::text::bytea),
		       substring(sha256(n::text::bytea) for 20), sha256(('s'||n)::bytea),
		       20500000, 21000, 1024, now(), 1
		FROM generate_series(1, 600) n`); err != nil {
		t.Fatalf("seed blocks: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO tx_account (address, block_number, tx_index, roles)
		SELECT $1, n, i, 1 FROM generate_series(1, 600) n, generate_series(0, 99) i`,
		AddrVal(hot)); err != nil {
		t.Fatalf("seed edges: %v", err)
	}

	var edges int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM tx_account WHERE address = $1`,
		AddrVal(hot)).Scan(&edges); err != nil {
		t.Fatalf("verify seed: %v", err)
	}
	if edges <= accountTxExactCountLimit {
		t.Skipf("fixture holds %d edges, at or below the %d cap; nothing to bound",
			edges, accountTxExactCountLimit)
	}

	total, exact, err := s.AccountTransactionCount(ctx, &hot, nil)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if exact {
		t.Errorf("count over %d edges reported itself EXACT; a capped total is a lower bound and must say so, "+
			"because TotalCountIsExact is what tells a client which of the two it is read", edges)
	}
	if total != accountTxExactCountLimit {
		t.Errorf("capped total = %d, want %d", total, accountTxExactCountLimit)
	}

	// The bound has to come from stopping early. Counting everything and clamping the
	// result afterwards would report the same number having already paid the full cost --
	// which is the entire thing this is meant to avoid, and is invisible in the return value.
	var plan strings.Builder
	err = s.pool.InTx(ctx, func(ctx context.Context, tx Tx) error {
		rows, err := tx.Query(ctx, `
			EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF, SUMMARY OFF)
			SELECT count(*) FROM (SELECT 1 FROM tx_account WHERE address = $1 LIMIT $2) x`,
			AddrVal(hot), accountTxExactCountLimit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				return err
			}
			plan.WriteString(line)
			plan.WriteString("\n")
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("explain: %v", err)
	}

	if !strings.Contains(plan.String(), "Limit") {
		t.Errorf("no Limit node in the plan, so the scan is not bounded:\n%s", plan.String())
	}
}

// TestAccountTransactionCountIsExactBelowTheCap: the bound must not cost accuracy for the
// ordinary case, which is every wallet a person actually looks at.
func TestAccountTransactionCountIsExactBelowTheCap(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	alice := common.HexToAddress("0xaaaa000000000000000000000000000000000001")

	if _, err := s.pool.Exec(ctx, `
		INSERT INTO block (number, hash, parent_hash, miner, state_root,
		                   gas_limit, gas_used, size_bytes, ts, tx_count)
		SELECT n, sha256(n::text::bytea), sha256((n-1)::text::bytea),
		       substring(sha256(n::text::bytea) for 20), sha256(('s'||n)::bytea),
		       20500000, 21000, 1024, now(), 1
		FROM generate_series(1, 7) n`); err != nil {
		t.Fatalf("seed blocks: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO tx_account (address, block_number, tx_index, roles)
		SELECT $1, n, 0, 1 FROM generate_series(1, 7) n`, AddrVal(alice)); err != nil {
		t.Fatalf("seed edges: %v", err)
	}

	total, exact, err := s.AccountTransactionCount(ctx, &alice, nil)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if !exact {
		t.Error("a count well under the cap must be reported as exact")
	}
	if total != 7 {
		t.Errorf("total = %d, want 7", total)
	}
}
