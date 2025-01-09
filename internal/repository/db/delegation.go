// Package db implements bridge to persistent storage represented by Mongo database.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"ncogearthchain-api-graphql/internal/types"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// colDelegations represents the name of the delegations collection
const colDelegations = "delegations"

// ErrUnknownDelegation represents an error given on an unknown delegation update attempt.
var ErrUnknownDelegation = fmt.Errorf("unknown delegation")

// initDelegationCollection initializes the delegation collection with
// indexes and additional parameters needed by the app.
func (db *MongoDbBridge) initDelegationCollection(col *mongo.Collection) {
	// prepare index models
	ix := make([]mongo.IndexModel, 0)

	// index delegation address and the validator; this is how we find a specific unique delegation
	unique := true
	ix = append(ix, mongo.IndexModel{
		Keys: bson.D{{Key: types.FiDelegationAddress, Value: 1}, {Key: types.FiDelegationToValidator, Value: 1}},
		Options: &options.IndexOptions{
			Unique: &unique,
		},
	})

	// index delegator, receiving validator, and creation time stamp
	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: types.FiDelegationAddress, Value: 1}}})
	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: types.FiDelegationToValidator, Value: 1}}})
	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: types.FiDelegationOrdinal, Value: -1}}})
	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: types.FiDelegationStamp, Value: -1}}})

	// create indexes
	if _, err := col.Indexes().CreateMany(context.Background(), ix); err != nil {
		db.log.Panicf("can not create indexes for delegation collection; %s", err.Error())
	}

	// log we're done that
	db.log.Debugf("delegation collection initialized")
}

