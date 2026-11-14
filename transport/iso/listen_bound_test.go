// SPDX-License-Identifier: MIT

package iso

import (
	"context"
	"net"
	"sync"
	"testing"
	"testing/synctest"
)

// pipeListener hands out the connections queued on conns.
type pipeListener struct {
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
}

func newPipeListener(n int) *pipeListener {
	return &pipeListener{conns: make(chan net.Conn, n), closed: make(chan struct{})}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *pipeListener) Addr() net.Addr { return &net.TCPAddr{} }

// At most maxPendingHandshakes connections handshake at a time; the next
// one waits until a handshake ends (L12). Close returns after every
// handshake goroutine stopped (L15).
func TestListenerPendingHandshakeBound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const n = maxPendingHandshakes + 2
		pl := newPipeListener(n)
		l := NewListener(pl, WithHandshakeTimeout(-1))
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _, _ = l.Accept(ctx) }() // starts the accept loop

		var peers []net.Conn
		for range n {
			c, s := net.Pipe()
			peers = append(peers, c)
			pl.conns <- s
		}
		defer func() {
			for _, c := range peers {
				_ = c.Close()
			}
		}()
		synctest.Wait() // every peer is silent
		// The loop holds the next connection, waiting for a slot.
		if got, queued := len(l.pending), len(pl.conns); got != maxPendingHandshakes || queued != n-maxPendingHandshakes-1 {
			t.Fatalf("%d handshakes, %d connections queued; want %d and %d", got, queued, maxPendingHandshakes, n-maxPendingHandshakes-1)
		}

		_ = peers[0].Close() // one handshake fails and frees its slot
		synctest.Wait()
		if got, queued := len(l.pending), len(pl.conns); got != maxPendingHandshakes || queued != 0 {
			t.Fatalf("after one handshake ended: %d handshakes, %d queued; want %d and 0", got, queued, maxPendingHandshakes)
		}

		if err := l.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if got := len(l.pending); got != 0 {
			t.Fatalf("Close returned with %d handshakes still running", got)
		}
	})
}
