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

		if err := s.writeTransactions(ctx, tx, data.Transactions, uint64(data.Block.Number)); err != nil {
			return err
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

// txInsertSQL upserts one transaction row.
//
// ON CONFLICT (hash) DO UPDATE rather than DO NOTHING, and it cannot be relaxed to a plain
// COPY: purgeBlockRows clears rows by block_number, so a transaction that MOVES to a
// different height in a reorg still has its old row present under the old block when the
// new one is written. The upsert is what re-points it. Logs and account edges have no such
// hazard -- they are keyed by block_number, so the purge always reaches them.
const txInsertSQL = `
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
	    ddb_contract     = EXCLUDED.ddb_contract`

// edgeInsertSQL records one account's participation in one transaction.
const edgeInsertSQL = `
	INSERT INTO tx_account (address, block_number, tx_index, roles)
	VALUES ($1,$2,$3,$4)
	ON CONFLICT (address, block_number, tx_index)
	DO UPDATE SET roles = tx_account.roles | EXCLUDED.roles`

// maxBatchStatements bounds how many statements are pipelined before being flushed.
//
// pgx buffers an entire batch in memory and writes it as one payload, so an unbounded
// batch makes peak memory a function of the largest block the chain ever produces. The
// flush point is per statement rather than per transaction because a single transaction
// contributes a variable number of them.
const maxBatchStatements = 1024

// writeTransactions stores every transaction of a block: the transaction rows, their
// account edges, their DDB commit records and their logs.
//
// The unit of work is the BLOCK, not the transaction. Writing row-at-a-time measured a
// ceiling of roughly 2,372 tx/s, and the limit was not PostgreSQL -- it was the round trip
// per statement, of which each transaction cost two or three (one for tx, one or two for
// its account edges, plus a COPY for its logs). Pipelining them collapses that to a
// constant few round trips per block regardless of how many transactions it carries.
//
// The SQL and its conflict semantics are unchanged from the row-at-a-time version, which
// is deliberate: this is a latency fix, not a behaviour change, and the ingest path is
// where a subtle difference would silently corrupt the index rather than fail loudly.
func (s *Store) writeTransactions(ctx context.Context, q Querier, txs []*types.Transaction, blockNumber uint64) error {
	if len(txs) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	// owners[i] is the transaction that queued statement i, so a failure deep inside a
	// pipelined batch still names the transaction that caused it.
	owners := make([]*types.Transaction, 0, len(txs)*2)
	logRows := make([][]any, 0, len(txs))

	flush := func() error {
		if batch.Len() == 0 {
			return nil
		}
		res := q.SendBatch(ctx, batch)
		// Every queued result must be consumed before the connection can be used
		// again, including after a failure -- Close alone would leave the rest
		// unread and the error attributed to the wrong statement.
		var firstErr error
		for i := 0; i < batch.Len(); i++ {
			if _, err := res.Exec(); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("can not store transaction %s: %w",
					owners[i].Hash.String(), err)
			}
		}
		if err := res.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("can not complete writes for block %d: %w", blockNumber, err)
		}
		batch = &pgx.Batch{}
		owners = owners[:0]
		return firstErr
	}

	for _, t := range txs {
		if t == nil {
			// A nil here means a transaction the loader could not fetch. Under
			// MongoDB this was skipped silently and the block still counted as
			// complete, which is precisely how permanent gaps were created. Fail
			// the block instead: an incomplete block must not be recorded as whole.
			return fmt.Errorf("block %d has a transaction that could not be loaded; refusing to record the block as complete",
				blockNumber)
		}
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

		txBlockNumber := int64(*t.BlockNumber)
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

		batch.Queue(txInsertSQL,
			HashVal(t.Hash),
			txBlockNumber,
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
		owners = append(owners, t)

		for addr, role := range accountRoles(t) {
			batch.Queue(edgeInsertSQL, AddrVal(addr), txBlockNumber, txIndex, role)
			owners = append(owners, t)
		}

		logRows = appendLogRows(logRows, t, txBlockNumber, txIndex)

		if batch.Len() >= maxBatchStatements {
			if err := flush(); err != nil {
				return err
			}
		}
	}

	if err := flush(); err != nil {
		return err
	}

	// One COPY for the whole block rather than one per transaction. Safe against
	// duplicates because purgeBlockRows has already cleared this block's logs -- COPY
	// has no ON CONFLICT, so the delete-then-copy order is load-bearing.
	if len(logRows) > 0 {
		if _, err := q.CopyFrom(ctx, pgx.Identifier{"tx_log"}, txLogColumns, pgx.CopyFromRows(logRows)); err != nil {
			return fmt.Errorf("can not store logs for block %d: %w", blockNumber, err)
		}
	}

	// The DDB records, for those transactions that carry one. Kept per-transaction
	// because writeDdbCommit returns immediately when the transaction is not a DDB
	// commit, so ordinary traffic pays nothing, and because a commit fans out into
	// several ordered statements whose sequence is load-bearing. Inside the block's
	// transaction, so the dual-consensus proof and the transaction that carried it
	// commit together.
	for _, t := range txs {
		if err := s.writeDdbCommit(ctx, q, t, int64(*t.BlockNumber), int32(*t.Index)); err != nil {
			return err
		}
	}
	return nil
}

