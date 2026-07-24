package pg

import (
	"context"
	"fmt"
	"math/big"
	"ncogearthchain-api-graphql/internal/types"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/jackc/pgx/v5"
)

// Block ingest.
//
// This is the piece that fixes the defect the audit called the most dangerous in the
// whole explorer, so it is worth stating plainly what was wrong.
//
// Under MongoDB there was no transaction at any granularity larger than a single
// InsertOne. A block's rows were written independently, and the watermark was written by
// a detached per-transaction goroutine. Three consequences followed:
//
//  1. A crash midway through a block left some of its rows present and the rest missing,
//     with nothing recording which.
//  2. A transaction whose RPC fetch failed was silently skipped -- the loader returned
//     nil and the caller stepped over it -- while the block was already counted as
//     dispatched. The block appeared complete and was not.
//  3. The scanner only ever rewinds a fixed rescan depth, so a gap older than that
//     window could never be revisited. The hole was permanent, and nothing distinguished
//     it from "this range genuinely has no transactions".
//
// Here a block is one transaction: its rows and its watermark commit together or not at
// all. The watermark is derived from the data rather than asserted alongside it, so it
// cannot overstate progress. Re-ingesting a block is idempotent, so healing a gap is
// just scanning it again.

// BlockData is everything derived from one block, ready to be written atomically.
type BlockData struct {
	Block        *types.Block
	Transactions []*types.Transaction
}

// StoreBlock writes one block and all of its transactions in a single transaction.
//
// Idempotent: re-ingesting the same block replaces its rows rather than failing or
// duplicating, which is what makes a re-scan a safe repair operation.
func (s *Store) StoreBlock(ctx context.Context, data *BlockData) error {
	if data == nil || data.Block == nil {
		return fmt.Errorf("can not store a nil block")
	}

	return s.pool.InTx(ctx, func(ctx context.Context, tx Tx) error {
		if err := s.writeBlock(ctx, tx, data.Block, len(data.Transactions)); err != nil {
			return err
		}

		// Purge before insert so a re-ingest cannot leave rows from a previous version
		// of this block behind. A reorg changes a block's contents at the same height,
		// and ON CONFLICT alone would update the transactions that still exist while
		// orphaning those that no longer do.
		if err := s.purgeBlockRows(ctx, tx, uint64(data.Block.Number)); err != nil {
			return err
		}

		for _, trx := range data.Transactions {
			if trx == nil {
				// A nil here means a transaction the loader could not fetch. Under
				// MongoDB this was skipped silently and the block still counted as
				// complete, which is precisely how permanent gaps were created. Fail
				// the block instead: an incomplete block must not be recorded as whole.
				return fmt.Errorf("block %d has a transaction that could not be loaded; refusing to record the block as complete",
					uint64(data.Block.Number))
			}
			if err := s.writeTransaction(ctx, tx, trx); err != nil {
				return err
			}
		}

		// Advance the watermark last, inside the same transaction, and only as far as
		// the data actually supports.
		return s.advanceContiguousHead(ctx, tx)
	})
}

// writeBlock upserts the block header.
func (s *Store) writeBlock(ctx context.Context, q Querier, b *types.Block, txCount int) error {
	_, err := q.Exec(ctx, `
		INSERT INTO block (number, hash, parent_hash, miner, state_root,
		                   gas_limit, gas_used, size_bytes, ts, tx_count, epoch)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (number) DO UPDATE SET
		    hash        = EXCLUDED.hash,
		    parent_hash = EXCLUDED.parent_hash,
		    miner       = EXCLUDED.miner,
		    state_root  = EXCLUDED.state_root,
		    gas_limit   = EXCLUDED.gas_limit,
		    gas_used    = EXCLUDED.gas_used,
		    size_bytes  = EXCLUDED.size_bytes,
		    ts          = EXCLUDED.ts,
		    tx_count    = EXCLUDED.tx_count,
		    epoch       = EXCLUDED.epoch`,
		int64(b.Number),
		HashVal(b.Hash),
		HashVal(b.ParentHash),
		AddrVal(b.Miner),
		HashVal(b.StateRoot),
		int64(b.GasLimit),
		int64(b.GasUsed),
		int64(b.Size),
		time.Unix(int64(b.TimeStamp), 0).UTC(),
		txCount,
		blockEpoch(b.Hash),
	)
	if err != nil {
		return fmt.Errorf("can not store block %d: %w", uint64(b.Number), err)
	}
	return nil
}

