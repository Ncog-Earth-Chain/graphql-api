package rpc

// ddb_verdict.go -- SEC-07c: ask the NODE whether a DDB commit's calldata may be served.
//
// The explorer has no business deciding this. The read policy is one predicate on the node --
// ddb_select, ddb_signedSelect, ddb_callReadProcedure and every chain-history formatter reach the same
// anonymousReadPolicy (ethapi/ddb_calldata_redaction.go) -- and the whole shape of SEC-07 was two
// surfaces that were "individually correct and jointly wrong" because one of them never asked. A
// second copy of the rule in this repository would be a third.
//
// So the verdict is fetched, per commit transaction, from the one place that already states it:
// eth_getTransactionByHash serves `input` empty with an `inputRedacted` descriptor
// {reason, schema, size, keccak256} when it withholds, and the bytes verbatim when it does not. That
// one answer carries all three parts of the rule the explorer cannot reconstruct:
//
//   - the verdict is keyed on the schema the operation's contract_address DERIVES for a deploy or an
//     upgrade, not on the human name its schema_name carries (ddb.ReadPolicyTarget);
//   - a deploy or an upgrade is withheld when the definition it CARRIES declares a `reader` role, even
//     though the registry has no row for it yet;
//   - a mined commit is judged only at or below the node's contiguous applied frontier
//     (DdbStorage.AppliedThrough), because an upgrade adding a reader role in a block the node has not
//     applied leaves the registry saying "public".
//
// The frontier in particular is not reachable any other way: no RPC method on the node returns it.
// A per-schema verdict resolved locally could therefore not implement the third rule at all, which is
// the reason this is per transaction rather than per contract.
//
// FAILS CLOSED, per transaction rather than per call. A hash the node answers with a null, or declines
// with an element error, becomes `undecidable` for that transaction alone -- withheld, and transient,
// so a node that is catching up or whose Postgres is down does not freeze the explorer's view of a
// public contract. This is deliberately NOT the ingest loader's "any element fails the whole call"
// rule (Transactions, transaction.go): there a partial answer must not be recorded as a whole block,
// while here a partial answer must not withhold more than it has to.

import (
	"context"
	"fmt"

	"ncogearthchain-api-graphql/internal/logger"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	nec2 "github.com/ethereum/go-ethereum/rpc"
)

// ddbVerdictBatchChunk bounds how many verdict requests go into one JSON-RPC batch, for the same
// reason txBatchChunk does: go-ethereum's server caps both the request count and the response size of
// a batch and advertises neither.
const ddbVerdictBatchChunk = 128

// ddbVerdictTx is the slice of eth_getTransactionByHash this needs: the hash, so a null answer is
// recognisable, the calldata, and the descriptor the node puts in its place.
type ddbVerdictTx struct {
	Hash          common.Hash        `json:"hash"`
	Input         hexutil.Bytes      `json:"input"`
	InputRedacted *types.DdbWithheld `json:"inputRedacted,omitempty"`
}

// DdbCommitVerdicts asks the node, in batched calls, whether each of these commit transactions' calldata
// may be served to an anonymous caller.
//
// Every hash asked for is present in the result. A hash the node cannot answer maps to an `undecidable`
// withhold, never to a missing entry, so a caller cannot read "absent" as "allowed".
func (nec *NecBridge) DdbCommitVerdicts(ctx context.Context, hashes []common.Hash) (map[common.Hash]types.DdbCommitVerdict, error) {
	out := make(map[common.Hash]types.DdbCommitVerdict, len(hashes))
	if len(hashes) == 0 {
		return out, nil
	}

	for start := 0; start < len(hashes); start += ddbVerdictBatchChunk {
		end := start + ddbVerdictBatchChunk
		if end > len(hashes) {
			end = len(hashes)
		}
		chunk := hashes[start:end]

		txs := make([]ddbVerdictTx, len(chunk))
		batch := make([]nec2.BatchElem, len(chunk))
		for i := range chunk {
			batch[i] = nec2.BatchElem{
				Method: "eth_getTransactionByHash",
				Args:   []interface{}{chunk[i]},
				Result: &txs[i],
			}
		}
		if err := nec.rpc.BatchCallContext(ctx, batch); err != nil {
			// The whole batch failed -- the node is unreachable, not declining. Report it: the caller
			// withholds every transaction it was asking about, and a transport failure that silently
			// read as "undecidable" would hide a broken node behind an explanation about block height.
			return nil, fmt.Errorf("DDB calldata verdict batch failed: %w", err)
		}

		for i := range chunk {
			out[chunk[i]] = ddbVerdictOf(&batch[i], &txs[i], chunk[i])
		}
	}
	return out, nil
}

// ddbVerdictOf reads one batch element's answer.
func ddbVerdictOf(elem *nec2.BatchElem, tx *ddbVerdictTx, hash common.Hash) types.DdbCommitVerdict {
	// An element error is the node declining ONE transaction. The raw forms refuse with an error rather
	// than a descriptor (a raw transaction cannot be partially served), and that refusal can carry any
	// of the four reasons -- including `undecidable` -- so the text is not something to classify. It is
	// withheld, transiently.
	if elem.Error != nil {
		return types.DdbUndecidableVerdict(0, common.Hash{}, "")
	}
	// A null result leaves the target zero-valued: the node does not have this transaction (no tx
	// index, pruned history, a different chain). Nothing about it can be determined.
	if tx.Hash != hash {
		return types.DdbUndecidableVerdict(0, common.Hash{}, "")
	}
	if tx.InputRedacted != nil {
		return types.DdbCommitVerdict{Withheld: tx.InputRedacted}
	}
	return types.DdbCommitVerdict{Input: tx.Input}
}

// NewDdbVerdictBridge is a bridge that can do nothing but DdbCommitVerdicts and Transaction over an
// existing client. It exists for tests that stand a node up in process (rpc.DialInProc) to answer as
// the node's redaction walk does; production code builds its bridge with New.
func NewDdbVerdictBridge(c *nec2.Client, log logger.Logger) *NecBridge {
	return &NecBridge{rpc: c, log: log}
}
