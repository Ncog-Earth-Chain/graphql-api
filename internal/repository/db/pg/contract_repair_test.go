package pg

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// migrationUpSQL returns the Up body of a migration file, with goose's annotations removed.
//
// The test executes THE MIGRATION ITSELF rather than a transcription of it. A repair
// migration is a one-shot data change with no runtime code behind it, so a test asserting
// on a hand-copied version would keep passing after someone edited the real thing -- which
// is the only failure worth guarding against here.
func migrationUpSQL(t *testing.T, name string) string {
	t.Helper()

	path := filepath.Join("..", "migrate", "sql", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration %s: %v", name, err)
	}

	text := string(raw)
	start := strings.Index(text, "-- +goose Up")
	if start < 0 {
		t.Fatalf("migration %s has no Up section", name)
	}
	body := text[start:]
	if end := strings.Index(body, "-- +goose Down"); end >= 0 {
		body = body[:end]
	}

	var out []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "-- +goose") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// TestRepairSyntheticContracts covers 00014, which deletes contract rows written for
// addresses that are not contracts.
//
// The four cases are the ones the repair has to tell apart. Getting any of them wrong is
// worse than the bug: deleting a genesis contract or a verified one destroys data that
// cannot be recovered from the chain by re-scanning.
func TestRepairSyntheticContracts(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	cleanDB(t, s)

	// One block, and one transaction in it that deploys contract C.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO block (number, hash, parent_hash, miner, state_root,
		                   gas_limit, gas_used, size_bytes, ts, tx_count)
		VALUES (1, sha256('b1'::bytea), sha256('b0'::bytea),
		        substring(sha256('m'::bytea) for 20), sha256('s'::bytea),
		        20500000, 21000, 1024, now(), 1)`); err != nil {
		t.Fatalf("seed block: %v", err)
	}

	// created_contract is C. The sender E is a plain wallet.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO tx (hash, block_number, tx_index, block_hash, from_addr,
		                created_contract, value_wei, nonce, gas_limit, gas_price_wei,
		                input, tx_type, status, ts, is_ddb)
		VALUES (sha256('t1'::bytea), 1, 0, sha256('b1'::bytea),
		        substring(sha256('E'::bytea) for 20),
		        substring(sha256('C'::bytea) for 20),
		        0, 0, 21000, 1000000000, ''::bytea, 0, 1, now(), false)`); err != nil {
		t.Fatalf("seed tx: %v", err)
	}

	// Four contract rows, only one of which the repair may remove.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO contract (address, acct_type, deploy_tx, block_number, tx_index, deploy_seq, ts, is_ddb, is_verified)
		VALUES
		  -- (a) the real contract: address matches its deploy tx's created_contract
		  (substring(sha256('C'::bytea) for 20), 2, sha256('t1'::bytea), 1, 0, 0, now(), false, false),
		  -- (b) the bug's output: the SENDER, recorded against the same deploy tx
		  (substring(sha256('E'::bytea) for 20), 2, sha256('t1'::bytea), 1, 1, 0, now(), true,  false),
		  -- (c) a genesis/precompiled contract: its deploy tx is not in the index
		  (substring(sha256('G'::bytea) for 20), 2, sha256('unknown'::bytea), 1, 2, 0, now(), false, false),
		  -- (d) bogus BUT verified: carries human-supplied source, must be preserved
		  (substring(sha256('V'::bytea) for 20), 2, sha256('t1'::bytea), 1, 3, 0, now(), false, true)
		`); err != nil {
		t.Fatalf("seed contracts: %v", err)
	}

	if _, err := s.pool.Exec(ctx, migrationUpSQL(t, "00014_repair_synthetic_contracts.sql")); err != nil {
		t.Fatalf("run repair migration: %v", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT CASE
		         WHEN address = substring(sha256('C'::bytea) for 20) THEN 'C-real-contract'
		         WHEN address = substring(sha256('E'::bytea) for 20) THEN 'E-sender-eoa'
		         WHEN address = substring(sha256('G'::bytea) for 20) THEN 'G-genesis'
		         WHEN address = substring(sha256('V'::bytea) for 20) THEN 'V-verified'
		         ELSE 'unexpected'
		       END
		FROM contract ORDER BY 1`)
	if err != nil {
		t.Fatalf("read survivors: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var label string
		if err := rows.Scan(&label); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, label)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	want := []string{"C-real-contract", "G-genesis", "V-verified"}
	sort.Strings(got)
	sort.Strings(want)

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("survivors = %v, want %v\n"+
			"  E-sender-eoa must be deleted (it is the bug's output);\n"+
			"  G-genesis must survive (no indexed deploy tx);\n"+
			"  V-verified must survive (deleting it cascades away human-supplied source)",
			got, want)
	}
}
