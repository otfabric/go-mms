# go-mms Releases

## v1.9.2

**Date:** 2026-11-12
**Previous release:** v1.9.1

No API change. The dialer set with `iso.WithDialer` now follows the `net.Dialer` contract (policy 1.5, API-R03).

### Fixed

- A dialer that returns neither a connection nor an error fails the dial with an error. Before, `DialTCP` and `Dial` panicked on the nil connection.
- A connection a dialer returns together with an error is closed. Before, it leaked.

### Documentation

- `iso.WithDialer`: the uses (allowlist against the resolved IP, proxy, jump host, source address, in-memory connection), one call per retry attempt with network `tcp` and the given address, the context carrying the caller's deadline and cancellation that a custom dialer must honour, the connection/error contract and error wrapping.
- SECURITY.md: an address allowlist belongs in the dialer (`net.Dialer.Control`), not in a host-name check before dialling.
- AGENTS.md names policy 1.5, the version `.otfabric/policy.yaml` pins.

### Testing

- `transport/iso/dialer_test.go`: the network, address and deadline the dialer receives, cancellation reaching the dialer, error wrapping (`errors.Is`), the nil-connection case, a connection returned with an error being closed, and a fault injected through the dialer (the peer cuts the connection after the CR).

---

## v1.9.1

**Date:** 2026-11-11
**Previous release:** v1.9.0

No API change. One internal fix; otherwise tests and CI.

### Fixed

- Integer conversions in the PDU unwrapper, the 6-octet TimeOfDay encoder and the association-refusal classification check the converted value itself. A session refusal reason above one octet (never produced by the decoder, which reads one octet) is no longer read modulo 256 when deciding whether a refusal is transient.

### Testing

- `TestRobustness_HalfCloseMidPDU`: a peer that cuts a response TPKT in half and half-closes the connection ends the request in flight and every request waiting at the negotiated max-services-outstanding-calling (1 and 3) with `ErrClosed`; `Done` closes and `Err` is set.
- `TestLifecycle_CloseWithRequestsWaitingAtLimit`: `Close` while a request is outstanding at a limit of 1 and four wait: the waiters end with `ErrClosed` and `Close` returns once the server has answered in order.
- `TestRobustness_NoFDLeak`: 40 associations ended by Close, Abort, a mid-PDU cut and a failed dial leave no socket open (counted through `/dev/fd`).
- `TestSpec_9506_2_8_6_InformationReportNestedTooDeep` no longer fails when the server has not yet registered the connection at the first Broadcast.

### CI

- The nightly chaos job failed every night since 2026-11-04: six timed tests at 10 minutes each exceeded its 40-minute test timeout. It now runs them for 5 minutes each (`CHAOS_TIMEOUT=45m`).
- The nightly fuzz job splits the 70 targets over three jobs (`FUZZ_SHARD=i/3`, new in `scripts/fuzz.sh`); one job at 2 minutes per target no longer fit its 150-minute limit.
- Shared workflows `otfabric/.github` v3.6.0 (from v3.3.1 and v3.5.0), `otfabric-policy` v1.5.0, and golangci-lint v2.15.0 in CI instead of the shared default v2.1.6.
- The interop matrix uses the iec61850-interop v1.5.0 images (contract 1.4; the MMS behaviour of the adapters is unchanged).

---

## v1.9.0

**Date:** 2026-11-11
**Previous release:** v1.8.0

The client enforces what Initiate negotiated (ISO 9506-2 clause 8.2): the number of confirmed requests outstanding and the data structure nesting level. Both were listed as not enforced in KNOWN_LIMITATIONS.md. New exported API: `ErrMaxOutstanding` and `MMSOptions.FailFastOutstanding`; no signature changes, nothing removed.

### Changed

**Outstanding requests (negotiatedMaxServOutstandingCalling)**

- The client never has more confirmed requests outstanding than `Client.Negotiated().MaxOutCalling`. A request made at the limit waits for a response to free a slot, first come first served, bounded by its context and `DialOptions.RequestTimeout`; a request whose context ends while it waits is not sent. Before this release every concurrent caller's request was sent at once.
- `MMSOptions.FailFastOutstanding` makes such a request fail at once with the new `ErrMaxOutstanding` instead of waiting.
- A request whose caller gave up after it was sent keeps its invoke ID and slot until the server answers it (the late response is discarded at Debug instead of logged as an unknown invoke ID). `Close`, `Abort` and a lost association end waiting requests with `ErrClosed`.
- The server is unchanged: it serves confirmed requests one at a time, so it never has more than one outstanding and never sends Reject max-serv-outstanding-exceeded; requests pipelined beyond the negotiated value are answered in order.

**Nesting level (negotiatedDataStructureNestingLevel)**

- The client refuses a Write, WriteObject, WriteVariables or WriteNamedVariableList whose data, and a ReadVariables, WriteVariables or DefineNamedVariableList whose AlternateAccess chain, nests deeper than `Client.Negotiated().NestingLevel`, with `ErrInvalidArgument`; nothing is sent. The server would answer such a Write with Reject max-recursion-exceeded.
- A Read, GetVariableAccessAttributes or ReadJournal response whose Data or TypeSpecification nests deeper fails with `ErrDecodeFailed` (it was accepted), and the client answers it with Reject confirmed-responsePDU max-recursion-exceeded (5) (clauses 6.4 and 8.6). An InformationReport nested deeper is not delivered and is answered with Reject unconfirmedPDU max-recursion-exceeded (3). The association continues.
- The nesting count is the one the server already applied to Write: a simple value is 0, a structure or array of simple values 1, one level per AlternateAccess selection. A negotiated level of 0 is not enforced.

### On the wire

- New client PDUs: RejectPDU `a4 06 80 01 <invokeID> 82 01 05` for a too-deep response (invoke ID below 128; longer IDs lengthen the `80` element), and `a4 03 84 01 03` for a too-deep InformationReport.
- Requests over the negotiated outstanding limit or nesting level are held back or not sent; no other encoding changes.

### Testing

- `TestSpec_9506_2_8_2_*` and `TestSpec_9506_2_8_6_*` for the limit, waiting, fail-fast, abandoned requests, Close, nesting on send and on receive, and the server's serial handling; `TestChaos_OutstandingLimit` drives 32 goroutines against a limit of 2 through chaos connections and checks the peak on the client's wire, under goleak.

---

## v1.8.0

**Date:** 2026-11-11
**Previous release:** v1.7.2

Spec review against ISO 9506-2:2003 and the IEC 61850-8-1:2011 MMS profile (A-profile, Initiate, services, data). No exported signature changes; several wire behaviours change to what the standards require, each listed under **Changed**. `make interop-all` passes against the four pinned peers (libiec61850, dscsystems, iec61850-rust, iec61850bean).

### Changed

**Association and release (ISO 8327-1, 8823-1, 8650-1; 9506-2 §8.2, Annex A.1.2; 8-1 Tables 3, 13, 109–113)**

