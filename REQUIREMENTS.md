Use this as the AI agent instruction set to generate a strong PLAN.md for go-mms.

# AI Agent Instructions — Create PLAN.md for otfabric/go-mms

You are working in the repository:

- path: `/Users/bartvanbos/Git/otfabric/go-mms`

Your task is to create a **clear, opinionated, implementation-oriented `PLAN.md`** for a brand new **native Go MMS library**.

The plan must be based on the provided **C reference implementation layout** under `sources/mms`, but the output must define a **clean Go-native design**, not a C-to-Go translation.

---

## Mission

Design a plan for building a production-ready `go-mms` library that:

- is **strictly MMS only**
- contains **no IEC 61850 domain logic**
- can be combined cleanly with:
  - `otfabric/go-tpkt`
  - `otfabric/go-cotp`
  - a reusable ASN.1 / BER package if needed
- has a **clean public Go API**
- uses **strong typing**
- has **well-defined errors with sentinels**
- has **strong logging / traceability**
- uses a **good package structure**
- is easy to test, fuzz, and evolve

The resulting `PLAN.md` should be suitable as a roadmap for serious implementation work.

---

## Critical constraints

These are non-negotiable and must be stressed explicitly in the plan.

### 1. No IEC 61850 leakage
`go-mms` must remain a **generic MMS library**.

Do **not** mix in:
- logical devices
- logical nodes
- functional constraints
- reports as IEC 61850 concepts
- control models
- datasets as IEC 61850 domain abstractions
- SCL / ICD / CID / SCD
- IED-specific naming helpers
- any `iec61850` package concerns

The plan must explicitly state that:
- IEC 61850 belongs in a **separate higher-level package/repo**
- `go-mms` should expose only **generic MMS concepts**
- any future `iec61850` package should be built **on top of** `go-mms`

### 2. Clean layering with otfabric/go-tpkt and otfabric/go-cotp
The plan must make it clear that `go-mms` is intended to sit above:

- TCP
- TPKT (`otfabric/go-tpkt`)
- COTP (`otfabric/go-cotp`)
- then MMS-related upper-layer handling

The plan should not assume custom transport code if existing packages can be reused.

The plan must discuss where to draw the line between:
- external dependencies / integration points
- `go-mms` internal responsibilities

### 3. Do not mirror the C structure blindly
The C code is a reference, not a package blueprint.

The plan must explicitly reject:
- direct file-by-file porting
- exposing C-oriented internal structure as public Go API
- leaking ASN.1 compiler artifacts into public API design
- overfitting the Go design to the layout of generated C files

Instead:
- extract concepts
- group by Go responsibilities
- design ergonomic APIs first
- keep internals private where appropriate

---

## Source material to analyze

The plan must analyze the following source tree as the baseline:

- `sources/mms/asn1`
- `sources/mms/inc`
- `sources/mms/inc_private`
- `sources/mms/iso_acse`
- `sources/mms/iso_client`
- `sources/mms/iso_common`
- `sources/mms/iso_cotp`
- `sources/mms/iso_mms/asn1c`
- `sources/mms/iso_mms/client`
- `sources/mms/iso_mms/common`
- `sources/mms/iso_mms/server`
- `sources/mms/iso_presentation`
- `sources/mms/iso_server`
- `sources/mms/iso_session`

But the plan must clearly classify what matters most **for an initial Go client-first MMS implementation**.

---

## What PLAN.md must contain

The output must be a polished `PLAN.md` in Markdown with clear sections.

It must contain at least the following sections.

### 1. Scope and non-goals
Define what `go-mms` is and is not.

Must include:
- goal: clean Go-native MMS library
- initial focus: client-side MMS first
- explicit non-goal: IEC 61850 domain logic
- explicit non-goal: GOOSE / SV / other stacks
- explicit non-goal: premature server implementation if client-first is preferred
- explicit non-goal: public APIs for session/presentation/acse unless proven necessary

### 2. Why this library exists
Explain the motivation in repo/ecosystem terms:
- native Go OT stack
- remove dependency on external C library
- fit within OTfabric ecosystem
- reuse with `go-tpkt` / `go-cotp`
- better observability, testing, packaging, and Go ergonomics

### 3. Architecture principles
This section must be strong and opinionated.

Must include principles like:
- MMS only, no IEC 61850
- composition over monolith
- clean public API, private protocol plumbing
- strong boundaries between transport, protocol, and domain
- context-first APIs where appropriate
- explicit negotiation/config over hidden globals
- deterministic request/response correlation
- strict decoding and validation
- traceability and debuggability
- zero magical stringly typed APIs unless carefully wrapped

### 4. Mapping from C code to Go responsibilities
Analyze the C tree and classify it into:
- core MMS concepts to keep
- upper-layer ISO internals to keep internal
- generated ASN.1 artifacts to treat as wire/schema reference only
- server-side pieces to defer
- pieces to ignore in phase 1

This section should explicitly identify priority areas such as:
- `inc`
- `iso_mms/common`
- `iso_mms/client`
- `asn1`
- `iso_acse`
- `iso_session`
- `iso_presentation`

And explain why.

### 5. Proposed Go package structure
This is very important.

The plan must propose a **clean Go package layout**.
Prefer a practical structure such as:

- public root package for the client-facing API
- public typed subpackages only if truly helpful
- `internal/...` for session/presentation/acse/protocol plumbing
- `internal/ber` or separate `encoding/asn1/ber` style package if justified
- internal wire/message codecs separated from high-level client operations

The plan must discuss:
- what should be public
- what should remain internal
- how to avoid a giant god package
- how to avoid exposing unstable wire-level details too early

