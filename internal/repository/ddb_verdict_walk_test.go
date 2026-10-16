package repository

// ddb_verdict_walk_test.go -- SEC-07c (NEC-174): every field this repository serves that carries DDB
// commit calldata, walked over the node redaction walk's fixtures
// (ncogearthchain ethapi/ddb_calldata_redaction_test.go).
//
// The node is stood up IN PROCESS (go-ethereum rpc.NewServer + DialInProc) and answers
// eth_getTransactionByHash for each fixture exactly as the node's walk says it must: bytes verbatim
// for an applied public commit, `input` "0x" + an inputRedacted descriptor for everything else. What
// is under test is not the node's verdict -- that is the node's own walk -- but that this repository
// asks for it on every field and binds it to the right row: the indexed rows below carry the FULL
// operation JSON, as an explorer that ingested before SEC-07 holds it, so any field served without
// asking leaks the fixture's marker.

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"ncogearthchain-api-graphql/internal/logger"
	"ncogearthchain-api-graphql/internal/repository/rpc"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	nec2 "github.com/ethereum/go-ethereum/rpc"
)

// walkLogger drops everything; the walk asserts on what is served, not on what is logged.
type walkLogger struct{ logger.Logger }

func (walkLogger) Errorf(string, ...interface{})   {}
func (walkLogger) Warningf(string, ...interface{}) {}
func (walkLogger) Debugf(string, ...interface{})   {}
func (walkLogger) Infof(string, ...interface{})    {}

// walkFixture is one commit of the node walk.
type walkFixture struct {
	name     string
	opType   int16
	schema   string // schema_name as the operation carries it: the HUMAN name for a deploy/upgrade
	contract common.Address
	reader   bool   // the carried definition declares a `reader` role (deploy/upgrade)
	reason   string // the node's verdict: "" = served verbatim
	public   bool   // an applied public commit: MUST be served
}

func (f walkFixture) marker() string { return "SECRET-" + strings.ReplaceAll(f.name, " ", "-") }

func (f walkFixture) hash() common.Hash {
	return common.BytesToHash([]byte(f.name))
}

// operation is the operation JSON as it rides the wire.
func (f walkFixture) operation() []byte {
	op := map[string]any{"type": f.opType, "schema_name": f.schema, "note": f.marker()}
	if f.opType == types.DdbOpCreateSchema || f.opType == types.DdbOpUpdateSchema {
		def := map[string]any{"contract_address": f.contract.Hex(), "contract_name": f.schema, "secret": f.marker()}
		if f.reader {
			def["roles"] = map[string]any{"reader": []string{"0x00000000000000000000000000000000000000aa"}}
		}
		op["data"] = def
	}
	b, _ := json.Marshal(op)
	return b
}

// calldata is the commit transaction's input: the legacy "DDBE" + JSON form, which the decoder and the
// node both accept.
func (f walkFixture) calldata() []byte {
	return []byte(`DDBE{"endorsement":{"requestId":"0x00000000000000000000000000000000000000000000000000000000000000c3",` +
		`"requester":"0x000000000000000000000000000000000000000f","blockNumber":9,` +
		`"validatorSet":["0x0000000000000000000000000000000000000001"],"signatures":[{}]},` +
		`"operation":` + string(f.operation()) + `}`)
}

// row is the indexed DdbOperation as an explorer that ingested before SEC-07 holds it: payload whole.
func (f walkFixture) row() *types.DdbOperation {
	c := f.contract
	return &types.DdbOperation{
		TxHash: f.hash(), OpType: f.opType, SchemaName: f.schema, ContractAddress: &c,
		Payload: f.operation(),
	}
}

// tx is the indexed Transaction: calldata whole, no descriptor (the local index never stored one).
func (f walkFixture) tx() *types.Transaction {
	return &types.Transaction{Hash: f.hash(), To: &types.DDBContractAddress, InputData: f.calldata()}
}

// fakeEth answers eth_getTransactionByHash as the node's redaction walk does.
type fakeEth struct {
	mu   sync.Mutex
	fixt map[common.Hash]walkFixture
}

