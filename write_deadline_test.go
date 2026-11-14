// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
)

// deadlineTransport wraps a transport and records the deadline of every
// Send. Once stalled, Send blocks until its context is done or the
// transport is closed, as a write with a deadline to a peer that stopped
// reading does when the socket buffers are full.
type deadlineTransport struct {
	Transport
	stall     atomic.Bool
	mu        sync.Mutex
	deadlines []time.Time // the zero time: no deadline
	closed    chan struct{}
	once      sync.Once
}

func newDeadlineTransport(inner Transport) *deadlineTransport {
	return &deadlineTransport{Transport: inner, closed: make(chan struct{})}
}

func (d *deadlineTransport) Send(ctx context.Context, b []byte) error {
	dl, _ := ctx.Deadline()
	d.mu.Lock()
	d.deadlines = append(d.deadlines, dl)
	d.mu.Unlock()
	if d.stall.Load() {
		select {
		case <-d.closed:
			return net.ErrClosed
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return d.Transport.Send(ctx, b)
}

func (d *deadlineTransport) Close() error {
	d.once.Do(func() { close(d.closed) })
	return d.Transport.Close()
}

func (d *deadlineTransport) lastDeadline() time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.deadlines[len(d.deadlines)-1]
}

// deadlineClient connects a client through a deadlineTransport to the
// test server. It returns the client, the transport, and a function that
// waits for Serve to return.
func deadlineClient(t *testing.T, opts DialOptions) (*Client, *deadlineTransport, func() error) {
	t.Helper()
	srv := testServer(t)
	clientConn, serverConn := loopbackPair()
	served := make(chan error, 1)
	go func() { served <- srv.Serve(context.Background(), serverConn) }()
	dt := newDeadlineTransport(clientConn)
	c, err := NewClient(context.Background(), dt, opts)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	wait := sync.OnceValue(func() error { return waitServe(t, served) })
	t.Cleanup(func() { _ = dt.Close(); _ = serverConn.Close(); _ = wait() })
	return c, dt, wait
}

// The write of a request carries the request's deadline: the caller's, or
// the one RequestTimeout sets. Without either the write has no deadline.
func TestRequestWriteCarriesDeadline(t *testing.T) {
	t.Run("caller deadline", func(t *testing.T) {
		c, dt, _ := deadlineClient(t, DialOptions{})
		ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
		defer cancel()
		want, _ := ctx.Deadline()
		if _, err := c.Identify(ctx); err != nil {
			t.Fatal(err)
		}
		if got := dt.lastDeadline(); !got.Equal(want) {
			t.Fatalf("write deadline %v, want the caller's %v", got, want)
		}
	})
	t.Run("RequestTimeout", func(t *testing.T) {
		c, dt, _ := deadlineClient(t, DialOptions{RequestTimeout: time.Hour})
		before := time.Now()
		if _, err := c.Identify(context.Background()); err != nil {
			t.Fatal(err)
		}
		got := dt.lastDeadline()
		if got.Before(before.Add(time.Hour)) || got.After(time.Now().Add(time.Hour)) {
			t.Fatalf("write deadline %v, want one hour after the request", got)
		}
	})
	t.Run("no deadline", func(t *testing.T) {
		c, dt, _ := deadlineClient(t, DialOptions{})
		if _, err := c.Identify(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := dt.lastDeadline(); !got.IsZero() {
			t.Fatalf("write deadline %v, want none", got)
		}
	})
}

// A write that its deadline cuts ends the association: Done closes, Err
// matches ErrClosed, the requests queued behind the write fail, and no
// goroutine is left behind.
func TestStalledWriteEndsAssociation(t *testing.T) {
	ignore := goleak.IgnoreCurrent()
	c, dt, waitServed := deadlineClient(t, DialOptions{})
	dt.stall.Store(true)

	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for range cap(errs) {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			_, err := c.Identify(ctx)
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err == nil {
			t.Error("Identify over a stalled write succeeded")
		}
	}

	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("association still open after a write was cut by its deadline")
	}
	if !errors.Is(c.Err(), ErrClosed) {
		t.Fatalf("Err() = %v, want ErrClosed", c.Err())
	}
	if _, err := c.Identify(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Identify after the association ended = %v, want ErrClosed", err)
	}
	_ = waitServed() // the server sees the transport close
	goleak.VerifyNone(t, ignore)
}

// expiredContext has a deadline that has passed, but its timer has not
// fired yet: Err is nil and Done never closes.
type expiredContext struct{ context.Context }

func (expiredContext) Deadline() (time.Time, bool) { return time.Unix(1, 0), true }

// A request whose deadline passed before its write started is not
// written, and the association stays usable.
func TestExpiredRequestIsNotWritten(t *testing.T) {
	c, dt, _ := deadlineClient(t, DialOptions{})
	writes := func() int {
		dt.mu.Lock()
		defer dt.mu.Unlock()
		return len(dt.deadlines)
	}
	before := writes()
	if _, err := c.Identify(expiredContext{context.Background()}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Identify = %v, want DeadlineExceeded", err)
	}
	if n := writes() - before; n != 0 {
		t.Errorf("%d writes for a request whose deadline had passed, want none", n)
	}
	if err := c.Err(); err != nil {
		t.Fatalf("association ended: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := c.Identify(ctx); err != nil {
		t.Fatalf("Identify after the skipped request: %v", err)
	}
}
