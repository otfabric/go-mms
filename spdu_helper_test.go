// SPDX-License-Identifier: MIT

package mms

import "testing"

// mustEncode returns a function that fails tb when an SPDU encoder
// returns an error: mustEncode(t)(EncodeAccept(p, data)).
func mustEncode(tb testing.TB) func([]byte, error) []byte {
	return func(b []byte, err error) []byte {
		tb.Helper()
		if err != nil {
			tb.Fatal(err)
		}
		return b
	}
}
