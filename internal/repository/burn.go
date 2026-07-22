/*
Package repository implements repository for handling fast and efficient access to data required
by the resolvers of the API server.

Internally it utilizes RPC to access Ncogearthchain/Forest full node for blockchain interaction. Mongo database
for fast, robust and scalable off-chain data storage, especially for aggregated and pre-calculated data mining
results. BigCache for in-memory object storage to speed up loading of frequently accessed entities.
*/
package repository

import (
	"ncogearthchain-api-graphql/internal/types"
)

// StoreNecBurn stores the given native NEC burn per block record into the persistent storage.
func (p *proxy) StoreNecBurn(burn *types.NecBurn) error {
	p.cache.NecBurnUpdate(burn, p.db.BurnTotal)
	return p.db.StoreBurn(burn)
}

// NecBurnTotal provides the total amount of burned native NEC.
func (p *proxy) NecBurnTotal() (int64, error) {
	return p.cache.NecBurnTotal(p.db.BurnTotal)
}

// NecBurnList provides list of per-block burned native NEC tokens.
func (p *proxy) NecBurnList(count int64) ([]types.NecBurn, error) {
	return p.db.BurnList(count)
}
