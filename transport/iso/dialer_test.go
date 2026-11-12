// SPDX-License-Identifier: MIT

package iso_test

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-mms/transport/iso"
)

// The dialer contract of WithDialer (API-R03, otfabric/engineering
// standards/api-design.md#transport-dialers).

// TestDialerReceivesNetworkAddrAndDeadline: the dialer is called once with
// network "tcp", the address given to DialTCP and the caller's deadline,
// and the dial completes over the connection it returns.
func TestDialerReceivesNetworkAddrAndDeadline(t *testing.T) {
	var (
		calls       int
		gotNetwork  string
		gotAddr     string
		hasDeadline bool
	)
	dialer := func(ctx context.Context, network, addr string) (net.Conn, error) {
		calls++
		gotNetwork, gotAddr = network, addr
		_, hasDeadline = ctx.Deadline()
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}
	addr := ""
	_, _, err := captureCR(t, nil, iso.WithDialer(func(ctx context.Context, network, a string) (net.Conn, error) {
		addr = a
		return dialer(ctx, network, a)
	}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if calls != 1 || gotNetwork != "tcp" || gotAddr != addr || !strings.HasPrefix(gotAddr, "127.0.0.1:") || !hasDeadline {
		t.Fatalf("calls=%d network=%q addr=%q deadline=%v, want 1 tcp 127.0.0.1:port true", calls, gotNetwork, gotAddr, hasDeadline)
	}
}

// TestDialerCancelReachesDialer: cancelling the caller's context cancels
// the context the dialer holds.
func TestDialerCancelReachesDialer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	go func() {
		<-started
		cancel()
	}()
	_, err := iso.DialTCP(ctx, "192.0.2.1:102", iso.WithDialer(func(ctx context.Context, _, _ string) (net.Conn, error) {
		close(started)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
			return nil, errors.New("context not cancelled")
		}
	}))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// TestDialerErrorWrapped: the dialer's error keeps its identity and gains
// the address.
func TestDialerErrorWrapped(t *testing.T) {
	errRefused := errors.New("destination not in allowlist")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := iso.Dial(ctx, "192.0.2.1:102", iso.WithDialer(func(context.Context, string, string) (net.Conn, error) {
		return nil, errRefused
	}))
	if !errors.Is(err, errRefused) || !strings.Contains(err.Error(), "192.0.2.1:102") {
		t.Fatalf("err = %v, want it to wrap the dialer's error with the address", err)
	}
}

// TestDialerNilConnNilError: a dialer that returns neither a connection
// nor an error fails the dial instead of panicking.
func TestDialerNilConnNilError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tr, err := iso.DialTCP(ctx, "192.0.2.1:102", iso.WithDialer(func(context.Context, string, string) (net.Conn, error) {
		return nil, nil
	}))
	if err == nil || tr != nil {
		t.Fatalf("tr = %v err = %v, want an error and no transport", tr, err)
	}
}

// TestDialerConnWithErrorClosed: a connection returned together with an
// error is closed, and the error is returned.
func TestDialerConnWithErrorClosed(t *testing.T) {
	client, peer := net.Pipe()
	defer func() { _ = peer.Close() }()
	errHalf := errors.New("half-open")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := iso.DialTCP(ctx, "192.0.2.1:102", iso.WithDialer(func(context.Context, string, string) (net.Conn, error) {
		return client, errHalf
	}))
	if !errors.Is(err, errHalf) {
		t.Fatalf("err = %v, want the dialer's error", err)
	}
	_ = peer.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, rerr := peer.Read(make([]byte, 1)); !errors.Is(rerr, io.EOF) {
		t.Fatalf("peer read = %v, want io.EOF (connection closed)", rerr)
	}
}

// TestDialerInjectedFault: a connection the peer closes before the COTP
// handshake fails the dial with an error, without hanging.
func TestDialerInjectedFault(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := iso.DialTCP(ctx, "192.0.2.1:102", iso.WithDialer(func(context.Context, string, string) (net.Conn, error) {
		client, peer := net.Pipe()
		go func() {
			_, _ = peer.Read(make([]byte, 64)) // take the CR, then cut
			_ = peer.Close()
		}()
		return client, nil
	}))
	if err == nil || !strings.Contains(err.Error(), "cotp connect") {
		t.Fatalf("err = %v, want a cotp connect error", err)
	}
	if ctx.Err() != nil {
		t.Fatal("dial ran until the deadline")
	}
}
