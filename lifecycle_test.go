// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"go.uber.org/goleak"
)

// heldServer has the variable LD/v. A Read of it is held on the server:
// the callback reports on entered and returns only after release.
func heldServer(t *testing.T) (srv *Server, entered <-chan struct{}, release func()) {
	t.Helper()
	in := make(chan struct{}, 1)
	out := make(chan struct{})
	var once sync.Once
	srv = reentryServer(t, func(context.Context) (*Value, error) {
		select {
		case in <- struct{}{}:
		default:
		}
		<-out
		return NewInteger(1), nil
	})
	release = func() { once.Do(func() { close(out) }) }
	t.Cleanup(release)
	return srv, in, release
}

var heldVar = ReadRequest{DomainID: "LD", ItemID: "v"}

// readAsync starts a Read of LD/v and returns the channel of its error.
func readAsync(ctx context.Context, c *Client) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		_, err := c.Read(ctx, heldVar)
		errCh <- err
	}()
	return errCh
}

// Close while a request is held on the server (L3): the request fails
// with ErrClosed at once; Close completes the conclude once the server
// answers.
func TestCloseDuringInFlightRequest(t *testing.T) {
	srv, entered, release := heldServer(t)
	c, served := serveLoopback(t, srv, DialOptions{})
	errCh := readAsync(context.Background(), c)
	<-entered

	closed := make(chan error, 1)
	go func() { closed <- c.Close(context.Background()) }()
	if err := <-errCh; !errors.Is(err, ErrClosed) {
		t.Fatalf("request during Close = %v, want ErrClosed", err)
	}
	release()
	if err := <-closed; err != nil {
		t.Fatalf("Close: %v", err)
	}
	<-c.Done()
	if err := waitServe(t, served); err != nil {
		t.Fatalf("Serve after Close = %v, want nil", err)
	}
}

// Two concurrent Close calls (L2): both return nil, the association ends
// and nothing is left behind.
func TestDoubleCloseConcurrent(t *testing.T) {
	ignore := goleak.IgnoreCurrent()
	srv := testServer(t)
	c, served := serveLoopback(t, srv, DialOptions{})

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() { errs <- c.Close(context.Background()) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("Close: %v", err)
		}
	}
	<-c.Done()
	if err := waitServe(t, served); err != nil {
		t.Errorf("Serve = %v", err)
	}
	goleak.VerifyNone(t, ignore)
}

// A request whose context is cancelled while the server holds it (L6)
// returns context.Canceled; the association stays up and serves the next
// request.
func TestContextCancellationDuringRequest(t *testing.T) {
	srv, entered, release := heldServer(t)
	c, served := serveLoopback(t, srv, DialOptions{})
	t.Cleanup(func() { _ = c.Abort(context.Background()); _ = waitServe(t, served) })

	ctx, cancel := context.WithCancel(context.Background())
	errCh := readAsync(ctx, c)
	<-entered
	cancel()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request = %v, want context.Canceled", err)
	}
	if err := c.Err(); err != nil {
		t.Fatalf("association ended: %v", err)
	}
	release()
	rctx, rcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer rcancel()
	if res, err := c.Read(rctx, heldVar); err != nil || res.Value == nil {
		t.Fatalf("Read after the cancelled request = %+v, %v", res, err)
	}
}

// Abort while a request is held on the server: the request fails with
// ErrClosed, Abort returns nil, and the server's Serve returns.
func TestAbortDuringInFlightRequest(t *testing.T) {
	srv, entered, release := heldServer(t)
	c, served := serveLoopback(t, srv, DialOptions{})
	errCh := readAsync(context.Background(), c)
	<-entered

	if err := c.Abort(context.Background()); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if err := <-errCh; !errors.Is(err, ErrClosed) {
		t.Fatalf("request after Abort = %v, want ErrClosed", err)
	}
	release()
	if err := waitServe(t, served); err == nil {
		t.Fatal("Serve of an aborted association returned nil")
	}
}

// Close whose context ends before the server answers the Conclude
// returns the context's error, and the association is ended all the same.
func TestCloseWithTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv, entered, release := heldServer(t)
		c, served := serveLoopback(t, srv, DialOptions{})
		errCh := readAsync(context.Background(), c)
		<-entered // the server answers nothing until release

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		start := time.Now()
		if err := c.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Close = %v, want DeadlineExceeded", err)
		}
		if d := time.Since(start); d != 50*time.Millisecond {
			t.Errorf("Close took %v, its context 50ms", d)
		}
		select {
		case <-c.Done():
		default:
			t.Fatal("association still open after Close returned")
		}
		if err := <-errCh; !errors.Is(err, ErrClosed) {
			t.Errorf("request = %v, want ErrClosed", err)
		}
		if err := c.Close(context.Background()); err != nil {
			t.Errorf("second Close = %v, want nil", err)
		}
		release()
		_ = waitServe(t, served)
	})
}

// One association's association-scoped objects are not visible to
// another association of the same server (L13).
func TestAssociationScopeIsolation(t *testing.T) {
	srv := testServer(t)
	a, servedA := serveLoopback(t, srv, DialOptions{})
	b, servedB := serveLoopback(t, srv, DialOptions{})
	t.Cleanup(func() {
		_ = a.Abort(context.Background())
		_ = b.Abort(context.Background())
		_ = waitServe(t, servedA)
		_ = waitServe(t, servedB)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	list := ObjectName{Scope: ObjectScopeAssociation, ItemID: "aaList"}
	if err := a.DefineNamedVariableList(ctx, DefineNamedVariableListRequest{
		ListName:  list,
		Variables: []VariableSpec{{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "temperature"}}},
	}); err != nil {
		t.Fatalf("define on association A: %v", err)
	}
	names := func(c *Client) []string {
		t.Helper()
		res, err := c.GetNameList(ctx, NameListRequest{ObjectClass: ObjectClassNamedVariableList, Scope: ObjectScopeAssociation})
		if err != nil {
			t.Fatalf("GetNameList: %v", err)
		}
		return res.Names
	}
	if got := names(a); len(got) != 1 || got[0] != "aaList" {
		t.Fatalf("association A lists %q, want [aaList]", got)
	}
	if got := names(b); len(got) != 0 {
		t.Fatalf("association B lists %q, want none", got)
	}
	if _, err := b.GetNamedVariableListAttributes(ctx, list); err == nil {
		t.Fatal("association B reads the attributes of A's list")
	}
}
