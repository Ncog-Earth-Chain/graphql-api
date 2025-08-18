package types

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// InternalTransaction represents an internal transaction (call, create, etc.)
type InternalTransaction struct {
	From         common.Address  `json:"from"`
	To           *common.Address `json:"to,omitempty"`
	Value        hexutil.Big     `json:"value"`
	Gas          hexutil.Uint64  `json:"gas"`
	GasUsed      *hexutil.Uint64 `json:"gasUsed,omitempty"`
	Input        hexutil.Bytes   `json:"input"`
	Type         string          `json:"type"`
	TraceAddress []int           `json:"traceAddress"`
	Error        *string         `json:"error,omitempty"`
}
