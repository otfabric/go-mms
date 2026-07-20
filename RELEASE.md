# go-mms Releases

## v1.0.1

**Added**: `Server.SetVariableRead(domain, itemID, fn)` — replaces the read handler of a previously registered domain-scoped variable at runtime. Intended for post-registration configuration such as installing control-model-specific read handlers on CO attributes (e.g. `SBO[CO]` for SBO-normal controls) after the server model has been fully built.

---

## v1.0.0

**Changed**: `DataAccessErrorCode` constants realigned to ISO 9506-2 wire values.

The constants were previously offset by one from the MMS ASN.1 `data-access-error` ENUMERATED. They now match the wire encoding exactly (0–11). `DataAccessErrorNone` is now `-1`, a Go-internal sentinel that is never encoded on the wire. Names have been corrected: `DataAccessErrorTypeMismatch` → `DataAccessErrorTypeUnsupported`, `DataAccessErrorObjectExists` → `DataAccessErrorObjectAttributeInconsistent`. New constants added: `DataAccessErrorObjectNonExistent` (10), `DataAccessErrorObjectValueInvalid` (11). Old names are retained as deprecated aliases.

**Changed**: `AccessResult` and `WriteAccessResult` semantics clarified.

- `AccessResult.Value == nil` is now the authoritative signal for a per-variable error (previously `ErrorCode != 0`, which silently swallowed wire value 0 = `object-invalidated`).
- `AccessResult.ErrorCode` is `DataAccessErrorNone` on success and a validated wire code on error.
- `WriteAccessResult.ErrorCode` follows the same invariant.

**Added**: `encodeDataAccessError` / `decodeDataAccessError` helpers with validation.

All paths that previously cast raw wire integers to `DataAccessErrorCode` now go through `decodeDataAccessError`, which rejects out-of-range values. The server uses `wireErrCode()` (panics on sentinel/invalid codes) to catch accidental misuse at development time.

**Fixed**: BER tag correctness across Read, Write, and NVL PDUs.

- `ReadRequest.variableAccessSpecification` is now wrapped in the `[1] EXPLICIT` outer tag (`0xa1`) as required by the MMS ASN.1 definition.
- `ReadResponse.listOfAccessResult` now uses `[1] IMPLICIT` tag `0xa1` instead of the legacy universal `SEQUENCE` (`0x30`).
- `WriteRequest.listOfData` now uses `[0] IMPLICIT` tag `0xa0` instead of `0x30`.
- `VariableSpecification.name` is now wrapped in `[0] EXPLICIT` (`0xa0`) per ISO 9506-2 §15.4.
- `DeleteNamedVariableList-Response` fields `numberMatched`/`numberDeleted` now use `[0] IMPLICIT` (`0x80`) and `[1] IMPLICIT` (`0x81`) instead of `UNIVERSAL INTEGER` (`0x02`).
- `InitiateRequestPDU` and `InitiateResponsePDU` are now encoded as bare SEQUENCE fields (IMPLICIT outer tag) rather than double-wrapped; a compatibility path accepts both forms on decode.
- `DefineNamedVariableList-Response` is now encoded as primitive (`0x8b 0x00`) rather than constructed.

**Changed**: `codec.UnmarshalInner` replaced by `codec.UnmarshalImplicitSequence` and `codec.UnmarshalExplicit`.

`UnmarshalImplicitSequence` reconstructs the `0x30` wrapper that Go's `encoding/asn1` requires when decoding an IMPLICIT SEQUENCE. `UnmarshalExplicit` handles the EXPLICIT case where `raw.Bytes` already contains the full inner TLV. The old name is gone; all call sites have been updated.

**Added**: `codec.MarshalSequenceContent` and `codec.MarshalMmsPduBareSequence`.

`MarshalSequenceContent` strips the `0x30` wrapper from an `asn1.Marshal` result, producing bare SEQUENCE fields for IMPLICIT tags. `MarshalMmsPduBareSequence` is the MMS PDU entry point for IMPLICIT-tagged PDUs.

**Added**: `pdu.MarshalIdentifyResponse`, `pdu.MarshalStatusResponse`.

Server-side marshal helpers for Identify and Status responses. Both emit bare SEQUENCE fields (IMPLICIT tag). The `server.go` and `mms_test.go` mock server now use these instead of duplicating the ASN.1 struct inline.

**Changed**: `pdu.MarshalWriteResponse` signature.

The parameter type changed from `[]int` (integer sentinel) to `[]pdu.WriteResult` (`{Success bool; Code int}`), eliminating the ambiguity between wire value 0 (`object-invalidated`) and a success sentinel.

**Fixed**: Server-side ConfirmedError access-class codes.

`svcErrObjectNonExistent` corrected from `0` to `2` and `svcErrObjectAccessDenied` from `1` to `3`, matching the `mms-extended.asn1` `AccessError` ENUMERATED.

**Added**: `ServerConnFromContext` exported helper.

Exports the previously unexported `serverConnFromCtx` so that variable read/write handlers can access the `*ServerConn` from the request context.

**Changed**: COTP transport delegated to `go-cotp` v1.0.0.

The hand-rolled COTP CR/CC handshake, TPKT framing, and `cotpTransport` implementation have been replaced by `cotp.Connect` / `cotp.Accept` and `cotp.Conn.WriteTSDU` / `ReadTSDU`. The internal `sendTPDU`/`readTPDU` helpers, `tpkt.Reader`/`tpkt.Writer`, and per-listener `sourceRef` counter are removed. Error types from handshake failures are now `cotp.ErrConnectionRefused`, `cotp.ErrHandshake`, and `*cotp.RejectionError`.

