# Security Policy

## Supported releases

Security issues are addressed in the latest v1 patch release.

## Reporting a vulnerability

Please report security vulnerabilities **privately** rather than opening a
public GitHub issue.

Send a report to: **security@otfabric.io**

Include:

- A description of the vulnerability and its potential impact.
- Steps to reproduce or a minimal proof-of-concept.
- The affected version(s).

**Expected acknowledgement:** within 5 business days.

We will coordinate a fix and disclosure timeline with you. We aim to release a
patch within 90 days of confirmation.

## Scope

go-mms is a protocol library that parses untrusted network data. The following
are in scope for this policy:

- Panics triggered by malformed MMS PDUs.
- Memory exhaustion caused by peer-controlled allocations (e.g. unbounded
  nesting, oversized arrays).
- Incorrect authentication or authorisation behaviour in the ISO association
  layer.
- Data races exposed by the race detector.
- Any defect that allows a remote peer to crash or deadlock the process.

The following are **out of scope**:

- Vulnerabilities in dependencies not maintained by this project.
- Issues requiring physical access to the machine running the library.
- Theoretical attacks with no practical exploitation path.

## Certification statement

go-mms has not received a formal security audit or certification. The
implementation includes decoder strictness hardening and race-detector
coverage, but no independent security evaluation has been performed.

TLS transport support relies on Go's standard `crypto/tls` implementation.
This is not equivalent to IEC 62351 conformance or certification.

## Disclosure policy

We follow coordinated disclosure. We request that you allow a reasonable fix
window before public disclosure. We will credit reporters in release notes
unless they prefer to remain anonymous.
