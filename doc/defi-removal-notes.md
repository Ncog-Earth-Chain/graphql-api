# DeFi module removal — verified traps

Operator decision: remove the inherited Fantom DeFi modules (fMint, Uniswap, fLend) and
the SFC v1/v2 bindings, keeping SFC v3 only.

**SFC v1/v2 is DONE** (commit `230b24f`, 16,723 lines removed). The rest is not, and the
notes below are the reason it should not be done by pattern-matching on names. Each item
was found by adversarially reviewing a deletion list against the real source; each one
would have broken something that still works.

---

## The shared-artifact traps

### Uniswap Router is NOT deletable with the rest of Uniswap

`defiNativeToken` — a query we keep — resolves through the Uniswap Router:

```
resolvers/defi.go:44  ->  repository/uniswap.go:11 NativeTokenAddress
                      ->  rpc/uniswap.go:33 contracts.NewUniswapRouter(nec.uniswapConfig.Router, ...).WETH()
```

So deleting the Uniswap module wholesale breaks a live query. Three things must survive
unless `defiNativeToken` is itself removed or repointed:

- `rpc/contracts/uniswap_router.go` and `abi/uniswap-router.abi`
- `rpc/uniswap.go:29-46` (`NativeTokenAddress`) and the `uniswapConfig` field wiring in
  `rpc/bridge.go:47,81`
- `config.go:148` and `DeFiUniswap.Router` (`config.go:161`) — delete only `Core` and
  `PairsWhiteList`

**And the configuration file matters here.** `doc/example.config.json`'s `uniswap` block is
the ONLY source of `cfg.DeFi.Uniswap.Router`: the viper defaults at `keys.go:84-85` and
`default.go:104,107,181,182` are all commented out, so there is no fallback. Deleting the
block zeroes `Router`, and `defiNativeToken` then fails on every call — at runtime, with a
clean build. Keep the block with `router` only.

The alternative is to drop `defiNativeToken` too and take the whole Router chain with it.
Either is defensible; what is not defensible is doing half of each, which is what a naive
name-based deletion produces.

### `repository/interface.go` import orphans only when BOTH passes run

`interface.go:14` imports `rpc/contracts` for exactly three symbols:

| symbol | line | owner |
|---|---|---|
| `contracts.UniswapPair` | 409 | Uniswap |
| `contracts.UniswapFactory` | 421 | Uniswap |
| `contracts.ILendingPool` | 568 | fLend |

Neither deletion alone orphans the import. Doing both does, and the failure is invisible
from either list read in isolation. **Whichever pass runs second must also remove the
import.**

`repository/defi.go` has the same shape on its own: `contracts.ILendingPool` at `:85` is
the file's only `contracts.` reference, so the fLend deletion must drop the import in the
same edit.

---

## Range errors that would have broken the build

- `schema.graphql`: the Uniswap root-query block ends at **214**, not 218. Lines 216-218
  are the `erc20Token` query, which we keep. Delete 152-214.
- `schema/bundle.go`: the corresponding range **is** 156-218 — the offset differs from the
  definition file. Do not reuse one number for the other.
- `schema/schema_test.go`: delete **70-85**, not 71-85. Starting at 71 leaves the element's
  opening `{` at line 70 orphaned against the next element's `{` at 86 — a Go syntax error.

---

## Verification after any deletion pass

`go build ./...` is not sufficient. The schema-to-resolver parity guard lives in a test
that panics rather than failing to compile:

```
internal/graphql/resolvers/ddb_test.go:22
    graphql.MustParseSchema(gqlschema.Schema(), &rootResolver{}, UseFieldResolvers())
```

A root query left in the schema whose resolver method has been deleted builds cleanly and
panics at startup. Run **both**:

```
go test ./internal/graphql/schema/
go test ./internal/graphql/resolvers/
```

---

## Still to map

The **fMint** deletion list has not been produced — the agent generating it failed twice.
fMint is the largest remaining surface and touches at least:

- `svc/dispatch_log.go:121-136` (5 topic entries)
- `svc/logs_fmint.go`, `repository/fmint.go`
- `rpc/contracts/fmint_{addresses,minter,rewards,tokens}.go` + `abi/defi-fmint-*.abi`
- `types/fmint_trx_list.go`
- `config.go:147` and `:153-156` (`DeFiFMint`), `doc/example.config.json:39`
- `ERC20Token.totalDeposit` / `.totalDebt` (`resolvers/erc20.go:126-141`) — these are
  described as fMint collateral/debt and are fMint-owned, not SFC-owned

Do not delete fMint from this list alone; it is a starting point, not a verified map.

---

## A note on reading these reports

The safety review flagged five SFC artifacts as "do not exist — the evidence is
fabricated". They did exist; they had been deleted by the SFC v1/v2 pass that ran between
the two analyses. The line counts it called fabricated (5819 + 6291 = 12,110) match the
files exactly as measured before removal.

Worth recording because the failure mode generalises: an analysis of a moving tree can be
correct when written and stale when read, and "the evidence is fabricated" is a much
stronger claim than "the tree changed underneath me". Check the timeline before believing
the stronger one.
