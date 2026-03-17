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

### Key design decisions locked in

- **Value accessors:** `(T, bool)` pattern — no panics for type mismatch.
- **DialOptions:** Layered by responsibility (`TransportOptions`, `ISOOptions`, `MMSOptions`).
- **Logging:** `*slog.Logger` (stdlib), nil = disabled. No custom `Logger` interface.
- **All enum types:** Have stable `String()` methods.
- **Error hierarchy:** Sentinel errors + typed structs with `Unwrap()`.

---

## Phase 1 — ASN.1 / MMS codec feasibility

**Status: NOT STARTED**

Next step: Prove `encoding/asn1` feasibility with Initiate and a confirmed-service PDU family.

---

## Phase 2 — ISO upper-layer internals

**Status: NOT STARTED**

---

## Phase 3 — MMS initiate, identify, status

**Status: NOT STARTED**

---

## Phase 4 — Read and write

**Status: NOT STARTED**

---

## Phase 5 — Name list, variable access attributes, named variable lists

**Status: NOT STARTED**

---

## Phase 6 — Hardening, fuzzing, interop

**Status: NOT STARTED**
