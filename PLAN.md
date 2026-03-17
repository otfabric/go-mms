# PLAN.md — otfabric/go-mms

A native Go implementation of the MMS (Manufacturing Message Specification) protocol — ISO 9506.

---

## 1. Scope and non-goals

### Scope

- A clean, Go-native MMS protocol library.
- **Client-side MMS** (connect, initiate, read, write, name list, identify, status) — implemented in Phases 0–6.
- **Server-side MMS** (accept associations, negotiate, serve confirmed services via a handler API) — planned in Phase 7.
- Generic MMS only — usable for any MMS application, not tied to a specific domain.
- Designed to compose with `otfabric/go-tpkt` and `otfabric/go-cotp` for transport.
- Strong named types, structured errors, observable behavior.

### Non-goals

- **IEC 61850 domain logic.** No logical devices, logical nodes, functional constraints, report control blocks, control models, datasets-as-IEC-concepts, SCL/ICD/CID/SCD parsing, or IED naming helpers. IEC 61850 belongs in a separate higher-level package built on top of `go-mms`.
- **GOOSE / Sampled Values / other IEC 61850 stacks.** Out of scope entirely.
- **Public APIs for session, presentation, or ACSE.** These layers are internal protocol plumbing. They may be exposed later only if external consumers prove they need them.
- **File services and journal services.** Planned for Phase 12 and 13 respectively, using provider-based abstractions.
- **Unconfirmed services (InformationReport).** Planned for Phase 10, including a client reader loop.
- **TLS/authentication.** Planned for Phase 11 — TLS in the transport/runtime layer, ACSE auth hooks in go-mms.
- **Server-side device model framework.** No port of `mms_device_model.h` / `mms_value_cache.h` — the server uses a minimal Go-native registry.

---

## 2. Why this library exists

- **Native Go OT stack.** The OTfabric ecosystem needs a pure-Go MMS implementation — no CGo, no external C library dependency, no cross-compilation headaches.
- **Composable transport.** Reuses `otfabric/go-tpkt` for TPKT framing and `otfabric/go-cotp` for ISO 8073 COTP, keeping each layer independently testable and versioned.
- **Better observability.** Go-native structured logging, context propagation, and tracing from TCP to MMS PDU.
- **Better testing.** Table-driven tests, fuzzing, race detection, golden-frame tests — all idiomatic in Go.
- **Better packaging.** Single `go get`, no build system complexity, no platform-specific compilation.
- **Better ergonomics.** `context.Context` everywhere, explicit option structs, typed errors, no global state.

---

## 3. Architecture principles

1. **MMS only, no IEC 61850.** Every design decision must be evaluated against this boundary. If a type, name, or concept exists only in IEC 61850, it does not belong here.
2. **Composition over monolith.** Each protocol layer (TPKT, COTP, session, presentation, ACSE, MMS) is a distinct internal concern. They compose; they do not inherit.
3. **Clean public API, private protocol plumbing.** Users interact with `mms.Client` and typed request/response types. They never see session SPDUs, presentation context IDs, or ACSE AARQs.
4. **Strong boundaries.** Transport, protocol framing, and MMS domain logic have clear package boundaries. Changes to ASN.1 encoding do not ripple into the public API.
5. **stdlib ASN.1 first, custom only where forced.** `go-mms` uses Go's `encoding/asn1` package as the primary ASN.1 implementation tool where it cleanly fits the MMS wire structures. Custom low-level encoding/decoding helpers are introduced only when required by MMS structure complexity or stdlib limitations. Any such custom code remains internal. The public API exposes neither `encoding/asn1` internals (e.g. `asn1.RawValue`) nor custom codec types. `encoding/asn1` is an implementation aid, not the public model — the public API exposes MMS concepts, not ASN.1 struct layouts. Some MMS structures may not map cleanly onto `encoding/asn1` struct-based decoding — in those cases, prefer targeted internal decoders for those specific structures rather than broadening `internal/asn1util` into a generic ASN.1 framework.
6. **Context-first APIs.** Every blocking operation takes `context.Context` for cancellation and timeout.
7. **Explicit configuration over hidden globals.** Connection parameters, negotiation settings, and timeouts are passed explicitly — no package-level defaults that silently affect behavior.
8. **Deterministic request/response correlation.** Invoke IDs are managed internally with strict matching. Outstanding calls have bounded lifetimes.
9. **Strict decoding and validation.** Malformed PDUs produce clear errors, not silent corruption. Invalid lengths, unexpected tags, and truncated data are caught early.
10. **Traceability and debuggability.** Every association, request, and response can be traced through logs. Invoke IDs, PDU types, and timing are first-class log fields. Log field names must be stable across releases to support downstream tooling.
11. **No magical stringly typed APIs.** Important MMS concepts (domain names, variable names, object classes, service identifiers) use strong named types, not raw strings. Stdlib ASN.1 types like `asn1.ObjectIdentifier` may be used internally or selectively in public types where they are genuinely idiomatic.
12. **Clean-room Go implementation.** The C source tree is a behavioral and structural reference only. The Go implementation is original, idiomatic Go, using `encoding/asn1` where appropriate, with small internal helpers only where needed. Do not transliterate C source files or generated ASN.1 code into Go.

---

## 4. Mapping from C code to Go responsibilities

### Core MMS concepts to keep

| C source | Go responsibility | Priority |
|---|---|---|
| `inc/mms_client_connection.h` | Public client API shape — services, connection lifecycle | P0 |
| `inc/mms_value.h` | MMS value model — types, constructors, accessors | P0 |
| `inc/mms_common.h` | Error codes, MMS type enum, data access errors | P0 |
| `inc/mms_types.h`, `inc/mms_type_spec.h` | Variable specification / type spec model | P0 |
| `inc/iso_connection_parameters.h` | Connection parameters (AP-title, selectors, etc.) | P0 |
| `iso_mms/common/mms_value.c` | Value encode/decode (MMS Data ↔ Go types) | P0 |
| `iso_mms/common/mms_common_msg.c` | Data element encoding, service error parsing, reject parsing | P0 |
| `iso_mms/client/mms_client_initiate.c` | Initiate/conclude request/response construction and parsing | P0 |
| `iso_mms/client/mms_client_read.c` | Read request/response | P1 |
| `iso_mms/client/mms_client_write.c` | Write request/response | P1 |
| `iso_mms/client/mms_client_get_namelist.c` | GetNameList request/response | P1 |
| `iso_mms/client/mms_client_get_var_access.c` | GetVariableAccessAttributes | P1 |
| `iso_mms/client/mms_client_identify.c` | Identify request/response | P1 |
| `iso_mms/client/mms_client_status.c` | Status request/response | P1 |
| `iso_mms/client/mms_client_named_variable_list.c` | Named variable list operations | P2 |
| `iso_mms/client/mms_client_files.c` | File services | P2 |
| `iso_mms/client/mms_client_journals.c` | Journal services | P2 |

### Upper-layer ISO internals to keep internal

| C source | Go responsibility |
|---|---|
| `asn1/ber_decode.c`, `ber_encoder.c`, `ber_integer.c` | `internal/asn1util` — thin helpers for gaps in `encoding/asn1`; `internal/codec` — MMS-specific marshal/unmarshal wrappers |
| `iso_acse/acse.c` | `internal/acse` — AARQ/AARE construction and parsing |
| `iso_session/iso_session.c` | `internal/session` — ISO 8327 session SPDU handling |
| `iso_presentation/iso_presentation.c` | `internal/presentation` — ISO 8823 presentation PDU handling |
| `iso_cotp/cotp.c` | Likely delegated to `otfabric/go-cotp`; fallback: `internal/cotp` |
| `iso_client/iso_client_connection.c` | `internal/isostack` — ISO stack orchestration for client |
| `iso_common/iso_connection_parameters.c` | Partially public (config types), partially internal (parameter encoding) |

### Generated ASN.1 artifacts — wire/schema reference only

| C source | Use in Go |
|---|---|
| `iso_mms/asn1c/*` | Reference for PDU structure, field names, tag values, CHOICE layouts. Do **not** generate Go code from these. Do **not** expose their structure as public API. Model Go structs informed by these definitions, using `encoding/asn1` struct tags and `asn1.RawValue` where appropriate. |

### Server-side pieces — planned in Phase 7

| C source | Go responsibility in Phase 7 |
|---|---|
| `iso_server/*` | `internal/isostack/server.go` — server accept loop, per-connection lifecycle, session/presentation/ACSE server orchestration |
| `iso_connection.c` style flow | `internal/serverconn/conn.go` — connection runtime and dispatch |
| `iso_mms/server/*` | Server-side confirmed service dispatch and handlers for initiate/read/write/identify/status/getnamelist/getvaraccess |
| `inc/mms_server.h` | Reference for public `Server` API shape and handler concepts |

### Still deferred even inside server roadmap

| C area | Status |
|---|---|
| `mms_device_model.h`, `mms_value_cache.h` | Still deferred — do not port a full device model |
| `mms_information_report.c` | COMPLETE — Phase 10 |
| TLS/authentication in ACSE | COMPLETE — Phase 11A + 11B |
| File services | COMPLETE — Phase 12 |
| Journal services | COMPLETE — Phase 13 |

---

## 5. Proposed Go package structure

