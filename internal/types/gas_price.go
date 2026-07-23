// Package types implements different core types of the API.
package types

import (
	"time"
)

const (
	// GasPricePeriodTypeSuggestion represents the type of gas price period data from the Ncogearthchain node suggestion call.
	GasPricePeriodTypeSuggestion = iota
)

const (
	// FiGasPriceTimeFrom is the name of the starting time stamp column in the collection.
	FiGasPriceTimeFrom = "from"

	// FiGasPriceTimeTo is the name of the ending time stamp column in the collection.
	FiGasPriceTimeTo = "to"

	/*
		// FiGasPriceAmountOpen is the name of the adjusted opening gas price amount column in the collection.
		FiGasPriceAmountOpen = "open"

		// FiGasPriceAmountMax is the name of the adjusted maximum gas price amount column in the collection.
		FiGasPriceAmountMax = "max"

		// FiGasPriceAmountMin is the name of the adjusted minimum gas price amount column in the collection.
		FiGasPriceAmountMin = "min"

		// FiGasPriceAmountAvg is the name of the adjusted average gas price amount column in the collection.
		FiGasPriceAmountAvg = "avg"

		// FiGasPriceAmountClose is the name of the adjusted closing gas price amount column in the collection.
		FiGasPriceAmountClose = "close"

		// FiGasPriceTimeTick is the name of the tick speed column in the collection.
		FiGasPriceTimeTick = "tick"
	*/
)

// GasPrice represents an extended gas price estimator.
type GasPrice struct {
	Fast    float64 `json:"fast"`
	Fastest float64 `json:"fastest"`
	SafeLow float64 `json:"safeLow"`
	Average float64 `json:"average"`
}

// GasPricePeriod represents a data set of interval of gas price
// estimation provided by the Ncogearthchain node.
type GasPricePeriod struct {
	Type  int8      `json:"type"`
	Open  int64     `json:"open"`
	Close int64     `json:"close"`
	Min   int64     `json:"min"`
	Max   int64     `json:"max"`
	Avg   int64     `json:"avg"`
	From  time.Time `json:"from"`
	To    time.Time `json:"to"`
	Tick  int64     `json:"tick"`
}
