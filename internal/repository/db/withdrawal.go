// Package db implements bridge to persistent storage represented by Mongo database.
package db

import (
	"database/sql"
	"fmt"
	"math/big"
	"ncogearthchain-api-graphql/internal/types"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// colWithdrawals represents the name of the withdrawals' collection in database.
const colWithdrawals = "withdraws"

// // initWithdrawalsCollection initializes the withdrawal requests collection with
// // indexes and additional parameters needed by the app.
// func (db *MongoDbBridge) initWithdrawalsCollection(col *mongo.Collection) {
// 	// prepare index models
// 	ix := make([]mongo.IndexModel, 0)

// 	// index delegator + validator
// 	unique := true
// 	ix = append(ix, mongo.IndexModel{
// 		Keys: bson.D{
// 			{Key: types.FiWithdrawalAddress, Value: 1},
// 			{Key: types.FiWithdrawalToValidator, Value: 1},
// 			{Key: types.FiWithdrawalRequestID, Value: 1},
// 		},
// 		Options: &options.IndexOptions{
// 			Unique: &unique,
// 		},
// 	})

// 	// index request ID, delegator address, and creation time stamp
// 	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: types.FiWithdrawalAddress, Value: 1}}})
// 	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: types.FiWithdrawalOrdinal, Value: -1}}})

// 	// create indexes
// 	if _, err := col.Indexes().CreateMany(context.Background(), ix); err != nil {
// 		db.log.Panicf("can not create indexes for withdrawals collection; %s", err.Error())
// 	}

// 	// log we are done that
// 	db.log.Debugf("withdrawals collection initialized")
// }

// initWithdrawalsCollection initializes the withdrawal requests table with indexes in PostgreSQL.
func (db *PostgreSQLBridge) initWithdrawalsCollection() error {
	// Define the SQL statements to create indexes
	queries := []string{
		// Unique index for delegator + validator + request ID
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_withdrawals_delegator_validator_request_id 
         ON withdrawals (address, to_validator, request_id)`,

		// Index for delegator address
		`CREATE INDEX IF NOT EXISTS idx_withdrawals_address 
         ON withdrawals (address)`,

		// Index for ordinal
		`CREATE INDEX IF NOT EXISTS idx_withdrawals_ordinal 
         ON withdrawals (ordinal DESC)`,
	}

	// Execute each query
	for _, query := range queries {
		_, err := db.db.Exec(query)
		if err != nil {
			db.log.Panicf("cannot create index for withdrawals table; %s", err.Error())
			return err
		}
	}

	// Log that initialization is complete
	db.log.Debugf("withdrawals table initialized with indexes")
	return nil
}

// // Withdrawal returns details of a withdrawal request specified by the request ID.
// func (db *MongoDbBridge) Withdrawal(addr *common.Address, valID *hexutil.Big, reqID *hexutil.Big) (*types.WithdrawRequest, error) {
// 	// get the collection for withdrawals
// 	col := db.client.Database(db.dbName).Collection(colWithdrawals)

// 	// try to find the delegation in the database
// 	sr := col.FindOne(context.Background(), bson.D{
// 		{Key: types.FiWithdrawalAddress, Value: addr.String()},
// 		{Key: types.FiWithdrawalToValidator, Value: valID.String()},
// 		{Key: types.FiWithdrawalRequestID, Value: reqID.String()},
// 	})

// 	// do we know the request?
// 	if sr.Err() == mongo.ErrNoDocuments {
// 		db.log.Errorf("withdraw request [%s] of %s to #%d not found", reqID.String(), addr.String(), valID.ToInt().Uint64())
// 		return nil, sr.Err()
// 	}

// 	// try to decode
// 	var wr types.WithdrawRequest
// 	if err := sr.Decode(&wr); err != nil {
// 		return nil, err
// 	}
// 	return &wr, nil
// }

// Withdrawal returns details of a withdrawal request specified by the request ID.
func (db *PostgreSQLBridge) Withdrawal(addr *common.Address, valID *hexutil.Big, reqID *hexutil.Big) (*types.WithdrawRequest, error) {
	// SQL query to find the withdrawal request
	query := `
        SELECT 
            request_trx, 
            request_id, 
            address, 
            staker_id, 
            created_time, 
            amount, 
            type, 
            withdraw_trx, 
            withdraw_time, 
            penalty
        FROM withdrawals
        WHERE address = $1 AND staker_id = $2 AND request_id = $3
    `

	// Execute the query
	row := db.db.QueryRow(query, addr.String(), valID.ToInt().Uint64(), reqID.ToInt().Uint64())

	// Decode the result into a WithdrawRequest struct
	var wr types.WithdrawRequest
	err := row.Scan(
		&wr.RequestTrx,
		&wr.WithdrawRequestID,
		&wr.Address,
		&wr.StakerID,
		&wr.CreatedTime,
		&wr.Amount,
		&wr.Type,
		&wr.WithdrawTrx,
		&wr.WithdrawTime,
		&wr.Penalty,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			db.log.Errorf("withdraw request [%s] of %s to #%d not found", reqID.String(), addr.String(), valID.ToInt().Uint64())
			return nil, fmt.Errorf("withdrawal request not found")
		}
		return nil, err
	}

	return &wr, nil
}

// // AddWithdrawal stores a withdrawal request in the database if it doesn't exist.
// func (db *MongoDbBridge) AddWithdrawal(wr *types.WithdrawRequest) error {
// 	// get the collection for withdrawals
// 	col := db.client.Database(db.dbName).Collection(colWithdrawals)

// 	// do we already know this withdraws request
// 	uni, err := db.isUniqueWithdrawRequest(col, wr)
// 	if err != nil {
// 		db.log.Errorf("can not proceed with withdraw request; %s", err.Error())
// 		return err
// 	}

// 	// non-unique requests will be updated instead
// 	if !uni {
// 		return db.UpdateWithdrawal(wr)
// 	}

// 	// try to do the insert
// 	if _, err := col.InsertOne(context.Background(), wr); err != nil {
// 		db.log.Criticalf("failed to store %s to %d, %s, %s; %s",
// 			wr.Address.String(),
// 			wr.StakerID.ToInt().Uint64(),
// 			wr.WithdrawRequestID.String(),
// 			wr.RequestTrx.String(), err.Error())
// 		return err
// 	}

// 	// make sure delegation collection is initialized
// 	if db.initWithdrawals != nil {
// 		db.initWithdrawals.Do(func() { db.initWithdrawalsCollection(col); db.initWithdrawals = nil })
// 	}
// 	return nil
// }

// AddWithdrawal stores a withdrawal request in the database if it doesn't exist.
func (db *PostgreSQLBridge) AddWithdrawal(wr *types.WithdrawRequest) error {
	// Begin a new transaction
	tx, err := db.db.Begin()
	if err != nil {
		db.log.Critical(err)
		return fmt.Errorf("failed to begin database transaction: %v", err)
	}
	defer tx.Rollback()

	// Check if the withdrawal request already exists
	exists, err := db.isUniqueWithdrawRequest(tx, wr)
	if err != nil {
		db.log.Errorf("cannot proceed with withdraw request; %s", err.Error())
		return err
	}

	// If the request is not unique, update it instead
	if !exists {
		if err := db.UpdateWithdrawal(tx, wr); err != nil {
			db.log.Critical(err)
			return fmt.Errorf("failed to update withdrawal request: %v", err)
		}
		return tx.Commit()
	}

	// Insert the new withdrawal request
	query := `
		INSERT INTO withdrawals (address, staker_id, withdraw_request_id, request_trx, created_time, amount, type, withdraw_trx, withdraw_time, penalty)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err = tx.Exec(
		query,
		wr.Address.String(),
		wr.StakerID.String(),
		wr.WithdrawRequestID.String(),
		wr.RequestTrx.String(),
		wr.CreatedTime,
		wr.Amount.String(),
		wr.Type,
		sql.NullString{String: wr.WithdrawTrx.String(), Valid: wr.WithdrawTrx != nil},
		sql.NullInt64{Int64: int64(*wr.WithdrawTime), Valid: wr.WithdrawTime != nil},
		sql.NullString{String: wr.Penalty.String(), Valid: wr.Penalty != nil},
	)
	if err != nil {
		db.log.Criticalf("failed to store %s to %d, %s, %s; %s",
			wr.Address.String(),
			wr.StakerID.ToInt().Uint64(),
			wr.WithdrawRequestID.String(),
			wr.RequestTrx.String(), err.Error())
		return err
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		db.log.Critical(err)
		return fmt.Errorf("failed to commit database transaction: %v", err)
	}

	return nil
}

// // isUniqueWithdrawRequest checks if the withdrawal request is unique
// // and if not, it tries to push the existing and closed request to a different ID
// // to keep the history even for repeated requests.
// func (db *MongoDbBridge) isUniqueWithdrawRequest(col *mongo.Collection, wr *types.WithdrawRequest) (bool, error) {
// 	// do we already know this withdraws request? if not than let it be saved
// 	if !db.isWithdrawalKnown(col, wr) {
// 		return true, nil
// 	}

// 	// we already know this withdraws request
// 	db.log.Infof("known withdraw by %s to #%d, request ID %s, by trx %s",
// 		wr.Address.String(),
// 		wr.StakerID.ToInt().Uint64(),
// 		wr.WithdrawRequestID.String(),
// 		wr.RequestTrx.String())

// 	// try to shift finalised withdraw request
// 	shifted, err := db.shiftClosedWithdrawRequest(col, wr)
// 	if err != nil {
// 		db.log.Errorf("withdrawal shift failed; %s", err.Error())
// 		return false, err
// 	}
// 	return shifted, nil
// }

// isUniqueWithdrawRequest checks if the withdrawal request is unique
// and if not, it tries to push the existing and closed request to a different ID
// to keep the history even for repeated requests.
func (db *PostgreSQLBridge) isUniqueWithdrawRequest(tx *sql.Tx, wr *types.WithdrawRequest) (bool, error) {
	// Check if the withdrawal request is already known
	if !db.isWithdrawalKnown(tx, wr) {
		return true, nil
	}

	// Log known withdrawal request
	db.log.Infof("known withdrawal by %s to #%d, request ID %s, by trx %s",
		wr.Address.String(),
		wr.StakerID.ToInt().Uint64(),
		wr.WithdrawRequestID.String(),
		wr.RequestTrx.String())

	// Try to shift finalized withdrawal request
	shifted, err := db.shiftClosedWithdrawRequest(tx, wr)
	if err != nil {
		db.log.Errorf("withdrawal shift failed; %s", err.Error())
		return false, err
	}

	return shifted, nil
}

// // shiftClosedWithdrawRequest updates a request ID of an existing withdrawal request to preserve requests
// // history if the withdrawal request is already closed.
// func (db *MongoDbBridge) shiftClosedWithdrawRequest(col *mongo.Collection, wr *types.WithdrawRequest) (bool, error) {
// 	// generate new ID
// 	reqID := (*hexutil.Big)(new(big.Int).SetBytes(wr.RequestTrx.Bytes()[:16])).String()

// 	// try to shift a closed withdrawal request to a different reqID by updating it in the database
// 	er, err := col.UpdateOne(context.Background(), bson.D{
// 		{Key: types.FiWithdrawalAddress, Value: wr.Address.String()},
// 		{Key: types.FiWithdrawalToValidator, Value: wr.StakerID.String()},
// 		{Key: types.FiWithdrawalRequestID, Value: wr.WithdrawRequestID.String()},
// 		{Key: types.FiWithdrawalFinTime, Value: bson.D{{Key: "$exists", Value: true}, {Key: "$ne", Value: nil}}},
// 	}, bson.D{{Key: "$set", Value: bson.D{
// 		{Key: types.FiWithdrawalRequestID, Value: reqID},
// 	}}})
// 	if err != nil {
// 		db.log.Criticalf("can not shift withdrawal; %s", err.Error())
// 		return false, err
// 	}

// 	// do we actually have the document updated? if not the request was not closed and can not be shifted safely
// 	if 0 == er.MatchedCount {
// 		db.log.Criticalf("miss in withdrawal shift of %s to #%d on req %s", wr.Address.String(), wr.StakerID.ToInt().Uint64(), wr.WithdrawRequestID.String())
// 		return false, nil
// 	}

// 	// shift successful, log what we did
// 	db.log.Infof("shifted withdrawal request ID %s to %s of delegation %s to %d",
// 		wr.WithdrawRequestID.String(),
// 		reqID,
// 		wr.Address.String(),
// 		wr.StakerID.ToInt().Uint64())
// 	return true, nil
// }

// shiftClosedWithdrawRequest updates a request ID of an existing withdrawal request to preserve requests
// history if the withdrawal request is already closed.
func (db *PostgreSQLBridge) shiftClosedWithdrawRequest(tx *sql.Tx, wr *types.WithdrawRequest) (bool, error) {
	// generate new ID
	reqID := (*hexutil.Big)(new(big.Int).SetBytes(wr.RequestTrx.Bytes()[:16])).String()

	// try to shift a closed withdrawal request to a different reqID by updating it in the database
	query := `
		UPDATE withdrawals
		SET withdraw_request_id = $1
		WHERE address = $2 AND staker_id = $3 AND withdraw_request_id = $4
		AND withdraw_time IS NOT NULL
	`
	result, err := tx.Exec(query, reqID, wr.Address.String(), wr.StakerID.String(), wr.WithdrawRequestID.String())
	if err != nil {
		db.log.Criticalf("cannot shift withdrawal; %s", err.Error())
		return false, err
	}

	// check if the document was actually updated
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		db.log.Criticalf("error checking rows affected; %s", err.Error())
		return false, err
	}

	if rowsAffected == 0 {
		db.log.Criticalf("miss in withdrawal shift of %s to #%d on req %s", wr.Address.String(), wr.StakerID.ToInt().Uint64(), wr.WithdrawRequestID.String())
		return false, nil
	}

	// shift successful, log what we did
	db.log.Infof("shifted withdrawal request ID %s to %s of delegation %s to %d",
		wr.WithdrawRequestID.String(),
		reqID,
		wr.Address.String(),
		wr.StakerID.ToInt().Uint64())
	return true, nil
}

// // UpdateWithdrawal updates the given withdraw request in database.
// func (db *MongoDbBridge) UpdateWithdrawal(wr *types.WithdrawRequest) error {
// 	// get the collection for withdrawals
// 	col := db.client.Database(db.dbName).Collection(colWithdrawals)

// 	// calculate the value to 9 digits (and 18 billions remain available)
// 	val := new(big.Int).Div(wr.Amount.ToInt(), types.WithdrawDecimalsCorrection).Uint64()

// 	// withdraw transaction
// 	var trx *string = nil
// 	if wr.WithdrawTrx != nil {
// 		t := wr.WithdrawTrx.String()
// 		trx = &t
// 	}

// 	// penalty amount
// 	var pen *string = nil
// 	if wr.Penalty != nil {
// 		p := wr.Penalty.String()
// 		pen = &p
// 	}

// 	// try to update a withdraw request by replacing it in the database
// 	// we use request ID identify unique withdrawal
// 	er, err := col.UpdateOne(context.Background(), bson.D{
// 		{Key: types.FiWithdrawalAddress, Value: wr.Address.String()},
// 		{Key: types.FiWithdrawalToValidator, Value: wr.StakerID.String()},
// 		{Key: types.FiWithdrawalRequestID, Value: wr.WithdrawRequestID.String()},
// 	}, bson.D{{Key: "$set", Value: bson.D{
// 		{Key: types.FiWithdrawalType, Value: wr.Type},
// 		{Key: types.FiWithdrawalOrdinal, Value: wr.OrdinalIndex()},
// 		{Key: types.FiWithdrawalCreated, Value: uint64(wr.CreatedTime)},
// 		{Key: types.FiWithdrawalStamp, Value: time.Unix(int64(wr.CreatedTime), 0)},
// 		{Key: types.FiWithdrawalValue, Value: val},
// 		{Key: types.FiWithdrawalSlash, Value: pen},
// 		{Key: types.FiWithdrawalRequestTrx, Value: wr.RequestTrx.String()},
// 		{Key: types.FiWithdrawalFinTrx, Value: trx},
// 		{Key: types.FiWithdrawalFinTime, Value: (*uint64)(wr.WithdrawTime)},
// 	}}}, new(options.UpdateOptions).SetUpsert(true))
// 	if err != nil {
// 		db.log.Critical(err)
// 		return err
// 	}

// 	// do we actually have the document
// 	if 0 == er.MatchedCount {
// 		return fmt.Errorf("can not update, the withdraw request not found in database")
// 	}
// 	return nil
// }

// UpdateWithdrawal updates the given withdrawal request in the database.
func (db *PostgreSQLBridge) UpdateWithdrawal(tx *sql.Tx, wr *types.WithdrawRequest) error {
	// calculate the value to 9 digits (and 18 billions remain available)
	val := new(big.Int).Div(wr.Amount.ToInt(), types.WithdrawDecimalsCorrection).Uint64()

	// withdraw transaction
	var trx *string = nil
	if wr.WithdrawTrx != nil {
		t := wr.WithdrawTrx.String()
		trx = &t
	}

	// penalty amount
	var pen *string = nil
	if wr.Penalty != nil {
		p := wr.Penalty.String()
		pen = &p
	}

	// try to update a withdraw request by replacing it in the database
	// we use request ID to identify unique withdrawal
	query := `
		INSERT INTO withdrawals (address, staker_id, withdraw_request_id, type, ordinal_index, created_time, created_stamp, value, penalty, request_trx, withdraw_trx, withdraw_time)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (address, staker_id, withdraw_request_id)
		DO UPDATE SET
			type = EXCLUDED.type,
			ordinal_index = EXCLUDED.ordinal_index,
			created_time = EXCLUDED.created_time,
			created_stamp = EXCLUDED.created_stamp,
			value = EXCLUDED.value,
			penalty = EXCLUDED.penalty,
			request_trx = EXCLUDED.request_trx,
			withdraw_trx = EXCLUDED.withdraw_trx,
			withdraw_time = EXCLUDED.withdraw_time
	`

	_, err := tx.Exec(
		query,
		wr.Address.String(),
		wr.StakerID.String(),
		wr.WithdrawRequestID.String(),
		wr.Type,
		wr.OrdinalIndex(),
		uint64(wr.CreatedTime),
		time.Unix(int64(wr.CreatedTime), 0),
		val,
		pen,
		wr.RequestTrx.String(),
		trx,
		(*uint64)(wr.WithdrawTime),
	)
	if err != nil {
		db.log.Critical(err)
		return err
	}

	return nil
}

// // isWithdrawalKnown checks if the given delegation exists in the database.
// func (db *MongoDbBridge) isWithdrawalKnown(col *mongo.Collection, wr *types.WithdrawRequest) bool {
// 	// try to find the delegation in the database
// 	sr := col.FindOne(context.Background(), bson.D{
// 		{Key: types.FiWithdrawalAddress, Value: wr.Address.String()},
// 		{Key: types.FiWithdrawalToValidator, Value: wr.StakerID.String()},
// 		{Key: types.FiWithdrawalRequestID, Value: wr.WithdrawRequestID.String()},
// 	}, options.FindOne().SetProjection(bson.D{
// 		{Key: types.FiWithdrawalPk, Value: true},
// 	}))

// 	// error on lookup?
// 	if sr.Err() != nil {
// 		// may be ErrNoDocuments, which we seek
// 		if sr.Err() == mongo.ErrNoDocuments {
// 			return false
// 		}

// 		// inform that we can not get the PK; should not happen
// 		db.log.Errorf("can not get existing withdraw request pk; %s", sr.Err().Error())
// 		return false
// 	}
// 	return true
// }

// isWithdrawalKnown checks if the given withdrawal request exists in the database.
func (db *PostgreSQLBridge) isWithdrawalKnown(tx *sql.Tx, wr *types.WithdrawRequest) bool {
	query := `
		SELECT 1 FROM withdrawals 
		WHERE address = $1 AND staker_id = $2 AND withdraw_request_id = $3
	`
	row := tx.QueryRow(query, wr.Address.String(), wr.StakerID.String(), wr.WithdrawRequestID.String())

	var exists int
	if err := row.Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return false
		}

		// Log any unexpected errors
		db.log.Errorf("error checking existing withdrawal request: %s", err.Error())
		return false
	}
	return true
}

