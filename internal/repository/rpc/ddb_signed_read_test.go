package rpc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"ncogearthchain-api-graphql/internal/types"

	nec "github.com/ethereum/go-ethereum/rpc"
)

// fakeDdbNode is a stand-in for the node's JSON-RPC endpoint. It records every request it receives,
// raw, so a test can assert on exactly what the bridge put on the wire, and answers every call with
// the same row set.
type fakeDdbNode struct {
	mu     sync.Mutex
	calls  int
	method string
	params []json.RawMessage
}

// fakeDdbNodeRows is the result the fake node serves for any call.
const fakeDdbNodeRows = `[{"id":1,"name":"alice"}]`

// newFakeDdbNode starts a fake node and returns a bridge wired to it. Only the rpc field is set: the
// signed-read path touches nothing else on the bridge.
func newFakeDdbNode(t *testing.T) (*NecBridge, *fakeDdbNode) {
	t.Helper()
	fn := &fakeDdbNode{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("fake node: undecodable request: %v", err)
			return
		}
		fn.mu.Lock()
		fn.calls++
		fn.method = req.Method
		fn.params = req.Params
		fn.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(req.ID) + `,"result":` + fakeDdbNodeRows + `}`))
	}))
	t.Cleanup(srv.Close)

	c, err := nec.DialHTTP(srv.URL)
	if err != nil {
		t.Fatalf("dial fake node: %v", err)
	}
	t.Cleanup(c.Close)
	return &NecBridge{rpc: c}, fn
}

// clientSignedRead is a read envelope as a client (the SDK or a wallet) would hand it to the bridge.
// The values are deliberately in forms a "helpful" bridge would be tempted to normalize: a
// mixed-case checksummed address, and hex quantities, keys and signatures exactly as the client
// encoded them. Nothing here is a valid ML-DSA signature; the bridge must not care, because
// verifying is the node's job and the bridge has no business forming an opinion about it.
func clientSignedRead() *types.SignedDdbRead {
	return &types.SignedDdbRead{
		SchemaName:   "medical_records",
		TableName:    "patients",
		JoinTables:   []string{"doctors", "visits"},
		From:         "0xAbCdEf0123456789aBcDeF0123456789AbCdEf01",
		Timestamp:    "0x6553f100",
		Nonce:        "0x1234abcd5678ef90",
		CallerPubKey: "0x0a0b0c0d0e0f",
		CallerSig:    "0xdeadbeefcafebabe",
	}
}

// TestDdbSignedSelectForwardsEnvelopeVerbatim pins the bridge's one job on the signed-read path:
// PASS-THROUGH. A server-side bridge holds no reader's key, so it must never sign; and because every
// field of the envelope is inside the ML-DSA preimage (SignedReadHash), a bridge that fills, rewrites
// or "normalizes" any of them turns a valid read into a signature failure at the node -- or, worse,
// into a read as someone else. So the envelope the node receives must be the one the client sent,
// field for field and byte for byte, and the query options must ride alongside untouched.
func TestDdbSignedSelectForwardsEnvelopeVerbatim(t *testing.T) {
	br, node := newFakeDdbNode(t)
	read := clientSignedRead()
	opts := map[string]interface{}{"limit": float64(10), "orderBy": "id"}

	rows, err := br.DdbSignedSelect(read, opts)
	if err != nil {
		t.Fatalf("DdbSignedSelect: %v", err)
	}

	if node.calls != 1 {
		t.Fatalf("node saw %d calls, want exactly 1", node.calls)
	}
	if node.method != "ddb_signedSelect" {
		t.Fatalf("forwarded to %q, want ddb_signedSelect", node.method)
	}
	if len(node.params) != 2 {
		t.Fatalf("forwarded %d params, want 2 (signed envelope, unsigned query)", len(node.params))
	}

	// params[0]: every key the client sent, with exactly the bytes it sent, and nothing added. The
	// expectation is built from a FRESH copy of the client's envelope, never from `read`: the bridge
	// holds that pointer, so comparing against it would agree with any rewrite the bridge made in place.
	sent := clientSignedRead()
	var got map[string]interface{}
	if err := json.Unmarshal(node.params[0], &got); err != nil {
		t.Fatalf("forwarded envelope is not a JSON object: %v (%s)", err, node.params[0])
	}
	want := map[string]interface{}{
		"schemaName":   sent.SchemaName,
		"tableName":    sent.TableName,
		"joinTables":   []interface{}{"doctors", "visits"},
		"from":         sent.From,
		"timestamp":    sent.Timestamp,
		"nonce":        sent.Nonce,
		"callerPubKey": sent.CallerPubKey,
		"callerSig":    sent.CallerSig,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("forwarded envelope differs from the client's\n got: %s\nwant: %v", node.params[0], want)
	}

	// and the caller's own envelope is left as it was handed in.
	if !reflect.DeepEqual(read, sent) {
		t.Fatalf("bridge altered the caller's envelope in place\n got: %+v\nwant: %+v", read, sent)
	}

	// params[1]: the unsigned query, as given.
	var gotOpts map[string]interface{}
	if err := json.Unmarshal(node.params[1], &gotOpts); err != nil || !reflect.DeepEqual(gotOpts, opts) {
		t.Fatalf("forwarded options = %s, want %v", node.params[1], opts)
	}

	// and the node's rows come back as the result.
	wantRows := []interface{}{map[string]interface{}{"id": float64(1), "name": "alice"}}
	if !reflect.DeepEqual(rows, wantRows) {
		t.Fatalf("rows = %v, want %v", rows, wantRows)
	}
}

