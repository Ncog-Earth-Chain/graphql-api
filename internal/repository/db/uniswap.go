// Package db implements bridge to persistent storage represented by Mongo database.
package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"math/big"
	"ncogearthchain-api-graphql/internal/types"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (

	// coUniswap is the name of the off-chain database collection storing Uniswap swap details.
	coUniswap = "uniswap"

	// fiSwapPk is the name of the primary key field of the swap collection.
	fiSwapPk         = "_id"
	fiSwapOrdIndex   = "orx"
	fiSwapType       = "type"
	fiSwapBlock      = "blk"
	fiSwapTxHash     = "tx"
	fiSwapPair       = "pair"
	fiSwapDate       = "date"
	fiSwapSender     = "sender"
	fiSwapAmount0in  = "am0in"
	fiSwapAmount0out = "am0out"
	fiSwapAmount1in  = "am1in"
	fiSwapAmount1out = "am1out"
	fiSwapReserve0   = "reserve0"
	fiSwapReserve1   = "reserve1"
)

// swapAmountDecimalsCorrection represents the decimal correction on swap value.
var swapAmountDecimalsCorrection = new(big.Int).SetUint64(1000000000)

// swapReserveDecimalsCorrection represents the decimal correction on swap reserve amount.
var swapReserveDecimalsCorrection = new(big.Int).SetUint64(1000000000000)

// getHash generates hash for swap from transaction hash and pair address
func getHash(swap *types.Swap) *common.Hash {
	hashBytes := swap.Hash.Big().Bytes()
	pairBytes := swap.Pair.Bytes()
	sum := sha256.Sum256(append(hashBytes, pairBytes...))
	swapHash := common.BytesToHash(sum[:])
	return &swapHash
}

// removeDecimals applies decimal correction to the given value.
func removeDecimals(am *big.Int, cr *big.Int) uint64 {
	return new(big.Int).Div(am, cr).Uint64()
}

// removes decimal correction from the given value.
func returnDecimals(am *big.Int, cr *big.Int) *big.Int {
	return new(big.Int).Mul(am, cr)
}

// initUniswapCollection initializes the swap collection with
// indexes and additional parameters needed by the app.
func (db *MongoDbBridge) initUniswapCollection(col *mongo.Collection) {
	// prepare index models
	ix := make([]mongo.IndexModel, 0)

	// index for primary key
	ix = append(ix, mongo.IndexModel{
		Keys: bson.D{{Key: fiSwapPk, Value: 1}},
	})

	// index date, sender, blk
	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: fiSwapDate, Value: 1}}})
	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: fiSwapSender, Value: 1}}})
	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: fiSwapOrdIndex, Value: -1}}})

	// create indexes
	if _, err := col.Indexes().CreateMany(context.Background(), ix); err != nil {
		db.log.Panicf("can not create indexes for swap collection; %s", err.Error())
	}

	// log we're done that
	db.log.Debugf("swap collection initialized")
}

func (db *PostgreSQLBridge) initUniswapCollection() error {
	// Log the initialization process
	db.log.Debugf("initializing uniswap collection with indexes")

	// Define the SQL statements to create indexes
	indexQueries := []string{
		// Index for primary key (assume primary key is already created on table definition)
		`CREATE UNIQUE INDEX IF NOT EXISTS uniswap_pk_idx ON uniswap_table (primary_key_column);`,

		// Index on the date column
		`CREATE INDEX IF NOT EXISTS uniswap_date_idx ON uniswap_table (swap_date_column);`,

		// Index on the sender column
		`CREATE INDEX IF NOT EXISTS uniswap_sender_idx ON uniswap_table (swap_sender_column);`,

		// Index on the ordinal index column in descending order
		`CREATE INDEX IF NOT EXISTS uniswap_ord_index_idx ON uniswap_table (swap_ord_index_column DESC);`,
	}

	// Execute each index creation query
	for _, query := range indexQueries {
		if _, err := db.db.ExecContext(context.Background(), query); err != nil {
			db.log.Panicf("can not create indexes for uniswap collection; %s", err.Error())
			return err
		}
	}

	// Log success
	db.log.Debugf("uniswap collection initialized with indexes")
	return nil
}

// shouldAddSwap validates if the swap should be added to the persistent storage.
func (db *MongoDbBridge) shouldAddSwap(col *mongo.Collection, swap *types.Swap) bool {
	// check if swap already exists
	swapHash := getHash(swap)
	exists, err := db.IsSwapKnown(col, swapHash, swap)
	if err != nil {
		db.log.Critical(err)
		return false
	}

	// if the transaction already exists, we don't need to do anything here
	return !exists
}

func (db *PostgreSQLBridge) shouldAddSwap(tableName string, swap *types.Swap) (bool, error) {
	// Calculate the swap hash
	swapHash := getHash(swap)
	// Convert swapHash to string
	swapHashStr := swapHash.String()

	// Check if the swap already exists using IsSwapKnown
	exists, err := db.IsSwapKnown(tableName, swapHashStr)
	if err != nil {
		db.log.Errorf("error checking if swap is known; %s", err.Error())
		return false, err
	}

	// If the transaction already exists, we don't need to do anything here
	return !exists, nil
}

// isZeroSwap checks if amounts are not zero to avoid divide by 0 during calculations in db
func isZeroSwap(swap *types.Swap) bool {
	if swap.Type == types.SwapSync {
		return false
	}
	am0small := removeDecimals(new(big.Int).Add(swap.Amount0In, swap.Amount0Out), swapAmountDecimalsCorrection)
	am1small := removeDecimals(new(big.Int).Add(swap.Amount1In, swap.Amount1Out), swapAmountDecimalsCorrection)
	return am0small == 0 || am1small == 0
}

func isZeroSwapPostgres(swap *types.Swap) bool {
	// Check if the swap is of type "Sync"
	if swap.Type == types.SwapSync {
		return false
	}

	// Calculate the adjusted amounts
	am0small := removeDecimals(new(big.Int).Add(swap.Amount0In, swap.Amount0Out), swapAmountDecimalsCorrection)
	am1small := removeDecimals(new(big.Int).Add(swap.Amount1In, swap.Amount1Out), swapAmountDecimalsCorrection)

	// Check if either amount is zero
	return am0small == 0 || am1small == 0
}

// UniswapAdd stores a swap reference in connected persistent storage.
func (db *MongoDbBridge) UniswapAdd(swap *types.Swap) error {
	// do we have all needed data?
	if swap == nil {
		return fmt.Errorf("can not add empty swap")
	}

	// get the collection for transactions
	col := db.client.Database(db.dbName).Collection(coUniswap)

	// check for zero amounts in the swap, because of future div by 0 during aggregation in db
	if isZeroSwap(swap) {
		db.log.Debugf("swap from block %d will not be added, because swap amount is 0 after removing decimals", uint64(*swap.BlockNumber))
		return nil
	}

	// if the swap already exists, we don't need to add it
	// just make sure the transaction accounts were processed
	if !db.shouldAddSwap(col, swap) {
		return nil
	}

	// calculate swap hash to use it as a pk
	swapHash := getHash(swap)

	// try to do the insert
	if _, err := col.InsertOne(context.Background(),
		swapData(&bson.D{
			{Key: fiSwapPk, Value: swapHash.String()},
			{Key: fiSwapBlock, Value: uint64(*swap.BlockNumber)},
			{Key: fiSwapOrdIndex, Value: swap.OrdIndex},
			{Key: fiSwapDate, Value: primitive.NewDateTimeFromTime(time.Unix((int64)(*swap.TimeStamp), 0).UTC())},
		}, swap)); err != nil {

		db.log.Critical(err)
		return err
	}

	// add transaction to the db
	db.log.Debugf("swap %s added to database", swapHash.String())

	// make sure uniswap collection is initialized
	if db.initSwaps != nil {
		db.initSwaps.Do(func() { db.initUniswapCollection(col); db.initSwaps = nil })
	}
	return nil
}