```
go-mms/
├── mms.go                          # Package mms — public client API
├── server.go                       # Public Server API (Phase 7)
├── server_options.go               # ServerOptions, ListenOptions (Phase 7)
├── server_handlers.go              # Handler interfaces / adapters (Phase 7)
├── server_model.go                 # Public lightweight MMS server model types (Phase 7)
├── value.go                        # MMS Value types, constructors, accessors
├── types.go                        # Named types: ObjectName, DomainID, TypeSpec, enums
├── errors.go                       # Sentinel errors, typed error structs
├── options.go                      # Option structs for Connect, Read, Write, etc.
├── doc.go                          # Package documentation
│
├── internal/
│   ├── codec/                      # MMS-specific marshal/unmarshal wrappers over encoding/asn1
│   │   ├── marshal.go             # High-level encode helpers
│   │   ├── unmarshal.go           # High-level decode helpers
│   │   └── choice.go             # CHOICE dispatch helpers
│   │
│   ├── asn1util/                   # Thin helpers for gaps in encoding/asn1
│   │   ├── raw.go                 # RawValue manipulation, tag inspection
│   │   ├── tags.go                # ASN.1 tag/class constants for MMS
│   │   └── params.go             # Struct tag helpers if needed
│   │
│   ├── pdu/                        # MMS PDU construction and parsing (shared by client+server)
│   │   ├── mmspdu.go              # Top-level MmsPdu CHOICE dispatch
│   │   ├── initiate.go            # Initiate request/response
│   │   ├── confirmed.go           # ConfirmedRequest/Response framing
│   │   ├── read.go                # Read service PDU
│   │   ├── write.go               # Write service PDU
│   │   ├── getnamelist.go         # GetNameList service PDU
│   │   ├── getvaraccess.go        # GetVariableAccessAttributes PDU
│   │   ├── identify.go            # Identify service PDU
│   │   ├── status.go              # Status service PDU
│   │   ├── namedvarlist.go        # Named variable list PDUs
│   │   ├── error.go               # ConfirmedErrorPDU, RejectPDU parsing
│   │   └── data.go                # MMS Data encoding/decoding
│   │
│   ├── acse/                       # ACSE association handling
│   │   └── acse.go
│   │
│   ├── session/                    # ISO 8327 session layer
│   │   └── session.go
│   │
│   ├── presentation/               # ISO 8823 presentation layer
│   │   └── presentation.go
│   │
│   ├── isostack/                   # ISO stack orchestration
│   │   ├── client.go              # Client-side ISO stack (session+pres+acse)
│   │   ├── server.go              # Server-side ISO stack orchestration (Phase 7)
│   │   └── params.go              # Internal parameter encoding
│   │
│   ├── serverconn/                 # Per-connection server state machine (Phase 7)
│   │   ├── conn.go                # Connection lifecycle and state
│   │   ├── dispatch.go            # Confirmed request dispatch to handlers
│   │   ├── association.go         # Association accept/reject
│   │   └── services.go            # Service-specific request/response helpers
│   │
│   ├── servermodel/                # Internal helpers around server registry (Phase 7)
│   │   ├── registry.go            # Domain/variable/named-var-list registry
│   │   ├── names.go               # Name list iteration with deterministic ordering
│   │   └── namedvarlist.go        # Named variable list storage
│   │
│   ├── invoke/                     # Invoke ID management and request correlation
│   │   └── tracker.go
│   │
│   └── transport/                  # Transport abstraction (extended with listener/accept in Phase 7)
│       └── transport.go
│
├── _examples/
│   ├── basic/                      # Client CLI example
│   ├── server-basic/               # Minimal server example (Phase 7)
│   └── server-readwrite/           # Server with read/write handlers (Phase 7)
│
└── testdata/                       # Golden frames, reference PDUs
    └── *.bin / *.json
```

### What is public

- **`mms` (root package) — client:** `Client`, `Dial`, `NewClient`, `DialOptions`, `ReadResult`, `WriteResult`, `NameListResult`, `IdentifyResult`, `StatusResult`, `VariableAccessAttributes`, `NamedVariableListAttributes`, `DeleteNamedVariableListResult`.
- **`mms` (root package) — server (Phase 7):** `Server`, `NewServer`, `ServerOptions`, `ListenOptions`, handler function types (e.g., `HandleIdentify`, `HandleStatus`, `RegisterVariable`), lightweight model/registration types (`Variable`, `Domain`).
- **Value types:** `Value`, `TypeSpec`, `ObjectName`, `DomainID`, `ItemID`, `DataAccessError` — strong types for MMS data, shared between client and server.
- **Error types:** Sentinel errors, `ServiceError`, `ProtocolError`, `DecodeError`.

Note: a public `mmstest` helper package is deferred. It is not needed up front and can be introduced later if downstream consumers require test scaffolding.

### What stays internal

- **`internal/codec`:** MMS-specific marshal/unmarshal wrappers built on `encoding/asn1`. Not part of the public contract.
- **`internal/asn1util`:** Thin helpers for stdlib gaps (e.g., CHOICE dispatch, raw tag inspection, MMS-specific tag constants). Minimal by design — only what `encoding/asn1` cannot express.
- **`internal/pdu`:** Wire-level PDU construction and parsing. Shared by client and server. Users never see these.
- **`internal/acse`, `internal/session`, `internal/presentation`:** ISO upper-layer handling. Users don't know these exist.
- **`internal/isostack`:** Orchestrates the full ISO stack for both client connection establishment and server accept (Phase 7).
- **`internal/invoke`:** Invoke ID allocation and outstanding-call tracking.
- **`internal/serverconn` (Phase 7):** Per-connection server runtime — state machine, confirmed request dispatch, association management.
- **`internal/servermodel` (Phase 7):** Internal helpers for the server registry — domain/variable/named-variable-list storage and lookup.

### Avoiding a god package

The root `mms` package is the public entry point but delegates heavily:
- PDU encoding/decoding → `internal/pdu`
- ASN.1 marshaling → `internal/codec` + `encoding/asn1`
- ISO stack → `internal/isostack`
- Request correlation → `internal/invoke`

The root package owns the `Client` type and public method signatures. It translates between user-facing types and internal wire types.

### Avoiding premature exposure

Wire-level details (tag values, encoding choices, PDU byte layouts) stay in `internal/`. If the codec strategy evolves, the public API is unaffected. The `internal/codec` or `internal/asn1util` packages can be promoted to a top-level shared module later without breaking consumers.

---

## 6. Public API design guidance

### What the API should feel like

```go
client, err := mms.Dial(ctx, "10.0.0.1:102", mms.DialOptions{
    Transport: mms.TransportOptions{
        LocalTSelector:  []byte{0x00, 0x01},
        RemoteTSelector: []byte{0x00, 0x01},
    },
    ISO: mms.ISOOptions{
        LocalAPTitle:  mms.APTitle{1, 1, 1, 1},
        RemoteAPTitle: mms.APTitle{1, 1, 1, 1},
    },
    MMS: mms.MMSOptions{
        MaxPDUSize: 65000,
    },
    Logger: slog.Default(),
})
defer client.Close(ctx)

result, err := client.Read(ctx, mms.ReadRequest{
    DomainID: "MyDomain",
    ItemID:   "MyVariable",
})

val := result.Value
fmt.Println(val.Type(), val.Int32())
```

Connection options must remain layered and grouped by responsibility so `DialOptions` does not become a flat, cross-layer catch-all. The exact grouping names (`TransportOptions`, `ISOOptions`, `MMSOptions`) may evolve, but the principle of separation by layer is non-negotiable.

### Must encourage

- **`context.Context`** on every blocking call — `Dial`, `Read`, `Write`, `GetNameList`, `Close`.
- **Explicit option structs** — `DialOptions`, `ReadRequest`, `WriteRequest`, not positional parameters.
- **Named types** — `DomainID`, `ItemID`, `ObjectName`, `ServiceError` — not raw strings.
- **Typed enums** — `ObjectClass`, `ValueType`, `DataAccessError` as string-backed or int-backed named types.
- **Minimal public surface** — export only what users need; grow the API deliberately.
- **Interfaces only where they buy value** — not interfaces for every internal component. Prefer concrete stdlib types (e.g., `*slog.Logger`) over custom interfaces when they already fit.

### Must discourage

- **Giant mutable connection objects.** `Client` should be focused: connect, send request, receive response, close. State management is internal.
- **Too many exported structs.** Don't expose separate types for every ASN.1 CHOICE branch. Flatten where ergonomic.
- **Exposing generated ASN.1 structures.** Users should never import an `asn1c` or `pdu` package.
- **Forcing session/presentation/ACSE understanding.** The API abstracts over the ISO stack. A user says "connect to this MMS server" — they don't say "establish a session, then a presentation context, then an ACSE association."

---

## 7. Typing strategy

### Named types for important concepts

```go
type DomainID string
type ItemID string
type InvokeID uint32
type ObjectClass int

const (
    ObjectClassNamedVariable     ObjectClass = 0
    ObjectClassScatteredAccess   ObjectClass = 1
    ObjectClassNamedVariableList ObjectClass = 2
    // ...
)

func (c ObjectClass) String() string { /* stable, human-readable output */ }
```

### Value types

```go
type ValueType int

const (
    ValueTypeBoolean      ValueType = iota
    ValueTypeInteger
    ValueTypeUnsigned
    ValueTypeFloat
    ValueTypeBitString
    ValueTypeOctetString
    ValueTypeVisibleString
    ValueTypeMmsString
    ValueTypeUTCTime
    ValueTypeBinaryTime
    ValueTypeArray
    ValueTypeStructure
    ValueTypeDataAccessError
)

func (t ValueType) String() string { /* stable, human-readable output */ }
```

**Rule: all exported enum-like types must have a stable `String()` method** producing output suitable for logs, debugging, and CLI rendering. Internal representation may be int-backed for efficiency, but the string output must be human-readable and must not change between releases.

`Value` is a concrete struct with a type tag and accessor methods. Accessors use `(T, bool)` signatures for simple typed reads:

```go
func (v *Value) Bool() (bool, bool)
func (v *Value) Int32() (int32, bool)
func (v *Value) Float64() (float64, bool)
func (v *Value) String() (string, bool)
func (v *Value) UTCTime() (time.Time, bool)
func (v *Value) Structure() ([]*Value, bool)
func (v *Value) ArrayElements() ([]*Value, bool)
```

The second return value is `false` if the value's type does not match the accessor. This avoids panics for ordinary API misuse. Panics are reserved exclusively for internal invariant violations that indicate a library bug, never for caller mistakes.

### What to avoid

- **`map[string]any` for structured data.** MMS values have well-defined types; use them.
- **Raw `string` for domain IDs, item IDs, object classes.** Named types catch misuse at compile time.
- **Collapsing error categories.** `DataAccessError`, `ServiceError`, and `ProtocolError` are distinct concepts — don't merge them into a single error type.

### Type specifications

```go
type TypeSpec struct {
    Type     ValueType
    Elements []TypeSpecElement  // for Structure
    Count    int                // for Array
    Size     int                // for Integer/Unsigned bit width, string max length
    // ...
}
```

---

## 8. Error strategy

### Sentinel errors

```go
var (
    ErrClosed              = errors.New("mms: connection closed")
    ErrInvokeTimeout       = errors.New("mms: invoke timeout")
    ErrConnectionRejected  = errors.New("mms: connection rejected")
    ErrAssociationFailed   = errors.New("mms: association failed")
    ErrNegotiationFailed   = errors.New("mms: negotiation failed")
    ErrInvalidPDU          = errors.New("mms: invalid PDU")
    ErrDecodeFailed        = errors.New("mms: decode failed")
    ErrUnsupported         = errors.New("mms: unsupported")
    ErrServiceRejected     = errors.New("mms: service rejected")
)
```

Note: there is no generic `ErrTimeout`. Timeout semantics are distinguished explicitly:
- **Context timeout/cancellation:** Callers check `ctx.Err()` or `errors.Is(err, context.DeadlineExceeded)`.
- **Network timeout:** Callers check `errors.As(err, &netErr)` where `netErr` is `net.Error` with `Timeout() == true`.
- **Invoke/protocol timeout:** `ErrInvokeTimeout` indicates that a confirmed request's invoke ID expired without a response from the remote server.

### Typed errors

