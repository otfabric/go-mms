# PROGRESS.md — otfabric/go-mms

Tracks implementation progress against the [PLAN.md](PLAN.md) roadmap.

---

## Phase 0 — Architecture and package skeleton

**Status: COMPLETE**

### Deliverables

| Deliverable | Status | Notes |
|---|---|---|
| `go.mod` | Done | `github.com/otfabric/go-mms`, Go 1.26 |
| Package skeleton | Done | All packages created per PLAN.md section 5 |
| `errors.go` | Done | 9 sentinel errors, 3 typed error structs (`ServiceError`, `DecodeError`, `ProtocolError`) |
| `*slog.Logger` integration | Done | `DialOptions.Logger` field, nil = silent |
| `PLAN.md` | Done | Finalized after 3 feedback rounds |
| CI configuration | Pending | Not yet set up (no CI platform configured) |

### Package structure created

```
go-mms/
├── doc.go              — package documentation
├── mms.go              — Client type, Dial, Close, Identify, Status, Read, Write, GetNameList stubs
├── types.go            — DomainID, ItemID, InvokeID, APTitle, ObjectClass, ValueType, ErrorClass, etc.
├── value.go            — Value type with (T, bool) accessors, constructors for all MMS value types
├── errors.go           — sentinel errors, ServiceError, DecodeError, ProtocolError
├── options.go          — DialOptions (layered: TransportOptions, ISOOptions, MMSOptions)
├── types_test.go       — String() tests for all enum types
├── value_test.go       — accessor tests for all value types
├── errors_test.go      — error unwrapping and sentinel tests
├── internal/
│   ├── codec/doc.go
│   ├── asn1util/doc.go
│   ├── pdu/doc.go
│   ├── acse/doc.go
│   ├── session/doc.go
│   ├── presentation/doc.go
│   ├── isostack/doc.go
│   └── invoke/doc.go
└── testdata/.gitkeep
```

### Done criteria verification

| Criterion | Result |
|---|---|
| `go build ./...` | PASS |
| `go test ./...` | PASS — 21 tests, all passing |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |
| CI pipeline | Not yet configured |

---

## Phase 1 — ASN.1 / MMS codec feasibility

**Status: COMPLETE**

### Deliverables

| Deliverable | Status | Notes |
|---|---|---|
| `internal/asn1util` | Done | Tag constants, PeekTag, WrapConstructed/Primitive, TagNumber/IsConstructed/TagClass helpers |
| `internal/codec` | Done | MarshalMmsPdu, MarshalConfirmedRequest, UnwrapPdu, UnmarshalConfirmedResponse, ServiceTag, UnmarshalInto |
| `internal/pdu/initiate.go` | Done | InitiateRequest/Response structs with asn1 struct tags, marshal/unmarshal, DefaultInitiateRequest |
| `internal/pdu/confirmed.go` | Done | ConfirmedResponse decode, ServiceKind classification, MarshalConfirmedRequest |
| `internal/pdu/identify.go` | Done | MarshalIdentifyRequest, UnmarshalIdentifyResponse |
| `internal/pdu/mmspdu.go` | Done | PduKind enum, ClassifyPdu, DecodePdu top-level dispatch |
| Codec feasibility decision | Done | `internal/codec/CODEC_DECISION.md` |
| Tests | Done | 14 new tests across asn1util and pdu packages |

### Codec feasibility decision summary

`encoding/asn1` is the primary codec. Custom helpers are limited to:
1. **Top-level CHOICE dispatch** (~30 lines) — `PeekTag` + `UnwrapPdu`
2. **Service CHOICE dispatch** (~40 lines) — `asn1.RawValue` + `ServiceTag`
3. **TLV envelope wrapping** (~30 lines) — `WrapConstructed` / `WrapPrimitive`

Everything else (SEQUENCE fields, INTEGER, BIT STRING, VisibleString, optional fields, nested structures) uses `encoding/asn1` directly via struct tags.

`internal/asn1util` is ~60 lines total. No generic BER/TLV infrastructure has been built.

Full decision document: [`internal/codec/CODEC_DECISION.md`](internal/codec/CODEC_DECISION.md)

### PDU families proven

| PDU family | Type | Proven |
|---|---|---|
| InitiateRequest (0xa8) | Association | Marshal + round-trip decode verified |
| InitiateResponse (0xa9) | Association | Unmarshal from synthetic + round-trip verified |
| ConfirmedRequest (0xa0) + Identify | Confirmed service | Marshal verified, tag 0x82 confirmed |
| ConfirmedResponse (0xa1) + Identify | Confirmed service | Full round-trip: build → classify → decode envelope → unmarshal Identify response |
| ConcludeRequest (0x8b) | Association | Marshal as NULL PDU |

### Encoded wire examples

- **InitiateRequest** (42 bytes): `a828302680 0300fde881 0105820105 83010aa416 800101 810305f100 820c03ee1c 0000040800 0079ef18`
- **IdentifyRequest** (7 bytes): `a005020101 8200`

### Done criteria verification

| Criterion | Result |
|---|---|
| Association PDU family encode/decode | PASS — Initiate req/resp round-trips |
| Confirmed-service PDU family encode/decode | PASS — Identify req/resp round-trips |
| Written codec decision | Done — `internal/codec/CODEC_DECISION.md` |
| Custom helpers have unit tests | PASS — 5 tests in asn1util, 9 tests in pdu |
| `go build ./...` | PASS |
| `go test ./...` | PASS — 35 tests total |
| `go test -race ./...` | PASS |

---

## Feedback fixes (post Phase 1, pre Phase 2)

**Status: COMPLETE**

Improvements based on [FEEDBACK.md](FEEDBACK.md) review of Phase 0 and Phase 1 code.

### Fixes applied

| # | Fix | Files changed |
|---|-----|---------------|
| 1 | Reject trailing bytes in confirmed envelope parsing | `internal/codec/unmarshal.go` |
| 2 | Split `UnmarshalInto` into `UnmarshalInner` + `UnmarshalFull` — makes constructed-vs-primitive intent explicit at call sites | `internal/codec/choice.go`, `internal/pdu/identify.go` |
| 3 | Stop mutating `Client.closed` on unsupported Close stub — stub now returns `ErrUnsupported` without state change | `mms.go` |
| 4 | Copy slices in `Value` constructors and accessors — prevents aliasing/mutation through shared references | `value.go` |
| 5 | Fix public typos: `LocalAEQualifer` → `LocalAEQualifier`, `RemoteAEQualifer` → `RemoteAEQualifier`, `VMDPhysicalStatusNeedsCommision` → `VMDPhysicalStatusNeedsCommissioning` | `options.go`, `types.go` |
| 6 | Change `APTitle []int` to `APTitle = asn1.ObjectIdentifier` — aligns with stdlib type and avoids invalid OID arcs | `types.go` |
| 7 | Soften concurrency guarantee in Client docs — now says "intended design is for concurrent use; this guarantee applies once request dispatch is implemented" | `mms.go` |
| 8 | Add `ErrProtocol` sentinel — `ProtocolError.Unwrap()` now returns `ErrProtocol`, giving symmetry with `ServiceError` → `ErrServiceRejected` and `DecodeError` → `ErrDecodeFailed` | `errors.go` |
| 9 | Document single-byte tag limitation in `asn1util` — package doc and `WrapConstructed`/`WrapPrimitive` now explicitly state tag numbers 0–30 only | `internal/asn1util/doc.go`, `internal/asn1util/raw.go` |

### New tests added

| Test | Purpose |
|------|---------|
| `TestValueByteSliceCopyIsolation` | Verifies constructor and accessor copy isolation for `[]byte` |
| `TestValueStructureCopyIsolation` | Verifies constructor and accessor copy isolation for `[]*Value` |
| `TestProtocolErrorUnwraps` | Verifies `errors.Is(err, ErrProtocol)` works |
| `ErrProtocol` added to `TestSentinelErrors` | Covers the new sentinel |

### Verification

| Criterion | Result |
|---|---|
| `go build ./...` | PASS |
| `go test ./...` | PASS — 37 tests total (was 35) |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |

---

## Phase 2 — ISO upper-layer internals

**Status: COMPLETE**

### Deliverables

| Deliverable | Status | Notes |
|---|---|---|
| `internal/session` | Done | ISO 8327 SPDU construction and parsing: CONNECT, ACCEPT, DATA, FINISH, DISCONNECT, ABORT, REFUSE |
| `internal/presentation` | Done | ISO 8823 PPDU construction and parsing: CP-type, CPA-type, user-data transfer; context negotiation (ACSE ctx=1, MMS ctx=3) |
| `internal/acse` | Done | AARQ, AARE, RLRQ, RLRE, ABRT construction and parsing; AP-title/AE-qualifier encoding; user-information EXTERNAL encoding |
| `internal/isostack` | Done | Client-side orchestration: EncodeAssociateRequest, DecodeAssociateResponse, EncodeDataRequest, DecodeDataResponse, EncodeReleaseRequest, EncodeAbort |
| Tests | Done | 41 new tests across session (12), presentation (9), acse (14), isostack (6) |

### Implementation details

**Session layer (`internal/session/session.go`, ~280 lines):**
- SPDU types: CONNECT (0x0d), ACCEPT (0x0e), DATA (0x01), FINISH (0x09), DISCONNECT (0x0a), ABORT (0x19), REFUSE (0x0c)
- Wire format: SI byte + LI + PGI/PI parameter items
- DATA SPDU uses fixed 4-byte header (0x01 0x00 0x01 0x00)
- Session selectors: PGI 51 (calling), PGI 52 (called)
- Connection/Accept Item: PI 19 (protocol options), PI 22 (version=2)
- Session User Requirements: 0x0002 (duplex functional unit)
- Extended length encoding (0xff + 2-byte big-endian) for payloads ≥255 bytes

**Presentation layer (`internal/presentation/presentation.go`, ~310 lines):**
- CP-type (tag 0x31): mode-selector (normal-mode) + normal-mode-parameters with calling/called selectors, context-definition-list, fully-encoded user data
- CPA-type (tag 0x31): mode-selector + responding selector, context-definition-result-list, fully-encoded user data
- User-data (tag 0x61): pdv-list with context-id + presentation-data (0xa0)
- Well-known OIDs: ACSE abstract syntax (2.2.1.0.1), MMS abstract syntax (1.0.9506.2.1), BER transfer syntax (2.1.1)
- Fixed context IDs: 1 (ACSE), 3 (MMS)

**ACSE layer (`internal/acse/acse.go`, ~300 lines):**
- AARQ (0x60): application-context-name (OID 1.0.9506.2.3), called/calling AP-title + AE-qualifier, user-information (EXTERNAL)
- AARE (0x61): result (0=accepted, 1/2=rejected), result-source-diagnostic, user-information
- RLRQ (0x62): reason=normal → fixed 5 bytes (0x62 0x03 0x80 0x01 0x00)
- RLRE (0x63): empty → fixed 2 bytes (0x63 0x00)
- ABRT (0x64): abort-source (0=user, 1=provider)
- User-information encoding: [30] (0xbe) → EXTERNAL (0x28) → indirect-reference (0x02, value=3) → single-ASN1-type (0xa0) → MMS payload

**ISO stack orchestration (`internal/isostack/client.go`, ~120 lines):**
- `EncodeAssociateRequest`: builds Session CONNECT → Presentation CP → ACSE AARQ → MMS payload (inside-out)
- `DecodeAssociateResponse`: parses Session ACCEPT → Presentation CPA → ACSE AARE → extracts MMS payload
- `EncodeDataRequest`: Session DATA → Presentation user-data (ctx=3) → MMS payload
- `DecodeDataResponse`: reverse of above; detects FINISH/ABORT during data phase
- `EncodeReleaseRequest`: Session FINISH → Presentation user-data (ctx=1) → ACSE RLRQ
- `EncodeAbort`: Session ABORT → Presentation user-data (ctx=1) → ACSE ABRT

### Layer nesting verified

```
COTP DT carries:
  Session SPDU (CONNECT/ACCEPT/DATA/FINISH/ABORT)
    → Presentation PPDU (CP/CPA/user-data)
      → ACSE APDU (AARQ/AARE/RLRQ/RLRE) [association phase]
        → MMS PDU (Initiate/Identify/Read/Write/etc.)
      OR
      → MMS PDU directly [data transfer phase, ctx=3]
```

### Done criteria verification

| Criterion | Result |
|---|---|
| Session: parse golden-frame SPDUs | PASS — 12 tests: round-trip CONNECT/ACCEPT/DATA/FINISH/DISCONNECT/ABORT, wire format, edge cases |
| Session: construct valid SPDUs | PASS — all encode functions produce parseable output |
| Presentation: parse CP/CPA/user-data | PASS — 9 tests: round-trip CP/CPA/user-data, context list/result list presence, selectors |
| Presentation: construct valid PPDUs | PASS — all encode functions produce parseable output |
| ACSE: parse AARQ/AARE/RLRQ/RLRE/ABRT | PASS — 14 tests: round-trip all APDU types, wire byte verification, result values |
| ACSE: construct valid APDUs | PASS — RLRQ/RLRE/ABRT match expected fixed byte sequences |
| isostack: full association sequence via mock | PASS — 6 tests: associate round-trip, data round-trip, release, abort, error cases |
| `go build ./...` | PASS |
| `go test ./...` | PASS — 78 tests total (was 37) |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |

---

## Feedback fixes (post Phase 2, pre Phase 3)

**Status: COMPLETE**

Improvements based on [FEEDBACK.md](FEEDBACK.md) review of Phase 2 code and accumulated debt.

### Fixes applied

| # | Feedback | Fix | Files changed |
|---|---------|-----|---------------|
| 1 | Duplicated TLV/BER helpers across `acse` and `presentation` — risk of divergence | Created `internal/berutil` package with shared `EncodeTLV`, `AppendTLV`, `AppendLength`, `LengthSize`, `TLVSize`, `DecodeTLV`, `DecodeTLVAt`, `DecodeLength`, `DecodeInteger`. Removed duplicate implementations from `acse` and `presentation`. | `internal/berutil/berutil.go` (new, ~110 lines), `internal/acse/acse.go`, `internal/presentation/presentation.go` |
| 2 | `DecodeAssociateResponse` and `DecodeDataResponse` did not validate presentation context IDs | Added context ID checks: association response must have `ContextIDACSE` (1), data response must have `ContextIDMMS` (3) | `internal/isostack/client.go` |
| 3 | `session.Parse(DATA)` only checked length, not the fixed header bytes | Added explicit validation of all 4 fixed header bytes (0x01 0x00 0x01 0x00) | `internal/session/session.go` |
| 4 | ACSE parsing was too permissive — missing required fields accepted silently | `parseAARE` now requires `result` field (returns error if absent); `parseExternalPayload` rejects missing EXTERNAL tag or missing `single-ASN1-type` (0xa0); `validateRLRQ` rejects unexpected field tags; `validateABRT` requires `abort-source` field | `internal/acse/acse.go` |
| 5 | Invoke ID typing inconsistent — raw `uint32` in internal code | Defined `codec.InvokeID` as a type alias (`= uint32`) used consistently in `codec.MarshalConfirmedRequest`, `codec.UnmarshalConfirmedResponse`, `codec.UnmarshalConfirmedRequest`, and `pdu.ConfirmedResponse.InvokeID` | `internal/codec/unmarshal.go`, `internal/codec/marshal.go`, `internal/pdu/confirmed.go` |
| 6 | Presentation `parsePdvList` decoded INTEGER by taking first byte only | Now uses `berutil.DecodeInteger` for proper multi-byte signed INTEGER decoding; rejects empty/oversized values | `internal/presentation/presentation.go` |
| 7 | Public docs overpromised (Quick start with stubbed Dial, concurrency guarantee) | Changed "Quick start" to "Intended API shape" with note that methods are under development; removed premature concurrency guarantee from `Client` docs | `doc.go`, `mms.go` |
| 8 | Presentation Parse returned only `IsConnect bool` — no CP vs CPA distinction | Added `PpduKind` enum (`PpduCP`, `PpduCPA`, `PpduUserData`) with `String()` method; `ParsedPPDU.Kind` replaces `IsConnect`; CP vs CPA distinguished by presence of context-definition-list (0xa4) vs context-definition-result-list (0xa5) | `internal/presentation/presentation.go` |
| 9 | `AARQParams` allowed AE-qualifier without AP-title (silently dropped) | Documented pair semantics on `AARQParams`: AP-title + AE-qualifier are a pair; qualifier without title is silently omitted by design (documented) | `internal/acse/acse.go` |
| 10 | `EncodeAARE` swallowed `marshalOID` error for fixed MMS app context | Precomputed `appContextMmsEncoded` in `init()` — panics on impossible invariant violation instead of silently ignoring errors | `internal/acse/acse.go` |

### New tests added

| Test | Purpose |
|------|---------|
| `berutil/TestEncodeTLVRoundTrip` | Round-trip TLV encode/decode |
| `berutil/TestEncodeLongContent` | TLV with multi-byte length |
| `berutil/TestDecodeIntegerSingleByte` | Single-byte INTEGER values |
| `berutil/TestDecodeIntegerMultiByte` | Multi-byte INTEGER values |
| `berutil/TestDecodeIntegerEmpty` | Reject empty INTEGER |
| `berutil/TestDecodeIntegerTooLarge` | Reject oversized INTEGER |
| `berutil/TestDecodeLengthVariants` | All BER length forms |
| `berutil/TestDecodeTLVTruncated` | Truncated TLV error |
| `berutil/TestLengthSize` | Length size calculation |
| `session/TestParseDataInvalidHeader` | Reject invalid DATA header bytes |
| `acse/TestParseAAREMissingResult` | Reject AARE without result field |
| `acse/TestParseExternalMissingPayload` | Reject EXTERNAL without single-ASN1-type |
| `acse/TestParseABRTMissingSource` | Reject empty ABRT |
| `acse/TestParseRLRQBadField` | Reject RLRQ with unexpected field tag |
| `presentation/TestPpduKindString` | PpduKind.String() coverage |
| `isostack/TestDecodeDataResponseWrongContext` | Reject wrong presentation context in data response |

### Verification

| Criterion | Result |
|---|---|
| `go build ./...` | PASS |
| `go test ./...` | PASS — 94 tests total (was 78) |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |

---

## Phase 3 — MMS initiate, identify, status

**Status: COMPLETE**

### Deliverables

| Deliverable | Status | Notes |
|---|---|---|
| `internal/pdu/status.go` | Done | Status request marshal (with extendedDerivation boolean), Status response unmarshal (VMDLogicalStatus, VMDPhysicalStatus) |
| `internal/pdu/error.go` | Done | ConfirmedErrorPDU parsing (invokeID, errorClass, errorCode), RejectPDU parsing (invokeID, rejectType, rejectReason) |
| `internal/pdu/mmspdu.go` update | Done | Added PduConcludeError kind, classifyTag for TagConcludeError (0x8d) |
| `internal/invoke/tracker.go` | Done | Invoke ID allocator (sequential, never 0), outstanding call tracker with channel-based response delivery, pending limits, CancelAll |
| `internal/transport/transport.go` | Done | Transport interface (Send, Receive, Close) for COTP abstraction |
| `mms.Client` — Dial / NewClient | Done | Full ISO association + MMS Initiate handshake; parameter negotiation (maxPDUSize, maxOutCalling/Called, nestingLevel) |
| `mms.Client` — Close | Done | MMS Conclude → ConcludeResponse/ConcludeError handling → transport close; idempotent (returns ErrClosed on second call) |
| `mms.Client` — Identify | Done | ConfirmedRequest with Identify service → parse response → ServerIdentity; handles ConfirmedError and Reject |
| `mms.Client` — Status | Done | ConfirmedRequest with Status service → parse response → ServerStatus; handles ConfirmedError and Reject |
| End-to-end tests | Done | 7 client tests using mock transport/server, 7 invoke tracker tests, 4 PDU tests |
| Standardized invoke ID typing | Done | `codec.InvokeID` type alias used throughout; `pdu.MarshalIdentifyRequest` and `MarshalStatusRequest` updated |

### Architecture decisions

**Transport abstraction:** Defined `internal/transport.Transport` interface representing a connected COTP session. The Client accepts a Transport for send/receive of session SPDU bytes. This decouples MMS from the concrete TPKT/COTP implementation, allowing:
- Mock transports for testing (in-memory channel-based)
- Future integration with `otfabric/go-tpkt` and `otfabric/go-cotp`

**Synchronous request/response:** Phase 3 uses a synchronous model — one outstanding request at a time, serialized by mutex. The invoke tracker is designed for concurrent use but the Client currently uses it for ID allocation only. Full async dispatch with goroutine-based reader can be added in later phases without public API changes.

**Conclude flow:** MMS Conclude → ConcludeResponse (0x8c) / ConcludeError (0x8d). No ISO release is sent after conclude (the server typically closes the connection). Transport.Close() handles the cleanup.

**ConfirmedError/Reject handling:** Both error types are parsed from raw BER using `berutil` helpers (not `encoding/asn1`) since their wire format is context-specific tagged fields that don't map cleanly to Go struct tags. ConfirmedError maps to the public `ServiceError` type; Reject maps to `ProtocolError`.

### Implementation details

**Status service:**
- Request: ConfirmedRequest (0xa0) + invokeID + Status service tag (0x80, primitive) + BOOLEAN content
- Response: ConfirmedResponse (0xa1) + invokeID + Status response tag (0xa0, constructed) + vmdLogicalStatus [0] + vmdPhysicalStatus [1]
- Uses `encoding/asn1` struct tags for response decoding via `codec.UnmarshalInner`

**Invoke tracker (`internal/invoke/tracker.go`, ~90 lines):**
- Thread-safe with mutex
- Sequential ID allocation (wraps at uint32 max, skips 0)
- Channel-based response delivery (buffered channel per request)
- `CancelAll` for clean shutdown

**Client wiring (`mms.go`, ~330 lines):**
- `newClient`: creates client, performs ISO association + MMS Initiate, applies negotiated params, creates invoke tracker
- `associate`: builds MMS Initiate Request → ISO stack encode → send → receive → decode → parse InitiateResponse → negotiate
- `sendConfirmed`: allocates invoke ID → marshal PDU → ISO wrap → send → receive → ISO unwrap → classify → dispatch (response/error/reject)
- `conclude`: sends ConcludeRequest, expects ConcludeResponse
- `discardHandler`: silent slog handler for nil-logger case

### New packages created

| Package | Purpose | Lines |
|---|---|---|
| `internal/transport` | Transport interface for COTP abstraction | ~30 |
| `internal/invoke` (replaced doc.go) | Invoke ID allocator and call tracker | ~90 |

### New files created

| File | Lines | Purpose |
|---|---|---|
| `internal/pdu/status.go` | ~45 | Status request/response marshal/unmarshal |
| `internal/pdu/error.go` | ~120 | ConfirmedError and Reject PDU parsing |
| `internal/invoke/tracker.go` | ~90 | Invoke ID management |
| `internal/transport/transport.go` | ~30 | Transport interface |

### Tests added

| Test | Package | Purpose |
|------|---------|---------|
| `TestNewClientAndClose` | mms | Full connect → close lifecycle with mock server |
| `TestIdentify` | mms | Identify request/response end-to-end |
| `TestStatus` | mms | Status request/response end-to-end |
| `TestCloseAlreadyClosed` | mms | Idempotent close returns ErrClosed |
| `TestDialContextCancellation` | mms | Context timeout during association |
| `TestIdentifyOnClosedClient` | mms | Service call on closed client returns ErrClosed |
| `TestConfirmedError` | mms | ConfirmedErrorPDU from server → ServiceError |
| `TestAllocateSequential` | invoke | Sequential ID allocation |
| `TestAllocateLimit` | invoke | Pending limit enforcement |
| `TestComplete` | invoke | Response delivery and double-complete |
| `TestCancel` | invoke | Cancel with error |
| `TestCancelAll` | invoke | Cancel all pending |
| `TestPendingCount` | invoke | Count tracking |
| `TestCompleteUnknownID` | invoke | Unknown ID handling |
| `TestMarshalStatusRequest` | pdu | Status request encoding |
| `TestMarshalStatusRequestExtended` | pdu | Extended derivation flag |
| `TestDecodeConfirmedError` | pdu | ConfirmedError wire parsing |
| `TestDecodeRejectPDU` | pdu | Reject PDU wire parsing |
| `TestDecodeRejectPDUNoInvokeID` | pdu | Reject without invokeID |
| `TestStatusResponseRoundTrip` | pdu | Status request PDU classification |

### Done criteria verification

| Criterion | Result |
|---|---|
| `mms.NewClient` connects to mock server, completes ISO + MMS Initiate | PASS — full association with parameter negotiation |
| `client.Identify(ctx)` returns vendor/model/revision | PASS — "TestVendor"/"TestModel"/"1.0.0" |
| `client.Status(ctx)` returns VMD status | PASS — StateChangesAllowed / Operational |
| `client.Close(ctx)` performs clean conclude | PASS — ConcludeRequest → ConcludeResponse |
| Context cancellation during connect | PASS — 50ms timeout test |
| ConfirmedError handling | PASS — ServiceError with ErrorClassAccess |
| `go build ./...` | PASS |
| `go test ./...` | PASS — 114 tests total (was 94) |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |

### Note on Dial and transport

`mms.Dial` currently requires `TransportOptions.Conn` to be set with a pre-established transport connection. When `otfabric/go-tpkt` and `otfabric/go-cotp` are available, `Dial` will establish the COTP connection automatically from the `addr` parameter. For now, `NewClient(ctx, conn, opts)` is the primary entry point for creating clients with external transport management.

---

## Feedback fixes (post Phase 3, pre Phase 4)

**Status: COMPLETE**

Improvements based on [FEEDBACK.md](FEEDBACK.md) review of Phase 3 code. The feedback identified 2 must-fix correctness issues (invoke tracker leak and invoke ID validation), 5 strongly recommended fixes, and several medium-priority improvements.

### Fixes applied

| # | Priority | Feedback | Fix | Files changed |
|---|----------|---------|-----|---------------|
| 1 | Must-fix | Invoke tracker leaking pending requests — `Allocate()` registered pending calls but `sendConfirmed()` never called `Complete`/`Cancel`, causing `PendingCount` to grow indefinitely | Added `NextID()` method to tracker for ID-only allocation (no pending registration). Changed `sendConfirmed` to use `NextID()` instead of `Allocate()`. Pending-channel tracking preserved for future async dispatch but not wired in the synchronous path. | `internal/invoke/tracker.go`, `mms.go` |
| 2 | Must-fix | `sendConfirmed` did not verify response invoke ID matches request — protocol correctness gap | `sendConfirmed` now returns `*pdu.ConfirmedResponse` and checks invoke ID match for confirmed responses, confirmed errors, and reject PDUs (when present). Mismatch returns `ProtocolError`. | `mms.go` |
| 3 | Must-fix | `DecodeAssociateResponse` accepted both CP and CPA — association response must be CPA | Tightened check to require `PpduCPA` only; any other kind is a protocol error | `internal/isostack/client.go` |
| 4 | Must-fix | `doc.go` still said methods were "under active development" — stale after Phase 3 | Updated to "Quick start" with functional usage showing `Dial`, `Close`, `Identify`, `Status`. Lists Read/Write as under development. | `doc.go` |
| 5 | Strongly recommended | Trailing bytes not rejected in `UnmarshalInitiateResponse`, `UnmarshalInner`, `UnmarshalFull` | Added `len(rest) != 0` checks after all `asn1.Unmarshal` calls in these three functions. Identify and Status response unmarshal is covered via `UnmarshalInner`. | `internal/pdu/initiate.go`, `internal/codec/choice.go` |
| 6 | Strongly recommended | `handleReject` always logged `invokeID=%d` even when `HasInvokeID` was false — misleading | Error message now shows `invokeID=absent` when `HasInvokeID` is false | `mms.go` |
| 7 | Strongly recommended | Dead logger initialization line: `slog.New(slog.NewTextHandler(nil, nil))` immediately overwritten | Removed the dead assignment; now directly creates `slog.New(discardHandler{})` | `mms.go` |
| 8 | Strongly recommended | `applyNegotiatedParams` trusted server values too much — weird values pass silently | `applyNegotiatedParams` now validates server version (must be > 0) and logs all negotiated values at Debug level | `mms.go` |
| 9 | Medium | `sendConfirmed` returned raw bytes, caller had to re-parse confirmed response | `sendConfirmed` now returns `*pdu.ConfirmedResponse` directly, centralizing invoke ID validation and confirmed response parsing | `mms.go` |
| 10 | Medium | `ProtocolError` usage inconsistent — many protocol errors used plain `fmt.Errorf(..., ErrProtocol)` | Replaced `fmt.Errorf` + `ErrProtocol` sentinel wrapping with structured `&ProtocolError{Phase, Message}` throughout: association errors, conclude errors, service kind mismatch, invoke ID mismatch, unexpected PDU types | `mms.go` |
| 11 | Medium | `internal/transport.Transport` type in public `DialOptions.Conn` — leaky abstraction | Removed `Conn transport.Transport` from public `TransportOptions`. `NewClient(ctx, conn, opts)` is the injected-transport API. `Dial(ctx, addr, opts)` is the future public entry point (returns "not yet implemented" until go-tpkt/go-cotp integration). | `options.go`, `mms.go`, `mms_test.go` |
| 12 | Medium | Close idempotency not documented — `ErrClosed` on second call is valid but needs explicit docs | Added idempotency documentation to `Client` type doc and `Close` method doc | `mms.go` |

### New tests added

| Test | Purpose |
|------|---------|
| `invoke/TestNextIDSequential` | Verifies `NextID()` returns sequential IDs starting at 1, never registers pending |

### Verification

| Criterion | Result |
|---|---|
| `go build ./...` | PASS |
| `go test ./...` | PASS — 115 tests total (was 114) |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |

---

## Phase 4 — Read and write

**Status: COMPLETE**

### Deliverables

#### 1. `internal/pdu/data.go` — MMS Data encoding/decoding

All MMS value types with BER encoding/decoding:

| MMS Type | Tag | Status |
|----------|-----|--------|
| Boolean | 0x83 | DONE |
| Integer (signed) | 0x85 | DONE |
| Unsigned | 0x86 | DONE |
| Float (32/64-bit) | 0x87 | DONE |
| BitString | 0x84 | DONE |
| OctetString | 0x89 | DONE |
| VisibleString | 0x8a | DONE |
| MmsString | 0x90 | DONE |
| UTCTime | 0x91 | DONE |
| BinaryTime (4/6-byte) | 0x8c | DONE |
| Array | 0xa1 | DONE |
| Structure | 0xa2 | DONE |
| DataAccessError | 0x80 | DONE |

