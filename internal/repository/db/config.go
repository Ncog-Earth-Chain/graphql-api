// Package db implements bridge to persistent storage represented by Mongo database.
package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	// coConfiguration is the name of the off-chain database collection storing configuration details.
	coConfiguration = "config"

	// fiConfigPk is the name of the primary key field of the configuration collection.
	fiConfigPk = "_id"

	// fiConfigValue is the name of the value field of the configuration collection.
	fiConfigValue = "val"

	// keyConfigLastKnownBlock is the primary key for the Last Known Block value.
	keyConfigLastKnownBlock = "lnb"
)

// ConfigRow represents a row in configuration collection.
type ConfigRow struct {
	Key   string `bson:"_id"`
	Value string `bson:"val"`
}

// UpdateLastKnownBlock stores the last known block into the config collection.
func (db *MongoDbBridge) UpdateLastKnownBlock(blockNo *hexutil.Uint64) error {
	// do we have all needed data?
	if blockNo == nil {
		return fmt.Errorf("can not add empty block")
	}

	// get the collection for cfg
	col := db.client.Database(db.dbName).Collection(coConfiguration)

	// insert/update
	_, err := col.UpdateByID(context.Background(), keyConfigLastKnownBlock, bson.D{{Key: "$set", Value: bson.D{
		{Key: fiConfigPk, Value: keyConfigLastKnownBlock},
		{Key: fiConfigValue, Value: blockNo.String()},
	}}}, new(options.UpdateOptions).SetUpsert(true))
	if err != nil {
		return err
	}
	return nil
}

// UpdateLastKnownBlock stores the last known block into the configuration table in PostgreSQL.
// UpdateLastKnownBlockPost updates the last known block in PostgreSQL.
func (db *PostgreSQLBridge) UpdateLastKnownBlock(blockNo *hexutil.Uint64) error {
	if blockNo == nil {
		return fmt.Errorf("cannot add empty block")
	}

	key := "lnb"

	// Adjusted query to work with unique constraint on the `key` column
	query := `
		INSERT INTO config (key, value)
		VALUES ($1, $2)
		ON CONFLICT (key) 
		DO UPDATE SET value = EXCLUDED.value;
	`

	_, err := db.db.Exec(query, key, blockNo.String())
	if err != nil {
		db.log.Errorf("Postgres UpdateLastKnownBlockPost failed: %s", err.Error())
		return fmt.Errorf("failed to update last known block; %w", err)
	}

	db.log.Infof("Updated last known block in PostgreSQL: %s", blockNo.String())
	return nil
}

// LastKnownBlock returns the last known block from the database.
func (db *MongoDbBridge) LastKnownBlock() (uint64, error) {
	// get the collection for cfg
	col := db.client.Database(db.dbName).Collection(coConfiguration)

	// get the last known block from the config collection
	res := col.FindOne(context.Background(), bson.D{{Key: fiConfigPk, Value: keyConfigLastKnownBlock}})
	if res.Err() == nil {
		// get the data
		var row ConfigRow
		err := res.Decode(&row)
		if err != nil {
			db.log.Error("can not decode the config collection row")
			return 0, err
		}
		return hexutil.DecodeUint64(row.Value)
	}

	// any unknown error?
	if res.Err() != mongo.ErrNoDocuments {
		db.log.Errorf("config collection record not found; %s", res.Err().Error())
		return 0, res.Err()
	}

	// load the slow way
	return db.lastKnownBlock()
}

// LastKnownBlock returns the last known block from the PostgreSQL database.
func (db *PostgreSQLBridge) LastKnownBlock() (uint64, error) {
	// Prepare SQL query to fetch the last known block from the configuration table
	query := `
		SELECT value 
		FROM config 
		WHERE key = $1;
	`

	// Execute the query
	var value string
	err := db.db.QueryRow(query, "last_known_block").Scan(&value)
	if err == nil {
		// Successfully found the value, decode it into uint64
		blockNumber, err := hexutil.DecodeUint64(value)
		if err != nil {
			db.log.Error("cannot decode the last known block value")
			return 0, err
		}
		return blockNumber, nil
	}

	// Handle any errors
	if err != sql.ErrNoRows {
		db.log.Errorf("config record not found; %s", err.Error())
		return 0, err
	}

	// If the key does not exist, load the slow way (you might have a fallback logic here)
	return db.lastKnownBlock()
}

// lastKnownBlock returns number of the last known block stored in the transactions table.
func (db *MongoDbBridge) lastKnownBlock() (uint64, error) {
	// prep search options
	opt := options.FindOne()
	opt.SetSort(bson.D{{Key: fiTransactionBlock, Value: -1}})
	opt.SetProjection(bson.D{{Key: fiTransactionBlock, Value: true}})

	// get the collection for account transactions
	col := db.client.Database(db.dbName).Collection(coTransactions)
	res := col.FindOne(context.Background(), bson.D{}, opt)
	if res.Err() != nil {
		// may be no block at all
		if res.Err() == mongo.ErrNoDocuments {
			db.log.Info("no blocks found in database")
			return 1, nil
		}

		// log issue
		db.log.Error("can not get the top block")
		return 0, res.Err()
	}

	// get the actual value
	var tx struct {
		Block uint64 `bson:"blk"`
	}

	// get the data
	err := res.Decode(&tx)
	if err != nil {
		db.log.Error("can not decode the top block")
		return 0, res.Err()
	}
	return tx.Block, nil
}

// lastKnownBlock returns number of the last known block stored in the transactions table.
func (db *PostgreSQLBridge) lastKnownBlock() (uint64, error) {
	// Prepare the SQL query to retrieve the last known block (maximum block number)
	query := `
		SELECT block_number 
		FROM transactions 
		ORDER BY block_number DESC 
		LIMIT 1;
	`

	// Execute the query
	var block uint64
	err := db.db.QueryRow(query).Scan(&block)
	if err != nil {
		// Handle the case where no rows are found
		if err == sql.ErrNoRows {
			db.log.Info("no blocks found in database")
			return 1, nil
		}

		// Log the error and return
		db.log.Error("can not get the top block")
		return 0, err
	}

	// Return the found block number
	return block, nil
}
