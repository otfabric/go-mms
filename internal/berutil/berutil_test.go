package berutil

import (
	"bytes"
	"testing"
)

func TestEncodeTLVRoundTrip(t *testing.T) {
	content := []byte{0x01, 0x02, 0x03}
	encoded := EncodeTLV(0x30, content)

	tag, decoded, err := DecodeTLV(encoded)
	if err != nil {
		t.Fatalf("DecodeTLV: %v", err)
	}
	if tag != 0x30 {
		t.Errorf("tag = 0x%02x, want 0x30", tag)
	}
	if !bytes.Equal(decoded, content) {
		t.Errorf("content = %x, want %x", decoded, content)
	}
}

func TestEncodeLongContent(t *testing.T) {
	content := make([]byte, 200)
	for i := range content {
		content[i] = byte(i)
	}
	encoded := EncodeTLV(0x04, content)

	tag, decoded, err := DecodeTLV(encoded)
	if err != nil {
		t.Fatalf("DecodeTLV: %v", err)
	}
	if tag != 0x04 {
		t.Errorf("tag = 0x%02x, want 0x04", tag)
	}
	if !bytes.Equal(decoded, content) {
		t.Errorf("content length = %d, want %d", len(decoded), len(content))
	}
}

func TestDecodeIntegerSingleByte(t *testing.T) {
	tests := []struct {
		data []byte
		want int
	}{
		{[]byte{0x00}, 0},
		{[]byte{0x01}, 1},
		{[]byte{0x03}, 3},
		{[]byte{0x7f}, 127},
	}
	for _, tt := range tests {
		got, err := DecodeInteger(tt.data)
		if err != nil {
			t.Errorf("DecodeInteger(%x): %v", tt.data, err)
			continue
		}
		if got != tt.want {
			t.Errorf("DecodeInteger(%x) = %d, want %d", tt.data, got, tt.want)
		}
	}
}

func TestDecodeIntegerMultiByte(t *testing.T) {
	tests := []struct {
		data []byte
		want int
	}{
		{[]byte{0x00, 0xff}, 255},
		{[]byte{0x01, 0x00}, 256},
	}
	for _, tt := range tests {
		got, err := DecodeInteger(tt.data)
		if err != nil {
			t.Errorf("DecodeInteger(%x): %v", tt.data, err)
			continue
		}
		if got != tt.want {
			t.Errorf("DecodeInteger(%x) = %d, want %d", tt.data, got, tt.want)
		}
	}
}

func TestDecodeIntegerEmpty(t *testing.T) {
	_, err := DecodeInteger(nil)
	if err == nil {
		t.Error("expected error for empty INTEGER")
	}
}

func TestDecodeIntegerTooLarge(t *testing.T) {
	_, err := DecodeInteger([]byte{0x01, 0x02, 0x03, 0x04, 0x05})
	if err == nil {
		t.Error("expected error for 5-byte INTEGER")
	}
}

func TestDecodeLengthVariants(t *testing.T) {
	tests := []struct {
		data     []byte
		wantLen  int
		wantSize int
	}{
		{[]byte{0x00}, 0, 1},
		{[]byte{0x7f}, 127, 1},
		{[]byte{0x81, 0x80}, 128, 2},
		{[]byte{0x81, 0xff}, 255, 2},
		{[]byte{0x82, 0x01, 0x00}, 256, 3},
	}
	for _, tt := range tests {
		l, s, err := DecodeLength(tt.data)
		if err != nil {
			t.Errorf("DecodeLength(%x): %v", tt.data, err)
			continue
		}
		if l != tt.wantLen || s != tt.wantSize {
			t.Errorf("DecodeLength(%x) = (%d, %d), want (%d, %d)", tt.data, l, s, tt.wantLen, tt.wantSize)
		}
	}
}

func TestDecodeTLVTruncated(t *testing.T) {
	_, _, err := DecodeTLV([]byte{0x30, 0x05, 0x01})
	if err == nil {
		t.Error("expected error for truncated TLV")
	}
}

