// Package db implements bridge to persistent storage represented by Mongo database.
package db

import (
	"context"
	"fmt"
	"math/big"
	"ncogearthchain-api-graphql/internal/types"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// colRewards represents the name of the reward claim collection in database.
const colRewards = "rewards"

// initRewardsCollection initializes the reward claims collection with
// indexes and additional parameters needed by the app.
func (db *MongoDbBridge) initRewardsCollection(col *mongo.Collection) {
	// prepare index models
	ix := make([]mongo.IndexModel, 0)

	// index delegator, receiving validator, and creation time stamp
	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: types.FiRewardClaimAddress, Value: 1}}})
	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: types.FiRewardClaimToValidator, Value: 1}}})
	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: types.FiRewardClaimOrdinal, Value: -1}}})
	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: types.FiRewardClaimedTimeStamp, Value: -1}}})

	// create indexes
	if _, err := col.Indexes().CreateMany(context.Background(), ix); err != nil {
		db.log.Panicf("can not create indexes for reward claims collection; %s", err.Error())
	}

	// log we done that
	db.log.Debugf("reward claims collection initialized")
}

// initRewardsCollection initializes the reward claims table with indexes needed by the app.
func (db *PostgreSQLBridge) initRewardsCollection() {
	ctx := context.Background()

	// Define the SQL commands to create indexes
	queries := []string{
		`CREATE INDEX IF NOT EXISTS idx_reward_claim_address ON reward_claims (claim_address)`,
		`CREATE INDEX IF NOT EXISTS idx_reward_claim_to_validator ON reward_claims (to_validator)`,
		`CREATE INDEX IF NOT EXISTS idx_reward_claim_ordinal ON reward_claims (ordinal DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_reward_claim_timestamp ON reward_claims (claimed_timestamp DESC)`,
	}

	// Execute each query
	for _, query := range queries {
		if _, err := db.db.ExecContext(ctx, query); err != nil {
			db.log.Panicf("cannot create indexes for reward claims table; %s", err.Error())
		}
	}

	// Log completion
	db.log.Debugf("reward claims table initialized with indexes")
}

// AddRewardClaim stores a reward claim in the database if it doesn't exist.
func (db *MongoDbBridge) AddRewardClaim(rc *types.RewardClaim) error {
	// get the collection for delegations
	col := db.client.Database(db.dbName).Collection(colRewards)

	// if the delegation already exists, update it with the new one
	if db.isRewardClaimKnown(col, rc) {
		return nil
	}

	// try to do the insert
	if _, err := col.InsertOne(context.Background(), rc); err != nil {
		db.log.Critical(err)
		return err
	}

	// make sure delegation collection is initialized
	if db.initRewards != nil {
		db.initRewards.Do(func() { db.initRewardsCollection(col); db.initRewards = nil })
	}
	return nil
}

// AddRewardClaim stores a reward claim in the database if it doesn't exist.
func (db *PostgreSQLBridge) AddRewardClaim(rc *types.RewardClaim) error {
	// Validate the input
	if rc == nil {
		return fmt.Errorf("reward claim is nil")
	}

	// Check if the reward claim already exists
	if db.isRewardClaimKnown(rc) {
		return nil
	}

	// Insert the reward claim into the database
	query := `
        INSERT INTO reward_claims (delegator, to_validator_id, claimed, claim_trx, amount, is_delegated)
        VALUES ($1, $2, $3, $4, $5, $6)
        ON CONFLICT (delegator, to_validator_id, claim_trx) DO NOTHING`

	_, err := db.db.Exec(
		query,
		rc.Delegator.Hex(),        // Convert common.Address to string
		rc.ToValidatorId.String(), // Convert hexutil.Big to string
		uint64(rc.Claimed),        // Convert hexutil.Uint64 to uint64
		rc.ClaimTrx.Hex(),         // Convert common.Hash to string
		rc.Amount.String(),        // Convert hexutil.Big to string
		rc.IsDelegated,            // Boolean value
	)
	if err != nil {
		db.log.Criticalf("failed to insert reward claim; %s", err.Error())
		return err
	}

	// Ensure the rewards collection (table) is initialized
	if db.initRewards != nil {
		db.initRewards.Do(func() {
			db.initRewardsCollection()
			db.initRewards = nil
		})
	}

	return nil
}

