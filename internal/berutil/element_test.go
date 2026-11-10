// SPDX-License-Identifier: MIT

package berutil

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func unhex(t testing.TB, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestSpec_X690_8_1_DecodeElement covers the receive-side BER forms of
// X.690 §8.1: multi-octet identifiers (§8.1.2.4), non-minimal long-form
// lengths (§8.1.3.5 NOTE 2) and the indefinite form (§8.1.3.6).
func TestSpec_X690_8_1_DecodeElement(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		tag     int
		class   byte
		cons    bool
		content string
		n       int
		indef   bool
	}{
		{"short", "a1020500", 1, 0x80, true, "0500", 4, false},
		{"non-minimal 81", "a181020500", 1, 0x80, true, "0500", 5, false},
		{"non-minimal 82", "a18200020500", 1, 0x80, true, "0500", 6, false},
		{"non-minimal 84", "a1840000000105", 1, 0x80, true, "05", 7, false},
		{"long tag 46", "bf2e00", 46, 0x80, true, "", 3, false},
		{"long tag 200", "9f814801ff", 200, 0x80, false, "ff", 5, false},
		{"indefinite", "a18002010100 00", 1, 0x80, true, "020101", 7, true},
		{"nested indefinite", "a180a080020101000000 00", 1, 0x80, true, "a080020101 0000", 11, true},
		{"universal", "020101", 2, 0x00, false, "01", 3, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := unhex(t, despace(tc.in))
			el, n, err := DecodeElement(in, 0)
			if err != nil {
				t.Fatalf("DecodeElement(%x): %v", in, err)
			}
			if el.Tag != tc.tag || el.Class != tc.class || el.Constructed != tc.cons || n != tc.n || el.Indefinite != tc.indef ||
				!bytes.Equal(el.Content, unhex(t, despace(tc.content))) || !bytes.Equal(el.Full, in[:n]) {
				t.Fatalf("DecodeElement(%x) = %+v, %d", in, el, n)
			}
		})
	}
}

func despace(s string) string { return string(bytes.ReplaceAll([]byte(s), []byte(" "), nil)) }

func TestSpec_X690_8_1_DecodeElementErrors(t *testing.T) {
	deep := bytes.Repeat([]byte{0xa0, 0x80}, MaxIndefiniteDepth+1)
	for name, in := range map[string][]byte{
		"empty":                   nil,
		"missing length":          {0xa1},
		"truncated content":       {0xa1, 0x05, 0x00},
		"truncated long length":   {0xa1, 0x82, 0x00},
		"reserved length 0xff":    {0xa1, 0xff},
		"length too large":        {0xa1, 0x85, 0x01, 0x00, 0x00, 0x00, 0x00},
		"primitive indefinite":    {0x81, 0x80, 0x01, 0x00, 0x00},
		"missing end-of-contents": {0xa1, 0x80, 0x02, 0x01, 0x01},
		"bad inner indefinite":    {0xa1, 0x80, 0x02, 0x05, 0x01},
		"truncated long tag":      {0xbf, 0x81},
		"non-minimal long tag":    {0xbf, 0x80, 0x2e, 0x00},
		"huge long tag":           {0xbf, 0xff, 0xff, 0xff, 0xff, 0x7f, 0x00},
		"long tag no length":      {0xbf, 0x2e},
		"indefinite too deep":     deep,
	} {
		if _, _, err := DecodeElement(in, 0); err == nil {
			t.Errorf("%s: DecodeElement(%x) accepted", name, in)
		}
	}
	if _, _, err := DecodeElement([]byte{0x05, 0x00}, 5); err == nil {
		t.Error("offset beyond data accepted")
	}
}