- The server advertises its own servicesSupportedCalled (file services and obtainFile only with a `FileProvider`, readJournal only with a `JournalProvider`) and the parameter CBB proposed AND str1/str2/vnam/valt/vlis, instead of echoing the client's bit strings.
- Initiate negotiation never exceeds the proposal: an absent localDetailCalling or nesting level takes the server's own value (was 128 octets / 1). Version < 1, max-services-outstanding 0 or localDetailCalling < 1 get an Initiate-ErrorPDU; the version is min(proposed, 1). The client fails with `ErrNegotiationFailed` when the response exceeds its proposal.
- Orderly release: `Client.Close` sends FINISH + RLRQ after the Conclude-Response and waits for DISCONNECT + RLRE (`DialOptions.ReleaseTimeout`, default 2s, negative skips); the server waits for that FINISH and answers it (`ServerOptions.ReleaseTimeout`). A Conclude-ErrorPDU (now tag 0xad, §8.4.3) makes the client abort and `Close` return the `*ServiceError`; the server aborts a FINISH without a prior Conclude.
- Refusals are Session REFUSE + presentation CPR + AARE rejected with result-source-diagnostic (13/14/11 for authentication) + Initiate-ErrorPDU, instead of ACCEPT + CPA + AARE with diagnostic null. The client reports them as `*RejectionError` (still `ErrConnectionRejected`; `ErrAuthenticationFailed` for diagnostics 11–14; `IsRetryable` only for transient refusals) and still accepts the old form.
- User abort is an ARU-PPDU carrying ABRT (was TD user data). The server uses the presentation context ids the client proposes.
- PDU size per direction: the client's receive limit is its own localDetailCalling, and a request above the server's localDetailCalled fails locally with `ErrInvalidArgument`.

**PDUs and services (ISO 9506-2, 8-1 §8.1.3)**

- An unsupported service gets Reject unrecognized-service (was a ConfirmedError with an invalid vmd-state code). A malformed body gets Reject invalid-argument, an oversized request Reject value-out-of-range, a malformed PDU Reject pdu-error; a response that does not fit gets ServiceError service/pdu-size. The service error class constant is 4. One malformed PDU no longer ends the association.
- Receive accepts BER as X.690 allows (non-minimal lengths and INTEGERs, indefinite length, multi-octet tags); go-mms still sends canonical DER-style encodings.
- Nested AlternateAccess is sent as an untagged SEQUENCE OF (0x30, as libiec61850); 0xa5 is still accepted; sibling selections are refused instead of misread.
- **`NewFloat` is single precision**; use the new `NewFloat64` for a double. A server Read result uses the width of the variable's TypeSpec (IEC 61850 FLOAT32).
- Invoke IDs are 0..2^31−1 (Unsigned32); the client counter wraps there.
- Journal times are always a 6-octet TimeOfDay; times before 1984 are clamped to the epoch.
- FileName is a SEQUENCE OF GraphicString, at most 255 octets (8-1 §23.1).
- Names sent and registered are limited to 64 characters (8-1 Annex F.3.2). The Identifier character set is not enforced, because peers serve names outside it; `ValidateIdentifier` is the strict check.
- A Write whose data count differs from the named variable list is rejected; a negative BCD is refused.
- GetNameList Table 15 conflicts get object-constraint-conflict; DeleteNamedVariableList accepts every OPTIONAL component.

### Fixed

- A Reject without originalInvokeID fails the outstanding request(s) instead of leaving them hanging.
- ServiceError is decoded in full: class 0..12, additionalCode, additionalDescription, serviceSpecificInfo, modifierPosition.
- ReadJournal serves every 8-1 Table 124 option (start/stop only, numberOfEntries, listOfVariables, start + entryToStartAfter); the client accepts annotation entries (Table 125).
- GetVariableAccessAttributes never answers with a top-level typeName; TypeSpecification decoding checks class and form and skips accessControlList/meaning.
- BIT STRING length must match the declared bit count; padding bits are zeroed. GeneralizedTime accepts every X.680 §46 form.
- Session: REFUSE, version and user-requirements negotiation, Extended User Data (PGI 194), ABORT ACCEPT; selectors above 16 octets fail with `ErrInvalidArgument`. ACSE: RLRQ/ABRT parsed as extensible SEQUENCEs; EXTERNAL octet-aligned accepted. Presentation: octet-aligned PDVs accepted.
- The documentation now matches the code on Cancel, tags, lengths, Reject behaviour and limits.

### Added

- Server services InitializeJournal (`JournalInitializer`, `JournalLimit`) and GetDomainAttributes (8-1 Table 111).
- `RejectionError`; `DialOptions.ReleaseTimeout`; `ServerOptions.ReleaseTimeout`, `RespondingAPTitle`, `RespondingAEQualifier`; `NegotiatedParameters.ServicesSupportedCalled`, `ParameterCBB`.
- `NewFloat32`, `NewFloat64`, `Value.FloatWidth`; `JournalQuery`, `JournalQueryProvider`; `JournalEntry.HasAnnotation`, `Annotation`; `ServiceError.AdditionalCode`, `AdditionalDescription`, `ServiceSpecific`, `ModifierPosition`, `ServiceSpecificInfo`; `VMDLogicalStatusSupportServices`; `MaxIdentifierLength`, `ValidateIdentifier`.

### Documented

- Data `[8]` REAL is a non-standard extension (reserved in ISO 9506-2:2003) and is still sent for `NewReal`. The client does not enforce the negotiated nesting level on receive. The client still proposes libiec61850's servicesSupportedCalling bit string. MMS version 1 against the Ed2 "2.1" text (INTEROP.md).

### Tests

- `TestSpec_*` tests named after their clause (ISO 8327, 8823, 8650, 9506-2 and 8-1 tables) across the session, presentation, ACSE, isostack, serverconn, pdu, berutil, codec and root packages, with byte vectors.
- Chaos with goroutine-leak checks: `TestChaos_ReleaseRaces`, `TestChaos_ReleaseAbortRaces`, `TestChaos_RejectsKeepAssociation`.
- New fuzz targets (69 in total): `FuzzDecodeElement`, `FuzzCanonicalize`, `FuzzDecodeServiceError`, `FuzzRecoverInvokeID`, `FuzzUnmarshalInitializeJournalRequest`, `FuzzInitiatePresence`, `FuzzPresentationParseAbort`, `FuzzDecodeReleaseResponse`; fuzz-found inputs kept as corpus.
- Interop: the coverage gate gains written exemptions for services no peer adapter sends.
- Assertions that contradicted the standard were rewritten, each citing the clause in its commit.

---

## v1.7.2

**Date:** 2026-11-10
**Previous release:** v1.7.1

