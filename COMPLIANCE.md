# MMS Service Compliance Matrix

> Auto-generated compliance audit of `go-mms` against ISO 9506 (MMS) services.
> Each cell is verified against the source code and test suites.

## Service Support

| Service | Client | Server | Public API | Encode Test | Decode Test | Negative Tests | Fuzzed | Interop vs C | Notes |
|---------|:------:|:------:|:----------:|:-----------:|:-----------:|:--------------:|:------:|:------------:|-------|
| Initiate | ✅ | ✅ | `NewClient`, `Server.Serve` | ✅ | ✅ | ✅ | ✅ | ✅ | Full handshake with parameter negotiation; `MarshalInitiateRequest`/`UnmarshalInitiateResponse` round-trip tested |
| Conclude | ✅ | ✅ | `Client.Close` | ✅ | ✅ | ✅ | ❌ | ❌ | Graceful close with `ConcludeRequest`/`ConcludeResponse`; server handles both MMS Conclude and ISO Release |
| Abort | ✅ | ⚠️ | `Client.Abort` | ✅ | N/A | ✅ | ❌ | ❌ | Client sends ACSE ABRT; server detects disconnect but does not send Abort PDU |
| Identify | ✅ | ✅ | `Client.Identify`, `Server.HandleIdentify` | ✅ | ✅ | ✅ | ❌ | ❌ | Full request/response; server returns `ServiceError` when no handler registered |
| Status | ✅ | ✅ | `Client.Status`, `Client.StatusWithOptions`, `Server.HandleStatus` | ✅ | ✅ | ✅ | ❌ | ❌ | Supports `ExtendedDerivation` flag; VMD logical/physical status |
| GetNameList | ✅ | ✅ | `Client.GetNameList`, `Client.GetNameListAll` | ✅ | ✅ | ✅ | ✅ | ✅ | All scopes (VMD/Domain/AA); pagination with `ContinueAfter`; stall detection; supports Domain, NamedVariable, NVL, Journal object classes |
| GetVariableAccessAttributes | ✅ | ✅ | `Client.GetVariableAccessAttributes` | ✅ | ✅ | ✅ | ✅ | ✅ | Full TypeSpec decode including Boolean, Integer, Unsigned, Float, BitString, OctetString, VisibleString, MmsString, UTCTime, BinaryTime, Array, Structure, NamedType, GeneralizedTime, BCD, ObjectIdentifier |
| Read | ✅ | ✅ | `Client.Read`, `ReadMultiple`, `ReadObject`, `ReadVariables`, `ReadComponent`, `ReadByIndex`, `ReadArrayElement`, `ReadArrayRange`, `ReadNamedVariableList` | ✅ | ✅ | ✅ | ✅ | ✅ | Single/multi-variable; alternate access (component, index, index-range); NVL read by listName; `SpecificationWithResult`; per-variable `DataAccessError` |
| Write | ✅ | ✅ | `Client.Write`, `WriteObject`, `WriteVariables`, `WriteComponent`, `WriteArrayElement`, `WriteNamedVariableList` | ✅ | ✅ | ✅ | ✅ | ✅ | Single/multi-variable; alternate access; NVL write by listName; partial-success semantics |
| DefineNamedVariableList | ✅ | ✅ | `Client.DefineNamedVariableList`, `Server.RegisterNamedVariableList` | ✅ | ✅ | ✅ | ✅ | ❌ | All scopes; alternate access in member specs; server-side dynamic define via client request |
| GetNamedVariableListAttributes | ✅ | ✅ | `Client.GetNamedVariableListAttributes` | ✅ | ✅ | ✅ | ✅ | ❌ | Returns deletable flag + member list with alternate access |
| DeleteNamedVariableList | ✅ | ✅ | `Client.DeleteNamedVariableList` | ✅ | ✅ | ✅ | ✅ | ❌ | Returns `NumberMatched`/`NumberDeleted`; only `scopeOfDelete=0` (specific) supported |
| InformationReport | ✅ | ✅ | `Client.OnInformationReport`, `ServerConn.SendInformationReport`, `Server.Broadcast` | ✅ | ✅ | ✅ | ❌ | ❌ | Both list-of-variable and named-variable-list forms; concurrent with confirmed services; handler panic recovery |
| FileOpen | ✅ | ✅ | `Client.FileOpen` | ✅ | ✅ | ✅ | ❌ | ❌ | FRSM state machine; returns frsmId, size, lastModified |
| FileRead | ✅ | ✅ | `Client.FileRead`, `FileReadAll` | ✅ | ✅ | ✅ | ❌ | ❌ | Chunked read with `MoreFollows`; `FileReadAll` convenience |
| FileClose | ✅ | ✅ | `Client.FileClose` | ✅ | ✅ | ✅ | ❌ | ❌ | Releases FRSM handle; auto-close on disconnect |
| FileDirectory | ✅ | ✅ | `Client.FileDirectory`, `FileDirectoryAll` | ✅ | ✅ | ✅ | ❌ | ❌ | Pagination with `ContinueAfter`; `FileDirectoryAll` convenience with stall detection |
| FileDelete | ✅ | ✅ | `Client.FileDelete` | ✅ | ✅ | ✅ | ❌ | ❌ | Maps `fs.ErrNotExist` → file-non-existent |
| FileRename | ✅ | ✅ | `Client.FileRename` | ✅ | ✅ | ✅ | ❌ | ❌ | Collision tests included |
| ObtainFile | ✅ | ✅ | `Client.ObtainFile` | ✅ | ✅ | ✅ | ❌ | ❌ | Two-party file transfer |
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
| UTCTime          | ✅ | ✅ | ✅ | ✅ | `0x91` [17] | MMS UTC time wire format |

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

