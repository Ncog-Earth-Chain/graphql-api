/*
Package rpc implements bridge to Forest full node API interface.

We recommend using local IPC for fast and the most efficient inter-process communication between the API server
and an Ncogearthchain/Forest node. Any remote RPC connection will work, but the performance may be significantly degraded
by extra networking overhead of remote RPC calls.

You should also consider security implications of opening Forest RPC interface for a remote access.
If you considering it as your deployment strategy, you should establish encrypted channel between the API server
and Forest RPC interface with connection limited to specified endpoints.

We strongly discourage opening Forest RPC interface for unrestricted Internet access.
*/
package rpc

//go:generate tools/abigen.sh --abi ./contracts/abi/price-oracle-proxy-interface.abi --pkg contracts --type PriceOracleProxyInterface --out ./contracts/oracle_proxy.go
//go:generate tools/abigen.sh --abi ./contracts/abi/defi-token-storage.abi --pkg contracts --type DeFiTokenStorage --out ./contracts/token_storage.go
//go:generate tools/abigen.sh --abi ./contracts/abi/defi-fmint-reward-distribution.abi --pkg contracts --type FMintRewardsDistribution --out ./contracts/fmint_rewards.go

import (
	"context"
	"math/big"
	"ncogearthchain-api-graphql/internal/repository/rpc/contracts"
	"ncogearthchain-api-graphql/internal/types"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// fMintRewardsPushTimeBarrier represents the minimal time before a new reward push can be made.
// We use this barrier to subtract from current time, hence the negative value.
const fMintRewardsPushTimeBarrier = time.Duration(-70) * time.Minute

// FMintAccount loads details of a DeFi/fMint protocol account identified by the owner address.
func (nec *NecBridge) FMintAccount(owner *common.Address) (*types.FMintAccount, error) {
	// make the container
	var err error
	da := types.FMintAccount{Address: *owner}

	// load list of collateral tokens
	tokenList, err := nec.DefiTokenList()
	if err != nil {
		nec.log.Errorf("collateral tokens list loader failed; %s", err.Error())
		return nil, err
	}

	// debt tokens are the same
	da.CollateralList = nec.FMintFilterTokens(owner, tokenList, types.DefiTokenTypeCollateral)
	da.DebtList = nec.FMintFilterTokens(owner, tokenList, types.DefiTokenTypeDebt)

	// get the current values of the account tokens on both collateral and debt
	da.CollateralValue, da.DebtValue, err = nec.fMintAccountValue(*owner)
	if err != nil {
		nec.log.Errorf("can not pull account tokens value; %s", err.Error())
		return nil, err
	}

	// return the account detail
	return &da, nil
}

// FMintFilterTokens filter list of tokens to include only tokens with a balance of given type.
func (nec *NecBridge) FMintFilterTokens(owner *common.Address, list []common.Address, tp types.DefiTokenType) []common.Address {
	zero := new(big.Int)
	result := make([]common.Address, 0)
	for _, adr := range list {
		val, err := nec.FMintTokenBalance(owner, &adr, tp)
		if err != nil {
			nec.log.Errorf("failed to get the token balance; %s", err.Error())
			continue
		}

		if 0 != val.ToInt().Cmp(zero) {
			result = append(result, adr)
		}
	}
	return result
}

// FMintPoolBalance loads balance of an fMint token from the given pool contract.
func (nec *NecBridge) FMintPoolBalance(pool *contracts.DeFiTokenStorage, owner *common.Address, token *common.Address) (hexutil.Big, error) {
	// get the collateral token balance
	val, err := pool.BalanceOf(nil, *owner, *token)
	if err != nil {
		nec.log.Debugf("pool balance failed on token %s, account %s; %s", token.String(), owner.String(), err.Error())
		return hexutil.Big{}, err
	}

	// do we have a value?
	if val == nil {
		nec.log.Debugf("token %s balance not available for owner %s", token.String(), owner.String())
		return hexutil.Big{}, nil
	}

	return hexutil.Big(*val), nil
}

// FMintTokenBalance loads balance of a single DeFi token in fMint contract by its address.
func (nec *NecBridge) FMintTokenBalance(owner *common.Address, token *common.Address, tp types.DefiTokenType) (hexutil.Big, error) {
	var err error
	var pool *contracts.DeFiTokenStorage

	// pull the right value based to token type
	switch tp {
	case types.DefiTokenTypeCollateral:
		pool, err = nec.fMintCfg.fMintCollateralPool()
	case types.DefiTokenTypeDebt:
		pool, err = nec.fMintCfg.fMintDebtPool()
	}

	// error in pool loading?
	if err != nil {
		nec.log.Debugf("token storage pool failed to load; %s", err.Error())
		return hexutil.Big{}, err
	}

	// do we have the pool?
	if pool == nil {
		nec.log.Debugf("token storage pool not available")
		return hexutil.Big{}, nil
	}

	return nec.FMintPoolBalance(pool, owner, token)
}

// FMintTokenTotalBalance loads total balance of a single DeFi token by it's address.
func (nec *NecBridge) FMintTokenTotalBalance(token *common.Address, tp types.DefiTokenType) (hexutil.Big, error) {
	var err error
	var pool *contracts.DeFiTokenStorage

	// pull the right value based to token type
	switch tp {
	case types.DefiTokenTypeCollateral:
		pool, err = nec.fMintCfg.fMintCollateralPool()
	case types.DefiTokenTypeDebt:
		pool, err = nec.fMintCfg.fMintDebtPool()
	}

	// error in pool loading?
	if err != nil {
		nec.log.Debugf("token storage pool failed to load; %s", err.Error())
		return hexutil.Big{}, err
	}

	// do we have the pool?
	if pool == nil {
		nec.log.Debugf("token storage pool not available")
		return hexutil.Big{}, nil
	}

	// get the collateral token balance
	val, err := pool.TotalBalance(nil, *token)
	if err != nil {
		nec.log.Debugf("pool total balance failed on token %s", token.String(), err.Error())
		return hexutil.Big{}, err
	}

	// do we have a value?
	if val == nil {
		nec.log.Debugf("token %s total balance not available", token.String())
		return hexutil.Big{}, nil
	}

	return hexutil.Big(*val), nil
}

// FMintTokenValue loads value of a single DeFi token by it's address in fUSD.
func (nec *NecBridge) FMintTokenValue(owner *common.Address, token *common.Address, tp types.DefiTokenType) (hexutil.Big, error) {
	// get the balance
	balance, err := nec.FMintTokenBalance(owner, token, tp)
	if err != nil {
		nec.log.Errorf("token %s balance unknown; %s", token.String(), err.Error())
		return hexutil.Big{}, err
	}

	// get the price for the given token from oracle
	val, err := nec.FMintTokenPrice(token)
	if err != nil {
		nec.log.Errorf("token %s price not available; %s", token.String(), err.Error())
		return hexutil.Big{}, err
	}

	// calculate the target value
	value := new(big.Int).Mul(val.ToInt(), balance.ToInt())
	return hexutil.Big(*value), nil
}

// FMintTokenPrice loads the current price of the given token from on-chain price oracle.
func (nec *NecBridge) FMintTokenPrice(token *common.Address) (hexutil.Big, error) {
	// get the price oracle address
	oracle, err := nec.fMintCfg.priceOracleProxyContract()
	if err != nil {
		return hexutil.Big{}, err
	}

	// get the price for the given token from oracle
	val, err := oracle.GetPrice(nil, *token)
	if err != nil {
		nec.log.Errorf("price not available for token %s; %s", token.String(), err.Error())
		return hexutil.Big{}, nil
	}

	// do we have the value?
	if val == nil {
		nec.log.Debugf("token %s has no value", token.String())
		return hexutil.Big{}, nil
	}

	return hexutil.Big(*val), nil
}

// fMintAccountTokensValue loads total value status of a given fMint account.
func (nec *NecBridge) fMintAccountValue(owner common.Address) (hexutil.Big, hexutil.Big, error) {
	// connect the contract
	contract, err := nec.fMintCfg.fMintMinterContract()
	if err != nil {
		return hexutil.Big{}, hexutil.Big{}, err
	}

	// get joined collateral value
	cValue, err := contract.CollateralValueOf(nil, owner, common.Address{}, new(big.Int))
	if err != nil {
		nec.log.Errorf("joined collateral value loader failed")
		return hexutil.Big{}, hexutil.Big{}, err
	}

	// get joined debt value
	dValue, err := contract.DebtValueOf(nil, owner, common.Address{}, new(big.Int))
	if err != nil {
		nec.log.Errorf("joined debt value loader failed")
		return hexutil.Big{}, hexutil.Big{}, err
	}

	// return the value we got
	return hexutil.Big(*cValue), hexutil.Big(*dValue), nil
}

// FMintRewardsEarned resolves the total amount of rewards
// accumulated on the account for the excessive collateral deposits.
func (nec *NecBridge) FMintRewardsEarned(addr *common.Address) (hexutil.Big, error) {
	// connect the contract
	contract, err := nec.fMintCfg.fMintRewardsDistribution()
	if err != nil {
		return hexutil.Big{}, err
	}

	// get the block height
	bl, err := nec.BlockHeight()
	if err != nil {
		return hexutil.Big{}, err
	}

	// make new call opts so we force the block
	// we want to be used for the calculation
	co := bind.CallOpts{
		Pending:     false,
		From:        *addr,
		BlockNumber: bl.ToInt(),
		Context:     context.Background(),
	}

	// get the rewards
	rw, err := contract.RewardEarned(&co, *addr)
	if err != nil {
		nec.log.Errorf("can not calculate earned rewards; %s", err.Error())
		return hexutil.Big{}, err
	}

	return hexutil.Big(*rw), nil
}

// FMintRewardsStashed resolves the total amount of rewards
// accumulated on the account for the excessive collateral deposits.
func (nec *NecBridge) FMintRewardsStashed(addr *common.Address) (hexutil.Big, error) {
	// connect the contract
	contract, err := nec.fMintCfg.fMintRewardsDistribution()
	if err != nil {
		return hexutil.Big{}, err
	}

	// get the rewards
	rw, err := contract.RewardStash(nil, *addr)
	if err != nil {
		nec.log.Errorf("can not calculate stashed rewards; %s", err.Error())
		return hexutil.Big{}, err
	}

	return hexutil.Big(*rw), nil
}

// FMintCanClaimRewards resolves the fMint account flag for being allowed
// to claim earned rewards.
func (nec *NecBridge) FMintCanClaimRewards(addr *common.Address) (bool, error) {
	// connect the contract
	contract, err := nec.fMintCfg.fMintMinterContract()
	if err != nil {
		nec.log.Errorf("rewards claim check failed; %s", err.Error())
		return false, err
	}

	// ask if the claim is possible
	flag, err := contract.RewardCanClaim(nil, *addr)
	if err != nil {
		nec.log.Errorf("can not check rewards claim flag; %s", err.Error())
		return false, err
	}

	return flag, nil
}

// FMintCanReceiveRewards resolves the fMint account flag for being eligible
// to receive earned rewards. If the collateral to debt ration drop below
// certain value, earned rewards are burned.
func (nec *NecBridge) FMintCanReceiveRewards(addr *common.Address) (bool, error) {
	// connect the contract
	contract, err := nec.fMintCfg.fMintMinterContract()
	if err != nil {
		nec.log.Errorf("rewards eligibility check failed; %s", err.Error())
		return false, err
	}

	// ask if the claim is possible
	flag, err := contract.RewardIsEligible(nil, *addr)
	if err != nil {
		nec.log.Errorf("can not check rewards eligibility flag; %s", err.Error())
		return false, err
	}

	return flag, nil
}

// FMintCanPushRewards signals if there are any rewards unlocked
// on the rewards distribution contract and can be pushed to accounts.
func (nec *NecBridge) FMintCanPushRewards() (bool, error) {
	// connect the contract
	contract, err := nec.fMintCfg.fMintRewardsDistribution()
	if err != nil {
		return false, err
	}

	// get the last time rewards were pushed
	lastPush, err := contract.LastRewardPush(nil)
	if err != nil {
		nec.log.Errorf("can not check rewards last push; %s", err.Error())
		return false, err
	}

	// are we safely within the time barrier?
	// if not simply return false
	if time.Now().Add(fMintRewardsPushTimeBarrier).UTC().Unix() < int64(lastPush.Uint64()) {
		return false, nil
	}

	// @todo Check the amount of rewards available so we know that it will push.
	return true, nil
}
