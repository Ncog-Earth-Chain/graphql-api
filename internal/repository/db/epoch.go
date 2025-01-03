// Package db implements bridge to persistent storage represented by Mongo database.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	// colEpochs represents the name of the epochs collection in database.
	colEpochs = "epochs"

	// fiEpochPk is the name of the primary key of the collection.
	fiEpochPk = "_id"

	// fiEpochEndTime is the name of the epoch end field in the collection.
	fiEpochEndTime = "end"
)

// initEpochsCollection initializes the epochs collection with
// indexes and additional parameters needed by the app.
func (db *MongoDbBridge) initEpochsCollection(col *mongo.Collection) {
	// prepare index models
	ix := make([]mongo.IndexModel, 0)

	// index ordinal key sorted from high to low since this is the way we usually list
	ix = append(ix, mongo.IndexModel{
		Keys:    bson.D{{Key: fiEpochEndTime, Value: -1}},
		Options: new(options.IndexOptions).SetUnique(true),
	})

	// create indexes
	if _, err := col.Indexes().CreateMany(context.Background(), ix); err != nil {
		db.log.Panicf("can not create indexes for epoch collection; %s", err.Error())
	}
	db.log.Debugf("epochs collection initialized")
}

// initEpochsTable initializes the epochs table with indexes and additional parameters needed by the app.
func (db *PostgreSQLBridge) initEpochsCollection() error {
	// Create the epochs table if it doesn't already exist
	createTableQuery := `
	CREATE TABLE IF NOT EXISTS epochs (
		id SERIAL PRIMARY KEY,
		end_time TIMESTAMP NOT NULL UNIQUE
	);`

	// Execute the table creation query
	if _, err := db.db.Exec(createTableQuery); err != nil {
		db.log.Panicf("cannot create epochs table; %s", err.Error())
		return err
	}

	// Create an index on the end_time column, sorted in descending order
	createIndexQuery := `
	CREATE UNIQUE INDEX IF NOT EXISTS idx_epochs_end_time_desc
	ON epochs (end_time DESC);`

	// Execute the index creation query
	if _, err := db.db.Exec(createIndexQuery); err != nil {
		db.log.Panicf("cannot create index for epochs table; %s", err.Error())
		return err
	}

	db.log.Debugf("epochs table initialized with indexes")
	return nil
}

// AddEpoch stores an epoch reference in connected persistent storage.
func (db *MongoDbBridge) AddEpoch(e *types.Epoch) error {
	// do we have all needed data? we reject epochs without any stake
	if e == nil || e.EndTime == 0 || e.StakeTotalAmount.ToInt().Cmp(intZero) <= 0 {
		return fmt.Errorf("empty epoch received")
	}

	// get the collection for transactions
	col := db.client.Database(db.dbName).Collection(colEpochs)

	// if the transaction already exists, we don't need to add it
	// just make sure the transaction accounts were processed
	if db.isEpochKnown(col, e) {
		return nil
	}

	// try to do the insert
	if _, err := col.InsertOne(context.Background(), e); err != nil {
		db.log.Critical(err)
		return err
	}

	// make sure epochs collection is initialized
	if db.initEpochs != nil {
		db.initEpochs.Do(func() { db.initEpochsCollection(col); db.initEpochs = nil })
	}

	// log what we did
	db.log.Debugf("epoch #%d added to database", e.Id)
	return nil
}

