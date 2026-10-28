# MMS Service Compliance Matrix

> Auto-generated compliance audit of `go-mms` against ISO 9506 (MMS) services.
> Each cell is verified against the source code and test suites.

## Service Support

| Service | Client | Server | Public API | Encode Test | Decode Test | Negative Tests | Fuzzed | Interop vs C | Notes |
|---------|:------:|:------:|:----------:|:-----------:|:-----------:|:--------------:|:------:|:------------:|-------|
| Initiate | ✅ | ✅ | `NewClient`, `Server.Serve` | ✅ | ✅ | ✅ | ✅ | ✅ | Full handshake with parameter negotiation; `MarshalInitiateRequest`/`UnmarshalInitiateResponse` round-trip tested |
| Conclude | ✅ | ✅ | `Client.Close` | ✅ | ✅ | ✅ | ❌ | ✅ | Graceful close with `ConcludeRequest`/`ConcludeResponse`; server handles both MMS Conclude and ISO Release |
| Abort | ✅ | ✅ | `Client.Abort`, `ServerConn.Abort` | ✅ | N/A | ✅ | ❌ | ✅ | Client and server both send ACSE ABRT; server `SendAbort` is best-effort before transport close |
| Identify | ✅ | ✅ | `Client.Identify`, `Server.HandleIdentify` | ✅ | ✅ | ✅ | ❌ | ✅ | Full request/response; server returns `ServiceError` when no handler registered |
| Status | ✅ | ✅ | `Client.Status`, `Client.StatusWithOptions`, `Server.HandleStatus` | ✅ | ✅ | ✅ | ❌ | ✅ | Supports `ExtendedDerivation` flag; VMD logical/physical status |
| UnsolicitedStatus | N/A | ✅ | `ServerConn.SendUnsolicitedStatus` | ✅ | N/A | ✅ | ❌ | ❌ | Server-originated unconfirmed PDU; follows InformationReport pattern |
| Cancel | N/A | ✅ | Handled in `serverconn.Conn` dispatch | ✅ | ✅ | ✅ | ❌ | ❌ | Server responds with CancelError (invoke-id-unknown) since requests are synchronous. Tags per ISO 9506-2: cancel-RequestPDU [5], cancel-ResponsePDU [6], cancel-ErrorPDU [7] (serviceError [1]); earlier releases used [6]/[7]/[8] |
| GetNameList | ✅ | ✅ | `Client.GetNameList`, `Client.GetNameListAll` | ✅ | ✅ | ✅ | ✅ | ✅ | All scopes (VMD/Domain/Association); pagination with `ContinueAfter`; stall detection; supports Domain, NamedVariable, NVL, Journal object classes; association-scope variable and NVL listing via per-connection storage |
| GetVariableAccessAttributes | ✅ | ✅ | `Client.GetVariableAccessAttributes` | ✅ | ✅ | ✅ | ✅ | ✅ | Full TypeSpec decode including Boolean, Integer, Unsigned, Float, BitString, OctetString, VisibleString, MmsString, UTCTime, BinaryTime, Array, Structure, NamedType, GeneralizedTime [11], BCD [13], ObjectIdentifier [15]; the server encodes the same set (Real and BooleanArray have no TypeDescription in ISO 9506-2) |
| Read | ✅ | ✅ | `Client.Read`, `ReadMultiple`, `ReadObject`, `ReadVariables`, `ReadComponent`, `ReadByIndex`, `ReadArrayElement`, `ReadArrayRange`, `ReadNamedVariableList` | ✅ | ✅ | ✅ | ✅ | ✅ | Single/multi-variable; alternate access (component, index, index-range); NVL read by listName; `SpecificationWithResult`; per-variable `DataAccessError` |
| Write | ✅ | ✅ | `Client.Write`, `WriteObject`, `WriteVariables`, `WriteComponent`, `WriteArrayElement`, `WriteNamedVariableList` | ✅ | ✅ | ✅ | ✅ | ✅ | Single/multi-variable; alternate access; NVL write by listName; partial-success semantics |
| DefineNamedVariableList | ✅ | ✅ | `Client.DefineNamedVariableList`, `Server.RegisterNamedVariableList` | ✅ | ✅ | ✅ | ✅ | ✅ | All scopes; alternate access in member specs; server-side dynamic define via client request |
| GetNamedVariableListAttributes | ✅ | ✅ | `Client.GetNamedVariableListAttributes` | ✅ | ✅ | ✅ | ✅ | ✅ | Returns deletable flag + member list with alternate access |
| DeleteNamedVariableList | ✅ | ✅ | `Client.DeleteNamedVariableList`, `DeleteAllDomainNVLs`, `DeleteAllVMDNVLs` | ✅ | ✅ | ✅ | ✅ | ✅ | Returns `NumberMatched`/`NumberDeleted`; server handles all four `scopeOfDelete` values: 0 (specific), 1 (aa-specific), 2 (domain bulk), 3 (VMD bulk); client helpers cover 0, 2, 3 |
| InformationReport | ✅ | ✅ | `Client.OnInformationReport`, `ServerConn.SendInformationReport`, `Server.Broadcast` | ✅ | ✅ | ✅ | ❌ | ✅ | Both list-of-variable and named-variable-list forms; concurrent with confirmed services; handler panic recovery |
| FileOpen | ✅ | ✅ | `Client.FileOpen` | ✅ | ✅ | ✅ | ❌ | ✅ | FRSM state machine; returns frsmId, size, lastModified |
| FileRead | ✅ | ✅ | `Client.FileRead`, `FileReadAll` | ✅ | ✅ | ✅ | ❌ | ✅ | Chunked read with `MoreFollows`; `FileReadAll` convenience |
| FileClose | ✅ | ✅ | `Client.FileClose` | ✅ | ✅ | ✅ | ❌ | ✅ | Releases FRSM handle; auto-close on disconnect |
| FileDirectory | ✅ | ✅ | `Client.FileDirectory`, `FileDirectoryAll` | ✅ | ✅ | ✅ | ❌ | ✅ | Pagination with `ContinueAfter`; `FileDirectoryAll` convenience with stall detection |
| FileDelete | ✅ | ✅ | `Client.FileDelete` | ✅ | ✅ | ✅ | ❌ | ✅ | Maps `fs.ErrNotExist` → file-non-existent |
| FileRename | ✅ | ✅ | `Client.FileRename` | ✅ | ✅ | ✅ | ❌ | ✅ | Collision tests included |
| ObtainFile | ✅ | ✅ | `Client.ObtainFile` | ✅ | ✅ | ✅ | ❌ | ✅ | Server delegates to `FileProvider`; MMS segmented role-reversal (server→client FileOpen/FileRead/FileClose) is not implemented — see `KNOWN_LIMITATIONS.md` |
| ReadJournal | ✅ | ✅ | `Client.ReadJournalTimeRange`, `ReadJournalStartAfter` | ✅ | ✅ | ✅ | ❌ | ✅ | Time-range and start-after cursors; pagination with `MoreFollows`; `JournalProvider` interface |
| Reject PDU | ✅ | ✅ | Handled in `Client.processConfirmedPDU` | ✅ | ✅ | ✅ | ✅ | ❌ | Client decodes into `*RejectError` (`Type`, `Reason`, invoke ID), which unwraps to `*ProtocolError` (phase `mms`); server sends Reject for malformed requests |
| ConfirmedError | ✅ | ✅ | `ServiceError`, `ErrorClass`, `ErrorCode` | ✅ | ✅ | ✅ | ✅ | ✅ | Client decodes into `*ServiceError` (`CodeName()` gives the ISO 9506-2 code name); server generates via `serverconn.ServiceError`, and passes through a `*ServiceError` returned by Identify/Status handlers; trailing-byte strictness |
| DownloadFile | ✅ | ✅ | `Client.DownloadFile` | ✅ | ✅ | ✅ | ❌ | ✅ | Convenience: Open → ReadAll → Close |

