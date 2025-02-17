// Package db implements bridge to persistent storage represented by Mongo database.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"ncogearthchain-api-graphql/internal/types"
	"strings"
	"time"
)

const (
	// db.trx_volume.createIndex({"stamp": 1}, {unique: true})
	// coTransactionVolume represents the name of the trx flow collection.
	coTransactionVolume = "trx_volume"

	// fiTrxVolumePk name of the primary key of the transaction volume row.
	fiTrxVolumePk = "_id"

	// fiTrxVolumeStamp name of the field of the trx volume time stamp.
	fiTrxVolumeStamp = "stamp"
)

// // TrxDailyFlowList loads a range of daily trx volumes from the database.
// func (db *MongoDbBridge) TrxDailyFlowList(from *time.Time, to *time.Time) ([]*types.DailyTrxVolume, error) {
// 	// log what we do
// 	db.log.Debugf("loading trx flow between %s and %s", from.String(), to.String())

// 	// get the collection and context
// 	ctx := context.Background()
// 	col := db.client.Database(db.dbName).Collection(coTransactionVolume)

// 	// pull the data; make sure there is a limit to the range
// 	ld, err := col.Find(ctx, trxDailyFlowListFilter(from, to), options.Find().SetSort(bson.D{{Key: fiTrxVolumePk, Value: 1}}).SetLimit(365))
// 	if err != nil {
// 		db.log.Errorf("can not load daily flow; %s", err.Error())
// 		return nil, err
// 	}

// 	// close the cursor as we leave
// 	defer db.closeCursor(ld)

// 	// load the list
// 	return loadTrxDailyFlowList(ld)
// }

func (db *PostgreSQLBridge) TrxDailyFlowList(from *time.Time, to *time.Time) ([]*types.DailyTrxVolume, error) {
	// Log the operation
	db.log.Debugf("loading trx flow between %s and %s", from.String(), to.String())

	// Prepare the SQL query
	query := `
		SELECT
		date_trunc('day', transaction_time) AS date,
		SUM(volume) AS total_volume
		FROM trx_daily_volume
		WHERE transaction_time BETWEEN $1 AND $2
		GROUP BY date_trunc('day', transaction_time)
		ORDER BY date_trunc('day', transaction_time) ASC
		LIMIT 365
	`

	// Execute the query
	rows, err := db.db.QueryContext(context.Background(), query, from, to)
	if err != nil {
		db.log.Errorf("can not load daily flow; %s", err.Error())
		return nil, err
	}
	defer rows.Close()

	// Use the helper function to process the result set
	return loadTrxDailyFlowList(rows)
}

// // TrxGasSpeed provides amount of gas consumed by transaction per second
// // in the given time range.
// func (db *MongoDbBridge) TrxGasSpeed(from *time.Time, to *time.Time) (float64, error) {
// 	// check the time range
// 	if !from.Before(*to) {
// 		return 0.0, fmt.Errorf("invalid time range requested")
// 	}

// 	// get the collection and context
// 	ctx := context.Background()
// 	col := db.client.Database(db.dbName).Collection(coTransactions)

// 	// aggregate the gas used from the given time range
// 	cr, err := col.Aggregate(ctx, mongo.Pipeline{
// 		{{Key: "$match", Value: trxDailyFlowListFilter(from, to)}},
// 		{{Key: "$group", Value: bson.D{
// 			{Key: "_id", Value: nil},
// 			{Key: "volume", Value: bson.D{{Key: "$sum", Value: "$gas_use"}}},
// 		}}},
// 	})
// 	if err != nil {
// 		db.log.Errorf("can not collect gas speed; %s", err.Error())
// 		return 0.0, err
// 	}

// 	// close the cursor as we leave
// 	defer db.closeCursor(cr)
// 	return db.trxGasSpeed(cr, from, to)
// }