1. ~~No golden-file corpus~~ — **Resolved.** 33 golden hex fixtures in `testdata/golden/` covering all PDU services and codec envelopes. Interop tests additionally use inline byte sequences for C-compatible validation.

2. **Abort PDU (server→client)** — The server detects client disconnects and handles ISO Release, but does not proactively send an MMS Abort PDU to the client. Client-initiated Abort is fully implemented.

3. **Association-scope object listing** — Association-scoped variables and NVLs can be stored and accessed, but `GetNameList` for association scope returns `service-not-supported`. Listing and lifecycle management for association-scope objects is deferred.

4. **DeleteNamedVariableList scope** — Only `scopeOfDelete=0` (specific list names) is supported. Scope-based bulk deletion (e.g., delete all domain NVLs) is not implemented.

5. ~~No fuzz coverage for file/journal PDUs~~ — **Resolved.** Fuzz targets now cover file service decoders (FileOpenRequest, FileOpenResponse, FileReadResponse, FileDirectoryRequest, FileDirectoryResponse), journal service decoders (ReadJournalRequest, ReadJournalResponse), server request decoders, ACSE/Session/Presentation parsers, and data type decoders (GeneralizedTime, BCD, ObjectIdentifier).

6. **No live-wire interop testing** — The interop tests validate encoding compatibility via inline byte patterns. There is no automated test harness that connects to a real MMS server or C reference implementation.

7. **Semaphore, Event, Program Invocation services** — These MMS services (ISO 9506 Part 2) are not implemented. The `ObjectClass` enum includes them for `GetNameList` classification, but no client methods or server handlers exist.

8. **Unsolicited Status** — The MMS UnsolicitedStatus service is not implemented. Only request/response Status is supported.

9. **Cancel service** — The MMS Cancel service (to cancel an in-flight confirmed request) is not implemented.

10. **Segmented file transfer** — `ObtainFile` delegates to the `FileProvider` as a single synchronous operation. The MMS-specified segmented transfer protocol (where the server calls back to the client's file services) is not implemented.