// AddEpoch stores an epoch reference in connected PostgreSQL storage.
func (db *PostgreSQLBridge) AddEpoch(e *types.Epoch) error {
	// Validate input data
	if e == nil || e.EndTime == 0 || e.StakeTotalAmount.ToInt().Cmp(intZero) <= 0 {
		return fmt.Errorf("empty or invalid epoch received")
	}

	// Initialize the epochs table (if not already done)
	db.initEpochsCollection()

	// Check if the epoch is already known
	epochExists, err := db.isEpochKnown(int64(e.Id))
	if err != nil {
		db.log.Errorf("failed to check if epoch is known; %s", err.Error())
		return err
	}
	if epochExists {
		return nil
	}

	// Insert the epoch into the database
	query := `
	INSERT INTO epochs (id, end_time, stake_total_amount)
	VALUES ($1, $2, $3)
	ON CONFLICT (id) DO NOTHING;`

	_, err = db.db.Exec(query, e.Id, e.EndTime, e.StakeTotalAmount.String())
	if err != nil {
		db.log.Criticalf("failed to insert epoch; %s", err.Error())
		return err
	}

	// Log the addition
	db.log.Debugf("epoch #%d added to database", e.Id)
	return nil
}

// isEpochKnown checks if the given epoch has already been added to the database
func (db *MongoDbBridge) isEpochKnown(col *mongo.Collection, e *types.Epoch) bool {
	// try to find the epoch in the database (it may already exist)
	sr := col.FindOne(context.Background(), bson.D{
		{Key: fiEpochPk, Value: int64(e.Id)},
	}, options.FindOne().SetProjection(bson.D{
		{Key: fiEpochPk, Value: true},
	}))

	// error on lookup?
	if sr.Err() == nil {
		return true
	}

	db.log.Debugf("epoch #%d not found in database; %s", e.Id, sr.Err().Error())
	return false
}

// isEpochKnown checks if the given epoch has already been added to the database.
func (db *PostgreSQLBridge) isEpochKnown(epochID int64) (bool, error) {
	// Prepare the query to check if the epoch exists
	query := `
	SELECT 1 
	FROM epochs 
	WHERE id = $1 
	LIMIT 1;`

	// Execute the query
	var exists int
	err := db.db.QueryRow(query, epochID).Scan(&exists)
	if err != nil {
		if err == sql.ErrNoRows {
			// Epoch not found
			db.log.Debugf("epoch #%d not found in database", epochID)
			return false, nil
		}

		// Log any other error
		db.log.Errorf("failed to check if epoch exists; %s", err.Error())
		return false, err
	}

	// Epoch found
	return true, nil
}

// LastKnownEpoch provides the number of the newest epoch stored in the database.
func (db *MongoDbBridge) LastKnownEpoch() (uint64, error) {
	return db.epochListBorderPk(db.client.Database(db.dbName).Collection(colEpochs), options.FindOne().SetSort(bson.D{{Key: fiEpochEndTime, Value: -1}}))
}

// LastKnownEpoch provides the number of the newest epoch stored in the PostgreSQL database.
func (db *PostgreSQLBridge) LastKnownEpoch() (uint64, error) {
	// Query to get the most recent epoch by EndTime
	query := `
		SELECT id
		FROM epochs
		ORDER BY end_time DESC
		LIMIT 1
	`

	// Execute the query
	var epochId uint64
	err := db.db.QueryRow(query).Scan(&epochId)
	if err != nil {
		// Handle errors: if no rows were found, return an error
		if err == sql.ErrNoRows {
			return 0, nil // Return 0 if no epoch is found
		}
		// Log and return any other errors
		db.log.Errorf("error fetching last known epoch; %s", err.Error())
		return 0, err
	}

	return epochId, nil
}

// EpochsCount calculates total number of epochs in the database.
func (db *MongoDbBridge) EpochsCount() (uint64, error) {
	return db.EstimateCount(db.client.Database(db.dbName).Collection(colEpochs))
}

// EpochsCount calculates the total number of epochs in the database.
func (db *PostgreSQLBridge) EpochsCount() (int64, error) {
	// Define the SQL query to count rows in the 'epochs' table
	query := "SELECT COUNT(*) FROM epochs"

	// Execute the query and scan the result into a variable
	var count int64
	err := db.db.QueryRow(query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get epochs count: %w", err)
	}

	// Return the count as uint64
	return int64(count), nil
}

