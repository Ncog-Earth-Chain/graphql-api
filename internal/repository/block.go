/*
Package repository implements repository for handling fast and efficient access to data required
by the resolvers of the API server.

Internally it utilizes RPC to access Ncogearthchain/Forest full node for blockchain interaction. Mongo database
for fast, robust and scalable off-chain data storage, especially for aggregated and pre-calculated data mining
results. BigCache for in-memory object storage to speed up loading of frequently accessed entities.
*/
package repository

import (
	"context"
	"errors"
	"fmt"
	"ncogearthchain-api-graphql/internal/repository/cache"
	"ncogearthchain-api-graphql/internal/repository/rpc"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	etc "github.com/ethereum/go-ethereum/core/types"
	eth "github.com/ethereum/go-ethereum/rpc"
)

// ErrBlockNotFound represents an error returned if a block can not be found.
var ErrBlockNotFound = errors.New("requested block can not be found in Ncogearthchain blockchain")

// ObservedHeaders provides a channel fed with new headers observed
// by the connected blockchain node.
func (p *proxy) ObservedHeaders() chan *etc.Header {
	return p.rpc.ObservedBlockProxy()
}

// BlockHeight returns the current height of the Ncogearthchain blockchain in blocks.
func (p *proxy) BlockHeight() (*hexutil.Big, error) {
	return p.rpc.BlockHeight()
}

// LastKnownBlock returns number of the last block known to the repository.
func (p *proxy) LastKnownBlock(ctx context.Context) (uint64, error) {
	return p.pg.LastKnownBlock(ctx)
}

// ContiguousHead returns the highest block below which nothing is missing -- the ingest
// watermark. Gaps to heal live between this and StoredBlockHeight.
func (p *proxy) ContiguousHead(ctx context.Context) (uint64, error) {
	return p.pg.ContiguousHead(ctx)
}

// StoredBlockHeight returns the HIGHEST BLOCK PRESENT in the index, gaps included.
//
// Distinct from BlockHeight above, which asks the NODE for the chain head, and from
// LastKnownBlock, which is the ingest watermark -- pg/config.go:8-22 spells out why
// conflating those two is what let gaps become permanent under MongoDB.
//
// It exists because the gap-heal loop needs the upper end of the range that may contain
// holes, and there was no way to ask for it: LastKnownBlock is literally
// `return s.ContiguousHead(ctx)` (pg/config.go:36-38), so healGaps compared the watermark
// against itself and its `if last <= head+1 { return }` guard was unconditionally true.
// The whole heal path was unreachable.
func (p *proxy) StoredBlockHeight(ctx context.Context) (uint64, error) {
	return p.pg.BlockHeight(ctx)
}

// MissingBlocks lists gaps in the stored range [from, to] (bounded by limit), so a heal loop
// can re-fetch the blocks a transient RPC failure left absent.
func (p *proxy) MissingBlocks(ctx context.Context, from, to uint64, limit int) ([]uint64, error) {
	return p.pg.MissingBlocks(ctx, from, to, limit)
}

// ForkedPredecessors lists stored blocks (above aboveBlock, bounded by limit) whose hash does
// not match the parent_hash of the block above them, so a reorg heal can re-fetch them.
func (p *proxy) ForkedPredecessors(ctx context.Context, aboveBlock uint64, limit int) ([]uint64, error) {
	return p.pg.ForkedPredecessors(ctx, aboveBlock, limit)
}

// CacheBlock puts a block to the internal block cache.
func (p *proxy) CacheBlock(blk *types.Block) {
	p.cache.AddBlock(blk)
}

// BlockByNumber returns a block at Ncogearthchain blockchain represented by a number. Top block is returned if the number
// is not provided.
// If the block is not found, ErrBlockNotFound error is returned.
func (p *proxy) BlockByNumber(num *hexutil.Uint64) (*types.Block, error) {
	// return the top block if block number is not provided
	if num == nil {
		tag := rpc.BlockTypeLatest
		return p.blockByTag(&tag)
	}
	return p.getBlock(num.String(), p.blockByTag)
}

// BlockByHash returns a block at Ncogearthchain blockchain represented by a hash. Top block is returned if the hash
// is not provided.
// If the block is not found, ErrBlockNotFound error is returned.
func (p *proxy) BlockByHash(hash *common.Hash) (*types.Block, error) {
	// do we have a hash?
	if hash == nil {
		tag := rpc.BlockTypeLatest
		return p.blockByTag(&tag)
	}
	return p.getBlock(hash.String(), p.rpc.BlockByHash)
}

// IndexedBlockByNumber returns a block for the READ path: from the index when it is there,
// from the node when it is not.
//
// Separate from BlockByNumber rather than replacing it, for the same reason
// IndexedTransaction is separate from Transaction: the SCANNER calls BlockByNumber to fetch
// the blocks it is about to ingest (svc/scan_blk.go, svc/orchestrator.go). Serving those
// from the index would be circular -- the indexer would read back whatever it had already
// written and could never discover a block it had missed, or notice one that changed under
// a reorg.
//
// A nil number means "latest", which is deliberately left with the node: the head is the one
// height whose contents are still moving, and the index is by definition behind it.
func (p *proxy) IndexedBlockByNumber(ctx context.Context, num *hexutil.Uint64) (*types.Block, error) {
	if num == nil {
		return p.BlockByNumber(nil)
	}

	blk, err := p.pg.Block(ctx, uint64(*num))
	if err != nil {
		return nil, err
	}
	if blk != nil {
		return blk, nil
	}

	// Not indexed yet -- above the ingest watermark, or a gap still to be healed.
	return p.BlockByNumber(num)
}

