SHELL := /bin/bash

GO       ?= go
PKGS     := ./...
FUZZTIME ?= 15s

.PHONY: help test test-race test-verbose vet lint fmt fuzz fuzz-ber fuzz-pdu bench tidy check clean coverage coverage-html coverage-clean interop ai-print-all ai-print-test ai-print ai-diff ai-digest ai-context

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*## "}; /^[a-zA-Z0-9_.-]+:.*## / {printf "%-16s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

test: ## Run unit tests
	$(GO) test $(PKGS)

interop: ## Run interoperability tests against mms-interop adapter images (-tags=interop).
	$(GO) test -tags=interop -v -timeout 300s ./interop/...

test-verbose: ## Run unit tests with verbose output
	$(GO) test -v $(PKGS)

test-race: ## Run tests with race detector
	$(GO) test -race $(PKGS)

vet: ## Run go vet
	$(GO) vet $(PKGS)

lint: ## Run golangci-lint (install: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run $(PKGS)

fmt: ## Format all Go source files
	@gofmt -w .

coverage: ## Run tests with coverage profile and text summary
	$(GO) test -coverprofile=coverage.out $(PKGS)
	$(GO) tool cover -func=coverage.out | tee coverage.txt

coverage-html: coverage ## Generate HTML coverage report
	$(GO) tool cover -html=coverage.out -o coverage.html

coverage-clean: ## Remove coverage artifacts
	rm -f coverage.out coverage.txt coverage.html

fuzz: ## Run all fuzz targets for FUZZTIME each (default 15s)
	@echo "=== Fuzzing berutil ==="
	$(GO) test -fuzz=FuzzDecodeTLV      -fuzztime=$(FUZZTIME) ./internal/berutil
	$(GO) test -fuzz=FuzzDecodeLength    -fuzztime=$(FUZZTIME) ./internal/berutil
	$(GO) test -fuzz=FuzzDecodeInteger   -fuzztime=$(FUZZTIME) ./internal/berutil
	$(GO) test -fuzz=FuzzDecodeUnsigned  -fuzztime=$(FUZZTIME) ./internal/berutil
	@echo "=== Fuzzing pdu ==="
	$(GO) test -fuzz=FuzzDecodeTypeSpec                   -fuzztime=$(FUZZTIME) ./internal/pdu
	$(GO) test -fuzz=FuzzDecodeObjectName                 -fuzztime=$(FUZZTIME) ./internal/pdu
	$(GO) test -fuzz=FuzzUnmarshalDataElement              -fuzztime=$(FUZZTIME) ./internal/pdu
	$(GO) test -fuzz=FuzzUnmarshalAccessResults            -fuzztime=$(FUZZTIME) ./internal/pdu
	$(GO) test -fuzz=FuzzDecodePdu                         -fuzztime=$(FUZZTIME) ./internal/pdu
	$(GO) test -fuzz=FuzzDecodeConfirmedError              -fuzztime=$(FUZZTIME) ./internal/pdu
	$(GO) test -fuzz=FuzzDecodeRejectPDU                   -fuzztime=$(FUZZTIME) ./internal/pdu
	$(GO) test -fuzz=FuzzDecodeConfirmedResponse           -fuzztime=$(FUZZTIME) ./internal/pdu
	$(GO) test -fuzz=FuzzUnmarshalReadResponse             -fuzztime=$(FUZZTIME) ./internal/pdu
	$(GO) test -fuzz=FuzzUnmarshalWriteResponse            -fuzztime=$(FUZZTIME) ./internal/pdu
	$(GO) test -fuzz=FuzzUnmarshalGetNameListResponse      -fuzztime=$(FUZZTIME) ./internal/pdu
	$(GO) test -fuzz=FuzzUnmarshalGetVarAccessResponse     -fuzztime=$(FUZZTIME) ./internal/pdu
	$(GO) test -fuzz=FuzzUnmarshalGetNamedVarListAttrsResponse -fuzztime=$(FUZZTIME) ./internal/pdu
	$(GO) test -fuzz=FuzzUnmarshalDeleteNamedVarListResponse   -fuzztime=$(FUZZTIME) ./internal/pdu

fuzz-ber: ## Fuzz only BER utility decoders
	$(GO) test -fuzz=FuzzDecodeTLV      -fuzztime=$(FUZZTIME) ./internal/berutil
	$(GO) test -fuzz=FuzzDecodeLength    -fuzztime=$(FUZZTIME) ./internal/berutil
	$(GO) test -fuzz=FuzzDecodeInteger   -fuzztime=$(FUZZTIME) ./internal/berutil
	$(GO) test -fuzz=FuzzDecodeUnsigned  -fuzztime=$(FUZZTIME) ./internal/berutil

