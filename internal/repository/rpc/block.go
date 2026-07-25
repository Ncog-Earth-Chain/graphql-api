/*
Package rpc implements bridge to Forest full node API interface.

We recommend using local IPC for fast and the most efficient inter-process communication between the API server
and an Ncogearthchain/Forest node. Any remote RPC connection will work, but the performance may be significantly degraded
by extra networking overhead of remote RPC calls.

You should also consider security implications of opening Forest RPC interface for remote access.
If you considering it as your deployment strategy, you should establish encrypted channel between the API server
and Forest RPC interface with connection limited to specified endpoints.

We strongly discourage opening Forest RPC interface for unrestricted Internet access.
*/
package rpc

import (
	"fmt"
	"math/big"
	"ncogearthchain-api-graphql/internal/types"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
)

// BlockTypeLatest represents the latest available block in blockchain.
const (
	BlockTypeLatest   = "latest"
	BlockTypeEarliest = "earliest"
)

// MustBlockHeight returns the current block height
// of the blockchain. It returns nil if the block height can not be pulled.
func (nec *NecBridge) MustBlockHeight() *big.Int {
	var val hexutil.Big
	if err := nec.rpc.Call(&val, "nec_blockNumber"); err != nil {
		nec.log.Errorf("failed block height check; %s", err.Error())
		return nil
	}
	return val.ToInt()
}

// BlockHeight returns the current block height of the Ncogearthchain blockchain.
func (nec *NecBridge) BlockHeight() (*hexutil.Big, error) {
	// keep track of the operation
	nec.log.Debugf("checking current block height")

	// call for data
	var height hexutil.Big
	err := nec.rpc.Call(&height, "nec_blockNumber")
	if err != nil {
		nec.log.Error("block height could not be obtained")
		return nil, err
	}

	// inform and return
	nec.log.Debugf("current block height is %s", height.String())
	return &height, nil
}

// Block returns information about a blockchain block by encoded hex number, or by a type tag.
// For tag based loading use predefined BlockType contacts.
func (nec *NecBridge) Block(numTag *string) (*types.Block, error) {
	// keep track of the operation
	nec.log.Debugf("loading details of block num/tag %s", *numTag)

	// call for data
	var block types.Block
	err := nec.rpc.Call(&block, "nec_getBlockByNumber", numTag, false)
	if err != nil {
		nec.log.Error("block could not be extracted")
		return nil, err
	}

	// Detect the "block not found" situation. A non-existent block-by-number query returns a
	// zero-valued struct (JSON null unmarshals to all-zero fields), which we must reject. We cannot
	// key that on hash==0 alone: on Ncogearthchain/Opera-style chains a block hash encodes
	// epoch<<32|height, so the *real* genesis (epoch 0, height 0) legitimately has an all-zero hash.
	// The genesis is a real block with a real timestamp, whereas a not-found result is zero in every
	// field -- so require the timestamp to also be zero before treating this as not-found. Without
	// this the block scanner stalls forever on genesis and no chain ever gets indexed.
	if uint64(block.Number) == 0 && block.Hash.Big().Cmp(big.NewInt(0)) == 0 && uint64(block.TimeStamp) == 0 {
		nec.log.Debugf("block [%s] not found", *numTag)
		return nil, fmt.Errorf("block not found")
	}

	// keep track of the operation
	nec.log.Debugf("block #%d found at mark %s",
		uint64(block.Number), time.Unix(int64(block.TimeStamp), 0).String())
	return &block, nil
}

// BlockByHash returns information about a blockchain block by hash.
func (nec *NecBridge) BlockByHash(hash *string) (*types.Block, error) {
	// keep track of the operation
	nec.log.Debugf("loading details of block %s", *hash)

	// call for data
	var block types.Block
	err := nec.rpc.Call(&block, "nec_getBlockByHash", hash, false)
	if err != nil {
		nec.log.Error("block could not be extracted")
		return nil, err
	}

	// Detect the "block not found" situation. As in Block() above, a not-found result unmarshals to
	// an all-zero struct, but the real genesis legitimately sits at number 0 (with an all-zero hash on
	// Opera-style chains). Require the timestamp to also be zero so a genesis lookup by its real hash
	// still resolves rather than being misreported as not-found.
	if uint64(block.Number) == 0 && uint64(block.TimeStamp) == 0 {
		nec.log.Debugf("block [%s] not found", *hash)
		return nil, fmt.Errorf("block not found")
	}

	// inform and return
	nec.log.Debugf("block #%d found at mark %s by hash %s",
		uint64(block.Number), time.Unix(int64(block.TimeStamp), 0).String(), *hash)
	return &block, nil
}
