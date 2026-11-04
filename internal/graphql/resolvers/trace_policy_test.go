package resolvers

import (
	"reflect"
	"testing"

	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// The raw trace fix must preserve the already reviewed, typed internal-call
// view: restricted inputs are blanked, while public metadata remains available.
func TestRawTracePolicyPreservesFilteredInternalView(t *testing.T) {
	for _, restricted := range []bool{false, true} {
		name := "public"
		if restricted {
			name = "restricted"
		}
		t.Run(name, func(t *testing.T) {
			input := hexutil.Bytes("DDBRsynthetic-internal-input")
			trx := NewTransaction(&types.Transaction{InputData: input})
			if restricted {
				trx.Transaction.InputData = nil
				trx.Transaction.InputRedacted = &types.DdbWithheld{Reason: types.DdbWithheldReaderRole}
			}
			to := common.HexToAddress("0x1234")
			frame := types.InternalTransaction{From: common.HexToAddress("0x5678"), To: &to,
				Input: input, Gas: 123, Type: "CALL"}
			frames := []*InternalTransaction{NewInternalTransaction(&frame)}
			want := frame
			if restricted {
				want.Input = nil
			}
			trx.redactInternalInputs(frames)
			if !reflect.DeepEqual(frames[0].InternalTransaction, want) {
				t.Fatalf("filtered internal metadata changed: got=%+v want=%+v", frames[0].InternalTransaction, want)
			}
		})
	}
}
