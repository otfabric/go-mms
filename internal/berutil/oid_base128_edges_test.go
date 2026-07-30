// SPDX-License-Identifier: MIT

package berutil

import "testing"

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
