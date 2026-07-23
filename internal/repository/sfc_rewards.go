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
	"math/big"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
)

// StoreRewardClaim stores reward claim record in the persistent repository.
func (p *proxy) StoreRewardClaim(ctx context.Context, rc *types.RewardClaim) error {
	return p.pg.AddRewardClaim(ctx, rc)
}

// RewardClaims provides a list of reward claims for the given delegation and/or filter.
func (p *proxy) RewardClaims(ctx context.Context, adr *common.Address, valID *big.Int, cursor *string, count int32) (*types.RewardClaimsList, error) {
	// Both filters are optional and both are now typed arguments; the store renders them
	// to bound predicates. This replaces a bson.D built here, above the storage seam.
	return p.pg.RewardClaims(ctx, adr, valID, cursor, count)
}

// RewardsClaimed returns sum of all claimed rewards for the given delegator address and validator ID.
func (p *proxy) RewardsClaimed(ctx context.Context, adr *common.Address, valId *big.Int, since *int64, until *int64) (*big.Int, error) {
	// All four filters are optional typed arguments now; the store renders them to bound
	// predicates. This replaces a bson.D assembled above the storage seam.
	return p.pg.RewardsClaimed(ctx, adr, valId, since, until)
}