// blockEpoch derives the epoch from a block hash.
//
// On this chain the block hash IS the Atropos event ID, and an event ID's first four
// bytes are its epoch, big-endian. So the epoch is already present in data the explorer
// holds and needs no extra RPC call.
//
// Read as an unsigned 32-bit value: epochs above 2^31 must not become negative.
func blockEpoch(h common.Hash) int64 {
	return int64(uint32(h[0])<<24 | uint32(h[1])<<16 | uint32(h[2])<<8 | uint32(h[3]))
}

// purgeBlockRows removes every row derived from a block, so re-ingest is a clean
// replace.
//
// tx_log and tx_account are deleted explicitly rather than relying on cascade: they key
// on block_number rather than on a transaction hash, so a transaction that disappears in
// a reorg would otherwise leave its logs and edges behind, attached to nothing.
func (s *Store) purgeBlockRows(ctx context.Context, q Querier, number uint64) error {
	// Every table keyed by block_number must be listed here. A table left out survives a
	// reorg at its old (block_number, log_index) position: the discarded version's rows
	// stay, and because re-ingest is ON CONFLICT DO NOTHING they are never overwritten.
	// The result is phantom transfers and permanently wrong reward totals -- the exact
	// failure this function exists to prevent, reappearing one table at a time.
	//
	// Order matters only where a foreign key would block the delete; tx goes last because
	// other tables reference the block position.
	for _, tbl := range purgeOrder {
		if _, err := q.Exec(ctx,
			`DELETE FROM `+tbl+` WHERE block_number = $1`, int64(number)); err != nil {
			return fmt.Errorf("can not purge %s for block %d before re-ingest: %w", tbl, number, err)
		}
	}
	return nil
}

// purgeOrder lists every table a block's rows must be removed from, in dependency order:
// `tx` goes last because the others reference a block position.
//
// The names are a fixed list of identifiers, never caller input, so interpolating them is
// safe -- DELETE cannot parameterise a table name. TestPurgeCoversEveryBlockKeyedTable
// checks this list against the database's own catalog, so a new block-keyed table breaks
// a test instead of quietly surviving the next reorg.
var purgeOrder = []string{
	"tx_log",
	"tx_account",
	"token_tx",
	"reward_claim",
	"ddb_operation",
	"ddb_endorsement",
	"tx",
}

// purgedTables exposes purgeOrder as a set, for the guard test.
func purgedTables() map[string]bool {
	out := make(map[string]bool, len(purgeOrder))
	for _, t := range purgeOrder {
		out[t] = true
	}
	return out
}

