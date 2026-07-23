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

const blockColumns = `
	number, hash, parent_hash, miner, state_root,
	gas_limit, gas_used, size_bytes, ts, tx_count, epoch`

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
	return b, nil
}

// BlockList pages through blocks, newest first.
//
// This is the query that replaces N sequential RPC calls per page.
func (s *Store) BlockList(ctx context.Context, cursor string, count int32) ([]*types.Block, error) {
	page := NewPage(count, maxListLimit)

	cur, err := DecodeCursor(cursor, 1)
	if err != nil {
		return nil, err
	}

	var where string
	var args []any

	if len(cur) == 1 {
		pred, curArgs, err := blockKeyset.After([]any{cur[0]}, page.Reverse, 0)
		if err != nil {
			return nil, err
		}
		where = "WHERE " + pred
		args = append(args, curArgs...)
	}

	sql := `SELECT ` + blockColumns + ` FROM block ` + where + ` ` +
		blockKeyset.OrderBy(page.Reverse) + ` LIMIT $` + itoa(len(args)+1)
	args = append(args, page.Limit)

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("block list query failed: %w", err)
	}
	defer rows.Close()

	out := make([]*types.Block, 0, page.Limit)
	for rows.Next() {
		b, err := scanBlock(rows)
		if err != nil {
			return nil, fmt.Errorf("can not scan block: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
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
		txCount                       int32
		epoch                         *int64
	)

	if err := row.Scan(&number, &hash, &parentHash, &miner, &root,
		&gasLimit, &gasUsed, &size, &ts, &txCount, &epoch); err != nil {
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