// account edge roles, kept as a bitmask so one row covers an address that is both sender
// and recipient of the same transaction.
const (
	roleSender    = 1 << 0
	roleRecipient = 1 << 1
)

// accountRoles reports which accounts a transaction touches, and in what capacity.
//
// This edge table is what makes an account's transaction history one index scan. The
// alternative -- filtering the transaction table with (from = $1 OR to = $1) -- cannot be
// served by a single index, so PostgreSQL either scans or combines two index scans with a
// sort, and it is the single most requested page in an explorer.
//
// A self-transfer produces ONE entry with both role bits set, not two, so it appears once
// in the account's history. That merge happening HERE rather than in the database is also
// what keeps (address, block_number, tx_index) unique within a block.
func accountRoles(t *types.Transaction) map[common.Address]int16 {
	roles := map[common.Address]int16{}
	roles[t.From] |= roleSender
	if t.To != nil {
		roles[*t.To] |= roleRecipient
	}
	return roles
}

// txLogColumns is the COPY column list for tx_log, in the order appendLogRows emits.
var txLogColumns = []string{
	"block_number", "log_index", "tx_index", "tx_hash", "address",
	"topic0", "topic1", "topic2", "topic3", "topic_count",
	"data", "removed", "ts",
}

// appendLogRows appends one transaction's event logs to a block-level COPY buffer.
//
// Logs were persisted under MongoDB but embedded inside the transaction document and
// indexed by nothing, so the explorer paid full write amplification on its largest
// collection and could not answer a single log query. As their own table with indexes on
// address and topic they become queryable, which is the largest missing feature in the
// API after DDB visibility.
func appendLogRows(rows [][]any, t *types.Transaction, blockNumber int64, txIndex int32) [][]any {
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
	return rows
}

// advanceContiguousHead moves the watermark to the highest block N such that every block
// from 1 to N is present.
//
// Derived from the data, never asserted. The MongoDB watermark was a value a detached
// goroutine wrote after storing a transaction, so it could -- and did -- claim progress
// past a block that had not fully landed. Computing it from the block table means it is
// true by construction: if a gap exists, the watermark simply does not pass it, and the
// scanner keeps seeing work to do rather than skipping over the hole forever.
// It scans only FORWARD FROM THE CURRENT WATERMARK, and that bound is what makes this
// affordable. Searching from block 1 every time is a hash anti-join over two full scans of
// `block`, so the cost of ingesting one block grows with the length of the whole chain and
// the cost of ingesting the chain is quadratic. Measured on PostgreSQL 16 with 2,002,001
// blocks: 951 ms PER INGESTED BLOCK, already spilling to 16 temp batches. A chain that
// produces a block a second needs this under a second merely to keep pace, so the indexer
// fell behind at ~2M blocks and, extrapolating the linear per-block growth, every ingest
// transaction would exceed the 30 s statement_timeout at ~60M blocks and ingest would stop
// for good. Bounded, the same call is a short index-only walk of the unverified tail.
//
// The bound is sound because the watermark is monotonic (GREATEST, below) and means
// "every block from 1 to value is present". Blocks at or below it have already been
// proven contiguous, so re-examining them can never move it. Starting the scan AT the
// watermark -- not above it -- keeps the anchor row in the scan, so a watermark of N with
// N+1 absent still evaluates the NOT EXISTS on N and correctly holds the value at N.
//
// This does not weaken the guarantee the gap tests pin down: the watermark still cannot
// cross a hole, because the first missing block at or above the watermark is exactly the
// hole the unbounded form would have found first. A gap BELOW the watermark cannot exist
// while the value is only ever raised, and if one were ever introduced the unbounded form
// would not have repaired it either -- GREATEST never lowers the value.
func (s *Store) advanceContiguousHead(ctx context.Context, q Querier) error {
	// The watermark is read first and passed as a BIND PARAMETER rather than computed in a
	// CTE inside the statement. That is not cosmetic: `cur` had to be referenced three
	// times, so PostgreSQL materialised it, and a materialised CTE cannot serve as an index
	// bound. The bounded-looking query still planned as a parallel hash anti-join reading
	// all 11,193 buffers of block_pkey -- exactly what the bound was meant to avoid. With a
	// real parameter the planner emits `Index Cond: (number >= $1)` and a nested-loop anti
	// join: 13 buffers, 0.04 ms, against 11,193 buffers and 951 ms for the unbounded form
	// on the same 2,002,001-block table.
	head, err := s.counter(ctx, q, "contiguous_head")
	if err != nil {
		return fmt.Errorf("can not read the ingest watermark: %w", err)
	}
	if head < 0 {
		head = 0
	}

	_, err = q.Exec(ctx, contiguousHeadGapCTE+`
		UPDATE meta_counter
		SET    value = GREATEST(value, (SELECT first_missing - 1 FROM gap)),
		       updated_at = now()
		WHERE  key = 'contiguous_head'`, head)
	if err != nil {
		return fmt.Errorf("can not advance the ingest watermark: %w", err)
	}
	return nil
}