// TrxGasSpeed provides the amount of gas consumed by transactions per second
// in the given time range.
// TrxGasSpeed provides the amount of gas consumed by transactions per second in the given time range.
func (db *PostgreSQLBridge) TrxGasSpeed(from *time.Time, to *time.Time) (float64, error) {
	// Validate the time range
	if !from.Before(*to) {
		return 0.0, fmt.Errorf("invalid time range requested")
	}

	// Generate the WHERE clause using trxDailyFlowListFilter
	whereClause, args := trxDailyFlowListFilter(from, to)

	// Prepare the SQL query dynamically
	query := fmt.Sprintf(`
		SELECT SUM(gas_use) AS total_gas_used
		FROM transactions
		WHERE %s
	`, whereClause)

	// Execute the query
	var totalGasUsed int64
	err := db.db.QueryRowContext(context.Background(), query, args...).Scan(&totalGasUsed)
	if err != nil {
		db.log.Errorf("can not collect gas speed; %s", err.Error())
		return 0.0, err
	}

	// Calculate the duration in seconds
	duration := to.Sub(*from).Seconds()
	if duration <= 0 {
		return 0.0, fmt.Errorf("invalid duration: time range must have a positive length")
	}

	// // Calculate the gas speed (gas consumed per second)
	// return float64(totalGasUsed) / duration, nil
	//}

	// Call trxGasSpeed to calculate the gas speed from the database query
	return db.trxGasSpeed(from, to)

}

// // trxGasSpeed makes the gas speed calculation from the given aggregation cursor.
// func (db *MongoDbBridge) trxGasSpeed(cr *mongo.Cursor, from *time.Time, to *time.Time) (float64, error) {
// 	// get the row
// 	if !cr.Next(context.Background()) {
// 		db.log.Errorf("can not navigate gas speed results")
// 		return 0.0, fmt.Errorf("gas speed aggregation failure")
// 	}

// 	// the row struct for parsing
// 	var row struct {
// 		Volume int64 `bson:"volume"`
// 	}
// 	if err := cr.Decode(&row); err != nil {
// 		db.log.Errorf("can not decode gas speed cursor; %s", err.Error())
// 		return 0.0, err
// 	}

// 	// calculate the gas volume per second
// 	return float64(row.Volume) / to.Sub(*from).Seconds(), nil
// }

// trxGasSpeed calculates the gas speed (gas consumed per second) from the database query result.
func (db *PostgreSQLBridge) trxGasSpeed(from *time.Time, to *time.Time) (float64, error) {
	// Validate the time range
	if !from.Before(*to) {
		return 0.0, fmt.Errorf("invalid time range requested")
	}

	// Prepare the SQL query to get the total gas used in the given time range
	query := `
		SELECT SUM(gas_use) AS total_gas_used
		FROM transactions
		WHERE transaction_time BETWEEN $1 AND $2
	`

	// Execute the query and get the total gas used
	var totalGasUsed int64
	err := db.db.QueryRowContext(context.Background(), query, from, to).Scan(&totalGasUsed)
	if err != nil {
		db.log.Errorf("can not collect gas speed; %s", err.Error())
		return 0.0, err
	}

	// Calculate the duration in seconds
	duration := to.Sub(*from).Seconds()
	if duration <= 0 {
		return 0.0, fmt.Errorf("invalid duration: time range must have a positive length")
	}

	// Calculate the gas speed (gas per second)
	return float64(totalGasUsed) / duration, nil
}

// // TrxRecentTrxSpeed provides the number of transaction per second on the defined range in seconds.
// func (db *MongoDbBridge) TrxRecentTrxSpeed(sec int32) (float64, error) {
// 	// make sure the request makes sense and calculate the left boundary
// 	if sec < 60 {
// 		sec = 60
// 	}
// 	from := time.Now().UTC().Add(time.Duration(-sec) * time.Second)
// 	col := db.client.Database(db.dbName).Collection(coTransactions)

// 	// find how many transactions do we have in the database
// 	total, err := col.CountDocuments(context.Background(), bson.D{
// 		{Key: fiTransactionTimeStamp, Value: bson.D{
// 			{Key: "$gte", Value: from},
// 		}},
// 	})
// 	if err != nil {
// 		db.log.Errorf("can not count recent transactions")
// 		return 0, err
// 	}

// 	// any transactions at all?
// 	if total == 0 {
// 		return 0, nil
// 	}
// 	return float64(total) / float64(sec), nil
// }

// TrxRecentTrxSpeed provides the number of transactions per second in the defined range in seconds.
func (db *PostgreSQLBridge) TrxRecentTrxSpeed(sec int32) (float64, error) {
	// Make sure the request makes sense and calculate the left boundary
	if sec < 60 {
		sec = 60
	}
	from := time.Now().UTC().Add(time.Duration(-sec) * time.Second)

	// Prepare the SQL query to count transactions in the given time range
	query := `
		SELECT COUNT(*) AS total_transactions
		FROM transactions
		WHERE transaction_time >= $1
	`

	// Execute the query and get the total transaction count
	var total int64
	err := db.db.QueryRowContext(context.Background(), query, from).Scan(&total)
	if err != nil {
		db.log.Errorf("can not count recent transactions; %s", err.Error())
		return 0, err
	}

	// If there are no transactions, return 0
	if total == 0 {
		return 0, nil
	}

	// Return transactions per second (total transactions / requested duration in seconds)
	return float64(total) / float64(sec), nil
}