// initDelegationCollection initializes the delegation table with
// indexes and additional parameters needed by the app.
func (db *PostgreSQLBridge) initDelegationCollection() error {
	// Prepare index creation queries
	indexQueries := []string{
		// Create unique index on delegation address and validator
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_delegation_address_validator ON delegations (delegation_address, validator_address);`,

		// Create index on delegator address
		`CREATE INDEX IF NOT EXISTS idx_delegation_address ON delegations (delegation_address);`,

		// Create index on receiving validator address
		`CREATE INDEX IF NOT EXISTS idx_delegation_validator ON delegations (validator_address);`,

		// Create index on delegation ordinal (descending order)
		`CREATE INDEX IF NOT EXISTS idx_delegation_ordinal ON delegations (ordinal DESC);`,

		// Create index on delegation creation timestamp (descending order)
		`CREATE INDEX IF NOT EXISTS idx_delegation_stamp ON delegations (timestamp DESC);`,
	}

	// Execute index creation queries
	for _, query := range indexQueries {
		_, err := db.db.Exec(query)
		if err != nil {
			db.log.Errorf("Error creating index: %s", err.Error())
			return fmt.Errorf("failed to create index: %v", err)
		}
	}

	// Log that indexes have been created
	db.log.Debugf("Delegation table indexes initialized")

	return nil
}

// Delegation returns details of a delegation from an address to a validator ID.
func (db *MongoDbBridge) Delegation(addr *common.Address, valID *hexutil.Big) (*types.Delegation, error) {
	// get the collection for delegations
	col := db.client.Database(db.dbName).Collection(colDelegations)

	// try to find the delegation in the database
	sr := col.FindOne(context.Background(), bson.D{
		{Key: types.FiDelegationAddress, Value: addr.String()},
		{Key: types.FiDelegationToValidator, Value: valID.String()},
	})

	// do we have the data?
	if sr.Err() != nil {
		if sr.Err() == mongo.ErrNoDocuments {
			db.log.Errorf("delegation %s to #%d not found", addr.String(), valID.ToInt().Uint64())
			return nil, ErrUnknownDelegation
		}
		return nil, sr.Err()
	}

	// try to decode
	var dlg types.Delegation
	if err := sr.Decode(&dlg); err != nil {
		return nil, err
	}
	return &dlg, nil
}

// Delegation returns details of a delegation from an address to a validator ID.
func (db *PostgreSQLBridge) Delegation(addr *common.Address, valID *hexutil.Big) (*types.Delegation, error) {
	// Prepare the SQL query to fetch the delegation details
	query := `
        SELECT id, trx, delegation_address, to_staker_id, to_staker_address, created_time, ordinal_index, 
               amount_staked, amount_delegated 
        FROM delegations 
        WHERE delegation_address = $1 AND to_staker_id = $2
    `

	// Execute the query
	row := db.db.QueryRowContext(context.Background(), query, addr.String(), valID.String())

	// Prepare a struct to store the result
	var dlg types.Delegation

	// Scan the result into the Delegation struct
	err := row.Scan(&dlg.ID, &dlg.Transaction, &dlg.Address, &dlg.ToStakerId, &dlg.ToStakerAddress,
		&dlg.CreatedTime, &dlg.Index, &dlg.AmountStaked, &dlg.AmountDelegated)
	if err != nil {
		if err == sql.ErrNoRows {
			// Handle case when no delegation is found
			db.log.Errorf("delegation %s to #%d not found", addr.String(), valID.ToInt().Uint64())
			return nil, ErrUnknownDelegation
		}
		// Handle other errors
		return nil, fmt.Errorf("failed to query delegation: %v", err)
	}

	// Return the delegation details
	return &dlg, nil
}

// AddDelegation stores a delegation in the database if it doesn't exist.
func (db *MongoDbBridge) AddDelegation(dl *types.Delegation) error {
	// get the collection for delegations
	col := db.client.Database(db.dbName).Collection(colDelegations)

	// if the delegation already exists, update it with the new data
	if db.isDelegationKnown(col, dl) {
		return db.UpdateDelegation(dl)
	}

	// try to do the insert
	if _, err := col.InsertOne(context.Background(), dl); err != nil {
		db.log.Criticalf("can not add delegation %s to %d; %s", dl.Address.String(), dl.ToStakerId.ToInt().Uint64(), err.Error())
		return err
	}

	// make sure delegation collection is initialized
	if db.initDelegations != nil {
		db.initDelegations.Do(func() { db.initDelegationCollection(col); db.initDelegations = nil })
	}
	return nil
}

// AddDelegation stores a delegation in the PostgreSQL database if it doesn't exist.
func (db *PostgreSQLBridge) AddDelegation(dl *types.Delegation) error {
	// Check if the delegation already exists in the database
	exists, err := db.isDelegationKnown(dl)
	if err != nil {
		return err
	}

	// If the delegation already exists, update it with the new data
	if exists {
		return db.UpdateDelegation(dl)
	}

	// Insert the new delegation into the database
	query := `
        INSERT INTO delegations (delegation_address, to_staker_id, to_staker_address, 
                                 created_time, ordinal_index, amount_staked, amount_delegated)
        VALUES ($1, $2, $3, $4, $5, $6, $7)
        RETURNING id
    `

	// Execute the insert query
	var id string
	err = db.db.QueryRowContext(context.Background(), query, dl.Address.String(), dl.ToStakerId.String(), dl.ToStakerAddress.String(),
		dl.CreatedTime, dl.Index, dl.AmountStaked.String(), dl.AmountDelegated.String()).Scan(&id)
	if err != nil {
		db.log.Criticalf("cannot add delegation %s to %d; %s", dl.Address.String(), dl.ToStakerId.ToInt().Uint64(), err.Error())
		return fmt.Errorf("failed to insert delegation: %v", err)
	}

	// Optionally log the insertion or do other tasks
	db.log.Debugf("delegation %s to %d added with ID %s", dl.Address.String(), dl.ToStakerId.ToInt().Uint64(), id)

	return nil
}

// UpdateDelegation updates the given delegation in database.
func (db *MongoDbBridge) UpdateDelegation(dl *types.Delegation) error {
	// get the collection for delegations
	col := db.client.Database(db.dbName).Collection(colDelegations)

	// calculate the value to 9 digits (and 18 billions remain available)
	val := new(big.Int).Div(dl.AmountDelegated.ToInt(), types.DelegationDecimalsCorrection).Uint64()

	// notify
	db.log.Debugf("updating delegation %s to #%d value to %d",
		dl.Address.String(), dl.ToStakerId.ToInt().Uint64(), val)

	// try to update a delegation by replacing it in the database
	// we use address and validator ID to identify unique delegation
	er, err := col.UpdateOne(context.Background(), bson.D{
		{Key: types.FiDelegationAddress, Value: dl.Address.String()},
		{Key: types.FiDelegationToValidator, Value: dl.ToStakerId.String()},
	}, bson.D{{Key: "$set", Value: bson.D{
		{Key: types.FiDelegationOrdinal, Value: dl.OrdinalIndex()},
		{Key: types.FiDelegationStamp, Value: time.Unix(int64(dl.CreatedTime), 0)},
		{Key: types.FiDelegationTransaction, Value: dl.Transaction.String()},
		{Key: types.FiDelegationToValidatorAddress, Value: dl.ToStakerAddress.String()},
		{Key: types.FiDelegationAmountActive, Value: dl.AmountDelegated.String()},
		{Key: types.FiDelegationValue, Value: val},
	}}}, new(options.UpdateOptions).SetUpsert(true))
	if err != nil {
		db.log.Critical(err)
		return err
	}

	// do we actually have the document
	if 0 == er.MatchedCount {
		db.log.Errorf("delegation %s to %d not found", dl.Address.String(), dl.ToStakerId.ToInt().Uint64())
	}

	// make sure delegation collection is initialized
	if db.initDelegations != nil {
		db.initDelegations.Do(func() { db.initDelegationCollection(col); db.initDelegations = nil })
	}
	return nil
}

// UpdateDelegation updates the given delegation in PostgreSQL.
func (db *PostgreSQLBridge) UpdateDelegation(dl *types.Delegation) error {
	// Calculate the value to 9 digits (and 18 billions remain available)
	val := new(big.Int).Div(dl.AmountDelegated.ToInt(), types.DelegationDecimalsCorrection).Uint64()

	// Log the update operation
	db.log.Debugf("updating delegation %s to #%d value to %d",
		dl.Address.String(), dl.ToStakerId.ToInt().Uint64(), val)

	// Prepare the SQL query for updating the delegation
	query := `
        INSERT INTO delegations (delegation_address, to_staker_id, to_staker_address, created_time, ordinal_index, 
                                 amount_delegated, amount_active, transaction, value)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
        ON CONFLICT (delegation_address, to_staker_id) DO UPDATE
        SET ordinal_index = $5, created_time = $4, transaction = $8, to_staker_address = $3,
            amount_active = $7, value = $9
    `

	// Execute the query (upsert operation)
	_, err := db.db.ExecContext(
		context.Background(), query,
		dl.Address.String(), dl.ToStakerId.String(), dl.ToStakerAddress.String(),
		time.Unix(int64(dl.CreatedTime), 0), dl.OrdinalIndex(),
		dl.AmountDelegated.String(), dl.AmountStaked.String(), dl.Transaction.String(), val,
	)
	if err != nil {
		db.log.Criticalf("failed to update delegation %s to %d; %s", dl.Address.String(), dl.ToStakerId.ToInt().Uint64(), err.Error())
		return fmt.Errorf("failed to update delegation: %v", err)
	}

	// Check if the delegation was updated or inserted
	db.log.Debugf("delegation %s to %d successfully updated or inserted", dl.Address.String(), dl.ToStakerId.ToInt().Uint64())

	return nil
}

// UpdateDelegationBalance updates the given delegation active balance in database to the given amount.
func (db *MongoDbBridge) UpdateDelegationBalance(addr *common.Address, valID *hexutil.Big, amo *hexutil.Big) error {
	// get the collection for delegations
	col := db.client.Database(db.dbName).Collection(colDelegations)
	val := new(big.Int).Div(amo.ToInt(), types.DelegationDecimalsCorrection).Uint64()

	// notify
	db.log.Debugf("%s delegation to #%d value changed to %d", addr.String(), valID.ToInt().Uint64(), val)

	// update the transaction details
	ur, err := col.UpdateOne(context.Background(),
		bson.D{
			{Key: types.FiDelegationAddress, Value: addr.String()},
			{Key: types.FiDelegationToValidator, Value: valID.String()},
		},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: types.FiDelegationAmountActive, Value: amo.String()},
			{Key: types.FiDelegationValue, Value: val},
		}}})
	if err != nil {
		// log the issue
		db.log.Criticalf("delegation balance can not be updated; %s", err.Error())
		return err
	}

	// any match?
	if ur.MatchedCount == 0 {
		db.log.Errorf("delegation %s to %d not found", addr.String(), valID.ToInt().Uint64())
		return ErrUnknownDelegation
	}
	return nil
}

// UpdateDelegationBalance updates the given delegation active balance in the database to the given amount.
func (db *PostgreSQLBridge) UpdateDelegationBalance(addr *common.Address, valID *hexutil.Big, amo *hexutil.Big) error {
	// Calculate the value to 9 digits (and 18 billions remain available)
	val := new(big.Int).Div(amo.ToInt(), types.DelegationDecimalsCorrection).Uint64()

	// Log the change in the delegation balance
	db.log.Debugf("%s delegation to #%d value changed to %d", addr.String(), valID.ToInt().Uint64(), val)

	// Prepare the SQL query to update the delegation balance
	query := `
        UPDATE delegations
        SET amount_active = $1, value = $2
        WHERE delegation_address = $3 AND to_staker_id = $4
        RETURNING delegation_address
    `

	// Execute the update query
	var updatedAddress string
	err := db.db.QueryRowContext(context.Background(), query, amo.String(), val, addr.String(), valID.String()).Scan(&updatedAddress)
	if err != nil {
		if err == sql.ErrNoRows {
			// If no rows were matched, the delegation does not exist
			db.log.Errorf("delegation %s to %d not found", addr.String(), valID.ToInt().Uint64())
			return ErrUnknownDelegation
		}
		// Log the issue and return the error
		db.log.Criticalf("delegation balance cannot be updated; %s", err.Error())
		return fmt.Errorf("failed to update delegation balance: %v", err)
	}

	// Log the successful update
	db.log.Debugf("delegation balance for %s to %d successfully updated", addr.String(), valID.ToInt().Uint64())

	return nil
}

// isDelegationKnown checks if the given delegation exists in the database.
func (db *MongoDbBridge) isDelegationKnown(col *mongo.Collection, dl *types.Delegation) bool {
	// try to find the delegation in the database
	sr := col.FindOne(context.Background(), bson.D{
		{Key: types.FiDelegationAddress, Value: dl.Address.String()},
		{Key: types.FiDelegationToValidator, Value: dl.ToStakerId.String()},
	}, options.FindOne().SetProjection(bson.D{
		{Key: types.FiDelegationPk, Value: true},
	}))

	// error on lookup?
	if sr.Err() != nil {
		// may be ErrNoDocuments, which we seek
		if sr.Err() == mongo.ErrNoDocuments {
			return false
		}

		// inform that we can not get the PK; should not happen
		db.log.Errorf("can not get existing delegation pk; %s", sr.Err().Error())
		return false
	}
	return true
}

// isDelegationKnown checks if the given delegation exists in the PostgreSQL database.
func (db *PostgreSQLBridge) isDelegationKnown(dl *types.Delegation) (bool, error) {
	// Prepare the SQL query to check if the delegation exists
	query := `
        SELECT 1 FROM delegations 
        WHERE delegation_address = $1 AND to_staker_id = $2
    `

	// Execute the query
	var exists int
	err := db.db.QueryRowContext(context.Background(), query, dl.Address.String(), dl.ToStakerId.String()).Scan(&exists)
	if err != nil {
		if err == sql.ErrNoRows {
			// No matching delegation found
			return false, nil
		}
		// Log error if query execution fails
		db.log.Errorf("cannot check if delegation exists; %s", err.Error())
		return false, fmt.Errorf("failed to check if delegation exists: %v", err)
	}

	// Return true if a matching delegation exists
	return exists > 0, nil
}

// DelegationsCountFiltered calculates total number of delegations in the database for the given filter.
func (db *MongoDbBridge) DelegationsCountFiltered(filter *bson.D) (uint64, error) {
	return db.CountFiltered(db.client.Database(db.dbName).Collection(colDelegations), filter)
}

// DelegationsCountFiltered calculates total number of delegations in the database for the given filter.
func (db *PostgreSQLBridge) DelegationsCountFiltered(filter map[string]interface{}) (uint64, error) {
	// Start with the base SQL query
	query := "SELECT COUNT(*) FROM delegations WHERE 1=1"
	var args []interface{}

	// Process the filter and build the WHERE clause dynamically
	index := 1
	for key, value := range filter {
		// Build the condition (e.g., "delegation_address = $1")
		query += fmt.Sprintf(" AND %s = $%d", key, index)
		args = append(args, value)
		index++
	}

	// Execute the query
	var count uint64
	err := db.db.QueryRowContext(context.Background(), query, args...).Scan(&count)
	if err != nil {
		db.log.Errorf("failed to count filtered delegations: %s", err.Error())
		return 0, fmt.Errorf("failed to count filtered delegations: %v", err)
	}

	return count, nil
}

// DelegationsCount calculates total number of delegations in the database.
func (db *MongoDbBridge) DelegationsCount() (uint64, error) {
	return db.EstimateCount(db.client.Database(db.dbName).Collection(colDelegations))
}

// DelegationsCount calculates the total number of delegations in the database.
func (db *PostgreSQLBridge) DelegationsCount() (int64, error) {
	// Define the query to count the rows in the 'delegations' table
	query := "SELECT COUNT(*) FROM delegations"

	// Execute the query and scan the result into a variable
	var count int64
	err := db.db.QueryRow(query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get delegations count: %w", err)
	}

	// Return the count as uint64
	return int64(count), nil
}

// dlgListInit initializes list of delegations based on provided cursor, count, and filter.
func (db *MongoDbBridge) dlgListInit(col *mongo.Collection, cursor *string, count int32, filter *bson.D) (*types.DelegationList, error) {
	// make sure some filter is used
	if nil == filter {
		filter = &bson.D{}
	}

	// find how many transactions do we have in the database
	total, err := col.CountDocuments(context.Background(), *filter)
	if err != nil {
		db.log.Errorf("can not count delegations")
		return nil, err
	}

	// make the list and notify the size of it
	db.log.Debugf("found %d filtered delegations", total)
	list := types.DelegationList{
		Collection: make([]*types.Delegation, 0),
		Total:      uint64(total),
		First:      0,
		Last:       0,
		IsStart:    total == 0,
		IsEnd:      total == 0,
		Filter:     *filter,
	}

	// is the list non-empty? return the list with properly calculated range marks
	if 0 < total {
		return db.dlgListCollectRangeMarks(col, &list, cursor, count)
	}

	// this is an empty list
	db.log.Debug("empty delegations list created")
	return &list, nil
}

// dlgListInit initializes a list of delegations based on the provided cursor, count, and filter.
func (db *PostgreSQLBridge) dlgListInit(cursor *string, count int32, filter string, args ...interface{}) (*types.PostDelegationList, error) {
	// Default filter is applied if none provided
	if filter == "" {
		filter = "1=1" // Matches all rows
	}

	// Count the total number of delegations matching the filter
	var total int64
	countQuery := `SELECT COUNT(*) FROM delegations WHERE ` + filter
	err := db.db.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		db.log.Errorf("can not count delegations: %s", err.Error())
		return nil, err
	}

	// Create the delegation list
	db.log.Debugf("found %d filtered delegations", total)
	list := &types.PostDelegationList{
		Collection: make([]*types.Delegation, 0),
		Total:      uint64(total),
		First:      0,
		Last:       0,
		IsStart:    total == 0,
		IsEnd:      total == 0,
		Filter:     filter,
	}

	// If there are delegations, collect them with proper range marks
	if total > 0 {
		return db.dlgListCollectRangeMarks(list, cursor, count)
	}

	// No delegations found; return an empty list
	db.log.Debug("empty delegations list created")
	return list, nil
}

// trxListWithRangeMarks returns a list of delegations with proper First/Last marks.
func (db *MongoDbBridge) dlgListCollectRangeMarks(col *mongo.Collection, list *types.DelegationList, cursor *string, count int32) (*types.DelegationList, error) {
	var err error

	// find out the cursor ordinal index
	if cursor == nil && count > 0 {
		// get the highest available pk
		list.First, err = db.dlgListBorderPk(col,
			list.Filter,
			options.FindOne().SetSort(bson.D{{Key: types.FiDelegationOrdinal, Value: -1}}))
		list.IsStart = true

	} else if cursor == nil && count < 0 {
		// get the lowest available pk
		list.First, err = db.dlgListBorderPk(col,
			list.Filter,
			options.FindOne().SetSort(bson.D{{Key: types.FiDelegationOrdinal, Value: 1}}))
		list.IsEnd = true

	} else if cursor != nil {
		// decode the cursor
		id, err := primitive.ObjectIDFromHex(*cursor)
		if err != nil {
			db.log.Errorf("invalid delegation cursor ID; %s", err.Error())
			return nil, err
		}

		// look for the first ordinal to make sure it's there
		list.First, err = db.dlgListBorderPk(col,
			append(list.Filter, bson.E{Key: types.FiDelegationPk, Value: id}),
			options.FindOne())
	}

	// check the error
	if err != nil {
		db.log.Errorf("can not find the initial delegation; %s", err.Error())
		return nil, err
	}

	// inform what we are about to do
	db.log.Debugf("delegation list starts from #%d", list.First)
	return list, nil
}

// dlgListCollectRangeMarks returns a list of delegations with proper First/Last marks.
func (db *PostgreSQLBridge) dlgListCollectRangeMarks(list *types.PostDelegationList, cursor *string, count int32) (*types.PostDelegationList, error) {
	var err error

	// Use the filter directly as a SQL WHERE clause
	filter := list.Filter
	if filter == "" {
		filter = "1=1" // Default to match all rows if no filter is provided
	}

	// Define limit
	limit := 1

	if cursor == nil && count > 0 {
		// Get the highest ordinal_index (last item in descending order)
		query := `SELECT ordinal_index FROM delegations WHERE ` + filter + ` ORDER BY ordinal_index DESC LIMIT $1`
		err = db.db.QueryRow(query, limit).Scan(&list.First)
		list.IsStart = true

	} else if cursor == nil && count < 0 {
		// Get the lowest ordinal_index (first item in ascending order)
		query := `SELECT ordinal_index FROM delegations WHERE ` + filter + ` ORDER BY ordinal_index ASC LIMIT $1`
		err = db.db.QueryRow(query, limit).Scan(&list.First)
		list.IsEnd = true

	} else if cursor != nil {
		// Decode the cursor and find the matching ordinal_index
		query := `SELECT ordinal_index FROM delegations WHERE ` + filter + ` AND id = $1 ORDER BY ordinal_index ASC LIMIT $2`
		err = db.db.QueryRow(query, *cursor, limit).Scan(&list.First)
	}

	// Handle errors
	if err != nil {
		db.log.Errorf("cannot find the initial delegation; %s", err.Error())
		return nil, err
	}

	// Log the starting point
	db.log.Debugf("delegation list starts from #%d", list.First)
	return list, nil
}

// dlgListBorderPk finds the top PK of the delegations collection based on given filter and options.
func (db *MongoDbBridge) dlgListBorderPk(col *mongo.Collection, filter bson.D, opt *options.FindOneOptions) (uint64, error) {
	// prep container
	var row struct {
		Value uint64 `bson:"orx"`
	}

	// make sure we pull only what we need
	opt.SetProjection(bson.D{{Key: types.FiDelegationOrdinal, Value: true}})
	sr := col.FindOne(context.Background(), filter, opt)

	// try to decode
	err := sr.Decode(&row)
	if err != nil {
		return 0, err
	}

	return row.Value, nil
}

// dlgListBorderPk finds the top PK of the delegations table based on the given filter and order.
func (db *PostgreSQLBridge) dlgListBorderPk(filter string, args []interface{}, order string) (uint64, error) {
	// Prepare the query
	query := `SELECT ordinal_index FROM delegations WHERE ` + filter + ` ORDER BY ordinal_index ` + order + ` LIMIT 1`

	// Execute the query
	var value uint64
	err := db.db.QueryRow(query, args...).Scan(&value)
	if err != nil {
		db.log.Errorf("failed to find top PK: %s", err.Error())
		return 0, err
	}

	return value, nil
}

// dlgListFilter creates a filter for delegations list loading.
func (db *MongoDbBridge) dlgListFilter(cursor *string, count int32, list *types.DelegationList) *bson.D {
	// build an extended filter for the query; add PK (decoded cursor) to the original filter
	if cursor == nil {
		if count > 0 {
			list.Filter = append(list.Filter, bson.E{Key: types.FiDelegationOrdinal, Value: bson.D{{Key: "$lte", Value: list.First}}})
		} else {
			list.Filter = append(list.Filter, bson.E{Key: types.FiDelegationOrdinal, Value: bson.D{{Key: "$gte", Value: list.First}}})
		}
	} else {
		if count > 0 {
			list.Filter = append(list.Filter, bson.E{Key: types.FiDelegationOrdinal, Value: bson.D{{Key: "$lt", Value: list.First}}})
		} else {
			list.Filter = append(list.Filter, bson.E{Key: types.FiDelegationOrdinal, Value: bson.D{{Key: "$gt", Value: list.First}}})
		}
	}

	// return the new filter
	return &list.Filter
}

// dlgListFilter creates a filter for delegations list loading in PostgreSQL.
func (db *PostgreSQLBridge) dlgListFilter(cursor *string, count int32, list *types.PostDelegationList) (string, []interface{}) {
	var conditions []string
	var args []interface{}

	// Start with the existing filter if it exists
	if list.Filter != "" {
		conditions = append(conditions, list.Filter)
	}

	// Add additional conditions based on cursor and count
	if cursor == nil {
		if count > 0 {
			// Add condition for <= when no cursor is provided and count > 0
			conditions = append(conditions, "ordinal_index <= $1")
			args = append(args, list.First)
		} else {
			// Add condition for >= when no cursor is provided and count < 0
			conditions = append(conditions, "ordinal_index >= $1")
			args = append(args, list.First)
		}
	} else {
		if count > 0 {
			// Add condition for < when cursor is provided and count > 0
			conditions = append(conditions, "ordinal_index < $1")
			args = append(args, list.First)
		} else {
			// Add condition for > when cursor is provided and count < 0
			conditions = append(conditions, "ordinal_index > $1")
			args = append(args, list.First)
		}
	}

	// Combine all conditions into a single WHERE clause
	filter := strings.Join(conditions, " AND ")
	return filter, args
}

// dlgListOptions creates a filter options set for delegations list search.
func (db *MongoDbBridge) dlgListOptions(count int32) *options.FindOptions {
	// prep options
	opt := options.Find()

	// how to sort results in the collection
	// from high (new) to low (old) by default; reversed if loading from bottom
	sd := -1
	if count < 0 {
		sd = 1
		count = -count
	}

	// sort with the direction we want
	opt.SetSort(bson.D{{Key: types.FiDelegationOrdinal, Value: sd}})

	// apply the limit, try to get one more record so we can detect list end
	opt.SetLimit(int64(count) + 1)
	return opt
}

// dlgListOptions creates a SQL ORDER BY clause and limit for delegations list search.
func (db *PostgreSQLBridge) dlgListOptions(count int32) (string, int32) {
	// Determine the sort direction
	sortDirection := "DESC" // Default: high (new) to low (old)
	if count < 0 {
		sortDirection = "ASC" // Reversed for loading from bottom
		count = -count
	}

	// Return the sort direction and limit (+1 to detect list end)
	limit := count + 1
	return sortDirection, limit
}

// dlgListLoad load the initialized list of delegations from database.
func (db *MongoDbBridge) dlgListLoad(col *mongo.Collection, cursor *string, count int32, list *types.DelegationList) (err error) {
	// get the context for loader
	ctx := context.Background()

	// load the data
	ld, err := col.Find(ctx, db.dlgListFilter(cursor, count, list), db.dlgListOptions(count))
	if err != nil {
		db.log.Errorf("error loading delegations list; %s", err.Error())
		return err
	}

	// close the cursor as we leave
	defer func() {
		err = ld.Close(ctx)
		if err != nil {
			db.log.Errorf("error closing delegations list cursor; %s", err.Error())
		}
	}()

	// loop and load the list; we may not store the last value
	var dlg *types.Delegation
	for ld.Next(ctx) {
		// append a previous value to the list, if we have one
		if dlg != nil {
			list.Collection = append(list.Collection, dlg)
		}

		// try to decode the next row
		var row types.Delegation
		if err = ld.Decode(&row); err != nil {
			db.log.Errorf("can not decode the delegation list row; %s", err.Error())
			return err
		}

		// use this row as the next item
		dlg = &row
	}

	// we should have all the items already; we may just need to check if a boundary was reached
	list.IsEnd = (cursor == nil && count < 0) || (count > 0 && int32(len(list.Collection)) < count)
	list.IsStart = (cursor == nil && count > 0) || (count < 0 && int32(len(list.Collection)) < -count)

	// add the last item as well if we hit the boundary
	if (list.IsStart || list.IsEnd) && dlg != nil {
		list.Collection = append(list.Collection, dlg)
	}
	return nil
}

// dlgListLoad loads the initialized list of delegations from the PostgreSQL database.
func (db *PostgreSQLBridge) dlgListLoad(cursor *string, count int32, list *types.PostDelegationList) error {
	// Prepare the base query and filter
	filter, args := db.dlgListFilter(cursor, count, list)
	sortDirection, limit := db.dlgListOptions(count)

	// Construct the SQL query
	query := `
        SELECT 
            ordinal_index, 
            id, 
            transaction, 
            address, 
            to_staker_id, 
            to_staker_address, 
            created_time, 
            amount_staked, 
            amount_delegated
        FROM delegations
        WHERE ` + filter + `
        ORDER BY ordinal_index ` + sortDirection + `
        LIMIT $1`

	// Add the limit to the arguments
	args = append(args, limit)

	// Execute the query
	rows, err := db.db.Query(query, args...)
	if err != nil {
		db.log.Errorf("error loading delegations list; %s", err.Error())
		return err
	}
	defer rows.Close()

	// Loop through the rows and populate the list
	var dlg *types.Delegation
	for rows.Next() {
		// Append the previous row if one exists
		if dlg != nil {
			list.Collection = append(list.Collection, dlg)
		}

		// Decode the current row
		var row types.Delegation
		var transaction common.Hash
		var address, toStakerAddress common.Address
		var toStakerID, amountStaked, amountDelegated *hexutil.Big
		var createdTime hexutil.Uint64

		if err := rows.Scan(
			&row.Index,
			&row.ID,
			&transaction,
			&address,
			&toStakerID,
			&toStakerAddress,
			&createdTime,
			&amountStaked,
			&amountDelegated,
		); err != nil {
			db.log.Errorf("cannot decode the delegation list row; %s", err.Error())
			return err
		}

		row.Transaction = transaction
		row.Address = address
		row.ToStakerId = toStakerID
		row.ToStakerAddress = toStakerAddress
		row.CreatedTime = createdTime
		row.AmountStaked = amountStaked
		row.AmountDelegated = amountDelegated

		// Use this row as the next item
		dlg = &row
	}

	// Handle errors from row iteration
	if err := rows.Err(); err != nil {
		db.log.Errorf("error iterating delegation rows; %s", err.Error())
		return err
	}

	// Determine if the list is at the start or end
	list.IsEnd = (cursor == nil && count < 0) || (count > 0 && int32(len(list.Collection)) < count)
	list.IsStart = (cursor == nil && count > 0) || (count < 0 && int32(len(list.Collection)) < -count)

	// Append the last row if we hit the boundary
	if (list.IsStart || list.IsEnd) && dlg != nil {
		list.Collection = append(list.Collection, dlg)
	}

	return nil
}

// Delegations pulls list of delegations starting at the specified cursor.
func (db *MongoDbBridge) Delegations(cursor *string, count int32, filter *bson.D) (*types.DelegationList, error) {
	// nothing to load?
	if count == 0 {
		return nil, fmt.Errorf("nothing to do, zero delegations requested")
	}

	// get the collection and context
	col := db.client.Database(db.dbName).Collection(colDelegations)

	// init the list
	list, err := db.dlgListInit(col, cursor, count, filter)
	if err != nil {
		db.log.Errorf("can not build delegation list; %s", err.Error())
		return nil, err
	}

	// load data if there are any
	if list.Total > 0 {
		err = db.dlgListLoad(col, cursor, count, list)
		if err != nil {
			db.log.Errorf("can not load delegation list from database; %s", err.Error())
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

// Delegations pulls a list of delegations starting at the specified cursor.
func (db *PostgreSQLBridge) Delegations(cursor *string, count int32, filter string, args ...interface{}) (*types.PostDelegationList, error) {
	// Nothing to load?
	if count == 0 {
		return nil, fmt.Errorf("nothing to do, zero delegations requested")
	}

	// Initialize the list
	list, err := db.dlgListInit(cursor, count, filter, args...)
	if err != nil {
		db.log.Errorf("cannot build delegation list; %s", err.Error())
		return nil, err
	}

	// Load data if there are any
	if list.Total > 0 {
		err = db.dlgListLoad(cursor, count, list)
		if err != nil {
			db.log.Errorf("cannot load delegation list from database; %s", err.Error())
			return nil, err
		}

		// Reverse the order on negative count to have newer delegations on top
		if count < 0 {
			list.Reverse()
			count = -count
		}

		// Cut the list to the requested size
		if len(list.Collection) > int(count) {
			list.Collection = list.Collection[:len(list.Collection)-1]
		}
	}

	return list, nil
}

// DelegationsAll pulls list of delegations for the given filter un-paged.
func (db *MongoDbBridge) DelegationsAll(filter *bson.D) ([]*types.Delegation, error) {
	// get the collection and context
	col := db.client.Database(db.dbName).Collection(colDelegations)
	list := make([]*types.Delegation, 0)
	ctx := context.Background()

	// load the data
	ld, err := col.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: types.FiDelegationStamp, Value: -1}}))
	if err != nil {
		db.log.Errorf("error loading full delegations list; %s", err.Error())
		return nil, err
	}

	// close the cursor as we leave
	defer db.closeCursor(ld)

	for ld.Next(ctx) {
		// try to decode the next row
		var row types.Delegation
		if err = ld.Decode(&row); err != nil {
			db.log.Errorf("can not decode the full delegation list row; %s", err.Error())
			return nil, err
		}

		// use this row as the next item
		list = append(list, &row)
	}
	return list, nil
}

// DelegationsAll pulls the full list of delegations for the given filter un-paged.
func (db *PostgreSQLBridge) DelegationsAll(filter string, args ...interface{}) ([]*types.Delegation, error) {
	// Initialize the list to hold delegations
	list := make([]*types.Delegation, 0)

	// Construct the SQL query
	query := `
        SELECT 
            id, 
            transaction, 
            address, 
            to_staker_id, 
            to_staker_address, 
            created_time, 
            ordinal_index, 
            amount_staked, 
            amount_delegated
        FROM delegations
        WHERE ` + filter + `
        ORDER BY created_time DESC`

	// Execute the query
	rows, err := db.db.Query(query, args...)
	if err != nil {
		db.log.Errorf("error loading full delegations list; %s", err.Error())
		return nil, err
	}
	defer rows.Close()

	// Iterate through the rows and decode them
	for rows.Next() {
		var row types.Delegation
		var transaction common.Hash
		var address, toStakerAddress common.Address
		var toStakerID, amountStaked, amountDelegated *hexutil.Big
		var createdTime hexutil.Uint64

		if err := rows.Scan(
			&row.ID,
			&transaction,
			&address,
			&toStakerID,
			&toStakerAddress,
			&createdTime,
			&row.Index,
			&amountStaked,
			&amountDelegated,
		); err != nil {
			db.log.Errorf("cannot decode the full delegation list row; %s", err.Error())
			return nil, err
		}

		row.Transaction = transaction
		row.Address = address
		row.ToStakerId = toStakerID
		row.ToStakerAddress = toStakerAddress
		row.CreatedTime = createdTime
		row.AmountStaked = amountStaked
		row.AmountDelegated = amountDelegated

		// Append the row to the list
		list = append(list, &row)
	}

	// Handle any errors encountered during iteration
	if err := rows.Err(); err != nil {
		db.log.Errorf("error iterating through delegation rows; %s", err.Error())
		return nil, err
	}

	return list, nil
}
