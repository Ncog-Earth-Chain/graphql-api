// Package types implements different core types of the API.
package types

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"go.mongodb.org/mongo-driver/bson"
)

var (
	// BurnDecimalsCorrection is used to reduce precision of an amount of burned NEC
	BurnDecimalsCorrection = new(big.Int).SetUint64(10_000_000_000)

	// BurnNECDecimalsCorrection is used to convert reduced precision burned amount to NEC units.
	BurnNECDecimalsCorrection = float64(100_000_000)
)

// NecBurn represents deflation of native tokens by burning.
type NecBurn struct {
	BlockNumber  hexutil.Uint64 `bson:"block"`
	BlkTimeStamp time.Time      `bson:"ts"`
	Amount       hexutil.Big    `bson:"amount"`
	TxList       []common.Hash  `bson:"tx_list"`
}

// MarshalBSON returns a BSON document for the NEC burn.
func (burn *NecBurn) MarshalBSON() ([]byte, error) {
	amount := new(big.Int).Div(burn.Amount.ToInt(), BurnDecimalsCorrection)
	row := struct {
		Block     int64     `bson:"block"`
		TimeStamp time.Time `bson:"ts"`
		Value     string    `bson:"value"`
		Amount    int64     `bson:"amount"`
		TxList    []string  `bson:"tx_list"`
	}{
		Block:     int64(burn.BlockNumber),
		TimeStamp: burn.BlkTimeStamp,
		Value:     burn.Amount.String(),
		Amount:    amount.Int64(),
		TxList:    make([]string, len(burn.TxList)),
	}
	for i, v := range burn.TxList {
		row.TxList[i] = v.String()
	}
	return bson.Marshal(row)
}

// UnmarshalBSON updates the value from BSON source.
func (burn *NecBurn) UnmarshalBSON(data []byte) (err error) {
	var row struct {
		Block     int64     `bson:"block"`
		TimeStamp time.Time `bson:"ts"`
		Value     string    `bson:"value"`
		Amount    int64     `bson:"amount"`
		TxList    []string  `bson:"tx_list"`
	}

	err = bson.Unmarshal(data, &row)
	if err != nil {
		return err
	}

	burn.BlockNumber = hexutil.Uint64(row.Block)
	burn.BlkTimeStamp = row.TimeStamp
	burn.Amount = (hexutil.Big)(*hexutil.MustDecodeBig(row.Value))

	burn.TxList = make([]common.Hash, len(row.TxList))
	for i, v := range row.TxList {
		burn.TxList[i] = common.HexToHash(v)
	}
	return nil
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