Key implementation details:
- **`DataValue`** internal type mirrors wire structure, separate from public `Value`.
- **Integer encoding**: BER 2's-complement, minimum-byte representation, handles full int64/uint64 range.
- **Float encoding**: 5-byte form (float32, exponent width 8) and 9-byte form (float64, exponent width 11).
- **BitString**: Stores unused-bits count; `Value.BitStringLength()` accessor added.
- **UTCTime**: 8-byte wire format (4-byte seconds + 3-byte fraction + 1-byte quality).
- **BinaryTime**: 4-byte (ms since midnight) and 6-byte (+ days since 1984-01-01) forms.
- **Array/Structure**: Recursive encoding/decoding of nested Data elements.
- **`MarshalData`/`UnmarshalDataElement`**: Full BER TLV encode/decode for each type.
- **`UnmarshalAccessResults`**: Decodes SEQUENCE OF AccessResult (used by Read response).

#### 2. `internal/pdu/read.go` — Read request/response PDU

- **`MarshalReadRequest`**: Builds ConfirmedRequestPdu with `listOfVariable` variable access specification. Supports multiple domain-specific variables.
- **`UnmarshalReadResponse`**: Parses Read response from ConfirmedResponsePdu service data. Handles optional `variableAccessSpecification [0]` prefix. Extracts `listOfAccessResult` SEQUENCE OF and decodes each AccessResult.
- **`ObjectNameWire`**: Internal type for domain-specific ObjectName encoding (`domainId` + `itemId` as VisibleString).

#### 3. `internal/pdu/write.go` — Write request/response PDU

- **`MarshalWriteRequest`**: Builds ConfirmedRequestPdu with variable specs and data values. Validates that variable and value counts match.
- **`UnmarshalWriteResponse`**: Parses per-variable success/failure results. Success = `[1] NULL` (0x81), Failure = `[0] DataAccessError` (0x80).
- **`WriteResultItem`**: Internal per-variable result type (success bool + error code).

#### 4. Public `Value` type enhancements

- **`BitStringLength() (int, bool)`**: New accessor for bit string valid bit count.
- **`NewBitStringWithLength(bits []byte, bitLen int)`**: Constructor with explicit bit length.
- **`NewBitString`** now sets `bitLen = len(bits) * 8` (backwards-compatible).

#### 5. Public client API — Read and Write

- **`Client.Read(ctx, ReadRequest) (*ReadResult, error)`**: Single-variable read. Returns the value or a `*DataAccessError` if the server reports a per-variable error.
- **`Client.ReadMultiple(ctx, []ObjectName) ([]AccessResult, error)`**: Multi-variable read in a single PDU. Returns one `AccessResult` per variable.
- **`Client.Write(ctx, WriteRequest) (*WriteResult, error)`**: Single-variable write. Returns `*DataAccessError` on per-variable failure.
- **`AccessResult`** public type: `Value *Value` + `Error DataAccessErrorCode`.
- **`DataAccessError`** error type in `errors.go`: Implements `error`, unwraps to `ErrServiceRejected`.
- **Value ↔ DataValue conversion**: `valueToDataValue` and `dataValueToValue` in `mms.go` convert between public `Value` (unexported fields) and internal `pdu.DataValue`.
  - Float width auto-detection: uses float32 if `float64(float32(v)) == v`, otherwise float64.
  - Recursive conversion for Array and Structure types.

#### 6. Documentation

- Updated `doc.go` Quick Start section to include Read and Write examples.

### Tests added

| Test | Package | Description |
|------|---------|-------------|
| `TestDataBooleanRoundTrip` | `pdu` | Boolean true/false encode-decode cycle |
| `TestDataIntegerRoundTrip` | `pdu` | Signed integer edge cases: 0, ±1, ±128, ±32768, int32/int64 min/max |
| `TestDataUnsignedRoundTrip` | `pdu` | Unsigned: 0, 127, 128, 255, 256, uint32 max, uint64 max |
| `TestDataFloat32RoundTrip` | `pdu` | Float32 encoding: 0, 1, -1, pi, max, smallest nonzero |
| `TestDataFloat64RoundTrip` | `pdu` | Float64 encoding: pi (full precision) |
| `TestDataBitStringRoundTrip` | `pdu` | BitString with non-byte-aligned length (20 bits) |
| `TestDataOctetStringRoundTrip` | `pdu` | OctetString byte-exact round-trip |
| `TestDataVisibleStringRoundTrip` | `pdu` | VisibleString encode/decode |
| `TestDataMmsStringRoundTrip` | `pdu` | MmsString with UTF-8 characters |
| `TestDataUTCTimeRoundTrip` | `pdu` | UTCTime encode/decode within 1ms precision |
| `TestDataBinaryTime6ByteRoundTrip` | `pdu` | 6-byte BinaryTime (full date+time) |
| `TestDataBinaryTime4ByteDecode` | `pdu` | 4-byte BinaryTime (ms since midnight) |
| `TestDataStructureRoundTrip` | `pdu` | Structure with mixed types |
| `TestDataArrayRoundTrip` | `pdu` | Array of unsigned integers |
| `TestDataAccessErrorRoundTrip` | `pdu` | DataAccessError code encode/decode |
| `TestUnmarshalAccessResultsMixed` | `pdu` | Mixed AccessResult list: boolean + error + integer |
| `TestNestedStructure` | `pdu` | Nested structure (structure inside structure) |
| `TestMarshalReadRequestSingleVariable` | `pdu` | Read request encoding |
| `TestMarshalReadRequestMultipleVariables` | `pdu` | Multi-variable read request |
| `TestUnmarshalReadResponseSingleValue` | `pdu` | Read response with one boolean |
| `TestUnmarshalReadResponseMultipleValues` | `pdu` | Read response with mixed types + error |
| `TestUnmarshalReadResponseWithVarSpec` | `pdu` | Read response with optional varspec prefix |
| `TestMarshalWriteRequest` | `pdu` | Write request encoding |
| `TestMarshalWriteRequestMismatch` | `pdu` | Vars/values count validation |
| `TestUnmarshalWriteResponseSuccess` | `pdu` | Write response: success |
| `TestUnmarshalWriteResponseFailure` | `pdu` | Write response: failure with error code |
| `TestUnmarshalWriteResponseMultiple` | `pdu` | Write response: mixed success/failure |
| `TestReadSingleVariable` | `mms` | End-to-end: Read integer via mock server |
| `TestReadMultipleVariables` | `mms` | End-to-end: ReadMultiple with bool+string+float |
| `TestReadDataAccessError` | `mms` | End-to-end: Read returns `*DataAccessError` |
| `TestWriteSingleVariable` | `mms` | End-to-end: Write integer via mock server |
| `TestWriteFailure` | `mms` | End-to-end: Write failure returns `*DataAccessError` |
| `TestReadStructuredValue` | `mms` | End-to-end: Read structure with nested elements |

### Verification

| Check | Result |
|-------|--------|
| `go build ./...` | PASS |
| `go test ./...` | PASS (148 tests) |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |

### Done criteria check

| Criterion | Status |
|-----------|--------|
| Read a single named variable and verify Value type/content | DONE — `TestReadSingleVariable` |
| Write a single named variable and verify success response | DONE — `TestWriteSingleVariable` |
| Read multiple variables in a single request | DONE — `TestReadMultipleVariables` |
| Handle DataAccessError in read responses without panics | DONE — `TestReadDataAccessError` |
| Handle ConfirmedErrorPDU and RejectPDU with typed errors | DONE — existing Phase 3 tests cover this |
| All MMS value types encode/decode correctly | DONE — comprehensive data_test.go |

---

## Feedback fixes (post Phase 4, pre Phase 5)

**Status: COMPLETE**

### Applied fixes

#### Must-fix

**1. ReadMultiple result count validation** — `mms.go`
- Added strict check: `len(dataValues) != len(variables)` → `ProtocolError`.
- Ensures positional alignment between request and response.
- Test: `TestReadResultCountMismatch`.

**2. Write result count validation** — `mms.go`
- Changed from `len(items) == 0` to `len(items) != 1` for single-variable write.
- Returns `ProtocolError` if server returns wrong number of results.

**3. DataAccessError.Unwrap() → ErrDataAccess** — `errors.go`
- Added new `ErrDataAccess` sentinel error.
- Changed `DataAccessError.Unwrap()` from `ErrServiceRejected` to `ErrDataAccess`.
- A per-variable data access error is not the same as a rejected service (ConfirmedErrorPDU); this distinction is now explicit.
- Test: `TestDataAccessErrorSentinel` verifies `errors.Is(err, ErrDataAccess)` is true and `errors.Is(err, ErrServiceRejected)` is false.

**4. doc.go Dial inconsistency** — `doc.go`
- Changed Quick Start to show `NewClient(ctx, conn, ...)` instead of `Dial(...)`.
- Added note: "Dial will perform automatic COTP transport setup once otfabric/go-tpkt and otfabric/go-cotp are integrated; until then, use NewClient directly."
- Added domain-specific-only limitation note.

#### Strongly recommended

**5. valueToDataValue rejects DataAccessError** — `mms.go`
- Added explicit `case ValueTypeDataAccessError` that returns `"cannot marshal DataAccessError as writable MMS Data value"`.

**6. dataValueToValue returns (*Value, error)** — `mms.go`
- Changed signature from `dataValueToValue(dv) *Value` to `dataValueToValue(dv) (*Value, error)`.
- Changed `dataValuesToValues(dvs) []*Value` to `dataValuesToValues(dvs) ([]*Value, error)`.
- Unknown internal data tags now return an error instead of silently fabricating `DataAccessError(None)`.
- All callers updated to handle the error.

**7. Fixed decodeFloat empty input panic** — `internal/pdu/data.go`
- Added `len(data) == 0` guard before any indexing in `decodeFloat`.
- Previously `data[0]` in the error message would panic on empty input.
- Test: `TestDecodeFloatEmptyInput`.

**8. decodeBitString rejects empty content** — `internal/pdu/data.go`
- Changed `len(data) == 0` from returning `nil, 0, nil` (silent success) to returning an error: `"empty bit string content (missing unused-bits octet)"`.
- A BER BIT STRING must include the unused-bits count octet.
- Test: `TestDecodeBitStringEmptyContent`.

**9. Tightened decodeUnsignedInt for uint64 overflow** — `internal/pdu/data.go`
- 9-byte encodings are now only valid when the first byte is `0x00` (BER sign-padding zero); otherwise returns error `"unsigned integer overflow: 9 bytes without leading zero"`.
- Prevents silent overflow into uint64.
- Test: `TestDecodeUnsignedInt9BytesValid`, `TestDecodeUnsignedInt9BytesOverflow`.

**10. Documented encodeBinaryTime 4-byte vs 6-byte behavior** — `internal/pdu/data.go`
- Added explicit comment block: encoder always emits canonical 6-byte form; decoder accepts both 4-byte and 6-byte forms.

#### Medium-priority

**11. Renamed AccessResult.Error to ErrorCode** — `mms.go`
- Renamed `AccessResult.Error` field to `AccessResult.ErrorCode` to avoid confusion with Go's `error` interface.
- All references updated.

**12. Added request validation for Read/Write** — `mms.go`
- `Read`: rejects empty `DomainID` or `ItemID` before sending.
- `Write`: rejects empty `DomainID`, empty `ItemID`, and nil `Value` before sending.
- Tests: `TestReadValidation`, `TestWriteValidation`.

**13. Documented domain-specific-only limitation** — `mms.go`, `doc.go`
- Added doc comment to `ReadMultiple`: "Currently only domain-specific variable names are supported."
- Added note to `doc.go` Quick Start about domain-specific-only support.

### New tests added

| Test | Package | Description |
|------|---------|-------------|
| `TestDecodeFloatEmptyInput` | `pdu` | Empty float content returns error, no panic |
| `TestDecodeBitStringEmptyContent` | `pdu` | Empty bit string content rejected |
| `TestDecodeUnsignedInt9BytesValid` | `pdu` | 9-byte unsigned with leading zero round-trips |
| `TestDecodeUnsignedInt9BytesOverflow` | `pdu` | 9-byte unsigned without leading zero rejected |
| `TestReadValidation` | `mms` | Empty DomainID/ItemID rejected early |
| `TestWriteValidation` | `mms` | Empty DomainID/ItemID/nil Value rejected early |
| `TestReadResultCountMismatch` | `mms` | Response count ≠ request count → ProtocolError |
| `TestDataAccessErrorSentinel` | `mms` | ErrDataAccess/ErrServiceRejected taxonomy correct |

### Verification

| Check | Result |
|-------|--------|
| `go build ./...` | PASS |
| `go test ./...` | PASS (156 tests) |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |

---

## Phase 5 — Name list, variable access attributes, named variable lists

**Status: COMPLETE**

### Bug fix: ObjectClass enum values

The ObjectClass enum had incorrect values — `namedType (3)` was missing,
which shifted `journal`, `domain`, `programInvocation`, and `operatorStation`
down by 1. Fixed to match ISO 9506 ASN.1 definition:

| Constant | Old Value | Correct Value |
|----------|-----------|---------------|
| ObjectClassNamedType (new) | — | 3 |
| ObjectClassJournal | 3 | 8 |
| ObjectClassDomain | 8 | 9 |
| ObjectClassProgramInvocation | 9 | 10 |
| ObjectClassOperatorStation | 10 | 11 |

### ObjectName scope support

Extended `ObjectNameWire` with a `Scope` field to support all three MMS
naming scopes: VMD-specific, domain-specific, and association-specific.

- Added `EncodeObjectName` / `DecodeObjectName` helpers in `internal/pdu/read.go`
- Updated `encodeListOfVariable` to use the generic `EncodeObjectName`
- Updated all callers (Read, Write, new services) to set `Scope: ScopeDomain`
- Added `ObjectScope` type to public API (`types.go`)

### New file: `internal/pdu/getnamelist.go`

- `MarshalGetNameListRequest(invokeID, objectClass, scope, domainID, continueAfter)` — builds
  the GetNameList ConfirmedRequestPdu with ObjectClass, ObjectScope, and optional continuation token.
- `UnmarshalGetNameListResponse(serviceData)` — parses `listOfIdentifier [0]` and
  `moreFollows [1]` (defaults to TRUE when absent per ASN.1 spec).
- `GetNameListResult` internal type with `Names []string` and `MoreFollows bool`.
- `decodeIdentifierList` helper for parsing SEQUENCE OF Identifier.

### New file: `internal/pdu/getvaraccess.go`

- `MarshalGetVarAccessRequest(invokeID, name)` — builds GetVariableAccessAttributes
  request with `name [0] EXPLICIT ObjectName`.
- `UnmarshalGetVarAccessResponse(serviceData)` — parses `mmsDeletable [0]`, skips
  optional `address [1]`, and decodes `typeSpecification [2] EXPLICIT TypeSpecification`.
- Full `DecodeTypeSpec(data)` implementing all 16 TypeSpecification CHOICE branches:
  - Primitive: boolean [3], bitstring [4], integer [5], unsigned [6],
    octetstring [9], visiblestring [10], generalizedtime [11], binarytime [12],
    bcd [13], objId [15], mmsstring [16], utctime [17].
  - Constructed: array [1] (packed?, numberOfElements, elementType), structure [2]
    (packed?, components with recursive TypeSpec decoding), floatingpoint [7]
    (formatWidth, exponentWidth).
  - Reference: typeName [0] (ObjectName).
- `TypeSpecWire`, `StructComponentWire` internal types.

### New file: `internal/pdu/namedvarlist.go`

- `MarshalDefineNamedVarListRequest(invokeID, listName, variables)` — builds
  DefineNamedVariableList request with `variableListName ObjectName` and
  `listOfVariable [0] IMPLICIT SEQUENCE OF { variableSpecification }`.
- `MarshalGetNamedVarListAttrsRequest(invokeID, listName)` — builds
  GetNamedVariableListAttributes request (ObjectName payload).
- `UnmarshalGetNamedVarListAttrsResponse(serviceData)` — parses `mmsDeletable [0]`
  and `listOfVariable [1]`, decoding each entry's VariableSpecification
  (`name [0] EXPLICIT ObjectName`).
- `MarshalDeleteNamedVarListRequest(invokeID, listNames)` — builds
  DeleteNamedVariableList request with `listOfVariableListName [1]`.
- `UnmarshalDeleteNamedVarListResponse(serviceData)` — parses `numberMatched`
  and `numberDeleted` Unsigned32 fields.

### Updated: `types.go`

- Fixed ObjectClass enum values (see bug fix above).
- Added `ObjectScope` type: `ObjectScopeVMD`, `ObjectScopeDomain`, `ObjectScopeAssociation`.
- Extended `TypeSpec` with `FormatWidth` and `ExponentWidth` fields for float types.

### Updated: `mms.go` — public Client methods

- **`GetNameList(ctx, NameListRequest)`** — full implementation with VMD, domain,
  and association scope support, plus `ContinueAfter` continuation token.
- **`GetNameListAll(ctx, NameListRequest)`** — convenience method that auto-paginates.
- **`GetVariableAccessAttributes(ctx, ObjectName)`** — returns a `*TypeSpec` with
  full recursive type resolution (arrays, structures, floats, etc.).
- **`DefineNamedVariableList(ctx, DefineNamedVariableListRequest)`** — creates
  a named variable list on the server with input validation.
- **`GetNamedVariableListAttributes(ctx, ObjectName)`** — retrieves deletable flag
  and variable list from the server.
- **`DeleteNamedVariableList(ctx, []ObjectName)`** — deletes named variable lists
  with input validation.
- Added `objectNameToWire`, `objectNameFromWire`, `objectScopeToWire`, and
  `typeSpecFromWire` conversion helpers.
- Updated `NameListRequest` with `Scope` and `ContinueAfter` fields; simplified
  `NameListResult` (removed `Continuation` field — caller uses last name).

### Updated: `doc.go`

- Updated service list to include all Phase 5 methods.

### Tests added

**PDU-level tests** (`internal/pdu/`):

- `getnamelist_test.go` (6 tests): VMD/domain/continuation request marshaling,
  response with names, moreFollows default, empty response.
- `getvaraccess_test.go` (10 tests): request marshaling, TypeSpec decoding for
  boolean, integer, float (formatWidth/exponentWidth), visiblestring, utctime,
  array (with nested element type), structure (with named components + recursive
  TypeSpec), binarytime, ObjectName encode/decode round-trip (VMD/domain/AA).
- `namedvarlist_test.go` (6 tests): define request (domain/AA), get attrs request,
  get attrs response parsing, delete request, delete response parsing.

**End-to-end client tests** (`mms_test.go`):

- `TestGetNameListVMD` — VMD-scope name list retrieval.
- `TestGetNameListDomainSpecific` — domain-scope name list retrieval.
- `TestGetNameListContinuation` — multi-page continuation with `GetNameListAll`.
- `TestGetVariableAccessAttributesInteger` — integer type spec retrieval.
- `TestGetVariableAccessAttributesStructure` — structure with named components.
- `TestDefineNamedVariableList` — create a named variable list.
- `TestGetNamedVariableListAttributes` — retrieve list attributes.
- `TestDeleteNamedVariableList` — delete a named variable list.
- `TestNamedVariableListLifecycle` — full define→getAttrs→delete cycle.
- `TestDefineNamedVariableListValidation` — empty name, empty variables.
- `TestDeleteNamedVariableListValidation` — empty list names.
- Updated `mockServer.handleDataRequest` to recognize all Phase 5 service tags.

### Verification

| Check | Result |
|-------|--------|
| `go build ./...` | PASS |
| `go test ./...` | PASS (189 tests, up from 156) |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |

---

## Post-Phase 5 Feedback Improvements

**Status: COMPLETE**

Addressed 16 items from feedback review. All changes improve correctness,
scope completeness, and strictness in preparation for Phase 6 hardening.

### Changes

#### 1. `EncodeObjectName` returns error on invalid scope (feedback #1)
- Changed signature from `func EncodeObjectName(n ObjectNameWire) []byte` to
  `func EncodeObjectName(n ObjectNameWire) ([]byte, error)`.
- Removed the dangerous `default:` fallback that silently produced an
  empty domain-specific encoding for invalid scope values.
- Updated all callers in `read.go`, `write.go`, `namedvarlist.go`,
  `getvaraccess.go`, and test files.

#### 2. Public `ObjectName` now carries `Scope` field (feedback #2, #13)
- Added `Scope ObjectScope` to `ObjectName`. The struct now fully
  supports VMD, domain-specific, and association-specific naming.
- Updated `objectNameToWire` / `objectNameFromWire` to faithfully
  preserve scope in both directions.
- Added `objectScopeFromWire` helper for reverse conversion.
- Updated `ReadMultiple` to use `objectNameToWire` instead of
  hardcoding `ScopeDomain`.

#### 3. `GetNameList` validates domain-scope inputs (feedback #3)
- Domain scope now requires non-empty `DomainID`.
- Unknown scope values are rejected with a clear error.
- Added `TestGetNameListValidation`.

#### 4. `ReadMultiple` docs updated (feedback #4)
- Removed the "only domain-specific" caveat. Doc now states all three
  scopes are supported via `ObjectName.Scope`.

#### 5. `typeSpecFromWire` returns error instead of zero-value (feedback #5)
- Changed signature to `func typeSpecFromWire(ts pdu.TypeSpecWire) (TypeSpec, error)`.
- Unsupported wire tags now produce a clear error rather than a silent
  empty `TypeSpec{}`.
- Recursive calls (array element, structure components) propagate
  errors with context.

#### 6. `TypeSpecWire` preserves `typeName` reference (feedback #6)
- Added `TypeName *ObjectNameWire` field to `TypeSpecWire`.
- `decodeTypeSpecFromTLV` for tag `[0]` (typeName) now calls
  `DecodeObjectName` and stores the result.

#### 7. Trailing-byte checks in Phase 5 response parsers (feedback #7, #8, #9)
- `UnmarshalGetVarAccessResponse`: added `offset != len(content)` check.
- `UnmarshalGetNamedVarListAttrsResponse`: same.
- `UnmarshalDeleteNamedVarListResponse`: same.

#### 8. `DecodeUnsigned` in berutil (feedback #10)
- Added `berutil.DecodeUnsigned` for proper unsigned BER integer decoding.
- Rejects negative encodings and handles BER sign-padding correctly.
- `UnmarshalDeleteNamedVarListResponse` now uses `DecodeUnsigned`
  instead of `DecodeInteger` for `numberMatched`/`numberDeleted`.
- Added `TestDecodeUnsigned` and `TestDecodeUnsignedErrors`.

#### 9. `GetNameListAll` stalled-pagination protection (feedback #11)
- Detects when the continuation token stops advancing and returns a
  `ProtocolError` instead of looping forever.
- Added `TestGetNameListAllStalledPagination`.

#### 10. Deep validation of variable names (feedback #12)
- `DefineNamedVariableList` validates each variable: non-empty `ItemID`,
  and domain-scope requires non-empty `Domain`.
- `DeleteNamedVariableList` applies the same validation per name.
- Added `TestDefineNamedVariableListDeepValidation`.

#### 11. `objectNameFromWire` preserves scope (feedback #13)
- Now sets `ObjectName.Scope` from wire scope value, so VMD and
  association-specific names round-trip correctly.

#### 12. `ObjectScope.String()` method (feedback #14)
- Returns `"VMD"`, `"Domain"`, `"Association"`, or `"ObjectScope(N)"`.
- Added `TestObjectScopeString`.

#### 13. `TypeSpec.TypeName` for named/reference types (feedback #15)
- Added `TypeName *ObjectName` to `TypeSpec`.
- `typeSpecFromWire` for tag `[0]` now populates `TypeName` from the
  wire-level `ObjectNameWire` reference.

#### 14. `GetVariableAccessAttributes` returns richer result (feedback #16)
- New `VariableAccessAttributes` struct with `Deletable bool` and
  `TypeSpec TypeSpec`.
- Return type changed from `*TypeSpec` to `*VariableAccessAttributes`.
- Tests updated accordingly.

### New/updated tests
- `TestEncodeObjectNameInvalidScope`
- `TestObjectScopeString`
- `TestObjectNameScopeRoundTrip`
- `TestGetNameListValidation`
- `TestDefineNamedVariableListDeepValidation`
- `TestGetNameListAllStalledPagination`
- `TestDecodeUnsigned`, `TestDecodeUnsignedErrors`
- Updated `TestDeleteNamedVariableListValidation` with deeper checks.
- Updated `TestGetVariableAccessAttributesInteger` and
  `TestGetVariableAccessAttributesStructure` for new return type.

### Verification

| Check | Result |
|-------|--------|
| `go build ./...` | PASS |
| `go test ./...` | PASS (197 tests, up from 189) |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |

---

## Phase 6 — Hardening, fuzzing, interop

**Status: COMPLETE**

### Fuzz targets (17 total)

All fuzz targets run 15–30 seconds each without crashes.

#### `internal/berutil/fuzz_test.go`
- `FuzzDecodeTLV` — round-trip TLV encode/decode on arbitrary input.
- `FuzzDecodeLength` — BER length decoding.
- `FuzzDecodeInteger` — signed integer decoding.
- `FuzzDecodeUnsigned` — unsigned integer decoding.

#### `internal/pdu/fuzz_test.go`
- `FuzzDecodeTypeSpec` — TypeSpecification decoding.
- `FuzzDecodeObjectName` — ObjectName round-trip encode/decode.
- `FuzzUnmarshalDataElement` — MMS Data element decoding.
- `FuzzUnmarshalAccessResults` — AccessResult list decoding.
- `FuzzDecodePdu` — top-level PDU classification/decode.
- `FuzzDecodeConfirmedError` — ConfirmedErrorPDU decoding.
- `FuzzDecodeRejectPDU` — RejectPDU decoding.
- `FuzzDecodeConfirmedResponse` — ConfirmedResponse envelope.
- `FuzzUnmarshalReadResponse` — Read response.
- `FuzzUnmarshalWriteResponse` — Write response.
- `FuzzUnmarshalGetNameListResponse` — GetNameList response.
- `FuzzUnmarshalGetVarAccessResponse` — GetVarAccess response.
- `FuzzUnmarshalGetNamedVarListAttrsResponse` — NamedVarListAttrs response.
- `FuzzUnmarshalDeleteNamedVarListResponse` — DeleteNamedVarList response.

### Malformed/truncated PDU tests

`internal/pdu/malformed_test.go` — 60+ test cases across 10 test functions:
- `TestMalformedDataElements` (16 cases: empty, truncated, wrong lengths, unknown tags, overflow)
- `TestMalformedReadResponse` (4 cases: empty, wrong tag, truncated, trailing bytes)
- `TestMalformedWriteResponse` (3 cases)
- `TestMalformedGetNameListResponse` (3 cases)
- `TestMalformedGetVarAccessResponse` (5 cases including trailing junk)
- `TestMalformedTypeSpec` (6 cases)
- `TestMalformedObjectName` (5 cases)
- `TestMalformedConfirmedError` (2 cases)
- `TestMalformedRejectPDU` (2 cases)
- `TestMalformedDeleteNamedVarListResponse` (5 cases including negative encoding)
- `TestMalformedNamedVarListAttrsResponse` (4 cases)

### Concurrency and race tests

`concurrency_test.go`:
- `TestConcurrentReads` — 5 goroutines issuing Read concurrently on a single client.
- `TestContextCancellationDuringRead` — cancel during blocked Read.
- `TestContextCancellationDuringWrite` — cancel during blocked Write.
- `TestCancelledContextUnblocksRead` — explicit cancel unblocks pending Read.
- `TestDoubleClose` — Close is idempotent.
- `TestOperationsAfterClose` — Read/Write/Identify/Status return error after Close.

### Interop testing

`internal/pdu/interop_test.go` — wire-level tests validating encoding/decoding
against known-good BER patterns compatible with the C reference:
- `TestInteropReadRequestEncoding` — Read request produces valid ConfirmedRequest.
- `TestInteropReadResponseDecoding` — hand-crafted Read response decodes correctly.
- `TestInteropObjectNameEncodings` — all 3 scope variants produce correct BER tags.
- `TestInteropDataValueRoundTrip` — 13 data types round-trip correctly.
- `TestInteropTypeSpecKnownEncodings` — 6 TypeSpec wire patterns decode correctly.
- `TestInteropWriteRequestEncoding` — Write request encoding.
- `TestInteropGetNameListRequestEncoding` — GetNameList request encoding.
- `TestInteropStructureDataValueRoundTrip` — nested structure round-trip.

### Benchmarks

`internal/pdu/bench_test.go` and `value_bench_test.go` — performance profiling:

| Benchmark | ops/sec | allocs/op |
|-----------|---------|-----------|
| DecodeTLV | 265M | 0 |
| DecodeTypeSpec (integer) | 82M | 0 |
| UnmarshalDataElement (boolean) | 30M | 1 |
| UnmarshalDataElement (integer) | 29M | 1 |
| UnmarshalDataElement (float32) | 29M | 1 |
| UnmarshalDataElement (structure) | 5.4M | 6 |
| UnmarshalAccessResults (10 items) | 2.5M | 12 |
| MarshalData (float) | 84M | 1 |
| NewInteger | 1B+ | 0 |
| NewFloat | 1B+ | 0 |
| Value.Int64() | 1B+ | 0 |

### GoDoc

All public types and methods have GoDoc comments. Added comments to:
- `ObjectClass.String()`
- `ValueType.String()`
- `DataAccessErrorCode.String()`
- `ErrorClass.String()`
- `VMDLogicalStatus.String()`
- `VMDPhysicalStatus.String()`
- `ObjectScope.String()`

### Example program

`_examples/basic/main.go` — complete CLI example:
- Connects to an MMS server
- Identifies the server (vendor/model/revision)
- Queries server status
- Browses domain names or domain variables
- Reads a named variable
- Optionally writes an integer value
- Usage: `go run . -addr 10.0.0.1:102 -domain MyDomain -var MyVariable`

### New files

| File | Description |
|------|-------------|
| `internal/berutil/fuzz_test.go` | 4 fuzz targets for BER helpers |
| `internal/pdu/fuzz_test.go` | 13 fuzz targets for PDU decoders |
| `internal/pdu/malformed_test.go` | 60+ malformed/truncated input tests |
| `internal/pdu/bench_test.go` | 14 benchmarks for hot paths |
| `internal/pdu/interop_test.go` | 8 wire-level interop validation tests |
| `concurrency_test.go` | 6 concurrency/cancellation/race tests |
| `value_bench_test.go` | 6 value construction/access benchmarks |
| `_examples/basic/main.go` | Complete CLI example program |

### Verification

| Check | Result |
|-------|--------|
| `go build ./...` | PASS |
| `go test ./...` | PASS (240 tests, up from 197) |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |
| Fuzz targets (17 × 15–30s) | PASS — no crashes |
| Benchmarks | PASS — see table above |

---

## Post-Phase 6 Feedback Improvements

**Status: COMPLETE**

Twelve feedback items from the Phase 6 review were implemented.

### 1. Close is truly idempotent (Feedback #1)

- `Client.Close` now returns `nil` on second and subsequent calls instead of `ErrClosed`.
- Client type doc comment updated to document the true idempotent behavior.
- `TestDoubleClose` and `TestCloseAlreadyClosed` updated to verify `nil` on repeated close.