// isRewardClaimKnown checks if the given delegation exists in the database.
func (db *MongoDbBridge) isRewardClaimKnown(col *mongo.Collection, rc *types.RewardClaim) bool {
	// try to find the delegation in the database
	sr := col.FindOne(context.Background(), bson.D{
		{Key: types.FiRewardClaimPk, Value: rc.Pk()},
	}, options.FindOne().SetProjection(bson.D{
		{Key: types.FiRewardClaimPk, Value: true},
	}))

	// error on lookup?
	if sr.Err() != nil {
		// may be ErrNoDocuments, which we seek
		if sr.Err() == mongo.ErrNoDocuments {
			return false
		}
		// inform that we can not get the PK; should not happen
		db.log.Errorf("can not get existing reward claim pk; %s", sr.Err().Error())
		return false
	}

	return true
}

// isRewardClaimKnown checks if the given reward claim exists in the database.
func (db *PostgreSQLBridge) isRewardClaimKnown(rc *types.RewardClaim) bool {
	var exists bool

	// SQL query to check if the reward claim exists
	query := `
        SELECT EXISTS (
            SELECT 1
            FROM reward_claims
            WHERE pk = $1
        )`

	// Execute the query
	err := db.db.QueryRow(query, rc.Pk()).Scan(&exists)
	if err != nil {
		db.log.Errorf("failed to check reward claim existence; %s", err.Error())
		return false
	}

	return exists
}

// RewardsCountFiltered calculates total number of reward claims in the database for the given filter.
func (db *MongoDbBridge) RewardsCountFiltered(filter *bson.D) (uint64, error) {
	return db.CountFiltered(db.client.Database(db.dbName).Collection(colRewards), filter)
}

// RewardsCountFiltered calculates the total number of reward claims in the database for the given filter.
func (db *PostgreSQLBridge) RewardsCountFiltered(filter string, args ...interface{}) (uint64, error) {
	var count uint64

	// Construct the SQL query with the provided filter
	query := `SELECT COUNT(*) FROM reward_claims WHERE ` + filter

	// Execute the query
	err := db.db.QueryRow(query, args...).Scan(&count)
	if err != nil {
		db.log.Errorf("failed to count filtered reward claims; %s", err.Error())
		return 0, err
	}

	return count, nil
}

// RewardsCount calculates total number of reward claims in the database.
func (db *MongoDbBridge) RewardsCount() (uint64, error) {
	return db.EstimateCount(db.client.Database(db.dbName).Collection(colRewards))
}

// // RewardsCount calculates the total number of reward claims in the database.
func (db *PostgreSQLBridge) RewardsCount() (int64, error) {
	// Define the SQL query to count rows in the 'rewards' table
	query := "SELECT COUNT(*) FROM rewards"

	// Execute the query and scan the result into a variable
	var count int64
	err := db.db.QueryRow(query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get rewards count: %w", err)
	}

	// Return the count as uint64
	return int64(count), nil
}

// rewListInit initializes list of delegations based on provided cursor, count, and filter.
func (db *MongoDbBridge) rewListInit(col *mongo.Collection, cursor *string, count int32, filter *bson.D) (*types.RewardClaimsList, error) {
	// make sure some filter is used
	if nil == filter {
		filter = &bson.D{}
	}

	// find how many transactions do we have in the database
	total, err := col.CountDocuments(context.Background(), *filter)
	if err != nil {
		db.log.Errorf("can not count reward claims")
		return nil, err
	}

	// make the list and notify the size of it
	db.log.Debugf("found %d filtered reward claims", total)
	list := types.RewardClaimsList{
		Collection: make([]*types.RewardClaim, 0),
		Total:      uint64(total),
		First:      0,
		Last:       0,
		IsStart:    total == 0,
		IsEnd:      total == 0,
		Filter:     *filter,
	}

	// is the list non-empty? return the list with properly calculated range marks
	if 0 < total {
		return db.rewListCollectRangeMarks(col, &list, cursor, count)
	}

	// this is an empty list
	db.log.Debug("empty reward claims list created")
	return &list, nil
}

