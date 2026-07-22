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

import (
	"fmt"
	"math/big"
	"ncogearthchain-api-graphql/internal/repository/rpc/contracts"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

//go:generate tools/abigen.sh --abi ./contracts/abi/defi-fmint-address-provider.abi --pkg contracts --type DefiFMintAddressProvider --out ./contracts/fmint_addresses.go

// tConfigItemsLoaders defines a map between DeFi config elements and their respective loaders.
type tConfigItemsLoaders map[*hexutil.Big]func(*bind.CallOpts) (*big.Int, error)

// DefiConfiguration resolves the current DeFi contract settings.
func (nec *NecBridge) DefiConfiguration() (*types.DefiSettings, error) {
	// access the contract
	contract, err := nec.fMintCfg.fMintMinterContract()
	if err != nil {
		return nil, err
	}

	// create the container
	ds := types.DefiSettings{
		FMintContract:           nec.fMintCfg.mustContractAddress(fMintAddressMinter),
		FMintAddressProvider:    nec.fMintCfg.addressProvider,
		FMintTokenRegistry:      nec.fMintCfg.mustContractAddress(fMintAddressTokenRegistry),
		FMintRewardDistribution: nec.fMintCfg.mustContractAddress(fMintAddressRewardDistribution),
		FMintCollateralPool:     nec.fMintCfg.mustContractAddress(fMintCollateralPool),
		FMintDebtPool:           nec.fMintCfg.mustContractAddress(fMintDebtPool),
		PriceOracleAggregate:    nec.fMintCfg.mustContractAddress(fMintAddressPriceOracleProxy),
	}

	// prep to load certain values
	loaders := tConfigItemsLoaders{
		&ds.MintFee4:               contract.GetFMintFee4dec,
		&ds.MinCollateralRatio4:    contract.GetCollateralLowestDebtRatio4dec,
		&ds.RewardCollateralRatio4: contract.GetRewardEligibilityRatio4dec,
	}

	// load all the configured values
	if err := nec.pullSetOfDefiConfigValues(loaders); err != nil {
		nec.log.Errorf("can not pull defi config values; %s", err.Error())
		return nil, err
	}

	// load the decimals correction
	if ds.Decimals, err = nec.pullDefiDecimalCorrection(contract); err != nil {
		nec.log.Errorf("can not pull defi decimals correction; %s", err.Error())
		return nil, err
	}

	// return the config
	return &ds, nil
}

// pullSetOfDefiConfigValues pulls set of DeFi configuration values for the given
// config loaders map.
func (nec *NecBridge) pullDefiDecimalCorrection(con *contracts.DefiFMintMinter) (int32, error) {
	// load the decimals correction
	val, err := nec.pullDefiConfigValue(con.FMintFeeDigitsCorrection)
	if err != nil {
		nec.log.Errorf("can not pull decimals correction; %s", err.Error())
		return 0, err
	}

	// calculate number of decimals
	var dec int32
	var value = val.ToInt().Uint64()
	for value > 1 {
		value /= 10
		dec++
	}

	// convert and return
	return dec, nil
}

// pullSetOfDefiConfigValues pulls set of DeFi configuration values for the given
// config loaders map.
func (nec *NecBridge) pullSetOfDefiConfigValues(loaders tConfigItemsLoaders) error {
	// collect loaders error
	var err error

	// loop the map and load the values
	for ref, fn := range loaders {
		*ref, err = nec.pullDefiConfigValue(fn)
		if err != nil {
			return err
		}
	}

	return nil
}

// tradeFee4 pulls DeFi trading fee from the Liquidity Pool contract.
func (nec *NecBridge) pullDefiConfigValue(cf func(*bind.CallOpts) (*big.Int, error)) (hexutil.Big, error) {
	// pull the trading fee value
	val, err := cf(nil)
	if err != nil {
		return hexutil.Big{}, err
	}

	// do we have the value? we should always have
	if val == nil {
		return hexutil.Big{}, fmt.Errorf("defi config value not available")
	}

	return hexutil.Big(*val), nil
}
