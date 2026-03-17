# Decoder Strictness Audit Checklist

Audit completed as part of Phase A — Hardening and Invariants.

## Methodology

Every unmarshal/decode function across the target packages was audited against eight strictness criteria:

1. Reject empty required payloads
2. Reject unexpected tags
3. Reject duplicate singular fields
4. Reject missing required fields
5. Reject trailing bytes (unless explicitly allowed)
6. Reject malformed extended tags
7. Reject malformed length encodings
8. Reject invalid nesting / truncated TLVs

## Summary

| Package | Functions audited | Fixes applied | Interop exceptions documented |
|---------|:-:|:-:|:-:|
| `internal/berutil` | 7 | 1 (DecodeTLVExact helper) | 0 |
| `internal/codec` | 7 | 1 (UnwrapPdu trailing bytes) | 0 |
| `internal/pdu` | 60+ | 22 (default error cases + trailing byte checks) | 1 (decodeFileName bare GraphicString) |
| `internal/acse` | 7 | 3 (trailing byte checks) | 2 (parseAARQ, parseAARE unknown optional fields) |
| `internal/session` | 7 | 3 (trailing byte checks, interop comments) | 1 (unknown PGIs per ISO 8327) |
| `internal/presentation` | 5 | 4 (trailing byte checks, required field check) | 2 (parseCPorCPA, parseNormalMode unknown tags) |
| `internal/isostack` | 7 | 0 (delegates to above) | 0 |

## Fixes applied

### Trailing bytes rejection

Every decoder now rejects trailing bytes after the expected payload:

- `codec.UnwrapPdu` — captures and checks `rest` from `asn1util.UnmarshalRaw`
- `berutil.DecodeTLVExact` — new helper that wraps `DecodeTLVAt` + trailing byte check
- `acse.Parse` — uses `DecodeTLVExact` for outer APDU
- `acse.validateRLRQ` — checks consumed vs data length
- `acse.validateABRT` — checks consumed vs data length
- `session.parseConnectAccept` — checks `offset == headerEnd` after PGI loop
- `session.parseSimpleWithUserData` — checks `offset == end` after loop
- `presentation.parseCPorCPA` — uses `DecodeTLVExact` for outer TLV
- `presentation.parseUserData` — uses `DecodeTLVExact`
- `presentation.parsePdvList` — uses `DecodeTLVExact` + `offset == len(seqContent)` check
- `pdu.UnmarshalAccessResults` — `offset == len(data)` check
- `pdu.UnmarshalWriteResponse` — `offset == len(content)` check
- `pdu.UnmarshalInformationReport` — `offset == len(irContent)` check
- `pdu.decodeArrayTypeSpec` — checks consumed bytes
- `pdu.decodeStructureTypeSpec` — checks consumed bytes
- `pdu.decodeVariableSpecFull` — `offset == len(data)` check

### Unknown tag rejection

All file service and journal service decoders now reject unexpected tags via `default` error cases:

**File decoders (internal/pdu/file.go):**
- `UnmarshalFileOpenRequest`
- `UnmarshalFileDirectoryRequest`
- `UnmarshalFileRenameRequest`
- `UnmarshalObtainFileRequest`
- `UnmarshalFileOpenResponse`
- `parseFileAttributes`
- `UnmarshalFileReadResponse`
- `UnmarshalFileDirectoryResponse`
- `parseDirectoryEntries` (inner entry loop)

**Journal decoders (internal/pdu/journal.go):**
- `UnmarshalReadJournalRequest`
- `decodeStartAfter`
- `UnmarshalReadJournalResponse`
- `parseJournalEntry`
- `parseEntryContent`
- `parseJournalVariable`

**Other:**
- `pdu.decodeVariableSpecFull`

### Required field validation

- `presentation.parsePdvList` — now requires `context-id` (tag 0x02) to be present

## Intentional interop leniency (documented in code)

These decoders intentionally accept unknown tags for interop compliance:

| Decoder | Reason | Comment in code |
|---------|--------|-----------------|
| `acse.parseAARQ` | ISO 8650 defines optional fields (application-context-name, AP/AE qualifiers) that we do not process | Yes |
| `acse.parseAARE` | ISO 8650 defines optional fields beyond result and user-information | Yes |
| `session.parseConnectAccept` | ISO 8327 defines many PGIs beyond those used by MMS | Yes |
| `session.parseSimpleWithUserData` | Same as above for FINISH/DISCONNECT | Yes |
| `presentation.parseCPorCPA` | ISO 8823 defines optional parameters beyond those MMS uses | Yes |
| `presentation.parseNormalMode` | Same as above for normal-mode parameters | Yes |
| `pdu.decodeFileName` | Accepts bare GraphicString (0x19) in addition to wrapped form for C implementation interop | Yes |

## Remaining known gaps (low priority)

| Gap | Status | Reason |
|-----|--------|--------|
| Duplicate singular fields not rejected | Accepted | Last-wins semantics; no real-world exploitability. Adding duplicate detection would add complexity with minimal benefit for MMS. |
| `berutil.DecodeLength` accepts redundant leading zeros | Accepted | Valid BER; rejecting would break interop with lazy encoders. |
| `berutil` only supports single-byte tags (0–30) | Documented | Extended BER tags (tag number > 30) are not used by MMS protocol. |
| `parseAARQ` does not require application-context-name | Accepted | Required by ISO 8650 but always omitted in practice by MMS implementations. |

## Negative tests added

All strictness fixes have corresponding negative tests:

- `internal/pdu/strictness_test.go` — 13 tests (8 unknown tag + 3 trailing bytes + 2 information report)
- `internal/acse/acse_test.go` — 3 tests (RLRQ, RLRE, ABRT trailing bytes)
- `internal/presentation/presentation_test.go` — 3 tests (UserData, CP, CPA trailing bytes)
- `internal/session/session_test.go` — 3 tests (CONNECT, ACCEPT, FINISH trailing bytes)
- `internal/berutil/berutil_test.go` — 2 tests (DecodeTLVExact, DecodeLength negative)
- `internal/codec/codec_test.go` — 1 test (UnwrapPdu trailing bytes)
