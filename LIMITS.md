# Defensive Bounds and Resource Limits

This document lists all hard limits enforced by go-mms to protect against malformed or malicious peer input.

## PDU size

| Check | Location | Behavior |
|-------|----------|----------|
| Client receive | `mms.go` (`receiveRaw`) | Rejects PDUs exceeding negotiated `maxPDUSize` (after association only) |
| Server receive | `serverconn/conn.go` (`Serve`) | Logs warning and skips PDUs exceeding negotiated `MaxPDUSize` |

The negotiated `maxPDUSize` is the minimum of what each side proposes during association. Before negotiation (during the initial handshake), no size limit is enforced.

## Nesting depth

| Limit | Value | Location | Purpose |
|-------|:-----:|----------|---------|
| Data nesting | 64 | `internal/pdu/data.go` | Prevents stack overflow from deeply nested arrays/structures in `DataValue` |
| TypeSpec nesting | 32 | `internal/pdu/getvaraccess.go` | Prevents stack overflow from deeply nested type specifications |

These limits apply to recursive decoding of `DataValue` and `TypeSpec` trees. The limits are enforced internally; public API signatures are unchanged.

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
| File name | 1,024 bytes | `internal/pdu/file.go` | Caps decoded file names |
| Identifier name | 1,024 bytes | `internal/pdu/getnamelist.go` | Caps individual identifier strings |

## BER encoding limits

| Limit | Value | Location | Purpose |
|-------|------:|----------|---------|
| Length field | 3 bytes (max 16 MB) | `internal/berutil/berutil.go` | Maximum content length from BER length encoding |
| Tag field | 1 byte (tags 0–30) | `internal/berutil/berutil.go` | Extended multi-byte tags not supported |
| INTEGER | 4 bytes (32-bit) | `internal/berutil/berutil.go` | Maximum signed integer size |
| Unsigned INTEGER | 4 bytes (32-bit) | `internal/berutil/berutil.go` | Maximum unsigned integer size |
| Indefinite length | Rejected | `internal/berutil/berutil.go` | BER indefinite length form not supported |

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

## Limits not enforced (by design)

| Area | Reason |
|------|--------|
| File data chunk size | Bounded by PDU size limit; no separate cap needed |
| File size metadata | `Unsigned32` (max 4 GB) from BER; read in chunks so no single allocation |
| OID arc count | Bounded by content length which is bounded by BER length limit |
| Structure/array element count per level | Bounded by access results limit and PDU size |
