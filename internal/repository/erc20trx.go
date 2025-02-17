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
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"go.mongodb.org/mongo-driver/bson"
)

// StoreTokenTransaction stores ERC20/ERC721/ERC1155 transaction into the repository.
func (p *proxy) StoreTokenTransaction(trx *types.TokenTransaction) error {
	return p.pdDB.AddERC20Transaction(trx)
}

// // StoreTokenTransactionPost stores ERC20/ERC721/ERC1155 transaction into the repository.
// func (p *proxy) StoreTokenTransactionPost(trx *types.TokenTransaction) error {
// 	return p.pdDB.AddERC20Transaction(trx)
// }

// TokenTransactionsByCall provides a list of token transaction made inside a specific
// transaction call (blockchain transaction).
func (p *proxy) TokenTransactionsByCall(trxHash *common.Hash) ([]*types.TokenTransaction, error) {
	return p.pdDB.TokenTransactionsByCall(trxHash)
}

// TokenTransactionsByCallPost provides a list of token transaction made inside a specific
// transaction call (blockchain transaction).
// func (p *proxy) TokenTransactionsByCallPost(trxHash *common.Hash) ([]*types.TokenTransaction, error) {
// 	return p.pdDB.TokenTransactionsByCall(trxHash)
// }

// TokenTransactions provides list of ERC20/ERC721/ERC1155 transactions based on given filters.
func (p *proxy) TokenTransactions(tokenType string, token *common.Address, tokenId *big.Int, acc *common.Address, txType []int32, cursor *string, count int32) (*types.TokenTransactionList, error) {
	// prep the filter
	fi := bson.D{}

	// token type (ERC20/ERC721/ERC1155...)
	fi = append(fi, bson.E{
		Key:   types.FiTokenTransactionTokenType,
		Value: tokenType,
	})

	// filter specific token
	if token != nil {
		fi = append(fi, bson.E{
			Key:   types.FiTokenTransactionToken,
			Value: token.String(),
		})
	}

	// filter specific token id (for multi-token contracts)
	if tokenId != nil {
		fi = append(fi, bson.E{
			Key:   types.FiTokenTransactionTokenId,
			Value: (*hexutil.Big)(tokenId).String(),
		})
	}

	// common address (sender or recipient)
	if acc != nil {
		fi = append(fi, bson.E{
			Key: "$or",
			Value: bson.A{bson.D{{
				Key:   types.FiTokenTransactionSender,
				Value: acc.String(),
			}}, bson.D{{
				Key:   types.FiTokenTransactionRecipient,
				Value: acc.String(),
			}}},
		})
	}

	// type of the transaction
	if txType != nil {
		fi = append(fi, bson.E{
			Key: types.FiTokenTransactionType,
			Value: bson.D{{
				Key:   "$in",
				Value: txType,
			}},
		})
	}

	// do loading
	return p.db.Erc20Transactions(cursor, count, &fi)
}

// func (p *proxy) TokenTransactions(
// 	tokenType string, token *common.Address, tokenId *big.Int, acc *common.Address, txType []int32, cursor *string, count int32,
// ) (*types.TokenTransactionList, error) {
// 	// Initialize the filter for ERC20 transactions
// 	filter := &types.TokenTransactionList{}

// 	// Token type (ERC20, ERC721, ERC1155...)
// 	filter.TokenType = tokenType

// 	// Filter specific token
// 	if token != nil {
// 		filter.Token = token.String()
// 	}

// 	// Filter specific token ID
// 	if tokenId != nil {
// 		filter.TokenID = (*hexutil.Big)(tokenId).String()
// 	}

// 	// Common address (sender or recipient)
// 	if acc != nil {
// 		filter.Sender = acc.String()
// 		filter.Recipient = acc.String()
// 	}

// 	// Filter transaction types (for ERC20)
// 	if txType != nil && len(txType) > 0 {
// 		filter.TxType = txType
// 	}

// 	// Call the `Erc20Transactions` function to fetch the list of ERC20 transactions
// 	if tokenType == "ERC20" {
// 		return p.db.Erc20Transactions(cursor, count, filter)
// 	}

// 	// If tokenType is not ERC20, return an error or another appropriate fallback
// 	return nil, fmt.Errorf("unsupported token type: %s", tokenType)
// }

// Erc20Assets provides a list of known assets for the given owner.
func (p *proxy) Erc20Assets(owner common.Address, count int32) ([]common.Address, error) {
	return p.pdDB.Erc20Assets(owner, count)
}

// // Erc20AssetsPost provides a list of known assets for the given owner.
// func (p *proxy) Erc20AssetsPost(owner common.Address, count int32) ([]common.Address, error) {
// 	return p.pdDB.Erc20Assets(owner, count)
// }
