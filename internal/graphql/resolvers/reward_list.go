// Package resolvers implements GraphQL resolvers to incoming API requests.
package resolvers

import (
	"context"
	"ncogearthchain-api-graphql/internal/repository"
	"ncogearthchain-api-graphql/internal/repository/db/pg"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// RewardClaims resolves the SFC reward claims of the given address, across all validators.
//
// Reward claims were previously reachable only under a delegation (Delegation.rewardClaims). The
// stake-delegation surface has been removed, so this root query keeps the reward-claims history
// queryable. A nil validator filter means "claims to any validator".
func (rs *rootResolver) RewardClaims(ctx context.Context, args struct {
	Address common.Address
	Cursor  *Cursor
	Count   int32
}) (*RewardClaimList, error) {
	// limit query size; the count can be either positive or negative to control direction
	args.Count = listLimitCount(args.Count, listMaxEdgesPerRequest)

	cl, err := repository.R().RewardClaims(ctx, &args.Address, nil, (*string)(args.Cursor), args.Count)
	if err != nil {
		log.Errorf("can not get reward claims of %s; %s", args.Address.String(), err.Error())
		return nil, err
	}
	return NewRewardClaimList(cl), nil
}

// RewardClaimList represents resolvable list of blockchain reward claim edges structure.
type RewardClaimList struct {
	types.RewardClaimsList
}

// RewardClaimListEdge represents a single edge of a reward claim list structure.
type RewardClaimListEdge struct {
	Claim *RewardClaim
}

// NewRewardClaimList builds new resolvable list of reward claims.
func NewRewardClaimList(dl *types.RewardClaimsList) *RewardClaimList {
	return &RewardClaimList{*dl}
}

// TotalCount resolves the total number of delegations in the list.
func (rl *RewardClaimList) TotalCount() hexutil.Uint64 {
	return hexutil.Uint64(rl.Total)
}

// PageInfo resolves the current page information for the reward claims list.
func (rl *RewardClaimList) PageInfo() (*ListPageInfo, error) {
	// do we have any items?
	if rl.Collection == nil || len(rl.Collection) == 0 {
		return NewListPageInfo(nil, nil, false, false)
	}

	// get the first and last elements
	//
	// Opaque keyset cursor (base64 of block_number:log_index) matching DecodeCursor(_, 2) in
	// pg.RewardClaims. The old claim-tx-hash cursor could not be decoded (page 2 errored) and
	// was not even unique per claim (two claims in one tx shared it).
	first := Cursor(pg.RewardClaimCursor(rl.Collection[0]))
	last := Cursor(pg.RewardClaimCursor(rl.Collection[len(rl.Collection)-1]))
	return NewListPageInfo(&first, &last, !rl.IsEnd, !rl.IsStart)
}

// Edges resolves list of reward claim list edges for the linked block list.
func (rl *RewardClaimList) Edges() []*RewardClaimListEdge {
	// do we have any items? return empty list if not
	if rl.Collection == nil || len(rl.Collection) == 0 {
		return make([]*RewardClaimListEdge, 0)
	}

	// make the list
	edges := make([]*RewardClaimListEdge, len(rl.Collection))
	for i, d := range rl.Collection {
		edges[i] = &RewardClaimListEdge{Claim: NewRewardClaim(d)}
	}
	return edges
}

// Cursor generates the list edge cursor.
func (rce *RewardClaimListEdge) Cursor() Cursor {
	return Cursor(pg.RewardClaimCursor(&rce.Claim.RewardClaim))
}
