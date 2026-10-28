// SPDX-License-Identifier: MIT

package asn1util

import (
	"bytes"
	"encoding/asn1"
	"testing"
)

// maxWrapTagNum is the largest tag number encoding/asn1 decodes.
const maxWrapTagNum = 1<<31 - 1

func FuzzUnmarshalRaw(f *testing.F) {
	seeds := [][]byte{
		WrapPrimitive(0, nil),
		WrapPrimitive(11, []byte{0x01}),
		WrapConstructed(4, WrapPrimitive(1, []byte{0x2a})),
		WrapContextTag(30, true, []byte{0x80, 0x00}),
		WrapContextTag(31, false, []byte{0x05}),
		WrapContextTag(72, true, []byte{0x80, 0x01, 0x41}),
		WrapContextTag(200, false, bytes.Repeat([]byte{0x22}, 200)),
		WrapContextTag(20000, true, bytes.Repeat([]byte{0x33}, 300)),
		append(WrapPrimitive(1, []byte{0x01}), 0x8b, 0x00), // trailing TLV
		{0x30, 0x03, 0x02, 0x01, 0x05},                     // universal SEQUENCE
		{},
		{0xa0},
		{0xa0, 0x81},
		{0xa0, 0x81, 0x01, 0x00}, // non-minimal length
		{0xa0, 0x80, 0x00, 0x00}, // indefinite length
		{0xbf, 0x80, 0x80, 0x00}, // malformed long-form tag
		{0xbf, 0x1e, 0x00},       // long-form tag < 31
		{0x9f, 0xff, 0xff, 0xff, 0xff, 0x7f, 0x00},
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		tag, err := PeekTag(data)
		if (err == nil) != (len(data) > 0) || (err == nil && tag != data[0]) {
			t.Fatalf("PeekTag(%x) = 0x%02x, %v", data, tag, err)
		}
		_ = IsConstructed(tag)
		_ = TagClass(tag)
		_ = TagNumber(tag)

		raw, rest, err := UnmarshalRaw(data)
		if err != nil {
			return
		}
		consumed := len(data) - len(rest)
		if !bytes.Equal(raw.FullBytes, data[:consumed]) {
			t.Fatalf("FullBytes %x != consumed input %x", raw.FullBytes, data[:consumed])
		}
		if raw.Class != asn1.ClassContextSpecific || raw.Tag > maxWrapTagNum {
			return
		}
		// encoding/asn1 only accepts minimal tag and length encodings, so a
		// context-specific value must re-encode to exactly the same bytes.
		enc := WrapContextTag(raw.Tag, raw.IsCompound, raw.Bytes)
		if !bytes.Equal(enc, raw.FullBytes) {
			t.Fatalf("WrapContextTag(%d, %v, %x) = %x, want %x",
				raw.Tag, raw.IsCompound, raw.Bytes, enc, raw.FullBytes)
		}
	})
}

func FuzzWrapContextTag(f *testing.F) {
	f.Add(0, false, []byte{})
	f.Add(30, true, []byte{0x80, 0x00})
	f.Add(31, false, []byte{0x01})
	f.Add(127, true, bytes.Repeat([]byte{0x01}, 127))
	f.Add(128, false, bytes.Repeat([]byte{0x01}, 128))
	f.Add(16383, true, bytes.Repeat([]byte{0x01}, 255))
	f.Add(16384, false, bytes.Repeat([]byte{0x01}, 256))
	f.Add(maxWrapTagNum, true, []byte{0x00})

	f.Fuzz(func(t *testing.T, tagNum int, constructed bool, content []byte) {
		if tagNum < 0 || tagNum > maxWrapTagNum {
			return
		}
		enc := WrapContextTag(tagNum, constructed, content)
		raw, rest, err := UnmarshalRaw(enc)
		if err != nil {
			t.Fatalf("UnmarshalRaw(WrapContextTag(%d, %v, %x)) = %v", tagNum, constructed, content, err)
		}
		if len(rest) != 0 || raw.Class != asn1.ClassContextSpecific || raw.Tag != tagNum ||
			raw.IsCompound != constructed || !bytes.Equal(raw.Bytes, content) {
			t.Fatalf("round-trip mismatch: class=%d tag=%d compound=%v bytes=%x rest=%x",
				raw.Class, raw.Tag, raw.IsCompound, raw.Bytes, rest)
		}
	})
}
