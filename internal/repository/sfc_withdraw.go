/*
Package repository implements repository for handling fast and efficient access to data required
by the resolvers of the API server.

Internally it utilizes RPC to access Ncogearthchain/Forest full node for blockchain interaction. Mongo database
for fast, robust and scalable off-chain data storage, especially for aggregated and pre-calculated data mining
results. BigCache for in-memory object storage to speed up loading of frequently accessed entities.
*/
package repository

import (
	"context"
	"fmt"
	"math/big"
	"ncogearthchain-api-graphql/internal/repository/db/pg"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// StoreWithdrawRequest stores the given withdraw request in persistent storage.
func (p *proxy) StoreWithdrawRequest(ctx context.Context, wr *types.WithdrawRequest) error {
	return p.pg.AddWithdrawal(ctx, wr)
}

// UpdateWithdrawRequest stores the given updated withdraw request in persistent storage.
func (p *proxy) UpdateWithdrawRequest(ctx context.Context, wr *types.WithdrawRequest) error {
	return p.pg.UpdateWithdrawal(ctx, wr)
}

// WithdrawRequest extracts details of a withdraw request specified by the delegator, validator and request ID.
func (p *proxy) WithdrawRequest(ctx context.Context, addr *common.Address, valID *hexutil.Big, reqID *hexutil.Big) (*types.WithdrawRequest, error) {
	return p.pg.Withdrawal(ctx, addr, valID, reqID)
}

// WithdrawRequests extracts a list of partial withdraw requests for the given address.
func (p *proxy) WithdrawRequests(ctx context.Context, addr *common.Address, stakerID *hexutil.Big, cursor *string, count int32) (*types.WithdrawRequestList, error) {
	if addr == nil {
		return nil, fmt.Errorf("address not given")
	}

	// get all the requests for the given delegator address
	if stakerID == nil {
		// log the action and pull the list for all vals
		p.log.Debugf("loading withdraw requests of %s to any validator", addr.String())
		f, err := pg.WithdrawalsOf(addr, nil)
		if err != nil {
			return nil, err
		}
		return p.pg.Withdrawals(ctx, derefCursor(cursor), count, f)
	}

	// log the action and pull the list for specific address and val
	p.log.Debugf("loading withdraw requests of %s to #%d", addr.String(), stakerID.ToInt().Uint64())
	f, err := pg.WithdrawalsOf(addr, stakerID)
	if err != nil {
		return nil, err
	}
	return p.pg.Withdrawals(ctx, derefCursor(cursor), count, f)
}

// WithdrawRequestsPendingTotal is the total value of all pending withdrawal requests
// for the given delegator and target staker ID.
//
// "Pending" was written in MongoDB as {fin_trx: {$type: 10}} -- BSON type code 10,
// meaning NULL. Reading it required knowing the BSON type table, and nothing in it said
// "not finalized". It also failed to match documents where the field was ABSENT rather
// than null, so requests written by the $set-only upsert path were left out of pending
// totals entirely. PendingWithdrawalsOf spells it IS NULL, which covers both.
func (p *proxy) WithdrawRequestsPendingTotal(ctx context.Context, addr *common.Address, stakerID *hexutil.Big) (*big.Int, error) {
	if addr == nil {
		return nil, fmt.Errorf("address not given")
	}

	// stakerID nil means "to any validator"
	f, err := pg.PendingWithdrawalsOf(addr, stakerID)
	if err != nil {
		return nil, err
	}
	return p.pg.WithdrawalsSumValue(ctx, f)
}
