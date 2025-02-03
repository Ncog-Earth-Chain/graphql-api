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
	"fmt"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	retypes "github.com/ethereum/go-ethereum/core/types"
)

// Transaction returns information about a blockchain transaction by hash.
func (nec *NecBridge) Transaction(hash *common.Hash) (*types.Transaction, error) {
	// keep track of the operation
	nec.log.Debugf("loading transaction %s", hash.String())

	// call for data
	var trx types.Transaction
	err := nec.Rpc.Call(&trx, "nec_getTransactionByHash", hash)
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
		err := nec.Rpc.Call(&rec, "nec_getTransactionReceipt", hash)
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
	}

	// keep track of the operation
	nec.log.Debugf("transaction %s loaded", hash.String())
	return &trx, nil
}

// PendingTransactions fetches all pending transactions from the blockchain.
func (nec *NecBridge) PendingTransactions() ([]types.Transaction, error) {
	// Log the pending transactions fetch attempt
	nec.log.Infof("Fetching pending transactions from blockchain RPC...")

	// Call the RPC to get the list of pending transactions
	var pendingTxs []types.Transaction
	err := nec.Rpc.Call(&pendingTxs, "nec_pendingTransactions")
	if err != nil {
		nec.log.Errorf("Failed to fetch pending transactions: %v", err)
		return nil, fmt.Errorf("failed to fetch pending transactions: %w", err)
	}

	// Log and return the fetched pending transactions
	nec.log.Infof("Fetched %d pending transactions", len(pendingTxs))
	return pendingTxs, nil
}

// SendTransaction sends raw signed and RLP encoded transaction to the block chain.
func (nec *NecBridge) SendTransaction(tx hexutil.Bytes) (*common.Hash, error) {
	// keep track of the operation
	nec.log.Debug("sending new transaction to block chain")

	var hash common.Hash
	err := nec.Rpc.Call(&hash, "eth_sendRawTransaction", tx)
	if err != nil {
		nec.log.Error("transaction could not be sent")
		return nil, err
	}

	// keep track of the operation
	nec.log.Debugf("transaction has been accepted with hash %s", hash.String())
	return &hash, nil
}

// BlockByNumber fetches block details by block number
// BlockByNumber fetches block details by block number
func (nec *NecBridge) BlockByNumber(blockNumber hexutil.Uint64) (*types.Block, error) {
	// Log the block fetch attempt
	nec.log.Infof("Fetching block %d from blockchain RPC...", blockNumber)

	// Call the RPC for block data, passing 'true' for full transaction data
	var block types.Block
	err := nec.Rpc.Call(&block, "nec_getBlockByNumber", blockNumber, true) // Pass `true` to fetch full transaction data
	if err != nil {
		nec.log.Errorf("RPC call failed for block %d: %v", blockNumber, err)
		return nil, fmt.Errorf("failed to fetch block: %w", err)
	}

	// Log and return the fetched block
	nec.log.Infof("Fetched block %d: %+v", blockNumber, block)
	return &block, nil
}