// rewListInit initializes a list of reward claims based on the provided cursor, count, and filter.
func (db *PostgreSQLBridge) rewListInit(cursor *string, count int32, filter string, args ...interface{}) (*types.PostRewardClaimsList, error) {
	// Ensure a valid filter
	if filter == "" {
		filter = "1=1" // Default to match all rows
	}

	// Count the total number of reward claims matching the filter
	var total int64
	countQuery := `SELECT COUNT(*) FROM reward_claims WHERE ` + filter
	err := db.db.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		db.log.Errorf("cannot count reward claims; %s", err.Error())
		return nil, err
	}

	// Create the reward claims list
	db.log.Debugf("found %d filtered reward claims", total)
	list := &types.PostRewardClaimsList{
		Collection: make([]*types.RewardClaim, 0),
		Total:      uint64(total),
		First:      0,
		Last:       0,
		IsStart:    total == 0,
		IsEnd:      total == 0,
		Filter:     filter,
	}

	// If there are claims, collect them with proper range marks
	if total > 0 {
		return db.rewListCollectRangeMarks(list, cursor, count, filter, args...)
	}

	// This is an empty list
	db.log.Debug("empty reward claims list created")
	return list, nil
}

// rewListCollectRangeMarks returns a list of reward claims with proper First/Last marks.
func (db *MongoDbBridge) rewListCollectRangeMarks(col *mongo.Collection, list *types.RewardClaimsList, cursor *string, count int32) (*types.RewardClaimsList, error) {
	var err error

	// find out the cursor ordinal index
	if cursor == nil && count > 0 {
		// get the highest available pk
		list.First, err = db.rewListBorderPk(col,
			list.Filter,
			options.FindOne().SetSort(bson.D{{Key: types.FiRewardClaimOrdinal, Value: -1}}))
		list.IsStart = true

	} else if cursor == nil && count < 0 {
		// get the lowest available pk
		list.First, err = db.rewListBorderPk(col,
			list.Filter,
			options.FindOne().SetSort(bson.D{{Key: types.FiRewardClaimOrdinal, Value: 1}}))
		list.IsEnd = true

	} else if cursor != nil {
		// the cursor itself is the starting point
		list.First, err = db.rewListBorderPk(col,
			bson.D{{Key: types.FiRewardClaimPk, Value: *cursor}},
			options.FindOne())
	}

	// check the error
	if err != nil {
		db.log.Errorf("can not find the initial reward claim")
		return nil, err
	}

	// inform what we are about to do
	db.log.Debugf("reward claim list initialized with ordinal %d", list.First)
	return list, nil
}

// rewListCollectRangeMarks returns a list of reward claims with proper First/Last marks for PostgreSQL.
func (db *PostgreSQLBridge) rewListCollectRangeMarks(list *types.PostRewardClaimsList, cursor *string, count int32, filter string, args ...interface{}) (*types.PostRewardClaimsList, error) {
	var err error
	var query string

	// Determine the starting point based on the cursor and count
	if cursor == nil && count > 0 {
		// Get the highest ordinal
		query = `
            SELECT ordinal
            FROM reward_claims
            WHERE ` + filter + `
            ORDER BY ordinal DESC
            LIMIT 1`
	} else if cursor == nil && count < 0 {
		// Get the lowest ordinal
		query = `
            SELECT ordinal
            FROM reward_claims
            WHERE ` + filter + `
            ORDER BY ordinal ASC
            LIMIT 1`
	} else if cursor != nil {
		// The cursor itself is the starting point
		query = `
            SELECT ordinal
            FROM reward_claims
            WHERE pk = $1`
		args = []interface{}{*cursor}
	}

	// Execute the query to find the first ordinal
	err = db.db.QueryRow(query, args...).Scan(&list.First)
	if err != nil {
		db.log.Errorf("cannot find the initial reward claim; %s", err.Error())
		return nil, err
	}

	// Log the starting point
	db.log.Debugf("reward claim list initialized with ordinal %d", list.First)
	return list, nil
}

