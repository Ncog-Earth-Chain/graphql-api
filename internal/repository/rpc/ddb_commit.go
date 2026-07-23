package rpc

// ddb_commit.go decodes on-chain DDB commit-tx payloads for the explorer. It MIRRORS the node's encoder
// (ncogearthchain/gossip/ddb/commit_tx.go + commit_tx_rlp.go): the tx Data is a 4-byte prefix followed by
// the endorsement proof — "DDBR" + RLP (current) or "DDBE" + JSON (legacy). The proof carries the DDB
// operation as VERBATIM JSON (the data-contract definition), which is the useful part for display.

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

const (
	ddbCommitPrefixJSON = "DDBE" // legacy
	ddbCommitPrefixRLP  = "DDBR" // current
)

// The *RLP structs MIRROR the node's proofRLP/endorsementRLP/sigRLP EXACTLY — RLP is positional, so field
// ORDER is load-bearing; do not reorder without matching gossip/ddb/commit_tx_rlp.go.
type ddbSigRLP struct {
	Validator common.Address
	Signature []byte
	Timestamp uint64
	Approved  bool
}

type ddbEndorsementRLP struct {
	OperationHash common.Hash
	DataHash      common.Hash
	Signatures    []ddbSigRLP
	ValidatorSet  []common.Address
	BlockNumber   uint64
	Timestamp     uint64
	RequestID     common.Hash
	Requester     common.Address
	TxID          string
	StateHash     common.Hash

	// Epoch is the endorsing committee's epoch (the grace window). It is the 11th
	// and last field of the node's endorsementRLP and is ALWAYS encoded --
	// encodeDdbCommitTxRLP sets it unconditionally.
	//
	// This field was missing here, and its absence broke every DDB commit tx: RLP
	// is positional, so a 10-field target against an 11-field payload leaves the
	// epoch unconsumed and the decode fails. The failure was swallowed at
	// extractDDBContractAddress, leaving ContractAddress nil, which gates contract
	// indexing -- so `isDDB` was permanently false and data contracts never
	// appeared in the explorer at all.
	//
	// Note this cannot use `rlp:"optional"`: this RLP fork predates that tag (see
	// rlp/typecache.go, which handles only "nil"/"nilString"/"nilList"/"tail").
	Epoch uint64
}

type ddbProofRLP struct {
	Endorsement        ddbEndorsementRLP
	OperationJSON      []byte
	Timestamp          uint64
	PriorPostStateHash []byte
	PostStateHash      []byte
}

// DdbCommitInfo is the explorer-friendly decoded view of a DDB commit tx.
type DdbCommitInfo struct {
	Operation    json.RawMessage  `json:"operation"` // verbatim DDB operation JSON (the data contract)
	RequestID    common.Hash      `json:"requestId"`
	Requester    common.Address   `json:"requester"`
	BlockNumber  uint64           `json:"blockNumber"`
	ValidatorSet []common.Address `json:"validatorSet"`
	Signatures   int              `json:"signatures"`

	// The rest of the endorsement, which was previously decoded and dropped. The node
	// exposes NO RPC for DDB history -- ddb_getEndorsementStatus is in-memory and resets
	// on restart -- so anything not captured from the commit transaction here cannot be
	// recovered afterwards.
	OperationHash common.Hash `json:"operationHash"`
	DataHash      common.Hash `json:"dataHash"`
	StateHash     common.Hash `json:"stateHash"`

	// Epoch is the endorsing committee's epoch. Its absence from the mirrored struct is
	// what made every DDB commit transaction fail to decode.
	Epoch uint64 `json:"epoch"`

	// The per-contract state-hash chain links.
	//
	// Variable-length and EMPTY for a contract's first operation and for non-contract
	// operations -- the node guards on len()==0. They are []byte rather than a hash type
	// for exactly that reason: a fixed 32-byte type cannot represent "no prior state".
	PriorPostStateHash []byte `json:"priorPostStateHash,omitempty"`
	PostStateHash      []byte `json:"postStateHash,omitempty"`

	Timestamp uint64 `json:"timestamp"`
}

// DecodeDdbCommitTx is the exported decoder used by the ingest path.
//
// The result was previously computed and thrown away -- transaction.go kept only the
// contract address and logged the rest at Debug -- so the entire dual-consensus record
// was parsed and dropped on the floor on every DDB transaction.
func DecodeDdbCommitTx(data []byte) (*DdbCommitInfo, error) {
	return decodeDdbCommitTxData(data)
}

