# Compatibility Policy

## Semantic versioning

go-mms follows [Semantic Versioning 2.0.0](https://semver.org/).

The module path is `github.com/otfabric/go-mms`. The v1 series is API-stable.

## What patch releases may change

Patch releases (v1.x.y → v1.x.z) may:

- Fix protocol encoding or decoding defects.
- Strengthen PDU validation and strictness.
- Add tests and documentation.
- Improve error messages.

**Important for protocol libraries:** a standards-correct wire fix can change
observable on-wire behaviour while remaining Go API-compatible. Such corrections
are made in patch releases without being classified as breaking changes.

## What minor releases may change

Minor releases (v1.x → v1.y) may:

- Add backward-compatible exported types, functions, methods, and constants.
- Add optional fields to option structs (zero-value safe where possible).
- Extend server handler interfaces with optional methods via interface
  segregation.
- Add new `DataAccessErrorCode` or similar enum values where forward-compatible.

## Breaking changes

Breaking public API changes are reserved for a future v2 major release.

## Minimum supported Go version

The minimum supported Go version tracks the two most recent stable releases.
The current minimum is recorded in `go.mod`.

## Dependency compatibility

go-mms has no public API dependency on third-party packages. The `go.mod`
indirect dependencies are internal implementation details.

## SCL and wire format

The internal wire format and SCL parsing utilities are not covered by the
compatibility guarantee. Applications should not depend on the byte-level output
of internal PDU encoding functions.

## Protocol behaviour

This library aims to be correct per the MMS standard (ISO 9506). Where the
standard is ambiguous or conflicting implementations exist, the chosen behaviour
is documented in `KNOWN_LIMITATIONS.md` and `COMPLIANCE.md`. Correcting a
deviation from the standard is not considered a breaking change.
