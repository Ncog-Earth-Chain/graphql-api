// Package db implements bridge to persistent storage represented by Mongo database.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"ncogearthchain-api-graphql/internal/types"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// colErcTransactions represents the name of the ERC20 transaction collection in database.
const colErcTransactions = "erc20trx"

// initErc20TrxCollection initializes the ERC20 transaction list collection with
// indexes and additional parameters needed by the app.
func (db *MongoDbBridge) initErc20TrxCollection(col *mongo.Collection) {
	// prepare index models
	ix := make([]mongo.IndexModel, 0)

	// index specific elements
	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: types.FiTokenTransactionToken, Value: 1}}})
	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: types.FiTokenTransactionSender, Value: 1}}})
	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: types.FiTokenTransactionRecipient, Value: 1}}})
	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: types.FiTokenTransactionOrdinal, Value: -1}}})
	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: types.FiTokenTransactionCallHash, Value: 1}}})

	// sender + ordinal index
	tox := "to_tok"
	ix = append(ix, mongo.IndexModel{
		Keys: bson.D{{Key: types.FiTokenTransactionRecipient, Value: 1}, {Key: types.FiTokenTransactionToken, Value: 1}},
		Options: &options.IndexOptions{
			Name: &tox,
		},
	})

	// create indexes
	if _, err := col.Indexes().CreateMany(context.Background(), ix); err != nil {
		db.log.Panicf("can not create indexes for ERC20 trx collection; %s", err.Error())
	}

	// log we are done that
	db.log.Debugf("ERC20 trx collection initialized")
}

func (db *PostgreSQLBridge) initErc20TrxCollection() error {
	// List of SQL commands to create indexes
	indexQueries := []string{
		`CREATE INDEX IF NOT EXISTS idx_token ON erc20_transactions (token);`,
		`CREATE INDEX IF NOT EXISTS idx_sender ON erc20_transactions (sender);`,
		`CREATE INDEX IF NOT EXISTS idx_recipient ON erc20_transactions (recipient);`,
		`CREATE INDEX IF NOT EXISTS idx_ordinal ON erc20_transactions (ordinal DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_call_hash ON erc20_transactions (call_hash);`,
		`CREATE INDEX IF NOT EXISTS idx_to_tok ON erc20_transactions (recipient, token);`, // Composite index
	}

	// Execute each index creation query
	for _, query := range indexQueries {
		_, err := db.db.Exec(query)
		if err != nil {
			db.log.Panicf("can not create index for ERC20 trx table; %s", err.Error())
			return err
		}
	}

	// Log success
	db.log.Debugf("ERC20 trx table initialized with indexes")
	return nil
}

// AddERC20Transaction stores an ERC20 transaction in the database if it doesn't exist.
func (db *MongoDbBridge) AddERC20Transaction(trx *types.TokenTransaction) error {
	// get the collection for delegations
	col := db.client.Database(db.dbName).Collection(colErcTransactions)

	// is it a new one?
	if db.isErcTransactionKnown(col, trx) {
		return nil
	}

	// try to do the insert
	if _, err := col.InsertOne(context.Background(), trx); err != nil {
		db.log.Critical(err)
		return err
	}

	// make sure delegation collection is initialized
	if db.initErc20Trx != nil {
		db.initErc20Trx.Do(func() { db.initErc20TrxCollection(col); db.initErc20Trx = nil })
	}
	return nil
}

func (db *PostgreSQLBridge) AddERC20Transaction(trx *types.TokenTransaction) error {
	// Check if the transaction already exists
	exists := db.isErcTransactionKnown(trx) // Only receiving the boolean value, not an error

	// If the transaction is already known, return early
	if exists {
		return nil
	}

	// Insert the transaction into the database
	query := `
		INSERT INTO erc20_transactions (sender, recipient)
		VALUES ($1, $2)
		ON CONFLICT (id) DO NOTHING;
	`
	_, err := db.db.Exec(
		query,
		trx.Sender,    // Sender address
		trx.Recipient, // Recipient address
	)
	if err != nil {
		db.log.Criticalf("failed to insert ERC20 transaction: %s", err)
		return err
	}

	// Initialize the ERC20 transactions table indexes, if required
	if db.initErc20Trx != nil {
		db.initErc20Trx.Do(func() {
			if err := db.initErc20TrxCollection(); err != nil {
				db.log.Criticalf("failed to initialize ERC20 transaction table: %s", err)
			}
			db.initErc20Trx = nil
		})
	}

	return nil
}

