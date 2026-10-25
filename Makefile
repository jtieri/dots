# Run "make" to list the targets.

.DEFAULT_GOAL := help

BINARY := bin/dot

.PHONY: help build install test test-race cover lint fmt vet tidy check clean

help: ## Show this list
	@awk 'BEGIN { FS = ":.*## " } /^[a-zA-Z0-9_-]+:.*## / { printf "  %-10s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

build: ## Build ./bin/dot
	go build -o $(BINARY) .

install: ## Install dot into your Go bin directory
	go install .

test: ## Run the tests
	go test ./...

test-race: ## Run the tests with the race detector and write coverage.out
	go test -race -covermode=atomic -coverprofile=coverage.out ./...

cover: test-race ## Open the coverage report in a browser
	go tool cover -html=coverage.out

lint: ## Run golangci-lint
	golangci-lint run

fmt: ## Format the code
	golangci-lint fmt

vet: ## Run go vet
	go vet ./...

tidy: ## Tidy go.mod and go.sum
	go mod tidy

check: ## Run everything CI runs
	go mod tidy -diff
	go vet ./...
	golangci-lint run
	go test -race ./...

clean: ## Remove build output
	rm -rf bin coverage.out
