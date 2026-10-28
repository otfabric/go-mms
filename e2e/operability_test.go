// SPDX-License-Identifier: MIT

package e2e

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

// recorder is a Metrics implementation that keeps every event.
type recorder struct {
	mms.NopMetrics
	mu       sync.Mutex
	requests []mms.RequestEvent
	conns    []mms.ConnectionEvent
}

func (r *recorder) ObserveRequest(ev mms.RequestEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, ev)
}

func (r *recorder) ObserveConnection(ev mms.ConnectionEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.conns = append(r.conns, ev)
}

func (r *recorder) snapshot() ([]mms.RequestEvent, []mms.ConnectionEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]mms.RequestEvent(nil), r.requests...), append([]mms.ConnectionEvent(nil), r.conns...)
}

func (r *recorder) hasConn(role mms.Role, state mms.ConnectionState) bool {
	_, conns := r.snapshot()
	for _, ev := range conns {
		if ev.Role == role && ev.State == state {
			return true
		}
	}
	return false
}

// Done and Err report the end of an association; Retry and
// DefaultRetryPolicy re-establish it.
func TestClient_Done_Err_Reconnect(t *testing.T) {
	m := startModel(t)
	ctx := timeout(t)
	c := dial(t, m.addr)
	if err := c.Err(); err != nil {
		t.Fatalf("Err on a live association = %v", err)
	}
	select {
	case <-c.Done():
		t.Fatal("Done closed on a live association")
	default:
	}

	eventually(t, "server connection", func() bool { return len(m.srv.Connections()) == 1 })
	if err := m.srv.Connections()[0].Abort(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.Done():
	case <-ctx.Done():
		t.Fatal("Done not closed after the server aborted")
	}
	lost := c.Err()
	if !errors.Is(lost, mms.ErrClosed) {
		t.Fatalf("Err = %v, want ErrClosed", lost)
	}
	if !mms.IsRetryable(lost) {
		t.Errorf("IsRetryable(%v) = false, want true", lost)
	}

	policy := mms.DefaultRetryPolicy()
	policy.InitialBackoff = time.Millisecond
	var next *mms.Client
	err := mms.Retry(ctx, policy, func(ctx context.Context) error {
		var err error
		next, err = iso.Dial(ctx, m.addr)
		return err
	})
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}
	defer func() { _ = next.Close(ctx) }()
	if _, err := next.Identify(ctx); err != nil {
		t.Fatalf("Identify after reconnect: %v", err)
	}

	// Close also ends the association.
	if err := next.Close(ctx); err != nil {
		t.Fatal(err)
	}
	<-next.Done()
	if !errors.Is(next.Err(), mms.ErrClosed) {
		t.Errorf("Err after Close = %v, want ErrClosed", next.Err())
	}
}

// iso.WithRetry dials a server that starts listening only after the first
// attempts failed.
func TestDial_WithRetry(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	_ = probe.Close()

	srv := mms.NewServer(mms.ServerOptions{})
	var retries int
	retried := make(chan struct{})
	policy := mms.RetryPolicy{
		MaxAttempts:    -1,
		InitialBackoff: 20 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
		Jitter:         0.5,
		OnRetry: func(int, error, time.Duration) {
			retries++
			if retries == 2 {
				close(retried)
			}
		},
	}
	go func() {
		<-retried
		tcp, err := net.Listen("tcp", addr)
		if err != nil {
			return
		}
		_ = srv.ListenAndServe(context.Background(), iso.NewListener(tcp))
	}()
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })

	c, err := iso.Dial(timeout(t), addr, iso.WithRetry(policy))
	if err != nil {
		t.Fatalf("Dial with retry: %v", err)
	}
	defer func() { _ = c.Close(context.Background()) }()
	if retries < 2 {
		t.Errorf("retries = %d, want >= 2", retries)
	}

	// A rejected association is not retried.
	rejecting := mms.NewServer(mms.ServerOptions{Authenticate: func(context.Context, *mms.AuthContext) (mms.AuthResult, error) {
		return mms.AuthResult{}, nil
	}})
	raddr := serve(t, rejecting)
	attempts := 0
	_, err = iso.Dial(timeout(t), raddr, iso.WithRetry(mms.RetryPolicy{
		MaxAttempts: 5,
		OnRetry:     func(int, error, time.Duration) { attempts++ },
	}))
	if !errors.Is(err, mms.ErrConnectionRejected) || !errors.Is(err, mms.ErrAssociationFailed) {
		t.Fatalf("Dial to rejecting server = %v, want ErrConnectionRejected and ErrAssociationFailed", err)
	}
	if attempts != 0 {
		t.Errorf("rejected association retried %d times", attempts)
	}
}