func (e *fakeEth) GetTransactionByHash(h common.Hash) map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	f, ok := e.fixt[h]
	if !ok {
		return nil
	}
	if f.reason == "" {
		return map[string]any{"hash": h, "input": hexutil.Bytes(f.calldata())}
	}
	return map[string]any{"hash": h, "input": "0x", "inputRedacted": map[string]any{
		"reason": f.reason, "schema": types.DdbDeriveDbName(f.contract.Hex()),
		"size": hexutil.Uint64(len(f.calldata())), "keccak256": common.Hash{}}}
}

func (e *fakeEth) set(f walkFixture) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.fixt[f.hash()] = f
}

func addr(b byte) common.Address { var a common.Address; a[19] = b; a[0] = 0xc0; return a }

// nodeWalkFixtures mirrors ethapi/ddb_calldata_redaction_test.go: in the applied block, above the
// applied frontier, and in the pool.
func nodeWalkFixtures() []walkFixture {
	restricted, tomb, unreg, pub := addr(1), addr(2), addr(3), addr(4)
	return []walkFixture{
		// in the applied block
		{name: "restricted call", opType: types.DdbOpCallProcedure, schema: types.DdbDeriveDbName(restricted.Hex()), contract: restricted, reason: types.DdbWithheldReaderRole},
		{name: "restricted deploy under a human name", opType: types.DdbOpCreateSchema, schema: "acct", contract: restricted, reader: true, reason: types.DdbWithheldReaderRole},
		{name: "restricted upgrade under a human name", opType: types.DdbOpUpdateSchema, schema: "acct", contract: restricted, reader: true, reason: types.DdbWithheldReaderRole},
		{name: "tombstoned deploy", opType: types.DdbOpCreateSchema, schema: "gone", contract: tomb, reason: types.DdbWithheldTombstoned},
		{name: "unregistered reader deploy", opType: types.DdbOpCreateSchema, schema: "unreg", contract: unreg, reader: true, reason: types.DdbWithheldReaderRole},
		{name: "call on the unregistered reader contract", opType: types.DdbOpCallProcedure, schema: types.DdbDeriveDbName(unreg.Hex()), contract: unreg, reason: types.DdbWithheldUndecidable},
		{name: "public call", opType: types.DdbOpCallProcedure, schema: types.DdbDeriveDbName(pub.Hex()), contract: pub, public: true},
		{name: "public deploy under a human name", opType: types.DdbOpCreateSchema, schema: "pub", contract: pub, public: true},
		// above the applied frontier
		{name: "reader upgrade above the frontier", opType: types.DdbOpUpdateSchema, schema: "pub", contract: pub, reader: true, reason: types.DdbWithheldUndecidable},
		{name: "call after the reader upgrade", opType: types.DdbOpCallProcedure, schema: types.DdbDeriveDbName(pub.Hex()), contract: pub, reason: types.DdbWithheldUndecidable},
		// in the pool
		{name: "pool reader upgrade", opType: types.DdbOpUpdateSchema, schema: "pub", contract: pub, reader: true, reason: types.DdbWithheldUndecidable},
		{name: "pool call after it", opType: types.DdbOpCallProcedure, schema: types.DdbDeriveDbName(pub.Hex()), contract: pub, reason: types.DdbWithheldUndecidable},
		{name: "pool public call", opType: types.DdbOpCallProcedure, schema: types.DdbDeriveDbName(pub.Hex()), contract: pub, reason: types.DdbWithheldUndecidable},
		{name: "pool public deploy", opType: types.DdbOpCreateSchema, schema: "pub2", contract: addr(5), reason: types.DdbWithheldUndecidable},
	}
}

func walkProxy(t *testing.T, fixtures []walkFixture) (*proxy, *fakeEth) {
	t.Helper()
	eth := &fakeEth{fixt: make(map[common.Hash]walkFixture)}
	for _, f := range fixtures {
		eth.set(f)
	}
	srv := nec2.NewServer()
	if err := srv.RegisterName("eth", eth); err != nil {
		t.Fatalf("register fake node: %v", err)
	}
	t.Cleanup(srv.Stop)
	client := nec2.DialInProc(srv)
	t.Cleanup(client.Close)
	// No verdict cache: every read asks the node, so the walk sees the node's answer as of now.
	return &proxy{rpc: rpc.NewDdbVerdictBridge(client, walkLogger{}), log: walkLogger{}}, eth
}

