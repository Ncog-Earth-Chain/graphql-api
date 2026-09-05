# Running the GraphQL API in a container

This is the deployment surface for `apiserver`: a multi-stage image, a compose stack that stands
it up against its own PostgreSQL, and two systemd units. Everything here was built and run against
a live two-validator chain; the commands below are the ones that were executed, not a sketch.

---

## 0. Quick start

```shell
cd graphql-api
cp .env.example .env
$EDITOR .env                     # EXPLORER_PG_PASSWORD and NEC_NODE_RPC_URL have no defaults
make up                          # == docker compose up -d --build --wait
```

Then:

```shell
curl -s http://127.0.0.1:16761/health
# {"contiguousHead":1,"status":"ok"}

curl -s -X POST -H 'Content-Type: application/json' \
  --data '{"query":"{ version block(number:1){ number hash transactionCount } }"}' \
  http://127.0.0.1:16761/graphql
```

`make down` stops it and keeps the database; `make destroy` also deletes the volume.

---

## 1. What an operator MUST set

Exactly two things have no default, in either the image or the compose file, and both refuse to
start rather than fall back:

| Variable | What it is |
|---|---|
| `EXPLORER_PG_PASSWORD` | password for the explorer's own PostgreSQL. The compose file assembles the DSN from it. |
| `NEC_NODE_RPC_URL` | RPC endpoint of the NCOG Earth Chain node this API reads. |

Unset, compose stops before creating anything:

```
$ docker compose up -d
error while interpolating services.postgres.environment.POSTGRES_PASSWORD: required variable
EXPLORER_PG_PASSWORD is missing a value: set EXPLORER_PG_PASSWORD (put it in a .env file next to
this compose file) - there is deliberately no default
```

and the image enforces the same rule on its own, so `docker run` cannot slip past compose:

```
$ docker run --rm ncog-apiserver:latest
apiserver-entrypoint: NEC_API_PG_URL is not set.
...
$ echo $?
1
```

This mirrors `ncogearthchain/docker-compose.ddb.yml`, where `POSTGRES_PASSWORD` lost its
`password` fallback for the same reason: the failure mode of a weak default is that nothing looks
wrong.

### The database

`postgres:16-alpine`, on its own volume, published on **127.0.0.1:5442** so it cannot collide with
a validator's DDB PostgreSQL on 5432/5433.

**This is not the validator's DDB and must never be pointed at it.** DDB rows are consensus state;
explorer rows are a derived index that a re-scan rebuilds from the chain. Disk, WAL,
`max_connections` and the postmaster lifetime are all cluster-scoped, so an explorer re-scan that
fills a disk would take the validator down with it. `doc/postgres-cutover.md` §0 is the long form.

To use a PostgreSQL this stack does not run, set `EXPLORER_PG_DSN` to a full libpq DSN
(percent-encode the password) and comment the `postgres` service out. Use `sslmode=require` or
`verify-full` for anything off-host; the bundled DSN uses `sslmode=disable` because that hop never
leaves the compose bridge.

### The node

The node is deliberately **not** part of this stack: it has its own lifecycle, its own compose
file, and usually its own machine. `NEC_NODE_RPC_URL` accepts anything `rpc.Dial` accepts --
`http://`, `ws://`, or a filesystem path for IPC.

Three things bite here, all of them verified on this testbed:

1. **A node bound to loopback is not reachable from a container.** The testbed launcher starts the
   node with `--http.addr 127.0.0.1`. Nothing outside that network namespace can reach it -- not
   the container, not `host.docker.internal`. Start the node with `--http.addr 0.0.0.0` (behind a
   firewall) or put a forwarder in front of it.

2. **`--http.vhosts` will reject the container's `Host` header.** go-ethereum's HTTP RPC defaults
   to `--http.vhosts localhost`, and a request to `http://host.docker.internal:18545` carries
   `Host: host.docker.internal:18545`, which is not in that list:

   ```
   wget: server returned error: HTTP/1.1 403 Forbidden
   ```

   Two fixes. Add the name to the node's `--http.vhosts`, or address the host by **IP** -- the
   vhost check passes anything that parses as an IP address:

   ```
   NEC_NODE_RPC_URL=http://192.168.65.254:18800     # Docker Desktop's host-gateway address
   ```

   `docker run --rm --add-host=host.docker.internal:host-gateway alpine getent hosts
   host.docker.internal` prints that address. Prefer the *IPv4* line; the IPv6 one cannot be
   written into a `http://host:port` URL without brackets.

3. **IPC is the best option when the API and the node share a host** -- fastest, and it exposes no
   port. Bind-mount the socket and point at it inside the container:

   ```yaml
   volumes:
     - /var/run/ncog/ncogearthchain.ipc:/var/run/ncog/ncogearthchain.ipc
   environment:
     NEC_API_NODE_URL: /var/run/ncog/ncogearthchain.ipc
   ```

   The socket must be readable by uid 10002, the user the image runs as.

