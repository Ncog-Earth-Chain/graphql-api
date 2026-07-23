package pg

import (
	"context"
	"errors"
	"fmt"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/jackc/pgx/v5"
)

// Account reads and writes.
//
// The account type is stored as a SMALLINT rather than the domain's string. Strings in a
// column that every account row carries cost storage and index size for no benefit, and
// they admit values the code does not define -- a typo becomes a new account type rather
// than an error.
//
// The mapping is TOTAL in both directions and unknown values are rejected rather than
// defaulted. Silently mapping an unrecognised type to `wallet` would misreport a contract
// as an ordinary address, which is a claim about the chain rather than a display detail.

// account type codes. These are persisted, so the numbers are part of the on-disk format
// and must never be reordered or reused -- only appended to.
const (
	acctWallet   int16 = 0
	acctContract int16 = 1
	acctSFC      int16 = 2
	acctERC20    int16 = 3
	acctERC721   int16 = 4
	acctERC1155  int16 = 5
)

// accountTypeCode maps the domain string onto its stored code.
func accountTypeCode(s string) (int16, error) {
	switch s {
	case types.AccountTypeWallet:
		return acctWallet, nil
	case types.AccountTypeContract:
		return acctContract, nil
	case types.AccountTypeSFC:
		return acctSFC, nil
	case types.AccountTypeERC20Token:
		return acctERC20, nil
	case types.AccountTypeERC721Contract:
		return acctERC721, nil
	case types.AccountTypeERC1155Contract:
		return acctERC1155, nil
	default:
		return 0, fmt.Errorf("unknown account type %q", s)
	}
}

// accountTypeName maps a stored code back onto the domain string.
func accountTypeName(c int16) (string, error) {
	switch c {
	case acctWallet:
		return types.AccountTypeWallet, nil
	case acctContract:
		return types.AccountTypeContract, nil
	case acctSFC:
		return types.AccountTypeSFC, nil
	case acctERC20:
		return types.AccountTypeERC20Token, nil
	case acctERC721:
		return types.AccountTypeERC721Contract, nil
	case acctERC1155:
		return types.AccountTypeERC1155Contract, nil
	default:
		return "", fmt.Errorf("unknown account type code %d", c)
	}
}

// Account loads one account.
//
// Returns (nil, nil) when absent. The caller relies on that to synthesise a plain wallet
// account for an address the indexer has never seen, so a not-found must not be an error.
//
// tx_count and last_activity come from the account_stat materialized view via a LEFT
// join. The join must be LEFT: the view has no row for an account with no transaction
// edges, and an inner join would make such accounts disappear from the account page
// entirely.
func (s *Store) Account(ctx context.Context, addr *common.Address) (*types.Account, error) {
	if addr == nil {
		return nil, fmt.Errorf("no account address given")
	}

	row := s.pool.QueryRow(ctx, `
		SELECT a.address, a.acct_type, a.sc_tx_hash,
		       COALESCE(st.tx_count, 0),
		       COALESCE(EXTRACT(EPOCH FROM st.last_activity)::BIGINT, 0)
		FROM   account a
		LEFT   JOIN account_stat st ON st.address = a.address
		WHERE  a.address = $1`, AddrVal(*addr))

	acc, err := scanAccount(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("can not load account %s: %w", addr.String(), err)
	}
	return acc, nil
}

// AddAccount stores an account if it is not already known.
//
// DO NOTHING rather than DO UPDATE, which preserves the MongoDB behaviour exactly: the
// caller short-circuits on a known account, so the type is frozen at first sighting. An
// address first seen as a plain transfer recipient stays `wallet` even after it is later
// detected as an ERC-20 contract.
//
// That is arguably wrong, but it is EXISTING behaviour and changing it here would mean a
// port that silently alters what the explorer reports. It is called out so it can be
// fixed deliberately, as its own change, with its own reasoning.
func (s *Store) AddAccount(ctx context.Context, acc *types.Account) error {
	if acc == nil {
		return fmt.Errorf("can not add an empty account")
	}

	code, err := accountTypeCode(acc.Type)
	if err != nil {
		return fmt.Errorf("account %s: %w", acc.Address.String(), err)
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO account (address, acct_type, sc_tx_hash, first_block)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (address) DO NOTHING`,
		AddrVal(acc.Address), code, Hash(acc.ContractTx), nil)
	if err != nil {
		return fmt.Errorf("can not store account %s: %w", acc.Address.String(), err)
	}
	return nil
}

// IsAccountKnown reports whether an account exists.
//
// A false result is not an error -- most addresses a client asks about will not be
// stored.
func (s *Store) IsAccountKnown(ctx context.Context, addr *common.Address) (bool, error) {
	if addr == nil {
		return false, fmt.Errorf("no account address given")
	}

	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM account WHERE address = $1)`,
		AddrVal(*addr)).Scan(&exists); err != nil {
		return false, fmt.Errorf("can not check account %s: %w", addr.String(), err)
	}
	return exists, nil
}