A good plan should distinguish between:
- public client API
- public data/value/types API if needed
- internal wire codec
- internal association/session/presentation logic
- internal request correlation/state

### 6. Public API design guidance
This section must define what a good Go API should feel like.

Must strongly encourage:
- idiomatic Go naming
- `context.Context`
- explicit option structs
- typed enums using string-backed types where useful
- typed operation/service identifiers
- typed object/name/model references where useful
- minimal but expressive public surface
- stable interfaces only where they buy real value
- avoiding Java/C-style handle soup

Must discourage:
- giant mutable connection objects with too many responsibilities
- too many exported structs
- exposing generated ASN.1 structures directly
- forcing users to understand session/presentation/acse details

### 7. Typing strategy
You must explicitly stress:
- prefer **string-backed named types** where interoperability and logs benefit
- use enums/sum-style patterns where helpful
- avoid raw string parameters for important MMS concepts
- do not collapse everything into `map[string]any`
- preserve strong type information in values and service responses

Examples of topics to address:
- service names
- object classes
- variable names / identifiers
- value kinds
- error codes / service errors
- association result categories

### 8. Error strategy
This section must be detailed.

The plan must require:
- sentinel errors for major categories
- wrapped errors with context using `%w`
- typed protocol/service/decode/transport errors where appropriate
- stable `errors.Is` / `errors.As` behavior
- explicit distinction between:
  - config errors
  - transport errors
  - association errors
  - negotiation errors
  - decode errors
  - protocol violations
  - remote/service errors
  - unsupported/not-implemented cases

The plan must discourage:
- opaque error strings
- comparing error text
- mixing remote MMS service errors with local Go runtime errors

### 9. Logging and observability strategy
This must be a real section, not a one-liner.

The plan must require strong observability:
- structured logging friendly design
- correlation IDs / invoke IDs in logs
- connect / associate / initiate / request / response tracing
- optional frame or PDU debug hooks
- redaction awareness
- no noisy logging by default
- pluggable logger interface or standard logger integration strategy

The plan should discuss levels such as:
- disabled
- basic lifecycle
- protocol summary
- wire/debug trace

The plan must make debugging field interop a first-class goal.

### 10. Testing strategy
The plan must include serious testing guidance:
- unit tests for encoding/decoding
- table-driven tests
- golden frame/PDU tests
- interoperability tests against the C implementation later
- negative tests
- fuzzing for BER and PDU parsing
- boundary/length tests
- race/concurrency tests
- timeout/cancellation tests

The plan should mention that protocol parsing and association handling are high-risk areas deserving extra coverage.

### 11. Phased implementation roadmap
The plan must propose phases.

A good structure is:

- Phase 0: architecture and package skeleton
- Phase 1: BER foundations / minimal codec strategy
- Phase 2: association/session/presentation internals
- Phase 3: MMS initiate / identify / status / connection flow
- Phase 4: read / write
- Phase 5: name list / variable access attributes / named variable lists
- Phase 6: hardening, fuzzing, interop
- Phase 7: optional server-side roadmap

Each phase must define:
- goals
- deliverables
- what is intentionally deferred

### 12. Reading priorities from the C source
Include a prioritized reading plan:
- P0: must read first
- P1: read after architecture is set
- P2: defer until later

The plan should identify the most important files/folders for:
- API concepts
- client behavior
- BER / ASN.1
- association/session/presentation
- server/deferred work

### 13. Deliverables
The plan should name practical repo deliverables such as:
- `PLAN.md`
- package skeleton
- error taxonomy
- logging design note
- first client milestones
- test/fuzz backlog
- examples backlog

### 14. Explicit anti-patterns
Include a section listing what to avoid, such as:
- mixing IEC 61850 concerns into MMS
- blindly porting C structs/functions
- exposing generated ASN.1 C-equivalent types
- public APIs tightly coupled to wire representations
- weak error semantics
- unbounded logging noise
- unclear package boundaries
- giant all-knowing client objects
- premature support for every MMS service before core read/write flow is solid

---

## Guidance on interpreting the C tree

The plan must explicitly interpret the source tree like this:

### Highest-value areas for initial client-first `go-mms`
- `sources/mms/inc`
- `sources/mms/asn1`
- `sources/mms/iso_mms/common`
- `sources/mms/iso_mms/client`
- `sources/mms/inc_private`
- `sources/mms/iso_acse`
- `sources/mms/iso_session`
- `sources/mms/iso_presentation`
- `sources/mms/iso_common`

### Useful but lower priority / often internal only
- `sources/mms/iso_client`
- `sources/mms/iso_server`
- `sources/mms/iso_mms/server`

### Treat mainly as schema/wire reference, not API model
- `sources/mms/iso_mms/asn1c`

The plan must be explicit that the `asn1c` generated files are useful to understand:
- PDUs
- field names
- structure relationships
- service coverage

But they should **not** dictate public Go API shape.

---

## Style requirements for PLAN.md

The generated `PLAN.md` must be:

- concise but substantial
- opinionated
- implementation-focused
- clearly structured with headings
- easy for an engineer to execute
- not fluffy
- not generic project-management filler
- not overloaded with irrelevant background theory

Prefer:
- direct language
- concrete recommendations
- clean bullets where appropriate
- crisp phase definitions
- explicit boundaries and tradeoffs

---

## Final output requirements

Generate only the contents of `PLAN.md`.

Do not generate code.
Do not generate multiple alternative plans.
Do not ask clarifying questions.
Make the plan strong enough that an engineer can start implementation immediately.

Most importantly:
- keep `go-mms` generic
- keep IEC 61850 out
- keep the API idiomatic and strong
- keep internals internal
- design for interoperability, debugging, and long-term maintainability

If you want, I can also generate the actual PLAN.md next.