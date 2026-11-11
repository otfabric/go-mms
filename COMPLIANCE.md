# MMS Service Compliance Matrix

> Auto-generated compliance audit of `go-mms` against ISO 9506 (MMS) services.
> Each cell is verified against the source code and test suites.

## Service Support

| Service | Client | Server | Public API | Encode Test | Decode Test | Negative Tests | Fuzzed | Interop vs C | Notes |
|---------|:------:|:------:|:----------:|:-----------:|:-----------:|:--------------:|:------:|:------------:|-------|
| Initiate | ✅ | ✅ | `NewClient`, `Server.Serve` | ✅ | ✅ | ✅ | ✅ | ✅ | Full handshake with parameter negotiation (ISO 9506-2 §8.2, IEC 61850-8-1 Tables 109–113): the server advertises its own servicesSupportedCalled and the parameter CBB proposed AND str1/str2/vnam/valt/vlis, never exceeds a proposal, takes its own value for an absent localDetailCalling/nesting level, and refuses version < 1 or a zero max-services-outstanding with an Initiate-ErrorPDU; the client fails with `ErrNegotiationFailed` when the response exceeds its proposal and exposes the server's bit strings in `Client.Negotiated`. The client keeps at most the negotiated max-services-outstanding-calling confirmed requests outstanding (further requests wait, or fail with `ErrMaxOutstanding` under `MMSOptions.FailFastOutstanding`) and enforces the negotiated dataStructureNestingLevel on Data, AlternateAccess and TypeSpecifications it sends and receives. MMS version 1 (see INTEROP.md); `MarshalInitiateRequest`/`UnmarshalInitiateResponse` round-trip tested |
| Conclude | ✅ | ✅ | `Client.Close` | ✅ | ✅ | ✅ | ❌ | ✅ | Graceful close with `ConcludeRequest`/`ConcludeResponse` followed by the A-RELEASE (FINISH + RLRQ, DISCONNECT + RLRE; ISO 9506-2 Annex A.1.2) on both sides, bounded by `ReleaseTimeout`; a Conclude-Error makes the client abort (§24.3) and `Close` return the `*ServiceError`; the Conclude-ErrorPDU is `[13]` 0xad (ISO 9506-2 clause 8.4.3); the server aborts a FINISH before Conclude. Interop passes against the four pinned peers (INTEROP.md) |
| Abort | ✅ | ✅ | `Client.Abort`, `ServerConn.Abort` | ✅ | ✅ | ✅ | ✅ | ✅ | Client and server both send Session ABORT + ARU-PPDU + ACSE ABRT (ISO 8823-1); a received ABRT/ARP is decoded for the log (`DescribeAbort`); server `SendAbort` is best-effort before transport close |
| Identify | ✅ | ✅ | `Client.Identify`, `Server.HandleIdentify` | ✅ | ✅ | ✅ | ❌ | ✅ | Full request/response; server returns `ServiceError` when no handler registered |
| Status | ✅ | ✅ | `Client.Status`, `Client.StatusWithOptions`, `Server.HandleStatus` | ✅ | ✅ | ✅ | ❌ | ✅ | Supports `ExtendedDerivation` flag; VMD logical/physical status |
| UnsolicitedStatus | N/A | ✅ | `ServerConn.SendUnsolicitedStatus` | ✅ | N/A | ✅ | ❌ | ❌ | Server-originated unconfirmed PDU; follows InformationReport pattern |
| Cancel | N/A | ✅ | Handled in `serverconn.Conn` dispatch | ✅ | ✅ | ✅ | ❌ | ❌ | Server responds with CancelError (invoke-id-unknown) since requests are synchronous. Tags per ISO 9506-2: cancel-RequestPDU [5], cancel-ResponsePDU [6], cancel-ErrorPDU [7] (serviceError [1]); earlier releases used [6]/[7]/[8] |
| GetNameList | ✅ | ✅ | `Client.GetNameList`, `Client.GetNameListAll` | ✅ | ✅ | ✅ | ✅ | ✅ | All scopes (VMD/Domain/Association); pagination with `ContinueAfter`; stall detection; supports Domain, NamedVariable, NVL, Journal object classes; association-scope variable and NVL listing via per-connection storage |
| GetVariableAccessAttributes | ✅ | ✅ | `Client.GetVariableAccessAttributes` | ✅ | ✅ | ✅ | ✅ | ✅ | Full TypeSpec decode including Boolean, Integer, Unsigned, Float, BitString, OctetString, VisibleString, MmsString, UTCTime, BinaryTime, Array, Structure, NamedType, GeneralizedTime [11], BCD [13], ObjectIdentifier [15], with the class and constructed bit of each alternative checked; optional accessControlList [3] / meaning [4] in the response are skipped; the server encodes the same set (Real and BooleanArray have no TypeDescription in ISO 9506-2) but no top-level typeName (clause 14.9.2: access `object-access-unsupported`) |
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
| ReadJournal | ✅ | ✅ | `Client.ReadJournalTimeRange`, `ReadJournalStartAfter` | ✅ | ✅ | ✅ | ✅ | ✅ | Client: time-range and start-after cursors; pagination with `MoreFollows`; data and annotation entries. Server: every request option of IEC 61850-8-1 Table 124 (`JournalProvider`, or `JournalQueryProvider` for the full query); EntryTime always 6 octets |
| InitializeJournal | ❌ | ✅ | `JournalInitializer` | ✅ | ✅ | ✅ | ✅ | ❌ | ISO 9506-2 clause 23.4; served when the `JournalProvider` implements `JournalInitializer`, otherwise Reject unrecognized-service |
| GetDomainAttributes | ❌ | ✅ | `Server.RegisterDomain` | ✅ | ✅ | ✅ | ✅ | ❌ | ISO 9506-2 clause 11.13; a registered domain is reported as static (state ready, no capabilities or program invocations) |
| Reject PDU | ✅ | ✅ | Handled in `Client.processConfirmedPDU` | ✅ | ✅ | ✅ | ✅ | ❌ | Client decodes into `*RejectError` (`Type`, `Reason`, invoke ID), which unwraps to `*ProtocolError` (phase `mms`); a Reject without originalInvokeID fails the only outstanding request, or every outstanding request when there are several. Client answers a malformed Confirmed-Response or Confirmed-Error whose invoke ID can be read with a Reject (confirmed-responsePDU / confirmed-errorPDU, reason other) and fails that request, instead of dropping the association. Client answers a Confirmed-Response whose Data or TypeSpecification nests deeper than the negotiated level with Reject confirmed-responsePDU max-recursion-exceeded, and such an InformationReport with Reject unconfirmedPDU max-recursion-exceeded. Server sends Reject (ISO 9506-2 clause 8.6, IEC 61850-8-1 clause 8.1.3.4): unrecognized-service for a service it does not implement, invalid-argument for a malformed service body, invalid-invokeID for an invoke ID outside 0..2^31−1, value-out-of-range for a request above the negotiated PDU size, max-recursion-exceeded for data nested deeper than negotiated, pdu-error unknown-pdu-type / invalid-pdu otherwise; the association stays up |
| ConfirmedError | ✅ | ✅ | `ServiceError`, `ErrorClass`, `ErrorCode` | ✅ | ✅ | ✅ | ✅ | ✅ | Client decodes the whole ServiceError (ISO 9506-2 clause 7.4.1): errorClass 0..12, additionalCode, additionalDescription, serviceSpecificInfo and the Confirmed-ErrorPDU modifierPosition, into `*ServiceError` (`CodeName()` gives the ISO 9506-2 code name); server generates via `serverconn.ServiceError`, and passes through a `*ServiceError` returned by Identify/Status handlers; a response that does not fit the negotiated PDU size gets service/pdu-size (IEC 61850-8-1 Table 17); trailing-byte strictness |
| DownloadFile | ✅ | ✅ | `Client.DownloadFile` | ✅ | ✅ | ✅ | ❌ | ✅ | Convenience: Open → ReadAll → Close |

