// SPDX-License-Identifier: MIT

package mms

import (
	"testing"

	"github.com/otfabric/go-mms/internal/isostack"
)

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

// encodeAccept builds a server's acceptance of the association request
// this package's client sends (contexts ACSE 1, MMS 3) carrying payload.
func encodeAccept(tb testing.TB, payload []byte) []byte {
	tb.Helper()
	b, err := isostack.EncodeAssociateRequest(isostack.Params{}, []byte{0xa8, 0x00})
	if err != nil {
		tb.Fatal(err)
	}
	req, err := isostack.DecodeAssociateRequest(b)
	if err != nil {
		tb.Fatal(err)
	}
	return mustEncode(tb)(req.EncodeAssociateResponse(isostack.ResponseParams{}, payload))
}
