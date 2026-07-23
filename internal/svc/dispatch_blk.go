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

	txs := make([]*types.Transaction, 0, len(blk.Txs))
	for i, th := range blk.Txs {
		log.Debugf("loading trx #%d from block #%d", i, blk.Number)

		trx := bld.load(blk, th)
		if trx == nil {
			log.Errorf("block #%d incomplete: transaction %s could not be loaded; the block will be retried",
				blk.Number, th.String())
			return nil, false
		}
		txs = append(txs, trx)
	}
	return txs, true
}

// load a transaction detail from repository, if possible.
func (bld *blockDispatcher) load(blk *types.Block, th *common.Hash) *types.Transaction {
	// get transaction
	trx, err := repo.Transaction(th)
	if err != nil {
		log.Errorf("transaction %s detail not available; %s", th.String(), err.Error())
		return nil
	}

	// update time stamp using the block data
	trx.TimeStamp = time.Unix(int64(blk.TimeStamp), 0)
	return trx
}