func TestCanonicalize(t *testing.T) {
	minimal := unhex(t, "a1050201010500")
	out, changed, err := Canonicalize(minimal)
	if err != nil || changed || &out[0] != &minimal[0] {
		t.Fatalf("minimal input rewritten: %x, %v, %v", out, changed, err)
	}
	cases := map[string]string{
		"a18105020101 0500":               "a1050201010500",
		"a180020101050000 00":             "a1050201010500",
		"a180a080020101000000 00bf2e8100": "a105a003020101bf2e00",
	}
	for in, want := range cases {
		out, changed, err := Canonicalize(unhex(t, despace(in)))
		if err != nil || !changed || !bytes.Equal(out, unhex(t, want)) {
			t.Errorf("Canonicalize(%s) = %x, %v, %v; want %s", in, out, changed, err, want)
		}
	}
	if _, _, err := Canonicalize([]byte{0xa1, 0x80, 0x02}); err == nil {
		t.Error("malformed input canonicalized")
	}
	deep := []byte{0x05, 0x00}
	for range MaxIndefiniteDepth + 2 {
		deep = EncodeTLV(0xa0, deep)
	}
	deep = append([]byte{0xa0, 0x81, byte(len(deep))}, deep...)
	if _, _, err := Canonicalize(deep); err == nil {
		t.Error("nesting beyond MaxIndefiniteDepth canonicalized")
	}
}

// TestSpec_X690_8_3_NonMinimalIntegers: BER receivers accept redundant
// leading octets; the value is unchanged (MMS-PDU-06).
func TestSpec_X690_8_3_NonMinimalIntegers(t *testing.T) {
	for in, want := range map[string]int{"0001": 1, "000000ff": 255, "ffff": -1, "ff80": -128, "00000000007f": 127} {
		if got, err := DecodeInteger(unhex(t, in)); err != nil || got != want {
			t.Errorf("DecodeInteger(%s) = %d, %v; want %d", in, got, err, want)
		}
	}
	for in, want := range map[string]uint32{"0001": 1, "0000ff": 255, "000000ffffffff": 0xffffffff} {
		if got, err := DecodeUnsigned(unhex(t, in)); err != nil || got != want {
			t.Errorf("DecodeUnsigned(%s) = %d, %v; want %d", in, got, err, want)
		}
	}
	if got := TrimInteger(unhex(t, "0080")); !bytes.Equal(got, unhex(t, "0080")) {
		t.Errorf("TrimInteger(0080) = %x", got)
	}
}

// FuzzDecodeElement checks DecodeElement never panics, reports a
// consumed count within the input, and that Full/Content are slices of
// it consistent with Header.
func FuzzDecodeElement(f *testing.F) {
	for _, s := range []string{"a1020500", "a181020500", "a1800201010000", "bf2e00", "9f814801ff", "a180a08000000000", "ff00", "a1ff", "bf8000"} {
		f.Add(unhex(f, s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		el, n, err := DecodeElement(data, 0)
		if err != nil {
			return
		}
		if n <= 0 || n > len(data) || len(el.Full) != n || !bytes.Equal(el.Full, data[:n]) {
			t.Fatalf("DecodeElement(%x): n=%d full=%x", data, n, el.Full)
		}
		extra := 0
		if el.Indefinite {
			extra = 2
		}
		if len(el.Header)+len(el.Content)+extra != n {
			t.Fatalf("DecodeElement(%x): header %x + content %x != %d", data, el.Header, el.Content, n)
		}
	})
}

// FuzzCanonicalize checks that canonical output is a fixed point and
// that the element structure survives the rewrite.
func FuzzCanonicalize(f *testing.F) {
	for _, s := range []string{"a1050201010500", "a1810502010105 00", "a18002010105000000", "a180a080020101000000 00bf2e8100", "a180"} {
		f.Add(unhex(f, despace(s)))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		out, _, err := Canonicalize(data)
		if err != nil {
			return
		}
		again, changed, err := Canonicalize(out)
		if err != nil || changed || !bytes.Equal(again, out) {
			t.Fatalf("Canonicalize not idempotent on %x: %x -> %x, %v, %v", data, out, again, changed, err)
		}
	})
}
