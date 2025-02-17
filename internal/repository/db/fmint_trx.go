// Package db implements bridge to persistent storage represented by Mongo database.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"ncogearthchain-api-graphql/internal/types"
	"strconv"

	//"github.com/jackc/pgx"
	"github.com/jackc/pgx"
)

// colFMintTransactions represents the name of the fMint transaction collection in database.
const colFMintTransactions = "fmint_trx"

// // initFMintTrxCollection initializes the fMint transaction list collection with
// // indexes and additional parameters needed by the app.
// func (db *MongoDbBridge) initFMintTrxCollection(col *mongo.Collection) {
// 	// prepare index models
// 	ix := make([]mongo.IndexModel, 0)

// 	// index specific elements
// 	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: types.FiFMintTransactionToken, Value: 1}}})
// 	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: types.FiFMintTransactionUser, Value: 1}}})
// 	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: types.FiFMintTransactionTimestamp, Value: -1}}})
// 	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: types.FiFMintTransactionOrdinal, Value: -1}}})

// 	// create indexes
// 	if _, err := col.Indexes().CreateMany(context.Background(), ix); err != nil {
// 		db.log.Panicf("can not create indexes for fMint trx collection; %s", err.Error())
// 	}

// 	// log we are done that
// 	db.log.Debugf("fMint trx collection initialized")
// }

// initFMintTrxTable initializes the fMint transaction table with indexes and additional parameters needed by the app.
func (db *PostgreSQLBridge) initFMintTrxTable() {
	// Prepare SQL queries to create the indexes
	indexQueries := []string{
		`CREATE INDEX IF NOT EXISTS idx_fmint_transaction_token ON erc20_transactions ("token_address")`,
		`CREATE INDEX IF NOT EXISTS idx_fmint_transaction_user ON erc20_transactions ("user_address")`,
		`CREATE INDEX IF NOT EXISTS idx_fmint_transaction_timestamp ON erc20_transactions ("timestamp" DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_fmint_transaction_ordinal ON erc20_transactions ("ordinal" DESC)`,
	}

	// Execute each query to create the indexes
	for _, query := range indexQueries {
		_, err := db.db.Exec(query)
		if err != nil {
			db.log.Panicf("can not create index for fMint trx table; %s", err.Error())
		}
	}

	// Log that the indexes are created
	db.log.Debugf("fMint trx table indexes initialized")
}

// // AddFMintTransaction stores an fMint transaction in the database if it doesn't exist.
// func (db *MongoDbBridge) AddFMintTransaction(trx *types.FMintTransaction) error {
// 	// get the collection for delegations
// 	col := db.client.Database(db.dbName).Collection(colFMintTransactions)

// 	// is it a new one?
// 	if db.isFMintTransactionKnown(col, trx) {
// 		return nil
// 	}

// 	// try to do the insert
// 	if _, err := col.InsertOne(context.Background(), trx); err != nil {
// 		db.log.Critical(err)
// 		return err
// 	}

// 	// make sure delegation collection is initialized
// 	if db.initFMintTrx != nil {
// 		db.initFMintTrx.Do(func() { db.initFMintTrxCollection(col); db.initErc20Trx = nil })
// 	}
// 	return nil
// }

// AddFMintTransaction stores an fMint transaction in the PostgreSQL database if it doesn't exist.
func (db *PostgreSQLBridge) AddFMintTransaction(trx *types.FMintTransaction) error {
	// Check if the fMint transaction already exists in the database
	if db.isFMintTransactionKnown(trx) {
		return nil
	}

	// Prepare the INSERT query with ON CONFLICT to avoid duplicates
	query := `
		INSERT INTO fmint_transactions (token_address, user_address, timestamp, ordinal, other_columns)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (token_address, user_address, timestamp) DO NOTHING
	`

	// Execute the insert query
	_, err := db.db.Exec(query, trx.TokenAddress, trx.UserAddress)
	if err != nil {
		db.log.Critical(err)
		return err
	}

	// Ensure the fMint transaction table is initialized (if necessary)
	if db.initFMintTrx != nil {
		db.initFMintTrx.Do(func() {
			db.initFMintTrxTable() // Initialize the table and indexes (if needed)
			db.initErc20Trx = nil
		})
	}
	return nil
}

