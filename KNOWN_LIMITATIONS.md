# Known Limitations

This document lists intentional boundaries and known gaps in go-mms.
It is intended to be read alongside [COMPLIANCE.md](COMPLIANCE.md) (service
support matrix) and [LIMITS.md](LIMITS.md) (defensive resource bounds).

## Unimplemented MMS Services

These MMS services defined in ISO 9506 are not implemented.
The `ObjectClass` enum includes some of them for `GetNameList` classification,
but no client methods, server handlers, or PDU encoders/decoders exist.

| Service | Reason |
|---------|--------|
| Semaphore services (TakeControl, RelinquishControl, ReportSemaphoreStatus, etc.) | Not needed for IEC 61850; rarely used in modern MMS deployments |
| Event Management (DefineEventCondition, EventNotification, etc.) | Not needed for IEC 61850 |
| Program Invocation (CreateProgramInvocation, Start, Stop, Resume, etc.) | Not needed for IEC 61850 |
| Cancel (client side) | The go-mms client never sends Cancel. The server answers a Cancel-RequestPDU with Cancel-ErrorPDU (invoke-id-unknown), because it serves confirmed requests one at a time; see COMPLIANCE.md |
| UnsolicitedStatus (client side) | The server sends it (`ServerConn.SendUnsolicitedStatus`); the client has no callback, logs a decode warning and drops the PDU |
| ScatteredAccess | Not needed for IEC 61850; no known real-world usage |
| OperatorStation services | Not needed for IEC 61850 |

## Protocol Limitations

### BER Encoding

| Limitation | Details |
|-----------|---------|
| Received BER vs sent encoding | The receive path accepts BER as X.690 allows it: non-minimal long-form lengths (§8.1.3.5 NOTE 2), the indefinite length form on constructed encodings (§8.1.3.6, at most 64 levels deep) and non-minimal INTEGER contents. The PDU is rewritten to minimal definite lengths before the service decoders run. go-mms always sends minimal definite lengths. |
| Multi-octet BER tags | MMS uses multi-octet service tags (for example ObtainFile [46], ReadJournal [65], InitializeJournal [67], the file services [72]–[77]); the PDU and service-choice layers decode and encode them (X.690 §8.1.2.4). The tags inside service bodies are all below 31 and the hand-rolled inner decoders accept only the single-octet form. |
| BER length field (inner decoders) | After the rewrite above, inner TLVs take lengths of up to 3 octets (content up to ~16 MB), far above any negotiated PDU size. |
| INTEGER size | `berutil.DecodeInteger` / `DecodeUnsigned` (sizes, counts, invoke IDs) take up to 4 octets after removing redundant leading octets. Data INTEGER and Unsigned values take up to 8 octets (9 with the sign pad). |
| Unsigned32 | MMS Unsigned32 is 0..2^31−1 (ISO 9506-2 clause 7.5.2). Invoke IDs above 2^31−1 are rejected, and the client's invoke ID counter wraps after 2^31−1. |

### Data Values and Type Descriptions

