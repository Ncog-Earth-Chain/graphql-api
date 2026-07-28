package pg

import (
	"context"
	"math/big"
	"ncogearthchain-api-graphql/internal/types"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// TestTransactionsByAccountRecipientFilter covers account.txList(recipient:).
//
// The argument used to be accepted by the schema, threaded through the resolver and the
// repository, and then dropped: the page and its total both described the account's entire
// history. A filter that silently does nothing is worse than one that errors, because the
// answer looks right.
//
// The count is asserted alongside the page deliberately. Narrowing only the page would
// swap one wrong answer for another -- a total describing a wider set than the rows it
// accompanies, with paging promising rows that never arrive.
func TestTransactionsByAccountRecipientFilter(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	alice := common.HexToAddress("0xaaaa000000000000000000000000000000000001")
	bob := common.HexToAddress("0xbbbb000000000000000000000000000000000002")
	carol := common.HexToAddress("0xcccc000000000000000000000000000000000003")

	// alice -> bob twice, alice -> carol once, bob -> alice once (alice as RECIPIENT),
	// and one self-transfer alice -> alice.
	if err := s.StoreBlock(ctx, &BlockData{
		Block: mkBlock(1),
		Transactions: []*types.Transaction{
			mkTx(1, 0, alice, bob, big.NewInt(100)),
			mkTx(1, 1, alice, bob, big.NewInt(150)),
			mkTx(1, 2, alice, carol, big.NewInt(200)),
			mkTx(1, 3, bob, alice, big.NewInt(300)),
			mkTx(1, 4, alice, alice, big.NewInt(400)),
		},
	}); err != nil {
		t.Fatalf("store: %v", err)
	}

	check := func(name string, addr common.Address, rec *common.Address, want int) {
		t.Helper()

		page, err := s.TransactionsByAccount(ctx, &addr, rec, "", 25)
		if err != nil {
			t.Fatalf("%s: page: %v", name, err)
		}
		total, _, err := s.AccountTransactionCount(ctx, &addr, rec)
		if err != nil {
			t.Fatalf("%s: count: %v", name, err)
		}

		if len(page) != want {
			t.Errorf("%s: page has %d transactions, want %d", name, len(page), want)
		}
		// The page and the total must describe the same set. Everything here fits in one
		// page, so the count is directly comparable.
		if int(total) != want {
			t.Errorf("%s: totalCount is %d but the page holds %d; the count must narrow with the filter",
				name, total, len(page))
		}
	}

	// Unfiltered: everything alice touches, in either role. Five transactions involve her,
	// but the self-transfer produces ONE edge, not two.
	check("alice, no filter", alice, nil, 5)

	// Filtered: only what alice SENT to bob. Excludes alice->carol and, critically,
	// bob->alice, which the unfiltered list does include.
	check("alice -> bob", alice, &bob, 2)

	check("alice -> carol", alice, &carol, 1)

	// A self-transfer is still a transaction alice sent to that address.
	check("alice -> alice", alice, &alice, 1)

	// A pair that never transacted must come back empty, not fall back to the full history.
	dave := common.HexToAddress("0xdddd000000000000000000000000000000000004")
	check("alice -> dave (never)", alice, &dave, 0)
}

// TestRecipientFilterUsesTheTwoPartyIndex guards the implementation, not just the answer.
//
// Filtering alice's edge list after the join would return the same rows while walking every
// edge she has -- unbounded for an exchange or system address. tx_from_to_idx exists for
// exactly this predicate and had no caller until now, so the plan is the thing worth
// pinning.
func TestRecipientFilterUsesTheTwoPartyIndex(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	alice := common.HexToAddress("0xaaaa000000000000000000000000000000000001")
	bob := common.HexToAddress("0xbbbb000000000000000000000000000000000002")

	// Enough rows, with a realistic shape, for the choice to mean something. A planner
	// assertion over a handful of rows proves nothing: every access path costs about the
	// same and the winner is arbitrary. Here alice sends widely and only rarely to bob,
	// which is the distribution that makes the two-party index worth having -- and the one
	// under which choosing any other index means reading far more rows than are returned.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO block (number, hash, parent_hash, miner, state_root,
		                   gas_limit, gas_used, size_bytes, ts, tx_count)
		SELECT n, sha256(n::text::bytea), sha256((n-1)::text::bytea),
		       substring(sha256(n::text::bytea) for 20), sha256(('s'||n)::bytea),
		       20500000, 21000, 1024, now() - (n || ' seconds')::interval, 5
		FROM generate_series(1, 400) n`); err != nil {
		t.Fatalf("seed blocks: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO tx (hash, block_number, tx_index, block_hash, from_addr, to_addr,
		                value_wei, nonce, gas_limit, gas_price_wei, input,
		                tx_type, status, ts, is_ddb)
		SELECT sha256((n || '-' || i)::bytea), n, i, sha256(n::text::bytea),
		       $1,
		       CASE WHEN (n * 5 + i) % 500 = 0 THEN $2
		            ELSE substring(sha256(('r'||n||i)::bytea) for 20) END,
		       1, i, 21000, 1000000000, ''::bytea, 0, 1,
		       now() - (n || ' seconds')::interval, false
		FROM generate_series(1, 400) n, generate_series(0, 4) i`,
		AddrVal(alice), AddrVal(bob)); err != nil {
		t.Fatalf("seed transactions: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `ANALYZE tx`); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	var plan strings.Builder
	err := s.pool.InTx(ctx, func(ctx context.Context, tx Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			EXPLAIN (COSTS OFF)
			SELECT hash FROM tx WHERE from_addr = $1 AND to_addr = $2
			ORDER BY block_number DESC, tx_index DESC LIMIT 26`,
			AddrVal(alice), AddrVal(bob))
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

	if !strings.Contains(plan.String(), "tx_from_to_idx") {
		t.Errorf("the two-party filter is not served by tx_from_to_idx; it will walk a wider set than it returns.\nplan:\n%s", plan.String())
	}
}
