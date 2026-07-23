// Package types implements different core types of the API.
package types

import (
	"encoding/binary"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

const (
	FiRewardClaimPk          = "_id"
	FiRewardClaimOrdinal     = "orx"
	FiRewardClaimAddress     = "addr"
	FiRewardClaimToValidator = "to"
	FiRewardClaimedValue     = "value"
	FiRewardClaimedTimeStamp = "stamp"
)

// RewardDecimalsCorrection is used to manipulate precision of a rewards value,
// so it can be stored in database as UINT64 without loosing too much data
var RewardDecimalsCorrection = new(big.Int).SetUint64(1000000000)

// RewardClaim represents a reward claim record in Ncogearthchain staking
// SFC contract. A reward can be claimed directly towards the account balance,
// or it can be re-staked into the SFC contract as an increased delegation.
type RewardClaim struct {
	Delegator     common.Address
	ToValidatorId hexutil.Big
	Claimed       hexutil.Uint64
	ClaimTrx      common.Hash
	Amount        hexutil.Big
	IsDelegated   bool

	// BlockNumber, LogIndex and TxIndex locate the emitting log on chain.
	//
	// They exist because the claim is identified by its log position, not by its
	// transaction hash. Keying on the hash - which is what Pk() does - drops the second
	// claim whenever one transaction emits two reward logs, which two different SFC
	// handlers and any batching contract both do. All three values are available at the
	// only construction site (svc/logs_sfc_reward.go, from the LogRecord) and are
	// required by the PostgreSQL reward_claim table.
	//
	// The BSON codec below ignores them, so the MongoDB representation is unchanged.
	BlockNumber uint64
	LogIndex    uint
	TxIndex     uint
}

// Pk returns a unique primary key of the claim.
func (rwc *RewardClaim) Pk() string {
	return rwc.ClaimTrx.String()
}

// OrdinalIndex returns an ordinal index for the given reward claim request.
func (rwc *RewardClaim) OrdinalIndex() uint64 {
	return (uint64(rwc.Claimed)&0x7FFFFFFFFF)<<24 | (binary.BigEndian.Uint64(rwc.ClaimTrx[:8]) & 0xFFFFFF)
}
