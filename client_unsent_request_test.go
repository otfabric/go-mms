// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"testing"

	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/pdu"
)

// A request queued behind another write is not yet on the wire, so no PDU
// from the peer can answer it (ISO 9506-2 clause 8.6: a Reject answers a
// received PDU; a confirmed response answers an issued request). These
// tests hold one write in the transport while a second request waits for
// the send lock, let the peer send a PDU, and check that the queued
// request is either written and answered by its own response, or fails
// with nothing written.

// unsentSetup starts a client on a mock server behind a gateTransport.
func unsentSetup(t *testing.T) (*Client, *mockServer, *gateTransport) {
	t.Helper()
	ctx := context.Background()
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	go srv.handleAssociation(ctx)
	gt := &gateTransport{Transport: mt, entered: make(chan struct{}, 1), gate: make(chan struct{})}
	c, err := NewClient(ctx, gt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Abort(ctx) })
	return c, srv, gt
}

// startHeldIdentify starts an Identify whose write is held in the
// transport (it holds the send lock), and returns its result channel.
func startHeldIdentify(c *Client, gt *gateTransport) <-chan error {
	gt.hold.Store(true)
	res := make(chan error, 1)
	go func() { _, err := c.Identify(context.Background()); res <- err }()
	<-gt.entered
	return res
}

// startQueuedWrite starts a Write that waits for the send lock behind
// the held write, once want requests are outstanding.
func startQueuedWrite(t *testing.T, c *Client, want int) <-chan error {
	t.Helper()
	res := make(chan error, 1)
	go func() {
		_, err := c.Write(context.Background(), WriteRequest{DomainID: "D", ItemID: "V", Value: NewInteger(1)})
		res <- err
	}()
	until(t, func() bool { return c.tracker.PendingCount() == want })
	return res
}

// expectRequest reads the next request the server receives.
func expectRequest(t *testing.T, srv *mockServer, wantID codec.InvokeID, wantKind pdu.ServiceKind) {
	t.Helper()
	id, kind, _ := srv.handleDataRequest(context.Background())
	if id != wantID || kind != wantKind {
		t.Fatalf("server received %s invoke %d, want %s invoke %d", kind, id, wantKind, wantID)
	}
}

// A Reject without invoke ID goes to the only request on the wire, not
// to a request still waiting to be written; that one is then written and
// gets its own response.
func TestUnsentRequest_RejectWithoutInvokeID_Sole(t *testing.T) {
	ctx := context.Background()
	c, srv, gt := unsentSetup(t)

	xres := startHeldIdentify(c, gt)  // invoke 1, on the wire
	yres := startQueuedWrite(t, c, 2) // invoke 2, queued

	srv.sendDataResponse(ctx, codec.MarshalRejectPDUNoInvokeID(int(RejectConfirmedRequest), 1))
	until(t, func() bool { return c.tracker.PendingCount() == 1 })
	close(gt.gate)

	if err := <-xres; !errors.As(err, new(*RejectError)) {
		t.Errorf("Identify = %v, want the Reject", err)
	}
	expectRequest(t, srv, 1, pdu.ServiceIdentify)
	expectRequest(t, srv, 2, pdu.ServiceWrite)
	srv.sendWriteResponse(ctx, 2, true, 0)
	if err := <-yres; err != nil {
		t.Errorf("queued Write = %v, want its own (successful) response", err)
	}
}

// A Reject without invoke ID with several requests on the wire fails
// those, and leaves the queued request to be written and answered.
func TestUnsentRequest_RejectWithoutInvokeID_Several(t *testing.T) {
	ctx := context.Background()
	c, srv, gt := unsentSetup(t)

	x1res := make(chan error, 1)
	go func() { _, err := c.Identify(ctx); x1res <- err }()
	expectRequest(t, srv, 1, pdu.ServiceIdentify) // invoke 1 written, unanswered
	x2res := startHeldIdentify(c, gt)             // invoke 2, on the wire
	yres := startQueuedWrite(t, c, 3)             // invoke 3, queued

	srv.sendDataResponse(ctx, codec.MarshalRejectPDUNoInvokeID(int(RejectConfirmedRequest), 1))
	until(t, func() bool { return c.tracker.PendingCount() == 1 })
	close(gt.gate)

	for _, res := range []<-chan error{x1res, x2res} {
		if err := <-res; !errors.As(err, new(*RejectError)) {
			t.Errorf("Identify = %v, want the Reject", err)
		}
	}
	expectRequest(t, srv, 2, pdu.ServiceIdentify)
	expectRequest(t, srv, 3, pdu.ServiceWrite)
	srv.sendWriteResponse(ctx, 3, true, 0)
	if err := <-yres; err != nil {
		t.Errorf("queued Write = %v, want its own (successful) response", err)
	}
}

