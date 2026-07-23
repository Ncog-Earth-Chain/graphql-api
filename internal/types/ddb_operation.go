package types

import (
	"encoding/json"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// DDB operation types, mirroring inter.DdbOperationType on the node.
//
// These are the numeric codes the wire carries, so they are part of the on-disk format and
// must never be reordered.
const (
	DdbOpCreateSchema  int16 = 0
	DdbOpUpdateSchema  int16 = 1
	DdbOpDeleteSchema  int16 = 2
	DdbOpCreateTable   int16 = 3
	DdbOpInsertData    int16 = 4
	DdbOpUpdateData    int16 = 5
	DdbOpDeleteData    int16 = 6
	DdbOpCallProcedure int16 = 7
	DdbOpGrantRole     int16 = 8
	DdbOpRevokeRole    int16 = 9
)

// DdbOperationTypeName maps an operation code to its name.
//
// Returns "UNKNOWN" for a code this build does not recognise rather than failing: the
// chain can introduce a new operation type before the explorer knows about it, and
// refusing to display the rest of a valid operation because of one unfamiliar enum would
// be the wrong trade.
func DdbOperationTypeName(t int16) string {
	switch t {
	case DdbOpCreateSchema:
		return "CREATE_SCHEMA"
	case DdbOpUpdateSchema:
		return "UPDATE_SCHEMA"
	case DdbOpDeleteSchema:
		return "DELETE_SCHEMA"
	case DdbOpCreateTable:
		return "CREATE_TABLE"
	case DdbOpInsertData:
		return "INSERT_DATA"
	case DdbOpUpdateData:
		return "UPDATE_DATA"
	case DdbOpDeleteData:
		return "DELETE_DATA"
	case DdbOpCallProcedure:
		return "CALL_PROCEDURE"
	case DdbOpGrantRole:
		return "GRANT_ROLE"
	case DdbOpRevokeRole:
		return "REVOKE_ROLE"
	default:
		return "UNKNOWN"
	}
}

// DdbOperation is a DDB operation committed on chain, with the quorum proof that
// authorised it.
//
// This is history the node cannot serve: it exposes no DDB history RPC, so these rows
// exist only because ingest captured them from the commit-transaction stream.
type DdbOperation struct {
	BlockNumber hexutil.Uint64 `json:"blockNumber"`
	TxIndex     hexutil.Uint64 `json:"txIndex"`
	TxHash      common.Hash    `json:"txHash"`

	// RequestID is the operation's identity in the endorsement protocol.
	RequestID common.Hash    `json:"requestId"`
	Requester common.Address `json:"requester"`

	// OpType is the numeric operation code; see DdbOperationTypeName.
	OpType int16 `json:"opType"`

	SchemaName      string          `json:"schemaName,omitempty"`
	ContractAddress *common.Address `json:"contractAddress,omitempty"`
	ContractName    string          `json:"contractName,omitempty"`

	// Version is a STRING on the wire ("1", "1.2.0"), not a number.
	Version string `json:"version,omitempty"`

	Author *common.Address `json:"author,omitempty"`

	// Payload is the operation JSON verbatim. Its shape is user-defined -- a CreateSchema
	// carries table and column definitions, an InsertData carries rows -- so it stays
	// dynamic rather than being forced into a fixed type it does not have.
	Payload json.RawMessage `json:"payload"`

	TimeStamp hexutil.Uint64 `json:"timestamp"`

	// Endorsement is the quorum proof. Nil only for a row written outside the normal
	// ingest path, since both are written in one transaction.
	Endorsement *DdbEndorsement `json:"endorsement,omitempty"`
}

// DdbEndorsement is the validator quorum proof that authorised an operation.
//
// This is the dual-consensus story the explorer previously could not tell at all: the
// proof was decoded on every DDB transaction and thrown away.
type DdbEndorsement struct {
	OperationHash common.Hash `json:"operationHash"`
	DataHash      common.Hash `json:"dataHash"`

	// StateHash is the PRIOR per-contract state hash the quorum signed against. Every
	// validator signature commits to it, which is what makes the per-contract hash chain
	// verifiable rather than merely asserted.
	StateHash common.Hash `json:"stateHash"`

	// The chain links. Absent for a contract's FIRST operation and for non-contract
	// operations -- which is why they are variable-length rather than fixed hashes.
	PriorPostStateHash hexutil.Bytes `json:"priorPostStateHash,omitempty"`
	PostStateHash      hexutil.Bytes `json:"postStateHash,omitempty"`

	// Epoch is the endorsing committee's epoch (the grace window).
	Epoch hexutil.Uint64 `json:"epoch"`

	// ValidatorCount is the committee size; SignatureCount is how many signed. The RATIO
	// is the interesting number -- a quorum that barely cleared threshold reads very
	// differently from a unanimous one.
	ValidatorCount int32 `json:"validatorCount"`
	SignatureCount int32 `json:"signatureCount"`

	ValidatorSet []common.Address `json:"validatorSet"`
}

// DdbContract is the current view of a data contract, folded from its operations.
type DdbContract struct {
	Address common.Address `json:"address"`

	// DbName is the contract's actual PostgreSQL schema name on the node. DERIVED, not
	// carried on the wire: lower(contractName) + the last six hex characters of the
	// address. Stored because it is what an operator needs to find the data.
	DbName string `json:"dbName,omitempty"`

	ContractName string `json:"contractName,omitempty"`

	// Author, not owner. There is no owner field anywhere in the operation payload; the
	// node's local metadata has one, but it is not chain-derived and therefore not
	// something this explorer can honestly claim.
	Author *common.Address `json:"author,omitempty"`

	LatestVersion string `json:"latestVersion,omitempty"`

	FirstBlock     hexutil.Uint64 `json:"firstBlock"`
	LastBlock      hexutil.Uint64 `json:"lastBlock"`
	OperationCount hexutil.Uint64 `json:"operationCount"`

	CreatedAt hexutil.Uint64 `json:"createdAt"`
	UpdatedAt hexutil.Uint64 `json:"updatedAt"`
}
