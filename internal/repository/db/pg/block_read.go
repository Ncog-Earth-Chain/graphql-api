package pg

import (
	"context"
	"errors"
	"fmt"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Block reads.
//
// Blocks were not stored at all under MongoDB. Every block list was N sequential JSON-RPC
// calls to the node -- a 25-block page cost 25 round trips, and a block-to-transactions
// join was impossible because the block side did not exist locally. Storing them turns
// both into single queries and removes the node from the read path for historical data.

// blockKeyset orders blocks newest-first. `number` is the primary key, so the ordering is
// total and pagination cannot repeat or skip.
var blockKeyset = Keyset{Columns: []KeyColumn{{Name: "number", Dir: Desc}}}

// blockColumns is what types.Block can actually hold.
//
// tx_count and epoch are deliberately NOT selected. They were, and scanBlock read them into
// variables it then dropped: types.Block has no field for either, transactionCount is
// derived from the transaction hashes, and no resolver exposes a block's epoch. Selecting a
// column in order to discard it invites the reader to believe it arrives somewhere.
const blockColumns = `
	number, hash, parent_hash, miner, state_root,
	gas_limit, gas_used, size_bytes, ts`

// Block loads one block by number.
//
// Returns (nil, nil) when absent, which is an ordinary outcome for a height the indexer
// has not reached.
func (s *Store) Block(ctx context.Context, number uint64) (*types.Block, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+blockColumns+` FROM block WHERE number = $1`, int64(number))

	b, err := scanBlock(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("can not load block %d: %w", number, err)
	}
	if err := s.fillBlockTransactions(ctx, []*types.Block{b}); err != nil {
		return nil, err
	}
	return b, nil
}

// BlockByHash loads one block by hash.
func (s *Store) BlockByHash(ctx context.Context, hash *common.Hash) (*types.Block, error) {
	if hash == nil {
		return nil, fmt.Errorf("no block hash given")
	}

	row := s.pool.QueryRow(ctx, `SELECT `+blockColumns+` FROM block WHERE hash = $1`, HashVal(*hash))

	b, err := scanBlock(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("can not load block %s: %w", hash.String(), err)
	}
	if err := s.fillBlockTransactions(ctx, []*types.Block{b}); err != nil {
		return nil, err
	}
	return b, nil
}

