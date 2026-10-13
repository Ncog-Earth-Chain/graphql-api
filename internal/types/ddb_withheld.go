package types

// ddb_withheld.go -- SEC-07c: what the node withheld, and which contract an operation belongs to.
//
// A DDB commit transaction carries the tenant's operation VERBATIM in its calldata: the procedure
// arguments of a CallProcedure op ARE the row values, and a deploy's definition is the contract's
// tables, columns, roles, reader accounts and procedure bodies. The node refuses to serve that
// calldata to an anonymous caller when the contract it belongs to declares a `reader` role, has been
// tombstoned, or cannot be judged -- SEC-07 / SEC-07b, ethapi/ddb_calldata_redaction.go -- and says
// so with an `inputRedacted` descriptor in place of the bytes.
//
// This explorer re-published the same bytes, twice over: `DdbOperation.payload` and
// `ddbContract.operations` serve the operation JSON out of the index, and `Transaction.inputData`
// serves the raw calldata out of the index. Both are anonymous and both are on by default, so a
// restricted contract's rows were readable from the explorer even while the node refused them.
//
// Two things live here, and they are the two the node does NOT answer for us:
//
//   - whether a refusal is SETTLED or TRANSIENT (DdbWithholdIsFinal). `reader-role`, `tombstoned` and
//     `undecodable` are verdicts on the contract as the node holds it and may be recorded. The node
//     also answers `undecidable` -- for every commit in a block above its contiguous applied frontier,
//     every pending commit, a freshly deployed public contract it has not applied yet, and everything
//     a DDB-off or Postgres-down node returns -- and that one is a statement about the NODE, not the
//     contract: the same commit is served verbatim once applied. Persisting it would write "withheld"
//     over a public contract permanently.
//
//   - WHICH contract an operation belongs to (DdbOperationTarget), resolved the way the node's
//     ddb.ReadPolicyTarget resolves it. For a call, a grant, a revoke or a delete that is schema_name,
//     which IS the db_name. For a deploy or an upgrade it is NOT: schema_name there is the contract's
//     human name ("payroll") or whatever the client sent, while the registry keys the contract on
//     DeriveDbName(contract_address) of the definition the operation carries. Keying on schema_name
//     finds no contract and reads as public, which is precisely the blocker SEC-07's fix round found
//     ("a restricted contract's deploy commit was served in full").
//
// The read POLICY itself is deliberately not here. It stays on the node, where ddb_select, the signed
// reads and the chain-history formatters all reach the same single predicate; a second copy of it in
// this repository is the defect this file exists to close, not the fix.

