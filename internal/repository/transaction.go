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
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	eth "github.com/ethereum/go-ethereum/rpc"
)

// ErrTransactionNotFound represents an error returned if a transaction can not be found.
var ErrTransactionNotFound = errors.New("requested transaction can not be found in Ncogearthchain blockchain")

// StoreTransaction notifies a new incoming transaction from blockchain to the repository.
// func (p *proxy) StoreTransaction(block *types.Block, trx *types.Transaction) error {
// 	return p.pdDB.AddTransaction(block, trx)
// }

// StoreTransaction stores a transaction to the database.
// StoreTransaction stores a transaction to the database.
func (p *proxy) StoreTransaction(block *types.Block, trx *types.Transaction) error {
	p.log.Infof("Fetching pending transactions from blockchain to store in DB")

	// Fetch pending transactions from blockchain
	pendingTxs, err := p.getPendingTransactionsFromBlockchain()
	if err != nil {
		p.log.Errorf("Error fetching pending transactions from blockchain: %v", err)
		return err
	}

	// Loop through fetched pending transactions and store them
	for _, trx := range pendingTxs {
		p.log.Infof("Processing pending transaction %s", trx.Hash.String())

		// Set placeholder values for pending transactions
		trx.BlockHash = nil   // No block hash for pending transactions
		trx.BlockNumber = nil // No block number for pending transactions

		// Store the pending transaction directly into the database (no caching)
		err := p.pdDB.AddTransaction(nil, &trx) // Use AddTransaction for pending
		if err != nil {
			p.log.Errorf("Error storing pending transaction %s in DB: %v", trx.Hash.String(), err)
		} else {
			p.log.Infof("Pending transaction %s successfully stored in PostgreSQL!", trx.Hash.String())
		}
	}

	return nil
}

// CacheTransaction puts a transaction to the internal ring cache.
func (p *proxy) CacheTransaction(trx *types.Transaction) {
	//p.cache.AddTransaction(trx)
}

// Transaction returns a transaction at Ncogearthchain blockchain by a hash, nil if not found.
// If the transaction is not found, ErrTransactionNotFound error is returned.
// func (p *proxy) Transaction(hash *common.Hash) (*types.Transaction, error) {
// 	p.log.Debugf("requested transaction %s", hash.String())

// 	// try to use the in-memory cache
// 	if trx := p.cache.PullTransaction(hash); trx != nil {
// 		p.log.Debugf("transaction %s loaded from cache", hash.String())
// 		return trx, nil
// 	}

// 	// return the value
// 	trx, err := p.LoadTransaction(hash)
// 	if err != nil {
// 		return nil, err
// 	}

// 	// push the transaction to the cache to speed things up next time
// 	// we don't cache pending transactions since it would cause issues
// 	// when re-loading data of such transactions on the client side
// 	if trx.BlockHash == nil {
// 		p.log.Debugf("pending transaction %s found", trx.Hash)
// 		return trx, nil
// 	}

// 	// store to cache
// 	p.cache.PushTransaction(trx)
// 	return trx, nil
// }

func (p *proxy) Transaction(hash *common.Hash) (*types.Transaction, error) {
	p.log.Debugf("requested transaction %s", hash.String())

	// Fetch the transaction directly from the database or blockchain (skip cache)
	trx, err := p.LoadTransaction(hash)
	if err != nil {
		return nil, err
	}

	return trx, nil
}

func (p *proxy) getPendingTransactionsFromBlockchain() ([]types.Transaction, error) {
	p.log.Infof("Fetching pending transactions from blockchain via RPC")

	var pendingTxs []types.Transaction
	err := p.rpc.Rpc.CallContext(context.Background(), &pendingTxs, "nec_pendingTransactions")
	if err != nil {
		p.log.Errorf("Failed to fetch pending transactions from blockchain: %v", err)
		return nil, err
	}

	p.log.Infof("Fetched pending transactions from blockchain: %v", len(pendingTxs))

	return pendingTxs, nil
}

// LoadTransaction returns a transaction at Ncogearthchain blockchain
// by a hash loaded directly from the node.
func (p *proxy) LoadTransaction(hash *common.Hash) (*types.Transaction, error) {
	return p.rpc.Transaction(hash)
}

// LoadTransaction loads a transaction from the blockchain by hash.
// func (p *proxy) LoadTransaction(hash *common.Hash) (*types.Transaction, error) {
// 	p.log.Infof("Loading transaction with hash: %s", hash.String())

// 	// First, try to fetch from the blockchain (confirmed transactions)
// 	trx, err := p.rpc.Transaction(hash)
// 	if err != nil {
// 		p.log.Errorf("Failed to load transaction %s: %v", hash.String(), err)

// 		// If the transaction is not found, try fetching from the pending transactions
// 		p.log.Infof("Transaction %s not found in confirmed transactions, checking pending...", hash.String())

// 		// Fetch pending transactions
// 		pendingTxs, err := p.FetchPendingTransactions()
// 		if err != nil {
// 			p.log.Errorf("Failed to fetch pending transactions: %v", err)
// 			return nil, err
// 		}

// 		// Search for the specific transaction in pending transactions
// 		for _, pendingTx := range pendingTxs {
// 			if pendingTx.Hash == *hash {
// 				p.log.Infof("Transaction %s found in pending transactions.", hash.String())

// 				// Store the pending transaction in DB
// 				err := p.StoreTransaction(nil, &pendingTx) // No block reference for pending
// 				if err != nil {
// 					p.log.Errorf("Failed to store pending transaction %s in DB: %v", hash.String(), err)
// 				}

// 				return &pendingTx, nil
// 			}
// 		}

// 		// If the transaction is not found in pending transactions either
// 		p.log.Errorf("Transaction %s not found in pending transactions.", hash.String())
// 		return nil, fmt.Errorf("transaction %s not found", hash.String())
// 	}

// 	// If the transaction was found in confirmed transactions
// 	p.log.Infof("Loaded confirmed transaction: %+v", trx)
// 	err = p.StoreTransaction(nil, trx) // Store confirmed transaction (no block reference)
// 	if err != nil {
// 		p.log.Errorf("Failed to store confirmed transaction %s in DB: %v", hash.String(), err)
// 	}

// 	return trx, nil
// }

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
func (p *proxy) Transactions(cursor *string, count int32) (*types.PostTransactionList, error) {
	// we may be able to pull the list faster than from the db
	if cursor == nil && count > 0 && count < cache.TransactionRingCacheSize {
		// pull the quick list
		tl := p.cache.ListTransactions(int(count))

		// does it make sense? if so, make the list from it
		if len(tl) > 0 {
			return &types.PostTransactionList{
				Collection: tl,
				Total:      uint64(p.MustEstimateTransactionsCount()),
				First:      tl[0].Uid(),
				Last:       tl[len(tl)-1].Uid(),
				IsStart:    true,
				IsEnd:      false,
				Filter:     nil,
			}, nil
		}
	}

	// use slow trx list pulling
	return p.pdDB.Transactions(cursor, count, "")
}

// StoreGasPricePeriod stores the given gas price period data in the persistent storage
func (p *proxy) StoreGasPricePeriod(gp *types.GasPricePeriod) error {
	return p.pdDB.AddGasPricePeriod(gp)
}
