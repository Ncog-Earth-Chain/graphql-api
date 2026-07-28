// Package resolvers implements GraphQL resolvers to incoming API requests.
package resolvers

import (
	"context"
	"ncogearthchain-api-graphql/internal/repository"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Block represents resolvable blockchain block structure.
type Block struct {
	types.Block
}

// NewBlock builds new resolvable block structure.
func NewBlock(blk *types.Block) *Block {
	if blk == nil {
		return nil
	}
	return &Block{Block: *blk}
}

// Block resolves blockchain block by number or by hash. If neither is provided, the most recent block is given.
//
// Served from the index, which holds every block on its primary key, falling back to the
// node only for the head and for heights the indexer has not reached.
func (rs *rootResolver) Block(ctx context.Context, args *struct {
	Number *hexutil.Uint64
	Hash   *common.Hash
}) (*Block, error) {
	// do we have the number, or hash is not given?
	if args.Number != nil || args.Hash == nil {
		b, err := repository.R().IndexedBlockByNumber(ctx, args.Number)
		return NewBlock(b), err
	}

	// simply pull the block by hash
	b, err := repository.R().IndexedBlockByHash(ctx, args.Hash)
	return NewBlock(b), err
}

// Parent resolves parent block information to the given block.
//
// Once per block edge, so on a block page this fired as many times as the page was wide --
// each an eth_getBlockByHash for a block that is, almost always, another row of the very
// page being rendered.
func (blk *Block) Parent(ctx context.Context) (*Block, error) {
	// get the parent block by hash
	parent, err := repository.R().IndexedBlockByHash(ctx, &blk.ParentHash)
	return NewBlock(parent), err
}

// TxHashList resolves list of hashes of transaction bundled in the block.
func (blk *Block) TxHashList() []common.Hash {
	// make the container and fill it with data
	txs := make([]common.Hash, len(blk.Txs))
	for i, hash := range blk.Txs {
		txs[i] = *hash
	}
	return txs
}

// TxList resolves list of transaction details of the transactions bundled in the block.
// Served from the local index, not the node.
//
// This is the widest read amplification in the API: one transaction per hash in the block,
// resolved once per BLOCK of a block page. Through repository.Transaction each of those
// costs TWO serial JSON-RPC round trips (eth_getTransactionByHash +
// eth_getTransactionReceipt), so `blocks(count:250){edges{block{txList{hash}}}}` multiplied
// out to tens of thousands of node calls from one small POST -- for data Postgres already
// holds, indexed on the primary key. IndexedTransaction reads the index and falls back to
// the node only for what is not indexed yet (above the ingest watermark, or pending).
//
// ctx is threaded so the loop dies with the request rather than running on after the client
// or the resolver timeout has gone. TxList is a method on Block, not on the ApiResolver
// interface, so the signature change is local.
func (blk *Block) TxList(ctx context.Context) ([]*Transaction, error) {
	// make the container
	txs := make([]*Transaction, len(blk.Txs))

	// loop the hashes and extract transactions
	for i, hash := range blk.Txs {
		trx, err := repository.R().IndexedTransaction(ctx, hash)
		if err != nil {
			return nil, err
		}

		// make a resolvable transaction
		txs[i] = NewTransaction(trx)
	}

	return txs, nil
}

// TransactionCount resolves number of transactions in the block.
func (blk *Block) TransactionCount() *int32 {
	count := int32(len(blk.Txs))
	return &count
}
