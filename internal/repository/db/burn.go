// Package db implements bridge to persistent storage represented by Mongo database.
package db

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/lib/pq"
	_ "github.com/lib/pq"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// colBurns represents the name of the native NEC burns collection in database.
const colBurns = "burns"

// initBurnsCollection initializes the burn collection indexes.
func (db *MongoDbBridge) initBurnsCollection(col *mongo.Collection) {
	// prepare index models
	ix := make([]mongo.IndexModel, 0)

	// index delegator + validator
	unique := true
	ix = append(ix, mongo.IndexModel{
		Keys: bson.D{
			{Key: "block", Value: 1},
		},
		Options: &options.IndexOptions{
			Unique: &unique,
		},
	})

	// create indexes
	if _, err := col.Indexes().CreateMany(context.Background(), ix); err != nil {
		db.log.Panicf("can not create indexes for withdrawals collection; %s", err.Error())
	}

	// log we are done that
	db.log.Debugf("burns collection initialized")
}

// initBurnsCollection initializes the burn collection indexes in PostgreSQL.
func (db *PostgreSQLBridge) initBurnsCollection() {
	// Prepare SQL queries for creating indexes

	// Index on 'block' column (unique index)
	indexQuery := `CREATE UNIQUE INDEX IF NOT EXISTS idx_burns_block ON burns (block)`

	// Execute the query to create the index
	_, err := db.db.Exec(indexQuery)
	if err != nil {
		db.log.Panicf("could not create indexes for burns table; %s", err.Error())
	}

	// Log that we are done
	db.log.Debugf("burns table indexes initialized")
}

// StoreBurn stores the given native NEC burn record.
func (db *MongoDbBridge) StoreBurn(burn *types.NecBurn) error {
	if burn == nil {
		return nil
	}

	col := db.client.Database(db.dbName).Collection(colBurns)

	// make sure burns collection is initialized
	if db.initBurns != nil {
		db.initBurns.Do(func() { db.initBurnsCollection(col); db.initBurns = nil })
	}

	// try to find existing burn
	sr := col.FindOne(context.Background(), bson.D{{Key: "block", Value: burn.BlockNumber}})
	if sr.Err() != nil {
		// if the burn has not been found, add this as a new one
		if sr.Err() == mongo.ErrNoDocuments {
			_, err := col.InsertOne(context.Background(), burn)
			return err
		}

		db.log.Errorf("could not load NEC burn at #%d; %s", burn.BlockNumber, sr.Err())
		return sr.Err()
	}

	// decode existing burn and update
	var ex types.NecBurn
	if err := sr.Decode(&sr); err != nil {
		db.log.Errorf("could not decode NEC burn at #%d; %s", burn.BlockNumber, sr.Err())
		return sr.Err()
	}

	// all the transactions can already be included
	if ex.TxList != nil && burn.TxList != nil {
		var found int

		for _, in := range burn.TxList {
			for _, e := range ex.TxList {
				if bytes.Compare(in.Bytes(), e.Bytes()) == 0 {
					found++
					break
				}
			}
		}

		// do we have them all? if so, we have nothing to do here
		if found == len(burn.TxList) {
			return nil
		}

		// we can not handle partial update (some transactions are already included, but not all)
		if found > 0 && found < len(burn.TxList) {
			db.log.Criticalf("invalid partial burn received at #%d", burn.BlockNumber)
			return fmt.Errorf("partial burn update rejected at #%d", burn.BlockNumber)
		}
	}

	// add the new value to the existing one
	val := new(big.Int).Add((*big.Int)(&ex.Amount), (*big.Int)(&burn.Amount))
	ex.Amount = (hexutil.Big)(*val)

	// update the list of included transactions
	if burn.TxList != nil && len(burn.TxList) > 0 {
		if ex.TxList == nil {
			ex.TxList = make([]common.Hash, 0)
		}

		for _, v := range burn.TxList {
			ex.TxList = append(ex.TxList, v)
		}
	}

	// update the record
	_, err := col.UpdateOne(context.Background(), bson.D{{Key: "block", Value: ex.BlockNumber}}, bson.D{{Key: "$set", Value: ex}})
	return err
}