// // WithdrawalCountFiltered calculates total number of withdraw requests in the database for the given filter.
// func (db *MongoDbBridge) WithdrawalCountFiltered(filter *bson.D) (uint64, error) {
// 	return db.CountFiltered(db.client.Database(db.dbName).Collection(colWithdrawals), filter)
// }

// WithdrawalCountFiltered calculates the total number of withdrawal requests in the database for the given filter.
func (db *PostgreSQLBridge) WithdrawalCountFiltered(filter string, args ...interface{}) (uint64, error) {
	// Build the base query
	query := `
		SELECT COUNT(*) 
		FROM withdrawals
		WHERE ` + filter

	// Execute the query
	var count uint64
	err := db.db.QueryRow(query, args...).Scan(&count)
	if err != nil {
		db.log.Critical(err)
		return 0, fmt.Errorf("failed to count filtered withdrawals: %v", err)
	}

	return count, nil
}

// // WithdrawalsCount calculates total number of withdraws in the database.
// func (db *MongoDbBridge) WithdrawalsCount() (uint64, error) {
// 	return db.EstimateCount(db.client.Database(db.dbName).Collection(colWithdrawals))
// }

func (db *PostgreSQLBridge) WithdrawalsCount() (int64, error) {
	// Define the query to count the rows in the 'withdrawals' table
	query := "SELECT COUNT(*) FROM withdrawals"

	// Execute the query and scan the result into a variable
	var count int64
	err := db.db.QueryRow(query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get withdrawals count: %w", err)
	}

	// Return the count as uint64
	return int64(count), nil
}

