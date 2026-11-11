# Defensive Bounds and Resource Limits

This document lists all hard limits enforced by go-mms to protect against malformed or malicious peer input.

## PDU size

| Check | Location | Behavior |
|-------|----------|----------|
| Client receive | `mms.go` (`receiveRaw`) | Rejects PDUs exceeding negotiated `maxPDUSize` (after association only) |
| Server receive | `serverconn/conn.go` (`Serve`) | Answers a confirmed request larger than the negotiated `MaxPDUSize` with Reject confirmed-requestPDU value-out-of-range (IEC 61850-8-1 clause 8.1.3.4.3.3), any other oversized PDU with Reject pdu-error invalid-pdu; the association stays up |
| Server send | `server_pdusize.go` | A response that does not fit is truncated where the service allows it (lists with moreFollows) and otherwise answered with ServiceError service/pdu-size (IEC 61850-8-1 Table 17) |

The negotiated `maxPDUSize` is the minimum of what each side proposes during association. Before negotiation (during the initial handshake), no size limit is enforced.

## Nesting depth

| Limit | Value | Location | Purpose |
|-------|:-----:|----------|---------|
| Data nesting | 64 | `internal/pdu/data.go` | Prevents stack overflow from deeply nested arrays/structures in `DataValue` |
| TypeSpec nesting | 32 | `internal/pdu/getvaraccess.go` | Prevents stack overflow from deeply nested type specifications |

These limits apply to recursive decoding of `DataValue` and `TypeSpec` trees. The limits are enforced internally; public API signatures are unchanged.

The server also enforces the negotiated `dataStructureNestingLevel` (ISO 9506-2 clause 8.2): a Write whose data nests deeper gets Reject confirmed-requestPDU max-recursion-exceeded. The client does not check received data against the negotiated level; only the fixed bounds above apply.

## Collection sizes

| Limit | Value | Location | Purpose |
|-------|------:|----------|---------|
| Access results | 65,536 | `internal/pdu/data.go` | Caps elements in a single read/write response |
| Directory entries | 10,000 | `internal/pdu/file.go` | Caps file directory listing entries |
| Journal entries | 10,000 | `internal/pdu/journal.go` | Caps journal read response entries |
| Name list identifiers | 100,000 | `internal/pdu/getnamelist.go` | Caps GetNameList response items |

## String lengths

| Limit | Value | Location | Purpose |
|-------|------:|----------|---------|
| File name | 255 bytes | `internal/pdu/file.go` | Decoded (all elements joined with `/`) and sent FileName; IEC 61850-8-1 clause 23.1 |
| Identifier name (received) | 1,024 bytes | `internal/pdu/getnamelist.go` | Caps individual identifier strings from a peer |
| Identifier name (sent, registered) | 1..64 characters, A–Z a–z 0–9 `$` `_` | `mms.go` (`validateIdentifier`) | ISO 9506-2 clause 7.5.2, IEC 61850-8-1 Annex F.3.2 |

## BER encoding limits

| Limit | Value | Location | Purpose |
|-------|------:|----------|---------|
| Length field (PDU and service layer) | up to 2^31−1, any number of octets | `internal/berutil/element.go` | Non-minimal long forms accepted on receive (X.690 §8.1.3.5 NOTE 2); octet 0xff rejected |
| Length field (inner decoders) | 3 octets (max 16 MB) | `internal/berutil/berutil.go` | Applies after the receive path rewrites lengths to the minimal form |
| Tag field | multi-octet at the PDU and service layer; 1 octet (0–30) inside service bodies | `internal/berutil/element.go`, `berutil.go` | MMS service tags 31 and above (for example 46, 65, 67, 72–77) use the multi-octet form |
| INTEGER (sizes, counts, IDs) | 4 octets after trimming redundant leading octets | `internal/berutil/berutil.go` | Data INTEGER / Unsigned values take up to 8 octets (9 with the pad) |
| Indefinite length | Accepted on constructed encodings, at most 64 levels | `internal/berutil/element.go` | X.690 §8.1.3.6; rewritten to definite lengths on receive, never sent |

## Connection setup and timeouts

| Limit | Default | Configured by | Location | Behavior |
|-------|--------:|---------------|----------|----------|
| TLS + COTP handshake per accepted connection | 10 s | `iso.WithHandshakeTimeout` (0 → default, negative disables) | `transport/iso/listen.go` | Peer closed and logged; never blocks `Accept` for others |
| Pending handshakes per `iso.Listener` | 64 | fixed (`maxPendingHandshakes`) | `transport/iso/listen.go` | Connections handshaking or waiting for `Accept`; further connections wait in the kernel backlog |
| Temporary accept error backoff | 5 ms → 1 s | fixed | `transport/iso/listen.go` | Doubles per consecutive error, resets on success |
| Association (AARQ → Authenticate → AARE) | 30 s | `ServerOptions.AssociationTimeout` (0 → default, negative disables) | `server.go` | Transport closed; `Serve` returns `ErrAssociationFailed` |
| Confirmed request | none | `DialOptions.RequestTimeout` or the request context | `mms.go` | `ErrInvokeTimeout` + `context.DeadlineExceeded` |
| ABRT send in `Server.Shutdown` | 1 s per association | fixed | `server.go` | Transport closed regardless |
| File handle cleanup on disconnect | 10 s | fixed | `server_file.go` | `FileProvider.Close` context |
| Client dial / association | none | `ctx` of `iso.Dial` / `NewClient`; retries via `iso.WithRetry` | `transport/iso/dial.go` | — |

## Invoke tracker / outstanding requests

| Limit | Configured by | Enforced? |
|-------|---------------|-----------|
| Client outstanding requests | Proposed via `DialOptions.MMS.MaxOutstandingCalling` (default 5); negotiated value in `Client.Negotiated()` | **Not enforced** at runtime (tracker created with no pending cap) |
| Server outstanding requests | Proposed via `ServerMMSOptions.MaxOutstandingCalled` (default 5); negotiated per association | Server handles confirmed requests **serially** per connection (natural bound of 1 in-flight handler); negotiated value is not a separate pending queue cap |

Canonical concurrency wording: package `doc.go` (Concurrency) and [KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md).

## Wire value ranges

| Limit | Enforced where |
|-------|----------------|
| Session SPDU parameters (CONNECT/ACCEPT user data) at most 65535 bytes, the ISO 8327-1 extended length form; longer is an encode error | `internal/session` `encodeSPDU` |
| Invoke IDs are Unsigned32, 0..2^31−1 in MMS (ISO 9506-2 clause 7.5.2); negative or larger values are rejected (server: Reject invalid-invokeID); the client counter wraps after 2^31−1 | `internal/codec` `unmarshalConfirmedEnvelope`, `internal/pdu` `ExtractInvokeIDFor`, `internal/invoke` |
| InitializeJournal entriesDeleted is capped at 2^31−1 | `server_journal.go` |
| `UtcTime` seconds 0..2^32-1 (1970-01-01..2106-02-07); outside is an encode error; the zero `time.Time` is written as the epoch (unset) | `internal/pdu` `encodeUTCTime` |
| Alternate-access indices, file sizes: Unsigned32; FRSM IDs: Integer32 | `internal/pdu` |

## Limits not enforced (by design)

| Area | Reason |
|------|--------|
| File data chunk size | Bounded by PDU size limit; no separate cap needed |
| File size metadata | `Unsigned32` (max 4 GB) from BER; read in chunks so no single allocation |
| OID arc count | Bounded by content length which is bounded by BER length limit |
| Structure/array element count per level | Bounded by access results limit and PDU size |
