// SPDX-License-Identifier: MIT

// Package berutil provides minimal BER TLV encoding and decoding helpers.
//
// It is used by:
//   - ISO upper-layer packages (session, presentation, ACSE) for SPDU/PPDU framing
//   - internal/pdu for manual MMS PDU encoding/decoding where encoding/asn1
//     struct-based marshaling does not fit the wire format
//
// Boundary with internal/asn1util: berutil handles raw tag-length-value
// operations and integer codec primitives. asn1util provides stdlib
// encoding/asn1 boundary helpers (tag inspection, RawValue manipulation).
//
// This is not a generic ASN.1 library. It provides only the focused
// operations needed by the protocol layers.
//
// This package is internal — it is not part of the public API contract.
package berutil

import "fmt"

// EncodeTLV returns a BER TLV (tag + length + content) as a new byte slice.
func EncodeTLV(tag byte, content []byte) []byte {
	buf := make([]byte, 0, TLVSize(len(content)))
	return AppendTLV(buf, tag, content)
}

// AppendTLV appends a BER TLV (tag + length + content) to buf.
func AppendTLV(buf []byte, tag byte, content []byte) []byte {
	buf = append(buf, tag)
	buf = AppendLength(buf, len(content))
	buf = append(buf, content...)
	return buf
}

// AppendLength appends a BER definite-form length to buf.
func AppendLength(buf []byte, l int) []byte {
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

// LengthSize returns the number of bytes needed to encode the given
// length in BER definite form.
func LengthSize(l int) int {
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

// TLVSize returns the total encoded size of a TLV with the given
// content length (tag + length + content).
func TLVSize(contentLen int) int {
	return 1 + LengthSize(contentLen) + contentLen
}

// DecodeTLVExact decodes exactly one TLV from data and returns an error
// if the TLV does not consume the entire buffer.
func DecodeTLVExact(data []byte) (tag byte, content []byte, err error) {
	tag, content, consumed, err := DecodeTLVAt(data, 0)
	if err != nil {
		return 0, nil, err
	}
	if consumed != len(data) {
		return 0, nil, fmt.Errorf("berutil: %d trailing bytes after TLV", len(data)-consumed)
	}
	return tag, content, nil
}

// DecodeTLV decodes tag + length + value from the start of data.
func DecodeTLV(data []byte) (tag byte, content []byte, err error) {
	tag, content, _, err = DecodeTLVAt(data, 0)
	return
}

// DecodeTLVAt decodes a TLV at a given offset, returning the tag,
// content, and the total number of bytes consumed (tag + length + value).
func DecodeTLVAt(data []byte, offset int) (tag byte, content []byte, consumed int, err error) {
	if offset >= len(data) {
		return 0, nil, 0, fmt.Errorf("offset %d beyond data length %d", offset, len(data))
	}
	tag = data[offset]
	l, lSize, err := DecodeLength(data[offset+1:])
	if err != nil {
		return 0, nil, 0, fmt.Errorf("tag 0x%02x length: %w", tag, err)
	}
	start := offset + 1 + lSize
	end := start + l
	if end > len(data) {
		return 0, nil, 0, fmt.Errorf("tag 0x%02x content truncated (need %d, have %d)", tag, l, len(data)-start)
	}
	return tag, data[start:end], end - offset, nil
}

// DecodeLength decodes a BER definite-form length from the start of data,
// returning the length value and how many bytes were consumed.
func DecodeLength(data []byte) (length int, consumed int, err error) {
	if len(data) == 0 {
		return 0, 0, fmt.Errorf("missing length byte")
	}
	b := data[0]
	if b < 128 {
		return int(b), 1, nil
	}
	numBytes := int(b & 0x7f)
	if numBytes == 0 || numBytes > 3 {
		return 0, 0, fmt.Errorf("unsupported length form 0x%02x", b)
	}
	if len(data) < 1+numBytes {
		return 0, 0, fmt.Errorf("truncated multi-byte length")
	}
	var l int
	for i := 0; i < numBytes; i++ {
		l = (l << 8) | int(data[1+i])
	}
	return l, 1 + numBytes, nil
}

// EncodeInt encodes a signed integer as BER INTEGER content bytes
// (no tag/length wrapper). Produces the minimal two's complement encoding.
func EncodeInt(v int) []byte {
	if v == 0 {
		return []byte{0}
	}
	if v >= -128 && v <= 127 {
		return []byte{byte(v)}
	}
	if v >= -32768 && v <= 32767 {
		return []byte{byte(v >> 8), byte(v)}
	}
	buf := make([]byte, 4)
	buf[0] = byte(v >> 24)
	buf[1] = byte(v >> 16)
	buf[2] = byte(v >> 8)
	buf[3] = byte(v)
	for len(buf) > 1 && ((buf[0] == 0x00 && buf[1]&0x80 == 0) || (buf[0] == 0xff && buf[1]&0x80 != 0)) {
		buf = buf[1:]
	}
	return buf
}

// EncodeUint32 encodes an unsigned 32-bit integer as BER INTEGER content
// bytes with a leading 0x00 pad when the high bit is set.
func EncodeUint32(v uint32) []byte {
	if v == 0 {
		return []byte{0}
	}
	buf := make([]byte, 0, 5)
	for shift := 24; shift >= 0; shift -= 8 {
		b := byte(v >> uint(shift))
		if len(buf) > 0 || b != 0 {
			buf = append(buf, b)
		}
	}
	if buf[0]&0x80 != 0 {
		buf = append([]byte{0}, buf...)
	}
	return buf
}

// DecodeInteger decodes a BER INTEGER value (content bytes only, no tag/length)
// into a Go int. Handles multi-byte signed encoding correctly.
func DecodeInteger(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, fmt.Errorf("empty INTEGER content")
	}
	if len(data) > 4 {
		return 0, fmt.Errorf("INTEGER too large (%d bytes)", len(data))
	}
	var val int
	if data[0]&0x80 != 0 {
		val = -1 // sign extend
	}
	for _, b := range data {
		val = (val << 8) | int(b)
	}
	return val, nil
}

// EncodeObjectIdentifier encodes an OID (slice of int arcs) to BER content bytes.
func EncodeObjectIdentifier(oid []int) ([]byte, error) {
	if len(oid) < 2 {
		return nil, fmt.Errorf("OID must have at least 2 arcs")
	}
	first := oid[0]*40 + oid[1]
	var buf []byte
	buf = appendBase128(buf, first)
	for _, arc := range oid[2:] {
		buf = appendBase128(buf, arc)
	}
	return buf, nil
}

// DecodeObjectIdentifier decodes BER-encoded OID content bytes to a slice of int arcs.
func DecodeObjectIdentifier(data []byte) ([]int, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty OID content")
	}
	first, n, err := decodeBase128(data, 0)
	if err != nil {
		return nil, fmt.Errorf("OID first component: %w", err)
	}
	oid := []int{first / 40, first % 40}
	offset := n
	for offset < len(data) {
		arc, n, err := decodeBase128(data, offset)
		if err != nil {
			return nil, fmt.Errorf("OID arc at offset %d: %w", offset, err)
		}
		offset += n
		oid = append(oid, arc)
	}
	return oid, nil
}

