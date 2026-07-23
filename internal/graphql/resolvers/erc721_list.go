// Package resolvers implements GraphQL resolvers to incoming API requests.
package resolvers

import (
	"context"
	"ncogearthchain-api-graphql/internal/repository"
)

// Erc721ContractList resolves an instance of ERC721 token list if available.
func (rs *rootResolver) Erc721ContractList(ctx context.Context, args struct{ Count int32 }) ([]*ERC721Contract, error) {
	// limit query size; the count can be either positive or negative
	// this controls the loading direction
	args.Count = listLimitCount(args.Count, listMaxEdgesPerRequest)

	// get the list of addresses of active tokens
	al, err := repository.R().Erc721ContractsList(ctx, args.Count)
	if err != nil {
		return nil, err
	}

	// make the container and create resolvable
	list := make([]*ERC721Contract, len(al))
	for i, adr := range al {
		list[i] = NewErc721Contract(&adr)
	}

	return list, nil
}
