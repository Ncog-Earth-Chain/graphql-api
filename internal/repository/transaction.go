/*
Package repository implements repository for handling fast and efficient access to data required
by the resolvers of the API server.

Internally it utilizes RPC to access Ncogearthchain/Forest full node for blockchain interaction. Mongo database
for fast, robust and scalable off-chain data storage, especially for aggregated and pre-calculated data mining
results. BigCache for in-memory object storage to speed up loading of frequently accessed entities.
*/
package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"ncogearthchain-api-graphql/internal/repository/cache"
	"ncogearthchain-api-graphql/internal/repository/db/pg"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	eth "github.com/ethereum/go-ethereum/rpc"
)

// ErrTransactionNotFound represents an error returned if a transaction can not be found.
var ErrTransactionNotFound = errors.New("requested transaction can not be found in Ncogearthchain blockchain")

// StoreBlockAtomic stores a block and ALL of its transactions in one database
// transaction, advancing the ingest watermark inside it.
//
// This is the write the indexer should use. A block is either wholly present or absent,
// and the watermark -- derived from the block table rather than asserted -- cannot pass a
// gap, so a scanner resuming from it re-scans anything incomplete instead of stepping
// over it forever.
func (p *proxy) StoreBlockAtomic(ctx context.Context, block *types.Block, txs []*types.Transaction) error {
	return p.pg.StoreBlock(ctx, &pg.BlockData{Block: block, Transactions: txs})
}

// CacheTransaction puts a transaction to the internal ring cache.
func (p *proxy) CacheTransaction(trx *types.Transaction) {
	p.cache.AddTransaction(trx)
}

// Transaction returns a transaction at Ncogearthchain blockchain by a hash, nil if not found.
// If the transaction is not found, ErrTransactionNotFound error is returned.
func (p *proxy) Transaction(hash *common.Hash) (*types.Transaction, error) {
	p.log.Debugf("requested transaction %s", hash.String())

	// try to use the in-memory cache
	if trx := p.cache.PullTransaction(hash); trx != nil {
		p.log.Debugf("transaction %s loaded from cache", hash.String())
		return trx, nil
	}

	// return the value
	trx, err := p.LoadTransaction(hash)
	if err != nil {
		return nil, err
	}

	// push the transaction to the cache to speed things up next time
	// we don't cache pending transactions since it would cause issues
	// when re-loading data of such transactions on the client side
	if trx.BlockHash == nil {
		p.log.Debugf("pending transaction %s found", trx.Hash)
		return trx, nil
	}

	// store to cache
	p.cache.PushTransaction(trx)
	return trx, nil
}

// LoadTransactions loads a block's transactions from the node in BATCHED JSON-RPC calls.
//
// The ingest counterpart to LoadTransaction, and the reason the indexer can keep up with a
// large chain. Loading transactions one at a time costs two SEQUENTIAL round trips each, so
// a block cost 2N and the chain cost twice its transaction count; round-trip latency, not
// PostgreSQL, was the binding constraint (measured: ~2,372 writes/s into the database
// against 1/(2 x RTT) out of the RPC path). Batching makes it a fixed two round trips per
// chunk however many transactions the chunk holds.
//
// Node-authoritative on purpose, exactly like LoadTransaction and for the same reason: this
// is the path a re-ingest uses, and reading the local index here would hand a reorg back the
// row it is about to replace.
//
// Results are positional -- out[i] is the transaction for hashes[i] -- and any hash the node
// cannot answer fails the whole call, so a partial block is never mistaken for a complete one.
func (p *proxy) LoadTransactions(ctx context.Context, hashes []common.Hash) ([]*types.Transaction, error) {
	if len(hashes) == 0 {
		return nil, nil
	}

	txs, err := p.rpc.Transactions(ctx, hashes)
	if err != nil {
		return nil, err
	}

	// Cache the mined ones, matching what Transaction() does for a single load. Pending
	// transactions are deliberately not cached: their fields change when they are mined.
	for _, trx := range txs {
		if trx != nil && trx.BlockHash != nil {
			p.cache.PushTransaction(trx)
		}
	}
	return txs, nil
}

