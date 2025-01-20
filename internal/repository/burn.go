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
	p.cache.NecBurnUpdate(burn, p.pdDB.BurnTotal)
	return p.pdDB.StoreBurn(burn)
}

// StoreNecBurnPost stores the given native NEC burn per block record into the persistent storage.
// func (p *proxy) StoreNecBurnPost(burn *types.NecBurn) error {
// 	// Update the cache with the NEC burn data
// 	p.cache.NecBurnUpdate(burn, p.pdDB.BurnTotal)

// 	// Store the NEC burn data in the PostgreSQL database
// 	return p.pdDB.StoreBurn(burn)
// }

// NecBurnTotal provides the total amount of burned native NEC.
func (p *proxy) NecBurnTotal() (int64, error) {
	return p.cache.NecBurnTotal(p.pdDB.BurnTotal)
}

//  NecBurnTotalPost provides the total amount of burned native NEC.
// func (p *proxy) NecBurnTotalPost() (int64, error) {
// 	return p.cache.NecBurnTotal(p.pdDB.BurnTotal)
// }

// NecBurnList provides list of per-block burned native NEC tokens.
func (p *proxy) NecBurnList(count int64) ([]types.NecBurn, error) {
	return p.pdDB.BurnList(count)
}

// NecBurnListPost provides list of per-block burned native NEC tokens.
// func (p *proxy) NecBurnListPost(count int64) ([]types.NecBurn, error) {
// 	return p.pdDB.BurnList(count)
// }
