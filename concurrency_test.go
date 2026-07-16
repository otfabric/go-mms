// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/pdu"
)

// TestConcurrentReads verifies that multiple goroutines can issue Read
// operations without data races. The mock server handles each request
// sequentially, so this primarily tests for races in client state.
func TestConcurrentReads(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	const numReads = 5
	var wg sync.WaitGroup
	errs := make(chan error, numReads)

	// Server goroutine handles all requests.
	go func() {
		for i := 0; i < numReads; i++ {
			invokeID, _, _ := srv.handleDataRequest(ctx)
			srv.sendReadResponse(ctx, invokeID, []*pdu.DataValue{{Tag: pdu.TagDataInteger, Int: int64(i)}})
		}
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()

	for i := 0; i < numReads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := client.Read(ctx, ReadRequest{DomainID: "D", ItemID: "V"})
			if err != nil {
				errs <- err
			}
		}()
		// Small delay to allow ordered processing through the transport.
		time.Sleep(10 * time.Millisecond)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent read error: %v", err)
	}

	client.Close(ctx)
}

// TestContextCancellationDuringRead verifies that cancelling the context
// during a Read properly unblocks the operation.
func TestContextCancellationDuringRead(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	bgCtx := context.Background()

	go srv.handleAssociation(bgCtx)

	client, err := NewClient(bgCtx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// Server goroutine: accept request but never respond.
	go func() {
		srv.handleDataRequest(bgCtx)
		// Deliberately don't send a response — the client should time out.
	}()

	ctx, cancel := context.WithTimeout(bgCtx, 200*time.Millisecond)
	defer cancel()

	_, err = client.Read(ctx, ReadRequest{DomainID: "D", ItemID: "V"})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if ctx.Err() == nil {
		t.Error("context should have been cancelled/timed out")
	}
}

// TestContextCancellationDuringWrite verifies that cancelling the context
// during a Write properly unblocks the operation.
func TestContextCancellationDuringWrite(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	bgCtx := context.Background()

	go srv.handleAssociation(bgCtx)

	client, err := NewClient(bgCtx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	go func() {
		srv.handleDataRequest(bgCtx)
		// No response — trigger timeout.
	}()

	ctx, cancel := context.WithTimeout(bgCtx, 200*time.Millisecond)
	defer cancel()

	_, err = client.Write(ctx, WriteRequest{
		DomainID: "D",
		ItemID:   "V",
		Value:    NewInteger(42),
	})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// TestCancelledContextUnblocksRead verifies that a cancelled context
// properly unblocks a pending Read operation when the server never responds.
func TestCancelledContextUnblocksRead(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	bgCtx := context.Background()

	go srv.handleAssociation(bgCtx)

	client, err := NewClient(bgCtx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	go func() {
		// Consume the request but never respond.
		srv.handleDataRequest(bgCtx)
	}()

	ctx, cancel := context.WithCancel(bgCtx)
	done := make(chan error, 1)
	go func() {
		_, err := client.Read(ctx, ReadRequest{DomainID: "D", ItemID: "V"})
		done <- err
	}()

	// Give the read time to send the request and block on receive.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Error("expected error after context cancel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read did not unblock after context cancel")
	}
}

// TestDoubleClose verifies that Close is truly idempotent:
// the second call returns nil (not an error).
func TestDoubleClose(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()

	err = client.Close(ctx)
	if err != nil {
		t.Fatalf("first Close: %v", err)
	}

	err = client.Close(ctx)
	if err != nil {
		t.Errorf("second Close should return nil, got %v", err)
	}
}

// TestOperationsAfterClose verifies that all operations return ErrClosed
// after the client is closed.
func TestOperationsAfterClose(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()

	client.Close(ctx)

	_, err = client.Read(ctx, ReadRequest{DomainID: "D", ItemID: "V"})
	if err == nil {
		t.Error("Read after Close should fail")
	}

	_, err = client.Write(ctx, WriteRequest{DomainID: "D", ItemID: "V", Value: NewInteger(1)})
	if err == nil {
		t.Error("Write after Close should fail")
	}

	_, err = client.Identify(ctx)
	if err == nil {
		t.Error("Identify after Close should fail")
	}

	_, err = client.Status(ctx)
	if err == nil {
		t.Error("Status after Close should fail")
	}
}