| Limitation | Details |
|-----------|---------|
| BinaryTime range | TimeOfDay (ISO 9506-2:2003 §7.5.1) has a 28-bit millisecond field and, in the 6-byte form, 16-bit days since 1984-01-01. `NewBinaryTime` values 0 to 2^28−1 are sent in the 4-octet form (milliseconds since midnight, no date, possibly more than one day), which is also what a received 4-octet value decodes to. Instants from 1984-01-01 through 2163-06-05 go in the 6-octet form with the field below one day, and up to 2^28−1 ms after the start of 2163-06-05 on that last day with the remainder in the field. Other values fail to encode instead of wrapping. A received field of one day or more is accepted in both forms (the 6-octet form carries it into the following days); a field with bits 28–31 set is rejected. Journal times are always sent in the 6-octet form (see EntryTime below). |
| Real and BooleanArray type descriptions | ISO 9506-2 `TypeDescription` has no alternative for these two data kinds. A server cannot register a variable whose type is `TypeReal` or `TypeBooleanArray`; declare the variable as floating-point or bit-string instead. |
| Object identifier arcs | `NewObjectIdentifier` requires a first arc of 0, 1 or 2 and, unless the first arc is 2, a second arc below 40 (X.690 §8.19.4). |
| Real `[8]` is an extension | ISO 9506-2:2003 clause 14.4.2 reserves Data `[8]`. go-mms encodes `NewReal` values there as an ASN.1 REAL, as earlier MMS editions did, and decodes it. Conformant IEC 61850 peers reject it: use `NewFloat32` / `NewFloat64` (floating-point `[7]`). |
| Floating-point width | `NewFloat` and `NewFloat32` produce a single-precision value (format width 32, exponent width 8), `NewFloat64` a double. A server encodes a Read result with the width its variable's TypeSpec declares. A decoded value keeps its width. |
| Identifier constraint | Names the client sends and names a server registers must have 1..64 characters (`MaxIdentifierLength`, IEC 61850-8-1 Annex F.3.2). The character set A–Z a–z 0–9 `$` `_` of ISO 9506-2 clause 7.5.2 is not enforced, because peers serve names outside it (see INTEROP.md); `ValidateIdentifier` is the strict check. Names received from a peer are not checked (up to 1,024 octets). |
| FileName | A received FileName may have several GraphicString elements (ISO 9506-2 clause 7.5.7); they are joined with `/`. A FileName has at most 255 octets (IEC 61850-8-1 clause 23.1); go-mms sends one element. |
| GeneralizedTime | All X.680 clause 46 forms are accepted; local time (no `Z` or offset) is read as UTC. go-mms sends UTC with `Z`. |
| EntryTime | Journal times (start, stop, after, occurrence) are always sent as a 6-octet TimeOfDay (IEC 61850-8-1 clause 8.1.3.8); times before 1984-01-01 are sent as 1984-01-01. |
| AlternateAccess | Only a chain of single selections (component, index, index range, each optionally with one nested selection) is supported. Several selections side by side in one AlternateAccess, the named `[5]` form and allElements get a ServiceError access `object-access-unsupported` from the server; the client cannot decode them (a decode error). The nested form is sent as an untagged SEQUENCE OF (0x30, ISO 9506-2 clause 14.3.1, as libiec61850 does); the earlier `[5]` (0xa5) form is still accepted. |
| Named type in GetVariableAccessAttributes | The response carries a TypeDescription, which has no typeName (ISO 9506-2 clause 14.9.2). A server variable registered with `ValueTypeNamedType` is answered with access `object-access-unsupported`. |
### Nesting and Collection Bounds

| Limitation | Value | Details |
|-----------|:-----:|---------|
| Data nesting depth | 64 levels | Recursive array/structure decoding in `DataValue` trees |
| TypeSpec nesting depth | 32 levels | Recursive type specification decoding |
| Access results per response | 65,536 | Read/write response element cap |
| File directory entries | 10,000 | Per-response cap for file listings |
| Journal entries per response | 10,000 | Per-response cap for journal reads |
| GetNameList identifiers | 100,000 | Per-response cap for name lists |
| File name length | 255 bytes | Decoded and sent FileName (IEC 61850-8-1 clause 23.1) |
| Identifier name length | 1,024 bytes received, 64 sent | Received identifier cap; sent and registered names follow the Identifier length above |

### PDU Size

Each side's localDetail is the largest PDU it accepts (ISO 9506-2 §8.2). The
client refuses to send a request larger than the server's localDetailCalled
(`ErrInvalidArgument`, nothing sent) and treats a received PDU larger than its
own localDetailCalling as invalid; `Client.Negotiated().MaxPDUSize` is the
smaller of the two. The server answers min(own, proposed), or its own value
when the client omits localDetailCalling. Before negotiation (during the
initial handshake), no size limit is enforced. After negotiation, the server
answers an oversized PDU with a Reject (confirmed-requestPDU
value-out-of-range for a confirmed request whose invoke ID can be read,
otherwise pdu-error invalid-pdu) and the association continues; see
[LIMITS.md](LIMITS.md). The server refuses a localDetailCalling below 64
octets with Initiate-ErrorPDU max-segment-insufficient.

## Transport Limitations

| Limitation | Details |
|-----------|---------|
| COTP class | Only COTP class 0 (ISO 8073) is supported via the `transport/iso` subpackage. Higher classes (1–4) with error recovery, multiplexing, or expedited data are not implemented. |
| Transport protocol | TCP only (RFC 1006 TPKT framing). No native OSI transport, UDP, or serial support. |
| TLS | Supported via `iso.WithTLSConfig`, but DTLS and other non-TCP-TLS variants are not supported. |
| Custom transports | The `Transport` interface allows arbitrary implementations, but the library does not provide any beyond `transport/iso`. |