import (
	"encoding/json"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// The redaction reasons the node serves. Constants, not free text: the ingest path switches on them
// to decide whether a refusal may be recorded, and the walk test asserts on them.
const (
	// DdbWithheldReaderRole: the contract declares a `reader` role, so its data is served only to an
	// authorized caller (node: ddb.ReadRestrictedReaderRole).
	DdbWithheldReaderRole = "reader-role"

	// DdbWithheldTombstoned: the contract was deleted. The data still exists and the deletion is
	// reversible, but the contract is retired and its rows are no longer served
	// (node: ddb.ReadRestrictedTombstoned).
	DdbWithheldTombstoned = "tombstoned"

	// DdbWithheldUndecodable: the calldata carries a DDB commit prefix but does not decode as an
	// endorsement proof, so the contract it belongs to cannot be named at all.
	DdbWithheldUndecodable = "undecodable"

	// DdbWithheldUndecidable: the node could not decide. TRANSIENT -- see the file comment.
	DdbWithheldUndecidable = "undecidable"
)

// DdbWithheld describes calldata the node would not serve. It mirrors ethapi.RPCRedactedInput field
// for field, including the JSON names, so the explorer can hand a client exactly what the node would
// have: how much was withheld, what it hashes to -- so a party who obtains the bytes through an
// authorized read can bind them to the chain -- and why.
type DdbWithheld struct {
	// Reason is one of the four constants above. A reason this build does not recognise is passed
	// through verbatim and treated as transient.
	Reason string `json:"reason"`

	// Schema is the data-contract schema (db_name) the operation targets, when it could be resolved.
	// For a deploy or an upgrade that is the schema its contract_address derives, NOT the human
	// contract name its schema_name carries.
	Schema string `json:"schema,omitempty"`

	// Size is the byte length of the withheld calldata.
	Size hexutil.Uint64 `json:"size"`

	// Keccak256 is the hash of the withheld calldata.
	Keccak256 common.Hash `json:"keccak256"`
}

// DdbCommitVerdict is the node's answer for one commit transaction's calldata: the bytes, or the
// descriptor saying why they were withheld. Withheld == nil means the node served the calldata and
// the explorer may serve what it holds for that transaction.
type DdbCommitVerdict struct {
	// Input is the calldata the node served. Empty when Withheld is set.
	Input hexutil.Bytes

	// Withheld is nil when the node served the bytes.
	Withheld *DdbWithheld
}

// Served reports whether the node authorized this commit's calldata.
func (v DdbCommitVerdict) Served() bool { return v.Withheld == nil }

// DdbUndecidableVerdict is the fail-closed verdict for a transaction the node could not be asked
// about at all -- it answered null, the call errored, or there is no node reachable. It is withheld,
// and it is TRANSIENT, because "I could not check" must never be served as "there was nothing to
// check" and must never be recorded as settled either.
func DdbUndecidableVerdict(size int, keccak common.Hash, schema string) DdbCommitVerdict {
	return DdbCommitVerdict{Withheld: &DdbWithheld{
		Reason:    DdbWithheldUndecidable,
		Schema:    schema,
		Size:      hexutil.Uint64(size),
		Keccak256: keccak,
	}}
}

// DdbWithholdIsFinal reports whether a refusal reason is a settled verdict on the contract, and may
// therefore be recorded as the answer for that commit, or a transient one that must be re-asked.
//
// Unknown reasons are transient on purpose. A node newer than this build can introduce a reason, and
// the two ways to be wrong are not symmetric: treating a new settled reason as transient costs a
// re-fetch that keeps answering the same way, while treating a new transient reason as settled
// freezes a public contract's history as withheld with nothing to undo it.
func DdbWithholdIsFinal(reason string) bool {
	switch reason {
	case DdbWithheldReaderRole, DdbWithheldTombstoned, DdbWithheldUndecodable:
		return true
	default:
		return false
	}
}

// DdbDeriveDbName mirrors the node's ddbschema.DeriveDbName: "c_" plus the 40 lowercase hex digits of
// the contract address, which is the name the registry keys the contract on (contracts.db_name) and
// the name of its actual PostgreSQL schema.
//
// Returns "" for anything that is not an address -- empty, short, long, non-hex -- because a schema
// name that cannot be derived is not a schema name the node would recognise, and guessing one would
// produce a verdict about a contract that does not exist.
func DdbDeriveDbName(contractAddress string) string {
	addr := strings.ToLower(strings.TrimSpace(contractAddress))
	addr = strings.TrimPrefix(addr, "0x")
	if len(addr) != 2*common.AddressLength || !isHexASCII(addr) {
		return ""
	}
	// "c_" plus 40 hex = 42 characters, comfortably inside Postgres's 63-byte identifier limit.
	return "c_" + addr
}

// isHexASCII reports whether s is non-empty and entirely lowercase hexadecimal.
func isHexASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return len(s) > 0
}

// DdbOperationTarget resolves the schema (db_name) whose read policy governs a committed DDB
// operation, the way the node's ddb.ReadPolicyTarget resolves it:
//
//   - CREATE_SCHEMA / UPDATE_SCHEMA: DdbDeriveDbName of the contract_address in the definition the
//     operation CARRIES, whatever schemaName says;
//   - every other type: schemaName, which IS the db_name for a procedure call, a grant, a revoke and
//     a delete.
//
// operationJSON is the operation as it rode the wire; it is read only for the first two types.
//
// A non-nil error means no target can be named -- a definition that does not parse, an address no
// schema name derives from, an operation naming no schema -- and the caller MUST treat it as a
// refusal: an operation whose contract cannot be named cannot be shown to be public.
func DdbOperationTarget(opType int16, schemaName string, operationJSON []byte) (string, error) {
	if opType != DdbOpCreateSchema && opType != DdbOpUpdateSchema {
		if schemaName == "" {
			return "", errDdbNoSchemaNamed
		}
		return schemaName, nil
	}

	var op struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(operationJSON, &op); err != nil {
		return "", errDdbOperationUnparseable
	}
	var def struct {
		ContractAddress string `json:"contract_address"`
	}
	if err := json.Unmarshal(op.Data, &def); err != nil {
		return "", errDdbDefinitionUnparseable
	}
	dbName := DdbDeriveDbName(def.ContractAddress)
	if dbName == "" {
		return "", errDdbNoContractAddress
	}
	return dbName, nil
}
