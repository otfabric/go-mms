// SPDX-License-Identifier: MIT

package asn1util

import (
	"bytes"
	"encoding/asn1"
	"testing"
)

func TestPeekTag(t *testing.T) {
	tests := []struct {
		data []byte
		want byte
		err  bool
	}{
		{[]byte{0xa0, 0x05}, 0xa0, false},
		{[]byte{0x02, 0x01, 0x42}, 0x02, false},
		{nil, 0, true},
		{[]byte{}, 0, true},
	}
	for _, tt := range tests {
		got, err := PeekTag(tt.data)
		if tt.err {
			if err == nil {
				t.Errorf("PeekTag(%v): expected error", tt.data)
			}
			continue
		}
		if err != nil {
			t.Errorf("PeekTag(%v): %v", tt.data, err)
			continue
		}
		if got != tt.want {
			t.Errorf("PeekTag(%v) = 0x%02x, want 0x%02x", tt.data, got, tt.want)
		}
	}
}

func TestTagHelpers(t *testing.T) {
	if TagNumber(0xa4) != 4 {
		t.Error("TagNumber(0xa4) should be 4")
	}
	if !IsConstructed(0xa0) {
		t.Error("0xa0 should be constructed")
	}
	if IsConstructed(0x82) {
		t.Error("0x82 should not be constructed")
	}
	if TagClass(0xa0) != ClassContext {
		t.Error("0xa0 should be context class")
	}
}

func TestUnmarshalRaw(t *testing.T) {
	encoded, _ := asn1.Marshal(42)
	raw, rest, err := UnmarshalRaw(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) != 0 {
		t.Fatalf("unexpected trailing bytes: %d", len(rest))
	}
	if raw.Tag != 2 {
		t.Fatalf("tag = %d, want 2 (INTEGER)", raw.Tag)
	}

	_, _, err = UnmarshalRaw([]byte{0xff})
	if err == nil {
		t.Fatal("expected error for invalid data")
	}
}

func TestWrapConstructed(t *testing.T) {
	content := []byte{0x01, 0x02, 0x03}
	data := WrapConstructed(8, content)
	if data[0] != 0xa8 {
		t.Errorf("tag = 0x%02x, want 0xa8", data[0])
	}
	if data[1] != 3 {
		t.Errorf("length = %d, want 3", data[1])
	}
}

func TestWrapPrimitive(t *testing.T) {
	data := WrapPrimitive(11, nil)
	if data[0] != 0x8b {
		t.Errorf("tag = 0x%02x, want 0x8b", data[0])
	}
	if data[1] != 0 {
		t.Errorf("length = %d, want 0", data[1])
	}
}

func TestWrapConstructed_LongLength(t *testing.T) {
	content := make([]byte, 200)
	data := WrapConstructed(0, content)
	if data[0] != 0xa0 {
		t.Errorf("tag = 0x%02x, want 0xa0", data[0])
	}
	if data[1] != 0x81 {
		t.Errorf("length form byte = 0x%02x, want 0x81", data[1])
	}
	if data[2] != 200 {
		t.Errorf("length = %d, want 200", data[2])
	}
}

func TestWrapContextTag(t *testing.T) {
	tests := []struct {
		name        string
		tagNum      int
		constructed bool
		content     []byte
		wantPrefix  []byte
	}{
		{
			name:        "short constructed tag 0",
			tagNum:      0,
			constructed: true,
			content:     []byte{0x01},
			wantPrefix:  []byte{0xa0, 0x01},
		},
		{
			name:        "short primitive tag 11",
			tagNum:      11,
			constructed: false,
			content:     nil,
			wantPrefix:  []byte{0x8b, 0x00},
		},
		{
			name:        "short constructed tag 30",
			tagNum:      30,
			constructed: true,
			content:     []byte{0x01, 0x02},
			wantPrefix:  []byte{0xbe, 0x02},
		},
		{
			name:        "long-form tag 31 (boundary) primitive",
			tagNum:      31,
			constructed: false,
			content:     []byte{0xFF},
			wantPrefix:  []byte{0x9f, 31, 0x01},
		},
		{
			name:        "long-form tag 72 (FileOpen) constructed",
			tagNum:      72,
			constructed: true,
			content:     []byte{0xAA, 0xBB},
			wantPrefix:  []byte{0xbf, 72, 0x02},
		},
		{
			name:        "long-form tag 77 (FileDirectory) constructed",
			tagNum:      77,
			constructed: true,
			content:     []byte{0x01},
			wantPrefix:  []byte{0xbf, 77, 0x01},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WrapContextTag(tt.tagNum, tt.constructed, tt.content)
			if !bytes.HasPrefix(got, tt.wantPrefix) {
				t.Errorf("WrapContextTag(%d, %v, ...): prefix %x, want %x",
					tt.tagNum, tt.constructed, got[:len(tt.wantPrefix)], tt.wantPrefix)
			}
			totalLen := len(tt.wantPrefix) + len(tt.content)
			if len(got) != totalLen {
				t.Errorf("total length = %d, want %d", len(got), totalLen)
			}
		})
	}
}

func TestWrapContextTag_RoundTrip(t *testing.T) {
	content := []byte{0x02, 0x01, 0x42}
	for _, tagNum := range []int{0, 2, 30, 31, 72, 77, 127} {
		for _, constructed := range []bool{true, false} {
			data := WrapContextTag(tagNum, constructed, content)

			var raw asn1.RawValue
			rest, err := asn1.Unmarshal(data, &raw)
			if err != nil {
				t.Fatalf("tagNum=%d constructed=%v: unmarshal: %v", tagNum, constructed, err)
			}
			if len(rest) != 0 {
				t.Errorf("tagNum=%d: %d trailing bytes", tagNum, len(rest))
			}
			if raw.Tag != tagNum {
				t.Errorf("tagNum=%d: round-trip tag = %d", tagNum, raw.Tag)
			}
			if raw.IsCompound != constructed {
				t.Errorf("tagNum=%d: compound = %v, want %v", tagNum, raw.IsCompound, constructed)
			}
			if !bytes.Equal(raw.Bytes, content) {
				t.Errorf("tagNum=%d: content mismatch", tagNum)
			}
		}
	}
}