// rewListBorderPk finds the top PK of the reward claims collection based on given filter and options.
func (db *MongoDbBridge) rewListBorderPk(col *mongo.Collection, filter bson.D, opt *options.FindOneOptions) (uint64, error) {
	// prep container
	var row struct {
		Value uint64 `bson:"orx"`
	}

	// make sure we pull only what we need
	opt.SetProjection(bson.D{{Key: types.FiRewardClaimOrdinal, Value: true}})

	// try to decode
	sr := col.FindOne(context.Background(), filter, opt)
	err := sr.Decode(&row)
	if err != nil {
		return 0, err
	}
	return row.Value, nil
}

// rewListBorderPk finds the top PK of the reward claims table based on the given filter and sorting.
func (db *PostgreSQLBridge) rewListBorderPk(filter string, order string, args ...interface{}) (uint64, error) {
	var value uint64

	// Construct the SQL query
	query := `
        SELECT ordinal
        FROM reward_claims
        WHERE ` + filter + `
        ORDER BY ordinal ` + order + `
        LIMIT 1`

	// Execute the query
	err := db.db.QueryRow(query, args...).Scan(&value)
	if err != nil {
		db.log.Errorf("cannot find the top PK for reward claims; %s", err.Error())
		return 0, err
	}

	return value, nil
}

// rewListFilter creates a filter for reward claims list loading.
func (db *MongoDbBridge) rewListFilter(cursor *string, count int32, list *types.RewardClaimsList) *bson.D {
	// build an extended filter for the query; add PK (decoded cursor) to the original filter
	if cursor == nil {
		if count > 0 {
			list.Filter = append(list.Filter, bson.E{Key: types.FiRewardClaimOrdinal, Value: bson.D{{Key: "$lte", Value: list.First}}})
		} else {
			list.Filter = append(list.Filter, bson.E{Key: types.FiRewardClaimOrdinal, Value: bson.D{{Key: "$gte", Value: list.First}}})
		}
	} else {
		if count > 0 {
			list.Filter = append(list.Filter, bson.E{Key: types.FiRewardClaimOrdinal, Value: bson.D{{Key: "$lt", Value: list.First}}})
		} else {
			list.Filter = append(list.Filter, bson.E{Key: types.FiRewardClaimOrdinal, Value: bson.D{{Key: "$gt", Value: list.First}}})
		}
	}
	// return the new filter
	return &list.Filter
}

// rewListFilter creates a filter string for reward claims list loading in PostgreSQL.
func (db *PostgreSQLBridge) rewListFilter(cursor *string, count int32, list *types.PostRewardClaimsList, args ...interface{}) (string, []interface{}) {
	var filter string

	// Start with the base filter from the list
	filter = list.Filter

	// Extend the filter based on the cursor and count
	if cursor == nil {
		if count > 0 {
			filter += " AND ordinal <= $1"
		} else {
			filter += " AND ordinal >= $1"
		}
		args = append(args, list.First)
	} else {
		if count > 0 {
			filter += " AND ordinal < $1"
		} else {
			filter += " AND ordinal > $1"
		}
		args = append(args, list.First)
	}

	return filter, args
}

// rewListOptions creates a filter options set for reward claims list search.
func (db *MongoDbBridge) rewListOptions(count int32) *options.FindOptions {
	// prep options
	opt := options.Find()

	// how to sort results in the collection
	// from high (new) to low (old) by default; reversed if loading from bottom
	sd := -1
	if count < 0 {
		sd = 1
	}

	// sort with the direction we want
	opt.SetSort(bson.D{{Key: types.FiRewardClaimOrdinal, Value: sd}})

	// prep the loading limit
	var limit = int64(count)
	if limit < 0 {
		limit = -limit
	}

	// apply the limit, try to get one more record so we can detect list end
	opt.SetLimit(limit + 1)
	return opt
}