The node needs the `eth` and `net` RPC namespaces and transaction indexing.

---

## 2. How the container is configured

**The application is configured by a file, and only by a file.** `internal/config/load.go` builds
a viper reader that searches `$HOME/.ncogearthchainapi` and `.` for a file named `apiserver`
(`internal/config/keys.go`), or takes a path from `-cfg`. There is no environment binding anywhere
in the tree -- `grep -rn 'AutomaticEnv\|BindEnv\|SetEnvPrefix' internal/ cmd/` returns nothing.

So the container gives you two ways in, and they do not mix:

### (a) Environment (the default)

`docker/entrypoint.sh` renders `/run/ncog/apiserver.json` from the environment and starts the
server with `-cfg` pointing at it. Only keys whose variable is actually set are written, so
everything you leave alone still gets the application's own default from
`internal/config/default.go` rather than a frozen copy of it. The rendered file is `0600` and owned
by the app user, because it carries the DSN password.

What it logs at start (note the masked credential):

```
apiserver-entrypoint: rendered /run/ncog/apiserver.json from the environment
apiserver-entrypoint:   node.url   = http://192.168.65.254:18800
apiserver-entrypoint:   pg.url     = postgres://***:***@postgres:5432/nec_explorer?sslmode=disable
apiserver-entrypoint:   server.bind= 0.0.0.0:16761
```

### (b) A mounted config file

Mount a complete config at `/etc/ncog/apiserver.json` (override the path with `NEC_API_CONFIG`).
If that file exists it is used verbatim and the environment is ignored:

```
apiserver-entrypoint: using mounted configuration /etc/ncog/apiserver.json (environment configuration ignored)
```

This is the escape hatch for the parts of the config that are not env-shaped: the governance
contract list, the DeFi price symbols, a custom log format. Viper reads one file, so there is no
merging -- by design, because a half-env/half-file configuration behaves like neither.

### The variables

Required: `NEC_API_PG_URL`, `NEC_API_NODE_URL`.

| Variable | Config key | Image default |
|---|---|---|
| `NEC_API_BIND` | `server.bind` | `0.0.0.0:16761` (the app default `localhost:16761` is unreachable in a container) |
| `NEC_API_DOMAIN` | `server.domain` | app default |
| `NEC_API_ORIGIN` | `server.origin` | app default |
| `NEC_API_CORS_ORIGINS` | `server.cors_origins` | app default (`*`); comma-separated list |
| `NEC_API_PEERS` | `server.peers` | app default; comma-separated list |
| `NEC_API_READ_TIMEOUT` | `server.read_timeout` | app default |
| `NEC_API_WRITE_TIMEOUT` | `server.write_timeout` | app default — must exceed the resolver timeout, and startup raises it if it does not |
| `NEC_API_IDLE_TIMEOUT` | `server.idle_timeout` | app default |
| `NEC_API_HEADER_TIMEOUT` | `server.header_timeout` | app default |
| `NEC_API_RESOLVER_TIMEOUT` | `server.resolver_timeout` | app default |
| `NEC_API_MAX_QUERY_DEPTH` | `server.max_query_depth` | app default |
| `NEC_API_MAX_QUERY_COMPLEXITY` | `server.max_query_complexity` | app default |
| `NEC_API_MAX_REQUEST_BODY` | `server.max_request_body` | app default |
| `NEC_API_MAX_PARALLELISM` | `server.max_parallelism` | app default |
| `NEC_API_GRAPHI_ENABLED` | `server.graphi_enabled` | app default (off) — the IDE on `/graphi` is unauthenticated |
| `NEC_API_LOG_LEVEL` | `log.level` | `INFO` |
| `NEC_API_LOG_OUTPUT` | `log.output` | `stdout` (the app default is stderr; stdout is what log shippers read) |
| `NEC_API_LOG_FORMAT` | `log.format` | app default |
| `NEC_API_PG_URL` | `pg.url` | **required** |
| `NEC_API_PG_MAX_CONNS` | `pg.max_conns` | app default — size against the server's `max_connections`, not expected traffic |
| `NEC_API_PG_MIN_CONNS` | `pg.min_conns` | app default |
| `NEC_API_PG_STATEMENT_TIMEOUT` | `pg.statement_timeout` | app default |
| `NEC_API_PG_AUTO_MIGRATE` | `pg.auto_migrate` | app default (on) |
| `NEC_API_NODE_URL` | `node.url` | **required** |
| `NEC_API_SFC_CONTRACT` | `staking.sfc` | app default |
| `NEC_API_STI_CONTRACT` | `staking.sti` | app default |
| `NEC_API_ME_ADDRESS` | `me.address` | app default — the `from` of read-only contract calls. There is no private key option; this server never signs anything. |
| `NEC_API_MONITOR_STAKERS` | `repository.stakers` | app default |
| `NEC_API_SOLC_PATH` | `compiler.sol` | app default |
| `NEC_API_COMPILER_TEMP` | `compiler.temp` | app default |
| `NEC_API_CACHE_SIZE` | `cache.size` | app default |
| `NEC_API_CACHE_EVICTION` | `cache.eviction` | app default (a duration, e.g. `15m`) |
| `NEC_API_APP_NAME` | `app_name` | app default |
| `NEC_API_ERC20_TOKENS_FILE` | `erc20_tokens_file` | `/app/tokens.json` (absolute, because the app default is relative to the working directory) |
| `NEC_API_CONFIG` | — | `/etc/ncog/apiserver.json`; where a mounted config is looked for |
| `NEC_API_RENDERED_CONFIG` | — | `/run/ncog/apiserver.json`; where the rendered one is written |

