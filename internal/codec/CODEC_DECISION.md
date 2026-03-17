# ASN.1 Codec Feasibility Decision

## Summary

`encoding/asn1` is viable as the primary codec for MMS PDU encoding and decoding.
Custom helpers are needed only for two specific patterns: top-level CHOICE dispatch
and TLV envelope wrapping.

## What works with encoding/asn1

### SEQUENCE with IMPLICIT context tags (the majority of MMS)

All MMS PDU bodies that are SEQUENCE structures with implicitly-tagged fields
map cleanly to Go structs with `asn1:"tag:N,implicit"` struct tags.

**Proven with:**
- `InitiateRequest` — 5 fields, all context-tagged, including nested `InitRequestDetail`
- `InitiateResponse` — same structure, round-trip verified
- `identifyResponseASN1` — 3 VisibleString fields with implicit context tags

### INTEGER, BIT STRING, VisibleString

Standard ASN.1 types marshal/unmarshal correctly via `int`, `asn1.BitString`,
and `string` (with `ia5` tag hint for VisibleString).

### Optional fields

`asn1:"optional"` combined with `asn1:"tag:N,implicit"` works correctly for
fields like `localDetailCalling` and `proposedDataStructureNesting`.

### Nested SEQUENCE

`InitRequestDetail` as a nested struct inside `InitiateRequest` works via
`asn1:"tag:4,implicit"` — encoding/asn1 handles nested implicit tagging.

## What requires custom helpers

### 1. Top-level PDU CHOICE dispatch

MMS uses a CHOICE at the top level (`MmsPdu`), where the first tag byte
determines the PDU type (0xa0 = ConfirmedRequest, 0xa8 = InitiateRequest, etc.).

`encoding/asn1` does not support CHOICE. We handle this with:
- `asn1util.PeekTag()` to inspect the first byte
- `codec.UnwrapPdu()` to strip the outer TLV and return inner content
- `pdu.ClassifyPdu()` to map tags to `PduKind`

**Size of custom code:** ~30 lines in `asn1util/raw.go`, ~20 lines in `codec/unmarshal.go`.

### 2. ConfirmedServiceRequest/Response CHOICE dispatch

Inside a ConfirmedRequest/ResponsePdu, the service-specific payload is also
a CHOICE. We parse the invoke ID with `asn1.Unmarshal`, then use `asn1.RawValue`
to capture the service-specific payload for dispatch.

**Size of custom code:** ~40 lines in `codec/unmarshal.go`.

### 3. TLV envelope construction

When building PDUs, we need to wrap marshaled content in context-specific
constructed tags (e.g., 0xa8 for InitiateRequest). `encoding/asn1.Marshal`
produces the inner SEQUENCE content, but the outer tag must be applied manually.

**Size of custom code:** ~30 lines in `asn1util/raw.go` (`WrapConstructed`, `WrapPrimitive`).

## What does NOT require custom helpers

- INTEGER encoding/decoding
- BIT STRING encoding/decoding
- VisibleString/IA5String encoding/decoding
- SEQUENCE construction and parsing
- Nested struct marshaling
- Optional field handling
- Tag/class assignment via struct tags

## Assessment

| Category | Stdlib | Custom |
|---|---|---|
| SEQUENCE fields | Yes | No |
| Primitive types | Yes | No |
| Optional fields | Yes | No |
| Nested structures | Yes | No |
| Top-level CHOICE | No | PeekTag + UnwrapPdu |
| Service CHOICE | Partial (RawValue) | ServiceTag + dispatch |
| TLV envelope | No | WrapConstructed/Primitive |

**`internal/asn1util`** contains ~60 lines of custom code total. This is well
within the "handful of focused helpers" threshold.

**`internal/codec`** contains ~100 lines of wrapper code. This is MMS-specific
marshaling logic, not a generic ASN.1 framework.

## Conclusion

The stdlib-first strategy is validated. `encoding/asn1` handles all structural
encoding/decoding. Custom code is limited to CHOICE dispatch and TLV envelope
wrapping — patterns that are fundamental to any ASN.1 CHOICE-based protocol
and cannot be avoided regardless of codec approach.

No generic BER/TLV infrastructure has been built. `internal/asn1util` is thin
and focused. The approach scales to the remaining MMS services (Read, Write,
GetNameList, etc.) which follow the same ConfirmedRequest/Response pattern.
