// Package db implements bridge to persistent storage represented by Mongo database.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"ncogearthchain-api-graphql/internal/types"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	_ "github.com/lib/pq"
)

const (
	// coAccount is the name of the off-chain database collection storing account details.
	coAccounts = "account"

	// fiAccountTransactionPk is the name of the primary key field
	// of the account to transaction collection.
	fiAccountPk = "_id"

	// fiAccountType is the name of the field of the account contract type.
	fiAccountType = "type"

	// fiAccountLastActivity is the name of the field of the account last activity time stamp.
	fiAccountLastActivity = "ats"

	// fiAccountTransactionCounter is the name of the field of the account transaction counter.
	fiAccountTransactionCounter = "atc"

	// fiScCreationTx is the name of the field of the transaction hash
	// which created the contract, if the account is a contract.
	fiScCreationTx = "sc"

	// defaultTokenListLength is the number of ERC20 tokens pulled by default on negative count
	defaultTokenListLength = 25
)

// AccountRow is the account base row
type AccountRow struct {
	Address  string       `bson:"_id"`
	Type     string       `bson:"type"`
	Sc       *string      `bson:"sc"`
	Activity uint64       `bson:"ats"`
	Counter  uint64       `bson:"atc"`
	ScHash   *common.Hash `bson:"-"`
}

// type PostAccountRow struct {
// 	Address  string       `db:"address"`         // Maps to the "address" column in PostgreSQL
// 	Type     string       `db:"type"`            // Maps to the "type" column in PostgreSQL
// 	Sc       *string      `db:"sc"`              // Maps to the "sc" column in PostgreSQL
// 	Activity uint64       `db:"activity"`        // Maps to the "activity" column in PostgreSQL
// 	Counter  uint64       `db:"counter"`         // Maps to the "counter" column in PostgreSQL
// 	ScHash   *common.Hash `db:"sc_hash"`         // Maps to the "sc_hash" column in PostgreSQL
// }

// type PostAccountRow struct {
// 	Sc       *string
// 	Type     string
// 	Activity uint64
// 	Counter  uint64
// 	ScHash   *common.Hash
// }

type PostAccountRow struct {
	Name         string
	Address      string `bson:"_id"`
	ContractTx   *types.Transaction
	Type         string
	LastActivity uint64
	TrxCounter   uint64
	Balance      float64
	Sc           *string
	//Type     string
	Activity uint64
	Counter  uint64
	ScHash   *common.Hash
}

// // initAccountsCollection initializes the account collection with
// // indexes and additional parameters needed by the app.
// func (db *MongoDbBridge) initAccountsCollection() {
// 	db.log.Debugf("accounts collection initialized")
// }

// indexes and additional parameters needed by the app.
func (db *PostgreSQLBridge) initAccountsTable() {
	db.log.Debugf("Initializing accounts table...")

	// Create table if it does not exist
	createTableSQL := `
 CREATE TABLE accounts (
                id SERIAL PRIMARY KEY,
                address TEXT UNIQUE,
                sc TEXT,
				type TEXT,
                activity BIGINT DEFAULT 0,
                counter BIGINT DEFAULT 0,
               created_at TIMESTAMP DEFAULT NOW()
            )
    `
	_, err := db.db.Exec(createTableSQL)
	if err != nil {
		db.log.Panicf("could not create accounts table: %s", err.Error())
	}

	// Create indexes for performance, example index on account_number
	createIndexSQL := `
    CREATE INDEX IF NOT EXISTS idx_account_number ON accounts(account_number);
    `
	_, err = db.db.Exec(createIndexSQL)
	if err != nil {
		db.log.Panicf("could not create index for account_number: %s", err.Error())
	}

	// Log that the table and index have been initialized
	db.log.Debugf("accounts table and index initialized successfully")
}

// initAccountsTable initializes the accounts table with
// indexes and additional parameters needed by the app.
// func (db *PostgreSQLBridge) initAccountsTable() {
// 	db.log.Debugf("Initializing accounts table...")

