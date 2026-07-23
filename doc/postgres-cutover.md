# PostgreSQL cutover runbook

The explorer no longer uses MongoDB. This is how to bring it up on PostgreSQL in
production, verify it, and roll back if it is wrong.

**The whole plan rests on one property: the explorer database is a DERIVED INDEX, not a
source of truth.** Every row except contract verification can be reproduced by re-scanning
the chain. So the cutover is a *rebuild* rather than a data migration, and rollback is
"point the load balancer back", not "restore a backup". Everything below follows from
that.

---

## 0. Before you start

**Provision a SEPARATE PostgreSQL instance from the one the node runs for its DDB.** Not a
separate database on the same server — a separate instance. Disk, WAL, `max_connections`,
autovacuum workers and the postmaster lifetime are all cluster-scoped, so a re-scan that
fills a disk or exhausts connections would take the validator's DDB down with it. DDB rows
are consensus state; explorer rows are a rebuildable index. They should not be able to
hurt each other.

Requirements:

| | |
|---|---|
| PostgreSQL | 14 minimum, 16 recommended (developed and verified against 16.14) |
| Disk | see §6 — dominated by `tx` and `tx_log` |
| A node | with `TxIndex` enabled, reachable over IPC or HTTP |

Create the role and database:

```sql
CREATE ROLE explorer LOGIN PASSWORD '<strong-password>';
CREATE DATABASE nec_explorer OWNER explorer;
```

Configuration (`apiserver.json`, alongside the existing `node` block):

```json
"pg": {
  "url": "postgres://explorer:<password>@<host>:5432/nec_explorer?sslmode=require",
  "max_conns": 16,
  "min_conns": 2,
  "statement_timeout": 30,
  "auto_migrate": true
}
```

Size `max_conns` against the server's `max_connections`, not against expected traffic. The
old `db` (MongoDB) block is ignored and can be deleted.

---

## 1. What is NOT rebuildable

Everything the explorer stores is derived from the chain **except contract verification** —
source code, ABI, compiler settings, proxy resolution. That is submitted by users through a
GraphQL mutation, exists nowhere on chain, and no amount of re-scanning recovers it.

If the outgoing MongoDB deployment holds verified contracts, export them before you retire
it. They live in the `contract` collection, on documents where `is_ok` is set:

```
mongoexport --uri="<old-mongo-uri>" --collection=contract \
            --query='{"is_ok":{"$ne":null}}' --out=verified.json
```

Load them into `contract_verification` after the rebuild reaches head. The schema splits
chain-derived deployment facts (`contract`) from user-submitted verification
(`contract_verification`) precisely so this data can never be destroyed by a re-scan or a
reorg purge.

**If nothing is verified yet, there is nothing to migrate and this section is a no-op.**

---

## 2. Rebuild

Bring the API server up against the empty database. It will:

1. apply the schema migrations (fatal on failure — a half-migrated schema produces errors
   that look like data corruption)
2. open the connection pool
3. find `contiguous_head = 0` and scan from genesis

Nothing needs a flag. An empty database *is* the instruction to rebuild.

Watch progress:

```sql
SELECT value AS contiguous_head FROM meta_counter WHERE key = 'contiguous_head';
SELECT max(number) AS height, count(*) AS blocks FROM block;
```

`contiguous_head` is the number that matters. It is the highest block below which
**nothing is missing**, derived inside the transaction that writes each block, so it cannot
overstate progress. `max(number)` is merely how far the scanner has reached.

If those two diverge, blocks are missing in between — which is fine during a rebuild, and
`MissingBlocks` will tell you exactly which:

```sql
SELECT n FROM generate_series(1, (SELECT max(number) FROM block)) n
WHERE NOT EXISTS (SELECT 1 FROM block WHERE number = n)
ORDER BY n LIMIT 20;
```

**Run the whole rebuild with the API server serving no public traffic.** It is not
"partially correct" until `contiguous_head` reaches the chain head; it is incomplete, and
an incomplete explorer answering queries reports missing data as fact.

---

## 3. Verify before switching

Do not cut over on "it started successfully". Check these in order; each catches a
different class of failure.

### 3.1 No gaps

```sql
SELECT (SELECT value FROM meta_counter WHERE key='contiguous_head') AS contiguous_head,
       (SELECT max(number) FROM block)                              AS height;
```

These must be equal, and within a few blocks of the node's `eth_blockNumber`.

### 3.2 Agreement with the node

Sample blocks across the range and compare against the chain directly. A block the
explorer stored with the wrong transaction count is the failure this catches:

```sql
SELECT number, tx_count, (SELECT count(*) FROM tx WHERE tx.block_number = block.number) AS actual
FROM   block
WHERE  number IN (1, 1000, 100000, (SELECT max(number) FROM block))
```

`tx_count` and `actual` must match on every row. Then spot-check a handful of those block
numbers against `nec_getBlockByNumber` on the node and confirm the hashes agree.