// UniswapAdd stores a swap reference in connected persistent storage for PostgreSQL.
func (db *PostgreSQLBridge) UniswapAdd(swap *types.Swap) error {
	// Do we have all needed data?
	if swap == nil {
		return fmt.Errorf("can not add empty swap")
	}

	// Check for zero amounts in the swap, because of future division by 0 during aggregation in the database
	if isZeroSwapPostgres(swap) {
		db.log.Debugf("swap from block %d will not be added, because swap amount is 0 after removing decimals", uint64(*swap.BlockNumber))
		return nil
	}

	// If the swap already exists, we don't need to add it
	// Just make sure the transaction accounts were processed
	shouldAdd, err := db.shouldAddSwap("swaps", swap)
	if err != nil {
		db.log.Errorf("error checking if swap should be added; %s", err.Error())
		return err
	}

	if !shouldAdd {
		return nil
	}

	// Calculate swap hash to use it as a pk
	swapHash := getHash(swap)

	// Prepare the SQL query and data for insertion
	query, values, err := swapDataPostgres(swap)
	if err != nil {
		db.log.Errorf("error preparing data for swap insert; %s", err.Error())
		return err
	}

	// Try to do the insert
	_, err = db.db.ExecContext(context.Background(), query, values...)
	if err != nil {
		db.log.Criticalf("error inserting swap into database; %s", err.Error())
		return err
	}

	// Log the successful insertion
	db.log.Debugf("swap %s added to database", swapHash.String())

	// Ensure the swap table is initialized (optional step, depending on how your application is designed)
	// Make sure the collection (table) is set up properly.
	if db.initSwaps != nil {
		db.initSwaps.Do(func() {
			// Call initialization for the swap table, if needed.
			db.initUniswapCollection()
			db.initSwaps = nil
		})
	}

	return nil
}

// swapData collects the data for the given swap.
func swapData(base *bson.D, swap *types.Swap) bson.D {
	// make a new instance if needed
	if base == nil {
		base = &bson.D{}
	}

	// add the extended data
	*base = append(*base,
		bson.E{Key: fiSwapType, Value: swap.Type},
		bson.E{Key: fiSwapTxHash, Value: swap.Hash.String()},
		bson.E{Key: fiSwapPair, Value: swap.Pair.String()},
		bson.E{Key: fiSwapSender, Value: swap.Sender.String()},
		bson.E{Key: fiSwapAmount0in, Value: removeDecimals(swap.Amount0In, swapAmountDecimalsCorrection)},
		bson.E{Key: fiSwapAmount0out, Value: removeDecimals(swap.Amount0Out, swapAmountDecimalsCorrection)},
		bson.E{Key: fiSwapAmount1in, Value: removeDecimals(swap.Amount1In, swapAmountDecimalsCorrection)},
		bson.E{Key: fiSwapAmount1out, Value: removeDecimals(swap.Amount1Out, swapAmountDecimalsCorrection)},
		bson.E{Key: fiSwapReserve0, Value: removeDecimals(swap.Reserve0, swapReserveDecimalsCorrection)},
		bson.E{Key: fiSwapReserve1, Value: removeDecimals(swap.Reserve1, swapReserveDecimalsCorrection)},
	)
	return *base
}

// swapData collects the data for the given swap and prepares it for PostgreSQL insertion.
func swapDataPostgres(swap *types.Swap) (string, []interface{}, error) {
	// Prepare the base SQL query
	query := `
		INSERT INTO swaps (
			swap_type, 
			swap_tx_hash, 
			swap_pair, 
			swap_sender, 
			swap_amount0in, 
			swap_amount0out, 
			swap_amount1in, 
			swap_amount1out, 
			swap_reserve0, 
			swap_reserve1
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10
		)
	`

	// Prepare the values to insert
	values := []interface{}{
		swap.Type,            // fiSwapType
		swap.Hash.String(),   // fiSwapTxHash
		swap.Pair.String(),   // fiSwapPair
		swap.Sender.String(), // fiSwapSender
		removeDecimals(swap.Amount0In, swapAmountDecimalsCorrection),  // fiSwapAmount0in
		removeDecimals(swap.Amount0Out, swapAmountDecimalsCorrection), // fiSwapAmount0out
		removeDecimals(swap.Amount1In, swapAmountDecimalsCorrection),  // fiSwapAmount1in
		removeDecimals(swap.Amount1Out, swapAmountDecimalsCorrection), // fiSwapAmount1out
		removeDecimals(swap.Reserve0, swapReserveDecimalsCorrection),  // fiSwapReserve0
		removeDecimals(swap.Reserve1, swapReserveDecimalsCorrection),  // fiSwapReserve1
	}

	// Return the query and the corresponding values to be used in an insert
	return query, values, nil
}

// IsSwapKnown checks if swap document already exists in the database.
func (db *MongoDbBridge) IsSwapKnown(col *mongo.Collection, hash *common.Hash, swap *types.Swap) (bool, error) {
	// try to find swap in the database (it may already exist)
	sr := col.FindOne(context.Background(), bson.D{
		{Key: fiSwapPk, Value: hash.String()}})

	// error on lookup?
	if sr.Err() != nil {
		// may be ErrNoDocuments, which we seek
		if sr.Err() == mongo.ErrNoDocuments {
			// add swap to the db
			db.log.Debugf("swap %s not found in database", hash.String())
			return false, nil
		}

		// log the error of the lookup
		db.log.Error("can not get existing swap pk")
		return false, sr.Err()
	}

	// swap is known, jus log and return true
	db.log.Debugf("Swap %s is already in database.", hash.String())

	// if swap is sync type, then update reserves
	if swap.Type == types.SwapSync {
		db.log.Debugf("Updating reserves for Swap %s", hash.String())
		_, err := col.UpdateOne(context.Background(),
			bson.M{fiSwapPk: hash.String()},
			bson.D{
				{Key: "$set", Value: bson.M{fiSwapReserve0: removeDecimals(swap.Reserve0, swapReserveDecimalsCorrection)}},
				{Key: "$set", Value: bson.M{fiSwapReserve1: removeDecimals(swap.Reserve1, swapReserveDecimalsCorrection)}}})
		if err != nil {
			db.log.Errorf("unable to update reserves for swap %s", hash.String())
		}
	} else {
		// in case the sync event was recorded first, update reserves into actual swap
		// and delete sync record.
		type Values struct {
			Type     int   `bson:"type"`
			Reserve0 int64 `bson:"reserve0"`
			Reserve1 int64 `bson:"reserve1"`
		}
		var values Values
		if err := sr.Decode(&values); err != nil {
			db.log.Criticalf("can not decode swap; %s", err.Error())
			return false, err
		}

		if types.SwapSync == values.Type {
			// log issue
			db.log.Debugf("updating reserve for swap: %s, reserve0: %v, reserve1: %v", hash.String(), values.Reserve0, values.Reserve1)
			if _, err := col.DeleteOne(context.Background(), bson.D{{Key: fiSwapPk, Value: hash.String()}}); err != nil {
				db.log.Errorf("can not delete swap data; %s", err.Error())
			}

			swap.Reserve0 = returnDecimals(big.NewInt(values.Reserve0), swapReserveDecimalsCorrection)
			swap.Reserve1 = returnDecimals(big.NewInt(values.Reserve1), swapReserveDecimalsCorrection)
			return false, nil
		}
	}
	return true, nil
}

