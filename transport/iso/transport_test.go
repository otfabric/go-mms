// SPDX-License-Identifier: MIT

package iso_test

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-cotp"
	"github.com/otfabric/go-tpkt"

	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

func TestCOTPTransportRoundTrip(t *testing.T) {
	clientConn, serverConn := tcpLoopbackTransport(t)
	defer clientConn.Close()
	defer serverConn.Close()

	ctx := context.Background()
	msg := []byte("hello from client")

	if err := clientConn.Send(ctx, msg); err != nil {
		t.Fatalf("client send: %v", err)
	}

	got, err := serverConn.Receive(ctx)
	if err != nil {
		t.Fatalf("server receive: %v", err)
	}
	if string(got) != string(msg) {
		t.Fatalf("got %q, want %q", got, msg)
	}

	reply := []byte("hello from server")
	if err := serverConn.Send(ctx, reply); err != nil {
		t.Fatalf("server send: %v", err)
	}

	got, err = clientConn.Receive(ctx)
	if err != nil {
		t.Fatalf("client receive: %v", err)
	}
	if string(got) != string(reply) {
		t.Fatalf("got %q, want %q", got, reply)
	}
}

func TestDialTCPAndListen(t *testing.T) {
	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var serverConn mms.Transport
	var acceptErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		serverConn, acceptErr = ln.Accept(ctx)
	}()

	clientConn, err := iso.DialTCP(ctx, ln.Addr().String(),
		iso.WithCallingTSelector([]byte{0x00, 0x01}),
		iso.WithCalledTSelector([]byte{0x00, 0x02}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close()

	wg.Wait()
	if acceptErr != nil {
		t.Fatalf("accept: %v", acceptErr)
	}
	defer serverConn.Close()

	msg := []byte("integration test data")
	if err := clientConn.Send(ctx, msg); err != nil {
		t.Fatalf("send: %v", err)
	}
	got, err := serverConn.Receive(ctx)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if string(got) != string(msg) {
		t.Fatalf("got %q, want %q", got, msg)
	}
}

func TestTransportCloseIdempotent(t *testing.T) {
	clientConn, serverConn := tcpLoopbackTransport(t)
	defer serverConn.Close()

	if err := clientConn.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := clientConn.Close(); err != nil {
		t.Fatalf("second close should be nil, got: %v", err)
	}
}

func TestTransportSendAfterClose(t *testing.T) {
	clientConn, serverConn := tcpLoopbackTransport(t)
	defer serverConn.Close()

	clientConn.Close()
	err := clientConn.Send(context.Background(), []byte("data"))
	if err == nil {
		t.Fatal("expected error on send after close")
	}
	if !errors.Is(err, net.ErrClosed) {
		t.Errorf("got %v, want net.ErrClosed", err)
	}
}

// TestSendAfterCloseNoDeadlock is a regression test for the Send mutex
// leak. It verifies that calling Send, Close, and then Send again does
// not deadlock.
func TestSendAfterCloseNoDeadlock(t *testing.T) {
	clientConn, serverConn := tcpLoopbackTransport(t)
	defer serverConn.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		ctx := context.Background()
		clientConn.Send(ctx, []byte("before close"))
		clientConn.Close()
		clientConn.Send(ctx, []byte("after close"))
		clientConn.Close()
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("deadlock: Send/Close sequence did not complete within 3s")
	}
}

func TestReceiveAfterClose(t *testing.T) {
	clientConn, serverConn := tcpLoopbackTransport(t)
	defer serverConn.Close()

	clientConn.Close()
	_, err := clientConn.Receive(context.Background())
	if err == nil {
		t.Fatal("expected error on receive after close")
	}
	if !errors.Is(err, net.ErrClosed) {
		t.Errorf("got %v, want net.ErrClosed", err)
	}
}

func TestConcurrentCloseSend(t *testing.T) {
	clientConn, serverConn := tcpLoopbackTransport(t)
	defer serverConn.Close()

	ctx := context.Background()
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		clientConn.Close()
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			clientConn.Send(ctx, []byte("data"))
		}
	}()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock: concurrent Close+Send did not complete")
	}
}

func TestConcurrentCloseReceive(t *testing.T) {
	clientConn, serverConn := tcpLoopbackTransport(t)
	defer serverConn.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)
		clientConn.Close()
	}()

	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		clientConn.Receive(ctx)
	}()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock: concurrent Close+Receive did not complete")
	}
}

