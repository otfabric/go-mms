// SPDX-License-Identifier: MIT

package serverconn

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"
)

// stallingPeer delivers the frames queued on recv, then blocks in Receive
// until closed. Send never completes: it blocks until its context is done
// or the transport is closed, as a write to a peer that stopped reading
// does. It reports the deadline of every Send on sends.
type stallingPeer struct {
	recv   chan []byte
	sends  chan time.Time // the deadline of each Send; the zero time: none
	closed chan struct{}
	once   sync.Once
}

func newStallingPeer(frames ...[]byte) *stallingPeer {
	p := &stallingPeer{
		recv:   make(chan []byte, len(frames)),
		sends:  make(chan time.Time, 128),
		closed: make(chan struct{}),
	}
	for _, f := range frames {
		p.recv <- f
	}
	return p
}

func (p *stallingPeer) Send(ctx context.Context, _ []byte) error {
	dl, _ := ctx.Deadline()
	p.sends <- dl
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.closed:
		return errors.New("transport closed")
	}
}

func (p *stallingPeer) Receive(ctx context.Context) ([]byte, error) {
	select {
	case f := <-p.recv:
		return f, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.closed:
		return nil, errors.New("transport closed")
	}
}

func (p *stallingPeer) Close() error {
	p.once.Do(func() { close(p.closed) })
	return nil
}

func (p *stallingPeer) isClosed() bool {
	select {
	case <-p.closed:
		return true
	default:
		return false
	}
}

func serveStalled(t *testing.T, writeTimeout time.Duration) (*Conn, *stallingPeer, <-chan error) {
	t.Helper()
	p := newStallingPeer(dataFrame(mustConfirmedReq(t, 1, 2, nil)))
	t.Cleanup(func() { _ = p.Close() })
	opts := defaultOpts()
	opts.WriteTimeout = writeTimeout
	c := New(p, discardLogger(), nopHandler, opts)
	served := make(chan error, 1)
	go func() { served <- c.Serve(context.Background()) }()
	select {
	case <-p.sends: // the response write is stuck
	case <-time.After(5 * time.Second):
		t.Fatal("no response write")
	}
	return c, p, served
}

// A response write to a peer that stopped reading is cut by the write
// timeout: the transport is closed and Serve returns.
func TestServe_StalledResponseWriteEndsAssociation(t *testing.T) {
	_, p, served := serveStalled(t, 20*time.Millisecond)
	select {
	case err := <-served:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Serve = %v, want DeadlineExceeded", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after the write timeout")
	}
	if !p.isClosed() {
		t.Fatal("transport still open after the write timeout")
	}
}

// Unsolicited sends queued behind a stuck response write return at their
// deadline without leaving a goroutine behind and without writing; the
// skipped writes do not end the association.
func TestSendUnconfirmed_BehindStalledWriterLeavesNoGoroutines(t *testing.T) {
	c, p, served := serveStalled(t, time.Hour)
	ignore := goleak.IgnoreCurrent()

	const n = 50
	for range n {
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		err := c.SendUnconfirmed(ctx, []byte{0xa3, 0x00})
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("SendUnconfirmed = %v, want DeadlineExceeded", err)
		}
	}
	goleak.VerifyNone(t, ignore)
	select {
	case <-p.sends:
		t.Fatal("a skipped unsolicited send was written")
	default:
	}
	if p.isClosed() {
		t.Fatal("a skipped unsolicited send closed the transport")
	}

	_ = c.Close()
	if err := <-served; err == nil {
		t.Fatal("Serve ended without an error")
	}
}

// A write without a deadline on its context is bounded by the write
// timeout; the default applies when WriteTimeout is zero and a negative
// value disables it.
func TestWriteTimeoutDefaultAndDisabled(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout time.Duration
		want    time.Duration // 0: no deadline
	}{
		{"default", 0, DefaultWriteTimeout},
		{"set", time.Minute, time.Minute},
		{"disabled", -1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newStallingPeer()
			opts := defaultOpts()
			opts.WriteTimeout = tc.timeout
			c := New(p, discardLogger(), nopHandler, opts)
			start := time.Now()
			errc := make(chan error, 1)
			go func() { errc <- c.SendAbort(context.Background()) }()
			dl := <-p.sends
			_ = p.Close()
			if err := <-errc; err == nil {
				t.Fatal("SendAbort on a closed transport succeeded")
			}
			if tc.want == 0 {
				if !dl.IsZero() {
					t.Fatalf("write deadline %v, want none", dl)
				}
				return
			}
			if got := dl.Sub(start); got < tc.want-time.Second || got > tc.want+time.Second {
				t.Fatalf("write deadline in %v, want about %v", got, tc.want)
			}
		})
	}
}

// An unsolicited send without a deadline is bounded by the write timeout,
// which ends the association when it cuts the write.
func TestSendUnconfirmed_NoDeadlineBoundedByWriteTimeout(t *testing.T) {
	p := newStallingPeer()
	t.Cleanup(func() { _ = p.Close() })
	opts := defaultOpts()
	opts.WriteTimeout = 20 * time.Millisecond
	c := New(p, discardLogger(), nopHandler, opts)
	if err := c.SendUnconfirmed(context.Background(), []byte{0xa3, 0x00}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("SendUnconfirmed = %v, want DeadlineExceeded", err)
	}
	if !p.isClosed() {
		t.Fatal("transport still open after the write timeout cut the write")
	}
}

// A server write that cannot get the send lock within the write timeout
// (the association's writer is stalled) closes the transport.
func TestSendData_LockWaitBoundedByWriteTimeout(t *testing.T) {
	p := newStallingPeer()
	t.Cleanup(func() { _ = p.Close() })
	opts := defaultOpts()
	opts.WriteTimeout = 10 * time.Millisecond
	c := New(p, discardLogger(), nopHandler, opts)
	c.sendSem <- struct{}{} // a write in progress
	if err := c.sendData(context.Background(), []byte{0xa3, 0x00}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("sendData = %v, want DeadlineExceeded", err)
	}
	if !p.isClosed() {
		t.Fatal("transport still open after the lock wait timed out")
	}
	select {
	case <-p.sends:
		t.Fatal("written without the send lock")
	default:
	}
}

// A cancelled unsolicited send waiting for the lock returns at once and
// leaves the association usable.
func TestSendUnconfirmed_CancelledWhileWaiting(t *testing.T) {
	p := newStallingPeer()
	t.Cleanup(func() { _ = p.Close() })
	c := New(p, discardLogger(), nopHandler, defaultOpts())
	c.sendSem <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(5*time.Millisecond, cancel)
	if err := c.SendUnconfirmed(ctx, []byte{0xa3, 0x00}); !errors.Is(err, context.Canceled) {
		t.Fatalf("SendUnconfirmed = %v, want Canceled", err)
	}
	if p.isClosed() {
		t.Fatal("a skipped send closed the transport")
	}
	<-c.sendSem
}
