// Package svc implements blockchain data processing services.
package svc

import (
	"fmt"
	"ncogearthchain-api-graphql/internal/types"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// trxBufferCapacity is the number of new packed transactions kept in the trx channel.
const trxBufferCapacity = 50000

// eventTrx represents a packed transaction event
// sent between block dispatcher and transaction dispatcher
type eventTrx struct {
	blk *types.Block
	trx *types.Transaction
}

// blockDispatcher implements a service responsible for processing new blocks on the blockchain.
type blockDispatcher struct {
	service
	onBlock        chan *types.Block
	inBlock        chan *types.Block
	outTransaction chan *eventTrx
	outDispatched  chan uint64
}

// name returns the name of the service used by orchestrator.
func (bld *blockDispatcher) name() string {
	return "block dispatcher"
}

// init prepares the block dispatcher to perform its function.
func (bld *blockDispatcher) init() {
	bld.sigStop = make(chan bool, 1)
	bld.outTransaction = make(chan *eventTrx, trxBufferCapacity)
	bld.outDispatched = make(chan uint64, blsBlockBufferCapacity)
}

// run starts the block dispatcher
func (bld *blockDispatcher) run() {
	// make sure we are orchestrated
	if bld.mgr == nil {
		panic(fmt.Errorf("no svc manager set on %s", bld.name()))
	}

	// signal orchestrator we started and go
	bld.mgr.started(bld)
	go bld.execute()
}

// execute collects blocks from an input channel
// and processes them.
func (bld *blockDispatcher) execute() {
	// make sure to clean up
	defer func() {
		// close our channels
		close(bld.sigStop)
		close(bld.outTransaction)
		close(bld.outDispatched)

		// signal we are done
		bld.mgr.finished(bld)
	}()

	// loop here
	for {
		select {
		case <-bld.sigStop:
			return

		case blk, ok := <-bld.inBlock:
			// do we have a working channel?
			if !ok {
				log.Notice("block channel closed, terminating %s", bld.name())
				return
			}

			// process the new block
			log.Debugf("block #%d arrived", uint64(blk.Number))
			if !bld.process(blk) {
				continue
			}

			// broadcast the block event
			select {
			case bld.onBlock <- blk:
			case <-time.After(200 * time.Millisecond):
			}

			// add the block to the ring
			repo.CacheBlock(blk)
		}
	}
}

// process the given block: load every transaction, store the block ATOMICALLY, then fan
// the transactions out for derived processing.
//
// The order here is the whole point, and it is the reverse of what this function used to
// do. Previously the first statement announced the block as dispatched -- before a single
// byte of its content had been loaded -- and a transaction whose RPC fetch failed was
// silently skipped, leaving the block recorded as complete when it was not. Because the
// scanner only ever rewinds a fixed rescan depth, a hole older than that window could
// never be revisited: it was permanent, and afterwards nothing distinguished it from a
// range that genuinely had no transactions.
//
// Now: load everything first, fail the whole block if anything is missing, write it in one
// database transaction, and only then report it dispatched. A block is either wholly
// present or absent, and the ingest watermark -- derived inside that same transaction --
// cannot advance past a gap.
func (bld *blockDispatcher) process(blk *types.Block) bool {
	txs, ok := bld.loadTxs(blk)
	if !ok {
		// Deliberately NOT dispatched. Leaving the block unannounced is what makes the
		// scanner come back for it; announcing it here is what used to make the hole
		// permanent.
		return true
	}

	// One database transaction for the block, its transactions, their logs and their
	// account edges. The watermark advances inside it, so it can never claim progress
	// the data does not back.
	if err := repo.StoreBlockAtomic(bgCtx(), blk, txs); err != nil {
		log.Errorf("block #%d not stored; %s", blk.Number, err.Error())
		return true
	}

	// Report dispatched only after the block is durable.
	select {
	case bld.outDispatched <- uint64(blk.Number):
	case <-bld.sigStop:
		bld.sigStop <- true
		return false
	}

	if len(txs) == 0 {
		// A block with no transactions burns nothing. If this is a RE-INGEST of a block that
		// previously had transactions -- a reorg reduced it to empty -- its old burn row and its
		// contribution to burn_total_wei are still stored: the transaction fan-out below is the
		// only path that corrects a burn, and it does not run for an empty block. Reconcile it
		// here, driven by the block rather than by its (absent) transactions, so a reorg cannot
		// leave a phantom burn behind. A block that never had a burn is a cheap no-op.
		if err := repo.ClearNecBurn(bgCtx(), uint64(blk.Number)); err != nil {
			log.Errorf("could not clear burn for empty block #%d; %s", blk.Number, err.Error())
		}
		log.Debugf("empty block #%d processed", blk.Number)
		return true
	}

	// Fan out for the DERIVED work -- accounts, logs, token transfers, burns. These read
	// the transaction that is already stored; they no longer own writing it.
	log.Debugf("%d transaction found in block #%d", len(txs), blk.Number)
	for _, trx := range txs {
		select {
		case bld.outTransaction <- &eventTrx{blk: blk, trx: trx}:
		case <-bld.sigStop:
			bld.sigStop <- true
			return false
		}
	}

	log.Debugf("block #%d processed", blk.Number)
	return true
}

// loadTxs loads every transaction of a block, or reports failure.
//
// All-or-nothing on purpose. The previous loop skipped a transaction it could not fetch
// and carried on, so one transient RPC error produced a block that looked complete and was
// missing a transaction forever. Returning false here costs a re-scan of one block, which
// is cheap and idempotent.
func (bld *blockDispatcher) loadTxs(blk *types.Block) ([]*types.Transaction, bool) {
	if len(blk.Txs) == 0 {
		return nil, true
	}

	// BATCHED, not one at a time. This loop was the indexer's throughput ceiling: each
	// repo.Transaction cost two SEQUENTIAL JSON-RPC round trips
	// (eth_getTransactionByHash then eth_getTransactionReceipt), so a block cost 2N round
	// trips and the chain cost twice its transaction count. Latency, not PostgreSQL, was
	// what bound it -- the database sustains ~2,372 transaction writes per second here,
	// while 1/(2 x RTT) is about 1,000/s on a unix socket and 50/s across a 10 ms link. At
	// the measured 626 bytes per row a terabyte of `tx` is ~1.76 billion transactions, so
	// the serial loader put a full backfill at roughly three weeks even colocated.
	//
	// LoadTransactions issues a fixed two round trips per chunk however many transactions
	// the chunk holds.
	hashes := make([]common.Hash, len(blk.Txs))
	for i, th := range blk.Txs {
		hashes[i] = *th
	}

	txs, err := repo.LoadTransactions(bgCtx(), hashes)
	if err != nil {
		// Unchanged semantics, and load-bearing: a block that cannot be loaded WHOLE is not
		// dispatched, so the scanner comes back for it. Recording a partial block as complete
		// is what made gaps permanent under MongoDB.
		log.Errorf("block #%d incomplete: %s; the block will be retried", blk.Number, err.Error())
		return nil, false
	}
	if len(txs) != len(blk.Txs) {
		log.Errorf("block #%d incomplete: node returned %d of %d transactions; the block will be retried",
			blk.Number, len(txs), len(blk.Txs))
		return nil, false
	}

	for i, trx := range txs {
		if trx == nil {
			log.Errorf("block #%d incomplete: transaction %s could not be loaded; the block will be retried",
				blk.Number, blk.Txs[i].String())
			return nil, false
		}
		// The block carries the authoritative timestamp; the receipt does not have one.
		// Previously applied by load() per transaction.
		trx.TimeStamp = time.Unix(int64(blk.TimeStamp), 0)
	}
	return txs, true
}
