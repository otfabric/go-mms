// SPDX-License-Identifier: MIT

package iso

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"
)

// syncBuffer is a goroutine-safe log sink.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestRetryPolicy_LogsAndChains(t *testing.T) {
	var logs syncBuffer
	var chained []int
	o := applyOptions([]Option{
		WithLogger(slog.New(slog.NewTextHandler(&logs, nil))),
		WithRetry(mms.RetryPolicy{
			MaxAttempts: 3, InitialBackoff: time.Microsecond,
			OnRetry: func(attempt int, _ error, _ time.Duration) { chained = append(chained, attempt) },
		}),
	})
	p := o.retryPolicy("h:1")
	p.OnRetry(1, errors.New("boom"), time.Millisecond)
	if len(chained) != 1 || !strings.Contains(logs.String(), "retrying") || !strings.Contains(logs.String(), "h:1") {
		t.Errorf("chained=%v logs=%q", chained, logs.String())
	}

	// Without a logger the policy is returned unchanged.
	if applyOptions(nil).retryPolicy("x").OnRetry != nil {
		t.Error("OnRetry added without a logger")
	}
	// A logger without a user OnRetry still logs.
	o = applyOptions([]Option{WithLogger(slog.New(slog.NewTextHandler(&logs, nil)))})
	o.retryPolicy("x").OnRetry(1, errors.New("e"), 0)
}

func TestDialTCP_WithDialerAndRetry(t *testing.T) {
	_, srvAddr := startTestListener(t)
	var calls atomic.Int32
	dialer := func(ctx context.Context, network, addr string) (net.Conn, error) {
		if calls.Add(1) < 3 {
			return nil, errors.New("transient")
		}
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var logs syncBuffer
	tr, err := DialTCP(ctx, srvAddr,
		WithDialer(dialer),
		WithLogger(slog.New(slog.NewTextHandler(&logs, nil))),
		WithRetry(mms.RetryPolicy{MaxAttempts: 5, InitialBackoff: time.Millisecond}))
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	_ = tr.Close()
	if calls.Load() != 3 {
		t.Errorf("dialer calls = %d, want 3", calls.Load())
	}
	if strings.Count(logs.String(), "retrying") != 2 {
		t.Errorf("logs = %q", logs.String())
	}

	// Exhausted attempts return the last error.
	calls.Store(-100)
	_, err = DialTCP(ctx, srvAddr, WithDialer(dialer), WithRetry(mms.RetryPolicy{MaxAttempts: 2, InitialBackoff: time.Microsecond}))
	if err == nil || !strings.Contains(err.Error(), "transient") {
		t.Errorf("DialTCP = %v, want transient", err)
	}
}

func TestDial_RetriesDialFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var calls atomic.Int32
	_, err := Dial(ctx, "127.0.0.1:1",
		WithDialer(func(context.Context, string, string) (net.Conn, error) {
			calls.Add(1)
			return nil, errors.New("refused")
		}),
		WithRetry(mms.RetryPolicy{MaxAttempts: 3, InitialBackoff: time.Microsecond}))
	if err == nil || calls.Load() != 3 {
		t.Errorf("Dial = %v after %d calls", err, calls.Load())
	}
}

// startTestListener runs a Listener that accepts and keeps transports.
func startTestListener(t *testing.T, opts ...Option) (*Listener, string) {
	t.Helper()
	ln, err := Listen("127.0.0.1:0", opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			tr, err := ln.Accept(context.Background())
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = tr.Close() })
		}
	}()
	return ln, ln.Addr().String()
}

func TestListen_BadAddress(t *testing.T) {
	if _, err := Listen("256.0.0.1:bad"); err == nil || !strings.Contains(err.Error(), "iso: listen") {
		t.Errorf("Listen = %v", err)
	}
}