// IndexedBlockByHash returns a block by hash for the READ path, index first.
//
// By hash rather than by (number - 1) for a parent, even though the number is cheaper: the
// parent hash names one specific block, while the height names whichever block currently
// occupies it. Across a reorg those differ, and following the height would quietly walk into
// a chain the child was never part of.
func (p *proxy) IndexedBlockByHash(ctx context.Context, hash *common.Hash) (*types.Block, error) {
	if hash == nil {
		return p.BlockByHash(nil)
	}

	blk, err := p.pg.BlockByHash(ctx, hash)
	if err != nil {
		return nil, err
	}
	if blk != nil {
		return blk, nil
	}
	return p.BlockByHash(hash)
}

// getBlock gets a block of given tag from cache, or from a repository pull function.
func (p *proxy) getBlock(tag string, pull func(*string) (*types.Block, error)) (*types.Block, error) {
	// inform what we do
	p.log.Debugf("block [%s] requested", tag)

	// try to use the in-memory cache
	if blk := p.cache.PullBlock(tag); blk != nil {
		// inform what we do
		p.log.Debugf("block [%s] loaded from cache", tag)

		// return the block
		return blk, nil
	}

	// extract the block from the chain
	blk, err := pull(&tag)
	if err != nil {
		// block simply not found?
		if err == eth.ErrNoResult {
			p.log.Warning("block not found in the blockchain")
			return nil, ErrBlockNotFound
		}

		// something went wrong
		return nil, err
	}

	// try to store the block in cache for future use
	err = p.cache.PushBlock(tag, blk)
	if err != nil {
		p.log.Errorf("can not cache; %s", err.Error())
	}

	// inform what we do
	p.log.Debugf("block [%s] loaded by pulling", tag)
	return blk, nil
}

// blockByTag returns a block at Ncogearthchain blockchain represented by given tag.
// The tag could be an encoded block number, or a predefined string tag for "earliest", "latest" or "pending" block.
func (p *proxy) blockByTag(tag *string) (*types.Block, error) {
	// inform what we do
	p.log.Debugf("loading block [%s]", *tag)

	// extract the block
	block, err := p.rpc.Block(tag)
	if err != nil {
		// block simply not found?
		if err == eth.ErrNoResult {
			p.log.Warning("block not found in the blockchain")
			return nil, ErrBlockNotFound
		}

		// something went wrong
		return nil, err
	}

	return block, nil
}

// Blocks pulls list of blocks starting on the specified block number and going up, or down based on count number.
// If the initial block number is not provided, we start on top, or bottom based on count value.
//
// No-number boundaries are handled as follows:
//   - For positive count we start from the most recent block and scan to older blocks.
//   - For negative count we start from the first block and scan to newer blocks.
//
// It is served from the local index. Walking the node instead cost one
// eth_getBlockByNumber PER BLOCK, serially: a 25-block page was ~28 round trips and the
// API's own 250 cap made it ~253, from one small POST, for rows PostgreSQL holds on its
// primary key. This was the last node-walking list in the API; `transactions` and
// `block.txList` were moved earlier.
//
// Like `transactions`, it does NOT fall back to the node when the index lags: the ring cache
// covers the head, and beyond that the index is the source of truth for historical data.
// Reintroducing a per-block walk as a fallback would restore exactly the cost being removed.
func (p *proxy) Blocks(ctx context.Context, num *uint64, count int32) (*types.BlockList, error) {
	// nothing to load?
	if count == 0 {
		return nil, fmt.Errorf("nothing to do, zero blocks requested")
	}

	// fast blocks list from the rings available?
	if num == nil && count > 0 && count < cache.BlockRingCacheSize {
		bl, err := p.RecentBlocks(int(count))
		// A ring that has not been filled yet returns FEWER blocks than asked for, and a
		// short page here is indistinguishable from "the chain has no more". Only take the
		// fast path when it actually answered the question.
		if err == nil && bl != nil && len(bl.Collection) == int(count) {
			return bl, nil
		}
	}

	rows, err := p.pg.BlockList(ctx, num, count)
	if err != nil {
		return nil, err
	}

	// The store returns one row past the page when more exist; that probe is what
	// distinguishes an exactly-full page from the last one.
	page := count
	if page < 0 {
		page = -page
	}
	hasMore := len(rows) > int(page)
	if hasMore {
		rows = rows[:page]
	}

	// Boundary flags exactly as the removed node walk computed them: a no-cursor request
	// starts at whichever end its direction implies, and the far end is reached when the
	// scan finds nothing beyond the page. Preserved deliberately -- these drive
	// hasNextPage/hasPreviousPage, so a change here is visible to every paging client.
	list := &types.BlockList{
		Collection: rows,
		IsStart:    (num == nil && count > 0) || (count < 0 && !hasMore),
		IsEnd:      (num == nil && count < 0) || (count > 0 && !hasMore),
	}

	// A negative count scans upward; the list is presented newest-first either way.
	if count < 0 {
		list.Reverse()
	}
	return list, nil
}

// RecentBlocks pulls a list of the most recent blocks from the ring cache.
func (p *proxy) RecentBlocks(length int) (*types.BlockList, error) {
	// pull the quick list
	bl := p.cache.ListBlocks(length)

	// does it make sense? if so, make the list from it
	if len(bl) > 0 {
		return &types.BlockList{
			Collection: bl,
			IsStart:    true,
			IsEnd:      false,
		}, nil
	}
	return nil, fmt.Errorf("recent blocks list not available")
}
