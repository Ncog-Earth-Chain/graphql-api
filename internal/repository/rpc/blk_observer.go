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
	"context"
	"time"

	"github.com/ethereum/go-ethereum"
)

// necHeadsObserverSubscribeTick represents the time between subscription attempts.
const necHeadsObserverSubscribeTick = 30 * time.Second

// observeBlocks collects new blocks from the blockchain network
// and posts them into the proxy channel for processing.
func (nec *NecBridge) observeBlocks() {
	var sub ethereum.Subscription
	defer func() {
		if sub != nil {
			sub.Unsubscribe()
		}
		nec.log.Noticef("block observer done")
		nec.wg.Done()
	}()

	sub = nec.blockSubscription()
	for {
		// re-subscribe if the subscription ref is not valid
		if sub == nil {
			tm := time.NewTimer(necHeadsObserverSubscribeTick)
			select {
			case <-nec.sigClose:
				return
			case <-tm.C:
				sub = nec.blockSubscription()
				continue
			}
		}

		// use the subscriptions
		select {
		case <-nec.sigClose:
			return
		case err := <-sub.Err():
			nec.log.Errorf("block subscription failed; %s", err.Error())
			sub = nil
		}
	}
}

// blockSubscription provides a subscription for new blocks received
// by the connected blockchain node.
func (nec *NecBridge) blockSubscription() ethereum.Subscription {
	sub, err := nec.Rpc.EthSubscribe(context.Background(), nec.headers, "newHeads")
	if err != nil {
		nec.log.Criticalf("can not observe new blocks; %s", err.Error())
		return nil
	}
	return sub
}
