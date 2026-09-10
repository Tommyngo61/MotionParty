# MeterNode Agent
#
# Everything a contributor runs day to day. CI runs the same targets, so a green
# `make ci` locally means a green pipeline.

SHELL := /bin/bash
.DEFAULT_GOAL := help

VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GO         ?= go

PKG     := github.com/MeterHome/Meter-Node/agent/internal/version
LDFLAGS := -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X $(PKG).BuildDate=$(BUILD_DATE)

# A scratch directory so `make run` never needs root and never touches
# /etc/meternode or /var/lib/meternode.
DEV_DIR := dev

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

## --- build ---------------------------------------------------------------

.PHONY: build
build: ## Build the agent and meternodectl for the host
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/meternode-agent ../agent/cmd/agent
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/meternodectl    ../agent/cmd/agentctl

.PHONY: cross
cross: ## Cross-compile for the only target we ship: linux/amd64, static
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "-s -w $(LDFLAGS)" \
		-o bin/linux-amd64/meternode-agent ../agent/cmd/agent
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "-s -w $(LDFLAGS)" \
		-o bin/linux-amd64/meternodectl ../agent/cmd/agentctl
	@file bin/linux-amd64/meternode-agent 2>/dev/null || true

.PHONY: deb
deb: ## Build the .deb (needs dpkg-deb, which ships with Debian/Ubuntu)
	VERSION=$(VERSION) ./packaging/deb/build.sh

.PHONY: image
image: ## Build the development OCI image
	docker build -f packaging/Dockerfile -t meternode-agent:$(VERSION) --build-arg VERSION=$(VERSION) ..

.PHONY: clean
clean: ## Remove build output
	rm -rf bin/ dist/ $(DEV_DIR)/

## --- test ----------------------------------------------------------------

.PHONY: test
test: ## Run the unit tests
	$(GO) test ./...

.PHONY: test-race
test-race: ## Run the unit tests under the race detector
	$(GO) test -race ./...

.PHONY: cover
cover: ## Run tests with a coverage report
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

.PHONY: netem
netem: ## Run the tc netem harness (needs Docker and NET_ADMIN; M3)
	./test/netem/run.sh

.PHONY: ci
ci: lint test-race cross ## Everything CI runs

## --- run locally ---------------------------------------------------------

.PHONY: dev-config
dev-config:
	@mkdir -p $(DEV_DIR)/state $(DEV_DIR)/run
	@if [ ! -f $(DEV_DIR)/agent.yaml ]; then \
		sed -e 's|url: "https://control.example.com"|url: "http://localhost:8080"|' \
		    -e 's|state_dir: /var/lib/meternode|state_dir: ./$(DEV_DIR)/state|' \
		    -e 's|socket_path: /run/meternode/agent.sock|socket_path: $(CURDIR)/$(DEV_DIR)/run/agent.sock|' \
		    -e 's|format: json|format: text|' \
		    packaging/systemd/agent.yaml.example > $(DEV_DIR)/agent.yaml; \
		echo "wrote $(DEV_DIR)/agent.yaml"; \
	fi

.PHONY: run
run: build dev-config ## Run the agent against a scratch config (no root needed)
	./bin/meternode-agent -config $(DEV_DIR)/agent.yaml

.PHONY: status
status: ## Ask a locally-running agent for its status
	./bin/meternodectl -socket $(CURDIR)/$(DEV_DIR)/run/agent.sock status

.PHONY: check-config
check-config: build dev-config ## Validate the scratch config
	./bin/meternode-agent -config $(DEV_DIR)/agent.yaml -check-config