func TestDecodeUnsigned(t *testing.T) {
	tests := []struct {
		data []byte
		want uint32
	}{
		{[]byte{0x00}, 0},
		{[]byte{0x01}, 1},
		{[]byte{0x7f}, 127},
		{[]byte{0x00, 0x80}, 128},
		{[]byte{0x00, 0xff}, 255},
		{[]byte{0x01, 0x00}, 256},
		{[]byte{0x00, 0xff, 0xff, 0xff, 0xff}, 0xffffffff},
	}
	for _, tt := range tests {
		got, err := DecodeUnsigned(tt.data)
		if err != nil {
			t.Errorf("DecodeUnsigned(%x): %v", tt.data, err)
			continue
		}
		if got != tt.want {
			t.Errorf("DecodeUnsigned(%x) = %d, want %d", tt.data, got, tt.want)
		}
	}
}

func TestDecodeUnsignedErrors(t *testing.T) {
	// Empty
	_, err := DecodeUnsigned(nil)
	if err == nil {
		t.Error("expected error for empty unsigned")
	}

	// Negative encoding
	_, err = DecodeUnsigned([]byte{0xff})
	if err == nil {
		t.Error("expected error for negative encoding")
	}

	// Too large
	_, err = DecodeUnsigned([]byte{0x01, 0x02, 0x03, 0x04, 0x05})
	if err == nil {
		t.Error("expected error for too-large unsigned")
	}
}

func TestLengthSize(t *testing.T) {
	if LengthSize(0) != 1 {
		t.Error("LengthSize(0) != 1")
	}
	if LengthSize(127) != 1 {
		t.Error("LengthSize(127) != 1")
	}
	if LengthSize(128) != 2 {
		t.Error("LengthSize(128) != 2")
	}
	if LengthSize(255) != 2 {
		t.Error("LengthSize(255) != 2")
	}
	if LengthSize(256) != 3 {
		t.Error("LengthSize(256) != 3")
	}
}

func TestEncodeInt(t *testing.T) {
	tests := []struct {
		name string
		v    int
		want []byte
	}{
		{"zero", 0, []byte{0x00}},
		{"one", 1, []byte{0x01}},
		{"127", 127, []byte{0x7f}},
		{"128", 128, []byte{0x00, 0x80}},
		{"255", 255, []byte{0x00, 0xff}},
		{"256", 256, []byte{0x01, 0x00}},
		{"32767", 32767, []byte{0x7f, 0xff}},
		{"32768", 32768, []byte{0x00, 0x80, 0x00}},
		{"65535", 65535, []byte{0x00, 0xff, 0xff}},
		{"0x7FFFFF", 0x7FFFFF, []byte{0x7f, 0xff, 0xff}},
		{"0x7FFFFFFF", 0x7FFFFFFF, []byte{0x7f, 0xff, 0xff, 0xff}},
		{"neg1", -1, []byte{0xff}},
		{"neg128", -128, []byte{0x80}},
		{"neg129", -129, []byte{0xff, 0x7f}},
		{"neg32768", -32768, []byte{0x80, 0x00}},
		{"neg32769", -32769, []byte{0xff, 0x7f, 0xff}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EncodeInt(tt.v)
			if !bytes.Equal(got, tt.want) {
				t.Errorf("EncodeInt(%d) = %x, want %x", tt.v, got, tt.want)
			}
		})
	}
}

func TestEncodeIntRoundTrip(t *testing.T) {
	values := []int{0, 1, -1, 127, -128, 128, -129, 255, 256, 32767, -32768, 32768, -32769, 65535, 0x7FFFFF, 0x7FFFFFFF}
	for _, v := range values {
		encoded := EncodeInt(v)
		decoded, err := DecodeInteger(encoded)
		if err != nil {
			t.Errorf("DecodeInteger(EncodeInt(%d)): %v", v, err)
			continue
		}
		if decoded != v {
			t.Errorf("roundtrip(%d): got %d", v, decoded)
		}
	}
}

func TestEncodeUint32(t *testing.T) {
	tests := []struct {
		name string
		v    uint32
		want []byte
	}{
		{"zero", 0, []byte{0x00}},
		{"one", 1, []byte{0x01}},
		{"127", 127, []byte{0x7f}},
		{"128", 128, []byte{0x00, 0x80}},
		{"255", 255, []byte{0x00, 0xff}},
		{"256", 256, []byte{0x01, 0x00}},
		{"65535", 65535, []byte{0x00, 0xff, 0xff}},
		{"65536", 65536, []byte{0x01, 0x00, 0x00}},
		{"0xFFFFFF", 0xFFFFFF, []byte{0x00, 0xff, 0xff, 0xff}},
		{"0xFFFFFFFF", 0xFFFFFFFF, []byte{0x00, 0xff, 0xff, 0xff, 0xff}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EncodeUint32(tt.v)
			if !bytes.Equal(got, tt.want) {
				t.Errorf("EncodeUint32(%d) = %x, want %x", tt.v, got, tt.want)
			}
		})
	}
}