// 	// Create table if it does not exist
// 	createTableSQL := `
//     CREATE TABLE IF NOT EXISTS accounts (
//         id SERIAL PRIMARY KEY,
//         account_number TEXT NOT NULL,
//         balance NUMERIC NOT NULL,
//         created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
//     );
//     `
// 	_, err := db.db.Exec(createTableSQL)
// 	if err != nil {
// 		db.log.Panicf("could not create accounts table: %s", err.Error())
// 	}

// 	// Create indexes for performance, example index on account_number
// 	createIndexSQL := `
//     CREATE INDEX IF NOT EXISTS idx_account_number ON accounts(account_number);
//     `
// 	_, err = db.db.Exec(createIndexSQL)
// 	if err != nil {
// 		db.log.Panicf("could not create index for account_number: %s", err.Error())
// 	}

// 	// Log that the table and index have been initialized
// 	db.log.Debugf("accounts table and index initialized successfully")
// }

// // Account tries to load an account identified by the address given from
// // the off-chain database.
// func (db *MongoDbBridge) Account(addr *common.Address) (*types.Account, error) {
// 	// get the collection for account transactions
// 	col := db.client.Database(db.dbName).Collection(coAccounts)

// 	// try to find the account
// 	sr := col.FindOne(context.Background(), bson.D{{Key: fiAccountPk, Value: addr.String()}}, options.FindOne())

// 	// error on lookup?
// 	if sr.Err() != nil {
// 		// may be ErrNoDocuments, which we seek
// 		if sr.Err() == mongo.ErrNoDocuments {
// 			return nil, nil
// 		}

// 		db.log.Error("can not get existing account %s; %s", addr.String(), sr.Err().Error())
// 		return nil, sr.Err()
// 	}

// 	// try to decode the row
// 	var row AccountRow
// 	err := sr.Decode(&row)
// 	if err != nil {
// 		db.log.Error("can not decode account %s; %s", addr.String(), err.Error())
// 		return nil, err
// 	}

// 	// any hash?
// 	if row.Sc != nil {
// 		h := common.HexToHash(*row.Sc)
// 		row.ScHash = &h
// 	}

// 	return &types.Account{
// 		Address:      *addr,
// 		ContractTx:   row.ScHash,
// 		Type:         row.Type,
// 		LastActivity: hexutil.Uint64(row.Activity),
// 		TrxCounter:   hexutil.Uint64(row.Counter),
// 	}, nil
// }

func (db *PostgreSQLBridge) Account(addr *common.Address) (*types.Account, error) {
	// Prepare the SQL query to retrieve the account from PostgreSQL
	query := `SELECT sc, type, activity, counter FROM accounts WHERE address = $1`

	// Execute the query with context (we don't need to pass context to QueryRow unless using context-specific operations)
	row := db.db.QueryRowContext(context.Background(), query, addr.String())

	// Initialize a variable to store the result
	var rowData PostAccountRow

	// Scan the row into the PostAccountRow struct
	err := row.Scan(&rowData.Sc, &rowData.Type, &rowData.Activity, &rowData.Counter)
	if err != nil {
		if err == sql.ErrNoRows {
			// If no rows are found, return nil
			return nil, nil
		}
		db.log.Error("Cannot get existing account %s; %s", addr.String(), err.Error())
		return nil, err
	}

	// If there's a smart contract field, decode the hash
	if rowData.Sc != nil {
		h := common.HexToHash(*rowData.Sc)
		rowData.ScHash = &h
	}

	// Return the result in the expected format
	return &types.Account{
		Address:      *addr,
		ContractTx:   rowData.ScHash,
		Type:         rowData.Type,
		LastActivity: hexutil.Uint64(rowData.Activity),
		TrxCounter:   hexutil.Uint64(rowData.Counter),
	}, nil
}