## Behavioral Limitations

| Limitation | Details |
|-----------|---------|
| Write cancellation | Request writes are detached from the cancellation of the request context, not from its deadline: the caller returns when its context ends, an unstarted write is skipped, and a write in progress continues in the background (a half-written PDU would corrupt the stream) until it completes or the request's deadline passes, which ends the association. A request without a deadline (none on the context, `RequestTimeout` zero) has an unbounded write. See [TIMEOUT_AND_CLOSE.md](TIMEOUT_AND_CLOSE.md). |
| ListenAndServe cancellation | Cancelling the `ListenAndServe` context closes every association's transport without a protocol-level abort. Use `Server.Shutdown` to let in-flight requests finish and send ACSE ABRT. |
| No automatic reconnect | A lost association is reported through `Client.Done`/`Client.Err`; the caller reconnects (e.g. with `mms.Retry`). Server-side state (association-scoped objects, open files) is not restored. |
| Client write serialization | Transport writes are serialized via a send mutex so PDUs do not interleave. Concurrent callers may have up to the negotiated max-services-outstanding-calling confirmed requests outstanding (wait for response happens outside the write lock); responses are correlated by invoke ID. Further callers wait for a slot (see [LIMITS.md](LIMITS.md)). |
| Abandoned requests keep their slot | A request whose caller gave up after it was sent stays outstanding until the server answers it. A server that never answers keeps that slot taken until the association ends; the client sends no Cancel to free it. |
| Negotiated nesting level 0 | A negotiated dataStructureNestingLevel of 0 (only simple data) is not enforced by the client or the server. |
| Server reaction to excess outstanding requests | The server serves confirmed requests one at a time, so it never sees more than one outstanding and never sends Reject confirmed-requestPDU max-serv-outstanding-exceeded (6): requests a client pipelines beyond the negotiated value are answered in order. |
| Server request handling | Confirmed requests within a single server connection are handled serially. Concurrent request handling per-connection is not supported. |
| Server Abort PDU | The server sends an abort on its own only for a FINISH before Conclude, or a PDU other than FINISH + RLRQ after the Conclude-Response (ISO 9506-2 Annex A.1.2.2). Applications can abort a connection explicitly with `ServerConn.Abort`, which sends Session ABORT + ARU + ACSE ABRT and closes the connection. |
| Parameter CBB insufficiency | The server answers the proposed parameter CBB AND its own (str1, str2, vnam, valt, vlis) and never refuses with Initiate-Error parameter-CBB-insufficient (6) or service-CBB-insufficient (5): a client that does not propose a CBB the server relies on gets errors from the services that need it. |
| Initiate nesting level | When the client omits proposedDataStructureNestingLevel the server uses its own value and omits negotiatedDataStructureNestingLevel from the response (both are OPTIONAL in ISO 9506-2 §8.2). |
| Client servicesSupportedCalling | The client still proposes libiec61850's servicesSupportedCalling bit string, which names services the client does not serve as a responder (MMS-PDU-26). Servers use it only informationally. |
| Session protocol | Version 2 and version 1 are negotiated; only the kernel and duplex functional units are used. A CONNECT without duplex is refused (reason 134). Extended User Data (PGI 194) is sent for CONNECT user data above 512 octets (ISO 8327-1); libiec61850 does not parse it, but an MMS AARQ is far below 512 octets. |
| Presentation contexts | Any context ids the client proposes for ACSE and MMS (BER transfer syntax) are accepted and used; other proposed contexts are rejected per context. Default-context and multiple PDV-list fully-encoded data are not supported. |
| RawHook availability | Raw wire hooks (`RawHook`) are only available on the client side via `DialOptions`. The server does not expose a raw hook for wire-level inspection. |

## Named Variable List Limitations

| Limitation | Details |
|-----------|---------|
| DeleteNamedVariableList scope | The server handles all four `scopeOfDelete` values (0 specific, 1 aa-specific, 2 domain, 3 VMD). The client exposes scopes 0, 2 and 3 (`DeleteNamedVariableList`, `DeleteAllDomainNVLs`, `DeleteAllVMDNVLs`); there is no client helper for aa-specific (scope 1) deletion. |
| Association-scope objects | Association-scoped variables and NVLs are stored per connection and discarded when it closes. `GetNameList` for association scope lists the calling connection's own objects. |

