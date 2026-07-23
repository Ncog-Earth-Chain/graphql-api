package types

import (
	"encoding/json"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// DdbCommit is the decoded dual-consensus record carried by a DDB commit transaction.
//
// It exists because the node exposes NO RPC for DDB history: ddb_getEndorsementStatus and
// ddb_getConsensusStats are in-memory and reset on restart, and ddb_getSchema returns only
// current state. The commit-transaction stream is the only chain-derived, historical,
// reorg-safe source of DDB activity there is -- so what is not captured here at ingest
// cannot be asked for later.
type DdbCommit struct {
	// Operation is the DDB operation as verbatim JSON. Kept whole because its shape is
	// user-defined: a CreateSchema carries table and column definitions, an InsertData
	// carries rows.
	Operation json.RawMessage `json:"operation"`

	// OpType mirrors inter.DdbOperationType (0..9).
	OpType int16 `json:"opType"`

	SchemaName      string          `json:"schemaName,omitempty"`
	ContractAddress *common.Address `json:"contractAddress,omitempty"`
	ContractName    string          `json:"contractName,omitempty"`
	Version         string          `json:"version,omitempty"`
	Author          *common.Address `json:"author,omitempty"`

	// RequestID is the operation's identity in the endorsement protocol.
	RequestID common.Hash    `json:"requestId"`
	Requester common.Address `json:"requester"`

	OperationHash common.Hash `json:"operationHash"`
	DataHash      common.Hash `json:"dataHash"`

	// StateHash is the PRIOR per-contract state hash the quorum signed against. Every
	// validator signature commits to it, which is what makes the chain verifiable rather
	// than merely asserted.
	StateHash common.Hash `json:"stateHash"`

	// Epoch is the endorsing committee's epoch (the grace window).
	Epoch hexutil.Uint64 `json:"epoch"`

	// The per-contract state-hash chain links. EMPTY for a contract's first operation and
	// for non-contract operations -- variable-length rather than fixed 32 bytes precisely
	// so "no prior state" is representable.
	PriorPostStateHash hexutil.Bytes `json:"priorPostStateHash,omitempty"`
	PostStateHash      hexutil.Bytes `json:"postStateHash,omitempty"`

	ValidatorSet []common.Address `json:"validatorSet"`
	Signatures   int32            `json:"signatures"`
}
