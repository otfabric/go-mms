// SPDX-License-Identifier: MIT

package serverconn

import (
	"context"
	"errors"
	"testing"

	"github.com/otfabric/go-mms/internal/pdu"
)

// A localDetailCalling below minLocalDetailCalling is refused with
// Initiate-Error max-segment-insufficient; from the minimum on it is
// negotiated as proposed.
func TestNegotiateMinimumLocalDetailCalling(t *testing.T) {
	for _, tc := range []struct {
		proposed int
		ok       bool
	}{
		{1, false}, {24, false}, {minLocalDetailCalling - 1, false},
		{minLocalDetailCalling, true}, {minLocalDetailCalling + 1, true}, {65000, true},
	} {
		b, err := pdu.MarshalInitiateRequest(pdu.DefaultInitiateRequest(tc.proposed, 5, 5, 10))
		if err != nil {
			t.Fatal(err)
		}
		c := New(&scriptTransport{in: [][]byte{mustAssocReq(t, b)}}, discardLogger(), nopHandler, defaultOpts())
		_, err = c.ReceiveAssociation(context.Background())
		if !tc.ok {
			ie, ok := errors.AsType[*InitiateError](err)
			if !ok || ie.Code != pdu.InitiateErrMaxSegmentInsufficient {
				t.Errorf("localDetailCalling %d: err = %v, want max-segment-insufficient", tc.proposed, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("localDetailCalling %d: %v", tc.proposed, err)
			continue
		}
		if err := c.AcceptAssociation(context.Background()); err != nil || c.MaxPDUSize() != tc.proposed {
			t.Errorf("localDetailCalling %d: MaxPDUSize %d, %v", tc.proposed, c.MaxPDUSize(), err)
		}
	}
}
