package types

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// SEC-07c: the contract a committed DDB operation belongs to, and whether the node's refusal to
// serve its calldata is a settled verdict or a transient one.
//
// Both are mirrors of the node, and they are pinned here because getting either wrong is a leak:
// keying a deploy on its HUMAN name finds no contract and reads as public (node:
// gossip/ddb/read_policy_target.go), and treating `undecidable` as settled writes "withheld
// forever" over a public contract whose block the node simply had not applied yet.

func TestDdbOperationTargetKeysSchemaOpsOnTheDerivedName(t *testing.T) {
	// A deploy and an upgrade carry the contract definition in `data`, and their schema_name is the
	// HUMAN contract name. The target is DeriveDbName(data.contract_address), whatever schema_name says.
	const addr = "0x00000000000000000000000000000000000000a1"
	op := []byte(`{"type":0,"schema_name":"payroll","data":{"contract_address":"` + addr + `"}}`)

	for _, opType := range []int16{DdbOpCreateSchema, DdbOpUpdateSchema} {
		got, err := DdbOperationTarget(opType, "payroll", op)
		if err != nil {
			t.Fatalf("op type %d: %v", opType, err)
		}
		if want := "c_00000000000000000000000000000000000000a1"; got != want {
			t.Errorf("op type %d target = %q, want %q (the human name must never be the key)", opType, got, want)
		}
	}
}

func TestDdbOperationTargetKeysEveryOtherOpOnItsSchemaName(t *testing.T) {
	// A call, a grant, a revoke and a delete name the db_name directly in schema_name.
	const schema = "c_00000000000000000000000000000000000000a1"
	for _, opType := range []int16{DdbOpCallProcedure, DdbOpGrantRole, DdbOpRevokeRole, DdbOpDeleteSchema} {
		got, err := DdbOperationTarget(opType, schema, []byte(`{"type":7,"schema_name":"`+schema+`"}`))
		if err != nil {
			t.Fatalf("op type %d: %v", opType, err)
		}
		if got != schema {
			t.Errorf("op type %d target = %q, want %q", opType, got, schema)
		}
	}
}

func TestDdbOperationTargetRefusesAnUnresolvableTarget(t *testing.T) {
	cases := []struct {
		label  string
		opType int16
		schema string
		op     []byte
	}{
		{"deploy with no derivable address", DdbOpCreateSchema, "payroll", []byte(`{"data":{"contract_address":""}}`)},
		{"deploy with a non-hex address", DdbOpCreateSchema, "payroll", []byte(`{"data":{"contract_address":"not-an-address"}}`)},
		{"deploy whose definition does not parse", DdbOpCreateSchema, "payroll", []byte(`{"data":"-"}`)},
		{"deploy with no operation JSON at all", DdbOpCreateSchema, "payroll", nil},
		{"call naming no schema", DdbOpCallProcedure, "", []byte(`{"type":7}`)},
	}
	for _, c := range cases {
		if got, err := DdbOperationTarget(c.opType, c.schema, c.op); err == nil {
			t.Errorf("%s: target %q with no error; an operation whose contract cannot be named must "+
				"not be shown to be public", c.label, got)
		}
	}
}

func TestDdbWithholdIsFinalOnlyForASettledVerdict(t *testing.T) {
	for _, r := range []string{DdbWithheldReaderRole, DdbWithheldTombstoned, DdbWithheldUndecodable} {
		if !DdbWithholdIsFinal(r) {
			t.Errorf("reason %q reads as transient; it is a verdict on the contract as the node holds it", r)
		}
	}
	// `undecidable` -- and any reason a later node version introduces -- must never be persisted as
	// final: the safe direction for "I do not know this reason" is "ask again".
	for _, r := range []string{DdbWithheldUndecidable, "", "some-future-reason"} {
		if DdbWithholdIsFinal(r) {
			t.Errorf("reason %q reads as final; it must be re-fetched", r)
		}
	}
}

func TestDdbDeriveDbNameMirrorsTheNode(t *testing.T) {
	cases := map[string]string{
		"0x00000000000000000000000000000000000000A1": "c_00000000000000000000000000000000000000a1",
		"  00000000000000000000000000000000000000a2": "c_00000000000000000000000000000000000000a2",
		"":     "",
		"0x":   "",
		"0xzz": "",
		"0x00000000000000000000000000000000000000a": "", // 39 nibbles is not an address
	}
	for in, want := range cases {
		if got := DdbDeriveDbName(in); got != want {
			t.Errorf("DdbDeriveDbName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDdbIndexedTargetKeysSchemaOpsOnTheDerivedName(t *testing.T) {
	// The index row of a deploy or an upgrade holds the HUMAN name in schema_name and the contract
	// address in its own column, so the target comes from the address.
	addr := common.HexToAddress("0x00000000000000000000000000000000000000a1")
	for _, opType := range []int16{DdbOpCreateSchema, DdbOpUpdateSchema} {
		got, err := DdbIndexedTarget(opType, "payroll", &addr)
		if err != nil {
			t.Fatalf("op type %d: %v", opType, err)
		}
		if want := "c_00000000000000000000000000000000000000a1"; got != want {
			t.Errorf("op type %d target = %q, want %q", opType, got, want)
		}
	}
	// A deploy row with no contract address cannot be bound to a verdict at all.
	if got, err := DdbIndexedTarget(DdbOpCreateSchema, "payroll", nil); err == nil {
		t.Errorf("deploy row with no contract address resolved to %q; it must be withheld", got)
	}
	// Everything else is keyed on the schema it names, and a row naming none is unresolvable.
	got, err := DdbIndexedTarget(DdbOpCallProcedure, "c_00000000000000000000000000000000000000a2", nil)
	if err != nil || got != "c_00000000000000000000000000000000000000a2" {
		t.Errorf("call row target = %q, %v; want the schema it names", got, err)
	}
	if _, err := DdbIndexedTarget(DdbOpCallProcedure, "", &addr); err == nil {
		t.Error("a call row naming no schema resolved; it must be withheld")
	}
}

func TestDdbOperationTargetFromJSONReadsTheOperationsOwnType(t *testing.T) {
	// The node's served calldata states its own type, so the target is resolved from the bytes alone.
	deploy := []byte(`{"type":0,"schema_name":"payroll","data":{"contract_address":"0x00000000000000000000000000000000000000a1"}}`)
	if got, err := DdbOperationTargetFromJSON(deploy); err != nil || got != "c_00000000000000000000000000000000000000a1" {
		t.Errorf("deploy target = %q, %v; want the derived name", got, err)
	}
	call := []byte(`{"type":7,"schema_name":"c_00000000000000000000000000000000000000a2"}`)
	if got, err := DdbOperationTargetFromJSON(call); err != nil || got != "c_00000000000000000000000000000000000000a2" {
		t.Errorf("call target = %q, %v; want the schema it names", got, err)
	}
	if _, err := DdbOperationTargetFromJSON([]byte(`not json`)); err == nil {
		t.Error("unparseable operation JSON resolved a target")
	}
}

func TestIsDdbCommitTxDataMatchesOnPrefixAlone(t *testing.T) {
	for _, in := range [][]byte{[]byte("DDBR\x01"), []byte("DDBE{"), []byte("DDBR")} {
		if !IsDdbCommitTxData(in) {
			t.Errorf("%q not recognised as a DDB commit", in)
		}
	}
	for _, in := range [][]byte{nil, []byte("DDB"), []byte("0xdeadbeef"), []byte("ddbr....")} {
		if IsDdbCommitTxData(in) {
			t.Errorf("%q recognised as a DDB commit", in)
		}
	}
}
