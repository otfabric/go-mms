// Package asn1util provides helpers for working with stdlib encoding/asn1
// types at the boundary between Go struct marshaling and raw BER data.
//
// It handles tag inspection, RawValue manipulation, and ASN.1 struct tag
// constants used by MMS PDU definitions. Unlike berutil, this package
// operates on encoding/asn1 types rather than raw byte slices.
//
// Boundary with internal/berutil: berutil handles raw TLV encoding/decoding
// and integer primitives. asn1util bridges the gap between encoding/asn1
// and MMS-specific needs.
package asn1util

import (
	"encoding/asn1"
	"fmt"
)

// PeekTag returns the first tag byte from raw BER-encoded data without
// consuming the input. Returns an error if data is empty.
func PeekTag(data []byte) (byte, error) {
	if len(data) == 0 {
		return 0, fmt.Errorf("asn1util: empty data")
	}
	return data[0], nil
}

// UnmarshalRaw parses raw BER data into an asn1.RawValue, returning
// the value and any remaining bytes.
func UnmarshalRaw(data []byte) (asn1.RawValue, []byte, error) {
	var raw asn1.RawValue
	rest, err := asn1.Unmarshal(data, &raw)
	if err != nil {
		return asn1.RawValue{}, nil, fmt.Errorf("asn1util: unmarshal raw: %w", err)
	}
	return raw, rest, nil
}

// WrapConstructed wraps content bytes in a context-specific constructed
// tag with the given tag number. This produces the BER TLV envelope.
//
// Only tag numbers 0–30 are supported (single-byte tag form).
func WrapConstructed(tagNum int, content []byte) []byte {
	tag := byte(ClassContext | ConstructedFlag | (tagNum & 0x1f))
	return encodeTLV(tag, content)
}

// WrapPrimitive wraps content bytes in a context-specific primitive
// tag with the given tag number.
//
// Only tag numbers 0–30 are supported (single-byte tag form).
func WrapPrimitive(tagNum int, content []byte) []byte {
	tag := byte(ClassContext | (tagNum & 0x1f))
	return encodeTLV(tag, content)
}

// WrapContextTag wraps content in a context-specific BER tag. It handles
// both short-form tags (0–30, single-byte encoding) and long-form tags
// (>30, multi-byte encoding used by MMS file services).
func WrapContextTag(tagNum int, constructed bool, content []byte) []byte {
	if tagNum <= 30 {
		if constructed {
			return WrapConstructed(tagNum, content)
		}
		return WrapPrimitive(tagNum, content)
	}

	lead := byte(ClassContext)
	if constructed {
		lead |= ConstructedFlag
	}
	lead |= 0x1f // long-form indicator

	tagBytes := encodeTagNumber(tagNum)

	l := len(content)
	size := 1 + len(tagBytes) + lengthSize(l) + l
	buf := make([]byte, 0, size)
	buf = append(buf, lead)
	buf = append(buf, tagBytes...)
	buf = appendLength(buf, l)
	buf = append(buf, content...)
	return buf
}

func encodeTagNumber(n int) []byte {
	if n < 0x80 {
		return []byte{byte(n)}
	}
	if n < 0x4000 {
		return []byte{byte(n>>7) | 0x80, byte(n & 0x7f)}
	}
	return []byte{byte(n>>14) | 0x80, byte(n>>7) | 0x80, byte(n & 0x7f)}
}

func lengthSize(l int) int {
	if l < 128 {
		return 1
	}
	if l < 256 {
		return 2
	}
	if l < 65536 {
		return 3
	}
	return 4
}

func appendLength(buf []byte, l int) []byte {
	if l < 128 {
		return append(buf, byte(l))
	}
	if l < 256 {
		return append(buf, 0x81, byte(l))
	}
	if l < 65536 {
		return append(buf, 0x82, byte(l>>8), byte(l))
	}
	return append(buf, 0x83, byte(l>>16), byte(l>>8), byte(l))
}

func encodeTLV(tag byte, content []byte) []byte {
	l := len(content)
	size := 1 + lengthSize(l) + l
	buf := make([]byte, 0, size)
	buf = append(buf, tag)
	buf = appendLength(buf, l)
	buf = append(buf, content...)
	return buf
}