// IndexedTransaction serves a transaction to the READ path, preferring the local index.
//
// Separate from Transaction above, and deliberately so. Transaction is what the INGEST path
// calls (svc/dispatch_blk.go loadTxs), and it must stay node-authoritative: a re-ingest --
// a reorg replacing a block's contents, or a gap heal -- would otherwise read back the very
// row it is about to replace and re-store it unchanged, defeating the delete-then-insert
// that makes re-ingest a repair.
//
// Why the read path wants the index instead: Block.TxList resolves one transaction per hash
// in the block, and Transaction costs TWO serial JSON-RPC round trips each
// (eth_getTransactionByHash + eth_getTransactionReceipt, rpc/transaction.go). A block page
// therefore multiplied out to hundreds of node calls for data Postgres already holds,
// indexed on the primary key.
//
// Every field the GraphQL Transaction type exposes survives the substitution; that was
// checked field by field rather than assumed. The columns scanTransaction does not fill are
// Logs, PubKey, LargeInput, TrxIndex and DDB -- and none of them back a resolver: `logs` is
// served from tx_log through its own root query, `pubKey` is not in the schema at all
// (deliberately -- 00002_block_tx.sql explains why signatures and public keys are not
// stored), LargeInput is json:"-", TrxIndex has no consumer, and `ddb` resolves through
// DdbOperationAt(blockNumber, index), both of which ARE populated.
func (p *proxy) IndexedTransaction(ctx context.Context, hash *common.Hash) (*types.Transaction, error) {
	if hash == nil {
		return nil, fmt.Errorf("no transaction hash given")
	}

	if trx := p.cache.PullTransaction(hash); trx != nil {
		return trx, nil
	}

	// A database error must not turn a readable transaction into a failed request: the node
	// can still answer. Log it and fall through.
	trx, err := p.pg.Transaction(ctx, hash)
	if err != nil {
		p.log.Errorf("can not read transaction %s from the index; falling back to the node: %s",
			hash.String(), err.Error())
	} else if trx != nil {
		return trx, nil
	}

	// Not indexed yet -- above the ingest watermark, or pending. The node is authoritative
	// for those, and Transaction caches what it loads.
	return p.Transaction(hash)
}

// LoadTransaction returns a transaction at Ncogearthchain blockchain
// by a hash loaded directly from the node.
func (p *proxy) LoadTransaction(hash *common.Hash) (*types.Transaction, error) {
	return p.rpc.Transaction(hash)
}

// SendTransaction sends raw signed and RLP encoded transaction to the block chain.
func (p *proxy) SendTransaction(tx hexutil.Bytes) (*types.Transaction, error) {
	p.log.Debugf("announcing trx %s", tx.String())

	// try to send it and get the tx hash
	hash, err := p.rpc.SendTransaction(tx)
	if err != nil {
		p.log.Errorf("can not send transaction to block chain; %s", err.Error())
		return nil, err
	}

	// check the hash makes sense by comparing it to empty hash
	if bytes.Compare(hash.Bytes(), common.Hash{}.Bytes()) == 0 {
		p.log.Criticalf("transaction not send; %s", tx.String())
		return nil, fmt.Errorf("transaction could not be send")
	}

	// we do have the hash, so we can use it to get the transaction details
	// we always need to go to RPC, and we will not try to store the transaction in cache yet
	trx, err := p.rpc.Transaction(hash)
	if err != nil {
		// transaction simply not found?
		if err == eth.ErrNoResult {
			p.log.Warning("transaction not found in the blockchain")
			return nil, ErrTransactionNotFound
		}

		// something went wrong
		return nil, err
	}

	// do we have the transaction we expected?
	if bytes.Compare(hash.Bytes(), trx.Hash.Bytes()) != 0 {
		p.log.Criticalf("transaction %s not confirmed, got %s", hash.String(), trx.Hash.String())
		return nil, fmt.Errorf("transaction %s could not be confirmed", hash.String())
	}

	// log transaction hash
	p.log.Noticef("trx %s from %s submitted", hash.String(), trx.From.String())
	return trx, nil
}

// Transactions pulls list of transaction hashes starting on the specified cursor.
// If the initial transaction cursor is not provided, we start on top, or bottom based on count value.
//
// No-number boundaries are handled as follows:
//   - For positive count we start from the most recent transaction and scan to older transactions.
//   - For negative count we start from the first transaction and scan to newer transactions.
func (p *proxy) Transactions(ctx context.Context, cursor *string, count int32) (*types.TransactionList, error) {
	// we may be able to pull the list faster than from the db
	if cursor == nil && count > 0 && count < cache.TransactionRingCacheSize {
		// pull the quick list
		tl := p.cache.ListTransactions(int(count))

		// does it make sense? if so, make the list from it
		if len(tl) > 0 {
			return &types.TransactionList{
				Collection: tl,
				Total:      uint64(p.MustEstimateTransactionsCount()),
				First:      tl[0].Uid(),
				Last:       tl[len(tl)-1].Uid(),
				IsStart:    true,
				IsEnd:      false,
			}, nil
		}
	}

	// use slow trx list pulling
	rows, err := p.pg.TransactionList(ctx, derefCursor(cursor), count)
	if err != nil {
		return nil, err
	}
	total, err := p.pg.TransactionsCount(ctx)
	if err != nil {
		return nil, err
	}
	// NOT exact: TransactionsCount reads pg_class.reltuples rather than counting the
	// largest table in the database on every request.
	return buildTransactionList(rows, total, count, derefCursor(cursor), false), nil
}

// StoreGasPricePeriod stores the given gas price period data in the persistent storage
func (p *proxy) StoreGasPricePeriod(ctx context.Context, gp *types.GasPricePeriod) error {
	return p.pg.AddGasPricePeriod(ctx, gp)
}
