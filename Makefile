# --------------------------------------------------------------------------
# Makefile for the NCOG Earth Chain API GraphQL Server
#
# v0.1 (2020/03/09)  - Initial version, base API server build.
# (c) NCOG Earth Chain, 2023
# --------------------------------------------------------------------------

# project related vars
PROJECT := $(shell basename "$(PWD)")

# go related vars
GO_BASE := $(shell pwd)
GO_BIN := $(CURDIR)/build

# compile time variables will be injected into the app
APP_VERSION := 1.1.0
BUILD_DATE := $(shell date)
BUILD_COMPILER := $(shell go version)
BUILD_COMMIT := $(shell git show --format="%H" --no-patch)
BUILD_COMMIT_TIME := $(shell git show --format="%cD" --no-patch)

## server: Make the API server as build/apiserver
server:
	go build \
	-ldflags="-X 'ncogearthchain-api-graphql/cmd/apiserver/build.Version=$(APP_VERSION)' -X 'ncogearthchain-api-graphql/cmd/apiserver/build.Time=$(BUILD_DATE)' -X 'ncogearthchain-api-graphql/cmd/apiserver/build.Compiler=$(BUILD_COMPILER)' -X 'ncogearthchain-api-graphql/cmd/apiserver/build.Commit=$(BUILD_COMMIT)' -X 'ncogearthchain-api-graphql/cmd/apiserver/build.CommitTime=$(BUILD_COMMIT_TIME)'" \
	-o $(GO_BIN)/apiserver \
	./cmd/apiserver

bundle:
	cd internal/graphql/schema/; tools/make_bundle.sh

## test: Run the test suite
test:
	go test \
	-ldflags="-X 'ncogearthchain-api-graphql/cmd/apiserver/build.Version=$(APP_VERSION)' -X 'ncogearthchain-api-graphql/cmd/apiserver/build.Time=$(BUILD_DATE)' -X 'ncogearthchain-api-graphql/cmd/apiserver/build.Compiler=$(BUILD_COMPILER)' -X 'ncogearthchain-api-graphql/cmd/apiserver/build.Commit=$(BUILD_COMMIT)' -X 'ncogearthchain-api-graphql/cmd/apiserver/build.CommitTime=$(BUILD_COMMIT_TIME)'" \
	./...

# --------------------------------------------------------------------------
# Container packaging. See doc/container.md.
#
# THE BUILD CONTEXT IS THE PARENT DIRECTORY, not this one. go.mod carries
#     replace github.com/ethereum/go-ethereum => ../ncog-evm
# so the sibling ncog-evm/ has to be inside the context or the module graph
# cannot resolve; `docker build .` from here fails at
#     failed to compute cache key: "/ncog-evm": not found
# The context is filtered by docker/Dockerfile.apiserver.dockerignore, a
# per-Dockerfile ignore file that BuildKit prefers over the context-root
# .dockerignore -- which the sibling node repo owns and which excludes this
# repo entirely. That is why these targets do NOT write ../.dockerignore the
# way the node repo's image target does: nothing here touches the parent.
# --------------------------------------------------------------------------
IMAGE ?= ncog-apiserver:latest
# Overridable because the CLI is not always called `docker` on the PATH that make sees -- from a
# WSL distro without Docker Desktop's integration enabled, for instance, it is `docker.exe`.
DOCKER ?= docker
COMPOSE ?= $(DOCKER) compose

## image: Build the container image as $(IMAGE) (context is the parent dir)
image:
	cd .. && $(DOCKER) build \
		-f graphql-api/docker/Dockerfile.apiserver \
		--build-arg APP_VERSION="$(APP_VERSION)" \
		--build-arg BUILD_DATE="$(BUILD_DATE)" \
		--build-arg BUILD_COMMIT="$(BUILD_COMMIT)" \
		--build-arg BUILD_COMMIT_TIME="$(BUILD_COMMIT_TIME)" \
		-t $(IMAGE) .

## up: Build if needed and start the API + its PostgreSQL (needs .env)
up:
	$(COMPOSE) up -d --build --wait

## down: Stop the stack, keeping the database volume
down:
	$(COMPOSE) down

## destroy: Stop the stack and DELETE the explorer database volume
destroy:
	$(COMPOSE) down -v

## logs: Follow the API server log
logs:
	$(COMPOSE) logs -f apiserver

## ps: Show the stack and each container's health
ps:
	$(COMPOSE) ps

## migrate: Apply schema migrations as a one-shot job (auto_migrate=false deployments)
migrate:
	$(COMPOSE) --profile migrate run --rm migrate

## check: Validate the compose file and both systemd units without starting anything
#
# The compose check is a GATE: a malformed file or a missing required variable fails the target.
# The unit check is INFORMATIONAL, because systemd-analyze also reports every ExecStart= whose
# binary is absent -- and on a build machine neither /usr/bin/docker nor an installed
# /usr/local/bin/apiserver need exist. Read its output; "no output" means the units are clean.
check:
	$(COMPOSE) config -q && echo "compose: ok"
	@command -v systemd-analyze >/dev/null 2>&1 || { echo "systemd-analyze not present; skipped unit verification"; exit 0; }; \
	echo "systemd units (findings about absent ExecStart binaries are expected off the deployment host):"; \
	systemd-analyze verify deploy/systemd/*.service || true

.PHONY: help test server bundle image up down destroy logs ps migrate check
all: help
help: Makefile
	@echo
	@echo "Choose a make command in "$(PROJECT)":"
	@echo
	@sed -n 's/^##//p' $< | column -t -s ':' |  sed -e 's/^/ /'
	@echo
