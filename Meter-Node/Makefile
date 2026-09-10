# MeterNode — the whole repository.
#
# One module, so `make test` covers the controller, the agent, and the schema
# they share in a single command. The per-component Makefiles in console/ and
# agent/ still work for focused work; this one is what CI runs.

SHELL := /bin/bash
.DEFAULT_GOAL := help

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GO      ?= go

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: test
test: ## Run every unit test in the repository
	$(GO) test -race ./...

.PHONY: lint
lint: ## Vet and check formatting
	$(GO) vet ./...
	@unformatted=$$(gofmt -l . | grep -v '/bin/' || true); \
	if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi

.PHONY: tidy
tidy: ## Verify go.mod is tidy
	$(GO) mod tidy
	@git diff --exit-code go.mod go.sum

.PHONY: build
build: ## Build every binary into bin/
	$(GO) build -o bin/controller ./console/cmd/controller
	$(GO) build -o bin/simulator  ./console/cmd/simulator
	$(GO) build -o bin/meternode-agent ./agent/cmd/agent
	$(GO) build -o bin/meternodectl    ./agent/cmd/agentctl

.PHONY: budget
budget: ## Print the projected control-plane traffic per node
	$(GO) test -v -run 'TestMonthlyBudget|TestHeartbeatFitsBudget' ./proto/

.PHONY: ci
ci: lint test build ## Everything CI runs that needs no database

.PHONY: dev
dev: ## Bring up the control plane (Postgres/Timescale + Redis + controller)
	$(MAKE) -C console dev

.PHONY: simulate
simulate: ## Point simulated nodes at the running control plane
	$(MAKE) -C console simulate

.PHONY: agent
agent: ## Run the agent locally against a scratch config
	$(MAKE) -C agent run

.PHONY: deb
deb: ## Build the agent .deb
	$(MAKE) -C agent deb

.PHONY: clean
clean: ## Remove build output
	rm -rf bin/ console/bin console/coverage.out agent/bin agent/dist agent/dev agent/coverage.out
