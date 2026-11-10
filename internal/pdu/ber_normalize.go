// SPDX-License-Identifier: MIT

package pdu

import "github.com/otfabric/go-mms/internal/berutil"

// The Initiate and Status PDUs are decoded with encoding/asn1, which is
// DER-strict: it rejects non-minimal INTEGER contents and BIT STRINGs
// whose unused bits are not zero. Both are valid BER (X.690 §8.3.2 and
// §8.6.2.3 constrain only DER/CER senders, X.690 §10/§11), and MMS PDUs
// are BER (ISO 9506-2 clause 1). UnwrapPdu has already rewritten lengths
// to the minimal definite form; the helpers below normalise the remaining
// receive-side differences for the IMPLICIT INTEGER and BIT STRING fields
// of these PDUs before encoding/asn1 sees them (MMS-PDU-06).

// fieldRule tells normalizeFields how to treat one context tag.
type fieldRule int

const (
	fieldKeep fieldRule = iota
	fieldInteger
	fieldBitString
	fieldDescend
)

// initiateRules covers Initiate-RequestPDU / Initiate-ResponsePDU
// (ISO 9506-2 clause 8.2): [0]..[3] IMPLICIT Integer, [4] the
// Init(Request|Response)Detail SEQUENCE.
var initiateRules = map[byte]fieldRule{0x80: fieldInteger, 0x81: fieldInteger, 0x82: fieldInteger, 0x83: fieldInteger, 0xa4: fieldDescend}

// initDetailRules covers InitRequestDetail / InitResponseDetail: [0]
// IMPLICIT Integer16 version, [1] ParameterSupportOptions and [2]
// ServiceSupportOptions, both IMPLICIT BIT STRING.
var initDetailRules = map[byte]fieldRule{0x80: fieldInteger, 0x81: fieldBitString, 0x82: fieldBitString}

// statusRules covers Status-Response (ISO 9506-2 clause 10.3): [0] and
// [1] IMPLICIT INTEGER.
var statusRules = map[byte]fieldRule{0x80: fieldInteger, 0x81: fieldInteger}

// normalizeInitiate normalises Initiate PDU contents. An explicit 0x30
// wrapper (sent by some peers, see UnmarshalInitiateRequest) is kept.
func normalizeInitiate(content []byte) []byte {
	if len(content) > 0 && content[0] == 0x30 {
		tag, inner, err := berutil.DecodeTLVExact(content)
		if err != nil {
			return content
		}
		return berutil.EncodeTLV(tag, normalizeFields(inner, initiateRules, initDetailRules))
	}
	return normalizeFields(content, initiateRules, initDetailRules)
}

// normalizeFields rewrites the elements of content per rules; a
// fieldDescend element is rewritten per inner. Content that does not
// parse is returned unchanged for the decoder to reject.
func normalizeFields(content []byte, rules, inner map[byte]fieldRule) []byte {
	out := make([]byte, 0, len(content))
	for off := 0; off < len(content); {
		tag, v, n, err := berutil.DecodeTLVAt(content, off)
		if err != nil {
			return content
		}
		off += n
		switch rules[tag] {
		case fieldInteger:
			v = berutil.TrimInteger(v)
		case fieldBitString:
			v = maskBitStringPadding(v)
		case fieldDescend:
			v = normalizeFields(v, inner, nil)
		default:
		}
		out = berutil.AppendTLV(out, tag, v)
	}
	return out
}

// maskBitStringPadding clears the unused bits of BIT STRING contents
// (initial octet = number of unused bits in the final octet, X.690
// §8.6.2.2). Invalid contents are returned unchanged.
func maskBitStringPadding(v []byte) []byte {
	if len(v) < 2 || v[0] == 0 || v[0] > 7 {
		return v
	}
	mask := byte(0xff) << v[0]
	if v[len(v)-1]&^mask == 0 {
		return v
	}
	out := append([]byte(nil), v...)
	out[len(out)-1] &= mask
	return out
}
