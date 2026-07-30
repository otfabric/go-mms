// SPDX-License-Identifier: MIT

package berutil

import "testing"

func TestAppendLengthAndLengthSize_AllForms(t *testing.T) {
	cases := []struct {
		l    int
		want []byte
		size int
	}{
		{0, []byte{0x00}, 1},
		{127, []byte{0x7f}, 1},
		{128, []byte{0x81, 0x80}, 2},
		{255, []byte{0x81, 0xff}, 2},
		{256, []byte{0x82, 0x01, 0x00}, 3},
		{65535, []byte{0x82, 0xff, 0xff}, 3},
		{65536, []byte{0x83, 0x01, 0x00, 0x00}, 4},
		{0x010203, []byte{0x83, 0x01, 0x02, 0x03}, 4},
	}
	for _, tc := range cases {
		got := AppendLength(nil, tc.l)
		if string(got) != string(tc.want) {
			t.Fatalf("AppendLength(%d)=%x want %x", tc.l, got, tc.want)
		}
		if n := LengthSize(tc.l); n != tc.size {
			t.Fatalf("LengthSize(%d)=%d want %d", tc.l, n, tc.size)
		}
		// EncodeTLV uses AppendLength for large content.
		if tc.l <= 256 {
			content := make([]byte, tc.l)
			enc := EncodeTLV(0x04, content)
			if LengthSize(tc.l) != len(enc)-1-tc.l {
				t.Fatalf("EncodeTLV length field size mismatch for l=%d", tc.l)
			}
		}
	}
}