// Shutdown stops ListenAndServe and aborts open associations.
func TestServer_Shutdown(t *testing.T) {
	rec := &recorder{}
	srv := mms.NewServer(mms.ServerOptions{Metrics: rec})
	srv.HandleIdentify(func(context.Context, mms.IdentifyRequest) (*mms.ServerIdentity, error) {
		return &mms.ServerIdentity{Vendor: "v"}, nil
	})
	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe(context.Background(), ln) }()

	c := dial(t, ln.Addr().String())
	ctx := timeout(t)
	if _, err := c.Identify(ctx); err != nil {
		t.Fatal(err)
	}
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, mms.ErrServerClosed) {
			t.Errorf("ListenAndServe after Shutdown = %v, want ErrServerClosed", err)
		}
	case <-ctx.Done():
		t.Fatal("ListenAndServe did not return after Shutdown")
	}
	select {
	case <-c.Done():
	case <-ctx.Done():
		t.Fatal("client association survived Shutdown")
	}
	eventually(t, "connection closed event", func() bool { return rec.hasConn(mms.RoleServer, mms.ConnectionClosed) })

	if err := srv.Serve(ctx, nil); !errors.Is(err, mms.ErrServerClosed) {
		t.Errorf("Serve after Shutdown = %v, want ErrServerClosed", err)
	}
	ln2, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.ListenAndServe(ctx, ln2); !errors.Is(err, mms.ErrServerClosed) {
		t.Errorf("ListenAndServe after Shutdown = %v, want ErrServerClosed", err)
	}
	if err := srv.Shutdown(ctx); err != nil {
		t.Errorf("second Shutdown = %v", err)
	}
}

// Shutdown waits for the request in progress, but not past its context.
func TestServer_Shutdown_InFlight(t *testing.T) {
	srv := mms.NewServer(mms.ServerOptions{})
	release := make(chan struct{})
	entered := make(chan struct{})
	srv.HandleIdentify(func(context.Context, mms.IdentifyRequest) (*mms.ServerIdentity, error) {
		close(entered)
		<-release
		return &mms.ServerIdentity{Vendor: "v"}, nil
	})
	addr := serve(t, srv)
	c := dial(t, addr)
	ctx := timeout(t)

	identified := make(chan error, 1)
	go func() {
		_, err := c.Identify(ctx)
		identified <- err
	}()
	<-entered

	short, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if err := srv.Shutdown(short); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Shutdown with a stuck handler = %v, want DeadlineExceeded", err)
	}
	close(release)
	if err := <-identified; !errors.Is(err, mms.ErrClosed) {
		t.Errorf("Identify interrupted by Shutdown = %v, want ErrClosed", err)
	}
}