func TestAcceptContextCancel(t *testing.T) {
	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, err = ln.Accept(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled Accept")
	}
	if ctx.Err() == nil {
		t.Fatal("expected context deadline exceeded")
	}

	// Verify the listener is NOT destroyed: a new Accept with a fresh
	// context should be possible.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel2()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ln.Accept(ctx2)
	}()

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 500*time.Millisecond)
	if err != nil {
		t.Fatalf("dial after cancel should succeed (listener alive): %v", err)
	}
	conn.Close()

	wg.Wait()
}

func TestListenerCloseWhileBlocked(t *testing.T) {
	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := ln.Accept(ctx)
		done <- err
	}()

	time.Sleep(100 * time.Millisecond)
	ln.Close()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error after listener close")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Accept did not return after listener close")
	}
}

func TestNewListener(t *testing.T) {
	tcpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	ln := iso.NewListener(tcpLn)
	defer ln.Close()

	if ln.Addr() == nil {
		t.Fatal("Addr() returned nil")
	}
	if ln.Addr().String() != tcpLn.Addr().String() {
		t.Fatalf("Addr() = %s, want %s", ln.Addr(), tcpLn.Addr())
	}
}

// --- COTP handshake negative tests ---

// TestHandshakeWrongTPDUType verifies that a client sending a non-CR TPDU
// does not kill the listener. Accept skips the bad connection and continues.
func TestHandshakeWrongTPDUType(t *testing.T) {
	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	accepted := make(chan mms.Transport, 1)
	go func() {
		tr, _ := ln.Accept(ctx)
		accepted <- tr
	}()

	// First: send a bad (DT) TPDU — should be silently skipped.
	badConn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	dt := &cotp.DT{EOT: true, UserData: []byte("fake")}
	raw, _ := dt.MarshalBinary()
	w := tpkt.NewWriter(badConn)
	w.WriteFrame(raw)
	badConn.Close()

	// Second: good client connection — Accept should succeed.
	goodConn, err := iso.DialTCP(ctx, ln.Addr().String())
	if err != nil {
		t.Fatalf("good dial after bad client should succeed: %v", err)
	}
	defer goodConn.Close()

	select {
	case tr := <-accepted:
		if tr == nil {
			t.Fatal("Accept returned nil transport for good client")
		}
		tr.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("Accept did not return after bad+good client sequence")
	}
}

