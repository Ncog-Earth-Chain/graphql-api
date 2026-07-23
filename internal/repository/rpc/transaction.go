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

	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	retypes "github.com/ethereum/go-ethereum/core/types"
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
