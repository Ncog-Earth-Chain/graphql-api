package rpc

import (
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

// ddbCommitGoldenVectorHex is a real "DDBR" commit-tx payload produced by the NODE's
// encoder, captured from:
//
//	ncogearthchain$ go test ./gossip/ddb/ -run TestEmitCommitTxGoldenVector -v
//
// This is the only test here that can actually detect struct drift. The round-trip
// tests below encode with the same mirror structs they decode with, so they agree with
// themselves no matter how far they have drifted from the node -- which is exactly how
// the missing `Epoch` field went unnoticed while every DDB commit tx on chain failed to
// decode.
//
// If this vector stops decoding, the node's endorsementRLP/proofRLP changed. Re-run the
// generator above and reconcile the mirror structs; do not simply re-capture the bytes.
const ddbCommitGoldenVectorHex = "44444252f90273f9011fa01111111111111111111111111111111111111111111111111111111111111111a02222222222222222222222222222222222222222222222222222222222222222f842e094aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa84deadbeef846553f10001e094bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb84feedface846553f10101ea94aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa94bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb83067932846553f102a0333333333333333333333333333333333333333333333333333333333333333394cccccccccccccccccccccccccccccccccccccccc8c676f6c64656e2d74782d6964a0444444444444444444444444444444444444444444444444444444444444444407b9013f7b2274797065223a302c22736368656d615f6e616d65223a22676f6c64656e5f736368656d61222c226761735f6c696d6974223a302c226761735f7072696365223a6e756c6c2c2266726f6d223a22307830303030303030303030303030303030303030303030303030303030303030303030303030303030222c226e6f6e6365223a312c2274696d657374616d70223a302c2263726561746f72223a22307830303030303030303030303030303030303030303030303030303030303030303030303030303030222c22617574686f72223a22307830303030303030303030303030303030303030303030303030303030303030303030303030303030222c22636f6e747261637441646472657373223a22307864646464646464646464646464646464646464646464646464646464646464646464646464646464227d846553f10384010203048405060708"

// TestDecodeDdbCommitTxData_NodeGoldenVector decodes a payload built by the node's own
// encoder. This is the drift guard.
//
// Regression: the explorer's ddbEndorsementRLP was missing the node's 11th field,
// `Epoch`. RLP is positional, so the epoch byte was left unconsumed and EVERY DDB commit
// transaction failed to decode. The error was swallowed by extractDDBContractAddress,
// leaving ContractAddress nil, which gates contract indexing -- so `isDDB` stayed false
// forever and data contracts were invisible in the explorer.
func TestDecodeDdbCommitTxData_NodeGoldenVector(t *testing.T) {
	data, err := hex.DecodeString(ddbCommitGoldenVectorHex)
	if err != nil {
		t.Fatalf("bad golden vector hex: %v", err)
	}

	info, err := decodeDdbCommitTxData(data)
	if err != nil {
		t.Fatalf("decode node-produced payload: %v\n"+
			"the explorer's mirror structs have drifted from gossip/ddb/commit_tx_rlp.go", err)
	}

	if want := common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333"); info.RequestID != want {
		t.Errorf("requestID = %s, want %s", info.RequestID, want)
	}
	if want := common.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc"); info.Requester != want {
		t.Errorf("requester = %s, want %s", info.Requester, want)
	}
	if info.BlockNumber != 424242 {
		t.Errorf("blockNumber = %d, want 424242", info.BlockNumber)
	}
	if len(info.ValidatorSet) != 2 || info.Signatures != 2 {
		t.Errorf("validatorSet = %d, signatures = %d, want 2 / 2", len(info.ValidatorSet), info.Signatures)
	}

	// the operation must survive as usable JSON, since that is what the explorer displays
	var op map[string]interface{}
	if err := json.Unmarshal(info.Operation, &op); err != nil {
		t.Fatalf("operation JSON is not parseable: %v", err)
	}
	if op["schema_name"] != "golden_schema" {
		t.Errorf("schema_name = %v, want golden_schema", op["schema_name"])
	}

	// and the data-contract address must be recoverable, since a nil address is what
	// silently disabled DDB contract indexing
	if got, want := ddbContractAddressFromOperation(info.Operation),
		"0xdddddddddddddddddddddddddddddddddddddddd"; got != want {
		t.Errorf("contract address = %q, want %q", got, want)
	}
}