// contiguousHeadGapCTE finds the lowest block at or above the watermark ($1) whose
// successor is absent -- the point the watermark must stop just below.
//
// It is a named constant rather than inline SQL so that the test guarding its plan shape
// can EXPLAIN THIS EXACT TEXT. Asserting on a copy pasted into the test file would keep
// passing after someone edited the query here, which is the one thing that guard exists
// to catch.
const contiguousHeadGapCTE = `
	WITH gap AS (
	    SELECT COALESCE(MIN(number + 1), $1::BIGINT + 1) AS first_missing
	    FROM   block b
	    WHERE  b.number >= $1::BIGINT
	      AND  NOT EXISTS (
	               SELECT 1 FROM block n
	               -- BOTH sides carry the bound. Constraining only the outer scan lets
	               -- the planner build the anti-join over every row in the table, which
	               -- is where the cost actually sat. A successor of a block at or above
	               -- the watermark is itself above it, so this excludes nothing.
	               WHERE  n.number >= $1::BIGINT
	                 AND  n.number = b.number + 1)
	)`

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

// ensureBlockPartitionsMaxRounds bounds the ensure loop.
//
// Since 00013 the SQL creates exactly ONE partition per call -- that is the transaction
// boundary, so a range that cannot be created leaves the ranges below it committed instead
// of rolling them back -- which means a round is now a partition rather than a batch of
// ahead_target. At the configured 1,000,000-block width the old 1024 would have capped a
// from-scratch rebuild at block ~1.02e9 instead of the ~4.09e9 it allowed before. This is a
// defensive stop against a logic error spinning forever, not a runway limit, so it is set
// where it cannot bind first: 8192 rounds is 8.19e9 blocks.
const ensureBlockPartitionsMaxRounds = 8192

// EnsureBlockPartitions creates any missing block-range partitions for a partitioned
// table up to the runway configured ahead of head, returning the number created.
//
// The migration seeds partitions once and nothing else extends them; without this the
// table eventually writes every row into its DEFAULT partition, which cannot be pruned
// and defeats the per-partition indexes. Each SQL call creates exactly one partition and
// commits it on its own, so we loop until it reports nothing left to create; rows already
// sitting in the DEFAULT partition at a range being covered are moved into the new
// partition by that same call (00013). Idempotent once covered.
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

// PartitionHealth is one row of partition_health(): the runway left on a partitioned table
// and the rows that fell outside it.
type PartitionHealth struct {
	Table           string
	PartitionsAhead int64
	DefaultRows     int64
	OrphanAttached  int64
	OrphanDetached  int64
}

// PartitionHealth reports, per partitioned table, how much partition runway is left and how
// many rows sit where no per-partition index reaches them.
//
// partition_health() shipped in migration 00004 and had NO Go caller, so the one condition
// the maintenance job exists to prevent was observable only to someone running psql by hand
// -- which is how a wedged runway could stay invisible behind a log line repeated every 12
// hours. DefaultRows is the number that matters: those rows are correct and queryable, but
// they live in the catch-all partition, so an address- or topic-filtered log query scans all
// of them instead of pruning to one block range.
func (s *Store) PartitionHealth(ctx context.Context) ([]PartitionHealth, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT table_name, partitions_ahead, default_rows, orphan_attached, orphan_detached
		FROM   partition_health()`)
	if err != nil {
		return nil, fmt.Errorf("can not read partition health: %w", err)
	}
	defer rows.Close()

	var out []PartitionHealth
	for rows.Next() {
		var h PartitionHealth
		if err := rows.Scan(&h.Table, &h.PartitionsAhead, &h.DefaultRows,
			&h.OrphanAttached, &h.OrphanDetached); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
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

// ForkedPredecessors returns the numbers of stored blocks whose hash does NOT match the
// parent_hash of the block stored immediately above them -- predecessors left on an abandoned
// fork by a reorg. Re-fetching such a block overwrites it (StoreBlock is purge-then-insert)
// with the canonical block; walking upward one step per pass heals a multi-block reorg.
//
// Only blocks strictly above `aboveBlock` are considered -- reorgs are shallow and near the
// head, so bounding the scan there keeps this cheap -- and at most `limit` are returned. The
// parent_hash is the block header's own field; the JOIN is served by the block primary key.
func (s *Store) ForkedPredecessors(ctx context.Context, aboveBlock uint64, limit int) ([]uint64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.number
		FROM   block b
		JOIN   block p ON p.number = b.number - 1
		WHERE  b.number > $1
		  AND  b.parent_hash <> p.hash
		ORDER  BY b.number DESC
		LIMIT  $2`, int64(aboveBlock), limit)
	if err != nil {
		return nil, fmt.Errorf("can not scan for reorged blocks: %w", err)
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
		return s.writeTransactions(ctx, tx, []*types.Transaction{trx}, uint64(blk.Number))
	})
}