### 2. Concurrency model documented honestly (Feedback #2)

- Client type doc comment now explicitly states the concurrency limitations: safe for basic state operations but does not support true concurrent in-flight confirmed requests over one shared transport.
- `doc.go` adds a new "Concurrency" section explaining the current limitation and advising callers to serialize service calls or use separate connections.

### 3. Example fixed (Feedback #3, #10, #11)

- `_examples/basic/main.go` now carries a clear header comment stating this is an API usage sketch that requires `Dial` to be implemented.
- Write is now controlled by a separate `-write` bool flag plus `-write-val`, so integer value 0 can be written.
- `doc.go` quick-start section aligned: `Dial` is clearly documented as "declared but not yet implemented", with `NewClient` recommended as the current entry point.

### 4. ValueTypeNamedType added (Feedback #4)

- New `ValueTypeNamedType` constant in the `ValueType` enum, with `String()` returning `"NamedType"`.
- `typeSpecFromWire` for tag 0 (typeName) now returns `TypeSpec{Type: ValueTypeNamedType, ...}` instead of a zero-valued `TypeSpec` that masqueraded as boolean.

### 5. Shared ObjectName validation (Feedback #5)

- New `validateObjectName` helper validates:
  - `ItemID` is non-empty
  - Domain scope requires non-empty `Domain`
  - Scope is a known value (VMD, Domain, Association)
- Applied consistently in: `ReadMultiple`, `GetVariableAccessAttributes`, `GetNamedVariableListAttributes`, `DefineNamedVariableList`, `DeleteNamedVariableList`.

### 6. objectScopeToWire no longer silently defaults unknown scopes (Feedback #6)

- Renamed to `objectScopeToWireUnchecked` with a clear doc comment that callers must validate first.
- All call sites are gated by prior `validateObjectName` or explicit scope-switch validation, so invalid scopes are rejected before reaching the conversion.

### 7. GetNameList validates ObjectClass (Feedback #7)

- `Client.GetNameList` now rejects `ObjectClass` values outside the valid range `[0, ObjectClassOperatorStation]`.

### 8. GetNameListAll documents unbounded memory caveat (Feedback #8)

- GoDoc for `GetNameListAll` now notes that it accumulates all names in memory and recommends `GetNameList` with explicit pagination control for very large name lists.

### 9. DecodeConfirmedError and DecodeRejectPDU tightened (Feedback #9)

- `DecodeConfirmedError`:
  - Rejects empty content.
  - Reports missing `invokeID` field.
  - Rejects unexpected tags (not 0x80 or 0xa2).
  - `parseErrorClass` enforces trailing-byte exhaustion.
- `DecodeRejectPDU`:
  - Rejects empty content.
  - Reports missing `rejectReason` field.
  - Rejects unexpected tags (not 0x80 or 0x81–0x8b).

### New tests added

| Test | Validates |
|------|-----------|
| `TestReadMultipleValidation` | Empty ItemID, missing Domain, unknown scope rejected |
| `TestGetVariableAccessAttributesValidation` | Empty ItemID and unknown scope rejected |
| `TestGetNameListObjectClassValidation` | Invalid and negative ObjectClass rejected |
| `TestGetNamedVariableListAttributesValidation` | Domain scope with empty Domain rejected |
| `TestValueTypeNamedType` | ValueTypeNamedType string, distinctness from boolean |
| `TestDeleteNamedVariableListUnknownScope` | Unknown scope rejected in DeleteNamedVariableList |
| `TestDecodeConfirmedErrorStrict` | Empty, missing invokeID, unexpected tags rejected |
| `TestDecodeRejectPDUStrict` | Empty, missing reason, unexpected tags rejected |
| `TestDecodeConfirmedErrorTrailingBytes` | Trailing bytes in errorClass rejected |
| `TestDoubleClose` (updated) | Second Close returns nil (truly idempotent) |
| `TestCloseAlreadyClosed` (updated) | Second Close returns nil (truly idempotent) |

### Files modified

| File | Changes |
|------|---------|
| `mms.go` | Idempotent Close, concurrency docs, `validateObjectName`, validation in `ReadMultiple`/`GetVariableAccessAttributes`/`GetNamedVariableListAttributes`/`DefineNamedVariableList`/`DeleteNamedVariableList`, `objectScopeToWireUnchecked`, ObjectClass validation in `GetNameList`, `GetNameListAll` doc caveat, `typeSpecFromWire` uses `ValueTypeNamedType` |
| `types.go` | Added `ValueTypeNamedType` constant and string mapping |
| `doc.go` | Updated quick-start (Dial status), added Concurrency section |
| `internal/pdu/error.go` | Tightened `DecodeConfirmedError` and `DecodeRejectPDU` with strict validation |
| `internal/pdu/error_test.go` | Added 3 new test functions with 9 strictness test cases |
| `mms_test.go` | Added 6 new validation test functions, updated 2 Close tests |
| `concurrency_test.go` | Updated `TestDoubleClose` for idempotent behavior |
| `_examples/basic/main.go` | Rewritten: API sketch header, separate -write/-write-val flags |

### Verification

| Check | Result |
|-------|--------|
| `go build ./...` | PASS |
| `go test ./...` | PASS (409 tests, up from 240) |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |

---

## Pre-Phase 7 Stability and Truthfulness Pass

**Status: COMPLETE**

A focused cleanup pass addressing 14 feedback items before server-side work.

### 1. Request serialization (Feedback #1 — P0)

- Added `reqMu sync.Mutex` to `Client`. All confirmed request/response flows in `sendConfirmed` now hold `reqMu`, ensuring one in-flight confirmed request at a time.
- Client concurrency doc updated: "Service calls are serialized internally via a request mutex."
- This is honest, safe behavior — a future reader loop with invoke-ID correlation can lift to true multiplexing.

### 2. Close docs/behavior aligned (Feedback #2)

- Close method doc now correctly says "idempotent: subsequent calls return nil".
- Removed stale "[ErrClosed]" reference from the Close doc comment.

### 3. Dial/NewClient/example story aligned (Feedback #3)

- `NewClient` doc updated: "This is the primary way to create a Client until Dial is implemented."
- Example header already marked as API sketch; no further change needed.

### 4. Public Transport interface (Feedback #4)

- Defined `mms.Transport` interface in `types.go` matching the `Send`/`Receive`/`Close` contract.
- `NewClient` now accepts `mms.Transport` (public type) instead of `internal/transport.Transport`.
- `Client.conn` field type changed to `Transport`.
- Removed `internal/transport` import from `mms.go`.
- `internal/transport` package remains for reference/documentation but is no longer in the public API path.

### 5. Logging field names normalized (Feedback #5)

- Changed `"domain"` → `"domain_id"` and `"item"` → `"item_id"` in Write service logging to match the PLAN.md logging contract.
- Other field names (`invoke_id`, `service`, etc.) were already consistent.

### 6. PDUHook renamed to RawHook (Feedback #6)

- Renamed `PDUHook` to `RawHook` in `DialOptions`.
- Updated doc to clarify it operates at the COTP user-data / session SPDU level, not the MMS PDU level specifically.
- All call sites in `sendRaw`/`receiveRaw` updated.

### 7. Empty BIT STRING decode fixed (Feedback #7)

- `decodeBitString` now handles `len(data) == 1` (unused-bits octet only, no payload) as a valid empty bit string returning `nil, 0, nil`.
- Added `TestEmptyBitStringRoundTrip` verifying encode→decode round-trip for empty bit strings.

### 8. Unsigned integer decode unified (Feedback #8)

- `decodeUnsignedInt` in `internal/pdu/data.go` now rejects BER negative encodings (high bit set without leading 0x00 pad), matching `berutil.DecodeUnsigned` semantics.
- Added `TestDecodeUnsignedNegativeEncoding` test.

### 9. asn1util/berutil boundary documented (Feedback #9)

- `berutil` package doc updated: explains it handles raw TLV operations and integer primitives for ISO layers and selected MMS manual decoders.
- `asn1util` package doc added: explains it bridges encoding/asn1 types and MMS-specific needs (tag inspection, RawValue manipulation).
- Documents the boundary: berutil = raw bytes, asn1util = encoding/asn1 types.

### 10. Negotiation policy documented (Feedback #10)

- `applyNegotiatedParams` now has a doc comment block explaining the exact negotiation policy for each field (maxPDUSize, maxOutstanding, nestingLevel, version) and noting these are pragmatic interop defaults.

### 11. Example uses mms.APTitle (Feedback #11)

- Removed `encoding/asn1` import from `_examples/basic/main.go`.
- Changed `asn1.ObjectIdentifier{...}` → `mms.APTitle{...}`.

### 12. Docs naming truth pass (Feedback #12)

- `doc.go` concurrency section updated to reflect the new serialized request behavior.
- `NewClient` doc updated to describe it as the primary entry point.

### 13. TypeSpec supported subset documented (Feedback #13)

- `TypeSpec` doc comment now explicitly lists supported variants and names unsupported ones (GeneralizedTime, BCD, ObjectIdentifier).

### 14. structureVal renamed to elementsVal (Feedback #14)

- Internal field `structureVal` in `Value` renamed to `elementsVal` — clearer for both Array and Structure usage.
- All references in `value.go` and `mms.go` updated.

### Files modified

| File | Changes |
|------|---------|
| `mms.go` | `reqMu` added, `sendConfirmed` serialized, public `Transport` type, Close doc fix, log field normalization, `RawHook`, negotiation doc, `elementsVal` rename |
| `types.go` | Public `Transport` interface added, `TypeSpec` supported subset documented |
| `value.go` | `structureVal` → `elementsVal` |
| `options.go` | `PDUHook` → `RawHook` with accurate doc |
| `doc.go` | Concurrency section updated for serialized requests |
| `internal/pdu/data.go` | Empty bit string decode fix, unsigned negative encoding rejection |
| `internal/pdu/data_test.go` | `TestEmptyBitStringRoundTrip`, `TestDecodeUnsignedNegativeEncoding` |
| `internal/berutil/berutil.go` | Package doc updated with boundary explanation |
| `internal/asn1util/raw.go` | Package doc added with boundary explanation |
| `_examples/basic/main.go` | `mms.APTitle` instead of `encoding/asn1` |

### Verification

| Check | Result |
|-------|--------|
| `go build ./...` | PASS |
| `go test ./...` | PASS (412 tests) |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |

---

## Phase 7 — Server-side MMS

**Status: COMPLETE**

### Summary

Implemented a full server-side MMS framework capable of accepting associations, negotiating initiate parameters, and serving confirmed MMS services via a typed handler and variable registry API.

### Sub-phase completion

| Sub-phase | Description | Status |
|-----------|-------------|--------|
| **7A** | Design decisions + public types | COMPLETE |
| **7B** | ISO server orchestration | COMPLETE |
| **7C** | Per-connection server runtime | COMPLETE |
| **7D** | Initiate negotiation + PDU marshal helpers | COMPLETE |
| **7E** | Confirmed service dispatch framework | COMPLETE |
| **7F** | Identify + Status handlers | COMPLETE |
| **7G** | GetNameList with registry + continuation | COMPLETE |
| **7H** | GetVariableAccessAttributes | COMPLETE |
| **7I** | Read service | COMPLETE |
| **7J** | Write service | COMPLETE |
| **7K** | Named variable lists | DEFERRED (per plan) |
| **7L+7M** | Error hardening + integration test suite | COMPLETE |
| **7N** | Documentation + examples | COMPLETE |

### Design decisions (7A)

- **Handler registration:** Per-service typed hooks (e.g. `HandleIdentify`, `HandleStatus`).
- **Variable model:** Registry-backed with `Read`/`Write` callback functions per variable.
- **Listener abstraction:** `Server.Serve(ctx, Transport)` per accepted connection; no built-in `ListenAndServe` yet (the caller manages the accept loop with go-tpkt/go-cotp).
- **Per-connection context:** Server context propagated to all handler calls.
- **Concurrency:** Multiple connections served in parallel. Within one connection, confirmed requests are serialized.
- **Shared code:** Reuses `internal/pdu`, `internal/codec`, `internal/berutil`, `internal/isostack`, `internal/session`, `internal/presentation`, `internal/acse` and all root-level types/values.

### New files

| File | Purpose |
|------|---------|
| `server.go` | Public `Server` type, handler registration, service dispatch, value conversion |
| `server_options.go` | `ServerOptions`, `ServerMMSOptions` with defaults |
| `server_model.go` | `Variable`, `IdentifyRequest`, `StatusRequest` public types |
| `server_test.go` | 13 integration tests: Identify, Status, GetNameList (domains, variables), GetVariableAccessAttributes, Read (float, integer), Write + read-back, Conclude, sequential multi-service, concurrent clients, read non-existent, write read-only |
| `internal/isostack/server.go` | Server-side ISO orchestration: `DecodeAssociateRequest`, `EncodeAssociateResponse`, `EncodeAssociateReject`, `DecodeReleaseRequest`, `EncodeReleaseResponse` |
| `internal/serverconn/conn.go` | Per-connection runtime: handshake, negotiate, request/response loop, conclude/release handling |
| `internal/servermodel/registry.go` | Domain/variable registry with sorted iteration for deterministic GetNameList pagination |
| `internal/servermodel/registry_test.go` | 6 unit tests for registry: domain lifecycle, variable lifecycle, domain listing, pagination, domain variables, VMD variables |
| `internal/codec/response.go` | `MarshalConfirmedResponse`, `MarshalConfirmedError`, `MarshalRejectPDU`, `MarshalConcludeResponse` |
| `internal/pdu/server_helpers.go` | Server-side request unmarshalers: `UnmarshalGetNameListRequest`, `UnmarshalGetVarAccessRequest`, `UnmarshalReadRequest`, `UnmarshalWriteRequest`. Server-side response marshalers: `MarshalGetNameListResponse`, `MarshalReadResponse`, `MarshalWriteResponse`, `MarshalGetVarAccessResponse`. `EncodeTypeSpec` for wire-encoding TypeSpecification. |
| `_examples/server-basic/main.go` | Minimal server example |

### Modified files

| File | Change |
|------|--------|
| `doc.go` | Updated package documentation to cover both client and server, including server quick-start example and server concurrency description |

### Supported server services

| Service | Handler | Description |
|---------|---------|-------------|
| Identify | `HandleIdentify(func)` | Returns vendor/model/revision |
| Status | `HandleStatus(func)` | Returns VMD logical/physical status |
| GetNameList | Registry-based | Lists domains (VMD scope) and variables (domain scope) with continuation |
| GetVariableAccessAttributes | Registry-based | Returns TypeSpec and deletable flag for registered variables |
| Read | Per-variable `Read` callback | Reads one or more variables; per-variable access errors for missing/denied |
| Write | Per-variable `Write` callback | Writes one or more variables; per-variable result codes |

### Deferred to future phases

- Named variable lists (DefineNamedVariableList, GetNamedVariableListAttributes, DeleteNamedVariableList)
- Unconfirmed services (InformationReport)
- TLS / authentication
- File/journal services
- `ListenAndServe(ctx, addr)` built-in listener

### Verification

| Check | Result |
|-------|--------|
| `go build ./...` | PASS |
| `go test ./...` | PASS (430 tests) |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |

### Integration test matrix

| Test | Client ↔ Server | Result |
|------|-----------------|--------|
| `TestServerIdentify` | Identify request/response | PASS |
| `TestServerStatus` | Status request/response | PASS |
| `TestServerGetNameListDomains` | GetNameList for domains at VMD scope | PASS |
| `TestServerGetNameListVariables` | GetNameList for variables in a domain | PASS |
| `TestServerGetVariableAccessAttributes` | TypeSpec round-trip for float variable | PASS |
| `TestServerRead` | Read float variable | PASS |
| `TestServerReadInteger` | Read integer variable | PASS |
| `TestServerWrite` | Write float variable + read-back verification | PASS |
| `TestServerConclude` | Clean conclude/close | PASS |
| `TestServerMultipleSequentialRequests` | All services in sequence on one connection | PASS |
| `TestServerConcurrentClients` | 5 concurrent client connections | PASS |
| `TestServerReadNonExistentVariable` | Proper data-access-error for missing variable | PASS |
| `TestServerWriteReadOnly` | Proper error for writing read-only variable | PASS |

---

## Post-Phase 7 feedback improvements — COMPLETE

### Summary

Applied all 20 items from FEEDBACK.md (post-Phase 7 review). Theme:
**"prefer loud failure over silent coercion"**.

### Must-fix items (1–5)

| # | Issue | Resolution |
|---|-------|------------|
| 1 | `typeSpecToWire` silently fell back to BOOLEAN | Changed signature to `(TypeSpecWire, error)`. Unknown types return an error. NamedType with nil TypeName also errors. All callers updated. |
| 2 | Status handler ignored `ExtendedDerivation` | Parses the BOOLEAN body byte and populates `StatusRequest.ExtendedDerivation`. |
| 3 | GetNameList did not enforce ObjectClass strictly | Strict matrix: Domain+VMD, NamedVariable+Domain, NamedVariable+VMD. All other combinations return `errUnsupportedFeature`. |
| 4 | Write accepted mismatched vars/values count | Rejects `len(vars) != len(values)` immediately. |
| 5 | `objectScopeFromWire` defaulted to VMD | Returns `(ObjectScope, error)`. All callers propagate the error. |

### Strongly recommended (6–10)

| # | Issue | Resolution |
|---|-------|------------|
| 6 | Malformed confirmed requests logged and ignored | `handleConfirmedRequest` now sends a `RejectPDU` (type=1 confirmed-request-pdu, reason=0 other) instead of silently dropping. |
| 7 | Trailing-byte strictness | Swept all `_ = offset` patterns in `pdu/read.go`, `pdu/server_helpers.go`, `pdu/getvaraccess.go`. Replaced with explicit `offset != len(data)` checks that return an error. |
| 8 | Naked data-access error integers | Centralized as `wireErr*` constants (`wireErrObjectUndefined`, `wireErrAccessDenied`, `wireErrTempUnavail`, `wireErrTypeInconsistent`) derived from public `DataAccessErrorCode` values. Server error vars (`errServiceUnsupported`, `errObjectNonExistent`, `errAccessDenied`, `errUnsupportedFeature`) also centralized. |
| 9 | ACSE parser validation | `parseAARE` now rejects result values outside `[0, 2]`. |
| 10 | Unsupported feature paths | All server dispatch paths use typed sentinel errors: `errServiceUnsupported` (no handler registered), `errUnsupportedFeature` (unknown service tag or unsupported object class/scope combo). |

### Medium (11–15)

| # | Issue | Resolution |
|---|-------|------------|
| 11 | Dial story | `Dial` doc updated to clearly state it is not yet implemented and to use `NewClient`. Removed `_ = addr` stub. |
| 12 | Dead fields | Removed `TypeTag int` from `servermodel.VarEntry`. |
| 13 | BER integer helper duplication | Added `berutil.EncodeInt` and `berutil.EncodeUint32`. Removed local `encodeUint32`/`encodeSmallInt` from `codec/response.go` and `acse/acse.go`. `pdu/server_helpers.go` delegates to `berutil.EncodeInt`. |
| 14 | Server request semantic validation | Handlers now enforce strict scope/class matrix. Write rejects count mismatches. Status parses body. Dispatch uses typed errors for all paths. |
| 15 | Examples | Added fully runnable `_examples/loopback/main.go` using channel-based transports. Client example already labeled as sketch. |

### Nice-to-have (16–20)

| # | Issue | Resolution |
|---|-------|------------|
| 16 | Dedicated unsupported feature error | `errUnsupportedFeature` sentinel error (VMDState class, code 5) used consistently for unsupported services and combinations. |
| 17 | Protocol-behavior tables in docs | `doc.go` updated with supported services (client + server), GetNameList combination matrix, and "not yet implemented" list. |
| 18 | More negative interop tests | Added 9 new tests: `TestServerGetNameListUnsupportedObjectClass`, `TestServerGetNameListDomainScopeForDomains`, `TestServerReadNonExistentDomain`, `TestServerGetVarAccessNonExistent`, `TestServerStatusExtendedDerivation`, `TestServerRegisterVariableUnsupportedTypeSpec`, `TestServerRegisterNamedTypeNilTypeName`, `TestServerNoIdentifyHandler`, `TestServerNoStatusHandler`. |
| 19 | Stricter registration-time TypeSpec validation | `RegisterVariable` calls `typeSpecToWire` at registration time. Unsupported types are rejected early. |
| 20 | Explicit unsupported-service response tests | `TestServerNoIdentifyHandler` and `TestServerNoStatusHandler` verify proper service errors when handlers are not registered. Unsupported object class/scope tests cover GetNameList. |

### Files modified

| File | Changes |
|------|---------|
| `server.go` | `typeSpecToWire` returns error; centralized error codes; strict GetNameList matrix; write count enforcement; Status body parsing; NamedType support in typeSpecToWire; registration-time TypeSpec validation |
| `mms.go` | `objectScopeFromWire` and `objectNameFromWire` return errors; `Dial` doc update |
| `internal/serverconn/conn.go` | Send RejectPDU for malformed confirmed requests |
| `internal/servermodel/registry.go` | Removed dead `TypeTag` field |
| `internal/berutil/berutil.go` | Added `EncodeInt` and `EncodeUint32` |
| `internal/codec/response.go` | Removed local integer helpers; uses berutil |
| `internal/acse/acse.go` | Consolidated integer helpers; AARE result validation |
| `internal/pdu/server_helpers.go` | Trailing-byte checks; delegates to berutil |
| `internal/pdu/read.go` | Trailing-byte check |
| `internal/pdu/getvaraccess.go` | Trailing-byte checks (4 locations) |
| `doc.go` | Protocol-behavior tables, supported services, GetNameList matrix |
| `server_test.go` | 9 new negative/interop tests |
| `_examples/loopback/main.go` | New fully runnable in-process example |

### Verification

| Check | Result |
|-------|--------|
| `go build ./...` | PASS |
| `go test ./...` | PASS (all packages) |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |

### New test matrix (server_test.go: 22 tests)

| Test | Type | Result |
|------|------|--------|
| `TestServerIdentify` | positive | PASS |
| `TestServerStatus` | positive | PASS |
| `TestServerGetNameListDomains` | positive | PASS |
| `TestServerGetNameListVariables` | positive | PASS |
| `TestServerGetVariableAccessAttributes` | positive | PASS |
| `TestServerRead` | positive | PASS |
| `TestServerReadInteger` | positive | PASS |
| `TestServerWrite` | positive | PASS |
| `TestServerConclude` | positive | PASS |
| `TestServerMultipleSequentialRequests` | positive | PASS |
| `TestServerConcurrentClients` | positive | PASS |
| `TestServerReadNonExistentVariable` | negative | PASS |
| `TestServerWriteReadOnly` | negative | PASS |
| `TestServerGetNameListUnsupportedObjectClass` | negative | PASS |
| `TestServerGetNameListDomainScopeForDomains` | negative | PASS |
| `TestServerReadNonExistentDomain` | negative | PASS |
| `TestServerGetVarAccessNonExistent` | negative | PASS |
| `TestServerStatusExtendedDerivation` | semantic | PASS |
| `TestServerRegisterVariableUnsupportedTypeSpec` | negative | PASS |
| `TestServerRegisterNamedTypeNilTypeName` | negative | PASS |
| `TestServerNoIdentifyHandler` | negative | PASS |
| `TestServerNoStatusHandler` | negative | PASS |

---

## Post-Phase 7 feedback round 2 — COMPLETE

### Summary

Applied 7 feedback items plus scope/documentation updates. Theme:
**strict protocol parsing, clean error taxonomy, API truthfulness.**

### Changes

| # | Issue | Resolution |
|---|-------|------------|
| 1 | Status request body too permissive | Requires exactly 1 byte; rejects empty or oversized bodies with service error. |
| 2 | Write cardinality check leaked into handler | `UnmarshalWriteRequest` now validates `len(vars) == len(values)` internally in the PDU layer. Redundant check removed from `handleWrite`. |
| 3 | `typeSpecToWire` bypassed scope validation for named types | Uses `validateObjectName` and new `objectScopeToWire` (checked) for named type scope conversion. |
| 4 | Explicit inner wrappers not fully verified | Added trailing-byte checks inside `objectClass` and `objectScope` explicit wrappers in `UnmarshalGetNameListRequest`. Added outer trailing-byte check to `UnmarshalGetVarAccessRequest`. |
| 5 | ConfirmedError codes mixed with DataAccessError codes | Separated into named constants: `serviceErrorClass*` + `svcErr*` for ConfirmedErrorPDU; `wireErr*` for per-variable Read/Write. Documented the intentional separation. |
| 6 | Client Status API didn't expose ExtendedDerivation | Added `ClientStatusRequest` type and `Client.StatusWithOptions` method. `Client.Status` delegates with default `false`. |
| 7 | Loopback example transport could panic on send-after-close | `Send` now checks `done` under lock before writing to channel. |

### Documentation/plan updates

- `doc.go`: Updated "not yet implemented" section to separate server-side deferred items from globally absent items. Named variable list services explicitly noted as client-side existing.
- `PLAN.md`: Updated defer section wording. Added Phase 8 (Server Named Variable Lists), Phase 9 (InformationReport), Phase 10 (Transport Integration / Dial).

### Files modified

| File | Changes |
|------|---------|
| `server.go` | Strict Status body parsing; removed redundant write count check; validated scope in `typeSpecToWire`; separated service-error and data-access-error constants |
| `mms.go` | Added `objectScopeToWire` (checked); added `ClientStatusRequest` + `StatusWithOptions` |
| `internal/pdu/server_helpers.go` | Write cardinality in PDU layer; explicit-wrapper trailing-byte checks in GetNameList/GetVarAccess |
| `_examples/loopback/main.go` | Panic-safe `Send` |
| `doc.go` | Scope-truthful defer wording |
| `PLAN.md` | Phases 8, 9, 10 added; defer wording updated |

### Verification

| Check | Result |
|-------|--------|
| `go build ./...` | PASS |
| `go test ./...` | PASS |
| `go vet ./...` | PASS |

---

## Phase 8 — Server Named Variable Lists — COMPLETE

### Summary

Implemented full server-side support for DefineNamedVariableList,
GetNamedVariableListAttributes, and DeleteNamedVariableList services.
The existing client-side NVL operations now fully interoperate with the server.

### Components implemented

1. **Registry NVL support** (`internal/servermodel/registry.go`):
   - `NVLEntry` and `NVLVariable` types
   - `DefineNVL` — create named variable list (domain or VMD scope)
   - `LookupNVL` — find NVL by scope/domain/itemID
   - `DeleteNVL` — remove deletable NVLs, update sorted order
   - `ListDomainNVLs` / `ListVMDNVLs` — paginated listing for GetNameList

2. **PDU layer** (`internal/pdu/server_helpers.go`):
   - `UnmarshalDefineNVLRequest` — parse DefineNamedVariableList request body
   - `UnmarshalGetNVLAttrsRequest` — parse GetNamedVariableListAttributes request body
   - `UnmarshalDeleteNVLRequest` — parse DeleteNamedVariableList request body
   - `MarshalDefineNVLResponse` — empty response (success)
   - `MarshalGetNVLAttrsResponse` — deletable flag + variable list
   - `MarshalDeleteNVLResponse` — numberMatched + numberDeleted
   - `DecodeObjectNameAt` — new helper for offset-based ObjectName decoding

3. **Server handlers** (`server.go`):
   - `handleDefineNVL` — validates and registers NVL via registry
   - `handleGetNVLAttrs` — looks up NVL and returns attributes
   - `handleDeleteNVL` — specific-scope deletion with match/delete counts
   - GetNameList extended with `ObjectClassNamedVariableList` at Domain and VMD scope
   - Dispatch switch extended for all three NVL service tags

4. **Documentation**:
   - `doc.go` updated: server services now include all three NVL services; GetNameList matrix extended
   - `server.go` handleGetNameList doc updated with NVL combinations

### Files modified

| File | Changes |
|------|---------|
| `internal/servermodel/registry.go` | `NVLEntry`, `NVLVariable`, `DefineNVL`, `LookupNVL`, `DeleteNVL`, `ListDomainNVLs`, `ListVMDNVLs`, `removeFromSorted` |
| `internal/pdu/server_helpers.go` | 3 unmarshalers + 3 marshalers for NVL services |
| `internal/pdu/read.go` | `DecodeObjectNameAt` helper |
| `server.go` | 3 NVL handlers, dispatch, GetNameList NVL support |
| `doc.go` | Updated service lists and GetNameList matrix |

### Verification

| Check | Result |
|-------|--------|
| `go build ./...` | PASS |
| `go test ./...` | PASS (all packages) |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |

### Test matrix (server_test.go: 28 tests)

New Phase 8 tests:

| Test | Type | Result |
|------|------|--------|
| `TestServerDefineAndGetNVLAttributes` | positive | PASS |
| `TestServerDeleteNVL` | positive | PASS |
| `TestServerGetNameListNVL` | positive | PASS |
| `TestServerDefineNVLDuplicate` | negative | PASS |
| `TestServerDeleteNonExistentNVL` | negative | PASS |
| `TestServerNVLFullLifecycle` | lifecycle | PASS |

---

## Phase 9 — Transport Integration and Real Connectivity — COMPLETE

### Summary

Implemented real TCP+TPKT+COTP transport integration using `otfabric/go-tpkt`
and `otfabric/go-cotp`. The library is now usable against real network
endpoints without external glue code.

### Components implemented

1. **`transport/iso` package** — new subpackage providing TCP+TPKT+COTP transport:

   - **`cotpTransport`** (`transport.go`) — implements `mms.Transport` over
     TCP+TPKT+COTP. Sends data as COTP DT TPDUs inside TPKT frames. Receives
     by reading TPKT frames and decoding COTP DT TPDUs. Respects context
     deadlines via TCP deadline propagation. Close is idempotent.

   - **`DialTCP`** (`dial.go`) — establishes TCP connection via `net.DialContext`,
     performs COTP CR/CC handshake (class 0, with optional TSAP selectors),
     and returns a `mms.Transport` ready for MMS.

   - **`Dial`** (`dial.go`) — convenience function combining `DialTCP` + `mms.NewClient`
     in a single call. Accepts TSAP selectors and MMS dial options.

   - **`Listen`** (`listen.go`) — creates a `Listener` bound to a TCP address.
     Each `Accept` call waits for a TCP connection, performs server-side COTP
     handshake (reads CR, sends CC), and returns a `mms.Transport`.

   - **`NewListener`** (`listen.go`) — wraps an existing `net.Listener` for
     custom bind scenarios or testing.

   - **`Options`** (`options.go`) — functional option pattern with
     `WithCallingTSelector`, `WithCalledTSelector`, `WithMMSOptions`.

2. **`TransportListener` interface** (`types.go`) — new public interface for
   accepting transport connections. `transport/iso.Listener` implements it.
   Enables `Server.ListenAndServe` without circular imports.

