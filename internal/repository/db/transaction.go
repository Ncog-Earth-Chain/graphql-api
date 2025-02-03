// Package db implements bridge to persistent storage represented by Mongo database.
package db

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	// coTransaction is the name of the off-chain database collection storing transaction details.
	coTransactions = "transaction"

	// fiTransactionPk is the name of the primary key field of the transaction collection.
	fiTransactionPk = "_id"

	// fiTransactionOrdinalIndex is the name of the transaction ordinal index in the blockchain field.
	// db.transaction.createIndex({_id:1,orx:-1},{unique:true})
	fiTransactionOrdinalIndex = "orx"

	// fiTransactionBlock is the name of the block number field of the transaction.
	fiTransactionBlock = "blk"

	// fiTransactionSender is the name of the address field of the sender account.
	// db.transaction.createIndex({from:1}).
	fiTransactionSender = "from"

	// fiTransactionRecipient is the name of the address field of the recipient account.
	// null for contract creation.
	// db.transaction.createIndex({to:1}).
	fiTransactionRecipient = "to"

	// fiTransactionValue is the name of the field of the transaction value.
	fiTransactionValue = "value"

	// fiTransactionTimeStamp is the name of the field of the transaction time stamp.
	fiTransactionTimeStamp = "stamp"
)

// initTransactionsCollection initializes the transaction collection with
// indexes and additional parameters needed by the app.
func (db *MongoDbBridge) initTransactionsCollection(col *mongo.Collection) {
	// prepare index models
	ix := make([]mongo.IndexModel, 0)

	// index ordinal key sorted from high to low since this is the way we usually list
	unique := true
	ix = append(ix, mongo.IndexModel{
		Keys: bson.D{{Key: fiTransactionOrdinalIndex, Value: -1}},
		Options: &options.IndexOptions{
			Unique: &unique,
		},
	})

	// index sender and recipient
	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: fiTransactionSender, Value: 1}}})
	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: fiTransactionRecipient, Value: 1}}})
	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: fiTransactionTimeStamp, Value: 1}}})

	// sender + ordinal index
	fox := "from_orx"
	ix = append(ix, mongo.IndexModel{
		Keys: bson.D{{Key: fiTransactionSender, Value: 1}, {Key: fiTransactionOrdinalIndex, Value: -1}},
		Options: &options.IndexOptions{
			Name:   &fox,
			Unique: &unique,
		},
	})

	// recipient + ordinal index
	rox := "to_orx"
	ix = append(ix, mongo.IndexModel{
		Keys: bson.D{{Key: fiTransactionRecipient, Value: 1}, {Key: fiTransactionOrdinalIndex, Value: -1}},
		Options: &options.IndexOptions{
			Name:   &rox,
			Unique: &unique,
		},
	})

	// create indexes
	if _, err := col.Indexes().CreateMany(context.Background(), ix); err != nil {
		db.log.Panicf("can not create indexes for transaction collection; %s", err.Error())
	}

	// log we are done that
	db.log.Debugf("transactions collection initialized")
}