// epochListInit initializes list of epochs based on provided cursor, count.
func (db *MongoDbBridge) epochListInit(col *mongo.Collection, cursor *string, count int32) (*types.EpochList, error) {
	// find how many transactions do we have in the database
	total, err := db.EpochsCount()
	if err != nil {
		db.log.Errorf("can not count epochs")
		return nil, err
	}

	// make the list and notify the size of it
	list := types.EpochList{
		Collection: make([]*types.Epoch, 0),
		Total:      total,
		First:      0,
		Last:       0,
		IsStart:    total == 0,
		IsEnd:      total == 0,
	}

	// is the list non-empty? return the list with properly calculated range marks
	if 0 < total {
		return db.epochListCollectRangeMarks(col, &list, cursor, count)
	}

	// this is an empty list
	db.log.Debug("empty epoch list created")
	return &list, nil
}

// epochListInit initializes list of epochs based on provided cursor, count.
func (db *PostgreSQLBridge) epochListInit(cursor *string, count int32) (*types.EpochList, error) {
	// Query to count the number of epochs in the database
	query := `SELECT COUNT(*) FROM epochs`
	var total int64
	err := db.db.QueryRow(query).Scan(&total)
	if err != nil {
		db.log.Errorf("can not count epochs; %s", err.Error())
		return nil, err
	}

	// Initialize the list
	list := types.EpochList{
		Collection: make([]*types.Epoch, 0),
		Total:      uint64(total),
		First:      0,
		Last:       0,
		IsStart:    total == 0,
		IsEnd:      total == 0,
	}

	// If the list is non-empty, collect the range marks
	if total > 0 {
		return db.epochListCollectRangeMarks(&list, cursor, count)
	}

	// This is an empty list
	db.log.Debug("empty epoch list created")
	return &list, nil
}

// epochListCollectRangeMarks returns a list of epochs with proper First/Last marks.
func (db *MongoDbBridge) epochListCollectRangeMarks(col *mongo.Collection, list *types.EpochList, cursor *string, count int32) (*types.EpochList, error) {
	var err error

	// find out the cursor ordinal index
	if cursor == nil && count > 0 {
		// get the highest available pk
		list.First, err = db.epochListBorderPk(col, options.FindOne().SetSort(bson.D{{Key: fiEpochEndTime, Value: -1}}))
		list.IsStart = true

	} else if cursor == nil && count < 0 {
		// get the lowest available pk
		list.First, err = db.epochListBorderPk(col, options.FindOne().SetSort(bson.D{{Key: fiEpochEndTime, Value: 1}}))
		list.IsEnd = true

	} else if cursor != nil {
		// the cursor itself is the starting point
		list.First, err = hexutil.DecodeUint64(*cursor)
	}

	// check the error
	if err != nil {
		db.log.Errorf("can not find the initial epoch")
		return nil, err
	}

	// inform what we are about to do
	db.log.Debugf("epoch list initialized with epoch #%d", list.First)
	return list, nil
}

// epochListCollectRangeMarks returns a list of epochs with proper First/Last marks.
func (db *PostgreSQLBridge) epochListCollectRangeMarks(list *types.EpochList, cursor *string, count int32) (*types.EpochList, error) {
	var err error

	// Find out the cursor ordinal index
	if cursor == nil && count > 0 {
		// Get the highest available pk (latest epoch)
		list.First, err = db.epochListBorderPk("DESC")
		list.IsStart = true

	} else if cursor == nil && count < 0 {
		// Get the lowest available pk (oldest epoch)
		list.First, err = db.epochListBorderPk("ASC")
		list.IsEnd = true

	} else if cursor != nil {
		// The cursor itself is the starting point
		list.First, err = hexutil.DecodeUint64(*cursor)
	}

	// Check the error
	if err != nil {
		db.log.Errorf("can not find the initial epoch")
		return nil, err
	}

	// Inform what we are about to do
	db.log.Debugf("epoch list initialized with epoch #%d", list.First)
	return list, nil
}

