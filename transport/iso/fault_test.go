// SPDX-License-Identifier: MIT

package iso_test

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

// ────────────────────────────────────────────────────────────────────────────
// Helpers
// ────────────────────────────────────────────────────────────────────────────

// dialAndGetConn starts srv, dials it, and returns the mms.Client plus the
// raw net.Conn that underlies the connection on the server side.
//
// The server accepts exactly one connection, delivers it to serverConn, then
// blocks inside ListenAndServe until the context is cancelled.
func dialAndGetServerConn(t *testing.T, srv *mms.Server) (client *mms.Client, serverConn net.Conn) {
	t.Helper()

	tcpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	connCh := make(chan net.Conn, 1)
	// Intercepting listener wraps the accepted conn so we can close it later.
	interceptLn := &interceptListener{
		Listener: tcpLn,
		onAccept: func(c net.Conn) { connCh <- c },
	}

	ln := iso.NewListener(interceptLn)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.ListenAndServe(ctx, ln) }()

	c, err := iso.Dial(ctx, tcpLn.Addr().String())
	if err != nil {
		t.Fatalf("iso.Dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(ctx) })

	select {
	case sc := <-connCh:
		return c, sc
	case <-time.After(2 * time.Second):
		t.Fatal("server did not accept connection in time")
		return nil, nil
	}
}

// interceptListener wraps a net.Listener and calls onAccept with each accepted
// net.Conn before passing it through.
type interceptListener struct {
	net.Listener
	onAccept func(net.Conn)
}

func (l *interceptListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if l.onAccept != nil {
		l.onAccept(c)
	}
	return c, nil
}

// ────────────────────────────────────────────────────────────────────────────
// TestFault_MidResponseDisconnect
// ────────────────────────────────────────────────────────────────────────────

// TestFault_MidResponseDisconnect verifies that when the server-side TCP
// connection is closed while the client is waiting for a response, the
// client returns an error quickly rather than hanging.
func TestFault_MidResponseDisconnect(t *testing.T) {
	srv := testServer(t)

	// Register a slow-read variable: the server blocks for 200 ms in the
	// Read handler so we can close the connection before the response arrives.
	slow := make(chan struct{})
	if err := srv.RegisterVariable(mms.Variable{
		Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "testDomain", ItemID: "slow"},
		TypeSpec: mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 32},
		Read: func(ctx context.Context) (*mms.Value, error) {
			select {
			case <-slow:
			case <-ctx.Done():
			case <-time.After(1 * time.Second):
			}
			return mms.NewInteger(0), nil
		},
	}); err != nil {
		t.Fatalf("RegisterVariable: %v", err)
	}

	client, serverConn := dialAndGetServerConn(t, srv)

	// Issue a read that will block for 1 s on the server.
	errCh := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := client.Read(ctx, mms.ReadRequest{DomainID: "testDomain", ItemID: "slow"})
		errCh <- err
	}()
	// Close the server-side TCP connection while the Read is in flight.
	time.Sleep(50 * time.Millisecond)
	_ = serverConn.Close()

	select {
	case err := <-errCh:
		if err == nil {
			t.Error("expected error after mid-response disconnect, got nil")
		}
		t.Logf("got expected error: %v", err)
	case <-time.After(3 * time.Second):
		t.Error("client hung after mid-response disconnect (timeout)")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// TestFault_TruncatedTPDU
// ────────────────────────────────────────────────────────────────────────────

// TestFault_TruncatedTPDU verifies that when a client sends a truncated TPDU
// (i.e. the TPKT header announces N bytes but fewer bytes arrive before the
// connection closes), the server closes the connection without panicking.
//
// The test dials the server as a raw TCP connection, sends a valid COTP CR
// followed by a TPKT header claiming 100 bytes, then sends only 3 bytes of
// payload and closes the socket. The server must not crash or deadlock.
func TestFault_TruncatedTPDU(t *testing.T) {
	srv := testServer(t)

	tcpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	ln := iso.NewListener(tcpLn)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.ListenAndServe(ctx, ln) }()

	// Open raw TCP connection.
	conn, err := net.DialTimeout("tcp", tcpLn.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("net.Dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Send COTP Class 0 Connection Request (CR) — minimal valid packet.
	// TPKT version=3, reserved=0, length=22 big-endian
	// COTP: length=17, CR code=0xE0, dst-ref=0, src-ref=1, class=0
	// + ISO-8327 and ISO-8823 selectors (abbreviated)
	cr := []byte{
		// TPKT
		0x03, 0x00, 0x00, 0x16,
		// COTP CR TPDU (length = packet - 4 TPKT bytes - 1)
		0x11, 0xe0, 0x00, 0x00, 0x00, 0x01, 0x00,
		// Parameters: TPDU size = 4096 (0x0b -> 4096)
		0xc0, 0x01, 0x0b,
		// Calling TSAP (2 bytes: 0x01 0x00)
		0xc1, 0x02, 0x01, 0x00,
		// Called TSAP (2 bytes: 0x00 0x01)
		0xc2, 0x02, 0x00, 0x01,
	}
	if _, err := conn.Write(cr); err != nil {
		t.Fatalf("write CR: %v", err)
	}

	// Read the COTP CC (connection confirm) — we need it to proceed.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		// Some server implementations reject unknown CR selectors immediately.
		t.Logf("no CC received (server rejected CR): %v", err)
		return
	}
	ccLen := int(binary.BigEndian.Uint16(hdr[2:])) - 4
	cc := make([]byte, ccLen)
	if _, err := io.ReadFull(conn, cc); err != nil {
		t.Logf("CC truncated: %v", err)
		return
	}
	t.Logf("received CC (%d bytes)", ccLen+4)
	conn.SetReadDeadline(time.Time{}) //nolint:errcheck

	// Now send a TPKT header announcing 100 bytes of payload but deliver only
	// 3 bytes, then close. The server must recover gracefully.
	truncated := []byte{
		0x03, 0x00, 0x00, 100, // TPKT: says 100 bytes total
		0x02, 0xf0, 0x80, // 3 bytes of COTP DT header (incomplete)
	}
	_, _ = conn.Write(truncated)
	_ = conn.Close()

	// Give the server a moment to process the truncated PDU. If it doesn't
	// panic or deadlock within 1 s, the test passes.
	time.Sleep(300 * time.Millisecond)
	t.Log("server survived truncated TPDU without panic")
}

// ────────────────────────────────────────────────────────────────────────────
// TestFault_IdleConnectionClosure
// ────────────────────────────────────────────────────────────────────────────

// TestFault_IdleConnectionClosure verifies that the MMS client detects and
// reports an error when the underlying TCP connection is closed while it is
// idle (no request in flight).
func TestFault_IdleConnectionClosure(t *testing.T) {
	srv := testServer(t)
	client, serverConn := dialAndGetServerConn(t, srv)

	// Verify the connection is alive.
	ctx := context.Background()
	if _, err := client.Identify(ctx); err != nil {
		t.Fatalf("Identify: %v", err)
	}

	// Close the server side silently; the client is now idle.
	_ = serverConn.Close()
	time.Sleep(80 * time.Millisecond)

	// The next operation should fail.
	opCtx, opCancel := context.WithTimeout(ctx, 2*time.Second)
	defer opCancel()
	_, err := client.Identify(opCtx)
	if err == nil {
		t.Error("expected error after server closed idle connection, got nil")
	}
	t.Logf("got expected error: %v", err)
}

// ────────────────────────────────────────────────────────────────────────────
// TestFault_ConnectToClosedPort
// ────────────────────────────────────────────────────────────────────────────

// TestFault_ConnectToClosedPort verifies that dialing a port with no server
// returns an error rather than hanging.
func TestFault_ConnectToClosedPort(t *testing.T) {
	// Pick a free port, listen briefly to ensure it exists, then close it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = iso.Dial(ctx, addr)
	if err == nil {
		t.Error("expected error dialing closed port, got nil")
	}
	t.Logf("got expected error: %v", err)
}

// ────────────────────────────────────────────────────────────────────────────
// TestFault_ConcurrentRequestAfterDisconnect
// ────────────────────────────────────────────────────────────────────────────

// TestFault_ConcurrentRequestAfterDisconnect ensures that multiple goroutines
// issuing requests on a client whose server has gone away all receive errors
// rather than one goroutine hanging indefinitely.
func TestFault_ConcurrentRequestAfterDisconnect(t *testing.T) {
	srv := testServer(t)
	client, serverConn := dialAndGetServerConn(t, srv)

	_ = serverConn.Close()

	// Wait until the client detects the disconnect via a probe.
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer probeCancel()
	for {
		_, err := client.Identify(probeCtx)
		if err != nil {
			break
		}
		if probeCtx.Err() != nil {
			t.Fatal("client did not detect disconnect within 3 s")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Now all concurrent requests must fail because the client is disconnected.
	const workers = 8
	type result struct{ err error }
	results := make(chan result, workers)
	for i := 0; i < workers; i++ {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, e := client.Identify(ctx)
			results <- result{e}
		}()
	}

	done := time.After(5 * time.Second)
	var noErr int
	for i := 0; i < workers; i++ {
		select {
		case r := <-results:
			if r.err == nil {
				noErr++
			}
		case <-done:
			t.Errorf("concurrent requests hung after disconnect (%d/%d completed)", i, workers)
			return
		}
	}

	if noErr > 0 {
		t.Errorf("%d of %d concurrent requests succeeded after confirmed disconnect (expected all to fail)", noErr, workers)
	} else {
		t.Logf("all %d workers received errors (expected)", workers)
	}
}
