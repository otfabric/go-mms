// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSetVariableRead(t *testing.T) {
	srv := testServer(t)
	called := false
	err := srv.SetVariableRead("testDomain", "counter", func(context.Context) (*Value, error) {
		called = true
		return NewInteger(99), nil
	})
	if err != nil {
		t.Fatalf("SetVariableRead: %v", err)
	}
	entry, ok := srv.registry.LookupVariable(int(ObjectScopeDomain), "testDomain", "counter")
	if !ok {
		t.Fatal("variable not found after SetVariableRead")
	}
	rf, ok := entry.ReadFunc.(func(context.Context) (*Value, error))
	if !ok || rf == nil {
		t.Fatalf("ReadFunc type %T", entry.ReadFunc)
	}
	v, err := rf(context.Background())
	if err != nil || !called {
		t.Fatalf("handler: called=%v err=%v", called, err)
	}
	if i, ok := v.Int64(); !ok || i != 99 {
		t.Fatalf("value=%v ok=%v", v, ok)
	}

	if err := srv.SetVariableRead("testDomain", "missing", func(context.Context) (*Value, error) {
		return nil, nil
	}); err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestServerConnFromContext(t *testing.T) {
	if ServerConnFromContext(context.Background()) != nil {
		t.Fatal("expected nil without key")
	}
	sc := &ServerConn{}
	ctx := context.WithValue(context.Background(), serverConnCtxKey{}, sc)
	if got := ServerConnFromContext(ctx); got != sc {
		t.Fatalf("got %p want %p", got, sc)
	}
}

type mockListener struct {
	mu     sync.Mutex
	accept chan acceptJob
	closed bool
	addr   net.Addr
}

type acceptJob struct {
	conn Transport
	err  error
}

func newMockListener() *mockListener {
	return &mockListener{
		accept: make(chan acceptJob, 8),
		addr:   &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 102},
	}
}

func (l *mockListener) Accept(ctx context.Context) (Transport, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case job, ok := <-l.accept:
		if !ok {
			return nil, errors.New("listener closed")
		}
		return job.conn, job.err
	}
}

func (l *mockListener) Addr() net.Addr { return l.addr }

func (l *mockListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.closed {
		l.closed = true
		close(l.accept)
	}
	return nil
}

type tempNetError struct{ msg string }

func (e *tempNetError) Error() string   { return e.msg }
func (e *tempNetError) Temporary() bool { return true }
func (e *tempNetError) Timeout() bool   { return false }

func TestListenAndServe_ContextCancel(t *testing.T) {
	srv := testServer(t)
	ln := newMockListener()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe(ctx, ln) }()

	// Unblock Accept with context cancel.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ListenAndServe: %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ListenAndServe")
	}
}

func TestListenAndServe_TemporaryThenFatal(t *testing.T) {
	srv := testServer(t)
	ln := newMockListener()
	ctx := context.Background()

	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe(ctx, ln) }()

	ln.accept <- acceptJob{err: &tempNetError{msg: "temp"}}
	ln.accept <- acceptJob{err: errors.New("fatal accept")}

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "accept") {
			t.Fatalf("want accept error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestListenAndServe_AcceptsConnection(t *testing.T) {
	srv := testServer(t)
	ln := newMockListener()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe(ctx, ln) }()

	client, server := loopbackPair()
	ln.accept <- acceptJob{conn: server}

	// Close client so Serve exits; then cancel accept loop.
	_ = client.Close()
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}