// // isFMintTransactionKnown checks if the given delegation exists in the database.
// func (db *MongoDbBridge) isFMintTransactionKnown(col *mongo.Collection, trx *types.FMintTransaction) bool {
// 	// try to find the delegation in the database
// 	sr := col.FindOne(context.Background(), bson.D{
// 		{Key: types.FiFMintTransactionId, Value: trx.Pk()},
// 	}, options.FindOne().SetProjection(bson.D{
// 		{Key: types.FiFMintTransactionId, Value: true},
// 	}))

// 	// error on lookup?
// 	if sr.Err() != nil {
// 		// may be ErrNoDocuments, which we seek
// 		if sr.Err() == mongo.ErrNoDocuments {
// 			return false
// 		}
// 		// inform that we can not get the PK; should not happen
// 		db.log.Errorf("can not get existing fMint transaction pk; %s", sr.Err().Error())
// 		return false
// 	}
// 	return true
// }

// isFMintTransactionKnown checks if the given fMint transaction exists in the database.
func (db *PostgreSQLBridge) isFMintTransactionKnown(trx *types.FMintTransaction) bool {
	// Prepare the SQL query to check if the fMint transaction exists by its PK
	query := `
		SELECT 1 FROM fmint_transactions
		WHERE token_address = $1 AND user_address = $2 AND timestamp = $3
		LIMIT 1
	`

	// Execute the query with the provided transaction's unique identifiers (or other fields)
	var exists bool
	err := db.db.QueryRow(query, trx.TokenAddress, trx.UserAddress).Scan(&exists)

	// If an error occurred while querying, log and return false
	if err != nil {
		// If no rows found, the transaction doesn't exist (no need to log)
		if err == sql.ErrNoRows {
			return false
		}
		// For other errors, log the issue
		db.log.Errorf("can not check if fMint transaction exists; %s", err.Error())
		return false
	}

	// If we found the transaction, return true
	return exists
}

// // FMintTransactionCount calculates total number of fMint transactions in the database.
// func (db *MongoDbBridge) FMintTransactionCount() (uint64, error) {
// 	return db.EstimateCount(db.client.Database(db.dbName).Collection(colFMintTransactions))
// }

// FMintTransactionCount calculates the total number of fMint transactions in the database.
func (db *PostgreSQLBridge) FMintTransactionCount() (int64, error) {
	// Define the SQL query to count rows in the 'fmint_transactions' table
	query := "SELECT COUNT(*) FROM fmint_transactions"

	// Execute the query and scan the result into a variable
	var count int64
	err := db.db.QueryRow(query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get fMint transactions count: %w", err)
	}

	// Return the count as uint64
	return int64(count), nil
}

// // FMintTransactionCountFiltered calculates total number of sMint transactions
// // in the database for the given filter.
// func (db *MongoDbBridge) FMintTransactionCountFiltered(filter *bson.D) (uint64, error) {
// 	return db.CountFiltered(db.client.Database(db.dbName).Collection(colFMintTransactions), filter)
// }

// FMintTransactionCountFiltered calculates the total number of fMint transactions
// in the database for the given filter.
func (db *PostgreSQLBridge) FMintTransactionCountFiltered(filter map[string]interface{}) (uint64, error) {
	// Build the SQL query for counting filtered fMint transactions
	query := `SELECT COUNT(*) FROM fmint_transactions WHERE 1 = 1`

	// Prepare the arguments for the query
	var args []interface{}

	// Apply filter dynamically
	for key, value := range filter {
		query += " AND \"" + key + "\" = $" + strconv.Itoa(len(args)+1)
		args = append(args, value)
	}

	// Execute the query to get the total count
	var totalCount uint64
	err := db.db.QueryRow(query, args...).Scan(&totalCount)
	if err != nil {
		db.log.Errorf("error counting filtered fMint transactions; %s", err.Error())
		return 0, err
	}

	return totalCount, nil
}

// // FMintTransactions pulls list of fMint transactions starting at the specified cursor.
// func (db *MongoDbBridge) FMintTransactions(cursor *string, count int32, filter *bson.D) (*types.FMintTransactionList, error) {
// 	// nothing to load?
// 	if count == 0 {
// 		return nil, fmt.Errorf("nothing to do, zero fMint transactions requested")
// 	}

