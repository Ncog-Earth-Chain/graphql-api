// Package svc implements blockchain data processing services.
package svc

import (
	"fmt"
	"ncogearthchain-api-graphql/internal/types"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	retypes "github.com/ethereum/go-ethereum/core/types"
)

// trxAddressQueueCapacity is the number of addresses kept in the dispatch buffer.
const trxAddressQueueCapacity = 1000

// trxLogQueueCapacity is the number of transaction logs kept in the dispatch buffer.
const trxLogQueueCapacity = 5000

// trxDispatchBlockUpdateTicker represents the period of block registry updater.
const trxDispatchBlockUpdateTicker = 15 * time.Second

// eventAcc represents a structure of a mentioned account.
type eventAcc struct {
	watchDog *sync.WaitGroup
	addr     *common.Address
	act      string
	blk      *types.Block
	trx      *types.Transaction
	deploy   *common.Hash
}

// trxDispatcher implements dispatcher of new transactions in the blockchain.
type trxDispatcher struct {
	service
	onTransaction  chan *types.Transaction
	bot            *time.Ticker
	inTransaction  chan *eventTrx
	outTransaction chan *eventTrx
	outAccount     chan *eventAcc
	outLog         chan *types.LogRecord
}

// name returns the name of the service used by orchestrator.
func (trd *trxDispatcher) name() string {
	return "transaction dispatcher"
}

// init prepares the transaction dispatcher to perform its function.
func (trd *trxDispatcher) init() {
	trd.sigStop = make(chan bool, 1)
	trd.outAccount = make(chan *eventAcc, trxAddressQueueCapacity)
	trd.outLog = make(chan *types.LogRecord, trxLogQueueCapacity)
	trd.outTransaction = make(chan *eventTrx, trxLogQueueCapacity)
}

// run starts the transaction dispatcher job
func (trd *trxDispatcher) run() {
	// make sure we are orchestrated
	if trd.mgr == nil {
		panic(fmt.Errorf("no svc manager set on %s", trd.name()))
	}

	// start the block observer ticker
	trd.bot = time.NewTicker(trxDispatchBlockUpdateTicker)

	// signal orchestrator we started and go
	trd.mgr.started(trd)
	go trd.execute()
}

// close terminates the block dispatcher.
func (trd *trxDispatcher) close() {
	if trd.bot != nil {
		trd.bot.Stop()
	}
	if trd.sigStop != nil {
		trd.sigStop <- true
	}
}

// execute implements the dispatcher reader and router routine.
func (trd *trxDispatcher) execute() {
	// don't forget to sign off after we are done
	defer func() {
		close(trd.sigStop)
		close(trd.outAccount)
		close(trd.outLog)
		close(trd.outTransaction)

		trd.mgr.finished(trd)
	}()

	// wait for transactions and process them
	for {
		// try to read next transaction
		select {
		case <-trd.sigStop:
			return
		case <-trd.bot.C:
			// The watermark is no longer maintained here. It is derived inside the
			// block's own database transaction, where completeness is knowable; a
			// periodic writer could only ever assert a number it had not verified.
			// The ticker is kept as the dispatcher's liveness heartbeat.
			log.Debugf("%s alive", trd.name())
		case evt, ok := <-trd.inTransaction:
			// is the channel even available for reading
			if !ok {
				log.Notice("trx channel closed, terminating %s", trd.name())
				return
			}

			if evt.blk == nil || evt.trx == nil {
				log.Criticalf("dispatcher dry loop")
				continue
			}
			trd.process(evt)
		}
	}
}

// process the given transaction event into the required targets.
func (trd *trxDispatcher) process(evt *eventTrx) {
	// send the transaction out for burns processing
	trd.outTransaction <- evt

	// process transaction accounts; exit if terminated
	var wg sync.WaitGroup
	if !trd.pushAccounts(evt, &wg) {
		return
	}

	// process transaction logs; exit if terminated
	for _, lg := range evt.trx.Logs {
		if !trd.pushLog(lg, evt.blk, evt.trx, &wg) {
			return
		}
	}

	// store the transaction into the database once the processing is done
	// we spawn a lot of go-routines here, so we should test the optimal queue length above
	go trd.waitAndFinish(evt, &wg)

	// broadcast new transaction; if it can not be broadcast quickly, skip
	select {
	case trd.onTransaction <- evt.trx:
	case <-time.After(200 * time.Millisecond):
	}
}

// waitAndFinish waits for the derived processing to finish, then updates the caches.
//
// It no longer STORES the transaction: the block dispatcher already wrote it, atomically,
// together with every other transaction in its block. Writing here was what made the
// ingest non-atomic -- one row per detached goroutine, with no notion of whether the
// block they belonged to was complete.
//
// It also no longer advances a watermark. blkObserver used to be updated here, from a
// goroutine nothing joined at shutdown, which is how the watermark came to claim progress
// past blocks that had not fully landed. The watermark is now derived inside the block's
// own database transaction.
func (trd *trxDispatcher) waitAndFinish(evt *eventTrx, wg *sync.WaitGroup) {
	// wait until all the sub-processors finish their job
	wg.Wait()

	repo.IncTrxCountEstimate(1)
	repo.CacheTransaction(evt.trx)
}

// pushAccounts pushes given transaction accounts on both sides observing terminate signal on process.
func (trd *trxDispatcher) pushAccounts(evt *eventTrx, wg *sync.WaitGroup) bool {
	// the sender is always present
	if !trd.pushAccount(types.AccountTypeWallet, &evt.trx.From, evt.blk, evt.trx, wg) {
		return false
	}

	// do we have a recipient?
	if evt.trx.To != nil && !trd.pushAccount(types.AccountTypeWallet, evt.trx.To, evt.blk, evt.trx, wg) {
		return false
	}

	// if there is no contract created, we are done here
	if evt.trx.ContractAddress == nil {
		return true
	}

	// queue the new contract to be processed as well
	log.Debugf("contract %s found at trx %s", evt.trx.ContractAddress.String(), evt.trx.Hash.String())
	return trd.pushAccount(types.AccountTypeContract, evt.trx.ContractAddress, evt.blk, evt.trx, wg)
}

// pushAccount pushes given account event to output queue observing terminate signal.
func (trd *trxDispatcher) pushAccount(at string, adr *common.Address, blk *types.Block, trx *types.Transaction, wg *sync.WaitGroup) bool {
	wg.Add(1)
	select {
	case trd.outAccount <- &eventAcc{
		watchDog: wg,
		addr:     adr,
		act:      at,
		blk:      blk,
		trx:      trx,
		deploy:   nil,
	}:
	case <-trd.sigStop:
		trd.sigStop <- true
		return false
	}
	return true
}

// pushLog pushes specified log record into a processing queue observing terminate signal.
func (trd *trxDispatcher) pushLog(lg retypes.Log, blk *types.Block, trx *types.Transaction, wg *sync.WaitGroup) bool {
	wg.Add(1)
	select {
	case trd.outLog <- &types.LogRecord{
		WatchDog: wg,
		Block:    blk,
		Trx:      trx,
		Log:      lg,
	}:
	case <-trd.sigStop:
		trd.sigStop <- true
		return false
	}
	return true
}