```go
type ServiceError struct {
    ErrorClass ErrorClass
    ErrorCode  int
    InvokeID   InvokeID
}

type DecodeError struct {
    Offset  int
    Tag     byte
    Message string
}

type ProtocolError struct {
    Phase   string   // "session", "presentation", "acse", "mms"
    Message string
}

type DataAccessError struct {
    Code DataAccessErrorCode
}
```

### Error wrapping

All errors are wrapped with `fmt.Errorf("mms: <context>: %w", err)` so callers can use `errors.Is` and `errors.As`.

### Error categories

| Category | Sentinel / Type | When |
|---|---|---|
| Config | validation errors | Invalid parameters before connect |
| Transport | `ErrClosed`, `net.Error` | TCP failures, network timeouts |
| Association | `ErrAssociationFailed`, `ErrConnectionRejected` | ACSE AARE reject, COTP CC failure |
| Negotiation | `ErrNegotiationFailed` | MMS Initiate parameter mismatch |
| Invoke timeout | `ErrInvokeTimeout` | Confirmed request invoke ID expired without response |
| Decode | `DecodeError`, `ErrInvalidPDU` | Malformed PDU, unexpected tags, truncated data |
| Protocol | `ProtocolError` | Unexpected SPDU, wrong state, sequence errors |
| Remote/Service | `ServiceError`, `ErrServiceRejected` | ConfirmedErrorPDU from server |
| Data access | `DataAccessError` | Per-variable access errors in read/write responses |
| Unsupported | `ErrUnsupported` | Services or features not yet implemented |

### What to avoid

- Comparing error text with `strings.Contains`.
- Mixing remote MMS service errors with local Go runtime errors in the same type.
- Opaque error strings without structured fields.

---

## 9. Logging and observability strategy

### Design principles

- **Structured logging friendly.** All log output should be key/value pairs, not formatted strings.
- **No logging by default.** Silent unless a logger is provided.
- **Pluggable logger.** Accept an optional `*slog.Logger` at `Dial` time.

### Logger integration

Accept an optional `*slog.Logger` at `Dial` time. When `nil`, logging is disabled (silent by default).

```go
client, err := mms.Dial(ctx, addr, mms.DialOptions{
    Logger: slog.Default(),
    // ...
})
```

Using `*slog.Logger` directly avoids defining a custom interface, leverages the stdlib ecosystem, and gives callers full control over handlers, levels, and output format.

### Log levels and what they emit

| Level | Content |
|---|---|
| **Disabled** | Nothing. Default. |
| **Info** | Connection lifecycle: connect, associate, initiate, close. Errors. |
| **Debug** | Request/response summaries: invoke ID, service type, domain, item, duration. Negotiation parameters. |

Raw PDU hex dumps, codec CHOICE decisions, frame boundaries, and custom decode path entries are **not** emitted via log levels. They are available exclusively through the trace hooks described below. This keeps the `*slog.Logger` output clean and avoids conflating structured log levels with wire-level diagnostics.

### Required log fields

- `invoke_id` — on every request/response log line.
- `service` — MMS service name (read, write, getNameList, etc.).
- `domain_id`, `item_id` — where applicable.
- `duration_ms` — request round-trip time.
- `pdu_type` — confirmed request, confirmed response, error, reject.
- `remote_addr` — peer address.
- `error_class`, `error_code` — on service errors.

### Debug hooks

- **PDU hook:** Optional `PDUHook func(direction string, raw []byte)` on the client for packet capture / replay tooling.
- **Decode trace hook:** Optional internal-use hook for logging codec-boundary decisions — which PDU family was chosen, which CHOICE branch was selected, key `asn1.RawValue` tag/class observations, and whether a fallback/custom decode path was taken. This is not part of the public API but supports internal troubleshooting of interop issues.

### Codec-boundary observability

