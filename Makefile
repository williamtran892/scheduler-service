SHELL := /bin/bash
.DEFAULT_GOAL := help

BIN       := bin/scheduler
GO_PKGS   := ./internal/... ./cmd/...
TAGS      ?=

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

## ---- build and run ----------------------------------------------------------

.PHONY: build
build: ## Build the binary into bin/
	go build -o $(BIN) ./cmd/scheduler

.PHONY: run
run: ## Run the service locally against the compose database (start it with make db-up)
	go run ./cmd/scheduler

.PHONY: image
image: ## Build the Linux binary and package it into the scheduler:local image
	./scripts/build-image.sh

.PHONY: up
up: image ## Build the image, start the app and MySQL, wait until both are healthy
	docker compose up -d --wait

.PHONY: down
down: ## Stop the app and MySQL and discard all data
	docker compose down -v

.PHONY: logs
logs: ## Follow the app container log
	docker compose logs -f app

## ---- tests ------------------------------------------------------------------

.PHONY: test
test: ## Unit tests: no database, race detector on
	go test $(GO_PKGS) -race -count=1

.PHONY: test-integration
test-integration: ## Rebuild the image, compose up app + MySQL, wait until healthy, run the Gherkin suite (TAGS=@bootstrap)
	GODOG_TAGS="$(TAGS)" ./scripts/integration.sh

.PHONY: test-all
test-all: test test-integration ## Unit tests, then the acceptance suite

.PHONY: cover
cover: ## Unit test coverage report in coverage.html
	go test $(GO_PKGS) -count=1 -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html
	@go tool cover -func=coverage.out | tail -1

## ---- code quality -----------------------------------------------------------

.PHONY: generate
generate: ## Regenerate mocks (go:generate directives)
	go generate ./...

.PHONY: check-generate
check-generate: generate ## Fail if generated files are out of date
	git diff --exit-code -- '*mock_*_test.go'

.PHONY: fmt
fmt: ## Format the code
	gofmt -w -s .

.PHONY: vet
vet: ## go vet
	go vet ./...

.PHONY: lint
lint: ## golangci-lint (install: brew install golangci-lint)
	@command -v golangci-lint >/dev/null || { echo "golangci-lint not found: brew install golangci-lint"; exit 1; }
	golangci-lint run ./...

.PHONY: tidy
tidy: ## go mod tidy
	go mod tidy

.PHONY: ci
ci: check-generate vet lint test test-integration ## Everything CI runs, locally

## ---- database ---------------------------------------------------------------

.PHONY: db-up
db-up: ## Start only MySQL 8.4 with schema.sql and data.sql applied, wait until healthy
	docker compose up -d --wait mysql

.PHONY: db-reset
db-reset: ## Recreate MySQL from schema.sql and data.sql
	docker compose rm -sfv mysql
	docker compose up -d --wait mysql

.PHONY: db-shell
db-shell: ## Open a mysql shell on the compose database
	docker compose exec mysql mysql -uscheduler -pscheduler scheduler

.PHONY: clean
clean: ## Remove build and coverage output
	rm -rf bin coverage.out coverage.html acceptance.xml
