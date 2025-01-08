// Package db implements bridge to persistent storage represented by Mongo database.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"ncogearthchain-api-graphql/internal/types"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	// coContract is the name of the off-chain database collection storing smart contract details.
	coContract = "contract"

	// fiContractPk is the name of the primary key field of the contract collection.
	fiContractPk = "_id"

	// fiContractOrdinalIndex is the name of the contract ordinal index in the blockchain.
	// db.contract.createIndex({_id:1,orx:-1},{unique:true})
	fiContractOrdinalIndex = "orx"

	// fiContractSourceValidated is the name of the contract source code
	// validation timestamp field.
	fiContractSourceValidated = "val"
)

type ContractList struct {
	First   []types.Contract // <-- Change this to a slice of contracts
	Total   uint64
	IsStart bool
	IsEnd   bool
}

// initContractsCollection initializes the contracts collection with
// indexes and additional parameters needed by the app.
func (db *MongoDbBridge) initContractsCollection(col *mongo.Collection) {
	// prepare index models
	ix := make([]mongo.IndexModel, 0)

	// index ordinal key along with the primary key
	unique := true
	ix = append(ix, mongo.IndexModel{
		Keys: bson.D{{Key: fiContractPk, Value: 1}, {Key: fiContractOrdinalIndex, Value: -1}},
		Options: &options.IndexOptions{
			Unique: &unique,
		},
	})

	// create indexes
	if _, err := col.Indexes().CreateMany(context.Background(), ix); err != nil {
		db.log.Panicf("can not create indexes for contracts collection; %s", err.Error())
	}

	// log we done that
	db.log.Debugf("contracts collection initialized")
}

// initContractsCollection initializes the contracts table with
// indexes and additional parameters needed by the app.
func (db *PostgreSQLBridge) initContractsCollection() {
	// Prepare SQL query for creating indexes
	// First, we need to create an index on (contract primary key, contract ordinal index)
	query := `
		CREATE INDEX IF NOT EXISTS idx_contract_pk_ordinal
		ON contracts (contract_pk, contract_ordinal_index DESC);
	`

	// Execute the query to create the index
	_, err := db.db.Exec(query)
	if err != nil {
		db.log.Panicf("can not create indexes for contracts table; %s", err.Error())
	}

	// Log that the indexes were created
	db.log.Debugf("contracts table initialized")
}

// AddContract stores a smart contract reference in connected persistent storage.
func (db *MongoDbBridge) AddContract(sc *types.Contract) error {
	// do we have all needed data?
	if sc == nil {
		return fmt.Errorf("can not add empty contract")
	}

	// get the collection for contracts
	col := db.client.Database(db.dbName).Collection(coContract)

	// check if the contract already exists
	exists, err := db.isContractKnown(col, &sc.Address)
	if err != nil {
		db.log.Critical(err)
		return err
	}

	// if the contract already exists, we update it to match the new content
	if exists {
		db.log.Debugf("contract %s known, updating", sc.Address.String())
		return db.UpdateContract(sc)
	}

	// try to do the insert
	if _, err = col.InsertOne(context.Background(), sc); err != nil {
		db.log.Critical(err)
		return err
	}

	// make sure contracts collection is initialized
	if db.initContracts != nil {
		db.initContracts.Do(func() { db.initContractsCollection(col); db.initContracts = nil })
	}

	db.log.Debugf("added smart contract at %s", sc.Address.String())
	return nil
}

// AddContract stores a smart contract reference in the connected PostgreSQL database.
func (db *PostgreSQLBridge) AddContract(sc *types.Contract) error {
	// Do we have all needed data?
	if sc == nil {
		return fmt.Errorf("can not add empty contract")
	}

	// Check if the contract already exists in the database
	exists, err := db.isContractKnown(&sc.Address)
	if err != nil {
		db.log.Critical(err)
		return err
	}

	// If the contract already exists, update it
	if exists {
		db.log.Debugf("contract %s known, updating", sc.Address.String())
		return db.UpdateContract(sc)
	}

	// Insert the new contract into the database
	query := `
		INSERT INTO contracts (address, other_column1, other_column2) 
		VALUES ($1, $2, $3)
		ON CONFLICT (address) 
		DO UPDATE SET address = EXCLUDED.address;` // Replace with actual column names

	_, err = db.db.Exec(query, sc.Address) // Adjust column values
	if err != nil {
		db.log.Critical(err)
		return err
	}

	// Optionally initialize the contracts table (e.g., create indexes)
	if db.initContracts != nil {
		db.initContracts.Do(func() { db.initContractsCollection(); db.initContracts = nil })
	}

	db.log.Debugf("added smart contract at %s", sc.Address.String())
	return nil
}

