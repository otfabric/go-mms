package berutil

import "testing"

func FuzzDecodeTLV(f *testing.F) {
	f.Add([]byte{0x30, 0x03, 0x01, 0x02, 0x03})
	f.Add([]byte{0x04, 0x00})
	f.Add([]byte{0x02, 0x01, 0x00})
	f.Add([]byte{0x81, 0x80}) // truncated long-form
	f.Add([]byte{})
	f.Add([]byte{0x30})

	f.Fuzz(func(t *testing.T, data []byte) {
		tag, content, err := DecodeTLV(data)
		if err != nil {
			return
		}
		// Re-encode and verify tag preservation.
		reEncoded := EncodeTLV(tag, content)
		tag2, content2, err := DecodeTLV(reEncoded)
		if err != nil {
			t.Fatalf("re-encode round-trip failed: %v", err)
		}
		if tag2 != tag {
			t.Fatalf("tag mismatch: %02x vs %02x", tag, tag2)
		}
		if len(content2) != len(content) {
			t.Fatalf("content length mismatch: %d vs %d", len(content), len(content2))
		}
	})
}

func FuzzDecodeLength(f *testing.F) {
	f.Add([]byte{0x00})
	f.Add([]byte{0x7f})
	f.Add([]byte{0x81, 0x80})
	f.Add([]byte{0x82, 0x01, 0x00})
	f.Add([]byte{0x83, 0x01, 0x00, 0x00})
	f.Add([]byte{})
	f.Add([]byte{0x84}) // unsupported 4-byte form

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = DecodeLength(data)
	})
}

func FuzzDecodeInteger(f *testing.F) {
	f.Add([]byte{0x00})
	f.Add([]byte{0x7f})
	f.Add([]byte{0xff})
	f.Add([]byte{0x00, 0xff})
	f.Add([]byte{0x80})
	f.Add([]byte{})
	f.Add([]byte{0x01, 0x02, 0x03, 0x04, 0x05}) // too large

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = DecodeInteger(data)
	})
}

func FuzzDecodeUnsigned(f *testing.F) {
	f.Add([]byte{0x00})
	f.Add([]byte{0x7f})
	f.Add([]byte{0x00, 0x80})
	f.Add([]byte{0x00, 0xff, 0xff, 0xff, 0xff})
	f.Add([]byte{0xff}) // negative
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = DecodeUnsigned(data)
	})
}