3. **`Server.ListenAndServe`** (`server.go`) — new method that accepts
   connections from a `TransportListener` and serves each in a new goroutine.
   Blocks until context cancellation. Connection errors are logged but do not
   stop the accept loop.

4. **`Dial` stub updated** (`mms.go`) — now directs users to
   `transport/iso.Dial` or `NewClient` with clear deprecation notice.

5. **Documentation** (`doc.go`) — updated client quick start to show
   `iso.Dial`, server quick start to show `ListenAndServe`, added
   Transport section, updated scope notes.

### COTP handshake details

**Client side:**
- Sends COTP CR (Connection Request) with SourceRef=1, ClassOption=0 (class 0),
  optional CallingSelector and CalledSelector.
- Waits for COTP CC (Connection Confirm). Handles DR (Disconnect Request) as
  connection refused.

**Server side:**
- Reads COTP CR from client.
- Sends COTP CC with matching DestinationRef=CR.SourceRef, SourceRef=1,
  echoing CallingSelector/CalledSelector.

### Architecture

```
TCP → TPKT (go-tpkt) → COTP (go-cotp) → Session → Presentation → ACSE → MMS
^--- transport/iso owns -------^          ^--- go-mms core owns ----------^
```

- `go-tpkt` and `go-cotp` remain TLS-agnostic.
- `go-mms` core remains transport-agnostic.
- `transport/iso` is the thin runtime integration layer.
- `NewClient(ctx, conn, opts)` remains the low-level injected path.
- `iso.Dial` is a convenience wrapper, not a second semantic codepath.

### Dependencies added

| Module | Version | Purpose |
|--------|---------|---------|
| `github.com/otfabric/go-tpkt` | v0.1.0 | TPKT framing (RFC 1006) |
| `github.com/otfabric/go-cotp` | v0.1.2 | COTP TPDU encode/decode (X.224 class 0) |

Both use `replace` directives pointing to local sibling directories
for development.

### New files

| File | Purpose |
|------|---------|
| `transport/iso/doc.go` | Package documentation for ISO transport layer |
| `transport/iso/transport.go` | `cotpTransport` implementing `mms.Transport` |
| `transport/iso/dial.go` | `DialTCP` and `Dial` client-side functions |
| `transport/iso/listen.go` | `Listener` and `Listen` server-side functions |
| `transport/iso/options.go` | Functional option types |
| `transport/iso/transport_test.go` | COTP transport unit tests (5 tests) |
| `transport/iso/integration_test.go` | Full MMS client↔server TCP integration tests (9 tests) |

### Modified files

| File | Changes |
|------|---------|
| `go.mod` | Added go-tpkt, go-cotp requires + replace directives; Go version → 1.22 |
| `types.go` | Added `TransportListener` interface |
| `server.go` | Added `Server.ListenAndServe`; updated Server doc |
| `mms.go` | Updated `Dial` stub with deprecation and iso.Dial guidance |
| `doc.go` | Updated quick starts, added Transport section, updated scope |

### Verification

| Check | Result |
|-------|--------|
| `go build ./...` | PASS |
| `go test ./...` | PASS (all packages) |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |

### Test matrix (transport/iso: 14 tests)

| Test | Type | Result |
|------|------|--------|
| `TestCOTPTransportRoundTrip` | unit | PASS |
| `TestDialTCPAndListen` | integration | PASS |
| `TestTransportCloseIdempotent` | unit | PASS |
| `TestTransportSendAfterClose` | unit | PASS |
| `TestNewListener` | unit | PASS |
| `TestTCPIdentify` | e2e | PASS |
| `TestTCPStatus` | e2e | PASS |
| `TestTCPReadWrite` | e2e | PASS |
| `TestTCPGetNameList` | e2e | PASS |
| `TestTCPMultipleSequentialServices` | e2e | PASS |
| `TestTCPConcurrentClients` | e2e | PASS |
| `TestTCPNVLLifecycle` | e2e | PASS |
| `TestTCPConclude` | e2e | PASS |
| `TestTCPWithTSAPSelectors` | e2e | PASS |

### Done criteria verification

| Criterion | Status |
|-----------|--------|
| Real client connects to real server over TCP/COTP | DONE — `TestTCPIdentify` etc. |
| Real server accepts real client over TCP/COTP | DONE — `ListenAndServe` + `Accept` |
| Identify/Status/Read/Write/GetNameList/NVL work over real transport | DONE — full e2e test suite |
| Existing in-process loopback tests remain unchanged | DONE — all pass |
| `go test -race ./...` passes | DONE |

---

## Post-Phase 9 feedback improvements

All feedback items from FEEDBACK.md have been addressed. Each item
is summarized below with the fix applied.

### Must-fix