## Protocol Layer Support

| Layer | Status | Notes |
|-------|--------|-------|
| BER/TLV encoding | ✅ Implemented | `internal/berutil` — hand-rolled TLV encoder/decoder; supports short/long-form lengths, signed/unsigned integers, float32/float64; 4 fuzz targets |
| MMS PDU marshaling | ✅ Implemented | `internal/pdu` — 39 source files covering all 17+ service types; tag-driven dispatch; confirmed request/response/error/reject envelope; PDUs and service choices with long-form (multi-byte) tags are rejected instead of being truncated to a short tag |
| ACSE (association) | ✅ Implemented | `internal/acse` — AARQ/AARE/ABRT encoding/decoding; password authentication; AP-title/AE-qualifier; 34 unit tests |
| Presentation layer | ✅ Implemented | `internal/presentation` — ISO 8823 presentation context negotiation; ASN.1 abstract/transfer syntax; 13 unit tests |
| Session layer | ✅ Implemented | `internal/session` — ISO 8327 CONNECT/ACCEPT/DATA/FINISH/RELEASE SPDUs; selector negotiation; 19 unit tests |
| ISO transport (TPKT/COTP) | ✅ Implemented | `transport/iso` — RFC 1006 TPKT framing; ISO 8073 COTP class 0; TLS support; concurrent accept-side handshakes with timeout; dial retry; per-direction deadlines for full-duplex use |