## Protocol Layer Support

| Layer | Status | Notes |
|-------|--------|-------|
| BER/TLV encoding | ✅ Implemented | `internal/berutil` — hand-rolled TLV encoder/decoder; supports short/long-form lengths, signed/unsigned integers, float32/float64; receive side accepts BER (non-minimal lengths, indefinite length on constructed encodings, multi-octet tags, non-minimal INTEGERs) and rewrites it to minimal definite lengths; always sends minimal definite lengths; 10 fuzz targets |
| MMS PDU marshaling | ✅ Implemented | `internal/pdu` — covering all 17+ service types; tag-driven dispatch; confirmed request/response/error/reject envelope; multi-octet service tags (X.690 §8.1.2.4) at the PDU and service-choice layers |
| ACSE (association) | ✅ Implemented | `internal/acse` — AARQ/AARE/RLRQ/RLRE/ABRT encoding/decoding (ISO 8650-1); AARQ protocol-version and application-context validation; AARE result-source-diagnostic, responding AP-title/AE-qualifier and responder-acse-requirements; password authentication; RLRQ/ABRT parsed as extensible SEQUENCEs; EXTERNAL single-ASN1-type and octet-aligned |
| Presentation layer | ✅ Implemented | `internal/presentation` — ISO 8823-1 CP/CPA/CPR/ARU/ARP; per-context negotiation by abstract and transfer syntax with the result list in proposal order; data on the negotiated context ids; PDV single-ASN1-type and octet-aligned |
| Session layer | ✅ Implemented | `internal/session` — ISO 8327-1 CONNECT/ACCEPT/REFUSE/DATA/FINISH/DISCONNECT/ABORT/ABORT-ACCEPT SPDUs; version 1/2 and duplex negotiation; REFUSE reason codes; Extended User Data (PGI 194) above 512 octets; selectors limited to 16 octets |
| ISO transport (TPKT/COTP) | ✅ Implemented | `transport/iso` — RFC 1006 TPKT framing; ISO 8073 COTP class 0; TLS support; concurrent accept-side handshakes with timeout; dial retry; per-direction deadlines for full-duplex use |

