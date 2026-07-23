package types

import (
	"math/big"
	"time"
)

// DailyTrxVolume represents a volume of daily transaction aggregation.
type DailyTrxVolume struct {
	Day            string
	Stamp          time.Time
	Counter        int64
	AmountAdjusted int64
	Gas            int64

	// Amount is the exact total value transferred on the day, in wei.
	//
	// AmountAdjusted cannot carry it: it is the sum of a per-transaction value truncated
	// to gwei, so the daily total was short by up to (1e9 - 1) wei per transaction, and
	// an int64 of wei overflows at ~9.2 NEC of daily volume in any case.
	//
	// Not persisted by the MongoDB bridge -- it has no source column there -- so it is
	// nil on that path and callers must fall back to AmountAdjusted. The PostgreSQL
	// store always sets it.
	Amount *big.Int
}