// isErcTransactionKnown checks if the given delegation exists in the database.
func (db *MongoDbBridge) isErcTransactionKnown(col *mongo.Collection, trx *types.TokenTransaction) bool {
	// try to find the delegation in the database
	sr := col.FindOne(context.Background(), bson.D{
		{Key: types.FiTokenTransactionPk, Value: trx.Pk()},
	}, options.FindOne().SetProjection(bson.D{
		{Key: types.FiTokenTransactionPk, Value: true},
	}))

	// error on lookup?
	if sr.Err() != nil {
		// may be ErrNoDocuments, which we seek
		if sr.Err() == mongo.ErrNoDocuments {
			return false
		}
		// inform that we can not get the PK; should not happen
		db.log.Errorf("can not get existing ERC transaction pk; %s", sr.Err().Error())
		return false
	}
	return true
}

// isErcTransactionKnown checks if the given ERC20 transaction exists in the database.
func (db *PostgreSQLBridge) isErcTransactionKnown(trx *types.TokenTransaction) bool {
	// Define the query to check for the transaction
	query := `SELECT EXISTS (SELECT 1 FROM erc20_transactions WHERE id = $1);`

	// Execute the query
	var exists bool
	err := db.db.QueryRow(query, trx.Pk()).Scan(&exists)
	if err != nil {
		// Log and return false if there’s an error (should not occur in normal operations)
		db.log.Errorf("failed to check if ERC20 transaction exists: %s", err)
		return false
	}

	return exists
}

// ErcTransactionCountFiltered calculates total number of ERC20 transactions
// in the database for the given filter.
// func (db *MongoDbBridge) ErcTransactionCountFiltered(filter *bson.D) (uint64, error) {
// 	return db.CountFiltered(db.client.Database(db.dbName).Collection(colErcTransactions), filter)
// }

// ErcTransactionCountFiltered calculates the total number of ERC20 transactions
// in the database for the given filter.
func (db *PostgreSQLBridge) ErcTransactionCountFiltered(filter map[string]interface{}) (uint64, error) {
	// Start constructing the base query
	query := `SELECT COUNT(*) FROM erc20_transactions WHERE 1=1`

	// Create a slice to hold the parameters for the query
	var params []interface{}
	paramIndex := 1

	// Add conditions based on the filter
	for key, value := range filter {
		query += fmt.Sprintf(" AND %s = $%d", key, paramIndex)
		params = append(params, value)
		paramIndex++
	}

	// Execute the query to count the rows
	var count uint64
	err := db.db.QueryRow(query, params...).Scan(&count)
	if err != nil {
		db.log.Errorf("failed to count filtered ERC20 transactions: %s", err)
		return 0, err
	}

	return count, nil
}

// // ErcTransactionCount calculates total number of ERC20 transactions in the database.
// func (db *MongoDbBridge) ErcTransactionCount() (uint64, error) {
// 	return db.EstimateCount(db.client.Database(db.dbName).Collection(colErcTransactions))
// }

// ErcTransactionCount calculates the total number of ERC20 transactions in the database.
func (db *PostgreSQLBridge) ErcTransactionCount() (int64, error) {
	// Define the SQL query to count rows in the 'erc_transactions' table
	query := "SELECT COUNT(*) FROM erc_transactions"

	// Execute the query and scan the result into a variable
	var count int64
	err := db.db.QueryRow(query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get ERC20 transactions count: %w", err)
	}

	// Return the count as uint64
	return int64(count), nil
}

