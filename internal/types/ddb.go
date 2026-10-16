package types

import "github.com/ethereum/go-ethereum/common"

// DDBContractAddress is the DDB system sentinel every on-chain DDB commit transaction is addressed To
// (mirrors the node's utils.DDBContractAddress = 0x…DDDB). Compare with IsDDBTransaction rather than
// string literals so the match is checksum-agnostic and byte-exact.
var DDBContractAddress = common.HexToAddress("0x000000000000000000000000000000000000dddb")

// IsDDBTransaction reports whether a transaction recipient is the DDB system address.
func IsDDBTransaction(to *common.Address) bool {
	return to != nil && *to == DDBContractAddress
}

// The 4-byte prefixes a DDB commit transaction's calldata starts with, mirroring the node's
// gossip/ddb/commit_tx.go: "DDBR" + RLP (current) or "DDBE" + JSON (legacy).
const (
	DdbCommitPrefixJSON = "DDBE" // legacy
	DdbCommitPrefixRLP  = "DDBR" // current
)

// IsDdbCommitTxData reports whether calldata carries a DDB commit proof, by prefix alone.
//
// The CHEAP test, and it is a wire fact rather than a policy: the node's redactor runs the same one
// first (ddb.IsDdbCommitTxData) so that the overwhelming majority of transactions, which are not DDB
// commits, never pay for a decode let alone a read-policy lookup. The read path here uses it for the
// same reason -- a block page must not cost one node round trip per plain transfer.
//
// The recipient address is NOT a substitute. A commit tx is addressed to the DDB sentinel, but what
// decides whether calldata embeds an operation verbatim is the calldata, and the index stores it
// without the recipient's involvement.
func IsDdbCommitTxData(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	switch string(data[:4]) {
	case DdbCommitPrefixRLP, DdbCommitPrefixJSON:
		return true
	default:
		return false
	}
}

// SignedDdbRead is a client-signed ddb_signedSelect envelope, carried through the API on its way to the
// node. Its fields and JSON keys are exactly those of the node's ethapi.SignedDdbRead, every one of which
// is inside the ML-DSA-87 preimage (SignedReadHash).
//
// Every value is kept as the STRING the client sent, not parsed into an address or a hexutil quantity,
// on purpose: the bridge forwards this envelope verbatim and must never be in a position to re-encode,
// normalize, fill in or sign any part of it. It holds no reader's key, and a bridge that signed with
// its own would make every read the bridge's read. Validation of these values is the node's job.
type SignedDdbRead struct {
	SchemaName   string   `json:"schemaName"`
	TableName    string   `json:"tableName"`
	JoinTables   []string `json:"joinTables,omitempty"`
	From         string   `json:"from"`
	Timestamp    string   `json:"timestamp"`
	Nonce        string   `json:"nonce"`
	CallerPubKey string   `json:"callerPubKey"`
	CallerSig    string   `json:"callerSig"`
}
