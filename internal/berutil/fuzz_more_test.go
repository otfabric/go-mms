// SPDX-License-Identifier: MIT

package berutil

import (
	"bytes"
	"testing"
)

func tlvSeeds() [][]byte {
	seq := EncodeTLV(0x30, append(EncodeTLV(0x02, EncodeInt(300)), EncodeTLV(0x1a, []byte("LLN0"))...))
	return [][]byte{
		seq,
		EncodeTLV(0xa0, seq),
		EncodeTLV(0x04, bytes.Repeat([]byte{0x01}, 200)),            // 0x81 length
		EncodeTLV(0x04, bytes.Repeat([]byte{0x01}, 300)),            // 0x82 length
		append(EncodeTLV(0x80, nil), EncodeTLV(0x81, []byte{1})...), // two TLVs
		{0x02, 0x81, 0x01, 0x05},                                    // non-minimal length
		{0x02, 0x84, 0x00, 0x00, 0x00, 0x01, 0x05},
		{0x30, 0x80, 0x00, 0x00}, // indefinite length
		{0x04, 0x83, 0xff, 0xff, 0xff},
		{0x30, 0x05, 0x02},
		{0x30},
		{},
	}
}

// FuzzDecodeTLVAt walks every TLV in data starting at offset and checks the
// returned slices stay in bounds and re-encode to an equivalent TLV.
func FuzzDecodeTLVAt(f *testing.F) {
	for _, s := range tlvSeeds() {
		f.Add(s, 0)
		f.Add(s, 1)
		f.Add(s, len(s))
	}

	f.Fuzz(func(t *testing.T, data []byte, offset int) {
		if offset < 0 {
			// Negative offsets are a caller bug; DecodeTLVAt does not
			// guard against them.
			return
		}
		for steps := 0; offset < len(data); steps++ {
			if steps > len(data) {
				t.Fatalf("no progress walking TLVs at offset %d", offset)
			}
			tag, content, consumed, err := DecodeTLVAt(data, offset)
			if err != nil {
				return
			}
			if tag != data[offset] {
				t.Fatalf("tag 0x%02x != data[%d] 0x%02x", tag, offset, data[offset])
			}
			if consumed < 2+len(content) || offset+consumed > len(data) {
				t.Fatalf("consumed %d out of range (offset %d, content %d, len %d)",
					consumed, offset, len(content), len(data))
			}
			end := offset + consumed
			if !bytes.Equal(data[end-len(content):end], content) {
				t.Fatal("content is not the tail of the consumed bytes")
			}
			tag2, content2, err := DecodeTLVExact(EncodeTLV(tag, content))
			if err != nil || tag2 != tag || !bytes.Equal(content2, content) {
				t.Fatalf("re-encode round-trip failed: tag=0x%02x content=%x err=%v", tag2, content2, err)
			}
			offset = end
		}
	})
}

// FuzzDecodeTLVExact checks DecodeTLVExact agrees with DecodeTLVAt and
// only succeeds when the TLV spans the whole input.
func FuzzDecodeTLVExact(f *testing.F) {
	for _, s := range tlvSeeds() {
		f.Add(s)
		f.Add(append(bytes.Clone(s), 0x00))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		tag, content, err := DecodeTLVExact(data)
		atTag, atContent, consumed, atErr := DecodeTLVAt(data, 0)
		if err == nil {
			if atErr != nil || consumed != len(data) || atTag != tag || !bytes.Equal(atContent, content) {
				t.Fatalf("DecodeTLVExact disagrees with DecodeTLVAt: %v / consumed=%d len=%d", atErr, consumed, len(data))
			}
			return
		}
		if atErr == nil && consumed == len(data) {
			t.Fatalf("DecodeTLVExact failed (%v) on a complete TLV", err)
		}
	})
}