Numeric variables are validated before the file is written, so a typo fails at start with a
message rather than silently decoding as zero:

```
apiserver-entrypoint: NEC_API_PG_MAX_CONNS must be a non-negative integer; got 'sixteen'
```

**solc is not in the image.** It is needed only by the contract-verification mutation, it is a
large per-version download, and `internal/solidity` resolves and fetches versions at run time.
Mount a volume and point `NEC_API_SOLC_PATH` / `NEC_API_COMPILER_TEMP` into it if you serve
verification.

---

## 3. Health and readiness

The image ships a `HEALTHCHECK` that runs `/usr/local/bin/healthcheck.sh`, which queries
`/health`. That endpoint (`internal/handlers/health.go`) calls `repository.Healthy()`, which probes **both**
PostgreSQL and the node RPC before reporting the ingest watermark. The node probe is the part
that is easy to leave out and was: `rpc.Dial` over `http://` is lazy, so an API pointed at a dead
chain used to start cleanly, log `node connection open` against a black hole, and answer `/health`
with **200** while every resolver returned `internal server error`. `compose up --wait` then
returned 0 in 13s and the systemd unit reported `active` on a stack that could serve nothing.
Measured after the fix: dead node -> `--wait` exits 1 and the container is `unhealthy`;
responsive node -> healthy in 7s. It pings
PostgreSQL **and** reads the ingest watermark, bounded by a 3s server-side timeout. It answers
`200 {"contiguousHead":N,"status":"ok"}` or `503 {"status":"unavailable"}`.

The probe asserts both the exit status and the body, because a 200 that is not this endpoint's 200
-- a proxy error page, a truncated response -- must not count as healthy.

Why not a TCP check. With the database stopped underneath a running API:

```
$ docker compose stop postgres
$ docker run --rm --network ncog-explorer-api_explorer-net alpine \
    sh -c 'nc -z -w 3 apiserver 16761 && echo "a port check would report healthy"'
a port check would report healthy

$ curl -s -i http://127.0.0.1:16761/health | head -1
HTTP/1.1 503 Service Unavailable

$ docker exec ncog-apiserver /usr/local/bin/healthcheck.sh; echo "exit=$?"
healthcheck: http://127.0.0.1:16761/health did not return success (backend unavailable, or not listening)
exit=1

$ docker inspect --format '{{.State.Health.Status}}' ncog-apiserver     # after 6 failed probes
unhealthy
```

and it recovers on its own once the database is back. Use this for load-balancer cutover and for
`depends_on: condition: service_healthy`.

`contiguousHead` is the highest block below which nothing is missing. **Do not serve public
traffic until it has reached the chain head** -- an incomplete explorer reports missing data as
fact (`doc/postgres-cutover.md` §2).

The other endpoints: `/graphql` and `/api` (identical; both are registered because `/api` is what
the API advertises to peers), `/graphql-ws` for subscriptions, `/json/gas` for the gas-price REST
resolver, and `/graphi` only when `NEC_API_GRAPHI_ENABLED=true`.

---

## 4. Migrations

The server applies them at startup and treats failure as fatal, because serving from a
half-migrated schema produces errors that look like data corruption. To make migrations a
deliberate step instead, set `NEC_API_PG_AUTO_MIGRATE=false` and run:

```shell
make migrate       # docker compose --profile migrate run --rm migrate
```

```
2026/09/05 05:52:51 goose: no migrations to run. current version: 14
2026/09/05 05:52:51 database schema up to date at version 14
2026/09/05 05:52:51 migrations applied
```

That is the same `cmd/migrate` binary, running the same embedded migrations through the same
`migrate.Up` entry point the server uses -- not a second, subtly different schema tool.

---

## 5. The image

```
$ docker images ncog-apiserver:latest
ncog-apiserver:latest  64.6MB
```

