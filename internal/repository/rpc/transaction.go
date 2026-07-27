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
	"context"
	"encoding/json"
	"fmt"

	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	retypes "github.com/ethereum/go-ethereum/core/types"
	nec2 "github.com/ethereum/go-ethereum/rpc"
)

// extractDDBContractAddress decodes a DDB commit tx's endorsement proof ("DDBR"+RLP, or legacy
// "DDBE"+JSON) and sets trx.ContractAddress to the data-contract address it targets, for explorer
// display. Non-fatal: on any decode miss it just leaves ContractAddress unset (Debug-level log).
func (nec *NecBridge) extractDDBContractAddress(trx *types.Transaction) {
	if !types.IsDDBTransaction(trx.To) {
		return
	}
	info, err := decodeDdbCommitTxData(trx.InputData)
	if err != nil {
		nec.log.Debugf("DDB tx %s: could not decode commit payload (%d bytes): %v", trx.Hash.String(), len(trx.InputData), err)
		return
	}
	// Keep the whole decoded record, not just the address.
	//
	// This is the change that gives the explorer DDB history. Everything below already
	// ran; the result was used for one field and thrown away, and because the node serves
	// no DDB history RPC, that discarded record was unrecoverable.
	trx.DDB = ddbCommitFromInfo(info)

	addrStr := ddbContractAddressFromOperation(info.Operation)
	contractAddr := common.HexToAddress(addrStr)
	// Leave ContractAddress nil for a zero/absent address (a schema-scoped call/grant, or the node's
	// always-serialized zero top-level contractAddress). Setting a non-nil &0x0000...0000 here would make
	// a DDB commit tx look like a contract creation at the zero address in the explorer + wallet history.
	if addrStr == "" || contractAddr == (common.Address{}) {
		nec.log.Debugf("DDB tx %s: operation carries no non-zero contract address (scoped by schema only)", trx.Hash.String())
		return
	}
	trx.ContractAddress = &contractAddr
	nec.log.Debugf("DDB tx %s: data-contract %s (requester %s, %d sigs)", trx.Hash.String(), contractAddr.String(), info.Requester.String(), info.Signatures)
}

