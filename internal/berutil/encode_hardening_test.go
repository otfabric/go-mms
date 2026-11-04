// SPDX-License-Identifier: MIT

package berutil

import (
	"bytes"
	"encoding/asn1"
	"math"
	"testing"
)

// asn1Int returns the BER INTEGER content encoding/asn1 produces for v.
func asn1Int(t testing.TB, v int64) []byte {
	t.Helper()
	b, err := asn1.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	_, content, err := DecodeTLVExact(b)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

// EncodeInt must produce the minimal two's complement encoding for the
// whole int range; it used to truncate values outside int32.
func TestEncodeInt_FullRange(t *testing.T) {
	vals := []int64{
		0, 1, -1, 127, 128, -128, -129, 32767, 32768, -32768, -32769,
		math.MaxInt32, math.MaxInt32 + 1, math.MinInt32, math.MinInt32 - 1,
		1 << 40, -(1 << 40), math.MaxUint32, math.MaxInt64, math.MinInt64,
	}
	for _, v := range vals {
		if int64(int(v)) != v {
			continue // not an int on this platform
		}
		got := EncodeInt(int(v))
		if want := asn1Int(t, v); !bytes.Equal(got, want) {
			t.Errorf("EncodeInt(%d) = %x, want %x", v, got, want)
		}
	}
}

func TestEncodeInt_ResultNotAliased(t *testing.T) {
	a := EncodeInt(1)
	b := EncodeInt(2)
	if a[0] != 1 || b[0] != 2 {
		t.Fatalf("EncodeInt results alias: %x %x", a, b)
	}
}

func TestAppendLength_LongForms(t *testing.T) {
	cases := []struct {
		l    int
		want []byte
	}{
		{0, []byte{0x00}},
		{127, []byte{0x7f}},
		{128, []byte{0x81, 0x80}},
		{0xffff, []byte{0x82, 0xff, 0xff}},
		{0xffffff, []byte{0x83, 0xff, 0xff, 0xff}},
		// 16 MiB used to be written as 0x83 00 00 00 (length 0).
		{1 << 24, []byte{0x84, 0x01, 0x00, 0x00, 0x00}},
		{math.MaxInt32, []byte{0x84, 0x7f, 0xff, 0xff, 0xff}},
		{-5, []byte{0x00}},
	}
	for _, tc := range cases {
		got := AppendLength([]byte{0xee}, tc.l)
		if got[0] != 0xee || !bytes.Equal(got[1:], tc.want) {
			t.Errorf("AppendLength(%d) = %x, want ee%x", tc.l, got, tc.want)
		}
		if n := LengthSize(tc.l); n != len(tc.want) {
			t.Errorf("LengthSize(%d) = %d, want %d", tc.l, n, len(tc.want))
		}
	}
}

func TestDecodeInt32(t *testing.T) {
	for _, tc := range []struct {
		in   []byte
		want int32
		ok   bool
	}{
		{[]byte{0x7f, 0xff, 0xff, 0xff}, math.MaxInt32, true},
		{[]byte{0x80, 0x00, 0x00, 0x00}, math.MinInt32, true},
		{[]byte{0xff}, -1, true},
		{[]byte{0x00, 0x80, 0x00, 0x00, 0x00}, 0, false}, // 2^31: five octets
		{nil, 0, false},
	} {
		got, err := DecodeInt32(tc.in)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("DecodeInt32(%x) = %d, %v; want %d ok=%v", tc.in, got, err, tc.want, tc.ok)
		}
	}
}

// FuzzEncodeIntRoundTrip checks EncodeInt against encoding/asn1, which
// rejects non-minimal encodings.
func FuzzEncodeIntRoundTrip(f *testing.F) {
	for _, v := range []int64{0, -1, 127, 128, math.MaxInt32 + 1, math.MinInt64, math.MaxInt64} {
		f.Add(v)
	}
	f.Fuzz(func(t *testing.T, v int64) {
		if int64(int(v)) != v {
			return
		}
		enc := EncodeTLV(0x02, EncodeInt(int(v)))
		var got int64
		rest, err := asn1.Unmarshal(enc, &got)
		if err != nil || len(rest) != 0 || got != v {
			t.Fatalf("EncodeInt(%d) = %x: asn1 decodes %d, %v", v, enc, got, err)
		}
	})
}

// FuzzAppendLengthRoundTrip checks AppendLength against DecodeLength and
// LengthSize for every length DecodeLength accepts (below 2^24).
func FuzzAppendLengthRoundTrip(f *testing.F) {
	for _, l := range []uint32{0, 127, 128, 255, 256, 0xffff, 0x10000, 0xffffff} {
		f.Add(l)
	}
	f.Fuzz(func(t *testing.T, l32 uint32) {
		l := int(l32 & 0xffffff)
		enc := AppendLength(nil, l)
		if len(enc) != LengthSize(l) {
			t.Fatalf("len(AppendLength(%d)) = %d, LengthSize = %d", l, len(enc), LengthSize(l))
		}
		got, n, err := DecodeLength(enc)
		if err != nil || got != l || n != len(enc) {
			t.Fatalf("DecodeLength(AppendLength(%d) = %x) = %d, %d, %v", l, enc, got, n, err)
		}
	})
}