// TestDdbEndorsementRLPFieldCount pins the field count of the mirror struct.
//
// RLP is positional and carries no field names, so a decoder with the wrong arity fails
// at runtime with no compile-time signal. Adding a field to the node without adding it
// here should break a test, not production.
func TestDdbEndorsementRLPFieldCount(t *testing.T) {
	// node gossip/ddb/commit_tx_rlp.go endorsementRLP: 11 fields
	const nodeEndorsementFields = 11
	// node proofRLP: 5 fields
	const nodeProofFields = 5
	// node sigRLP: 4 fields
	const nodeSigFields = 4

	if got := reflectFieldCount(ddbEndorsementRLP{}); got != nodeEndorsementFields {
		t.Errorf("ddbEndorsementRLP has %d fields, node endorsementRLP has %d", got, nodeEndorsementFields)
	}
	if got := reflectFieldCount(ddbProofRLP{}); got != nodeProofFields {
		t.Errorf("ddbProofRLP has %d fields, node proofRLP has %d", got, nodeProofFields)
	}
	if got := reflectFieldCount(ddbSigRLP{}); got != nodeSigFields {
		t.Errorf("ddbSigRLP has %d fields, node sigRLP has %d", got, nodeSigFields)
	}
}

// reflectFieldCount returns the number of fields on a struct value.
func reflectFieldCount(v interface{}) int {
	return reflect.TypeOf(v).NumField()
}

// TestGoldenVectorIsArirySensitive proves the golden vector can actually detect the
// class of bug it exists to catch.
//
// A golden vector is only a drift guard if a wrong-arity decoder fails on it. This
// decodes the same node-produced bytes into a struct with the pre-fix 10-field
// endorsement and asserts it FAILS. Without this, a future change that made decoding
// silently lenient would leave the guard above passing while it no longer guards
// anything.
func TestGoldenVectorIsAritySensitive(t *testing.T) {
	// the endorsement exactly as the explorer had it before the fix: no Epoch
	type endorsementWithoutEpoch struct {
		OperationHash common.Hash
		DataHash      common.Hash
		Signatures    []ddbSigRLP
		ValidatorSet  []common.Address
		BlockNumber   uint64
		Timestamp     uint64
		RequestID     common.Hash
		Requester     common.Address
		TxID          string
		StateHash     common.Hash
	}
	type proofWithoutEpoch struct {
		Endorsement        endorsementWithoutEpoch
		OperationJSON      []byte
		Timestamp          uint64
		PriorPostStateHash []byte
		PostStateHash      []byte
	}

	data, err := hex.DecodeString(ddbCommitGoldenVectorHex)
	if err != nil {
		t.Fatalf("bad golden vector hex: %v", err)
	}

	var stale proofWithoutEpoch
	err = rlp.DecodeBytes(data[len(ddbCommitPrefixRLP):], &stale)
	if err == nil {
		t.Fatal("the pre-fix 10-field struct decoded a node payload successfully -- " +
			"the golden vector is no longer arity-sensitive and cannot detect struct drift")
	}
	t.Logf("pre-fix struct correctly rejected the node payload: %v", err)
}

