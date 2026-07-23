package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// DDB reads.
//
// These answer questions the node cannot. It serves no DDB history RPC at all, so an
// operation timeline, a requester's activity, or a contract's version history exist only
// because ingest captured them from the commit-transaction stream.

var ddbOpKeyset = Keyset{Columns: []KeyColumn{
	{Name: "block_number", Dir: Desc},
	{Name: "tx_index", Dir: Desc},
}}

const ddbOpColumns = `
	o.block_number, o.tx_index, o.tx_hash, o.request_id, o.requester,
	o.op_type, o.schema_name, o.contract_addr, o.contract_name, o.version,
	o.author, o.payload, o.ts,
	e.operation_hash, e.data_hash, e.state_hash,
	e.prior_post_state_hash, e.post_state_hash, e.epoch,
	e.validator_count, e.signature_count, e.validators`

const ddbOpFrom = `
	FROM ddb_operation o
	LEFT JOIN ddb_endorsement e
	       ON e.block_number = o.block_number AND e.tx_index = o.tx_index`

// DdbOpCriteria narrows an operation query. Every combination it can express is served by
// an index on ddb_operation.
type DdbOpCriteria struct {
	ContractAddress *common.Address
	SchemaName      string
	Requester       *common.Address
	OpType          *int16
}

// DdbOperations lists DDB operations, newest first.
func (s *Store) DdbOperations(ctx context.Context, c DdbOpCriteria, cursor string, count int32) ([]*types.DdbOperation, error) {
	page := NewPage(count, maxListLimit)

	f := NewFilter()
	if c.ContractAddress != nil {
		f.Eq("o.contract_addr", AddrVal(*c.ContractAddress))
	}
	if c.SchemaName != "" {
		f.Eq("o.schema_name", c.SchemaName)
	}
	if c.Requester != nil {
		f.Eq("o.requester", AddrVal(*c.Requester))
	}
	if c.OpType != nil {
		f.Eq("o.op_type", *c.OpType)
	}

	where, args := f.Render(0)

	cur, err := DecodeCursor(cursor, 2)
	if err != nil {
		return nil, err
	}
	if len(cur) == 2 {
		pred, curArgs, err := keysetOn("o", ddbOpKeyset).After(
			[]any{cur[0], int32(cur[1])}, page.Reverse, len(args))
		if err != nil {
			return nil, err
		}
		if where != "" {
			where += " AND "
		}
		where += pred
		args = append(args, curArgs...)
	}

	sql := `SELECT ` + ddbOpColumns + ddbOpFrom
	if where != "" {
		sql += ` WHERE ` + where
	}
	sql += ` ` + keysetOn("o", ddbOpKeyset).OrderBy(page.Reverse) + ` LIMIT $` + itoa(len(args)+1)
	args = append(args, page.Limit)

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("DDB operation query failed: %w", err)
	}
	defer rows.Close()

	out := make([]*types.DdbOperation, 0, page.Limit)
	for rows.Next() {
		op, err := scanDdbOperation(rows)
		if err != nil {
			return nil, fmt.Errorf("can not scan DDB operation: %w", err)
		}
		out = append(out, op)
	}
	return out, rows.Err()
}

// DdbOperationAt loads the operation committed at a block position.
//
// This is what makes Transaction.ddb work. Without it a DDB commit is indistinguishable
// from a plain transfer to the DDB system address.
func (s *Store) DdbOperationAt(ctx context.Context, blockNumber uint64, txIndex uint64) (*types.DdbOperation, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+ddbOpColumns+ddbOpFrom+` WHERE o.block_number = $1 AND o.tx_index = $2`,
		int64(blockNumber), int32(txIndex))

	op, err := scanDdbOperation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("can not load DDB operation at %d/%d: %w", blockNumber, txIndex, err)
	}
	return op, nil
}