fuzz-pdu: ## Fuzz only PDU decoders
	$(GO) test -fuzz=FuzzDecodeTypeSpec                   -fuzztime=$(FUZZTIME) ./internal/pdu
	$(GO) test -fuzz=FuzzDecodeObjectName                 -fuzztime=$(FUZZTIME) ./internal/pdu
	$(GO) test -fuzz=FuzzUnmarshalDataElement              -fuzztime=$(FUZZTIME) ./internal/pdu
	$(GO) test -fuzz=FuzzUnmarshalAccessResults            -fuzztime=$(FUZZTIME) ./internal/pdu
	$(GO) test -fuzz=FuzzDecodePdu                         -fuzztime=$(FUZZTIME) ./internal/pdu
	$(GO) test -fuzz=FuzzDecodeConfirmedError              -fuzztime=$(FUZZTIME) ./internal/pdu
	$(GO) test -fuzz=FuzzDecodeRejectPDU                   -fuzztime=$(FUZZTIME) ./internal/pdu
	$(GO) test -fuzz=FuzzDecodeConfirmedResponse           -fuzztime=$(FUZZTIME) ./internal/pdu
	$(GO) test -fuzz=FuzzUnmarshalReadResponse             -fuzztime=$(FUZZTIME) ./internal/pdu
	$(GO) test -fuzz=FuzzUnmarshalWriteResponse            -fuzztime=$(FUZZTIME) ./internal/pdu
	$(GO) test -fuzz=FuzzUnmarshalGetNameListResponse      -fuzztime=$(FUZZTIME) ./internal/pdu
	$(GO) test -fuzz=FuzzUnmarshalGetVarAccessResponse     -fuzztime=$(FUZZTIME) ./internal/pdu
	$(GO) test -fuzz=FuzzUnmarshalGetNamedVarListAttrsResponse -fuzztime=$(FUZZTIME) ./internal/pdu
	$(GO) test -fuzz=FuzzUnmarshalDeleteNamedVarListResponse   -fuzztime=$(FUZZTIME) ./internal/pdu

bench: ## Run all benchmarks
	$(GO) test -run=^$$ -bench=. -benchmem $(PKGS)

tidy: ## Tidy and verify module files
	$(GO) mod tidy
	$(GO) mod verify

check: fmt tidy vet lint test test-race coverage ## Run all pre-commit checks

clean: coverage-clean ## Clean test cache and coverage artifacts
	$(GO) clean -testcache

ai-print-all: ## Print all Go files (code + tests)
	@find . -type f -name '*.go' -print0 | sort -z | \
	while IFS= read -r -d '' f; do \
		echo "=== START $$f ==="; \
		cat "$$f"; \
		echo; \
		echo "=== END $$f ==="; \
	done

ai-print-test: ## Print all Go files (tests)
	@find . -type f -name '*_test.go' -print0 | sort -z | \
	while IFS= read -r -d '' f; do \
		echo "=== START $$f ==="; \
		cat "$$f"; \
		echo; \
		echo "=== END $$f ==="; \
	done

ai-print: ## Print all Go files (code only no tests)
	@find . -type f -name '*.go' ! -name '*_test.go' -print0 | sort -z | \
	while IFS= read -r -d '' f; do \
		echo "=== START $$f ==="; \
		cat "$$f"; \
		echo; \
		echo "=== END $$f ==="; \
	done

ai-diff: ## Diff against main, including untracked files, plus full changed Go file dump
	@mkdir -p ai
	@files=$$( \
		{ \
			git diff --name-only main; \
			git ls-files --others --exclude-standard; \
		} | grep -E '\.go$$' | grep -v '^ai/changes\.patch$$' | grep -v '^ai/changed\.full$$' | sort -u \
	); \
	{ \
		git diff --binary main; \
		for f in $$(git ls-files --others --exclude-standard | grep -v '^ai/changes\.patch$$' | grep -v '^ai/changed\.full$$'); do \
			if [ -f "$$f" ]; then \
				echo; \
				echo "=== START NEW FILE: $$f ==="; \
				cat "$$f"; \
				echo; \
				echo "=== END NEW FILE: $$f ==="; \
			fi; \
		done; \
	} > ai/changes.patch; \
	{ \
		for f in $$files; do \
			if [ -f "$$f" ]; then \
				echo "=== START $$f ==="; \
				cat "$$f"; \
				echo; \
				echo "=== END $$f ==="; \
				echo; \
			fi; \
		done; \
	} > ai/changed.full
	@echo "Written ai/changes.patch"
	@echo "Written ai/changed.full"

ai-digest: ## Generate repo digest (structure only)
	@echo "# Repo Digest" > ai/digest.md
	@echo "" >> ai/digest.md
	@echo "## Files" >> ai/digest.md
	@for f in $(GOFILES); do \
		echo "- $$f" >> ai/digest.md; \
	done
	@echo "" >> ai/digest.md
	@echo "## Packages" >> ai/digest.md
	@go list ./... >> ai/digest.md 2>/dev/null || true
	@echo "Written ai/digest.md"

ai-context: ## Compress context
	@echo "Summarize progress.md into max 30 lines." > ai/context.md
	@echo "Then append decisions and current state." >> ai/context.md
	@echo "" >> ai/context.md
	@cat ai/progress.md >> ai/context.md
	@echo "Written ai/context.md"