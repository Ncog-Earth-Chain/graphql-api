/*
Package repository implements repository for handling fast and efficient access to data required
by the resolvers of the API server.

Internally it utilizes RPC to access Ncogearthchain/Forest full node for blockchain interaction. Mongo database
for fast, robust and scalable off-chain data storage, especially for aggregated and pre-calculated data mining
results. BigCache for in-memory object storage to speed up loading of frequently accessed entities.
*/
package repository

import (
	"bytes"
	"context"
	"math/big"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// sfcDecimalUnit represents decimal units adjustment used by SFC contract
// on certain values calculation to preserve calculations precision.
var sfcDecimalUnit = new(big.Int).SetUint64(1e18)

// SfcDecimalUnit returns the decimal unit adjustment used by the SFC contract.
func (p *proxy) SfcDecimalUnit() *big.Int {
	return sfcDecimalUnit
}

// SfcVersion returns current version of the SFC contract.
func (p *proxy) SfcVersion() (hexutil.Uint64, error) {
	return p.rpc.SfcVersion()
}

// SfcConfiguration provides SFC contract configuration.
func (p *proxy) SfcConfiguration() (*types.SfcConfig, error) {
	// try cache first
	c := p.cache.PullSfcConfig()
	if c == nil {
		// load the config with all the values filled
		c = &types.SfcConfig{
			MinValidatorStake:      p.pullSfcConfigValue(p.rpc.SfcMinValidatorStake),
			MaxDelegatedRatio:      p.pullSfcConfigValue(p.rpc.SfcMaxDelegatedRatio),
			MinLockupDuration:      p.pullSfcConfigValue(p.rpc.SfcMinLockupDuration),
			MaxLockupDuration:      p.pullSfcConfigValue(p.rpc.SfcMaxLockupDuration),
			WithdrawalPeriodEpochs: p.pullSfcConfigValue(p.rpc.SfcWithdrawalPeriodEpochs),
			WithdrawalPeriodTime:   p.pullSfcConfigValue(p.rpc.SfcWithdrawalPeriodTime),
		}
		// cache for future use
		p.cache.PushSfcConfig(c)
	}
	return c, nil
}

// pullSfcConfigValue pulls SFC config value for the given value loader function.
func (p *proxy) pullSfcConfigValue(f func() (*big.Int, error)) hexutil.Big {
	val, err := f()
	if err != nil {
		p.log.Errorf("can not load SFC config value; %s", err.Error())
		return hexutil.Big{}
	}
	return (hexutil.Big)(*val)
}

// CurrentEpoch returns the id of the current epoch.
func (p *proxy) CurrentEpoch() (hexutil.Uint64, error) {
	return p.rpc.CurrentEpoch()
}

// IndexedEpoch returns a sealed epoch for the READ path: from the index when it is there,
// from the SFC contract when it is not.
//
// Separate from Epoch rather than replacing it, for the same reason IndexedTransaction and
// IndexedBlockByNumber are separate. The epoch SCANNER calls Epoch to fetch the epochs it is
// about to store (svc/scan_epochs.go), and CurrentSealedEpoch calls it for the head; serving
// either from the index would be circular -- the scanner would read back what it had already
// written and could never discover an epoch it had not yet recorded.
//
// A nil or zero id means "the current sealed epoch", whose identity has to be asked of the
// chain, so that resolves through the node exactly as before.
//
// An epoch is immutable once sealed (AddEpoch is deliberately ON CONFLICT DO NOTHING), so
// unlike a block there is no version of this row that can go stale underneath the reader.
func (p *proxy) IndexedEpoch(ctx context.Context, id *hexutil.Uint64) (*types.Epoch, error) {
	if id == nil || *id == 0 {
		return p.Epoch(id)
	}

	// The in-memory cache is checked first either way; it is keyed by id and holds the
	// same immutable record.
	if ep := p.cache.PullEpoch(id); ep != nil {
		return ep, nil
	}

	ep, err := p.pg.Epoch(ctx, uint64(*id))
	if err != nil {
		return nil, err
	}
	if ep != nil {
		p.cache.PushEpoch(ep)
		return ep, nil
	}

	// Not recorded yet -- ahead of the epoch scanner, or never sealed.
	return p.Epoch(id)
}

// Epoch returns the structure of the current epoch.
func (p *proxy) Epoch(id *hexutil.Uint64) (*types.Epoch, error) {
	// get the current epoch if the id has not been provided
	if id == nil || *id == 0 {
		// ask for the sealed epoch id
		val, err := p.rpc.CurrentSealedEpoch()
		if err != nil {
			return nil, err
		}
		id = &val
	}

	// try the cache first
	ep := p.cache.PullEpoch(id)
	if ep != nil {
		return ep, nil
	}

	// pull from remote
	ep, err := p.rpc.Epoch(*id)
	if err != nil {
		return nil, err
	}

	// cache for future use
	p.cache.PushEpoch(ep)
	return ep, nil
}

// CurrentSealedEpoch returns the data of the latest sealed epoch.
// This is used for reward estimation calculation and we don't need
// real time data, but rather faster response time.
// So, we use cache for handling the response.
// It will not be updated in sync with the SFC contract.
// If you need real time response, please use the Epoch(id) function instead.
func (p *proxy) CurrentSealedEpoch() (*types.Epoch, error) {
	// inform what we do
	p.log.Debug("latest sealed epoch requested")

	// we need to go the slow path
	id, err := p.rpc.CurrentSealedEpoch()
	if err != nil {
		p.log.Errorf("can not get the id of the last sealed epoch; %s", err.Error())
		return nil, err
	}
	return p.Epoch(&id)
}

// TotalStaked calculates current total staked amount for all stakers.
func (p *proxy) TotalStaked() (*hexutil.Big, error) {
	// try cache first
	value := p.cache.PullTotalStaked()
	if value != nil {
		p.log.Debugf("total staked amount loaded from memory cache")
		return value, nil
	}

	// get the actual live value
	total, err := p.rpc.TotalStaked()
	if err != nil {
		p.log.Errorf("can not get the total staked amount; %s", err.Error())
		return nil, err
	}

	// store in cache
	if err := p.cache.PushTotalStaked((*hexutil.Big)(total)); err != nil {
		// log issue
		p.log.Errorf("can not store total staked amount in memory; %s", err.Error())
	}

	// return the value
	return (*hexutil.Big)(total), nil
}

// RewardsAllowed returns the reward lock status from SFC.
func (p *proxy) RewardsAllowed() (bool, error) {
	return p.rpc.RewardsAllowed()
}

// LockingAllowed indicates if the stake locking has been enabled in SFC.
func (p *proxy) LockingAllowed() (bool, error) {
	return p.rpc.LockingAllowed()
}

// IsSfcContract returns true if the given address points to the SFC contract.
func (p *proxy) IsSfcContract(addr *common.Address) bool {
	return bytes.Equal(addr.Bytes(), p.cfg.Staking.SFCContract.Bytes())
}

// LastKnownEpoch returns the id of the last known and scanned epoch.
func (p *proxy) LastKnownEpoch(ctx context.Context) (uint64, error) {
	return p.pg.LastKnownEpoch(ctx)
}

// AddEpoch stores an epoch reference in connected persistent storage.
func (p *proxy) AddEpoch(ctx context.Context, e *types.Epoch) error {
	return p.pg.AddEpoch(ctx, e)
}

// Epochs pulls list of epochs starting at the specified cursor.
func (p *proxy) Epochs(ctx context.Context, cursor *string, count int32) (*types.EpochList, error) {
	return p.pg.Epochs(ctx, cursor, count)
}
