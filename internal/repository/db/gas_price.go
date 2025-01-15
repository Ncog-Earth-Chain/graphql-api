// Package db implements bridge to persistent storage represented by Mongo database.
package db

import (
	"context"
	"fmt"
	"ncogearthchain-api-graphql/internal/types"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	// colEpochs represents the name of the epochs' collection in database.
	colGasPrice = "gas_price"
)

// initGasPriceCollection initializes the gas price period collection with
// indexes and additional parameters needed by the app.
func (db *MongoDbBridge) initGasPriceCollection(col *mongo.Collection) {
	// prepare index models
	ix := make([]mongo.IndexModel, 0)

	// index sender and recipient
	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: types.FiGasPriceTimeFrom, Value: 1}}})
	ix = append(ix, mongo.IndexModel{Keys: bson.D{{Key: types.FiGasPriceTimeTo, Value: 1}}})

	// create indexes
	if _, err := col.Indexes().CreateMany(context.Background(), ix); err != nil {
		db.log.Panicf("can not create indexes for gas price collection; %s", err.Error())
	}

	// log we are done that
	db.log.Debugf("gas price collection initialized")
}

// initGasPriceTable initializes the gas price table and creates required indexes.
func (db *PostgreSQLBridge) initGasPriceTable() {
	ctx := context.Background()

	// Create the table if it does not exist
	createTableQuery := `
        CREATE TABLE IF NOT EXISTS gas_price_periods (
            id SERIAL PRIMARY KEY,
            type TEXT NOT NULL,
            open NUMERIC NOT NULL,
            close NUMERIC NOT NULL,
            min NUMERIC NOT NULL,
            max NUMERIC NOT NULL,
            avg NUMERIC NOT NULL,
            time_from TIMESTAMP NOT NULL,
            time_to TIMESTAMP NOT NULL,
            tick BIGINT NOT NULL
        )
    `
	if _, err := db.db.ExecContext(ctx, createTableQuery); err != nil {
		db.log.Panicf("failed to create gas_price_periods table: %s", err)
	}

	// Define index creation queries
	indexQueries := []string{
		`CREATE INDEX IF NOT EXISTS idx_gas_price_time_from ON gas_price_periods (time_from)`,
		`CREATE INDEX IF NOT EXISTS idx_gas_price_time_to ON gas_price_periods (time_to)`,
	}

	// Execute each index query
	for _, query := range indexQueries {
		if _, err := db.db.ExecContext(ctx, query); err != nil {
			db.log.Panicf("failed to create index for gas price table; %s", err)
		}
	}

	db.log.Debugf("Gas price table initialized with schema and indexes.")
}

// AddGasPricePeriod stores a new record for the gas price evaluation
// into the persistent collection.
func (db *MongoDbBridge) AddGasPricePeriod(gp *types.GasPricePeriod) error {
	// do we have anything to store at all?
	if gp == nil {
		return fmt.Errorf("no value to store")
	}

	// get the collection
	col := db.client.Database(db.dbName).Collection(colGasPrice)

	// try to do the insert
	if _, err := col.InsertOne(context.Background(), gp); err != nil {
		db.log.Errorf("can not store gas price value; %s", err)
		return err
	}

	// make sure gas price collection is initialized
	if db.initGasPrice != nil {
		db.initGasPrice.Do(func() { db.initGasPriceCollection(col); db.initGasPrice = nil })
	}
	return nil
}

