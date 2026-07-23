// Package types implements different core types of the API.
package types

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

var (
	// BurnDecimalsCorrection is used to reduce precision of an amount of burned NEC
	BurnDecimalsCorrection = new(big.Int).SetUint64(10_000_000_000)

	// BurnNECDecimalsCorrection is used to convert reduced precision burned amount to NEC units.
	BurnNECDecimalsCorrection = float64(100_000_000)
)

// NecBurn represents deflation of native tokens by burning.
type NecBurn struct {
	BlockNumber  hexutil.Uint64
	BlkTimeStamp time.Time
	Amount       hexutil.Big
	TxList       []common.Hash
}

// Timestamp return UNIX stamp of the burn.
func (burn NecBurn) Timestamp() hexutil.Uint64 {
	return hexutil.Uint64(burn.BlkTimeStamp.Unix())
}

// NecValue returns NEC amount of burned tokens.
func (burn NecBurn) NecValue() float64 {
	return float64(new(big.Int).Div(burn.Amount.ToInt(), BurnDecimalsCorrection).Int64()) / BurnNECDecimalsCorrection
}

// Value returns NEC amount of burned tokens.
func (burn *NecBurn) Value() int64 {
	return new(big.Int).Div(burn.Amount.ToInt(), BurnDecimalsCorrection).Int64()
}
