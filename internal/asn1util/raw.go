// SPDX-License-Identifier: MIT

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

	"github.com/otfabric/go-mms/internal/berutil"
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
// Tag numbers above 30 use the multi-byte form (X.690 §8.1.2.4).
func WrapConstructed(tagNum int, content []byte) []byte {
	return WrapContextTag(tagNum, true, content)
}

// WrapPrimitive wraps content bytes in a context-specific primitive
// tag with the given tag number. Tag numbers above 30 use the
// multi-byte form (X.690 §8.1.2.4).
func WrapPrimitive(tagNum int, content []byte) []byte {
	return WrapContextTag(tagNum, false, content)
}

// WrapContextTag wraps content in a context-specific BER tag. It handles
// both short-form tags (0–30, single-byte encoding) and long-form tags
// (>30, multi-byte encoding used by MMS file services). A negative tag
// number is a caller bug and is encoded as tag 0.
func WrapContextTag(tagNum int, constructed bool, content []byte) []byte {
	lead := byte(ClassContext)
	if constructed {
		lead |= ConstructedFlag
	}
	tagNum = max(tagNum, 0)
	var tagBytes []byte
	if tagNum <= 30 {
		lead |= byte(tagNum & 0x1f)
	} else {
		lead |= 0x1f // long-form indicator
		tagBytes = encodeTagNumber(tagNum)
	}

	l := len(content)
	buf := make([]byte, 0, 1+len(tagBytes)+berutil.LengthSize(l)+l)
	buf = append(buf, lead)
	buf = append(buf, tagBytes...)
	buf = berutil.AppendLength(buf, l)
	buf = append(buf, content...)
	return buf
}

func encodeTagNumber(n int) []byte {
	// Base-128, most significant group first, continuation bit on all
	// but the last byte (X.690 §8.1.2.4.2).
	var groups [10]byte
	i := len(groups) - 1
	groups[i] = byte(n & 0x7f)
	for n >>= 7; n > 0; n >>= 7 {
		i--
		groups[i] = byte(n&0x7f) | 0x80
	}
	return append([]byte(nil), groups[i:]...)
}
