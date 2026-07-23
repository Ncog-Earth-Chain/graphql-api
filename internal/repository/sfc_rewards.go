/*
Package repository implements repository for handling fast and efficient access to data required
by the resolvers of the API server.

Internally it utilizes RPC to access Ncogearthchain/Forest full node for blockchain interaction. Mongo database
for fast, robust and scalable off-chain data storage, especially for aggregated and pre-calculated data mining
results. BigCache for in-memory object storage to speed up loading of frequently accessed entities.
*/
package repository

import (
	"math/big"
	"ncogearthchain-api-graphql/internal/types"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"go.mongodb.org/mongo-driver/bson"
)

// StoreRewardClaim stores reward claim record in the persistent repository.
func (p *proxy) StoreRewardClaim(rc *types.RewardClaim) error {
	return p.pg.AddRewardClaim(storeCtx(), rc)
}

// RewardClaims provides a list of reward claims for the given delegation and/or filter.
func (p *proxy) RewardClaims(adr *common.Address, valID *big.Int, cursor *string, count int32) (*types.RewardClaimsList, error) {
	// Both filters are optional and both are now typed arguments; the store renders them
	// to bound predicates. This replaces a bson.D built here, above the storage seam.
	return p.pg.RewardClaims(storeCtx(), adr, valID, cursor, count)
}

// RewardsClaimed returns sum of all claimed rewards for the given delegator address and validator ID.
func (p *proxy) RewardsClaimed(adr *common.Address, valId *big.Int, since *int64, until *int64) (*big.Int, error) {
	// prep the filter
	fi := bson.D{}

	// filter by delegator address
	if adr != nil {
		fi = append(fi, bson.E{
			Key:   types.FiRewardClaimAddress,
			Value: adr.String(),
		})
	}

	// filter by validator ID
	if valId != nil {
		fi = append(fi, bson.E{
			Key:   types.FiRewardClaimToValidator,
			Value: (*hexutil.Big)(valId).String(),
		})
	}

	// starting time stamp provided
	if since != nil {
		fi = append(fi, bson.E{
			Key:   types.FiRewardClaimedTimeStamp,
			Value: bson.D{{Key: "$gte", Value: time.Unix(*since, 0)}},
		})
	}

	// ending time stamp provided
	if until != nil {
		fi = append(fi, bson.E{
			Key:   types.FiRewardClaimedTimeStamp,
			Value: bson.D{{Key: "$lte", Value: time.Unix(*until, 0)}},
		})
	}
	return p.db.RewardsSumValue(&fi)
}
