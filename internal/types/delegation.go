// Package types implements different core types of the API.
package types

import (
	"encoding/binary"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

const (
	// FiDelegationPk defines primary key column of the delegation table.
	FiDelegationPk = "_id"

	// FiDelegationOrdinal defines ordinal index column of the delegation table.
	FiDelegationOrdinal = "orx"

	// FiDelegationAddress defines delegation address column of the delegation table.
	FiDelegationAddress = "adr"

	// FiDelegationToValidator defines id of the validator column of the delegation table.
	FiDelegationToValidator = "to"

	// FiDelegationTransaction defines transaction has column of the delegation table.
	FiDelegationTransaction = "trx"

	// FiDelegationToValidatorAddress defines validator address column of the delegation table.
	FiDelegationToValidatorAddress = "toad"

	// FiDelegationAmountActive defines amount delegated column of the delegation table.
	FiDelegationAmountActive = "act"

	// FiDelegationValue defines value of the delegation column of the delegation table.
	FiDelegationValue = "val"

	// FiDelegationStamp defines time stamp column of the delegation table.
	FiDelegationStamp = "stamp"
)

// Delegation represents a delegator in Ncogearthchain blockchain.
type Delegation struct {
	ID              string         `json:"id"`
	Transaction     common.Hash    `json:"trx"`
	Address         common.Address `json:"address"`
	ToStakerId      *hexutil.Big   `json:"toStakerID"`
	ToStakerAddress common.Address `json:"toStakerAddr"`
	CreatedTime     hexutil.Uint64 `json:"createdTime"`
	Index           uint64         `json:"ordinalIndex"`

	// AmountStaked represents the current staked amount
	AmountStaked *hexutil.Big `json:"amountStaked"`

	// AmountDelegated is the original amount delegated
	AmountDelegated *hexutil.Big `json:"amountDelegated"`
}

// DelegationDecimalsCorrection is used to adjust decimal precision of a delegation active value.
var DelegationDecimalsCorrection = new(big.Int).SetUint64(1000000000)

// OrdinalIndex returns an ordinal index for the given delegation.
// We construct the UID from the time the delegation was created (40 bits = 1099511627775s = 34000 years),
// a part of the creation transaction hash and part of the target validator index (12 bits = 4096).
func (dl *Delegation) OrdinalIndex() uint64 {
	return (uint64(dl.CreatedTime)&0x7FFFFFFFFF)<<24 | (dl.ToStakerId.ToInt().Uint64()&0xFFF)<<12 | (binary.BigEndian.Uint64(dl.Transaction[:8]) & 0xFFF)
}