// ercTrxListInit initializes list of ERC20 transactions based on provided cursor, count, and filter.
func (db *MongoDbBridge) ercTrxListInit(col *mongo.Collection, cursor *string, count int32, filter *bson.D) (*types.TokenTransactionList, error) {
	// make sure some filter is used
	if nil == filter {
		filter = &bson.D{}
	}

	// find how many transactions do we have in the database
	total, err := col.CountDocuments(context.Background(), *filter)
	if err != nil {
		db.log.Errorf("can not count ERC20 transactions")
		return nil, err
	}

	// make the list and notify the size of it
	db.log.Debugf("found %d filtered ERC20 transactions", total)
	list := types.TokenTransactionList{
		Collection: make([]*types.TokenTransaction, 0),
		Total:      uint64(total),
		First:      0,
		Last:       0,
		IsStart:    total == 0,
		IsEnd:      total == 0,
		Filter:     *filter,
	}

	// is the list non-empty? return the list with properly calculated range marks
	if 0 < total {
		return db.ercTrxListCollectRangeMarks(col, &list, cursor, count)
	}
	// this is an empty list
	db.log.Debug("empty erc trx list created")
	return &list, nil
}

// ercTrxListInit initializes a list of ERC20 transactions based on provided cursor, count, and filter.
func (db *PostgreSQLBridge) ercTrxListInit(cursor *string, count int32) (*types.TokenTransactionList, error) {

	// Prepare the SQL query for counting filtered transactions
	query := `SELECT COUNT(*) FROM erc20_transactions WHERE 1 = 1`

	// Apply the filters dynamically (if any)
	var args []interface{}
	// Example of adding dynamic filter conditions
	// If there's a filter applied, append it to the query (you can add more filters as needed)
	if cursor != nil {
		query += ` AND some_column = $1`
		args = append(args, *cursor)
	}

	// Execute the query to count the filtered transactions
	var total int64
	err := db.db.QueryRow(query, args...).Scan(&total)
	if err != nil {
		db.log.Errorf("failed to count filtered ERC20 transactions: %s", err)
		return nil, err
	}

	// Create the list and set the range marks
	list := types.TokenTransactionList{
		Collection: make([]*types.TokenTransaction, 0),
		Total:      uint64(total),
		First:      0,
		Last:       0,
		IsStart:    total == 0,
		IsEnd:      total == 0,
	}

	// If the list is non-empty, fetch the range of transactions
	if total > 0 {
		// Correct the argument order and types by passing the pointer to list
		//return db.ercTrxListCollectRangeMarks(cursor, count, &list)
	}

	// Empty list
	db.log.Debug("empty ERC20 transaction list created")
	return &list, nil
}

// ercTrxListCollectRangeMarks returns a list of ERC20 transactions with proper First/Last marks.
func (db *MongoDbBridge) ercTrxListCollectRangeMarks(col *mongo.Collection, list *types.TokenTransactionList, cursor *string, count int32) (*types.TokenTransactionList, error) {
	var err error

	// find out the cursor ordinal index
	if cursor == nil && count > 0 {
		// get the highest available pk
		list.First, err = db.ercTrxListBorderPk(col,
			list.Filter,
			options.FindOne().SetSort(bson.D{{Key: types.FiTokenTransactionOrdinal, Value: -1}}))
		list.IsStart = true

	} else if cursor == nil && count < 0 {
		// get the lowest available pk
		list.First, err = db.ercTrxListBorderPk(col,
			list.Filter,
			options.FindOne().SetSort(bson.D{{Key: types.FiTokenTransactionOrdinal, Value: 1}}))
		list.IsEnd = true

	} else if cursor != nil {
		// the cursor itself is the starting point
		list.First, err = db.ercTrxListBorderPk(col,
			bson.D{{Key: types.FiTokenTransactionPk, Value: *cursor}},
			options.FindOne())
	}

	// check the error
	if err != nil {
		db.log.Errorf("can not find the initial ERC20 trx")
		return nil, err
	}

	// inform what we are about to do
	db.log.Debugf("ERC20 transaction list initialized with ordinal %d", list.First)
	return list, nil
}

