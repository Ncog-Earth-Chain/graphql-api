package resolvers

import (
	"context"
	"encoding/json"

	"ncogearthchain-api-graphql/internal/repository"
	"ncogearthchain-api-graphql/internal/repository/db/pg"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// DdbOperation resolves a DDB operation committed on chain.
type DdbOperation struct{ types.DdbOperation }

// DdbEndorsement resolves the quorum proof.
type DdbEndorsement struct{ types.DdbEndorsement }

// DdbContract resolves a data contract.
type DdbContract struct{ types.DdbContract }

// DdbOperationList resolves a page of operations.
type DdbOperationList struct {
	list  []*types.DdbOperation
	asked int32
}

// DdbOperationListEdge resolves one entry with its cursor.
type DdbOperationListEdge struct {
	Operation *DdbOperation
}

// DdbOperations resolves the on-chain DDB operation history.
//
// This query has no equivalent on the node: it serves no DDB history RPC at all, so these
// records exist only because ingest captured them from the commit-transaction stream.
func (rs *rootResolver) DdbOperations(ctx context.Context, args struct {
	ContractAddress *common.Address
	SchemaName      *string
	Requester       *common.Address
	OpType          *string
	RequestId       *common.Hash
	Cursor          *Cursor
	Count           int32
}) (*DdbOperationList, error) {
	args.Count = listLimitCount(args.Count, listMaxEdgesPerRequest)

	c := pg.DdbOpCriteria{
		ContractAddress: args.ContractAddress,
		Requester:       args.Requester,
		RequestID:       args.RequestId,
	}
	if args.SchemaName != nil {
		c.SchemaName = *args.SchemaName
	}
	if args.OpType != nil {
		if code, ok := ddbOpTypeCode(*args.OpType); ok {
			// A recognised type name maps to its stored op_type code.
			c.OpType = &code
		} else {
			// The only name that reaches here is the enum's UNKNOWN member (all other names are
			// rejected by GraphQL enum validation). It selects operations whose code this build
			// does not recognise -- op_type outside 0..9 -- which is what the unfiltered feed
			// already labels UNKNOWN. Mapping it to a sentinel op_type = -1 (as before) matched no
			// row, so the filter silently dropped exactly the rows the enum value names.
			c.UnknownOpType = true
		}
	}

	rows, err := repository.R().DdbOperations(ctx, c, (*string)(args.Cursor), args.Count)
	if err != nil {
		log.Errorf("can not get DDB operations; %s", err.Error())
		return nil, err
	}
	return &DdbOperationList{list: rows, asked: args.Count}, nil
}

// DdbContracts resolves the known data contracts, most recently active first, paginated.
func (rs *rootResolver) DdbContracts(ctx context.Context, args struct {
	Cursor *Cursor
	Count  int32
}) (*DdbContractList, error) {
	args.Count = listLimitCount(args.Count, listMaxEdgesPerRequest)

	rows, err := repository.R().DdbContracts(ctx, (*string)(args.Cursor), args.Count)
	if err != nil {
		log.Errorf("can not get DDB contracts; %s", err.Error())
		return nil, err
	}
	return &DdbContractList{list: rows, asked: args.Count}, nil
}

// DdbContract resolves a single data contract by its address, or nil if unknown.
//
// The list only pages through contracts by recent activity; this makes any contract
// reachable directly, which the top-N list alone could not do.
func (rs *rootResolver) DdbContract(ctx context.Context, args struct {
	Address common.Address
}) (*DdbContract, error) {
	c, err := repository.R().DdbContract(ctx, args.Address)
	if err != nil {
		log.Errorf("can not get DDB contract %s; %s", args.Address.String(), err.Error())
		return nil, err
	}
	if c == nil {
		return nil, nil
	}
	return &DdbContract{DdbContract: *c}, nil
}

// DdbContractList resolves a page of data contracts.
type DdbContractList struct {
	list  []*types.DdbContract
	asked int32
}

// DdbContractListEdge resolves one contract with its cursor.
type DdbContractListEdge struct {
	Contract *DdbContract
}

// Edges resolves the page entries.
func (dl *DdbContractList) Edges() []DdbContractListEdge {
	out := make([]DdbContractListEdge, len(dl.list))
	for i, c := range dl.list {
		out[i] = DdbContractListEdge{Contract: &DdbContract{DdbContract: *c}}
	}
	return out
}

// PageInfo resolves the page boundaries from whether the page came back short.
func (dl *DdbContractList) PageInfo() (*ListPageInfo, error) {
	if len(dl.list) == 0 {
		return NewListPageInfo(nil, nil, false, false)
	}

	first := Cursor(pg.DdbContractCursor(
		uint64(dl.list[0].LastBlock), uint64(dl.list[0].LastTxIndex)))
	l := dl.list[len(dl.list)-1]
	last := Cursor(pg.DdbContractCursor(uint64(l.LastBlock), uint64(l.LastTxIndex)))

	short := int32(len(dl.list)) < absCount(dl.asked)
	return NewListPageInfo(&first, &last, !short, false)
}

// Cursor resolves an edge's pagination cursor.
func (e DdbContractListEdge) Cursor() Cursor {
	return Cursor(pg.DdbContractCursor(
		uint64(e.Contract.LastBlock), uint64(e.Contract.LastTxIndex)))
}

// Edges resolves the page entries.
func (dl *DdbOperationList) Edges() []DdbOperationListEdge {
	out := make([]DdbOperationListEdge, len(dl.list))
	for i, op := range dl.list {
		out[i] = DdbOperationListEdge{Operation: &DdbOperation{DdbOperation: *op}}
	}
	return out
}

// PageInfo resolves the page boundaries from whether the page came back short.
func (dl *DdbOperationList) PageInfo() (*ListPageInfo, error) {
	if len(dl.list) == 0 {
		return NewListPageInfo(nil, nil, false, false)
	}

	first := Cursor(pg.DdbOperationCursor(
		uint64(dl.list[0].BlockNumber), uint64(dl.list[0].TxIndex)))
	l := dl.list[len(dl.list)-1]
	last := Cursor(pg.DdbOperationCursor(uint64(l.BlockNumber), uint64(l.TxIndex)))

	short := int32(len(dl.list)) < absCount(dl.asked)
	return NewListPageInfo(&first, &last, !short, false)
}

// Cursor resolves an edge's pagination cursor.
func (e DdbOperationListEdge) Cursor() Cursor {
	return Cursor(pg.DdbOperationCursor(
		uint64(e.Operation.BlockNumber), uint64(e.Operation.TxIndex)))
}

// OpType resolves the operation kind as its enum name.
func (op *DdbOperation) OpType() string {
	return types.DdbOperationTypeName(op.DdbOperation.OpType)
}

// SchemaName resolves the schema name, or nil when the operation names none.
func (op *DdbOperation) SchemaName() *string {
	if op.DdbOperation.SchemaName == "" {
		return nil
	}
	s := op.DdbOperation.SchemaName
	return &s
}

// ContractName resolves the contract name, or nil.
func (op *DdbOperation) ContractName() *string {
	if op.DdbOperation.ContractName == "" {
		return nil
	}
	s := op.DdbOperation.ContractName
	return &s
}

// Version resolves the contract version, or nil. A string on the wire, not a number.
func (op *DdbOperation) Version() *string {
	if op.DdbOperation.Version == "" {
		return nil
	}
	s := op.DdbOperation.Version
	return &s
}

// Payload resolves the operation JSON verbatim.
//
// Stays dynamic because the operation's shape is user-defined; a fixed type would be a
// claim the data does not support.
func (op *DdbOperation) Payload() JSONAny {
	var v interface{}
	if err := jsonUnmarshal(op.DdbOperation.Payload, &v); err != nil {
		return JSONAny{Value: nil}
	}
	return JSONAny{Value: v}
}

// Endorsement resolves the quorum proof.
func (op *DdbOperation) Endorsement() *DdbEndorsement {
	if op.DdbOperation.Endorsement == nil {
		return nil
	}
	return &DdbEndorsement{DdbEndorsement: *op.DdbOperation.Endorsement}
}

// Transaction resolves the commit transaction that carried this operation.
func (op *DdbOperation) Transaction(ctx context.Context) (*Transaction, error) {
	trx, err := repository.R().Transaction(&op.DdbOperation.TxHash)
	if err != nil || trx == nil {
		return nil, err
	}
	return NewTransaction(trx), nil
}

// PriorPostStateHash resolves the previous link in the per-contract state-hash chain.
// Nil for a contract's first operation.
func (e *DdbEndorsement) PriorPostStateHash() *hexutil.Bytes {
	if len(e.DdbEndorsement.PriorPostStateHash) == 0 {
		return nil
	}
	b := e.DdbEndorsement.PriorPostStateHash
	return &b
}

// PostStateHash resolves this operation's own post-state hash.
func (e *DdbEndorsement) PostStateHash() *hexutil.Bytes {
	if len(e.DdbEndorsement.PostStateHash) == 0 {
		return nil
	}
	b := e.DdbEndorsement.PostStateHash
	return &b
}

// DbName resolves the contract's PostgreSQL schema name, or nil.
func (c *DdbContract) DbName() *string {
	if c.DdbContract.DbName == "" {
		return nil
	}
	s := c.DdbContract.DbName
	return &s
}

// ContractName resolves the contract name, or nil.
func (c *DdbContract) ContractName() *string {
	if c.DdbContract.ContractName == "" {
		return nil
	}
	s := c.DdbContract.ContractName
	return &s
}

// LatestVersion resolves the most recent version, or nil.
func (c *DdbContract) LatestVersion() *string {
	if c.DdbContract.LatestVersion == "" {
		return nil
	}
	s := c.DdbContract.LatestVersion
	return &s
}

// StateHashChainValid reports whether this contract's per-operation state-hash chain is
// continuous -- the verification the "verifiable" priorPostStateHash/postStateHash fields
// imply but which nothing performed until now. False means a link is broken: an operation's
// priorPostStateHash does not equal the previous operation's postStateHash.
func (c *DdbContract) StateHashChainValid(ctx context.Context) (bool, error) {
	return repository.R().DdbStateHashChainValid(ctx, c.DdbContract.Address)
}

// Operations resolves this contract's operation history, newest first.
func (c *DdbContract) Operations(ctx context.Context, args struct {
	Cursor *Cursor
	Count  int32
}) ([]*DdbOperation, error) {
	args.Count = listLimitCount(args.Count, listMaxEdgesPerRequest)

	addr := c.DdbContract.Address
	rows, err := repository.R().DdbOperations(ctx,
		pg.DdbOpCriteria{ContractAddress: &addr}, (*string)(args.Cursor), args.Count)
	if err != nil {
		return nil, err
	}

	out := make([]*DdbOperation, len(rows))
	for i, op := range rows {
		out[i] = &DdbOperation{DdbOperation: *op}
	}
	return out, nil
}

// ddbOpTypeCode maps an operation-type name to its wire code.
// jsonUnmarshal is a thin indirection so the resolver does not import encoding/json
// directly alongside the GraphQL scalar handling.
func jsonUnmarshal(b []byte, v interface{}) error {
	return json.Unmarshal(b, v)
}

func ddbOpTypeCode(name string) (int16, bool) {
	for c := int16(0); c <= types.DdbOpRevokeRole; c++ {
		if types.DdbOperationTypeName(c) == name {
			return c, true
		}
	}
	return 0, false
}

// Ddb resolves the DDB operation this transaction committed, if it is a DDB commit.
//
// Without this a DDB commit transaction is indistinguishable from a plain transfer to the
// DDB system address -- which is how it looked before, and why data-contract activity was
// invisible in the explorer even when it was being indexed.
func (trx *Transaction) Ddb(ctx context.Context) (*DdbOperation, error) {
	if trx.BlockNumber == nil || trx.Index == nil {
		return nil, nil
	}

	op, err := repository.R().DdbOperationAt(ctx, uint64(*trx.BlockNumber), uint64(*trx.Index))
	if err != nil || op == nil {
		return nil, err
	}
	return &DdbOperation{DdbOperation: *op}, nil
}