// 	// get the collection and context
// 	col := db.client.Database(db.dbName).Collection(colFMintTransactions)

// 	// init the list
// 	list, err := db.fMintTrxListInit(col, cursor, count, filter)
// 	if err != nil {
// 		db.log.Errorf("can not build fMint transaction list; %s", err.Error())
// 		return nil, err
// 	}

// 	// load data if there are any
// 	if list.Total > 0 {
// 		err = db.fMintTrxListLoad(col, cursor, count, list)
// 		if err != nil {
// 			db.log.Errorf("can not load fMint transaction list from database; %s", err.Error())
// 			return nil, err
// 		}

// 		// reverse on negative so new-er trx will be on top
// 		if count < 0 {
// 			list.Reverse()
// 		}
// 	}
// 	return list, nil
// }

// FMintTransactions retrieves a list of fMint transactions starting at the specified cursor using PostgreSQL.
func (db *PostgreSQLBridge) FMintTransactions(cursor *string, count int32, filter *string) (*types.FMintTransactionList, error) {
	// nothing to load?
	if count == 0 {
		return nil, fmt.Errorf("nothing to do, zero fMint transactions requested")
	}

	// Initialize the list using fMintTrxListInit
	list, err := db.fMintTrxListInit(cursor, count, filter)
	if err != nil {
		db.log.Errorf("failed to initialize fMint transaction list; %s", err.Error())
		return nil, err
	}

	// Load data if there are any transactions
	if list.Total > 0 {
		query := list.Query // Prebuilt query from fMintTrxListInit
		// args := list.Args   // Prebuilt arguments from fMintTrxListInit

		err = db.fMintTrxListLoad(&query, count, list)
		if err != nil {
			db.log.Errorf("failed to load fMint transaction list from database; %s", err.Error())
			return nil, err
		}

		// Reverse the list if count is negative
		if count < 0 {
			list.Reverse()
		}
	}

	return list, nil
}

// // fMintTrxListInit initializes list of fMint transactions based on provided cursor, count, and filter.
// func (db *MongoDbBridge) fMintTrxListInit(col *mongo.Collection, cursor *string, count int32, filter *bson.D) (*types.FMintTransactionList, error) {
// 	// make sure some filter is used
// 	if nil == filter {
// 		filter = &bson.D{}
// 	}

// 	// find how many transactions do we have in the database
// 	total, err := col.CountDocuments(context.Background(), *filter)
// 	if err != nil {
// 		db.log.Errorf("can not count fMint transactions")
// 		return nil, err
// 	}

// 	// make the list and notify the size of it
// 	db.log.Debugf("found %d filtered fmint transactions", total)
// 	list := types.FMintTransactionList{
// 		Collection: make([]*types.FMintTransaction, 0),
// 		Total:      uint64(total),
// 		First:      0,
// 		Last:       0,
// 		IsStart:    total == 0,
// 		IsEnd:      total == 0,
// 		Filter:     *filter,
// 	}

// 	// is the list non-empty? return the list with properly calculated range marks
// 	if 0 < total {
// 		return db.fMintTrxListCollectRangeMarks(col, &list, cursor, count)
// 	}
// 	// this is an empty list
// 	db.log.Debug("empty fMint trx list created")
// 	return &list, nil
// }

