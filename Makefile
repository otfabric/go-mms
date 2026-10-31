SHELL := /bin/bash

# Avoid accidental go.work from a sibling checkout affecting module resolution.
export GOWORK := off

PKGS     := ./...
FUZZTIME ?= 15s

# Per-package and total floor (TEST-012), enforced by scripts/coverage-gate.sh.
# Lowest package when set: internal/pdu, 95.5 % with -coverpkg.
COVER_PKG_MIN := 90

.PHONY: help interop-all test test-race test-verbose vet lint fmt fuzz fuzz-short fuzz-ber fuzz-pdu chaos bench tidy check clean coverage coverage-check coverage-pkg-check coverage-html coverage-clean cross e2e interop test-armv7 vuln

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*## "}; /^[a-zA-Z0-9_.-]+:.*## / {printf "%-16s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

test: ## Run unit tests
	@echo "Running unit tests"
	@go test $(PKGS)

INTEROP_STACKS ?= libiec61850 dscsystems iec61850-rust iec61850bean

interop: ## Run interop tests against one peer stack (MMS_INTEROP_STACK, default libiec61850; -tags=interop).
	@echo "Running interoperability tests against $${MMS_INTEROP_STACK:-libiec61850}"
	@go test -tags=interop -v -count=1 -timeout 600s ./interop/...

interop-all: ## Run interop tests against every peer stack in INTEROP_STACKS.
	@set -e; for s in $(INTEROP_STACKS); do \
		echo "=== interop: $$s"; \
		MMS_INTEROP_STACK=$$s go test -tags=interop -count=1 -timeout 600s ./interop/...; \
	done

test-verbose: ## Run unit tests with verbose output
	@echo "Running tests with verbose output"
	@go test -v $(PKGS)

test-race: ## Run tests with race detector
	@echo "Running tests with race detector"
	@go test -race -timeout 20m $(PKGS)

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

coverage: ## Run tests with coverage and fail below COVER_PKG_MIN per package and in total
	@echo "Running coverage gate"
	@set -o pipefail; COVER_PKG_MIN=$(COVER_PKG_MIN) scripts/coverage-gate.sh coverage.out | tee coverage.txt

coverage-check: coverage ## Alias of coverage: fail below COVER_PKG_MIN per package and in total

coverage-pkg-check: coverage ## Alias of coverage: fail below COVER_PKG_MIN per package and in total

coverage-html: coverage ## Generate HTML coverage report
	@echo "Generating HTML coverage report"
	@go tool cover -html=coverage.out -o coverage.html

coverage-clean: ## Remove coverage artifacts
	@echo "Removing coverage artifacts"
	rm -f coverage.out coverage.txt coverage.html

fuzz: ## Run every fuzz target for FUZZTIME each (default 15s)
	@scripts/fuzz.sh

fuzz-short: ## Run every fuzz target for 2s (smoke test, part of check)
	@FUZZTIME=2s scripts/fuzz.sh

fuzz-ber: ## Fuzz only BER utility decoders
	@scripts/fuzz.sh ./internal/berutil ./internal/asn1util

fuzz-pdu: ## Fuzz only PDU decoders
	@scripts/fuzz.sh ./internal/pdu ./internal/codec

e2e: ## Run the end-to-end suite (go-mms client against go-mms server over TCP) under -race
	@echo "Running end-to-end tests"
	@go test -race -count=1 -timeout 20m ./e2e

CHAOS_DURATION ?= 30s
CHAOS_SEED     ?= 0

chaos: ## Run chaos and stress tests under -race for CHAOS_DURATION (default 30s) with a random seed (CHAOS_SEED=0)
	@echo "Running chaos tests ($(CHAOS_DURATION), seed $(CHAOS_SEED))"
	@MMS_CHAOS_DURATION=$(CHAOS_DURATION) MMS_CHAOS_SEED=$(CHAOS_SEED) \
		go test -race -run 'Chaos|Stress' -count=1 -v -timeout 30m ./e2e

bench: ## Run all benchmarks
	@echo "Running benchmarks"
	@go test -run=^$$ -bench=. -benchmem $(PKGS)

tidy: ## Tidy and verify module files
	@echo "Running go mod tidy"
	@go mod tidy
	@echo "Running go mod verify"
	@go mod verify

CROSS_PLATFORMS ?= linux/amd64 linux/arm64 linux/arm darwin/amd64 darwin/arm64 windows/amd64 windows/arm64 freebsd/amd64

cross: ## Cross-compile without cgo for CROSS_PLATFORMS (build only, tests do not run)
	@for p in $(CROSS_PLATFORMS); do \
		echo "Building $$p"; \
		CGO_ENABLED=0 GOOS=$${p%/*} GOARCH=$${p#*/} go build $(PKGS) || exit 1; \
	done

test-armv7: ## Compile internal/pdu for linux/armv7 (catches 32-bit int overflows)
	@echo "Compiling ./internal/pdu for GOOS=linux GOARCH=arm GOARM=7"
	@tmp=$$(mktemp); \
	GOOS=linux GOARCH=arm GOARM=7 go test -c -o "$$tmp" ./internal/pdu/; \
	rc=$$?; rm -f "$$tmp"; exit $$rc

check: fmt tidy vet lint vuln test test-race coverage fuzz-short test-armv7 ## Run all pre-commit checks

clean: coverage-clean ## Clean test cache and coverage artifacts
	@echo "Cleaning test cache and coverage artifacts"
	@go clean -testcache
