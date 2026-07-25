// Package svc implements blockchain data processing services.
package svc

import (
	"math/big"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Withdrawal (un-delegation) log handling.
//
// Stake DELEGATION is not offered on this chain, so the SFC Delegated event and the delegation
// table it fed have been removed. The SFC still emits Undelegated/Withdrawn events for existing
// stake being withdrawn, and those are tracked here as withdraw requests -- the withdrawal surface
// is intentionally kept. These handlers no longer touch any delegation record (there is none).

// handleSfcUndelegated handles a new withdrawal request from the SFCv3 contract.
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
	// The block position of the emitting log MUST travel onto the request: block_number and
	// tx_index are NOT NULL and are the withdrawal keyset ordering, so pg.AddWithdrawal rejects a
	// request that has neither. The values ride on the embedded go-ethereum log record.
	bn := hexutil.Uint64(lr.BlockNumber)
	ix := hexutil.Uint64(uint64(lr.TxIndex))

	// make the request
	wr := types.WithdrawRequest{
		Type:              wrt,
		RequestTrx:        lr.TxHash,
		WithdrawRequestID: (*hexutil.Big)(reqID),
		Address:           adr,
		StakerID:          (*hexutil.Big)(valID),
		BlockNumber:       &bn,
		TxIndex:           &ix,
		CreatedTime:       lr.Block.TimeStamp,
		Amount:            (*hexutil.Big)(amo),
	}

	// log what we do
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
}

// handleFinishedWithdrawRequest handles withdrawal request finalisation event.
func handleFinishedWithdrawRequest(adr common.Address, valID *big.Int, reqID *big.Int, penalty *big.Int, lr *types.LogRecord) {
	// log what we do
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
	// sanity check for data (4x topic + 1 x uint256 = 32 bytes)
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