// DdbContracts lists known data contracts, most recently active first.
func (s *Store) DdbContracts(ctx context.Context, count int32) ([]*types.DdbContract, error) {
	page := NewPage(count, maxListLimit)

	rows, err := s.pool.Query(ctx, `
		SELECT contract_addr, db_name, contract_name, author, latest_version,
		       first_block, last_block, op_count, created_at, updated_at
		FROM   ddb_contract
		ORDER  BY last_block DESC, last_tx_index DESC
		LIMIT  $1`, page.Limit)
	if err != nil {
		return nil, fmt.Errorf("DDB contract query failed: %w", err)
	}
	defer rows.Close()

	out := make([]*types.DdbContract, 0, page.Limit)
	for rows.Next() {
		var (
			addr, author          []byte
			dbName, name, version *string
			firstBlock, lastBlock int64
			opCount               int64
			createdAt, updatedAt  pgtype.Timestamptz
		)
		if err := rows.Scan(&addr, &dbName, &name, &author, &version,
			&firstBlock, &lastBlock, &opCount, &createdAt, &updatedAt); err != nil {
			return nil, err
		}

		a, err := ToAddr(addr)
		if err != nil {
			return nil, err
		}
		au, _ := ToAddr(author)

		out = append(out, &types.DdbContract{
			Address:        *a,
			DbName:         deref(dbName),
			ContractName:   deref(name),
			Author:         au,
			LatestVersion:  deref(version),
			FirstBlock:     hexutil.Uint64(firstBlock),
			LastBlock:      hexutil.Uint64(lastBlock),
			OperationCount: hexutil.Uint64(opCount),
			CreatedAt:      hexutil.Uint64(createdAt.Time.Unix()),
			UpdatedAt:      hexutil.Uint64(updatedAt.Time.Unix()),
		})
	}
	return out, rows.Err()
}

// DdbOperationCursor renders the pagination cursor for an operation.
func DdbOperationCursor(blockNumber, txIndex uint64) string {
	return EncodeCursor([]int64{int64(blockNumber), int64(txIndex)})
}

// scanDdbOperation maps a joined operation + endorsement row onto the domain type.
func scanDdbOperation(row rowScanner) (*types.DdbOperation, error) {
	var (
		blockNumber              int64
		txIndex                  int32
		txHash, requestID        []byte
		requester                []byte
		opType                   int16
		schemaName               string
		contractAddr, author     []byte
		contractName, version    *string
		payload                  []byte
		ts                       pgtype.Timestamptz
		opHash, dataHash, stHash []byte
		priorState, postState    []byte
		epoch                    *int64
		validatorCount, sigCount *int32
		validators               [][]byte
	)

	if err := row.Scan(&blockNumber, &txIndex, &txHash, &requestID, &requester,
		&opType, &schemaName, &contractAddr, &contractName, &version,
		&author, &payload, &ts,
		&opHash, &dataHash, &stHash, &priorState, &postState, &epoch,
		&validatorCount, &sigCount, &validators); err != nil {
		return nil, err
	}

	th, err := ToHash(txHash)
	if err != nil {
		return nil, err
	}
	rid, err := ToHash(requestID)
	if err != nil {
		return nil, err
	}
	req, err := ToAddr(requester)
	if err != nil {
		return nil, err
	}
	ca, _ := ToAddr(contractAddr)
	au, _ := ToAddr(author)

	op := &types.DdbOperation{
		BlockNumber:     hexutil.Uint64(blockNumber),
		TxIndex:         hexutil.Uint64(txIndex),
		TxHash:          *th,
		RequestID:       *rid,
		Requester:       *req,
		OpType:          opType,
		SchemaName:      schemaName,
		ContractAddress: ca,
		ContractName:    deref(contractName),
		Version:         deref(version),
		Author:          au,
		Payload:         json.RawMessage(payload),
		TimeStamp:       hexutil.Uint64(ts.Time.Unix()),
	}

	// The endorsement arrives through a LEFT join. Both rows are written in the same
	// transaction, so it is present for everything this explorer wrote -- but LEFT means
	// a row that arrived by some other path still returns its operation rather than
	// disappearing from the feed entirely.
	if epoch != nil {
		e := &types.DdbEndorsement{Epoch: hexutil.Uint64(*epoch)}

		if h, err := ToHash(opHash); err == nil && h != nil {
			e.OperationHash = *h
		}
		if h, err := ToHash(dataHash); err == nil && h != nil {
			e.DataHash = *h
		}
		if h, err := ToHash(stHash); err == nil && h != nil {
			e.StateHash = *h
		}

		vs := make([]common.Address, 0, len(validators))
		for _, raw := range validators {
			if a, err := ToAddr(raw); err == nil && a != nil {
				vs = append(vs, *a)
			}
		}
		e.ValidatorSet = vs

		if validatorCount != nil {
			e.ValidatorCount = *validatorCount
		}
		if sigCount != nil {
			e.SignatureCount = *sigCount
		}

		// Left as nil when absent rather than zero-length: a contract's first operation
		// genuinely has no prior state, and an empty byte string would read as "the prior
		// state hash was empty", which is a different claim.
		e.PriorPostStateHash = priorState
		e.PostStateHash = postState

		op.Endorsement = e
	}

	return op, nil
}