// Transaction returns information about a blockchain transaction by hash.
func (nec *NecBridge) Transaction(hash *common.Hash) (*types.Transaction, error) {
	// keep track of the operation
	nec.log.Debugf("loading transaction %s", hash.String())

	// call for data
	var trx types.Transaction
	err := nec.rpc.Call(&trx, "eth_getTransactionByHash", hash)
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
		err := nec.rpc.Call(&rec, "eth_getTransactionReceipt", hash)
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
		nec.extractDDBContractAddress(&trx)
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

// RawTransaction fetches a transaction's canonical RLP encoding from the node.
//
// eth_getRawTransactionByHash returns tx.MarshalBinary(), which for a legacy transaction
// on this chain is the RLP of the inner struct carrying Signature, PubKey, ChainID, From
// and SigVer. That is what lets the explorer prove sender attribution without storing
// ~2592-byte public keys: the blob is self-verifying, because keccak256 of it equals the
// transaction hash exactly.
//
// An empty result is not an error. It means this node cannot answer -- the call needs
// TxIndex enabled and full history -- and the caller renders that as NULL rather than as
// a failed verification, which is a different claim.
func (nec *NecBridge) RawTransaction(ctx context.Context, hash *common.Hash) ([]byte, error) {
	var raw hexutil.Bytes
	if err := nec.rpc.CallContext(ctx, &raw, "eth_getRawTransactionByHash", hash); err != nil {
		nec.log.Debugf("raw transaction %s not available; %s", hash.String(), err.Error())
		return nil, nil
	}
	return raw, nil
}

// ddbCommitFromInfo maps the decoded proof onto the domain type, pulling the operation's
// own fields out of its verbatim JSON.
//
// Every field is optional on purpose: an operation scoped only by schema name has no
// contract address, a call has no version, and a contract's first operation has no prior
// state hash. Absent and zero are kept distinct throughout -- the node always serializes a
// zero address for an unset contract, so treating zero as a real value would attribute
// every schema-scoped operation to the zero address.
func ddbCommitFromInfo(info *DdbCommitInfo) *types.DdbCommit {
	c := &types.DdbCommit{
		Operation:          info.Operation,
		RequestID:          info.RequestID,
		Requester:          info.Requester,
		OperationHash:      info.OperationHash,
		DataHash:           info.DataHash,
		StateHash:          info.StateHash,
		Epoch:              hexutil.Uint64(info.Epoch),
		PriorPostStateHash: info.PriorPostStateHash,
		PostStateHash:      info.PostStateHash,
		ValidatorSet:       info.ValidatorSet,
		Signatures:         int32(info.Signatures),
	}

	// The operation payload. Version is a STRING on the wire ("1", "1.2.0"), not a
	// number, and the fields carry snake_case aliases from the older format.
	var op struct {
		Type         int16  `json:"type"`
		SchemaName   string `json:"schema_name"`
		ContractName string `json:"contract_name"`
		Name         string `json:"name"`
		Version      string `json:"version"`
		Author       string `json:"author"`
		Creator      string `json:"creator"`
	}
	if err := json.Unmarshal(info.Operation, &op); err == nil {
		c.OpType = op.Type
		c.SchemaName = op.SchemaName
		c.ContractName = op.ContractName
		if c.ContractName == "" {
			c.ContractName = op.Name
		}
		c.Version = op.Version

		author := op.Author
		if author == "" {
			author = op.Creator
		}
		if nonZeroAddr(author) {
			a := common.HexToAddress(author)
			c.Author = &a
		}
	}

	if addr := ddbContractAddressFromOperation(info.Operation); nonZeroAddr(addr) {
		a := common.HexToAddress(addr)
		c.ContractAddress = &a
	}
	return c
}

// txBatchChunk bounds how many JSON-RPC requests go into one batch.
//
// go-ethereum's server caps both the number of requests in a batch and the total response
// size, and the caps are not advertised, so a block with thousands of transactions must not
// be sent as one enormous batch. 128 request pairs is well inside any published default and
// still turns a 1,000-transaction block from 2,000 round trips into 16.
const txBatchChunk = 128

// Transactions loads a set of transactions by hash in BATCHED JSON-RPC calls.
//
// This is the ingest path's loader, and the reason it exists is throughput. Loading one
// transaction costs two SEQUENTIAL round trips (eth_getTransactionByHash, then
// eth_getTransactionReceipt), so a block was indexed at 2N round trips and the whole chain
// at 2 x (transaction count). Round-trip latency, not the database, was the binding
// constraint: measured against this codebase, PostgreSQL sustains ~2,372 transaction writes
// per second while the RPC path caps out at 1/(2 x RTT) -- about 1,000/s on a unix socket
// and 50/s across a 10 ms link. A terabyte of `tx` is ~1.76 billion rows at the measured
// 626 bytes each, so at 1,000/s a full backfill runs for about three weeks.
//
// Batching collapses that to a fixed TWO round trips per chunk regardless of how many
// transactions the chunk holds, because the whole point of a JSON-RPC batch is that the
// server answers every element of it in one response.
//
// Semantics are deliberately identical to Transaction() called in a loop, including the
// failure mode: any element that errors, or that the node answers with a null, fails the
// WHOLE call. The caller (svc/dispatch_blk.go loadTxs) treats a partial block as a block
// that must be retried rather than recorded, and that invariant is what keeps a gap from
// becoming permanent -- it must not be weakened here for throughput.
func (nec *NecBridge) Transactions(ctx context.Context, hashes []common.Hash) ([]*types.Transaction, error) {
	if len(hashes) == 0 {
		return nil, nil
	}

	out := make([]*types.Transaction, 0, len(hashes))
	for start := 0; start < len(hashes); start += txBatchChunk {
		end := start + txBatchChunk
		if end > len(hashes) {
			end = len(hashes)
		}
		chunk, err := nec.transactionChunk(ctx, hashes[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, chunk...)
	}
	return out, nil
}

// txReceiptFields is the subset of the receipt the transaction record needs. It mirrors the
// anonymous struct in Transaction() exactly; the two must not drift.
type txReceiptFields struct {
	Index             hexutil.Uint64  `json:"transactionIndex"`
	CumulativeGasUsed hexutil.Uint64  `json:"cumulativeGasUsed"`
	GasUsed           hexutil.Uint64  `json:"gasUsed"`
	ContractAddress   *common.Address `json:"contractAddress,omitempty"`
	Status            hexutil.Uint64  `json:"status"`
	Logs              []retypes.Log   `json:"logs"`
}

// transactionChunk loads one batch worth of transactions and their receipts.
func (nec *NecBridge) transactionChunk(ctx context.Context, hashes []common.Hash) ([]*types.Transaction, error) {
	// Round trip 1: every transaction body.
	txs := make([]types.Transaction, len(hashes))
	batch := make([]nec2.BatchElem, len(hashes))
	for i := range hashes {
		batch[i] = nec2.BatchElem{
			Method: "eth_getTransactionByHash",
			Args:   []interface{}{hashes[i]},
			Result: &txs[i],
		}
	}
	if err := nec.rpc.BatchCallContext(ctx, batch); err != nil {
		return nil, fmt.Errorf("transaction batch failed: %w", err)
	}

	// A per-element error is the node declining ONE transaction, which is exactly the case
	// the serial loader turned into a failed block. Keep that.
	for i := range batch {
		if batch[i].Error != nil {
			return nil, fmt.Errorf("transaction %s could not be loaded: %w", hashes[i].String(), batch[i].Error)
		}
		// A null result leaves the target zero-valued rather than raising an error, so an
		// unknown hash would otherwise pass through as an all-zero transaction and be stored
		// as though it were real.
		if txs[i].Hash == (common.Hash{}) {
			return nil, fmt.Errorf("transaction %s not found on the node", hashes[i].String())
		}
	}

	// Round trip 2: the receipt for every transaction that is mined. An unmined one has no
	// receipt to ask for, and asking would return null and fail the block.
	recs := make([]txReceiptFields, len(hashes))
	rbatch := make([]nec2.BatchElem, 0, len(hashes))
	rindex := make([]int, 0, len(hashes))
	for i := range txs {
		if txs[i].BlockNumber == nil {
			continue
		}
		rbatch = append(rbatch, nec2.BatchElem{
			Method: "eth_getTransactionReceipt",
			Args:   []interface{}{hashes[i]},
			Result: &recs[i],
		})
		rindex = append(rindex, i)
	}
	if len(rbatch) > 0 {
		if err := nec.rpc.BatchCallContext(ctx, rbatch); err != nil {
			return nil, fmt.Errorf("receipt batch failed: %w", err)
		}
		for j := range rbatch {
			if rbatch[j].Error != nil {
				i := rindex[j]
				return nil, fmt.Errorf("receipt for transaction %s could not be loaded: %w",
					hashes[i].String(), rbatch[j].Error)
			}
		}
	}

	out := make([]*types.Transaction, len(hashes))
	for i := range txs {
		trx := txs[i]
		if trx.BlockNumber != nil {
			rec := recs[i]
			trx.Index = &rec.Index
			trx.CumulativeGasUsed = &rec.CumulativeGasUsed
			trx.GasUsed = &rec.GasUsed
			trx.ContractAddress = rec.ContractAddress
			trx.Status = &rec.Status
			trx.Logs = rec.Logs
			// Local decode, no round trip -- but it must still run, because it is what gives
			// the explorer its DDB history.
			nec.extractDDBContractAddress(&trx)
		}
		out[i] = &trx
	}
	return out, nil
}
