// Package db implements bridge to persistent storage represented by Mongo database.
package db

import (
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/lib/pq"
)

// fMintUserTokensRow represents the structure of the fMint user tokens aggregation output row.
type fMintUserTokensRow struct {
	User   string   `bson:"_id"`
	Tokens []string `bson:"tokens"`
}

// fMintUserTokensRow represents the structure of the fMint user tokens aggregation output row for PostgreSQL.
type PostfMintUserTokensRow struct {
	User   string   `json:"user"`   // The user ID or address
	Tokens []string `json:"tokens"` // The list of tokens associated with the user
}

// // FMintUsers loads the list of fMint users and their associated tokens
// // used for a specified transaction type  from the collected database using aggregation pipeline.
// func (db *MongoDbBridge) FMintUsers(tt int32) ([]*types.FMintUserTokens, error) {
// 	// prep the aggregation pipeline to be executed
// 	ap := mongo.Pipeline{
// 		/* match transactions of the given trx type */
// 		{{Key: "$match", Value: bson.D{
// 			{Key: "typ", Value: tt},
// 		}}},
// 		/* group by user account, collect list of tokens */
// 		{{Key: "$group", Value: bson.D{
// 			{Key: "_id", Value: "$usr"},
// 			{Key: "tokens", Value: bson.D{
// 				{Key: "$addToSet", Value: "$tok"},
// 			}},
// 		}}},
// 	}

// 	// make output container
// 	list := make([]*types.FMintUserTokens, 0)

// 	// execute aggregation pipeline on the fMint transactions collection and collect results
// 	col := db.client.Database(db.dbName).Collection(colFMintTransactions)
// 	cursor, err := col.Aggregate(context.Background(), ap)
// 	if err != nil {
// 		db.log.Errorf("can not aggregate fMint users; %s", err.Error())
// 		return nil, err
// 	}

// 	defer func() {
// 		if err := cursor.Close(context.Background()); err != nil {
// 			db.log.Errorf("can not close cursor; %s", err.Error())
// 		}
// 	}()

// 	// iterate through results and construct data
// 	for cursor.Next(context.Background()) {
// 		var row fMintUserTokensRow
// 		if err := cursor.Decode(&row); err != nil {
// 			db.log.Errorf("can not decode aggregation row; %s", err.Error())
// 			return nil, err
// 		}
// 		list = append(list, &types.FMintUserTokens{
// 			Purpose: tt,
// 			User:    common.HexToAddress(row.User),
// 			Tokens:  decodeFMintTokensList(row.Tokens),
// 		})
// 	}
// 	return list, nil
// }

// FMintUsers loads the list of fMint users and their associated tokens
// used for a specified transaction type from the database.
func (db *PostgreSQLBridge) FMintUsers(tt int32) ([]*types.FMintUserTokens, error) {
	// Prepare the SQL query
	query := `
        SELECT usr AS user, ARRAY_AGG(tok) AS tokens
        FROM fmint_transactions
        WHERE typ = $1
        GROUP BY usr`

	// Execute the query
	rows, err := db.db.Query(query, tt)
	if err != nil {
		db.log.Errorf("cannot aggregate fMint users; %s", err.Error())
		return nil, err
	}
	defer rows.Close()

	// Make output container
	list := make([]*types.FMintUserTokens, 0)

	// Iterate through results and construct data
	for rows.Next() {
		var user string
		var tokens []string

		if err := rows.Scan(&user, pq.Array(&tokens)); err != nil {
			db.log.Errorf("cannot decode aggregation row; %s", err.Error())
			return nil, err
		}

		// Append the result to the list
		list = append(list, &types.FMintUserTokens{
			Purpose: tt,
			User:    common.HexToAddress(user),
			Tokens:  decodeFMintTokensList(tokens),
		})
	}

	// Handle any errors encountered during iteration
	if err := rows.Err(); err != nil {
		db.log.Errorf("error iterating fMint user rows; %s", err.Error())
		return nil, err
	}

	return list, nil
}

// decodeFMintTokensList decodes a list of hex coded address into a list
// of typed address instance.
func decodeFMintTokensList(in []string) []common.Address {
	list := make([]common.Address, len(in))
	for i, a := range in {
		list[i] = common.HexToAddress(a)
	}
	return list
}