// writeTransaction stores one transaction, its logs and its account edges.
func (s *Store) writeTransaction(ctx context.Context, q Querier, t *types.Transaction) error {
	if t.BlockNumber == nil || t.Index == nil {
		return fmt.Errorf("transaction %s has no block position; only mined transactions are stored", t.Hash.String())
	}

	value, err := Wei((*big.Int)(&t.Value))
	if err != nil {
		return fmt.Errorf("transaction %s value: %w", t.Hash.String(), err)
	}
	gasPrice, err := Wei((*big.Int)(&t.GasPrice))
	if err != nil {
		return fmt.Errorf("transaction %s gas price: %w", t.Hash.String(), err)
	}

	blockNumber := int64(*t.BlockNumber)
	txIndex := int32(*t.Index)

	// is_ddb marks a DDB commit transaction. The authoritative signal is the decoded
	// dual-consensus record (t.DDB), the same one writeDdbCommit gates on -- not the
	// destination address. The previous predicate ANDed "creates a contract" with "is
	// addressed to the DDB system contract", which cannot both hold (a contract-creation
	// transaction has no recipient), so the flag was always false and its partial index
	// tx_ddb_idx was permanently empty. ddb_contract carries the data-contract address the
	// commit targets, when it names one.
	isDDB := t.DDB != nil
	var ddbContract []byte
	if isDDB {
		ddbContract = Addr(t.DDB.ContractAddress)
	}

	_, err = q.Exec(ctx, `
		INSERT INTO tx (hash, block_number, tx_index, block_hash, from_addr, to_addr,
		                value_wei, nonce, gas_limit, gas_used, gas_cumulative,
		                gas_price_wei, input, tx_type, chain_id, sig_version,
		                status, created_contract, ts, is_ddb, ddb_contract)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
		ON CONFLICT (hash) DO UPDATE SET
		    block_number     = EXCLUDED.block_number,
		    tx_index         = EXCLUDED.tx_index,
		    block_hash       = EXCLUDED.block_hash,
		    gas_used         = EXCLUDED.gas_used,
		    gas_cumulative   = EXCLUDED.gas_cumulative,
		    status           = EXCLUDED.status,
		    created_contract = EXCLUDED.created_contract,
		    ts               = EXCLUDED.ts,
		    is_ddb           = EXCLUDED.is_ddb,
		    ddb_contract     = EXCLUDED.ddb_contract`,
		HashVal(t.Hash),
		blockNumber,
		txIndex,
		Hash(t.BlockHash),
		AddrVal(t.From),
		Addr(t.To),
		value,
		int64(t.Nonce),
		int64(t.Gas),
		nullableInt64(t.GasUsed),
		nullableInt64(t.CumulativeGasUsed),
		gasPrice,
		[]byte(t.InputData),
		0,
		nullableBig(t.ChainID),
		nullableUint16(t.SigVersion),
		txStatus(t.Status),
		Addr(t.ContractAddress),
		t.TimeStamp.UTC(),
		isDDB,
		ddbContract,
	)
	if err != nil {
		return fmt.Errorf("can not store transaction %s: %w", t.Hash.String(), err)
	}

	if err := s.writeAccountEdges(ctx, q, t, blockNumber, txIndex); err != nil {
		return err
	}

	// The DDB record, when this is a commit transaction. Inside the block's transaction,
	// so the dual-consensus proof and the transaction that carried it commit together.
	if err := s.writeDdbCommit(ctx, q, t, blockNumber, txIndex); err != nil {
		return err
	}
	return s.writeLogs(ctx, q, t, blockNumber, txIndex)
}

// account edge roles, kept as a bitmask so one row covers an address that is both sender
// and recipient of the same transaction.
const (
	roleSender    = 1 << 0
	roleRecipient = 1 << 1
)

// writeAccountEdges records which accounts a transaction touches.
//
// This edge table is what makes an account's transaction history one index scan. The
// alternative -- filtering the transaction table with (from = $1 OR to = $1) -- cannot be
// served by a single index, so PostgreSQL either scans or combines two index scans with a
// sort, and it is the single most requested page in an explorer.
//
// A self-transfer produces ONE row with both role bits set, not two rows, so it appears
// once in the account's history.
func (s *Store) writeAccountEdges(ctx context.Context, q Querier, t *types.Transaction, blockNumber int64, txIndex int32) error {
	roles := map[common.Address]int16{}
	roles[t.From] |= roleSender
	if t.To != nil {
		roles[*t.To] |= roleRecipient
	}

	for addr, role := range roles {
		if _, err := q.Exec(ctx, `
			INSERT INTO tx_account (address, block_number, tx_index, roles)
			VALUES ($1,$2,$3,$4)
			ON CONFLICT (address, block_number, tx_index)
			DO UPDATE SET roles = tx_account.roles | EXCLUDED.roles`,
			AddrVal(addr), blockNumber, txIndex, role); err != nil {
			return fmt.Errorf("can not store account edge for %s: %w", addr.String(), err)
		}
	}
	return nil
}