// UpdateContract updates smart contract information in database to reflect
// new validation or similar changes passed from repository.
func (db *MongoDbBridge) UpdateContract(sc *types.Contract) error {
	// complain about missing contract data
	if sc == nil {
		db.log.Criticalf("can not update empty contract")
		return fmt.Errorf("no contract given to update")
	}

	// get the collection for contracts
	col := db.client.Database(db.dbName).Collection(coContract)

	// update the contract details
	if _, err := col.UpdateOne(context.Background(),
		bson.D{{Key: fiContractPk, Value: sc.Address.String()}},
		bson.D{{Key: "$set", Value: sc}}); err != nil {
		// log the issue
		db.log.Errorf("can not update contract details at %s; %s", sc.Address.String(), err.Error())
		return err
	}

	return nil
}

// UpdateContract updates smart contract information in PostgreSQL database to reflect
// new validation or similar changes passed from the repository.
func (db *PostgreSQLBridge) UpdateContract(sc *types.Contract) error {
	// Complain about missing contract data
	if sc == nil {
		db.log.Criticalf("cannot update empty contract")
		return fmt.Errorf("no contract given to update")
	}

	// Prepare the SQL query to update the contract details
	query := `
		UPDATE contracts
		SET other_column1 = $1, other_column2 = $2, ...  -- Add more columns as needed
		WHERE address = $3
		RETURNING address;` // Optionally, return the updated address

	// Execute the update query
	err := db.db.QueryRow(query, sc.Address).Scan(&sc.Address)
	if err != nil {
		// Log the issue
		db.log.Errorf("cannot update contract details at %s; %s", sc.Address.String(), err.Error())
		return err
	}

	return nil
}

// IsContractKnown checks if a smart contract document already exists in the database.
func (db *MongoDbBridge) IsContractKnown(addr *common.Address) bool {
	// check the contract existence in the database
	known, err := db.isContractKnown(db.client.Database(db.dbName).Collection(coContract), addr)
	if err != nil {
		return false
	}

	return known
}

// IsContractKnown checks if a smart contract document already exists in the PostgreSQL database.
func (db *PostgreSQLBridge) IsContractKnown(addr *common.Address) bool {
	// Prepare the SQL query to check if the contract exists
	query := `
		SELECT EXISTS(
			SELECT 1 
			FROM contracts 
			WHERE address = $1
		);`

	var known bool

	// Execute the query
	err := db.db.QueryRow(query, addr.String()).Scan(&known)
	if err != nil {
		// Log the error and return false as default
		db.log.Errorf("error checking if contract is known at %s; %s", addr.String(), err.Error())
		return false
	}

	return known
}

// isContractKnown checks if a smart contract document already exists in the database.
func (db *MongoDbBridge) isContractKnown(col *mongo.Collection, addr *common.Address) (bool, error) {
	// try to find the contract in the database (it may already exist)
	sr := col.FindOne(context.Background(), bson.D{
		{Key: fiContractPk, Value: addr.String()},
	}, options.FindOne().SetProjection(bson.D{
		{Key: fiContractPk, Value: true},
	}))

	// error on lookup?
	if sr.Err() != nil {
		// may be ErrNoDocuments, which we seek
		if sr.Err() == mongo.ErrNoDocuments {
			return false, nil
		}

		// inform that we can not get the PK; should not happen
		db.log.Error("can not get existing contract pk")
		return false, sr.Err()
	}

	return true, nil
}

// isContractKnown checks if a smart contract document already exists in the PostgreSQL database.
func (db *PostgreSQLBridge) isContractKnown(addr *common.Address) (bool, error) {
	// SQL query to check if the contract exists
	query := `
		SELECT EXISTS(
			SELECT 1 
			FROM contracts 
			WHERE address = $1
		);`

	var exists bool

	// Execute the query
	err := db.db.QueryRow(query, addr.String()).Scan(&exists)
	if err != nil {
		// Log the error and return
		db.log.Errorf("error checking if contract is known at %s; %s", addr.String(), err.Error())
		return false, err
	}

	return exists, nil
}

