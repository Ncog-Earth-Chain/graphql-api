package pg

import (
	"context"
	"math/big"
	"ncogearthchain-api-graphql/internal/types"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// TestAccountTypeMappingIsTotal: every domain account type must survive a round trip
// through its stored code.
//
// A type stored as an integer is only safe if the mapping is total. A gap would surface
// as an account whose type cannot be decoded, and a default-to-wallet fallback would
// misreport a contract as an ordinary address -- a claim about the chain, not a display
// detail. This test is a pure unit test and needs no database.
func TestAccountTypeMappingIsTotal(t *testing.T) {
	all := []string{
		types.AccountTypeWallet,
		types.AccountTypeContract,
		types.AccountTypeSFC,
		types.AccountTypeERC20Token,
		types.AccountTypeERC721Contract,
		types.AccountTypeERC1155Contract,
	}

	seen := map[int16]string{}

	for _, name := range all {
		code, err := accountTypeCode(name)
		if err != nil {
			t.Errorf("no stored code for account type %q: %v", name, err)
			continue
		}

		if prev, dup := seen[code]; dup {
			t.Errorf("account types %q and %q share code %d; they would be indistinguishable once stored",
				prev, name, code)
		}
		seen[code] = name

		back, err := accountTypeName(code)
		if err != nil {
			t.Errorf("code %d (for %q) does not map back: %v", code, name, err)
			continue
		}
		if back != name {
			t.Errorf("round trip changed the type: %q -> %d -> %q", name, code, back)
		}
	}
}

// TestAccountTypeRejectsUnknown: an unrecognised value must be an error, never a silent
// default.
func TestAccountTypeRejectsUnknown(t *testing.T) {
	if _, err := accountTypeCode("not-a-real-type"); err == nil {
		t.Error("accountTypeCode accepted an unknown type instead of failing")
	}
	if _, err := accountTypeName(99); err == nil {
		t.Error("accountTypeName accepted an unknown code instead of failing")
	}
}

// TestAccountRoundTrip covers store and load.
func TestAccountRoundTrip(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	addr := common.HexToAddress("0xaaaa000000000000000000000000000000000001")
	scTx := common.HexToHash("0x1234567890123456789012345678901234567890123456789012345678901234")

	in := &types.Account{
		Address:    addr,
		Type:       types.AccountTypeERC20Token,
		ContractTx: &scTx,
	}
	if err := s.AddAccount(ctx, in); err != nil {
		t.Fatalf("add: %v", err)
	}

	got, err := s.Account(ctx, &addr)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got == nil {
		t.Fatal("account not found after adding it")
	}
	if got.Address != addr {
		t.Errorf("address: got %s want %s", got.Address, addr)
	}
	if got.Type != types.AccountTypeERC20Token {
		t.Errorf("type: got %q want %q", got.Type, types.AccountTypeERC20Token)
	}
	if got.ContractTx == nil || *got.ContractTx != scTx {
		t.Errorf("contract tx: got %v want %s", got.ContractTx, scTx)
	}
}

// TestUnknownAccountIsNotAnError: the caller synthesises a wallet account for an address
// the indexer has never seen, so a miss must not be an error.
func TestUnknownAccountIsNotAnError(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)

	addr := common.HexToAddress("0xffff000000000000000000000000000000009999")

	got, err := s.Account(context.Background(), &addr)
	if err != nil {
		t.Errorf("an unknown account returned an error: %v", err)
	}
	if got != nil {
		t.Errorf("an unknown account returned %v", got)
	}

	known, err := s.IsAccountKnown(context.Background(), &addr)
	if err != nil {
		t.Errorf("IsAccountKnown errored: %v", err)
	}
	if known {
		t.Error("IsAccountKnown reported an unstored address as known")
	}
}

// TestAddAccountIsIdempotent: re-adding must not fail, because the ingest path can see
// the same address in many blocks.
func TestAddAccountIsIdempotent(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	addr := common.HexToAddress("0xaaaa000000000000000000000000000000000002")
	acc := &types.Account{Address: addr, Type: types.AccountTypeWallet}

	for i := 0; i < 3; i++ {
		if err := s.AddAccount(ctx, acc); err != nil {
			t.Fatalf("add pass %d: %v", i, err)
		}
	}

	var n int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM account WHERE address = $1", AddrVal(addr)).
		Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("three adds produced %d rows, want 1", n)
	}
}

// TestAccountWithNoTransactionsIsStillVisible pins the LEFT join.
//
// account_stat has no row for an account with no transaction edges. An INNER join would
// make such accounts vanish from the account page entirely -- they would look like
// addresses the explorer had never seen, even though it had stored them.
func TestAccountWithNoTransactionsIsStillVisible(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	addr := common.HexToAddress("0xaaaa000000000000000000000000000000000003")
	if err := s.AddAccount(ctx, &types.Account{Address: addr, Type: types.AccountTypeWallet}); err != nil {
		t.Fatalf("add: %v", err)
	}

	got, err := s.Account(ctx, &addr)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got == nil {
		t.Fatal("an account with no transactions disappeared; the join is not LEFT")
	}
	if uint64(got.TrxCounter) != 0 {
		t.Errorf("transaction counter = %d, want 0", uint64(got.TrxCounter))
	}
}

// TestAccountTransactionCountIsExact: the MongoDB equivalent counted an $or over the
// transaction collection with a 500 ms budget, and on timeout reported the WHOLE CHAIN's
// transaction count as the account's. Here it is an index-only scan of tx_account.
func TestAccountTransactionCountIsExact(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	alice := common.HexToAddress("0xaaaa000000000000000000000000000000000001")
	bob := common.HexToAddress("0xbbbb000000000000000000000000000000000002")
	carol := common.HexToAddress("0xcccc000000000000000000000000000000000003")

	// alice touches 2 of the 3 transactions
	if err := s.StoreBlock(ctx, &BlockData{
		Block: mkBlock(1),
		Transactions: []*types.Transaction{
			mkTx(1, 0, alice, bob, big.NewInt(1)),
			mkTx(1, 1, bob, alice, big.NewInt(2)),
			mkTx(1, 2, bob, carol, big.NewInt(3)),
		},
	}); err != nil {
		t.Fatalf("store: %v", err)
	}

	n, err := s.AccountTransactionCount(ctx, &alice)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("alice's transaction count = %d, want 2 (NOT the chain total of 3)", n)
	}
}
