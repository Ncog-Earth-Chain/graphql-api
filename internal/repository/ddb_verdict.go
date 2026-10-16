package repository

// ddb_verdict.go -- SEC-07c: every field that carries DDB commit calldata passes through the node's
// read verdict on its way out of this repository.
//
// There are three such fields, and all three served the same bytes: `DdbOperation.payload` (also
// reached through `ddbContract.operations` and `Transaction.ddb`), `Transaction.inputData`, and
// `InternalTransaction.input` for the top-level frame of a trace. The first two are served from the
// local INDEX, which is what makes re-filtering on READ the requirement rather than filtering at
// ingest: rows captured before SEC-07 landed on the node -- or by a node whose DDB was off, which
// served everything -- are already in the index, and an ingest-time filter cannot reach back for them.
//
// WHAT IS ASKED, AND OF WHOM. The verdict is the node's, per commit transaction
// (rpc/ddb_verdict.go). Nothing here reimplements the read policy: the explorer cannot see a
// contract's `reader` role, cannot see a tombstone, and above all cannot see the node's contiguous
// applied frontier -- no RPC method exposes it -- so a locally computed verdict would be wrong in
// exactly the direction that leaks.
//
// WHAT IS DECIDED HERE is the binding: does the verdict the node gave belong to the row being served?
// The explorer's index is keyed on (block_number, tx_index) and carries the tx_hash; a reorg this
// explorer has not yet purged, a gap heal, or an index built against a different chain can leave a row
// whose tx_hash names a transaction the node now reports carrying a DIFFERENT operation. Releasing a
// row's payload on the strength of a verdict about some other operation is the same leak by another
// route, so the row's contract and the authorized operation's contract must be the same contract --
// and "the same contract" is resolved the node's way, DeriveDbName(contract_address) for a deploy or
// an upgrade rather than the human name their schema_name carries (types.DdbIndexedTarget).
//
// FAIL CLOSED, every exit. A verdict that cannot be fetched, a row whose contract cannot be named, a
// node that authorized bytes carrying a different contract's operation: all withheld, all as
// `undecidable`, which is TRANSIENT -- the next read asks again, and the commit is served in full once
// the node can vouch for it.