// ContractTransaction returns contract creation transaction hash if available.
func (db *MongoDbBridge) ContractTransaction(addr *common.Address) (*common.Hash, error) {
	// get the contract details from database
	c, err := db.Contract(addr)
	if err != nil {
		db.log.Errorf("can not get the contract transaction for [%s]; %s", addr.String(), err.Error())
		return nil, err
	}

	// contract not found
	if c == nil {
		return nil, nil
	}

	// return the hash
	return &c.TransactionHash, nil
}

// ContractTransaction returns contract creation transaction hash if available.
func (db *PostgreSQLBridge) ContractTransaction(addr *common.Address) (*common.Hash, error) {
	// Prepare the SQL query to fetch the contract from the database
	query := `SELECT transaction_hash FROM contracts WHERE address = $1`

	var transactionHash string

	// Execute the query
	err := db.db.QueryRow(query, addr.String()).Scan(&transactionHash)

	// Handle errors
	if err != nil {
		if err == sql.ErrNoRows {
			// No contract found
			return nil, nil
		}
		db.log.Errorf("can not get the contract transaction for [%s]; %s", addr.String(), err.Error())
		return nil, err
	}

	// If transaction hash is empty, return nil
	if transactionHash == "" {
		return nil, nil
	}

	// Return the transaction hash
	hash := common.HexToHash(transactionHash)
	return &hash, nil
}

// Contract returns details of a smart contract stored in the Mongo database
// if available, or nil if contract does not exist.
func (db *MongoDbBridge) Contract(addr *common.Address) (*types.Contract, error) {
	// get the collection for transactions
	col := db.client.Database(db.dbName).Collection(coContract)

	// try to find the contract in the database (it may already exist)
	sr := col.FindOne(context.Background(), bson.D{{Key: fiContractPk, Value: addr.String()}})

	// error on lookup?
	if sr.Err() != nil {
		// may be ErrNoDocuments, which we seek
		if sr.Err() == mongo.ErrNoDocuments {
			return nil, nil
		}

		// inform that we can not get the PK; should not happen
		db.log.Errorf("can not get contract %s details; %s", addr.String(), sr.Err().Error())
		return nil, sr.Err()
	}

	// try to decode the contract data
	var con types.Contract
	err := sr.Decode(&con)
	if err != nil {
		db.log.Errorf("can not decode contract %s details; %s", addr.String(), err.Error())
		return nil, err
	}

	// inform
	db.log.Debugf("loaded contract %s", addr.String())
	return &con, nil
}

// Contract returns details of a smart contract stored in the PostgreSQL database
// if available, or nil if the contract does not exist.
func (db *PostgreSQLBridge) Contract(addr *common.Address) (*types.Contract, error) {
	// Prepare the SQL query to fetch the contract from the database
	query := `SELECT address, transaction_hash, other_column1, other_column2 FROM contracts WHERE address = $1`

	// Define a variable to hold the contract details
	var con types.Contract

	// Execute the query
	err := db.db.QueryRow(query, addr.String()).Scan(&con.Address, &con.TransactionHash)

	// Handle errors
	if err != nil {
		if err == sql.ErrNoRows {
			// No contract found
			return nil, nil
		}
		db.log.Errorf("can not get contract %s details; %s", addr.String(), err.Error())
		return nil, err
	}

	// Inform about successful loading
	db.log.Debugf("loaded contract %s", addr.String())
	return &con, nil
}

// ContractCount calculates total number of contracts in the database.
func (db *MongoDbBridge) ContractCount() (uint64, error) {
	return db.EstimateCount(db.client.Database(db.dbName).Collection(coContract))
}

func (db *PostgreSQLBridge) ContractCount() (int64, error) {
	// Define the query to count the rows in the 'contracts' table
	query := "SELECT COUNT(*) FROM contracts"

	// Execute the query and scan the result into a variable
	var count int64
	err := db.db.QueryRow(query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get contract count: %w", err)
	}

	// Return the count as uint64
	return int64(count), nil
}