// / ercTrxListCollectRangeMarks returns a list of ERC20 transactions with proper First/Last marks.
func (db *PostgreSQLBridge) ercTrxListCollectRangeMarks(list *types.TokenTransactionList, cursor *string, count int32) (*types.TokenTransactionList, error) {
	var err error

	// Get the lowest available pk or cursor position based on count
	if cursor == nil && count > 0 {
		// Use ercTrxListBorderPk to get the highest available pk (first item)
		list.First, err = db.ercTrxListBorderPk("filter = $1", []interface{}{list.Filter})
		if err != nil {
			db.log.Errorf("cannot find the highest available pk for ERC20 trx: %s", err)
			return nil, err
		}
		list.IsStart = true

	} else if cursor == nil && count < 0 {
		// Use ercTrxListBorderPk to get the lowest available pk (last item)
		list.First, err = db.ercTrxListBorderPk("filter = $1", []interface{}{list.Filter})
		if err != nil {
			db.log.Errorf("cannot find the lowest available pk for ERC20 trx: %s", err)
			return nil, err
		}
		list.IsEnd = true

	} else if cursor != nil {
		// Use ercTrxListBorderPk to get the cursor pk (starting point)
		list.First, err = db.ercTrxListBorderPk("pk = $1", []interface{}{*cursor})
		if err != nil {
			db.log.Errorf("cannot find the cursor pk for ERC20 trx: %s", err)
			return nil, err
		}
	}

	// Inform what we are about to do
	db.log.Debugf("ERC20 transaction list initialized with ordinal %d", list.First)
	return list, nil
}

// ercTrxListBorderPk finds the top PK of the ERC20 transactions collection based on given filter and options.
func (db *MongoDbBridge) ercTrxListBorderPk(col *mongo.Collection, filter bson.D, opt *options.FindOneOptions) (uint64, error) {
	// prep container
	var row struct {
		Value uint64 `bson:"orx"`
	}

	// make sure we pull only what we need
	opt.SetProjection(bson.D{{Key: types.FiTokenTransactionOrdinal, Value: true}})

	// try to decode
	sr := col.FindOne(context.Background(), filter, opt)
	err := sr.Decode(&row)
	if err != nil {
		return 0, err
	}
	return row.Value, nil
}

// ercTrxListBorderPk finds the top PK of the ERC20 transactions collection based on given filter and options.
func (db *PostgreSQLBridge) ercTrxListBorderPk(filter string, args []interface{}) (uint64, error) {
	// Prepare the SQL query to fetch the top PK based on the filter
	query := `SELECT FiTokenTransactionOrdinal FROM erc20_transactions WHERE ` + filter + ` LIMIT 1`

	// Declare a variable to hold the result
	var value uint64

	// Execute the query
	err := db.db.QueryRow(query, args...).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			// No rows found, handle accordingly
			return 0, nil
		}
		// Return any other error
		return 0, err
	}

	// Return the value found
	return value, nil
}

// ercTrxListFilter creates a filter for ERC20 transaction list loading.
func (db *MongoDbBridge) ercTrxListFilter(cursor *string, count int32, list *types.TokenTransactionList) *bson.D {
	// build an extended filter for the query; add PK (decoded cursor) to the original filter
	if cursor == nil {
		if count > 0 {
			list.Filter = append(list.Filter, bson.E{Key: types.FiTokenTransactionOrdinal, Value: bson.D{{Key: "$lte", Value: list.First}}})
		} else {
			list.Filter = append(list.Filter, bson.E{Key: types.FiTokenTransactionOrdinal, Value: bson.D{{Key: "$gte", Value: list.First}}})
		}
	} else {
		if count > 0 {
			list.Filter = append(list.Filter, bson.E{Key: types.FiTokenTransactionOrdinal, Value: bson.D{{Key: "$lt", Value: list.First}}})
		} else {
			list.Filter = append(list.Filter, bson.E{Key: types.FiTokenTransactionOrdinal, Value: bson.D{{Key: "$gt", Value: list.First}}})
		}
	}
	// return the new filter
	return &list.Filter
}

// ercTrxListFilter creates a SQL WHERE clause and arguments for loading the ERC20 transaction list.
func (db *PostgreSQLBridge) ercTrxListFilter(cursor *string, count int32, list *types.TokenTransactionList) (string, []interface{}) {
	var filter string
	var args []interface{}

	// Start with the existing filter conditions from list.Filter (assume it's a map or similar structure)
	if list.Filter != nil {
		// Assuming list.Filter is a slice of key-value pairs, e.g., []struct{ Key string; Value interface{} }
		for _, condition := range list.Filter {
			filter += ` AND "` + condition.Key + `" = $` + strconv.Itoa(len(args)+1)
			args = append(args, condition.Value)
		}
	}

	// Add the PK filter to the existing conditions
	if cursor == nil {
		if count > 0 {
			filter += ` AND "FiTokenTransactionOrdinal" <= $` + strconv.Itoa(len(args)+1)
			args = append(args, list.First)
		} else {
			filter += ` AND "FiTokenTransactionOrdinal" >= $` + strconv.Itoa(len(args)+1)
			args = append(args, list.First)
		}
	} else {
		if count > 0 {
			filter += ` AND "FiTokenTransactionOrdinal" < $` + strconv.Itoa(len(args)+1)
			args = append(args, list.First)
		} else {
			filter += ` AND "FiTokenTransactionOrdinal" > $` + strconv.Itoa(len(args)+1)
			args = append(args, list.First)
		}
	}

	// Trim leading " AND " for the final filter string
	if len(filter) > 5 {
		filter = filter[5:]
	}

	// Return the SQL WHERE clause and the arguments
	return filter, args
}

