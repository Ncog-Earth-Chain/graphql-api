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
