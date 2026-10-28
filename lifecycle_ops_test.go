// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

// eventLog records Metrics events.
type eventLog struct {
	NopMetrics
	mu       sync.Mutex
	requests []RequestEvent
	conns    []ConnectionEvent
}

func (l *eventLog) ObserveRequest(ev RequestEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.requests = append(l.requests, ev)
}

func (l *eventLog) ObserveConnection(ev ConnectionEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.conns = append(l.conns, ev)
}

func (l *eventLog) count(state ConnectionState) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, ev := range l.conns {
		if ev.State == state {
			n++
		}
	}
	return n
}

// serveLoopback serves srv on an in-memory transport pair and returns the
// client end plus a channel with Serve's result.
func serveLoopback(t *testing.T, srv *Server, opts DialOptions) (*Client, <-chan error) {
	t.Helper()
	clientConn, serverConn := loopbackPair()
	served := make(chan error, 1)
	go func() { served <- srv.Serve(context.Background(), serverConn) }()
	c, err := NewClient(context.Background(), clientConn, opts)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c, served
}

func TestServer_Metrics_Shutdown_Loopback(t *testing.T) {
	srvLog := &eventLog{}
	srv := NewServer(ServerOptions{Metrics: srvLog})
	srv.HandleIdentify(func(context.Context, IdentifyRequest) (*ServerIdentity, error) {
		return nil, errors.Join(errors.New("ctx"), &ServiceError{Class: ErrorClassAccess, Code: 3})
	})
	cliLog := &eventLog{}
	c, served := serveLoopback(t, srv, DialOptions{Metrics: cliLog})
	ctx := context.Background()

	_, err := c.Identify(ctx)
	var se *ServiceError
	if !errors.As(err, &se) || se.Class != ErrorClassAccess || se.Code != 3 {
		t.Fatalf("Identify = %v, want ServiceError access/3", err)
	}
	if err := c.Err(); err != nil {
		t.Fatalf("Err = %v", err)
	}

	waitForConnections(t, srv, 1, 2*time.Second)
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := <-served; !errors.Is(err, ErrClosed) {
		t.Errorf("Serve after Shutdown = %v, want ErrClosed", err)
	}
	select {
	case <-c.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("client not done after Shutdown")
	}
	if !errors.Is(c.Err(), ErrClosed) {
		t.Errorf("Err = %v", c.Err())
	}
	if _, err := c.Identify(ctx); !errors.Is(err, ErrClosed) {
		t.Errorf("Identify after Shutdown = %v, want ErrClosed", err)
	}
	if srvLog.count(ConnectionEstablished) != 1 || srvLog.count(ConnectionClosed) != 1 {
		t.Errorf("server connection events = %+v", srvLog.conns)
	}
	if cliLog.count(ConnectionClosed) != 1 {
		t.Errorf("client connection events = %+v", cliLog.conns)
	}
	if len(srvLog.requests) != 1 || srvLog.requests[0].Service != ServiceIdentify || srvLog.requests[0].Role != RoleServer {
		t.Errorf("server request events = %+v", srvLog.requests)
	}

	// After Shutdown nothing is served.
	_, sconn := loopbackPair()
	if err := srv.Serve(ctx, sconn); !errors.Is(err, ErrServerClosed) {
		t.Errorf("Serve = %v, want ErrServerClosed", err)
	}
	ln := &fakeListener{}
	if err := srv.ListenAndServe(ctx, ln); !errors.Is(err, ErrServerClosed) || !ln.closed {
		t.Errorf("ListenAndServe = %v (closed=%v), want ErrServerClosed", err, ln.closed)
	}
	_ = c.Abort(ctx)
}

// fakeListener blocks in Accept until closed.
type fakeListener struct {
	mu     sync.Mutex
	closed bool
	done   chan struct{}
	once   sync.Once
}

func (l *fakeListener) init() { l.once.Do(func() { l.done = make(chan struct{}) }) }