// ercTrxListOptions creates a filter options set for ERC20 transactions list search.
func (db *MongoDbBridge) ercTrxListOptions(count int32) *options.FindOptions {
	// prep options
	opt := options.Find()

	// how to sort results in the collection
	// from high (new) to low (old) by default; reversed if loading from bottom
	sd := -1
	if count < 0 {
		sd = 1
	}

	// sort with the direction we want
	opt.SetSort(bson.D{{Key: types.FiTokenTransactionOrdinal, Value: sd}})

	// prep the loading limit
	var limit = int64(count)
	if limit < 0 {
		limit = -limit
	}

	// apply the limit, try to get one more record so we can detect list end
	opt.SetLimit(limit + 1)
	return opt
}

// ercTrxListOptions creates SQL clauses and arguments for ERC20 transactions list search.
func (db *PostgreSQLBridge) ercTrxListOptions(count int32) (string, string, int64) {
	// Determine sort direction: DESC for positive count, ASC for negative count
	sortDirection := "DESC"
	if count < 0 {
		sortDirection = "ASC"
	}

	// Determine the limit: always a positive value
	limit := int64(count)
	if limit < 0 {
		limit = -limit
	}

	// Add 1 to the limit to detect the end of the list
	limit += 1

	// Return the ORDER BY clause, LIMIT clause, and limit value
	orderByClause := `ORDER BY "FiTokenTransactionOrdinal" ` + sortDirection
	limitClause := `LIMIT $1`

	return orderByClause, limitClause, limit
}

// ercTrxListLoad load the initialized list of ERC20 transactions from database.
func (db *MongoDbBridge) ercTrxListLoad(col *mongo.Collection, cursor *string, count int32, list *types.TokenTransactionList) (err error) {
	// get the context for loader
	ctx := context.Background()

	// load the data
	ld, err := col.Find(ctx, db.ercTrxListFilter(cursor, count, list), db.ercTrxListOptions(count))
	if err != nil {
		db.log.Errorf("error loading ERC20 transactions list; %s", err.Error())
		return err
	}

	// close the cursor as we leave
	defer func() {
		err = ld.Close(ctx)
		if err != nil {
			db.log.Errorf("error closing ERC20 transactions list cursor; %s", err.Error())
		}
	}()

	// loop and load the list; we may not store the last value
	var trx *types.TokenTransaction
	for ld.Next(ctx) {
		// append a previous value to the list, if we have one
		if trx != nil {
			list.Collection = append(list.Collection, trx)
		}

		// try to decode the next row
		var row types.TokenTransaction
		if err = ld.Decode(&row); err != nil {
			db.log.Errorf("can not decode the ERC20 transaction list row; %s", err.Error())
			return err
		}

		// use this row as the next item
		trx = &row
	}

	// we should have all the items already; we may just need to check if a boundary was reached
	list.IsEnd = (cursor == nil && count < 0) || (count > 0 && int32(len(list.Collection)) < count)
	list.IsStart = (cursor == nil && count > 0) || (count < 0 && int32(len(list.Collection)) < -count)

	// add the last item as well if we hit the boundary
	if ((count < 0 && list.IsStart) || (count > 0 && list.IsEnd)) && trx != nil {
		list.Collection = append(list.Collection, trx)
	}
	return nil
}

