package pg

// ddb_withheld.go -- SEC-07c (NEC-174): what ingest records for a DDB commit the node would not serve,
// and how a TRANSIENT refusal is retried. Migration 00015 explains the two tables; in short, a final
// reason is recorded and never decoded from anywhere else, and `undecidable` is queued, re-fetched,
// and indexed the moment the node serves the commit.

import (
	"context"
	"fmt"
	"ncogearthchain-api-graphql/internal/types"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// ddbPendingBackoffMin and ddbPendingBackoffMax bound how often one undecidable commit is re-asked.
// The common cause -- a block above the node's applied frontier -- clears in seconds; a DDB-off node
// may never clear, and must not cost the node a lookup per commit per tick forever.
const (
	ddbPendingBackoffMin = 15 * time.Second
	ddbPendingBackoffMax = time.Hour
)

// DdbPendingVerdict is one commit queued for a re-fetch because the node answered `undecidable`.
type DdbPendingVerdict struct {
	TxHash      common.Hash
	BlockNumber uint64
	TxIndex     uint32
	TimeStamp   time.Time
	Attempts    int
}

// writeDdbWithheld records a DDB commit the node served without its calldata. It is a no-op for every
// transaction that is not one: ordinary traffic, and a commit that decoded (t.DDB, which
// writeDdbCommit stores).
func (s *Store) writeDdbWithheld(ctx context.Context, q Querier, t *types.Transaction, blockNumber int64, txIndex int32) error {
	if t.DDB != nil || t.InputRedacted == nil || !types.IsDDBTransaction(t.To) {
		return nil
	}
	if types.DdbWithholdIsFinal(t.InputRedacted.Reason) {
		return writeDdbWithheldFinal(ctx, q, t, blockNumber, txIndex)
	}
	// Anything not final is transient -- `undecidable`, and any reason a newer node may add that this
	// explorer does not know. Treating an unknown reason as final would freeze a commit the node may
	// serve tomorrow; treating it as transient costs a bounded number of re-asks.
	if _, err := q.Exec(ctx, `
		INSERT INTO ddb_verdict_pending (block_number, tx_index, tx_hash, ts)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (block_number, tx_index) DO UPDATE SET
		    tx_hash = EXCLUDED.tx_hash,
		    ts      = EXCLUDED.ts`,
		blockNumber, txIndex, HashVal(t.Hash), t.TimeStamp.UTC(),
	); err != nil {
		return fmt.Errorf("can not queue DDB commit %s for a verdict re-fetch: %w", t.Hash.String(), err)
	}
	return nil
}

func writeDdbWithheldFinal(ctx context.Context, q Querier, t *types.Transaction, blockNumber int64, txIndex int32) error {
	w := t.InputRedacted
	if _, err := q.Exec(ctx, `
		INSERT INTO ddb_withheld_commit (block_number, tx_index, tx_hash, reason, schema_name, size, keccak256, ts)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (block_number, tx_index) DO UPDATE SET
		    tx_hash     = EXCLUDED.tx_hash,
		    reason      = EXCLUDED.reason,
		    schema_name = EXCLUDED.schema_name,
		    size        = EXCLUDED.size,
		    keccak256   = EXCLUDED.keccak256,
		    ts          = EXCLUDED.ts`,
		blockNumber, txIndex, HashVal(t.Hash), w.Reason, w.Schema, int64(w.Size), HashVal(w.Keccak256), t.TimeStamp.UTC(),
	); err != nil {
		return fmt.Errorf("can not record withheld DDB commit %s: %w", t.Hash.String(), err)
	}
	return nil
}

// DdbPendingVerdicts lists queued commits due for a re-fetch at `now`, oldest block first.
func (s *Store) DdbPendingVerdicts(ctx context.Context, now time.Time, limit int) ([]DdbPendingVerdict, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT tx_hash, block_number, tx_index, ts, attempts FROM ddb_verdict_pending
		WHERE next_try <= $1
		ORDER BY block_number, tx_index
		LIMIT $2`, now.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("can not list pending DDB verdicts: %w", err)
	}
	defer rows.Close()

	var out []DdbPendingVerdict
	for rows.Next() {
		var (
			hash  []byte
			block int64
			index int32
			p     DdbPendingVerdict
		)
		if err := rows.Scan(&hash, &block, &index, &p.TimeStamp, &p.Attempts); err != nil {
			return nil, fmt.Errorf("can not read a pending DDB verdict: %w", err)
		}
		p.TxHash = common.BytesToHash(hash)
		p.BlockNumber, p.TxIndex = uint64(block), uint32(index)
		out = append(out, p)
	}
	return out, rows.Err()
}

// ResolveDdbPending applies a re-fetch's answer to one queued commit, in one transaction:
//
//   - served (it decoded): the operation is indexed, the transaction row gets its calldata and its
//     DDB marks, and the queue entry goes;
//   - withheld for a FINAL reason: recorded as withheld, and the queue entry goes;
//   - still not decidable (or anything else): the entry stays, with its attempt counted and its next
//     try backed off.
//
// trx is the transaction as the node serves it NOW. It must be the queued one: a re-fetch answered
// for another hash (a reorg moved it) is treated as not decidable, and the re-scan that follows a
// reorg purges the entry with its block.
func (s *Store) ResolveDdbPending(ctx context.Context, p DdbPendingVerdict, trx *types.Transaction) error {
	block, index := int64(p.BlockNumber), int32(p.TxIndex)
	return s.pool.InTx(ctx, func(ctx context.Context, q Tx) error {
		switch {
		case trx != nil && trx.Hash == p.TxHash && trx.DDB != nil && trx.InputRedacted == nil:
			trx.TimeStamp = p.TimeStamp
			if err := s.writeDdbCommit(ctx, q, trx, block, index); err != nil {
				return err
			}
			if _, err := q.Exec(ctx, `
				UPDATE tx SET input = $1, is_ddb = TRUE, ddb_contract = $2
				WHERE block_number = $3 AND tx_index = $4`,
				[]byte(trx.InputData), Addr(trx.DDB.ContractAddress), block, index); err != nil {
				return fmt.Errorf("can not mark DDB commit %s served: %w", p.TxHash.String(), err)
			}
		case trx != nil && trx.Hash == p.TxHash && trx.InputRedacted != nil && types.DdbWithholdIsFinal(trx.InputRedacted.Reason):
			trx.TimeStamp = p.TimeStamp
			if err := writeDdbWithheldFinal(ctx, q, trx, block, index); err != nil {
				return err
			}
		default:
			_, err := q.Exec(ctx, `
				UPDATE ddb_verdict_pending SET attempts = attempts + 1, next_try = $1
				WHERE block_number = $2 AND tx_index = $3`,
				time.Now().UTC().Add(ddbPendingBackoff(p.Attempts+1)), block, index)
			if err != nil {
				return fmt.Errorf("can not back off DDB verdict re-fetch for %s: %w", p.TxHash.String(), err)
			}
			return nil
		}
		if _, err := q.Exec(ctx, `DELETE FROM ddb_verdict_pending WHERE block_number = $1 AND tx_index = $2`, block, index); err != nil {
			return fmt.Errorf("can not dequeue DDB commit %s: %w", p.TxHash.String(), err)
		}
		return nil
	})
}

// ddbPendingBackoff doubles from the minimum per attempt, capped.
func ddbPendingBackoff(attempts int) time.Duration {
	d := ddbPendingBackoffMin
	for i := 1; i < attempts && d < ddbPendingBackoffMax; i++ {
		d *= 2
	}
	if d > ddbPendingBackoffMax {
		d = ddbPendingBackoffMax
	}
	return d
}
