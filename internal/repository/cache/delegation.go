// Package cache implements bridge to fast in-memory object cache.
package cache

import (
	"encoding/json"
	"ncogearthchain-api-graphql/internal/types"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// delegationCacheKey generates cache key for the given delegation.
// The in-memory cache serialises with encoding/json.
//
// It previously used BSON, which came from the MongoDB driver -- a dependency this
// explorer no longer has. The cache only needs SOME self-describing form: entries are
// written and read by this process alone, are Snappy-compressed on both sides, and are
// discarded on restart. Nothing outside reads them, so the encoding is an internal detail
// rather than a compatibility surface.
//
// The domain types already carry `json` tags for the API, so this reuses a mapping that is
// exercised on every request rather than a second one exercised only here.

func delegationCacheKey(adr common.Address, valID *hexutil.Big) string {
	var key strings.Builder
	key.WriteString("dlg")
	key.WriteString(adr.String())
	key.WriteString("to")
	key.WriteString(valID.String())
	return key.String()
}

// PullDelegation tries to pull delegation from the given address to the given validator
// from internal in-memory cache.
func (b *MemBridge) PullDelegation(adr common.Address, valID *hexutil.Big) *types.Delegation {
	// try to get the account data from the cache
	data, err := b.cache.Get(delegationCacheKey(adr, valID))
	if err != nil {
		return nil
	}

	// do we have the data?
	dlg := new(types.Delegation)
	if err := json.Unmarshal(data, dlg); err != nil {
		b.log.Criticalf("can not decode delegation data from in-memory cache; %s", err.Error())
		return nil
	}
	return dlg
}

// PushDelegation stored the given delegation in memory cache.
func (b *MemBridge) PushDelegation(dlg *types.Delegation) {
	// no need to store nil
	if dlg == nil {
		return
	}

	// encode account
	data, err := json.Marshal(dlg)
	if err != nil {
		b.log.Criticalf("can not marshal delegation of %s to #%d; %s", dlg.Address.String(), dlg.ToStakerId.ToInt().Uint64(), err.Error())
		return
	}

	// set the data to cache by block number
	if err := b.cache.Set(delegationCacheKey(dlg.Address, dlg.ToStakerId), data); err != nil {
		b.log.Criticalf("can not cache delegation of %s to #%d; %s", dlg.Address.String(), dlg.ToStakerId.ToInt().Uint64(), err.Error())
	}
}