// fMintTrxListInit initializes the list of fMint transactions based on the provided cursor, count, and filter.
func (db *PostgreSQLBridge) fMintTrxListInit(cursor *string, count int32, filter *string) (*types.FMintTransactionList, error) {
	// Ensure a filter exists
	if filter == nil {
		defaultFilter := "TRUE" // Default filter to select all rows
		filter = &defaultFilter
	}

	// Build the base query for counting
	countQuery := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM fMintTransactions
		WHERE %s
	`, *filter)

	var total int64
	err := db.db.QueryRow(countQuery).Scan(&total)
	if err != nil {
		db.log.Errorf("failed to count fMint transactions: %s", err.Error())
		return nil, err
	}

	// Log the total count of filtered transactions
	db.log.Debugf("found %d filtered fMint transactions", total)

	// Initialize the transaction list
	list := types.FMintTransactionList{
		Collection:     make([]*types.FMintTransaction, 0),
		Total:          uint64(total),
		First:          0,
		Last:           0,
		IsStart:        total == 0,
		IsEnd:          total == 0,
		FilterPostgres: *filter,
	}

	// If the total is greater than zero, calculate range marks
	if total > 0 {
		return db.fMintTrxListCollectRangeMarks(&list, cursor, count)
	}

	// This is an empty list
	db.log.Debug("empty fMint transaction list created")
	return &list, nil
}

// // fMintTrxListCollectRangeMarks finds range marks of a list of fMint transactions with proper First/Last marks.
// func (db *MongoDbBridge) fMintTrxListCollectRangeMarks(col *mongo.Collection, list *types.FMintTransactionList, cursor *string, count int32) (*types.FMintTransactionList, error) {
// 	var err error

// 	// find out the cursor ordinal index
// 	if cursor == nil && count > 0 {
// 		// get the highest available pk
// 		list.First, err = db.fMintTrxListBorderPk(col,
// 			list.Filter,
// 			options.FindOne().SetSort(bson.D{{Key: types.FiFMintTransactionOrdinal, Value: -1}}))
// 		list.IsStart = true

// 	} else if cursor == nil && count < 0 {
// 		// get the lowest available pk
// 		list.First, err = db.fMintTrxListBorderPk(col,
// 			list.Filter,
// 			options.FindOne().SetSort(bson.D{{Key: types.FiFMintTransactionOrdinal, Value: 1}}))
// 		list.IsEnd = true

// 	} else if cursor != nil {
// 		// the cursor itself is the starting point
// 		list.First, err = db.fMintTrxListBorderPk(col,
// 			bson.D{{Key: types.FiFMintTransactionId, Value: *cursor}},
// 			options.FindOne())
// 	}

// 	// check the error
// 	if err != nil {
// 		db.log.Errorf("can not find the initial fMint trx")
// 		return nil, err
// 	}

// 	// inform what we are about to do
// 	db.log.Debugf("fMint transaction list initialized with ordinal %s", list.First)
// 	return list, nil
// }

// fMintTrxListCollectRangeMarks finds range marks of a list of fMint transactions with proper First/Last marks.
func (db *PostgreSQLBridge) fMintTrxListCollectRangeMarks(list *types.FMintTransactionList, cursor *string, count int32) (*types.FMintTransactionList, error) {
	var err error

	// Determine the range based on cursor and count
	if cursor == nil && count > 0 {
		// Get the highest available primary key (descending order)
		err = db.fMintTrxListBorderPk(
			list,
			list.FilterPostgres,
			"DESC",
		)
		list.IsStart = true

	} else if cursor == nil && count < 0 {
		// Get the lowest available primary key (ascending order)
		err = db.fMintTrxListBorderPk(
			list,
			list.FilterPostgres,
			"ASC",
		)
		list.IsEnd = true

	} else if cursor != nil {
		// The cursor itself is the starting point
		err = db.fMintTrxListBorderPk(
			list,
			fmt.Sprintf("%s AND id = $1", list.Filter),
			"",
			*cursor,
		)
	}

	// Check for errors
	if err != nil {
		db.log.Errorf("cannot find the initial fMint transaction: %s", err.Error())
		return nil, err
	}

	// Log initialization information
	db.log.Debugf("fMint transaction list initialized with ID %d", list.First)
	return list, nil
}

// // fMintTrxListBorderPk finds the top PK of the ERC20 transactions collection based on given filter and options.
// func (db *MongoDbBridge) fMintTrxListBorderPk(col *mongo.Collection, filter bson.D, opt *options.FindOneOptions) (uint64, error) {
// 	// prep container
// 	var row struct {
// 		Value uint64 `bson:"orx"`
// 	}

// 	// make sure we pull only what we need
// 	opt.SetProjection(bson.D{{Key: types.FiFMintTransactionOrdinal, Value: true}})

// 	// try to decode
// 	sr := col.FindOne(context.Background(), filter, opt)
// 	err := sr.Decode(&row)
// 	if err != nil {
// 		return 0, err
// 	}
// 	return row.Value, nil
// }

// fMintTrxListBorderPk retrieves the border primary key for fMint transactions.
func (db *PostgreSQLBridge) fMintTrxListBorderPk(list *types.FMintTransactionList, filter string, sortOrder string, args ...interface{}) error {
	// Build the query to fetch the border primary key
	query := fmt.Sprintf(`
		SELECT id
		FROM fMintTransactions
		WHERE %s
	`, filter)

	// Apply sorting for the border (highest or lowest)
	if sortOrder != "" {
		query += fmt.Sprintf(" ORDER BY id %s", sortOrder)
	}

	// Limit to a single row
	query += " LIMIT 1"

	// Execute the query
	var pk int64
	err := db.db.QueryRowContext(context.Background(), query, args...).Scan(&pk)
	if err != nil {
		if err == pgx.ErrNoRows {
			// No rows found, return nil without error
			return nil
		}
		db.log.Errorf("failed to retrieve border primary key: %s", err.Error())
		return err
	}

	// Update the appropriate field in the list
	if sortOrder == "DESC" || sortOrder == "" {
		list.First = uint64(pk)
	} else {
		list.Last = uint64(pk)
	}
	return nil
}

// // fMintTrxListFilter creates a filter for fMint transaction list loading.
// func (db *MongoDbBridge) fMintTrxListFilter(cursor *string, count int32, list *types.FMintTransactionList) *bson.D {
// 	// build an extended filter for the query; add PK (decoded cursor) to the original filter
// 	if cursor == nil {
// 		if count > 0 {
// 			list.Filter = append(list.Filter, bson.E{Key: types.FiFMintTransactionOrdinal, Value: bson.D{{Key: "$lte", Value: list.First}}})
// 		} else {
// 			list.Filter = append(list.Filter, bson.E{Key: types.FiFMintTransactionOrdinal, Value: bson.D{{Key: "$gte", Value: list.First}}})
// 		}
// 	} else {
// 		if count > 0 {
// 			list.Filter = append(list.Filter, bson.E{Key: types.FiFMintTransactionOrdinal, Value: bson.D{{Key: "$lt", Value: list.First}}})
// 		} else {
// 			list.Filter = append(list.Filter, bson.E{Key: types.FiFMintTransactionOrdinal, Value: bson.D{{Key: "$gt", Value: list.First}}})
// 		}
// 	}
// 	// return the new filter
// 	return &list.Filter
// }

// fMintTrxListFilter creates a SQL filter for fMint transaction list loading.
func (db *PostgreSQLBridge) fMintTrxListFilter(cursor *string, count int32, list *types.FMintTransactionList) (string, []interface{}) {
	// Base filter from the list
	filter := list.FilterPostgres
	args := []interface{}{}
	argIndex := 1 // For parameterized queries, starting at $1

	// Add the PK (cursor or ordinal) to the filter
	if cursor == nil {
		if count > 0 {
			filter += fmt.Sprintf(" AND ordinal <= $%d", argIndex)
			args = append(args, list.First)
		} else {
			filter += fmt.Sprintf(" AND ordinal >= $%d", argIndex)
			args = append(args, list.First)
		}
		argIndex++
	} else {
		if count > 0 {
			filter += fmt.Sprintf(" AND ordinal < $%d", argIndex)
			args = append(args, list.First)
		} else {
			filter += fmt.Sprintf(" AND ordinal > $%d", argIndex)
			args = append(args, list.First)
		}
		argIndex++
	}

	return filter, args
}

// // fMintTrxListOptions creates a filter options set for fMint transactions list search.
// func (db *MongoDbBridge) fMintTrxListOptions(count int32) *options.FindOptions {
// 	// prep options
// 	opt := options.Find()

// 	// how to sort results in the collection
// 	// from high (new) to low (old) by default; reversed if loading from bottom
// 	sd := -1
// 	if count < 0 {
// 		sd = 1
// 	}

// 	// sort with the direction we want
// 	opt.SetSort(bson.D{{Key: types.FiFMintTransactionOrdinal, Value: sd}})

// 	// prep the loading limit
// 	var limit = int64(count)
// 	if limit < 0 {
// 		limit = -limit
// 	}

// 	// apply the limit, try to get one more record, so we can detect list end
// 	opt.SetLimit(limit + 1)
// 	return opt
// }

// fMintTrxListOptions creates SQL ordering and limit clauses for fMint transactions list search.
func (db *PostgreSQLBridge) fMintTrxListOptions(count int32) (string, int64) {
	// Determine sort direction: descending (-1) or ascending (1)
	order := "DESC"
	if count < 0 {
		order = "ASC"
	}

	// Calculate the absolute limit (include +1 to detect list end)
	limit := int64(count)
	if limit < 0 {
		limit = -limit
	}
	limit += 1

	// Return the SQL ORDER BY clause and limit
	return order, limit
}

// // fMintTrxListLoad load the initialized list of fMint transactions from database.
// func (db *MongoDbBridge) fMintTrxListLoad(col *mongo.Collection, cursor *string, count int32, list *types.FMintTransactionList) (err error) {
// 	ctx := context.Background()

// 	// load the data
// 	ld, err := col.Find(ctx, db.fMintTrxListFilter(cursor, count, list), db.fMintTrxListOptions(count))
// 	if err != nil {
// 		db.log.Errorf("error loading fMint transactions list; %s", err.Error())
// 		return err
// 	}

// 	// close the cursor as we leave
// 	defer func() {
// 		err = ld.Close(ctx)
// 		if err != nil {
// 			db.log.Errorf("error closing fMint transactions list cursor; %s", err.Error())
// 		}
// 	}()

// 	// loop and load the list; we may not store the last value
// 	var trx *types.FMintTransaction
// 	for ld.Next(ctx) {
// 		// append a previous value to the list, if we have one
// 		if trx != nil {
// 			list.Collection = append(list.Collection, trx)
// 		}

// 		// try to decode the next row
// 		var row types.FMintTransaction
// 		if err = ld.Decode(&row); err != nil {
// 			db.log.Errorf("can not decode the fMint transaction list row; %s", err.Error())
// 			return err
// 		}

// 		// use this row as the next item
// 		trx = &row
// 	}

// 	// we should have all the items already; we may just need to check if a boundary was reached
// 	list.IsEnd = (cursor == nil && count < 0) || (count > 0 && int32(len(list.Collection)) < count)
// 	list.IsStart = (cursor == nil && count > 0) || (count < 0 && int32(len(list.Collection)) < -count)

// 	// add the last item as well if we hit the boundary
// 	if (list.IsStart || list.IsEnd) && trx != nil {
// 		list.Collection = append(list.Collection, trx)
// 	}
// 	return nil
// }

// fMintTrxListLoad loads the initialized list of fMint transactions from the database.
func (db *PostgreSQLBridge) fMintTrxListLoad(cursor *string, count int32, list *types.FMintTransactionList) error {
	ctx := context.Background()

	// Prepare the filter and arguments
	filter, args := db.fMintTrxListFilter(cursor, count, list)
	order, limit := db.fMintTrxListOptions(count)

	// Construct the query
	query := fmt.Sprintf(
		"SELECT id, ordinal, amount, timestamp FROM fMintTransactions WHERE %s ORDER BY ordinal %s LIMIT $%d",
		filter, order, len(args)+1,
	)
	args = append(args, limit)

	// Execute the query
	rows, err := db.db.QueryContext(ctx, query, args...)
	if err != nil {
		db.log.Errorf("error loading fMint transactions list; %s", err.Error())
		return err
	}
	defer rows.Close()

	// Loop and load the transactions into the list
	var trx *types.FMintTransaction
	for rows.Next() {
		// Append the previous transaction if present
		if trx != nil {
			list.Collection = append(list.Collection, trx)
		}

		// Decode the current row
		var row types.FMintTransaction
		if err := rows.Scan(&row.Amount, &row.TimeStamp); err != nil {
			db.log.Errorf("cannot decode the fMint transaction list row; %s", err.Error())
			return err
		}

		// Use this row as the next item
		trx = &row
	}

	// Handle potential error after looping
	if err := rows.Err(); err != nil {
		db.log.Errorf("error iterating fMint transactions list rows; %s", err.Error())
		return err
	}

	// Check if boundaries (start or end of the list) were reached
	list.IsEnd = (cursor == nil && count < 0) || (count > 0 && int32(len(list.Collection)) < count)
	list.IsStart = (cursor == nil && count > 0) || (count < 0 && int32(len(list.Collection)) < -count)

	// Append the last transaction if we hit a boundary
	if (list.IsStart || list.IsEnd) && trx != nil {
		list.Collection = append(list.Collection, trx)
	}

	return nil
}