func (db *PostgreSQLBridge) IsSwapKnown(tableName string, swapHash string) (bool, error) {
	// Example query to check if the swap already exists in the database
	query := `SELECT 1 FROM ` + tableName + ` WHERE swap_pk = $1`
	var exists int
	err := db.db.QueryRowContext(context.Background(), query, swapHash).Scan(&exists)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// SwapCount returns the number of swaps stored in the database.
func (db *MongoDbBridge) SwapCount() (uint64, error) {
	return db.EstimateCount(db.client.Database(db.dbName).Collection(coUniswap))
}

func (db *PostgreSQLBridge) SwapCount() (int64, error) {
	// Define the query to count the rows in the 'uniswap' table
	query := "SELECT COUNT(*) FROM uniswap"

	// Execute the query and scan the result into a variable
	var count int64
	err := db.db.QueryRow(query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get swap count: %w", err)
	}

	// Return the count as uint64
	return int64(count), nil
}

// LastKnownSwapBlock returns number of the last known block stored in the database.
func (db *MongoDbBridge) LastKnownSwapBlock() (uint64, error) {
	// search for document with last swap block number
	query := bson.D{
		{Key: "lastSwapSyncBlk", Value: bson.D{
			{Key: "$exists", Value: "true"}}},
	}

	// get the swaps collection
	col := db.client.Database(db.dbName).Collection(coUniswap)
	res := col.FindOne(context.Background(), query)
	if res.Err() != nil {
		// may be no block at all
		if res.Err() == mongo.ErrNoDocuments {
			db.log.Info("No document with last swap block number in database starting from 0.")
			return 0, nil
		}

		// log issue
		db.log.Error("Can not get the last correct swap block number, starting from 0.")
		return 0, res.Err()
	}

	// get the actual value
	var swap struct {
		Block uint64 `bson:"lastSwapSyncBlk"`
	}

	// get the data
	err := res.Decode(&swap)
	if err != nil {
		db.log.Error("Can not resolve id of the last correct swap block in db. Starting from 0.")
		return 0, res.Err()
	}

	return swap.Block, nil
}

// LastKnownSwapBlock returns the number of the last known block stored in the database.
func (db *PostgreSQLBridge) LastKnownSwapBlock() (uint64, error) {
	// Query to find the last known swap block number
	query := `SELECT last_swap_sync_blk FROM swaps WHERE last_swap_sync_blk IS NOT NULL ORDER BY last_swap_sync_blk DESC LIMIT 1`

	// Execute the query
	var block uint64
	err := db.db.QueryRowContext(context.Background(), query).Scan(&block)

	// Check if we got an error
	if err != nil {
		// If no rows are returned, this means there's no block, so we return 0
		if err == sql.ErrNoRows {
			db.log.Info("No document with last swap block number in the database, starting from 0.")
			return 0, nil
		}

		// Log any other errors
		db.log.Error("Can not get the last correct swap block number, starting from 0.")
		return 0, err
	}

	return block, nil
}

// UniswapUpdateLastKnownSwapBlock stores a last correctly saved swap block number into persistent storage.
func (db *MongoDbBridge) UniswapUpdateLastKnownSwapBlock(blkNumber uint64) error {
	// is valid block number
	if blkNumber == 0 {
		return fmt.Errorf("no need to store zero value, will start from 0 next time")
	}

	// document for update with last swap block number
	query := bson.D{
		{Key: "lastSwapSyncBlk", Value: bson.D{
			{Key: "$exists", Value: "true"}}},
	}

	data := bson.D{
		{Key: "$set", Value: bson.D{
			{Key: "lastSwapSyncBlk", Value: blkNumber}}},
	}

	// get the collection for transactions and insert data
	col := db.client.Database(db.dbName).Collection(coUniswap)
	if _, err := col.UpdateOne(context.Background(),
		query, data, options.Update().SetUpsert(true)); err != nil {

		db.log.Critical(err)
		return err
	}

	// log
	db.log.Debugf("Block %d was set as a last correct uniswap block into database", blkNumber)
	return nil
}

// UniswapUpdateLastKnownSwapBlock stores the last correctly saved swap block number into persistent storage.
func (db *PostgreSQLBridge) UniswapUpdateLastKnownSwapBlock(blkNumber uint64) error {
	// Validate block number
	if blkNumber == 0 {
		return fmt.Errorf("no need to store zero value, will start from 0 next time")
	}

	// SQL query for upserting the block number
	query := `
		INSERT INTO swaps (last_swap_sync_blk)
		VALUES ($1)
		ON CONFLICT (id) 
		DO UPDATE SET last_swap_sync_blk = EXCLUDED.last_swap_sync_blk
	`

	// Execute the query
	_, err := db.db.ExecContext(context.Background(), query, blkNumber)
	if err != nil {
		db.log.Criticalf("Failed to update last swap block in the database: %s", err.Error())
		return err
	}

	// Log success
	db.log.Debugf("Block %d was set as the last correct uniswap block in the database", blkNumber)
	return nil
}

// Volume represents one single sum of volumes for specified pair
type Volume struct {
	ID    string `bson:"_id"`
	Total int64  `bson:"total"`
}

// UniswapVolume resolves volume of swap trades for specified pair and date interval.
// If toTime is 0, then it calculates volumes till now
func (db *MongoDbBridge) UniswapVolume(pairAddress *common.Address, fromTime int64, toTime int64) (types.DefiSwapVolume, error) {

	// translate unix time into mongo primitive date
	fTime := primitive.NewDateTimeFromTime(time.Unix(fromTime, 0))

	var dt bson.D

	// construct date condition
	if toTime != 0 {
		tTime := primitive.NewDateTimeFromTime(time.Unix(toTime, 0))
		dt = bson.D{{Key: "$gte", Value: fTime}, {Key: "$lte", Value: tTime}}
	} else {
		dt = bson.D{{Key: "$gte", Value: fTime}}
	}

	// create command pipeline
	pipe := mongo.Pipeline{
		{{Key: "$match", Value: bson.D{
			{Key: "date", Value: dt},
			{Key: "pair", Value: pairAddress.String()}}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$pair"},
			{Key: "total", Value: bson.M{"$sum": bson.D{
				{Key: "$add", Value: bson.A{"$am0in", "$am0out"}}}}},
		}}},
	}

	// query collection
	col := db.client.Database(db.dbName).Collection(coUniswap)
	cursor, err := col.Aggregate(context.Background(), pipe)
	def := types.DefiSwapVolume{
		PairAddress: pairAddress,
		Volume:      big.NewInt(0)}

	if err != nil {
		db.log.Errorf("Can not get swap volumes: %s", err.Error())
		return def, err
	}

	// make sure to close the cursor
	defer func() {
		if err := cursor.Close(context.Background()); err != nil {
			db.log.Errorf("can not close cursor; %s", err.Error())
		}
	}()

	// get result and fill return data
	for cursor.Next(context.Background()) {
		var val Volume
		err := cursor.Decode(&val)
		if err != nil {
			fmt.Println(err.Error())
		}

		v := returnDecimals(big.NewInt(val.Total), swapAmountDecimalsCorrection)
		def.Volume = v
	}

	return def, nil
}

