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
	return p.db.AddRewardClaim(rc)
}

// StoreRewardClaim stores reward claim record in the persistent repository.
func (p *proxy) StoreRewardClaimPost(rc *types.RewardClaim) error {
	return p.pdDB.AddRewardClaim(rc)
}

// RewardClaims provides a list of reward claims for the given delegation and/or filter.
func (p *proxy) RewardClaims(adr *common.Address, valID *big.Int, cursor *string, count int32) (*types.RewardClaimsList, error) {
	// prep the filter
	fi := bson.D{}

	// add delegator address to the filter
	if adr != nil {
		fi = append(fi, bson.E{
			Key:   types.FiRewardClaimAddress,
			Value: adr.String(),
		})
	}

	// add validator ID to the filter
	if valID != nil {
		fi = append(fi, bson.E{
			Key:   types.FiRewardClaimToValidator,
			Value: (*hexutil.Big)(valID).String(),
		})
	}
	return p.db.RewardClaims(cursor, count, &fi)
}

// RewardClaimsPostgres provides a list of reward claims for the given delegation and/or filter.
// RewardClaimsPostgres provides a list of reward claims for the given delegation and/or filter.
func (p *proxy) RewardClaimsPostgres(adr *common.Address, valID *big.Int, cursor *string, count int32) (*types.RewardClaimsList, error) {
	// Prep the filter
	filter := ""

	// Arguments to be passed to PostgreSQL query
	args := []interface{}{}

	// Add delegator address to the filter
	if adr != nil {
		filter += "reward_claim_address = $1"
		args = append(args, adr.String())
	}

	// Add validator ID to the filter
	if valID != nil {
		if len(args) > 0 {
			filter += " AND "
		}
		filter += "reward_claim_to_validator = $2"
		args = append(args, (*hexutil.Big)(valID).String())
	}

	// Fetch reward claims using the PostgreSQL bridge
	postRewardClaims, err := p.pdDB.RewardClaims(cursor, count, filter, args...)
	if err != nil {
		p.log.Errorf("failed to load reward claims for address %s and validator #%d: %v", adr.String(), valID, err)
		return nil, err
	}

	// Convert PostRewardClaimsList to RewardClaimsList
	rewardClaims := make([]*types.RewardClaim, len(postRewardClaims.Collection))
	for i, prc := range postRewardClaims.Collection {
		rewardClaims[i] = &types.RewardClaim{
			Delegator:   prc.Delegator,
			Claimed:     prc.Claimed,
			ClaimTrx:    prc.ClaimTrx,
			IsDelegated: prc.IsDelegated,
		}
	}

	// Return the result using the correct field
	return &types.RewardClaimsList{Collection: rewardClaims}, nil
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

// RewardsClaimed returns sum of all claimed rewards for the given delegator address and validator ID.
func (p *proxy) RewardsClaimedPost(adr *common.Address, valId *big.Int, since *int64, until *int64) (*big.Int, error) {
	// Prep the filter and arguments for the PostgreSQL query
	filter := ""
	args := []interface{}{}

	// Filter by delegator address
	if adr != nil {
		filter += "reward_claim_address = $1"
		args = append(args, adr.String())
	}

	// Filter by validator ID
	if valId != nil {
		if len(args) > 0 {
			filter += " AND "
		}
		filter += "reward_claim_to_validator = $2"
		args = append(args, (*hexutil.Big)(valId).String())
	}

	// Starting timestamp provided (filter by 'since')
	if since != nil {
		if len(args) > 0 {
			filter += " AND "
		}
		filter += "reward_claimed_timestamp >= $3"
		args = append(args, time.Unix(*since, 0))
	}

	// Ending timestamp provided (filter by 'until')
	if until != nil {
		if len(args) > 0 {
			filter += " AND "
		}
		filter += "reward_claimed_timestamp <= $4"
		args = append(args, time.Unix(*until, 0))
	}

	// Call RewardsSumValue to calculate the sum of rewards
	return p.pdDB.RewardsSumValue(filter, args...)
}