Because interop bugs often live at the codec boundary, the logging layer must support visibility into:
- Top-level PDU family chosen (initiate, confirmed request, confirmed response, error, reject).
- CHOICE branch selected within a service.
- Key `asn1.RawValue` tag/class observations where relevant.
- Fallback to custom decode paths (i.e., cases where `encoding/asn1` couldn't handle the structure directly).

This visibility is available exclusively through the trace hooks, not through `*slog.Logger` levels. It does not expose raw ASN.1 internals in the public API.

### Redaction

Sensitive fields (passwords, authentication tokens) must not appear in logs. The logging layer must be redaction-aware from day one.

### Goal

Debugging field interoperability issues is a first-class use case. An engineer should be able to enable debug logging and see the full association flow, every PDU exchanged, and every error — with invoke IDs for correlation.

---

## 10. Testing strategy

### Unit tests

- **PDU construction:** Each MMS service request/response encoder produces expected bytes. Table-driven with known-good reference frames.
- **PDU parsing:** Each MMS service response parser handles valid and malformed input correctly.
- **Service-level encode/decode round-trips:** Full marshal → unmarshal cycle for every supported MMS service, verifying field-level correctness.
- **`encoding/asn1` integration boundaries:** Tests that verify correct behavior at the boundary between stdlib ASN.1 and any custom helpers — especially CHOICE dispatch, context-specific tags, and `asn1.RawValue` handling.
- **Value model:** Constructors, accessors, type conversions. Invalid access returns `(zero, false)`.
- **Custom ASN.1 helpers:** If any custom low-level helpers are introduced in `internal/asn1util`, they get dedicated encode/decode round-trip tests and edge case coverage.

### Golden frame tests

- Capture real MMS frames from the C reference implementation or from Wireshark.
- Store as `testdata/*.bin` with corresponding `.json` expected-output files.
- Parse and assert field-by-field.

### Table-driven tests

Every codec, parser, and public API method should have table-driven tests with named sub-tests for clarity.

### Negative tests

- Truncated PDUs at every possible boundary.
- Wrong tags in expected positions.
- Invalid lengths (too long, too short, overflow).
- Unexpected CHOICE branches.
- Duplicate invoke IDs.

### Fuzzing

- **MMS PDU decode paths:** Fuzz the top-level MMS PDU parser and each service-specific decoder with arbitrary bytes.
- **CHOICE dispatch:** Fuzz the CHOICE dispatch logic in `internal/codec` to ensure unexpected tags produce clean errors, not panics.
- **Custom ASN.1 helpers:** Fuzz any custom code in `internal/asn1util` that handles gaps in `encoding/asn1`.
- **ISO upper layers:** Fuzz SPDU/PPDU/ACSE parsers in `internal/session`, `internal/presentation`, `internal/acse`.

Fuzzing targets should be registered as `func FuzzXxx(f *testing.F)` with seed corpora from golden frames.

### Concurrency tests

- Concurrent reads/writes on a single client.
- Connection close during outstanding requests.
- Context cancellation mid-request.
- Run with `-race` in CI.

### Timeout and cancellation tests

- Context deadline exceeded during connect, associate, and read.
- Server not responding — ensure client doesn't hang.
- Invoke ID timeout and cleanup.

### Interoperability tests

- Deferred to Phase 6, but the test framework should be designed to support them from the start.
- Test against the C reference implementation's server.
- Capture and replay real protocol exchanges.

### High-risk areas deserving extra coverage

- MMS PDU CHOICE dispatch (wrong service type → clear error, not panic or silent corruption).
- `encoding/asn1` integration boundaries — especially context-specific tags, optional fields, and `asn1.RawValue` round-trips.
- Service-specific decode decisions (e.g., read response with mixed access results and data access errors).
- Association state machine (out-of-order messages, unexpected disconnect).
- Invoke ID correlation (response for wrong invoke ID, duplicate IDs).
- Any custom ASN.1 helpers — these are the riskiest code in the project by definition.

---

## 11. Phased implementation roadmap

### Phase 0 — Architecture and package skeleton

**Goals:** Establish the repository structure, dependencies, and build/test infrastructure.

**Deliverables:**
- `go.mod` with module path `github.com/otfabric/go-mms`.
- Package skeleton matching the structure in section 5.
- `errors.go` with initial sentinel errors.
- `*slog.Logger` integration in `DialOptions`.
- `PLAN.md` (this document).
- CI configuration (lint, test, race, fuzz).

**Deferred:** All protocol logic.

**Done criteria:**
- `go build ./...` succeeds.
- `go test ./...` passes (even if tests are trivial/empty).
- CI pipeline runs lint + test + race.

---

### Phase 1 — ASN.1 / MMS codec feasibility

**Goals:** Prove which MMS PDUs and structures can be modeled cleanly with `encoding/asn1`, identify where `asn1.RawValue` is sufficient, and determine where small custom helpers are required.

**Deliverables:**
- Internal Go structs for key PDU families using `encoding/asn1` struct tags:
  - Initiate request/response.
  - ConfirmedRequest/Response envelope (invoke ID + service CHOICE).
  - Read/Write basic service payloads.
  - ServiceError / Reject structures where practical.
- Identification of where `asn1.RawValue` is needed for CHOICE-heavy or awkward MMS constructs.
- Written decision document (in code comments or a design note) on which parts use stdlib directly and which require internal helpers.
- Minimal `internal/asn1util` helpers only for proven gaps.
- `internal/codec` wrappers for MMS-specific marshal/unmarshal patterns.

**Deferred:** Full service coverage, ISO upper layers.

**Done criteria:**
- At least one association-related PDU family (e.g., Initiate request/response) and one confirmed-service PDU family (e.g., Read or Identify) successfully encoded and decoded with `encoding/asn1`, verified against golden frames from the C reference implementation. The second is required because confirmed-service PDUs are where CHOICE/dispatch complexity starts to matter.
- Clear written decision on stdlib vs. custom for each major PDU category, with documentation of exactly which structures required custom handling and why.
- All custom helpers have unit tests.

---

### Phase 2 — ISO upper-layer internals

**Goals:** Implement session, presentation, and ACSE layers in `internal/`.

**Deliverables:**
- `internal/session` — ISO 8327: CONNECT, ACCEPT, DATA, FINISH, DISCONNECT, ABORT SPDU construction and parsing.
- `internal/presentation` — ISO 8823: CP-type, CPA-type, user data, context negotiation.
- `internal/acse` — AARQ, AARE, ABRT, RLRQ, RLRE construction and parsing. AP-title, AE-qualifier encoding.
- `internal/isostack` — Client-side orchestration: TCP → COTP (via `go-cotp`) → session → presentation → ACSE.
- Integration with `otfabric/go-tpkt` and `otfabric/go-cotp`. Clear boundary: `go-mms` owns everything above COTP; `go-tpkt` and `go-cotp` own their respective layers. `go-mms` does not reimplement TPKT framing or COTP connection management.
- Tests with golden frames for each layer.

**Deferred:** MMS PDU handling, public API.

**Done criteria:**
- Each layer can parse golden-frame SPDUs/PPDUs from the C reference implementation.
- Each layer can construct valid SPDUs/PPDUs that match reference output byte-for-byte (or semantically equivalent).
- `internal/isostack` can orchestrate a full association sequence in a unit test using a mock transport.

---

### Phase 3 — MMS initiate, identify, status

**Goals:** Establish a working MMS association and implement the simplest services.

**Deliverables:**
- `internal/pdu` — MmsPdu top-level CHOICE dispatch.
- Initiate request/response encoding and parsing. Parameter negotiation (maxPDUSize, services supported, nesting level).
- Conclude request/response.
- `internal/invoke` — Invoke ID allocator, outstanding call tracker with timeouts.
- Identify request/response (vendor, model, revision).
- Status request/response (VMD logical/physical status).
- Public `mms.Client` with `Dial`, `Close`, `Identify`, `Status`.
- End-to-end test: connect to a real or simulated MMS server, initiate, identify, close.

**Deferred:** Read, write, name list, named variable lists.

**Done criteria:**
- `mms.Dial` successfully connects to a test MMS server (C reference or simulated), completes the full ISO stack + MMS Initiate handshake, and returns a connected `Client`.
- `client.Identify(ctx)` returns the server's vendor/model/revision.
- `client.Status(ctx)` returns VMD status.
- `client.Close(ctx)` performs a clean conclude/disconnect.
- Context cancellation during Dial is tested and works.

---

### Phase 4 — Read and write

**Goals:** Implement the core data access services.

**Deliverables:**
- `internal/pdu/data.go` — MMS Data encoding/decoding (all value types).
- `internal/pdu/read.go` — Read request (single variable, component access, array elements, multiple variables, named variable list). Read response parsing with per-variable access results.
- `internal/pdu/write.go` — Write request (single variable, component, array, multiple). Write response parsing.
- Public `Value` type with full accessor surface.
- Public `client.Read()` and `client.Write()` methods.
- ConfirmedErrorPDU and RejectPDU handling.
- Golden frame tests for read/write exchanges.

**Deferred:** Named variable list management, name list browsing.

**Done criteria:**
- Read a single named variable from a test server and verify the returned `Value` type and content.
- Write a single named variable and verify success response.
- Read multiple variables in a single request.
- Handle `DataAccessError` in read responses (e.g., object-not-defined) without panics.
- Handle `ConfirmedErrorPDU` and `RejectPDU` with typed errors.
- All MMS value types (boolean, integer, unsigned, float, bit string, octet string, visible string, MMS string, UTC time, binary time, array, structure) encode/decode correctly.

---

### Phase 5 — Name list, variable access attributes, named variable lists

**Goals:** Implement browsing and metadata services.

**Deliverables:**
- GetNameList — VMD-specific, domain-specific, association-specific. Continuation handling.
- GetVariableAccessAttributes — retrieve type specification for a named variable.
- DefineNamedVariableList, GetNamedVariableListAttributes, DeleteNamedVariableList.
- Public `TypeSpec` model.
- Public methods on `Client`: `GetNameList`, `GetVariableAccessAttributes`, `DefineNamedVariableList`, `DeleteNamedVariableList`.

**Deferred:** File services, journal services.

**Done criteria:**
- `GetNameList` returns domain names from a test server with continuation (multiple pages).
- `GetVariableAccessAttributes` returns a correct `TypeSpec` for a known variable.
- Define, read attributes of, and delete a named variable list.

---

### Phase 6 — Hardening, fuzzing, interop

**Goals:** Production-readiness.

**Deliverables:**
- Fuzz targets for all PDU decode paths and custom ASN.1 helpers, with CI integration.
- Interoperability testing against the C reference implementation.
- Malformed/truncated PDU tests at the MMS layer.
- Concurrency and race tests.
- Timeout and cancellation coverage.
- Error path hardening — every decode path must handle truncation and corruption gracefully.
- Performance profiling and optimization of hot paths (PDU decode, value construction).
- Documentation: GoDoc for all public types and methods.
- Example programs in `_examples/`.

**Deferred:** Server-side.

**Done criteria:**
- All fuzz targets run for at least 1 minute in CI without crashes.
- Full end-to-end interop test suite passes against the C reference server.
- `go test -race ./...` passes with concurrent client operations.
- All public types and methods have GoDoc comments.
- At least one complete example program (connect, read, write, close).

---

### Phase 7 — Server-side MMS

**Goals:** Add a generic Go-native MMS server capable of accepting associations, negotiating initiate parameters, handling core confirmed services, and serving a minimal registry-backed MMS object model. The server reuses shared wire/PDU packages and shared value/types/errors, but keeps orchestration in new internal server packages.

#### Design principles for server-side

1. **No client/server API contamination.** Do not turn the existing client into a shared "god connection" abstraction. Shared code lives in `internal/pdu`, `internal/berutil`, and root-level types. Separate server orchestration lives in `internal/serverconn` and `internal/servermodel`.
2. **Handler-driven, not callback soup.** The C server uses listener callbacks around raw buffers. In Go, the first-class abstraction is a typed handler API, not raw byte callbacks.
3. **Minimal but composable server model.** The first server release supports Identify, Status, GetNameList, GetVariableAccessAttributes, Read, Write. No giant "virtual device framework" up front.
4. **Explicit MMS object model.** The server needs a small Go-native registry of VMD metadata, domains, named variables (with type specs and read/write callbacks), and optionally named variable lists. This is not a port of the C device model.
5. **Strict protocol behavior.** The server produces proper ConfirmedResponsePDU, ConfirmedErrorPDU, and RejectPDU with correct invoke ID reflection and association/conclude behavior.

#### Target public API shape

```go
srv := mms.NewServer(mms.ServerOptions{
    Logger: slog.Default(),
    MMS: mms.ServerMMSOptions{
        MaxPDUSize:               65000,
        MaxOutstandingCalling:    5,
        MaxOutstandingCalled:     5,
        DataStructureNestingLevel: 10,
    },
})

srv.HandleIdentify(func(ctx context.Context, req mms.IdentifyRequest) (*mms.ServerIdentity, error) {
    return &mms.ServerIdentity{
        Vendor: "OTfabric", Model: "go-mms", Revision: "dev",
    }, nil
})

srv.HandleStatus(func(ctx context.Context, req mms.StatusRequest) (*mms.ServerStatus, error) {
    return &mms.ServerStatus{
        Logical:  mms.VMDLogicalStatusStateChangesAllowed,
        Physical: mms.VMDPhysicalStatusOperational,
    }, nil
})

srv.RegisterDomain("process")
srv.RegisterVariable(mms.Variable{
    Name: mms.ObjectName{
        Scope: mms.ObjectScopeDomain, Domain: "process", ItemID: "temperature",
    },
    TypeSpec: mms.TypeSpec{Type: mms.ValueTypeFloat, FormatWidth: 32, ExponentWidth: 8},
    Read: func(ctx context.Context) (*mms.Value, error) {
        return mms.NewFloat(21.5), nil
    },
    Write: func(ctx context.Context, v *mms.Value) error {
        return nil
    },
})

err := srv.ListenAndServe(ctx, ":102")
```

#### First server milestone (MVP)

**Must include:**
- Server-side ISO orchestration
- Initiate negotiation
- Identify, Status
- GetNameList (VMD and domain scope, continuation)
- GetVariableAccessAttributes
- Read, Write
- Strict error handling
- Client↔server integration tests

**Deferred for later phases (server-side only):**
- Named variable list services (DefineNamedVariableList, GetNamedVariableListAttributes, DeleteNamedVariableList) — these already exist on the client side; only the server implementation is deferred → **Phase 8**

**Still out of scope:**
- Unconfirmed services (InformationReport) → **Phase 9**
- Transport integration (Dial with go-tpkt/go-cotp, ListenAndServe) → **Phase 10**
- TLS/authentication in ACSE
- File services, journal services
- IEC 61850 object models or naming helpers
- `mms_device_model.h` / `mms_value_cache.h` style device-model caching frameworks
- Advanced server-side data-set/report/subscription abstractions

---

#### Phase 7A — Server API and architecture scoping

**Goals:** Define the public server API and internal boundaries before coding.

**Deliverables:**
- Public `Server` API proposal with stable type signatures.
- Internal package split finalized (`serverconn`, `servermodel`, `isostack/server.go`).
- Explicit client/server code-sharing rules (what is shared, what is not).
- Decision on listener abstraction: `ListenAndServe` only, or also `Serve(listener)`.
- Decision on whether `go-cotp` listener support is a prerequisite.
- Decision on per-connection context management.
- Decision on handler registration style: monolithic handler vs per-service hooks.
- Explicit "not in first server pass" list.

**Done criteria:**
- Stable proposed API signatures reviewed and accepted.
- Stable server package structure documented.
- No ambiguity about what ships in first server milestone.

---

#### Phase 7B — Server-side ISO stack orchestration

**Goals:** Implement server-side equivalents of the current client ISO stack flow. This is where the deferred `iso_server/*` and `iso_connection.c` style logic comes into play.

**Deliverables:**
- `internal/isostack/server.go` — server-side ISO stack orchestration.
- Connection accept flow.
- COTP connection indication / response path via Go transport stack.
- Session CONNECT / ACCEPT handling.
- Presentation CP / CPA handling.
- ACSE AARQ parse / AARE encode.
- Release / abort handling.
- Per-connection lifecycle state machine.

**Must support:**
- Accept association.
- Reject malformed association cleanly.
- Pass MMS initiate request payload upward.
- Send initiate response back through ACSE/presentation/session stack.
- Handle DATA, FINISH, ABORT.

**Explicitly deferred:** TLS, ACSE authentication policy beyond minimal accept/reject hooks.

**Done criteria:**
- End-to-end association works against a mock MMS server client.
- Existing `mms.Client` can connect to the new server.
- Conclude/release works cleanly.

---

#### Phase 7C — Per-connection server runtime

**Goals:** Add a dedicated connection object for one MMS association.

**Deliverables:**
- `internal/serverconn/conn.go` — connection state: open → associated → stopped → terminated.
- One request-at-a-time dispatch model initially (serialized confirmed request handling per connection).
- Request decode → handler call → response encode pipeline.

**Design note:** Start with serialized confirmed request handling per connection. Do not start with multi-request concurrent server dispatch unless the protocol/state requirements demand it. This keeps it simple and aligns with the current client's practical behavior.

**Done criteria:**
- One active client connection can perform multiple sequential requests.
- Close/abort paths are deterministic.
- Malformed request does not crash connection manager.

---

#### Phase 7D — Initiate negotiation on server side

**Goals:** Handle MMS Initiate Request and produce Initiate Response.

**Deliverables:**
- Parse initiate request from ACSE payload.
- Negotiate: max PDU size, outstanding calls, nesting level, services supported.
- Server-side configuration type: `ServerMMSOptions`.

**Done criteria:**
- Client handshake succeeds with negotiated parameters.
- Negotiation failures produce correct association/protocol errors.

---

#### Phase 7E — Core confirmed service dispatch framework

**Goals:** Build generic dispatch for confirmed requests.

**Deliverables:**
- Top-level confirmed request decode.
- Service tag classification.
- Invoke ID reflection.
- Response/error/reject helpers: confirmed response, confirmed error, reject.

**Design rule:** The handler layer operates on typed service requests, not raw BER blobs.

**Done criteria:**
- Server can decode service kind and route to the proper handler.
- Unknown/unsupported services return consistent typed protocol/service errors.

---

#### Phase 7F — Server support for Identify and Status

**Goals:** Implement the easiest server-side services first.

**Deliverables:**
- Identify handler support.
- Status handler support.
- Default behavior if handlers are absent (configurable server identity and fixed status).

**Done criteria:**
- Existing client `Identify` works against server.
- Existing client `Status` works against server.

---

#### Phase 7G — Server support for GetNameList

**Goals:** Implement browsing.

**Deliverables:**
- VMD scope browsing (domains).
- Domain scope browsing (named variables in a domain).
- Association scope browsing if supported by registry.
- Continuation handling with deterministic ordering.

**Dependency:** The registry/model must provide stable ordering for deterministic pagination.

**Done criteria:**
- Client `GetNameList` and `GetNameListAll` interoperate against server.
- Continuation token behavior is deterministic.

---

#### Phase 7H — Server support for GetVariableAccessAttributes

**Goals:** Return server-side TypeSpec metadata.

**Deliverables:**
- Variable lookup by `ObjectName`.
- Response encoding for: deletable flag, type specification.
- Support for arrays, structures, named type references as far as the public model can represent them.

**Dependency:** The public/internal TypeSpec model must be sufficiently expressive.

**Done criteria:**
- Client `GetVariableAccessAttributes` interoperates against server.
- Nested arrays/structures are correctly encoded.

---

#### Phase 7I — Server support for Read

**Goals:** Implement actual value serving.

**Deliverables:**
- Variable lookup.
- Read callback/provider model.
- Conversion: `Value` → `DataValue` → PDU.
- Support multiple variables in one request.
- Support per-variable access errors.
- Policy for: undefined variable, unsupported access, handler failure, type mismatch between model and value.

**Done criteria:**
- Existing client `Read` and `ReadMultiple` work against server.
- Mixed success/error result lists work correctly.

---

#### Phase 7J — Server support for Write

**Goals:** Implement writing.

**Deliverables:**
- Request decode to `Value`.
- Write callback/provider.
- Per-variable result encoding.
- Typed write errors.
- Clear mapping of handler outcomes to `DataAccessError`, `ServiceError`, and protocol reject.

**Done criteria:**
- Client `Write` interoperates against server.
- Write failures map to correct MMS outcomes.

---

#### Phase 7K — Named variable lists (optional second milestone)

**Goals:** Implement DefineNamedVariableList, GetNamedVariableListAttributes, DeleteNamedVariableList.

**Scoping decision:** Include only after Read/Write/GetNameList/GetVarAccess are solid. This is the recommended second milestone.

**Deliverables:**
- Named variable list storage (per-association or global, depending on scope rules).
- Lifecycle interop with current client.

**Model questions to resolve:**
- Are lists global or per-association?
- How do scope rules map for VMD/domain/association named lists?

**Design preference:** Implement only what the spec requires for current client interop. Keep storage simple. Avoid mirroring all C server behaviors.

**Done criteria:**
- Client named variable list lifecycle interoperates against server.

---

#### Phase 7L — Error mapping and protocol hardening

**Goals:** Make server failures precise and stable.

**Deliverables:**
- Internal error mapping table: model not found, handler unavailable, invalid request shape, unsupported service, write denied.
- Helpers for producing: ConfirmedErrorPDU, RejectPDU.
- Malformed request handling tests.
- Wrong-state tests.
- Release/abort robustness.

**Done criteria:**
- Malformed client traffic never panics server.
- Protocol violations lead to deterministic error outcomes.

---

#### Phase 7M — Server test harness and interop suite

**Goals:** Prove the server against the client and vice versa.

**Deliverables:**
- Loopback client↔server integration tests.
- Interop matrix: current Go client ↔ Go server (future: C client ↔ Go server if available).
- Malformed request corpus.
- Concurrency tests.
- Shutdown behavior tests.

**Specific scenarios:**
- Connect, identify, status, close.
- Get domain list, get variable list.
- Get variable access attributes.
- Read, write.
- Define/get/delete named variable list (if 7K is included).
- Close during active request.
- Malformed request.
- Duplicate invoke ID from client.
- Unexpected PDU type.

**Done criteria:**
- End-to-end suite passes reliably.
- `go test -race ./...` passes for server integration tests.

---

#### Phase 7N — Server documentation and examples

**Goals:** Make the server usable.

**Deliverables:**
- GoDoc for all new public types.
- `_examples/server-basic` — minimal server.
- `_examples/server-readwrite` — server with read/write handlers.
- Design notes explaining: generic MMS scope, no IEC 61850 logic, deferred features.

**Done criteria:**
- At least one runnable server example.
- Client example can talk to server example.

---

#### Recommended implementation order

1. **7A** — Architecture/API scoping (design doc, not code)
2. **7B** — ISO server orchestration
3. **7C** — Connection runtime
4. **7D** — Initiate negotiation
5. **7E** — Confirmed service dispatch framework
6. **7F** — Identify/Status
7. **7G** — GetNameList
8. **7H** — GetVariableAccessAttributes
9. **7I** — Read
10. **7J** — Write
11. **7L** — Error mapping/hardening
12. **7M** — Integration test suite
13. **7K** — Named variable lists (if still desired in first server line)
14. **7N** — Documentation/examples

---

## 11b. Phase 8 — Server Named Variable Lists

Named variable list services on the server side. Client-side support already
exists (Phase 5). This phase adds the server-side counterpart so that
the existing client `DefineNamedVariableList`, `GetNamedVariableListAttributes`,
and `DeleteNamedVariableList` calls interoperate against the Go server.

### Goals

- Implement server-side support for all three named variable list services.
- Named variable lists are stored per-domain or per-VMD scope in the registry.
- Fully interoperable with the existing client.

### Deliverables

- **Registry support:** `internal/servermodel/registry.go` extended with named
  variable list storage, lookup, define, and delete operations.
- **PDU support:** `internal/pdu/server_helpers.go` extended with unmarshalers
  for DefineNamedVariableList request, GetNamedVariableListAttributes request,
  and DeleteNamedVariableList request. Response marshalers added.
- **Server handlers:** `server.go` gains `handleDefineNVL`, `handleGetNVLAttrs`,
  and `handleDeleteNVL` dispatched from the confirmed service router.
- **GetNameList update:** `handleGetNameList` supports
  `ObjectClassNamedVariableList` at VMD and domain scope.
- **Integration tests:** Client↔server round-trip tests covering define, get
  attributes, list, read from NVL, and delete lifecycle. Negative tests for
  deleting non-existent lists and double-define.

### Done criteria

- Existing client NVL lifecycle (define → getAttrs → delete) works against server.
- `GetNameList` with `ObjectClassNamedVariableList` returns defined lists.
- `go test -race ./...` passes.

---

## 11c. Phase 9 — Transport Integration and Real Connectivity

### Goals

Make the library usable against real endpoints without external glue.
Implement a real connection/runtime layer that owns TCP dial/listen,
TPKT + COTP wiring, and handing an established `mms.Transport` to go-mms.

### Architectural decisions

**TLS placement (settled):**
TLS belongs in the connection/runtime layer, not in MMS core protocol logic.

Target layering: TCP → TLS → TPKT → COTP → Session → Presentation → ACSE → MMS.

Therefore:
- `go-tpkt`: stays TLS-agnostic
- `go-cotp`: stays TLS-agnostic
- `go-mms` core: stays transport-agnostic
- TLS lives in a thin transport/runtime integration layer (e.g.
  `go-mms/transport/iso` or a separate `go-iso` package)

**Authentication split (settled):**
- TLS transport security (handshake, certs, verification) → runtime layer
- ACSE / application authentication (accept/reject policy, peer identity
  mapping) → go-mms association layer

### Deliverables

- **`Dial(ctx, addr, opts)`:** Real client dial over plaintext TCP + TPKT + COTP.
  Convenience wrapper that creates a Transport and calls `NewClient`.
- **Server listener/accept:** Server-side `ListenAndServe` or equivalent
  that accepts TCP connections, wraps each as a Transport via TPKT + COTP,
  and calls `Server.Serve` per connection.
- **Production transport adapter:** Concrete `Transport` implementation backed
  by `go-tpkt` + `go-cotp`.
- **Configuration:** COTP parameters (TSAPs), remote/local selectors
  configurable via existing option structs.
- **Loopback TCP integration tests:** End-to-end over real TCP localhost.
- **Runnable real-network examples.**

### Suggested package placement

Use a thin runtime package:
- `go-mms/transport/iso` or a separate `go-iso`

This package owns:
- `DialTCP` / `ListenTCP`
- Later: `DialTLS` / `ListenTLS`
- Peer identity extraction

### Design rules

- Keep `NewClient(ctx, conn, opts)` as the low-level injected path.
- Make `Dial` a convenience wrapper, not a second semantic codepath.
- Do not put TLS logic into `internal/pdu`, `internal/acse`,
  `internal/session`, or MMS service code.
- Do not put TLS policy into `go-tpkt` or `go-cotp`.

### Dependencies

- `otfabric/go-tpkt` and `otfabric/go-cotp` must be available.

### Done criteria

- Real client connects to real server over TCP/COTP.
- Real server accepts real client over TCP/COTP.
- Existing Identify/Status/Read/Write/GetNameList/NVL flows work
  end-to-end over the real transport path.
- Existing in-process loopback tests remain unchanged.

### C source reference

| File | Purpose |
|------|---------|
| `sources/mms/iso_server/iso_server.c` | TCP listen, accept, connection lifecycle |
| `sources/mms/iso_server/iso_connection.c` | Per-connection I/O, TLS socket setup |
| `sources/mms/iso_client/iso_client_connection.c` | TCP connect, COTP/ACSE handshake states |
| `sources/mms/iso_cotp/cotp.c` | COTP TP0 over TCP (RFC 1006) |
| `sources/mms/inc/iso_connection_parameters.h` | TCP port, TLS config, selectors |

---

## 11d. Phase 10 — InformationReport and Asynchronous Inbound Handling

### Goals

Add the first unconfirmed/server-initiated MMS feature. This is the natural
point to introduce runtime changes needed for unsolicited inbound traffic.

### Required runtime change

**Introduce a background reader loop.**

The client currently uses synchronous request/response reads, which works
for confirmed services but not for unconfirmed inbound traffic. Phase 10
adds:
- One connection reader goroutine.
- Confirmed response dispatch by invoke ID.
- Unconfirmed dispatch to callback/channel.
- Clean shutdown and race-safe coordination.

### Deliverables

- **PDU support:** `internal/pdu/informationreport.go` — marshal (server)
  and unmarshal (client) for InformationReport.
- **Client reader loop:** Background goroutine reads from transport,
  classifies PDUs, dispatches confirmed responses to waiting callers via
  invoke ID, and dispatches unconfirmed PDUs to registered handlers.
- **Client callback API:** `Client.OnInformationReport(handler)` for
  registering a callback to receive incoming reports.
- **Server send API:** `ServerAssociation.SendInformationReport(ctx, vars, values)`
  or equivalent for pushing data to a specific connected client.
- **Integration tests:** Server sends InformationReport; client receives it.
  Tests for idle and concurrent confirmed + unconfirmed traffic. Tests for
  clean shutdown with pending inbound.

### C source reference

| File | Purpose |
|------|---------|
| `sources/mms/iso_mms/server/mms_information_report.c` | Server: encode and send InformationReport (tag `0xa3`, variable specs, access results) |
| `sources/mms/iso_mms/client/mms_client_connection.c` | Client: parse `MmsPdu_PR_unconfirmedPDU`, dispatch InformationReport to `reportHandler` |
| `sources/mms/iso_mms/asn1c/InformationReport.h` | ASN.1 type: `variableAccessSpecification` + `listOfAccessResult` |
| `sources/mms/iso_mms/asn1c/UnconfirmedPDU.h` | UnconfirmedPDU wrapper for unconfirmedService CHOICE |

### Design notes

The C reference uses `reportHandler` callbacks per connection. The Go API
should use a similar pattern — a registered handler function called for
each incoming report. The handler receives the variable list name (or
individual variable names) and the corresponding values.

Three InformationReport styles exist in the C code:
1. Single VMD-specific variable
2. List of variable specifications + values
3. Named variable list reference (VMD-specific)

The Go implementation should support all three.

### Done criteria

- Client receives InformationReport without breaking confirmed request handling.
- Server emits valid reports on demand.
- No races, deadlocks, or lost confirmed responses.
- `go test -race ./...` passes.

---

## 11e. Phase 11 — Security: TLS Transport and ACSE Auth Hooks

Split into two subphases to respect the architectural boundary.

### Phase 11A — TLS Transport Support

**Goal:** Add secure transport in the runtime/transport integration layer.

**Deliverables:**
- Client secure dial options (`tls.Config` plumbing).
- Server secure listener options.
- Certificate verification and peer certificate extraction.
- Typed TLS/verification errors.
- Port 3782 (MMS over TLS) support alongside port 102 (plaintext).

**Design rules:**
- TLS handshake and cert handling stays outside MMS protocol packages.
- Plaintext and TLS paths coexist cleanly.
- High-level config exposed through `DialOptions` / `ServerOptions`, but
  TLS implementation lives in the runtime layer.

**C source reference:**

| File | Purpose |
|------|---------|
| `sources/mms/iso_server/iso_connection.c` | `TLSSocket` creation from accepted socket |
| `sources/mms/iso_client/iso_client_connection.c` | TLS handshake after TCP connect |
| `sources/mms/iso_cotp/cotp.c` | `TLSSocket_write` / `TLSSocket_read` when TLS enabled |
| `sources/mms/inc/iso_connection_parameters.h` | `IsoConnectionParameters_setTlsConfiguration` |
| `sources/mms/inc/mms_client_connection.h` | `MmsConnection_createSecure(TLSConfiguration)` |

**Done criteria:**
- Secure client/server connect successfully.
- Verification failures return typed errors.

### Phase 11B — ACSE / Application Authentication Hooks

**Goal:** Surface peer identity and add association-level auth policy.

**Deliverables:**
- Peer identity surfaced upward from the transport layer.
- Server-side association auth policy hooks (accept/reject decisions
  during association based on peer identity).
- Optional mapping from peer certificate identity to MMS authorization.
- `AcseAuthenticator`-equivalent callback on `ServerOptions`.

**C source reference:**

| File | Purpose |
|------|---------|
| `sources/mms/iso_acse/acse.c` | ACSE auth: password, certificate, TLS cert; `checkAuthentication()` |
| `sources/mms/inc/iso_connection_parameters.h` | `AcseAuthenticationMechanism`, `AcseAuthenticator` callback |
| `sources/mms/inc/mms_server.h` | `MmsServerConnection_getSecurityToken()` |
| `sources/mms/inc_private/acse.h` | `AcseConnection` with `TLSSocket`, `authenticator` |

**Done criteria:**
- Peer identity is available to upper layers.
- Server can make association accept/reject decisions based on peer identity.
- Both password-based and certificate-based auth mechanisms are supported.

---

## 11f. Phase 12 — File Services

### Goals

Add a minimal coherent MMS file-service implementation using a
provider-based design (not direct filesystem coupling).

### Suggested first subset

- FileDirectory (list/directory)
- FileOpen
- FileRead
- FileClose
- FileDelete (optional)

### Required architecture

**Server-side file provider abstraction.** Core server logic depends on
an interface, not the local filesystem. Example:

```go
type FileProvider interface {
    List(ctx context.Context, path string) ([]FileEntry, error)
    Open(ctx context.Context, path string) (FileHandle, error)
    Read(ctx context.Context, handle FileHandle, maxBytes int) ([]byte, bool, error)
    Close(ctx context.Context, handle FileHandle) error
    Delete(ctx context.Context, path string) error // optional
}
```

The server maintains a File Read State Machine (FRSM) per open file
handle, per connection (matching the C reference pattern in
`mms_file_service.c`).

### Deliverables

- **PDU support:** `internal/pdu/file.go` — encode/decode for FileDirectory,
  FileOpen, FileRead, FileClose, FileDelete request/response.
- **Client API:** `Client.FileDirectory`, `Client.FileOpen`, `Client.FileRead`,
  `Client.FileClose`, `Client.FileDelete`.
- **Server file provider interface:** `FileProvider` registered on `Server`.
- **In-memory provider:** For tests.
- **Optional filesystem-backed example provider.**
- **Service tags:** `0x48` (file-open), `0x49` (file-read), `0x4a` (file-close),
  `0x4c` (file-delete), `0x4d` (file-directory).

### C source reference

| File | Purpose |
|------|---------|
| `sources/mms/iso_mms/server/mms_file_service.c` | FRSM management, file attribute encoding, all file service handlers |
| `sources/mms/iso_mms/client/mms_client_files.c` | Client-side file request/response handling |
| `sources/mms/iso_mms/server/mms_server_connection.c` | Routes file service tags: `0x48`–`0x4d` |

### Done criteria

- Client can list and read files from server.
- Server serves file content via provider abstraction.
- Chunking, handle lifecycle, and error paths are well tested.
- `go test -race ./...` passes.

---

## 11g. Phase 13 — Journal Services

### Goals

Add journal object support after the async/runtime and provider patterns
are proven.

### Suggested first subset

- Discover journal names (GetNameList with ObjectClassJournal).
- ReadJournal (time range and start-after queries).
- Journal entry parsing with continuation/paging.

### Required architecture

**Server-side journal provider abstraction:**

```go
type JournalProvider interface {
    ListJournals(ctx context.Context, domain string) ([]string, error)
    ReadTimeRange(ctx context.Context, domain, journal string,
        start, stop time.Time, maxEntries int) (*JournalResult, error)
    ReadStartAfter(ctx context.Context, domain, journal string,
        afterID []byte, afterTime time.Time, maxEntries int) (*JournalResult, error)
}
```

### Deliverables

- **PDU support:** `internal/pdu/journal.go` — encode/decode for
  ReadJournal request/response, journal entry structure.
- **Client API:** `Client.ReadJournalTimeRange`, `Client.ReadJournalStartAfter`.
- **Server journal provider interface:** `JournalProvider` registered per domain.
- **GetNameList update:** Support `ObjectClassJournal` at domain scope.
- **In-memory provider for tests.**
- **Continuation semantics:** `moreFollows` flag with deterministic paging.

### C source reference

| File | Purpose |
|------|---------|
| `sources/mms/iso_mms/server/mms_journal_service.c` | `mmsServer_handleReadJournalRequest()`, `entryCallback`, `entryDataCallback`, `LogStorage_getEntries` |
| `sources/mms/iso_mms/server/mms_journal.c` | `MmsJournal_create()`, journal model |
| `sources/mms/iso_mms/client/mms_client_journals.c` | `mmsClient_parseReadJournalResponse()`, `mmsClient_createReadJournalRequestWithTimeRange/StartAfter` |
| `sources/mms/inc/mms_client_connection.h` | `MmsJournalEntry`, `MmsJournalVariable` types |
| `sources/mms/inc_private/mms_device_model.h` | `struct sMmsJournal`, domain-level journal list |

### Design notes

The C reference uses `LogStorage` as the backing abstraction with callback-
based entry iteration. The Go version should use a simpler slice-based
return pattern. The `entryID` is an opaque `[]byte` for continuation.

### Done criteria

- Client can discover and read journal entries from server.
- Server serves journal data from provider abstraction.
- Continuation semantics are tested strictly.
- `go test -race ./...` passes.

---

## 11h. Phase 14 — Alternate Access and Component/Array Operations

### Goals

Close the biggest functional gap versus libiec61850: sub-variable
addressing. Introduce a Go-native alternate-access model for reading
and writing structure components, array elements, and array ranges.

### Public API design

```go
type AlternateAccess struct {
    Component string   // structure component name
    Index     *int     // array element index (0-based)
    IndexRange *IndexRange // array range
}

type IndexRange struct {
    Start int
    Count int
}
```

The `AlternateAccess` is attached to `ObjectName` in read/write
requests. Multiple selectors can be chained for nested paths
(e.g., component within an array element).

```go
type VariableSpec struct {
    Name           ObjectName
    AlternateAccess []AlternateAccess // optional selector chain
}
```

### Client methods

- `ReadVariables(ctx, []VariableSpec) ([]AccessResult, error)` —
  general-purpose multi-variable read supporting alternate access.
- `WriteVariables(ctx, []VariableSpec, []*Value) ([]WriteResultItem, error)` —
  general-purpose multi-variable write supporting alternate access.

Convenience methods that wrap these:
- `ReadComponent(ctx, name ObjectName, component string) (*ReadResult, error)`
- `WriteComponent(ctx, name ObjectName, component string, value *Value) error`
- `ReadArrayElement(ctx, name ObjectName, index int) (*ReadResult, error)`
- `WriteArrayElement(ctx, name ObjectName, index int, value *Value) error`
- `ReadArrayRange(ctx, name ObjectName, start, count int) (*ReadResult, error)`

### PDU layer

- Extend `encodeListOfVariable` to accept optional alternate access
  per variable.
- Implement `encodeAlternateAccess([]AlternateAccess)` in
  `internal/pdu/read.go` or a new `internal/pdu/altaccess.go`.
- Server-side `UnmarshalReadRequest` extended to parse alternate access.

### BER wire format

- AlternateAccess is tag `[5]` (0xa5) inside each `ListOfVariableSeq`.
- Each `AlternateAccessSelection` supports:
  - `component` (Identifier)
  - `index` (Unsigned32)
  - `indexRange` (SEQUENCE { lowIndex, numberOfElements })
  - `allElements` (NULL)

### Server support

- Extend `handleRead`/`handleWrite` to pass alternate access through
  to the read/write callbacks.
- Variable providers receive the selector chain and return the
  appropriate sub-value.

### C source reference

| File | Purpose |
|------|---------|
| `sources/mms/iso_mms/client/mms_client_read.c` | `createAlternateAccessComponent()` |
| `sources/mms/iso_mms/client/mms_client_common.c` | `mmsClient_createAlternateAccess*()` |
| `sources/mms/iso_mms/asn1c/AlternateAccess.h` | ASN.1 type structure |
| `sources/mms/iso_mms/server/mms_server_common.c` | Server alternate access parsing |

### Done criteria

- Client can read/write individual structure components by name.
- Client can read/write individual array elements by index.
- Client can read array ranges (start + count).
- Server-side read/write handlers receive alternate access selectors.
- Client↔server round-trip tests for all alternate access variants.
- `go test -race ./...` passes.

---

## 11i. Phase 15 — NVL Value Operations and Generic Object Addressing

### Goals

Complete Named Variable List support by adding value-plane operations
(read/write NVL values), and add generic object-scoped convenience
methods for single-variable read/write.

### NVL value operations

```go
func (c *Client) ReadNamedVariableList(ctx context.Context, listName ObjectName) ([]AccessResult, error)
func (c *Client) WriteNamedVariableList(ctx context.Context, listName ObjectName, values []*Value) error
```

These use the `variableListName` CHOICE (tag `[1]` = 0xa1) in the
VariableAccessSpecification instead of `listOfVariable` (tag `[0]`).

### Generic object addressing

```go
func (c *Client) ReadObject(ctx context.Context, name ObjectName) (*ReadResult, error)
func (c *Client) WriteObject(ctx context.Context, name ObjectName, value *Value) (*WriteResult, error)
```

These support all three scopes (VMD, domain, association) through
`ObjectName.Scope`.

### PDU layer

- Add `MarshalReadRequestByListName(invokeID, ObjectNameWire)` for the
  variableListName read variant.
- Add `MarshalWriteRequestByListName(invokeID, ObjectNameWire, []*DataValue)`
  for the variableListName write variant.
- Server-side: extend `handleRead`/`handleWrite` to recognize and
  dispatch `variableListName` requests.

### Server support

- When a read/write uses `variableListName`, the server resolves the
  NVL definition from the registry, reads/writes all member variables,
  and returns the aggregated results.

### C source reference

| File | Purpose |
|------|---------|
| `sources/mms/iso_mms/client/mms_client_read.c` | `mmsClient_createReadNamedVariableListRequest()` |
| `sources/mms/iso_mms/client/mms_client_write.c` | `mmsClient_createWriteRequestNamedVariableList()` |

### Done criteria

- Client can read all values from a named variable list in one PDU.
- Client can write all values to a named variable list in one PDU.
- `ReadObject`/`WriteObject` work for all scopes.
- Server handles NVL reads/writes by resolving list members.
- Integration tests for NVL value round-trips.
- `go test -race ./...` passes.

---

## 11j. Phase 16 — File Parity (Rename + ObtainFile)

### Goals

Complete file service parity with libiec61850 by adding FileRename
and ObtainFile.

### FileRename

Simple request/response service.

```go
func (c *Client) FileRename(ctx context.Context, currentName, newName string) error
```

BER wire format:
- Request tag: `0xbf 0x4b` (extended tag 75)
- `currentFileName [0]`, `newFileName [1]`
- Response: null response with extended tag `0x4b`

Server `FileProvider` extension:
```go
Rename(ctx context.Context, currentName, newName string) error
```

### ObtainFile

Two-party file transfer: client requests server to fetch a file.

```go
func (c *Client) ObtainFile(ctx context.Context, sourceFile, destinationFile string) error
```

BER wire format:
- Request tag: `0xbf 0x2e` (extended tag 46)
- `sourceFileName [1]`, `destinationFileName [2]`
- Response: null response with extended tag `0x2e`

Server-side involves a file-upload task where the server opens,
reads, and closes the source file via callbacks to the client. This
is a complex two-party flow.

### Implementation order

1. FileRename (straightforward request/response)
2. ObtainFile client request + server handler

### C source reference

| File | Purpose |
|------|---------|
| `sources/mms/iso_mms/client/mms_client_files.c` | `mmsClient_createFileRenameRequest()`, `mmsClient_createObtainFileRequest()` |
| `sources/mms/iso_mms/server/mms_file_service.c` | `mmsServer_handleFileRenameRequest()`, `mmsServer_handleObtainFileRequest()` |

### Done criteria

- Client can rename files on server.
- Client can request server to obtain (copy) files.
- Server FileProvider handles rename.
- Integration tests for both services.
- `go test -race ./...` passes.

---

## 11k. Phase 17 — Public Utility Pass

### Goals

Add essential utility methods to `Value` and `TypeSpec` that improve
ergonomics without bloating the API.

### Value utilities

```go
func (v *Value) Clone() *Value
func (v *Value) Equal(other *Value) bool
func (v *Value) String() string
```

- `Clone`: deep copy of the value including nested structures/arrays.
- `Equal`: structural equality comparison.
- `String`: human-readable representation for debugging/logging.

### TypeSpec utilities

```go
func (ts *TypeSpec) ChildByName(name string) (*TypeSpec, bool)
func (ts *TypeSpec) ChildByIndex(index int) (*TypeSpec, bool)
func (ts *TypeSpec) Compatible(v *Value) bool
func (ts *TypeSpec) DefaultValue() *Value
```

- `ChildByName`: look up a structure element's type by name.
- `ChildByIndex`: look up a structure element or array element type
  by index.
- `Compatible`: check whether a value's type matches this spec.
- `DefaultValue`: create a zero-valued `*Value` matching this spec.

### Done criteria

- All utility methods have comprehensive unit tests.
- Edge cases tested: nil values, empty structures, nested arrays,
  type mismatches.
- `go test -race ./...` passes.

---

## 12. Reading priorities from the C source

### P0 — Must read first

These files define the public API concepts and core data model. Read before writing any Go code.

| Path | Why |
|---|---|
| `sources/mms/inc/mms_client_connection.h` | Full client API surface: all service methods, connection lifecycle, callbacks |
| `sources/mms/inc/mms_value.h` | Value model: types, constructors, accessors, encoding |
| `sources/mms/inc/mms_common.h` | Error codes, MMS type enum, data access errors |
| `sources/mms/inc/mms_types.h` | Variable access specification |
| `sources/mms/inc/mms_type_spec.h` | Type specification model |
| `sources/mms/inc/iso_connection_parameters.h` | Connection parameters, AP-title, selectors |
| `sources/mms/asn1/ber_decode.c` | Reference for understanding BER wire format — useful for identifying where `encoding/asn1` gaps may exist |
| `sources/mms/asn1/ber_encoder.c` | Reference for encoding patterns — compare against `encoding/asn1` capabilities |
| `sources/mms/iso_mms/common/mms_value.c` | Value encode/decode (MMS Data ↔ wire) |
| `sources/mms/iso_mms/common/mms_common_msg.c` | Data element codec, service error parsing, reject parsing |

### P1 — Read after architecture is set

These files define client behavior and ISO stack internals needed for implementation.

| Path | Why |
|---|---|
| `sources/mms/iso_mms/client/mms_client_initiate.c` | Initiate/conclude request/response construction |
| `sources/mms/iso_mms/client/mms_client_read.c` | Read service PDU construction and response parsing |
| `sources/mms/iso_mms/client/mms_client_write.c` | Write service PDU construction and response parsing |
| `sources/mms/iso_mms/client/mms_client_get_namelist.c` | GetNameList PDU handling |
| `sources/mms/iso_mms/client/mms_client_identify.c` | Identify service |
| `sources/mms/iso_mms/client/mms_client_status.c` | Status service |
| `sources/mms/iso_mms/client/mms_client_connection.c` | Client connection management, invoke ID dispatch, response routing |
| `sources/mms/iso_acse/acse.c` | ACSE AARQ/AARE encoding and parsing |
| `sources/mms/iso_session/iso_session.c` | Session SPDU construction and parsing |
| `sources/mms/iso_presentation/iso_presentation.c` | Presentation PDU handling and context negotiation |
| `sources/mms/iso_client/iso_client_connection.c` | ISO stack orchestration — TCP → COTP → session → presentation → ACSE flow |
| `sources/mms/inc_private/mms_client_internal.h` | Internal client state: outstanding calls, invoke IDs, callbacks |

### P2 — Read for server-side (Phase 7) and transport (Phase 9)

| Path | Why |
|---|---|
| `sources/mms/iso_mms/client/mms_client_get_var_access.c` | Phase 5 (done) |
| `sources/mms/iso_mms/client/mms_client_named_variable_list.c` | Phase 5 (done) |
| `sources/mms/iso_mms/server/*` | Server-side — Phase 7 (done; behavioral reference for service dispatch and response encoding) |
| `sources/mms/iso_server/*` | Phase 7 (done) and Phase 9 (reference for accept loop, connection lifecycle) |
| `sources/mms/inc/mms_server.h` | Phase 7 (done; reference for public Server API shape and handler concepts) |
| `sources/mms/iso_mms/asn1c/*` | Reference only — consult for tag values and PDU structure when implementing codecs |
| `sources/mms/iso_cotp/cotp.c` | Phase 9 — reference for COTP wiring (replaced by `otfabric/go-cotp`) |
| `sources/mms/iso_client/iso_client_connection.c` | Phase 9 — TCP connect states and ISO stack orchestration |

### P3 — Read for InformationReport (Phase 10) and security (Phase 11)

| Path | Why |
|---|---|
| `sources/mms/iso_mms/server/mms_information_report.c` | Phase 10 — server-side InformationReport encoding (tag `0xa3`) |
| `sources/mms/iso_mms/client/mms_client_connection.c` | Phase 10 — client InformationReport parsing and `reportHandler` dispatch |
| `sources/mms/iso_mms/asn1c/InformationReport.h` | Phase 10 — ASN.1 type structure |
| `sources/mms/iso_mms/asn1c/UnconfirmedPDU.h` | Phase 10 — UnconfirmedPDU wrapper |
| `sources/mms/iso_acse/acse.c` | Phase 11B — ACSE auth mechanisms and `checkAuthentication()` |
| `sources/mms/inc/iso_connection_parameters.h` | Phase 11 — `AcseAuthenticationMechanism`, `AcseAuthenticator`, TLS config |

### P4 — Read for file services (Phase 12) and journal services (Phase 13)

| Path | Why |
|---|---|
| `sources/mms/iso_mms/server/mms_file_service.c` | Phase 12 — FRSM management, file attribute encoding, all file service handlers |
| `sources/mms/iso_mms/client/mms_client_files.c` | Phase 12 — client-side file request/response handling |
| `sources/mms/iso_mms/server/mms_journal_service.c` | Phase 13 — `mmsServer_handleReadJournalRequest()`, `LogStorage` callbacks |
| `sources/mms/iso_mms/server/mms_journal.c` | Phase 13 — journal model: `MmsJournal_create()` |
| `sources/mms/iso_mms/client/mms_client_journals.c` | Phase 13 — `mmsClient_parseReadJournalResponse()`, time range / start-after requests |
| `sources/mms/inc_private/mms_device_model.h` | Phase 13 — `struct sMmsJournal`, domain journal list |

---

## 13. Deliverables

| Deliverable | Phase | Description |
|---|---|---|
| `PLAN.md` | 0 | This document |
| Package skeleton | 0 | Directory structure, `go.mod`, empty packages with doc comments |
| Error taxonomy | 0 | `errors.go` with sentinels and typed error structs |
| `*slog.Logger` integration | 0 | Logger field in `DialOptions`, silent by default |
| ASN.1 codec feasibility decision | 1 | Written decision on stdlib vs. custom for each PDU category |
| `internal/codec` | 1 | MMS-specific marshal/unmarshal wrappers over `encoding/asn1` |
| `internal/asn1util` | 1 | Thin helpers for proven stdlib gaps only |
| `internal/session` | 2 | Session layer |
| `internal/presentation` | 2 | Presentation layer |
| `internal/acse` | 2 | ACSE layer |
| `internal/isostack` | 2 | ISO stack client orchestration |
| `mms.Client` — Dial, Close, Identify, Status | 3 | First working end-to-end connection |
| `mms.Client` — Read, Write | 4 | Core data access |
| `mms.Value` — full type surface | 4 | All MMS value types with `(T, bool)` accessors |
| `mms.Client` — GetNameList, variable lists | 5 | Browsing and metadata |
| Fuzz targets | 6 | All PDU decode paths and custom helpers fuzzed |
| Interop tests | 6 | Against C reference |
| `_examples/basic` | 6 | Client CLI example program |
| GoDoc | 6 | Complete public API documentation |
| Server API design doc | 7A | Public Server API proposal, package split, sharing rules |
| `internal/isostack/server.go` | 7B | Server-side ISO stack orchestration |
| `internal/serverconn/*` | 7C | Per-connection server runtime |
| Server initiate negotiation | 7D | MMS Initiate Request/Response server-side handling |
| Confirmed service dispatch | 7E | Generic confirmed request dispatch framework |
| `server.go` — Identify, Status | 7F | First server services |
| `server.go` — GetNameList | 7G | Server-side browsing with continuation |
| `server.go` — GetVarAccess | 7H | Server-side type spec metadata |
| `server.go` — Read | 7I | Server-side value serving |
| `server.go` — Write | 7J | Server-side value writing |
| Named variable lists (server) | 7K | Optional second milestone |
| Server error mapping/hardening | 7L | Strict error handling, malformed request resilience |
| Server integration test suite | 7M | Client↔server loopback tests, concurrency, shutdown |
| `_examples/server-basic`, `_examples/server-readwrite` | 7N | Server example programs |
| Server GoDoc | 7N | Documentation for all new public server types |
| Server NVL handlers | 8 | DefineNVL, GetNVLAttrs, DeleteNVL server-side |
| Registry NVL support | 8 | NVL storage, lookup, define, delete in registry |
| NVL integration tests | 8 | Full lifecycle and negative tests |
| `Dial(ctx, addr, opts)` | 9 | Real TCP + TPKT + COTP client dial |
| `Server.ListenAndServe` | 9 | Real TCP accept with TPKT + COTP wiring |
| Production `Transport` adapter | 9 | Concrete transport backed by go-tpkt + go-cotp |
| TCP loopback integration tests | 9 | End-to-end over real TCP localhost |
| `internal/pdu/informationreport.go` | 10 | InformationReport marshal/unmarshal |
| Client reader loop | 10 | Background goroutine for inbound PDU dispatch |
| `Client.OnInformationReport` | 10 | Callback registration for unconfirmed reports |
| `ServerAssociation.SendInformationReport` | 10 | Server-side report push API |
| TLS dial/listen options | 11A | `tls.Config` plumbing in runtime layer |
| Peer certificate extraction | 11A | Transport-level identity surfacing |
| ACSE auth hooks | 11B | Server association accept/reject policy |
| `FileProvider` interface | 12 | Provider abstraction for server file services |
| Client file API | 12 | FileDirectory, FileOpen, FileRead, FileClose |
| File PDU codecs | 12 | Encode/decode for file service tags `0x48`–`0x4d` |
| `JournalProvider` interface | 13 | Provider abstraction for server journal services |
| Client journal API | 13 | ReadJournalTimeRange, ReadJournalStartAfter |
| Journal PDU codecs | 13 | Encode/decode for ReadJournal request/response |
| Test/fuzz backlog | Ongoing | Tracked in issues |

---

## 14. Explicit anti-patterns

These patterns are **banned** in `go-mms`:

1. **Mixing IEC 61850 concerns into MMS.** No logical devices, no functional constraints, no report control blocks, no SCL references. If it's in IEC 61850 and not in ISO 9506, it doesn't belong here.

2. **Blindly porting C structs/functions.** The C source tree is a behavioral and structural reference only. The Go implementation is original, idiomatic Go, using `encoding/asn1` where appropriate, with small internal helpers only where needed. Do not transliterate C source files or generated ASN.1 code into Go. Don't create `mms_client_connection.go` because the C has `mms_client_connection.c`.

3. **Exposing generated ASN.1 C-equivalent types.** No `ConfirmedRequestPdu_t` in the public API. No `Data_PR_integer` enums leaking out. Equally, no `asn1.RawValue` in the public API.

4. **Public APIs tightly coupled to wire representations.** Users say `client.Read(ctx, req)`, not `client.SendConfirmedRequestPDU(invokeID, readRequest)`.

5. **Building a second ASN.1 library.** Do not default to writing a full BER engine. Use `encoding/asn1` as the primary codec. Only introduce custom helpers for proven gaps. If `internal/asn1util` grows beyond a handful of focused helpers, something has gone wrong. If it starts expanding into generic BER/TLV infrastructure, stop and reassess: pause feature work, document exactly which MMS structures are forcing the growth, and evaluate whether the codec strategy needs revision. That is a sign the project is drifting into building a second ASN.1 library instead of using focused MMS-specific wrappers.

6. **Weak error semantics.** No `error` values that can only be understood by string matching. Every error category has a sentinel or typed error.

7. **Unbounded logging noise.** No `fmt.Println` debug output. No logging without an explicit `*slog.Logger`. No logging that can't be silenced.

8. **Unclear package boundaries.** If you can't explain in one sentence what a package does and what it doesn't do, it's wrong.

9. **Giant all-knowing client objects.** `Client` dispatches to internal components. It doesn't contain 3000 lines of protocol logic.

10. **Premature support for every MMS service.** Get Initiate → Identify → Read → Write working end-to-end before touching file services or journals.

11. **`map[string]any` as a data model.** MMS has well-defined types. Use them.

12. **Global state.** No package-level variables that affect behavior. No `init()` side effects.

13. **Ignoring context.** Every blocking operation respects `context.Context`. No operations that can hang indefinitely.

14. **Porting IsoServer / IsoConnection 1:1 from C.** Use them as behavioral reference only. The C code is thread-model + buffer-management heavy. In Go, the right split is: listener/accept → per-connection runtime → typed service dispatch → handler/model layer.

15. **Exposing raw byte-buffer application callbacks.** The C server hands MMS bytes upward and expects response bytes back. The public Go server API operates on typed requests and responses, not raw buffers.

16. **Tying the server to IEC 61850-style data models.** Server variables remain generic MMS named objects. No logical devices, no data-sets-as-IEC-concepts.

17. **Starting with unconfirmed services on the server.** Keep `mms_information_report.c` out of the first server pass. Confirmed request/response services first.

18. **Overengineering subscriptions, caches, or device models.** The first server is request/response oriented. No polling cache, no historical store, no subscription engine, no report engine, no device model class hierarchy.

---

## 15. Phase sequencing summary

| Phase | Title | Status |
|-------|-------|--------|
| 0 | Skeleton, errors, logging | COMPLETE |
| 1 | ASN.1/BER codecs | COMPLETE |
| 2 | ISO stack (session/presentation/ACSE) | COMPLETE |
| 3 | Client Initiate, Identify, Status | COMPLETE |
| 4 | Client Read, Write, Value model | COMPLETE |
| 5 | GetNameList, GetVarAccess, Named Variable Lists | COMPLETE |
| 6 | Fuzz, interop, examples, docs | COMPLETE |
| 7 | Server-side MMS (confirmed services) | COMPLETE |
| 8 | Server Named Variable Lists | COMPLETE |
| 9 | Transport integration (Dial / ListenAndServe) | COMPLETE |
| 10 | InformationReport + async reader loop | COMPLETE |
| 11A | TLS transport support | COMPLETE |
| 11B | ACSE / application authentication hooks | COMPLETE |
| 12 | File services (provider-based) | COMPLETE |
| 13 | Journal services (provider-based) | COMPLETE |
| 14 | Alternate access + component/array operations | COMPLETE |
| 15 | NVL value operations + generic object addressing | COMPLETE |
| 16 | File parity (rename + obtain-file) | COMPLETE |
| 17 | Public utility pass (Value + TypeSpec helpers) | COMPLETE |

Execution order for completed phases:
1. Phase 9 — transport integration / real Dial / listener
2. Phase 10 — InformationReport + reader loop + invoke correlation
3. Phase 11 — TLS transport support (11A), then ACSE/application auth hooks (11B)
4. Phase 12 — file services via provider abstraction
5. Phase 13 — journal services via provider abstraction

Execution order for parity phases (all complete):
6. Phase 14 — alternate access model + component/array read/write
7. Phase 15 — NVL value read/write + ReadObject/WriteObject convenience
8. Phase 16 — file rename + obtain-file
9. Phase 17 — Value.Clone/Equal/String + TypeSpec traversal helpers

Key architectural decisions:
- **TLS in connection runtime, ACSE auth in MMS association layer.**
- **Background reader loop introduced in Phase 10 with InformationReport.**