// UniswapVolume resolves volume of swap trades for a specified pair and date interval.
// If toTime is 0, it calculates volumes till now.
func (db *PostgreSQLBridge) UniswapVolume(pairAddress *common.Address, fromTime int64, toTime int64) (types.DefiSwapVolume, error) {
	// Initialize the result structure
	def := types.DefiSwapVolume{
		PairAddress: pairAddress,
		Volume:      big.NewInt(0),
	}

	// Construct the query
	var query string
	var args []interface{}

	// Translate Unix timestamps to PostgreSQL format
	fromDate := time.Unix(fromTime, 0)
	if toTime != 0 {
		toDate := time.Unix(toTime, 0)
		query = `
			SELECT COALESCE(SUM(am0in + am0out), 0) AS total
			FROM swaps
			WHERE pair = $1
			AND date BETWEEN $2 AND $3
		`
		args = []interface{}{pairAddress.String(), fromDate, toDate}
	} else {
		query = `
			SELECT COALESCE(SUM(am0in + am0out), 0) AS total
			FROM swaps
			WHERE pair = $1
			AND date >= $2
		`
		args = []interface{}{pairAddress.String(), fromDate}
	}

	// Execute the query
	var totalVolume int64
	err := db.db.QueryRowContext(context.Background(), query, args...).Scan(&totalVolume)
	if err != nil {
		db.log.Errorf("Cannot get swap volumes: %s", err.Error())
		return def, err
	}

	// Apply decimals correction and set the volume
	def.Volume = returnDecimals(big.NewInt(totalVolume), swapAmountDecimalsCorrection)
	return def, nil
}

// UniswapTimeVolumes resolves volumes of swap trades for specified pair grouped by date interval.
// If toTime is 0, then it calculates volumes till now
func (db *MongoDbBridge) UniswapTimeVolumes(pairAddress *common.Address, resolution string, fromTime int64, toTime int64) ([]types.DefiSwapVolume, error) {

	fTime := primitive.NewDateTimeFromTime(time.Unix(fromTime, 0))

	var dt bson.D

	if toTime != 0 {
		tTime := primitive.NewDateTimeFromTime(time.Unix(toTime, 0))
		dt = bson.D{{Key: "$gte", Value: fTime}, {Key: "$lte", Value: tTime}}
	} else {
		dt = bson.D{{Key: "$gte", Value: fTime}}
	}

	// create query pipeline
	pipe := mongo.Pipeline{
		{{Key: "$match", Value: bson.D{
			{Key: "date", Value: dt},
			{Key: "pair", Value: pairAddress.String()}}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: getGroupBsonD(resolution)},
			{Key: "total", Value: bson.M{"$sum": bson.D{
				{Key: "$add", Value: bson.A{"$am0in", "$am0out"}}}}},
		}}},
		{{Key: "$sort", Value: bson.D{
			{Key: "_id", Value: 1},
		}}},
	}

	list := make([]types.DefiSwapVolume, 0)

	// execute query
	col := db.client.Database(db.dbName).Collection(coUniswap)
	cursor, err := col.Aggregate(context.Background(), pipe)

	if err != nil {
		db.log.Errorf(err.Error())
		return list, nil
	}

	defer func() {
		if err := cursor.Close(context.Background()); err != nil {
			db.log.Errorf("can not close cursor; %s", err.Error())
		}
	}()

	// iterate thru results and construct data
	for cursor.Next(context.Background()) {
		var val Volume
		err := cursor.Decode(&val)
		if err != nil {
			db.log.Errorf(err.Error())
		}
		def := types.DefiSwapVolume{
			PairAddress: pairAddress,
			Volume:      returnDecimals(big.NewInt(val.Total), swapAmountDecimalsCorrection),
			DateString:  val.ID,
		}
		list = append(list, def)
	}

	return list, nil
}

// getGroupBsonD is a helper function for constructing group db request
func getGroupBsonD(resolution string) bson.D {

	// initial value set to 1 minute
	mul := 60 * 1000

	switch resolution {
	case "month":
		mul *= 30 * 24 * 60
	case "day":
		mul *= 24 * 60
	case "4h":
		mul *= 4 * 60
	case "1h":
		mul *= 60
	case "30m":
		mul *= 30
	case "15m":
		mul *= 15
	case "5m":
		mul *= 5
	case "1m":
		mul *= 1
	default:
		mul *= 24 * 60
	}

	format := "%Y-%m-%dT%H:%M:%S.000Z"
	groupBson := bson.D{
		{Key: "$dateToString", Value: bson.D{
			{Key: "format", Value: format},
			{Key: "date", Value: bson.D{
				{Key: "$add", Value: bson.A{bson.D{
					{Key: "$subtract", Value: bson.A{
						bson.D{{Key: "$subtract", Value: bson.A{"$date", 0}}},
						bson.D{{Key: "$mod", Value: bson.A{bson.D{
							{Key: "$toLong", Value: bson.D{
								{Key: "$subtract", Value: bson.A{"$date", 0}}}}},
							mul}},
						},
					}}},
					0}},
			},
			}}},
	}
	return groupBson
}

// getDateBsonD is a helper function for constructing date db request
func getDateBsonD(fromTime int64, toTime int64) bson.D {
	fTime := primitive.NewDateTimeFromTime(time.Unix(fromTime, 0))

	var dt bson.D

	if toTime != 0 {
		tTime := primitive.NewDateTimeFromTime(time.Unix(toTime, 0))
		dt = bson.D{{Key: "$gte", Value: fTime}, {Key: "$lte", Value: tTime}}
	} else {
		dt = bson.D{{Key: "$gte", Value: fTime}}
	}

	return dt
}