// contractListTotal find the total amount of contracts for the criteria and populates the list
func (db *MongoDbBridge) contractListTotal(col *mongo.Collection, validatedOnly bool, list *types.ContractList) error {
	// validation filter
	filter := bson.D{}
	if validatedOnly {
		filter = bson.D{{Key: fiContractSourceValidated, Value: bson.D{{Key: "$ne", Value: nil}}}}
	}

	// find how many contracts do we have in the database
	total, err := col.CountDocuments(context.Background(), filter)
	if err != nil {
		db.log.Errorf("can not count contracts")
		return err
	}

	// apply the total count
	list.Total = uint64(total)
	return nil
}

// contractListTotal finds the total number of contracts for the criteria and populates the list
func (db *PostgreSQLBridge) contractListTotal(validatedOnly bool, list *types.ContractList) error {
	// Build the SQL query with the validation filter
	query := `SELECT COUNT(*) FROM contracts`
	if validatedOnly {
		query += ` WHERE validated IS NOT NULL`
	}

	// Execute the query to get the total count
	var total int64
	err := db.db.QueryRow(query).Scan(&total)
	if err != nil {
		db.log.Errorf("can not count contracts; %s", err.Error())
		return err
	}

	// Apply the total count to the list
	list.Total = uint64(total)
	return nil
}

// contractListTopFilter constructs a filter for finding the top item of the list.
// Consider creating DB index db.contract.createIndex({_id:1,orx:-1},{unique:true}).
func contractListTopFilter(validatedOnly bool, cursor *string) (*bson.D, error) {
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

	// with cursor and any validation status
	filter := bson.D{}
	if cursor != nil && !validatedOnly {
		filter = bson.D{{Key: fiContractOrdinalIndex, Value: ix}}
	}

	// no cursor, but validation status filter
	if cursor == nil && validatedOnly {
		filter = bson.D{{Key: fiContractSourceValidated, Value: bson.D{{Key: "$ne", Value: nil}}}}
	}

	// with cursor and also the validation filter
	if cursor != nil && validatedOnly {
		filter = bson.D{{Key: fiContractSourceValidated, Value: bson.D{{Key: "$ne", Value: nil}}}, {Key: fiContractOrdinalIndex, Value: ix}}
	}
	return &filter, nil
}

// contractListTopFilter constructs a WHERE clause for finding the top item of the list in PostgreSQL.
func contractListTopFilterpostgres(validatedOnly bool, cursor *string) (string, []interface{}, error) {
	// Prepare a slice to hold query parameters
	var conditions []string
	var args []interface{}

	// what is the requested ordinal index from cursor, if any
	if cursor != nil {
		// Get the ordinal index from cursor
		ix, err := strconv.ParseUint(*cursor, 10, 64)
		if err != nil {
			return "", nil, fmt.Errorf("invalid cursor value; %s", err.Error())
		}

		// Add condition for ordinal index if cursor is provided
		conditions = append(conditions, "ordinal_index = $1")
		args = append(args, ix)
	}

	// Add condition for validated status if requested
	if validatedOnly {
		conditions = append(conditions, "validated IS NOT NULL")
	}

	// Construct the WHERE clause
	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	return whereClause, args, nil
}

// contractListTop find the first contract of the list based on provided criteria and populates the list.
func (db *MongoDbBridge) contractListTop(col *mongo.Collection, validatedOnly bool, cursor *string, count int32, list *types.ContractList) error {
	// get the filter
	filter, err := contractListTopFilter(validatedOnly, cursor)
	if err != nil {
		db.log.Errorf("can not find top contract for the list; %s", err.Error())
		return err
	}

	// find out the cursor ordinal index
	if cursor == nil && count > 0 {
		// get the highest available ordinal index (top smart contract)
		list.First, err = db.findBorderOrdinalIndex(col,
			*filter,
			options.FindOne().SetSort(bson.D{{Key: fiContractOrdinalIndex, Value: -1}}))
		list.IsStart = true

	} else if cursor == nil && count < 0 {
		// get the lowest available ordinal index (bottom smart contract)
		list.First, err = db.findBorderOrdinalIndex(col,
			*filter,
			options.FindOne().SetSort(bson.D{{Key: fiContractOrdinalIndex, Value: 1}}))
		list.IsEnd = true

	} else if cursor != nil {
		// get the highest available ordinal index (top smart contract)
		list.First, err = db.findBorderOrdinalIndex(col,
			*filter,
			options.FindOne())
	}

	// check the error
	if err != nil {
		db.log.Errorf("can not find the initial contract")
		return err
	}
	return nil
}

