/*
Package repository implements repository for handling fast and efficient access to data required
by the resolvers of the API server.
*/
package repository

import (
	"math/big"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// SFC stake accessors, read live from the SFC contract.
//
// Stake DELEGATION is not offered on this chain: the persisted `delegation` table and its whole
// read/write path (StoreDelegation, UpdateDelegationBalance, Delegation, DelegationsByAddress[All],
// DelegationsOfValidator, IsDelegating) have been removed. What remains here are live SFC-contract
// accessors -- none of them touch the removed table.
//
//   - DelegationAmountStaked and DelegationLock ARE live: the validator (Staker) resolver reads a
//     validator's own stake and lock through them (on SFCv3 a validator's self-stake is modeled as
//     a self-delegation).
//   - DelegationAmountUnlocked, DelegationUnlockPenalty, PendingRewards, DelegationOutstandingSNEC,
//     DelegationTokenizerUnlocked and DelegationFluidStakingActive are currently UNREFERENCED (they
//     were used only by the removed Delegation resolver). They are retained deliberately as thin
//     SFC bindings for the planned reintroduction of stake delegation, not left dead by accident.

// DelegationAmountStaked returns the current amount of staked tokens for the given (address, validator).
func (p *proxy) DelegationAmountStaked(addr *common.Address, valID *hexutil.Big) (*big.Int, error) {
	val, err := p.rpc.AmountStaked(addr, (*big.Int)(valID))
	if err != nil {
		p.log.Errorf("can not get amount staked by %s to %d; %s", addr.String(), valID.ToInt().Uint64(), err.Error())
		return nil, err
	}
	p.log.Debugf("%s staked %d to %d", addr.String(), val.Uint64(), valID.ToInt().Uint64())
	return val, nil
}

// DelegationLock returns stake lock information using the SFC contract binding.
func (p *proxy) DelegationLock(addr *common.Address, valID *hexutil.Big) (*types.DelegationLock, error) {
	p.log.Debugf("loading lock information for %s to #%d", addr.String(), valID.ToInt().Uint64())
	return p.rpc.DelegationLock(addr, valID)
}

// DelegationAmountUnlocked returns the unlocked stake amount using the SFC contract binding.
func (p *proxy) DelegationAmountUnlocked(addr *common.Address, valID *big.Int) (hexutil.Big, error) {
	p.log.Debugf("loading unlocked amount for %s to #%d", addr.String(), valID.Uint64())

	val, err := p.rpc.AmountStakeUnlocked(addr, valID)
	if err != nil {
		return hexutil.Big{}, err
	}
	return hexutil.Big(*val), nil
}

// DelegationUnlockPenalty returns the amount of penalty applied on a given stake unlock.
func (p *proxy) DelegationUnlockPenalty(addr *common.Address, valID *big.Int, amount *big.Int) (hexutil.Big, error) {
	p.log.Debugf("checking unlock of %d penalty for %s to #%d", amount.Uint64(), addr.String(), valID.Uint64())

	val, err := p.rpc.StakeUnlockPenalty(addr, valID, amount)
	if err != nil {
		return hexutil.Big{}, err
	}
	return hexutil.Big(*val), nil
}

// PendingRewards returns a detail of pending rewards for the given stake address and validator ID.
func (p *proxy) PendingRewards(addr *common.Address, valID *hexutil.Big) (*types.PendingRewards, error) {
	p.log.Debugf("loading pending rewards of %s to #%d", addr.String(), valID.ToInt().Uint64())
	return p.rpc.PendingRewards(addr, valID.ToInt())
}

// DelegationOutstandingSNEC returns the amount of sNEC tokens for the stake identified by the
// address and the stakerId.
func (p *proxy) DelegationOutstandingSNEC(addr *common.Address, toStaker *hexutil.Big) (*hexutil.Big, error) {
	val, err := p.rpc.DelegationOutstandingSNEC(addr, toStaker.ToInt())
	if err != nil {
		return nil, err
	}
	return (*hexutil.Big)(val), nil
}

// DelegationTokenizerUnlocked returns the status of the SFC Tokenizer lock for the stake identified
// by the address and staker id.
func (p *proxy) DelegationTokenizerUnlocked(addr *common.Address, toStaker *hexutil.Big) (bool, error) {
	return p.rpc.DelegationTokenizerUnlocked(addr, toStaker.ToInt())
}

// DelegationFluidStakingActive signals if the stake is upgraded to the Fluid Staking model.
func (p *proxy) DelegationFluidStakingActive(_ *common.Address, _ *hexutil.Big) (bool, error) {
	return true, nil
}
