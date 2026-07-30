SHELL := /bin/bash

# Avoid accidental go.work from a sibling checkout affecting module resolution.
export GOWORK := off

PKGS     := ./...
FUZZTIME ?= 15s

.PHONY: help test test-race test-verbose vet lint fmt fuzz fuzz-ber fuzz-pdu bench tidy check clean coverage coverage-html coverage-clean interop test-armv7 vuln

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*## "}; /^[a-zA-Z0-9_.-]+:.*## / {printf "%-16s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

test: ## Run unit tests
	@echo "Running unit tests"
	@go test $(PKGS)

interop: ## Run interoperability tests against mms-interop adapter images (-tags=interop).
	@echo "Running interoperability tests"
	@go test -tags=interop -v -timeout 300s ./interop/...

test-verbose: ## Run unit tests with verbose output
	@echo "Running tests with verbose output"
	@go test -v $(PKGS)

test-race: ## Run tests with race detector
	@echo "Running tests with race detector"
	@go test -race $(PKGS)

vet: ## Run go vet
	@echo "Running go vet"
	@go vet $(PKGS)

lint: ## Run golangci-lint (install: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	@echo "Running golangci-lint"
	golangci-lint run $(PKGS)

vuln: ## Run govulncheck
	@echo "Running govulncheck"
	@govulncheck $(PKGS)

fmt: ## Format all Go source files
	@echo "Running gofmt"
	@gofmt -w .
	@echo "Running go fmt"
	@go fmt $(PKGS)

coverage: ## Run tests with coverage profile and text summary
	@echo "Running tests with coverage"
	@go test -coverprofile=coverage.out $(PKGS)
	@go tool cover -func=coverage.out | tee coverage.txt

coverage-html: coverage ## Generate HTML coverage report
	@echo "Generating HTML coverage report"
	@go tool cover -html=coverage.out -o coverage.html

coverage-clean: ## Remove coverage artifacts
	@echo "Removing coverage artifacts"
	rm -f coverage.out coverage.txt coverage.html

fuzz: ## Run all fuzz targets for FUZZTIME each (default 15s)
	@echo "=== Fuzzing berutil ==="
	@go test -fuzz=FuzzDecodeTLV       -fuzztime=$(FUZZTIME) ./internal/berutil
	@go test -fuzz=FuzzDecodeLength    -fuzztime=$(FUZZTIME) ./internal/berutil
	@go test -fuzz=FuzzDecodeInteger   -fuzztime=$(FUZZTIME) ./internal/berutil
	@go test -fuzz=FuzzDecodeUnsigned  -fuzztime=$(FUZZTIME) ./internal/berutil
	@echo "=== Fuzzing pdu ==="
	@go test -fuzz=FuzzDecodeTypeSpec                    -fuzztime=$(FUZZTIME) ./internal/pdu
	@go test -fuzz=FuzzDecodeObjectName                  -fuzztime=$(FUZZTIME) ./internal/pdu
	@go test -fuzz=FuzzUnmarshalDataElement              -fuzztime=$(FUZZTIME) ./internal/pdu
	@go test -fuzz=FuzzUnmarshalAccessResults            -fuzztime=$(FUZZTIME) ./internal/pdu
	@go test -fuzz=FuzzDecodePdu                         -fuzztime=$(FUZZTIME) ./internal/pdu
	@go test -fuzz=FuzzDecodeConfirmedError              -fuzztime=$(FUZZTIME) ./internal/pdu
	@go test -fuzz=FuzzDecodeRejectPDU                   -fuzztime=$(FUZZTIME) ./internal/pdu
	@go test -fuzz=FuzzDecodeConfirmedResponse           -fuzztime=$(FUZZTIME) ./internal/pdu
	@go test -fuzz=FuzzUnmarshalReadResponse             -fuzztime=$(FUZZTIME) ./internal/pdu
	@go test -fuzz=FuzzUnmarshalWriteResponse            -fuzztime=$(FUZZTIME) ./internal/pdu
	@go test -fuzz=FuzzUnmarshalGetNameListResponse      -fuzztime=$(FUZZTIME) ./internal/pdu
	@go test -fuzz=FuzzUnmarshalGetVarAccessResponse     -fuzztime=$(FUZZTIME) ./internal/pdu
	@go test -fuzz=FuzzUnmarshalGetNamedVarListAttrsResponse -fuzztime=$(FUZZTIME) ./internal/pdu
	@go test -fuzz=FuzzUnmarshalDeleteNamedVarListResponse   -fuzztime=$(FUZZTIME) ./internal/pdu

