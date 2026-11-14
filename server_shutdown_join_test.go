// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// Shutdown ends a connection that is not associated yet and returns only
// after its Serve returned, whatever the association timeout.
func TestShutdownWaitsForAssociatingServe(t *testing.T) {
	for _, timeout := range []time.Duration{0, -1, time.Hour} {
		t.Run(timeout.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				srv := NewServer(ServerOptions{AssociationTimeout: timeout})
				cl, sr := loopbackPair()
				defer func() { _ = cl.Close() }()
				served := make(chan error, 1)
				go func() { served <- srv.Serve(context.Background(), sr) }()
				synctest.Wait() // Serve waits for the CONNECT

				if err := srv.Shutdown(context.Background()); err != nil {
					t.Fatalf("timeout %v: Shutdown = %v", timeout, err)
				}
				select {
				case err := <-served:
					if !errors.Is(err, ErrServerClosed) {
						t.Errorf("timeout %v: Serve = %v, want ErrServerClosed", timeout, err)
					}
				default:
					t.Errorf("timeout %v: Shutdown returned while Serve was still associating", timeout)
					_ = cl.Close()
					_ = waitServe(t, served)
				}
			})
		})
	}
}

// A Serve started after Shutdown returns ErrServerClosed at once, and a
// second Shutdown returns nil (L1).
func TestShutdownTwice(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv := NewServer(ServerOptions{})
		c, served := serveLoopback(t, srv, DialOptions{})
		for i := range 2 {
			if err := srv.Shutdown(context.Background()); err != nil {
				t.Fatalf("Shutdown %d: %v", i+1, err)
			}
		}
		if err := <-served; err == nil {
			t.Error("Serve of an aborted association returned nil")
		}
		<-c.Done()
		cl, sr := loopbackPair()
		defer func() { _ = cl.Close(); _ = sr.Close() }()
		if err := srv.Serve(context.Background(), sr); !errors.Is(err, ErrServerClosed) {
			t.Errorf("Serve after Shutdown = %v, want ErrServerClosed", err)
		}
		ln := newChanListener()
		if err := srv.ListenAndServe(context.Background(), ln); !errors.Is(err, ErrServerClosed) {
			t.Errorf("ListenAndServe after Shutdown = %v, want ErrServerClosed", err)
		}
	})
}

// chanListener hands out the transports sent on conns.
type chanListener struct {
	conns     chan Transport
	closeOnce sync.Once
	closed    chan struct{}
}

func newChanListener() *chanListener {
	return &chanListener{conns: make(chan Transport), closed: make(chan struct{})}
}

func (l *chanListener) Accept(ctx context.Context) (Transport, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *chanListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return nil
}

func (l *chanListener) Addr() net.Addr { return &net.TCPAddr{} }

// holdCloseTransport keeps Close from returning until release is closed,
// so a test sees whether the goroutine that closes it has finished.
type holdCloseTransport struct {
	Transport
	release chan struct{}
}

func (h *holdCloseTransport) Close() error {
	err := h.Transport.Close()
	<-h.release
	return err
}

// ListenAndServe returns only after the goroutines of the connections it
// accepted have returned: when its context is cancelled, after Shutdown,
// and on a fatal accept error.
func TestListenAndServeJoinsConnections(t *testing.T) {
	for _, stop := range []string{"cancel", "shutdown", "accept error"} {
		t.Run(stop, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				srv := NewServer(ServerOptions{})
				ln := newChanListener()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				returned := make(chan error, 1)
				go func() { returned <- srv.ListenAndServe(ctx, ln) }()

				cl, sr := loopbackPair()
				defer func() { _ = cl.Close() }()
				release := make(chan struct{})
				ln.conns <- &holdCloseTransport{Transport: sr, release: release}
				synctest.Wait() // Serve waits for the CONNECT

				switch stop {
				case "cancel":
					cancel()
				case "shutdown":
					go func() { _ = srv.Shutdown(context.Background()) }()
				case "accept error":
					_ = ln.Close() // Accept fails with a permanent error
				}
				synctest.Wait()
				select {
				case err := <-returned:
					t.Errorf("%s: ListenAndServe returned (%v) while a connection goroutine was still running", stop, err)
					close(release)
					return
				default:
				}
				close(release)
				if err := <-returned; err == nil {
					t.Errorf("%s: ListenAndServe returned nil", stop)
				}
			})
		})
	}
}
