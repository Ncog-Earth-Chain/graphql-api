package types

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Log is a stored event log, as returned by a query.
//
// Deliberately NOT types.LogRecord, which carries a WaitGroup and back-pointers to the
// block and transaction because it exists to be passed through the ingest pipeline. A
// query result has no pipeline to coordinate with, and reusing that type would mean
// returning three nil pointers and inviting a caller to dereference them.
type Log struct {
	// Address is the contract that emitted the event.
	Address common.Address `json:"address"`

	// Topics are the indexed event parameters. Topics[0] is the event signature --
	// keccak256 of the canonical declaration -- for every event except an anonymous one,
	// which has none. The slice holds exactly what was emitted; it is never padded.
	Topics []common.Hash `json:"topics"`

	// Data is the ABI-encoded non-indexed parameters.
	Data hexutil.Bytes `json:"data"`

	// BlockNumber is the block that contains the emitting transaction.
	BlockNumber hexutil.Uint64 `json:"blockNumber"`

	// TxHash identifies the emitting transaction.
	TxHash common.Hash `json:"transactionHash"`

	// TxIndex is the transaction's position in its block.
	TxIndex hexutil.Uint64 `json:"transactionIndex"`

	// Index is the log's position within the block, which is what makes
	// (blockNumber, index) a total ordering.
	Index hexutil.Uint64 `json:"logIndex"`

	// Removed marks a log from a block that was reorged away.
	Removed bool `json:"removed"`

	// TimeStamp is the emitting block's timestamp.
	TimeStamp hexutil.Uint64 `json:"timestamp"`
}
