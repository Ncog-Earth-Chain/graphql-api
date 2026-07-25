// Package svc implements blockchain data processing services.
package svc

import (
	"math/big"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// handleSfcRewardClaim handles a rewards claim event.
func handleSfcRewardClaim(lr *types.LogRecord, addr common.Address, valID *hexutil.Big, amo *big.Int, isRestake bool) {
	// debug the event
	log.Debugf("%s claimed %d in stake to #%d", addr.String(), amo.Uint64(), valID.ToInt().Uint64())

	// add the rewards claim into the repository
	if err := repo.StoreRewardClaim(bgCtx(), &types.RewardClaim{
		Delegator:     addr,
		ToValidatorId: *valID,
		Claimed:       lr.Block.TimeStamp,
		ClaimTrx:      lr.TxHash,
		Amount:        (hexutil.Big)(*amo),
		IsDelegated:   isRestake,
	}); err != nil {
		log.Criticalf("can not store rewards claim; %s", err.Error())
		return
	}
}

// handleSfcCommonRewardClaim handles the common reward claim on SFC contract.
func handleSfcCommonRewardClaim(lr *types.LogRecord, isRestake bool) {
	// sanity check for data (3x uint256 = 3x32 bytes = 96 bytes)
	if len(lr.Data) != 96 {
		log.Criticalf("%s lr invalid data length; expected 96 bytes, given %d bytes", lr.TxHash.String(), len(lr.Data))
		return
	}

	// extract the basic info about the request
	addr := common.BytesToAddress(lr.Topics[1].Bytes())
	valID := (*hexutil.Big)(new(big.Int).SetBytes(lr.Topics[2].Bytes()))

	// collect values for each reward section
	amoA := new(big.Int).Add(
		new(big.Int).SetBytes(lr.Data[:32]),
		new(big.Int).SetBytes(lr.Data[32:64]),
	)
	amo := new(big.Int).Add(amoA, new(big.Int).SetBytes(lr.Data[64:]))

	// do the handling
	handleSfcRewardClaim(lr, addr, valID, amo, isRestake)
}

// handleSfcRestakeRewards handles a rewards re-stake event.
// event RestakedRewards(address indexed delegator, uint256 indexed toValidatorID, uint256 lockupExtraReward, uint256 lockupBaseReward, uint256 unlockedReward)
func handleSfcRestakeRewards(lr *types.LogRecord) {
	handleSfcCommonRewardClaim(lr, true)
}

// handleSfcClaimedRewards handles a rewards re-stake event.
// event ClaimedRewards(address indexed delegator, uint256 indexed toValidatorID, uint256 lockupExtraReward, uint256 lockupBaseReward, uint256 unlockedReward)
func handleSfcClaimedRewards(lr *types.LogRecord) {
	handleSfcCommonRewardClaim(lr, false)
}
