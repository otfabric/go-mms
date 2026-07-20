# MMS Service Compliance Matrix

> Auto-generated compliance audit of `go-mms` against ISO 9506 (MMS) services.
> Each cell is verified against the source code and test suites.

## Service Support

| Service | Client | Server | Public API | Encode Test | Decode Test | Negative Tests | Fuzzed | Interop vs C | Notes |
|---------|:------:|:------:|:----------:|:-----------:|:-----------:|:--------------:|:------:|:------------:|-------|
| Initiate | ✅ | ✅ | `NewClient`, `Server.Serve` | ✅ | ✅ | ✅ | ✅ | ✅ | Full handshake with parameter negotiation; `MarshalInitiateRequest`/`UnmarshalInitiateResponse` round-trip tested |
| Conclude | ✅ | ✅ | `Client.Close` | ✅ | ✅ | ✅ | ❌ | ❌ | Graceful close with `ConcludeRequest`/`ConcludeResponse`; server handles both MMS Conclude and ISO Release |
| Abort | ✅ | ✅ | `Client.Abort`, `ServerConn.Abort` | ✅ | N/A | ✅ | ❌ | ❌ | Client and server both send ACSE ABRT; server `SendAbort` is best-effort before transport close |
| Identify | ✅ | ✅ | `Client.Identify`, `Server.HandleIdentify` | ✅ | ✅ | ✅ | ❌ | ❌ | Full request/response; server returns `ServiceError` when no handler registered |
| Status | ✅ | ✅ | `Client.Status`, `Client.StatusWithOptions`, `Server.HandleStatus` | ✅ | ✅ | ✅ | ❌ | ❌ | Supports `ExtendedDerivation` flag; VMD logical/physical status |
| UnsolicitedStatus | N/A | ✅ | `ServerConn.SendUnsolicitedStatus` | ✅ | N/A | ✅ | ❌ | ❌ | Server-originated unconfirmed PDU; follows InformationReport pattern |
| Cancel | N/A | ✅ | Handled in `serverconn.Conn` dispatch | ✅ | ✅ | ✅ | ❌ | ❌ | Server responds with CancelError (invoke-id-unknown) since requests are synchronous |
| GetNameList | ✅ | ✅ | `Client.GetNameList`, `Client.GetNameListAll` | ✅ | ✅ | ✅ | ✅ | ✅ | All scopes (VMD/Domain/Association); pagination with `ContinueAfter`; stall detection; supports Domain, NamedVariable, NVL, Journal object classes; association-scope variable and NVL listing via per-connection storage |
| GetVariableAccessAttributes | ✅ | ✅ | `Client.GetVariableAccessAttributes` | ✅ | ✅ | ✅ | ✅ | ✅ | Full TypeSpec decode including Boolean, Integer, Unsigned, Float, BitString, OctetString, VisibleString, MmsString, UTCTime, BinaryTime, Array, Structure, NamedType, GeneralizedTime, BCD, ObjectIdentifier |
| Read | ✅ | ✅ | `Client.Read`, `ReadMultiple`, `ReadObject`, `ReadVariables`, `ReadComponent`, `ReadByIndex`, `ReadArrayElement`, `ReadArrayRange`, `ReadNamedVariableList` | ✅ | ✅ | ✅ | ✅ | ✅ | Single/multi-variable; alternate access (component, index, index-range); NVL read by listName; `SpecificationWithResult`; per-variable `DataAccessError` |
| Write | ✅ | ✅ | `Client.Write`, `WriteObject`, `WriteVariables`, `WriteComponent`, `WriteArrayElement`, `WriteNamedVariableList` | ✅ | ✅ | ✅ | ✅ | ✅ | Single/multi-variable; alternate access; NVL write by listName; partial-success semantics |
| DefineNamedVariableList | ✅ | ✅ | `Client.DefineNamedVariableList`, `Server.RegisterNamedVariableList` | ✅ | ✅ | ✅ | ✅ | ❌ | All scopes; alternate access in member specs; server-side dynamic define via client request |
| GetNamedVariableListAttributes | ✅ | ✅ | `Client.GetNamedVariableListAttributes` | ✅ | ✅ | ✅ | ✅ | ❌ | Returns deletable flag + member list with alternate access |
| DeleteNamedVariableList | ✅ | ✅ | `Client.DeleteNamedVariableList`, `DeleteAllDomainNVLs`, `DeleteAllVMDNVLs` | ✅ | ✅ | ✅ | ✅ | ❌ | Returns `NumberMatched`/`NumberDeleted`; all four `scopeOfDelete` values supported: 0 (specific), 1 (aa-specific), 2 (domain bulk), 3 (VMD bulk) |
| InformationReport | ✅ | ✅ | `Client.OnInformationReport`, `ServerConn.SendInformationReport`, `Server.Broadcast` | ✅ | ✅ | ✅ | ❌ | ❌ | Both list-of-variable and named-variable-list forms; concurrent with confirmed services; handler panic recovery |
| FileOpen | ✅ | ✅ | `Client.FileOpen` | ✅ | ✅ | ✅ | ❌ | ❌ | FRSM state machine; returns frsmId, size, lastModified |
| FileRead | ✅ | ✅ | `Client.FileRead`, `FileReadAll` | ✅ | ✅ | ✅ | ❌ | ❌ | Chunked read with `MoreFollows`; `FileReadAll` convenience |
| FileClose | ✅ | ✅ | `Client.FileClose` | ✅ | ✅ | ✅ | ❌ | ❌ | Releases FRSM handle; auto-close on disconnect |
| FileDirectory | ✅ | ✅ | `Client.FileDirectory`, `FileDirectoryAll` | ✅ | ✅ | ✅ | ❌ | ❌ | Pagination with `ContinueAfter`; `FileDirectoryAll` convenience with stall detection |
| FileDelete | ✅ | ✅ | `Client.FileDelete` | ✅ | ✅ | ✅ | ❌ | ❌ | Maps `fs.ErrNotExist` → file-non-existent |
| FileRename | ✅ | ✅ | `Client.FileRename` | ✅ | ✅ | ✅ | ❌ | ❌ | Collision tests included |
| ObtainFile | ✅ | ✅ | `Client.ObtainFile` | ✅ | ✅ | ✅ | ❌ | ❌ | Server delegates to `FileProvider`; MMS segmented role-reversal (server→client FileOpen/FileRead/FileClose) is not implemented — see `KNOWN_LIMITATIONS.md` |
| ReadJournal | ✅ | ✅ | `Client.ReadJournalTimeRange`, `ReadJournalStartAfter` | ✅ | ✅ | ✅ | ❌ | ❌ | Time-range and start-after cursors; pagination with `MoreFollows`; `JournalProvider` interface |
| Reject PDU | ✅ | ✅ | Handled in `Client.processConfirmedPDU` | ✅ | ✅ | ✅ | ✅ | ❌ | Client decodes and surfaces as `ProtocolError`; server sends Reject for malformed requests |
| ConfirmedError | ✅ | ✅ | `ServiceError`, `ErrorClass`, `ErrorCode` | ✅ | ✅ | ✅ | ✅ | ❌ | Client decodes into `*ServiceError`; server generates via `serverconn.ServiceError`; trailing-byte strictness |
| DownloadFile | ✅ | ✅ | `Client.DownloadFile` | ✅ | ✅ | ✅ | ❌ | ❌ | Convenience: Open → ReadAll → Close |