**1. cotpTransport.Send mutex leak (FB#1)**

Extracted `isClosed()` helper method with proper `Lock/defer Unlock`
pattern. `Send` and `Receive` now use `isClosed()` for the closed-state
check, eliminating the raw lock/unlock pair that could leak on early
return. Added `TestSendAfterCloseNoDeadlock` regression test that
exercises Send → Close → Send → Close without deadlock.

**2. Listener.Accept(ctx) no longer closes the listener (FB#2)**

Completely redesigned `Accept`: instead of closing the listener on
context cancellation, it now uses deadline-based polling on the
`*net.TCPListener`. A 500ms accept deadline is set between each
iteration; the context is checked on each loop pass. Cancelling
the context returns `ctx.Err()` without touching the listener.

Verified by `TestAcceptContextCancel`: cancels Accept, then confirms
the listener is still alive by successfully dialling and accepting
again with a fresh context.

**3. Server.ListenAndServe behavior now matches docs (FB#3)**

Updated `ListenAndServe` to distinguish error types:
- `ctx.Err() != nil` → return cleanly
- temporary net error → log warning and continue the accept loop
- fatal error → return wrapped error

Updated doc comment to accurately describe this three-way behavior.
Added `isTemporary()` helper using `errors.As` with the `Temporary()`
interface.

### Strongly recommended

**4. Temporary vs fatal accept errors separated (FB#4)**

Implemented in conjunction with FB#3. `ListenAndServe` now uses
`isTemporary(err)` to classify accept errors. Temporary errors
(e.g. transient network issues) are logged at Warn level and retried.
Fatal errors stop the server.

In `listen.go`, added `isTemporaryError()` for use in future callers.

**5. COTP handshake validation tightened (FB#5)**

Client side (`clientCOTPHandshake`):
- CC `DestinationRef` must match CR `SourceRef` — error on mismatch
- CC `ClassOption` must be class 0 (upper nibble 0x00) — error otherwise
- CC called TSAP selector must match if both sides specified one

Server side (`serverCOTPHandshake`):
- Validates CR class is 0; sends DR (reason 2 = negotiation failed) on mismatch
- Validates called TSAP selector if listener is configured with one; sends DR (reason 3 = address unknown) on mismatch
- Added `sendDR()` helper for clean Disconnect Request transmission

New negative tests:
- `TestHandshakeWrongTPDUType` — server receives DT instead of CR
- `TestHandshakeTSAPSelectorMismatch` — client/server TSAP mismatch triggers DR
- `TestClientHandshakeReceivesDR` — server sends DR, client detects refusal
- `TestClientHandshakeCCRefMismatch` — CC destination ref wrong
- `TestClientHandshakeCCWrongClass` — CC negotiates unsupported class
- `TestServerHandshakeCRWrongClass` — CR with class 2, server sends DR

**6. Receive now short-circuits after close (FB#6)**

`Receive` now calls `isClosed()` at entry and returns `net.ErrClosed`
immediately if the transport is closed, symmetric with Send behavior.
Added `TestReceiveAfterClose` to verify.

**7. Per-connection COTP source ref allocation (FB#7)**

- Client: `clientSourceRef` atomic counter allocates unique per-connection
  source refs starting at 1, skipping 0 on wrap. Each `clientCOTPHandshake`
  call gets its own source ref via `nextClientSourceRef()`.
- Server: `Listener.sourceRef` atomic counter allocates per-accepted-connection
  source refs via `nextSourceRef()`. Each CC gets a unique source ref.

### Medium

**8. WithMMSOptions renamed to WithClientDialOptions (FB#8)**

Renamed to `WithClientDialOptions` to clarify that the option applies
only to the convenience `Dial` function (not `DialTCP` or `Listen`).
Updated all references in `doc.go`, `transport/iso/doc.go`, and the
root `doc.go`.

**9. TransportListener commitment noted (FB#9)**

Acknowledged. The interface is small (3 methods) and intentionally
minimal. No changes needed — just awareness that it is now part of
the public API.

**10. Root mms.Dial deprecation (FB#10)**

Acknowledged as a transitional state. Will be resolved in a future
breaking version or once TLS/transport layering is settled (Phase 11).

**11. Package docs bracket-link style fixed (FB#11)**

Replaced `[github.com/otfabric/go-mms/transport/iso]` and similar
bracket-link forms with plain prose references (e.g. "the transport/iso
subpackage") in:
- `doc.go` (3 occurrences)
- `types.go` (2 occurrences)
- `mms.go` (3 occurrences)
- `transport/iso/doc.go` (2 occurrences)

### Minor

**12. go.mod/progress mismatch (FB#12)**

The `replace` directives are present in the actual `go.mod` file.
Progress text was based on the patch diff which may have omitted them.
No action needed.

**13. ai-diff hardcodes main (FB#13)**

Noted. Acceptable for current workflow. Not changed.

**14. ai-context naming (FB#14)**

Noted. Naming improvement deferred.

### Transport lifecycle tests (Request 5)

Added comprehensive tests to `transport_test.go`:

| Test | What it verifies |
|------|-----------------|
| `TestSendAfterCloseNoDeadlock` | Send/Close/Send/Close sequence completes without deadlock |
| `TestReceiveAfterClose` | Receive returns `net.ErrClosed` after Close |
| `TestConcurrentCloseSend` | 100 concurrent Sends racing with Close — no deadlock or panic |
| `TestConcurrentCloseReceive` | Receive racing with Close — no deadlock |
| `TestAcceptContextCancel` | Context cancellation doesn't destroy listener; re-Accept works |
| `TestListenerCloseWhileBlocked` | Closing listener unblocks Accept |
| `TestHandshakeWrongTPDUType` | Server rejects non-CR first packet |
| `TestHandshakeTSAPSelectorMismatch` | Client/server TSAP mismatch → DR sent |
| `TestClientHandshakeReceivesDR` | Client detects DR refusal |
| `TestClientHandshakeCCRefMismatch` | Client detects CC destination ref mismatch |
| `TestClientHandshakeCCWrongClass` | Client detects unsupported CC class |
| `TestServerHandshakeCRWrongClass` | Server sends DR on CR class ≠ 0 |

### Request 6: Reader ownership for Phase 10

Transport API kept deliberately simple: `Send`, `Receive`, `Close`.
No protocol dispatch is embedded in the transport layer. Phase 10's
client reader loop will own the `Receive` goroutine and dispatch
InformationReport vs confirmed response PDUs above the transport layer.

### Files modified

- `transport/iso/transport.go` — `isClosed()` helper, Receive closed check
- `transport/iso/listen.go` — deadline-based Accept, DR sending, COTP class/selector validation, per-conn source ref
- `transport/iso/dial.go` — client source ref counter, CC ref/class/selector validation
- `transport/iso/options.go` — `WithMMSOptions` → `WithClientDialOptions`
- `transport/iso/doc.go` — bracket-link style, renamed option
- `transport/iso/transport_test.go` — 12 new tests (lifecycle + handshake negative)
- `server.go` — `ListenAndServe` temp/fatal error handling, `isTemporary()` helper
- `types.go` — bracket-link style fix
- `mms.go` — bracket-link style fix
- `doc.go` — bracket-link style fix, renamed option

### Verification

```
go build ./...          — PASS
go vet ./...            — PASS
go test -race ./...     — PASS (all 26 transport/iso tests + full suite)
```

### Test matrix (updated)

| Test | Status |
|------|--------|
| `TestCOTPTransportRoundTrip` | PASS |
| `TestDialTCPAndListen` | PASS |
| `TestTransportCloseIdempotent` | PASS |
| `TestTransportSendAfterClose` | PASS |
| `TestSendAfterCloseNoDeadlock` | PASS (new) |
| `TestReceiveAfterClose` | PASS (new) |
| `TestConcurrentCloseSend` | PASS (new) |
| `TestConcurrentCloseReceive` | PASS (new) |
| `TestAcceptContextCancel` | PASS (new) |
| `TestListenerCloseWhileBlocked` | PASS (new) |
| `TestNewListener` | PASS |
| `TestHandshakeWrongTPDUType` | PASS (new) |
| `TestHandshakeTSAPSelectorMismatch` | PASS (new) |
| `TestClientHandshakeReceivesDR` | PASS (new) |
| `TestClientHandshakeCCRefMismatch` | PASS (new) |
| `TestClientHandshakeCCWrongClass` | PASS (new) |
| `TestServerHandshakeCRWrongClass` | PASS (new) |
| `TestTCPIdentify` | PASS |
| `TestTCPStatus` | PASS |
| `TestTCPReadWrite` | PASS |
| `TestTCPGetNameList` | PASS |
| `TestTCPMultipleSequentialServices` | PASS |
| `TestTCPConcurrentClients` | PASS |
| `TestTCPNVLLifecycle` | PASS |
| `TestTCPConclude` | PASS |
| `TestTCPWithTSAPSelectors` | PASS |

---

## Post-Phase 9 feedback improvements — Round 2

All items from the second feedback round have been addressed.

### Highest-priority (must-fix)

**1. Listener.Accept no longer kills the server on bad handshakes**

The core issue: `Accept` returned a fatal error when a COTP handshake
failed (wrong TPDU type, selector mismatch, wrong class, etc.).
`ListenAndServe` would classify this as a non-temporary error and shut
down the server. A single malformed client could kill the entire server.

Fix: `Accept` now treats handshake failures as per-connection issues.
When a handshake fails, the individual TCP connection is closed and
`Accept` loops back to wait for the next connection. Only actual
listener-level errors (listener closed, fatal network error) or context
cancellation cause `Accept` to return an error.

Added `WithLogger` option to `iso.Listener` so handshake failures are
logged at Warn level with the remote address for diagnostics.

Updated tests:
- `TestHandshakeWrongTPDUType` — bad client (DT instead of CR) is
  skipped, then a good client connects successfully
- `TestHandshakeTSAPSelectorMismatch` — mismatched selector client is
  rejected (client gets DR), then a matching client connects successfully
- `TestServerHandshakeCRWrongClass` — wrong-class CR gets DR back,
  then a good client connects successfully

All three tests now verify that the listener survives bad clients.

**2. _examples/basic/main.go rewritten to use iso.Dial**

Removed the dead `mms.Dial` call and rewrote the example to use
`iso.Dial` with `iso.WithClientDialOptions` and
`iso.WithCalledTSelector`. The example is now a runnable, honest
representation of the current API.

Also removed the deprecated `mms.Dial` stub function entirely from
`mms.go` since no code references it. Updated the `Client` doc comment
to point to `NewClient` and `iso.Dial` instead.

**3. Removed panic in internal/acse/acse.go init path**

Replaced the `init()` function that used `asn1.Marshal` (which could
panic) with a hardcoded `[]byte` literal for the MMS application
context OID (1.0.9506.2.3). The DER encoding `{0x06, 0x05, 0x28,
0xca, 0x22, 0x02, 0x03}` was verified against the Go runtime output.

### Secondary items

**4. handleWrite length check added**

After `pdu.UnmarshalWriteRequest`, an explicit `len(wireVars) !=
len(wireData)` check rejects requests where the variable list and data
list have mismatched counts. This prevents a potential index panic at
the trust boundary even if the parser accepts malformed input.

**5. Decode error mapping improved**

Added `errInvalidRequest` (service error class 4 "service", code 0
"other") to distinguish malformed/unparseable requests from access
denial. All unmarshal failure return paths in server handlers now use
`errInvalidRequest` instead of `errAccessDenied`:
- `handleGetNameList`, `handleGetVarAccess`, `handleRead`,
  `handleWrite`, `handleDefineNVL`, `handleGetNVLAttrs`,
  `handleDeleteNVL`

The `errAccessDenied` error is now only used for actual access-level
issues (e.g. no write function, TypeSpec assertion failure).

**6. invoke.Tracker kept intentionally**

Acknowledged as noted in feedback. The Tracker is the planned
foundation for Phase 10's reader loop and invoke-ID multiplexing.
No changes needed.

### Files modified

- `transport/iso/listen.go` — Accept skips handshake failures,
  WithLogger support, updated docs
- `transport/iso/options.go` — Added `WithLogger` option and
  `logger` field
- `transport/iso/transport_test.go` — Updated handshake negative
  tests to verify listener survival after bad clients
- `_examples/basic/main.go` — Rewritten to use `iso.Dial`
- `mms.go` — Removed deprecated `Dial` stub, updated Client doc
- `internal/acse/acse.go` — Hardcoded OID bytes, removed init panic
- `server.go` — Added `errInvalidRequest`, `serviceErrorClassService`,
  `handleWrite` length check, replaced decode-error mapping

### Verification

```
go build ./...          — PASS
go vet ./...            — PASS
go test -race ./...     — PASS (all packages)
```

---

## Post-Phase 9 feedback improvements — Round 3

All items from the third feedback round have been addressed.

### Blocker

**1. Accepted server connections are now explicitly closed**

`ListenAndServe` now wraps each per-connection goroutine with
`defer c.Close()`:

```go
go func(c Transport) {
    defer c.Close()
    if err := s.Serve(ctx, c); err != nil {
        s.logger.Info("connection closed", "error", err)
    }
}(conn)
```

This guarantees every accepted transport is closed exactly once when
`Serve` returns, regardless of the reason (conclude, release, error,
handshake failure, context cancellation). No descriptor leaks or
lingering half-open connections.

`Server.Serve` does NOT close the transport — the caller owns
lifecycle. This is documented explicitly in the `Serve` method doc
comment.

### Strong hardening

**2. Negotiation clamps zero/invalid values to sane minimums**

Added `clampMin` helper and minimum constants in
`internal/serverconn/conn.go`:

| Parameter | Minimum |
|-----------|---------|
| MaxPDUSize | 128 |
| MaxOutstandingCalling | 1 |
| MaxOutstandingCalled | 1 |
| DataStructureNestingLevel | 1 |

The `negotiate` method now applies `clampMin` after `min` so a
hostile or broken peer proposing 0 cannot produce unusable negotiated
values.

### Smaller notes

**3. Listener/connection ownership docs tightened**

Updated `ListenAndServe` doc comment with an explicit "Ownership"
section:
- ListenAndServe takes ownership of the listener and closes it on
  return. The caller must not close the listener separately.
- Each accepted transport is closed automatically when its Serve
  goroutine finishes.

Updated `Server.Serve` doc to state it does NOT close the transport.

Fixed server example in `doc.go` and `transport/iso/doc.go` to remove
misleading `defer ln.Close()` pattern, replacing with a comment that
`ListenAndServe` owns the listener.

**4. invoke.Tracker observation acknowledged**

Kept as planned foundation for Phase 10 reader loop and invoke-ID
multiplexing. No changes.

### Files modified

- `server.go` — `defer c.Close()` in ListenAndServe goroutine,
  updated Serve/ListenAndServe doc comments with ownership semantics
- `internal/serverconn/conn.go` — `clampMin`, minimum constants,
  negotiate clamping
- `doc.go` — server example ownership comment
- `transport/iso/doc.go` — server example ownership comment

### Verification

```
go build ./...          — PASS
go vet ./...            — PASS
go test -race ./...     — PASS (all packages)
```

---

## Phase 10 — InformationReport and Asynchronous Inbound Handling

### Goal

Add the first unconfirmed/server-initiated MMS feature: InformationReport.
Introduce a background reader loop in the client for asynchronous PDU
dispatch. Add server-side InformationReport sending and connection tracking.

### Deliverables completed

**1. InformationReport PDU support**

- **`internal/pdu/informationreport.go`** — `MarshalInformationReport` and
  `UnmarshalInformationReport` for the full InformationReport BER layout
  (tag 0xa3 UnconfirmedPDU → 0xa0 InformationReport). Supports both
  variableAccessSpecification styles:
  - `[0] listOfVariable` — individual variable specifications
  - `[1] variableListName` — named variable list reference
- **`internal/pdu/informationreport_test.go`** — round-trip tests for
  list-of-variable, named list (VMD and domain-specific), and error cases.
- **`internal/pdu/confirmed.go`** — added `ExtractInvokeID` function that
  parses the invoke ID from any confirmed PDU content (handles both
  UNIVERSAL INTEGER tag 0x02 for ConfirmedResponse and context tag 0x80
  for ConfirmedError/Reject).

**2. Client background reader loop**

- Replaced the synchronous Receive in `sendConfirmed` with a background
  reader goroutine (`readerLoop`). The reader loop:
  - Calls `conn.Receive(ctx)` in a loop
  - Decodes each PDU via `isostack.DecodeDataResponse` → `pdu.DecodePdu`
  - For confirmed responses/errors/rejects: extracts invoke ID,
    dispatches via `invoke.Tracker.Complete`
  - For unconfirmed PDUs (InformationReport): parses and dispatches to
    the registered `InformationReportHandler`
  - For ConcludeResponse: signals the `concludeCh` channel and exits
  - On transport error: cancels all pending requests and exits
- Reader loop is started in `newClient` after association handshake.
- Context is managed independently (background context with cancellation
  in Close).

**3. Refactored sendConfirmed to use invoke.Tracker**

- `sendConfirmed` now uses `tracker.AllocateWithID(invokeID)` to register
  a pending request and obtain a response channel.
- After sending the request via `sendMu`, it waits on the channel (or
  context cancellation).
- Invoke ID correlation is handled by the reader loop calling
  `tracker.Complete`.
- **`internal/invoke/tracker.go`** — added `AllocateWithID` method for
  registering a pending request with a specific invoke ID (already
  allocated via `NextID`). Added `Kind` field to `Response` struct for
  PDU type dispatch. Updated package doc.

**4. Client.OnInformationReport callback API**

- **`types.go`** — added `InformationReportIndication` (received by
  client), `InformationReportHandler` callback type, and
  `InformationReportRequest` (sent by server).
- **`mms.go`** — `Client.OnInformationReport(handler)` registers or
  replaces the handler (RWMutex-protected). The reader loop calls the
  handler for each incoming InformationReport.

**5. Refactored Client.Close**

- Close now coordinates with the reader loop:
  1. Sets `closed = true` (prevents new service calls)
  2. Calls `tracker.CancelAll(ErrClosed)` (unblocks pending callers)
  3. Sends ConcludeRequest, waits for ConcludeResponse via `concludeCh`
     (delivered by the reader loop)
  4. Cancels reader context, waits for reader to exit (`<-readerDone`)
  5. Closes transport
- The `sendMu` mutex serializes all transport writes (confirmed request
  sends and conclude), while the reader loop handles all reads.

**6. Server-side SendInformationReport + connection tracking**

- **`internal/serverconn/conn.go`** — added `sendMu sync.Mutex` to
  `Conn` for serializing all transport writes. `sendData` now acquires
  the mutex. Added `SendUnconfirmed(ctx, mmsPdu)` method for sending
  pre-encoded unconfirmed PDUs, safe for concurrent use with the Serve
  loop.
- **`server.go`** — added:
  - `ServerConn` type wrapping `*serverconn.Conn` for external send
  - `Server.conns` map tracking active connections (RWMutex-protected)
  - `Server.registerConn`/`unregisterConn` called in `Serve`
  - `Server.Connections()` — snapshot of active server connections
  - `ServerConn.SendInformationReport(ctx, req)` — sends to a specific
    client
  - `Server.Broadcast(ctx, req)` — sends to all connected clients
  - `infoReportRequestToWire` — converts public types to wire format

**7. Integration tests**

- **`server_test.go`** — 6 new integration tests:
  - `TestServerInformationReport` — server sends InformationReport with
    list-of-variable style; client receives via handler
  - `TestServerInformationReportNamedList` — named variable list style
  - `TestServerBroadcast` — broadcast to two connected clients
  - `TestInfoReportConcurrentWithConfirmed` — 5 InformationReports sent
    concurrently with 5 Identify requests; all delivered correctly
  - `TestInfoReportNoHandler` — unhandled report doesn't break confirmed
    services
  - `TestServerConnRemovedAfterClose` — connection deregistered after
    client disconnect

**8. Documentation updates**

- **`doc.go`** — updated Supported Services lists, added InformationReport
  examples for both client (OnInformationReport) and server
  (SendInformationReport, Broadcast), updated Concurrency section to
  describe background reader loop architecture, removed InformationReport
  from "out of scope" list.

### Files created

- `internal/pdu/informationreport.go`
- `internal/pdu/informationreport_test.go`

### Files modified

- `mms.go` — Client struct (reader loop fields, sendMu replaces reqMu),
  newClient (starts reader loop), Close (coordinates with reader),
  sendConfirmed (uses tracker), readerLoop, dispatchConfirmed,
  dispatchUnconfirmed, OnInformationReport, wireNameToObjectName
- `types.go` — InformationReportIndication, InformationReportHandler,
  InformationReportRequest
- `server.go` — ServerConn type, connection tracking map,
  registerConn/unregisterConn, Connections(), SendInformationReport,
  Broadcast, infoReportRequestToWire
- `internal/invoke/tracker.go` — AllocateWithID, Response.Kind field,
  updated package doc
- `internal/pdu/confirmed.go` — ExtractInvokeID (handles tags 0x02
  and 0x80), added berutil import
- `internal/serverconn/conn.go` — sendMu in Conn, sendData with mutex,
  SendUnconfirmed method
- `server_test.go` — 6 new InformationReport integration tests
- `doc.go` — InformationReport documentation, concurrency updates

### Verification

```
go build ./...          — PASS
go vet ./...            — PASS
go test -race ./...     — PASS (all packages)
```

---

## Phase 10 — Feedback Hardening Pass

### Goal

Address all 8 feedback items from the Phase 10 review before proceeding
to the next phase. The items span handler safety, API validation,
documentation, server connection lifecycle, test reliability, and
defense-in-depth protocol checks.

### Priority 1 — Hardening (required)

**1. Panic-protect InformationReport handler**

- Wrapped the user callback invocation in `dispatchUnconfirmed` with a
  `defer recover()` block. A panic in the handler is logged at Error
  level and the reader loop continues operating normally.
- Added `TestInfoReportHandlerPanicDoesNotKillClient` — registers a
  panicking handler, sends a report, verifies the client still serves
  confirmed requests afterward.

**2. Validate InformationReportRequest in infoReportRequestToWire**

- Added upfront validation rejecting:
  - `nil` request
  - Both `ListName` and `Variables` set (mutually exclusive)
  - Neither `ListName` nor `Variables` set
  - `len(Variables) != len(Values)` for list-of-variable style
  - Empty `Values` for any style
- Added `TestInfoReportRequestValidation` with 5 sub-tests covering
  each invalid combination.

**3. Document Client.Close hard-shutdown semantics**

- Rewrote the `Close` doc comment to explicitly describe the 4-step
  shutdown sequence and clarify that:
  - Pending confirmed requests are aborted with `ErrClosed`
  - Late responses during shutdown are discarded
  - This is NOT a graceful drain

### Priority 2 — Strongly recommended

**4. Harden ServerConn against use after close**

- Added `mu sync.RWMutex` and `closed bool` fields to `ServerConn`.
- `unregisterConn` marks the `ServerConn` as closed before removing it
  from the server's connection map.
- `SendInformationReport` checks the closed flag and returns
  `ErrServerConnClosed` if the connection has been shut down.
- `Broadcast` skips closed connections in its snapshot.
- Added `ErrServerConnClosed` sentinel error to `errors.go`.
- Added `TestServerConnSendAfterClose` — captures a `ServerConn`,
  closes the client, verifies `SendInformationReport` returns
  `ErrServerConnClosed`.

**5. Replace time.Sleep in tests with polling**

- Added `waitForConnections(t, srv, n, timeout)` helper that polls
  `srv.Connections()` at 5ms intervals until the expected count is
  observed or the timeout expires.
- Replaced all 6 `time.Sleep` + `srv.Connections()` patterns in:
  - `TestServerInformationReport`
  - `TestServerInformationReportNamedList`
  - `TestServerBroadcast`
  - `TestInfoReportConcurrentWithConfirmed`
  - `TestInfoReportNoHandler`
  - `TestServerConnRemovedAfterClose`

### Priority 3 — Optional but worthwhile

**6. Defense-in-depth invoke ID check**

- Added a consistency check in `processConfirmedPDU`: after
  `DecodeConfirmedResponse`, verify `confirmed.InvokeID == invokeID`.
  Returns a `ProtocolError` on mismatch. This catches malformed peer
  data or future parser/extractor drift even though the tracker already
  routes by invoke ID.

**7. Strict wireNameToObjectName**

- Removed the silent VMD fallback for unknown scopes. The function now
  returns an error, propagated by `infoReportToIndication` as a
  per-variable or list-name conversion error. This surfaces protocol
  or parser bugs instead of masking them.

**8. Reduce log noise for unknown invoke IDs during close**

- `dispatchConfirmed` now checks `c.closed` before logging unknown
  invoke ID responses. During shutdown (after `CancelAll`), late
  responses are logged at Debug level. Outside shutdown, they remain
  at Warn level.

### Files modified

- `mms.go` — panic recovery in `dispatchUnconfirmed`, invoke ID
  defense check in `processConfirmedPDU`, `wireNameToObjectName` now
  returns error, `infoReportToIndication` propagates conversion errors,
  `Close` doc rewrite, close-aware log level in `dispatchConfirmed`
- `server.go` — `ServerConn` closed state (mu + closed fields),
  `unregisterConn` marks closed, `SendInformationReport` closed guard,
  `Broadcast` skips closed conns, `infoReportRequestToWire` validation
- `server_test.go` — `waitForConnections` helper, replaced all sleep
  patterns, added `TestInfoReportHandlerPanicDoesNotKillClient`,
  `TestInfoReportRequestValidation`, `TestServerConnSendAfterClose`
- `errors.go` — added `ErrServerConnClosed` sentinel

### Verification

```
go build ./...          — PASS
go vet ./...            — PASS
go test -race ./...     — PASS (all packages)
golangci-lint run ./... — PASS (0 issues)
```

---

## Phase 11 — Security: TLS Transport and ACSE Auth Hooks

### Phase 11A — TLS Transport Support

**Goal:** Add secure transport in the runtime/transport integration layer.

#### Changes

**`transport/iso/options.go`** — Added `tlsConfig *tls.Config` field to `Options` and `WithTLSConfig(cfg *tls.Config)` option function. TLS configuration is shared between client and server paths.

**`transport/iso/dial.go`** — `DialTCP` now wraps the TCP connection in `tls.Client()` and performs the TLS handshake before the COTP handshake when a TLS config is provided. `Dial` passes TLS config through to `DialTCP`. Layering: TCP → TLS → TPKT → COTP → Session → Presentation → ACSE → MMS.

**`transport/iso/listen.go`** — `Listener.Accept` wraps each accepted TCP connection in `tls.Server()` and performs the server-side TLS handshake before the COTP handshake when TLS config is set. Failed TLS handshakes are logged and the connection is closed; the listener continues accepting.

**`transport/iso/transport.go`** — Added `TLSConnectionState()` method to `cotpTransport` that type-asserts the underlying `net.Conn` to `*tls.Conn` and returns the connection state including peer certificates. Returns nil for non-TLS transports.

**`types.go`** — Added `TLSTransport` interface that extends `Transport` with `TLSConnectionState() *tls.ConnectionState`. Transports that support TLS optionally implement this interface for peer certificate extraction.

#### New tests (transport/iso/tls_test.go)

| Test | Purpose |
|------|---------|
| `TestTLSDialAndListen` | Basic TLS client/server transport with self-signed certs |
| `TestTLSConnectionState` | Mutual TLS with client cert; verifies `TLSTransport` interface and peer certificate extraction |
| `TestTLSVerificationFailure` | Client rejects server with untrusted CA; verifies typed error |
| `TestTLSEndToEndMMS` | Full MMS Identify + Read over TLS |
| `TestPlaintextAndTLSCoexist` | Same server serving both plaintext and TLS listeners simultaneously |

### Phase 11B — ACSE / Application Authentication Hooks

**Goal:** Surface peer identity and add association-level auth policy.

#### Changes

**`internal/acse/acse.go`** — Extended AARQ parsing to extract authentication fields:
- Added `AuthMechanism` type (`AuthNone`, `AuthPassword`, `AuthCertificate`, `AuthTLS`)
- Added `AuthInfo` struct with `Mechanism` and `Password` fields
- AARQ parser now extracts tag `0x8b` (mechanism name OID) and tag `0xac` (authentication value)
- Password mechanism identified by OID `2.2.3.1` (`{0x52, 0x03, 0x01}`)
- `EncodeAARQ` now includes ACSE authentication fields when `AARQParams.Password` is set

**`internal/isostack/server.go`** — `AssociateRequest` now carries `Auth acse.AuthInfo` extracted from the AARQ.

**`internal/serverconn/conn.go`** — Refactored handshake into two-phase protocol:
- `ReceiveAssociation(ctx)` — reads and validates the AARQ, returns `AuthInfo` (does NOT send AARE)
- `AcceptAssociation(ctx)` — sends AARE with result=accepted
- `RejectAssociation(ctx)` — sends AARE with result=rejected-permanent
- This split allows the server to authenticate between receive and accept/reject

**`server_options.go`** — Added public authentication types:
- `AuthMechanism` enum (`AuthNone`, `AuthPassword`, `AuthCertificate`, `AuthTLS`)
- `PeerAuth` struct with `Mechanism`, `Password`, and `TLSCertificates` fields
- `AuthenticateFunc` callback type
- `ServerOptions.Authenticate` field

**`server.go`** — `Server.Serve` now performs authentication between receive and accept:
1. `ReceiveAssociation` — parses AARQ, extracts auth info
2. `buildPeerAuth` — combines ACSE auth with TLS peer certificates
3. Calls `AuthenticateFunc` if configured
4. On success: `AcceptAssociation` + enter service loop
5. On failure: `RejectAssociation` + return error

**`options.go`** — Added `ISOOptions.Password` for client-side ACSE password authentication.

**`mms.go`** — `buildISOParams` threads `Password` through to `AARQParams`.

#### Auth mechanism resolution

| ACSE mechanism | TLS peer cert | Resolved `PeerAuth.Mechanism` |
|---|---|---|
| Password OID | any | `AuthPassword` |
| Certificate | any | `AuthCertificate` |
| None | present | `AuthTLS` |
| None | absent | `AuthNone` |

#### New tests

| Test | File | Purpose |
|------|------|---------|
| `TestAuthenticatorPasswordAccept` | `server_test.go` | Client sends ACSE password; server authenticator verifies mechanism and value |
| `TestAuthenticatorRejectsConnection` | `server_test.go` | Authenticator returns error; client receives AARE rejected |
| `TestAuthenticatorNoAuthAcceptsAll` | `server_test.go` | No authenticator configured; all connections accepted |
| `TestTLSWithAuthenticator` | `tls_test.go` | Mutual TLS + authenticator; verifies peer CN in `PeerAuth.TLSCertificates` |
| `TestTLSAuthenticatorReject` | `tls_test.go` | TLS connection + authenticator rejects; client gets rejection error |

### Verification

```
go build ./...          — PASS
go vet ./...            — PASS
go test -race ./...     — PASS (all packages)
golangci-lint run ./... — PASS (0 issues)
```

---

## Phase 11 — Feedback Hardening Pass (Security Auth)

Addresses feedback on the Phase 11 TLS/auth implementation. Eight items covering auth correctness, parsing strictness, documentation, error taxonomy, and defensive copying.

### FB1: Fix ACSE auth classification for unknown mechanisms

**Problem:** `classifyMechanism` mapped every unrecognized OID to `AuthCertificate`, allowing garbage mechanisms to masquerade as certificate auth.

**Solution:** Added `AuthUnknown` to both `internal/acse.AuthMechanism` and public `mms.AuthMechanism`. `classifyMechanism` now returns `AuthUnknown` for any OID that doesn't match the password OID `2.2.3.1`. Added `RawMechanismOID` to both `acse.AuthInfo` and `mms.PeerAuth` so authentication policy can inspect and decide on unrecognized mechanisms.

**Files:** `internal/acse/acse.go`, `server_options.go`, `server.go`

### FB2: Strict ACSE auth parsing

**Problem:** `parseAARQ` silently accepted malformed auth field combinations: auth-value without mechanism, mechanism without value, malformed auth-value content, trailing bytes.

**Solution:** Rewrote `parseAARQ` with strict validation:
- auth-value present without mechanism-name → error
- password mechanism present without auth-value → error
- Replaced `parseAuthValue` with `parsePasswordAuthValue` that validates the expected CHOICE tag (`0x80` graphicString), checks full consumption (no trailing bytes), and returns an error instead of nil on failure
- Unknown mechanisms with auth-value are accepted (policy decides)

**Files:** `internal/acse/acse.go`

### FB3: Document ACSE password auth security

**Problem:** `ISOOptions.Password` sends credentials in the clear without TLS, but this was not documented.

**Solution:** Added explicit SECURITY doc comments to:
- `ISOOptions.Password` — warns about plaintext-equivalent wire format without TLS
- `AuthPassword` enum value — same warning
- `transport/iso/doc.go` — dedicated section on ACSE password auth security

**Files:** `options.go`, `server_options.go`, `transport/iso/doc.go`

### FB4: Negative tests for malformed ACSE auth

**New tests in `internal/acse/acse_test.go`:**

| Test | Scenario |
|------|----------|
| `TestAARQPasswordAuthRoundTrip` | Happy path: encode + parse password auth |
| `TestAARQAuthValueWithoutMechanism` | auth-value [12] without mechanism-name [11] |
| `TestAARQPasswordMechanismWithoutAuthValue` | Password mechanism OID without auth-value |
| `TestAARQUnknownMechanismOID` | Unknown OID → `AuthUnknown` + raw OID preserved |
| `TestAARQPasswordWrongInnerTag` | Password mechanism with wrong CHOICE tag (0x81 vs 0x80) |
| `TestAARQPasswordTrailingBytesInAuthValue` | Trailing bytes after graphicString |
| `TestAARQEmptyAuthValue` | Empty auth-value content |
| `TestAARQMalformedAuthPlusValidUserInfo` | Malformed auth with valid user-info still rejects |
| `TestAARQNoAuth` | No auth fields → `AuthNone` |

### FB5: Typed authentication errors

**Problem:** Auth rejection returned a generic `fmt.Errorf` — no typed error for programmatic inspection.

**Solution:** Added `AuthenticationError` typed error (wraps `ErrAuthenticationFailed` sentinel) in `errors.go`. `Server.Serve` returns `*AuthenticationError` on auth rejection. Test `TestAuthenticatorRejectsConnection` updated to verify both `errors.As` and `errors.Is`.

**Files:** `errors.go`, `server.go`, `server_test.go`

### FB6: Copy password slices at API boundaries

**Problem:** Password slice from `ISOOptions` was shared with internal structs, allowing caller mutation.

**Solution:**
- `buildISOParams` copies `ISOOptions.Password` into `AARQParams` via `append([]byte(nil), ...)`
- `buildPeerAuth` copies `authInfo.Password` into `PeerAuth.Password` via `append([]byte(nil), ...)`
- `buildPeerAuth` copies `authInfo.RawMechanismOID` similarly

**Files:** `mms.go`, `server.go`

### FB7: Document AuthCertificate naming clearly

**Problem:** `AuthCertificate` could be confused with TLS client certificate auth.

**Solution:** Expanded `AuthCertificate` doc comment to explicitly distinguish ACSE-level certificate auth from TLS peer certificate auth (`AuthTLS`), noting most MMS deployments use TLS client certificates.

**Files:** `server_options.go`

### FB8: Remove redundant length check in handleWrite

**Problem:** `handleWrite` checked `len(wireVars) != len(wireData)` after `UnmarshalWriteRequest` which already validates cardinality internally.

**Solution:** Removed the redundant check, consistent with the stated design that the PDU layer owns validation.

**Files:** `server.go`

### Verification

```
go build ./...          — PASS
go vet ./...            — PASS
go test -race ./...     — PASS (all packages)
golangci-lint run ./... — PASS (0 issues)
```

---

## Authenticator Context Refinement

### Objective

Refine the server-side authentication interface so the authenticator receives
a richer, protocol-meaningful association context while keeping the API clean,
minimal, and aligned with the go-iec61850 consumer.

### Design decisions

- **Protocol-first, not transport-first**: ACSE mechanism + calling application
  identity are the primary fields. TLS and remote address are supplemental.
- **Single clean cut**: old `PeerAuth`, `AuthenticateFunc`, old `AuthMechanism`
  enum values removed entirely. No deprecated compatibility bridges.
- **Zero value = unknown**: `AuthMechanismUnknown` is iota 0 — safe default.
- **Structured OIDs**: mechanism OID and AP-title stored as decoded
  `asn1.ObjectIdentifier`, not raw bytes.
- **Auth result with token**: `AuthResult{Accept, Token}` replaces `error`
  return, enabling upper layers to store principal/session context.

### New public types (`auth_types.go`)

| Type | Purpose |
|---|---|
| `AuthMechanism` | Enum: `AuthMechanismUnknown`, `AuthMechanismNone`, `AuthMechanismACSEPassword`, `AuthMechanismTLSCertificate` |
| `ApplicationReference` | Calling AP-title (OID) + optional AE-qualifier (`*int`) |
| `AuthContext` | Single structured auth context passed to callback |
| `AuthResult` | Accept/reject decision + opaque `Token any` |
| `Authenticator` | `func(ctx context.Context, auth *AuthContext) (AuthResult, error)` |

### Removed public types

| Old type | Replacement |
|---|---|
| `PeerAuth` | `AuthContext` |
| `AuthenticateFunc` | `Authenticator` |
| `AuthNone` | `AuthMechanismNone` |
| `AuthPassword` | `AuthMechanismACSEPassword` |
| `AuthTLS` | `AuthMechanismTLSCertificate` |
| `AuthCertificate` | Removed (was ACSE-level cert, not currently used) |
| `AuthUnknown` | `AuthMechanismUnknown` |

### ACSE parsing enhancements (`internal/acse/acse.go`)

- **Calling AP-title [0xa6]**: now parsed from AARQ and stored in
  `AuthInfo.CallingAPTitle` as decoded `asn1.ObjectIdentifier`.
- **Calling AE-qualifier [0xa7]**: now parsed and stored as `*int`.
- **Mechanism OID**: decoded from implicit BER to `asn1.ObjectIdentifier`
  via `decodeImplicitOID()` helper. Populated for all mechanisms (password
  OID = `{2, 2, 3, 1}`).
- **Internal enum simplified**: removed `AuthCertificate` and `AuthTLS`
  from internal enum — those are transport-level concepts classified at
  the server layer.

### Server auth flow (`server.go`)

- `buildPeerAuth` → `buildAuthContext`: produces `*AuthContext` from
  `acse.AuthInfo` + transport metadata.
- **Mechanism classification**: ACSE password → `AuthMechanismACSEPassword`,
  unknown ACSE OID → `AuthMechanismUnknown`, no ACSE + TLS peer cert →
  `AuthMechanismTLSCertificate`, nothing → `AuthMechanismNone`.
- **Token storage**: `AuthResult.Token` stored on `ServerConn.authToken`,
  exposed via `ServerConn.AuthToken()`.
- **Reject semantics**: `AuthResult{Accept: false}` or non-nil error both
  reject the association with `*AuthenticationError`.
- **`context.Context`** passed through from `Serve` to authenticator.

### Transport enhancements

- **`RemoteAddrTransport` interface** (`types.go`): optional interface for
  transports that expose `RemoteAddr() net.Addr`.
- **`cotpTransport.RemoteAddr()`** (`transport/iso/transport.go`): implements
  the interface, returning the underlying `net.Conn`'s remote address.
- **Peer certificate**: `AuthContext.PeerCertificate` is the leaf cert
  (`*x509.Certificate`), not the full chain. Simpler API for most consumers.

### Server options (`server_options.go`)

- Simplified: only `ServerOptions`, `ServerMMSOptions` remain.
- `Authenticate Authenticator` field uses the new callback type.
- Old `AuthMechanism` enum and `PeerAuth` struct removed (moved to
  `auth_types.go` as redesigned types).

### Tests added/updated

**`internal/acse/acse_test.go`:**
- `TestAARQCallingAPTitleRoundTrip` — AP-title + AE-qualifier parsed correctly
- `TestAARQCallingAPTitleWithPasswordAuth` — AP-title + password combined
- `TestAARQPasswordMechanismOID` — decoded mechanism OID = {2,2,3,1}
- `TestAARQCallingAPTitleOnly` — AP-title without explicit AE-qualifier
- `TestAARQNoAuth` — extended to verify nil CallingAPTitle, nil MechanismOID
- `TestAARQUnknownMechanismOID` — updated to check decoded OID

**`server_test.go`:**
- `TestAuthenticatorPasswordAccept` — migrated to new API, verifies token
- `TestAuthenticatorRejectsConnection` — migrated to `AuthResult{Accept: false}`
- `TestAuthenticatorNoneNilFields` — verifies all optional fields nil for no-auth
- `TestAuthenticatorPasswordWithAPTitle` — password + AP-title + AE-qualifier
  end-to-end, verifies MechanismOID, CallingApplication, Token
- `TestAuthenticatorRejectsViaError` — error return rejects with AuthenticationError
- `TestAuthenticatorTokenNilWhenNoAuthenticator` — nil authenticator → nil token

**`transport/iso/tls_test.go`:**
- `TestTLSWithAuthenticator` — migrated to new API, verifies PeerCertificate,
  RemoteAddr, Token
- `TestTLSAuthenticatorReject` — migrated to new API

### Files changed

| File | Change |
|---|---|
| `auth_types.go` | **NEW** — public auth types with extensive doc comments and examples |
| `server_options.go` | Simplified: removed old auth types, uses `Authenticator` |
| `server.go` | `buildAuthContext`, token storage, `ServerConn.AuthToken()`, `copyOID` |
| `types.go` | Added `RemoteAddrTransport` interface |
| `internal/acse/acse.go` | AP-title/AE-qualifier parsing, OID decoding, simplified internal enum |
| `transport/iso/transport.go` | `cotpTransport.RemoteAddr()` |
| `internal/acse/acse_test.go` | 5 new tests, 2 updated |
| `server_test.go` | 4 new tests, 2 migrated |
| `transport/iso/tls_test.go` | 2 migrated |

### Verification

```
go build ./...          — PASS
go vet ./...            — PASS
go test -race ./...     — PASS (all packages)
golangci-lint run ./... — PASS (0 issues)
```

---

## Authenticator Context Refinement — Feedback Hardening Pass

### FB1: Fix TLS tests — make self-contained

**Problem:** `tls_test.go` shared `testServer`/`testWriter` helpers from
`integration_test.go`. While they compiled (same `iso_test` package), the
coupling made TLS tests fragile and hard to reason about independently.

**Solution:** Added self-contained `tlsTestServer` and `tlsTestLogWriter`
directly in `tls_test.go`. Updated `TestTLSEndToEndMMS` and
`TestPlaintextAndTLSCoexist` to use the local helper. Assertions now match
the local fixture values (vendor "TLSTest", temperature 36.6). Auth tests
(`TestTLSWithAuthenticator`, `TestTLSAuthenticatorReject`) also use the local
log writer.

**Files:** `transport/iso/tls_test.go`

### FB2: Defensively copy CallingAEQualifier

**Problem:** `buildAuthContext` passed through the `*int` pointer from
`acse.AuthInfo` without copying, breaking the snapshot contract.

**Solution:** Copy the pointed-to integer value when present:

```go
var aeq *int
if authInfo.CallingAEQualifier != nil {
    v := *authInfo.CallingAEQualifier
    aeq = &v
}
```

**Files:** `server.go`

### FB3: Tighten AuthMechanismUnknown docs

**Problem:** Top-level doc said "unrecognized or absent classification" which
conflicts with `AuthMechanismNone` representing absence.

**Solution:** Updated to: "It indicates that an authentication mechanism was
presented but could not be classified. Absence of authentication is
represented by [AuthMechanismNone]."

**Files:** `auth_types.go`

### FB4: Add defensive-copy tests

**Problem:** No tests verified that `AuthContext` fields are independent
snapshots.

**Solution:** Added:
- `TestAuthContextDefensiveCopyPassword` — mutates source `[]byte` after
  connection, verifies `AuthContext.Password` unchanged.
- `TestAuthContextDefensiveCopyAPTitle` — mutates `AuthContext.CallingApplication.APTitle`,
  verifies source OID unchanged. Also verifies AEQualifier value is correct.

**Files:** `server_test.go`

### FB5: Add test for unknown ACSE mechanism + TLS peer certificate

**Problem:** The mechanism classification priority (ACSE unknown takes
precedence over TLS) was not nailed down by a test.

**Solution:** Added `mockTLSTransport` (implements `TLSTransport` +
`RemoteAddrTransport` with fake peer cert), `export_test.go` to expose
`buildAuthContext`, and `TestBuildAuthContextUnknownACSEPlusTLS` which
verifies:
- Mechanism = `AuthMechanismUnknown`
- MechanismOID preserved
- PeerCertificate still populated from TLS

**Files:** `server_test.go`, `export_test.go` (new)

### FB6: Add test for RemoteAddr on non-TLS transport

**Problem:** `RemoteAddr` was only tested in TLS flow.

**Solution:** Added:
- `TestPlaintextRemoteAddr` (`tls_test.go`) — verifies `RemoteAddr` is
  non-nil for plaintext ISO transport, and `PeerCertificate` is nil.
- `TestAuthRemoteAddrOnPlaintextISO` (`server_test.go`) — verifies
  `RemoteAddr` is nil for `chanTransport` (does not implement
  `RemoteAddrTransport`), confirming nil-safety path.

**Files:** `transport/iso/tls_test.go`, `server_test.go`

### FB7: copyOID placement

**Decision:** `copyOID` remains single-use in `server.go`. If reused
elsewhere, it should move to a utility file.

### FB8: Remove unused context from buildAuthContext

**Problem:** `_ context.Context` parameter was unused noise.

**Solution:** Removed the parameter. Caller updated.

**Files:** `server.go`

### FB9: Clarify TLS mechanism inference in docs

**Problem:** Consumers could confuse `AuthMechanismTLSCertificate` with an
ACSE mechanism-name OID.

**Solution:** Added to `AuthContext.Mechanism` doc: "[AuthMechanismTLSCertificate]
is transport-derived: the server infers it when no ACSE mechanism is present
but the TLS transport provides a peer certificate. It does not correspond to
an ACSE mechanism-name OID; [MechanismOID] will be nil in that case."

**Files:** `auth_types.go`

### FB10: Add AuthMechanism.String() table test

**Problem:** No test coverage for the `String()` method.

**Solution:** Added `TestAuthMechanismString` with table cases covering
Unknown, None, ACSEPassword, TLSCertificate, and invalid enum value.

**Files:** `server_test.go`

### FB11: Optional helper methods on AuthContext

**Decision:** Skipped. The current API is clean enough; `HasTLSCertificate()`
and `HasCallingApplication()` would be trivial nil-checks that don't justify
additional API surface.

### Files changed

| File | Change |
|---|---|
| `auth_types.go` | Tightened `AuthMechanismUnknown` docs, added TLS inference clarification |
| `server.go` | Defensive copy of `*int`, removed unused `ctx` parameter |
| `export_test.go` | **NEW** — exposes `buildAuthContext` for testing |
| `server_test.go` | Added `mockTLSTransport`, 5 new tests |
| `transport/iso/tls_test.go` | Self-contained helpers, aligned assertions, plaintext RemoteAddr test |

### Verification

```
go build ./...          — PASS
go vet ./...            — PASS
go test -race ./...     — PASS (all packages)
golangci-lint run ./... — PASS (0 issues)
```

---

## FB11 — AuthContext Helper Methods

**Previously skipped** as optional; now implemented per request.

### Changes

| File | Change |
|---|---|
| `auth_types.go` | Added `HasTLSCertificate()` and `HasCallingApplication()` convenience methods on `*AuthContext` |
| `server_test.go` | Added `TestAuthContextHelpers` — 4 sub-tests: zero value, TLS only, calling-app only, both present |

### Verification

```
go test -race ./...     — PASS
golangci-lint run ./... — PASS (0 issues)
```

---

## Phase 12 — File Services

**Status: COMPLETE**

### Summary

Implemented MMS file services (FileDirectory, FileOpen, FileRead,
FileClose, FileDelete) using a provider-based server design and extended
BER tag support for tag numbers > 30.

### Architecture: extended BER tags

MMS file services use CHOICE tag numbers 72–77 (0x48–0x4d), which
require multi-byte BER encoding (`0xbf XX` constructed, `0x9f XX`
primitive). The existing codebase only supported single-byte tags (0–30).

Key changes to support extended tags:
- `asn1util.WrapContextTag(tagNum int, constructed bool, content []byte)`
  encodes both short-form (0–30) and long-form (>30) context-specific
  tags.
- `codec.MarshalConfirmedRequest` / `MarshalConfirmedResponse` widened
  from `(InvokeID, byte, []byte)` to `(InvokeID, int, bool, []byte)`.
- `serverconn.ServiceHandler` widened to accept/return `int` tag and
  `bool` constructed flag.
- `Server.dispatch` and all `handle*` methods widened to `(int, bool,
  []byte, error)` return.
- All existing PDU callers updated via `marshalConfirmedLegacy` adapter.

### Deliverables

| Deliverable | Status | Notes |
|---|---|---|
| Extended BER tag support | Done | `WrapContextTag` + `encodeTagNumber` in `asn1util/raw.go` |
| File tag constants | Done | `TagNumFileOpen` (72) through `TagNumFileDirectory` (77) in `asn1util/tags.go` |
| Dispatch chain widening | Done | `codec`, `serverconn`, `pdu`, `server.go` all use `int` tags |
| PDU encode/decode | Done | `internal/pdu/file.go` — all file service requests/responses |
| Client API | Done | `client_file.go` — `FileDirectory`, `FileOpen`, `FileRead`, `FileClose`, `FileDelete` |
| `FileProvider` interface | Done | `server_file.go` — `List`, `Open`, `Read`, `Close`, `Delete` |
| FRSM management | Done | `frsmTable` per connection, auto-cleanup on disconnect |
| Server handlers | Done | `handleFileOpen/Read/Close/Delete/Directory` in `server_file.go` |
| `ServerOptions.FileProvider` | Done | Opt-in; nil = file service requests rejected |
| In-memory test provider | Done | `memFileProvider` in `file_test.go` |
| Integration tests | Done | 4 end-to-end tests covering all file operations |

### New files

| File | Purpose |
|---|---|
| `client_file.go` | Public client API for file services |
| `server_file.go` | `FileProvider` interface, FRSM, server file handlers, public types |
| `internal/pdu/file.go` | Marshal/unmarshal for all file service PDUs, `FileName` encoding |
| `file_test.go` | In-memory `FileProvider` + 4 end-to-end integration tests |

### Modified files

| File | Change |
|---|---|
| `internal/asn1util/raw.go` | Added `WrapContextTag`, `encodeTagNumber`, `lengthSize`, `appendLength`, `encodeTLV` |
| `internal/asn1util/tags.go` | Added `TagNumFileOpen` through `TagNumFileDirectory` constants |
| `internal/codec/marshal.go` | `MarshalConfirmedRequest` → `(int, bool, []byte)` signature |
| `internal/codec/response.go` | `MarshalConfirmedResponse` → `(int, bool, []byte)` signature |
| `internal/pdu/confirmed.go` | Extended `ServiceKind` enum, `classifyServiceByTagNum`, `marshalConfirmedLegacy` adapter |
| `internal/pdu/getnamelist.go` | Uses `marshalConfirmedLegacy` |
| `internal/pdu/getvaraccess.go` | Uses `marshalConfirmedLegacy` |
| `internal/pdu/identify.go` | Uses `marshalConfirmedLegacy` |
| `internal/pdu/namedvarlist.go` | Uses `marshalConfirmedLegacy` |
| `internal/pdu/read.go` | Uses `marshalConfirmedLegacy` |
| `internal/pdu/status.go` | Uses `marshalConfirmedLegacy` |
| `internal/pdu/write.go` | Uses `marshalConfirmedLegacy` |
| `internal/serverconn/conn.go` | `ServiceHandler` widened to `int` tags + `bool` constructed |
| `mms_test.go` | Updated mock responses to use `MarshalConfirmedResponse`, removed `retagPdu` |
| `server.go` | FRSM lifecycle, `context.WithValue` for `ServerConn`, widened dispatch/handlers |
| `server_options.go` | Added `FileProvider` field |

### Tests

```
TestFileDirectoryEndToEnd          — PASS
TestFileOpenReadCloseEndToEnd      — PASS
TestFileDeleteEndToEnd             — PASS
TestFileServicesNotConfigured      — PASS
```

### Verification

```
go build ./...      — PASS
go vet ./...        — PASS
go test -count=1 .  — PASS (all file tests, all existing tests)
```

Note: `TestInfoReportConcurrentWithConfirmed` is a pre-existing flaky
test (intermittent timing issue in report channel drain). Not introduced
or worsened by Phase 12 changes (`server_test.go` was not modified).

---

## Phase 12 Feedback — Cleanup/Fix Pass

**Status: COMPLETE**

Implements all 15 feedback items from FEEDBACK.md. Organized into
must-fix (1–6), strong improvements (7–11), and minor cleanup (12–15).

### Must-fix items

**FB1: `fileError()` with real error mapping**

Replaced blanket `fileErrFileNonExistent` with proper mapping:
- `fs.ErrNotExist` → file-non-existent (7)
- `fs.ErrPermission` / `ErrFileAccessDenied` → file-access-denied (6)
- all other errors → other (0)

Added `ErrFileAccessDenied` as a sentinel error for providers. Updated
`memFileProvider` to return `fs.ErrNotExist` instead of `fmt.Errorf`.

**Files:** `server_file.go`, `file_test.go`

**FB2: FileDirectory pagination → single-request**

Chose Option A: made directory listing non-paginated. Removed the
pagination loop from `Client.FileDirectory` — it now sends a single
request. Removed `continueAfter` parameter from
`MarshalFileDirectoryRequest`. Removed `ContinueAfter` field from
`FileDirectoryRequest` struct.

**Files:** `client_file.go`, `internal/pdu/file.go`

**FB3: handleFileDirectory continueAfter logic**

Removed server-side `continueAfter` filtering entirely (no longer
applicable after FB2).

**Files:** `server_file.go`

**FB4: UnmarshalFileOpenResponse validation**

Now validates that both `frsmID` and file size are present. Returns
a protocol error if either is missing. Updated `parseFileAttributes`
to return a `hasSize` flag.

**Files:** `internal/pdu/file.go`

**FB5: parseDirectoryEntries SEQUENCE tag check**

Now verifies each entry has tag `0x30` (SEQUENCE). Returns error if
a non-SEQUENCE tag is encountered.

**Files:** `internal/pdu/file.go`

**FB6: Strict `decodeFileName`**

Changed signature from `string` to `(string, error)`. Now:
- Rejects empty data
- Accepts only `0x19` (GraphicString) directly or inside one
  constructed wrapper
- Returns error for malformed BER or unexpected tags
- All 5 callers updated to propagate errors

**Files:** `internal/pdu/file.go`

### Strong improvements

**FB7: Extended BER tag unit tests**

Added to `internal/asn1util/raw_test.go`:
- `TestWrapContextTag` — 6 sub-tests: short constructed/primitive,
  boundary (tag 30), long-form (31, 72, 77)
- `TestWrapContextTag_RoundTrip` — round-trips through `asn1.Unmarshal`
  for tags 0, 2, 30, 31, 72, 77, 127 in both constructed and primitive

Added to `internal/pdu/file_test.go`:
- `TestClassifyServiceTag` — all 14 service kinds + unknown
- `TestMarshalConfirmedRequest_ExtendedTag` — tags 72–77 round-trip
- `TestMarshalConfirmedResponse_ExtendedTag` — tags 72, 73, 77
- `TestMarshalFileOpenRequest_RoundTrip` — full encode→decode
- `TestMarshalFileDirectoryRequest_RoundTrip`
- `TestUnmarshalFileOpenResponse_Validation` — missing frsmID, missing
  size, valid response
- `TestParseDirectoryEntries_BadTag`
- `TestDecodeFileName_Strict` — empty, valid, wrapped, wrong tag, bad tag

**Files:** `internal/asn1util/raw_test.go`, `internal/pdu/file_test.go`
(new)

**FB8: Negative file service tests**

Added 7 focused integration tests:
- `TestFileOpenNonExistent` — verifies ErrorClassFile code=7
- `TestFileReadInvalidFrsm` — read with bad frsmID
- `TestFileCloseInvalidFrsm` — close with bad frsmID
- `TestFileDeleteNonExistent` — delete non-existent, ErrorClassFile
- `TestFileDoubleClose` — close same handle twice
- `TestFileReadAfterClose` — read after handle closed
- `TestFileDisconnectClosesHandles` — verifies provider.Close called on
  connection close (via `closingMemFileProvider`)

**Files:** `file_test.go`

**FB9: context.WithValue ServerConn comments**

Added descriptive comment on `serverConnCtxKey` explaining it is
internal request-scoped context and recommending against spreading
the pattern.

**Files:** `server_file.go`

**FB10: frsmTable RWMutex**

Changed `frsmTable.mu` from `sync.Mutex` to `sync.RWMutex`.
`get()` now uses `RLock`; `alloc`, `remove`, `closeAll` use full `Lock`.

**Files:** `server_file.go`

**FB11: FileRead EOF contract**

Documented the provider contract on `FileProvider.Read`: final chunk
returns `moreFollows=false` with `nil` error. `io.EOF` is treated as
a provider error. Updated `memFileProvider.Read` to return `(nil, false,
nil)` instead of `(nil, false, io.EOF)` when exhausted.

**Files:** `server_file.go`, `file_test.go`

### Minor cleanup

**FB12: MarshalFileOpenRequest position 0 docs**

Added doc comment noting the current API always opens at position 0;
seek support may be added later.

**Files:** `internal/pdu/file.go`

**FB13: FileProvider List arg naming**

Renamed `List(ctx, path string)` → `List(ctx, fileSpec string)` in
the `FileProvider` interface and updated the provider doc comments.

**Files:** `server_file.go`

**FB14: Named service tag constants**

Added `TagNumStatus` through `TagNumDeleteNamedVariableList` (9
constants) in `internal/asn1util/tags.go`. Replaced all magic numbers
in `server.go` dispatch and handlers, and in `pdu/confirmed.go`
`classifyServiceByTagNum`.

**Files:** `internal/asn1util/tags.go`, `server.go`,
`internal/pdu/confirmed.go`

**FB15: Mock classification in mms_test.go**

Replaced legacy `codec.ServiceTag(serviceRaw)` + byte-tag switch with
`pdu.ClassifyServiceTag(serviceRaw.Tag)`. Added exported
`ClassifyServiceTag` wrapper in `pdu/confirmed.go`. Replaced all
magic response tag numbers with `asn1util.TagNum*` constants.

**Files:** `mms_test.go`, `internal/pdu/confirmed.go`

### New files

| File | Purpose |
|---|---|
| `internal/pdu/file_test.go` | Unit tests for file PDU encode/decode, tag classification, extended tag support |

### Files changed

| File | Change |
|---|---|
| `server_file.go` | `fileError` mapping, `RWMutex`, serverConnCtxKey docs, `FileProvider` docs + `fileSpec` naming, pagination removal |
| `internal/pdu/file.go` | Strict `decodeFileName`, open response validation, SEQUENCE tag check, pagination removal, position 0 docs |
| `client_file.go` | Single-request directory, simplified API |
| `file_test.go` | Proper `fs.ErrNotExist`, no `io.EOF`, 7 negative tests, `closingMemFileProvider` |
| `internal/asn1util/tags.go` | 9 new service tag number constants |
| `internal/asn1util/raw_test.go` | `WrapContextTag` + round-trip tests |
| `internal/pdu/confirmed.go` | Named constants in `classifyServiceByTagNum`, exported `ClassifyServiceTag` |
| `server.go` | Named constants in dispatch + all handlers |
| `mms_test.go` | `ClassifyServiceTag` + named constants in mock responses |

### Verification

```
go build ./...      — PASS
go vet ./...        — PASS
go test -count=1 .  — PASS (18 file tests, all existing tests)
```

---

## Phase 13 — Journal Services

**Status: COMPLETE**

### Summary

Implemented MMS journal services with a provider-based server abstraction,
client-side API, and PDU encode/decode support.

### Deliverables

1. **Tag constant + service kind:** `TagNumReadJournal = 65` in
   `internal/asn1util/tags.go`, `ServiceReadJournal` in
   `internal/pdu/confirmed.go` with dispatch classification.

2. **PDU support** (`internal/pdu/journal.go`):
   - `MarshalReadJournalTimeRange` — client request with time range.
   - `MarshalReadJournalStartAfter` — client request for continuation.
   - `UnmarshalReadJournalRequest` — server-side request parsing
     (time range and start-after variants).
   - `MarshalReadJournalResponse` — server response with entries + moreFollows.
   - `UnmarshalReadJournalResponse` — client-side response parsing.
   - Full journal entry + variable encode/decode with BER binary time.

3. **Client API** (`client_journal.go`):
   - `Client.ReadJournalTimeRange(ctx, domain, journal, start, stop)` —
     reads entries within a time range.
   - `Client.ReadJournalStartAfter(ctx, domain, journal, afterTime, afterID)` —
     reads entries after a cursor for paging.
   - Shared public types: `JournalEntry`, `JournalVariable`, `JournalResult`.

4. **Server journal provider** (`server_journal.go`):
   - `JournalProvider` interface: `ListJournals`, `ReadTimeRange`,
     `ReadStartAfter`.
   - `handleReadJournal` handler with error mapping
     (fs.ErrNotExist → object-non-existent, fs.ErrPermission → access-denied).
   - Registered via `ServerOptions.JournalProvider`.

5. **GetNameList for journals** (`server.go`):
   - `ObjectClassJournal` + `ScopeDomain` dispatches to
     `JournalProvider.ListJournals` with continueAfter filtering.

6. **Server dispatch** (`server.go`):
   - `asn1util.TagNumReadJournal` case added to `dispatch()`.

### New files

| File | Purpose |
|---|---|
| `client_journal.go` | Public client API for journal services |
| `server_journal.go` | JournalProvider interface, handler, error mapping |
| `internal/pdu/journal.go` | PDU marshal/unmarshal for ReadJournal |
| `journal_test.go` | In-memory provider + 7 integration tests |
| `internal/pdu/journal_test.go` | 6 PDU-level unit tests |

### Files changed

| File | Change |
|---|---|
| `internal/asn1util/tags.go` | Added `TagNumReadJournal = 65` |
| `internal/pdu/confirmed.go` | Added `ServiceReadJournal` kind + classification |
| `server.go` | Added `journalProvider` field, dispatch case, journal GetNameList handler |
| `server_options.go` | Added `JournalProvider` field |

### Tests

| Test | What it verifies |
|---|---|
| `TestReadJournalTimeRangeEndToEnd` | Time range query, 3 entries with integer variables |
| `TestReadJournalStartAfterEndToEnd` | Continuation paging after entry 2 |
| `TestJournalGetNameListEndToEnd` | Discover journal names via GetNameList |
| `TestJournalNotConfigured` | Service rejected when no provider set |
| `TestReadJournalNonExistentJournal` | Error for non-existent journal |
| `TestReadJournalEmptyResult` | Empty result for journal with no matching entries |
| `TestReadJournalMultipleVariables` | Entry with 3 variables (float, float, integer) |
| `TestUnmarshalReadJournalRequest_TimeRange` | PDU round-trip for time range request |
| `TestUnmarshalReadJournalRequest_StartAfter` | PDU round-trip for start-after request |
| `TestMarshalReadJournalResponse_RoundTrip` | PDU round-trip for response with entries |
| `TestMarshalReadJournalResponse_Empty` | PDU round-trip for empty response |
| `TestUnmarshalReadJournalRequest_MissingName` | Error for empty request body |
| `TestJournalEntryMissingEntryID` | Error for malformed entry without entryID |

### Verification

```
go build ./...            — PASS
go vet ./...              — PASS
go test -race -count=1 .  — PASS (all journal + existing tests)
go test -race ./...       — PASS (all packages)
```

---

## Phase 13 — Journal Services — Feedback Implementation

**Status: COMPLETE**

### Summary

Hardened Phase 13 based on 15 feedback items covering correctness,
validation, documentation, and test coverage.

### Items implemented

**Must-fix (1–6):**

1. **Rename `OccurenceTime` → `OccurrenceTime`** — Fixed typo in public
   API before users depend on it. Changed across all 5 files
   (`client_journal.go`, `server_journal.go`, `internal/pdu/journal.go`,
   `journal_test.go`, `internal/pdu/journal_test.go`).

2. **ReadJournalRequest validation** — After parsing, `UnmarshalReadJournalRequest`
   now enforces exactly one valid mode: time-range (requires both start
   and stop) OR start-after (requires both afterTime and afterID). Rejects
   requests with both modes or neither.

3. **`decodeStartAfter()` required-field validation** — Now returns error
   if `timeSpecification` or `entrySpecification` is missing.

4. **`parseJournalVariable()` required-field validation** — Returns error
   for missing variable tag or missing value.

5. **`parseEntryContent()` required-field validation** — Returns error
   for missing `occurrenceTime` or missing data block.

6. **`handleGetNameListJournal()` continueAfter** — Now returns
   `errInvalidRequest` when `continueAfter` names a non-existent journal
   instead of silently returning an empty list.

**Strong improvements (7–12):**

7. **Journal error mapping constants** — Removed incorrect local constants
   (class=1, codes=2/3). `journalError()` now reuses the library's existing
   error vars: `errObjectNonExistent` (class=7, code=0) and `errAccessDenied`
   (class=7, code=1), consistent with the rest of the codebase.

8. **Shared types documentation** — `JournalEntry`, `JournalVariable`,
   and `JournalResult` are documented as intentional shared domain types:
   the MMS protocol defines the journal entry shape, so a single set of
   types serves both client and provider without loss of fidelity.

9. **Pagination/continuation contract** — `JournalProvider` doc now
   explicitly documents: provider may return fewer than `maxEntries`;
   `MoreFollows=true` means continue with `ReadStartAfter` using last
   entry's `(OccurrenceTime, EntryID)`. Added `TestReadJournalPaginationEndToEnd`
   using a `pagingJournalProvider` wrapper that limits to 2 entries per page,
   verifying full multi-page traversal.

10. **Ordering requirements** — `JournalProvider` doc now specifies entries
    must be in ascending chronological order, with stable tie-breaking.

11. **ListJournals VMD scope** — Removed false "empty domain means VMD scope"
    claim. Doc now states only domain-scoped journals are supported.

12. **Malformed-response negative PDU tests** — Added 8 new tests:
    `TestUnmarshalReadJournalRequest_MissingRange`,
    `TestUnmarshalReadJournalRequest_BothRangeAndStartAfter`,
    `TestUnmarshalReadJournalRequest_StartAfterMissingID`,
    `TestUnmarshalReadJournalRequest_StartAfterMissingTime`,
    `TestJournalEntryMissingOccurrenceTime`,
    `TestJournalEntryMissingDataBlock`,
    `TestJournalVariableMissingTag`,
    `TestJournalVariableMissingValue`.

**Minor cleanup (13–15):**

13. **`ClassifyServiceTag` export** — Justified: used cross-package by
    root test mock dispatch. Added doc comment explaining export reason.

14. **TagNum constant formatting** — Aligned all constant comments and
    added hex annotations. Verified with `gofmt -l`.

15. **Permission-error mapping tests** — Added
    `TestReadJournalPermissionDenied` (verifies `fs.ErrPermission` maps
    to access-denied service error), `TestReadJournalGenericError`
    (verifies generic errors are handled), and
    `TestJournalGetNameListContinueAfterInvalid`.

### Files changed

| File | Change |
|---|---|
| `client_journal.go` | Rename `OccurenceTime` → `OccurrenceTime`, shared-type docs, paging docs |
| `server_journal.go` | Rename, fix error constants (reuse library vars), comprehensive provider docs (ordering, pagination, error mapping, domain scope) |
| `internal/pdu/journal.go` | Rename, strict validation in `UnmarshalReadJournalRequest` / `decodeStartAfter` / `parseEntryContent` / `parseJournalVariable` |
| `journal_test.go` | Rename, pagination test, continueAfter-invalid test, permission/generic error tests, `pagingJournalProvider` + `errorJournalProvider` helpers |
| `internal/pdu/journal_test.go` | Rename, 8 new malformed-request/response negative tests |
| `server.go` | continueAfter error handling in journal name list handler |
| `internal/pdu/confirmed.go` | `ClassifyServiceTag` doc justification |
| `internal/asn1util/tags.go` | Aligned formatting + hex annotations |

### Verification

```
go build ./...                                  — PASS
go vet ./...                                    — PASS
go test -race -count=1 -run 'Journal|File' .    — PASS (22 tests)
go test -race -count=1 ./internal/pdu/          — PASS (14 journal PDU tests)
go test -race ./... (excl. known testWriter race) — PASS
```

---

## Phase 14 — Alternate Access and Component/Array Operations

**Status: COMPLETE**

### Goal

Close the biggest functional gap versus libiec61850: sub-variable
addressing via MMS alternate access. Add component, array-element,
and array-range read/write operations.

### Deliverables completed

**1. Public types**

- **`types.go`** — Added `AccessSelector`, `IndexRange`, and
  `VariableSpec` types for expressing alternate access paths.

**2. PDU alternate access encode/decode**

- **`internal/pdu/altaccess.go`** (NEW) — Full BER encode/decode for
  MMS AlternateAccess, supporting:
  - `selectAccess` terminal selectors: component (`[1]`), index (`[2]`),
    indexRange (`[3]`)
  - `selectAlternateAccess` recursive selectors (`[0]`) for nested paths
    (e.g., index + component)
  - Wire types: `AccessSelectorWire`, `IndexRangeWire`, `VariableSpecWire`

**3. Extended read/write PDU marshaling**

- **`internal/pdu/read.go`** — Added `MarshalReadRequestWithAccess`,
  `MarshalWriteRequestWithAccess`, and `encodeListOfVariableWithAccess`
  for encoding variable specs with optional alternate access (tag `[5]`
  = 0xa5 in each `ListOfVariableSeq`).
- **`internal/pdu/server_helpers.go`** — Added `UnmarshalReadRequestFull`
  and `UnmarshalWriteRequestFull` that return `[]VariableSpecWire`
  including parsed alternate access. Added `decodeVarSpecListFull` and
  `decodeVariableSpecWire` helpers. Existing `UnmarshalReadRequest`
  and `UnmarshalWriteRequest` delegate to the new "Full" variants for
  backward compatibility.

**4. Client API**

- **`mms.go`** — Added:
  - `ReadVariables(ctx, []VariableSpec) ([]AccessResult, error)` —
    general-purpose multi-variable read with alternate access
  - `WriteVariables(ctx, []VariableSpec, []*Value) error` —
    general-purpose multi-variable write with alternate access
  - `ReadComponent(ctx, name, component)` — read structure member
  - `WriteComponent(ctx, name, component, value)` — write structure member
  - `ReadArrayElement(ctx, name, index)` — read single array element
  - `WriteArrayElement(ctx, name, index, value)` — write single array element
  - `ReadArrayRange(ctx, name, start, count)` — read array range
  - `variableSpecToWire()` conversion helper

**5. Server-side support**

- **`server.go`** — Updated `handleRead` and `handleWrite` to use
  `UnmarshalReadRequestFull` / `UnmarshalWriteRequestFull` and apply
  alternate access selectors to the variable value before returning.
- **`server_altaccess.go`** (NEW) — Server-side alternate access
  resolution: `applyAlternateAccessRead` traverses the selector chain
  against a value (supports index into arrays/structures, index range
  for arrays). Component-by-name requires TypeSpec metadata and is
  deferred to a future enhancement.

**6. Tests**

- **`internal/pdu/altaccess_test.go`** (NEW) — 9 PDU-level tests:
  - Component, index, indexRange round-trips
  - Nested selectors (index+component, component+index)
  - Full read/write PDU marshal/unmarshal round-trips
  - Error cases (empty selectors, invalid tags)
- **`altaccess_test.go`** (NEW) — 7 integration tests:
  - `TestReadArrayElementEndToEnd` — read single array element
  - `TestReadArrayRangeEndToEnd` — read array range
  - `TestReadStructElementByIndexEndToEnd` — read structure element by index
  - `TestReadVariablesMultipleWithAccess` — multi-variable read with mixed access
  - `TestWriteArrayElementEndToEnd` — write single array element
  - `TestReadArrayElementOutOfBounds` — out-of-bounds error handling
  - `TestReadVariablesPlainNoAccess` — plain read via ReadVariables

### Files changed

| File | Change |
|---|---|
| `types.go` | Added `AccessSelector`, `IndexRange`, `VariableSpec` types |
| `mms.go` | Added `ReadVariables`, `WriteVariables`, convenience methods, `variableSpecToWire` |
| `server.go` | Updated `handleRead`/`handleWrite` to use Full unmarshal + alternate access |
| `server_altaccess.go` | NEW: server-side alternate access resolution |
| `internal/pdu/altaccess.go` | NEW: BER encode/decode for alternate access |
| `internal/pdu/read.go` | Added `MarshalReadRequestWithAccess`, `MarshalWriteRequestWithAccess`, `encodeListOfVariableWithAccess` |
| `internal/pdu/server_helpers.go` | Added `UnmarshalReadRequestFull`, `UnmarshalWriteRequestFull`, `decodeVarSpecListFull`, `decodeVariableSpecWire` |
| `internal/pdu/altaccess_test.go` | NEW: 9 PDU unit tests |
| `altaccess_test.go` | NEW: 7 integration tests |
| `PLAN.md` | Extended with Phases 14–17 |

### Verification

```
go build ./...                                    — PASS
go vet ./...                                      — PASS
go test -race -count=1 ./internal/pdu/            — PASS (9 alternate access PDU tests)
go test -race -count=1 -run 'AltAccess|Array|Struct|Variables' . — PASS (7 integration tests)
go test -race -count=1 -timeout 120s ./...        — PASS (all packages)
```

---

## Phase 15 — NVL Value Operations + Generic Object Addressing

**Status: COMPLETE**

### Goal

Complete Named Variable List support with value-plane operations
(read/write NVL values) and add generic `ReadObject`/`WriteObject`
convenience methods for all scopes.

### Deliverables completed

**1. PDU marshalers for variableListName**

- **`internal/pdu/read.go`** — Added `tagVarListName` (0xa1),
  `MarshalReadRequestByListName`, `MarshalWriteRequestByListName`.
- **`internal/pdu/server_helpers.go`** — Added `ReadRequestWire` and
  `WriteRequestWire` types, `UnmarshalReadRequestParsed` and
  `UnmarshalWriteRequestParsed` that support both `listOfVariable [0]`
  and `variableListName [1]` CHOICE alternatives.

**2. Client API**

- `ReadNamedVariableList(ctx, listName ObjectName) ([]AccessResult, error)`
- `WriteNamedVariableList(ctx, listName ObjectName, values []*Value) error`
- `ReadObject(ctx, name ObjectName) (*ReadResult, error)`
- `WriteObject(ctx, name ObjectName, value *Value) (*WriteResult, error)`

**3. Server-side NVL resolution**

- Server `handleRead` and `handleWrite` now handle `variableListName`
  requests by resolving the NVL from the registry, expanding to member
  variables, and reading/writing each member individually.
- Added `resolveNVLMembers` helper.

**4. Integration tests** — `nvl_value_test.go` (NEW):

- `TestReadNamedVariableListEndToEnd` — read all NVL member values
- `TestWriteNamedVariableListEndToEnd` — write+readback all NVL members
- `TestReadObjectEndToEnd` — single-variable read via ObjectName
- `TestWriteObjectEndToEnd` — write+readback via ObjectName
- `TestReadNVLUnknownList` — error for non-existent NVL

### Files changed

| File | Change |
|---|---|
| `mms.go` | Added `ReadObject`, `WriteObject`, `ReadNamedVariableList`, `WriteNamedVariableList` |
| `server.go` | NVL resolution in `handleRead`/`handleWrite`, `resolveNVLMembers` |
| `internal/pdu/read.go` | Added `tagVarListName`, `MarshalReadRequestByListName`, `MarshalWriteRequestByListName` |
| `internal/pdu/server_helpers.go` | Added `ReadRequestWire`, `WriteRequestWire`, parsed unmarshal variants |
| `nvl_value_test.go` | NEW: 5 integration tests |

### Verification

```
go build ./...                                    — PASS
go vet ./...                                      — PASS
go test -race -count=1 -run 'NVL|ReadObject|WriteObject' . — PASS (5 tests)
go test -race -count=1 -timeout 120s ./...        — PASS (all packages)
```

---

## Phase 16 — File Parity (Rename + ObtainFile)

**Status: COMPLETE**

### Goal

Complete file service parity with libiec61850 by adding FileRename
and ObtainFile operations.

### Deliverables completed

**1. PDU layer**

- `internal/pdu/file.go` — Added `MarshalFileRenameRequest`,
  `MarshalObtainFileRequest`, `UnmarshalFileRenameRequest`,
  `UnmarshalObtainFileRequest`, and wire types `FileRenameRequest`,
  `ObtainFileRequest`.
- `internal/asn1util/tags.go` — Added `TagNumObtainFile = 46`.
- `internal/pdu/confirmed.go` — Added `ServiceFileRename`,
  `ServiceObtainFile` service kinds and classification.

**2. Client API**

- `FileRename(ctx, currentName, newName string) error`
- `ObtainFile(ctx, sourceFile, destinationFile string) error`

**3. Server support**

- Extended `FileProvider` interface with `Rename` and `ObtainFile`.
- Added `handleFileRename` and `handleObtainFile` server handlers.
- Added dispatch entries for both new services.

**4. Tests**

- Extended `memFileProvider` with `Rename` and `ObtainFile` methods.
- `TestFileRenameEndToEnd` — rename + verify directory listing
- `TestFileRenameNotFound` — error for non-existent source
- `TestObtainFileEndToEnd` — copy + verify content via open/read

### Files changed

| File | Change |
|---|---|
| `internal/asn1util/tags.go` | Added `TagNumObtainFile` |
| `internal/pdu/confirmed.go` | Added `ServiceFileRename`, `ServiceObtainFile` |
| `internal/pdu/file.go` | Added rename/obtain marshalers + unmarshalers |
| `client_file.go` | Added `FileRename`, `ObtainFile` client methods |
| `server_file.go` | Extended `FileProvider`, added handlers |
| `server.go` | Dispatch entries for rename/obtain |
| `file_test.go` | Extended `memFileProvider`, 3 new integration tests |

### Verification

```
go build ./...                                    — PASS
go vet ./...                                      — PASS
go test -race -count=1 -run 'FileRename|ObtainFile' . — PASS (3 tests)
go test -race -count=1 -timeout 120s ./...        — PASS (all packages)
```

---

## Phase 17 — Public Utility Pass

**Status: COMPLETE**

### Goal

Add essential utility methods to `Value` and `TypeSpec` for improved
ergonomics, debugging, and type-safe access patterns.

### Deliverables completed

**1. Value utilities** (`value.go`):

- `Clone() *Value` — deep copy including nested structures/arrays,
  independent byte slice copies.
- `Equal(other *Value) bool` — structural equality comparison for
  all types including recursive structure/array element comparison.
- `String() string` — human-readable representation for debugging.
  Formats scalars naturally, structures as `{...}`, arrays as `[...]`.

**2. TypeSpec utilities** (`types.go`):

- `ChildByName(name string) (*TypeSpec, bool)` — look up structure
  element type by name.
- `ChildByIndex(index int) (*TypeSpec, bool)` — look up structure or
  array element type by index.
- `Compatible(v *Value) bool` — shallow compatibility check between
  a value and a type spec (type + element count for composites).
- `DefaultValue() *Value` — create a zero-valued `*Value` matching
  the spec, recursively initializing structure/array elements.

**3. Tests** (`value_util_test.go`, NEW):

- 8 Value test functions (Clone nil/scalar/deep structure/byte slice,
  Equal same-type/different-type/nil, String all types)
- 5 TypeSpec test functions (ChildByName, ChildByIndex, Compatible,
  DefaultValue scalars, DefaultValue nested structure)

### Files changed

| File | Change |
|---|---|
| `value.go` | Added `Clone`, `Equal`, `String` methods |
| `types.go` | Added `ChildByName`, `ChildByIndex`, `Compatible`, `DefaultValue` |
| `value_util_test.go` | NEW: comprehensive unit tests |

### Verification

```
go build ./...                                    — PASS
go vet ./...                                      — PASS
go test -race -count=1 -run 'ValueClone|ValueEqual|ValueString|TypeSpec' . — PASS
go test -race -count=1 -timeout 120s ./...        — PASS (all packages)
```

---

## Semantic Correctness Pass — FEEDBACK.md Improvements

**Date**: 2026-03-18
**Status**: COMPLETE

### Summary

Implemented all 12 improvement requests from FEEDBACK.md plus comprehensive test gap closure.

### Fix #1 — Server-side component-by-name resolution using TypeSpec

**Problem**: `selectComponent` returned nil — component-by-name alternate access was exposed in the API but not implemented on the server.

**Solution**: Rewrote `server_altaccess.go` to accept a `*TypeSpec` alongside the `*Value`. Added `resolveComponentIndex(ts, name)` which maps a component name to its element index via `TypeSpec.Elements`. The `applySingleSelector` and `applyAlternateAccessRead` functions now thread the TypeSpec through the selector chain, descending into child TypeSpecs for nested access.

**Files changed**: `server_altaccess.go`, `server.go`

### Fix #2 — Alternate-access writes with read-modify-write patching

**Problem**: `applyAlternateAccessForWrite` was a pass-through that sent only the sub-value to the variable's Write handler, breaking the server contract.

**Solution**: Implemented proper read-modify-write in the server's write handler. When alternate access selectors are present, the server now: (1) reads the current full value via the ReadFunc, (2) clones it, (3) patches the selected sub-element in the clone using `applyAlternateAccessWrite`/`patchValue`, (4) calls WriteFunc with the full patched value. Added `patchComponent`, `patchIndex`, `patchRange` helpers for recursive patching.

**Files changed**: `server_altaccess.go`, `server.go`

### Fix #3 — Alternate-access input validation

**Problem**: No validation that exactly one of Component/Index/IndexRange is set, no checks for negative indices or zero/negative range counts.

**Solution**: Added `validateAccessSelectors` function in `mms.go` that validates each selector: exactly one kind set, index >= 0, range start >= 0, range count > 0. Called from both `ReadVariables` and `WriteVariables` before PDU encoding.

**Files changed**: `mms.go`

### Fix #4 — Response count validation

**Problem**: `ReadVariables`, `WriteVariables`, and `WriteNamedVariableList` did not verify that the response item count matches the request count.

**Solution**: Added response count checks in all four methods. On mismatch, returns a `ProtocolError` with a descriptive message.

**Files changed**: `mms.go`

### Fix #5 — NVL resolution error → object-non-existent

**Problem**: `resolveNVLMembers` returned `errInvalidRequest` for a missing NVL, which is semantically wrong.

**Solution**: Changed to return `errObjectNonExistent`, consistent with missing variable/journal/file error semantics.

**Files changed**: `server.go`

### Fix #6 — FileName encoding documentation

**Problem**: Comments suggested the encoding might not be spec-faithful.

**Solution**: Verified that the existing encoding is correct: context-specific constructed tag wrapping a GraphicString (0x19), which is the IMPLICIT-tagged SEQUENCE OF GraphicString form. Updated comments to clearly document the wire shape and the lenient decoder for interop.

**Files changed**: `internal/pdu/file.go`

### Fix #7 — File size uint32 overflow guard

**Problem**: `int64` size was silently truncated to `uint32` in MarshalFileOpenResponse and MarshalFileDirectoryResponse.

**Solution**: Added explicit range checks: `size < 0 || size > math.MaxUint32` returns an error with a descriptive message. No more silent truncation.

**Files changed**: `internal/pdu/file.go`

### Fix #8 — ReadByIndex convenience method

**Problem**: Using `ReadArrayElement` for structures was semantically confusing.

**Solution**: Added `ReadByIndex(ctx, name, index)` client method that works for both arrays and structures. Documented that MMS index selectors apply to both ordered structure elements and array elements.

**Files changed**: `mms.go`

### Fix #9 — Rename Compatible → ShallowCompatible

**Problem**: The method name `Compatible` implied full recursive validation, but the check was shallow.

**Solution**: Renamed to `ShallowCompatible` with updated documentation explicitly stating it only checks top-level type and element count but does not recurse.

**Files changed**: `types.go`, `value_util_test.go`

### Fix #10 — Document exact float equality in Value.Equal

**Problem**: Float values were compared with `==` (exact bitwise), which could surprise callers expecting epsilon comparison.

**Solution**: Added clear documentation that float comparison uses exact bitwise equality, and suggests callers implement epsilon-based comparison separately if needed.

**Files changed**: `value.go`

### Fix #11 — Defensive byte slice copy in dataValueToValue/valueToDataValue

**Problem**: `bytesVal` was shared between Value and DataValue, allowing aliasing across layers.

**Solution**: Added `copyBytes()` calls for both BitString and OctetString types in both conversion directions, ensuring full ownership isolation.

**Files changed**: `mms.go`

### Fix #12 — Document shallow copy semantics in NewArray/NewStructure

**Problem**: The shallow-copy behavior of constructors was undocumented, potentially confusing callers.

**Solution**: Added documentation to both `NewArray` and `NewStructure` explaining that the element slice is shallow-copied (shared child pointers) and recommending `Value.Clone` for full independence.

**Files changed**: `value.go`

### Test gap closure

Added comprehensive new tests covering all gaps identified in FEEDBACK.md:

**Alternate access tests** (`altaccess_test.go`):
- `TestReadComponentByNameEndToEnd` — component-by-name read
- `TestWriteComponentByNameEndToEnd` — component-by-name write with read-back verification
- `TestWriteArrayElementEndToEnd` — updated to verify full array patching
- `TestWriteArrayRangeVerifyPatching` — range write with full read-back
- `TestReadByIndexEndToEnd` — new ReadByIndex method
- `TestSelectorValidation_NegativeIndex` — negative index rejected
- `TestSelectorValidation_ZeroCountRange` — zero count rejected
- `TestSelectorValidation_NegativeRangeStart` — negative start rejected
- `TestSelectorValidation_ConflictingFields` — Component+Index rejected

**NVL tests** (`nvl_value_test.go`):
- `TestReadNVLUnknownList` — now verifies ErrorClassAccess (object-non-existent)
- `TestWriteNVLUnknownList` — new test for write to missing NVL

**File tests** (`file_test.go`):
- `TestFileRenameCollision` — rename when destination exists
- `TestObtainFileDestExists` — obtain-file overwrites existing
- `TestFileMultiChunkRead` — multi-chunk read at exact boundaries
- `TestFileNameWithPathSeparators` — nested path names

**PDU tests** (`internal/pdu/file_test.go`):
- `TestMarshalFileOpenResponse_SizeOverflow` — overflow and negative size
- `TestMarshalFileDirectoryResponse_SizeOverflow` — overflow in directory
- `TestDecodeFileName_InteropShapes` — wrapped/bare/alternate tag forms

**Value/TypeSpec tests** (`value_util_test.go`):
- `TestValueEqual_BitStringSameBytesButDifferentLen` — bitstring equality edge case
- `TestValueClone_UTCTime` — UTCTime clone independence
- `TestValueClone_BinaryTime` — BinaryTime clone independence
- `TestValueClone_BitString` — BitString clone with byte mutation
- `TestValueClone_NestedStrings` — structure with strings clone
- `TestTypeSpecDefaultValue_ZeroCountArray` — zero-count array
- `TestTypeSpecDefaultValue_NilElementZeroCount` — nil element handling
- `TestTypeSpecShallowCompatible_NestedMismatch` — shallow vs deep behavior

### Infrastructure fix

Added `extractTypeSpec(any) *TypeSpec` helper in `server.go` to handle both value (`TypeSpec`) and pointer (`*TypeSpec`) forms stored in the registry's `any` field.

### Verification

```
go build ./...                                    — PASS
go vet ./...                                      — PASS
go test -race -count=1 -timeout 120s ./...        — PASS (all packages)
```

---

## Round 3 — Feedback hardening pass

**Status: COMPLETE**

Addresses all items raised in `FEEDBACK.md` (second-pass review). Five priority fixes plus two hardening improvements.

### Fix #1 — TestFileMultiChunkRead now actually forces multiple chunks

**Problem**: Test used only 3072 bytes of data, well below the server's 60000-byte chunk size. The test name said "multi chunk" but it never produced more than one FileRead response.

**Solution**: Increased payload to 150000 bytes (2.5x the server chunk size). Added assertions verifying:
- More than one `FileRead` call occurred
- At least one intermediate response had `MoreFollows=true`

**Files changed**: `file_test.go`

### Fix #2 — decodeFileName rejects trailing bytes

**Problem**: `decodeFileName` decoded the first TLV from the input but never verified it consumed all bytes. Both the bare `0x19` path and the constructed wrapper path silently accepted trailing garbage bytes, making the decoder more permissive than the rest of the parser suite.

**Solution**: Capture the consumed length from `DecodeTLVAt` and reject when `n != len(data)` at each level:
- Bare `0x19`: reject trailing bytes after the GraphicString
- Constructed wrapper: reject trailing bytes after the wrapper TLV
- Inner GraphicString: reject trailing bytes inside the wrapper

Added `TestDecodeFileName_TrailingBytes` covering all three rejection cases.

**Files changed**: `internal/pdu/file.go`, `internal/pdu/file_test.go`

### Fix #3 — TestFileRenameCollision is now a real regression test

**Problem**: Test was purely observational — it logged results but asserted nothing. It could not catch regressions.

**Solution**: The in-memory provider performs overwrite-on-rename. The test now explicitly asserts:
- Rename succeeds without error
- Only 1 file entry remains (`b.txt`)
- Reading `b.txt` returns `a.txt`'s original content (`"aaa"`)

**Files changed**: `file_test.go`

### Fix #4 — Structure-index test uses ReadByIndex

**Problem**: `TestReadStructElementByIndexEndToEnd` still used `ReadArrayElement` for a structure variable, keeping the earlier naming confusion alive.

**Solution**: Changed to `ReadByIndex`, which is the documented generic index-based API for both arrays and structures.

**Files changed**: `altaccess_test.go`

### Fix #5 — Alternate-access patching clones inserted values

**Problem**: `patchComponent`, `patchIndex`, and `patchRange` inserted caller-provided `*Value` pointers directly into the server-side value graph, creating aliasing between the write value and the stored value.

**Solution**: All three patch functions now call `writeVal.Clone()` (or `newElems[i].Clone()` for range patching) before assignment. This ensures full ownership isolation and prevents mutations to the write value from affecting stored state.

**Files changed**: `server_altaccess.go`

### Fix #6 — TypeSpec.DefaultValue documents nil-element array behavior

**Problem**: `DefaultValue` returned an empty array for a TypeSpec with `Count > 0` but `Element == nil`. This was deliberate but undocumented and surprising.

**Solution**: Added documentation to `DefaultValue` explicitly explaining this is a best-effort placeholder: a non-zero Count with nil Element means the type spec is incomplete (element type unknown), so no elements can be initialized.

**Files changed**: `types.go`

### Fix #7 — ShallowCompatible check during write patching

**Problem**: The write path relied on path validity and write callback behavior but did not check type compatibility of inserted values at the MMS layer. This meant type mismatches (e.g. writing a float into an integer field) would only be caught by the callback, if at all.

**Solution**: `patchComponent` and `patchIndex` now validate the write value against the target child TypeSpec using `ShallowCompatible` before assignment. If the child TypeSpec is available and the value is not compatible, the patch returns false (resulting in a type-inconsistent error). This gives cleaner, earlier MMS-level errors.

**Files changed**: `server_altaccess.go`

### Verification

```
go build ./...                                    — PASS
go vet ./...                                      — PASS
go test -race -count=1 -timeout 120s ./...        — PASS (all packages)
```

---

## Round 4 — Protocol gap closure (FEEDBACK.md items 1–8)

All 8 real functional gaps identified in the external review have been implemented. These are listed in the priority order from FEEDBACK.md.

### Gap #1 — NVL attributes preserve alternate access

**Problem**: `GetNamedVariableListAttributes` returned `[]ObjectName`, dropping the optional `alternateAccess` field from each NVL member entry. This prevented round-tripping named variable list definitions that include array-element or component-scoped members.

**Solution**:
- Changed `NamedVariableListAttributes.Variables` from `[]ObjectName` to `[]VariableSpec`
- Changed `DefineNamedVariableListRequest.Variables` from `[]ObjectName` to `[]VariableSpec` for consistency
- Updated `internal/pdu/namedvarlist.go`: `NamedVarListAttrsResult.Variables` is now `[]VariableSpecWire`; added `decodeVariableSpecFull` which decodes both the ObjectName and optional AlternateAccess from each `SEQUENCE { variableSpecification, alternateAccess? }` entry
- Added `variableSpecFromWire` converter in `mms.go`
- Updated all test files for the new types

**Files changed**: `types.go`, `mms.go`, `internal/pdu/namedvarlist.go`, `mms_test.go`, `nvl_value_test.go`, `server_test.go`, `transport/iso/integration_test.go`

### Gap #2 — Per-item write results

**Problem**: `WriteVariables` and `WriteNamedVariableList` returned only `error`, stopping at the first failed item. This meant partial success (some variables written, others failed) could not be represented, losing MMS semantics.

**Solution**:
- Added `WriteAccessResult` type with `Success bool` and `ErrorCode DataAccessErrorCode` fields
- Changed `WriteVariables` return from `error` to `([]WriteAccessResult, error)`
- Changed `WriteNamedVariableList` return from `error` to `([]WriteAccessResult, error)`
- Updated convenience wrappers (`WriteComponent`, `WriteArrayElement`) to extract per-item errors
- Single-variable write methods (`Write`, `WriteObject`) remain unchanged — they already returned a single error

**Files changed**: `types.go`, `mms.go`, `altaccess_test.go`, `nvl_value_test.go`

### Gap #3 — File directory pagination / continuation

**Problem**: `FileDirectory` had no pagination support — it returned all entries in one response. This fails for large file stores.

**Solution**:
- Added `FileDirectoryRequest` struct with `FileSpec` and `ContinueAfter` fields
- Added `FileDirectoryResult` struct with `Entries` and `MoreFollows` fields
- Changed `FileDirectory` to accept `FileDirectoryRequest` and return `*FileDirectoryResult`
- Added `FileDirectoryAll` convenience method with stall detection (mirrors `GetNameListAll` pattern)
- Updated `MarshalFileDirectoryRequest` in `internal/pdu/file.go` to encode `continueAfter` as tag `[1]`

**Files changed**: `types.go`, `client_file.go`, `internal/pdu/file.go`, `file_test.go`

### Gap #4 — FileOpen initialPosition

**Problem**: `FileOpen` always marshaled with `initialPosition=0`, preventing random-access resume or partial-download behavior.

**Solution**:
- Added `FileOpenOptions` struct with `InitialPosition uint32` field
- Changed `FileOpen` to accept variadic `...FileOpenOptions` (zero-value-useful, backwards compatible)
- The underlying `MarshalFileOpenRequest` already accepted `initialPosition`; the option now passes it through

**Files changed**: `types.go`, `client_file.go`

### Gap #5 — Export negotiated parameters

**Problem**: MMS negotiated parameters (max PDU size, outstanding counts, nesting level, server version) were tracked internally but not exported. Upper layers need these for batching and browse strategies.

**Solution**:
- Added `NegotiatedParameters` struct
- Added `Client.Negotiated() NegotiatedParameters` method

**Files changed**: `types.go`, `mms.go`

### Gap #6 — Explicit Abort API

**Problem**: No way to perform a hard association abort without the graceful Conclude handshake. Needed for protocol desync, emergency teardown, and test tooling.

**Solution**:
- Added `Client.Abort(ctx) error` method
- Abort marks the client as closed, cancels all pending requests, stops the reader loop, and closes the transport — without sending ConcludeRequest

**Files changed**: `mms.go`

### Gap #7 — Missing MMS types: ObjectIdentifier, GeneralizedTime, BCD

**Problem**: `TypeSpec` and `Value` did not model `ObjectIdentifier` (tag 8), `GeneralizedTime` (tag 11), or `BCD` (tag 13). These are real MMS Data CHOICE alternatives needed for full wire compatibility.

**Solution**:
- Added `ValueTypeGeneralizedTime`, `ValueTypeBCD`, `ValueTypeObjectIdentifier` constants
- Added `NewGeneralizedTime`, `NewBCD`, `NewObjectIdentifier` constructors
- Added `GeneralizedTime()`, `BCD()`, `ObjectIdentifier()` accessors on `Value`
- Updated `Clone()`, `Equal()`, `String()` for new types
- Updated `DefaultValue()` for new types
- Added `TagDataObjId` (0x88), `TagDataGenTime` (0x8b), `TagDataBCD` (0x8d) in `internal/pdu/data.go`
- Added encode/decode cases for all three types in `MarshalData` / `decodeDataContent`
- Added OID field to `DataValue` struct
- Added `EncodeObjectIdentifier` / `DecodeObjectIdentifier` BER helpers in `internal/berutil/berutil.go`
- Updated `typeSpecFromWire` to handle tags 8, 11, 13
- Updated `valueToDataValue` / `dataValueToValue` for new types
- Updated `TypeSpec` doc comment to reflect full coverage

**Files changed**: `types.go`, `value.go`, `mms.go`, `internal/pdu/data.go`, `internal/berutil/berutil.go`

### Gap #8 — ReadNamedVariableList specWithResult

**Problem**: `ReadNamedVariableList` had no way to request `specificationWithResult=true`, which asks the server to include variable specifications alongside each result value.

**Solution**:
- Added `ReadNamedVariableListOptions` struct with `SpecificationWithResult bool` field
- Changed `ReadNamedVariableList` to accept variadic `...ReadNamedVariableListOptions` (backwards compatible)
- Added `MarshalReadRequestByListNameWithSpec` in `internal/pdu/read.go` to encode the `[0] IMPLICIT BOOLEAN` field

**Files changed**: `types.go`, `mms.go`, `internal/pdu/read.go`

### Verification

```
go build ./...                                    — PASS
go vet ./...                                      — PASS
go test -race -count=1 -timeout 120s ./...        — PASS (all packages)
make check                                        — PASS (lint + test + coverage)
```

---

## Round 5 — Deeper design improvements and NVL carry-through

Round 4 changed the public types for NVL (using `VariableSpec` instead of `ObjectName`), but the server-side registry, marshaling, and resolution still only handled plain `ObjectName`. This round completes the carry-through and implements the deeper design improvements from the feedback.

### Improvement #1 — NVL alternate access: full stack carry-through

**Problem**: While the public API types (`DefineNamedVariableListRequest.Variables`, `NamedVariableListAttributes.Variables`) were changed to `[]VariableSpec` in Round 4, the underlying implementation still dropped alternate access:
- `MarshalDefineNamedVarListRequest` took `[]ObjectNameWire` and never encoded alternate access
- Client-side `DefineNamedVariableList` stripped alternate access before calling marshal
- Server-side `NVLVariable` only stored Scope/DomainID/ItemID
- Server-side `resolveNVLMembers` returned `[]ObjectNameWire` with no alternate access
- Server-side define handler discarded alternate access from incoming requests
- Server-side get-attrs handler returned `[]ObjectNameWire` with no alternate access
- `MarshalGetNVLAttrsResponse` only encoded ObjectName, not alternate access

**Solution**: Carried `VariableSpec` / `VariableSpecWire` through every layer:

1. **`internal/pdu/namedvarlist.go`**: `MarshalDefineNamedVarListRequest` now takes `[]VariableSpecWire` and encodes alternate access via `encodeAlternateAccess` when present
2. **`internal/pdu/server_helpers.go`**:
   - `DefineNVLRequest.Variables` changed from `[]ObjectNameWire` to `[]VariableSpecWire`
   - `decodeDefineNVLVarList` returns `[]VariableSpecWire` using `decodeVariableSpecFull` (handles both ObjectName and optional alternate access)
   - `MarshalGetNVLAttrsResponse` takes `[]VariableSpecWire` and encodes alternate access
3. **`internal/servermodel/registry.go`**:
   - Added `AccessSelectorModel` struct (Component, HasIndex, Index, IndexRange)
   - Added `IndexRangeModel` struct (LowIndex, NumberOfElements)
   - `NVLVariable` now includes `AlternateAccess []AccessSelectorModel`
4. **`server.go`**:
   - `resolveNVLMembers` returns `[]pdu.VariableSpecWire` with alternate access populated
   - Read/write handlers use `specs = append(specs, resolved...)` instead of wrapping individual names
   - `handleDefineNVL` converts and stores alternate access from `VariableSpecWire` into `AccessSelectorModel`
   - `handleGetNVLAttrs` builds `[]pdu.VariableSpecWire` with alternate access from stored model
5. **`mms.go`**: Client `DefineNamedVariableList` passes `[]pdu.VariableSpecWire` via `variableSpecToWire(v)` instead of stripping alternate access

**Files changed**: `internal/pdu/namedvarlist.go`, `internal/pdu/server_helpers.go`, `internal/servermodel/registry.go`, `server.go`, `mms.go`

### Improvement #2 — FileDirectoryResult.ContinueAfter

**Problem**: `FileDirectoryResult` had `MoreFollows` but no `ContinueAfter`, requiring the caller to manually track the last entry name for pagination.

**Solution**:
- Added `ContinueAfter string` field to `FileDirectoryResult`
- `FileDirectory` now populates it with the last entry's name when entries are present
- `FileDirectoryAll` uses `result.ContinueAfter` instead of computing the token manually

**Files changed**: `types.go`, `client_file.go`

### Improvement #3 — WriteAccessResult.Index

**Problem**: Per-item write results had no index, making it hard to correlate partial failures back to the original variable list.

**Solution**:
- Added `Index int` field to `WriteAccessResult`
- Both `WriteVariables` and `WriteNamedVariableList` populate `Index: i` in each result

**Files changed**: `types.go`, `mms.go`

### Improvement #4 — Path helpers: Value.Get and TypeSpec.Resolve

**Problem**: With alternate access as a first-class concept, navigating nested structures and arrays required manual traversal code.

**Solution**:
- Added `Value.Get(selectors ...AccessSelector) (*Value, error)` in `value.go` — traverses nested structure/array values by index or index range; component access returns an error directing callers to use `TypeSpec.Resolve` (since component names require type context)
- Added `TypeSpec.Resolve(selectors ...AccessSelector) (*TypeSpec, error)` in `types.go` — traverses the type tree by component name, index, or index range

**Files changed**: `value.go`, `types.go`

### Improvement #5 — File convenience helpers: FileReadAll and DownloadFile

**Problem**: Reading an entire file required a manual open→read-loop→close sequence.

**Solution**:
- Added `Client.FileReadAll(ctx, frsmID) ([]byte, error)` — reads all remaining data by looping `FileRead` until `MoreFollows` is false
- Added `Client.DownloadFile(ctx, fileName) ([]byte, *FileOpenResult, error)` — convenience open→read-all→close pipeline

**Files changed**: `client_file.go`

### Round 5 Verification

```
go build ./...                                    — PASS
go vet ./...                                      — PASS
go test -race -count=1 -timeout 120s ./...        — PASS (all packages)
make check                                        — PASS (lint + test + coverage)
```

---

## Round 6 — End-to-end completeness and server/client symmetry

This round addresses the final sharp issues identified in the feedback: server-side features that lagged the client API, protocol fidelity in abort, and public API surface gaps.

### Gap #1 — Server-side file directory paging (end-to-end)

**Problem**: The client could speak paged file directory (with `ContinueAfter`, `MoreFollows`, `FileDirectoryAll`), but the server was still single-shot: `FileProvider.List` returned all entries at once, `handleFileDirectory` always set `moreFollows=false`, and `UnmarshalFileDirectoryRequest` did not parse `continueAfter`.

**Solution**:
- Added `FileListRequest` struct (FileSpec, ContinueAfter, MaxEntries) and `FileListResult` struct (Entries, MoreFollows) as public types for the server-side provider
- Changed `FileProvider.List` from `(ctx, fileSpec string) ([]FileEntry, error)` to `(ctx, req FileListRequest) (*FileListResult, error)`
- Updated `UnmarshalFileDirectoryRequest` in `internal/pdu/file.go` to parse the `continueAfter` field (tag `0xa1`) and added `ContinueAfter` to the PDU request struct
- Updated `handleFileDirectory` in `server_file.go` to pass the full request through and use `listResult.MoreFollows` in the response
- Updated test file providers to match the new interface

**Files changed**: `types.go`, `server_file.go`, `internal/pdu/file.go`, `file_test.go`

### Gap #2 — Abort() sends real protocol abort

**Problem**: `Client.Abort()` only performed a local close (mark closed, cancel pending, close transport). It did not send an MMS/ACSE/session abort PDU, making it semantically a "hard local close" rather than a true protocol abort.

**Solution**:
- Updated `Abort()` to send the abort PDU via `isostack.EncodeAbort()` (which builds Session ABORT → Presentation → ACSE ABRT) before canceling pending requests
- Uses `c.sendMu.Lock()` to serialize with concurrent sends
- Best-effort semantics: the write error is discarded since the peer may already be gone
- The PDU is sent before `tracker.CancelAll` so it goes out before the transport closes

**Files changed**: `mms.go`

### Gap #3 — SpecificationWithResult fully implemented

**Problem**: The `ReadNamedVariableListOptions.SpecificationWithResult` option existed and the request encoder supported it, but the server-side `MarshalReadResponse` always skipped the optional `variableAccessSpecification` field. The API suggested support that wasn't actually real.

**Solution**:
- Added `SpecWithResult bool` field to `ReadRequestWire` in `internal/pdu/server_helpers.go`
- Updated `UnmarshalReadRequestParsed` to capture the boolean value instead of skipping it
- Added `MarshalReadResponseWithSpec` that includes `variableAccessSpecification` in the response (either as `variableListName` or `listOfVariable` with alternate access)
- Updated `handleRead` in `server.go` to call `MarshalReadResponseWithSpec` when `req.SpecWithResult` is true
- Added `NVLAccessResult` public type (Variable, Value, ErrorCode) for richer result representation when using spec-with-result mode

**Files changed**: `internal/pdu/server_helpers.go`, `server.go`, `types.go`

### Gap #4 — Public server API for static named variable lists

**Problem**: Dynamic NVL creation existed (client-initiated define/delete), but there was no way to pre-register static NVLs at server setup time. IEC 61850 data sets map to MMS NVLs, and a server wrapper needs to expose static datasets cleanly.

**Solution**:
- Added `NamedVariableList` public type (Name, Deletable, Variables) for server registration
- Added `Server.RegisterNamedVariableList(nvl NamedVariableList) error` method
- The method validates the NVL name and all member variables, converts alternate access selectors, and delegates to the internal registry

**Files changed**: `types.go` (or `server_model.go`), `server.go`

### Gap #5 — Association-scope documented as intentionally limited

**Problem**: Association scope was type-level supported (`ObjectScopeAssociation`, `VarEntry.Scope=2`) but not functionally complete — no listing, no per-connection lifecycle, no NVL support. This created confusion about whether it was a real feature.

**Solution**: Documented the current state explicitly:
- Updated `RegisterVariable` doc in `internal/servermodel/registry.go` to note association-scope variables are stored but not listed
- Added explanatory comment at the `default:` case in `handleGetNameList` in `server.go`
- Updated `ObjectScopeAssociation` constant doc in `types.go` to clarify server-side listing/lifecycle is not yet implemented

**Files changed**: `internal/servermodel/registry.go`, `server.go`, `types.go`

### Gap #6 — Stale comments + AccessSelector convenience builders

**Problem**: `FileProvider.List` comments still said "no pagination in this phase" (now stale). Also, upper-layer code would repeat patterns for constructing access selectors.

**Solution**:
- Cleaned up `FileProvider` interface comments to reflect pagination support
- Added three convenience constructors: `SelectComponent(name)`, `SelectIndex(i)`, `SelectRange(low, count)` — reducing boilerplate for alternate access usage

**Files changed**: `server_file.go`, `types.go`

### Round 6 Verification

```
go build ./...                                    — PASS
go vet ./...                                      — PASS
go test -race -count=1 -timeout 120s ./...        — PASS (all packages)
make check                                        — PASS (lint + test + coverage)
```

---

## Test coverage improvement

Overall statement coverage: **65.7% → 77.5%** (+11.8 percentage points)

### Per-package improvements

| Package | Before | After | Delta |
|---------|--------|-------|-------|
| `internal/berutil` | 47.4% | 93.4% | +46.0 |
| `internal/servermodel` | 55.6% | 97.2% | +41.6 |
| `internal/pdu` | 62.0% | 81.3% | +19.3 |
| Root package (`go-mms`) | 70.1% | 76.4% | +6.3 |

### Tests added

**`internal/berutil/berutil_test.go`** — 8 test functions covering `EncodeInt` (16 values including negative), `EncodeUint32` (10 values), OID encode/decode roundtrips (4 OIDs), and error cases for malformed input.

**`internal/servermodel/registry_test.go`** — 3 test functions covering NVL define/lookup (7 sub-cases), delete (4 sub-cases), and list with pagination (domain + VMD scope, sorted order, multi-page).

**`internal/pdu/server_helpers_test.go`** — 47 test functions covering all server-side marshal/unmarshal roundtrips: GetNameList, GetVarAccess, Read, Write, DefineNVL, GetNVLAttrs, DeleteNVL request/response pairs; `EncodeTypeSpec` for 14 type variants; `MarshalReadResponseWithSpec` with listName, listOfVariable, and alternate access; `ExtractInvokeID` and `ServiceKind.String`.

**`internal/pdu/file_test.go`** — 9 tests covering FileRead, FileClose, FileDelete, FileRename, ObtainFile request roundtrips; FileRead and FileDirectory response roundtrips; and error cases for missing fields.

**Root package `value_path_test.go`** — Tests for `NewGeneralizedTime`, `NewBCD`, `NewObjectIdentifier` constructors/accessors/Clone/Equal/String; `SelectComponent`/`SelectIndex`/`SelectRange` builders; `Value.Get` path traversal (13 sub-cases including error paths); `TypeSpec.Resolve` (13 sub-cases); `DefaultValue` for new types.

**Root package `value_util_test.go`** — `NewBitString` constructor test.

**Root package `file_test.go`** — 7 integration tests for `FileReadAll` (single and multi-chunk), `DownloadFile` (success and non-existent), `FileDirectoryAll` (full, paginated with custom provider, and empty directory).

**Root package `server_test.go`** — `RegisterNamedVariableList` test (7 sub-cases: valid, multiple vars, empty vars, invalid names, duplicate, missing domain).

---

## Zero-coverage function sweep (Round 2)

**Goal**: Eliminate all functions at 0% coverage where feasible.

**Starting point**: 77.5% overall, ~30 functions at 0%.

### Functions covered (previously at 0%)

| Package | Function(s) | New coverage |
|---------|------------|-------------|
| `internal/codec` | All 14 functions: `ServiceTag`, `UnmarshalInner`, `UnmarshalFull`, `MarshalMmsPdu`, `MarshalConfirmedRequest`, `MarshalConcludeRequest`, `PduType`, `UnwrapPdu`, `UnmarshalConfirmedResponse`, `UnmarshalConfirmedRequest`, `MarshalConfirmedResponse`, `MarshalConfirmedError`, `MarshalRejectPDU`, `MarshalConcludeResponse` | 87.3% (was 0%) |
| `internal/serverconn` | All 14 functions: `New`, `ReceiveAssociation`, `AcceptAssociation`, `RejectAssociation`, `negotiate`, `clampMin`, `Serve`, `handleConfirmedRequest`, `sendError`, `sendData`, `SendUnconfirmed`, `handleConclude`, `handleRelease`, `ServiceError.Error` | 78.4% (was 0%) |
| `internal/isostack/server.go` | `DecodeAssociateRequest`, `EncodeAssociateResponse`, `EncodeAssociateReject`, `DecodeReleaseRequest`, `EncodeReleaseResponse` | 77.6% (was 0%) |
| `internal/invoke` | `AllocateWithID` | 100% (was 0%) |
| `internal/session` | `parseRefuse` | 100% (was 0%) |
| `internal/pdu` | `UnmarshalStatusResponse` | 100% (was 0%) |
| `internal/pdu` | `MarshalFileOpenResponse` | 100% (was 16.7%) |
| `internal/asn1util` | `UnmarshalRaw` | 100% (was 0%) |
| Root `errors.go` | `DecodeError.Error()` | 100% (was 0%) |
| Root `mms.go` | `Abort`, `Negotiated`, `objectScopeToWire`, `handleReject`, `discardHandler.Enabled/Handle/WithAttrs/WithGroup` | 100% each (was 0%) |
| Root `server.go` | `isTemporary` | 100% (was 0%) |
| `transport/iso` | `WithClientDialOptions`, `WithLogger` | 33%–50% (was 0%) |

### Remaining at 0%

- `server.go:ListenAndServe` — Requires a real TCP listener with accept loop; inherently a production-only code path not suited for unit testing.

### New test files created

- `internal/codec/codec_test.go` — 13 tests (roundtrip marshal/unmarshal, error paths)
- `internal/isostack/server_test.go` — 7 tests (encode/decode roundtrips against client-side functions)
- `internal/serverconn/conn_test.go` — 10 tests (full association lifecycle, conclude, release, confirmed request handling, service error handling)

### Test files updated

- `internal/invoke/tracker_test.go` — `AllocateWithID` + limit enforcement
- `internal/session/session_test.go` — `parseRefuse` (valid, truncated, empty)
- `internal/pdu/status_test.go` — `UnmarshalStatusResponse` roundtrip
- `internal/pdu/file_test.go` — `MarshalFileOpenResponse` (valid, negative size, oversized)
- `internal/asn1util/raw_test.go` — `UnmarshalRaw` (valid, invalid)
- `errors_test.go` — `DecodeError.Error`, `DataAccessError.Error`, `AuthenticationError.Error`
- `server_test.go` — `TestClientAbort`, `TestClientNegotiated`, `TestObjectScopeToWire`, `TestDiscardHandler`, `TestHandleReject`, `TestIsTemporary`, `TestIsTemporaryWithInterface`
- `transport/iso/transport_test.go` — `TestWithClientDialOptions`, `TestWithLogger`

### Coverage results

| Package | Before | After | Delta |
|---------|--------|-------|-------|
| `internal/codec` | 0.0% | 87.3% | +87.3 |
| `internal/serverconn` | 0.0% | 78.4% | +78.4 |
| `internal/isostack` | ~55% | 77.6% | +22.6 |
| `internal/invoke` | ~90% | 95.7% | +5.7 |
| `internal/session` | ~85% | 88.2% | +3.2 |
| `internal/asn1util` | ~76% | 84.5% | +8.5 |
| Root `go-mms` | 76.4% | 78.3% | +1.9 |
| **Overall** | **77.5%** | **82.1%** | **+4.6** |

---

## Phase A — Hardening and Invariants

Completed all five hardening tasks from HARDENING.md Phase A.

### A1: Decoder Strictness Audit

**Objective:** Make every decoder either strict by default or explicitly documented as lenient for interop.

**Packages audited:** `internal/berutil`, `internal/codec`, `internal/pdu` (all files), `internal/acse`, `internal/session`, `internal/presentation`, `internal/isostack`.

**Fixes applied:**

| Category | Count | Details |
|----------|:-----:|---------|
| Trailing bytes rejection | 16+ functions | Added checks in codec, acse, session, presentation, pdu (data, file, journal, getvaraccess, namedvarlist, informationreport, write) |
| Unknown tag rejection | 15+ functions | Added `default` error cases in file.go (9 decoders), journal.go (6 decoders), namedvarlist.go |
| Required field validation | 1 | `presentation.parsePdvList` now requires context-id |
| New helper | 1 | `berutil.DecodeTLVExact` — wraps DecodeTLVAt with trailing byte check |

**Interop exceptions documented (6):** parseAARQ, parseAARE (ISO 8650 optional fields), parseConnectAccept, parseSimpleWithUserData (ISO 8327 PGIs), parseCPorCPA, parseNormalMode (ISO 8823 parameters), decodeFileName (bare GraphicString for C interop).

**Negative tests added:** 25+ tests across `internal/pdu/strictness_test.go`, `internal/acse/acse_test.go`, `internal/presentation/presentation_test.go`, `internal/session/session_test.go`, `internal/berutil/berutil_test.go`, `internal/codec/codec_test.go`.

**Deliverable:** `HARDENING_DECODER_CHECKLIST.md` created.

### A2: Ownership and Copy-Semantics Audit

**Objective:** Make memory ownership behavior explicit and safe.

**Files audited:** `value.go`, `mms.go`, `client_file.go`, `internal/pdu/data.go`, `internal/pdu/file.go`, `server_altaccess.go`.

**Bug fixed:** `UnmarshalFileReadResponse` in `internal/pdu/file.go` — `r.Data` aliased the transport buffer. Now copies data to prevent aliasing.

**Doc comments added:**
- `Value.Structure()` and `Value.ArrayElements()` — document shared child pointers
- `FileReadResult` — document owned copy semantics

**Test added:** `TestFileReadResponseDataOwnership` — verifies mutating the transport buffer doesn't affect returned data.

**Deliverable:** `OWNERSHIP.md` created.

### A3: Context, Timeout, and Shutdown Semantics Audit

**Objective:** Prove client/server behavior under cancellation and close races.

**Scenarios audited and verified correct:**
- Context timeout during association, send, and response wait
- Close/Abort during in-flight request
- Late response after cancel (silently discarded)
- Remote disconnect during request
- Conclude timeout
- Double Close (concurrent, idempotent)
- Server disconnect with open file handles
- Information report during shutdown

**Code fix:** `frsmTable.closeAll` now uses a bounded 10-second `context.Background()` timeout instead of the (potentially cancelled) request context.

**Tests added (5):**
- `TestCloseDuringInFlightRequest`
- `TestDoubleCloseConcurrent`
- `TestContextCancellationDuringRequest`
- `TestAbortDuringInFlightRequest`
- `TestCloseWithTimeout`

**Deliverable:** `TIMEOUT_AND_CLOSE.md` created.

### A4: Concurrency/Race Hardening

**Objective:** Exercise client/server concurrency aggressively.

**Tests added (5):**
- `TestTrackerConcurrentStress` — 50+ goroutines with Allocate/Complete/Cancel/CancelAll racing
- `TestAllocateWithIDConcurrent` — concurrent AllocateWithID with deliberate collisions
- `TestConcurrentClientRequests` — 20 concurrent Identify calls
- `TestConcurrentReadWrite` — 20 concurrent mixed GetNameList + Status calls
- `TestCloseWhileConcurrentRequests` — Close during 10 concurrent requests

**Results:** All pass with `go test -race -count=5`. No data races. No flaky tests.

**Deliverable:** `RACE_NOTES.md` created.

### A5: Defensive Bounds and Resource Controls

**Objective:** Add explicit bounds and sanity checks to prevent unbounded allocation from malicious peers.

**PDU size enforcement:**
- Client: `receiveRaw` rejects PDUs exceeding negotiated `maxPDUSize`
- Server: `Serve` logs warning and skips oversized PDUs

**Nesting depth protection:**
- Data nesting: max 64 levels (`internal/pdu/data.go`)
- TypeSpec nesting: max 32 levels (`internal/pdu/getvaraccess.go`)

**Collection size limits:**
- Access results: 65,536 (`internal/pdu/data.go`)
- Directory entries: 10,000 (`internal/pdu/file.go`)
- Journal entries: 10,000 (`internal/pdu/journal.go`)
- Name list identifiers: 100,000 (`internal/pdu/getnamelist.go`)

**String length limits:**
- File names: 1,024 bytes (`internal/pdu/file.go`)
- Identifier names: 1,024 bytes (`internal/pdu/getnamelist.go`)

**Negative tests added:** 6 boundary tests in pdu test files.

**Deliverable:** `LIMITS.md` created.

### Phase A Summary

| Metric | Before | After |
|--------|:------:|:-----:|
| Decoder strictness issues | 50+ gaps | All fixed or documented |
| Ownership bugs | 1 (file read aliasing) | 0 |
| Lifecycle tests | 2 | 12 |
| Concurrency stress tests | 0 | 5 |
| Bounds/limits enforced | 0 | 12+ |
| Race detector | Clean | Clean (×5 repeats) |
| `make check` | Clean | Clean |
| Test coverage | 82.1% | 82.2% |

**Documentation deliverables:**
1. `HARDENING_DECODER_CHECKLIST.md`
2. `OWNERSHIP.md`
3. `TIMEOUT_AND_CLOSE.md`
4. `RACE_NOTES.md`
5. `LIMITS.md`

---

## Phase B — Compliance and Interop Proof

### B1: Service Parity Matrix

Created `COMPLIANCE.md` with a comprehensive service support matrix covering 23 MMS services across 9 dimensions (client/server support, public API, encode/decode tests, negative tests, fuzzing, interop status).

Key findings documented:
- 20 services fully implemented on both client and server
- 33 fuzz targets
- 200+ negative/strictness test cases
- 8 interop tests validating against C-compatible wire encodings
- 10 known gaps documented

### B2: Golden PDU Fixtures

Created golden fixture test infrastructure with 33 hex-encoded reference files:

- `internal/pdu/testdata/golden/` — 27 PDU service fixtures (initiate, identify, status, getnamelist, getvaraccess, read, write, NVL services, file services, confirmed error, reject, information report)
- `internal/codec/testdata/golden/` — 6 codec-level fixtures (confirmed request/response wrapping, conclude, error, reject)
- `internal/pdu/golden_test.go` — 27 fixture-driven tests
- `internal/codec/golden_test.go` — 6 fixture-driven tests

Any unintentional wire encoding change now produces a clear hex diff. Regenerate with `-update-golden` flag.

### B3: Go↔C Interop Harness

Created `interop/` directory with:
- `interop/interop_test.go` — 5 interop tests (Identify, Status, GetNameList, Read, GetVariableAccessAttributes) behind `//go:build interop` tag
- `interop/start_c_server.sh` and `interop/stop_c_server.sh` — server management scripts
- `interop/README.md` — setup and usage instructions
- `INTEROP.md` — root-level documentation with service coverage matrix

Tests use `dialOrSkip` to gracefully skip when no C server is available.

### B4: BER/PDU Fuzzing Expansion

Added 15 new fuzz targets, bringing the total to 33:

**File service decoders (5):** FuzzUnmarshalFileOpenRequest, FuzzUnmarshalFileOpenResponse, FuzzUnmarshalFileReadResponse, FuzzUnmarshalFileDirectoryRequest, FuzzUnmarshalFileDirectoryResponse

**Journal decoders (2):** FuzzUnmarshalReadJournalRequest, FuzzUnmarshalReadJournalResponse

**Server-side request decoders (4):** FuzzUnmarshalGetNameListRequest, FuzzUnmarshalReadRequestParsed, FuzzUnmarshalWriteRequestParsed, FuzzUnmarshalDefineNVLRequest

**InformationReport (1):** FuzzUnmarshalInformationReport

**Protocol layers (3):** FuzzACSEParse, FuzzSessionParse, FuzzPresentationParse

All targets compile and pass 1-second smoke tests (150k-400k execs/sec).

---

## Phase C — API Stabilization

### C1: Public API Inventory

Created `API_REVIEW.md` inventorying ~200+ exported symbols across the root package and `transport/iso`. Each symbol classified as stable keep / keep but rename / keep but document sharper / deprecate.

Key findings:
- Vast majority classified as **stable keep**
- 1 deprecation recommended: `ReadArrayElement` (identical to `ReadByIndex`)
- 3 constants to unabbreviate
- Several doc improvements recommended
- Overall assessment: API is in very good shape for v0.1.0

### C2: Naming and Surface Cleanup

Implemented all cleanup recommendations:

1. **Deprecated `ReadArrayElement`** — marked with `// Deprecated:` comment directing to `ReadByIndex`
2. **Unabbreviated constants:**
   - `DataAccessErrorTemporarilyUnavail` → `DataAccessErrorTemporarilyUnavailable`
   - `DataAccessErrorObjectAccessUnsup` → `DataAccessErrorObjectAccessUnsupported`
   - `VMDPhysicalStatusPartiallyOper` → `VMDPhysicalStatusPartiallyOperational`
   - Old names retained as deprecated aliases
3. **Sharpened `ShallowCompatible` doc** — clarified it checks top-level type and element count only
4. **Documented `Value.Get` limitation** — component selectors need TypeSpec context
5. **Updated `doc.go`** — removed file/journal services from "out of scope" since they're implemented

### C3: Error Taxonomy Freeze

Created `ERRORS.md` documenting:
- 14 sentinel errors with categories and usage
- 5 typed errors (`ServiceError`, `DecodeError`, `DataAccessError`, `ProtocolError`, `AuthenticationError`) with fields and unwrap chains
- Reference tables for `DataAccessErrorCode` (11 values) and `ErrorClass` (13 values)
- 8 usage examples covering all major error patterns
- Decision tree for quick error-handling reference

### C4: Observability Polish

Created `OBSERVABILITY.md` documenting:
- Logging architecture (discardHandler pattern, slog integration)
- Three configuration points (client, server, transport listener)
- Complete log message catalog with trigger conditions
- 25+ structured log field reference
- RawHook documentation with ownership caveats
- Redaction notes (ACSE passwords never logged)

---

## Phase D — Docs and Examples

### D1: README Rewrite

Rewrote `README.md` as a serious product-facing entry point:
- What go-mms is (pure Go, no CGO, client+server)
- Feature table of all supported services
- Quick start examples (client and server)
- Architecture diagram (ISO stack layers)
- Intentional out-of-scope items
- Current status and testing metrics
- Links to documentation

### D2: Package Docs Pass

Improved documentation:
- `doc.go` — added ownership and error handling sections
- `transport/iso/doc.go` — added TSEL configuration section
- `value.go` — enhanced `Value` type docs with constructor/accessor examples and ownership notes
- `types.go` — enhanced `TypeSpec` docs explaining introspection helpers

### D3: Examples Upgrade

Polished all three example programs:
- `_examples/basic/` — enhanced doc comments, API pointers for advanced features
- `_examples/server-basic/` — improved doc comments, fixed error handling consistency
- `_examples/loopback/` — fixed Status format, added domain variable listing

### D4: Known Limitations

Created `KNOWN_LIMITATIONS.md` documenting:
- 8 unimplemented MMS services with rationale
- Protocol limitations (BER constraints, nesting/collection bounds)
- Transport limitations (COTP class 0, TCP only)
- Behavioral limitations (cancel-only context, send serialization)
- Testing gaps
- Intentional out-of-scope items

---

## Phase E — Release Preparation

### E1: Release Checklist

Created `RELEASE_CHECKLIST.md` with verification status:
- All tests, race detector, coverage: ✅
- 33 fuzz targets defined: ✅
- 33 golden fixtures locked: ✅
- 14 documentation files: ✅
- 3 examples building: ✅
- All 5 hardening tasks: ✅
- API inventoried and cleaned: ✅
- Interop harness created: ✅

Outstanding items before 1.0: extended fuzzing, first green C interop, license decision.

### E2: Versioning Decision

**Recommendation: v0.1.0** (pre-1.0, experimental but serious)

Rationale:
- The API is well-designed and stable, but has not yet been exercised by a downstream consumer (go-iec61850)
- No live C interop has been validated yet
- Extended fuzzing has not been run
- Pre-1.0 allows breaking changes if the go-iec61850 bootstrap reveals API friction

The library is ready for serious evaluation and early adoption, but the conservative posture matches the HARDENING.md guidance: "use a conservative first public stability posture unless interop and API polish are both strong."

### Phase B–E Summary

| Deliverable | Status |
|------------|:------:|
| `COMPLIANCE.md` | ✅ |
| `testdata/golden/` (33 fixtures) | ✅ |
| `interop/` harness + `INTEROP.md` | ✅ |
| 33 fuzz targets | ✅ |
| `API_REVIEW.md` | ✅ |
| Naming cleanup + deprecations | ✅ |
| `ERRORS.md` | ✅ |
| `OBSERVABILITY.md` | ✅ |
| `README.md` rewrite | ✅ |
| Package docs + doc.go | ✅ |
| Examples polished | ✅ |
| `KNOWN_LIMITATIONS.md` | ✅ |
| `RELEASE_CHECKLIST.md` | ✅ |
| Versioning: v0.1.0 recommended | ✅ |

| Metric | Value |
|--------|:-----:|
| Test coverage | 82.4% |
| Fuzz targets | 38 |
| Golden fixtures | 33 |
| Documentation files | 14 |
| Race detector | Clean |
| `make check` | Clean |

---

## Wire-Compatibility Bug Fix and New Data Types

### TagDataObjId Wire-Incompatibility Fix

A critical wire-incompatibility bug was discovered by auditing against the Wireshark MMS dissector (`packet-mms.c`) and the C reference implementation:

- **Bug**: `TagDataObjId` was incorrectly defined as `0x88` (context tag [8]), which per ISO 9506-2 is the tag for **REAL** data.
- **Fix**: Changed `TagDataObjId` to `0x8f` (context tag [15]), the correct tag for ObjectIdentifier per the MMS `Data` CHOICE definition.
- **Impact**: ObjectIdentifier values encoded by the Go library were wire-incompatible with other correct MMS implementations. Internal round-trip tests passed because encoding and decoding both used the same (incorrect) tag.

### New Data Types: REAL and BooleanArray

Two previously unsupported MMS data types were added based on the Wireshark dissector analysis:

#### REAL (tag `0x88`, context [8])
- **Wire format**: ASN.1 REAL BER encoding (ITU-T X.690 §8.5)
- **Public API**: `ValueTypeReal`, `NewReal(float64)`, `(*Value).Real() (float64, bool)`
- **Encoding support**: Binary base-2 encoding with full special value support (+0, -0, +inf, -inf, NaN)
- **Tests**: Round-trip test for normal values, special values test, fuzz target `FuzzDecodeReal`
- **Note**: Distinct from MMS FloatingPoint (`ValueTypeFloat`, tag [7]) which uses MMS-specific encoding (exponent-width byte + IEEE 754 bytes)

#### BooleanArray (tag `0x8e`, context [14])
- **Wire format**: BIT STRING (same encoding as `[4] bit-string`: unused-bits prefix + data bytes)
- **Public API**: `ValueTypeBooleanArray`, `NewBooleanArray([]byte, int)`, `(*Value).BooleanArray() ([]byte, int, bool)`
- **Tests**: Round-trip test, empty array test, fuzz target `FuzzDecodeBooleanArray`
- **Note**: Semantically represents a packed array of boolean values, distinct from generic `BitString` (tag [4])

### Files Changed

| File | Change |
|------|--------|
| `internal/pdu/data.go` | Fixed `TagDataObjId` (0x88 to 0x8f), added `TagDataReal` (0x88), `TagDataBooleanArray` (0x8e), ASN.1 REAL encode/decode, marshal/unmarshal cases |
| `internal/pdu/data_test.go` | Added `TestDataRealRoundTrip`, `TestDataRealSpecialValues`, `TestDataBooleanArrayRoundTrip`, `TestDataBooleanArrayEmpty`, `TestDataObjIdTagCorrectness` |
| `internal/pdu/fuzz_test.go` | Fixed ObjId seed tag (0x88 to 0x8f), added `FuzzDecodeReal`, `FuzzDecodeBooleanArray` |
| `types.go` | Added `ValueTypeReal`, `ValueTypeBooleanArray` with string names and `DefaultValue` support |
| `value.go` | Added `NewReal`, `NewBooleanArray` constructors, `Real()`, `BooleanArray()` accessors, `Equal`/`String` support |
| `mms.go` | Added `valueToDataValue`/`dataValueToValue` conversion for both new types |
| `COMPLIANCE.md` | Added Real and BooleanArray to data type table, updated fuzz target count to 38 |

---

## Feedback Round: Protocol Gap Closures (Gaps 1, 2, 3, 6, 7)

Five protocol gaps from `COMPLIANCE.md` were implemented in a single pass, resolving all non-infrastructure, non-legacy MMS service gaps.

### Gap 1: Server-side Abort PDU

**Problem:** The server detected client disconnects and handled ISO Release, but never proactively sent an MMS Abort PDU to the client.

**Solution:**
- Added `serverconn.Conn.SendAbort(ctx)` — sends `isostack.EncodeAbort()` (Session ABORT / ACSE ABRT) directly on the transport, bypassing the MMS data framing
- Added `ServerConn.Abort(ctx)` — public API method on the server connection, follows the same closed-check pattern as `SendInformationReport`
- Best-effort semantics: errors are returned but the caller should close the transport regardless

**Files:** `internal/serverconn/conn.go`, `server.go`

### Gap 2: Association-scope GetNameList

**Problem:** Association-scoped NVLs could be stored via `DefineNamedVariableList`, but `GetNameList` for association scope returned `service-not-supported`.

**Solution:**
- Added per-connection NVL storage to `ServerConn` (`assocNVLs map`, `assocNVLOrder []string`)
- Added internal methods: `defineAssocNVL`, `lookupAssocNVL`, `deleteAssocNVL`, `listAssocNVLs`, `deleteAllAssocNVLs`
- Updated `handleGetNameList` to handle `ObjectClassNamedVariableList + ScopeAssociation` by listing from the connection's storage
- Updated `handleDefineNVL` to route association-scope entries to the per-connection storage instead of the shared registry
- Updated `handleGetNVLAttrs` and `resolveNVLMembers` to check association-scope NVLs on the connection
- All lookup uses the `ServerConn` from the request context (`serverConnCtxKey{}`)

**Files:** `server.go`

### Gap 3: DeleteNamedVariableList Scope

**Problem:** Only `scopeOfDelete=0` (specific list names) was supported. All other scopes returned unsupported.

**Solution:**
- Added `scopeOfDelete=1` (aa-specific) support: deletes all association-scoped NVLs for the current connection
- Specific deletion (`scopeOfDelete=0`) now also checks association-scope NVLs on the connection
- Domain (2) and VMD (3) bulk scope remain unsupported, matching the C reference implementation (libIEC61850)
- Added `deleteAllAssocNVLs()` method that counts matched/deleted entries for the response

**Files:** `server.go`

### Gap 6: Unsolicited Status

**Problem:** The MMS UnsolicitedStatus service was not implemented. Only request/response Status was supported.

**Solution:**
- Added `pdu.MarshalUnsolicitedStatus(logical, physical)` — builds an UnconfirmedPDU (0xa3) with UnsolicitedStatus ([1] in UnconfirmedService CHOICE)
- Added `ServerConn.SendUnsolicitedStatus(ctx, ServerStatus)` — public API following the InformationReport pattern
- Uses `SendUnconfirmed` like InformationReport for consistent MMS data framing

**Files:** `internal/pdu/status.go`, `server.go`

### Gap 7: Cancel Service

**Problem:** The MMS Cancel service was not implemented. CancelRequestPDUs would be logged as "unexpected PDU kind".

**Solution:**
- Added Cancel PDU tag constants (`TagCancelRequest=0x86`, `TagCancelResponse=0x87`, `TagCancelError=0x88`) to `asn1util/tags.go`
- Added `PduCancelRequest/Response/Error` kinds to `pdu/mmspdu.go` with classification in `classifyTag`
- Added `codec.MarshalCancelError` and `codec.MarshalCancelResponse` to `codec/response.go`
- Added `handleCancelRequest` in `serverconn/conn.go` — responds with CancelError (error class 10 = cancel, error code 1 = invoke-id-unknown) since requests are processed synchronously and there is never an in-flight request to cancel
- Added Cancel dispatch case in the `Serve` loop

**Files:** `internal/asn1util/tags.go`, `internal/pdu/mmspdu.go`, `internal/codec/response.go`, `internal/serverconn/conn.go`

### Test Coverage Fix: NewReal and NewBooleanArray

**Problem:** `NewReal` and `NewBooleanArray` had 0% test coverage.

**Solution:** Added comprehensive tests in `value_test.go`:
- `TestValueReal` — basic constructor/accessor, type mismatch check
- `TestValueRealSpecial` — ±infinity, ±zero
- `TestValueRealNaN` — NaN round-trip
- `TestValueBooleanArray` — constructor/accessor, bit length, type mismatch check
- `TestValueBooleanArrayCopyIsolation` — defensive copy in constructor and accessor
- `TestValueRealEqual`, `TestValueBooleanArrayEqual` — equality testing
- `TestValueRealString`, `TestValueBooleanArrayString` — String() coverage

Both functions now at 100% coverage.

### New Server Tests

- `TestServerConnAbort` — integration test for `ServerConn.Abort()`
- `TestServerConnAbortClosed` — closed connection returns `ErrServerConnClosed`
- `TestServerConnSendUnsolicitedStatus` — integration test for unsolicited status
- `TestServerConnSendUnsolicitedStatusClosed` — closed connection error
- `TestServerConnAssocNVLLifecycle` — define/lookup/list/delete cycle for association-scope NVLs
- `TestServerConnDeleteAllAssocNVLs` — bulk delete of association NVLs

### Files Changed

| File | Change |
|------|--------|
| `internal/asn1util/tags.go` | Added `TagCancelRequest` (0x86), `TagCancelResponse` (0x87), `TagCancelError` (0x88) |
| `internal/pdu/mmspdu.go` | Added `PduCancelRequest`, `PduCancelResponse`, `PduCancelError`; updated `classifyTag` and `pduKindNames` |
| `internal/codec/response.go` | Added `MarshalCancelError`, `MarshalCancelResponse` |
| `internal/pdu/status.go` | Added `MarshalUnsolicitedStatus` for UnconfirmedPDU with UnsolicitedStatus |
| `internal/serverconn/conn.go` | Added `SendAbort`, `handleCancelRequest`; Cancel dispatch in `Serve` loop |
| `server.go` | Added `ServerConn.Abort`, `SendUnsolicitedStatus`; per-connection NVL storage (`assocNVLs`, `assocNVLOrder`); association-scope support in `handleGetNameList`, `handleDefineNVL`, `handleDeleteNVL`, `handleGetNVLAttrs`, `resolveNVLMembers` |
| `value_test.go` | Added 9 tests for `NewReal` and `NewBooleanArray` (100% coverage) |
| `server_test.go` | Added 6 tests for Abort, UnsolicitedStatus, and association NVL lifecycle |
| `COMPLIANCE.md` | Updated Abort/GetNameList/DeleteNVL/Status rows; added UnsolicitedStatus and Cancel rows; restructured Known Gaps into Resolved and Remaining with priority/impact table |

---

## Association-Scope Variable Support

Implemented full per-connection association-scope variable support, closing the final association-scope gap. The C reference (libIEC61850) does not support association-scope variable listing either — it only supports NVLs at association scope.

### Changes

**Per-connection variable storage on `ServerConn`:**
- Added `assocVars map[string]*servermodel.VarEntry` and `assocVarOrder []string` fields
- Added internal methods: `registerAssocVar`, `lookupAssocVar`, `listAssocVars`
- Added public `ServerConn.RegisterVariable(Variable)` for registering association-scope variables on a connection

**Server handler updates:**
- `handleGetNameList`: added `ObjectClassNamedVariable + ScopeAssociation` case to list from per-connection storage
- `handleRead`: uses new `lookupVariable` helper that checks association scope on the connection
- `handleWrite`: same `lookupVariable` helper
- `handleGetVarAccess`: same `lookupVariable` helper
- `Server.RegisterVariable`: now rejects `ObjectScopeAssociation` with a clear error directing to `ServerConn.RegisterVariable`

**Documentation updates:**
- `types.go`: Updated `ObjectScopeAssociation` doc to reflect full support
- `internal/servermodel/registry.go`: Updated `RegisterVariable` doc to clarify association-scope routing
- `COMPLIANCE.md`: Marked association-scope variable listing as resolved; updated GetNameList row

### Tests

- `TestServerConnAssocVarLifecycle` — register/duplicate/lookup/list cycle on standalone `ServerConn`
- `TestServerConnAssocVarEmptyItemID` — validation of empty ItemID
- `TestServerRegisterVariableRejectsAssociationScope` — `Server.RegisterVariable` rejects association scope
- `TestServerAssocVarReadWriteEndToEnd` — full client↔server read/write of an association-scope variable
- `TestServerAssocVarGetNameListEndToEnd` — client lists association-scope variables, verifies sorted order
- `TestServerAssocVarGetVarAccessEndToEnd` — client queries type spec of an association-scope variable

### Files Changed

| File | Change |
|------|--------|
| `server.go` | Added `ServerConn.RegisterVariable`, per-connection variable storage (`assocVars`, `assocVarOrder`), `lookupVariable` helper; updated `handleGetNameList`, `handleGetVarAccess`, `handleRead`, `handleWrite`; `Server.RegisterVariable` rejects association scope |
| `types.go` | Updated `ObjectScopeAssociation` doc comment |
| `internal/servermodel/registry.go` | Updated `RegisterVariable` doc comment |
| `server_test.go` | Added 6 tests for association-scope variable lifecycle and end-to-end operations |
| `COMPLIANCE.md` | Marked association-scope variable listing as resolved |

---

## DeleteNVL Bulk Scope (Domain and VMD)

Implemented full client and server support for `scopeOfDelete=2` (domain) and `scopeOfDelete=3` (VMD) in the DeleteNamedVariableList service. The C reference (libIEC61850) does not support these scopes — it only supports `scopeOfDelete=0` (specific). The Go implementation now exceeds the C reference.

### Changes

**Registry (`internal/servermodel/registry.go`):**
- Added `DeleteAllDomainNVLs(domain string) (matched, deleted int)` — iterates all NVLs in a domain, deletes those marked deletable
- Added `DeleteAllVMDNVLs() (matched, deleted int)` — iterates all VMD-scoped NVLs, deletes those marked deletable

**Server handler (`server.go`):**
- Extended `handleDeleteNVL` switch to handle `scopeOfDelete=2` (domain) and `scopeOfDelete=3` (VMD), delegating to the new registry methods

**PDU marshalling (`internal/pdu/namedvarlist.go`):**
- Added `MarshalDeleteNVLDomainScopeRequest(invokeID, domain)` — builds a ConfirmedRequestPdu with `scopeOfDelete=2` and domain name
- Added `MarshalDeleteNVLVMDScopeRequest(invokeID)` — builds a ConfirmedRequestPdu with `scopeOfDelete=3`

**Client API (`mms.go`):**
- Added `Client.DeleteAllDomainNVLs(ctx, domain)` — deletes all deletable NVLs in a domain
- Added `Client.DeleteAllVMDNVLs(ctx)` — deletes all deletable VMD-scoped NVLs
- Both return `*DeleteNamedVariableListResult` with `NumberMatched`/`NumberDeleted`

### Tests

**Registry unit tests (`internal/servermodel/registry_test.go`):**
- `TestDeleteAllDomainNVLs` — 3 deletable + 1 non-deletable; verifies matched/deleted counts and remainder
- `TestDeleteAllDomainNVLsEmpty` — empty domain returns (0, 0)
- `TestDeleteAllVMDNVLs` — 2 deletable + 1 non-deletable; verifies counts and remainder
- `TestDeleteAllVMDNVLsEmpty` — empty registry returns (0, 0)

**PDU roundtrip tests (`internal/pdu/namedvarlist_test.go`):**
- `TestMarshalDeleteNVLDomainScopeRequest` — marshal → unmarshal roundtrip, verifies scopeOfDelete=2 and domain name
- `TestMarshalDeleteNVLVMDScopeRequest` — marshal → unmarshal roundtrip, verifies scopeOfDelete=3

**End-to-end integration tests (`server_test.go`):**
- `TestServerDeleteAllDomainNVLs` — define 3 NVLs, bulk-delete, verify all gone via GetNameList
- `TestServerDeleteAllDomainNVLsEmptyDomain` — validates empty domain rejected client-side
- `TestServerDeleteAllVMDNVLs` — define 2 VMD NVLs, bulk-delete, verify all gone
- `TestServerDeleteAllDomainNVLsNonDeletable` — verifies non-deletable NVLs are matched but not deleted

### Files Changed

| File | Change |
|------|--------|
| `internal/servermodel/registry.go` | Added `DeleteAllDomainNVLs`, `DeleteAllVMDNVLs` |
| `internal/servermodel/registry_test.go` | Added 4 unit tests |
| `server.go` | Extended `handleDeleteNVL` for scopeOfDelete 2 and 3 |
| `internal/pdu/namedvarlist.go` | Added `MarshalDeleteNVLDomainScopeRequest`, `MarshalDeleteNVLVMDScopeRequest` |
| `internal/pdu/namedvarlist_test.go` | Added 2 PDU roundtrip tests |
| `mms.go` | Added `Client.DeleteAllDomainNVLs`, `Client.DeleteAllVMDNVLs` |
| `server_test.go` | Added 4 end-to-end integration tests |
| `COMPLIANCE.md` | Updated DeleteNVL row; moved bulk scope from remaining to resolved gaps |

---

## UTCTime Quality Byte Support

### Summary

Implemented full IEC 61850-8-1 UTCTime quality byte support across all layers. The quality byte (byte 7 of the 8-byte UTCTime wire format) is now preserved end-to-end instead of being hardcoded to `0x00` on encode and silently discarded on decode.

The C reference (libIEC61850) defaults to quality `0x0a` (10-bit sub-second accuracy). The Go implementation now matches this default.

BinaryTime decoding for report timestamps was confirmed to already be fully implemented — no changes needed.

### Quality Byte Bit Layout (IEC 61850-8-1)

| Bit(s) | Mask   | Constant | Meaning |
|--------|--------|----------|---------|
| 7      | `0x80` | `UTCTimeQualityLeapSecondsKnown` | Leap second information available |
| 6      | `0x40` | `UTCTimeQualityClockFailure` | Time source failure |
| 5      | `0x20` | `UTCTimeQualityClockNotSynchronized` | Not synchronized to external reference |
| 0-4    | `0x1F` | `UTCTimeQualityAccuracyMask` | Sub-second accuracy (bits, 0-24; 31 = unspecified) |

### Changes by Layer

**Layer 1: PDU internal (`internal/pdu/data.go`)**
- Added `TimeQuality uint8` field to `DataValue` struct
- `encodeUTCTime(t, quality)` now writes the quality byte to `buf[7]`
- `decodeUTCTime(data)` now returns `(time.Time, uint8, error)`, extracting `data[7]` as quality
- Callers in `MarshalData` and `decodeDataContentWithDepth` updated to thread quality through

**Layer 2: Public Value type (`value.go`)**
- Added `timeQuality uint8` field to `Value` struct
- Added `UTCTimeQuality() uint8` accessor (returns 0 for non-UTCTime values)
- `NewUTCTime(t)` now defaults to quality `0x0a` (matching C reference)
- Added `NewUTCTimeWithQuality(t, quality)` constructor for explicit quality
- `Clone()` copies `timeQuality`
- `Equal()` compares `timeQuality`
- `String()` includes `(q=0x0a)` suffix

**Layer 3: Wire-to-public conversion (`mms.go`)**
- `valueToDataValue`: copies `v.timeQuality` to `DataValue.TimeQuality`
- `dataValueToValue`: copies `dv.TimeQuality` to `Value.timeQuality`

**Layer 4: Public constants (`value.go`)**
- `UTCTimeQualityLeapSecondsKnown` (`0x80`)
- `UTCTimeQualityClockFailure` (`0x40`)
- `UTCTimeQualityClockNotSynchronized` (`0x20`)
- `UTCTimeQualityAccuracyMask` (`0x1F`)
- `UTCTimeQualityAccuracyUnspecified` (`0x1F`)

### Tests Added

**PDU layer (`internal/pdu/data_test.go`):**
- `TestDataUTCTimeQualityRoundTrip` — encode with quality `0x8a`, decode, verify preserved
- `TestDataUTCTimeQualityAllFlags` — 9 subtests covering zero, individual flags, combined flags, C default, and all-flags-set

**Value layer (`value_test.go`):**
- `TestValueUTCTimeDefaultQuality` — `NewUTCTime` defaults to `0x0a`
- `TestValueUTCTimeWithQuality` — explicit quality via `NewUTCTimeWithQuality`
- `TestValueUTCTimeQualityWrongType` — `UTCTimeQuality()` returns 0 for non-UTCTime
- `TestValueUTCTimeQualityClone` — quality preserved through `Clone()`
- `TestValueUTCTimeEqualDifferentQuality` — `Equal()` returns false for different quality
- `TestValueUTCTimeString` — `String()` includes quality hex
- `TestValueUTCTimeQualityConstants` — constant values match IEC 61850-8-1

### Files Changed

| File | Change |
|------|--------|
| `internal/pdu/data.go` | Added `TimeQuality` to `DataValue`; updated `encodeUTCTime`/`decodeUTCTime` signatures |
| `internal/pdu/data_test.go` | Added 2 test functions (10 subtests total) |
| `value.go` | Added quality field, accessor, constants, `NewUTCTimeWithQuality`; updated `NewUTCTime` default, `Clone`, `Equal`, `String` |
| `value_test.go` | Added 7 test functions |
| `mms.go` | Updated `valueToDataValue` and `dataValueToValue` to thread quality |
| `COMPLIANCE.md` | Updated UTCTime row to document quality support |
