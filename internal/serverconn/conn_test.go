// SPDX-License-Identifier: MIT

package serverconn

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/isostack"
	"github.com/otfabric/go-mms/internal/pdu"
)

type chanTransport struct {
	send chan []byte
	recv chan []byte

	mu     sync.Mutex
	closed bool
}

func (t *chanTransport) Send(_ context.Context, data []byte) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return errors.New("transport closed")
	}
	t.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	t.send <- cp
	return nil
}

func (t *chanTransport) Receive(ctx context.Context) ([]byte, error) {
	select {
	case data := <-t.recv:
		if data == nil {
			return nil, errors.New("transport closed")
		}
		return data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (t *chanTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed {
		t.closed = true
		close(t.send)
	}
	return nil
}

func loopbackPair() (*chanTransport, *chanTransport) {
	c2s := make(chan []byte, 16)
	s2c := make(chan []byte, 16)
	return &chanTransport{send: c2s, recv: s2c}, &chanTransport{send: s2c, recv: c2s}
}

func TestServiceErrorError(t *testing.T) {
	e := &ServiceError{ErrorClass: 1, ErrorCode: 2}
	s := e.Error()
	if s == "" {
		t.Fatal("empty error string")
	}
}

func TestNewConn(t *testing.T) {
	client, server := loopbackPair()
	defer client.Close()
	defer server.Close()

	handler := func(_ context.Context, _ codec.InvokeID, _ int, _ []byte) (int, bool, []byte, error) {
		return 0, false, nil, nil
	}
	c := New(server, slog.Default(), handler, MMSOptions{
		MaxPDUSize:                65000,
		MaxOutstandingCalling:     5,
		MaxOutstandingCalled:      5,
		DataStructureNestingLevel: 10,
	})
	if c == nil {
		t.Fatal("nil conn")
	}
}

func TestRejectAssociation(t *testing.T) {
	client, server := loopbackPair()
	defer client.Close()

	handler := func(_ context.Context, _ codec.InvokeID, _ int, _ []byte) (int, bool, []byte, error) {
		return 0, false, nil, nil
	}
	c := New(server, slog.Default(), handler, MMSOptions{
		MaxPDUSize:                65000,
		MaxOutstandingCalling:     5,
		MaxOutstandingCalled:      5,
		DataStructureNestingLevel: 10,
	})

	mmsPayload, _ := codec.MarshalMmsPdu(0xa8, pdu.DefaultInitiateRequest(65000, 5, 5, 10))
	assocReq, _ := isostack.EncodeAssociateRequest(isostack.Params{}, mmsPayload)
	client.send <- assocReq

	ctx := context.Background()
	_, err := c.ReceiveAssociation(ctx)
	if err != nil {
		t.Fatalf("ReceiveAssociation: %v", err)
	}

	if err := c.RejectAssociation(ctx); err != nil {
		t.Fatalf("RejectAssociation: %v", err)
	}
}

func TestAcceptAssociationWithoutReceive(t *testing.T) {
	_, server := loopbackPair()
	defer server.Close()

	handler := func(_ context.Context, _ codec.InvokeID, _ int, _ []byte) (int, bool, []byte, error) {
		return 0, false, nil, nil
	}
	c := New(server, slog.Default(), handler, MMSOptions{})
	err := c.AcceptAssociation(context.Background())
	if err == nil {
		t.Fatal("expected error when no pending association")
	}
}

func TestClampMin(t *testing.T) {
	if clampMin(5, 10) != 10 {
		t.Fatal("should clamp up")
	}
	if clampMin(15, 10) != 15 {
		t.Fatal("should not clamp")
	}
}

func TestFullAssociationAndConclude(t *testing.T) {
	client, server := loopbackPair()

	handler := func(_ context.Context, _ codec.InvokeID, _ int, _ []byte) (int, bool, []byte, error) {
		return 0, false, nil, nil
	}
	c := New(server, slog.Default(), handler, MMSOptions{
		MaxPDUSize:                65000,
		MaxOutstandingCalling:     5,
		MaxOutstandingCalled:      5,
		DataStructureNestingLevel: 10,
	})

	mmsPayload, _ := codec.MarshalMmsPdu(0xa8, pdu.DefaultInitiateRequest(65000, 5, 5, 10))
	assocReq, _ := isostack.EncodeAssociateRequest(isostack.Params{}, mmsPayload)
	client.send <- assocReq

	ctx := context.Background()
	_, err := c.ReceiveAssociation(ctx)
	if err != nil {
		t.Fatalf("ReceiveAssociation: %v", err)
	}
	if err := c.AcceptAssociation(ctx); err != nil {
		t.Fatalf("AcceptAssociation: %v", err)
	}

	// Drain the accept response from the server side
	<-client.recv

	// Send a conclude request
	concludePdu := codec.MarshalConcludeRequest()
	concludeData := isostack.EncodeDataRequest(concludePdu)
	client.send <- concludeData

	// Serve should process the conclude and return
	err = c.Serve(ctx)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
}

func TestFullAssociationAndRelease(t *testing.T) {
	client, server := loopbackPair()

	handler := func(_ context.Context, _ codec.InvokeID, _ int, _ []byte) (int, bool, []byte, error) {
		return 0, false, nil, nil
	}
	c := New(server, slog.Default(), handler, MMSOptions{
		MaxPDUSize:                65000,
		MaxOutstandingCalling:     5,
		MaxOutstandingCalled:      5,
		DataStructureNestingLevel: 10,
	})

	mmsPayload, _ := codec.MarshalMmsPdu(0xa8, pdu.DefaultInitiateRequest(65000, 5, 5, 10))
	assocReq, _ := isostack.EncodeAssociateRequest(isostack.Params{}, mmsPayload)
	client.send <- assocReq

	ctx := context.Background()
	_, err := c.ReceiveAssociation(ctx)
	if err != nil {
		t.Fatalf("ReceiveAssociation: %v", err)
	}
	if err := c.AcceptAssociation(ctx); err != nil {
		t.Fatalf("AcceptAssociation: %v", err)
	}

	<-client.recv

	// Send a release (FINISH) request
	releaseData := isostack.EncodeReleaseRequest()
	client.send <- releaseData

	err = c.Serve(ctx)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
}

func TestConfirmedRequestHandling(t *testing.T) {
	client, server := loopbackPair()

	handler := func(_ context.Context, _ codec.InvokeID, serviceTag int, _ []byte) (int, bool, []byte, error) {
		return serviceTag, false, []byte{0x01}, nil
	}
	c := New(server, slog.Default(), handler, MMSOptions{
		MaxPDUSize:                65000,
		MaxOutstandingCalling:     5,
		MaxOutstandingCalled:      5,
		DataStructureNestingLevel: 10,
	})

	mmsPayload, _ := codec.MarshalMmsPdu(0xa8, pdu.DefaultInitiateRequest(65000, 5, 5, 10))
	assocReq, _ := isostack.EncodeAssociateRequest(isostack.Params{}, mmsPayload)
	client.send <- assocReq

	ctx := context.Background()
	_, err := c.ReceiveAssociation(ctx)
	if err != nil {
		t.Fatalf("ReceiveAssociation: %v", err)
	}
	if err := c.AcceptAssociation(ctx); err != nil {
		t.Fatalf("AcceptAssociation: %v", err)
	}
	<-client.recv

	// Send a confirmed request
	reqPdu, _ := codec.MarshalConfirmedRequest(1, 4, true, []byte{0x01, 0x01, 0xff})
	reqData := isostack.EncodeDataRequest(reqPdu)
	client.send <- reqData

	// Run Serve in goroutine, receive response, then conclude
	done := make(chan error, 1)
	go func() {
		done <- c.Serve(ctx)
	}()

	// Read the confirmed response
	<-client.recv

	// Conclude
	concludePdu := codec.MarshalConcludeRequest()
	client.send <- isostack.EncodeDataRequest(concludePdu)

	err = <-done
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
}

func TestServiceErrorHandling(t *testing.T) {
	client, server := loopbackPair()

	handler := func(_ context.Context, _ codec.InvokeID, _ int, _ []byte) (int, bool, []byte, error) {
		return 0, false, nil, &ServiceError{ErrorClass: 1, ErrorCode: 2}
	}
	c := New(server, slog.Default(), handler, MMSOptions{
		MaxPDUSize:                65000,
		MaxOutstandingCalling:     5,
		MaxOutstandingCalled:      5,
		DataStructureNestingLevel: 10,
	})

	mmsPayload, _ := codec.MarshalMmsPdu(0xa8, pdu.DefaultInitiateRequest(65000, 5, 5, 10))
	assocReq, _ := isostack.EncodeAssociateRequest(isostack.Params{}, mmsPayload)
	client.send <- assocReq

	ctx := context.Background()
	c.ReceiveAssociation(ctx) //nolint:errcheck
	c.AcceptAssociation(ctx)  //nolint:errcheck
	<-client.recv

	reqPdu, _ := codec.MarshalConfirmedRequest(1, 4, true, []byte{0x01, 0x01, 0xff})
	client.send <- isostack.EncodeDataRequest(reqPdu)

	done := make(chan error, 1)
	go func() {
		done <- c.Serve(ctx)
	}()

	// Read the error response
	<-client.recv

	concludePdu := codec.MarshalConcludeRequest()
	client.send <- isostack.EncodeDataRequest(concludePdu)

	if err := <-done; err != nil {
		t.Fatalf("Serve: %v", err)
	}
}

func TestSendUnconfirmed(t *testing.T) {
	client, server := loopbackPair()
	defer client.Close()

	handler := func(_ context.Context, _ codec.InvokeID, _ int, _ []byte) (int, bool, []byte, error) {
		return 0, false, nil, nil
	}
	c := New(server, slog.Default(), handler, MMSOptions{})

	ctx := context.Background()
	err := c.SendUnconfirmed(ctx, []byte{0xa0, 0x01, 0x00})
	if err != nil {
		t.Fatalf("SendUnconfirmed: %v", err)
	}
	<-client.recv
}