fuzz-ber: ## Fuzz only BER utility decoders
	@echo "Fuzzing BER utility decoders"
	@go test -fuzz=FuzzDecodeTLV       -fuzztime=$(FUZZTIME) ./internal/berutil
	@go test -fuzz=FuzzDecodeLength    -fuzztime=$(FUZZTIME) ./internal/berutil
	@go test -fuzz=FuzzDecodeInteger   -fuzztime=$(FUZZTIME) ./internal/berutil
	@go test -fuzz=FuzzDecodeUnsigned  -fuzztime=$(FUZZTIME) ./internal/berutil

fuzz-pdu: ## Fuzz only PDU decoders
	@echo "Fuzzing PDU decoders"
	@go test -fuzz=FuzzDecodeTypeSpec                   -fuzztime=$(FUZZTIME) ./internal/pdu
	@go test -fuzz=FuzzDecodeObjectName                 -fuzztime=$(FUZZTIME) ./internal/pdu
	@go test -fuzz=FuzzUnmarshalDataElement              -fuzztime=$(FUZZTIME) ./internal/pdu
	@go test -fuzz=FuzzUnmarshalAccessResults            -fuzztime=$(FUZZTIME) ./internal/pdu
	@go test -fuzz=FuzzDecodePdu                         -fuzztime=$(FUZZTIME) ./internal/pdu
	@go test -fuzz=FuzzDecodeConfirmedError              -fuzztime=$(FUZZTIME) ./internal/pdu
	@go test -fuzz=FuzzDecodeRejectPDU                   -fuzztime=$(FUZZTIME) ./internal/pdu
	@go test -fuzz=FuzzDecodeConfirmedResponse           -fuzztime=$(FUZZTIME) ./internal/pdu
	@go test -fuzz=FuzzUnmarshalReadResponse             -fuzztime=$(FUZZTIME) ./internal/pdu
	@go test -fuzz=FuzzUnmarshalWriteResponse            -fuzztime=$(FUZZTIME) ./internal/pdu
	@go test -fuzz=FuzzUnmarshalGetNameListResponse      -fuzztime=$(FUZZTIME) ./internal/pdu
	@go test -fuzz=FuzzUnmarshalGetVarAccessResponse     -fuzztime=$(FUZZTIME) ./internal/pdu
	@go test -fuzz=FuzzUnmarshalGetNamedVarListAttrsResponse -fuzztime=$(FUZZTIME) ./internal/pdu
	@go test -fuzz=FuzzUnmarshalDeleteNamedVarListResponse   -fuzztime=$(FUZZTIME) ./internal/pdu

bench: ## Run all benchmarks
	@echo "Running benchmarks"
	@go test -run=^$$ -bench=. -benchmem $(PKGS)

tidy: ## Tidy and verify module files
	@echo "Running go mod tidy"
	@go mod tidy
	@echo "Running go mod verify"
	@go mod verify

test-armv7: ## Compile internal/pdu for linux/armv7 (catches 32-bit int overflows)
	@echo "Compiling ./internal/pdu for GOOS=linux GOARCH=arm GOARM=7"
	@tmp=$$(mktemp); \
	GOOS=linux GOARCH=arm GOARM=7 go test -c -o "$$tmp" ./internal/pdu/; \
	rm -f "$$tmp"

check: fmt tidy vet lint vuln test test-race coverage test-armv7 ## Run all pre-commit checks

clean: coverage-clean ## Clean test cache and coverage artifacts
	@echo "Cleaning test cache and coverage artifacts"
	@go clean -testcache