// // wrListInit initializes list of withdraw requests based on provided cursor, count, and filter.
// func (db *MongoDbBridge) wrListInit(col *mongo.Collection, cursor *string, count int32, filter *bson.D) (*types.WithdrawRequestList, error) {
// 	// make sure some filter is used
// 	if nil == filter {
// 		filter = &bson.D{}
// 	}

// 	// find how many transactions do we have in the database
// 	total, err := col.CountDocuments(context.Background(), *filter)
// 	if err != nil {
// 		db.log.Errorf("can not count withdraw requests")
// 		return nil, err
// 	}

// 	// make the list and notify the size of it
// 	db.log.Debugf("found %d filtered withdraw requests", total)
// 	list := types.WithdrawRequestList{
// 		Collection: make([]*types.WithdrawRequest, 0),
// 		Total:      uint64(total),
// 		First:      0,
// 		Last:       0,
// 		IsStart:    total == 0,
// 		IsEnd:      total == 0,
// 		Filter:     *filter,
// 	}

// 	// is the list non-empty? return the list with properly calculated range marks
// 	if 0 < total {
// 		return db.wrListCollectRangeMarks(col, &list, cursor, count)
// 	}

// 	// this is an empty list
// 	db.log.Debug("empty withdraw requests list created")
// 	return &list, nil
// }

