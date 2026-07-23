// Package cache implements bridge to fast in-memory object cache.
package cache

import (
	"encoding/json"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/klauspost/compress/s2"
)

// PullTransaction extracts transaction information from the in-memory cache if available.
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

func (b *MemBridge) PullTransaction(hash *common.Hash) *types.Transaction {
	// try to get the account data from the cache
	data, err := b.cache.Get(hash.String())
	if err != nil {
		// cache returns ErrEntryNotFound if the key does not exist
		return nil
	}

	// decode compressed transaction data from Snappy S2
	data, err = s2.Decode(nil, data)
	if err != nil {
		return nil
	}

	// do we have the data?
	trx := new(types.Transaction)
	if err := json.Unmarshal(data, trx); err != nil {
		b.log.Criticalf("can not decode transaction data from in-memory cache; %s", err.Error())
		return nil
	}
	return trx
}

// PushTransaction stores provided transaction in the in-memory cache.
func (b *MemBridge) PushTransaction(trx *types.Transaction) {
	// we need valid account
	if nil == trx {
		b.log.Errorf("undefined transaction can not be pushed to the in-memory cache")
		return
	}

	// encode account
	data, err := json.Marshal(trx)
	if err != nil {
		b.log.Criticalf("can not marshal transaction %s; %s", trx.Hash.String(), err.Error())
		return
	}

	// recover from s2 encoder panic
	defer func() {
		if r := recover(); r != nil {
			b.log.Criticalf("can not encode transaction")
		}
	}()

	// set the data to cache by block number
	if err := b.cache.Set(trx.Hash.String(), s2.Encode(nil, data)); err != nil {
		b.log.Criticalf("can not cache transaction %s; %s", trx.Hash.String(), err.Error())
	}
}
