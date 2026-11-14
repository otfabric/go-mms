// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"testing"
	"time"
)

// reentryServer has the variable LD/v, whose read callback is read.
func reentryServer(t *testing.T, read func(context.Context) (*Value, error)) *Server {
	t.Helper()
	srv := NewServer(ServerOptions{})
	srv.HandleIdentify(func(context.Context, IdentifyRequest) (*ServerIdentity, error) {
		return &ServerIdentity{Vendor: "v1"}, nil
	})
	if err := srv.RegisterDomain("LD"); err != nil {
		t.Fatal(err)
	}
	if err := srv.RegisterVariable(Variable{
		Name:     ObjectName{Scope: ObjectScopeDomain, Domain: "LD", ItemID: "v"},
		TypeSpec: TypeSpec{Type: ValueTypeInteger, Size: 32},
		Read:     read,
	}); err != nil {
		t.Fatal(err)
	}
	return srv
}

func reentryClient(t *testing.T, srv *Server) *Client {
	t.Helper()
	c, served := serveLoopback(t, srv, DialOptions{})
	t.Cleanup(func() { _ = c.Abort(context.Background()); _ = waitServe(t, served) })
	return c
}

// within runs fn and reports whether it returned within d. When it did
// not, fn keeps running on its own goroutine.
func within(d time.Duration, fn func()) bool {
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// A read callback may register the Identify and Status handlers: the
// server holds no lock of its own while a callback runs.
func TestReadCallbackRegistersHandlers(t *testing.T) {
	var srv *Server
	srv = reentryServer(t, func(context.Context) (*Value, error) {
		registered := make(chan struct{})
		go func() {
			defer close(registered)
			srv.HandleIdentify(func(context.Context, IdentifyRequest) (*ServerIdentity, error) {
				return &ServerIdentity{Vendor: "v2"}, nil
			})
			srv.HandleStatus(func(context.Context, StatusRequest) (*ServerStatus, error) {
				return &ServerStatus{}, nil
			})
		}()
		select {
		case <-registered:
			return NewInteger(1), nil
		case <-time.After(2 * time.Second):
			return nil, errors.New("HandleIdentify and HandleStatus did not return while the callback ran")
		}
	})
	c := reentryClient(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := c.Read(ctx, ReadRequest{DomainID: "LD", ItemID: "v"}); err != nil {
		t.Fatalf("Read: %v", err)
	}
	id, err := c.Identify(ctx)
	if err != nil || id.Vendor != "v2" {
		t.Fatalf("Identify = %+v, %v; want the handler the callback registered", id, err)
	}
}

// While a callback of one association is blocked and a HandleIdentify
// call is made, another association is still served.
func TestBlockedCallbackDoesNotStopOtherAssociations(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	srv := reentryServer(t, func(context.Context) (*Value, error) {
		close(entered)
		<-release
		return NewInteger(1), nil
	})
	slow := reentryClient(t, srv)
	other := reentryClient(t, srv)

	readDone := make(chan error, 1)
	go func() {
		_, err := slow.Read(context.Background(), ReadRequest{DomainID: "LD", ItemID: "v"})
		readDone <- err
	}()
	<-entered

	ok := within(2*time.Second, func() {
		srv.HandleIdentify(func(context.Context, IdentifyRequest) (*ServerIdentity, error) {
			return &ServerIdentity{Vendor: "v2"}, nil
		})
	})
	if !ok {
		t.Error("HandleIdentify waited for a blocked read callback")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	id, err := other.Identify(ctx)
	cancel()
	if err != nil || id.Vendor != "v2" {
		t.Errorf("Identify on another association = %+v, %v; want the new handler's answer", id, err)
	}
	close(release)
	if err := <-readDone; err != nil {
		t.Fatalf("blocked Read: %v", err)
	}
}