## File and Journal Service Limitations

| Limitation | Details |
|-----------|---------|
| ObtainFile | Delegates to the `FileProvider` as a single synchronous operation. The MMS-specified segmented transfer protocol (where the server calls back to the client's file services) is not implemented on either side: the client does not answer FileOpen/FileRead from the server, so `Client.ObtainFile` against servers that use role reversal (libiec61850) fails with a File-class `ServiceError`. |
| Journal domain | `ReadJournal` and journal `GetNameList` are passed to the `JournalProvider` without checking the domain against registered domains; the provider decides what exists. |
| ReadJournal options | The server decodes every ReadJournal-Request option of IEC 61850-8-1 Table 124 (any combination of range start, range stop, listOfVariables and entryToStartAfter). A `JournalProvider` that implements `JournalQueryProvider` receives the full `JournalQuery`; otherwise the query is mapped onto `ReadTimeRange` / `ReadStartAfter` and the stop time, entry count and variable list are applied by the server, and a startingEntry without entryToStartAfter is answered with access `object-access-unsupported`. The client sends only the time-range and start-after forms. |
| InitializeJournal | Served only when the `JournalProvider` also implements `JournalInitializer`; otherwise Reject unrecognized-service. The client has no InitializeJournal method. |
| GetDomainAttributes | The server answers a registered domain as a static one (no capabilities, state ready, not deletable, sharable, no program invocations, no upload in progress); the ready(2) value is the ISO 9506-1 clause 11 DomainState, which is not part of the reviewed specification text. The client has no GetDomainAttributes method. |
| Journal annotation entries | Decoded and encoded (`JournalEntry.HasAnnotation` / `Annotation`, ISO 9506-2 clause 23.8.1); the event part of the data entryForm is skipped. |
| File size metadata | `uint32` maximum (~4 GB). Files larger than 4 GB cannot have their size accurately represented in the MMS protocol layer. |
| No server-initiated file transfer | The server cannot initiate an ObtainFile to pull files from the client. Only client→server ObtainFile is supported. |

## Observability Limitations

| Limitation | Details |
|-----------|---------|
| No built-in exporters | The `Metrics` interface reports request and connection events, but go-mms ships no Prometheus or OpenTelemetry adapter (see [OBSERVABILITY.md](OBSERVABILITY.md) for an example) and creates no tracing spans. |
| Metrics coverage | Only confirmed requests and association state changes are reported; unconfirmed PDUs (InformationReport, UnsolicitedStatus), Cancel and byte counts are not. |
| Server raw hook | Not available. Only the client exposes a `RawHook` for wire-level inspection. |

## Testing Gaps

These are known gaps in test coverage, not behavioral limitations. They are
listed here for transparency.

| Gap | Details |
|-----|---------|
| Deviations of the reference peers | Live-wire interop (`interop/`, see [INTEROP.md](INTEROP.md)) runs against libiec61850, dscsystems and iec61850-rust, and iec61850bean as an IED. Where these peers share a deviation (listOfVariable reports, FileOpen initial position, bulk NVL delete, ObtainFile role reversal) the go-mms side of the scenario is pinned by tests; which peer shows which deviation is in INTEROP.md. |
| Cancel and UnsolicitedStatus not interop-tested | The go-mms client sends no Cancel (the server side is unit-tested), and the libiec61850 client has no UnsolicitedStatus handler (the server side is covered by `e2e`). |

## Not in Scope

These are deliberate design boundaries, not implementation gaps.

- **IEC 61850 abstractions** — Data model, SCL parsing, GOOSE, SV, and IEC 61850 naming conventions belong in a separate `go-iec61850` package built on top of go-mms.
- **ASN.1 code generation** — PDU encoding/decoding is hand-rolled for control and performance. No ASN.1 schema compiler or code generator is used or provided.
- **Protocol performance optimization** — No zero-copy I/O, buffer pooling, or arena allocation. The library prioritizes correctness and clarity over raw throughput.
- **Backwards compatibility with non-MMS variants** — The library targets ISO 9506 MMS as used in IEC 61850 environments. Non-standard MMS extensions or vendor-specific protocol variants are not accommodated.
