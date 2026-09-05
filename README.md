# NCOG Earth Chain GraphQL API Server
[![Go Report Card](https://goreportcard.com/badge/github.com/Ncog-Earth-Chain/graphql-api)](https://goreportcard.com/report/github.com/Ncog-Earth-Chain/graphql-api)

GraphQL API server for NCOG Earth powered blockchain network.

## Releases
Please check the [release tags](https://github.com/Ncog-Earth-Chain/graphql-api/tags) to get more details and to download previous releases.

#### Version 0.2.0, pending
This version connects with the Forest v.0.7.0-rc1. The SFC contract ABI bundled with the API is version 2.0.2-rc1.

The release brings new fluid delegations and rewards system. Each address is be able to delegate to multiple stakers. Delegation can be locked to certain time, at least 14 days and up to 1 year, to get higher rewards. Please check our website [NCOGChain.Earth](https://ncogchain.earth) and the [Special Fee Contract repository](https://github.com/Ncog-Earth-Chain/ncogearthchain-sfc) for more details.

#### Version 0.1.0, released on 8/2020
This is the version you want to be able to connect with Forest v.0.6.0-rc2. The SFC contract ABI bundled with this API release is the version 1.1.0-rc1. The release uses Forest API v0.6.0 which recognizes single delegation per address and no delegation locking.

## Building the source

Building `apiserver` requires Go (version 1.25 or later). You can install
it using your favourite package manager. Once the dependencies are installed, run

```shell
make
```

The build output is ```build/apiserver``` executable.

You don't need to clone the project into $GOPATH, due to use of Go Modules you can
use any location.

## Running the API server

To run the API Server you need access to a RPC interface of a full Forest node. Please
follow [Forest](https://github.com/Ncog-Earth-Chain/go-forest) instructions to build
and run the node. Alternatively you can obtain access to a remotely running instance
of Forest.

We recommend using local IPC channel for communication between a Forest node and the
API Server for performance and security reasons. Please consider security implications
of opening Forest RPC to outside access, especially if you enable "personal" commands
on your node while keeping your account keys in the Forest key store.

Persistent data are stored in a PostgreSQL database (version 14 or later). Going through
the installation and configuration of PostgreSQL is out of scope here; please consult the
[PostgreSQL documentation](https://www.postgresql.org/docs/) to provision a server for your
deployment. Point the API at it with the `pg` block of the configuration file (see
[doc/example.config.json](doc/example.config.json)): set `pg.url` to a libpq DSN and leave
`pg.auto_migrate` enabled to apply the bundled schema migrations at startup.

The database schema, the operational runbook, and the migration from the previous MongoDB
storage are documented in [doc/postgres-cutover.md](doc/postgres-cutover.md).

## Running in a container

```shell
cp .env.example .env     # EXPLORER_PG_PASSWORD and NEC_NODE_RPC_URL have no defaults
$EDITOR .env
make up                  # builds the image, starts the API and its own PostgreSQL
curl -s http://127.0.0.1:16761/health
```

The compose stack runs the API against **its own** PostgreSQL, which must be a different instance
from the one a validator runs for its DDB. The node is not part of the stack: point
`NEC_NODE_RPC_URL` at whichever node this API should read.

There are no default credentials. Compose refuses to start when the database password or the node
RPC endpoint is unset, and the image enforces the same rule on its own, so a `docker run` cannot
slip past it.

`make image` builds the image, `make check` validates the compose file and the systemd units
without starting anything, `make destroy` removes the stack and its database volume.
`deploy/systemd/` holds two units: one for the compose stack, one for the bare binary.

The build context is the **parent** directory, because `go.mod` replaces `go-ethereum` with the
sibling `../ncog-evm`; `docker build .` from this repository cannot resolve that.

Full operator documentation -- every environment variable, the node RPC pitfalls, health and
readiness, migrations, and troubleshooting -- is in [doc/container.md](doc/container.md).