## Protocol Layer Support

| Layer | Status | Notes |
|-------|--------|-------|
| BER/TLV encoding | ✅ Implemented | `internal/berutil` — hand-rolled TLV encoder/decoder; supports short/long-form lengths, signed/unsigned integers, float32/float64; 4 fuzz targets |
| MMS PDU marshaling | ✅ Implemented | `internal/pdu` — 39 source files covering all 17+ service types; tag-driven dispatch; confirmed request/response/error/reject envelope |
| ACSE (association) | ✅ Implemented | `internal/acse` — AARQ/AARE/ABRT encoding/decoding; password authentication; AP-title/AE-qualifier; 34 unit tests |
| Presentation layer | ✅ Implemented | `internal/presentation` — ISO 8823 presentation context negotiation; ASN.1 abstract/transfer syntax; 13 unit tests |
| Session layer | ✅ Implemented | `internal/session` — ISO 8327 CONNECT/ACCEPT/DATA/FINISH/RELEASE SPDUs; selector negotiation; 19 unit tests |
| ISO transport (TPKT/COTP) | ✅ Implemented | `transport/iso` — RFC 1006 TPKT framing; ISO 8073 COTP class 0; TLS support; 36 unit tests + 9 integration tests |

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
| FloatingPoint    | ✅ | ✅ | ✅ | ✅ | `0x87` [7]  | MMS FloatingPoint, not ASN.1 REAL |
| Real             | ✅ | ✅ | ✅ | ✅ | `0x88` [8]  | ASN.1 REAL |
| OctetString      | ✅ | ✅ | ✅ | ✅ | `0x89` [9]  | Raw bytes |
| VisibleString    | ✅ | ✅ | ✅ | ✅ | `0x8a` [10] | VisibleString |
| GeneralizedTime  | ✅ | ✅ | ✅ | ✅ | `0x8b` [11] | ASN.1 GeneralizedTime |
| BinaryTime       | ✅ | ✅ | ✅ | ✅ | `0x8c` [12] | MMS BinaryTime, short and extended forms |
| BCD              | ✅ | ✅ | ✅ | ✅ | `0x8d` [13] | Binary-coded decimal |
| BooleanArray     | ✅ | ✅ | ✅ | ✅ | `0x8e` [14] | Packed booleans using BIT STRING encoding |
| ObjectIdentifier | ✅ | ✅ | ✅ | ✅ | `0x8f` [15] | ASN.1 OBJECT IDENTIFIER |
| MmsString        | ✅ | ✅ | ✅ | ✅ | `0x90` [16] | MMSString |
| UTCTime          | ✅ | ✅ | ✅ | ✅ | `0x91` [17] | MMS UTC time wire format; IEC 61850-8-1 TimeQuality byte fully exposed (`NewUTCTimeWithQuality`, `UTCTimeQuality()`, bit-mask constants); default quality `0x0a` matching C reference |