// decodeDdbCommitTxData dispatches on the 4-byte prefix and returns the decoded operation + a summary.
func decodeDdbCommitTxData(data []byte) (*DdbCommitInfo, error) {
	if len(data) < 4 {
		return nil, errors.New("ddb commit payload too short")
	}
	switch string(data[:4]) {
	case ddbCommitPrefixRLP:
		var pr ddbProofRLP
		if err := rlp.DecodeBytes(data[4:], &pr); err != nil {
			return nil, fmt.Errorf("rlp decode DDBR proof: %w", err)
		}
		return &DdbCommitInfo{
			Operation:    pr.OperationJSON,
			RequestID:    pr.Endorsement.RequestID,
			Requester:    pr.Endorsement.Requester,
			BlockNumber:  pr.Endorsement.BlockNumber,
			ValidatorSet: pr.Endorsement.ValidatorSet,
			Signatures:   len(pr.Endorsement.Signatures),

			OperationHash:      pr.Endorsement.OperationHash,
			DataHash:           pr.Endorsement.DataHash,
			StateHash:          pr.Endorsement.StateHash,
			Epoch:              pr.Endorsement.Epoch,
			PriorPostStateHash: pr.PriorPostStateHash,
			PostStateHash:      pr.PostStateHash,
			Timestamp:          pr.Timestamp,
		}, nil
	case ddbCommitPrefixJSON:
		// Legacy DDBE: {endorsement:{...}, operation:{...}, ...}.
		var env struct {
			Endorsement struct {
				RequestID    common.Hash       `json:"requestId"`
				Requester    common.Address    `json:"requester"`
				BlockNumber  uint64            `json:"blockNumber"`
				ValidatorSet []common.Address  `json:"validatorSet"`
				Signatures   []json.RawMessage `json:"signatures"`
			} `json:"endorsement"`
			Operation json.RawMessage `json:"operation"`
		}
		if err := json.Unmarshal(data[4:], &env); err != nil {
			return nil, fmt.Errorf("json decode DDBE proof: %w", err)
		}
		return &DdbCommitInfo{
			Operation:    env.Operation,
			RequestID:    env.Endorsement.RequestID,
			Requester:    env.Endorsement.Requester,
			BlockNumber:  env.Endorsement.BlockNumber,
			ValidatorSet: env.Endorsement.ValidatorSet,
			Signatures:   len(env.Endorsement.Signatures),
		}, nil
	default:
		return nil, fmt.Errorf("not a DDB commit payload (prefix %q)", string(data[:4]))
	}
}

// ddbContractAddressFromOperation extracts the data-contract address from a DDB operation JSON: the
// operation carries `contractAddress` (enriched) or, for a CreateSchema, `data.contract_address` inside
// the contract definition. Returns "" when absent (e.g. a call/grant scoped only by schema_name).
//
// IMPORTANT: the node ALWAYS serializes the operation's top-level `contractAddress`, even when unset —
// inter.DdbOperation.ContractAddress is a common.Address ([20]byte array), and encoding/json's `omitempty`
// never omits a fixed-length array, so an unset value marshals as "0x0000...0000". Treat the zero address
// as absent and fall through to data.contract_address, otherwise every DDB commit tx would report the
// zero address as its data-contract.
func ddbContractAddressFromOperation(opJSON []byte) string {
	if len(opJSON) == 0 {
		return ""
	}
	var op struct {
		ContractAddress string          `json:"contractAddress"`
		Data            json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(opJSON, &op); err != nil {
		return ""
	}
	if nonZeroAddr(op.ContractAddress) {
		return op.ContractAddress
	}
	if len(op.Data) > 0 {
		var def struct {
			ContractAddress string `json:"contract_address"`
		}
		if err := json.Unmarshal(op.Data, &def); err == nil && nonZeroAddr(def.ContractAddress) {
			return def.ContractAddress
		}
	}
	return ""
}

// nonZeroAddr reports whether s is a non-empty hex address that is not the zero address.
func nonZeroAddr(s string) bool {
	return s != "" && common.HexToAddress(s) != (common.Address{})
}