// UniswapTimePrices resolves price of swap trades for specified pair grouped by date interval.
// If toTime is 0, then it calculates prices till now
func (db *MongoDbBridge) UniswapTimePrices(pairAddress *common.Address, resolution string, fromTime int64, toTime int64, direction int32) ([]types.DefiTimePrice, error) {
	tokenASum := bson.D{{Key: "$add", Value: bson.A{"$am0in", "$am0out"}}}
	tokenBSum := bson.D{{Key: "$add", Value: bson.A{"$am1in", "$am1out"}}}

	// creating priceBsonD bson request object
	var priceBsonD bson.D
	if direction == 0 {
		priceBsonD = bson.D{{Key: "$divide", Value: bson.A{tokenASum, tokenBSum}}}
	} else {
		priceBsonD = bson.D{{Key: "$divide", Value: bson.A{tokenBSum, tokenASum}}}
	}

	// create query pipeline
	pipe := mongo.Pipeline{
		{{Key: "$match", Value: bson.D{
			{Key: "date", Value: getDateBsonD(fromTime, toTime)},
			{Key: "type", Value: bson.D{
				{Key: "$not", Value: bson.D{
					{Key: "$eq", Value: types.SwapSync}}}}},
			{Key: "pair", Value: pairAddress.String()}}}},
		{{Key: "$sort", Value: bson.D{
			{Key: "date", Value: 1},
		}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: getGroupBsonD(resolution)},
			{Key: "low", Value: bson.D{
				{Key: "$min", Value: priceBsonD}}},
			{Key: "high", Value: bson.D{
				{Key: "$max", Value: priceBsonD}}},
			{Key: "open", Value: bson.D{
				{Key: "$first", Value: priceBsonD}}},
			{Key: "close", Value: bson.D{
				{Key: "$last", Value: priceBsonD}}},
			{Key: "avg", Value: bson.D{
				{Key: "$avg", Value: priceBsonD}}},
		}}},
		{{Key: "$sort", Value: bson.D{
			{Key: "_id", Value: 1},
		}}},
	}

	list := make([]types.DefiTimePrice, 0)

	// execute query
	col := db.client.Database(db.dbName).Collection(coUniswap)
	cursor, err := col.Aggregate(context.Background(), pipe)
	if err != nil {
		db.log.Errorf(err.Error())
		return list, nil
	}

	defer func() {
		if err := cursor.Close(context.Background()); err != nil {
			db.log.Errorf("can not close cursor; %s", err.Error())
		}
	}()

	// iterate thru results and construct data
	for cursor.Next(context.Background()) {
		var priceVal types.DefiTimePrice
		err := cursor.Decode(&priceVal)
		if err != nil {
			db.log.Errorf(err.Error())
		}
		priceVal.PairAddress = *pairAddress
		list = append(list, priceVal)
	}

	return list, nil
}

// UniswapTimeReserves resolves reserves of uniswap trades for specified pair grouped by date interval.
// If toTime is 0, then it calculates prices till now
func (db *MongoDbBridge) UniswapTimeReserves(pairAddress *common.Address, resolution string, fromTime int64, toTime int64) ([]types.DefiTimeReserve, error) {

	// create query pipeline
	pipe := mongo.Pipeline{
		{{Key: "$match", Value: bson.D{
			{Key: "date", Value: getDateBsonD(fromTime, toTime)},
			{Key: "pair", Value: pairAddress.String()}}}},
		{{Key: "$sort", Value: bson.D{
			{Key: "date", Value: 1},
		}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: getGroupBsonD(resolution)},
			{Key: "close0", Value: bson.D{
				{Key: "$last", Value: "$" + fiSwapReserve0}}},

			{Key: "close1", Value: bson.D{
				{Key: "$last", Value: "$" + fiSwapReserve1}}},
		}}},
		{{Key: "$sort", Value: bson.D{
			{Key: "_id", Value: 1},
		}}},
	}

	type TimeReserve struct {

		// Time represents ISO time tag for this price
		Time string `bson:"_id"`

		// average price for this time period
		Close0 int64 `bson:"close0"`
		Close1 int64 `bson:"close1"`
	}

	list := make([]types.DefiTimeReserve, 0)

	// execute query
	col := db.client.Database(db.dbName).Collection(coUniswap)
	cursor, err := col.Aggregate(context.Background(), pipe)
	if err != nil {
		db.log.Errorf(err.Error())
		return list, nil
	}

	defer func() {
		if err := cursor.Close(context.Background()); err != nil {
			db.log.Errorf("can not close cursor; %s", err.Error())
		}
	}()

	// iterate thru results and construct data
	for cursor.Next(context.Background()) {
		var reserveVal TimeReserve
		err := cursor.Decode(&reserveVal)
		if err != nil {
			db.log.Errorf(err.Error())
		}

		res := types.DefiTimeReserve{
			Time: reserveVal.Time,
			ReserveClose: []hexutil.Big{
				hexutil.Big(*returnDecimals(new(big.Int).SetInt64(reserveVal.Close0), swapReserveDecimalsCorrection)),
				hexutil.Big(*returnDecimals(new(big.Int).SetInt64(reserveVal.Close1), swapReserveDecimalsCorrection))},
		}

		list = append(list, res)
	}

	return list, nil
}

// UniswapActions provides list of uniswap actions stored in the persistent storage.
func (db *MongoDbBridge) UniswapActions(pairAddress *common.Address, cursor *string, count int32, actionType int32) (*types.UniswapActionList, error) {
	// nothing to load?
	if count == 0 {
		return nil, fmt.Errorf("nothing to do, zero uniswap actions requested")
	}

	// get the collection and context
	col := db.client.Database(db.dbName).Collection(coUniswap)

	// init the list
	list, err := db.uniswapActionListInit(col, pairAddress, cursor, count, actionType)
	if err != nil {
		db.log.Errorf("can not build uniswap action list; %s", err.Error())
		return nil, err
	}

	// load data
	err = db.uniswapActionListLoad(col, pairAddress, actionType, cursor, count, list)
	if err != nil {
		db.log.Errorf("can not load uniswap action list from database; %s", err.Error())
		return nil, err
	}

	// shift the first item on cursor
	if cursor != nil {
		list.First = list.Collection[0].OrdIndex
	}

	return list, nil
}

// UniswapActions provides a list of uniswap actions stored in PostgreSQL.
func (db *PostgreSQLBridge) UniswapActions(pairAddress *common.Address, cursor *string, count int32, actionType int32) (*types.UniswapActionList, error) {
	// nothing to load?
	if count == 0 {
		return nil, fmt.Errorf("nothing to do, zero uniswap actions requested")
	}

	// get the database connection (directly use the client pool)
	conn := db.client // No need to call .Conn(), db.client is the connection pool

	// initialize the list
	list, err := db.uniswapActionListInit(conn, pairAddress, cursor, count, actionType)
	if err != nil {
		db.log.Errorf("cannot build uniswap action list; %s", err.Error())
		return nil, err
	}

	// load data using the PostgreSQL query
	err = db.uniswapActionListLoad(conn, pairAddress, actionType, cursor, count, list)
	if err != nil {
		db.log.Errorf("cannot load uniswap action list from database; %s", err.Error())
		return nil, err
	}

	// shift the first item on cursor (if cursor is provided)
	if cursor != nil && len(list.Collection) > 0 {
		list.First = list.Collection[0].OrdIndex
	}

	return list, nil
}

// contractListInit initializes list of contracts based on provided cursor and count.
func (db *MongoDbBridge) uniswapActionListInit(col *mongo.Collection, pairAddress *common.Address, cursor *string, count int32, actionType int32) (*types.UniswapActionList, error) {
	// make the list
	list := types.UniswapActionList{
		Collection: make([]*types.UniswapAction, 0),
		Total:      0,
		First:      0,
		Last:       0,
		IsStart:    false,
		IsEnd:      false,
	}

	// calculate the total number of contracts in the list
	if err := db.uniswapActionListTotal(col, pairAddress, &list, actionType); err != nil {
		return nil, err
	}

	// inform what we are about to do
	db.log.Debugf("Found %d uniswap actions in off-chain database for specified criteria", list.Total)

	// find the top uniswap action of the list
	if err := db.uniswapActionListTop(col, pairAddress, actionType, cursor, count, &list); err != nil {
		return nil, err
	}

	// inform what we are about to do
	db.log.Debugf("Uniswap action list initialized with ordinal index %d", list.First)
	return &list, nil
}