// StoreBurn stores the given native NEC burn record in PostgreSQL.
func (db *PostgreSQLBridge) StoreBurn(burn *types.NecBurn) error {
	if burn == nil {
		return nil
	}

	// Prepare SQL queries for insert and update
	insertQuery := `INSERT INTO burns (block, amount, tx_list) VALUES ($1, $2, $3) RETURNING id`
	updateQuery := `UPDATE burns SET amount = $1, tx_list = $2 WHERE block = $3`

	// Start a transaction
	tx, err := db.db.Begin()
	if err != nil {
		db.log.Errorf("could not start transaction: %s", err.Error())
		return err
	}
	defer tx.Rollback() // Ensure rollback in case of an error

	// Try to find the existing burn
	var ex types.NecBurn
	err = tx.QueryRow("SELECT id, block, amount, tx_list FROM burns WHERE block = $1", burn.BlockNumber).Scan(&ex.ID, &ex.BlockNumber, &ex.Amount, &ex.TxList)
	if err != nil {
		if err == sql.ErrNoRows {
			// If burn not found, insert new record
			_, err := tx.Exec(insertQuery, burn.BlockNumber, burn.Amount, burn.TxList)
			if err != nil {
				db.log.Errorf("could not insert burn at block #%d: %s", burn.BlockNumber, err.Error())
				return err
			}
			// Commit transaction
			err = tx.Commit()
			if err != nil {
				db.log.Errorf("could not commit transaction: %s", err.Error())
				return err
			}
			return nil
		}

		// If error is not related to missing record
		db.log.Errorf("could not load NEC burn at block #%d; %s", burn.BlockNumber, err.Error())
		return err
	}

	// Handle TxList comparison
	var found int
	for _, in := range burn.TxList {
		for _, e := range ex.TxList {
			if in == e {
				found++
				break
			}
		}
	}

	// Check if all transactions are included
	if found == len(burn.TxList) {
		return nil
	}

	// Handle partial update rejection
	if found > 0 && found < len(burn.TxList) {
		db.log.Criticalf("invalid partial burn received at block #%d", burn.BlockNumber)
		return fmt.Errorf("partial burn update rejected at block #%d", burn.BlockNumber)
	}

	// Add the new value to the existing one
	newAmount := new(big.Int).Add((*big.Int)(&ex.Amount), (*big.Int)(&burn.Amount))
	ex.Amount = hexutil.Big(*newAmount) // Assign the result as hexutil.Big
	// Update TxList
	if burn.TxList != nil && len(burn.TxList) > 0 {
		if ex.TxList == nil {
			ex.TxList = make([]common.Hash, 0)
		}
		ex.TxList = append(ex.TxList, burn.TxList...)
	}

	// Update the existing burn record
	_, err = tx.Exec(updateQuery, ex.Amount, ex.TxList, ex.BlockNumber)
	if err != nil {
		db.log.Errorf("could not update burn at block #%d: %s", burn.BlockNumber, err.Error())
		return err
	}

	// Commit the transaction
	err = tx.Commit()
	if err != nil {
		db.log.Errorf("could not commit transaction: %s", err.Error())
		return err
	}

	return nil
}

// BurnCount estimates the number of burn records in the database.
func (db *MongoDbBridge) BurnCount() (uint64, error) {
	return db.EstimateCount(db.client.Database(db.dbName).Collection(colBurns))
}

// BurnCount estimates the number of burn records in the database.
func (db *PostgreSQLBridge) BurnCount() (int64, error) {
	// Define the SQL query to count rows in the 'burns' table
	query := "SELECT COUNT(*) FROM burns"

	// Execute the query and scan the result into a variable
	var count int64
	err := db.db.QueryRow(query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get burn count: %w", err)
	}

	// Return the count as uint64
	return int64(count), nil
}

