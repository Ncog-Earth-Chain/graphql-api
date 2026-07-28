// Package resolvers implements GraphQL resolvers to incoming API requests.
package resolvers

import (
	"context"
	"fmt"
	"ncogearthchain-api-graphql/internal/repository"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Epoch represents a resolvable Epoch representation
type Epoch struct {
	types.Epoch
}

// Epoch resolves information about epoch of the given id.
//
// Served from the index. A sealed epoch is an immutable row the scanner has already stored,
// and reading it went to the SFC contract over RPC every time.
func (rs *rootResolver) Epoch(ctx context.Context, args *struct{ Id *hexutil.Uint64 }) (Epoch, error) {
	epo, err := repository.R().IndexedEpoch(ctx, args.Id)
	if err != nil {
		return Epoch{}, err
	}
	if epo == nil {
		return Epoch{}, fmt.Errorf("epoch not found")
	}
	return Epoch{*epo}, nil
}

// Duration resolves the time length of the given epoch
//
// This doubles the cost of every epoch resolved, because the length of an epoch is only
// knowable by comparing it with the one before -- so a page of epochs used to be two SFC
// contract calls per row.
func (ep Epoch) Duration(ctx context.Context) hexutil.Uint64 {
	// no length for the first epochs
	if uint64(ep.Id) < 2 {
		return 0
	}

	// get the previous epoch so we can compare end times
	pid := uint64(ep.Id) - 1
	prev, err := repository.R().IndexedEpoch(ctx, (*hexutil.Uint64)(&pid))
	if err != nil || prev == nil {
		return 0
	}

	// can we even calculate the duration?
	if ep.EndTime < prev.EndTime {
		return 0
	}
	return ep.EndTime - prev.EndTime
}
