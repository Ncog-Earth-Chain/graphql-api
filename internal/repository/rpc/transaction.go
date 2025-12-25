/*
Package rpc implements bridge to Forest full node API interface.

We recommend using local IPC for fast and the most efficient inter-process communication between the API server
and an Ncogearthchain/Forest node. Any remote RPC connection will work, but the performance may be significantly degraded
by extra networking overhead of remote RPC calls.

You should also consider security implications of opening Forest RPC interface for a remote access.
If you considering it as your deployment strategy, you should establish encrypted channel between the API server
and Forest RPC interface with connection limited to specified endpoints.

We strongly discourage opening Forest RPC interface for unrestricted Internet access.
*/
package rpc

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	retypes "github.com/ethereum/go-ethereum/core/types"
)

// ddbInput represents the structure of DDB transaction input data
type ddbInput struct {
	ContractAddress string `json:"contractAddress"`
}

// parseDDBInput parses DDB transaction input data and extracts the contract address
func parseDDBInput(data []byte) *ddbInput {
	trimmed := bytes.TrimSpace(data)

	// Case 1: Already JSON
	if json.Valid(trimmed) {
		var payload ddbInput
		if err := json.Unmarshal(trimmed, &payload); err == nil {
			return &payload
		}
	}

	// Case 2: Base64(JSON)
	decoded, err := base64.StdEncoding.DecodeString(string(trimmed))
	if err != nil {
		return nil
	}

	if !json.Valid(decoded) {
		return nil
	}

	var payload ddbInput
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return nil
	}

	return &payload
}

// extractDDBContractAddress extracts contract address from DDB transaction if applicable
func extractDDBContractAddress(trx *types.Transaction) {
	// Check if this is a DDB transaction (sent to the DDB address)
	if trx.To == nil || trx.To.String() != "0x000000000000000000000000000000000000Dddb" {
		return
	}

	// Parse the DDB payload
	ddbPayload := parseDDBInput(trx.InputData)
	if ddbPayload == nil || ddbPayload.ContractAddress == "" {
		fmt.Printf("[DDB] Transaction %s: Failed to parse DDB payload from input data (size: %d bytes)\n", trx.Hash.String(), len(trx.InputData))
		return
	}

	// Extract and set the contract address
	contractAddr := common.HexToAddress(ddbPayload.ContractAddress)
	trx.ContractAddress = &contractAddr
	fmt.Printf("[DDB] Transaction %s: Extracted contract address: %s\n", trx.Hash.String(), contractAddr.String())
}

// Transaction returns information about a blockchain transaction by hash.
func (nec *NecBridge) Transaction(hash *common.Hash) (*types.Transaction, error) {
	// keep track of the operation
	nec.log.Debugf("loading transaction %s", hash.String())

	// call for data
	var trx types.Transaction
	err := nec.rpc.Call(&trx, "nec_getTransactionByHash", hash)
	if err != nil {
		nec.log.Error("transaction could not be extracted")
		return nil, err
	}

	// is there a block reference already?
	if trx.BlockNumber != nil {
		// get transaction receipt
		var rec struct {
			Index             hexutil.Uint64  `json:"transactionIndex"`
			CumulativeGasUsed hexutil.Uint64  `json:"cumulativeGasUsed"`
			GasUsed           hexutil.Uint64  `json:"gasUsed"`
			ContractAddress   *common.Address `json:"contractAddress,omitempty"`
			Status            hexutil.Uint64  `json:"status"`
			Logs              []retypes.Log   `json:"logs"`
		}

		// call for the transaction receipt data
		err := nec.rpc.Call(&rec, "nec_getTransactionReceipt", hash)
		if err != nil {
			nec.log.Errorf("can not get receipt for transaction %s", hash)
			return nil, err
		}

		// copy some data
		trx.Index = &rec.Index
		trx.CumulativeGasUsed = &rec.CumulativeGasUsed
		trx.GasUsed = &rec.GasUsed
		trx.ContractAddress = rec.ContractAddress
		trx.Status = &rec.Status
		trx.Logs = rec.Logs
		// Extract DDB contract address if this is a DDB transaction
		extractDDBContractAddress(&trx)
	}

	// keep track of the operation
	nec.log.Debugf("transaction %s loaded", hash.String())
	return &trx, nil
}

// SendTransaction sends raw signed and RLP encoded transaction to the block chain.
func (nec *NecBridge) SendTransaction(tx hexutil.Bytes) (*common.Hash, error) {
	// keep track of the operation
	nec.log.Debug("sending new transaction to block chain")

	var hash common.Hash
	err := nec.rpc.Call(&hash, "eth_sendRawTransaction", tx)
	if err != nil {
		nec.log.Error("transaction could not be sent")
		return nil, err
	}

	// keep track of the operation
	nec.log.Debugf("transaction has been accepted with hash %s", hash.String())
	return &hash, nil
}