// contractListTop finds the first contract of the list based on the provided criteria and populates the list.
func (db *PostgreSQLBridge) contractListTop(validatedOnly bool, cursor *string, count int32, list *types.ContractList) error {
	// Build the WHERE clause and parameters
	whereClause, args, err := contractListTopFilterpostgres(validatedOnly, cursor)
	if err != nil {
		db.log.Errorf("cannot find top contract for the list; %s", err.Error())
		return err
	}

	// Construct the SQL query
	query := "SELECT * FROM contracts " + whereClause + " ORDER BY ordinal_index DESC LIMIT $1"
	args = append(args, count)

	// Execute the query to get the first contract based on the criteria
	rows, err := db.db.Query(query, args...)
	if err != nil {
		db.log.Errorf("error executing query to fetch top contract; %s", err.Error())
		return err
	}
	defer rows.Close()

	// Fetch the contract data
	if rows.Next() {
		var contract types.Contract
		if err := rows.Scan(&contract.Address, &contract.Validated); err != nil {
			db.log.Errorf("error scanning contract row; %s", err.Error())
			return err
		}
		//list.First = append(list.First, contract)
	}

	// Check if it's the start or end of the list
	if cursor == nil && count > 0 {
		list.IsStart = true
	} else if cursor == nil && count < 0 {
		list.IsEnd = true
	}

	return nil
}

// contractListInit initializes list of contracts based on provided cursor and count.
func (db *MongoDbBridge) contractListInit(col *mongo.Collection, validatedOnly bool, cursor *string, count int32) (*types.ContractList, error) {
	// make the list
	list := types.ContractList{
		Collection: make([]*types.Contract, 0),
		Total:      0,
		First:      0,
		Last:       0,
		IsStart:    false,
		IsEnd:      false,
	}

	// calculate the total number of contracts in the list
	if err := db.contractListTotal(col, validatedOnly, &list); err != nil {
		return nil, err
	}

	// inform what we are about to do
	db.log.Debugf("found %d contracts in off-chain database", list.Total)

	// find the top contract of the list
	if err := db.contractListTop(col, validatedOnly, cursor, count, &list); err != nil {
		return nil, err
	}

	// inform what we are about to do
	db.log.Debugf("contract list initialized with ordinal index %d", list.First)
	return &list, nil
}

// contractListInit initializes list of contracts based on provided cursor and count.
func (db *PostgreSQLBridge) contractListInit(validatedOnly bool, cursor *string, count int32) (*types.ContractList, error) {
	// Initialize the contract list structure
	list := types.ContractList{
		Collection: make([]*types.Contract, 0),
		Total:      0,
		First:      0,
		Last:       0,
		IsStart:    false,
		IsEnd:      false,
	}

	// Calculate the total number of contracts in the list
	if err := db.contractListTotal(validatedOnly, &list); err != nil {
		return nil, err
	}

	// Inform what we are about to do
	db.log.Debugf("found %d contracts in the off-chain database", list.Total)

	// Find the top contract of the list
	if err := db.contractListTop(validatedOnly, cursor, count, &list); err != nil {
		return nil, err
	}

	// Inform what we are about to do
	db.log.Debugf("contract list initialized with ordinal index %d", list.First)
	return &list, nil
}

// contractListFilter creates a filter for contract list search.
func (db *MongoDbBridge) contractListFilter(validatedOnly bool, cursor *string, count int32, list *types.ContractList) *bson.D {
	// inform what we are about to do
	db.log.Debugf("contract filter starts from index %d", list.First)
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

	// build the filter query
	var filter bson.D
	if validatedOnly {
		// filter validated only contracts
		filter = bson.D{
			{Key: fiContractOrdinalIndex, Value: bson.D{{Key: ordinalOp, Value: list.First}}},
			{Key: fiContractSourceValidated, Value: bson.D{{Key: "$ne", Value: nil}}},
		}
	} else {
		// filter all contracts
		filter = bson.D{{Key: fiContractOrdinalIndex, Value: bson.D{{Key: ordinalOp, Value: list.First}}}}
	}
	return &filter
}

