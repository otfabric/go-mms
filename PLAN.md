# go-mms — Roadmap

This file tracks all remaining work to make `go-mms` production-grade.

The master IEC 61850 interop roadmap lives in `go-iec61850/PLAN.md`.
This file focuses on the MMS transport and protocol layer.

**Current release: v1.0.1**
- Bidirectional libiec61850 MMS interop established.
- `SetVariableRead` added for runtime read-function override.

---

## Milestone M1 — Production MMS foundation (v1.1.0)

### Phase A — Transport and association hardening

#### A1. COTP/TPKT lifecycle ⬜

Black-box and fault-injection tests for the transport layer:

- [ ] Clean TCP close before COTP disconnect.
- [ ] COTP disconnect request and confirm.
- [ ] Remote reset during: connection setup, association, confirmed request, report delivery.
- [ ] Partial TPKT header (truncated mid-header).
- [ ] Partial COTP TPDU.
- [ ] Fragmented TCP delivery (one byte per read).
- [ ] Multiple TPKT packets coalesced into one TCP segment.
- [ ] Invalid TPKT version or length field.
- [ ] Invalid COTP header length.
- [ ] Invalid or unexpected COTP TPDU type.
- [ ] Oversized packet rejection.
- [ ] Context cancellation during blocked read and write.
- [ ] Read and write deadline enforcement.
- [ ] Reconnection after every failure category above.

Tests live in `go-cotp` and `transport/iso/`. Use the packet-fragmenting proxy
from `mms-interop` when testing against live adapters.

#### A2. Session, presentation, and ACSE ⬜

- [ ] Unsupported application context → clean rejection.
- [ ] Invalid presentation-context IDs.
- [ ] Unsupported transfer syntax.
- [ ] Missing or malformed ACSE fields.
- [ ] Association rejection propagation to caller.
- [ ] Peer abort handling.
- [ ] Local abort.
- [ ] Conclude timeout.
- [ ] Association timeout.
- [ ] Duplicate associate request on an active connection.
- [ ] Confirmed request arriving before association is complete.
- [ ] Server cleanup after association fails halfway.

#### A3. Resource ownership ⬜

Prove deterministic cleanup:

- [ ] Every connection owns exactly one association state machine.
- [ ] `Client.Close` releases: goroutines, sockets, timers, pending invoke IDs.
- [ ] `Server` shutdown terminates active associations deterministically.
- [ ] Reconnection creates fresh protocol state; no stale invoke IDs survive.
- [ ] Goroutine leak test using `goleak` or equivalent after every test.
- [ ] File-descriptor leak test on Linux.

**Required tooling:** `go test -race ./...`, goroutine leak checks, fd leak checks.

**Exit criterion for Phase A:** No hangs, leaks, panics, stale state, or ambiguous errors under transport and association disruption.

---

### Phase B — Production MMS semantics

#### B1. Confirmed-request state machine ✅

Already implemented and tested:
- Multiple outstanding requests supported via `internal/invoke.Tracker` (`pending` map).
- Out-of-order responses: each invoke ID has its own response channel.
- Invoke-ID wraparound at `uint32` max — skips 0, wraps to 1 (`TestInvokeIDWraparound` added).
- Invoke-ID exhaustion: `maxPending` enforced (`TestAllocateLimit` covers).
- Duplicate response: `Complete` returns false for unknown/already-delivered IDs (`TestResponseForUnknownInvokeID` added).
- Response for unknown invoke ID: does not block or panic (`TestResponseForUnknownInvokeID` added).
- Late response after caller cancelled: does not block or panic (`TestLateResponseAfterCancel` added).
- Connection loss cancels all pending requests (`TestCloseDuringInFlightRequest`).
- Concurrent reads from multiple goroutines (`TestConcurrentReads`).

Remaining:
- [ ] `MaxOutstandingCalling` enforcement test: hold N requests in flight, verify N+1 fails.
- [ ] Out-of-order confirmed responses test using mock server (multi-goroutine race).

#### B2. Negotiated limits ⬜

`MaxOutstandingCalling`, `MaxOutstandingCalled`, `MaxPDUSize`, `DataStructureNestingLevel` are decoded and stored. Tests that verify enforcement at runtime:

- [ ] `MaxOutstandingCalling=2`: launch 3 concurrent requests, verify 3rd blocked/rejected.
- [ ] `MaxPDUSize`: send a PDU exceeding the negotiated size → server/client rejects.
- [ ] Peer proposes values below local defaults → accepted (negotiate down).
- [ ] Peer proposes values above local limits → clamped.

#### B3. Data-type completeness ✅ (partially)

Fixture currently covers: boolean, integer, unsigned, float32, visible-string, octet-string, bit-string, UTC time, array, structure. All have interop round-trip tests.

Remaining:
- [ ] Float32 NaN/±infinity policy test.
- [ ] Bit string not divisible by 8 — boundary test.
- [ ] Deeply nested array/structure (at `DataStructureNestingLevel` limit).

#### B4. Error mapping ✅

Complete structured error taxonomy in `errors.go`:
- `ErrClosed`, `ErrInvokeTimeout`, `ErrConnectionRejected`, `ErrAssociationFailed`, `ErrNegotiationFailed`
- `ErrInvalidPDU`, `ErrDecodeFailed`, `ErrUnsupported`, `ErrServiceRejected`, `ErrDataAccess`, `ErrProtocol`
- Typed: `ServiceError`, `DecodeError`, `DataAccessError`, `ProtocolError`, `AuthenticationError`
- All sentinel errors are wrapped so `errors.Is` / `errors.As` work correctly.

#### B5. InformationReport ✅

Already implemented and tested:
- `OnInformationReport` handler registration (`TestServerInformationReport`).
- Reports interleaved with confirmed responses (`TestInfoReportConcurrentWithConfirmed`).
- Multiple back-to-back reports (`TestServerBroadcast`).
- Handler panic containment (`TestInfoReportHandlerPanicDoesNotKillClient`).
- Unknown report structure → log warning, association survives (`TestInfoReportNoHandler`).
- Server `SendInformationReport` and `Broadcast` (`TestServerInformationReport`, `TestServerBroadcast`).

Remaining:
- [ ] Interop test: go server sends InformationReport to libiec61850 MMS client (requires adapter extension).
- [ ] Slow consumer test: handler blocks for 1s, verify confirmed requests still work.

#### B6. File and journal services ✅

Already implemented:
- `FileProvider` and `JournalProvider` interfaces let applications opt in.
- When `nil`, all file/journal requests return `errServiceUnsupported` (protocol-correct).
- `FileProvider` and `JournalProvider` are documented in `server_options.go`.

No further work required for the production release claim.

**Exit criterion for Phase B:** `go-mms` safely supports all request/response/error/unsolicited patterns needed by `go-iec61850`, with deterministic concurrency and lifecycle behavior.

---

## Milestone M1 exit criteria

- [ ] Phase A: no hangs/leaks/panics under transport disruption.
- [ ] Phase B: all confirmed-service state machines proven; unsolicited dispatch proven.
- [ ] `go test -race ./...` clean.
- [ ] Goroutine leak gate added to CI.
- [ ] Structured error types stabilized.
- [ ] All Phase B interop tests pass against pinned adapter images.
- [ ] RELEASE.md entry for v1.1.0 written.

---

## Deferred (not blocking v1.1.0)

| Item | Reason |
|------|--------|
| GOOSE / Sampled Values transport | Different domain; separate repo. |
| File/journal MMS services | Not needed for IEC 61850 MMS claim. |
| Full ISO 9506 service coverage | Out of scope for the current profile. |
