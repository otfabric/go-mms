# Release Checklist

Pre-release verification for go-mms.

## Tests

- [x] All tests green: `go test ./...`
- [x] Race detector green: `go test -race ./...`
- [x] Repeated run stable (no flaky tests): `go test -race -count=5 ./...`
- [x] Coverage at 82.4%
- [x] `make check` passes (vet + lint + tests + coverage)

## Fuzzing

- [x] 33 fuzz targets defined across 6 packages
- [x] Seed corpus compiles and runs for all targets
- [ ] Extended fuzz run (30+ minutes per target) — recommended before release

## Golden Fixtures

- [x] 33 golden fixture files in `testdata/golden/`
- [x] Fixture-driven tests pass
- [x] Regeneration possible via `-update-golden` flag

## Documentation

- [x] README.md — product-facing entry point
- [x] doc.go — comprehensive package documentation
- [x] transport/iso doc.go — transport package docs
- [x] COMPLIANCE.md — service support matrix
- [x] ERRORS.md — error taxonomy with examples
- [x] OBSERVABILITY.md — logging and tracing guide
- [x] OWNERSHIP.md — memory ownership model
- [x] KNOWN_LIMITATIONS.md — explicit boundaries
- [x] API_REVIEW.md — exported symbol inventory
- [x] LIMITS.md — resource limits
- [x] TIMEOUT_AND_CLOSE.md — shutdown semantics
- [x] RACE_NOTES.md — concurrency model
- [x] HARDENING_DECODER_CHECKLIST.md — decoder audit results
- [x] INTEROP.md — interop testing guide

## Examples

- [x] `_examples/basic/` builds and is documented
- [x] `_examples/server-basic/` builds and is documented
- [x] `_examples/loopback/` builds and is documented

## Hardening

- [x] A1: Decoder strictness audit — all decode paths strict or documented lenient
- [x] A2: Ownership audit — no aliasing bugs, file read data copied
- [x] A3: Context/timeout/shutdown audit — deterministic lifecycle
- [x] A4: Race hardening — stress tests, race detector clean
- [x] A5: Bounds/limits — PDU size, nesting depth, collection caps

## API

- [x] Public API inventoried (API_REVIEW.md)
- [x] Deprecated symbols marked (`ReadArrayElement`, abbreviated constants)
- [x] Doc comments improved on key types
- [x] Zero-value safe option structs verified

## Interop

- [x] Interop test harness created (`interop/`)
- [x] C reference implementation available (`sources/`)
- [ ] First green interop scenario — requires C server build

## Outstanding items before 1.0

1. Run extended fuzzing (30+ minutes per target)
2. Execute at least one green interop scenario against C server
3. Decide on license
4. Tag version

## Version recommendation

See E2 discussion below. Recommended: **v0.1.0** (pre-1.0, experimental but serious).