func TestListener_HandshakeTimeoutLogged(t *testing.T) {
	var logs syncBuffer
	ln, addr := startTestListener(t,
		WithHandshakeTimeout(30*time.Millisecond),
		WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
	_ = ln
	silent, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = silent.Close() }()
	// The silent peer is disconnected by the listener.
	_ = silent.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := silent.Read(make([]byte, 1)); err == nil {
		t.Fatal("silent peer not disconnected")
	}
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(logs.String(), "cotp handshake failed") {
		if time.Now().After(deadline) {
			t.Fatalf("no handshake warning logged: %q", logs.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(logs.String(), "remote_addr=") {
		t.Errorf("log lacks remote_addr: %q", logs.String())
	}

	// A healthy client still connects after the timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tr, err := DialTCP(ctx, addr)
	if err != nil {
		t.Fatalf("DialTCP after timeout: %v", err)
	}
	_ = tr.Close()
}

func TestListener_NoHandshakeTimeout(t *testing.T) {
	_, addr := startTestListener(t, WithHandshakeTimeout(-1))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tr, err := DialTCP(ctx, addr)
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	_ = tr.Close()
}

// tempErr is a net.Error that reports itself as temporary.
type tempErr struct{}

func (tempErr) Error() string   { return "temporary" }
func (tempErr) Timeout() bool   { return false }
func (tempErr) Temporary() bool { return true }

// scriptedListener returns scripted Accept errors, then blocks until closed.
type scriptedListener struct {
	mu     sync.Mutex
	errs   []error
	calls  int
	closed chan struct{}
	once   sync.Once
}

func (l *scriptedListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	l.calls++
	if len(l.errs) > 0 {
		err := l.errs[0]
		l.errs = l.errs[1:]
		l.mu.Unlock()
		return nil, err
	}
	l.mu.Unlock()
	<-l.closed
	return nil, net.ErrClosed
}

func (l *scriptedListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *scriptedListener) Addr() net.Addr { return &net.TCPAddr{} }

func TestListener_TemporaryAcceptErrors(t *testing.T) {
	sl := &scriptedListener{errs: []error{tempErr{}, tempErr{}, errors.New("fatal")}, closed: make(chan struct{})}
	l := NewListener(sl)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := l.Accept(ctx)
	if err == nil || !strings.Contains(err.Error(), "fatal") {
		t.Fatalf("Accept = %v, want fatal", err)
	}
	if sl.calls != 3 {
		t.Errorf("Accept calls = %d, want 3 (two temporary errors retried)", sl.calls)
	}
	// Accept keeps reporting the loop's error.
	if _, err := l.Accept(ctx); err == nil {
		t.Error("second Accept succeeded")
	}
	if err := l.Close(); err != nil {
		t.Errorf("Close = %v", err)
	}
	if err := l.Close(); err != nil {
		t.Error("Close not idempotent")
	}
}

func TestListener_CloseDuringTemporaryBackoff(t *testing.T) {
	sl := &scriptedListener{errs: []error{tempErr{}}, closed: make(chan struct{})}
	l := NewListener(sl)
	done := make(chan error, 1)
	go func() {
		_, err := l.Accept(context.Background())
		done <- err
	}()
	time.Sleep(2 * time.Millisecond)
	_ = l.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Error("Accept succeeded after Close")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Accept did not return after Close")
	}
}

func TestIsTemporary(t *testing.T) {
	if !isTemporary(tempErr{}) || isTemporary(errors.New("x")) || isTemporary(nil) {
		t.Error("isTemporary")
	}
}

// deadlineRecorder records deadline calls.
type deadlineRecorder struct {
	net.Conn
	mu    sync.Mutex
	calls []string
}

func (d *deadlineRecorder) record(s string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, s)
	return nil
}

func (d *deadlineRecorder) SetDeadline(time.Time) error      { return d.record("both") }
func (d *deadlineRecorder) SetReadDeadline(time.Time) error  { return d.record("read") }
func (d *deadlineRecorder) SetWriteDeadline(time.Time) error { return d.record("write") }

func TestSplitDeadlineConn(t *testing.T) {
	rec := &deadlineRecorder{}
	s := newSplitDeadlineConn(rec)
	now := time.Now()
	_ = s.SetDeadline(now)
	_ = s.SetReadDeadline(now)
	_ = s.SetWriteDeadline(now)
	s.detach()
	_ = s.SetDeadline(now)
	_ = s.SetReadDeadline(now)
	_ = s.SetWriteDeadline(now)
	if got := strings.Join(rec.calls, ","); got != "both,read,write" {
		t.Errorf("forwarded deadline calls = %s, want both,read,write", got)
	}
}

func TestArmDirection(t *testing.T) {
	rec := &deadlineRecorder{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	disarm := armDirection(ctx, rec.SetReadDeadline)
	cancel()
	time.Sleep(5 * time.Millisecond) // AfterFunc runs asynchronously
	disarm()
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.calls) != 3 {
		t.Errorf("deadline calls = %v, want set, cancel, clear", rec.calls)
	}
}

func TestCtxIOError(t *testing.T) {
	io := errors.New("i/o timeout")
	if ctxIOError(context.Background(), io) != io {
		t.Error("error changed without a done context")
	}
	if ctxIOError(context.Background(), nil) != nil {
		t.Error("nil changed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := ctxIOError(ctx, io)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, io) {
		t.Errorf("ctxIOError = %v", err)
	}
	if ctxIOError(ctx, nil) != nil {
		t.Error("nil with done context")
	}
	// A passed deadline counts even before the context's timer fires.
	past, cancelPast := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelPast()
	if !errors.Is(ctxDoneErr(past), context.DeadlineExceeded) {
		t.Error("passed deadline not reported")
	}
	future, cancelFuture := context.WithTimeout(context.Background(), time.Hour)
	defer cancelFuture()
	if ctxDoneErr(future) != nil {
		t.Error("future deadline reported")
	}
}