## Data Type Support

| MMS Data Variant | Encode | Decode | Round-trip Test | Fuzzed | Tag | Notes |
|------------------|:------:|:------:|:---------------:|:------:|:-------------------:|-------|
| DataAccessError  | ✅ | ✅ | ✅ | ✅ | `0x80` [0]  | Per-variable error in Read/Write results |
| Array            | ✅ | ✅ | ✅ | ✅ | `0xa1` [1]  | Recursive/nested decode |
| Structure        | ✅ | ✅ | ✅ | ✅ | `0xa2` [2]  | Recursive/nested decode |
| Boolean          | ✅ | ✅ | ✅ | ✅ | `0x83` [3]  | |
| BitString        | ✅ | ✅ | ✅ | ✅ | `0x84` [4]  | BIT STRING with unused-bits prefix |
| Integer          | ✅ | ✅ | ✅ | ✅ | `0x85` [5]  | Signed BER integer |
| Unsigned         | ✅ | ✅ | ✅ | ✅ | `0x86` [6]  | Unsigned BER integer |
| FloatingPoint    | ✅ | ✅ | ✅ | ✅ | `0x87` [7]  | MMS FloatingPoint, not ASN.1 REAL. `Value.Equal` compares Float/Real bit for bit, with all NaNs equal (+0 ≠ −0) |
| Real             | ✅ | ✅ | ✅ | ✅ | `0x88` [8]  | ASN.1 REAL |
| OctetString      | ✅ | ✅ | ✅ | ✅ | `0x89` [9]  | Raw bytes |
| VisibleString    | ✅ | ✅ | ✅ | ✅ | `0x8a` [10] | VisibleString |
| GeneralizedTime  | ✅ | ✅ | ✅ | ✅ | `0x8b` [11] | ASN.1 GeneralizedTime |
| BinaryTime       | ✅ | ✅ | ✅ | ✅ | `0x8c` [12] | MMS TimeOfDay: 4-octet form (ms since midnight) for values 0–86399999, 6-octet dated form otherwise; ms-of-day ≥ 86400000 rejected on decode |
| BCD              | ✅ | ✅ | ✅ | ✅ | `0x8d` [13] | Binary-coded decimal |
| BooleanArray     | ✅ | ✅ | ✅ | ✅ | `0x8e` [14] | Packed booleans using BIT STRING encoding |
| ObjectIdentifier | ✅ | ✅ | ✅ | ✅ | `0x8f` [15] | ASN.1 OBJECT IDENTIFIER |
| MmsString        | ✅ | ✅ | ✅ | ✅ | `0x90` [16] | MMSString |
| UTCTime          | ✅ | ✅ | ✅ | ✅ | `0x91` [17] | MMS UTC time wire format; IEC 61850-8-1 TimeQuality byte fully exposed (`NewUTCTimeWithQuality`, `UTCTimeQuality()`, bit-mask constants); default quality `0x0a` matching C reference; the 24-bit fraction round-trips losslessly (decode → encode yields the same octets) |

## Test Infrastructure