// initTransactionsCollection initializes the transaction table with indexes needed by the app.
func (db *PostgreSQLBridge) initTransactionsTable() {
	// Define the index creation queries
	queries := []string{
		// Unique index on ordinal key (descending order)
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_transaction_ordinal_desc
         ON transactions (ordinal_index DESC)`,

		// Index on sender
		`CREATE INDEX IF NOT EXISTS idx_transaction_sender
         ON transactions (sender)`,

		// Index on recipient
		`CREATE INDEX IF NOT EXISTS idx_transaction_recipient
         ON transactions (recipient)`,

		// Index on timestamp
		`CREATE INDEX IF NOT EXISTS idx_transaction_timestamp
         ON transactions (timestamp)`,

		// Unique composite index on sender + ordinal index
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_from_orx
         ON transactions (sender, ordinal_index DESC)`,

		// Unique composite index on recipient + ordinal index
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_to_orx
         ON transactions (recipient, ordinal_index DESC)`,
	}

	// Execute each query
	for _, query := range queries {
		if _, err := db.db.Exec(query); err != nil {
			db.log.Panicf("cannot create index: %s", err.Error())
		}
	}

	// Log completion
	db.log.Debugf("transactions table initialized with indexes")
}

// shouldAddTransaction validates if the transaction should be added to the persistent storage.
func (db *MongoDbBridge) shouldAddTransaction(col *mongo.Collection, trx *types.Transaction) bool {
	// check if the transaction already exists
	exists, err := db.IsTransactionKnown(col, &trx.Hash)
	if err != nil {
		db.log.Critical(err)
		return false
	}

	// if the transaction already exists, we don't need to do anything here
	return !exists
}

func (db *PostgreSQLBridge) shouldAddTransaction(tx *sql.Tx, trx *types.Transaction) (bool, error) {
	// Check if the transaction already exists
	var exists bool
	query := `SELECT EXISTS (SELECT 1 FROM transactions WHERE hash = $1)`
	err := tx.QueryRow(query, trx.Hash.String()).Scan(&exists)
	if err != nil {
		db.log.Criticalf("error checking if transaction is known: %s", err.Error())
		return false, err
	}

	// Return whether the transaction should be added (i.e., it does not exist)
	return !exists, nil
}

// AddTransaction stores a transaction reference in connected persistent storage.
func (db *MongoDbBridge) AddTransaction(block *types.Block, trx *types.Transaction) error {
	// do we have all needed data?
	if block == nil || trx == nil {
		return fmt.Errorf("can not add empty transaction")
	}

	// get the collection for transactions
	col := db.client.Database(db.dbName).Collection(coTransactions)

	// if the transaction already exists, we don't need to add it
	// just make sure the transaction accounts were processed
	if !db.shouldAddTransaction(col, trx) {
		return db.UpdateTransaction(col, trx)
	}

	// try to do the insert
	if _, err := col.InsertOne(context.Background(), trx); err != nil {
		db.log.Critical(err)
		return err
	}

	// add transaction to the db
	db.log.Debugf("transaction %s added to database", trx.Hash.String())

	// make sure transactions collection is initialized
	if db.initTransactions != nil {
		db.initTransactions.Do(func() { db.initTransactionsCollection(col); db.initTransactions = nil })
	}

	return nil
}

// func BlockByNumber(blockNumber string) (*types.Block, error) {
// 	// Check if the input is already in hexadecimal format
// 	if !strings.HasPrefix(blockNumber, "0x") {
// 		// Convert decimal input to hexadecimal
// 		num, err := strconv.Atoi(blockNumber)
// 		if err != nil {
// 			return nil, fmt.Errorf("invalid block number: %v", err)
// 		}
// 		blockNumber = fmt.Sprintf("0x%x", num)
// 	}

// 	// Fetch the block from the database
// 	return fetchBlock(blockNumber)
// }

func (db *PostgreSQLBridge) AddTransaction(block *types.Block, trx *types.Transaction) error {
	if block == nil || trx == nil {
		db.log.Errorf("Cannot add empty transaction: block=%v, trx=%v", block, trx)
		return fmt.Errorf("cannot add empty transaction")
	}

	tx, err := db.db.Begin()
	if err != nil {
		db.log.Critical(err)
		return fmt.Errorf("failed to begin database transaction: %v", err)
	}
	defer tx.Rollback()

	shouldAdd, err := db.shouldAddTransaction(tx, trx)
	if err != nil {
		db.log.Critical(err)
		return fmt.Errorf("failed to check if transaction exists: %v", err)
	}

	if !shouldAdd {
		if err := db.UpdateTransaction(tx, trx); err != nil {
			db.log.Critical(err)
			return fmt.Errorf("failed to update transaction: %v", err)
		}
		return tx.Commit()
	}

	toAccount := ""
	if trx.To != nil {
		toAccount = trx.To.String()
	}
	inputData := hex.EncodeToString(trx.InputData)
	//timestamp := time.Unix(int64(trx.TimeStamp), 0)

	query := `
        INSERT INTO transactions (hash, from_account, to_account, value, gas, gas_price, block_number, block_hash, input, nonce)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
    `

	_, err = tx.Exec(
		query,
		trx.Hash.String(),
		trx.From.String(),
		toAccount,
		trx.Value.String(),
		trx.Gas,
		trx.GasPrice.String(),
		block.Number,
		block.Hash.String(),
		inputData,
		trx.Nonce,
		//timestamp,
	)
	if err != nil {
		db.log.Critical(err)
		return fmt.Errorf("failed to insert transaction: %v", err)
	}

	if err := tx.Commit(); err != nil {
		db.log.Critical(err)
		return fmt.Errorf("failed to commit database transaction: %v", err)
	}

	db.log.Debugf("transaction %s added to database", trx.Hash.String())
	if db.initTransactions != nil {
		db.initTransactions.Do(func() { db.initTransactionsTable(); db.initTransactions = nil })
	}

	return nil
}

// UpdateTransaction updates transaction data in the database collection.
func (db *MongoDbBridge) UpdateTransaction(col *mongo.Collection, trx *types.Transaction) error {
	// notify
	db.log.Debugf("updating transaction %s", trx.Hash.String())

	// try to update a delegation by replacing it in the database
	// we use address and validator ID to identify unique delegation
	er, err := col.UpdateOne(context.Background(), bson.D{
		{Key: fiTransactionPk, Value: trx.Hash.String()},
	}, bson.D{{Key: "$set", Value: bson.D{
		{Key: fiTransactionOrdinalIndex, Value: trx.Uid()},
		{Key: fiTransactionSender, Value: trx.From.String()},
		{Key: fiTransactionValue, Value: trx.Value.String()},
		{Key: fiTransactionTimeStamp, Value: trx.TimeStamp},
	}}}, new(options.UpdateOptions).SetUpsert(false))
	if err != nil {
		db.log.Critical(err)
		return err
	}

	// do we actually have the document
	if 0 == er.MatchedCount {
		return fmt.Errorf("can not update, the transaction not found in database")
	}
	return nil
}

// UpdateTransaction updates an existing transaction in the persistent storage.
func (db *PostgreSQLBridge) UpdateTransaction(tx *sql.Tx, trx *types.Transaction) error {
	query := `
		UPDATE transactions
		SET from_account = $2, to_account = $3, value = $4, gas = $5, gas_price = $6, block_number = $7, block_hash = $8, input = $9, nonce = $10
		WHERE hash = $1
	`
	_, err := tx.Exec(
		query,
		trx.Hash.String(),
		trx.From.String(),
		trx.To.String(),
		trx.Value.String(),
		trx.Gas,
		trx.GasPrice.String(),
		trx.BlockNumber,
		trx.BlockHash,
		trx.InputData,
		trx.Nonce,
	)
	return err
}

// IsTransactionKnown checks if a transaction document already exists in the PostgreSQL database.
func (db *PostgreSQLBridge) IsTransactionKnown(hash *common.Hash) (bool, error) {
	// Define the query to check for transaction existence
	query := `
        SELECT EXISTS (
            SELECT 1
            FROM transactions
            WHERE hash = $1
        )
    `

	var exists bool

	// Execute the query
	err := db.db.QueryRow(query, hash.Hex()).Scan(&exists)
	if err != nil {
		db.log.Errorf("failed to check transaction existence: %s", err.Error())
		return false, err
	}

	// Return whether the transaction exists
	if !exists {
		db.log.Debugf("transaction %s not found in database", hash.Hex())
	}
	return exists, nil
}

// IsTransactionKnown checks if a transaction document already exists in the database.
func (db *MongoDbBridge) IsTransactionKnown(col *mongo.Collection, hash *common.Hash) (bool, error) {
	// try to find the transaction in the database (it may already exist)
	sr := col.FindOne(context.Background(), bson.D{
		{Key: fiTransactionPk, Value: hash.String()},
	}, options.FindOne().SetProjection(bson.D{
		{Key: fiTransactionPk, Value: true},
	}))

	// error on lookup?
	if sr.Err() != nil {
		// may be ErrNoDocuments, which we seek
		if sr.Err() == mongo.ErrNoDocuments {
			// add transaction to the db
			db.log.Debugf("transaction %s not found in database", hash.String())
			return false, nil
		}

		// log the error of the lookup
		db.log.Error("can not get existing transaction pk")
		return false, sr.Err()
	}

	// add transaction to the db
	return true, nil
}

// initTrxList initializes list of transactions based on provided cursor and count.
func (db *MongoDbBridge) initTrxList(col *mongo.Collection, cursor *string, count int32, filter *bson.D) (*types.TransactionList, error) {
	// make sure some filter is used
	if nil == filter {
		filter = &bson.D{}
	}

	// find how many transactions do we have in the database
	total, err := db.listDocumentsCount(col, filter)
	if err != nil {
		db.log.Errorf("can not count transactions")
		return nil, err
	}

	// make the list and notify the size of it
	db.log.Debugf("found %d filtered transactions", total)
	list := types.TransactionList{
		Collection: make([]*types.Transaction, 0),
		Total:      uint64(total),
		First:      0,
		Last:       0,
		IsStart:    total == 0,
		IsEnd:      total == 0,
		//Filter:     *filter,
	}

	// is the list non-empty? return the list with properly calculated range marks
	if 0 < total {
		return db.trxListWithRangeMarks(col, &list, cursor, count, filter)
	}

	// this is an empty list
	db.log.Debug("empty transaction list created")
	return &list, nil
}

// initTrxList initializes a list of transactions based on the provided cursor and count.
func (db *PostgreSQLBridge) initTrxList(cursor *string, count int32, filter string, args ...interface{}) (*types.PostTransactionList, error) {
	// Count the total number of transactions matching the filter
	var total int64
	countQuery := `SELECT COUNT(*) FROM transactions WHERE ` + filter
	err := db.db.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		db.log.Errorf("cannot count transactions: %s", err.Error())
		return nil, err
	}

	// Create the transaction list
	db.log.Debugf("found %d filtered transactions", total)
	list := &types.PostTransactionList{
		Collection: make([]*types.Transaction, 0),
		Total:      uint64(total),
		First:      0,
		Last:       0,
		IsStart:    total == 0,
		IsEnd:      total == 0,
		Filter:     make(map[string]interface{}), // Initialize filter as an empty map
	}

	// If there are transactions, calculate range marks and load them
	if total > 0 {
		updatedList, err := db.trxListWithRangeMarks(list, cursor, count, filter, args...)
		if err != nil {
			return nil, err
		}
		return updatedList, nil
	}

	// If the list is empty, return the empty list
	db.log.Debug("empty transaction list created")
	return list, nil
}

// trxListWithRangeMarks returns the transaction list with proper First/Last marks of the transaction range.
func (db *MongoDbBridge) trxListWithRangeMarks(
	col *mongo.Collection,
	list *types.TransactionList,
	cursor *string,
	count int32,
	filter *bson.D,
) (*types.TransactionList, error) {
	var err error

	// find out the cursor ordinal index
	if cursor == nil && count > 0 {
		// get the highest available ordinal index (top transaction)
		list.First, err = db.findBorderOrdinalIndex(col,
			*filter,
			options.FindOne().SetSort(bson.D{{Key: fiTransactionOrdinalIndex, Value: -1}}))
		list.IsStart = true

	} else if cursor == nil && count < 0 {
		// get the lowest available ordinal index (top transaction)
		list.First, err = db.findBorderOrdinalIndex(col,
			*filter,
			options.FindOne().SetSort(bson.D{{Key: fiTransactionOrdinalIndex, Value: 1}}))
		list.IsEnd = true

	} else if cursor != nil {
		// get the highest available ordinal index (top transaction)
		list.First, err = db.findBorderOrdinalIndex(col,
			bson.D{{Key: fiTransactionPk, Value: *cursor}},
			options.FindOne())
	}

	// check the error
	if err != nil {
		db.log.Errorf("can not find the initial transactions")
		return nil, err
	}

	// inform what we are about to do
	db.log.Debugf("transaction list initialized with ordinal index %d", list.First)
	return list, nil
}

// trxListWithRangeMarks loads a list of transactions with proper range marks.
func (db *PostgreSQLBridge) trxListWithRangeMarks(list *types.PostTransactionList, cursor *string, count int32, filter string, args ...interface{}) (*types.PostTransactionList, error) {
	// Define the sorting direction
	sortDirection := "ASC"
	if count < 0 {
		sortDirection = "DESC"
		count = -count
	}

	// Add range filtering based on the cursor
	if cursor != nil {
		filter += ` AND hash >= $` + fmt.Sprintf("%d", len(args)+1)
		args = append(args, *cursor)
	}

	// Query to load the transactions
	query := fmt.Sprintf(`
        SELECT hash, block_hash, block_number, timestamp, from_account, to_account,
               value, gas, gas_used, cumulative_gas_used, gas_price, nonce,
               contract_address, trx_index, input_data, status
        FROM transactions
        WHERE %s
        ORDER BY block_number %s, trx_index %s
        LIMIT $%d
    `, filter, sortDirection, sortDirection, len(args)+1)

	args = append(args, count)

	// Execute the query
	rows, err := db.db.Query(query, args...)
	if err != nil {
		db.log.Errorf("failed to load transactions: %s", err.Error())
		return nil, err
	}
	defer rows.Close()

	// Populate the transaction list
	for rows.Next() {
		var trx types.Transaction
		err := rows.Scan(
			&trx.Hash,
			&trx.BlockHash,
			&trx.BlockNumber,
			&trx.TimeStamp,
			&trx.From,
			&trx.To,
			&trx.Value,
			&trx.Gas,
			&trx.GasUsed,
			&trx.CumulativeGasUsed,
			&trx.GasPrice,
			&trx.Nonce,
			&trx.ContractAddress,
			&trx.TrxIndex,
			&trx.InputData,
			&trx.Status,
		)
		if err != nil {
			db.log.Errorf("failed to scan transaction: %s", err.Error())
			return nil, err
		}
		list.Collection = append(list.Collection, &trx)
	}

	return list, nil
}

// findBorderOrdinalIndex finds the highest, or lowest ordinal index in the collection.
// For negative sort it will return highest and for positive sort it will return lowest available value.
func (db *MongoDbBridge) findBorderOrdinalIndex(col *mongo.Collection, filter bson.D, opt *options.FindOneOptions) (uint64, error) {
	// prep container
	var row struct {
		Value uint64 `bson:"orx"`
	}

	// make sure we pull only what we need
	opt.SetProjection(bson.D{{Key: "orx", Value: true}})
	sr := col.FindOne(context.Background(), filter, opt)

	// try to decode
	err := sr.Decode(&row)
	if err != nil {
		return 0, err
	}

	return row.Value, nil
}

// findBorderOrdinalIndex finds the highest or lowest ordinal index in the transactions table.
// For descending sort, it returns the highest value; for ascending sort, it returns the lowest.
func (db *PostgreSQLBridge) findBorderOrdinalIndex(filter string, sortDirection string, args ...interface{}) (uint64, error) {
	// Construct the query to get the border ordinal index
	query := `
        SELECT ordinal_index
        FROM transactions
        WHERE ` + filter + `
        ORDER BY ordinal_index ` + sortDirection + `
        LIMIT 1
    `

	// Execute the query
	var value uint64
	err := db.db.QueryRow(query, args...).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			// No rows found, return 0 as default
			return 0, nil
		}
		db.log.Errorf("failed to find border ordinal index: %s", err.Error())
		return 0, err
	}

	return value, nil
}

// txListFilter creates a filter for transaction list search.
// func (db *MongoDbBridge) txListFilter(cursor *string, count int32, list *types.TransactionList) *bson.D {
// 	// inform what we are about to do
// 	db.log.Debugf("transaction filter starts from index %d", list.First)

// 	// build the filter query
// 	if cursor == nil {
// 		if count > 0 {
// 			list.Filter = append(list.Filter, bson.E{Key: fiTransactionOrdinalIndex, Value: bson.D{{Key: "$lte", Value: list.First}}})
// 		} else {
// 			list.Filter = append(list.Filter, bson.E{Key: fiTransactionOrdinalIndex, Value: bson.D{{Key: "$gte", Value: list.First}}})
// 		}
// 	} else {
// 		if count > 0 {
// 			list.Filter = append(list.Filter, bson.E{Key: fiTransactionOrdinalIndex, Value: bson.D{{Key: "$lt", Value: list.First}}})
// 		} else {
// 			list.Filter = append(list.Filter, bson.E{Key: fiTransactionOrdinalIndex, Value: bson.D{{Key: "$gt", Value: list.First}}})
// 		}
// 	}

// 	// log the filter
// 	return &list.Filter
// }

// txListFilter creates a filter string and arguments for transaction list search in PostgreSQL.
func (db *PostgreSQLBridge) txListFilter(cursor *string, count int32, list *types.PostTransactionList) (string, []interface{}) {
	// Log the starting point of the filter
	db.log.Debugf("transaction filter starts from index %d", list.First)

	// Start with the base filter
	filter := "1=1" // Default filter to match all rows
	args := make([]interface{}, 0)

	// Build the filter query based on the cursor and count
	if cursor == nil {
		if count > 0 {
			filter += " AND ordinal_index <= $1"
			args = append(args, list.First)
		} else {
			filter += " AND ordinal_index >= $1"
			args = append(args, list.First)
		}
	} else {
		if count > 0 {
			filter += " AND ordinal_index < $1"
			args = append(args, list.First)
		} else {
			filter += " AND ordinal_index > $1"
			args = append(args, list.First)
		}
	}

	// Log the filter for debugging purposes
	db.log.Debugf("filter: %s, args: %v", filter, args)

	return filter, args
}

// txListOptions creates a filter options set for transactions list search.
func (db *MongoDbBridge) txListOptions(count int32) *options.FindOptions {
	// prep options
	opt := options.Find()

	// how to sort results in the collection
	if count > 0 {
		// from high (new) to low (old)
		opt.SetSort(bson.D{{Key: fiTransactionOrdinalIndex, Value: -1}})
	} else {
		// from low (old) to high (new)
		opt.SetSort(bson.D{{Key: fiTransactionOrdinalIndex, Value: 1}})
	}

	// prep the loading limit
	var limit = int64(count)
	if limit < 0 {
		limit = -limit
	}

	// apply the limit, try to get one more transaction
	// so we can detect list end
	opt.SetLimit(limit + 1)
	return opt
}

// txListOptions creates a sorting and limit clause for transactions list search in PostgreSQL.
func (db *PostgreSQLBridge) txListOptions(count int32) (string, int64) {
	// Determine the sorting direction
	sortDirection := "DESC"
	if count < 0 {
		sortDirection = "ASC"
	}

	// Calculate the limit, ensuring it is positive
	limit := int64(count)
	if limit < 0 {
		limit = -limit
	}

	// Return the ORDER BY clause and the limit
	return fmt.Sprintf("ORDER BY ordinal_index %s", sortDirection), limit + 1
}

// txListLoad load the initialized list from database
// func (db *MongoDbBridge) txListLoad(col *mongo.Collection, cursor *string, count int32, list *types.TransactionList) error {
// 	// get the context for loader
// 	ctx := context.Background()

// 	// load the data
// 	ld, err := col.Find(ctx, db.txListFilter(cursor, count, list), db.txListOptions(count))
// 	if err != nil {
// 		db.log.Errorf("error loading transactions list; %s", err.Error())
// 		return err
// 	}

// 	defer db.closeCursor(ld)

// 	// loop and load
// 	var trx *types.Transaction
// 	for ld.Next(ctx) {
// 		// process the last found hash
// 		if trx != nil {
// 			list.Collection = append(list.Collection, trx)
// 		}

// 		// try to decode the next row
// 		var row types.Transaction
// 		if err := ld.Decode(&row); err != nil {
// 			db.log.Errorf("can not decode the list row; %s", err.Error())
// 			return err
// 		}

// 		// we have one
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

// txListLoad loads the initialized list of transactions from the PostgreSQL database.
func (db *PostgreSQLBridge) txListLoad(cursor *string, count int32, list *types.PostTransactionList, filter string, args ...interface{}) error {
	// Determine sorting and limit
	orderBy, limit := db.txListOptions(count)

	// Add range filtering based on cursor
	if cursor != nil {
		filter += ` AND ordinal_index >= $` + fmt.Sprintf("%d", len(args)+1)
		args = append(args, *cursor)
	}

	// Construct the query
	query := fmt.Sprintf(`
        SELECT hash, block_hash, block_number, timestamp, from_account, to_account,
               value, gas, gas_used, cumulative_gas_used, gas_price, nonce,
               contract_address, trx_index, input_data, status
        FROM transactions
        WHERE %s
        %s
        LIMIT $%d
    `, filter, orderBy, len(args)+1)

	args = append(args, limit)

	// Execute the query
	rows, err := db.db.Query(query, args...)
	if err != nil {
		db.log.Errorf("error loading transactions list; %s", err.Error())
		return err
	}
	defer rows.Close()

	// Loop and load transactions
	var trx *types.Transaction
	for rows.Next() {
		// Process the previously found transaction
		if trx != nil {
			list.Collection = append(list.Collection, trx)
		}

		// Decode the current row
		var row types.Transaction
		err := rows.Scan(
			&row.Hash,
			&row.BlockHash,
			&row.BlockNumber,
			&row.TimeStamp,
			&row.From,
			&row.To,
			&row.Value,
			&row.Gas,
			&row.GasUsed,
			&row.CumulativeGasUsed,
			&row.GasPrice,
			&row.Nonce,
			&row.ContractAddress,
			&row.TrxIndex,
			&row.InputData,
			&row.Status,
		)
		if err != nil {
			db.log.Errorf("failed to scan transaction: %s", err.Error())
			return err
		}

		trx = &row
	}

	// Check if a boundary was reached
	list.IsEnd = (cursor == nil && count < 0) || (count > 0 && int32(len(list.Collection)) < count)
	list.IsStart = (cursor == nil && count > 0) || (count < 0 && int32(len(list.Collection)) < -count)

	// Add the last transaction if it reaches the boundary
	if (list.IsStart || list.IsEnd) && trx != nil {
		list.Collection = append(list.Collection, trx)
	}

	return nil
}

// TransactionsCount returns the number of transactions stored in the database.
func (db *MongoDbBridge) TransactionsCount() (uint64, error) {
	return db.EstimateCount(db.client.Database(db.dbName).Collection(coTransactions))
}

func (db *PostgreSQLBridge) TransactionsCount() (uint64, error) {
	var count uint64
	query := "SELECT COUNT(*) FROM transactions"
	err := db.db.QueryRow(query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count rows in transactions table: %w", err)
	}
	return count, nil
}

// Transactions pulls list of transaction hashes starting on the specified cursor.
// func (db *MongoDbBridge) Transactions(cursor *string, count int32, filter *bson.D) (*types.TransactionList, error) {
// 	// nothing to load?
// 	if count == 0 {
// 		return nil, fmt.Errorf("nothing to do, zero transactions requested")
// 	}

// 	// get the collection and context
// 	col := db.client.Database(db.dbName).Collection(coTransactions)

// 	// init the list
// 	list, err := db.initTrxList(col, cursor, count, filter)
// 	if err != nil {
// 		db.log.Errorf("can not build transactions list; %s", err.Error())
// 		return nil, err
// 	}

// 	// load data if there are any
// 	if list.Total > 0 {
// 		err = db.txListLoad(col, cursor, count, list)
// 		if err != nil {
// 			db.log.Errorf("can not load transactions list from database; %s", err.Error())
// 			return nil, err
// 		}

// 		// reverse on negative so new-er transaction will be on top
// 		if count < 0 {
// 			list.Reverse()
// 			count = -count
// 		}

// 		// cut the end?
// 		if len(list.Collection) > int(count) {
// 			list.Collection = list.Collection[:len(list.Collection)-1]
// 		}
// 	}

// 	return list, nil
// }

// Transactions pulls a list of transactions starting at the specified cursor.
func (db *PostgreSQLBridge) Transactions(cursor *string, count int32, filter string, args ...interface{}) (*types.PostTransactionList, error) {
	// Nothing to load?
	if count == 0 {
		return nil, fmt.Errorf("nothing to do, zero transactions requested")
	}

	// Initialize the list
	list, err := db.initTrxList(cursor, count, filter, args...)
	if err != nil {
		db.log.Errorf("cannot build transactions list; %s", err.Error())
		return nil, err
	}

	// Load data if there are any transactions
	if list.Total > 0 {
		err = db.txListLoad(cursor, count, list, filter, args...)
		if err != nil {
			db.log.Errorf("cannot load transactions list from database; %s", err.Error())
			return nil, err
		}

		// Reverse the list if count is negative to show newer transactions on top
		if count < 0 {
			list.Reverse()
			count = -count
		}

		// Trim the list if it exceeds the requested count
		if len(list.Collection) > int(count) {
			list.Collection = list.Collection[:len(list.Collection)-1]
		}
	}

	return list, nil
}