// // AddAccount stores an account in the blockchain if not exists.
// func (db *MongoDbBridge) AddAccount(acc *types.Account) error {
// 	// do we have account data?
// 	if acc == nil {
// 		return fmt.Errorf("can not add empty account")
// 	}

// 	// get the collection for account transactions
// 	col := db.client.Database(db.dbName).Collection(coAccounts)

// 	// extract contract creation transaction if available
// 	var conTx *string
// 	if acc.ContractTx != nil {
// 		cx := acc.ContractTx.String()
// 		conTx = &cx
// 	}

// 	// do the update based on given PK; we don't need to pull the document updated
// 	_, err := col.InsertOne(context.Background(), bson.D{
// 		{Key: fiAccountPk, Value: acc.Address.String()},
// 		{Key: fiScCreationTx, Value: conTx},
// 		{Key: fiAccountType, Value: acc.Type},
// 		{Key: fiAccountLastActivity, Value: uint64(acc.LastActivity)},
// 		{Key: fiAccountTransactionCounter, Value: uint64(acc.TrxCounter)},
// 	})

// 	// error on lookup?
// 	if err != nil {
// 		db.log.Error("can not insert new account")
// 		return err
// 	}

// 	// check init state
// 	// make sure transactions collection is initialized
// 	if db.initAccounts != nil {
// 		db.initAccounts.Do(func() { db.initAccountsCollection(); db.initAccounts = nil })
// 	}

// 	// log what we have done
// 	db.log.Debugf("added account at %s", acc.Address.String())
// 	return nil
// }

func (db *PostgreSQLBridge) GetAllAccounts() ([]*types.Account, error) {
	var accounts []*types.Account

	// Ensure the database connection is active
	if err := db.db.Ping(); err != nil {
		db.log.Errorf("Database connection lost: %v", err)
		return nil, fmt.Errorf("database connection lost: %v", err)
	}

	query := `SELECT address FROM accounts ORDER BY last_activity DESC;`
	//db.log.Infof("Running fresh DB query: %s", query)

	rows, err := db.db.Query(query)
	if err != nil {
		db.log.Errorf("Query failed: %v", err)
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var addressStr string
		if err := rows.Scan(&addressStr); err != nil {
			db.log.Errorf("Row scan failed: %v", err)
			continue
		}

		//  Convert string to common.Address
		address := common.HexToAddress(addressStr)

		accounts = append(accounts, &types.Account{
			Address: address,
		})
	}

	db.log.Infof("DB returned %d accounts: %v", len(accounts), accounts)
	return accounts, nil
}

// func (db *PostgreSQLBridge) AddAccount(acc *types.Account) error {
// 	// Do we have account data?
// 	if acc == nil {
// 		return fmt.Errorf("cannot add empty account")
// 	}

// 	// Prepare the contract creation transaction if available
// 	var conTx *string
// 	if acc.ContractTx != nil {
// 		cx := acc.ContractTx.String()
// 		conTx = &cx
// 	}

// 	// Use the provided timestamp or default to current time
// 	lastActivity := time.Unix(int64(acc.LastActivity), 0)
// 	if acc.LastActivity == 0 {
// 		lastActivity = time.Now()
// 	}

// 	// SQL query to insert the account into PostgreSQL
// 	query := `
//         INSERT INTO accounts (address, sc, type, counter,last_activity)
//         VALUES ($1, $2, $3, $4, $5)
//         ON CONFLICT (address) DO UPDATE
// 		SET last_activity = EXCLUDED.last_activity, counter = EXCLUDED.counter + 1;`

// 	// Execute the insert query
// 	result, err := db.db.ExecContext(context.Background(), query,
// 		acc.Address.String(),
// 		conTx,
// 		acc.Type,
// 		//acc.Activity,   // Correct field for activity
// 		acc.TrxCounter, // Correct field for counter
// 		lastActivity,
// 	)

// 	// Check for errors during the insert
// 	if err != nil {
// 		db.log.Error("cannot insert new account")
// 		return err
// 	}