// rewListOptions creates sorting and limit options for reward claims list search in PostgreSQL.
func (db *PostgreSQLBridge) rewListOptions(count int32) (string, int64) {
	// Determine the sort direction
	sortDirection := "DESC"
	if count < 0 {
		sortDirection = "ASC"
	}

	// Calculate the limit
	limit := int64(count)
	if limit < 0 {
		limit = -limit
	}

	// Return the sort direction and limit
	return sortDirection, limit + 1 // Add 1 to detect the list end
}

// rewListLoad load the initialized list of reward claims from database.
func (db *MongoDbBridge) rewListLoad(col *mongo.Collection, cursor *string, count int32, list *types.RewardClaimsList) (err error) {
	// get the context for loader
	ctx := context.Background()

	// load the data
	ld, err := col.Find(ctx, db.rewListFilter(cursor, count, list), db.rewListOptions(count))
	if err != nil {
		db.log.Errorf("error loading reward claims list; %s", err.Error())
		return err
	}

	// close the cursor as we leave
	defer db.closeCursor(ld)

	// loop and load the list; we may not store the last value
	var rwc *types.RewardClaim
	for ld.Next(ctx) {
		// append a previous value to the list, if we have one
		if rwc != nil {
			list.Collection = append(list.Collection, rwc)
		}

		// try to decode the next row
		var row types.RewardClaim
		if err = ld.Decode(&row); err != nil {
			db.log.Errorf("can not decode the reward claim list row; %s", err.Error())
			return err
		}

		// use this row as the next item
		rwc = &row
	}

	// we should have all the items already; we may just need to check if a boundary was reached
	list.IsEnd = (cursor == nil && count < 0) || (count > 0 && int32(len(list.Collection)) < count)
	list.IsStart = (cursor == nil && count > 0) || (count < 0 && int32(len(list.Collection)) < -count)

	// add the last item as well if we hit the boundary
	if (list.IsStart || list.IsEnd) && rwc != nil {
		list.Collection = append(list.Collection, rwc)
	}
	return nil
}

// rewListLoad loads the initialized list of reward claims from the PostgreSQL database.
func (db *PostgreSQLBridge) rewListLoad(cursor *string, count int32, list *types.PostRewardClaimsList, filter string, args ...interface{}) error {
	// Construct the SQL query
	sortDirection, limit := db.rewListOptions(count)
	query := `
        SELECT delegator, to_validator_id, claimed, claim_trx, amount, is_delegated
        FROM reward_claims
        WHERE ` + filter + `
        ORDER BY ordinal ` + sortDirection + `
        LIMIT $1`

	// Add the limit to the query arguments
	args = append(args, limit)

	// Execute the query
	rows, err := db.db.Query(query, args...)
	if err != nil {
		db.log.Errorf("error loading reward claims list; %s", err.Error())
		return err
	}
	defer rows.Close()

	// Load the data into the list
	var rwc *types.RewardClaim
	for rows.Next() {
		// Append the previous reward claim to the collection if it's not nil
		if rwc != nil {
			list.Collection = append(list.Collection, rwc)
		}

		// Decode the current row
		var row types.RewardClaim
		if err := rows.Scan(
			&row.Delegator,
			&row.ToValidatorId,
			&row.Claimed,
			&row.ClaimTrx,
			&row.Amount,
			&row.IsDelegated,
		); err != nil {
			db.log.Errorf("cannot decode the reward claim list row; %s", err.Error())
			return err
		}

		// Set the current row as the next item
		rwc = &row
	}

	// Check for any errors during row iteration
	if err := rows.Err(); err != nil {
		db.log.Errorf("error iterating reward claims rows; %s", err.Error())
		return err
	}

	// Determine if the list has reached its start or end boundary
	list.IsEnd = (cursor == nil && count < 0) || (count > 0 && int32(len(list.Collection)) < count)
	list.IsStart = (cursor == nil && count > 0) || (count < 0 && int32(len(list.Collection)) < -count)

	// Add the last item if a boundary is reached
	if (list.IsStart || list.IsEnd) && rwc != nil {
		list.Collection = append(list.Collection, rwc)
	}

	return nil
}