// AddGasPricePeriod stores a new record for the gas price evaluation into the database.
func (db *PostgreSQLBridge) AddGasPricePeriod(gp *types.GasPricePeriod) error {
	// Check if the input is valid
	if gp == nil {
		return fmt.Errorf("no value to store")
	}

	// Debug log the input to ensure correctness
	db.log.Infof("Inserting GasPricePeriod: %+v", gp)

	// Insert the record into the gas price table
	query := `
        INSERT INTO gas_price_periods (type, open, close, min, max, avg, time_from, time_to, tick)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	res, err := db.db.Exec(
		query,
		strconv.Itoa(int(gp.Type)), // Convert int8 to string
		gp.Open,
		gp.Close,
		gp.Min,
		gp.Max,
		gp.Avg,
		gp.From.Format("2006-01-02 15:04:05"), // Format timestamp
		gp.To.Format("2006-01-02 15:04:05"),   // Format timestamp
		gp.Tick,
	)
	if err != nil {
		db.log.Errorf("Error executing query: %s", err)
		return fmt.Errorf("failed to insert gas price period: %w", err)
	}

	// make sure gas price collection is initialized
	if db.initGasPrice != nil {
		db.initGasPrice.Do(func() { db.initGasPriceTable(); db.initGasPrice = nil })
	}

	// Log the number of rows affected
	affected, _ := res.RowsAffected()
	db.log.Infof("Rows affected: %d", affected)

	return nil

}

// GasPricePeriodCount calculates total number of gas price period records in the database.
func (db *MongoDbBridge) GasPricePeriodCount() (uint64, error) {
	return db.EstimateCount(db.client.Database(db.dbName).Collection(colGasPrice))
}

// GasPricePeriodCount calculates the total number of gas price period records in the database.
// GasPricePeriodCount calculates the total number of gas price period records in the database.
func (db *PostgreSQLBridge) GasPricePeriodCount() (int64, error) {
	// Define the SQL query to count rows in the 'gas_price_periods' table
	query := "SELECT COUNT(*) FROM gas_price_periods"

	// Execute the query and scan the result into a variable
	var count int64
	err := db.db.QueryRow(query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get gas price period count: %w", err)
	}

	// Return the count as uint64
	return int64(count), nil
}

// GasPriceTicks provides a list of gas price ticks for the given time period.
func (db *MongoDbBridge) GasPriceTicks(from *time.Time, to *time.Time) ([]types.GasPricePeriod, error) {
	// get the collection
	col := db.client.Database(db.dbName).Collection(colGasPrice)

	// find ticks inside the date/time range
	cursor, err := col.Find(context.Background(), bson.D{
		{Key: "from", Value: bson.D{{Key: "$gte", Value: from}}},
		{Key: "to", Value: bson.D{{Key: "$lte", Value: to}}},
	}, options.Find().SetSort(bson.D{{Key: "from", Value: 1}}))
	if err != nil {
		db.log.Errorf("can not pull gas price ticks; %s", err.Error())
		return nil, err
	}

	// make sure to close the cursor
	defer db.closeCursor(cursor)

	// load all the data from the database
	list := make([]types.GasPricePeriod, 0)
	for cursor.Next(context.Background()) {
		var row types.GasPricePeriod

		if err := cursor.Decode(&row); err != nil {
			db.log.Errorf("could not decode gas price tick; %s", err.Error())
			return nil, err
		}

		list = append(list, row)
	}

	return list, nil
}

func (db *PostgreSQLBridge) GasPriceTicks(from *time.Time, to *time.Time) ([]types.GasPricePeriod, error) {
	// Ensure the input times are valid
	if from == nil || to == nil {
		return nil, fmt.Errorf("invalid time range provided")
	}

	// Query to fetch gas price ticks in the given time range
	query := `
          SELECT type, open, close, min, max, avg, time_from, time_to, tick
          FROM gas_price_periods
          WHERE time_from >= $1 AND time_to <= $2
          ORDER BY time_from ASC`

	// Execute the query
	rows, err := db.db.Query(query, from, to)
	if err != nil {
		db.log.Errorf("cannot pull gas price ticks; %s", err.Error())
		return nil, err
	}
	defer rows.Close()

	// Load all the data from the database
	list := make([]types.GasPricePeriod, 0)
	for rows.Next() {
		var row types.GasPricePeriod
		if err := rows.Scan(
			&row.Type,
			&row.Open,
			&row.Close,
			&row.Min,
			&row.Max,
			&row.Avg,
			&row.From,
			&row.To,
			&row.Tick,
		); err != nil {
			db.log.Errorf("could not decode gas price tick; %s", err.Error())
			return nil, err
		}

		list = append(list, row)
	}

	// Handle any errors encountered during row iteration
	if err := rows.Err(); err != nil {
		db.log.Errorf("error iterating gas price ticks; %s", err.Error())
		return nil, err
	}

	return list, nil
}