func (db *PostgreSQLBridge) wrListInit(cursor *string, count int32, filter map[string]interface{}) (*types.PostWithdrawRequestList, error) {
	// Default filter if none provided
	if filter == nil {
		filter = make(map[string]interface{})
	}

	// Construct the WHERE clause and parameters from the filter
	whereClause, params := db.constructWhereClause(filter)

	// SQL query to count total withdrawals matching the filter
	query := fmt.Sprintf("SELECT COUNT(*) FROM withdrawals %s", whereClause)

	// Execute the query to count total records
	var total uint64
	err := db.db.QueryRow(query, params...).Scan(&total)
	if err != nil {
		db.log.Errorf("cannot count withdrawal requests: %v", err)
		return nil, err
	}

	// Log and initialize the withdrawal request list
	db.log.Debugf("found %d filtered withdrawal requests", total)
	list := &types.PostWithdrawRequestList{
		Collection: make([]*types.WithdrawRequest, 0),
		Total:      total,
		First:      0,
		Last:       0,
		IsStart:    total == 0,
		IsEnd:      total == 0,
		Filter:     filter, // Store the filter for future use
	}

	// If the list is non-empty, collect the range marks
	if total > 0 {
		return db.wrListCollectRangeMarks(list, cursor, count, whereClause, params...)
	}

	// Log and return the empty list
	db.log.Debug("empty withdrawal requests list created")
	return list, nil
}

