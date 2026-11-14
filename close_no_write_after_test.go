// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// gateTransport records every PDU sent. While held, the next Send waits
// for the gate, as a write to a slow peer does.
type gateTransport struct {
	Transport
	hold    atomic.Bool
	entered chan struct{} // receives once a held Send waits
	gate    chan struct{}

	mu   sync.Mutex
	sent [][]byte
}

func (g *gateTransport) Send(ctx context.Context, b []byte) error {
	if g.hold.CompareAndSwap(true, false) {
		g.entered <- struct{}{}
		<-g.gate
	}
	g.mu.Lock()
	g.sent = append(g.sent, append([]byte(nil), b...))
	g.mu.Unlock()
	return g.Transport.Send(ctx, b)
}

// confirmedRequests counts the confirmed requests sent from index from on.
func (g *gateTransport) confirmedRequests(t *testing.T, from int) int {
	t.Helper()
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, b := range g.sent[from:] {
		if c, err := DecodeTSDU(b); err == nil && c.PDU != nil && c.PDU.Kind == "confirmed-request" {
			n++
		}
	}
	return n
}

// until spins until cond holds, failing the test after five seconds.
func until(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		runtime.Gosched()
	}
}

// A request queued behind another write when Close starts is not
// written: no request follows the Conclude-Request (ISO 9506-2 clause
// 8.4).
func TestCloseWritesNoQueuedRequest(t *testing.T) {
	srv := testServer(t)
	clientConn, serverConn := loopbackPair()
	served := make(chan error, 1)
	go func() { served <- srv.Serve(context.Background(), serverConn) }()
	gt := &gateTransport{Transport: clientConn, entered: make(chan struct{}, 1), gate: make(chan struct{})}
	c, err := NewClient(context.Background(), gt, DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	gt.mu.Lock()
	from := len(gt.sent)
	gt.mu.Unlock()

	gt.hold.Store(true)
	errs := make(chan error, 2)
	go func() { _, err := c.Identify(context.Background()); errs <- err }()
	<-gt.entered // the first request holds the send lock
	go func() { _, err := c.Identify(context.Background()); errs <- err }()
	until(t, func() bool { return c.tracker.PendingCount() == 2 }) // the second is queued

	closed := make(chan error, 1)
	go func() { closed <- c.Close(context.Background()) }()
	until(t, func() bool { return c.tracker.PendingCount() == 0 }) // Close has started
	close(gt.gate)

	if err := <-closed; err != nil {
		t.Errorf("Close: %v", err)
	}
	for range 2 {
		if err := <-errs; !errors.Is(err, ErrClosed) {
			t.Errorf("request = %v, want ErrClosed", err)
		}
	}
	if n := gt.confirmedRequests(t, from); n != 1 {
		t.Errorf("%d confirmed requests written, want 1: the queued one was written after Close started", n)
	}
	if err := waitServe(t, served); err != nil {
		t.Errorf("Serve = %v", err)
	}
}
