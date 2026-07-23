package pg

import (
	"context"
	"fmt"
	"ncogearthchain-api-graphql/internal/types"
	"time"
)

// DDB writes.
//
// Called from inside StoreBlock's transaction, so a DDB record and the transaction that
// carried it commit together or not at all -- and the reorg purge removes both, because
// both are block-keyed.

// writeDdbCommit stores the operation and its endorsement proof.
//
// A malformed DDB payload must NOT fail the block. The chain is still valid and still
// worth indexing when one contract emits something this decoder cannot read; refusing the
// block would wedge the indexer at that height over a payload that no other consumer
// cares about. But it must not be silent either -- a swallowed decode is exactly how the
// missing Epoch field hid for as long as it did -- so the caller logs it.
func (s *Store) writeDdbCommit(ctx context.Context, q Querier, t *types.Transaction, blockNumber int64, txIndex int32) error {
	d := t.DDB
	if d == nil {
		return nil
	}

	ts := t.TimeStamp.UTC()

	if _, err := q.Exec(ctx, `
		INSERT INTO ddb_operation (block_number, tx_index, tx_hash, request_id, requester,
		                           op_type, schema_name, contract_addr, contract_name,
		                           version, author, payload, ts)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (block_number, tx_index) DO UPDATE SET
		    tx_hash       = EXCLUDED.tx_hash,
		    request_id    = EXCLUDED.request_id,
		    requester     = EXCLUDED.requester,
		    op_type       = EXCLUDED.op_type,
		    schema_name   = EXCLUDED.schema_name,
		    contract_addr = EXCLUDED.contract_addr,
		    contract_name = EXCLUDED.contract_name,
		    version       = EXCLUDED.version,
		    author        = EXCLUDED.author,
		    payload       = EXCLUDED.payload,
		    ts            = EXCLUDED.ts`,
		blockNumber, txIndex, HashVal(t.Hash),
		HashVal(d.RequestID), AddrVal(d.Requester),
		d.OpType, d.SchemaName, Addr(d.ContractAddress),
		nullableText(d.ContractName), nullableText(d.Version), Addr(d.Author),
		[]byte(d.Operation), ts,
	); err != nil {
		return fmt.Errorf("can not store DDB operation from %s: %w", t.Hash.String(), err)
	}

	// The validator set as an array of raw addresses.
	validators := make([][]byte, 0, len(d.ValidatorSet))
	for _, v := range d.ValidatorSet {
		validators = append(validators, AddrVal(v))
	}

	if _, err := q.Exec(ctx, `
		INSERT INTO ddb_endorsement (block_number, tx_index, operation_hash, data_hash,
		                             state_hash, prior_post_state_hash, post_state_hash,
		                             epoch, validator_count, signature_count, validators, ts)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (block_number, tx_index) DO UPDATE SET
		    operation_hash        = EXCLUDED.operation_hash,
		    data_hash             = EXCLUDED.data_hash,
		    state_hash            = EXCLUDED.state_hash,
		    prior_post_state_hash = EXCLUDED.prior_post_state_hash,
		    post_state_hash       = EXCLUDED.post_state_hash,
		    epoch                 = EXCLUDED.epoch,
		    validator_count       = EXCLUDED.validator_count,
		    signature_count       = EXCLUDED.signature_count,
		    validators            = EXCLUDED.validators,
		    ts                    = EXCLUDED.ts`,
		blockNumber, txIndex,
		HashVal(d.OperationHash), HashVal(d.DataHash), HashVal(d.StateHash),
		// NULL rather than empty: a contract's FIRST operation has no prior state, and a
		// zero-length BYTEA would read as "the prior state hash was empty", which is a
		// different and false claim.
		nullableBytes(d.PriorPostStateHash),
		nullableBytes(d.PostStateHash),
		int64(d.Epoch), len(d.ValidatorSet), d.Signatures, validators, ts,
	); err != nil {
		return fmt.Errorf("can not store DDB endorsement from %s: %w", t.Hash.String(), err)
	}

	return s.foldDdbContract(ctx, q, d, blockNumber, txIndex, ts)
}

// foldDdbContract maintains the current-state view of a data contract.
//
// DOMAIN-KEYED and therefore NOT covered by the reorg purge -- the same limitation as
// delegation and withdrawal. That is acceptable here in a way it is not there, because
// this table is a materialized convenience folded from ddb_operation, which IS purged and
// IS authoritative. A rebuild recomputes it exactly.
func (s *Store) foldDdbContract(ctx context.Context, q Querier, d *types.DdbCommit, blockNumber int64, txIndex int32, ts time.Time) error {
	if d.ContractAddress == nil {
		return nil
	}

	_, err := q.Exec(ctx, `
		INSERT INTO ddb_contract (contract_addr, db_name, contract_name, author,
		                          latest_version, first_block, last_block, last_tx_index,
		                          op_count, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$6,$7,
		        (SELECT count(*) FROM ddb_operation WHERE contract_addr = $1::address),
		        $8,$8)
		ON CONFLICT (contract_addr) DO UPDATE SET
		    -- COALESCE on every folded field: a later operation that does not restate the
		    -- name, author or version must not blank what an earlier one established.
		    db_name        = COALESCE(EXCLUDED.db_name, ddb_contract.db_name),
		    contract_name  = COALESCE(EXCLUDED.contract_name, ddb_contract.contract_name),
		    author         = COALESCE(EXCLUDED.author, ddb_contract.author),
		    latest_version = COALESCE(EXCLUDED.latest_version, ddb_contract.latest_version),
		    last_block     = GREATEST(EXCLUDED.last_block, ddb_contract.last_block),
		    last_tx_index  = EXCLUDED.last_tx_index,
		    -- DERIVED, not incremented. An increment double-counts on re-scan: the same
		    -- block ingested twice would add twice, and nothing would detect the drift.
		    -- Counting ddb_operation makes it a function of the stored operations, so it
		    -- cannot disagree with them -- the same reasoning that made AccountMarkActivity
		    -- a no-op in favour of the account_stat view.
		    op_count       = (SELECT count(*) FROM ddb_operation WHERE contract_addr = EXCLUDED.contract_addr),
		    updated_at     = EXCLUDED.updated_at`,
		AddrVal(*d.ContractAddress),
		nullableText(deriveDbName(d.ContractName, *d.ContractAddress)),
		nullableText(d.ContractName),
		Addr(d.Author),
		nullableText(d.Version),
		blockNumber, txIndex, ts,
	)
	if err != nil {
		return fmt.Errorf("can not fold DDB contract %s: %w", d.ContractAddress.String(), err)
	}
	return nil
}

// deriveDbName mirrors the node's schema-name derivation.
//
// The DDB's actual PostgreSQL schema is named lower(contractName) + '_' + the last six hex
// characters of the address. It is NOT carried on the wire, so it has to be recomputed --
// and it is worth storing because it is what an operator needs to find the data.
//
// Returns empty when the contract name is unknown, rather than inventing a name from the
// address alone, which would not match anything the node created.
func deriveDbName(contractName string, addr [20]byte) string {
	if contractName == "" {
		return ""
	}
	hexAddr := fmt.Sprintf("%x", addr)
	return toLowerASCII(contractName) + "_" + hexAddr[len(hexAddr)-6:]
}

func toLowerASCII(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

// nullableText maps an empty string to SQL NULL, keeping "not stated" distinct from "".
func nullableText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullableBytes maps an empty slice to SQL NULL.
func nullableBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}
