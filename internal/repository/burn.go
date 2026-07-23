/*
Package repository implements repository for handling fast and efficient access to data required
by the resolvers of the API server.

Internally it utilizes RPC to access Ncogearthchain/Forest full node for blockchain interaction. Mongo database
for fast, robust and scalable off-chain data storage, especially for aggregated and pre-calculated data mining
results. BigCache for in-memory object storage to speed up loading of frequently accessed entities.
*/
package repository

import (
	"context"
	"ncogearthchain-api-graphql/internal/types"
)

// StoreNecBurn stores the given native NEC burn per block record into the persistent storage.
func (p *proxy) StoreNecBurn(ctx context.Context, burn *types.NecBurn) error {
	p.cache.NecBurnUpdate(burn, func() (int64, error) { return p.burnTotal(ctx) })
	return p.pg.StoreBurn(ctx, burn)
}

// NecBurnTotal provides the total amount of burned native NEC.
func (p *proxy) NecBurnTotal(ctx context.Context) (int64, error) {
	return p.cache.NecBurnTotal(func() (int64, error) { return p.burnTotal(ctx) })
}

// NecBurnList provides list of per-block burned native NEC tokens.
func (p *proxy) NecBurnList(ctx context.Context, count int64) ([]types.NecBurn, error) {
	return p.pg.BurnList(ctx, int32(count))
}