// TestHandshakeTSAPSelectorMismatch verifies that a TSAP mismatch is
// rejected per-connection (client gets DR) without killing the listener.
func TestHandshakeTSAPSelectorMismatch(t *testing.T) {
	ln, err := iso.Listen("127.0.0.1:0",
		iso.WithCalledTSelector([]byte{0xAA, 0xBB}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	accepted := make(chan mms.Transport, 1)
	go func() {
		tr, _ := ln.Accept(ctx)
		accepted <- tr
	}()

	// Bad client: wrong called selector.
	_, err = iso.DialTCP(ctx, ln.Addr().String(),
		iso.WithCalledTSelector([]byte{0xCC, 0xDD}),
	)
	if err == nil {
		t.Fatal("expected handshake error for TSAP mismatch")
	}

	// Good client: correct called selector. Listener should still be alive.
	goodConn, err := iso.DialTCP(ctx, ln.Addr().String(),
		iso.WithCalledTSelector([]byte{0xAA, 0xBB}),
	)
	if err != nil {
		t.Fatalf("good dial after TSAP mismatch should succeed: %v", err)
	}
	defer goodConn.Close()

	select {
	case tr := <-accepted:
		if tr == nil {
			t.Fatal("Accept returned nil transport for good client")
		}
		tr.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("Accept did not return after mismatch+good client")
	}
}

func TestClientHandshakeReceivesDR(t *testing.T) {
	tcpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcpLn.Close()

	go func() {
		conn, err := tcpLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		r := tpkt.NewReader(conn)
		w := tpkt.NewWriter(conn)

		crFrame, err := r.ReadFrame()
		if err != nil {
			return
		}
		decoded, err := cotp.Decode(crFrame)
		if err != nil || decoded.CR == nil {
			return
		}

		dr := &cotp.DR{
			DestinationRef: decoded.CR.SourceRef,
			SourceRef:      0,
			Reason:         2,
		}
		raw, _ := dr.MarshalBinary()
		w.WriteFrame(raw)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = iso.DialTCP(ctx, tcpLn.Addr().String())
	if err == nil {
		t.Fatal("expected error when server sends DR")
	}
	if !strings.Contains(err.Error(), "DR reason=2") {
		t.Errorf("error = %v, want to mention DR reason", err)
	}
}

func TestClientHandshakeCCRefMismatch(t *testing.T) {
	tcpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcpLn.Close()

	go func() {
		conn, err := tcpLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		r := tpkt.NewReader(conn)
		w := tpkt.NewWriter(conn)

		crFrame, err := r.ReadFrame()
		if err != nil {
			return
		}
		decoded, err := cotp.Decode(crFrame)
		if err != nil || decoded.CR == nil {
			return
		}

		cc := &cotp.CC{
			DestinationRef: decoded.CR.SourceRef + 999, // wrong ref
			SourceRef:      1,
			ClassOption:    0,
		}
		raw, _ := cc.MarshalBinary()
		w.WriteFrame(raw)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = iso.DialTCP(ctx, tcpLn.Addr().String())
	if err == nil {
		t.Fatal("expected error for CC destination ref mismatch")
	}
	if !strings.Contains(err.Error(), "destination ref") {
		t.Errorf("error = %v, want to mention destination ref", err)
	}
}

func TestClientHandshakeCCWrongClass(t *testing.T) {
	tcpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcpLn.Close()

	go func() {
		conn, err := tcpLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		r := tpkt.NewReader(conn)
		w := tpkt.NewWriter(conn)

		crFrame, err := r.ReadFrame()
		if err != nil {
			return
		}
		decoded, err := cotp.Decode(crFrame)
		if err != nil || decoded.CR == nil {
			return
		}

		cc := &cotp.CC{
			DestinationRef: decoded.CR.SourceRef,
			SourceRef:      1,
			ClassOption:    0x40, // class 4
		}
		raw, _ := cc.MarshalBinary()
		w.WriteFrame(raw)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = iso.DialTCP(ctx, tcpLn.Addr().String())
	if err == nil {
		t.Fatal("expected error for unsupported class")
	}
	if !strings.Contains(err.Error(), "class") {
		t.Errorf("error = %v, want to mention class", err)
	}
}

// TestServerHandshakeCRWrongClass verifies the server sends DR for a
// wrong-class CR and continues accepting. The listener is not destroyed.
func TestServerHandshakeCRWrongClass(t *testing.T) {
	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	accepted := make(chan mms.Transport, 1)
	go func() {
		tr, _ := ln.Accept(ctx)
		accepted <- tr
	}()

	// Bad client: send CR with class 2.
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	cr := &cotp.CR{
		SourceRef:   42,
		ClassOption: 0x20, // class 2
	}
	raw, _ := cr.MarshalBinary()
	w := tpkt.NewWriter(conn)
	w.WriteFrame(raw)

	// Expect DR back from server.
	r := tpkt.NewReader(conn)
	drFrame, err := r.ReadFrame()
	if err != nil {
		t.Fatalf("expected DR from server: %v", err)
	}
	decoded, err := cotp.Decode(drFrame)
	if err != nil {
		t.Fatalf("decode DR: %v", err)
	}
	if decoded.DR == nil {
		t.Fatalf("expected DR, got %s", decoded.Type)
	}
	if decoded.DR.Reason != 2 {
		t.Errorf("DR reason = %d, want 2", decoded.DR.Reason)
	}
	conn.Close()

	// Good client: listener should still be alive.
	goodConn, err := iso.DialTCP(ctx, ln.Addr().String())
	if err != nil {
		t.Fatalf("good dial after bad class should succeed: %v", err)
	}
	defer goodConn.Close()

	select {
	case tr := <-accepted:
		if tr == nil {
			t.Fatal("Accept returned nil transport for good client")
		}
		tr.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("Accept did not return after bad+good client")
	}
}

// tcpLoopbackTransport creates a pair of COTP transports connected via
// TCP loopback. The COTP handshake (CR/CC) is performed before returning.
func tcpLoopbackTransport(t *testing.T) (client mms.Transport, server mms.Transport) {
	t.Helper()

	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var serverConn mms.Transport
	var acceptErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		serverConn, acceptErr = ln.Accept(ctx)
	}()

	clientConn, err := iso.DialTCP(ctx, ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	wg.Wait()
	if acceptErr != nil {
		clientConn.Close()
		t.Fatalf("accept: %v", acceptErr)
	}

	return clientConn, serverConn
}

func TestWithClientDialOptions(t *testing.T) {
	opt := iso.WithClientDialOptions(mms.DialOptions{})
	if opt == nil {
		t.Fatal("nil option func")
	}
}

func TestWithLogger(t *testing.T) {
	opt := iso.WithLogger(slog.Default())
	if opt == nil {
		t.Fatal("nil option func")
	}
}