// constructWhereClause constructs a WHERE clause from the provided filter map.
func (db *PostgreSQLBridge) constructWhereClause(filter map[string]interface{}) (string, []interface{}) {
	var clauses []string
	var params []interface{}
	i := 1

	for key, value := range filter {
		clauses = append(clauses, fmt.Sprintf("%s = $%d", key, i))
		params = append(params, value)
		i++
	}

	whereClause := ""
	if len(clauses) > 0 {
		whereClause = "WHERE " + strings.Join(clauses, " AND ")
	}

	return whereClause, params
}

// // wrListCollectRangeMarks returns the list of withdraw requests with proper First/Last marks.
// func (db *MongoDbBridge) wrListCollectRangeMarks(col *mongo.Collection, list *types.WithdrawRequestList, cursor *string, count int32) (*types.WithdrawRequestList, error) {
// 	var err error

// 	// find out the cursor ordinal index
// 	if cursor == nil && count > 0 {
// 		// get the highest available pk
// 		list.First, err = db.wrListBorderPk(col,
// 			list.Filter,
// 			options.FindOne().SetSort(bson.D{{Key: types.FiWithdrawalOrdinal, Value: -1}}))
// 		list.IsStart = true

// 	} else if cursor == nil && count < 0 {
// 		// get the lowest available pk
// 		list.First, err = db.wrListBorderPk(col,
// 			list.Filter,
// 			options.FindOne().SetSort(bson.D{{Key: types.FiWithdrawalOrdinal, Value: 1}}))
// 		list.IsEnd = true

// 	} else if cursor != nil {
// 		// the cursor itself is the starting point
// 		list.First, err = db.wrListBorderPk(col,
// 			bson.D{{Key: types.FiWithdrawalPk, Value: *cursor}},
// 			options.FindOne())
// 	}

// 	// check the error
// 	if err != nil {
// 		db.log.Errorf("can not find the initial withdraw request")
// 		return nil, err
// 	}

// 	// inform what we are about to do
// 	db.log.Debugf("withdraw requests list initialized with PK %s", list.First)
// 	return list, nil
// }

// wrListCollectRangeMarks returns the list of withdraw requests with proper First/Last marks for PostgreSQL.
func (db *PostgreSQLBridge) wrListCollectRangeMarks(list *types.PostWithdrawRequestList, cursor *string, count int32, filter string, args ...interface{}) (*types.PostWithdrawRequestList, error) {
	var query string
	var err error

	// Determine the query based on the cursor and count
	if cursor == nil && count > 0 {
		// Get the highest ordinal (last in descending order)
		query = `
            SELECT ordinal
            FROM withdrawals
            WHERE ` + filter + `
            ORDER BY ordinal DESC
            LIMIT 1`
	} else if cursor == nil && count < 0 {
		// Get the lowest ordinal (first in ascending order)
		query = `
            SELECT ordinal
            FROM withdrawals
            WHERE ` + filter + `
            ORDER BY ordinal ASC
            LIMIT 1`
	} else if cursor != nil {
		// Cursor itself is the starting point
		query = `
            SELECT ordinal
            FROM withdrawals
            WHERE pk = $1`
		args = append([]interface{}{*cursor}, args...)
	}

	// Execute the query
	err = db.db.QueryRow(query, args...).Scan(&list.First)
	if err != nil {
		if err == sql.ErrNoRows {
			db.log.Errorf("withdraw request not found")
			return nil, fmt.Errorf("withdraw request not found")
		}
		db.log.Errorf("failed to find the initial withdraw request; %s", err.Error())
		return nil, err
	}

	// Update start/end flags based on the query
	if cursor == nil && count > 0 {
		list.IsStart = true
	} else if cursor == nil && count < 0 {
		list.IsEnd = true
	}

	db.log.Debugf("withdraw requests list initialized with PK %d", list.First)
	return list, nil
}

// // wrListBorderPk finds the top PK of the withdrawal requests collection based on given filter and options.
// func (db *MongoDbBridge) wrListBorderPk(col *mongo.Collection, filter bson.D, opt *options.FindOneOptions) (uint64, error) {
// 	// prep container
// 	var row struct {
// 		Value uint64 `bson:"orx"`
// 	}