func TestEncodeUint32RoundTrip(t *testing.T) {
	values := []uint32{0, 1, 127, 128, 255, 256, 65535, 65536, 0xFFFFFF, 0xFFFFFFFF}
	for _, v := range values {
		encoded := EncodeUint32(v)
		decoded, err := DecodeUnsigned(encoded)
		if err != nil {
			t.Errorf("DecodeUnsigned(EncodeUint32(%d)): %v", v, err)
			continue
		}
		if decoded != v {
			t.Errorf("roundtrip(%d): got %d", v, decoded)
		}
	}
}

func TestEncodeObjectIdentifier(t *testing.T) {
	tests := []struct {
		name string
		oid  []int
	}{
		{"MMS OID", []int{1, 2, 840, 10003, 15, 1}},
		{"ISO 9506", []int{1, 0, 9506, 2, 1}},
		{"large arcs", []int{1, 3, 6, 1, 16384}},
		{"minimal", []int{0, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := EncodeObjectIdentifier(tt.oid)
			if err != nil {
				t.Fatalf("EncodeObjectIdentifier(%v): %v", tt.oid, err)
			}
			if len(encoded) == 0 {
				t.Fatal("encoded OID is empty")
			}
		})
	}
}

func TestDecodeObjectIdentifier(t *testing.T) {
	tests := []struct {
		name string
		oid  []int
	}{
		{"MMS OID", []int{1, 2, 840, 10003, 15, 1}},
		{"ISO 9506", []int{1, 0, 9506, 2, 1}},
		{"large arcs", []int{1, 3, 6, 1, 16384}},
		{"minimal", []int{0, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := EncodeObjectIdentifier(tt.oid)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			decoded, err := DecodeObjectIdentifier(encoded)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(decoded) != len(tt.oid) {
				t.Fatalf("decoded length = %d, want %d", len(decoded), len(tt.oid))
			}
			for i := range tt.oid {
				if decoded[i] != tt.oid[i] {
					t.Errorf("arc[%d] = %d, want %d", i, decoded[i], tt.oid[i])
				}
			}
		})
	}
}

func TestEncodeObjectIdentifierErrors(t *testing.T) {
	_, err := EncodeObjectIdentifier([]int{1})
	if err == nil {
		t.Error("expected error for single-arc OID")
	}
	_, err = EncodeObjectIdentifier(nil)
	if err == nil {
		t.Error("expected error for nil OID")
	}
}

func TestDecodeTLVExact(t *testing.T) {
	data := EncodeTLV(0x30, []byte{0x01, 0x02})
	tag, content, err := DecodeTLVExact(data)
	if err != nil {
		t.Fatal(err)
	}
	if tag != 0x30 {
		t.Fatalf("tag = 0x%02x, want 0x30", tag)
	}
	if len(content) != 2 {
		t.Fatalf("content len = %d, want 2", len(content))
	}

	// Trailing bytes
	_, _, err = DecodeTLVExact(append(data, 0xff))
	if err == nil {
		t.Fatal("expected error for trailing bytes")
	}

	// Empty
	_, _, err = DecodeTLVExact(nil)
	if err == nil {
		t.Fatal("expected error for nil")
	}
}

func TestDecodeLengthNegative(t *testing.T) {
	// Indefinite length (0x80)
	_, _, err := DecodeLength([]byte{0x80})
	if err == nil {
		t.Fatal("expected error for indefinite length")
	}

	// 4-byte form (0x84)
	_, _, err = DecodeLength([]byte{0x84, 0x00, 0x00, 0x00, 0x01})
	if err == nil {
		t.Fatal("expected error for 4-byte length")
	}

	// Truncated multi-byte
	_, _, err = DecodeLength([]byte{0x82, 0x01})
	if err == nil {
		t.Fatal("expected error for truncated multi-byte length")
	}
}

func TestDecodeObjectIdentifierErrors(t *testing.T) {
	_, err := DecodeObjectIdentifier(nil)
	if err == nil {
		t.Error("expected error for nil data")
	}
	_, err = DecodeObjectIdentifier([]byte{})
	if err == nil {
		t.Error("expected error for empty data")
	}
}
