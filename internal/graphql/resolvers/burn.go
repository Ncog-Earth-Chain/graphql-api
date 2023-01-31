// Package resolvers implements GraphQL resolvers to incoming API requests.
package resolvers

import (
	"math/big"
	"ncogearthchain-api-graphql/internal/repository"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common/hexutil"
)

// NecBurnedTotal resolves total amount of burned NEC tokens in WEI units.
func (rs *rootResolver) NecBurnedTotal() hexutil.Big {
	val, err := repository.R().NecBurnTotal()
	if err != nil {
		log.Criticalf("failed to load burned total; %s", err.Error())
		return hexutil.Big{}
	}
	return hexutil.Big(*new(big.Int).Mul(big.NewInt(val), types.BurnDecimalsCorrection))
}

// NecBurnedTotalAmount resolves total amount of burned NEC tokens in NEC units.
func (rs *rootResolver) NecBurnedTotalAmount() float64 {
	val, err := repository.R().NecBurnTotal()
	if err != nil {
		log.Criticalf("failed to load burned total; %s", err.Error())
		return 0
	}
	return float64(val) / types.BurnNECDecimalsCorrection
}

// NecLatestBlockBurnList resolves a list of the latest block burns.
func (rs *rootResolver) NecLatestBlockBurnList(args struct{ Count int32 }) ([]types.NecBurn, error) {
	if args.Count < 1 || args.Count > 50 {
		args.Count = 25
	}
	return repository.R().NecBurnList(int64(args.Count))
}