// 	// make sure we pull only what we need
// 	opt.SetProjection(bson.D{{Key: types.FiWithdrawalOrdinal, Value: true}})
// 	sr := col.FindOne(context.Background(), filter, opt)

// 	// try to decode
// 	if err := sr.Decode(&row); err != nil {
// 		return 0, err
// 	}
// 	return row.Value, nil
// }

// wrListBorderPk finds the top PK of the withdrawal requests collection based on the given filter and sort direction.
func (db *PostgreSQLBridge) wrListBorderPk(filter string, args []interface{}, sortDirection string) (uint64, error) {
	// SQL query to get the top PK (ordinal)
	query := fmt.Sprintf(`
        SELECT ordinal
        FROM withdrawals
        WHERE %s
        ORDER BY ordinal %s
        LIMIT 1
    `, filter, sortDirection)

	// Execute the query
	var ordinal uint64
	err := db.db.QueryRow(query, args...).Scan(&ordinal)
	if err != nil {
		if err == sql.ErrNoRows {
			db.log.Debugf("no withdrawal request found for the given filter")
			return 0, fmt.Errorf("no withdrawal request found")
		}
		db.log.Errorf("failed to find border PK for withdrawal requests: %s", err.Error())
		return 0, err
	}

	return ordinal, nil
}

// // wrListFilter creates a filter for withdraw requests list loading.
// func (db *MongoDbBridge) wrListFilter(cursor *string, count int32, list *types.WithdrawRequestList) *bson.D {
// 	// build an extended filter for the query; add PK (decoded cursor) to the original filter
// 	if cursor == nil {
// 		if count > 0 {
// 			list.Filter = append(list.Filter, bson.E{Key: types.FiWithdrawalOrdinal, Value: bson.D{{Key: "$lte", Value: list.First}}})
// 		} else {
// 			list.Filter = append(list.Filter, bson.E{Key: types.FiWithdrawalOrdinal, Value: bson.D{{Key: "$gte", Value: list.First}}})
// 		}
// 	} else {
// 		if count > 0 {
// 			list.Filter = append(list.Filter, bson.E{Key: types.FiWithdrawalOrdinal, Value: bson.D{{Key: "$lt", Value: list.First}}})
// 		} else {
// 			list.Filter = append(list.Filter, bson.E{Key: types.FiWithdrawalOrdinal, Value: bson.D{{Key: "$gt", Value: list.First}}})
// 		}
// 	}

// 	// return the new filter
// 	return &list.Filter
// }

// wrListFilter creates a WHERE clause for withdrawal requests list loading in PostgreSQL.
func (db *PostgreSQLBridge) wrListFilter(cursor *string, count int32, list *types.PostWithdrawRequestList) (string, []interface{}) {
	var filterClauses []string
	var args []interface{}
	paramIndex := 1 // Index for SQL placeholders ($1, $2, etc.)

	// Start with the base filter from the list
	for key, value := range list.Filter {
		filterClauses = append(filterClauses, fmt.Sprintf("%s = $%d", key, paramIndex))
		args = append(args, value)
		paramIndex++
	}

	// Add the ordinal filter based on the cursor and count
	if cursor == nil {
		if count > 0 {
			filterClauses = append(filterClauses, fmt.Sprintf("ordinal <= $%d", paramIndex))
		} else {
			filterClauses = append(filterClauses, fmt.Sprintf("ordinal >= $%d", paramIndex))
		}
		args = append(args, list.First)
	} else {
		if count > 0 {
			filterClauses = append(filterClauses, fmt.Sprintf("ordinal < $%d", paramIndex))
		} else {
			filterClauses = append(filterClauses, fmt.Sprintf("ordinal > $%d", paramIndex))
		}
		args = append(args, list.First)
	}

	// Combine all filter clauses into a single WHERE clause
	whereClause := ""
	if len(filterClauses) > 0 {
		whereClause = "WHERE " + strings.Join(filterClauses, " AND ")
	}

	return whereClause, args
}

// // wrListOptions creates a filter options set for withdraw requests list search.
// func (db *MongoDbBridge) wrListOptions(count int32) *options.FindOptions {
// 	// prep options
// 	opt := options.Find()

// 	// how to sort results in the collection
// 	// from high (new) to low (old) by default; reversed if loading from bottom
// 	sd := -1
// 	if count < 0 {
// 		sd = 1
// 	}

// 	// sort with the direction we want
// 	opt.SetSort(bson.D{{Key: types.FiWithdrawalOrdinal, Value: sd}})

// 	// prep the loading limit
// 	var limit = int64(count)
// 	if limit < 0 {
// 		limit = -limit
// 	}

// 	// apply the limit, try to get one more record so we can detect list end
// 	opt.SetLimit(limit + 1)
// 	return opt
// }

// wrListOptions creates an ORDER BY and LIMIT clause for withdrawal requests list search in PostgreSQL.
func (db *PostgreSQLBridge) wrListOptions(count int32) (string, int64) {
	// Determine the sort direction
	sortDirection := "DESC"
	if count < 0 {
		sortDirection = "ASC"
	}

	// Determine the limit, ensure it's positive
	limit := int64(count)
	if limit < 0 {
		limit = -limit
	}

	// Return the ORDER BY clause and limit
	orderByClause := fmt.Sprintf("ORDER BY ordinal %s", sortDirection)
	return orderByClause, limit + 1 // Add one to detect list end
}

// // wrListLoad load the initialized list of withdraw requests from database.
// func (db *MongoDbBridge) wrListLoad(col *mongo.Collection, cursor *string, count int32, list *types.WithdrawRequestList) (err error) {
// 	// get the context for loader
// 	ctx := context.Background()

