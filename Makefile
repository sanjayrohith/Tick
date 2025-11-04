BIN        := bin
CMDS       := tick-api tick-worker tick-scheduler
GO         ?= go
COMPOSE    ?= docker compose
COMPOSE_FILE := deploy/docker-compose.yml

VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS    := -s -w \
              -X main.version=$(VERSION) \
              -X main.commit=$(COMMIT)

.DEFAULT_GOAL := build
.PHONY: help build clean fmt lint test test-race test-integration cover migrate compose-up compose-down bench tidy

help: ## List available targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
	  | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

build: $(CMDS:%=$(BIN)/%) ## Build all binaries into bin/

$(BIN)/%:
	@mkdir -p $(BIN)
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $@ ./cmd/$*

clean: ## Remove build and coverage artifacts
	rm -rf $(BIN) coverage.out coverage.html

fmt: ## Format all Go source
	$(GO) fmt ./...

tidy: ## Tidy and verify module dependencies
	$(GO) mod tidy
	$(GO) mod verify

lint: ## Run golangci-lint
	golangci-lint run ./...

test: ## Run unit tests
	$(GO) test ./...

test-race: ## Run unit tests under the race detector
	$(GO) test -race ./...

test-integration: ## Run the integration suites (requires Docker)
	$(GO) test -race -tags=integration -timeout=20m ./...

cover: ## Generate an HTML coverage report
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "wrote coverage.html"

migrate: ## Apply database migrations
	$(GO) run ./cmd/tick-scheduler migrate

compose-up: ## Start the full local stack
	$(COMPOSE) -f $(COMPOSE_FILE) up --build -d

compose-down: ## Stop the local stack and remove volumes
	$(COMPOSE) -f $(COMPOSE_FILE) down -v

bench: ## Run the throughput and latency benchmark
	$(GO) run ./bench
