// Package types implements different core types of the API.
package types

import (
	"encoding/binary"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

const (
	FiWithdrawalPk          = "_id"
	FiWithdrawalType        = "type"
	FiWithdrawalOrdinal     = "orx"
	FiWithdrawalRequestID   = "req_id"
	FiWithdrawalAddress     = "adr"
	FiWithdrawalToValidator = "to"
	FiWithdrawalCreated     = "crt"
	FiWithdrawalStamp       = "stamp"
	FiWithdrawalValue       = "val"
	FiWithdrawalSlash       = "slash"
	FiWithdrawalRequestTrx  = "req_trx"
	FiWithdrawalFinTrx      = "fin_trx"
	FiWithdrawalFinTime     = "fin_time"

	WithdrawTypeUndelegated     = "SFC3:Undelegated"
	WithdrawTypeWithdrawRequest = "SFC1:WithdrawRequest"
	WithdrawTypeDeactivatedDlg  = "SFC1:DeactivatedDelegation"
	WithdrawTypeDeactivatedVal  = "SFC1:DeactivatedStake"
)

// WithdrawRequest represents a withdraw request in Ncogearthchain staking
// SFC contract. When partial withdraw is requested either on staking or delegation,
// this record is created in the SFC contract to track the withdrawal process.
type WithdrawRequest struct {
	// struct members for initiated withdraw
	RequestTrx        common.Hash
	WithdrawRequestID *hexutil.Big
	Address           common.Address
	StakerID          *hexutil.Big
	CreatedTime       hexutil.Uint64
	Amount            *hexutil.Big
	Type              string

	// BlockNumber and TxIndex locate the request-creating transaction in the chain.
	//
	// The MongoDB schema had no such position; it ordered withdrawals by a packed
	// `orx` ordinal that aliases validator IDs above 4095 and discriminates
	// transactions on 12 bits, so it is neither unique nor monotonic. The PostgreSQL
	// schema orders and paginates on the real position instead, and its columns are
	// NOT NULL -- so the position has to travel on this struct from the log handler
	// that already has it (types.LogRecord embeds retypes.Log).
	//
	// Pointers, not values: block 0 and transaction index 0 are both legitimate, so a
	// zero value must not be able to masquerade as "not set". A nil here is a bug in
	// the caller and the store rejects it rather than inventing a position.
	BlockNumber *hexutil.Uint64
	TxIndex     *hexutil.Uint64

	// struct members for finalized withdraw
	WithdrawTrx  *common.Hash
	WithdrawTime *hexutil.Uint64
	Penalty      *hexutil.Big
}

// WithdrawDecimalsCorrection is used to manipulate precision of a withdrawal value
// so it can be stored in database as UINT64 without loosing too much data
var WithdrawDecimalsCorrection = new(big.Int).SetUint64(1000000000)

// OrdinalIndex returns an ordinal index of the withdraw request.
func (wr *WithdrawRequest) OrdinalIndex() uint64 {
	return (uint64(wr.CreatedTime)&0xFFFFFFFFFF)<<24 | (wr.StakerID.ToInt().Uint64()&0xFFF)<<12 | (binary.BigEndian.Uint64(wr.RequestTrx[:8]) & 0xFFF)
}
