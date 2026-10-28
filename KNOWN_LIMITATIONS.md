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
| Cancel | Rarely used; cancelling in-flight confirmed requests is not supported |
| UnsolicitedStatus (client side) | The server sends it (`ServerConn.SendUnsolicitedStatus`); the client has no callback, logs a decode warning and drops the PDU |
| ScatteredAccess | Not needed for IEC 61850; no known real-world usage |
| OperatorStation services | Not needed for IEC 61850 |

## Protocol Limitations

### BER Encoding

| Limitation | Details |
|-----------|---------|
| Extended BER tags | Only single-byte tags (0–30) supported. Extended multi-byte tags are not used by MMS and are rejected; an MMS PDU or service tag in long form is rejected rather than truncated to a short tag. |
| Indefinite BER length | Not supported. Only definite-form lengths are accepted. Indefinite-length encodings are rejected. |
| BER length field | Maximum 3 bytes (content up to ~16 MB). Longer lengths are rejected. |
| INTEGER size | Signed integers limited to 4 bytes (32-bit). Larger BER INTEGERs are rejected. |
| Unsigned INTEGER size | Unsigned integers limited to 4 bytes (32-bit, with optional leading zero pad). Larger values are rejected. |

### Data Values and Type Descriptions

| Limitation | Details |
|-----------|---------|
| BinaryTime range | The 6-byte form counts days since 1984-01-01 in 16 bits, so `NewBinaryTime` values must lie between 1984-01-01 and 2163-06-05. Values 0–86399999 (below one day) are sent in the 4-octet TimeOfDay form (milliseconds since midnight, no date), which is also what a received 4-octet value decodes to. Other values fail to encode instead of wrapping. A received milliseconds-of-day field ≥ 86400000 is rejected. The same range applies to journal times (start, stop, after, occurrence). |
| Real and BooleanArray type descriptions | ISO 9506-2 `TypeDescription` has no alternative for these two data kinds. A server cannot register a variable whose type is `TypeReal` or `TypeBooleanArray`; declare the variable as floating-point or bit-string instead. |
| Object identifier arcs | `NewObjectIdentifier` requires a first arc of 0, 1 or 2 and, unless the first arc is 2, a second arc below 40 (X.690 §8.19.4). |

### Nesting and Collection Bounds

| Limitation | Value | Details |
|-----------|:-----:|---------|
| Data nesting depth | 64 levels | Recursive array/structure decoding in `DataValue` trees |
| TypeSpec nesting depth | 32 levels | Recursive type specification decoding |
| Access results per response | 65,536 | Read/write response element cap |
| File directory entries | 10,000 | Per-response cap for file listings |
| Journal entries per response | 10,000 | Per-response cap for journal reads |
| GetNameList identifiers | 100,000 | Per-response cap for name lists |
| File name length | 1,024 bytes | Decoded file name string cap |
| Identifier name length | 1,024 bytes | Decoded identifier string cap |

### PDU Size

The negotiated `maxPDUSize` is the minimum of what each side proposes during
association. Before negotiation (during the initial handshake), no size limit
is enforced. After negotiation, the client rejects oversized PDUs and the
server logs a warning and skips them.

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
| Write cancellation | Request writes are detached from the request context: the caller returns when its context ends, an unstarted write is skipped, and a write in progress completes in the background (a half-written PDU would corrupt the stream). In `transport/iso` the context deadline still bounds the write. See [TIMEOUT_AND_CLOSE.md](TIMEOUT_AND_CLOSE.md). |
| ListenAndServe cancellation | Cancelling the `ListenAndServe` context closes every association's transport without a protocol-level abort. Use `Server.Shutdown` to let in-flight requests finish and send ACSE ABRT. |
| No automatic reconnect | A lost association is reported through `Client.Done`/`Client.Err`; the caller reconnects (e.g. with `mms.Retry`). Server-side state (association-scoped objects, open files) is not restored. |
| Client write serialization | Transport writes are serialized via a send mutex so PDUs do not interleave. Concurrent callers may still have multiple confirmed requests outstanding (wait for response happens outside the write lock); responses are correlated by invoke ID. |
| Negotiated outstanding not enforced | `MaxOutstandingCalling` / `MaxOutstandingCalled` are negotiated and exposed via `Client.Negotiated()` / server association state, but the client invoke tracker is not capped by those values today. Limit concurrency in the caller if needed. |
| Server request handling | Confirmed requests within a single server connection are handled serially. Concurrent request handling per-connection is not supported. |
| Server Abort PDU | The server does not send an MMS Abort PDU on its own. It detects client disconnects and handles ISO Release/Conclude; applications can abort a connection explicitly with `ServerConn.Abort`, which sends ACSE ABRT and closes the connection. |
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
| Single peer implementation | Live-wire interop (`interop/`, see [INTEROP.md](INTEROP.md)) runs against libiec61850 only. Behaviour where libiec61850 deviates (listOfVariable reports, FileOpen initial position, bulk NVL delete, ObtainFile) is pinned by tests but not cross-checked against a second implementation. |
| Cancel and UnsolicitedStatus not interop-tested | The go-mms client sends no Cancel (the server side is unit-tested), and the libiec61850 client has no UnsolicitedStatus handler (the server side is covered by `e2e`). |

## Not in Scope

These are deliberate design boundaries, not implementation gaps.

- **IEC 61850 abstractions** — Data model, SCL parsing, GOOSE, SV, and IEC 61850 naming conventions belong in a separate `go-iec61850` package built on top of go-mms.
- **ASN.1 code generation** — PDU encoding/decoding is hand-rolled for control and performance. No ASN.1 schema compiler or code generator is used or provided.
- **Protocol performance optimization** — No zero-copy I/O, buffer pooling, or arena allocation. The library prioritizes correctness and clarity over raw throughput.
- **Backwards compatibility with non-MMS variants** — The library targets ISO 9506 MMS as used in IEC 61850 environments. Non-standard MMS extensions or vendor-specific protocol variants are not accommodated.
- **Runtime enforcement of negotiated outstanding limits** — Initiate proposes and negotiates max outstanding calling/called; those values are stored and logged but not used to reject excess in-flight client requests. See Behavioral Limitations above and [LIMITS.md](LIMITS.md).
