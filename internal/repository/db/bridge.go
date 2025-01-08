// Package db implements bridge to persistent storage represented by Mongo database.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"ncogearthchain-api-graphql/internal/config"
	"ncogearthchain-api-graphql/internal/logger"
	"strings"
	"sync"
	"time"

	_ "github.com/lib/pq"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoDbBridge represents Mongo DB abstraction layer.
type MongoDbBridge struct {
	client *mongo.Client
	log    logger.Logger
	dbName string

	// init state marks
	initAccounts     *sync.Once
	initTransactions *sync.Once
	initContracts    *sync.Once
	initSwaps        *sync.Once
	initDelegations  *sync.Once
	initWithdrawals  *sync.Once
	initRewards      *sync.Once
	initErc20Trx     *sync.Once
	initFMintTrx     *sync.Once
	initEpochs       *sync.Once
	initGasPrice     *sync.Once
	initBurns        *sync.Once
}

// Struct use for PostgreSQL.
type PostgreSQLBridge struct {
	db     *sql.DB
	log    logger.Logger
	dbName string

	// init state marks
	initAccounts     *sync.Once
	initTransactions *sync.Once
	initContracts    *sync.Once
	initSwaps        *sync.Once
	initDelegations  *sync.Once
	initWithdrawals  *sync.Once
	initRewards      *sync.Once
	initErc20Trx     *sync.Once
	initFMintTrx     *sync.Once
	initEpochs       *sync.Once
	initGasPrice     *sync.Once
	initBurns        *sync.Once
}

// docListCountAggregationTimeout represents a max duration of DB query executed to calculate
// exact document count in filtered collection. If this duration is exceeded, the query fails
// ad we fall back to full collection documents count estimation.
const docListCountAggregationTimeout = 500 * time.Millisecond

// intZero represents an empty big value.
var intZero = new(big.Int)

// func initializeDB() (*pgxpool.Pool, error) {
// 	connString := "postgres://postgres:King%23123@localhost:5432/ncog_backend"

// 	// Create a new connection pool
// 	config, err := pgxpool.ParseConfig(connString)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to parse PostgreSQL config: %v", err)
// 	}

// 	// Optional: Configure pool settings (e.g., max connections, timeouts)
// 	config.MaxConns = 10
// 	config.MinConns = 1
// 	config.MaxConnLifetime = time.Hour

// 	// Establish the connection pool
// 	dbPool, err := pgxpool.NewWithConfig(context.Background(), config)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to create PostgreSQL connection pool: %v", err)
// 	}

// 	// Test the connection
// 	if err := dbPool.Ping(context.Background()); err != nil {
// 		return nil, fmt.Errorf("failed to ping PostgreSQL database: %v", err)
// 	}

// 	log.Println("PostgreSQL database connection established")
// 	return dbPool, nil
// }

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

	// check the state
	db.CheckDatabaseInitState()
	return db, nil
}

func InitializePostgreSQLBridge(cfg *config.Config, log logger.Logger) (*PostgreSQLBridge, error) {
	// Use default DSN if not provided

	dsn := "postgres://postgres:King%23123@localhost:5432/ncgobackend"
	// Open a connection to the database
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open a DB connection: %v", err)
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to the database: %v", err)
	}
	log.Notice("PgSql database connection established")
	return &PostgreSQLBridge{
		db:     db,
		log:    log,
		dbName: "ncgobackend",
	}, nil

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

// getAggregateValue extracts a single aggregate value for a given SQL query in PostgreSQL.
func (db *PostgreSQLBridge) getAggregateValue(query string, args ...interface{}) (uint64, error) {
	// Prepare a container for the result
	var value int64

	// Execute the query with the provided arguments
	err := db.db.QueryRow(query, args...).Scan(&value)
	if err != nil {
		db.log.Errorf("can not get aggregate value; %s", err.Error())
		return 0, err
	}

	// Check if the value is valid
	if value < 0 {
		db.log.Error("aggregate value not found or invalid")
		return 0, fmt.Errorf("item not found")
	}

	// Return the result as an unsigned integer
	return uint64(value), nil
}