// uniswapActionListInit initializes the uniswap action list based on the provided cursor and count.
func (db *PostgreSQLBridge) uniswapActionListInit(conn *sql.DB, pairAddress *common.Address, cursor *string, count int32, actionType int32) (*types.UniswapActionList, error) {
	// Initialize the list
	list := &types.UniswapActionList{
		Collection: make([]*types.UniswapAction, 0),
		Total:      0,
		First:      0,
		Last:       0,
		IsStart:    false,
		IsEnd:      false,
	}

	// Calculate the total number of actions for the specified criteria
	err := db.uniswapActionListTotal(conn, pairAddress, actionType, &list.Total)
	if err != nil {
		return nil, err
	}

	// Log the total number of records
	db.log.Debugf("Found %d uniswap actions in PostgreSQL database for specified criteria", list.Total)

	// Find the first uniswap action in the list (based on ord_index or timestamp)
	err = db.uniswapActionListTop(conn, pairAddress, actionType, cursor, count, list)
	if err != nil {
		return nil, err
	}

	// Log the first item in the list
	db.log.Debugf("Uniswap action list initialized with ordinal index %d", list.First)

	return list, nil
}

// uniswapActionListTotal find the total amount of uniswap events for the criteria and populates the list
func (db *MongoDbBridge) uniswapActionListTotal(col *mongo.Collection, pairAddress *common.Address, list *types.UniswapActionList, actionType int32) error {
	// prep the empty filter
	filter := bson.D{}
	filterPair := bson.D{}
	filterType := bson.D{}

	// validation filter for pair address
	if pairAddress != nil {
		filterPair = bson.D{{Key: fiSwapPair, Value: pairAddress.String()}}
	}

	// validation filter for action type
	if actionType >= 0 {
		filterType = bson.D{{Key: fiSwapType, Value: actionType}}
	}

	filterBlk := bson.D{{Key: fiSwapBlock, Value: bson.D{{Key: "$exists", Value: true}}}}

	filter = bson.D{{Key: "$and", Value: bson.A{filterPair, filterType, filterBlk}}}

	// find how many uniswap events do we have in the database
	total, err := col.CountDocuments(context.Background(), filter)
	if err != nil {
		db.log.Errorf("Can not count uniswap actions: %v", err.Error())
		return err
	}

	// apply the total count
	list.Total = uint64(total)
	return nil
}

// uniswapActionListTotal finds the total amount of uniswap events for the criteria and populates the list
func (db *PostgreSQLBridge) uniswapActionListTotal(pairAddress *common.Address, list *types.UniswapActionList, actionType int32) error {
	// Start building the SQL query
	query := "SELECT COUNT(*) FROM uniswap_actions WHERE 1=1"

	// Add filter for pair address if provided
	if pairAddress != nil {
		query += " AND pair_address = $1"
	}

	// Add filter for action type if provided
	if actionType >= 0 {
		query += " AND action_type = $2"
	}

	// Execute the query and get the count
	var total int64
	err := db.db.QueryRow(query, pairAddress.String(), actionType).Scan(&total)
	if err != nil {
		db.log.Errorf("Can not count uniswap actions: %v", err.Error())
		return err
	}

	// Set the total count
	list.Total = uint64(total)
	return nil
}

// uniswapActionListTop find the first uniswap action of the list based on provided criteria and populates the list.
func (db *MongoDbBridge) uniswapActionListTop(col *mongo.Collection, pairAddress *common.Address, actionType int32, cursor *string, count int32, list *types.UniswapActionList) error {
	// get the filter
	filter, err := uniswapActionListTopFilter(pairAddress, cursor, actionType)
	if err != nil {
		db.log.Errorf("can not find top uniswap action for the list; %s", err.Error())
		return err
	}

	// find out the cursor ordinal index
	if cursor == nil && count > 0 {
		// get the highest available ordinal index (top uniswap action)
		list.First, err = db.findUniswapActionBorderOrdinalIndex(col,
			*filter,
			options.FindOne().SetSort(bson.D{{Key: fiSwapOrdIndex, Value: -1}}))
		list.IsStart = true

	} else if cursor == nil && count < 0 {
		// get the lowest available ordinal index (bottom uniswap action)
		list.First, err = db.findUniswapActionBorderOrdinalIndex(col,
			*filter,
			options.FindOne().SetSort(bson.D{{Key: fiSwapOrdIndex, Value: 1}}))
		list.IsEnd = true

	} else if cursor != nil {
		// get the highest available ordinal index (top uniswap action)
		list.First, err = db.findUniswapActionBorderOrdinalIndex(col,
			*filter,
			options.FindOne())
	}

	// check the error
	if err != nil {
		db.log.Errorf("can not find the initial uniswap action")
		return err
	}

	return nil
}

// uniswapActionListTop finds the first uniswap action of the list based on provided criteria and populates the list.
func (db *PostgreSQLBridge) uniswapActionListTop(pairAddress *common.Address, actionType int32, cursor *string, count int32, list *types.UniswapActionList) error {
	// Build the filter conditions based on the pair address, cursor, and action type
	query, args := uniswapActionListTopFilterPostgres(pairAddress, cursor, actionType)

	// Determine the sorting order based on the cursor and count
	var orderBy string
	//var limit int
	var isStart bool
	var isEnd bool

	if cursor == nil && count > 0 {
		// Get the highest available ordinal index (top uniswap action)
		orderBy = "ORDER BY ord_index DESC"
		//limit = 1
		isStart = true
	} else if cursor == nil && count < 0 {
		// Get the lowest available ordinal index (bottom uniswap action)
		orderBy = "ORDER BY ord_index ASC"
		//limit = 1
		isEnd = true
	} else if cursor != nil {
		// Use the cursor to fetch the next set of uniswap actions
		orderBy = "ORDER BY ord_index DESC"
		//limit = count
	}

	// Build the final query with the limit and sorting
	sqlQuery := fmt.Sprintf("%s %s LIMIT $%d", query, orderBy, len(args)+1)

	// Execute the query
	rows, err := db.db.Query(sqlQuery, append(args, cursor)...)
	if err != nil {
		db.log.Errorf("can not find the initial uniswap action: %v", err)
		return err
	}
	defer rows.Close()

	// Fetch the first record
	if rows.Next() {
		var ordIndex int32
		err := rows.Scan(&ordIndex) // Assume ord_index is the field to fetch
		if err != nil {
			db.log.Errorf("Error scanning row: %v", err)
			return err
		}

		list.First = uint64(ordIndex)

		// Set the IsStart and IsEnd flags based on the criteria
		list.IsStart = isStart
		list.IsEnd = isEnd
	}

	return nil
}

// uniswapActionListTopFilter constructs a filter for finding the top item of the list.
func uniswapActionListTopFilter(pairAddress *common.Address, cursor *string, actionType int32) (*bson.D, error) {
	// what is the requested ordinal index from cursor, if any
	var ix uint64
	if cursor != nil {
		var err error

		// get the ordinal index based on cursor
		ix, err = strconv.ParseUint(*cursor, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor value; %s", err.Error())
		}
	}

	// prep the empty filter (no cursor and any validation status)
	filter := bson.D{}
	filterPair := bson.D{}
	filterType := bson.D{}
	filterCursor := bson.D{}

	// filter for pair address
	if pairAddress != nil {
		filterPair = bson.D{{Key: fiSwapPair, Value: pairAddress.String()}}
	}

	// filter for action type
	if actionType >= 0 {
		filterType = bson.D{{Key: fiSwapType, Value: actionType}}
	}

	// filter for cursor
	if cursor != nil {
		filterCursor = bson.D{{Key: fiSwapOrdIndex, Value: ix}}
	}

	filter = bson.D{{Key: "$and", Value: bson.A{filterPair, filterType, filterCursor}}}

	return &filter, nil
}

