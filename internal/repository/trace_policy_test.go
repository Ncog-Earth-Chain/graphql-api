package repository

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"ncogearthchain-api-graphql/internal/repository/rpc"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	necrpc "github.com/ethereum/go-ethereum/rpc"
)

// The debug service deliberately returns a successful raw trace containing a
// nonsensitive marker even when eth withholds its calldata. A missing debug
// method or replay failure must never make this policy regression pass.
type tracePolicyNode struct {
	mu          sync.Mutex
	txs         map[common.Hash]map[string]interface{}
	txErr       error
	block       map[string]interface{}
	blockErr    error
	traces      int
	traceHash   common.Hash
	traceMethod string
}

var tracePolicyTx = common.HexToHash("0x1234")
var tracePolicyPlain = common.HexToHash("0x5678")
var tracePolicyBlock = common.HexToHash("0x9abc")

const tracePolicyMarker = "synthetic-trace-policy-marker"

func (n *tracePolicyNode) GetTransactionByHash(_ context.Context, hash common.Hash) (map[string]interface{}, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.txs[hash], n.txErr
}

func (n *tracePolicyNode) GetBlockByNumber(number hexutil.Uint64, full bool) (map[string]interface{}, error) {
	return n.block, n.blockErr
}

func (n *tracePolicyNode) GetBlockByHash(hash common.Hash, full bool) (map[string]interface{}, error) {
	return n.block, n.blockErr
}

type tracePolicyDebug struct{ node *tracePolicyNode }

func (d *tracePolicyDebug) result(method string, hash common.Hash) interface{} {
	d.node.mu.Lock()
	defer d.node.mu.Unlock()
	d.node.traces++
	d.node.traceHash = hash
	d.node.traceMethod = method
	return map[string]interface{}{"input": tracePolicyMarker, "publicControl": true}
}

func (d *tracePolicyDebug) TraceTransaction(hash common.Hash, opts *map[string]interface{}) (interface{}, error) {
	return d.result("transaction", hash), nil
}

func (d *tracePolicyDebug) TraceBlockByNumber(number hexutil.Uint64, opts *map[string]interface{}) (interface{}, error) {
	return d.result("number", common.Hash{}), nil
}

func (d *tracePolicyDebug) TraceBlockByHash(hash common.Hash, opts *map[string]interface{}) (interface{}, error) {
	return d.result("hash", hash), nil
}

func newTracePolicyBridge(t *testing.T) (*proxy, *tracePolicyNode) {
	t.Helper()
	n := &tracePolicyNode{
		txs: map[common.Hash]map[string]interface{}{
			tracePolicyTx:    {"hash": tracePolicyTx, "input": hexutil.Bytes("DDBR" + tracePolicyMarker)},
			tracePolicyPlain: {"hash": tracePolicyPlain, "input": "0x"},
		},
		block: map[string]interface{}{"hash": tracePolicyBlock, "number": hexutil.Uint64(5),
			"transactions": []common.Hash{tracePolicyPlain, tracePolicyTx}},
	}
	srv := necrpc.NewServer()
	if err := srv.RegisterName("eth", n); err != nil {
		t.Fatal(err)
	}
	if err := srv.RegisterName("debug", &tracePolicyDebug{node: n}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)
	client := necrpc.DialInProc(srv)
	t.Cleanup(client.Close)
	return &proxy{rpc: rpc.NewDdbVerdictBridge(client, walkLogger{}), log: walkLogger{}}, n
}

func callPolicyTrace(ctx context.Context, br *proxy, root string) (interface{}, error) {
	params := map[string]interface{}{"tracer": "callTracer"}
	switch root {
	case "transaction":
		return br.TraceTransaction(ctx, tracePolicyTx, params)
	case "number":
		return br.TraceBlockByNumber(ctx, 5, params)
	case "hash":
		return br.TraceBlockByHash(ctx, tracePolicyBlock, params)
	default:
		panic("unknown test root")
	}
}

func TestRawTracePolicyRefusesWithheldAndUnavailable(t *testing.T) {
	for _, root := range []string{"transaction", "number", "hash"} {
		for _, reason := range []string{types.DdbWithheldReaderRole, types.DdbWithheldTombstoned,
			types.DdbWithheldUndecidable, types.DdbWithheldUndecodable, "rpc-error", "missing", "wrong-hash"} {
			t.Run(root+"/"+reason, func(t *testing.T) {
				br, n := newTracePolicyBridge(t)
				switch reason {
				case "rpc-error":
					n.txErr = errors.New("policy backend unavailable")
				case "missing":
					delete(n.txs, tracePolicyTx)
				case "wrong-hash":
					n.txs[tracePolicyTx]["hash"] = tracePolicyPlain
				default:
					n.txs[tracePolicyTx] = map[string]interface{}{"hash": tracePolicyTx, "input": "0x",
						"inputRedacted": map[string]interface{}{"reason": reason}}
				}
				got, err := callPolicyTrace(t.Context(), br, root)
				if err == nil || got != nil {
					t.Fatalf("protected %s trace was served: result=%v error=%v", root, got, err)
				}
				if n.traces != 0 {
					t.Fatalf("debug executed %d times before refusal", n.traces)
				}
			})
		}
	}
}