// CheckDatabaseInitState verifies if database collections have been
// already initialized and marks the empty collections so they can be properly
// configured when created.
func (db *MongoDbBridge) CheckDatabaseInitState() {
	// log what we do
	db.log.Debugf("checking database init state")

	db.collectionNeedInit("accounts", db.AccountCount, &db.initAccounts)
	db.collectionNeedInit("transactions", db.TransactionsCount, &db.initTransactions)
	db.collectionNeedInit("contracts", db.ContractCount, &db.initContracts)
	db.collectionNeedInit("swaps", db.SwapCount, &db.initSwaps)
	db.collectionNeedInit("delegations", db.DelegationsCount, &db.initDelegations)
	db.collectionNeedInit("withdrawals", db.WithdrawalsCount, &db.initWithdrawals)
	db.collectionNeedInit("rewards", db.RewardsCount, &db.initRewards)
	db.collectionNeedInit("erc20 transactions", db.ErcTransactionCount, &db.initErc20Trx)
	db.collectionNeedInit("fmint transactions", db.FMintTransactionCount, &db.initFMintTrx)
	db.collectionNeedInit("epochs", db.EpochsCount, &db.initEpochs)
	db.collectionNeedInit("gas price periods", db.GasPricePeriodCount, &db.initGasPrice)
	db.collectionNeedInit("burned fees", db.BurnCount, &db.initBurns)
}

// CheckDatabaseInitState verifies if database tables have been
// already initialized and marks the empty tables so they can be properly
// configured when created.
func (db *PostgreSQLBridge) CheckDatabaseInitState() {
	// log what we do
	db.log.Debugf("checking database init state")

	db.tableNeedInit("accounts", db.AccountCount, &db.initAccounts)
	db.tableNeedInit("transactions", db.TransactionsCount, &db.initTransactions)
	db.tableNeedInit("contracts", db.ContractCount, &db.initContracts)
	db.tableNeedInit("swaps", db.SwapCount, &db.initSwaps)
	db.tableNeedInit("delegations", db.DelegationsCount, &db.initDelegations)
	db.tableNeedInit("withdrawals", db.WithdrawalsCount, &db.initWithdrawals)
	db.tableNeedInit("rewards", db.RewardsCount, &db.initRewards)
	db.tableNeedInit("erc20_transactions", db.ErcTransactionCount, &db.initErc20Trx)
	db.tableNeedInit("fmint_transactions", db.FMintTransactionCount, &db.initFMintTrx)
	db.tableNeedInit("epochs", db.EpochsCount, &db.initEpochs)
	db.tableNeedInit("gas_price_periods", db.GasPricePeriodCount, &db.initGasPrice)
	db.tableNeedInit("burned_fees", db.BurnCount, &db.initBurns)
}

// checkAccountCollectionState checks the Accounts' collection state.
func (db *MongoDbBridge) collectionNeedInit(name string, counter func() (uint64, error), init **sync.Once) {
	// use the counter to get the collection size
	count, err := counter()
	if err != nil {
		db.log.Errorf("can not check %s count; %s", name, err.Error())
		return
	}

	// collection not empty,
	if 0 != count {
		db.log.Debugf("found %d %s", count, name)
		return
	}

	// collection init needed, create the init control
	db.log.Noticef("%s collection empty", name)
	var once sync.Once
	*init = &once
}