// uniswapActionListTopFilter builds the WHERE clause and arguments for the filter
func uniswapActionListTopFilterPostgres(pairAddress *common.Address, cursor *string, actionType int32) (string, []interface{}) {
	// Start with the base query
	query := "SELECT ord_index FROM uniswap_actions WHERE 1=1"
	args := []interface{}{}

	// Add filter for pair address if provided
	if pairAddress != nil {
		query += " AND pair_address = $1"
		args = append(args, pairAddress.String())
	}

	// Add filter for action type if provided
	if actionType >= 0 {
		query += " AND action_type = $2"
		args = append(args, actionType)
	}

	// Add filter for cursor if provided (assuming cursor is an ordinal index)
	if cursor != nil {
		query += " AND ord_index > $3"
		args = append(args, cursor)
	}

	return query, args
}

// uniswapActionListLoad loads the initialized uniswap action list from persistent database.
func (db *MongoDbBridge) uniswapActionListLoad(col *mongo.Collection, pairAddress *common.Address, actionType int32, cursor *string, count int32, list *types.UniswapActionList) error {
	// get the context for loader
	ctx := context.Background()

	// load the data
	ld, err := col.Find(ctx, db.uniswapActionListFilter(pairAddress, actionType, cursor, count, list), db.uniswapActionListOptions(count))
	if err != nil {
		db.log.Errorf("error loading uniswap action list; %s", err.Error())
		return err
	}

	// close the cursor as we leave
	defer func() {
		err := ld.Close(ctx)
		if err != nil {
			db.log.Errorf("error closing uniswap action list cursor; %s", err.Error())
		}
	}()

	type UniswapActionDB struct {
		ID              string         `bson:"_id"`
		OrdIndex        uint64         `bson:"orx"`
		BlockNr         hexutil.Uint64 `bson:"blk"`
		Type            int32          `bson:"type"`
		PairAddress     string         `bson:"pair"`
		Sender          string         `bson:"sender"`
		TransactionHash string         `bson:"tx"`
		Time            time.Time      `bson:"date"`
		Amount0in       int64          `bson:"am0in"`
		Amount0out      int64          `bson:"am0out"`
		Amount1in       int64          `bson:"am1in"`
		Amount1out      int64          `bson:"am1out"`
	}
	// loop and load
	var uniswapAction *types.UniswapAction
	for ld.Next(ctx) {
		// process the last found hash
		if uniswapAction != nil {
			list.Collection = append(list.Collection, uniswapAction)
			list.Last = uniswapAction.OrdIndex
		}

		// try to decode the next row
		ua := types.UniswapAction{}
		var udb UniswapActionDB
		if err := ld.Decode(&udb); err != nil {
			db.log.Errorf("can not decode uniswap action list row; %s", err.Error())
			return err
		}

		// decode data
		ua.ID = common.HexToHash(udb.ID)
		ua.OrdIndex = udb.OrdIndex
		ua.BlockNr = udb.BlockNr
		ua.Type = udb.Type
		ua.PairAddress = common.HexToAddress(udb.PairAddress)
		ua.Sender = common.HexToAddress(udb.Sender)
		ua.TransactionHash = common.HexToHash(udb.TransactionHash)
		ua.Time = hexutil.Uint64(udb.Time.UTC().Unix())
		ua.Amount0in = *(*hexutil.Big)(returnDecimals(big.NewInt(udb.Amount0in), swapAmountDecimalsCorrection))
		ua.Amount0out = *(*hexutil.Big)(returnDecimals(big.NewInt(udb.Amount0out), swapAmountDecimalsCorrection))
		ua.Amount1in = *(*hexutil.Big)(returnDecimals(big.NewInt(udb.Amount1in), swapAmountDecimalsCorrection))
		ua.Amount1out = *(*hexutil.Big)(returnDecimals(big.NewInt(udb.Amount1out), swapAmountDecimalsCorrection))

		// keep this one
		uniswapAction = &ua
	}
	// we should have all the items already; we may just need to check if a boundary was reached
	if cursor != nil {
		list.IsEnd = count > 0 && int32(len(list.Collection)) < count
		list.IsStart = count < 0 && int32(len(list.Collection)) < -count

		// add the last item as well
		if (list.IsStart || list.IsEnd) && uniswapAction != nil {
			list.Collection = append(list.Collection, uniswapAction)
			list.Last = uniswapAction.OrdIndex
		}
	}

	return nil
}

// uniswapActionListLoad loads the initialized uniswap action list from PostgreSQL.
func (db *PostgreSQLBridge) uniswapActionListLoad(pairAddress *common.Address, actionType int32, cursor *string, count int32, list *types.UniswapActionList) error {
	// Build the query filter and arguments based on the pairAddress, actionType, and cursor
	query, args := uniswapActionListTopFilterPostgres(pairAddress, cursor, actionType)

	// Add ordering and pagination logic
	orderBy := "ORDER BY ord_index DESC" // Default to descending order
	if cursor != nil {
		orderBy = "ORDER BY ord_index ASC"
	}

	// Add the limit and offset for pagination
	limit := count
	offset := 0
	if cursor != nil {
		offset = 1
	}

	// Final SQL query
	sqlQuery := fmt.Sprintf("%s %s LIMIT $%d OFFSET $%d", query, orderBy, len(args)+1, len(args)+2)

	// Execute the query
	rows, err := db.db.Query(sqlQuery, append(args, cursor, limit, offset)...)
	if err != nil {
		db.log.Errorf("error loading uniswap action list; %s", err.Error())
		return err
	}
	defer rows.Close()

	// Define the structure to scan the data into
	type UniswapActionDB struct {
		ID              string    `json:"id"`
		OrdIndex        uint64    `json:"ord_index"`
		BlockNr         uint64    `json:"block_nr"`
		Type            int32     `json:"type"`
		PairAddress     string    `json:"pair_address"`
		Sender          string    `json:"sender"`
		TransactionHash string    `json:"transaction_hash"`
		Time            time.Time `json:"time"`
		Amount0in       int64     `json:"amount0_in"`
		Amount0out      int64     `json:"amount0_out"`
		Amount1in       int64     `json:"amount1_in"`
		Amount1out      int64     `json:"amount1_out"`
	}

	// Loop through the rows and process the data
	var uniswapAction *types.UniswapAction
	for rows.Next() {
		var udb UniswapActionDB
		if err := rows.Scan(&udb.ID, &udb.OrdIndex, &udb.BlockNr, &udb.Type, &udb.PairAddress, &udb.Sender, &udb.TransactionHash, &udb.Time, &udb.Amount0in, &udb.Amount0out, &udb.Amount1in, &udb.Amount1out); err != nil {
			db.log.Errorf("can not scan row into uniswap action; %s", err.Error())
			return err
		}

		// Map database row to UniswapAction
		ua := types.UniswapAction{
			ID:       common.HexToHash(udb.ID),
			OrdIndex: udb.OrdIndex,
			//BlockNr:         udb.BlockNr,
			Type:            udb.Type,
			PairAddress:     common.HexToAddress(udb.PairAddress),
			Sender:          common.HexToAddress(udb.Sender),
			TransactionHash: common.HexToHash(udb.TransactionHash),
			Time:            hexutil.Uint64(udb.Time.UTC().Unix()),
			Amount0in:       *(*hexutil.Big)(returnDecimals(big.NewInt(udb.Amount0in), swapAmountDecimalsCorrection)),
			Amount0out:      *(*hexutil.Big)(returnDecimals(big.NewInt(udb.Amount0out), swapAmountDecimalsCorrection)),
			Amount1in:       *(*hexutil.Big)(returnDecimals(big.NewInt(udb.Amount1in), swapAmountDecimalsCorrection)),
			Amount1out:      *(*hexutil.Big)(returnDecimals(big.NewInt(udb.Amount1out), swapAmountDecimalsCorrection)),
		}

		// Append the action to the list
		if uniswapAction != nil {
			list.Collection = append(list.Collection, uniswapAction)
			list.Last = uniswapAction.OrdIndex
		}

		// Keep track of the current action
		uniswapAction = &ua
	}

	// Check for boundary conditions (IsStart, IsEnd)
	if cursor != nil {
		list.IsEnd = count > 0 && int32(len(list.Collection)) < count
		list.IsStart = count < 0 && int32(len(list.Collection)) < -count

		// Add the last item to the list
		if (list.IsStart || list.IsEnd) && uniswapAction != nil {
			list.Collection = append(list.Collection, uniswapAction)
			list.Last = uniswapAction.OrdIndex
		}
	}

	return nil
}