## Data Type Support

| MMS Data Variant | Encode | Decode | Round-trip Test | Fuzzed | Tag | Notes |
|------------------|:------:|:------:|:---------------:|:------:|:-------------------:|-------|
| DataAccessError  | ✅ | ✅ | ✅ | ✅ | `0x80` [0]  | Per-variable error in Read/Write results |
| Array            | ✅ | ✅ | ✅ | ✅ | `0xa1` [1]  | Recursive/nested decode |
| Structure        | ✅ | ✅ | ✅ | ✅ | `0xa2` [2]  | Recursive/nested decode |
| Boolean          | ✅ | ✅ | ✅ | ✅ | `0x83` [3]  | |
| BitString        | ✅ | ✅ | ✅ | ✅ | `0x84` [4]  | BIT STRING with unused-bits prefix; the bit length must match the octets (X.690 §8.6.2) and unused bits are sent as zero |
| Integer          | ✅ | ✅ | ✅ | ✅ | `0x85` [5]  | Signed BER integer |
| Unsigned         | ✅ | ✅ | ✅ | ✅ | `0x86` [6]  | Unsigned BER integer |
| FloatingPoint    | ✅ | ✅ | ✅ | ✅ | `0x87` [7]  | MMS FloatingPoint, not ASN.1 REAL. The width follows the value's declared type (`NewFloat32`/`NewFloat`: format width 32, `NewFloat64`: 64; a server Read result uses its TypeSpec), not the value. `Value.Equal` compares Float/Real bit for bit, with all NaNs equal (+0 ≠ −0) |
| Real             | ✅ | ✅ | ✅ | ✅ | `0x88` [8]  | ASN.1 REAL. Non-standard: ISO 9506-2:2003 clause 14.4.2 reserves `[8]`; kept for earlier MMS editions, see KNOWN_LIMITATIONS.md |
| OctetString      | ✅ | ✅ | ✅ | ✅ | `0x89` [9]  | Raw bytes |
| VisibleString    | ✅ | ✅ | ✅ | ✅ | `0x8a` [10] | VisibleString |
| GeneralizedTime  | ✅ | ✅ | ✅ | ✅ | `0x8b` [11] | ASN.1 GeneralizedTime; every X.680 clause 46 form decodes (local time read as UTC), UTC with `Z` is sent |
| BinaryTime       | ✅ | ✅ | ✅ | ✅ | `0x8c` [12] | MMS TimeOfDay (ISO 9506-2:2003 §7.5.1): a 28-bit millisecond field, bits 28–31 zero, and in the 6-octet form 16-bit days since 1984-01-01. The field may exceed one day in both forms; the 6-octet form carries it into the following days. 4-octet form for values 0–2^28−1, 6-octet form (field below one day) for 1984-01-01…2163-06-05, and the last day with the remainder in the field after that; a field with bits 28–31 set is rejected |
| BCD              | ✅ | ✅ | ✅ | ✅ | `0x8d` [13] | Binary-coded decimal; never negative (ISO 9506-2 clause 14.4.2) |
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
| Fuzz targets | 69 | `internal/pdu` (39), `internal/berutil` (10), `internal/isostack` (5), `internal/codec` (4), root package (4: `FuzzDecodeTSDU`, `FuzzValueCodec`, `FuzzServerDispatch`, `FuzzServerSession`), `internal/asn1util`, `internal/presentation`, `internal/session` (2 each), `internal/acse` (1). `make fuzz` runs all (discovered with `go test -list`) |
| Chaos / stress tests | 4 | `e2e/chaos_test.go` (`TestChaos_*`, `TestStress_Concurrent`) over `internal/chaos` fault-injecting connections (delays, chunked I/O, bandwidth limit, drops, resets, stalls); `make chaos` |
| Benchmarks | 21 | `internal/pdu/bench_test.go`, `value_bench_test.go`, `internal/apisurface` |
| End-to-end API tests | 64 | `e2e/` — every exported `Client`, `Server` and `ServerConn` method and every `Value` constructor between a go-mms client and server over ISO transport; `TestAPICoverage` fails when a public method has no test |
| Live interop tests | 120 | `interop/` (`-tags=interop`) — both directions against libiec61850 `v1.6.2.1-89-g6e265c54` via iec61850-interop; `TestInteropCoverage` guards the API and the server's service dispatch (see [INTEROP.md](INTEROP.md)) |
| Wire-vector tests | 11 | `internal/pdu/interop_test.go`, `internal/pdu/peer_vectors_test.go` — BER patterns as encoded by libiec61850 |
| Golden fixture tests | 33 | 27 in `internal/pdu/testdata/golden/` + 6 in `internal/codec/testdata/golden/`; regenerate with `-update-golden` flag |
| Coverage gate | ≥ 90% | `make coverage` (`scripts/coverage-gate.sh`) fails when any package or the total is below `COVER_PKG_MIN` (default 90); run by the CI `quality` job |