// writeLogs stores a transaction's event logs.
//
// Logs were persisted under MongoDB but embedded inside the transaction document and
// indexed by nothing, so the explorer paid full write amplification on its largest
// collection and could not answer a single log query. As their own table with indexes on
// address and topic they become queryable, which is the largest missing feature in the
// API after DDB visibility.
func (s *Store) writeLogs(ctx context.Context, q Querier, t *types.Transaction, blockNumber int64, txIndex int32) error {
	if len(t.Logs) == 0 {
		return nil
	}

	rows := make([][]any, 0, len(t.Logs))
	for _, l := range t.Logs {
		// Topics are stored as four discrete columns rather than an array. EVM logs
		// carry at most four (one event signature plus three indexed parameters), so
		// the shape is fixed, and discrete columns can be plain btree-indexed. An array
		// column would need GIN, which is larger and slower for the equality lookups
		// that every log query actually performs.
		//
		// topic_count is kept because a NULL topic and an absent topic are different:
		// an anonymous event has no signature topic at all.
		var topics [4][]byte
		for i := 0; i < len(l.Topics) && i < 4; i++ {
			topics[i] = HashVal(l.Topics[i])
		}

		rows = append(rows, []any{
			blockNumber,
			int32(l.Index),
			txIndex,
			HashVal(t.Hash),
			AddrVal(l.Address),
			topics[0], topics[1], topics[2], topics[3],
			int16(len(l.Topics)),
			l.Data,
			l.Removed,
			t.TimeStamp.UTC(),
		})
	}

	// COPY rather than one INSERT per log. A busy block can carry thousands of logs,
	// and the per-statement round trip dominates at that volume.
	//
	// Safe against duplicates because purgeBlockRows has already cleared this block's
	// logs -- COPY has no ON CONFLICT, so the delete-then-copy order is load-bearing.
	_, err := q.CopyFrom(ctx,
		pgx.Identifier{"tx_log"},
		[]string{
			"block_number", "log_index", "tx_index", "tx_hash", "address",
			"topic0", "topic1", "topic2", "topic3", "topic_count",
			"data", "removed", "ts",
		},
		pgx.CopyFromRows(rows))
	if err != nil {
		return fmt.Errorf("can not store logs for transaction %s: %w", t.Hash.String(), err)
	}
	return nil
}

// advanceContiguousHead moves the watermark to the highest block N such that every block
// from 1 to N is present.
//
// Derived from the data, never asserted. The MongoDB watermark was a value a detached
// goroutine wrote after storing a transaction, so it could -- and did -- claim progress
// past a block that had not fully landed. Computing it from the block table means it is
// true by construction: if a gap exists, the watermark simply does not pass it, and the
// scanner keeps seeing work to do rather than skipping over the hole forever.
func (s *Store) advanceContiguousHead(ctx context.Context, q Querier) error {
	_, err := q.Exec(ctx, `
		WITH gap AS (
		    -- the lowest block number that is absent; the watermark stops just below it
		    SELECT COALESCE(MIN(number + 1), 1) AS first_missing
		    FROM   block b
		    WHERE  NOT EXISTS (SELECT 1 FROM block n WHERE n.number = b.number + 1)
		)
		UPDATE meta_counter
		SET    value = GREATEST(value, (SELECT first_missing - 1 FROM gap)),
		       updated_at = now()
		WHERE  key = 'contiguous_head'`)
	if err != nil {
		return fmt.Errorf("can not advance the ingest watermark: %w", err)
	}
	return nil
}

// ContiguousHead reports the highest block below which nothing is missing.
func (s *Store) ContiguousHead(ctx context.Context) (uint64, error) {
	v, err := s.counter(ctx, s.pool, "contiguous_head")
	if err != nil {
		return 0, err
	}
	if v < 0 {
		return 0, nil
	}
	return uint64(v), nil
}

// ensureBlockPartitionsMaxRounds bounds the ensure loop. The SQL creates at most
// ahead_target partitions per call and converges in a handful of rounds; this is a
// defensive stop so a logic error can never spin forever.
const ensureBlockPartitionsMaxRounds = 1024