// Metrics receives request and connection events on both sides.
func TestMetrics_Events(t *testing.T) {
	srvRec := &recorder{}
	cliRec := &recorder{}
	srv := mms.NewServer(mms.ServerOptions{Metrics: srvRec})
	if err := srv.RegisterDomain(dom); err != nil {
		t.Fatal(err)
	}
	if err := srv.RegisterVariable(mms.Variable{
		Name: domName("v"), TypeSpec: i32,
		Read: func(context.Context) (*mms.Value, error) { return mms.NewInteger(1), nil },
	}); err != nil {
		t.Fatal(err)
	}
	addr := serve(t, srv)
	c := dial(t, addr, mms.DialOptions{Metrics: cliRec})
	ctx := timeout(t)

	if _, err := c.Read(ctx, mms.ReadRequest{DomainID: dom, ItemID: "v"}); err != nil {
		t.Fatal(err)
	}
	_, err := c.Identify(ctx) // no handler: ServiceError
	var se *mms.ServiceError
	if !errors.As(err, &se) {
		t.Fatalf("Identify = %v, want ServiceError", err)
	}
	if err := c.Close(ctx); err != nil {
		t.Fatal(err)
	}

	reqs, conns := cliRec.snapshot()
	if len(reqs) != 2 || reqs[0].Service != mms.ServiceRead || reqs[0].Err != nil ||
		reqs[1].Service != mms.ServiceIdentify || !errors.As(reqs[1].Err, &se) || reqs[0].Role != mms.RoleClient {
		t.Errorf("client request events = %+v", reqs)
	}
	if len(conns) != 2 || conns[0].State != mms.ConnectionEstablished || conns[1].State != mms.ConnectionClosed || conns[1].Err != nil {
		t.Errorf("client connection events = %+v", conns)
	}
	eventually(t, "server close event", func() bool { return srvRec.hasConn(mms.RoleServer, mms.ConnectionClosed) })
	reqs, conns = srvRec.snapshot()
	if len(reqs) != 2 || reqs[0].Service != mms.ServiceRead || reqs[1].Err == nil || reqs[0].Role != mms.RoleServer {
		t.Errorf("server request events = %+v", reqs)
	}
	if conns[0].State != mms.ConnectionEstablished || conns[0].RemoteAddr == "" {
		t.Errorf("server connection events = %+v", conns)
	}
}

// RequestTimeout bounds requests; the association stays usable.
func TestClient_RequestTimeout(t *testing.T) {
	srv := mms.NewServer(mms.ServerOptions{})
	block := make(chan struct{})
	var once sync.Once
	srv.HandleIdentify(func(ctx context.Context, _ mms.IdentifyRequest) (*mms.ServerIdentity, error) {
		once.Do(func() { <-block })
		return &mms.ServerIdentity{Vendor: "v"}, nil
	})
	addr := serve(t, srv)
	c := dial(t, addr, mms.DialOptions{RequestTimeout: 50 * time.Millisecond})

	_, err := c.Identify(context.Background())
	if !errors.Is(err, mms.ErrInvokeTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Identify = %v, want ErrInvokeTimeout and DeadlineExceeded", err)
	}
	close(block)
	if _, err := c.Identify(context.Background()); err != nil {
		t.Fatalf("Identify after a timed-out request: %v", err)
	}
}

// A peer that connects but never sends the AARQ is dropped.
func TestServer_AssociationTimeout(t *testing.T) {
	rec := &recorder{}
	srv := mms.NewServer(mms.ServerOptions{AssociationTimeout: 100 * time.Millisecond, Metrics: rec})
	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	ctx := timeout(t)
	served := make(chan error, 1)
	go func() {
		conn, err := ln.Accept(ctx)
		if err != nil {
			served <- err
			return
		}
		defer func() { _ = conn.Close() }()
		served <- srv.Serve(ctx, conn)
	}()

	silent, err := iso.DialTCP(ctx, ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = silent.Close() }()
	select {
	case err := <-served:
		if !errors.Is(err, mms.ErrAssociationFailed) || !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Serve = %v, want ErrAssociationFailed and DeadlineExceeded", err)
		}
	case <-ctx.Done():
		t.Fatal("Serve did not give up on a silent peer")
	}
	if !rec.hasConn(mms.RoleServer, mms.ConnectionRejected) {
		t.Error("no rejected connection event")
	}
}

// A slow TCP peer that never finishes the COTP handshake does not block
// other connections.
func TestListener_SlowHandshake(t *testing.T) {
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := mms.NewServer(mms.ServerOptions{})
	srv.HandleIdentify(func(context.Context, mms.IdentifyRequest) (*mms.ServerIdentity, error) {
		return &mms.ServerIdentity{Vendor: "v"}, nil
	})
	ln := iso.NewListener(tcp, iso.WithHandshakeTimeout(200*time.Millisecond))
	go func() { _ = srv.ListenAndServe(context.Background(), ln) }()
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })

	silent, err := net.Dial("tcp", tcp.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = silent.Close() }()

	c := dial(t, tcp.Addr().String())
	if _, err := c.Identify(timeout(t)); err != nil {
		t.Fatalf("Identify while another peer stalls the handshake: %v", err)
	}
	// The silent peer is disconnected after the handshake timeout.
	_ = silent.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := silent.Read(make([]byte, 1)); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("silent peer read = %v, want EOF/reset", err)
	}
}

