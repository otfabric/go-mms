// SPDX-License-Identifier: MIT

package serverconn

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/isostack"
	"github.com/otfabric/go-mms/internal/pdu"
)

func TestSendAbort(t *testing.T) {
	client, server := loopbackPair()
	defer func() { _ = client.Close() }()

	c := New(server, slog.Default(), nil, MMSOptions{
		MaxPDUSize: 65000, MaxOutstandingCalling: 5, MaxOutstandingCalled: 5, DataStructureNestingLevel: 10,
	})
	ctx := context.Background()
	if err := c.SendAbort(ctx); err != nil {
		t.Fatalf("SendAbort: %v", err)
	}
	select {
	case data := <-client.recv:
		if len(data) == 0 {
			t.Fatal("empty abort PDU")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for abort")
	}
}

func TestServe_CancelRequest(t *testing.T) {
	client, server := loopbackPair()

	handler := func(_ context.Context, _ codec.InvokeID, _ int, _ []byte) (int, bool, []byte, error) {
		return 0, false, nil, nil
	}
	c := New(server, slog.Default(), handler, MMSOptions{
		MaxPDUSize: 65000, MaxOutstandingCalling: 5, MaxOutstandingCalled: 5, DataStructureNestingLevel: 10,
	})

	mmsPayload, _ := pdu.MarshalInitiateRequest(pdu.DefaultInitiateRequest(65000, 5, 5, 10))
	assocReq, _ := isostack.EncodeAssociateRequest(isostack.Params{}, mmsPayload)
	client.send <- assocReq

	ctx := context.Background()
	if _, err := c.ReceiveAssociation(ctx); err != nil {
		t.Fatalf("ReceiveAssociation: %v", err)
	}
	if err := c.AcceptAssociation(ctx); err != nil {
		t.Fatalf("AcceptAssociation: %v", err)
	}
	<-client.recv

	// CancelRequestPDU: context 6 primitive + Unsigned32 invoke ID.
	cancelPdu := asn1util.WrapPrimitive(6, berutil.EncodeUint32(42))
	client.send <- isostack.EncodeDataRequest(cancelPdu)

	// Follow with conclude so Serve returns.
	go func() {
		time.Sleep(30 * time.Millisecond)
		client.send <- isostack.EncodeDataRequest(codec.MarshalConcludeRequest())
	}()

	if err := c.Serve(ctx); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	// Expect CancelError then ConcludeResponse.
	gotCancel := false
	deadline := time.After(time.Second)
drain:
	for {
		select {
		case data := <-client.recv:
			mmsPayload, err := isostack.DecodeDataResponse(data)
			if err != nil {
				continue
			}
			if len(mmsPayload) > 0 && mmsPayload[0] == 0xa8 {
				gotCancel = true
			}
			if gotCancel {
				break drain
			}
		case <-deadline:
			break drain
		}
	}
	if !gotCancel {
		t.Fatal("expected CancelError PDU (0xa8)")
	}
}

func TestServe_CancelRequestMalformed(t *testing.T) {
	client, server := loopbackPair()

	handler := func(_ context.Context, _ codec.InvokeID, _ int, _ []byte) (int, bool, []byte, error) {
		return 0, false, nil, nil
	}
	c := New(server, slog.Default(), handler, MMSOptions{
		MaxPDUSize: 64, MaxOutstandingCalling: 5, MaxOutstandingCalled: 5, DataStructureNestingLevel: 10,
	})

	mmsPayload, _ := pdu.MarshalInitiateRequest(pdu.DefaultInitiateRequest(65000, 5, 5, 10))
	assocReq, _ := isostack.EncodeAssociateRequest(isostack.Params{}, mmsPayload)
	client.send <- assocReq

	ctx := context.Background()
	if _, err := c.ReceiveAssociation(ctx); err != nil {
		t.Fatalf("ReceiveAssociation: %v", err)
	}
	if err := c.AcceptAssociation(ctx); err != nil {
		t.Fatalf("AcceptAssociation: %v", err)
	}
	<-client.recv

	// Oversized PDU skipped; malformed cancel content logged and ignored.
	big := make([]byte, 200)
	client.send <- isostack.EncodeDataRequest(big)

	badCancel := asn1util.WrapPrimitive(6, []byte{0xff, 0xff, 0xff, 0xff, 0xff}) // invalid unsigned
	client.send <- isostack.EncodeDataRequest(badCancel)

	go func() {
		time.Sleep(30 * time.Millisecond)
		client.send <- isostack.EncodeDataRequest(codec.MarshalConcludeRequest())
	}()
	if err := c.Serve(ctx); err != nil {
		t.Fatalf("Serve: %v", err)
	}
}

func TestServe_UnexpectedPDUKind(t *testing.T) {
	client, server := loopbackPair()

	handler := func(_ context.Context, _ codec.InvokeID, _ int, _ []byte) (int, bool, []byte, error) {
		return 0, false, nil, nil
	}
	c := New(server, slog.Default(), handler, MMSOptions{
		MaxPDUSize: 65000, MaxOutstandingCalling: 5, MaxOutstandingCalled: 5, DataStructureNestingLevel: 10,
	})

	mmsPayload, _ := pdu.MarshalInitiateRequest(pdu.DefaultInitiateRequest(65000, 5, 5, 10))
	assocReq, _ := isostack.EncodeAssociateRequest(isostack.Params{}, mmsPayload)
	client.send <- assocReq

	ctx := context.Background()
	if _, err := c.ReceiveAssociation(ctx); err != nil {
		t.Fatalf("ReceiveAssociation: %v", err)
	}
	if err := c.AcceptAssociation(ctx); err != nil {
		t.Fatalf("AcceptAssociation: %v", err)
	}
	<-client.recv

	// Reject PDU kind (unexpected on server Serve loop).
	reject := codec.MarshalRejectPDU(0, 1, 0)
	client.send <- isostack.EncodeDataRequest(reject)

	go func() {
		time.Sleep(30 * time.Millisecond)
		client.send <- isostack.EncodeDataRequest(codec.MarshalConcludeRequest())
	}()
	if err := c.Serve(ctx); err != nil {
		t.Fatalf("Serve: %v", err)
	}
}
