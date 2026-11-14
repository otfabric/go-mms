// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
)

// stallTransport wraps a transport. Once stalled, Send blocks until the
// transport is closed, as a write to a peer that stopped reading does when
// the socket buffers are full.
type stallTransport struct {
	Transport
	stall  atomic.Bool
	closed chan struct{}
	once   sync.Once
}

func newStallTransport(inner Transport) *stallTransport {
	return &stallTransport{Transport: inner, closed: make(chan struct{})}
}

func (s *stallTransport) Send(ctx context.Context, b []byte) error {
	if s.stall.Load() {
		<-s.closed
		return net.ErrClosed
	}
	return s.Transport.Send(ctx, b)
}

func (s *stallTransport) Close() error {
	s.once.Do(func() { close(s.closed) })
	return s.Transport.Close()
}

// connectStallClient returns a client on a stallTransport and the server
// end of its loopback pair, through which a test can inject frames.
func connectStallClient(t *testing.T) (*Client, *stallTransport, Transport) {
	t.Helper()
	srv := testServer(t)
	clientConn, serverConn := loopbackPair()
	go func() { _ = srv.Serve(context.Background(), serverConn) }()
	st := newStallTransport(clientConn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := NewClient(ctx, st, DialOptions{
		Logger: slog.New(slog.NewTextHandler(newTestWriter(t), nil)),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return c, st, serverConn
}

// TestCloseBoundedByContextWhileWriteStalled: a request whose write is
// stuck (the peer stopped reading) holds the send lock. Close must still
// return when its context is done, closing the transport, as Abort does.
func TestCloseBoundedByContextWhileWriteStalled(t *testing.T) {
	ignore := goleak.IgnoreCurrent()
	c, st, peer := connectStallClient(t)
	st.stall.Store(true)

	rctx, rcancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	_, err := c.Identify(rctx)
	rcancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Identify over stalled write: %v, want DeadlineExceeded", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Close(ctx) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Close: %v, want DeadlineExceeded", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return after its context ended (send lock held by a stalled write)")
	}
	select {
	case <-st.closed:
	default:
		t.Fatal("transport not closed after Close")
	}
	// The stalled write and the server end once the transport is closed
	// (L4).
	_ = peer.Close()
	goleak.VerifyNone(t, ignore)
}

// TestReaderExitClosesTransport: when the reader stops on an undecodable
// PDU the association is unusable, so the transport is closed at once. A
// write stalled on it fails, and Close returns without waiting for its
// context.
func TestReaderExitClosesTransport(t *testing.T) {
	c, st, peer := connectStallClient(t)
	st.stall.Store(true)

	rctx, rcancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	_, _ = c.Identify(rctx) // leaves a write stalled, holding the send lock
	rcancel()

	// The peer sends a frame that is not a session SPDU.
	if err := peer.Send(context.Background(), []byte{0xff, 0x00}); err != nil {
		t.Fatalf("inject: %v", err)
	}
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("reader did not stop on an undecodable PDU")
	}
	select {
	case <-st.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("transport not closed after the reader stopped")
	}
	if !errors.Is(c.Err(), ErrDecodeFailed) {
		t.Fatalf("Err() = %v, want ErrDecodeFailed", c.Err())
	}

	done := make(chan error, 1)
	go func() { done <- c.Close(context.Background()) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Close after reader failure reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close blocked on a dead association")
	}
}