import (
	"context"
	"sync"
	"time"

	"ncogearthchain-api-graphql/internal/repository/rpc"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

// ddbVerdictTTL is how long one commit's verdict is reused across requests.
//
// Bounded amplification, for the same reason the node bounds its own: a verdict costs the node three
// Postgres round trips (the applied watermark, then the tombstone and registry lookups) and this
// explorer's read path is anonymous and uncapped, so a caller hammering one transaction hash would
// otherwise drive them on every request. One second is also what the node's own verdict cache uses, so
// a grant, a revoke or a tombstone is honoured within two seconds end to end -- far inside the
// endorsement-to-finality window a change takes to become visible anyway.
//
// The TTL applies to a SERVED verdict as much as to a withheld one. That is the cost of the bound: for
// up to a second after a reader role is added, a payload the node has just started refusing can still
// be served from here. The node accepts the same second for the same reason.
const ddbVerdictTTL = time.Second

// ddbVerdictCacheMax caps the cache, which is keyed by transaction hash and therefore by a key space
// anyone can mint -- any transaction whose calldata starts with a DDB commit prefix gets an entry.
// Over the cap, entries are EVICTED rather than the map dropped: dropping it wholesale would let a
// caller who lands enough such transactions flush every live verdict and force a re-query of the real
// ones, which turns the cap into an amplification lever on the path it exists to bound.
const ddbVerdictCacheMax = 4096

// ddbVerdictCache holds recent verdicts, keyed by commit transaction hash.
//
// Keyed by TRANSACTION, not by schema, and that is not an optimisation left on the table: two commits
// on one contract can have different verdicts in the same instant -- an upgrade that adds a `reader`
// role is withheld for the definition it carries while a call on the same contract in the same block
// is still served, and a commit above the node's applied frontier is withheld while an older one is
// not. A per-schema entry would have to answer for all of them.
type ddbVerdictCache struct {
	sync.Mutex
	entries map[common.Hash]ddbVerdictEntry
}

type ddbVerdictEntry struct {
	verdict types.DdbCommitVerdict
	expires time.Time
}

func newDdbVerdictCache() *ddbVerdictCache {
	return &ddbVerdictCache{entries: make(map[common.Hash]ddbVerdictEntry)}
}

// get returns a live verdict, if there is one.
func (c *ddbVerdictCache) get(hash common.Hash, now time.Time) (types.DdbCommitVerdict, bool) {
	if c == nil {
		return types.DdbCommitVerdict{}, false
	}
	c.Lock()
	defer c.Unlock()
	e, ok := c.entries[hash]
	if !ok || !now.Before(e.expires) {
		return types.DdbCommitVerdict{}, false
	}
	return e.verdict, true
}

// put stores a verdict, evicting to stay under the cap.
func (c *ddbVerdictCache) put(hash common.Hash, v types.DdbCommitVerdict, now time.Time) {
	if c == nil {
		return
	}
	c.Lock()
	defer c.Unlock()
	if len(c.entries) >= ddbVerdictCacheMax {
		// Expired entries first -- they are dead weight. If the map is still at the cap, entries go
		// one at a time until it is not; Go randomizes map iteration, so which live verdict goes is
		// not something a caller can steer, and exactly one is lost per insertion past the cap.
		for k, e := range c.entries {
			if !now.Before(e.expires) {
				delete(c.entries, k)
			}
		}
		for k := range c.entries {
			if len(c.entries) < ddbVerdictCacheMax {
				break
			}
			delete(c.entries, k)
		}
	}
	c.entries[hash] = ddbVerdictEntry{verdict: v, expires: now.Add(ddbVerdictTTL)}
}

// ddbCommitVerdicts resolves the node's read verdict for a set of commit transactions, through the
// short-lived cache in front of one batched node call.
//
// Every hash asked for is present in the result: a hash the node could not be asked about maps to an
// `undecidable` withhold, so no caller can read "absent" as "allowed".
func (p *proxy) ddbCommitVerdicts(ctx context.Context, hashes []common.Hash) map[common.Hash]types.DdbCommitVerdict {
	out := make(map[common.Hash]types.DdbCommitVerdict, len(hashes))
	if len(hashes) == 0 {
		return out
	}

	now := time.Now()
	miss := make([]common.Hash, 0, len(hashes))
	for _, h := range hashes {
		if _, done := out[h]; done {
			continue
		}
		if v, ok := p.ddbVerdicts.get(h, now); ok {
			out[h] = v
			continue
		}
		// Seeded withheld, so every early return below leaves the caller with a refusal rather than
		// an absent entry.
		out[h] = types.DdbUndecidableVerdict(0, common.Hash{}, "")
		miss = append(miss, h)
	}
	if len(miss) == 0 {
		return out
	}

	fetched, err := p.rpc.DdbCommitVerdicts(ctx, miss)
	if err != nil {
		// The node is unreachable. Every miss keeps the fail-closed `undecidable` it was seeded with,
		// and nothing is cached: a transport failure is not a verdict and must not be remembered as
		// one, not even for a second.
		p.log.Errorf("can not read DDB calldata verdicts from the node; withholding %d commit payloads; %s",
			len(miss), err.Error())
		return out
	}
	for _, h := range miss {
		v, ok := fetched[h]
		if !ok {
			// The bridge promises an entry per hash; if that ever stops being true, the missing one
			// stays withheld rather than served.
			continue
		}
		out[h] = v
		p.ddbVerdicts.put(h, v, now)
	}
	return out
}

// ddbFilterOperations re-resolves the read verdict for every operation about to be served and empties
// the payload of any the node will not vouch for. In place: the rows belong to this request.
func (p *proxy) ddbFilterOperations(ctx context.Context, ops []*types.DdbOperation) {
	if len(ops) == 0 {
		return
	}

	hashes := make([]common.Hash, 0, len(ops))
	for _, op := range ops {
		if op != nil {
			hashes = append(hashes, op.TxHash)
		}
	}
	verdicts := p.ddbCommitVerdicts(ctx, hashes)

	for _, op := range ops {
		if op == nil {
			continue
		}
		p.ddbFilterOperation(op, verdicts[op.TxHash])
	}
}

// ddbFilterOperation applies one verdict to one row.
func (p *proxy) ddbFilterOperation(op *types.DdbOperation, v types.DdbCommitVerdict) {
	// Which contract does this ROW claim? Resolved the node's way: the derived schema for a deploy or
	// an upgrade, whose schema_name column holds the human contract name, and schema_name for
	// everything else. A row whose contract cannot be named cannot be shown to be public.
	target, terr := types.DdbIndexedTarget(op.OpType, op.SchemaName, op.ContractAddress)
	if terr != nil {
		p.ddbWithholdOperation(op, &types.DdbWithheld{Reason: types.DdbWithheldUndecidable})
		return
	}

	if w := v.Withheld; w != nil {
		d := *w
		if d.Schema == "" {
			// The node named no schema (an undecodable commit, or a raw-form refusal). Report the row's
			// own target so an operator can still tell which contract was withheld.
			d.Schema = target
		}
		p.ddbWithholdOperation(op, &d)
		return
	}

	// The node served bytes. They must be the bytes of THIS row's operation, or the verdict is about
	// something else -- see the binding note at the top of this file.
	operation, served, err := ddbServedOperation(v.Input)
	if err != nil || served != target {
		p.log.Warningf("DDB operation at %d/%d (tx %s) claims contract %q but the calldata the node "+
			"serves for that transaction carries %q (%v); withholding the payload",
			uint64(op.BlockNumber), uint64(op.TxIndex), op.TxHash.String(), target, served, err)
		p.ddbWithholdOperation(op, &types.DdbWithheld{
			Reason: types.DdbWithheldUndecidable, Schema: target})
		return
	}

	// Authorized, and bound to this row. Serve the operation the node just handed over rather than the
	// indexed copy: it is the authoritative one, and it is also what fills a row whose payload was
	// withheld as `undecidable` at ingest and has since become servable.
	op.Payload = operation
	op.Withheld = nil
}

// ddbWithholdOperation empties a row's payload and records why. The size and hash of the bytes the
// explorer was about to serve are kept when the node did not state its own, so a party who obtains
// them through an authorized read can still bind them to the chain.
func (p *proxy) ddbWithholdOperation(op *types.DdbOperation, d *types.DdbWithheld) {
	if d.Size == 0 && len(op.Payload) > 0 {
		d.Size = hexutil.Uint64(len(op.Payload))
		d.Keccak256 = crypto.Keccak256Hash(op.Payload)
	}
	op.Payload = nil
	op.Withheld = d
}

// ddbFilterTxInput withholds the calldata of every DDB commit transaction the node will not vouch for.
//
// A transaction loaded from the NODE already carries the answer: it serves `input` empty with an
// `inputRedacted` descriptor, which types.Transaction deserializes directly, so there is nothing to
// ask. One read back from the local index or the in-memory cache carries the bytes as they were
// indexed and no descriptor, and that is the case this exists for.
//
// Non-DDB transactions cost nothing: the prefix test is local, and the overwhelming majority of a
// block page's transactions fail it.
func (p *proxy) ddbFilterTxInput(ctx context.Context, txs ...*types.Transaction) {
	pending := make([]common.Hash, 0, len(txs))
	for _, t := range txs {
		if ddbNeedsVerdict(t) {
			pending = append(pending, t.Hash)
		}
	}
	if len(pending) == 0 {
		return
	}
	verdicts := p.ddbCommitVerdicts(ctx, pending)

	for _, t := range txs {
		if !ddbNeedsVerdict(t) {
			continue
		}
		v := verdicts[t.Hash]
		if v.Withheld == nil {
			continue
		}
		d := *v.Withheld
		if d.Size == 0 {
			d.Size = hexutil.Uint64(len(t.InputData))
			d.Keccak256 = crypto.Keccak256Hash(t.InputData)
		}
		t.InputData = nil
		t.InputRedacted = &d
	}
}

// ddbNeedsVerdict reports whether a transaction carries DDB commit calldata that has not already been
// judged by the node that served it.
func ddbNeedsVerdict(t *types.Transaction) bool {
	return t != nil && t.InputRedacted == nil && types.IsDdbCommitTxData(t.InputData)
}

// ddbServedOperation decodes calldata the node has authorized and returns the operation JSON it
// carries together with the contract whose read policy governs it, so the latter can be compared with
// the contract the row being served claims.
func ddbServedOperation(input []byte) (operation []byte, target string, err error) {
	info, err := rpc.DecodeDdbCommitTx(input)
	if err != nil {
		return nil, "", err
	}
	target, err = types.DdbOperationTargetFromJSON(info.Operation)
	if err != nil {
		return nil, "", err
	}
	return info.Operation, target, nil
}