// A response carrying the invoke ID of a request not yet written is
// handled as one for an unknown invoke ID: the request is written and
// gets its own response.
func TestUnsentRequest_ResponseForNextInvokeID(t *testing.T) {
	ctx := context.Background()
	c, srv, gt := unsentSetup(t)

	xres := startHeldIdentify(c, gt)  // invoke 1, on the wire
	yres := startQueuedWrite(t, c, 2) // invoke 2, queued

	srv.sendWriteResponse(ctx, 2, false, 3) // object-access-denied, before invoke 2 is sent
	srv.sendIdentifyResponse(ctx, 1)        // read after it: marks that the reader has passed
	until(t, func() bool { return c.tracker.PendingCount() == 1 })
	close(gt.gate)

	if err := <-xres; err != nil {
		t.Errorf("Identify = %v", err)
	}
	expectRequest(t, srv, 1, pdu.ServiceIdentify)
	expectRequest(t, srv, 2, pdu.ServiceWrite)
	srv.sendWriteResponse(ctx, 2, true, 0)
	if err := <-yres; err != nil {
		t.Errorf("queued Write = %v, want its own (successful) response", err)
	}
}

// A queued request whose tracker entry is removed before its turn to be
// written fails with the result delivered to it, and nothing of it is
// written.
func TestUnsentRequest_EndedBeforeWriteIsNotSent(t *testing.T) {
	c, srv, gt := unsentSetup(t)

	xres := startHeldIdentify(c, gt)  // invoke 1, on the wire
	yres := startQueuedWrite(t, c, 2) // invoke 2, queued

	errEnded := errors.New("ended")
	if !c.tracker.Cancel(2, errEnded) {
		t.Fatal("queued request not pending")
	}
	gt.mu.Lock()
	from := len(gt.sent)
	gt.mu.Unlock()
	close(gt.gate)

	if err := <-yres; !errors.Is(err, errEnded) {
		t.Errorf("queued Write = %v, want the delivered result", err)
	}
	expectRequest(t, srv, 1, pdu.ServiceIdentify)
	srv.sendIdentifyResponse(context.Background(), 1)
	if err := <-xres; err != nil {
		t.Errorf("Identify = %v", err)
	}
	if n := gt.confirmedRequests(t, from); n != 1 {
		t.Errorf("%d confirmed requests written, want 1: the ended request was written", n)
	}
}

// sendRequest skips the write and reports errRequestEnded when markSent
// finds the entry gone.
func TestSendRequest_MarkSentGone(t *testing.T) {
	clientConn, _ := loopbackPair()
	c := &Client{conn: clientConn}
	onWire, err := c.sendRequest(context.Background(), []byte{1}, func() bool { return false })
	if onWire || !errors.Is(err, errRequestEnded) {
		t.Errorf("sendRequest = %v, %v; want false, errRequestEnded", onWire, err)
	}
	select {
	case d := <-clientConn.(*chanTransport).send:
		t.Errorf("PDU was sent: %x", d)
	default:
	}
}

// A confirmed response whose invoke ID cannot be read fails the only
// request on the wire with a decode error.
func TestUnsentRequest_ResponseWithoutInvokeIDFailsSole(t *testing.T) {
	ctx := context.Background()
	c, srv, _ := unsentSetup(t)

	res := make(chan error, 1)
	go func() { _, err := c.Identify(ctx); res <- err }()
	expectRequest(t, srv, 1, pdu.ServiceIdentify)
	srv.sendDataResponse(ctx, []byte{0xa1, 0x02, 0x05, 0x00}) // first element is not the invoke ID
	if err := <-res; !errors.Is(err, ErrDecodeFailed) {
		t.Errorf("Identify = %v, want a decode error", err)
	}
}