// BlockList pages through blocks, newest first, or oldest first for a negative count.
//
// This is the query that replaces N sequential RPC calls per page.
//
// `from` is a block NUMBER and is EXCLUSIVE, which is what the API's cursor already means:
// the public blocks cursor is a hex block number the resolver renders from the block itself,
// so this takes the number directly rather than an opaque keyset token. Passing one through
// DecodeCursor would have changed a client-visible format for no gain.
//
// Returns one row PAST the page when more exist. The caller cannot otherwise distinguish an
// exactly-full page from the last one, and would report "there is more" on every page whose
// size happens to divide the remaining rows -- the defect adapt.go documents having fixed
// for transactions.
func (s *Store) BlockList(ctx context.Context, from *uint64, count int32) ([]*types.Block, error) {
	page := NewPage(count, maxListLimit)

	var where string
	var args []any

	if from != nil {
		pred, curArgs, err := blockKeyset.After([]any{int64(*from)}, page.Reverse, 0)
		if err != nil {
			return nil, err
		}
		where = "WHERE " + pred
		args = append(args, curArgs...)
	}

	sql := `SELECT ` + blockColumns + ` FROM block ` + where + ` ` +
		blockKeyset.OrderBy(page.Reverse) + ` LIMIT $` + itoa(len(args)+1)
	args = append(args, page.Limit+1)

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("block list query failed: %w", err)
	}
	defer rows.Close()

	out := make([]*types.Block, 0, page.Limit+1)
	for rows.Next() {
		b, err := scanBlock(rows)
		if err != nil {
			return nil, fmt.Errorf("can not scan block: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := s.fillBlockTransactions(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// fillBlockTransactions populates Txs on every block given, in ONE query.
//
// Not optional, and not a detail of the list. Block.transactionCount, Block.txHashList and
// Block.txList are all derived from types.Block.Txs, so a block read from the index without
// it does not merely lack transactions -- it reports, with no error, that it HAS none. The
// node path returns the hash list as part of the block itself (eth_getBlockByNumber with
// full=false), so filling it here is parity, not extra work.
//
// One query for the whole page rather than one per block: the point of moving this list off
// the node was to stop paying per block, and an N+1 against PostgreSQL would only make that
// cheaper rather than fixing it. Ordered by tx_index so hashes come back in block position
// order, which is what txHashList promises.
func (s *Store) fillBlockTransactions(ctx context.Context, blocks []*types.Block) error {
	if len(blocks) == 0 {
		return nil
	}

	numbers := make([]int64, 0, len(blocks))
	byNumber := make(map[int64]*types.Block, len(blocks))
	for _, b := range blocks {
		n := int64(b.Number)
		numbers = append(numbers, n)
		byNumber[n] = b
		// Empty rather than nil: a block with no transactions must render as [], and a
		// block whose row is missing below must not inherit whatever it had before.
		b.Txs = []*common.Hash{}
	}

	rows, err := s.pool.Query(ctx, `
		SELECT block_number, hash FROM tx
		WHERE  block_number = ANY($1)
		ORDER  BY block_number, tx_index`, numbers)
	if err != nil {
		return fmt.Errorf("can not load block transactions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			number int64
			hash   []byte
		)
		if err := rows.Scan(&number, &hash); err != nil {
			return fmt.Errorf("can not scan block transaction: %w", err)
		}
		h, err := ToHash(hash)
		if err != nil {
			return err
		}
		if b, ok := byNumber[number]; ok {
			b.Txs = append(b.Txs, h)
		}
	}
	return rows.Err()
}

// BlockHeight returns the highest stored block number.
//
// Distinct from ContiguousHead: this is the furthest the indexer has reached, while the
// watermark is the furthest it has reached with nothing missing behind it. Reporting the
// height as if it were the watermark is what let gaps hide.
func (s *Store) BlockHeight(ctx context.Context) (uint64, error) {
	var n *int64
	if err := s.pool.QueryRow(ctx, `SELECT max(number) FROM block`).Scan(&n); err != nil {
		return 0, fmt.Errorf("can not read block height: %w", err)
	}
	if n == nil {
		return 0, nil
	}
	return uint64(*n), nil
}

// BlockCursor renders the pagination cursor for a block.
func BlockCursor(b *types.Block) string {
	if b == nil {
		return ""
	}
	return EncodeCursor([]int64{int64(b.Number)})
}

// scanBlock maps one row onto the domain type.
func scanBlock(row rowScanner) (*types.Block, error) {
	var (
		number                        int64
		hash, parentHash, miner, root []byte
		gasLimit, gasUsed, size       int64
		ts                            pgtype.Timestamptz
	)

	if err := row.Scan(&number, &hash, &parentHash, &miner, &root,
		&gasLimit, &gasUsed, &size, &ts); err != nil {
		return nil, err
	}

	h, err := ToHash(hash)
	if err != nil {
		return nil, err
	}
	ph, err := ToHash(parentHash)
	if err != nil {
		return nil, err
	}
	m, err := ToAddr(miner)
	if err != nil {
		return nil, err
	}
	sr, err := ToHash(root)
	if err != nil {
		return nil, err
	}

	return &types.Block{
		Number:     hexutil.Uint64(number),
		Hash:       *h,
		ParentHash: *ph,
		Miner:      *m,
		StateRoot:  *sr,
		GasLimit:   hexutil.Uint64(gasLimit),
		GasUsed:    hexutil.Uint64(gasUsed),
		Size:       hexutil.Uint64(size),
		TimeStamp:  hexutil.Uint64(ts.Time.Unix()),
	}, nil
}
