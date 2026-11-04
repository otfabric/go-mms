// SPDX-License-Identifier: MIT

package e2e

import (
	"bytes"
	"context"
	"testing"

	mms "github.com/otfabric/go-mms"
)

// An AARQ with a long ACSE password pushes the session CONNECT SPDU past
// 254 bytes, which needs the extended LI form (ISO 8327-1). The encoder
// used to write a one-octet LI, so such an association always failed.
func TestAssociation_LongPassword(t *testing.T) {
	for _, n := range []int{8, 200, 255, 1000, 8000} {
		want := bytes.Repeat([]byte{'p'}, n)
		got := make(chan []byte, 4)
		m := startModel(t, modelOptions{auth: func(_ context.Context, a *mms.AuthContext) (mms.AuthResult, error) {
			got <- bytes.Clone(a.Password)
			return mms.AuthResult{Accept: bytes.Equal(a.Password, want)}, nil
		}})
		c := dial(t, m.addr, mms.DialOptions{ISO: mms.ISOOptions{Password: want}})
		if _, err := c.Identify(timeout(t)); err != nil {
			t.Fatalf("password %d bytes: Identify: %v", n, err)
		}
		if p := <-got; !bytes.Equal(p, want) {
			t.Fatalf("password %d bytes: server saw %d bytes", n, len(p))
		}
	}
}
