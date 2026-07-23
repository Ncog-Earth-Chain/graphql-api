// Package resolvers implements GraphQL resolvers to incoming API requests.
package resolvers

import (
	"ncogearthchain-api-graphql/internal/repository"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
)

// DefiConfiguration represents a resolvable DeFi Configuration instance.
type DefiConfiguration struct {
	types.DefiSettings
}

// NewDefiConfiguration creates a new instance of resolvable DeFi token.
func NewDefiConfiguration(cf *types.DefiSettings) *DefiConfiguration {
	return &DefiConfiguration{
		DefiSettings: *cf,
	}
}

// DefiConfiguration resolves the current DeFi contract settings.
func (rs *rootResolver) DefiConfiguration() (*DefiConfiguration, error) {
	// pass the call to repository
	st, err := repository.R().DefiConfiguration()
	if err != nil {
		return nil, err
	}

	return NewDefiConfiguration(st), nil
}

// StakeTokenizerContract returns the address of the Stake Tokenizer contract
// from the app configuration.
func (dfc *DefiConfiguration) StakeTokenizerContract() common.Address {
	return cfg.Staking.TokenizerContract
}

// StakeTokenizedERC20Token returns the address of the ERC20 token representing
// the tokenized locked stake.
func (dfc *DefiConfiguration) StakeTokenizedERC20Token() common.Address {
	return cfg.Staking.TokenizedStakeToken
}