func appendBase128(buf []byte, v int) []byte {
	if v < 0x80 {
		return append(buf, byte(v))
	}
	var tmp [10]byte
	i := len(tmp) - 1
	tmp[i] = byte(v & 0x7f)
	v >>= 7
	for v > 0 {
		i--
		tmp[i] = byte(v&0x7f) | 0x80
		v >>= 7
	}
	return append(buf, tmp[i:]...)
}

func decodeBase128(data []byte, offset int) (int, int, error) {
	v := 0
	n := 0
	for i := offset; i < len(data); i++ {
		n++
		v = (v << 7) | int(data[i]&0x7f)
		if data[i]&0x80 == 0 {
			return v, n, nil
		}
		if n > 5 {
			return 0, 0, fmt.Errorf("base128 integer too large")
		}
	}
	return 0, 0, fmt.Errorf("truncated base128 integer")
}

// DecodeUnsigned decodes a BER INTEGER value as an unsigned quantity
// (content bytes only, no tag/length). Rejects negative encodings.
func DecodeUnsigned(data []byte) (uint32, error) {
	if len(data) == 0 {
		return 0, fmt.Errorf("empty unsigned INTEGER content")
	}
	// BER encodes integers as signed. A high bit on the first byte
	// without a leading 0x00 pad means negative — reject that.
	if data[0]&0x80 != 0 {
		return 0, fmt.Errorf("unsigned INTEGER has negative encoding")
	}
	// Leading 0x00 is BER sign-padding for positive values > 0x7f.
	if data[0] == 0x00 && len(data) > 1 {
		data = data[1:]
	}
	if len(data) > 4 {
		return 0, fmt.Errorf("unsigned INTEGER too large (%d bytes)", len(data))
	}
	var val uint32
	for _, b := range data {
		val = (val << 8) | uint32(b)
	}
	return val, nil
}