// contractListFilter creates a SQL WHERE clause for contract list search.
func (db *PostgreSQLBridge) contractListFilter(validatedOnly bool, cursor *string, count int32, list *types.ContractList) (string, []interface{}, error) {
	// Initialize the WHERE clause and parameters
	var whereClause string
	var args []interface{}
	ordinalOp := "<="

	// Inform what we are about to do
	db.log.Debugf("contract filter starts from index %d", list.First)

	// Determine the ordinal operation based on the cursor and count
	if cursor == nil && count < 0 {
		ordinalOp = ">="
	} else if cursor != nil && count > 0 {
		ordinalOp = "<"
	} else if cursor != nil && count < 0 {
		ordinalOp = ">"
	}

	// Build the WHERE clause
	if validatedOnly {
		// Filter validated only contracts
		whereClause = fmt.Sprintf("WHERE ordinal_index %s $1 AND validated IS NOT NULL", ordinalOp)
		args = append(args, list.First)
	} else {
		// Filter all contracts
		whereClause = fmt.Sprintf("WHERE ordinal_index %s $1", ordinalOp)
		args = append(args, list.First)
	}

	return whereClause, args, nil
}

// contractListOptions creates a filter options set for contract list search.
func (db *MongoDbBridge) contractListOptions(count int32) *options.FindOptions {
	// prep options
	opt := options.Find()

	// how to sort results in the collection
	if count > 0 {
		// from high (new) to low (old)
		opt.SetSort(bson.D{{Key: fiContractOrdinalIndex, Value: -1}})
	} else {
		// from low (old) to high (new)
		opt.SetSort(bson.D{{Key: fiContractOrdinalIndex, Value: 1}})
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

// contractListOptions constructs SQL clauses for sorting and limiting contract list results.
func (db *PostgreSQLBridge) contractListOptions(count int32) (string, int64) {
	// Determine sorting order
	sortOrder := "DESC"
	if count < 0 {
		sortOrder = "ASC"
	}

	// Determine the limit
	limit := int64(count)
	if limit < 0 {
		limit = -limit
	}
	// Add 1 to the limit to fetch one more record
	limit++

	// Build the ORDER BY clause
	orderByClause := fmt.Sprintf("ORDER BY ordinal_index %s", sortOrder)

	return orderByClause, limit
}

// contractListLoad loads the initialized contract list from persistent database.
func (db *MongoDbBridge) contractListLoad(col *mongo.Collection, validatedOnly bool, cursor *string, count int32, list *types.ContractList) error {
	// get the context for loader
	ctx := context.Background()

	// load the data
	ld, err := col.Find(ctx, db.contractListFilter(validatedOnly, cursor, count, list), db.contractListOptions(count))
	if err != nil {
		db.log.Errorf("error loading contract list; %s", err.Error())
		return err
	}

	defer db.closeCursor(ld)

	// loop and load
	var contract *types.Contract
	for ld.Next(ctx) {
		// process the last found hash
		if contract != nil {
			list.Collection = append(list.Collection, contract)
			list.Last = contract.Uid()
		}

		// try to decode the next row
		var con types.Contract
		if err := ld.Decode(&con); err != nil {
			db.log.Errorf("can not decode contract the list row; %s", err.Error())
			return err
		}

		// keep this one
		contract = &con
	}

	// we should have all the items already; we may just need to check if a boundary was reached
	if contract != nil {
		list.IsEnd = count > 0 && int32(len(list.Collection)) < count
		list.IsStart = count < 0 && int32(len(list.Collection)) < -count

		// add the last item as well
		if list.IsStart || list.IsEnd {
			list.Collection = append(list.Collection, contract)
			list.Last = contract.Uid()
		}
	}

	return nil
}

// contractListLoad loads the initialized contract list from the PostgreSQL database.
func (db *PostgreSQLBridge) contractListLoad(validatedOnly bool, cursor *string, count int32, list *types.ContractList) error {
	// Build the SQL filter and sorting clauses
	filter, args, err := db.contractListFilter(validatedOnly, cursor, count, list)
	if err != nil {
		db.log.Errorf("error creating filter for contract list; %s", err.Error())
		return err
	}

	orderByClause, limit := db.contractListOptions(count)
	args = append(args, limit) // Add the limit as the last argument

	// Construct the query
	query := fmt.Sprintf(`
		SELECT * 
		FROM contracts 
		%s 
		%s 
		LIMIT $%d`, filter, orderByClause, len(args))

	// Execute the query
	rows, err := db.db.Query(query, args...)
	if err != nil {
		db.log.Errorf("error loading contract list; %s", err.Error())
		return err
	}
	defer rows.Close()

	// Loop through the results
	var contract *types.Contract
	for rows.Next() {
		// Process the last found contract
		if contract != nil {
			list.Collection = append(list.Collection, contract)
			list.Last = contract.Uid()
		}

		// Decode the next row
		var con types.Contract
		if err := rows.Scan(&con.Address, &con.Validated); err != nil {
			db.log.Errorf("can not decode contract in the list row; %s", err.Error())
			return err
		}

		// Keep this one
		contract = &con
	}

	// Check if we reached the boundaries
	if contract != nil {
		list.IsEnd = count > 0 && int32(len(list.Collection)) < count
		list.IsStart = count < 0 && int32(len(list.Collection)) < -count

		// Add the last item to the collection if applicable
		if list.IsStart || list.IsEnd {
			list.Collection = append(list.Collection, contract)
			list.Last = contract.Uid()
		}
	}

	return nil
}

// Contracts provides list of smart contracts stored in the persistent storage.
func (db *MongoDbBridge) Contracts(validatedOnly bool, cursor *string, count int32) (*types.ContractList, error) {
	// nothing to load?
	if count == 0 {
		return nil, fmt.Errorf("nothing to do, zero contracts requested")
	}

	// get the collection and context
	col := db.client.Database(db.dbName).Collection(coContract)

	// init the list
	list, err := db.contractListInit(col, validatedOnly, cursor, count)
	if err != nil {
		db.log.Errorf("can not build contract list; %s", err.Error())
		return nil, err
	}

	// load data
	err = db.contractListLoad(col, validatedOnly, cursor, count, list)
	if err != nil {
		db.log.Errorf("can not load contracts list from database; %s", err.Error())
		return nil, err
	}

	// shift the first item on cursor
	if cursor != nil {
		list.First = list.Collection[0].Uid()
	}

	// reverse on negative so new-er contracts will be on top
	if count < 0 {
		list.Reverse()
		count = -count
	}

	// cut the end?
	if len(list.Collection) > int(count) {
		list.Collection = list.Collection[:len(list.Collection)-1]
	}
	return list, nil
}

// Contracts provides a list of smart contracts stored in the persistent PostgreSQL storage.
func (db *PostgreSQLBridge) Contracts(validatedOnly bool, cursor *string, count int32) (*types.ContractList, error) {
	// Nothing to load?
	if count == 0 {
		return nil, fmt.Errorf("nothing to do, zero contracts requested")
	}

	// Initialize the list
	// Initialize the list using contractListInit
	list, err := db.contractListInit(validatedOnly, cursor, count)
	if err != nil {
		db.log.Errorf("cannot initialize contract list; %s", err.Error())
		return nil, err
	}

	// Calculate the total number of contracts in the list
	if err := db.contractListTotal(validatedOnly, list); err != nil {
		db.log.Errorf("cannot calculate total number of contracts; %s", err.Error())
		return nil, err
	}

	// Inform what we are about to do
	db.log.Debugf("found %d contracts in the database", list.Total)

	// Find the top contract of the list
	if err := db.contractListTop(validatedOnly, cursor, count, list); err != nil {
		db.log.Errorf("cannot find the top contract for the list; %s", err.Error())
		return nil, err
	}

	// Load the data
	if err := db.contractListLoad(validatedOnly, cursor, count, list); err != nil {
		db.log.Errorf("cannot load contracts list from database; %s", err.Error())
		return nil, err
	}

	// Shift the first item on cursor
	if cursor != nil && len(list.Collection) > 0 {
		list.First = list.Collection[0].Uid()
	}

	// Reverse the list on negative count so newer contracts will be on top
	if count < 0 {
		list.Reverse()
		count = -count
	}

	// Trim the collection to the requested count if needed
	if len(list.Collection) > int(count) {
		list.Collection = list.Collection[:int(count)]
	}

	return list, nil
}
