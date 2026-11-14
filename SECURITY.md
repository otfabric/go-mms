# Security Policy

## Supported releases

Security issues are addressed in the latest v1 patch release.

| Version | Supported |
|---------|-----------|
| Latest release | Yes |
| Older releases | No |

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

## Client certificates

`AuthContext.PeerCertificate` is the certificate the client presented. It
is verified only when the listener's `tls.Config` sets `ClientAuth` to
`tls.RequireAndVerifyClientCert` or `tls.VerifyClientCertIfGiven` (with
`ClientCAs`); `AuthContext.PeerCertificateVerified` reports it. An
`Authenticator` that authorises on certificate fields uses one of those modes
or checks `PeerCertificateVerified`. With `RequestClientCert` or
`RequireAnyClientCert` the certificate is not verified, and only pinning its
exact bytes or key identifies the peer.

## Restricting which hosts a client can reach

An application that limits which IEDs its users can reach enforces the
allowlist in the dialer it passes to `iso.WithDialer`, for example a
`net.Dialer` with a `Control` hook, which sees the resolved IP right before
the connection is made. Checking the host name before calling `iso.Dial`
leaves a DNS-rebinding gap. `iso.DialTCP` and `iso.Dial` are the only paths
that open a connection, and both use the dialer on every retry attempt.

## Bounding what peers hold on a server

A `Server` bounds the associations it holds (`ServerOptions.MaxAssociations`),
the named variable lists and open files peers create (`MaxPeerNVLs`,
`MaxAssociationNVLs`, `MaxNVLMembers`, `MaxOpenFiles`) and how long each of
its writes may take (`WriteTimeout`), each with a default; see
[LIMITS.md](LIMITS.md). An association or a request beyond a limit is refused
with an MMS error and changes nothing. Application callbacks that do
not return normally are recovered where the library calls them and logged with
their stack (see [API.md](API.md)); the process goes on.

A connection of `transport/iso` receives TSDUs of up to the MMS PDU size plus
the session and presentation framing, and at least 65,539 octets (the largest
session SPDU other than DATA); go-cotp refuses a longer one before it holds
it. A request larger than the negotiated PDU size is answered from its
invokeID, without decoding the rest of it. See [LIMITS.md](LIMITS.md).

## Disclosure policy

We follow coordinated disclosure. We request that you allow a reasonable fix
window before public disclosure. We will credit reporters in release notes
unless they prefer to remain anonymous.
