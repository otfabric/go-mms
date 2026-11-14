// SPDX-License-Identifier: MIT

package berutil

import (
	"slices"
	"testing"
)

// longTag is a context-specific primitive element without content whose
// identifier carries the multi-octet tag number n (X.690 §8.1.2.4).
func longTag(n uint64) []byte {
	rev := []byte{byte(n & 0x7f)}
	for n >>= 7; n > 0; n >>= 7 {
		rev = append(rev, byte(n&0x7f)|0x80)
	}
	slices.Reverse(rev)
	return append(append([]byte{0x9f}, rev...), 0x00)
}

// A multi-octet tag number decodes up to maxTagNumber and is refused
// beyond it, with the same result on 32-bit and 64-bit platforms.
func TestDecodeElementTagNumberBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name string
		n    uint64
		ok   bool
	}{
		{"31", 31, true},
		{"max-1", maxTagNumber - 1, true},
		{"max", maxTagNumber, true},
		{"max+1", maxTagNumber + 1, false},
		{"2^31", 1 << 31, false},
		{"2^32", 1 << 32, false},
		{"2^35-1", 1<<35 - 1, false},
	} {
		data := longTag(tc.n)
		el, n, err := DecodeElement(data, 0)
		switch {
		case tc.ok && err != nil:
			t.Errorf("%s (% x): %v", tc.name, data, err)
		case tc.ok && (el.Tag < 0 || uint64(el.Tag) != tc.n || n != len(data)):
			t.Errorf("%s (% x): tag %d, %d octets; want %d, %d", tc.name, data, el.Tag, n, tc.n, len(data))
		case !tc.ok && err == nil:
			t.Errorf("%s (% x): accepted as tag %d, want an error", tc.name, data, el.Tag)
		}
	}
}