// 	rowsAffected, _ := result.RowsAffected()
// 	db.log.Infof("Account %s stored/updated, Rows affected: %d", acc.Address.String(), rowsAffected)

// 	// check init state
// 	// make sure transactions collection is initialized
// 	if db.initAccounts != nil {
// 		db.initAccounts.Do(func() { db.initAccountsTable(); db.initAccounts = nil })
// 	}

// 	// Log the successful addition
// 	db.log.Debugf("added account at %s", acc.Address.String())

// 	return nil
// }

func (db *PostgreSQLBridge) AddAccount(acc *types.Account) error {
	tx, err := db.db.Begin()
	if err := db.db.Ping(); err != nil {
		db.log.Errorf("Database connection lost: %v", err)
		return fmt.Errorf("database connection lost: %v", err)
	}

	// Use the provided timestamp or default to current time
	lastActivity := time.Unix(int64(acc.LastActivity), 0)
	if acc.LastActivity == 0 {
		lastActivity = time.Now()
	}

	query := `
        INSERT INTO accounts (address, type, last_activity, counter) 
        VALUES ($1, $2,$3, $4) 
        ON CONFLICT (address) DO UPDATE SET type = EXCLUDED.type,
		last_activity = EXCLUDED.last_activity, counter =  accounts.counter + 1;; 
    `

	// Debugging - print SQL execution
	//fmt.Printf("Executing query: %s\n With values: %s, %s, %v, %d\n", query, acc.Address.String(), acc.Type, lastActivity, 1)

	result, err := db.db.Exec(query, acc.Address.String(), acc.Type, lastActivity, 1)
	if err != nil {
		return fmt.Errorf("Failed to insert/update account: %v", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("No rows affected, account not stored")
	}
	// Commit transaction
	if err := tx.Commit(); err != nil {
		db.log.Errorf("Transaction commit failed: %v", err)
		return fmt.Errorf("transaction commit failed: %v", err)
	}

	db.log.Infof("Account %s stored successfully!", acc.Address.String())
	return nil

}

// // IsAccountKnown checks if an account document already exists in the database.
// func (db *MongoDbBridge) IsAccountKnown(addr *common.Address) (bool, error) {
// 	// get the collection for account transactions
// 	col := db.client.Database(db.dbName).Collection(coAccounts)

// 	// try to find the account in the database (it may already exist)
// 	sr := col.FindOne(context.Background(), bson.D{
// 		{Key: fiAccountPk, Value: addr.String()},
// 	}, options.FindOne().SetProjection(bson.D{{Key: fiAccountPk, Value: true}}))

// 	// error on lookup?
// 	if sr.Err() != nil {
// 		// may be ErrNoDocuments, which we seek
// 		if sr.Err() == mongo.ErrNoDocuments {
// 			return false, nil
// 		}

// 		db.log.Error("can not get existing account pk")
// 		return false, sr.Err()
// 	}

// 	return true, nil
// }

// IsAccountKnown checks if an account exists in the PostgreSQL database.
func (db *PostgreSQLBridge) IsAccountKnown(addr *common.Address) (bool, error) {
	// Prepare the SQL query to check if the account exists in the PostgreSQL database
	query := `SELECT 1 FROM accounts WHERE address = $1 LIMIT 1`

	// Execute the query
	row := db.db.QueryRowContext(context.Background(), query, addr.String())

	// Check if the row exists by scanning the result
	var exists int
	err := row.Scan(&exists)

	// Handle errors
	if err != nil {
		if err == sql.ErrNoRows {
			// Account does not exist
			return false, nil
		}
		db.log.Error("cannot check if account exists: %s", err.Error())
		return false, err
	}

	// If we have a result, the account exists
	return exists == 1, nil
}

// // AccountCount calculates total number of accounts in the database.
// func (db *MongoDbBridge) AccountCount() (uint64, error) {
// 	return db.EstimateCount(db.client.Database(db.dbName).Collection(coAccounts))
// }

func (db *PostgreSQLBridge) AccountCount() (uint64, error) {
	var count uint64
	query := "SELECT COUNT(*) FROM accounts"
	err := db.db.QueryRow(query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count rows in accounts table: %w", err)
	}
	return count, nil
}

// // AccountTransactions loads list of transaction hashes of an account.
// func (db *MongoDbBridge) AccountTransactions(addr *common.Address, rec *common.Address, cursor *string, count int32) (*types.TransactionList, error) {
// 	// nothing to load?
// 	if count == 0 {
// 		return nil, fmt.Errorf("nothing to do, zero blocks requested")
// 	}

// 	// no account given?
// 	if addr == nil {
// 		return nil, fmt.Errorf("can not list transactions of empty account")
// 	}

// 	// log what we do here
// 	db.log.Debugf("loading transactions of %s", addr.String())

// 	// make the filter for [(from = Account) OR (to = Account)]
// 	if rec == nil {
// 		filter := bson.D{{Key: "$or", Value: bson.A{bson.D{{Key: "from", Value: addr.String()}}, bson.D{{Key: "to", Value: addr.String()}}}}}
// 		return db.Transactions(cursor, count, &filter)
// 	}

// 	// return list of transactions filtered by the account and recipient
// 	filter := bson.D{{Key: "from", Value: addr.String()}, {Key: "to", Value: rec.String()}}
// 	return db.Transactions(cursor, count, &filter)
// }

func (db *PostgreSQLBridge) AccountTransactions(addr string, rec *string, cursor *string, count int32) (*types.PostTransactionList, error) {
	if count == 0 {
		return nil, fmt.Errorf("nothing to do, zero transactions requested")
	}

	if addr == "" {
		return nil, fmt.Errorf("cannot list transactions of an empty account")
	}

	db.log.Printf("Loading transactions for account %s", addr)

	query := `
		SELECT 
			hash, from_address, to_address, value, gas, gas_price, nonce, 
			data, block_hash, block_number, timestamp, contract_address, 
			status, cumulative_gas_used, gas_used, transaction_index
		FROM transactions
		WHERE (from_address = $1 OR to_address = $1)
	`

	params := []interface{}{addr}

	if rec != nil {
		query += " AND to_address = $2"
		params = append(params, *rec)
	}

	if cursor != nil {
		query += " AND hash > $3"
		params = append(params, *cursor)
	}

	query += " ORDER BY hash LIMIT $4"
	params = append(params, count)

	rows, err := db.db.Query(query, params...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %v", err)
	}
	defer rows.Close()

	var transactions []*types.Transaction
	for rows.Next() {
		var trx types.Transaction
		err := rows.Scan(
			&trx.Hash, &trx.From, &trx.To, &trx.Value, &trx.Gas, &trx.GasPrice, &trx.Nonce,
			&trx.InputData, &trx.BlockHash, &trx.BlockNumber, &trx.TimeStamp, &trx.ContractAddress,
			&trx.Status, &trx.CumulativeGasUsed, &trx.GasUsed, &trx.TrxIndex,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %v", err)
		}
		transactions = append(transactions, &trx)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error occurred during rows iteration: %v", err)
	}

	return &types.PostTransactionList{
		Collection: transactions,
		Total:      uint64(len(transactions)),
		First:      0,
		Last:       uint64(len(transactions) - 1),
		IsStart:    cursor == nil,
		IsEnd:      len(transactions) < int(count),
		Filter:     map[string]interface{}{"address": addr, "recipient": rec},
	}, nil

}

// // AccountMarkActivity marks the latest account activity in the repository.
// func (db *MongoDbBridge) AccountMarkActivity(addr *common.Address, ts uint64) error {
// 	// log what we do
// 	db.log.Debugf("account %s activity at %s", addr.String(), time.Unix(int64(ts), 0).String())

// 	// get the collection for contracts
// 	col := db.client.Database(db.dbName).Collection(coAccounts)

// 	// update the contract details
// 	if _, err := col.UpdateOne(context.Background(),
// 		bson.D{{Key: fiAccountPk, Value: addr.String()}},
// 		bson.D{
// 			{Key: "$set", Value: bson.D{{Key: fiAccountLastActivity, Value: ts}}},
// 			{Key: "$inc", Value: bson.D{{Key: fiAccountTransactionCounter, Value: 1}}},
// 		}); err != nil {
// 		// log the issue
// 		db.log.Errorf("can not update account %s details; %s", addr.String(), err.Error())
// 		return err
// 	}

// 	return nil
// }

// AccountMarkActivity marks the latest account activity in PostgreSQL.
func (db *PostgreSQLBridge) AccountMarkActivity(addr *common.Address, ts uint64) error {
	timestamp := time.Unix(int64(ts), 0)
	db.log.Debugf("account %s last_activity at %s", addr.String(), timestamp.String())

	// **Ensure the account exists before updating**
	query := `
        INSERT INTO accounts (address, last_activity, counter)
        VALUES ($1, $2, 1)
        ON CONFLICT (address) DO UPDATE 
        SET last_activity = EXCLUDED.last_activity, 
            counter = accounts.counter + 1;
    `

	_, err := db.db.ExecContext(context.Background(), query, addr.String(), timestamp)
	if err != nil {
		db.log.Errorf("cannot update account %s details; %s", addr.String(), err.Error())
		return fmt.Errorf("failed to update account activity: %v", err)
	}
	return nil
}

// // Erc20TokensList returns a list of known ERC20 tokens ordered by their activity.
// func (db *MongoDbBridge) Erc20TokensList(count int32) ([]common.Address, error) {
// 	// make sure the count is positive; use default size if not
// 	if count <= 0 {
// 		count = defaultTokenListLength
// 	}

// 	// log what we do
// 	db.log.Debugf("loading %d most active ERC20 token accounts", count)

// 	// get the collection for contracts
// 	col := db.client.Database(db.dbName).Collection(coAccounts)

// 	// make the filter for ERC20 tokens only and pull them ordered by activity
// 	filter := bson.D{{Key: "type", Value: types.AccountTypeERC20Token}}
// 	opt := options.Find().SetSort(bson.D{
// 		{Key: fiAccountTransactionCounter, Value: -1},
// 		{Key: fiAccountLastActivity, Value: -1},
// 	}).SetLimit(int64(count))

// 	// load the data
// 	cursor, err := col.Find(context.Background(), filter, opt)
// 	if err != nil {
// 		db.log.Errorf("error loading ERC20 tokens list; %s", err.Error())
// 		return nil, err
// 	}

// 	return db.loadErcContractsList(cursor)
// }

// Erc20TokensList returns a list of known ERC20 tokens ordered by their activity.
func (db *PostgreSQLBridge) Erc20TokensList(count int32) ([]common.Address, error) {
	// Ensure the count is positive; use default size if not
	if count <= 0 {
		count = defaultTokenListLength
	}

	// Log what we do
	db.log.Debugf("loading %d most active ERC20 token accounts", count)

	// Prepare the SQL query to fetch ERC20 tokens
	query := `
        SELECT address 
        FROM accounts 
        WHERE type = $1
        ORDER BY transaction_counter DESC, last_activity DESC
        LIMIT $2
    `

	// Execute the query
	rows, err := db.db.QueryContext(context.Background(), query, "ERC20Token", count)
	if err != nil {
		db.log.Errorf("error loading ERC20 tokens list; %s", err.Error())
		return nil, fmt.Errorf("error fetching ERC20 tokens list: %v", err)
	}
	defer rows.Close()

	// Collect the addresses
	var addresses []common.Address
	for rows.Next() {
		var addr string
		if err := rows.Scan(&addr); err != nil {
			db.log.Errorf("error scanning address; %s", err.Error())
			return nil, fmt.Errorf("error scanning address: %v", err)
		}
		addresses = append(addresses, common.HexToAddress(addr))
	}

	// Check for any errors during iteration
	if err := rows.Err(); err != nil {
		db.log.Errorf("error iterating over rows; %s", err.Error())
		return nil, fmt.Errorf("error iterating over rows: %v", err)
	}

	// Return the list of ERC20 token addresses
	return addresses, nil
}

// // Erc721ContractsList returns a list of known ERC20 tokens ordered by their activity.
// func (db *MongoDbBridge) Erc721ContractsList(count int32) ([]common.Address, error) {
// 	// make sure the count is positive; use default size if not
// 	if count <= 0 {
// 		count = defaultTokenListLength
// 	}

// 	// log what we do
// 	db.log.Debugf("loading %d most active ERC721 token accounts", count)

// 	// get the collection for contracts
// 	col := db.client.Database(db.dbName).Collection(coAccounts)

// 	// make the filter for ERC20 tokens only and pull them ordered by activity
// 	filter := bson.D{{Key: "type", Value: types.AccountTypeERC721Contract}}
// 	opt := options.Find().SetSort(bson.D{
// 		{Key: fiAccountTransactionCounter, Value: -1},
// 		{Key: fiAccountLastActivity, Value: -1},
// 	}).SetLimit(int64(count))

// 	// load the data
// 	cursor, err := col.Find(context.Background(), filter, opt)
// 	if err != nil {
// 		db.log.Errorf("error loading ERC721 tokens list; %s", err.Error())
// 		return nil, err
// 	}

// 	return db.loadErcContractsList(cursor)
// }

// Erc721ContractsList returns a list of known ERC721 contracts ordered by their activity.
func (db *PostgreSQLBridge) Erc721ContractsList(count int32) ([]common.Address, error) {
	// Ensure the count is positive; use default size if not
	if count <= 0 {
		count = defaultTokenListLength
	}

	// Log what we do
	db.log.Debugf("loading %d most active ERC721 token accounts", count)

	// Prepare the SQL query to fetch ERC721 contracts
	query := `
        SELECT address 
        FROM accounts 
        WHERE type = $1
        ORDER BY transaction_counter DESC, last_activity DESC
        LIMIT $2
    `

	// Execute the query
	rows, err := db.db.QueryContext(context.Background(), query, "ERC721Contract", count)
	if err != nil {
		db.log.Errorf("error loading ERC721 contracts list; %s", err.Error())
		return nil, fmt.Errorf("error fetching ERC721 contracts list: %v", err)
	}
	defer rows.Close()

	// Collect the addresses
	var addresses []common.Address
	for rows.Next() {
		var addr string
		if err := rows.Scan(&addr); err != nil {
			db.log.Errorf("error scanning address; %s", err.Error())
			return nil, fmt.Errorf("error scanning address: %v", err)
		}
		addresses = append(addresses, common.HexToAddress(addr))
	}

	// Check for any errors during iteration
	if err := rows.Err(); err != nil {
		db.log.Errorf("error iterating over rows; %s", err.Error())
		return nil, fmt.Errorf("error iterating over rows: %v", err)
	}

	// Return the list of ERC721 contract addresses
	return addresses, nil
}

// // Erc1155ContractsList returns a list of known ERC1155 contracts ordered by their activity.
// func (db *MongoDbBridge) Erc1155ContractsList(count int32) ([]common.Address, error) {
// 	// make sure the count is positive; use default size if not
// 	if count <= 0 {
// 		count = defaultTokenListLength
// 	}

// 	// log what we do
// 	db.log.Debugf("loading %d most active ERC1155 token accounts", count)

// 	// get the collection for contracts
// 	col := db.client.Database(db.dbName).Collection(coAccounts)

// 	// make the filter for ERC20 tokens only and pull them ordered by activity
// 	filter := bson.D{{Key: "type", Value: types.AccountTypeERC1155Contract}}
// 	opt := options.Find().SetSort(bson.D{
// 		{Key: fiAccountTransactionCounter, Value: -1},
// 		{Key: fiAccountLastActivity, Value: -1},
// 	}).SetLimit(int64(count))

// 	// load the data
// 	cursor, err := col.Find(context.Background(), filter, opt)
// 	if err != nil {
// 		db.log.Errorf("error loading ERC1155 tokens list; %s", err.Error())
// 		return nil, err
// 	}

// 	return db.loadErcContractsList(cursor)
// }

// Erc1155ContractsList returns a list of known ERC1155 contracts ordered by their activity.
func (db *PostgreSQLBridge) Erc1155ContractsList(count int32) ([]common.Address, error) {
	// Ensure the count is positive; use default size if not
	if count <= 0 {
		count = defaultTokenListLength
	}

	// Log what we do
	db.log.Debugf("loading %d most active ERC1155 token accounts", count)

	// Prepare the SQL query to fetch ERC1155 contracts
	query := `
        SELECT address 
        FROM accounts 
        WHERE type = $1
        ORDER BY transaction_counter DESC, last_activity DESC
        LIMIT $2
    `

	// Execute the query
	rows, err := db.db.QueryContext(context.Background(), query, "ERC1155Contract", count)
	if err != nil {
		db.log.Errorf("error loading ERC1155 contracts list; %s", err.Error())
		return nil, fmt.Errorf("error fetching ERC1155 contracts list: %v", err)
	}
	defer rows.Close()

	// Collect the addresses
	var addresses []common.Address
	for rows.Next() {
		var addr string
		if err := rows.Scan(&addr); err != nil {
			db.log.Errorf("error scanning address; %s", err.Error())
			return nil, fmt.Errorf("error scanning address: %v", err)
		}
		addresses = append(addresses, common.HexToAddress(addr))
	}

	// Check for any errors during iteration
	if err := rows.Err(); err != nil {
		db.log.Errorf("error iterating over rows; %s", err.Error())
		return nil, fmt.Errorf("error iterating over rows: %v", err)
	}

	// Return the list of ERC1155 contract addresses
	return addresses, nil
}

// func (db *MongoDbBridge) loadErcContractsList(cursor *mongo.Cursor) ([]common.Address, error) {
// 	// close the cursor as we leave
// 	defer db.closeCursor(cursor)

// 	// loop and load
// 	list := make([]common.Address, 0)
// 	var row AccountRow
// 	for cursor.Next(context.Background()) {
// 		// try to decode the next row
// 		if err := cursor.Decode(&row); err != nil {
// 			db.log.Errorf("can not decode ERC contracts list row; %s", err.Error())
// 			return nil, err
// 		}

// 		// decode the value
// 		list = append(list, common.HexToAddress(row.Address))
// 	}

// 	return list, nil
// }

// loadErcContractsList loads a list of ERC contracts from PostgreSQL.
func (db *PostgreSQLBridge) loadErcContractsList(rows *sql.Rows) ([]common.Address, error) {
	// Defer closing the rows when we're done
	defer rows.Close()

	// Create a slice to hold the list of addresses
	list := make([]common.Address, 0)

	// Loop through each row and scan the address
	for rows.Next() {
		var row AccountRow

		// Scan the address from the row
		if err := rows.Scan(&row.Address); err != nil {
			db.log.Errorf("can not scan ERC contract address; %s", err.Error())
			return nil, fmt.Errorf("failed to scan row: %v", err)
		}

		// Append the decoded address to the list
		list = append(list, common.HexToAddress(row.Address))
	}

	// Check if there was an error during the iteration
	if err := rows.Err(); err != nil {
		db.log.Errorf("error iterating over rows; %s", err.Error())
		return nil, fmt.Errorf("error iterating over rows: %v", err)
	}

	return list, nil
}
