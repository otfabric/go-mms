# PLAN.md — otfabric/go-mms

A native Go implementation of the MMS (Manufacturing Message Specification) protocol — ISO 9506.

---

## 1. Scope and non-goals

### Scope

- A clean, Go-native MMS protocol library.
- Initial focus: **client-side MMS** (connect, initiate, read, write, name list, identify, status).
- Generic MMS only — usable for any MMS application, not tied to a specific domain.
- Designed to compose with `otfabric/go-tpkt` and `otfabric/go-cotp` for transport.
- Strong named types, structured errors, observable behavior.

### Non-goals

- **IEC 61850 domain logic.** No logical devices, logical nodes, functional constraints, report control blocks, control models, datasets-as-IEC-concepts, SCL/ICD/CID/SCD parsing, or IED naming helpers. IEC 61850 belongs in a separate higher-level package built on top of `go-mms`.
- **GOOSE / Sampled Values / other IEC 61850 stacks.** Out of scope entirely.
- **Server-side MMS.** Out of scope for the initial implementation and roadmap acceptance target.
- **Public APIs for session, presentation, or ACSE.** These layers are internal protocol plumbing. They may be exposed later only if external consumers prove they need them.
- **File services and journal services in phase 1.** These are lower-priority MMS services deferred to later phases.

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

### Server-side pieces — deferred

| C source | Status |
|---|---|
| `iso_mms/server/*` | Deferred to Phase 7+ |
| `iso_server/*` | Deferred to Phase 7+ |
| `inc/mms_server.h` | Deferred |

### Pieces to ignore in phase 1

- `mms_device_model.h`, `mms_value_cache.h` — server-side device model concerns.
- `mms_information_report.c` — unconfirmed services, deferred.
- TLS/authentication in ACSE — deferred until core flow works.

---

## 5. Proposed Go package structure

```
go-mms/
├── mms.go                          # Package mms — public client API
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
│   ├── pdu/                        # MMS PDU construction and parsing
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
│   │   └── params.go              # Internal parameter encoding
│   │
│   └── invoke/                     # Invoke ID management and request correlation
│       └── tracker.go
│
└── testdata/                       # Golden frames, reference PDUs
    └── *.bin / *.json
```

### What is public

- **`mms` (root package):** `Client`, `Dial`, `DialOptions`, `ConnectParams`, `ReadResult`, `WriteResult`, `NameListResult`, `IdentifyResult`, `StatusResult`. This is the primary user-facing API.
- **Value types:** `Value`, `TypeSpec`, `ObjectName`, `DomainID`, `DataAccessError` — strong types for MMS data.
- **Error types:** Sentinel errors, `ServiceError`, `ProtocolError`, `DecodeError`.

Note: a public `mmstest` helper package is deferred. It is not needed up front and can be introduced later if downstream consumers require test scaffolding.

### What stays internal

- **`internal/codec`:** MMS-specific marshal/unmarshal wrappers built on `encoding/asn1`. Not part of the public contract.
- **`internal/asn1util`:** Thin helpers for stdlib gaps (e.g., CHOICE dispatch, raw tag inspection, MMS-specific tag constants). Minimal by design — only what `encoding/asn1` cannot express.
- **`internal/pdu`:** Wire-level PDU construction and parsing. Uses `internal/codec` and `internal/asn1util`. Users never see these.
- **`internal/acse`, `internal/session`, `internal/presentation`:** ISO upper-layer handling. Users don't know these exist.
- **`internal/isostack`:** Orchestrates the full ISO stack for connection establishment.
- **`internal/invoke`:** Invoke ID allocation and outstanding-call tracking.

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

### Phase 7 — Optional server-side roadmap

**Goals:** Define the path for MMS server support.

**Deliverables:**
- Design document for server API.
- `mms.Server` type with handler registration.
- ISO stack server-side orchestration in `internal/isostack`.
- Server-side initiate, read, write, name list, identify, status handlers.
- InformationReport (unconfirmed) support.

This phase is not planned in detail yet. It will be scoped after the client path is proven.

**Done criteria:** Defined at scoping time.

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

### P2 — Defer until later

| Path | Why |
|---|---|
| `sources/mms/iso_mms/client/mms_client_get_var_access.c` | Phase 5 |
| `sources/mms/iso_mms/client/mms_client_named_variable_list.c` | Phase 5 |
| `sources/mms/iso_mms/client/mms_client_files.c` | Deferred file services |
| `sources/mms/iso_mms/client/mms_client_journals.c` | Deferred journal services |
| `sources/mms/iso_mms/server/*` | Server-side — Phase 7 |
| `sources/mms/iso_server/*` | Server-side — Phase 7 |
| `sources/mms/iso_mms/asn1c/*` | Reference only — consult for tag values and PDU structure when implementing codecs |
| `sources/mms/iso_cotp/cotp.c` | Likely replaced by `otfabric/go-cotp` |

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
| `_examples/` | 6 | Example programs |
| GoDoc | 6 | Complete public API documentation |
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