// ercTrxListLoad loads the initialized list of ERC20 transactions from the PostgreSQL database.
func (db *PostgreSQLBridge) ercTrxListLoad(cursor *string, count int32, list *types.TokenTransactionList) (err error) {
	// Build the filter and options (SQL WHERE clause and parameters)
	filter, args := db.ercTrxListFilter(cursor, count, list)
	orderByClause, limitClause, limit := db.ercTrxListOptions(count)

	// Build the complete SQL query
	query := `
		SELECT "pk", "FiTokenTransactionOrdinal", "some_other_columns" 
		FROM "erc20_transactions"
		WHERE ` + filter + ` ` + orderByClause + ` ` + limitClause

	// Query the database
	rows, err := db.db.Query(query, append(args, limit)...)
	if err != nil {
		db.log.Errorf("error loading ERC20 transactions list; %s", err.Error())
		return err
	}
	defer rows.Close()

	// Loop and load the list; we may not store the last value
	var trx *types.TokenTransaction
	for rows.Next() {
		// Append the previous value to the list, if we have one
		if trx != nil {
			list.Collection = append(list.Collection, trx)
		}

		// Try to decode the next row into a TokenTransaction
		var row types.TokenTransaction
		if err := rows.Scan(&row); err != nil {
			db.log.Errorf("can not decode the ERC20 transaction list row; %s", err.Error())
			return err
		}

		// Use this row as the next item
		trx = &row
	}

	// We should have all the items already; we may just need to check if a boundary was reached
	list.IsEnd = (cursor == nil && count < 0) || (count > 0 && int32(len(list.Collection)) < count)
	list.IsStart = (cursor == nil && count > 0) || (count < 0 && int32(len(list.Collection)) < -count)

	// Add the last item as well if we hit the boundary
	if ((count < 0 && list.IsStart) || (count > 0 && list.IsEnd)) && trx != nil {
		list.Collection = append(list.Collection, trx)
	}

	return nil
}

// Erc20Transactions pulls list of ERC20 transactions starting at the specified cursor.
func (db *MongoDbBridge) Erc20Transactions(cursor *string, count int32, filter *bson.D) (*types.TokenTransactionList, error) {
	// nothing to load?
	if count == 0 {
		return nil, fmt.Errorf("nothing to do, zero erc transactions requested")
	}

	// get the collection and context
	col := db.client.Database(db.dbName).Collection(colErcTransactions)

	// init the list
	list, err := db.ercTrxListInit(col, cursor, count, filter)
	if err != nil {
		db.log.Errorf("can not build erc transaction list; %s", err.Error())
		return nil, err
	}

	// load data if there are any
	if list.Total > 0 {
		err = db.ercTrxListLoad(col, cursor, count, list)
		if err != nil {
			db.log.Errorf("can not load erc transaction list from database; %s", err.Error())
			return nil, err
		}

		// reverse on negative so new-er trx will be on top
		if count < 0 {
			list.Reverse()
		}
	}
	return list, nil
}

// Erc20Transactions pulls a list of ERC20 transactions starting at the specified cursor.
func (db *PostgreSQLBridge) Erc20Transactions(cursor *string, count int32, filter *types.TokenTransactionList) (*types.TokenTransactionList, error) {
	// Nothing to load?
	if count == 0 {
		return nil, fmt.Errorf("nothing to do, zero ERC transactions requested")
	}

	// Initialize the list
	list, err := db.ercTrxListInit(cursor, count)
	if err != nil {
		db.log.Errorf("cannot build ERC transaction list; %s", err.Error())
		return nil, err
	}

	// Load data if there are any
	if list.Total > 0 {
		err = db.ercTrxListLoad(cursor, count, list)
		if err != nil {
			db.log.Errorf("cannot load ERC transaction list from database; %s", err.Error())
			return nil, err
		}

		// Reverse on negative so newer transactions will be on top
		if count < 0 {
			list.Reverse()
		}
	}
	return list, nil
}

// Erc20Assets provides list of unique token addresses linked by transactions to the given owner address.
func (db *MongoDbBridge) Erc20Assets(owner common.Address, count int32) ([]common.Address, error) {
	// nothing to load?
	if count <= 1 {
		return nil, fmt.Errorf("nothing to do, zero erc assets requested")
	}

	// get the collection and context
	col := db.client.Database(db.dbName).Collection(colErcTransactions)
	refs, err := col.Distinct(context.Background(), types.FiTokenTransactionToken,
		bson.D{{Key: "to", Value: owner.String()}},
	)
	if err != nil {
		db.log.Errorf("can not pull assets for %s; %s", owner.String(), err.Error())
		return nil, err
	}

	// prep the output array
	res := make([]common.Address, len(refs))
	for i, a := range refs {
		res[i] = common.HexToAddress(a.(string))
	}
	return res, nil
}

