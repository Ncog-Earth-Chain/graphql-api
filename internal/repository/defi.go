package repository

import (
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// DefiConfiguration resolves the current DeFi contract settings.
func (p *proxy) DefiConfiguration() (*types.DefiSettings, error) {
	return p.rpc.DefiConfiguration()
}

// DefiToken loads details of a single DeFi token by it's address.
func (p *proxy) DefiToken(token *common.Address) (*types.DefiToken, error) {
	return p.rpc.DefiToken(token)
}

// DefiTokens resolves list of DeFi tokens available for the DeFi functions.
func (p *proxy) DefiTokens() ([]types.DefiToken, error) {
	return p.rpc.DefiTokens()
}

// DefiTokenPrice loads the current price of the given token
// from on-chain price oracle.
func (p *proxy) DefiTokenPrice(token *common.Address) (hexutil.Big, error) {
	return p.rpc.FMintTokenPrice(token)
}

// FMintRewardsEarned represents the total amount of rewards
// accumulated on the account for the excessive collateral deposits.
func (p *proxy) FMintRewardsEarned(addr *common.Address) (hexutil.Big, error) {
	return p.rpc.FMintRewardsEarned(addr)
}

// FMintRewardsStashed represents the total amount of rewards
// accumulated on the account in stash.
func (p *proxy) FMintRewardsStashed(addr *common.Address) (hexutil.Big, error) {
	return p.rpc.FMintRewardsStashed(addr)
}

// FMintCanClaimRewards resolves the fMint account flag for being allowed
// to claim earned rewards.
func (p *proxy) FMintCanClaimRewards(addr *common.Address) (bool, error) {
	return p.rpc.FMintCanClaimRewards(addr)
}

// FMintCanReceiveRewards resolves the fMint account flag for being eligible
// to receive earned rewards. If the collateral to debt ration drop below
// certain value, earned rewards are burned.
func (p *proxy) FMintCanReceiveRewards(addr *common.Address) (bool, error) {
	return p.rpc.FMintCanReceiveRewards(addr)
}

// FMintCanPushRewards signals if there are any rewards unlocked
// on the rewards distribution contract and can be pushed to accounts.
func (p *proxy) FMintCanPushRewards() (bool, error) {
	return p.rpc.FMintCanPushRewards()
}