// AccountCount returns an approximate number of known accounts.
//
// Approximate by design, matching the MongoDB behaviour: it fed EstimatedDocumentCount,
// not a count. An exact count(*) walks the whole table for a headline figure where being
// current to the last few rows does not matter. If exactness is ever needed, maintain a
// meta_counter inside the ingest transaction rather than swapping in count(*) here.
func (s *Store) AccountCount(ctx context.Context) (uint64, error) {
	var n int64
	// reltuples is -1 before the first ANALYZE on PostgreSQL 14+, hence the clamp
	if err := s.pool.QueryRow(ctx, `
		SELECT GREATEST(reltuples, 0)::BIGINT FROM pg_class WHERE oid = 'account'::regclass`).
		Scan(&n); err != nil {
		return 0, fmt.Errorf("can not estimate account count: %w", err)
	}
	return uint64(n), nil
}

// AccountTransactionCount returns the exact number of transactions touching an account.
//
// Exact and cheap: it is an index-only scan of tx_account, whose primary key leads with
// the address. The MongoDB equivalent counted an $or over the transaction collection,
// which no single index could serve -- which is why it had a 500 ms budget and a fallback
// that reported the WHOLE CHAIN's transaction count when it timed out.
func (s *Store) AccountTransactionCount(ctx context.Context, addr *common.Address) (uint64, error) {
	if addr == nil {
		return 0, fmt.Errorf("no account address given")
	}

	var n int64
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM tx_account WHERE address = $1`, AddrVal(*addr)).Scan(&n); err != nil {
		return 0, fmt.Errorf("can not count transactions for %s: %w", addr.String(), err)
	}
	return uint64(n), nil
}

// AccountsByType lists accounts of one type, most active first.
//
// This is the front-page token list. Under MongoDB the `account` collection carried no
// secondary index at all, so this query was a collection scan followed by a blocking
// in-memory sort -- which PostgreSQL's equivalent would abort once the sort exceeded its
// work limit. Here it is served by account_stat's rank index.
func (s *Store) AccountsByType(ctx context.Context, accountType string, limit int32) ([]common.Address, error) {
	code, err := accountTypeCode(accountType)
	if err != nil {
		return nil, err
	}

	page := NewPage(limit, maxListLimit)

	rows, err := s.pool.Query(ctx, `
		SELECT a.address
		FROM   account a
		LEFT   JOIN account_stat st ON st.address = a.address
		WHERE  a.acct_type = $1
		ORDER  BY COALESCE(st.tx_count, 0) DESC, COALESCE(st.last_activity, 'epoch') DESC
		LIMIT  $2`, code, page.Limit)
	if err != nil {
		return nil, fmt.Errorf("can not list accounts of type %s: %w", accountType, err)
	}
	defer rows.Close()

	out := make([]common.Address, 0, page.Limit)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		a, err := ToAddr(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// RefreshAccountStats rebuilds the account_stat materialized view.
//
// CONCURRENTLY so readers are never blocked; it needs the unique index on address, which
// the migration creates. Callers should run this on a schedule rather than per block --
// a full refresh per block would cost more than the ingest itself.
func (s *Store) RefreshAccountStats(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `REFRESH MATERIALIZED VIEW CONCURRENTLY account_stat`); err != nil {
		return fmt.Errorf("can not refresh account statistics: %w", err)
	}
	return nil
}

// scanAccount maps one row onto the domain type.
func scanAccount(row rowScanner) (*types.Account, error) {
	var (
		addr, scTx   []byte
		typeCode     int16
		txCount      int64
		lastActivity int64
	)

	if err := row.Scan(&addr, &typeCode, &scTx, &txCount, &lastActivity); err != nil {
		return nil, err
	}

	a, err := ToAddr(addr)
	if err != nil {
		return nil, err
	}
	sc, err := ToHash(scTx)
	if err != nil {
		return nil, err
	}
	name, err := accountTypeName(typeCode)
	if err != nil {
		return nil, err
	}

	return &types.Account{
		Address:      *a,
		ContractTx:   sc,
		Type:         name,
		TrxCounter:   hexutil.Uint64(txCount),
		LastActivity: hexutil.Uint64(lastActivity),
	}, nil
}