**Changed**: Dependency updates.

- `github.com/otfabric/go-cotp` v0.1.5 → v1.0.1
- `github.com/otfabric/go-tpkt` v0.1.3 → v1.0.0 (indirect)

**Added**: Bidirectional interoperability test suite against libiec61850.

`interop/` now contains a self-contained harness (`harness_test.go`) and two test files:
- `libiec61850_client_test.go` — go-mms client ↔ libiec61850-mms-server adapter: 21 scenarios covering all primitive types, array, structure, multi-variable read/write, NVL define/read/delete, negative cases, and reconnect.
- `libiec61850_server_test.go` — libiec61850-mms-client ↔ go-mms server: matching bidirectional coverage.

Tests start Docker containers from the [mms-interop](https://github.com/otfabric/mms-interop) adapter images, wait for a JSON readiness event, run assertions, and tear down. No pre-running containers or manual steps are required. Gated behind `-tags=interop`; run with `make interop`.

**Added**: `make interop` target and CI workflow.

`Makefile` gains an `interop` target (`go test -tags=interop -v -timeout 300s ./interop/...`). `.github/workflows/interop.yml` runs the suite on every push and PR using a pinned digest for the libiec61850 adapter image pulled from GHCR.

**Added**: `interop/testdata/interop.json` fixture.

Canonical fixture defining the `interop` domain with ten typed variables (`boolean`, `integer`, `unsigned`, `float32`, `visible-string`, `octet-string`, `bit-string`, `utc-time`, `array`, `structure`) and server identity. Synchronized with the pinned adapter image version.

**Changed**: `INTEROP.md` rewritten; `interop/README.md` updated.

Both documents now describe the Docker-based harness architecture, environment variables (`LIBIEC61850_IMAGE`, `MMS_SERVER_BINARY`, `MMS_CLIENT_BINARY`, `MMS_FIXTURE`), and the full MMS compatibility matrix (28 capabilities, bidirectional).

---

## v0.1.5

**Changed**: Open-source release under the MIT License.

- Added root `LICENSE` file (MIT, Copyright (c) 2026 OT Fabric).
- Added `// SPDX-License-Identifier: MIT` to all first-party Go source files.
- Updated README license section to reference the MIT License.

**Changed**: README improvements for public release.

- Standardized badge block (Go, pkg.go.dev, License, CI, Codecov, Release).
- Added pkg.go.dev documentation badge.
- Removed Go Report Card badge and Codecov upload token from badge URLs.
- Added table of contents.

**Changed**: Dependency updates.

- `github.com/otfabric/go-cotp` v0.1.4 → v0.1.5
- `github.com/otfabric/go-tpkt` v0.1.2 → v0.1.3

No API or behavior changes.

---

## v0.1.4

**Changed**: Increased minimum required Go version to 1.23 (was 1.21). All documentation, CI, and go.mod references updated accordingly. No code changes.

---

## v0.1.3

**Fixed**: Race condition in `Client.Close` / conclude handshake.

When the reader loop received a `ConcludeResponse`, it signaled `concludeCh` and then returned (closing `readerDone`). Because Go's `select` picks randomly among ready cases, `conclude()` could non-deterministically hit the `readerDone` case and return a spurious `"connection closed before conclude response"` error. The fix drains `concludeCh` when `readerDone` fires before reporting failure.

**Changed**: Lint configuration and code quality.

- Restored sensible test-file exclusions in `.golangci.yml` for `errcheck` and `staticcheck` SA2002.
- Fixed `godot` comment punctuation in `internal/acse` and `internal/presentation`.
- Fixed `exhaustive` switch in `internal/acse` (missing `default` case for `AuthMechanism`).
- Fixed `errcheck` in `transport/iso` (unchecked `SetDeadline` / `SetWriteDeadline` / `SetReadDeadline` return values).
- Fixed `staticcheck` ST1023 redundant type declarations in test files.
- `make check` now passes cleanly.

---

## v0.1.2

**Changed**: Lowered minimum required Go version to 1.21 (was 1.22). All documentation, CI, and go.mod references updated accordingly. No code changes.

This release ensures compatibility with Go 1.21. No new features or bugfixes are included.

---

## v0.1.1

**Changed**: Add UTCTime Quality Byte Support — completed across all 4 layers.

- PDU layer (internal/pdu/data.go): Added TimeQuality uint8 to DataValue. Updated encodeUTCTime to write the quality byte and decodeUTCTime to return it (previously hardcoded 0x00 / discarded).
- Public Value type (value.go): Added timeQuality field, UTCTimeQuality() accessor, and NewUTCTimeWithQuality(t, quality) constructor. Updated NewUTCTime(t) to default to 0x0a (matching C reference). Updated Clone, Equal, and String to handle quality.
- Wire conversion (mms.go): Updated valueToDataValue and dataValueToValue to thread the quality byte between public and internal representations.
- Public constants (value.go): Added 5 named constants — UTCTimeQualityLeapSecondsKnown (0x80), UTCTimeQualityClockFailure (0x40), UTCTimeQualityClockNotSynchronized (0x20), UTCTimeQualityAccuracyMask (0x1F), UTCTimeQualityAccuracyUnspecified (0x1F).

---

## v0.1.0

**Changed**: N/A

Initial release.

---