// BurnTotal aggregates the total amount of burned fee across all blocks.
func (db *MongoDbBridge) BurnTotal() (int64, error) {
	col := db.client.Database(db.dbName).Collection(colBurns)

	// aggregate the total amount of burned native tokens
	cr, err := col.Aggregate(context.Background(), mongo.Pipeline{
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: nil},
			{Key: "amount", Value: bson.D{{Key: "$sum", Value: "$amount"}}},
		}}},
	})
	if err != nil {
		db.log.Errorf("can not collect total burned fee; %s", err.Error())
		return 0, err
	}

	defer db.closeCursor(cr)
	if !cr.Next(context.Background()) {
		return 0, fmt.Errorf("burned fee aggregation failed")
	}

	var row struct {
		Amount int64 `bson:"amount"`
	}
	if err := cr.Decode(&row); err != nil {
		db.log.Errorf("can not decode burned fee aggregation cursor; %s", err.Error())
		return 0, err
	}
	return row.Amount, nil
}

// BurnTotal aggregates the total amount of burned fee across all blocks.
func (db *PostgreSQLBridge) BurnTotal() (int64, error) {
	// Create SQL query to sum the total amount of burned tokens from the "burns" table
	query := `SELECT SUM(amount) FROM burns`

	// Execute the query
	var totalBurned int64
	err := db.db.QueryRow(query).Scan(&totalBurned)
	if err != nil {
		if err == sql.ErrNoRows {
			// If no rows are found, return 0 as the total
			return 0, nil
		}
		// Log any other errors that occur during the query
		db.log.Errorf("can not collect total burned fee; %s", err.Error())
		return 0, err
	}

	return totalBurned, nil
}

// BurnList provides list of native NEC burns per blocks stored in the persistent database.
func (db *MongoDbBridge) BurnList(count int64) ([]types.NecBurn, error) {
	col := db.client.Database(db.dbName).Collection(colBurns)

	cr, err := col.Find(context.Background(), bson.D{}, options.Find().SetSort(bson.D{{Key: "block", Value: -1}}).SetLimit(count))
	if err != nil {
		db.log.Errorf("failed to load burns; %s", err.Error())
		return nil, err
	}
	defer db.closeCursor(cr)

	ctx := context.Background()
	list := make([]types.NecBurn, 0, count)

	for cr.Next(ctx) {
		var row types.NecBurn
		if err := cr.Decode(&row); err != nil {
			db.log.Errorf("failed to decode burn; %s", err.Error())
			continue
		}
		list = append(list, row)
	}

	return list, nil
}

// BurnList provides a list of native NEC burns per blocks stored in the persistent PostgreSQL database.
func (db *PostgreSQLBridge) BurnList(count int64) ([]types.NecBurn, error) {
	// Query to get the burns ordered by block in descending order with a limit
	query := `
        SELECT block, amount, tx_list
        FROM burns
        ORDER BY block DESC
        LIMIT $1
    `

	rows, err := db.db.Query(query, count)
	if err != nil {
		db.log.Errorf("failed to load burns; %s", err.Error())
		return nil, err
	}
	defer rows.Close()

	list := make([]types.NecBurn, 0, count)

	// Iterate over the result set and map each row to the NecBurn struct
	for rows.Next() {
		var row types.NecBurn

		// Scan values from the row into the struct fields
		err := rows.Scan(&row.BlockNumber, &row.Amount, pq.Array(&row.TxList))
		if err != nil {
			db.log.Errorf("failed to decode burn; %s", err.Error())
			continue
		}

		// Append the row to the list
		list = append(list, row)
	}

	// Check for any error that occurred during iteration
	if err := rows.Err(); err != nil {
		db.log.Errorf("error during row iteration; %s", err.Error())
		return nil, err
	}

	return list, nil
}