// // trxDailyFlowListFilter creates a filter for loading trx flow data based on provided
// // range dates.
// func trxDailyFlowListFilter(from *time.Time, to *time.Time) *bson.D {
// 	// prep the filter
// 	filter := bson.D{}

// 	// add start filter
// 	if from != nil {
// 		filter = append(filter, bson.E{Key: fiTrxVolumeStamp, Value: bson.D{{Key: "$gte", Value: *from}}})
// 	}

// 	// add end filter
// 	if to != nil {
// 		filter = append(filter, bson.E{Key: fiTrxVolumeStamp, Value: bson.D{{Key: "$lte", Value: *to}}})
// 	}

// 	return &filter
// }

// trxDailyFlowListFilter creates a SQL WHERE clause for loading trx flow data based on provided date range.
func trxDailyFlowListFilter(from *time.Time, to *time.Time) (string, []interface{}) {
	// Initialize the WHERE clause and arguments
	clauses := []string{}
	args := []interface{}{}

	// Add conditions based on the provided time range
	argIndex := 1
	if from != nil {
		clauses = append(clauses, fmt.Sprintf("transaction_time >= $%d", argIndex))
		args = append(args, *from)
		argIndex++
	}
	if to != nil {
		clauses = append(clauses, fmt.Sprintf("transaction_time <= $%d", argIndex))
		args = append(args, *to)
	}

	// Combine the clauses into a single WHERE clause
	whereClause := strings.Join(clauses, " AND ")
	return whereClause, args
}

// // loadTrxDailyFlowList load the trx flow list from provided DB cursor.
// func loadTrxDailyFlowList(ld *mongo.Cursor) ([]*types.DailyTrxVolume, error) {
// 	// prep the result list
// 	ctx := context.Background()
// 	list := make([]*types.DailyTrxVolume, 0)

// 	// loop and load
// 	for ld.Next(ctx) {
// 		// try to decode the next row
// 		var row types.DailyTrxVolume
// 		if err := ld.Decode(&row); err != nil {
// 			return nil, err
// 		}

// 		// we have one
// 		list = append(list, &row)
// 	}
// 	return list, nil
// }

func loadTrxDailyFlowList(rows *sql.Rows) ([]*types.DailyTrxVolume, error) {
	// Prepare the result list
	list := make([]*types.DailyTrxVolume, 0)

	// Loop through the rows and decode each one
	for rows.Next() {
		var row types.DailyTrxVolume
		var date time.Time

		// Scan the values from the row
		if err := rows.Scan(&date, &row.TotalVolume); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Set the date for the transaction volume
		row.Date = date

		// Add the row to the result list
		list = append(list, &row)
	}

	// Check for errors during iteration
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over rows: %w", err)
	}

	return list, nil
}

func (db *PostgreSQLBridge) TrxDailyFlowUpdate(from time.Time) error {
	// Log the operation
	db.log.Noticef("updating trx flow after %s", from.String())

	// Prepare the SQL query to aggregate transactions and update the trx_volume table
	query := `
		WITH aggregated AS (
			SELECT
				date_trunc('day', timestamp) AS stamp,
				SUM(volume) AS total_volume,
				SUM(gas_use) AS total_gas,
				COUNT(*) AS total_count
			FROM transactions
			WHERE transaction_time >= $1
			GROUP BY date_trunc('day', timestamp)
		)
		INSERT INTO trx_volume (stamp, volume, gas, value)
		SELECT
			stamp,
			total_volume,
			total_gas,
			total_count
		FROM aggregated
		ON CONFLICT (stamp)
		DO UPDATE SET
			volume = EXCLUDED.volume,
			gas = EXCLUDED.gas,
			value = EXCLUDED.value;
	`

	// Execute the query
	_, err := db.db.ExecContext(context.Background(), query, from)
	if err != nil {
		db.log.Errorf("can not update trx flow; %s", err.Error())
		return err
	}

	return nil
}