* **Builder** `golang:1.25.14-alpine`. `go.mod` declares `go 1.25.7` and the official images ship
  `GOTOOLCHAIN=local`, so the base image's Go must already satisfy that.
* **cgo is off.** This module is not the node module: `CGO_ENABLED=0 go build ./cmd/apiserver`
  succeeds and `CGO_ENABLED=0 go list -deps ./cmd/apiserver | grep -i secp256k1` finds nothing.
  The explorer reads the chain over RPC and verifies nothing locally, and this chain's crypto is
  ML-DSA via `cloudflare/circl`, which is pure Go.
* **Runtime** `alpine:3.22`, not distroless and not debian. Nothing links libc, so debian's extra
  ~75 MB buys nothing; and distroless/static has no shell, which this image needs twice over --
  to render the config file from the environment, and to make the healthcheck an application probe
  instead of a TCP connect.
* Runs as **uid 10002**, unprivileged. Both binaries are smoke-tested inside the runtime stage at
  build time, which is what turns a libc or architecture mismatch into a build failure instead of
  `exec ...: no such file or directory` at `docker run`.
* With `--read-only`, add `--tmpfs /run/ncog` so the rendered config has somewhere to go. Not
  needed when you mount a config instead.

### Build context

**The context is the parent directory**, the one holding both `graphql-api/` and `ncog-evm/`,
because `go.mod` carries `replace github.com/ethereum/go-ethereum => ../ncog-evm`.

```shell
make image
# == cd .. && docker build -f graphql-api/docker/Dockerfile.apiserver ... -t ncog-apiserver:latest .
```

`docker build .` from inside this repo cannot work; it fails at
`failed to compute cache key: "/ncog-evm": not found`.

That parent is shared with the node image, and there is only one `<context>/.dockerignore` -- the
node repo's, which denies `*` and re-admits only `ncogearthchain/` and `ncog-evm/`, excluding this
repo entirely. So this build uses **`docker/Dockerfile.apiserver.dockerignore`**, a per-Dockerfile
ignore file that BuildKit resolves in preference to the context-root one. The two images keep
independent filters over a shared context, and neither repo writes into the other or into the
parent. The consequence is that this Dockerfile requires BuildKit (Docker 23+, or buildx).

That filter also keeps `apiserver.exe` and `migrate.exe` -- 50 MB of Windows binaries that sit
beside `go.mod`, are untracked because `.gitignore` lists `*.exe`, and are therefore invisible to
git-based hygiene -- out of the build context, along with `build/`, both `.git` directories, and
any local `apiserver.json` carrying a live DSN.

---

## 6. systemd

Two units in `deploy/systemd/`, both verified with `systemd-analyze verify`:

* **`ncog-apiserver.service`** runs the compose stack. `Type=oneshot` + `RemainAfterExit=yes`,
  because `docker compose up -d` has no long-lived process for systemd to supervise and dockerd's
  own restart policy already supervises the containers. `--wait` is what makes the unit honest:
  without it the unit reports success as soon as the containers are *created*, so a stack that
  dies during startup boots "successfully".
* **`ncog-apiserver-native.service`** runs the binary with no container, for hosts without Docker.
  It passes `-cfg` explicitly rather than relying on viper's search path, and carries the full
  hardening set that a keyless read-only observer can afford (`systemd-analyze security` scores it
  2.0 OK). There is deliberately no `EnvironmentFile=`: the binary has no environment binding, so
  one would look like it configured something.

Installation steps are in the header comment of each unit. `make check` validates the compose file
and both units without starting anything.

---

## 7. Troubleshooting

| Symptom | Cause |
|---|---|
| `failed to compute cache key: "/ncog-evm": not found` | built with this repo as the context. Use the parent; see §5. |
| `exec /usr/local/bin/entrypoint.sh: no such file or directory` | the script has CRLF endings. `.gitattributes` pins `*.sh` to LF and the Dockerfile strips CRs; a context that reached the daemon some other way can still carry them. |
| API container exits immediately, log shows `repository init failed` | PostgreSQL is unreachable or the DSN is invalid. The node RPC does **not** cause this: `rpc.Dial` over `http://` is lazy, so a bad node URL starts cleanly and surfaces at the health probe instead (see §3). |
| `HTTP/1.1 403 Forbidden` from the node | `--http.vhosts` on the node. §1, item 2. |
| Health stays `starting` for a long time on a first run | initdb plus fourteen schema migrations. `start_period` is 90s in compose and 120s in the image. |
| `can not observe new blocks; notifications not supported` | the node's RPC transport does not support subscriptions (HTTP does not; WebSocket and IPC do). The scanner still ingests by polling; only push subscriptions are affected. |
| GraphQL answers but every list is empty | the scanner has not caught up. Watch `contiguousHead` from `/health`. |
