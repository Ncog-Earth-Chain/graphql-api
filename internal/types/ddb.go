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
