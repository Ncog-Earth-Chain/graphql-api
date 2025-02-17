/*
Package repository implements repository for handling fast and efficient access to data required
by the resolvers of the API server.

Internally it utilizes RPC to access Ncogearthchain/Forest full node for blockchain interaction. Mongo database
for fast, robust and scalable off-chain data storage, especially for aggregated and pre-calculated data mining
results. BigCache for in-memory object storage to speed up loading of frequently accessed entities.
*/
package repository

import (
	"database/sql"
	"fmt"
	"math/big"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// // StoreWithdrawRequest stores the given withdraw request in persistent storage.
// func (p *proxy) StoreWithdrawRequest(wr *types.WithdrawRequest) error {
// 	return p.db.AddWithdrawal(wr)
// }

// StoreWithdrawRequest stores the given withdraw request in persistent storage.
func (p *proxy) StoreWithdrawRequest(wr *types.WithdrawRequest) error {
	return p.pdDB.AddWithdrawal(wr)
}

// UpdateWithdrawRequest stores the given updated withdraw request in persistent storage.
// func (p *proxy) UpdateWithdrawRequest(wr *types.WithdrawRequest) error {
// 	return p.db.UpdateWithdrawal(wr)
// }

func (p *proxy) UpdateWithdrawRequest(tx *sql.Tx, wr *types.WithdrawRequest) error {
	// If no transaction is provided, start a new one
	if tx == nil {
		var err error
		tx, err = p.pdDB.Begin()
		if err != nil {
			log.Errorf("failed to start transaction: %s", err.Error())
			return err
		}
		defer func() {
			if err != nil {
				tx.Rollback()
				log.Errorf("transaction rolled back due to error: %s", err.Error())
			}
		}()
	}

	// Update withdrawal request in the database
	err := p.pdDB.UpdateWithdrawal(tx, wr)
	if err != nil {
		if tx == nil {
			tx.Rollback() // Rollback if we started this transaction
		}
		return err
	}

	// Commit only if we started the transaction
	if tx == nil {
		return tx.Commit()
	}

	return nil
}

// // WithdrawRequest extracts details of a withdraw request specified by the delegator, validator and request ID.
// func (p *proxy) WithdrawRequest(addr *common.Address, valID *hexutil.Big, reqID *hexutil.Big) (*types.WithdrawRequest, error) {
// 	return p.db.Withdrawal(addr, valID, reqID)
// }

// WithdrawRequest extracts details of a withdraw request specified by the delegator, validator and request ID.
func (p *proxy) WithdrawRequest(addr *common.Address, valID *hexutil.Big, reqID *hexutil.Big) (*types.WithdrawRequest, error) {
	return p.pdDB.Withdrawal(addr, valID, reqID)
}

// WithdrawRequests extracts a list of partial withdraw requests for the given address.
// func (p *proxy) WithdrawRequests(addr *common.Address, stakerID *hexutil.Big, cursor *string, count int32) (*types.WithdrawRequestList, error) {
// 	if addr == nil {
// 		return nil, fmt.Errorf("address not given")
// 	}

// 	// get all the requests for the given delegator address
// 	if stakerID == nil {
// 		// log the action and pull the list for all vals
// 		p.log.Debugf("loading withdraw requests of %s to any validator", addr.String())
// 		return p.db.Withdrawals(cursor, count, &bson.D{{Key: types.FiWithdrawalAddress, Value: addr.String()}})
// 	}

// 	// log the action and pull the list for specific address and val
// 	p.log.Debugf("loading withdraw requests of %s to #%d", addr.String(), stakerID.ToInt().Uint64())
// 	return p.db.Withdrawals(cursor, count, &bson.D{
// 		{Key: types.FiWithdrawalAddress, Value: addr.String()},
// 		{Key: types.FiWithdrawalToValidator, Value: stakerID.String()},
// 	})
// }

// WithdrawRequests extracts a list of partial withdraw requests for the given address.
func (p *proxy) WithdrawRequests(addr *common.Address, stakerID *hexutil.Big, cursor *string, count int32) (*types.PostWithdrawRequestList, error) {
	if addr == nil {
		return nil, fmt.Errorf("address not given")
	}

	// Prepare the filter
	filter := make(map[string]interface{})
	filter["address"] = addr.String() // Add the address to the filter

	// If a specific staker ID is provided, add it to the filter
	if stakerID != nil {
		filter["staker_id"] = stakerID.String()
		p.log.Debugf("loading withdraw requests of %s to #%d", addr.String(), stakerID.ToInt().Uint64())
	} else {
		p.log.Debugf("loading withdraw requests of %s to any validator", addr.String())
	}

	// Fetch the withdrawal requests
	return p.pdDB.Withdrawals(cursor, count, filter)
}

// // WithdrawRequestsPendingTotal is the total value of all pending withdrawal requests
// // for the given delegator and target staker ID.
// func (p *proxy) WithdrawRequestsPendingTotal(addr *common.Address, stakerID *hexutil.Big) (*big.Int, error) {
// 	if addr == nil {
// 		return nil, fmt.Errorf("address not given")
// 	}

// 	// all withdrawals for the address regardless of the target staker
// 	if stakerID == nil {
// 		return p.db.WithdrawalsSumValue(&bson.D{
// 			{Key: types.FiWithdrawalAddress, Value: addr.String()},
// 			{Key: types.FiWithdrawalFinTrx, Value: bson.D{{Key: "$type", Value: 10}}},
// 		})
// 	}

// 	// specific delegation withdrawal
// 	return p.db.WithdrawalsSumValue(&bson.D{
// 		{Key: types.FiWithdrawalAddress, Value: addr.String()},
// 		{Key: types.FiWithdrawalToValidator, Value: stakerID.String()},
// 		{Key: types.FiWithdrawalFinTrx, Value: bson.D{{Key: "$type", Value: 10}}},
// 	})
// }

// WithdrawRequestsPendingTotalPost calculates the total value of all pending withdrawal requests
// for the given delegator and target staker ID.
func (p *proxy) WithdrawRequestsPendingTotal(addr *common.Address, stakerID *hexutil.Big) (*big.Int, error) {
	if addr == nil {
		return nil, fmt.Errorf("address not given")
	}

	// Prepare the filter map
	filter := map[string]interface{}{
		"address": addr.String(),
	}

	// Add the condition for pending withdrawals
	filter["withdraw_fin_trx"] = nil // SQL equivalent of `IS NULL`

	// Add staker ID to the filter if provided
	if stakerID != nil {
		filter["staker_id"] = stakerID.String()
	}

	// Use the WithdrawalsSumValue function to calculate the sum
	return p.pdDB.WithdrawalsSumValue(filter)
}
