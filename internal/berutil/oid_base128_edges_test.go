// SPDX-License-Identifier: MIT

package berutil

import (
	"bytes"
	"fmt"
	"math"
	"testing"
)

func TestDecodeBase128_Edges(t *testing.T) {
	if _, _, err := decodeBase128([]byte{0x81}, 0); err == nil {
		t.Fatal("truncated")
	}
	// 6 continuation bytes → too large.
	tooBig := []byte{0x81, 0x80, 0x80, 0x80, 0x80, 0x80, 0x00}
	if _, _, err := decodeBase128(tooBig, 0); err == nil {
		t.Fatal("too large")
	}
	v, n, err := decodeBase128([]byte{0x81, 0x00}, 0)
	if err != nil || v != 0x80 || n != 2 {
		t.Fatalf("v=%d n=%d err=%v", v, n, err)
	}
}

func TestDecodeObjectIdentifier_Edges(t *testing.T) {
	if _, err := DecodeObjectIdentifier([]byte{0x81}); err == nil {
		t.Fatal("truncated first")
	}
	// Valid first arc then truncated subsequent arc.
	if _, err := DecodeObjectIdentifier([]byte{0x2a, 0x81}); err == nil {
		t.Fatal("truncated arc")
	}
	got, err := DecodeObjectIdentifier([]byte{0x2a, 0x86, 0x48}) // 1.2.840
	if err != nil || len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 840 {
		t.Fatalf("%v %v", got, err)
	}
}

// X.690 §8.19.5 example: {2 999 3} encodes as 88 37 03. Only the first
// arc 2 may carry a second arc >= 40.
func TestObjectIdentifier_X690FirstArcs(t *testing.T) {
	cases := []struct {
		oid  []int
		want []byte
	}{
		{[]int{2, 999, 3}, []byte{0x88, 0x37, 0x03}},
		{[]int{0, 39}, []byte{0x27}},
		{[]int{1, 0, 9506}, []byte{0x28, 0xca, 0x22}},
		{[]int{2, 40}, []byte{0x78}},
	}
	for _, c := range cases {
		enc, err := EncodeObjectIdentifier(c.oid)
		if err != nil {
			t.Fatalf("Encode %v: %v", c.oid, err)
		}
		if !bytes.Equal(enc, c.want) {
			t.Errorf("Encode %v = % x, want % x", c.oid, enc, c.want)
		}
		dec, err := DecodeObjectIdentifier(enc)
		if err != nil {
			t.Fatalf("Decode % x: %v", enc, err)
		}
		if fmt.Sprint(dec) != fmt.Sprint(c.oid) {
			t.Errorf("Decode % x = %v, want %v", enc, dec, c.oid)
		}
	}
	for _, bad := range [][]int{{3, 1}, {0, 40}, {1, 40}, {-1, 0}, {2, -1}, {1, 2, -3}} {
		if _, err := EncodeObjectIdentifier(bad); err == nil {
			t.Errorf("Encode %v: expected error", bad)
		}
	}
}

// An OID arc is kept in an int: a value of 2^31 or more is the value
// itself where an int holds it and an error where it does not, never a
// wrapped or negative arc.
func TestDecodeBase128PlatformWidth(t *testing.T) {
	wide := math.MaxInt == math.MaxInt64
	for _, tc := range []struct {
		in   []byte
		want int64
	}{
		{[]byte{0x87, 0xff, 0xff, 0xff, 0x7f}, math.MaxInt32},
		{[]byte{0x88, 0x80, 0x80, 0x80, 0x00}, math.MaxInt32 + 1},
		{[]byte{0x8f, 0xff, 0xff, 0xff, 0x7f}, math.MaxUint32},
		{[]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0x7f}, 1<<42 - 1}, // six octets
	} {
		v, n, err := decodeBase128(tc.in, 0)
		if wide || tc.want <= math.MaxInt32 {
			if err != nil || int64(v) != tc.want || n != len(tc.in) {
				t.Errorf("% x: %d (%d octets), %v; want %d", tc.in, v, n, err, tc.want)
			}
			continue
		}
		if err == nil {
			t.Errorf("% x: %d, want an error on a 32-bit platform", tc.in, v)
		}
	}
}
