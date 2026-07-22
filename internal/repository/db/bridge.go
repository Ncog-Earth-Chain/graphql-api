// Package db implements bridge to persistent storage represented by Mongo database.
package db

import (
	"context"
	"fmt"
	"math/big"
	"ncogearthchain-api-graphql/internal/config"
	"ncogearthchain-api-graphql/internal/logger"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoDbBridge represents Mongo DB abstraction layer.
type MongoDbBridge struct {
	client *mongo.Client
	log    logger.Logger
	dbName string
}

// docListCountAggregationTimeout represents a max duration of DB query executed to calculate
// exact document count in filtered collection. If this duration is exceeded, the query fails
// ad we fall back to full collection documents count estimation.
const docListCountAggregationTimeout = 500 * time.Millisecond

// intZero represents an empty big value.
var intZero = new(big.Int)

// New creates a new Mongo Db connection bridge.
func New(cfg *config.Config, log logger.Logger) (*MongoDbBridge, error) {
	// log what we do
	log.Debugf("connecting database at %s/%s", cfg.Db.Url, cfg.Db.DbName)

	// open the database connection
	con, err := connectDb(&cfg.Db)
	if err != nil {
		log.Criticalf("can not contact the database; %s", err.Error())
		return nil, err
	}

	// log the event
	log.Notice("database connection established")

	// return the bridge
	db := &MongoDbBridge{
		client: con,
		log:    log,
		dbName: cfg.Db.DbName,
	}

	// make sure every collection's indexes exist before we serve anything
	db.EnsureIndexes()
	return db, nil
}

// connectDb opens Mongo database connection
func connectDb(cfg *config.Database) (*mongo.Client, error) {
	// get empty unrestricted context
	ctx := context.Background()

	// create new Mongo client
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(cfg.Url))
	if err != nil {
		return nil, err
	}

	// validate the connection was indeed established
	err = client.Ping(ctx, nil)
	if err != nil {
		return nil, err
	}

	return client, nil
}

// Close will terminate or finish all operations and close the connection to Mongo database.
func (db *MongoDbBridge) Close() {
	// do we have a client?
	if db.client != nil {
		// prep context
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

		// try to disconnect
		err := db.client.Disconnect(ctx)
		if err != nil {
			db.log.Errorf("error on closing database connection; %s", err.Error())
		}

		// inform
		db.log.Info("database connection is closed")
		cancel()
	}
}

// getAggregateValue extract single aggregate value for a given collection and aggregation pipeline.
func (db *MongoDbBridge) getAggregateValue(col *mongo.Collection, pipeline *bson.A) (uint64, error) {
	// work with context
	ctx := context.Background()

	// use aggregate pipeline to get the result set, should be just one row
	res, err := col.Aggregate(ctx, *pipeline)
	if err != nil {
		db.log.Errorf("can not get aggregate value; %s", err.Error())
		return 0, err
	}

	// don't forget to close the result cursor
	defer func() {
		// close the cursor
		err = res.Close(ctx)
		if err != nil {
			db.log.Errorf("closing aggregation cursor failed; %s", err.Error())
		}
	}()

	// get the value
	if !res.Next(ctx) {
		db.log.Error("aggregate document not found")
		return 0, err
	}

	// prep container; we are interested in just one value
	var row struct {
		Id    string `bson:"_id"`
		Value int64  `bson:"value"`
	}

	// try to decode the response
	err = res.Decode(&row)
	if err != nil {
		db.log.Errorf("can not parse aggregate value; %s", err.Error())
		return 0, err
	}

	// not a valid aggregate value
	if row.Value < 0 {
		db.log.Error("aggregate value not found")
		return 0, fmt.Errorf("item not found")
	}

	return uint64(row.Value), nil
}

