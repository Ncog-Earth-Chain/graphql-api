// Package types implements different core types of the API.
package types

import (
	"encoding/binary"
	"encoding/json"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	retypes "github.com/ethereum/go-ethereum/core/types"
)

// trxLargeInputWall represents the largest transaction input block we store in the off-chain database.
// Larger inputs (like contract deployments) need to be loaded from the blockchain directly if needed.
const trxLargeInputWall = 32 * 8

// TransactionDecimalsCorrection is used to manipulate precision of a transaction amount value,
// so it can be stored in database as INT64 without loosing too much data
var TransactionDecimalsCorrection = new(big.Int).SetUint64(1000000000)

// TransactionGasCorrection is used to restore the precision on the transaction gas value calculations.
var TransactionGasCorrection = new(big.Int).SetUint64(10000000)

// Transaction represents basic information provided by the API about transaction inside Ncogearthchain blockchain.
type Transaction struct {
	// BlockHash represents hash of the block where this transaction was in. nil when its pending.
	BlockHash *common.Hash `json:"blockHash"`

	// BlockNumber represents number of the block where this transaction was in. nil when its pending.
	BlockNumber *hexutil.Uint64 `json:"blockNumber"`

	// TimeStamp represents the time stamp of the transaction.
	TimeStamp time.Time

	// From represents address of the sender.
	From common.Address `json:"from"`

	// Gas represents gas provided by the sender.
	Gas hexutil.Uint64 `json:"gas"`

	// Gas represents gas provided by the sender.
	GasUsed *hexutil.Uint64 `json:"gasUsed"`

	// CumulativeGasUsed represents the total amount of gas used when this transaction was executed in the block.
	CumulativeGasUsed *hexutil.Uint64 `json:"-"`

	// GasPrice represents gas price provided by the sender in Wei.
	GasPrice hexutil.Big `json:"gasPrice"`

	// Hash represents 32 bytes hash of the transaction.
	Hash common.Hash `json:"hash"`

	// Nonce represents the number of transactions made by the sender prior to this one.
	Nonce hexutil.Uint64 `json:"nonce"`

	// To represents the address of the receiver. nil when its a contract creation transaction.
	To *common.Address `json:"to,omitempty"`

	// ContractAddress represents the address of contract created, if a contract creation transaction, otherwise nil.
	ContractAddress *common.Address `json:"contract,omitempty"`

	// TrxIndex represents integer of the transaction's index position in the block. nil when its pending.
	TrxIndex *hexutil.Uint `json:"transactionIndex,omitempty"`

	// Value represents value transferred in Wei.
	Value hexutil.Big `json:"value"`

	// Input represents the data send along with the transaction.
	InputData hexutil.Bytes `json:"input"`

	// LargeInput indicates the input data block is large and needs to be loaded separately.
	LargeInput bool `json:"-"`

	// Index represents integer of the transaction's index position in the block.
	Index *hexutil.Uint64 `json:"index,omitempty"`

	// Status represents transaction status; value is either 1 (success) or 0 (failure)
	Status *hexutil.Uint64 `json:"status,omitempty"`

	// Logs represents a list of log records created along with the transaction
	Logs []retypes.Log `json:"logs"`
}

type DDBInput struct {
	ContractAddress string `json:"contractAddress"`
}

// Uid calculates an ordinal index of the transaction referenced.
// The ordinal index of a transaction should be unique across a consistent block chain.
// The calculation gives us about 700 years of index space with 50k blocks per second
// rate + 10 years to fix than. Max number of transactions in a block here is 14bits = 16383.
func (trx *Transaction) Uid() uint64 {
	// is this a processed transaction?
	if trx.Index != nil {
		return (uint64(*trx.BlockNumber) << 14) | (uint64(*trx.Index)&0x3fff)&0x7FFFFFFFFFFFFFFF
	}
	// pending transaction
	return binary.BigEndian.Uint64(trx.Hash[:8]) & 0x7FFFFFFFFFFFFFFF
}

// Marshal returns the JSON encoding of transaction.
func (trx *Transaction) Marshal() ([]byte, error) {
	return json.Marshal(trx)
}
