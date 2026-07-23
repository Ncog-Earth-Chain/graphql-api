// Package svc implements blockchain data processing services.
package svc

import (
	"math/big"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// handleDelegationLog handles a new delegation event from logs.
func handleNewDelegation(lr *types.LogRecord, stakerID *big.Int, addr common.Address, amo *big.Int) {
	// get the validator address
	val, err := repo.ValidatorAddress((*hexutil.Big)(stakerID))
	if err != nil {
		log.Errorf("unknown validator #%d; %s", stakerID.Uint64(), err.Error())
		return
	}

	// pull the current value of the stake
	staked, err := repo.DelegationAmountStaked(&addr, (*hexutil.Big)(stakerID))
	if err != nil {
		log.Errorf("delegation balance not available for %s to %d; %s", addr.String(), stakerID.Uint64(), err.Error())
		return
	}

	// make the delegation record
	dl := types.Delegation{
		Transaction:     lr.Trx.Hash,
		Address:         addr,
		ToStakerId:      (*hexutil.Big)(stakerID),
		ToStakerAddress: *val,
		AmountDelegated: (*hexutil.Big)(staked),
		AmountStaked:    (*hexutil.Big)(amo),
		CreatedTime:     lr.Block.TimeStamp,
	}

	// store the delegation
	if err := repo.StoreDelegation(bgCtx(), &dl); err != nil {
		log.Errorf("failed to store delegation; %s", err.Error())
	}
}

// handleSfcCreatedDelegation handles a new delegation event from the SFC contract.
//
// (SFCv3) event Delegated(address indexed delegator, uint256 indexed toValidatorID, uint256 amount)
//
// This handler was previously registered for the SFC1 CreatedDelegation topic as well,
// which had the same argument structure. The SFC v1/v2 bindings and their topic
// registrations are gone; only the SFC3 registration remains.
func handleSfcCreatedDelegation(lr *types.LogRecord) {
	handleNewDelegation(
		lr,
		new(big.Int).SetBytes(lr.Topics[2].Bytes()),
		common.BytesToAddress(lr.Topics[1].Bytes()),
		new(big.Int).SetBytes(lr.Data),
	)
}

// handleSfcUndelegated handles new withdrawal request from SFCv3 contract.
// We ignore withdrawals from previous SFC versions since after the upgrade all the pending
// withdrawals will be settled.
// event Undelegated(address indexed delegator, uint256 indexed toValidatorID, uint256 indexed wrID, uint256 amount)
func handleSfcUndelegated(lr *types.LogRecord) {
	// sanity check for data (1x uint256 = 32 bytes)
	if len(lr.Data) != 32 {
		log.Criticalf("%s lr invalid data length; expected 32 bytes, %d bytes given, %d topics given", lr.TxHash.String(), len(lr.Data), len(lr.Topics))
		return
	}

	// create withdraw request
	handleNewWithdrawRequest(
		types.WithdrawTypeUndelegated,
		common.BytesToAddress(lr.Topics[1].Bytes()),
		new(big.Int).SetBytes(lr.Topics[2].Bytes()),
		new(big.Int).SetBytes(lr.Topics[3].Bytes()),
		new(big.Int).SetBytes(lr.Data[:]),
		lr,
	)
}

// handleNewWithdrawRequest will create a new withdrawal request for the given stake.
func handleNewWithdrawRequest(wrt string, adr common.Address, valID *big.Int, reqID *big.Int, amo *big.Int, lr *types.LogRecord) {
	// make the request
	wr := types.WithdrawRequest{
		Type:              wrt,
		RequestTrx:        lr.TxHash,
		WithdrawRequestID: (*hexutil.Big)(reqID),
		Address:           adr,
		StakerID:          (*hexutil.Big)(valID),
		CreatedTime:       lr.Block.TimeStamp,
		Amount:            (*hexutil.Big)(amo),
	}

	// lr what we do
	log.Debugf("new withdrawal of type %s by %s to #%d, req %s at %s",
		wrt,
		adr.String(),
		valID.Uint64(),
		((*hexutil.Big)(reqID)).String(),
		lr.TxHash.String(),
	)

	// store the request
	if err := repo.StoreWithdrawRequest(bgCtx(), &wr); err != nil {
		log.Errorf("failed to store new withdraw request; %s", err.Error())
	}

	// check active amount on the delegation
	if err := repo.UpdateDelegationBalance(bgCtx(), &wr.Address, wr.StakerID, func(amo *big.Int) error {
		return makeAdHocDelegation(lr, &wr.Address, wr.StakerID, amo)
	}); err != nil {
		log.Errorf("failed to update delegation; %s", err.Error())
	}
}

// handleFinishedWithdrawRequest handles withdrawal request finalisation event.
func handleFinishedWithdrawRequest(adr common.Address, valID *big.Int, reqID *big.Int, penalty *big.Int, lr *types.LogRecord) {
	// make sure the delegation balance will be updated
	defer func() {
		// check active amount on the delegation
		if err := repo.UpdateDelegationBalance(bgCtx(), &adr, (*hexutil.Big)(valID), func(amo *big.Int) error {
			return makeAdHocDelegation(lr, &adr, (*hexutil.Big)(valID), amo)
		}); err != nil {
			log.Errorf("failed to update delegation; %s", err.Error())
		}
	}()

	// lr what we do
	log.Debugf("closing withdrawal by %s to #%d, req %s at %s",
		adr.String(),
		valID.Uint64(),
		((*hexutil.Big)(reqID)).String(),
		lr.TxHash.String(),
	)

	// try to get the request from database
	req, err := repo.WithdrawRequest(bgCtx(), &adr, (*hexutil.Big)(valID), (*hexutil.Big)(reqID))
	if err != nil {
		log.Errorf("can not load withdraw requests to finalise; %s", err.Error())
		return
	}

	// update the request to have the finalization details
	req.WithdrawTime = &lr.Block.TimeStamp
	req.WithdrawTrx = &lr.TxHash
	req.Penalty = (*hexutil.Big)(penalty)

	// store the updated request
	if err := repo.UpdateWithdrawRequest(bgCtx(), req); err != nil {
		log.Errorf("failed to store finalized withdraw request; %s", err.Error())
	}
}

// handleSfcWithdrawn handles a withdrawal request finalization event.
// event Withdrawn(address indexed delegator, uint256 indexed toValidatorID, uint256 indexed wrID, uint256 amount)
func handleSfcWithdrawn(lr *types.LogRecord) {
	// sanity check for data (4x topic + 1x + 1 x uint256 = 32 bytes)
	if len(lr.Topics) != 4 || len(lr.Data) != 32 {
		log.Criticalf("%s is not event Withdrawn; expected 32 bytes, %d bytes given; expected 4 topics, %d given", lr.TxHash.String(), len(lr.Data), len(lr.Topics))
		return
	}

	// finish the request
	handleFinishedWithdrawRequest(
		common.BytesToAddress(lr.Topics[1].Bytes()),
		new(big.Int).SetBytes(lr.Topics[2].Bytes()),
		new(big.Int).SetBytes(lr.Topics[3].Bytes()),
		new(big.Int),
		lr,
	)
}

// makeAdHocDelegation creates a new delegation in case an expected existing delegation
// could not be found on a new lr event processing.
func makeAdHocDelegation(lr *types.LogRecord, addr *common.Address, stakerID *hexutil.Big, amo *big.Int) error {
	// get staker address
	val, err := repo.ValidatorAddress(stakerID)
	if err != nil {
		return err
	}

	// do the insert
	log.Noticef("creating ad-hoc delegation of %s to #%d", addr.String(), stakerID.ToInt().Uint64())
	return repo.StoreDelegation(bgCtx(), &types.Delegation{
		Transaction:     lr.TxHash,
		Address:         *addr,
		ToStakerId:      stakerID,
		ToStakerAddress: *val,
		AmountDelegated: (*hexutil.Big)(amo),
		AmountStaked:    (*hexutil.Big)(amo),
		CreatedTime:     lr.Block.TimeStamp,
	})
}
