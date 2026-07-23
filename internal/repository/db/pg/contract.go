package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"ncogearthchain-api-graphql/internal/types"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Contract reads and writes.
//
// The MongoDB collection was one flat document per contract, mixing two kinds of data
// with completely different provenance:
//
//   - deployment facts, derived from the chain and rebuildable by re-scanning
//   - VERIFICATION, submitted by users through a GraphQL mutation: source code, ABI,
//     compiler settings, proxy resolution. None of it exists on chain, and no amount of
//     re-scanning recovers it.
//
// They are split here into `contract` and `contract_verification` because that
// difference governs what may be deleted. The reorg purge rebuilds chain-derived rows
// freely; it must never touch a contract, because doing so would cascade to the
// verification row and destroy the only copy. That exemption is enforced by a test, and
// the split is what makes it expressible.
//
// A contract with no verification simply has no row in the second table -- which is why
// every read here LEFT joins.

// contractKeyset orders contracts newest-deployed first. deploy_seq breaks ties for
// several contracts created by one transaction, making the ordering total.
var contractKeyset = Keyset{Columns: []KeyColumn{
	{Name: "block_number", Dir: Desc},
	{Name: "tx_index", Dir: Desc},
	{Name: "deploy_seq", Dir: Desc},
}}

const contractColumns = `
	c.address, c.acct_type, c.deploy_tx, c.block_number, c.tx_index, c.deploy_seq,
	c.ts, c.is_ddb, c.is_verified,
	v.validated_at, v.name, v.contract_version, v.compiler_version, v.support_contact,
	v.license, v.compiler, v.evm_version, v.via_ir, v.is_optimized, v.optimize_runs,
	v.source_code, v.source_hash, v.abi, v.metadata,
	v.creation_bytecode, v.runtime_bytecode, v.creation_link_refs, v.runtime_link_refs,
	v.is_proxy, v.proxy_type, v.impl_address`

const contractFrom = `
	FROM contract c
	LEFT JOIN contract_verification v ON v.address = c.address`

// Contract loads one contract by address.
//
// Returns (nil, nil) when absent, matching the MongoDB behaviour the callers depend on.
func (s *Store) Contract(ctx context.Context, addr *common.Address) (*types.Contract, error) {
	if addr == nil {
		return nil, fmt.Errorf("no contract address given")
	}

	row := s.pool.QueryRow(ctx,
		`SELECT `+contractColumns+contractFrom+` WHERE c.address = $1`, AddrVal(*addr))

	con, err := scanContract(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("can not load contract %s: %w", addr.String(), err)
	}
	return con, nil
}

// IsContractKnown reports whether a contract exists.
func (s *Store) IsContractKnown(ctx context.Context, addr *common.Address) (bool, error) {
	if addr == nil {
		return false, fmt.Errorf("no contract address given")
	}

	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM contract WHERE address = $1)`,
		AddrVal(*addr)).Scan(&exists); err != nil {
		return false, fmt.Errorf("can not check contract %s: %w", addr.String(), err)
	}
	return exists, nil
}

// ContractTransaction returns the hash of the transaction that deployed a contract.
func (s *Store) ContractTransaction(ctx context.Context, addr *common.Address) (*common.Hash, error) {
	if addr == nil {
		return nil, fmt.Errorf("no contract address given")
	}

	var raw []byte
	err := s.pool.QueryRow(ctx,
		`SELECT deploy_tx FROM contract WHERE address = $1`, AddrVal(*addr)).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("can not load deployment transaction for %s: %w", addr.String(), err)
	}
	return ToHash(raw)
}

// AddContract records a deployed contract.
//
// Writes ONLY the chain-derived row. Verification is written by UpdateContractValidation,
// which is the user-submitted path -- keeping them apart means a re-scan that re-observes
// a deployment can never clobber a verification.
func (s *Store) AddContract(ctx context.Context, con *types.Contract) error {
	if con == nil {
		return fmt.Errorf("can not add an empty contract")
	}

	code, err := accountTypeCode(con.Type)
	if err != nil {
		return fmt.Errorf("contract %s: %w", con.Address.String(), err)
	}

	// The block position is taken from the DEPLOY TRANSACTION rather than from the
	// domain type, which does not carry one: a contract's position in the chain IS the
	// position of the transaction that created it, so deriving it here keeps the two
	// from ever disagreeing and needs no change to types.Contract.
	//
	// This also replaces the MongoDB ordinal, which was
	//   (timestamp & 0xFFFFFFFFFF) << 24 | (first 8 bytes of the tx hash & 0xFFFFFF)
	// -- a timestamp with a 24-bit hash tiebreaker. Two contracts deployed in the same
	// second collided unless those 24 bits differed, and the ordering it produced was by
	// wall-clock rather than by chain position, so it disagreed with every other list in
	// the explorer.
	//
	// deploy_seq distinguishes several contracts created by ONE transaction; it is
	// assigned by counting the contracts already recorded for that transaction.
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO contract (address, acct_type, deploy_tx, block_number, tx_index,
		                      deploy_seq, ts, is_ddb)
		SELECT $1, $2, t.hash, t.block_number, t.tx_index,
		       (SELECT count(*) FROM contract c2 WHERE c2.deploy_tx = t.hash)::SMALLINT,
		       $4, $5
		FROM   tx t
		WHERE  t.hash = $3
		ON CONFLICT (address) DO UPDATE SET
		    acct_type = EXCLUDED.acct_type,
		    deploy_tx = EXCLUDED.deploy_tx,
		    ts        = EXCLUDED.ts,
		    is_ddb    = EXCLUDED.is_ddb`,
		AddrVal(con.Address), code, HashVal(con.TransactionHash),
		time.Unix(int64(con.TimeStamp), 0).UTC(), con.IsDDB)
	if err != nil {
		return fmt.Errorf("can not store contract %s: %w", con.Address.String(), err)
	}

	// A zero row count means the deploy transaction is not stored yet. Silently writing
	// nothing would leave a contract the explorer has seen but cannot list, so say so --
	// the caller can retry after the block lands.
	if tag.RowsAffected() == 0 {
		return fmt.Errorf(
			"can not store contract %s: its deployment transaction %s is not stored yet",
			con.Address.String(), con.TransactionHash.String())
	}
	return nil
}