// uniswapActionListFilter creates a filter for uniswap action list search.
func (db *MongoDbBridge) uniswapActionListFilter(pairAddress *common.Address, actionType int32, cursor *string, count int32, list *types.UniswapActionList) *bson.D {
	// inform what we are about to do
	db.log.Debugf("uniswap action filter starts from index %d", list.First)

	// prep base operator
	ordinalOp := "$lte"

	// no cursor and bottom up list
	if cursor == nil && count < 0 {
		ordinalOp = "$gte"
	}

	// we have the cursor and we scan from top
	if cursor != nil && count > 0 {
		ordinalOp = "$lt"
	}

	// we have the cursor and we scan from bottom
	if cursor != nil && count < 0 {
		ordinalOp = "$gt"
	}

	// prep the empty filter (no cursor and any validation status)
	filter := bson.D{}
	filterPair := bson.D{}
	filterType := bson.D{}
	filterCursor := bson.D{}

	// filter for cursor
	filterCursor = bson.D{{Key: fiSwapOrdIndex, Value: bson.D{{Key: ordinalOp, Value: list.First}}}}

	// filter for pair address
	if pairAddress != nil {
		filterPair = bson.D{{Key: fiSwapPair, Value: pairAddress.String()}}
	}

	// filter for action type
	if actionType >= 0 {
		filterType = bson.D{{Key: fiSwapType, Value: actionType}}
	}

	filter = bson.D{{Key: "$and", Value: bson.A{filterPair, filterType, filterCursor}}}

	return &filter
}

// uniswapActionListFilter creates a filter for uniswap action list search in PostgreSQL.
func (db *PostgreSQLBridge) uniswapActionListFilter(pairAddress *common.Address, actionType int32, cursor *string, count int32, list *types.UniswapActionList) (string, []interface{}) {
	// Inform what we are about to do
	db.log.Debugf("uniswap action filter starts from index %d", list.First)

	// Prepare the base SQL WHERE clause
	whereClause := "WHERE 1=1"
	args := []interface{}{}

	// Prepare the filter for the ordinal index
	ordinalOp := "<=" // Default to $lte for MongoDB equivalent
	if cursor == nil && count < 0 {
		ordinalOp = ">=" // MongoDB equivalent of $gte
	}

	if cursor != nil && count > 0 {
		ordinalOp = "<" // MongoDB equivalent of $lt
	}

	if cursor != nil && count < 0 {
		ordinalOp = ">" // MongoDB equivalent of $gt
	}

	// Filter for cursor (ordinal index)
	if cursor != nil {
		whereClause += fmt.Sprintf(" AND ord_index %s $%d", ordinalOp, len(args)+1)
		args = append(args, cursor)
	} else {
		whereClause += fmt.Sprintf(" AND ord_index %s $%d", ordinalOp, len(args)+1)
		args = append(args, list.First)
	}

	// Filter for pair address
	if pairAddress != nil {
		whereClause += fmt.Sprintf(" AND pair_address = $%d", len(args)+1)
		args = append(args, pairAddress.String())
	}

	// Filter for action type
	if actionType >= 0 {
		whereClause += fmt.Sprintf(" AND type = $%d", len(args)+1)
		args = append(args, actionType)
	}

	return whereClause, args
}

// uniswapActionListOptions creates a filter options set for uniswap action list search.
func (db *MongoDbBridge) uniswapActionListOptions(count int32) *options.FindOptions {
	// prep options
	opt := options.Find()

	// how to sort results in the collection
	if count > 0 {
		// from high (new) to low (old)
		opt.SetSort(bson.D{{Key: fiSwapOrdIndex, Value: -1}})
	} else {
		// from low (old) to high (new)
		opt.SetSort(bson.D{{Key: fiSwapOrdIndex, Value: 1}})
	}

	// prep the loading limit
	var limit = int64(count)
	if limit < 0 {
		limit = -limit
	}

	// try to get one more
	limit++

	// apply the limit
	opt.SetLimit(limit)
	return opt
}

// findUniswapActionBorderOrdinalIndex finds the highest, or lowest ordinal index in the collection.
// For negative sort it will return highest and for positive sort it will return lowest available value.
func (db *MongoDbBridge) findUniswapActionBorderOrdinalIndex(col *mongo.Collection, filter bson.D, opt *options.FindOneOptions) (uint64, error) {
	// prep container
	var row struct {
		Value uint64 `bson:"orx"`
	}

	// make sure we pull only what we need
	opt.SetProjection(bson.D{{Key: fiSwapOrdIndex, Value: true}})
	sr := col.FindOne(context.Background(), filter, opt)

	// try to decode
	err := sr.Decode(&row)
	if err != nil {
		return 0, err
	}

	return row.Value, nil
}

// findUniswapActionBorderOrdinalIndex finds the highest or lowest ordinal index in the collection.
// For negative sort, it will return the highest, and for positive sort, it will return the lowest available value.
func (db *PostgreSQLBridge) findUniswapActionBorderOrdinalIndex(filter string, args []interface{}) (uint64, error) {
	// Prepare the query to fetch the ordinal index based on the filter and sorting order
	query := `
		SELECT ord_index
		FROM uniswap_actions
		WHERE ` + filter + `
		ORDER BY ord_index
		LIMIT 1`

	// Execute the query
	var ordIndex uint64
	err := db.db.QueryRow(query, args...).Scan(&ordIndex)
	if err != nil {
		return 0, fmt.Errorf("could not find ordinal index: %v", err)
	}

	return ordIndex, nil
}