// Errors returned by Variable callbacks and handlers reach the client as
// the access or service errors they describe.
func TestServer_CallbackErrors(t *testing.T) {
	srv := mms.NewServer(mms.ServerOptions{})
	if err := srv.RegisterDomain(dom); err != nil {
		t.Fatal(err)
	}
	denied := &mms.DataAccessError{Code: mms.DataAccessErrorObjectAccessDenied}
	for name, rerr := range map[string]error{
		"denied":  denied,
		"wrapped": errors.Join(errors.New("ctx"), denied),
		"bogus":   &mms.DataAccessError{Code: mms.DataAccessErrorCode(99)},
		"plain":   errors.New("boom"),
	} {
		rerr := rerr
		if err := srv.RegisterVariable(mms.Variable{
			Name: domName(name), TypeSpec: i32,
			Read:  func(context.Context) (*mms.Value, error) { return nil, rerr },
			Write: func(context.Context, *mms.Value) error { return rerr },
		}); err != nil {
			t.Fatal(err)
		}
	}
	srv.HandleIdentify(func(context.Context, mms.IdentifyRequest) (*mms.ServerIdentity, error) {
		return nil, &mms.ServiceError{Class: mms.ErrorClassResource, Code: 1}
	})
	addr := serve(t, srv)
	c := dial(t, addr)
	ctx := timeout(t)

	for name, want := range map[string]mms.DataAccessErrorCode{
		"denied":  mms.DataAccessErrorObjectAccessDenied,
		"wrapped": mms.DataAccessErrorObjectAccessDenied,
		"bogus":   mms.DataAccessErrorTemporarilyUnavailable,
		"plain":   mms.DataAccessErrorTemporarilyUnavailable,
	} {
		_, err := c.Read(ctx, mms.ReadRequest{DomainID: dom, ItemID: mms.ItemID(name)})
		if got := dataAccessCode(t, err); got != want {
			t.Errorf("Read %s: code %s, want %s", name, got, want)
		}
		_, err = c.Write(ctx, mms.WriteRequest{DomainID: dom, ItemID: mms.ItemID(name), Value: mms.NewInteger(1)})
		if got := dataAccessCode(t, err); got != want {
			t.Errorf("Write %s: code %s, want %s", name, got, want)
		}
	}

	_, err := c.Identify(ctx)
	se := serviceError(t, err)
	if se.Class != mms.ErrorClassResource || se.Code != 1 || se.CodeName() != "memory-unavailable" {
		t.Errorf("Identify error = %+v (%s)", se, se.CodeName())
	}
}

// Caller mistakes are reported as ErrInvalidArgument without a round trip.
func TestClient_InvalidArgument(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)
	checks := map[string]error{}
	_, checks["Read empty domain"] = c.Read(ctx, mms.ReadRequest{ItemID: "x"})
	_, checks["Write nil value"] = c.Write(ctx, mms.WriteRequest{DomainID: dom, ItemID: "x"})
	_, checks["ReadMultiple bad name"] = c.ReadMultiple(ctx, []mms.ObjectName{{Scope: mms.ObjectScopeDomain, ItemID: "x"}})
	_, checks["WriteVariables mismatch"] = c.WriteVariables(ctx, specs(domName("a")), nil)
	_, checks["GetNameList bad class"] = c.GetNameList(ctx, mms.NameListRequest{ObjectClass: mms.ObjectClass(99)})
	checks["Define no vars"] = c.DefineNamedVariableList(ctx, mms.DefineNamedVariableListRequest{ListName: domName("l")})
	_, checks["Get bad selector"] = mms.NewInteger(1).Get(mms.SelectIndex(3))
	for what, err := range checks {
		if !errors.Is(err, mms.ErrInvalidArgument) {
			t.Errorf("%s: %v, want ErrInvalidArgument", what, err)
		}
	}
	if _, err := c.Identify(ctx); err != nil {
		t.Errorf("association broken by invalid arguments: %v", err)
	}
}
