// SPDX-License-Identifier: MIT

package asn1util

import "testing"

func TestEncodeTagNumber_AllForms(t *testing.T) {
	if got := encodeTagNumber(0x7f); len(got) != 1 || got[0] != 0x7f {
		t.Fatalf("short: %x", got)
	}
	// Two-byte form: 0x80 ≤ n < 0x4000
	got := encodeTagNumber(0x80)
	if len(got) != 2 || got[0] != 0x81 || got[1] != 0x00 {
		t.Fatalf("two-byte 0x80: %x", got)
	}
	got = encodeTagNumber(0x3fff)
	if len(got) != 2 || got[0] != 0xff || got[1] != 0x7f {
		t.Fatalf("two-byte max: %x", got)
	}
	// Three-byte form
	got = encodeTagNumber(0x4000)
	if len(got) != 3 || got[0] != 0x81 || got[1] != 0x80 || got[2] != 0x00 {
		t.Fatalf("three-byte: %x", got)
	}
}

func TestLengthHelpers_AllForms(t *testing.T) {
	cases := []struct {
		l    int
		size int
		want []byte
	}{
		{0, 1, []byte{0x00}},
		{127, 1, []byte{0x7f}},
		{128, 2, []byte{0x81, 0x80}},
		{255, 2, []byte{0x81, 0xff}},
		{256, 3, []byte{0x82, 0x01, 0x00}},
		{65535, 3, []byte{0x82, 0xff, 0xff}},
		{65536, 4, []byte{0x83, 0x01, 0x00, 0x00}},
	}
	for _, tc := range cases {
		if n := lengthSize(tc.l); n != tc.size {
			t.Fatalf("lengthSize(%d)=%d", tc.l, n)
		}
		got := appendLength(nil, tc.l)
		if string(got) != string(tc.want) {
			t.Fatalf("appendLength(%d)=%x want %x", tc.l, got, tc.want)
		}
	}

	// Exercise via WrapContextTag with large tag + large content.
	big := make([]byte, 256)
	enc := WrapContextTag(0x80, true, big) // hits two-byte tag + 0x82 length
	if len(enc) < 256 {
		t.Fatal("expected large encoding")
	}
	huge := make([]byte, 65536)
	enc = WrapContextTag(0x4000, false, huge) // three-byte tag + 0x83 length
	if len(enc) < 65536 {
		t.Fatal("expected huge encoding")
	}
}