func TestRawTracePolicyPublicControls(t *testing.T) {
	for _, root := range []string{"transaction", "number", "hash"} {
		for _, input := range []hexutil.Bytes{nil, hexutil.Bytes("DDBR" + tracePolicyMarker)} {
			t.Run(root+"/"+input.String(), func(t *testing.T) {
				br, n := newTracePolicyBridge(t)
				n.txs[tracePolicyTx]["input"] = input
				got, err := callPolicyTrace(t.Context(), br, root)
				want := map[string]interface{}{"input": tracePolicyMarker, "publicControl": true}
				if err != nil || !reflect.DeepEqual(got, want) || n.traces != 1 {
					t.Fatalf("successful trace control lost: result=%v error=%v debug calls=%d", got, err, n.traces)
				}
			})
		}
	}
}

func TestRawTracePolicyBlockMembershipFailsClosed(t *testing.T) {
	for _, root := range []string{"number", "hash"} {
		for _, bad := range []string{"missing-block", "block-error", "missing-hash", "wrong-hash",
			"missing-number", "missing-transactions", "null-transactions", "zero-transaction"} {
			t.Run(root+"/"+bad, func(t *testing.T) {
				br, n := newTracePolicyBridge(t)
				switch bad {
				case "missing-block":
					n.block = nil
				case "block-error":
					n.blockErr = errors.New("block unavailable")
				case "missing-hash":
					delete(n.block, "hash")
				case "wrong-hash":
					if root == "number" {
						n.block["number"] = hexutil.Uint64(6)
					} else {
						n.block["hash"] = tracePolicyPlain
					}
				case "missing-number":
					delete(n.block, "number")
				case "missing-transactions":
					delete(n.block, "transactions")
				case "null-transactions":
					n.block["transactions"] = nil
				case "zero-transaction":
					n.block["transactions"] = []common.Hash{{}}
				}
				got, err := callPolicyTrace(t.Context(), br, root)
				if err == nil || got != nil || n.traces != 0 {
					t.Fatalf("uncertain block membership accepted: result=%v error=%v debug calls=%d", got, err, n.traces)
				}
			})
		}
	}
}

func TestRawTracePolicyPinsBlockNumberToInspectedHash(t *testing.T) {
	br, n := newTracePolicyBridge(t)
	got, err := br.TraceBlockByNumber(t.Context(), 5, nil)
	if err != nil || got == nil {
		t.Fatalf("public block trace failed: %v", err)
	}
	if n.traceMethod != "hash" || n.traceHash != tracePolicyBlock {
		t.Fatalf("trace did not bind the inspected block: method=%s hash=%s", n.traceMethod, n.traceHash)
	}
}

func TestRawTracePolicyEmptyBlockAndZeroTransaction(t *testing.T) {
	for _, root := range []string{"number", "hash"} {
		t.Run(root, func(t *testing.T) {
			br, n := newTracePolicyBridge(t)
			n.block["transactions"] = []common.Hash{}
			got, err := callPolicyTrace(t.Context(), br, root)
			if err != nil || got == nil || n.traces != 1 {
				t.Fatalf("empty block refused: %v", err)
			}
		})
	}
	br, n := newTracePolicyBridge(t)
	got, err := br.TraceTransaction(t.Context(), common.Hash{}, nil)
	if err == nil || got != nil || n.traces != 0 {
		t.Fatalf("zero transaction accepted: result=%v error=%v", got, err)
	}
}

func TestRawTracePolicyHonorsCancellation(t *testing.T) {
	br, n := newTracePolicyBridge(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := callPolicyTrace(ctx, br, "transaction")
	if err == nil || got != nil || n.traces != 0 {
		t.Fatalf("canceled trace accepted: result=%v error=%v", got, err)
	}
}

func TestRawTracePolicyKeepsFilteredInternalSource(t *testing.T) {
	br, n := newTracePolicyBridge(t)
	n.txs[tracePolicyTx] = map[string]interface{}{"hash": tracePolicyTx, "input": "0x",
		"inputRedacted": map[string]interface{}{"reason": types.DdbWithheldReaderRole}}
	got, err := br.InternalTransactionTrace(t.Context(), tracePolicyTx)
	if err != nil || got == nil || n.traces != 1 {
		t.Fatalf("filtered internal view lost its trace source: result=%v error=%v", got, err)
	}
	got, err = callPolicyTrace(t.Context(), br, "transaction")
	if err == nil || got != nil || n.traces != 1 {
		t.Fatalf("raw root bypassed policy through internal source: result=%v error=%v debug calls=%d", got, err, n.traces)
	}
}

func TestRawTracePolicyChecksEveryBlockMember(t *testing.T) {
	for _, root := range []string{"number", "hash"} {
		for _, position := range []int{0, 1, 2} {
			t.Run(root+"/"+string(rune('0'+position)), func(t *testing.T) {
				br, n := newTracePolicyBridge(t)
				hashes := make([]common.Hash, 3)
				for i := range hashes {
					hashes[i] = common.Hash{31: byte(i + 1)}
					n.txs[hashes[i]] = map[string]interface{}{"hash": hashes[i], "input": "0x"}
				}
				hashes[position] = tracePolicyTx
				n.block["transactions"] = hashes
				n.txs[tracePolicyTx] = map[string]interface{}{"hash": tracePolicyTx, "input": "0x",
					"inputRedacted": map[string]interface{}{"reason": types.DdbWithheldReaderRole}}
				got, err := callPolicyTrace(t.Context(), br, root)
				if err == nil || got != nil || n.traces != 0 {
					t.Fatalf("member %d did not refuse the whole block: result=%v error=%v", position, got, err)
				}
			})
		}
	}
}
