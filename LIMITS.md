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

## Invoke tracker

| Limit | Configured by | Default |
|-------|---------------|---------|
| Client outstanding requests | Negotiated `maxOutCalling` | No hard limit if 0 |
| Server outstanding requests | Negotiated `maxOutCalled` | Per `maxPending` option |

## Limits not enforced (by design)

| Area | Reason |
|------|--------|
| File data chunk size | Bounded by PDU size limit; no separate cap needed |
| File size metadata | `Unsigned32` (max 4 GB) from BER; read in chunks so no single allocation |
| OID arc count | Bounded by content length which is bounded by BER length limit |
| Structure/array element count per level | Bounded by access results limit and PDU size |
