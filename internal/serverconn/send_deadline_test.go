// SPDX-License-Identifier: MIT

package serverconn

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// stalledTransport never completes a write: Send blocks until its context
// is done or the transport is closed, as a write to a peer that stopped
// reading does. It reports the deadline of every Send on sends.
type stalledTransport struct {
	sends  chan time.Time // the deadline of each Send; the zero time: none
	closed chan struct{}
	once   sync.Once
}

func (s *stalledTransport) Send(ctx context.Context, _ []byte) error {
	dl, _ := ctx.Deadline()
	s.sends <- dl
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.closed:
		return errors.New("transport closed")
	}
}

func (s *stalledTransport) Receive(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.closed:
		return nil, errors.New("transport closed")
	}
}

func (s *stalledTransport) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

// An unsolicited write carries the caller's deadline; when the deadline
// cuts it the transport is closed, which ends Serve.
func TestSendUnconfirmedWriteCarriesDeadline(t *testing.T) {
	tr := &stalledTransport{sends: make(chan time.Time, 1), closed: make(chan struct{})}
	t.Cleanup(func() { _ = tr.Close() })
	c := New(tr, nil, nil, MMSOptions{})
	served := make(chan error, 1)
	go func() { served <- c.Serve(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	want, _ := ctx.Deadline()
	if err := c.SendUnconfirmed(ctx, []byte{0xa3, 0x00}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("SendUnconfirmed = %v, want DeadlineExceeded", err)
	}
	if got := <-tr.sends; !got.Equal(want) {
		t.Fatalf("write deadline %v, want the caller's %v", got, want)
	}
	select {
	case <-tr.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("transport still open after the deadline cut the write")
	}
	select {
	case err := <-served:
		if err == nil {
			t.Fatal("Serve ended without an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after the transport closed")
	}
}

// expiredContext has a deadline that has passed, but its timer has not
// fired yet: Err is nil and Done never closes.
type expiredContext struct{ context.Context }

func (expiredContext) Deadline() (time.Time, bool) { return time.Unix(1, 0), true }

// An unsolicited write whose deadline passed before it started is
// skipped, and the transport stays open.
func TestSendUnconfirmedExpiredIsNotWritten(t *testing.T) {
	tr := &stalledTransport{sends: make(chan time.Time, 1), closed: make(chan struct{})}
	t.Cleanup(func() { _ = tr.Close() })
	c := New(tr, nil, nil, MMSOptions{})
	if err := c.SendUnconfirmed(expiredContext{context.Background()}, []byte{0xa3, 0x00}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("SendUnconfirmed = %v, want DeadlineExceeded", err)
	}
	select {
	case <-tr.sends:
		t.Error("written although its deadline had passed")
	default:
	}
	select {
	case <-tr.closed:
		t.Fatal("transport closed by a write that did not start")
	default:
	}
}
