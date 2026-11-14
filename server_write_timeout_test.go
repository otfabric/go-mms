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

// stallingServerTransport wraps the server end of a loopback pair. Once
// stalled, Send blocks until its context is done or the transport is
// closed, as a write to a peer that stopped reading does; the first
// stalled Send closes entered.
type stallingServerTransport struct {
	Transport
	stall     atomic.Bool
	entered   chan struct{}
	enterOnce sync.Once
	closed    chan struct{}
	closeOnce sync.Once
}

func (s *stallingServerTransport) Send(ctx context.Context, b []byte) error {
	if !s.stall.Load() {
		return s.Transport.Send(ctx, b)
	}
	s.enterOnce.Do(func() { close(s.entered) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.closed:
		return net.ErrClosed
	}
}

func (s *stallingServerTransport) Close() error {
	s.closeOnce.Do(func() { close(s.closed) })
	return s.Transport.Close()
}

// A client that stops reading while its confirmed response is written
// holds the association at most for ServerOptions.WriteTimeout: Serve
// returns, and unsolicited sends attempted meanwhile return at their
// deadline without leaving goroutines behind.
func TestServer_WriteTimeoutEndsStalledAssociation(t *testing.T) {
	const writeTimeout = 300 * time.Millisecond
	srv := NewServer(ServerOptions{
		Logger:       slog.New(slog.NewTextHandler(newTestWriter(t), nil)),
		WriteTimeout: writeTimeout,
	})
	srv.HandleIdentify(func(context.Context, IdentifyRequest) (*ServerIdentity, error) {
		return &ServerIdentity{Vendor: "v", Model: "m", Revision: "r"}, nil
	})
	clientConn, serverConn := loopbackPair()
	st := &stallingServerTransport{Transport: serverConn, entered: make(chan struct{}), closed: make(chan struct{})}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(context.Background(), st) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := NewClient(ctx, clientConn, DialOptions{Logger: slog.New(slog.NewTextHandler(newTestWriter(t), nil))})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = c.Abort(context.Background()) })

	st.stall.Store(true)
	start := time.Now()
	rctx, rcancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	_, _ = c.Identify(rctx)
	rcancel()
	<-st.entered

	ignore := goleak.IgnoreCurrent()
	for _, sc := range srv.Connections() {
		for range 20 {
			sctx, scancel := context.WithTimeout(context.Background(), time.Millisecond)
			err := sc.SendUnsolicitedStatus(sctx, ServerStatus{})
			scancel()
			if err == nil {
				t.Fatal("unsolicited send over a stalled writer succeeded")
			}
		}
	}
	goleak.VerifyNone(t, ignore)

	select {
	case err := <-served:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Serve = %v, want DeadlineExceeded", err)
		}
		if elapsed := time.Since(start); elapsed < writeTimeout/2 {
			t.Fatalf("Serve returned after %v, before the write timeout", elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return after the write timeout")
	}
	if n := len(srv.Connections()); n != 0 {
		t.Fatalf("%d connections registered after the association ended", n)
	}
}