// rewListBorderPk finds the top PK of the reward claims collection based on given filter and options.
func (db *MongoDbBridge) epochListBorderPk(col *mongo.Collection, opt *options.FindOneOptions) (uint64, error) {
	// prep container
	var row struct {
		Value uint64 `bson:"_id"`
	}

	// make sure we pull only what we need
	opt.SetProjection(bson.D{{Key: fiEpochPk, Value: true}})

	// try to decode
	sr := col.FindOne(context.Background(), bson.D{}, opt)
	err := sr.Decode(&row)
	if err != nil {
		return 0, err
	}
	return row.Value, nil
}

// epochListBorderPk gets the first or last epoch ID based on sorting order.
func (db *PostgreSQLBridge) epochListBorderPk(order string) (uint64, error) {
	var epochID uint64

	// Build the query to get the first or last epoch based on order
	query := fmt.Sprintf(`
		SELECT id
		FROM epochs
		ORDER BY end_time %s
		LIMIT 1`, order)

	// Execute the query
	err := db.db.QueryRow(query).Scan(&epochID)
	if err != nil {
		db.log.Errorf("error retrieving epoch border pk; %s", err.Error())
		return 0, err
	}

	return epochID, nil
}

// epochListFilter creates a filter for epoch list loading.
func (db *MongoDbBridge) epochListFilter(cursor *string, count int32, list *types.EpochList) *bson.D {
	// build an extended filter for the query; add PK (decoded cursor) to the original filter
	if cursor == nil {
		if count > 0 {
			return &bson.D{{Key: fiEpochPk, Value: bson.D{{Key: "$lte", Value: list.First}}}}
		}
		return &bson.D{{Key: fiEpochPk, Value: bson.D{{Key: "$gte", Value: list.First}}}}
	}

	// with cursor provided we need to skip the identified line
	if count > 0 {
		return &bson.D{{Key: fiEpochPk, Value: bson.D{{Key: "$lt", Value: list.First}}}}
	}
	return &bson.D{{Key: fiEpochPk, Value: bson.D{{Key: "$gt", Value: list.First}}}}
}

// epochListFilter creates a filter for epoch list loading.
func (db *PostgreSQLBridge) epochListFilter(cursor *string, count int32) (string, []interface{}) {
	filter := "1=1" // Default no-op condition
	params := []interface{}{}

	// Add cursor filtering
	if cursor != nil {
		if count > 0 {
			filter += " AND epoch_end_time > $1"
		} else {
			filter += " AND epoch_end_time < $1"
		}
		params = append(params, *cursor)
	}

	return filter, params
}

// epochListOptions creates a filter options set for epochs list search.
func (db *MongoDbBridge) epochListOptions(count int32) *options.FindOptions {
	// prep options
	opt := options.Find()

	// how to sort results in the collection
	// from high (new) to low (old) by default; reversed if loading from bottom
	sd := -1
	if count < 0 {
		sd = 1
	}

	// sort with the direction we want
	opt.SetSort(bson.D{{Key: fiEpochEndTime, Value: sd}})

	// prep the loading limit
	var limit = int64(count)
	if limit < 0 {
		limit = -limit
	}

	// apply the limit, try to get one more record so we can detect list end
	opt.SetLimit(limit + 1)
	return opt
}

// epochListOptions generates the SQL query options for epoch list search.
func (db *PostgreSQLBridge) epochListOptions(count int32) (string, []interface{}) {
	// Determine sort direction
	sortDirection := "DESC"
	if count < 0 {
		sortDirection = "ASC"
	}

	// Absolute value of count
	limit := int64(count)
	if limit < 0 {
		limit = -limit
	}

	// Construct the SQL query
	query := `
		SELECT * 
		FROM epochs 
		ORDER BY epoch_end_time %s 
		LIMIT $1
	`
	query = fmt.Sprintf(query, sortDirection)

	// Return query and parameters
	return query, []interface{}{limit + 1} // Fetch one more record to detect list end
}