// EnsureBlockPartitions creates any missing block-range partitions for a partitioned
// table up to the runway configured ahead of head, returning the number created.
//
// The migration seeds partitions once and nothing else extends them; without this the
// table eventually writes every row into its DEFAULT partition, which cannot be pruned
// and defeats the per-partition indexes. Each SQL call is bounded to ahead_target new
// partitions so its ACCESS EXCLUSIVE lock on the parent is never held for an unbounded
// run, so we loop until it reports nothing left to create. Idempotent once covered.
func (s *Store) EnsureBlockPartitions(ctx context.Context, table string, head uint64) (int, error) {
	total := 0
	for round := 0; round < ensureBlockPartitionsMaxRounds; round++ {
		var made int
		if err := s.pool.QueryRow(ctx,
			`SELECT ensure_block_partitions($1, $2)`, table, int64(head)).Scan(&made); err != nil {
			return total, fmt.Errorf("can not ensure block partitions for %s: %w", table, err)
		}
		total += made
		if made == 0 {
			return total, nil
		}
	}
	return total, fmt.Errorf("block partition creation for %s did not converge after %d rounds",
		table, ensureBlockPartitionsMaxRounds)
}

// MissingBlocks lists gaps in the stored range, so they can be healed by re-scanning.
//
// The MongoDB ingest had no equivalent: a gap was invisible, and the scanner's fixed
// rewind depth meant anything older than that window could never be revisited. Being
// able to enumerate holes is what turns a permanent defect into a repair job.
func (s *Store) MissingBlocks(ctx context.Context, from, to uint64, limit int) ([]uint64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT n FROM generate_series($1::BIGINT, $2::BIGINT) AS n
		WHERE NOT EXISTS (SELECT 1 FROM block WHERE number = n)
		ORDER BY n
		LIMIT $3`, int64(from), int64(to), limit)
	if err != nil {
		return nil, fmt.Errorf("can not scan for missing blocks: %w", err)
	}
	defer rows.Close()

	var out []uint64
	for rows.Next() {
		var n int64
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, uint64(n))
	}
	return out, rows.Err()
}

// nullableBig converts an optional chain id for storage, preserving NULL.
//
// NULL means "the node did not report one", which is different from chain 0 -- a
// transaction genuinely signed for chain 0 would carry no replay protection at all, so
// the two must not collapse.
func nullableBig(v *hexutil.Big) any {
	if v == nil {
		return nil
	}
	return v.ToInt().Int64()
}

// nullableUint16 converts an optional signature version for the SMALLINT column.
func nullableUint16(v *hexutil.Uint64) any {
	if v == nil {
		return nil
	}
	return int16(*v)
}

// nullableInt64 converts an optional hex quantity for storage, preserving NULL.
func nullableInt64(v *hexutil.Uint64) any {
	if v == nil {
		return nil
	}
	return int64(*v)
}

// txStatus normalises an optional receipt status into the NOT NULL status column.
// A missing status means the receipt has not been seen; 0 is a genuine failure, so the
// two must not collapse into each other.
func txStatus(s *hexutil.Uint64) int16 {
	if s == nil {
		return statusUnknown
	}
	if uint64(*s) == 1 {
		return statusSuccess
	}
	return statusFailed
}

const (
	statusUnknown int16 = -1
	statusFailed  int16 = 0
	statusSuccess int16 = 1
)

// StoreTransaction writes a single transaction with its block.
//
// This exists for the CURRENT scanner, which dispatches transactions individually rather
// than assembling a block. It writes the block header and the transaction in ONE
// database transaction, so the two can never be half-present -- already stronger than
// MongoDB, which had no transaction larger than a single InsertOne.
//
// It is NOT the destination. StoreBlock is: a block's rows and its watermark commit
// together there, and a transaction that fails to load fails the whole block instead of
// being skipped. Per-transaction writes cannot give that, because the writer never knows
// whether the block it is part of is complete.
//
// The watermark deliberately does NOT advance here. Advancing it per transaction is
// exactly how MongoDB came to claim progress past a block whose contents had not all
// landed; here it only moves in StoreBlock, where completeness is knowable.
func (s *Store) StoreTransaction(ctx context.Context, blk *types.Block, trx *types.Transaction) error {
	if blk == nil || trx == nil {
		return fmt.Errorf("can not store a nil transaction or block")
	}

	return s.pool.InTx(ctx, func(ctx context.Context, tx Tx) error {
		// The block row must exist first: tx.block_number references block(number).
		if err := s.writeBlock(ctx, tx, blk, 0); err != nil {
			return err
		}
		return s.writeTransaction(ctx, tx, trx)
	})
}