### 3.3 Money survived exactly

The MongoDB schema stored amounts twice and only the lossy `int64` copy was aggregatable,
so any value above 2^63 was silently wrong. Confirm nothing is truncated:

```sql
SELECT count(*) AS above_int64 FROM tx WHERE value_wei > 9223372036854775807;
SELECT max(value_wei) FROM tx;
```

If the chain has any transfer above ~9.2 NEC at 18 decimals, the first count is non-zero
and the maximum is exact — that is the fix working.

### 3.4 The API answers

```
curl -s -X POST http://<host>:16761/api \
  -H 'Content-Type: application/json' \
  -d '{"query":"{ state { blocks transactions } block(number:\"0x1\"){ hash txCount } }"}'
```

Compare a few account pages and transaction lists against the outgoing MongoDB explorer
running side by side. **Expect two deliberate differences:**

- **Burn totals will change.** The MongoDB accumulator was broken — `db/burn.go:71` was
  `sr.Decode(&sr)`, decoding into the result cursor instead of the target, so the existing
  value was always zero and each update *replaced* rather than accumulated. The new totals
  are correct; the old ones were not.
- **Contract list ordering will change.** The MongoDB ordinal was a wall-clock timestamp
  with a 24-bit hash tiebreaker, so it disagreed with every other list in the API. Contracts
  now order by chain position.

Anything else that differs is worth investigating before you switch.

---

## 4. Cut over

Run both stacks side by side, then move traffic at the load balancer. The old MongoDB
explorer stays running and untouched — it is the rollback.

1. Point a small share of read traffic at the new instance; watch error rates and latency.
2. Increase to 100%.
3. Leave the MongoDB stack running, idle, for at least one full day of traffic.
4. Retire it once you are satisfied.

**Rollback is moving the load-balancer weight back.** No data is migrated, so nothing needs
undoing. That is the payoff of treating the database as a derived index.

---

## 5. After cutover

Schedule these; none are optional in a long-running deployment.

**Refresh account statistics.** `account_stat` is a materialized view feeding transaction
counts and last-activity ordering. It is not maintained per write — deliberately, because
maintained counters drift and derived ones cannot:

```sql
REFRESH MATERIALIZED VIEW CONCURRENTLY account_stat;
```

Every few minutes is reasonable. `CONCURRENTLY` means readers are never blocked.

**Keep partitions ahead of the head.** `tx_log` and `gas_price_tick` are range-partitioned.
A row with no partition raises `no partition of relation ... found for row`, which fails the
block transaction and wedges the indexer at that height:

```sql
SELECT * FROM partition_health();
```

`partitions_ahead` should stay comfortably above zero. `orphan_attached` and
`orphan_detached` flag partitions that exist outside the registry — investigate rather than
ignore.

**Backups.** `pg_dump` nightly is adequate given the database is rebuildable; the only
irreplaceable table is `contract_verification`, which is small:

```
pg_dump -Fc -t contract_verification nec_explorer > verification-$(date +%F).dump
```

Take WAL archiving/PITR only if you would rather restore than re-scan.

---

## 6. Sizing

Dominated by `tx` and `tx_log`. Rough per-transaction cost: ~400 bytes of `tx` row plus
indexes, and logs on top of that for contract activity.

**Signatures and public keys are deliberately NOT stored.** ML-DSA-87 signatures are
~4.6 KB and public keys 2,592 bytes; storing both per transaction would be roughly 70–75%
of the database and would dominate WAL volume, making the backup window larger than the
data it protects. They remain recoverable from the node and are *self-verifying*:

```
keccak256(eth_getRawTransactionByHash(h)) == h
```

exactly, because the transaction hash is the RLP hash of the same inner struct that carries
them. One keccak verifies a returned signature and public key against a value this database
already holds — so attribution is still provable, on demand, at zero storage cost.

**The one operational requirement that follows: at least one full-history node with
`TxIndex` enabled must stay reachable**, or that path returns nothing. No configuration
setting can enforce this; it is a fleet commitment.

---

## 7. Known limitations

**`delegation` and `withdrawal` are not rolled back on reorg.** They are domain-keyed
current-state rows — `(delegator, validator_id)` and `(delegator, validator_id, request_id,
request_tx)` — whose `block_number` records the last event that touched the row, not the
row's identity. Deleting by block would remove a live delegation merely because its most
recent update landed in a reorged block, and rolling back correctly needs prior state that
is not stored.

A reorg that removes an SFC event can therefore leave these slightly stale until the next
event for the same key overwrites them. The fix is to make those two tables event-sourced,
which is a schema change and a product decision. Recorded here rather than papered over.

Every other block-keyed table *is* purged on reorg, and
`TestPurgeCoversEveryBlockKeyedTable` introspects the database catalog so a new table added
without updating the purge fails a test rather than silently surviving the next reorg.