// epochListLoad loads the initialized list of epochs from database.
func (db *MongoDbBridge) epochListLoad(col *mongo.Collection, cursor *string, count int32, list *types.EpochList) (err error) {
	// get the context for loader
	ctx := context.Background()

	// load the data
	ld, err := col.Find(ctx, db.epochListFilter(cursor, count, list), db.epochListOptions(count))
	if err != nil {
		db.log.Errorf("error loading epochs list; %s", err.Error())
		return err
	}

	defer db.closeCursor(ld)

	// loop and load the list; we may not store the last value
	var e *types.Epoch
	for ld.Next(ctx) {
		// append a previous value to the list, if we have one
		if e != nil {
			list.Collection = append(list.Collection, e)
		}

		// try to decode the next row
		var row types.Epoch
		if err = ld.Decode(&row); err != nil {
			db.log.Errorf("can not decode epoch list row; %s", err.Error())
			return err
		}

		// use this row as the next item
		e = &row
	}

	// we should have all the items already; we may just need to check if a boundary was reached
	list.IsEnd = (cursor == nil && count < 0) || (count > 0 && int32(len(list.Collection)) < count)
	list.IsStart = (cursor == nil && count > 0) || (count < 0 && int32(len(list.Collection)) < -count)

	// add the last item as well if we hit the boundary
	if (list.IsStart || list.IsEnd) && e != nil {
		list.Collection = append(list.Collection, e)
	}
	return nil
}

func (db *PostgreSQLBridge) epochListLoad(cursor *string, count int32, list *types.EpochList) (err error) {
	// Get sorting direction and limit from options
	sortDirection, limit := db.epochListOptions(count)

	// Generate the filter clause
	filter, params := db.epochListFilter(cursor, count)

	// Prepare the base query
	query := fmt.Sprintf(`
		SELECT id, epoch_end_time, epoch_fee, total_base_reward_weight, total_tx_reward_weight, 
		       base_reward_per_second, stake_total_amount, total_supply, other_columns
		FROM epochs 
		WHERE %s
		ORDER BY epoch_end_time %s
		LIMIT $%d
	`, filter, sortDirection, len(params)+1)

	// Append the limit to the parameters
	params = append(params, limit) // Fetch one more record to detect boundary

	// Execute the query
	rows, err := db.db.Query(query, params...)
	if err != nil {
		db.log.Errorf("error loading epochs list; %s", err.Error())
		return err
	}
	defer rows.Close()

	// Initialize the last epoch reference
	var e *types.Epoch

	// Loop through the results
	for rows.Next() {
		// Append the previous epoch to the list
		if e != nil {
			list.Collection = append(list.Collection, e)
		}

		// Decode the next row into an Epoch struct
		var row types.Epoch
		err = rows.Scan(
			&row.Id, &row.EndTime, &row.EpochFee, &row.TotalBaseRewardWeight, &row.TotalTxRewardWeight,
			&row.BaseRewardPerSecond, &row.StakeTotalAmount, &row.TotalSupply, &row.OtherColumns,
		)
		if err != nil {
			db.log.Errorf("can not decode epoch list row; %s", err.Error())
			return err
		}

		// Set the current row as the next item
		e = &row
	}

	// Handle boundary conditions
	list.IsEnd = (cursor == nil && count < 0) || (count > 0 && int32(len(list.Collection)) < count)
	list.IsStart = (cursor == nil && count > 0) || (count < 0 && int32(len(list.Collection)) < -count)

	// Add the last item if a boundary is reached
	if (list.IsStart || list.IsEnd) && e != nil {
		list.Collection = append(list.Collection, e)
	}

	return nil
}

// func (db *PostgreSQLBridge) epochListLoad(cursor *string, count int32, list *types.EpochList) (err error) {