// TestDecodeDdbCommitTxData_RLP round-trips a DDBR proof through the explorer's mirror structs and asserts
// the decoded operation + endorsement summary + extracted data-contract address. The struct field ORDER
// here must match the node's gossip/ddb/commit_tx_rlp.go (RLP is positional); this test guards against drift.
func TestDecodeDdbCommitTxData_RLP(t *testing.T) {
	// The top-level "contractAddress" is ALWAYS serialized by the node as the zero address (it is a
	// common.Address [20]byte array, which encoding/json omitempty never omits), so the real data-contract
	// address lives in data.contract_address. Include the zero top-level field here so the test exercises
	// the zero-guard fall-through — with the pre-fix short-circuit this would (wrongly) resolve to 0x0.
	opJSON := []byte(`{"type":0,"schema_name":"users","contractAddress":"0x0000000000000000000000000000000000000000","data":{"contract_address":"0x00000000000000000000000000000000000abcde","contract_name":"users"},"from":"0x000000000000000000000000000000000000000f"}`)
	pr := ddbProofRLP{
		Endorsement: ddbEndorsementRLP{
			OperationHash: common.HexToHash("0xa1"),
			DataHash:      common.HexToHash("0xb2"),
			Signatures: []ddbSigRLP{
				{Validator: common.HexToAddress("0x01"), Signature: []byte{1, 2, 3}, Approved: true},
				{Validator: common.HexToAddress("0x02"), Signature: []byte{4, 5, 6}, Approved: true},
			},
			ValidatorSet: []common.Address{common.HexToAddress("0x01"), common.HexToAddress("0x02")},
			BlockNumber:  7,
			RequestID:    common.HexToHash("0xc3"),
			Requester:    common.HexToAddress("0x0f"),
			TxID:         "0xc3",
		},
		OperationJSON: opJSON,
		Timestamp:     1,
	}
	body, err := rlp.EncodeToBytes(&pr)
	if err != nil {
		t.Fatal(err)
	}
	data := append([]byte(ddbCommitPrefixRLP), body...)

	info, err := decodeDdbCommitTxData(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(info.Operation) != string(opJSON) {
		t.Fatalf("operation JSON drift:\n got %s\nwant %s", info.Operation, opJSON)
	}
	if info.RequestID != common.HexToHash("0xc3") || info.Requester != common.HexToAddress("0x0f") || info.BlockNumber != 7 {
		t.Fatalf("endorsement summary drift: %+v", info)
	}
	if len(info.ValidatorSet) != 2 || info.Signatures != 2 {
		t.Fatalf("validatorSet/signatures drift: %d / %d", len(info.ValidatorSet), info.Signatures)
	}
	if got := ddbContractAddressFromOperation(info.Operation); got != "0x00000000000000000000000000000000000abcde" {
		t.Fatalf("contract address from data.contract_address = %q", got)
	}
}

// TestDecodeDdbCommitTxData_LegacyJSON covers the legacy DDBE+JSON payload path.
func TestDecodeDdbCommitTxData_LegacyJSON(t *testing.T) {
	data := []byte(`DDBE{"endorsement":{"requestId":"0x00000000000000000000000000000000000000000000000000000000000000c3","requester":"0x000000000000000000000000000000000000000f","blockNumber":9,"validatorSet":["0x0000000000000000000000000000000000000001"],"signatures":[{}]},"operation":{"contractAddress":"0x00000000000000000000000000000000000abcde"}}`)
	info, err := decodeDdbCommitTxData(data)
	if err != nil {
		t.Fatalf("decode legacy: %v", err)
	}
	if info.BlockNumber != 9 || info.Signatures != 1 {
		t.Fatalf("legacy summary drift: %+v", info)
	}
	if got := ddbContractAddressFromOperation(info.Operation); got != "0x00000000000000000000000000000000000abcde" {
		t.Fatalf("legacy contract address (enriched contractAddress field) = %q", got)
	}
}

// TestDdbContractAddressFromOperation_ZeroGuard covers the zero-address guard: the node always emits a
// top-level "contractAddress" (zero when unset), so it must be ignored in favour of data.contract_address,
// and a genuinely-absent address must resolve to "" (so the tx keeps a nil ContractAddress, not 0x0).
func TestDdbContractAddressFromOperation_ZeroGuard(t *testing.T) {
	zero := "0x0000000000000000000000000000000000000000"
	real := "0x00000000000000000000000000000000000abcde"
	cases := []struct {
		name string
		op   string
		want string
	}{
		{"zero top-level falls through to data.contract_address", `{"contractAddress":"` + zero + `","data":{"contract_address":"` + real + `"}}`, real},
		{"zero everywhere resolves to empty", `{"contractAddress":"` + zero + `","data":{"contract_address":"` + zero + `"}}`, ""},
		{"absent everywhere resolves to empty", `{"schema_name":"users"}`, ""},
		{"real top-level is used", `{"contractAddress":"` + real + `"}`, real},
	}
	for _, c := range cases {
		if got := ddbContractAddressFromOperation([]byte(c.op)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// TestDecodeDdbCommitTxData_Bad rejects non-DDB payloads.
func TestDecodeDdbCommitTxData_Bad(t *testing.T) {
	if _, err := decodeDdbCommitTxData([]byte("XXXXsomething")); err == nil {
		t.Fatal("expected error for a bad prefix")
	}
	if _, err := decodeDdbCommitTxData([]byte("DD")); err == nil {
		t.Fatal("expected error for too-short data")
	}
}