// 	// load the data
// 	ld, err := col.Find(ctx, db.wrListFilter(cursor, count, list), db.wrListOptions(count))
// 	if err != nil {
// 		db.log.Errorf("error loading with requests list; %s", err.Error())
// 		return err
// 	}

// 	// close the cursor as we leave
// 	defer db.closeCursor(ld)

// 	// loop and load the list; we may not store the last value
// 	var wr *types.WithdrawRequest
// 	for ld.Next(ctx) {
// 		// append a previous value to the list, if we have one
// 		if wr != nil {
// 			list.Collection = append(list.Collection, wr)
// 		}

// 		// try to decode the next row
// 		var row types.WithdrawRequest
// 		if err = ld.Decode(&row); err != nil {
// 			db.log.Errorf("can not decode the withdraw request list row; %s", err.Error())
// 			return err
// 		}

// 		// use this row as the next item
// 		wr = &row
// 	}

// 	// we should have all the items already; we may just need to check if a boundary was reached
// 	list.IsEnd = (cursor == nil && count < 0) || (count > 0 && int32(len(list.Collection)) < count)
// 	list.IsStart = (cursor == nil && count > 0) || (count < 0 && int32(len(list.Collection)) < -count)

// 	// add the last item as well if we hit the boundary
// 	if (list.IsStart || list.IsEnd) && wr != nil {
// 		list.Collection = append(list.Collection, wr)
// 	}
// 	return nil
// }

// wrListLoad loads the initialized list of withdraw requests from the PostgreSQL database.
func (db *PostgreSQLBridge) wrListLoad(cursor *string, count int32, list *types.PostWithdrawRequestList) error {
	// Construct the WHERE clause and parameters
	whereClause, args := db.wrListFilter(cursor, count, list)

	// Get the ORDER BY and LIMIT clauses
	orderByClause, limit := db.wrListOptions(count)

	// Construct the SQL query
	query := fmt.Sprintf(`
        SELECT request_trx, request_id, address, staker_id, created_time, amount, type, withdraw_trx, withdraw_time, penalty, ordinal
        FROM withdrawals
        %s
        %s
        LIMIT $%d
    `, whereClause, orderByClause, len(args)+1)

	// Append the limit to the arguments
	args = append(args, limit)

	// Execute the query
	rows, err := db.db.Query(query, args...)
	if err != nil {
		db.log.Errorf("error loading withdrawal requests list; %s", err.Error())
		return err
	}
	defer rows.Close()

	// Loop and load the list
	var wr *types.WithdrawRequest
	for rows.Next() {
		if wr != nil {
			list.Collection = append(list.Collection, wr)
		}

		// Decode the current row into a WithdrawRequest struct
		var row types.WithdrawRequest
		err := rows.Scan(
			&row.RequestTrx,
			&row.WithdrawRequestID,
			&row.Address,
			&row.StakerID,
			&row.CreatedTime,
			&row.Amount,
			&row.Type,
			&row.WithdrawTrx,
			&row.WithdrawTime,
			&row.Penalty,
			&list.First, // To track the ordinal
		)
		if err != nil {
			db.log.Errorf("cannot decode the withdrawal request list row; %s", err.Error())
			return err
		}

		// Use this row as the next item
		wr = &row
	}

	// Handle boundary checks
	list.IsEnd = (cursor == nil && count < 0) || (count > 0 && int32(len(list.Collection)) < count)
	list.IsStart = (cursor == nil && count > 0) || (count < 0 && int32(len(list.Collection)) < -count)

	// Add the last item as well if we hit the boundary
	if (list.IsStart || list.IsEnd) && wr != nil {
		list.Collection = append(list.Collection, wr)
	}

	return nil
}

// // Withdrawals pulls list of withdraw requests starting at the specified cursor.
// func (db *MongoDbBridge) Withdrawals(cursor *string, count int32, filter *bson.D) (*types.WithdrawRequestList, error) {
// 	// nothing to load?
// 	if count == 0 {
// 		return nil, fmt.Errorf("nothing to do, zero withdrawals requested")
// 	}

// 	// get the collection and context
// 	col := db.client.Database(db.dbName).Collection(colWithdrawals)

// 	// init the list
// 	list, err := db.wrListInit(col, cursor, count, filter)
// 	if err != nil {
// 		db.log.Errorf("can not build withdraw requests list; %s", err.Error())
// 		return nil, err
// 	}

// 	// load data if there are any
// 	if list.Total > 0 {
// 		err = db.wrListLoad(col, cursor, count, list)
// 		if err != nil {
// 			db.log.Errorf("can not load withdraw requests list from database; %s", err.Error())
// 			return nil, err
// 		}

// 		// reverse on negative so new-er delegations will be on top
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

// Withdrawals pulls list of withdraw requests starting at the specified cursor.
func (db *PostgreSQLBridge) Withdrawals(cursor *string, count int32, filter map[string]interface{}) (*types.PostWithdrawRequestList, error) {
	// Nothing to load?
	if count == 0 {
		return nil, fmt.Errorf("nothing to do, zero withdrawals requested")
	}

	// Initialize the list
	list, err := db.wrListInit(cursor, count, filter)
	if err != nil {
		db.log.Errorf("cannot build withdrawal requests list; %s", err.Error())
		return nil, err
	}

	// Load data if there are any
	if list.Total > 0 {
		err = db.wrListLoad(cursor, count, list)
		if err != nil {
			db.log.Errorf("cannot load withdrawal requests list from database; %s", err.Error())
			return nil, err
		}

		// Reverse on negative count so newer withdrawals will be on top
		if count < 0 {
			list.Reverse()
			count = -count
		}

		// Cut the end if we have one extra record
		if len(list.Collection) > int(count) {
			list.Collection = list.Collection[:len(list.Collection)-1]
		}
	}

	return list, nil
}