// UpdateContractValidation stores a user-submitted verification.
//
// This is the non-rebuildable half. It is an upsert on address so re-verifying replaces
// the previous submission, and it flips contract.is_verified in the same transaction so
// the flag can never disagree with whether a verification row exists.
func (s *Store) UpdateContractValidation(ctx context.Context, con *types.Contract) error {
	if con == nil {
		return fmt.Errorf("can not validate an empty contract")
	}

	creationRefs, err := marshalLinkRefs(con.CreationLinkReferences)
	if err != nil {
		return fmt.Errorf("contract %s creation link references: %w", con.Address.String(), err)
	}
	runtimeRefs, err := marshalLinkRefs(con.RuntimeLinkReferences)
	if err != nil {
		return fmt.Errorf("contract %s runtime link references: %w", con.Address.String(), err)
	}

	// A validation timestamp of nil means "not validated"; it must stay NULL rather than
	// becoming the epoch, which would read as a successful validation in 1970.
	var validatedAt any
	if con.Validated != nil {
		validatedAt = time.Unix(int64(*con.Validated), 0).UTC()
	}

	return s.pool.InTx(ctx, func(ctx context.Context, tx Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO contract_verification (
				address, validated_at, name, contract_version, compiler_version,
				support_contact, license, compiler, evm_version, via_ir, is_optimized,
				optimize_runs, source_code, source_hash, abi, metadata,
				creation_bytecode, runtime_bytecode, creation_link_refs, runtime_link_refs,
				is_proxy, proxy_type, impl_address)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)
			ON CONFLICT (address) DO UPDATE SET
			    validated_at      = EXCLUDED.validated_at,
			    name              = EXCLUDED.name,
			    contract_version  = EXCLUDED.contract_version,
			    compiler_version  = EXCLUDED.compiler_version,
			    support_contact   = EXCLUDED.support_contact,
			    license           = EXCLUDED.license,
			    compiler          = EXCLUDED.compiler,
			    evm_version       = EXCLUDED.evm_version,
			    via_ir            = EXCLUDED.via_ir,
			    is_optimized      = EXCLUDED.is_optimized,
			    optimize_runs     = EXCLUDED.optimize_runs,
			    source_code       = EXCLUDED.source_code,
			    source_hash       = EXCLUDED.source_hash,
			    abi               = EXCLUDED.abi,
			    metadata          = EXCLUDED.metadata,
			    creation_bytecode = EXCLUDED.creation_bytecode,
			    runtime_bytecode  = EXCLUDED.runtime_bytecode,
			    creation_link_refs= EXCLUDED.creation_link_refs,
			    runtime_link_refs = EXCLUDED.runtime_link_refs,
			    is_proxy          = EXCLUDED.is_proxy,
			    proxy_type        = EXCLUDED.proxy_type,
			    impl_address      = EXCLUDED.impl_address`,
			AddrVal(con.Address), validatedAt, con.Name, con.Version, con.CompilerVersion,
			con.SupportContact, con.License, con.Compiler, con.EvmVersion, con.ViaIR,
			con.IsOptimized, con.OptimizeRuns, con.SourceCode, Hash(con.SourceCodeHash),
			con.Abi, con.Metadata,
			hexBytes(con.CreationBytecode), hexBytes(con.RuntimeBytecode),
			creationRefs, runtimeRefs,
			con.IsProxy, con.ProxyType, AddrVal(con.ImplementationAddress),
		); err != nil {
			return fmt.Errorf("can not store verification for %s: %w", con.Address.String(), err)
		}

		// Derived from the verification row that was just written, not from the
		// parameter: one source of truth means the flag cannot disagree with the data
		// it describes. (Passing the timestamp again also failed type inference --
		// PostgreSQL cannot determine a bare parameter's type inside IS NOT NULL.)
		if _, err := tx.Exec(ctx, `
			UPDATE contract SET is_verified = EXISTS (
			    SELECT 1 FROM contract_verification v
			    WHERE v.address = contract.address AND v.validated_at IS NOT NULL
			)
			WHERE address = $1`,
			AddrVal(con.Address)); err != nil {
			return fmt.Errorf("can not flag contract %s as verified: %w", con.Address.String(), err)
		}
		return nil
	})
}

// Contracts lists contracts, newest deployment first.
//
// validatedOnly narrows to verified contracts. That predicate is served by
// contract_verified_idx, a PARTIAL index on exactly this condition, so the filtered list
// does not pay for the unverified majority.
func (s *Store) Contracts(ctx context.Context, validatedOnly bool, cursor string, count int32) ([]*types.Contract, error) {
	page := NewPage(count, maxListLimit)

	cur, err := DecodeCursor(cursor, 3)
	if err != nil {
		return nil, err
	}

	var conds []string
	var args []any

	if validatedOnly {
		conds = append(conds, "c.is_verified")
	}

	if len(cur) == 3 {
		pred, curArgs, err := keysetOn("c", contractKeyset).After(
			[]any{cur[0], int32(cur[1]), int16(cur[2])}, page.Reverse, len(args))
		if err != nil {
			return nil, err
		}
		conds = append(conds, pred)
		args = append(args, curArgs...)
	}

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + conds[0]
		for _, c := range conds[1:] {
			where += " AND " + c
		}
	}

	sql := `SELECT ` + contractColumns + contractFrom + ` ` + where + ` ` +
		keysetOn("c", contractKeyset).OrderBy(page.Reverse) + ` LIMIT $` + itoa(len(args)+1)
	args = append(args, page.Limit)

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("contract list query failed: %w", err)
	}
	defer rows.Close()

	out := make([]*types.Contract, 0, page.Limit)
	for rows.Next() {
		con, err := scanContract(rows)
		if err != nil {
			return nil, fmt.Errorf("can not scan contract: %w", err)
		}
		out = append(out, con)
	}
	return out, rows.Err()
}

// ContractCount returns the number of contracts, exactly when filtered and approximately
// when not.
//
// The filtered count is cheap because contract_verified_idx is partial on the predicate,
// so counting verified contracts touches only verified rows. The unfiltered count is an
// estimate for the same reason every other whole-table count here is.
func (s *Store) ContractCount(ctx context.Context, validatedOnly bool) (uint64, error) {
	var n int64

	if validatedOnly {
		if err := s.pool.QueryRow(ctx,
			`SELECT count(*) FROM contract WHERE is_verified`).Scan(&n); err != nil {
			return 0, fmt.Errorf("can not count verified contracts: %w", err)
		}
		return uint64(n), nil
	}

	if err := s.pool.QueryRow(ctx, `
		SELECT GREATEST(reltuples, 0)::BIGINT FROM pg_class WHERE oid = 'contract'::regclass`).
		Scan(&n); err != nil {
		return 0, fmt.Errorf("can not estimate contract count: %w", err)
	}
	return uint64(n), nil
}

// ContractCursor renders the pagination cursor from a row's stored position.
//
// It takes the position explicitly because types.Contract does not carry one -- the
// caller reads it from the same query that produced the contract.
func ContractCursor(blockNumber int64, txIndex int32, deploySeq int16) string {
	return EncodeCursor([]int64{blockNumber, int64(txIndex), int64(deploySeq)})
}

// hexBytes decodes an optional hex string for a BYTEA column.
//
// Bytecode is stored as bytes, not as the hex text MongoDB kept: half the size, and it
// cannot acquire a stray 0x prefix on one write path and not another. An empty string is
// NULL, because "no bytecode recorded" and "zero-length bytecode" are different.
func hexBytes(s string) any {
	if s == "" {
		return nil
	}
	b, err := hexutil.Decode(s)
	if err != nil {
		// Not valid hex; store the raw bytes rather than losing the submission. The
		// value came from a user and round-tripping it beats discarding it.
		return []byte(s)
	}
	return b
}

// marshalLinkRefs renders link references as JSONB.
func marshalLinkRefs(refs []types.LinkReferenceRange) (any, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	return json.Marshal(refs)
}

// unmarshalLinkRefs parses link references back from JSONB.
func unmarshalLinkRefs(raw []byte) ([]types.LinkReferenceRange, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var out []types.LinkReferenceRange
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// scanContract maps a joined contract + verification row onto the domain type.
func scanContract(row rowScanner) (*types.Contract, error) {
	var (
		address, deployTx          []byte
		acctType                   int16
		blockNumber                int64
		txIndex                    int32
		deploySeq                  int16
		ts                         pgtype.Timestamptz
		isDDB, isVerified          bool
		validatedAt                pgtype.Timestamptz
		name, version, compilerVer *string
		supportContact, license    *string
		compiler, evmVersion       *string
		viaIR, isOptimized         *bool
		optimizeRuns               *int32
		sourceCode                 *string
		sourceHash                 []byte
		abi, metadata              *string
		creationBytecode           []byte
		runtimeBytecode            []byte
		creationRefs, runtimeRefs  []byte
		isProxy                    *bool
		proxyType                  *string
		implAddress                []byte
	)

	if err := row.Scan(&address, &acctType, &deployTx, &blockNumber, &txIndex, &deploySeq,
		&ts, &isDDB, &isVerified,
		&validatedAt, &name, &version, &compilerVer, &supportContact,
		&license, &compiler, &evmVersion, &viaIR, &isOptimized, &optimizeRuns,
		&sourceCode, &sourceHash, &abi, &metadata,
		&creationBytecode, &runtimeBytecode, &creationRefs, &runtimeRefs,
		&isProxy, &proxyType, &implAddress); err != nil {
		return nil, err
	}

	addr, err := ToAddr(address)
	if err != nil {
		return nil, err
	}
	tx, err := ToHash(deployTx)
	if err != nil {
		return nil, err
	}
	typeName, err := accountTypeName(acctType)
	if err != nil {
		return nil, err
	}

	con := &types.Contract{
		Address:         *addr,
		Type:            typeName,
		TransactionHash: *tx,
		TimeStamp:       hexutil.Uint64(ts.Time.Unix()),
		IsDDB:           isDDB,
	}

	// Everything below comes from the LEFT-joined verification row and is absent for an
	// unverified contract. Dereferencing without the nil guards would panic on exactly
	// the common case.
	if validatedAt.Valid {
		v := hexutil.Uint64(validatedAt.Time.Unix())
		con.Validated = &v
	}
	con.Name = deref(name)
	con.Version = deref(version)
	con.CompilerVersion = deref(compilerVer)
	con.SupportContact = deref(supportContact)
	con.License = deref(license)
	con.Compiler = deref(compiler)
	con.EvmVersion = deref(evmVersion)
	con.ViaIR = derefBool(viaIR)
	con.IsOptimized = derefBool(isOptimized)
	con.SourceCode = deref(sourceCode)
	con.Abi = deref(abi)
	con.Metadata = deref(metadata)
	con.IsProxy = derefBool(isProxy)
	con.ProxyType = deref(proxyType)

	if optimizeRuns != nil {
		con.OptimizeRuns = *optimizeRuns
	}
	if len(creationBytecode) > 0 {
		con.CreationBytecode = hexutil.Encode(creationBytecode)
	}
	if len(runtimeBytecode) > 0 {
		con.RuntimeBytecode = hexutil.Encode(runtimeBytecode)
	}
	if sh, err := ToHash(sourceHash); err == nil {
		con.SourceCodeHash = sh
	}
	if ia, err := ToAddr(implAddress); err == nil && ia != nil {
		con.ImplementationAddress = *ia
	}
	if refs, err := unmarshalLinkRefs(creationRefs); err == nil {
		con.CreationLinkReferences = refs
	}
	if refs, err := unmarshalLinkRefs(runtimeRefs); err == nil {
		con.RuntimeLinkReferences = refs
	}

	return con, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefBool(b *bool) bool {
	return b != nil && *b
}
