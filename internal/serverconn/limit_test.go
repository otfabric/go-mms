// SPDX-License-Identifier: MIT

package serverconn

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/isostack"
	"github.com/otfabric/go-mms/internal/presentation"
)

// RejectAssociationLimit answers the pending AARQ with AARE
// rejected-transient, acse-service-user no-reason-given, and an
// Initiate-ErrorPDU resource capability-unavailable (ISO 9506-2 clauses
// 7.4.1 and 8.2).
func TestRejectAssociationLimit(t *testing.T) {
	tr := &scriptTransport{in: [][]byte{mustAssocReq(t, mustInitReq(t, 1000, 5, 5, 10))}}
	c := New(tr, discardLogger(), nopHandler, defaultOpts())
	ctx := context.Background()
	if _, err := c.ReceiveAssociation(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.RejectAssociationLimit(ctx); err != nil {
		t.Fatal(err)
	}
	sent := tr.sentFrames()
	if len(sent) != 1 {
		t.Fatalf("sent %d frames", len(sent))
	}
	_, err := isostack.DecodeAssociateResponse(sent[0])
	rj, ok := errors.AsType[*isostack.Rejection](err)
	if !ok || rj.ACSEResult != acse.ResultRejectedTransient || rj.DiagSource != acse.DiagSourceUser || rj.Diagnostic != acse.DiagUserNoReasonGiven {
		t.Fatalf("reject %+v (%v)", rj, err)
	}
	if want := []byte{0xaa, 0x05, 0xa0, 0x03, 0x83, 0x01, 0x04}; !bytes.Equal(rj.MmsPayload, want) {
		t.Fatalf("Initiate-Error %x, want %x", rj.MmsPayload, want)
	}

	// Without a pending association nothing is sent.
	if err := c.RejectAssociationLimit(ctx); err == nil || !strings.Contains(err.Error(), "no pending association") {
		t.Fatalf("second RejectAssociationLimit = %v", err)
	}
	// A refusal too large for a REFUSE SPDU is not sent.
	c.pendingReq = &isostack.AssociateRequest{Negotiation: presentation.Negotiation{Results: make([]presentation.ContextResult, 30000)}}
	if err := c.RejectAssociationLimit(ctx); err == nil || !strings.Contains(err.Error(), "encode REFUSE") {
		t.Fatalf("RejectAssociationLimit of an oversized refusal = %v", err)
	}
	if len(tr.sentFrames()) != 1 {
		t.Fatalf("sent %d frames, want 1", len(tr.sentFrames()))
	}
}
