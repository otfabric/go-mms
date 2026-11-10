// SPDX-License-Identifier: MIT

package isostack

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

// defaultRequest decodes the association request this package encodes
// with default parameters: contexts ACSE 1 and MMS 3, session version 2.
func defaultRequest(tb testing.TB) *AssociateRequest {
	tb.Helper()
	b, err := EncodeAssociateRequest(Params{}, []byte{0xa8, 0x00})
	if err != nil {
		tb.Fatal(err)
	}
	r, err := DecodeAssociateRequest(b)
	if err != nil {
		tb.Fatal(err)
	}
	return r
}

// encodeAccept builds the acceptance of defaultRequest with payload.
func encodeAccept(tb testing.TB, payload []byte) []byte {
	tb.Helper()
	return mustEncode(tb)(defaultRequest(tb).EncodeAssociateResponse(ResponseParams{}, payload))
}

// encodeReject builds a permanent AARE rejection of defaultRequest.
func encodeReject(tb testing.TB) []byte {
	tb.Helper()
	return mustEncode(tb)(defaultRequest(tb).EncodeReject(Reject{Result: 1, DiagSource: 1, Diagnostic: 1}))
}