## Known Gaps

### Resolved

1. ~~No golden-file corpus~~ — **Resolved.** 33 golden hex fixtures in `testdata/golden/` covering all PDU services and codec envelopes.
2. ~~Abort PDU (server→client)~~ — **Resolved.** `ServerConn.Abort()` sends ACSE ABRT before transport close.
3. ~~Association-scope object listing~~ — **Resolved.** `GetNameList` for association-scope NVLs is now implemented with per-connection storage.
4. ~~DeleteNamedVariableList scope~~ — **Resolved.** All four `scopeOfDelete` values (0=specific, 1=aa-specific, 2=domain, 3=VMD) are supported by the server; the client exposes 0, 2 and 3 (no aa-specific helper). Goes beyond C reference which only supports `scopeOfDelete=0`.
5. ~~No fuzz coverage for file/journal PDUs~~ — **Resolved.** 69 fuzz targets across all service categories, the value codec and server dispatch.
6. ~~Unsolicited Status~~ — **Resolved.** `ServerConn.SendUnsolicitedStatus()` sends UnconfirmedPDU with VMD status.
7. ~~Cancel service~~ — **Resolved.** Server handles CancelRequestPDU and responds with CancelError (invoke-id-unknown, since requests are processed synchronously).
8. ~~No live-wire interop testing~~ — **Resolved.** `interop/` runs both directions against libiec61850 through the iec61850-interop adapter images; see [INTEROP.md](INTEROP.md).

### Remaining Gaps

| # | Gap | Priority | Impact | In C ref? | Description |
|---|-----|:--------:|:------:|:---------:|-------------|
| 1 | Semaphore, Event, Program Invocation services | Low | Completeness | Not in C either | ISO 9506-2 service groups with complex state machines. Zero implementations in the C reference (libIEC61850). Legacy MMS features rarely used in IEC 61850 deployments. Effort: very high (thousands of lines from standard alone). |
| 2 | Segmented file transfer (ObtainFile) | Medium | Feature | **Fully in C** | C implements a multi-step state machine (~400 lines) with role reversal (server sends confirmed requests to client). Go currently delegates ObtainFile to FileProvider as a single synchronous operation. Requires new server→client confirmed request plumbing. Effort: ~500-800 lines. |
