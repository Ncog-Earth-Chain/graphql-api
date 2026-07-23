/*
Package repository implements repository for handling fast and efficient access to data required
by the resolvers of the API server.

Internally it utilizes RPC to access Ncogearthchain/Forest full node for blockchain interaction. Mongo database
for fast, robust and scalable off-chain data storage, especially for aggregated and pre-calculated data mining
results. BigCache for in-memory object storage to speed up loading of frequently accessed entities.
*/
package repository

import (
	"math/big"
	"ncogearthchain-api-graphql/internal/repository/db/pg"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
)

// StoreTokenTransaction stores ERC20/ERC721/ERC1155 transaction into the repository.
func (p *proxy) StoreTokenTransaction(trx *types.TokenTransaction) error {
	return p.pg.StoreTokenTransaction(storeCtx(), trx)
}

// TokenTransactionsByCall provides a list of token transaction made inside a specific
// transaction call (blockchain transaction).
func (p *proxy) TokenTransactionsByCall(trxHash *common.Hash) ([]*types.TokenTransaction, error) {
	return p.pg.TokenTransactionsByCall(storeCtx(), trxHash)
}

// TokenTransactions provides list of ERC20/ERC721/ERC1155 transactions based on given filters.
//
// The 40 lines of bson.D this replaces were the clearest instance of the storage layer
// not being a seam: MongoDB's query language was built HERE, above it, so the storage
// could not be swapped without rewriting its callers. The criteria are now a typed struct
// the store renders to SQL itself.
func (p *proxy) TokenTransactions(tokenType string, token *common.Address, tokenId *big.Int, acc *common.Address, txType []int32, cursor *string, count int32) (*types.TokenTransactionList, error) {
	return p.pg.TokenTransactions(storeCtx(), pg.TokenTxCriteria{
		TokenType:  tokenType,
		Token:      token,
		TokenId:    tokenId,
		Account:    acc,
		EventTypes: txType,
	}, derefCursor(cursor), count)
}

// Erc20Assets provides a list of known assets for the given owner.
func (p *proxy) Erc20Assets(owner common.Address, count int32) ([]common.Address, error) {
	return p.pg.TokenAssetsByOwner(storeCtx(), &owner, types.AccountTypeERC20Token, count)
}
