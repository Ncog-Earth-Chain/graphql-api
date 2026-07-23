package pg

import (
	"context"
	"math/big"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Integration tests against a real PostgreSQL.
//
// These exist because the unit tests prove the generated SQL is well-formed, not that
// PostgreSQL agrees with it. The properties that actually matter here -- that a 256-bit
// value survives a round trip through a NUMERIC(78,0) column, that the uint256 domain
// rejects NaN, and that a keyset predicate uses an index scan rather than a sort --
// can only be established by asking the server.
//
// Set EXPLORER_TEST_DSN to run them; they skip otherwise, so `go test ./...` stays
// green on a machine with no database.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("EXPLORER_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPLORER_TEST_DSN not set; skipping PostgreSQL integration test")
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return pool
}

// TestWeiRoundTripsThroughPostgres is the test the unit suite cannot be: it puts a
// 256-bit value through an actual NUMERIC(78,0) column.
func TestWeiRoundTripsThroughPostgres(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	cases := []string{
		"0",
		"1",
		"9223372036854775808", // MaxInt64 + 1
		"115792089237316195423570985008687907853269984665640564039457584007913129639935", // 2^256-1
	}

	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			in, _ := new(big.Int).SetString(s, 10)

			n, err := Wei(in)
			if err != nil {
				t.Fatalf("Wei: %v", err)
			}

			// cast through the real domain, so its CHECK participates
			var out string
			if err := pool.QueryRow(ctx, "SELECT ($1::uint256)::text", n).Scan(&out); err != nil {
				t.Fatalf("round trip through uint256: %v", err)
			}
			if out != s {
				t.Errorf("PostgreSQL returned %s, want %s", out, s)
			}
		})
	}
}

// TestUint256DomainRejectsNaN confirms at the server the reason the domain carries an
// upper bound rather than only VALUE >= 0.
//
// In PostgreSQL NaN sorts ABOVE every numeric, so 'NaN'::numeric >= 0 is TRUE. A
// lower-bound-only CHECK would admit it, and one NaN row turns every sum() over the
// column into NaN permanently and silently.
func TestUint256DomainRejectsNaN(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	// first: demonstrate the trap itself, so the reason for the bound is recorded
	var nanPassesLowerBound bool
	if err := pool.QueryRow(ctx, "SELECT 'NaN'::numeric >= 0").Scan(&nanPassesLowerBound); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !nanPassesLowerBound {
		t.Log("NOTE: 'NaN'::numeric >= 0 is false on this server; the upper bound may be redundant here")
	}

	// then: the domain must still reject it
	var v string
	err := pool.QueryRow(ctx, "SELECT ('NaN'::numeric)::uint256::text").Scan(&v)
	if err == nil {
		t.Errorf("uint256 accepted NaN (got %q); every sum() over such a column would be NaN forever", v)
	}
}

// TestUint256DomainRejectsOverflow confirms the upper bound is enforced server-side, so
// a writer that bypasses the Go codec still cannot store an impossible amount.
func TestUint256DomainRejectsOverflow(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	// 2^256 exactly -- one past the largest on-chain amount
	var v string
	err := pool.QueryRow(ctx,
		"SELECT ('115792089237316195423570985008687907853269984665640564039457584007913129639936'::numeric)::uint256::text").Scan(&v)
	if err == nil {
		t.Errorf("uint256 accepted 2^256 (got %q)", v)
	}
}

// TestKeysetPredicateUsesIndexScan checks the point of keyset pagination: that the
// generated predicate SEEKS through an index rather than reading and sorting the table.
//
// A predicate that is merely correct still ruins the feature if the planner cannot use
// it -- the query returns the right rows while reading the whole relation, which is what
// OFFSET pagination did and what this replaces.
//
// The table must be populated for this to mean anything. On an empty table PostgreSQL
// picks a sequential scan whatever the indexes say, and a sequential scan feeding an
// ORDER BY necessarily adds a Sort -- so asserting "no Sort" against an empty table is
// self-contradictory and can never pass. Rows are inserted inside a transaction that is
// always rolled back, so the test leaves no trace.
func TestKeysetPredicateUsesIndexScan(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 2,000 blocks with 5 transactions each: enough that an index scan is clearly
	// cheaper than a sort, without making the test slow.
	if _, err := tx.Exec(ctx, `
		INSERT INTO block (number, hash, parent_hash, miner, state_root,
		                   gas_limit, gas_used, size_bytes, ts, tx_count)
		SELECT n,
		       sha256(n::text::bytea), sha256((n-1)::text::bytea),
		       substring(sha256(n::text::bytea) for 20),
		       sha256(('s'||n)::bytea),
		       20500000, 21000, 1024,
		       now() - (n || ' seconds')::interval, 5
		FROM generate_series(1, 2000) n`); err != nil {
		t.Fatalf("seed blocks: %v", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO tx (hash, block_number, tx_index, block_hash, from_addr,
		                value_wei, nonce, gas_limit, gas_price_wei, input,
		                tx_type, status, ts, is_ddb)
		SELECT sha256((n || '-' || i)::bytea), n, i,
		       sha256(n::text::bytea),
		       substring(sha256(('f'||n)::bytea) for 20),
		       1000000000000000000, i, 21000, 1000000000, ''::bytea,
		       0, 1, now() - (n || ' seconds')::interval, false
		FROM generate_series(1, 2000) n, generate_series(0, 4) i`); err != nil {
		t.Fatalf("seed transactions: %v", err)
	}

	// The planner needs statistics; without ANALYZE it still works from defaults.
	if _, err := tx.Exec(ctx, "ANALYZE block, tx"); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	ks := Keyset{Columns: []KeyColumn{
		{Name: "block_number", Dir: Desc},
		{Name: "tx_index", Dir: Desc},
	}}

	pred, args, err := ks.After([]any{int64(1000), int64(0)}, false, 0)
	if err != nil {
		t.Fatalf("After: %v", err)
	}

	sql := "EXPLAIN (FORMAT TEXT) SELECT block_number, tx_index FROM tx WHERE " +
		pred + " " + ks.OrderBy(false) + " LIMIT 25"

	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		t.Fatalf("EXPLAIN failed -- the generated SQL is not valid: %v\nSQL: %s", err, sql)
	}

	var plan string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			rows.Close()
			t.Fatalf("scan plan: %v", err)
		}
		plan += line + "\n"
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("plan rows: %v", err)
	}

	t.Logf("plan:\n%s", plan)

	// The ordering must come from the index. A Sort means the query read rows and then
	// ordered them, which is the cost keyset pagination exists to avoid.
	if contains(plan, "Sort") {
		t.Errorf("keyset query plans a Sort; the ORDER BY is not served by the index:\n%s", plan)
	}
	if !contains(plan, "Index") {
		t.Errorf("keyset query does not use an index:\n%s", plan)
	}

	// And the row-value form must survive into the plan rather than being expanded.
	if !contains(plan, "ROW(") {
		t.Logf("note: the plan does not show a ROW() comparison; the planner may have rewritten it")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