// RewardClaims pulls list of reward claims starting at the specified cursor.
func (db *MongoDbBridge) RewardClaims(cursor *string, count int32, filter *bson.D) (*types.RewardClaimsList, error) {
	// nothing to load?
	if count == 0 {
		return nil, fmt.Errorf("nothing to do, zero reward claims requested")
	}

	// get the collection and context
	col := db.client.Database(db.dbName).Collection(colRewards)

	// init the list
	list, err := db.rewListInit(col, cursor, count, filter)
	if err != nil {
		db.log.Errorf("can not build reward claims list; %s", err.Error())
		return nil, err
	}

	// load data if there are any
	if list.Total > 0 {
		err = db.rewListLoad(col, cursor, count, list)
		if err != nil {
			db.log.Errorf("can not load reward claims list from database; %s", err.Error())
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

// RewardClaims pulls a list of reward claims starting at the specified cursor.
func (db *PostgreSQLBridge) RewardClaims(cursor *string, count int32, filter string, args ...interface{}) (*types.PostRewardClaimsList, error) {
	// Nothing to load?
	if count == 0 {
		return nil, fmt.Errorf("nothing to do, zero reward claims requested")
	}

	// Initialize the list
	list, err := db.rewListInit(cursor, count, filter, args...)
	if err != nil {
		db.log.Errorf("cannot build reward claims list; %s", err.Error())
		return nil, err
	}

	// Load data if there are any
	if list.Total > 0 {
		err = db.rewListLoad(cursor, count, list, filter, args...)
		if err != nil {
			db.log.Errorf("cannot load reward claims list from database; %s", err.Error())
			return nil, err
		}

		// Reverse the list if count is negative so newer claims appear on top
		if count < 0 {
			reverseRewardClaimsList(list)
			count = -count
		}

		// Trim the list if more items were loaded than requested
		if len(list.Collection) > int(count) {
			list.Collection = list.Collection[:len(list.Collection)-1]
		}
	}

	return list, nil
}

// reverseRewardClaimsList reverses the collection of reward claims in the list.
func reverseRewardClaimsList(list *types.PostRewardClaimsList) {
	n := len(list.Collection)
	for i := 0; i < n/2; i++ {
		list.Collection[i], list.Collection[n-1-i] = list.Collection[n-1-i], list.Collection[i]
	}
}

// RewardsSumValue calculates sum of values for all the reward claims by a filter.
func (db *MongoDbBridge) RewardsSumValue(filter *bson.D) (*big.Int, error) {
	return db.sumFieldValue(
		db.client.Database(db.dbName).Collection(colRewards),
		types.FiRewardClaimedValue,
		filter,
		types.RewardDecimalsCorrection)
}

// RewardsSumValue calculates the sum of values for all reward claims by a filter in PostgreSQL.
func (db *PostgreSQLBridge) RewardsSumValue(filter string, args ...interface{}) (*big.Int, error) {
	var sumValue string

	// Construct the SQL query
	query := `
        SELECT COALESCE(SUM(amount::numeric), 0)
        FROM reward_claims
        WHERE ` + filter

	// Execute the query
	err := db.db.QueryRow(query, args...).Scan(&sumValue)
	if err != nil {
		db.log.Errorf("error calculating sum of reward claims; %s", err.Error())
		return nil, err
	}

	// Convert the result to *big.Int
	result := new(big.Int)
	_, ok := result.SetString(sumValue, 10)
	if !ok {
		return nil, fmt.Errorf("failed to convert sum value to big.Int")
	}

	return result, nil
}