// served renders a value as a client would receive it.
func served(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// TestDdbCalldataFieldsWalkTheNodeVerdict: none of the fields carrying tx input returns a byte of a
// withheld commit's operation JSON, and the applied public ones are served whole.
func TestDdbCalldataFieldsWalkTheNodeVerdict(t *testing.T) {
	fixtures := nodeWalkFixtures()
	p, _ := walkProxy(t, fixtures)
	ctx := t.Context()

	ops := make([]*types.DdbOperation, len(fixtures))
	txs := make([]*types.Transaction, len(fixtures))
	for i, f := range fixtures {
		ops[i], txs[i] = f.row(), f.tx()
	}
	// DdbOperation.payload -- the field behind ddbOperations, ddbContract.operations, Transaction.ddb.
	p.ddbFilterOperations(ctx, ops)
	// Transaction.inputData -- and Transaction.inputRedacted, which the internal-transaction resolver
	// (redactInternalInputs) keys the top-level trace frame's input on.
	p.ddbFilterTxInput(ctx, txs...)

	for i, f := range fixtures {
		op, tx := served(t, ops[i]), served(t, txs[i])
		if f.public {
			if !strings.Contains(op, f.marker()) {
				t.Errorf("%s: an applied public commit's payload was not served: %s", f.name, op)
			}
			if !strings.Contains(string(txs[i].InputData), f.marker()) || txs[i].InputRedacted != nil {
				t.Errorf("%s: an applied public commit's calldata was not served", f.name)
			}
			continue
		}
		if strings.Contains(op, f.marker()) {
			t.Errorf("%s: DdbOperation.payload leaked the withheld operation: %s", f.name, op)
		}
		if strings.Contains(tx, f.marker()) {
			t.Errorf("%s: Transaction.inputData leaked the withheld operation", f.name)
		}
		if ops[i].Withheld == nil || ops[i].Withheld.Reason != f.reason {
			t.Errorf("%s: withheld = %+v, want reason %q", f.name, ops[i].Withheld, f.reason)
		}
		if txs[i].InputRedacted == nil {
			t.Errorf("%s: the transaction carries no inputRedacted, so its trace's top frame is served", f.name)
		}
	}
}

// TestDdbUndecidableIsServedOnceTheNodeApplies: a commit withheld as `undecidable` is served on the
// next read once the node has applied its block -- the read path never remembers a transient refusal.
func TestDdbUndecidableIsServedOnceTheNodeApplies(t *testing.T) {
	f := walkFixture{name: "freshly deployed public contract", opType: types.DdbOpCreateSchema, schema: "fresh",
		contract: addr(9), reason: types.DdbWithheldUndecidable}
	p, eth := walkProxy(t, []walkFixture{f})
	ctx := t.Context()

	row := f.row()
	p.ddbFilterOperations(ctx, []*types.DdbOperation{row})
	if row.Payload != nil || row.Withheld == nil || row.Withheld.Reason != types.DdbWithheldUndecidable {
		t.Fatalf("before the node applies: payload %q, withheld %+v; want withheld undecidable", row.Payload, row.Withheld)
	}

	f.reason = "" // the node has applied the block: it serves the commit verbatim
	eth.set(f)
	row = f.row()
	row.Payload = nil // as ingest stored it while undecidable
	p.ddbFilterOperations(ctx, []*types.DdbOperation{row})
	if row.Withheld != nil || !strings.Contains(string(row.Payload), f.marker()) {
		t.Fatalf("after the node applies: payload %q, withheld %+v; want served", row.Payload, row.Withheld)
	}
}

// TestDdbVerdictBindsToTheRowsContract: bytes the node authorizes for a transaction are served only if
// they are the operation the ROW claims -- a stale row whose hash now names another contract's
// commit is withheld rather than filled with the other commit.
func TestDdbVerdictBindsToTheRowsContract(t *testing.T) {
	pub := walkFixture{name: "public call", opType: types.DdbOpCallProcedure, schema: types.DdbDeriveDbName(addr(4).Hex()), contract: addr(4)}
	p, _ := walkProxy(t, []walkFixture{pub})

	stale := pub.row()
	stale.SchemaName = types.DdbDeriveDbName(addr(1).Hex()) // the row claims the restricted contract
	p.ddbFilterOperations(t.Context(), []*types.DdbOperation{stale})
	if stale.Withheld == nil || stale.Payload != nil {
		t.Fatalf("a row bound to a different contract than the served operation was served: %s", fmt.Sprint(stale.Payload))
	}
}
