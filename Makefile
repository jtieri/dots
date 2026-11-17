# Run "make" to list the targets.

.DEFAULT_GOAL := help

BINARY := bin/dots

.PHONY: help build install test test-race cover lint fmt vet tidy check vuln hooks clean

help: ## Show this list
	@awk 'BEGIN { FS = ":.*## " } /^[a-zA-Z0-9_-]+:.*## / { printf "  %-10s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

build: ## Build ./bin/dots
	go build -o $(BINARY) .

install: ## Install dots into your Go bin directory
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

check: ## Run the checks CI runs, without the network; writes coverage.out
	go mod tidy -diff
	go vet ./...
	golangci-lint run
	go test -race -covermode=atomic -coverprofile=coverage.out ./...

vuln: ## Check for known vulnerabilities in the code dots calls (needs the network)
	govulncheck ./...

hooks: ## Run make check before every git push from this clone
	git config core.hooksPath .githooks

clean: ## Remove build output
	rm -rf bin coverage.out