// WithdrawalsSumValue calculates sum of values for all the withdrawals by a filter.
// func (db *MongoDbBridge) WithdrawalsSumValue(filter *bson.D) (*big.Int, error) {
// 	return db.sumFieldValue(
// 		db.client.Database(db.dbName).Collection(colWithdrawals),
// 		types.FiWithdrawalValue,
// 		filter,
// 		types.WithdrawDecimalsCorrection)
// }

// WithdrawalsSumValue calculates the sum of values for all the withdrawals by a filter.
func (db *PostgreSQLBridge) WithdrawalsSumValue(filter map[string]interface{}) (*big.Int, error) {
	// Construct the WHERE clause and parameters
	whereClause, args := db.constructWhereClause(filter)

	// SQL query to calculate the sum of withdrawal values
	query := fmt.Sprintf(`
        SELECT COALESCE(SUM(value), 0) AS total
        FROM withdrawals
        %s
    `, whereClause)

	// Execute the query
	var sumValue string
	err := db.db.QueryRow(query, args...).Scan(&sumValue)
	if err != nil {
		db.log.Errorf("failed to calculate sum of withdrawal values: %v", err)
		return nil, err
	}

	// Convert the sum value to a big.Int
	sumBigInt := new(big.Int)
	_, ok := sumBigInt.SetString(sumValue, 10)
	if !ok {
		return nil, fmt.Errorf("failed to convert sum value to big.Int")
	}

	// Apply the decimal correction if necessary
	if types.WithdrawDecimalsCorrection != nil {
		sumBigInt.Div(sumBigInt, types.WithdrawDecimalsCorrection)
	}

	return sumBigInt, nil
}

// // sumFieldValue calculates sum of values for specified field of a specified collection by a given filter.
// func (db *MongoDbBridge) sumFieldValue(col *mongo.Collection, field string, filter *bson.D, decCorrection *big.Int) (*big.Int, error) {
// 	// make sure we have at least some filter
// 	if filter == nil {
// 		filter = &bson.D{}
// 	}
// 	// construct aggregate column name
// 	var sb strings.Builder
// 	sb.WriteString("$")
// 	sb.WriteString(field)

// 	// get the collection
// 	cr, err := col.Aggregate(context.Background(), mongo.Pipeline{
// 		{{Key: "$match", Value: filter}},
// 		{{Key: "$group", Value: bson.D{
// 			{Key: "_id", Value: nil},
// 			{Key: "total", Value: bson.D{{Key: "$sum", Value: sb.String()}}},
// 		}}},
// 	})
// 	if err != nil {
// 		db.log.Errorf("can not calculate withdrawal sum value; %s", err.Error())
// 		return nil, err
// 	}
// 	// read the data and return result
// 	return db.readAggregatedSumFieldValue(cr, decCorrection)
// }

// sumFieldValue calculates the sum of values for a specified field in a specified table by a given filter.
func (db *PostgreSQLBridge) sumFieldValue(table string, field string, filter map[string]interface{}, decCorrection *big.Int) (*big.Int, error) {
	// Construct the WHERE clause and parameters
	whereClause, args := db.constructWhereClause(filter)

	// SQL query to calculate the sum of the specified field
	query := fmt.Sprintf(`
        SELECT COALESCE(SUM(%s), 0) AS total
        FROM %s
        %s
    `, field, table, whereClause)

	// Execute the query
	var sumValue string
	err := db.db.QueryRow(query, args...).Scan(&sumValue)
	if err != nil {
		db.log.Errorf("failed to calculate sum for field %s in table %s: %v", field, table, err)
		return nil, err
	}

	// Convert the sum value to a big.Int
	sumBigInt := new(big.Int)
	_, ok := sumBigInt.SetString(sumValue, 10)
	if !ok {
		return nil, fmt.Errorf("failed to convert sum value to big.Int")
	}

	// Apply the decimals correction if necessary
	if decCorrection != nil {
		sumBigInt.Div(sumBigInt, decCorrection)
	}

	return sumBigInt, nil
}

// // readAggregatedSumFieldValue extract the aggregated value from the given result set.
// func (db *MongoDbBridge) readAggregatedSumFieldValue(cr *mongo.Cursor, decCorrection *big.Int) (*big.Int, error) {
// 	// make sure to close the cursor after we got the data
// 	defer db.closeCursor(cr)

// 	// do we have any data to read?
// 	if !cr.Next(context.Background()) {
// 		return new(big.Int), nil
// 	}

// 	// try to get the calculated value
// 	var row struct {
// 		Total uint64 `bson:"total"`
// 	}
// 	if err := cr.Decode(&row); err != nil {
// 		db.log.Errorf("can not read withdrawal sum value; %s", err.Error())
// 		return nil, err
// 	}

// 	// correct decimals?
// 	if nil != decCorrection {
// 		return new(big.Int).Mul(new(big.Int).SetUint64(row.Total), decCorrection), nil
// 	}
// 	return new(big.Int).SetUint64(row.Total), nil
// }

// readAggregatedSumFieldValue extracts the aggregated value from the result set.
func (db *PostgreSQLBridge) readAggregatedSumFieldValue(query string, args []interface{}, decCorrection *big.Int) (*big.Int, error) {
	// Execute the query
	var sumValue string
	err := db.db.QueryRow(query, args...).Scan(&sumValue)
	if err != nil {
		if err == sql.ErrNoRows {
			return new(big.Int), nil // No rows, return zero
		}
		db.log.Errorf("cannot read aggregated sum value: %s", err.Error())
		return nil, err
	}

	// Convert the sum value to a big.Int
	sumBigInt := new(big.Int)
	_, ok := sumBigInt.SetString(sumValue, 10)
	if !ok {
		return nil, fmt.Errorf("failed to convert sum value to big.Int")
	}

	// Apply the decimal correction if necessary
	if decCorrection != nil {
		sumBigInt.Mul(sumBigInt, decCorrection)
	}

	return sumBigInt, nil
}