| Category | Count | Notes |
|----------|:-----:|-------|
| Unit tests | ~1017 | `func Test*` outside `e2e/` and `interop/` |
| Integration tests (end-to-end) | ~42 | Client↔Server over loopback transport; file, journal, NVL lifecycle |
| Negative/strictness tests | ~200+ | Malformed PDU, unknown tags, trailing bytes, truncated inputs, missing fields, invalid scopes |
| Race/concurrency tests | ~14 | `TestConcurrent*`, `TestDoubleClose*`, `TestClose*During*`; designed for `go test -race` |
| Fuzz targets | 57 | `internal/pdu` (35), `internal/berutil` (6), `internal/codec` (4), `internal/isostack` (4), root package (3: `FuzzValueCodec`, `FuzzServerDispatch`, `FuzzServerSession`), `internal/asn1util` (2), `internal/acse`, `internal/session`, `internal/presentation` (1 each). `make fuzz` runs all (discovered with `go test -list`) |
| Chaos / stress tests | 4 | `e2e/chaos_test.go` (`TestChaos_*`, `TestStress_Concurrent`) over `internal/chaos` fault-injecting connections (delays, chunked I/O, bandwidth limit, drops, resets, stalls); `make chaos` |
| Benchmarks | 21 | `internal/pdu/bench_test.go`, `value_bench_test.go`, `internal/apisurface` |
| End-to-end API tests | 64 | `e2e/` — every exported `Client`, `Server` and `ServerConn` method and every `Value` constructor between a go-mms client and server over ISO transport; `TestAPICoverage` fails when a public method has no test |
| Live interop tests | 120 | `interop/` (`-tags=interop`) — both directions against libiec61850 `v1.6.2.1-89-g6e265c54` via iec61850-interop; `TestInteropCoverage` guards the API and the server's service dispatch (see [INTEROP.md](INTEROP.md)) |
| Wire-vector tests | 11 | `internal/pdu/interop_test.go`, `internal/pdu/peer_vectors_test.go` — BER patterns as encoded by libiec61850 |
| Golden fixture tests | 33 | 27 in `internal/pdu/testdata/golden/` + 6 in `internal/codec/testdata/golden/`; regenerate with `-update-golden` flag |
| Coverage gate | ≥ 90% | `make coverage` (`scripts/coverage-gate.sh`) fails when any package or the total is below `COVERAGE_MIN` (default 90); run by the CI `quality` job |

## Known Gaps

### Resolved

1. ~~No golden-file corpus~~ — **Resolved.** 33 golden hex fixtures in `testdata/golden/` covering all PDU services and codec envelopes.
2. ~~Abort PDU (server→client)~~ — **Resolved.** `ServerConn.Abort()` sends ACSE ABRT before transport close.
3. ~~Association-scope object listing~~ — **Resolved.** `GetNameList` for association-scope NVLs is now implemented with per-connection storage.
4. ~~DeleteNamedVariableList scope~~ — **Resolved.** All four `scopeOfDelete` values (0=specific, 1=aa-specific, 2=domain, 3=VMD) are supported by the server; the client exposes 0, 2 and 3 (no aa-specific helper). Goes beyond C reference which only supports `scopeOfDelete=0`.
5. ~~No fuzz coverage for file/journal PDUs~~ — **Resolved.** 57 fuzz targets across all service categories, the value codec and server dispatch.
6. ~~Unsolicited Status~~ — **Resolved.** `ServerConn.SendUnsolicitedStatus()` sends UnconfirmedPDU with VMD status.
7. ~~Cancel service~~ — **Resolved.** Server handles CancelRequestPDU and responds with CancelError (invoke-id-unknown, since requests are processed synchronously).
8. ~~No live-wire interop testing~~ — **Resolved.** `interop/` runs both directions against libiec61850 through the iec61850-interop adapter images; see [INTEROP.md](INTEROP.md).

### Remaining Gaps

| # | Gap | Priority | Impact | In C ref? | Description |
|---|-----|:--------:|:------:|:---------:|-------------|
| 1 | Semaphore, Event, Program Invocation services | Low | Completeness | Not in C either | ISO 9506-2 service groups with complex state machines. Zero implementations in the C reference (libIEC61850). Legacy MMS features rarely used in IEC 61850 deployments. Effort: very high (thousands of lines from standard alone). |
| 2 | Segmented file transfer (ObtainFile) | Medium | Feature | **Fully in C** | C implements a multi-step state machine (~400 lines) with role reversal (server sends confirmed requests to client). Go currently delegates ObtainFile to FileProvider as a single synchronous operation. Requires new server→client confirmed request plumbing. Effort: ~500-800 lines. |
