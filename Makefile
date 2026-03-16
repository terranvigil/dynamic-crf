SHELL := /bin/bash

GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOBINPATH=`go env GOPATH`/bin
GOFUMPTPATH=$(GOBINPATH)/gofumpt
GOLANGCI_LINT_VER="2.11.3"

BINARY_NAME=dynamic-crf

.PHONY: build test test-integration fmt lint clean vendor run test-fixtures

build:
	$(GOBUILD) -o $(BINARY_NAME) -v ./cmd/dynamic_crf.go
	chmod +x $(BINARY_NAME)

test:
	$(GOTEST) -v -race -count=1 -parallel=4 -tags=unit ./...

test-integration:
	$(GOTEST) -v -race -count=1 -parallel=4 -tags=integration ./...

fmt:
	$(GOFUMPTPATH) -w .

lint:
ifneq (${GOLANGCI_LINT_VER}, "$(shell golangci-lint version --format short 2>&1)")
	./install-golangcilint.sh v${GOLANGCI_LINT_VER}
endif
	$(GOBINPATH)/golangci-lint --timeout 3m run --allow-parallel-runners

clean:
	$(GOCLEAN)
	rm -f $(BINARY_NAME)

vendor:
	$(GOCMD) mod tidy
	$(GOCMD) mod vendor

test-fixtures:
	./scripts/setup-test-fixtures.sh

run: build
	./$(BINARY_NAME) $(ARGS)