## Test Infrastructure

| Category | Count | Notes |
|----------|:-----:|-------|
| Unit tests | ~632 | `func Test*` across 51 test files |
| Integration tests (end-to-end) | ~42 | Client↔Server over loopback transport; file, journal, NVL lifecycle |
| Negative/strictness tests | ~200+ | Malformed PDU, unknown tags, trailing bytes, truncated inputs, missing fields, invalid scopes (across 33 test files) |
| Race/concurrency tests | ~14 | `TestConcurrent*`, `TestDoubleClose*`, `TestClose*During*`; designed for `go test -race` |
| Fuzz targets | 38 | `internal/pdu/fuzz_test.go` (31): DecodePdu, DataElement, TypeSpec, ObjectName, AccessResults, ReadResponse, WriteResponse, GetNameList, GetVarAccess, NVLAttrs, DeleteNVL, ConfirmedError, RejectPDU, ConfirmedResponse, FileOpenRequest, FileOpenResponse, FileReadResponse, FileDirectoryRequest, FileDirectoryResponse, ReadJournalRequest, ReadJournalResponse, GetNameListRequest, ReadRequestParsed, WriteRequestParsed, DefineNVLRequest, InformationReport, GeneralizedTime, BCD, ObjectIdentifier, Real, BooleanArray + `internal/berutil/fuzz_test.go` (4): TLV, Length, Integer, Unsigned + `internal/acse/fuzz_test.go` (1), `internal/session/fuzz_test.go` (1), `internal/presentation/fuzz_test.go` (1) |
| Benchmarks | 20 | `internal/pdu/bench_test.go` (14) + `value_bench_test.go` (6) |
| Interop tests | 8 | `internal/pdu/interop_test.go` — validates wire encoding against known-good BER patterns compatible with C reference implementation |
| Golden fixture tests | 33 | 27 in `internal/pdu/testdata/golden/` + 6 in `internal/codec/testdata/golden/`; regenerate with `-update-golden` flag |

## Known Gaps

### Resolved

1. ~~No golden-file corpus~~ — **Resolved.** 33 golden hex fixtures in `testdata/golden/` covering all PDU services and codec envelopes.
2. ~~Abort PDU (server→client)~~ — **Resolved.** `ServerConn.Abort()` sends ACSE ABRT before transport close.
3. ~~Association-scope object listing~~ — **Resolved.** `GetNameList` for association-scope NVLs is now implemented with per-connection storage.
4. ~~DeleteNamedVariableList scope~~ — **Resolved.** All four `scopeOfDelete` values (0=specific, 1=aa-specific, 2=domain, 3=VMD) fully supported on both client and server. Goes beyond C reference which only supports `scopeOfDelete=0`.
5. ~~No fuzz coverage for file/journal PDUs~~ — **Resolved.** 38 fuzz targets across all service categories.
6. ~~Unsolicited Status~~ — **Resolved.** `ServerConn.SendUnsolicitedStatus()` sends UnconfirmedPDU with VMD status.
7. ~~Cancel service~~ — **Resolved.** Server handles CancelRequestPDU and responds with CancelError (invoke-id-unknown, since requests are processed synchronously).

### Remaining Gaps

| # | Gap | Priority | Impact | In C ref? | Description |
|---|-----|:--------:|:------:|:---------:|-------------|
| 1 | No live-wire interop testing | Medium-high | Quality | N/A | Interop tests use inline byte patterns. No automated harness connecting to a real MMS server or C reference. Requires Docker/CI infrastructure, not code porting. |
| 2 | Semaphore, Event, Program Invocation services | Low | Completeness | Not in C either | ISO 9506-2 service groups with complex state machines. Zero implementations in the C reference (libIEC61850). Legacy MMS features rarely used in IEC 61850 deployments. Effort: very high (thousands of lines from standard alone). |
| 3 | Segmented file transfer (ObtainFile) | Medium | Feature | **Fully in C** | C implements a multi-step state machine (~400 lines) with role reversal (server sends confirmed requests to client). Go currently delegates ObtainFile to FileProvider as a single synchronous operation. Requires new server→client confirmed request plumbing. Effort: ~500-800 lines. |
