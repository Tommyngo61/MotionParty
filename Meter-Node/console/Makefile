# MeterNode Console
#
# Everything a contributor runs day to day. CI runs the same targets, so a green
# `make ci` locally means a green pipeline.

SHELL := /bin/bash
.DEFAULT_GOAL := help

VERSION      ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS      := -X main.version=$(VERSION)
GO           ?= go
DC           ?= docker compose -f deploy/docker-compose.yml

# The dev stack's connection string, matching deploy/docker-compose.yml.
DEV_DATABASE_URL ?= postgres://meternode:meternode@localhost:5432/meternode?sslmode=disable

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

## --- build ---------------------------------------------------------------

.PHONY: build
build: ## Build the controller and simulator
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/controller ./cmd/controller
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/simulator  ./cmd/simulator

.PHONY: clean
clean: ## Remove build output
	rm -rf bin/

## --- test ----------------------------------------------------------------

.PHONY: test
test: ## Run unit tests (no database required)
	$(GO) test -race ./...

.PHONY: test-integration
test-integration: ## Run integration tests against the compose database
	METERNODE_TEST_DATABASE_URL="$(DEV_DATABASE_URL)" $(GO) test -tags integration -race -count=1 ./internal/store/

.PHONY: cover
cover: ## Run unit tests with a coverage report
	$(GO) test -race -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1

.PHONY: lint
lint: ## Vet and check formatting
	$(GO) vet ./...
	@unformatted=$$(gofmt -l . | grep -v '^bin/' || true); \
	if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi

.PHONY: tidy
tidy: ## Verify go.mod is tidy
	$(GO) mod tidy
	@git diff --exit-code go.mod go.sum

.PHONY: ci
ci: lint test build ## Everything CI runs that does not need a database

## --- database ------------------------------------------------------------

.PHONY: migrate
migrate: ## Apply migrations to the dev database
	METERNODE_DATABASE_URL="$(DEV_DATABASE_URL)" $(GO) run ./cmd/controller migrate

.PHONY: migrate-status
migrate-status: ## Show which migrations have been applied
	METERNODE_DATABASE_URL="$(DEV_DATABASE_URL)" $(GO) run ./cmd/controller migrate-status

.PHONY: migrate-down
migrate-down: ## Roll back one migration
	METERNODE_DATABASE_URL="$(DEV_DATABASE_URL)" $(GO) run ./cmd/controller migrate-down

.PHONY: psql
psql: ## Open a psql shell on the dev database
	$(DC) exec postgres psql -U meternode -d meternode

## --- web -----------------------------------------------------------------

.PHONY: web-install
web-install: ## Install the operator UI's dependencies
	cd web && npm install

.PHONY: web
web: ## Run the operator UI against a local controller (localhost:5173)
	cd web && npm run dev

.PHONY: web-build
web-build: ## Type-check and build the operator UI
	cd web && npm run build

.PHONY: web-lint
web-lint: ## Type-check the operator UI
	cd web && npm run lint

## --- dev stack -----------------------------------------------------------

.PHONY: dev
dev: ## Bring up Postgres/Timescale, Redis, and the controller
	$(DC) up --build -d postgres redis
	$(DC) up --build controller

.PHONY: dev-down
dev-down: ## Stop the dev stack
	$(DC) down

.PHONY: dev-reset
dev-reset: ## Stop the dev stack and DELETE its data
	$(DC) down -v

.PHONY: dev-logs
dev-logs: ## Tail the dev stack logs
	$(DC) logs -f

.PHONY: simulate
simulate: ## Run the agent simulator against the dev stack (N=..., LATENCY=..., FAIL_PCT=...)
	$(GO) run ./cmd/simulator \
		-controller http://localhost:8080 \
		-admin-token $${METERNODE_BOOTSTRAP_ADMIN_TOKEN:-dev-admin-token} \
		-n $${N:-25} -latency $${LATENCY:-0s} -jitter $${JITTER:-0s} -fail-pct $${FAIL_PCT:-0}