// Erc20Assets provides a list of unique token addresses linked by transactions to the given owner address.
func (db *PostgreSQLBridge) Erc20Assets(owner common.Address, count int32) ([]common.Address, error) {
	// Nothing to load?
	if count <= 1 {
		return nil, fmt.Errorf("nothing to do, zero ERC assets requested")
	}

	// Prepare the SQL query to fetch distinct token addresses linked to the given owner address
	query := `
		SELECT DISTINCT "token_address" 
		FROM "erc20_transactions" 
		WHERE "to_address" = $1
		LIMIT $2
	`

	// Execute the query
	rows, err := db.db.Query(query, owner.String(), count)
	if err != nil {
		db.log.Errorf("can not pull assets for %s; %s", owner.String(), err.Error())
		return nil, err
	}
	defer rows.Close()

	// Prepare the output array for addresses
	var addresses []common.Address
	for rows.Next() {
		var tokenAddress string
		if err := rows.Scan(&tokenAddress); err != nil {
			db.log.Errorf("error scanning token address for %s; %s", owner.String(), err.Error())
			return nil, err
		}

		// Convert string to common.Address and append to the result array
		addresses = append(addresses, common.HexToAddress(tokenAddress))
	}

	// Check for any errors encountered while iterating the rows
	if err := rows.Err(); err != nil {
		db.log.Errorf("error iterating rows for assets; %s", err.Error())
		return nil, err
	}

	// Return the list of token addresses
	return addresses, nil
}

// TokenTransactionsByCall provides list of token transactions for the given blockchain transaction call.
func (db *MongoDbBridge) TokenTransactionsByCall(trxHash *common.Hash) ([]*types.TokenTransaction, error) {
	col := db.client.Database(db.dbName).Collection(colErcTransactions)

	// search for values
	ld, err := col.Find(
		context.Background(),
		bson.D{{Key: types.FiTokenTransactionCallHash, Value: trxHash.String()}},
		options.Find().SetSort(bson.D{{Key: types.FiTokenTransactionOrdinal, Value: -1}}),
	)

	defer db.closeCursor(ld)

	// loop and load the list; we may not store the last value
	list := make([]*types.TokenTransaction, 0)
	for ld.Next(context.Background()) {
		var row types.TokenTransaction
		if err = ld.Decode(&row); err != nil {
			db.log.Errorf("can not decode the token transaction; %s", err.Error())
			return nil, err
		}

		// use this row as the next item
		list = append(list, &row)
	}
	return list, nil
}

// TokenTransactionsByCall provides a list of token transactions for the given blockchain transaction call.
func (db *PostgreSQLBridge) TokenTransactionsByCall(trxHash *common.Hash) ([]*types.TokenTransaction, error) {
	// Prepare the SQL query to fetch token transactions based on the call hash
	query := `
		SELECT "token","amount" 
		FROM "erc20_transactions"
		WHERE "transaction_hash" = $1
		ORDER BY "ordinal" DESC
	`

	// Execute the query
	rows, err := db.db.Query(query, trxHash.String())
	if err != nil {
		db.log.Errorf("error executing query to fetch token transactions by call hash; %s", err.Error())
		return nil, err
	}
	defer rows.Close()

	// Prepare the result list
	var list []*types.TokenTransaction
	for rows.Next() {
		// Scan each row into a TokenTransaction struct
		var trx types.TokenTransaction
		if err := rows.Scan(&trx.TokenAddress, &trx.Amount); err != nil {
			db.log.Errorf("error scanning token transaction row; %s", err.Error())
			return nil, err
		}

		// Append the scanned transaction to the list
		list = append(list, &trx)
	}

	// Check for any errors encountered during the iteration
	if err := rows.Err(); err != nil {
		db.log.Errorf("error iterating over rows for token transactions; %s", err.Error())
		return nil, err
	}

	// Return the list of token transactions
	return list, nil
}