TimeOfDay follows the 28-bit millisecond field of ISO 9506-2:2003 §7.5.1: a field of one day or more is accepted in both forms and can be sent, and bits 28 through 31 must be zero (otfabric/go-mms#30, @pascaldekloe).

### Changed

- **A TimeOfDay (BinaryTime) millisecond field of one day or more is accepted** instead of failing the whole PDU. §7.5.1 only requires bits 28–31 to be zero. In the 4-octet form the value is the field itself (up to 2^28−1 ms, about 3.1 days), so a deadline past midnight survives; in the 6-octet form the field carries into the following days. Applies to Data values and to journal times.
- **`NewBinaryTime` encodes more values.** 0 to 2^28−1 go out in the 4-octet form (previously only below one day). Instants after 2163-06-05, up to 2^28−1 ms after its start, go out on that last day with the remainder in the millisecond field (previously an error). Every value go-mms decodes can be sent back in the form it came in.
- A received millisecond field with bits 28–31 set is rejected, as §7.5.1 requires. Such values were already rejected before, since they exceed one day.

### Tests

- Unit table of accepted and rejected encodings in both forms, each accepted one checked to re-encode to the same value and length; FuzzValueCodec regression inputs for both forms, the last instant of the 6-octet form and a set leading bit.
- e2e over TCP: a TimeOfDay rewritten in transit to the carried form reads as the same instant and writes back; one with bit 28 set fails with `ErrDecodeFailed` and the association stays usable; a 4-octet deadline past midnight and 6-octet instants after 2163-06-05 write and read back unchanged.

---

## v1.7.1

**Date:** 2026-11-09
**Previous release:** v1.7.0

### Fixed

- **`Client.Close` is bounded by its context while a request write is stalled.** A request write to a peer that stopped reading holds the write lock, and the ConcludeRequest of `Close` waited for that lock without regard to `ctx`, so `Close` could block until the connection failed. As `Abort` already did, `Close` now closes the transport when `ctx` is done, which fails the stalled write; it returns an error matching the context error. Found by the byte-fault chaos test of go-iec61850 (`DialOptions.Dialer`, otfabric/go-iec61850#59).
- **The client closes its transport when the reader loop stops on its own** (connection lost, undecodable or oversized PDU, peer release). The association could not complete another request, but the connection stayed open, so a stalled write kept the write lock; now it fails at once, and `Close` returns without waiting for its context.

### Tests

- `TestCloseBoundedByContextWhileWriteStalled` and `TestReaderExitClosesTransport` use a transport whose writes block until it is closed; both failed before the fix. The in-memory loopback transport of the tests no longer closes its send channel, so `Close` may run concurrently with `Send` as on a real connection.

---

## v1.7.0

**Date:** 2026-11-09
**Previous release:** v1.6.0

### Changed

- **Passive decoding names every MMS service.** `CapturedPDU.Service` gives the ISO 9506-2 name of every confirmed service ("Kill", "TakeControl", "GetDomainAttributes", ...), and `Service<n>` for a tag the standard does not define; before, services go-mms does not implement were named `Unknown`.
- **A confirmed PDU whose envelope does not decode but whose invoke ID does** (such as a confirmed response without a service, which some stacks send in reply to a conclude) is returned by `DecodePDU` with its invoke ID, `Raw` and `DecodeError`, so that a trace can still pair it; before, it was an error.

### Fixed

- A GetVariableAccessAttributes response with the optional `address` [1] encoded primitive (`0x81`, as some servers send it) failed with "expected typeSpecification [2] (0xa2), got 0x81". The client and the passive decoder now skip it in either form.

### Tests

- Found by comparing go-mms-trace with Wireshark over public captures (ADSC Zeek MMS traces, ITI ICS-Security-Tools): the service names, the response without a service, and the primitive address.

---

## v1.6.0

**Date:** 2026-11-09
**Previous release:** v1.5.0

### Added

- **Passive decoding of captured MMS traffic: `DecodeTSDU` and `DecodePDU`.** `DecodeTSDU` decodes one transport service data unit (the user data of the COTP data TPDUs of a TSDU, as `DialOptions.RawHook` sees it) through the session, presentation and ACSE layers to the MMS PDU; `DecodePDU` decodes an MMS PDU alone. The result names the session SPDU, the presentation PPDU, an ACSE APDU (an AARE's result; an AARQ's authentication mechanism, never its credentials) and the MMS PDU: kind, invoke ID, service, and for Read, Write, GetNameList, Identify, GetVariableAccessAttributes, GetNamedVariableListAttributes, InformationReport, Initiate, errors and Reject the decoded content with the public types (`ObjectName`, `AccessSelector`, `Value`, `TypeSpec`, `DataAccessError`). Other services keep their content in `Raw`. Nothing is sent and no state is kept. `ErrNotMMS` marks a unit that is not a session SPDU. Requested by the trace analysis of otfabric/iec61850-explorer (milestone M4).

### Tests

- Every unit of a real association, client and server in both directions, decodes: association, Identify, Read with and without alternate access, Write, GetNameList, GetVariableAccessAttributes, a named variable list defined and inspected, a refused file service, an InformationReport and the conclude (unit and e2e over TCP). Release and abort, an AARQ with a password (reported as `password`, not decoded), a rejecting AARE, list-name reads, index and range selectors, write failures, reject, cancel, unmodelled services, and malformed bodies of every modelled service. `FuzzDecodeTSDU` (8 million inputs locally, no failure). `decode.go` is fully covered.

---

## v1.5.0

**Date:** 2026-11-08
**Previous release:** v1.4.0

### Added

- **`DialOptions.AllowedServices`: an allow-list of confirmed services for a client.** A request for a service that is not listed is refused before it reaches the transport: nothing is sent, the association stays usable, and the call returns an error matching the new sentinel `ErrServiceNotAllowed` (the `Metrics` request event carries the same error). Nil (the default) allows every service, as before; an empty non-nil slice refuses every confirmed request. Initiate, Conclude and Abort are association control and are never refused. It makes a client read-only in the library rather than by convention in the caller; go-iec61850 passes it through `DialOptions.MMS`. Requested by otfabric/iec61850-explorer, which must not send a state-changing service in its default mode.

### Tests

- A client allowed only Read reads a variable, is refused Write and Identify without a byte reaching the transport or a write reaching the server, keeps the association, and reports both refusals as request events; an empty list refuses Read and still concludes; a nil list writes as before.

---

## v1.4.0

**Date:** 2026-11-04
**Previous release:** v1.3.2

### Changed

- **A client now sends the ISO addressing IEC 61850 IEDs expect.** It used to omit every selector and the TPDU size unless configured, and an IED that requires them (an ABB REB500, otfabric/go-iec61850#28) refused the COTP CR with a Disconnect Request, reason 3 (address unknown). Defaults, the values libiec61850 sends:
  - COTP CR: calling and called T-selector `00 01`, TPDU size 1024 (0xC0 = 0x0A).
  - Session: calling and called S-selector `00 01`. Presentation: calling and called P-selector `00 00 00 01`.
  - ACSE: calling AP-title `1.1.1.999`, called AP-title `1.1.1.999.1`, AE-qualifier 12 each.

  A nil option takes the default; a non-nil empty selector or AP-title omits the parameter, as before (`iso.WithCalledTSelector([]byte{})`, `ISOOptions{LocalAPTitle: mms.APTitle{}}`, ...). `iso.WithMaxTPDULength(65531)` omits 0xC0 again. IEC 61850-8-1 mandates none of these values (§6.2.3.2.2 limits a T-selector to 4 octets; Table 132 lists them as SCL address parameters).
- A client refuses a T-selector longer than 4 octets (IEC 61850-8-1 §6.2.3.2.2) before dialing.
- A client accepts a CC whose responding T-selector differs from the called one (X.224 §13.3.4), for example an IED answering `0001` with `00000001` (go-cotp v1.3.0).
- An `iso.Listener` returns the CR's T-selectors in the CC, as libiec61850 servers do. It still accepts any called selector unless `WithCalledTSelector` is set; the strict comparison is byte for byte, so `0001` and `00000001` differ.

### Added

- `iso.WithMaxTPDULength`, `iso.DefaultMaxTPDULength` (1024), `iso.MaxTSelectorLength` (4).

### Tests

- The CR a default client sends, byte for byte; selectors of 1, 2 and 4 octets, omitted selectors, TPDU sizes 1024, 2048 and omitted; a 5-octet selector refused before dialing; a differing responding selector accepted; a Listener echoing absent, 1-, 2- and 4-octet selectors; a strict Listener accepting a default client and refusing an omitted, 4-octet or different called selector with reason 3; `DialOptions.Transport` selectors used by `Dial`.
- Upper-layer defaults, configured and omitted values, and the default calling AP-title and AE-qualifier decoded by a go-mms server.

### Dependencies

- go-cotp v1.3.0.

---

## v1.3.2

**Date:** 2026-11-04
**Previous release:** v1.3.1

### Fixed

- Registering a named variable, named variable list or domain re-sorted the whole name list each time, so building a server model of n variables cost O(n² log n): a 30 000-variable domain took 1.9 s, and a large IEC 61850 IED model several seconds (tens of seconds under the race detector). Names are now inserted at their sorted position (30 000 variables: 0.08 s). GetNameList order is unchanged.

---

## v1.3.1

**Date:** 2026-11-04
**Previous release:** v1.3.0

### Fixed

- v1.3.0 refused to encode the zero `time.Time` as a UtcTime, but `TypeSpec.DefaultValue` used it for every UtcTime, also inside structures and arrays. A server reading an unwritten variable that held a UtcTime (for example an IEC 61850 `Mod` with its `t`) answered with object-access-unsupported. The zero `time.Time` now encodes as the epoch, the all-zero UtcTime of an unset timestamp, and `DefaultValue` returns the epoch. Instants before 1970 or after 2106-02-07 are still rejected.
- `DefaultValue` of an ObjectIdentifier was an empty OID, which cannot be encoded; it is now `0.0`, the shortest valid one.

---

## v1.3.0

**Date:** 2026-11-04
**Previous release:** v1.2.4

### Breaking

- Requires Go 1.26 (was 1.23). Built on go-cotp v1.2.0 and go-tpkt v1.1.0. CI tests Go 1.26 and 1.27.

### Fixed

- An association whose session CONNECT or ACCEPT carried more than about 230 bytes of user data (for example an AARQ with a long ACSE password) was sent with a one-octet length indicator and could not be parsed by any peer, go-mms included. SPDUs now use the extended length form of ISO 8327-1 up to 65535 bytes, and the parser reads it.
- Invoke IDs are Unsigned32: a negative or wider invoke ID from a peer was wrapped and could match another outstanding request; it is now rejected (the server answers with a Reject). Invoke IDs from 2^31 upwards, which carry a 0x00 pad octet, were refused and are now accepted.
- A `UtcTime` value outside 1970-01-01..2106-02-07, including the zero `time.Time`, was written with wrapped seconds; encoding now fails with an error.
- BER lengths of 16 MiB or more were truncated to three octets; integers outside the int32 range were truncated; context tags above 30 lost their tag number. All now encode correctly.
- Negative alternate-access indices, DataAccessError codes and object classes, and file sizes outside Unsigned32, are rejected instead of wrapped.

### Changed

- Interop CI pins the iec61850-interop v1.1.1 images (same peer stack commits and `mms-v2` fixture as v1.0.1); shared workflows at v3.3.1.
- Retry jitter is drawn from crypto/rand. Request writes that outlive the caller's context keep its values (`context.WithoutCancel`).
- Tests: end-to-end long-password associations, a chaos test of large associations under fragmentation, invoke-ID range tests, and three new fuzz targets (BER integer and length round trips, session SPDU round trip); 60 fuzz targets in all.

---

## v1.2.4

**Date:** 2026-10-31
**Previous release:** v1.2.3

### Changed

- Tests, CI and documents only. The coverage variable `COVERAGE_MIN` is now `COVER_PKG_MIN`. Compiled binaries are gone from git history, and tags were rewritten.

---

## v1.2.3

**Date:** 2026-10-31
**Previous release:** v1.2.2

### Changed

- Tests, CI and documents only. The coverage variable `COVERAGE_MIN` is now `COVER_PKG_MIN`. Compiled binaries are gone from git history, and tags were rewritten.
- Built on go-cotp v1.1.1, go-tpkt v1.0.2.

---

## v1.2.2

**Date:** 2026-10-28
**Previous release:** v1.2.1

## Summary

Interop and documentation release; the library code is unchanged. The
interop peers' repository, otfabric/mms-interop, is now
[otfabric/iec61850-interop](https://github.com/otfabric/iec61850-interop),
and iec61850bean joins the CI matrix as a fourth stack.

## Changes

### Tests

- **iec61850bean** — iec61850-interop packages iec61850bean with IEC 61850 IED adapters only. The new `TestIEDServer_*` suite drives the go-mms client against `iec61850bean-ied-server` over plain MMS (association, name lists, data set attributes, variable access attributes, fixture reads, writes, Abort and Close). Its quirks are asserted, not skipped: no Identify and no Conclude response, and a read of an unknown name disconnects. The MMS contract suite skips for this stack.
- **iec61850-interop v1.0.1** — CI pins the v1.0.1 images (`ghcr.io/otfabric/iec61850-interop-<stack>`) for libiec61850, dscsystems, iec61850-rust and iec61850bean.

### Docs

- INTEROP.md, COMPATIBILITY.md and interop/README.md use the new repository and image names and list iec61850bean. Earlier entries in this file keep the image names they were published with.

---

## v1.2.1

**Date:** 2026-10-28
**Previous release:** v1.2.0

## Summary

Moves to go-cotp [v1.1.0](https://github.com/otfabric/go-cotp/releases/tag/v1.1.0),
which arms read and write deadlines per direction, and removes the go-mms
workaround for the shared deadline of go-cotp v1.0.4. No API changes.

## Changes

### Changed

- **go-cotp v1.1.0** (go-tpkt stays at v1.0.1). `transport/iso` no longer wraps the connection given to go-cotp (`splitDeadlineConn` and its per-direction deadline handling are removed); `Receive` passes its context to go-cotp, `Send` passes the deadline but not the cancellation of its context, so a cancelled request still never abandons a TSDU half-written.
- **Receive with an expired context** — a context that ends before any octet of the next TSDU arrived returns the context error and leaves the connection usable (it used to abort it); one that ends mid-TSDU aborts the connection. Errors caused by a context match `context.Canceled` / `context.DeadlineExceeded` without being wrapped twice. The MMS client reader and the server association timeout behave as before (`ErrClosed`, `ErrAssociationFailed` + `context.DeadlineExceeded`).

### Docs

- TIMEOUT_AND_CLOSE.md and RACE_NOTES.md describe go-cotp's per-direction deadlines; the go-cotp entry is removed from KNOWN_LIMITATIONS.md.

---

## v1.2.0

**Date:** 2026-10-28
**Previous release:** v1.1.0

## Summary

Operability and quality release. Errors are classified so `errors.Is` /
`errors.As` work across every failure path; a pluggable `Metrics` interface,
configurable retry with backoff and jitter, `Server.Shutdown`, association and
handshake timeouts and `Client.Done` / `Client.Err` are added. A cancelled
request no longer tears down its association, and a silent peer can no longer
block `iso.Listener.Accept`. Fuzzing found and this release fixes several
codec bugs (Cancel PDU tags, BinaryTime, UTCTime fractions, long-form tags).
Every package now has ≥90% test coverage, enforced in CI. All API changes are
additive.

Interop runs against three MMS peers from the mms-interop
[v0.3.0](https://github.com/otfabric/iec61850-interop/releases/tag/v0.3.0) images,
pinned by digest: libiec61850 (120 pass), dscsystems/go-iec61850 (112 pass,
8 skipped peer limitations) and iec61850-rust (120 pass).

## Changes

### Added

- **Errors** — `ErrServerClosed`, `ErrInvalidArgument`; `PhaseSession`, `PhasePresentation`, `PhaseACSE`, `PhaseMMS`; `RejectType` (with `String`) and `RejectError` for MMS Reject PDUs (it unwraps to the previous `*ProtocolError`, so `errors.Is(err, ErrProtocol)` still holds); `ServiceError.CodeName()` (ISO 9506-2 code names). Failures carry sentinel kinds without changing their messages: association → `ErrAssociationFailed`, negotiation → `ErrNegotiationFailed`, connection loss → `ErrClosed` (+ `ErrDecodeFailed` / `ErrInvalidPDU`), request deadline → `ErrInvokeTimeout` + `context.DeadlineExceeded`, argument validation → `ErrInvalidArgument`.
- **Metrics** — `Metrics` interface (`ObserveRequest`, `ObserveConnection`), `NopMetrics`, `Role`, `Service`, `RequestEvent`, `ConnectionState`, `ConnectionEvent`; set via `DialOptions.Metrics` and `ServerOptions.Metrics`. OBSERVABILITY.md has a Prometheus adapter example.
- **Retry** — `RetryPolicy` (attempts, exponential backoff, jitter, `Retryable`, `OnRetry`), `DefaultRetryPolicy`, `IsRetryable`, `Retry`; `iso.WithRetry` retries `DialTCP` and `Dial` (connect, TLS, COTP and association), honouring the context.
- **Lifecycle** — `Client.Done`, `Client.Err`, `DialOptions.RequestTimeout`, `Server.Shutdown(ctx)` (closes listeners, waits for in-flight handlers, aborts associations; `Serve` / `ListenAndServe` then return `ErrServerClosed`), `ServerOptions.AssociationTimeout`, `iso.WithHandshakeTimeout`, `iso.WithDialer`.
- `RawHookSend` / `RawHookRecv` constants for the raw PDU hook direction.

### Changed

- **Request cancellation** — a request whose context ends mid-write returns at once; the write completes in the background (an unstarted one is skipped) instead of closing the association.
- **Association timeout** — `Serve` aborts an association that has not completed its handshake within 30s by default (`ServerOptions.AssociationTimeout`; negative disables). `Serve` also closes the transport when its context is cancelled.
- **iso.Listener** — TLS and COTP handshakes run concurrently (up to 64 pending) with a 10s default timeout, so a peer that connects and stays silent no longer blocks other connections. Temporary accept errors (such as EMFILE) are retried with backoff.
- **`ServiceError.Error()`** includes the code name, e.g. `(object-non-existent)`. Compare with `errors.As` and the `Class` / `Code` fields, never the string.
- **Server error mapping** — a `*DataAccessError` returned by a Read/Write callback is sent with its code (other errors: temporarily-unavailable, as before); a `*ServiceError` from a handler is sent as returned; a malformed Status request gets the invalid-request service error.
- **`Value.Equal`** compares floats bitwise (all NaNs equal, `+0` ≠ `-0`).
- Logs carry `role` and `remote_addr`; requests are logged at debug level with `service`, `invoke_id`, `duration` and `error`. Messages are prefixed `mms:` / `iso:`.

### Fixed

- **Cancel PDUs** — cancel-Request/Response/Error use tags `[5]`, `[6]`, `[7]` (the tag constants were wrong).
- **BinaryTime** — values below one day encode in the 4-octet TimeOfDay form (previously not re-encodable after decoding); the decoder rejects a millisecond-of-day ≥ 86 400 000.
- **UTCTime** — the fraction of a second round-trips losslessly.
- **GeneralizedTime** — a fractional second accepted on decode is kept on encode (it was dropped); whole seconds encode as before.
- **Long-form tags** — MMS PDUs with a long-form outer tag are rejected; tag numbers above 30 encode correctly.
- **Deadline classification** — an I/O timeout caused by a context deadline is reported as `context.DeadlineExceeded` even when the connection deadline fires just before the context's timer (association timeout, ISO transport).
- **Full-duplex deadlines** — go-cotp v1.0.4 arms a both-direction deadline around each read and write; a write deadline could cut a concurrent read (`read tpkt header: i/o timeout`). transport/iso now uses per-direction deadlines.
- **`Client.Abort`** honours its context; closing a transport whose deadline already fired no longer returns a timeout error.
- Server callback errors that are not wire errors no longer reach the error-code encoder (possible panic path).

### Tests

- Coverage ≥90% in every package and in total, enforced by `scripts/coverage-gate.sh` (`make coverage`, CI quality job).
- `internal/chaos`: fault-injecting `net.Conn` / `net.Listener` (delays, chunked I/O, bandwidth limit, drops, resets, stalls). `e2e` chaos tests (lossy, drops with reconnect via `Retry`, slow peer) and a concurrent stress test under `-race`; `make chaos`.
- 57 fuzz targets, including `FuzzValueCodec`, `FuzzServerDispatch`, `FuzzServerSession` and new session/presentation/ACSE, codec and BER targets; `scripts/fuzz.sh` discovers them (`make fuzz`, `make fuzz-short` in `make check` and CI).
- Interop is stack-parametrized (`MMS_INTEROP_STACK`, `make interop-all`): one test body per scenario, run in CI against libiec61850, dscsystems/go-iec61850 and iec61850-rust from mms-interop v0.3.0. Peer limitations are explicit per-stack quirks or skips with reasons (INTEROP.md). New `TestClient_PeerDisconnect` (Done/Err) and `TestServer_Shutdown`.

### Docs

- ERRORS.md, OBSERVABILITY.md, TIMEOUT_AND_CLOSE.md, API.md, README.md, LIMITS.md, KNOWN_LIMITATIONS.md, COMPLIANCE.md, INTEROP.md, RACE_NOTES.md and RELEASE_CHECKLIST.md updated; PROGRESS.md replaced by a current-status summary. Removed stale PLAN.md, REQUIREMENTS.md, extract_c_functions.py and mms_functions.csv.

---

## v1.1.0

**Date:** 2026-10-28
**Previous release:** v1.0.7

## Summary

Fixes FileDirectory against conforming servers ([#2](https://github.com/otfabric/go-mms/issues/2))
and fifteen further wire and behaviour bugs. All of them were found by
extending the tests to the whole public API: a new `e2e` package runs every
exported method between a go-mms client and server, and the libiec61850
interop suite now covers files, journals, reports, Status, alternate access,
every NVL scope and both abort directions. Both suites fail when a new public
method has no test. No exported API is added or removed; some error codes and
edge behaviours change (see Changed).

Requires the mms-interop v0.2.0 adapters (files, journals, reports, Status,
alternate access, abort). CI pulls
`ghcr.io/otfabric/mms-interop-libiec61850@sha256:0ce17d8471eda3a3ef8949a5819b44359a2aeaec23695fee06c5dbea083f0391`
from that [release](https://github.com/otfabric/iec61850-interop/releases/tag/v0.2.0).

## Changes

### Fixed

- **FileDirectory** ([#2](https://github.com/otfabric/go-mms/issues/2)) — `listOfDirectoryEntry [0]` is an EXPLICIT tag; the server now encodes the inner SEQUENCE OF, and the client accepts both forms.
- **FileDelete request** — the file name is the direct content of `[76] IMPLICIT FileName`; the extra wrapper made libiec61850 reject the request.
- **ReadJournal response** — entries include the mandatory `originatingApplication`; the client skips it on decode.
- **TypeDescription objId** — the tag is `[15]`, not `[8]` (which does not exist). The server can now describe and register GeneralizedTime `[11]`, BCD `[13]` and ObjectIdentifier `[15]` variables; it previously failed to encode their type.
- **OBJECT IDENTIFIER** — first-subidentifier decoding follows X.690 §8.19.4 (first arc 2 with a second arc ≥ 40 decoded wrongly); invalid arcs are rejected on encode.
- **BinaryTime** — instants outside 1984-01-01…2163-06-05 fail to encode instead of silently wrapping the 16-bit day count. Applies to `NewBinaryTime` values and journal start, stop, after and occurrence times.
- **FileOpen `initialPosition`** — the server ignored it; reads now start at the requested offset, and a position past the end returns File / position-invalid.
- **Server responses and the negotiated PDU size** — FileRead, FileDirectory, GetNameList (including journals) and ReadJournal responses are sized to the negotiated maximum; lists continue on the next request with `moreFollows`. A response that cannot fit returns ServiceError Resource / other instead of an oversized PDU.
- **PDU size checks** — client and server measured the session/presentation-framed frame instead of the MMS PDU, rejecting valid PDUs close to the limit.
- **Client hang** — a request issued while the reader loop was exiting could wait until its context expired; it now fails immediately.
- **Peer abort / release** — when the peer aborts or releases the association, or the connection drops, pending and later calls fail with an error matching `ErrClosed`.
- **`ServerConn.Abort`** — sends ACSE ABRT and now also closes the connection; previously the association stayed open.
- **`iso.Listener.Accept` cancellation** — returns as soon as the context is cancelled instead of after the next 500 ms poll.
- **Unknown file times** — a zero `FileEntry.LastModified` or `FileAttributes.LastModified` is omitted from FileDirectory and FileOpen responses (the field is OPTIONAL). It was sent as `00010101000000Z`, which libiec61850 clients read as 18446744073709551616 ms.
- **Empty / oversized alternate access** — a File/Read/Write `[5]` with no selectors, an empty component name, or a component longer than 1024 bytes is rejected (libiec61850 1.6.2.1 LIB61850-572 / LIB61850-566). `FileDirectoryAll` already stops on `moreFollows` with no entries (LIB61850-579).

### Changed

- **Unknown domain or variable in Read/Write** — the server answers DataAccessError `object-non-existent` (10) instead of `object-undefined` (4), matching libiec61850. Alternate-access mismatches keep `object-undefined`.
- **GetNameList of an unknown domain** — the server returns ServiceError Access / object-non-existent instead of an empty list. Journal name lists are still delegated to the `JournalProvider`.

### Tests

- **`e2e/`** (runs in `go test ./...`) — every exported `Client`, `Server`, `ServerConn` method and `Value` constructor exercised between a go-mms client and server over ISO transport, including paging under small PDU sizes, both device directory conventions and entries without `lastModified` (checked on the wire), authentication, per-association variables, reports, UnsolicitedStatus and both abort directions. `TestAPICoverage` fails when a method or constructor has no test.
- **`interop/`** — 117 tests against libiec61850 `v1.6.2.1-89-g6e265c54` in both directions (from 53). `TestInteropCoverage` fails when a public method or a dispatched server service has neither an interop test nor a documented exemption. Peer limitations (listOfVariable reports, FileOpen initial position, bulk NVL delete, ObtainFile role reversal) are asserted, not skipped.
- Unit tests for the fixed encodings, and libiec61850-encoded wire vectors in `internal/pdu/peer_vectors_test.go`.

### Docs

- API.md: File and Journal signatures, directory listing conventions seen on devices (flattened `/` paths vs per-level listings with `dir\` entries) and optional `lastModified`; the same on the `FileDirectoryEntry` godoc.
- INTEROP.md matrix and coverage guard, COMPLIANCE.md interop column, KNOWN_LIMITATIONS.md (BinaryTime range, Real/BooleanArray type descriptions, OID arcs, ObtainFile role reversal, client UnsolicitedStatus), ERRORS.md (`ErrClosed`, unknown-object codes).

---

## v1.0.7

**Date:** 2026-07-30
**Previous release:** v1.0.6

## Summary

Patch release: bump go-cotp to v1.0.4 (pulls go-tpkt v1.0.1). No public API
surface or behavioural changes.

## Changes

### Dependencies

- **go-cotp** — `v1.0.3` → **v1.0.4**
- **go-tpkt** (indirect) — `v1.0.0` → **v1.0.1**

### Unchanged

- No API, wire, or behavioural changes. Import path remains `github.com/otfabric/go-mms`.

---

## v1.0.6

**Date:** 2026-07-30
**Previous release:** v1.0.5

## Summary

Documentation clarification for logger configuration and outstanding-request /
concurrency behaviour, a go-cotp bump, a small ReadJournal empty-name guard,
and a large unit-test coverage pass (non-example ~**94%**). No public API
surface changes.

## Changes

### Changed

- **Logger docs** (OBSERVABILITY.md, DialOptions/ServerOptions godoc) — MMS
  (`DialOptions.Logger` / `ServerOptions.Logger`) and ISO (`iso.WithLogger`)
  are independent; neither inherits from the other. Fixed `iso.Listen` example
  signature (`Listen(addr, opts...)`).
- **Outstanding / concurrency docs** (`doc.go`, Client godoc, LIMITS.md,
  KNOWN_LIMITATIONS.md, RACE_NOTES.md, API.md) — write serialization vs
  multi-outstanding wait clarified; negotiated MaxOutstanding* defaults are
  **5** (not 8); values are proposed/negotiated and exposed but **not**
  enforced as a runtime pending-request limit. Removed incorrect
  “one in flight / pipelining unsupported” claims.
- **API.md DialOptions** — aligned with `options.go` (`RawHook`, ISO field
  names, `Authenticate` on server).
- **README** — documentation links cleaned up.
- **ReadJournal** — reject empty journal identifier when marshaling
  time-range / start-after requests (`encodeJournalName`).

### Dependencies

- **go-cotp** — `v1.0.2` → **v1.0.3** (docs-only upstream release).

### Build / tooling

- **Makefile** — exports `GOWORK=off`.

### Tests

Broad coverage lift across client, server, and internal packages (non-example
module coverage ~**94%**). Notable additions:

- **Client** — reader loop / dispatch / info-report conversion; `Read` /
  `Write` / `ReadVariables` / `WriteVariables` / convenience accessors;
  NVL / GetNameList / GetVariableAccessAttributes / Define·Get·Delete NVL
  (including DeleteAllDomain/VMD) protocol-mismatch, closed-client, and
  validation edges; wire helpers (`typeSpecFromWire`, `valueToDataValue`,
  scopes).
- **Server** — `ListenAndServe`, Identify/Status/GetNameList/Read/Write/NVL
  and file-service handler edges; alternate-access read/write/patch helpers
  at full coverage.
- **ISO / stack** — `iso.WithClientDialOptions` / `WithLogger` / `RemoteAddr` /
  `isClosed`; isostack associate/data/release decode errors (session,
  presentation, context, APDU type); presentation parse edges; session PGI
  length helpers.
- **ACSE / BER / ASN.1** — AARE/AARQ/EXTERNAL/RLRQ/ABRT and OID marshal
  edges; berutil / asn1util long-form length and high tag-number encodings.
- **PDU / codec** — initiate/status/cancel/choice/mmspdu classify paths;
  Read/Write (list-of-variable, alternate access, list-name) marshal error
  matrix; ReadJournal request/response parse edges; TypeSpec / NVL /
  alternate-access decode; `server_helpers` request/response
  marshal+unmarshal error matrix (fully covered).
- **servermodel / serverconn** — stale NVL order cleanup; abort/cancel
  handling.

### Unchanged

- Silent-by-default `slog` / discard handler behaviour.
- Invoke tracker and sendMu implementation (docs corrected to match code).

Import path remains `github.com/otfabric/go-mms`.

---

## v1.0.5

**Fix**: 32-bit / linux armv7 builds failed because `fmt.Errorf` arguments used the untyped constant `math.MaxUint32` (does not fit in 32-bit `int`). Cast to `uint32` in `internal/pdu` file open/directory size error messages.

- Added Makefile `test-armv7` (compile `./internal/pdu` for `GOOS=linux GOARCH=arm GOARM=7`) and wired it into `make check`.

No API changes. No breaking changes.

Import path remains `github.com/otfabric/go-mms`.

---

## v1.0.4

Documentation and code-quality hardening. No API changes, no behavior changes.

### Dependencies

- Bumped `github.com/otfabric/go-cotp` from **v1.0.1 → v1.0.2** (documentation and lint hygiene; no API or behavior changes).

### Documentation — complete public API reference covering `Client`, `Server`, value constructors
  and accessors, `TypeSpec`, `ObjectName`, access selectors, all error types, transport interfaces,
  dial/server options, authentication, and deprecated symbol table.
- Added link to `API.md` as the first entry in the `README.md` Documentation section.
- Removed blanket `errcheck` and `staticcheck SA2002` exclusions from `.golangci.yml`; all
  underlying code issues were fixed instead (see below).

### Linting and code quality

- **`golangci-lint` exclusion-free**: removed the blanket `errcheck` suppression for test files
  and the dead `SA2002` test-file exclusion.
- Fixed ~200 unchecked error returns across all test files:
  - Cleanup calls (`client.Close`, `ln.Close`, `tr.Close`, goroutine `srv.ListenAndServe`, etc.)
    now use explicit `_ = ...` or `defer func() { _ = ... }()` to document intentional dismissal.
  - Setup calls (`srv.RegisterVariable`) in `altaccess_test.go` and `nvl_value_test.go` now check
    errors with `t.Fatal`.
- Fixed real `SA2002` issue in `concurrency_test.go`: `mockServer` methods called from goroutines
  replaced `s.t.Fatalf` with `s.t.Errorf` + `return` (goroutine-safe test failure reporting).
- Added `vuln` target (`govulncheck`) to `Makefile` and wired it into `make check`.

No API changes. No breaking changes.

Import path remains `github.com/otfabric/go-mms`.

---

## v1.0.3

**Added**: Transport fault injection tests (`transport/iso/fault_test.go`).

Five new tests covering real-world transport failure scenarios:

- `TestFault_MidResponseDisconnect` — server closes TCP mid-response; client Read returns an error quickly rather than hanging indefinitely.
- `TestFault_TruncatedTPDU` — client sends a TPKT header announcing 100 bytes but delivers only 3 then closes; server recovers without panic or deadlock.
- `TestFault_IdleConnectionClosure` — server closes an idle connection; next client operation detects the dead peer.
- `TestFault_ConnectToClosedPort` — `iso.Dial` returns an error on connection refused rather than hanging.
- `TestFault_ConcurrentRequestAfterDisconnect` — 8 concurrent goroutines all receive errors after confirmed disconnect; none hang indefinitely.

**Race detector**: `go test -race ./...` passes cleanly.

---

## v1.0.2

**Fixed**: `Server.handleWrite` now propagates specific `DataAccessError` codes from write handlers.

Previously, any error returned by a variable write handler that was not a `*DataAccessError` was mapped to `TemporarilyUnavailable`. A `*DataAccessError` with a non-`None` code was also silently coerced to the same value. Write handlers (e.g. those installed by `go-iec61850`'s `ReportEngine.HandleRCBWrite` or `RegisterControl`) can now return precise codes such as `DataAccessErrorObjectAccessDenied` or `DataAccessErrorTypeUnsupported`, which are carried verbatim to the client in the `WriteResponse`.

**Added**: Invoke-ID edge-case tests in `internal/invoke/tracker_test.go`.

New unit tests covering correctness at the boundary of the `Tracker` protocol:
- `TestInvokeIDWraparound` — ID counter wraps from `0xFFFFFFFF` through `0` (skipped) to `1`; ID `0` is never issued.
- `TestOutOfOrderResponses` — responses completed in reverse allocation order are each delivered to their correct channel.
- `TestResponseForUnknownInvokeID` — `Complete` on an unknown ID is a no-op, not a panic.
- `TestLateResponseAfterCancel` — a `Complete` that races with `Cancel` is safely discarded.
- `TestConcurrentAllocate` — 100 goroutines allocate concurrently; all receive unique, non-zero IDs.

---

## v1.0.1

**Added**: `Server.SetVariableRead(domain, itemID, fn)` — replaces the read handler of a previously registered domain-scoped variable at runtime. Intended for post-registration configuration such as installing control-model-specific read handlers on CO attributes (e.g. `SBO[CO]` for SBO-normal controls) after the server model has been fully built.

---

## v1.0.0

**Changed**: `DataAccessErrorCode` constants realigned to ISO 9506-2 wire values.

The constants were previously offset by one from the MMS ASN.1 `data-access-error` ENUMERATED. They now match the wire encoding exactly (0–11). `DataAccessErrorNone` is now `-1`, a Go-internal sentinel that is never encoded on the wire. Names have been corrected: `DataAccessErrorTypeMismatch` → `DataAccessErrorTypeUnsupported`, `DataAccessErrorObjectExists` → `DataAccessErrorObjectAttributeInconsistent`. New constants added: `DataAccessErrorObjectNonExistent` (10), `DataAccessErrorObjectValueInvalid` (11). Old names are retained as deprecated aliases.

**Changed**: `AccessResult` and `WriteAccessResult` semantics clarified.

- `AccessResult.Value == nil` is now the authoritative signal for a per-variable error (previously `ErrorCode != 0`, which silently swallowed wire value 0 = `object-invalidated`).
- `AccessResult.ErrorCode` is `DataAccessErrorNone` on success and a validated wire code on error.
- `WriteAccessResult.ErrorCode` follows the same invariant.

**Added**: `encodeDataAccessError` / `decodeDataAccessError` helpers with validation.

All paths that previously cast raw wire integers to `DataAccessErrorCode` now go through `decodeDataAccessError`, which rejects out-of-range values. The server uses `wireErrCode()` (panics on sentinel/invalid codes) to catch accidental misuse at development time.

**Fixed**: BER tag correctness across Read, Write, and NVL PDUs.

- `ReadRequest.variableAccessSpecification` is now wrapped in the `[1] EXPLICIT` outer tag (`0xa1`) as required by the MMS ASN.1 definition.
- `ReadResponse.listOfAccessResult` now uses `[1] IMPLICIT` tag `0xa1` instead of the legacy universal `SEQUENCE` (`0x30`).
- `WriteRequest.listOfData` now uses `[0] IMPLICIT` tag `0xa0` instead of `0x30`.
- `VariableSpecification.name` is now wrapped in `[0] EXPLICIT` (`0xa0`) per ISO 9506-2 §15.4.
- `DeleteNamedVariableList-Response` fields `numberMatched`/`numberDeleted` now use `[0] IMPLICIT` (`0x80`) and `[1] IMPLICIT` (`0x81`) instead of `UNIVERSAL INTEGER` (`0x02`).
- `InitiateRequestPDU` and `InitiateResponsePDU` are now encoded as bare SEQUENCE fields (IMPLICIT outer tag) rather than double-wrapped; a compatibility path accepts both forms on decode.
- `DefineNamedVariableList-Response` is now encoded as primitive (`0x8b 0x00`) rather than constructed.

**Changed**: `codec.UnmarshalInner` replaced by `codec.UnmarshalImplicitSequence` and `codec.UnmarshalExplicit`.

`UnmarshalImplicitSequence` reconstructs the `0x30` wrapper that Go's `encoding/asn1` requires when decoding an IMPLICIT SEQUENCE. `UnmarshalExplicit` handles the EXPLICIT case where `raw.Bytes` already contains the full inner TLV. The old name is gone; all call sites have been updated.

**Added**: `codec.MarshalSequenceContent` and `codec.MarshalMmsPduBareSequence`.

`MarshalSequenceContent` strips the `0x30` wrapper from an `asn1.Marshal` result, producing bare SEQUENCE fields for IMPLICIT tags. `MarshalMmsPduBareSequence` is the MMS PDU entry point for IMPLICIT-tagged PDUs.

**Added**: `pdu.MarshalIdentifyResponse`, `pdu.MarshalStatusResponse`.

Server-side marshal helpers for Identify and Status responses. Both emit bare SEQUENCE fields (IMPLICIT tag). The `server.go` and `mms_test.go` mock server now use these instead of duplicating the ASN.1 struct inline.

**Changed**: `pdu.MarshalWriteResponse` signature.

The parameter type changed from `[]int` (integer sentinel) to `[]pdu.WriteResult` (`{Success bool; Code int}`), eliminating the ambiguity between wire value 0 (`object-invalidated`) and a success sentinel.

**Fixed**: Server-side ConfirmedError access-class codes.

`svcErrObjectNonExistent` corrected from `0` to `2` and `svcErrObjectAccessDenied` from `1` to `3`, matching the `mms-extended.asn1` `AccessError` ENUMERATED.

**Added**: `ServerConnFromContext` exported helper.

Exports the previously unexported `serverConnFromCtx` so that variable read/write handlers can access the `*ServerConn` from the request context.

**Changed**: COTP transport delegated to `go-cotp` v1.0.0.

The hand-rolled COTP CR/CC handshake, TPKT framing, and `cotpTransport` implementation have been replaced by `cotp.Connect` / `cotp.Accept` and `cotp.Conn.WriteTSDU` / `ReadTSDU`. The internal `sendTPDU`/`readTPDU` helpers, `tpkt.Reader`/`tpkt.Writer`, and per-listener `sourceRef` counter are removed. Error types from handshake failures are now `cotp.ErrConnectionRefused`, `cotp.ErrHandshake`, and `*cotp.RejectionError`.

**Changed**: Dependency updates.

- `github.com/otfabric/go-cotp` v0.1.5 → v1.0.1
- `github.com/otfabric/go-tpkt` v0.1.3 → v1.0.0 (indirect)

**Added**: Bidirectional interoperability test suite against libiec61850.

`interop/` now contains a self-contained harness (`harness_test.go`) and two test files:
- `libiec61850_client_test.go` — go-mms client ↔ libiec61850-mms-server adapter: 21 scenarios covering all primitive types, array, structure, multi-variable read/write, NVL define/read/delete, negative cases, and reconnect.
- `libiec61850_server_test.go` — libiec61850-mms-client ↔ go-mms server: matching bidirectional coverage.

Tests start Docker containers from the [mms-interop](https://github.com/otfabric/iec61850-interop) adapter images, wait for a JSON readiness event, run assertions, and tear down. No pre-running containers or manual steps are required. Gated behind `-tags=interop`; run with `make interop`.

**Added**: `make interop` target and CI workflow.

`Makefile` gains an `interop` target (`go test -tags=interop -v -timeout 300s ./interop/...`). `.github/workflows/interop.yml` runs the suite on every push and PR using a pinned digest for the libiec61850 adapter image pulled from GHCR.

**Added**: `interop/testdata/interop.json` fixture.

Canonical fixture defining the `interop` domain with ten typed variables (`boolean`, `integer`, `unsigned`, `float32`, `visible-string`, `octet-string`, `bit-string`, `utc-time`, `array`, `structure`) and server identity. Synchronized with the pinned adapter image version.

**Changed**: `INTEROP.md` rewritten; `interop/README.md` updated.

Both documents now describe the Docker-based harness architecture, environment variables (`LIBIEC61850_IMAGE`, `MMS_SERVER_BINARY`, `MMS_CLIENT_BINARY`, `MMS_FIXTURE`), and the full MMS compatibility matrix (28 capabilities, bidirectional).

---

## v0.1.5

**Changed**: Open-source release under the MIT License.

- Added root `LICENSE` file (MIT, Copyright (c) 2026 OT Fabric).
- Added `// SPDX-License-Identifier: MIT` to all first-party Go source files.
- Updated README license section to reference the MIT License.

**Changed**: README improvements for public release.

- Standardized badge block (Go, pkg.go.dev, License, CI, Codecov, Release).
- Added pkg.go.dev documentation badge.
- Removed Go Report Card badge and Codecov upload token from badge URLs.
- Added table of contents.

**Changed**: Dependency updates.

- `github.com/otfabric/go-cotp` v0.1.4 → v0.1.5
- `github.com/otfabric/go-tpkt` v0.1.2 → v0.1.3

No API or behavior changes.

---

## v0.1.4

**Changed**: Increased minimum required Go version to 1.23 (was 1.21). All documentation, CI, and go.mod references updated accordingly. No code changes.

---

## v0.1.3

**Fixed**: Race condition in `Client.Close` / conclude handshake.

When the reader loop received a `ConcludeResponse`, it signaled `concludeCh` and then returned (closing `readerDone`). Because Go's `select` picks randomly among ready cases, `conclude()` could non-deterministically hit the `readerDone` case and return a spurious `"connection closed before conclude response"` error. The fix drains `concludeCh` when `readerDone` fires before reporting failure.

**Changed**: Lint configuration and code quality.

- Restored sensible test-file exclusions in `.golangci.yml` for `errcheck` and `staticcheck` SA2002.
- Fixed `godot` comment punctuation in `internal/acse` and `internal/presentation`.
- Fixed `exhaustive` switch in `internal/acse` (missing `default` case for `AuthMechanism`).
- Fixed `errcheck` in `transport/iso` (unchecked `SetDeadline` / `SetWriteDeadline` / `SetReadDeadline` return values).
- Fixed `staticcheck` ST1023 redundant type declarations in test files.
- `make check` now passes cleanly.

---

## v0.1.2

**Changed**: Lowered minimum required Go version to 1.21 (was 1.22). All documentation, CI, and go.mod references updated accordingly. No code changes.

This release ensures compatibility with Go 1.21. No new features or bugfixes are included.

---

## v0.1.1

**Changed**: Add UTCTime Quality Byte Support — completed across all 4 layers.

- PDU layer (internal/pdu/data.go): Added TimeQuality uint8 to DataValue. Updated encodeUTCTime to write the quality byte and decodeUTCTime to return it (previously hardcoded 0x00 / discarded).
- Public Value type (value.go): Added timeQuality field, UTCTimeQuality() accessor, and NewUTCTimeWithQuality(t, quality) constructor. Updated NewUTCTime(t) to default to 0x0a (matching C reference). Updated Clone, Equal, and String to handle quality.
- Wire conversion (mms.go): Updated valueToDataValue and dataValueToValue to thread the quality byte between public and internal representations.
- Public constants (value.go): Added 5 named constants — UTCTimeQualityLeapSecondsKnown (0x80), UTCTimeQualityClockFailure (0x40), UTCTimeQualityClockNotSynchronized (0x20), UTCTimeQualityAccuracyMask (0x1F), UTCTimeQualityAccuracyUnspecified (0x1F).

---

## v0.1.0

**Changed**: N/A

Initial release.

---