// EnsureIndexes creates every collection's indexes at startup.
//
// This REPLACES the previous CheckDatabaseInitState/collectionNeedInit scheme, which
// only armed a sync.Once when a collection was EMPTY and therefore created no index at
// all on any database that already held data -- i.e. on every real deployment. Index
// models existed in the code and never existed in the database, so every list query ran
// a full collection scan.
//
// Running unconditionally is safe: Mongo's createIndexes is idempotent, so an index that
// already exists with the same specification is a no-op. It is cheap for the same reason,
// and it means an index added to the code later actually reaches an existing database.
//
// Failures are logged, not fatal. Index creation used to Panicf, which made the one
// moment indexes were ever created also a process-kill path.
func (db *MongoDbBridge) EnsureIndexes() {
	db.log.Debugf("ensuring database indexes")

	d := db.client.Database(db.dbName)

	db.initAccountsCollection(d.Collection(coAccounts))
	db.initTransactionsCollection(d.Collection(coTransactions))
	db.initContractsCollection(d.Collection(coContract))
	db.initUniswapCollection(d.Collection(coUniswap))
	db.initDelegationCollection(d.Collection(colDelegations))
	db.initWithdrawalsCollection(d.Collection(colWithdrawals))
	db.initRewardsCollection(d.Collection(colRewards))
	db.initErc20TrxCollection(d.Collection(colErcTransactions))
	db.initFMintTrxCollection(d.Collection(colFMintTransactions))
	db.initEpochsCollection(d.Collection(colEpochs))
	db.initGasPriceCollection(d.Collection(colGasPrice))
	db.initBurnsCollection(d.Collection(colBurns))

	db.log.Notice("database indexes ensured")
}

// CountFiltered calculates total number of documents in the given collection for the given filter.
func (db *MongoDbBridge) CountFiltered(col *mongo.Collection, filter *bson.D) (uint64, error) {
	// make sure some filter is used
	if nil == filter {
		filter = &bson.D{}
	}

	// do the counting
	val, err := col.CountDocuments(context.Background(), *filter)
	if err != nil {
		db.log.Errorf("can not count documents in rewards collection; %s", err.Error())
		return 0, err
	}
	return uint64(val), nil
}

// EstimateCount calculates an estimated number of documents in the given collection.
func (db *MongoDbBridge) EstimateCount(col *mongo.Collection) (uint64, error) {
	// do the counting
	val, err := col.EstimatedDocumentCount(context.Background())
	if err != nil {
		db.log.Errorf("can not count documents in rewards collection; %s", err.Error())
		return 0, err
	}
	return uint64(val), nil
}

// filteredCountCap bounds the fallback count. Counting with a limit lets Mongo stop as
// soon as the cap is reached, which is fast regardless of how many documents match, and
// yields an honest "at least N" instead of a fabricated total.
const filteredCountCap = 10000

// listDocumentsCount calculates the number of documents matching a filter.
//
// It returns (count, exact). When exact is false the count is a lower bound, not a total.
//
// The previous implementation fell back to EstimatedDocumentCount on the WHOLE
// collection, silently dropping the filter. On the account-transactions page -- whose
// filter is an $or over sender/recipient, which no single index can serve, so the 500 ms
// budget is genuinely reachable -- an account with three transactions would report the
// entire chain's transaction count as its own. A number that wrong is worse than no
// number, because nothing marks it as untrustworthy.
func (db *MongoDbBridge) listDocumentsCount(col *mongo.Collection, filter *bson.D) (int64, bool, error) {
	// try for the exact count first
	total, err := col.CountDocuments(context.Background(), filter,
		options.Count().SetMaxTime(docListCountAggregationTimeout))
	if err == nil {
		return total, true, nil
	}

	// it failed in the limited time we gave it
	db.log.Warningf("exact document count timed out, falling back to a capped count; %s", err.Error())

	// Count the SAME filter, but stop at the cap. This keeps the answer about the
	// documents the caller actually asked about.
	total, err = col.CountDocuments(context.Background(), filter,
		options.Count().SetLimit(filteredCountCap).SetMaxTime(docListCountAggregationTimeout))
	if err != nil {
		db.log.Errorf("can not count documents; %s", err.Error())
		return 0, false, err
	}

	// hitting the cap means there are at least this many; below it, the capped count
	// ran to completion and is therefore exact after all
	return total, total < filteredCountCap, nil
}

// closeCursor closes the given query cursor and reports possible issue if it fails.
func (db *MongoDbBridge) closeCursor(c *mongo.Cursor) {
	if err := c.Close(context.Background()); err != nil {
		db.log.Errorf("failed to close query cursor; %s", err.Error())
	}
}
