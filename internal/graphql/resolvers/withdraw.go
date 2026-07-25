// Package resolvers implements GraphQL resolvers to incoming API requests.
package resolvers

import (
	"context"
	"ncogearthchain-api-graphql/internal/repository"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// WithdrawRequests resolves the withdraw (un-delegation) requests of the given address, across all
// validators.
//
// Withdraw requests were previously reachable only under a delegation (Delegation.withdrawRequests).
// The stake-delegation surface has been removed, so this root query keeps them queryable. A nil
// validator filter means "requests to any validator".
func (rs *rootResolver) WithdrawRequests(ctx context.Context, args struct {
	Address common.Address
	Cursor  *Cursor
	Count   int32
}) ([]WithdrawRequest, error) {
	// limit query size; the count can be either positive or negative to control direction
	args.Count = listLimitCount(args.Count, listMaxEdgesPerRequest)

	wr, err := repository.R().WithdrawRequests(ctx, &args.Address, nil, (*string)(args.Cursor), args.Count)
	if err != nil {
		log.Errorf("can not get withdraw requests of %s; %s", args.Address.String(), err.Error())
		return nil, err
	}

	list := make([]WithdrawRequest, len(wr.Collection))
	for i, req := range wr.Collection {
		list[i] = NewWithdrawRequest(req)
	}
	return list, nil
}

// WithdrawRequest represents resolvable partial withdraw request
// from either stake or delegation structure.
type WithdrawRequest struct {
	types.WithdrawRequest
}

// NewWithdrawRequest builds new resolvable partial withdraw request structure.
func NewWithdrawRequest(wr *types.WithdrawRequest) WithdrawRequest {
	return WithdrawRequest{WithdrawRequest: *wr}
}

// Id resolves unique internal identifier of the Withdraw request.
func (wr WithdrawRequest) Id() Cursor {
	return Cursor(wr.RequestTrx.String())
}

// WithdrawRequestID resolves the SFC identifier of the request
// unique to the delegation address and the target validator ID.
func (wr WithdrawRequest) WithdrawRequestID() hexutil.Big {
	if wr.WithdrawRequest.WithdrawRequestID == nil {
		return hexutil.Big{}
	}
	return *wr.WithdrawRequest.WithdrawRequestID
}

// StakerID resolves the identifier of the validator ID the request points to.
func (wr WithdrawRequest) StakerID() hexutil.Big {
	if wr.WithdrawRequest.StakerID == nil {
		return hexutil.Big{}
	}
	return *wr.WithdrawRequest.StakerID
}

// Amount resolves the amount of tokens the withdraw request is for.
func (wr WithdrawRequest) Amount() hexutil.Big {
	if wr.WithdrawRequest.Amount == nil {
		return hexutil.Big{}
	}
	return *wr.WithdrawRequest.Amount
}

// Account resolves the account detail of the partial withdraw request.
func (wr WithdrawRequest) Account(ctx context.Context) (*Account, error) {
	// get the account detail by address
	acc, err := repository.R().Account(ctx, &wr.Address)
	if err != nil {
		return nil, err
	}

	// return the account detail
	return NewAccount(acc), nil
}

// Staker resolves the withdraw request staker detail, if available.
func (wr WithdrawRequest) Staker() (*Staker, error) {
	// get staker detail by the staker id
	st, err := repository.R().Validator(wr.WithdrawRequest.StakerID)
	if err != nil {
		return nil, err
	}

	// return the staker information
	return NewStaker(st), nil
}