func (l *fakeListener) Accept(ctx context.Context) (Transport, error) {
	l.init()
	select {
	case <-l.done:
		return nil, net.ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (l *fakeListener) Close() error {
	l.init()
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.closed {
		l.closed = true
		close(l.done)
	}
	return nil
}

func (l *fakeListener) Addr() net.Addr { return &net.TCPAddr{} }

func TestServer_Shutdown_StopsListenAndServe(t *testing.T) {
	srv := NewServer(ServerOptions{})
	ln := &fakeListener{}
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe(context.Background(), ln) }()
	// Wait until the listener is registered.
	for i := 0; ; i++ {
		srv.connsMu.RLock()
		n := len(srv.listeners)
		srv.connsMu.RUnlock()
		if n == 1 {
			break
		}
		if i > 1000 {
			t.Fatal("listener not registered")
		}
		time.Sleep(time.Millisecond)
	}
	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrServerClosed) {
		t.Errorf("ListenAndServe = %v, want ErrServerClosed", err)
	}
}

func TestServer_AssociationTimeout_Loopback(t *testing.T) {
	srv := NewServer(ServerOptions{AssociationTimeout: 20 * time.Millisecond})
	_, sconn := loopbackPair()
	err := srv.Serve(context.Background(), sconn)
	if !errors.Is(err, ErrAssociationFailed) || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Serve = %v", err)
	}

	// Cancelling Serve's context ends a silent association too.
	srv = NewServer(ServerOptions{AssociationTimeout: -1})
	_, sconn = loopbackPair()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := srv.Serve(ctx, sconn); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Serve with cancelled ctx = %v", err)
	}
}

func TestClient_RequestTimeout_Loopback(t *testing.T) {
	srv := NewServer(ServerOptions{})
	release := make(chan struct{})
	srv.HandleIdentify(func(context.Context, IdentifyRequest) (*ServerIdentity, error) {
		<-release
		return &ServerIdentity{}, nil
	})
	c, _ := serveLoopback(t, srv, DialOptions{RequestTimeout: 20 * time.Millisecond})
	defer func() { _ = c.Abort(context.Background()) }()
	_, err := c.Identify(context.Background())
	close(release)
	if !errors.Is(err, ErrInvokeTimeout) {
		t.Fatalf("Identify = %v, want ErrInvokeTimeout", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Identify(cancelled); !errors.Is(err, context.Canceled) {
		t.Errorf("Identify with cancelled ctx = %v", err)
	}
}

// failingSendTransport fails every Send after the association.
type failingSendTransport struct {
	Transport
	mu   sync.Mutex
	fail bool
}

func (f *failingSendTransport) Send(ctx context.Context, data []byte) error {
	f.mu.Lock()
	fail := f.fail
	f.mu.Unlock()
	if fail {
		return errors.New("broken pipe")
	}
	return f.Transport.Send(ctx, data)
}

func TestClient_SendFailureClosesAssociation(t *testing.T) {
	srv := NewServer(ServerOptions{})
	clientConn, serverConn := loopbackPair()
	go func() { _ = srv.Serve(context.Background(), serverConn) }()
	ft := &failingSendTransport{Transport: clientConn}
	c, err := NewClient(context.Background(), ft, DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ft.mu.Lock()
	ft.fail = true
	ft.mu.Unlock()
	_, err = c.Identify(context.Background())
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("Identify = %v, want ErrClosed", err)
	}
	select {
	case <-c.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("association not closed after a failed send")
	}
	_ = c.Abort(context.Background())
}

func TestClient_SendAbandonedBeforeWrite(t *testing.T) {
	clientConn, _ := loopbackPair()
	c := &Client{conn: clientConn}
	c.sendMu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := c.send(ctx, []byte{1}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("send = %v", err)
	}
	c.sendMu.Unlock()
	// The abandoned write must not reach the transport.
	time.Sleep(10 * time.Millisecond)
	select {
	case d := <-clientConn.(*chanTransport).send:
		t.Errorf("abandoned PDU was sent: %x", d)
	default:
	}
}

func TestToWireServiceError(t *testing.T) {
	if toWireServiceError(nil) != nil {
		t.Error("nil")
	}
	plain := errors.New("x")
	if toWireServiceError(plain) != plain {
		t.Error("plain error changed")
	}
	if toWireServiceError(errResourceOther) != errResourceOther {
		t.Error("wire error changed")
	}
}