// 	// Prepare the base query
// 	query := `
// 		SELECT id, epoch_end_time, other_columns
// 		FROM epochs
// 		WHERE ($1::text IS NULL OR epoch_end_time > $1::text)
// 		ORDER BY epoch_end_time %s
// 		LIMIT $2
// 	`
// 	// Determine sort direction
// 	sortDirection := "DESC"
// 	if count < 0 {
// 		sortDirection = "ASC"
// 	}
// 	query = fmt.Sprintf(query, sortDirection)

// 	// Absolute value of count
// 	limit := int64(count)
// 	if limit < 0 {
// 		limit = -limit
// 	}

// 	// Execute the query
// 	rows, err := db.db.Query(query, cursor, limit+1) // Fetch one more record to detect boundary
// 	if err != nil {
// 		db.log.Errorf("error loading epochs list; %s", err.Error())
// 		return err
// 	}
// 	defer rows.Close()

// 	// Load the data into the list
// 	var e *types.Epoch
// 	for rows.Next() {
// 		// Append the previous value to the list
// 		if e != nil {
// 			list.Collection = append(list.Collection, e)
// 		}

// 		// Decode the next row
// 		var row types.Epoch
// 		err = rows.Scan(&row.Id, &row.EndTime, &row.OtherColumns) // Adjust columns as needed
// 		if err != nil {
// 			db.log.Errorf("can not decode epoch list row; %s", err.Error())
// 			return err
// 		}

// 		// Use this row as the next item
// 		e = &row
// 	}

// 	// Check if a boundary was reached
// 	list.IsEnd = (cursor == nil && count < 0) || (count > 0 && int32(len(list.Collection)) < count)
// 	list.IsStart = (cursor == nil && count > 0) || (count < 0 && int32(len(list.Collection)) < -count)

// 	// Add the last item as well if we hit the boundary
// 	if (list.IsStart || list.IsEnd) && e != nil {
// 		list.Collection = append(list.Collection, e)
// 	}
// 	return nil
// }

// Epochs pulls list of epochs starting at the specified cursor.
func (db *MongoDbBridge) Epochs(cursor *string, count int32) (*types.EpochList, error) {
	// nothing to load?
	if count == 0 {
		return nil, fmt.Errorf("nothing to do, zero epochs requested")
	}

	// get the collection and context
	col := db.client.Database(db.dbName).Collection(colEpochs)

	// init the list
	list, err := db.epochListInit(col, cursor, count)
	if err != nil {
		db.log.Errorf("can not build epoch list; %s", err.Error())
		return nil, err
	}

	// load data if there are any
	if list.Total > 0 {
		err = db.epochListLoad(col, cursor, count, list)
		if err != nil {
			db.log.Errorf("can not load epoch list; %s", err.Error())
			return nil, err
		}

		// reverse on negative so new-er delegations will be on top
		if count < 0 {
			list.Reverse()
			count = -count
		}

		// cut the end?
		if len(list.Collection) > int(count) {
			list.Collection = list.Collection[:len(list.Collection)-1]
		}
	}
	return list, nil
}

// Epochs pulls a list of epochs starting at the specified cursor.
func (db *PostgreSQLBridge) Epochs(cursor *string, count int32) (*types.EpochList, error) {
	// Nothing to load?
	if count == 0 {
		return nil, fmt.Errorf("nothing to do, zero epochs requested")
	}

	// Initialize the list
	list, err := db.epochListInit(cursor, count)
	if err != nil {
		db.log.Errorf("cannot build epoch list; %s", err.Error())
		return nil, err
	}

	// Load data if there are any
	if list.Total > 0 {
		err = db.epochListLoad(cursor, count, list)
		if err != nil {
			db.log.Errorf("cannot load epoch list; %s", err.Error())
			return nil, err
		}

		// Reverse the list on negative count to show newer epochs first
		if count < 0 {
			list.Reverse()
			count = -count
		}

		// Trim the list to the requested count
		if len(list.Collection) > int(count) {
			list.Collection = list.Collection[:len(list.Collection)-1]
		}
	}

	return list, nil
}