// TestDdbSignedSelectSingleTableOmitsJoinTables checks that a single-table read (no joinTables) is
// forwarded without the key, the node's own omitempty form, and with a null query.
func TestDdbSignedSelectSingleTableOmitsJoinTables(t *testing.T) {
	br, node := newFakeDdbNode(t)
	read := clientSignedRead()
	read.JoinTables = nil

	if _, err := br.DdbSignedSelect(read, nil); err != nil {
		t.Fatalf("DdbSignedSelect: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(node.params[0], &got); err != nil {
		t.Fatalf("forwarded envelope is not a JSON object: %v", err)
	}
	if _, ok := got["joinTables"]; ok {
		t.Fatalf("single-table read forwarded a joinTables key: %s", node.params[0])
	}
	if len(node.params) != 2 || string(node.params[1]) != "null" {
		t.Fatalf("forwarded params = %s, want the envelope and a null query", node.params)
	}
}

// TestDdbSignedSelectRefusesUnsignedBeforeNetwork: an envelope with no signature or no public key is
// not a signed read, and the bridge must not dress it up as one or fall back to anything else. It is
// refused here, before any network call, so an unsigned request never costs the node a round trip.
func TestDdbSignedSelectRefusesUnsignedBeforeNetwork(t *testing.T) {
	cases := map[string]func(r *types.SignedDdbRead){
		"no signature":     func(r *types.SignedDdbRead) { r.CallerSig = "" },
		"empty signature":  func(r *types.SignedDdbRead) { r.CallerSig = "0x" },
		"no public key":    func(r *types.SignedDdbRead) { r.CallerPubKey = "" },
		"empty public key": func(r *types.SignedDdbRead) { r.CallerPubKey = "0x" },
		"neither":          func(r *types.SignedDdbRead) { r.CallerSig, r.CallerPubKey = "", "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			br, node := newFakeDdbNode(t)
			read := clientSignedRead()
			mutate(read)
			if _, err := br.DdbSignedSelect(read, nil); err == nil {
				t.Fatal("unsigned read was accepted")
			}
			if node.calls != 0 {
				t.Fatalf("unsigned read reached the node (%d calls)", node.calls)
			}
		})
	}

	t.Run("nil envelope", func(t *testing.T) {
		br, node := newFakeDdbNode(t)
		if _, err := br.DdbSignedSelect(nil, nil); err == nil {
			t.Fatal("nil envelope was accepted")
		}
		if node.calls != 0 {
			t.Fatalf("nil envelope reached the node (%d calls)", node.calls)
		}
	})
}
