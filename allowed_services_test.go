// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

// allowedServicesServer returns a server with one readable and writable
// integer variable d/v and a counter of the writes that reached it.
func allowedServicesServer(t *testing.T) (*Server, *atomic.Int32) {
	t.Helper()
	srv := NewServer(ServerOptions{})
	srv.HandleIdentify(func(context.Context, IdentifyRequest) (*ServerIdentity, error) {
		return &ServerIdentity{}, nil
	})
	if err := srv.RegisterDomain("d"); err != nil {
		t.Fatal(err)
	}
	writes := &atomic.Int32{}
	if err := srv.RegisterVariable(Variable{
		Name:     ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "v"},
		TypeSpec: TypeSpec{Type: ValueTypeInteger, Size: 32},
		Read:     func(context.Context) (*Value, error) { return NewInteger(7), nil },
		Write: func(context.Context, *Value) error {
			writes.Add(1)
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	return srv, writes
}

// sendTap counts the payloads the client hands to the transport.
type sendTap struct {
	mu    sync.Mutex
	sends int
}

func (s *sendTap) hook(direction string, _ []byte) {
	if direction != RawHookSend {
		return
	}
	s.mu.Lock()
	s.sends++
	s.mu.Unlock()
}

func (s *sendTap) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sends
}

func TestClient_AllowedServices_RefusesBeforeSending(t *testing.T) {
	srv, writes := allowedServicesServer(t)
	tap := &sendTap{}
	log := &eventLog{}
	c, _ := serveLoopback(t, srv, DialOptions{
		RawHook:         tap.hook,
		Metrics:         log,
		AllowedServices: []Service{ServiceRead},
	})
	ctx := context.Background()
	defer func() { _ = c.Abort(ctx) }()
	name := ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "v"}

	res, err := c.ReadObject(ctx, name)
	if err != nil {
		t.Fatalf("ReadObject: %v", err)
	}
	if v, ok := res.Value.Int64(); !ok || v != 7 {
		t.Fatalf("ReadObject value = %v", res.Value)
	}
	before := tap.count()

	if _, err := c.WriteObject(ctx, name, NewInteger(1)); !errors.Is(err, ErrServiceNotAllowed) {
		t.Fatalf("WriteObject = %v, want ErrServiceNotAllowed", err)
	}
	if _, err := c.Identify(ctx); !errors.Is(err, ErrServiceNotAllowed) {
		t.Fatalf("Identify = %v, want ErrServiceNotAllowed", err)
	}
	if got := tap.count(); got != before {
		t.Errorf("refused requests sent %d payloads", got-before)
	}
	if writes.Load() != 0 {
		t.Errorf("server saw %d writes", writes.Load())
	}

	// The association is still usable after a refusal.
	if _, err := c.ReadObject(ctx, name); err != nil {
		t.Fatalf("ReadObject after refusal: %v", err)
	}

	log.mu.Lock()
	defer log.mu.Unlock()
	var refused []Service
	for _, ev := range log.requests {
		if errors.Is(ev.Err, ErrServiceNotAllowed) {
			refused = append(refused, ev.Service)
		}
	}
	if len(refused) != 2 || refused[0] != ServiceWrite || refused[1] != ServiceIdentify {
		t.Errorf("refused request events = %v, want [Write Identify]", refused)
	}
}

func TestClient_AllowedServices_EmptyRefusesAll(t *testing.T) {
	srv, _ := allowedServicesServer(t)
	c, _ := serveLoopback(t, srv, DialOptions{AllowedServices: []Service{}})
	ctx := context.Background()
	defer func() { _ = c.Abort(ctx) }()
	_, err := c.ReadObject(ctx, ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "v"})
	if !errors.Is(err, ErrServiceNotAllowed) {
		t.Fatalf("ReadObject = %v, want ErrServiceNotAllowed", err)
	}
	// Conclude is association control, not a confirmed service.
	if err := c.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestClient_AllowedServices_NilAllowsAll(t *testing.T) {
	srv, writes := allowedServicesServer(t)
	c, _ := serveLoopback(t, srv, DialOptions{})
	ctx := context.Background()
	defer func() { _ = c.Abort(ctx) }()
	if _, err := c.WriteObject(ctx, ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "v"}, NewInteger(1)); err != nil {
		t.Fatalf("WriteObject: %v", err)
	}
	if writes.Load() != 1 {
		t.Errorf("server saw %d writes, want 1", writes.Load())
	}
}