func (db *PostgreSQLBridge) tableNeedInit(name string, counter func() (int64, error), init **sync.Once) {
	// Use the counter function to get the table's row count
	count, err := counter()
	if err != nil {
		db.log.Errorf("cannot check %s count; %s", name, err.Error())
		return
	}

	// If the table is not empty
	if count != 0 {
		db.log.Debugf("found %d rows in %s", count, name)
		return
	}

	// Table is empty, mark it for initialization
	db.log.Noticef("%s table is empty", name)
	var once sync.Once
	*init = &once
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


// CountFiltered calculates the total number of records in the given table for the given filter.
func (db *PostgreSQLBridge) CountFiltered(tableName string, filter map[string]interface{}) (uint64, error) {
	// Build the WHERE clause for filtering
	whereClauses := []string{}
	args := []interface{}{}

	// If no filter is provided, return count of all rows
	if filter == nil || len(filter) == 0 {
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)
		var count int64
		err := db.db.QueryRow(query).Scan(&count)
		if err != nil {
			return 0, fmt.Errorf("failed to count rows in table %s: %w", tableName, err)
		}
		return uint64(count), nil
	}

	// Construct WHERE clause from the filter map
	for key, value := range filter {
		whereClauses = append(whereClauses, fmt.Sprintf("%s = $%d", key, len(whereClauses)+1))
		args = append(args, value)
	}

	// Create the query with the WHERE clause
	whereClause := fmt.Sprintf("WHERE %s", strings.Join(whereClauses, " AND "))
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s %s", tableName, whereClause)

	// Execute the query with the provided filter
	var count int64
	err := db.db.QueryRow(query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count filtered rows in table %s: %w", tableName, err)
	}

	// Return the count as uint64
	return uint64(count), nil
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

// EstimateCount calculates an estimated number of records in the given table.
func (db *PostgreSQLBridge) EstimateCount(tableName string) (uint64, error) {
	// Query to estimate the number of rows in the given table
	query := fmt.Sprintf("SELECT reltuples::bigint FROM pg_class WHERE relname = '%s'", tableName)

	// Execute the query and get the estimated count
	var estimatedCount int64
	err := db.db.QueryRow(query).Scan(&estimatedCount)
	if err != nil {
		db.log.Errorf("could not estimate count for table %s; %s", tableName, err.Error())
		return 0, err
	}

	// Return the result as uint64
	return uint64(estimatedCount), nil
}

// listDocumentsCount tries to calculate precise documents count and if it's not counted in limited
// time, use general estimation to speed up the loader.
func (db *MongoDbBridge) listDocumentsCount(col *mongo.Collection, filter *bson.D) (int64, error) {
	// try to count the proper way
	total, err := col.CountDocuments(context.Background(), filter, options.Count().SetMaxTime(docListCountAggregationTimeout))
	if err == nil {
		return total, nil
	}

	// it failed in the limited time we gave it
	db.log.Errorf("can not count documents properly; %s", err.Error())

	// just estimate the whole collection size
	total, err = col.EstimatedDocumentCount(context.Background())
	if err != nil {
		db.log.Errorf("can not count documents")
		return 0, err
	}
	return total, nil
}

// listDocumentsCount tries to calculate precise records count and if it's not counted in limited
// time, use general estimation to speed up the loader.
func (db *PostgreSQLBridge) listDocumentsCount(tableName string, filter map[string]interface{}) (int64, error) {
	// Construct the WHERE clause for filtering
	whereClauses := []string{}
	args := []interface{}{}

	// If a filter is provided, build the WHERE clause
	if filter != nil && len(filter) > 0 {
		for key, value := range filter {
			whereClauses = append(whereClauses, fmt.Sprintf("%s = $%d", key, len(whereClauses)+1))
			args = append(args, value)
		}
	}

	// Create the query with the WHERE clause if any filter is provided
	whereClause := ""
	if len(whereClauses) > 0 {
		whereClause = fmt.Sprintf("WHERE %s", strings.Join(whereClauses, " AND "))
	}
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s %s", tableName, whereClause)

	// Try to count with the filter
	var count int64
	start := time.Now()

	err := db.db.QueryRow(query, args...).Scan(&count)
	if err == nil && time.Since(start) < docListCountAggregationTimeout {
		// Successfully counted within time limit
		return count, nil
	}

	// If counting with filter failed or took too long, use an estimate
	if err != nil {
		db.log.Errorf("could not count documents with filter; %s", err.Error())
	}

	// Use an estimated count for the whole table
	estimateQuery := fmt.Sprintf("SELECT reltuples::bigint FROM pg_class WHERE relname = '%s'", tableName)
	var estimatedCount int64
	err = db.db.QueryRow(estimateQuery).Scan(&estimatedCount)
	if err != nil {
		db.log.Errorf("could not estimate document count; %s", err.Error())
		return 0, err
	}

	return estimatedCount, nil
}

// closeCursor closes the given query cursor and reports possible issue if it fails.
func (db *MongoDbBridge) closeCursor(c *mongo.Cursor) {
	if err := c.Close(context.Background()); err != nil {
		db.log.Errorf("failed to close query cursor; %s", err.Error())
	}
}
