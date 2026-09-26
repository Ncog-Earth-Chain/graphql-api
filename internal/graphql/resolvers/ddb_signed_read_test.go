package resolvers

import (
	"reflect"
	"testing"

	"ncogearthchain-api-graphql/internal/types"
)

// signedReadInput is a GraphQL SignedDdbReadInput as a client would send it.
func signedReadInput(joins *[]string) SignedDdbReadInput {
	return SignedDdbReadInput{
		SchemaName:   "medical_records",
		TableName:    "patients",
		JoinTables:   joins,
		From:         "0xAbCdEf0123456789aBcDeF0123456789AbCdEf01",
		Timestamp:    "0x6553f100",
		Nonce:        "0x1234abcd5678ef90",
		CallerPubKey: "0x0a0b0c0d0e0f",
		CallerSig:    "0xdeadbeefcafebabe",
	}
}

// TestSignedReadInputEnvelopeIsVerbatim: the GraphQL input becomes the forwarded envelope field for
// field, with nothing filled in, rewritten or dropped. Every one of these fields is inside the reader's
// signature, so any change here is a read the node will refuse, or a read as somebody else.
func TestSignedReadInputEnvelopeIsVerbatim(t *testing.T) {
	joins := []string{"doctors", "visits"}
	got := signedReadInput(&joins).envelope()
	want := &types.SignedDdbRead{
		SchemaName:   "medical_records",
		TableName:    "patients",
		JoinTables:   []string{"doctors", "visits"},
		From:         "0xAbCdEf0123456789aBcDeF0123456789AbCdEf01",
		Timestamp:    "0x6553f100",
		Nonce:        "0x1234abcd5678ef90",
		CallerPubKey: "0x0a0b0c0d0e0f",
		CallerSig:    "0xdeadbeefcafebabe",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("envelope = %+v, want %+v", got, want)
	}

	// a single-table read carries no join set at all
	if env := signedReadInput(nil).envelope(); env.JoinTables != nil {
		t.Fatalf("single-table read gained a join set: %v", env.JoinTables)
	}
}
