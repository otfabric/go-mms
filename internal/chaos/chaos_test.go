// SPDX-License-Identifier: MIT

package chaos

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"
)

// tcpPair returns a connected TCP loopback pair (client, server).
func tcpPair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	type res struct {
		c   net.Conn
		err error
	}
	ch := make(chan res, 1)
	go func() {
		c, err := ln.Accept()
		ch <- res{c, err}
	}()
	cli, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	r := <-ch
	if r.err != nil {
		t.Fatal(r.err)
	}
	t.Cleanup(func() {
		_ = cli.Close()
		_ = r.c.Close()
	})
	return cli, r.c
}

func randBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

// transfer writes data on w in a goroutine and reads it fully from r.
func transfer(t *testing.T, w io.Writer, r io.Reader, data []byte) []byte {
	t.Helper()
	errc := make(chan error, 1)
	go func() {
		n, err := w.Write(data)
		if err == nil && n != len(data) {
			err = io.ErrShortWrite
		}
		errc <- err
	}()
	got := make([]byte, len(data))
	if _, err := io.ReadFull(r, got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("write: %v", err)
	}
	return got
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.Is(err, os.ErrDeadlineExceeded) && errors.As(err, &ne) && ne.Timeout()
}

func TestPassThroughPipe(t *testing.T) {
	t.Parallel()
	a, b := net.Pipe()
	ca, cb := Wrap(a, Config{}), Wrap(b, Config{})
	defer func() { _ = ca.Close() }()
	defer func() { _ = cb.Close() }()

	data := []byte("hello, mms")
	got := transfer(t, ca, cb, data)
	if !bytes.Equal(got, data) {
		t.Fatalf("got %q want %q", got, data)
	}
	if s := ca.Stats(); s.BytesWritten != int64(len(data)) || s.SplitWrites != 0 || s.Delays != 0 {
		t.Fatalf("writer stats: %+v", s)
	}
	if s := cb.Stats(); s.BytesRead != int64(len(data)) || s.PartialReads != 0 {
		t.Fatalf("reader stats: %+v", s)
	}
	// Zero-length operations pass through.
	go func() { _, _ = ca.Write(nil) }()
	if n, err := cb.Read(nil); n != 0 || err != nil {
		t.Fatalf("zero read: %d %v", n, err)
	}
}

func TestPassThroughTCP(t *testing.T) {
	t.Parallel()
	cli, srv := tcpPair(t)
	cc, cs := Wrap(cli, Config{}), Wrap(srv, Config{})
	data := randBytes(t, 64<<10)
	if got := transfer(t, cc, cs, data); !bytes.Equal(got, data) {
		t.Fatal("data mismatch")
	}
	if got := transfer(t, cs, cc, data); !bytes.Equal(got, data) {
		t.Fatal("data mismatch (reverse)")
	}
	if cc.LocalAddr().String() != cli.LocalAddr().String() || cc.RemoteAddr().String() != cli.RemoteAddr().String() {
		t.Fatal("addresses not forwarded")
	}
	if cc.Seed() == 0 {
		t.Fatal("expected a non-zero effective seed")
	}
}

func TestChunkedStreamIntegrity(t *testing.T) {
	t.Parallel()
	cli, srv := tcpPair(t)
	cfg := Config{Seed: 7, ChunkReads: true, ChunkWrites: true}
	cc, cs := Wrap(cli, cfg), Wrap(srv, cfg)
	data := randBytes(t, 1<<20)
	got := transfer(t, cc, cs, data)
	if !bytes.Equal(got, data) {
		t.Fatal("chunked stream corrupted")
	}
	if s := cc.Stats(); s.SplitWrites != 1 || s.BytesWritten != int64(len(data)) {
		t.Fatalf("writer stats: %+v", s)
	}
	if s := cs.Stats(); s.PartialReads == 0 || s.BytesRead != int64(len(data)) {
		t.Fatalf("reader stats: %+v", s)
	}
}

func TestConcurrentBidirectional(t *testing.T) {
	t.Parallel()
	cli, srv := tcpPair(t)
	cfg := Config{Seed: 3, ChunkReads: true, ChunkWrites: true, MaxDelay: 50 * time.Microsecond}
	cc, cs := Wrap(cli, cfg), Wrap(srv, cfg)
	a, b := randBytes(t, 32<<10), randBytes(t, 32<<10)
	var wg sync.WaitGroup
	var gotA, gotB []byte
	wg.Add(2)
	go func() { defer wg.Done(); gotA = transfer(t, cc, cs, a) }()
	go func() { defer wg.Done(); gotB = transfer(t, cs, cc, b) }()
	wg.Wait()
	if !bytes.Equal(gotA, a) || !bytes.Equal(gotB, b) {
		t.Fatal("data mismatch")
	}
	if cc.Stats().Delays == 0 {
		t.Fatal("expected delays")
	}
}

func TestThrottle(t *testing.T) {
	t.Parallel()
	a, b := net.Pipe()
	cfg := Config{BytesPerSecond: 100_000}
	ca, cb := Wrap(a, cfg), Wrap(b, Config{})
	defer func() { _ = ca.Close() }()
	defer func() { _ = cb.Close() }()
	data := randBytes(t, 25_000) // ~250ms at 100 kB/s.
	start := time.Now()
	if got := transfer(t, ca, cb, data); !bytes.Equal(got, data) {
		t.Fatal("data mismatch")
	}
	if el := time.Since(start); el < 150*time.Millisecond {
		t.Fatalf("throttled transfer took only %v", el)
	}
	if ca.Stats().Throttles == 0 {
		t.Fatal("expected throttle waits")
	}
}

func TestThrottleReadAndDeadline(t *testing.T) {
	t.Parallel()
	a, b := net.Pipe()
	ca, cb := Wrap(a, Config{}), Wrap(b, Config{BytesPerSecond: 1000})
	defer func() { _ = ca.Close() }()
	defer func() { _ = cb.Close() }()
	go func() { _, _ = ca.Write(make([]byte, 100)) }()
	buf := make([]byte, 100)
	n, err := cb.Read(buf)
	if err != nil || n != 20 { // Granularity is BytesPerSecond/50.
		t.Fatalf("read %d %v", n, err)
	}
	_ = cb.SetReadDeadline(time.Now().Add(5 * time.Millisecond))
	if _, err := cb.Read(buf); !isTimeout(err) {
		t.Fatalf("expected timeout during throttle, got %v", err)
	}

	// A throttled Write interrupted by its deadline reports a short write.
	_ = cb.SetReadDeadline(time.Time{})
	go func() { _, _ = io.Copy(io.Discard, ca) }()
	_ = cb.SetWriteDeadline(time.Now().Add(30 * time.Millisecond))
	n, err = cb.Write(make([]byte, 1000))
	if n == 0 || n >= 1000 || !isTimeout(err) {
		t.Fatalf("throttled write: n=%d err=%v", n, err)
	}
}

func TestDropAfterBytesWrite(t *testing.T) {
	t.Parallel()
	cli, srv := tcpPair(t)
	cc := Wrap(cli, Config{DropAfterBytes: 1000, ChunkWrites: true, Seed: 1})
	readErr := make(chan error, 1)
	var got int64
	go func() {
		var err error
		got, err = io.Copy(io.Discard, srv)
		if err == nil {
			err = io.EOF
		}
		readErr <- err
	}()
	n, err := cc.Write(make([]byte, 4000))
	if n != 1000 || !errors.Is(err, ErrInjected) {
		t.Fatalf("write: n=%d err=%v", n, err)
	}
	if err := <-readErr; err == nil {
		t.Fatal("peer saw no error")
	}
	if got != 1000 {
		t.Fatalf("peer read %d bytes", got)
	}
	if _, err := cc.Write([]byte{1}); !errors.Is(err, ErrInjected) {
		t.Fatalf("write after drop: %v", err)
	}
	if _, err := cc.Read(make([]byte, 1)); !errors.Is(err, ErrInjected) {
		t.Fatalf("read after drop: %v", err)
	}
	if s := cc.Stats(); s.Drops != 1 || s.BytesWritten != 1000 {
		t.Fatalf("stats: %+v", s)
	}
	if err := cc.Close(); err != nil {
		t.Fatalf("close after drop: %v", err)
	}
	if err := cc.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestDropAfterBytesReadJitter(t *testing.T) {
	t.Parallel()
	cli, srv := tcpPair(t)
	cs := Wrap(srv, Config{DropAfterBytes: 500, ResetJitter: 300, Seed: 99, ChunkReads: true})
	go func() { _, _ = cli.Write(make([]byte, 2000)) }()
	total, err := io.Copy(io.Discard, cs)
	if !errors.Is(err, ErrInjected) {
		t.Fatalf("expected injected error, got %v", err)
	}
	if total < 500 || total >= 800 {
		t.Fatalf("dropped after %d bytes, want [500,800)", total)
	}
	// The peer observes the drop too.
	_ = cli.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := cli.Read(make([]byte, 1)); err == nil {
		t.Fatal("peer read succeeded after drop")
	}
}

func TestDropProbability(t *testing.T) {
	t.Parallel()
	cli, srv := tcpPair(t)
	cc := Wrap(cli, Config{DropProbability: 1, Seed: 5})
	n, err := cc.Write(make([]byte, 100))
	if n >= 100 || !errors.Is(err, ErrInjected) {
		t.Fatalf("write: n=%d err=%v", n, err)
	}
	_ = srv.SetReadDeadline(time.Now().Add(2 * time.Second))
	if m, err := io.Copy(io.Discard, srv); err != nil || m != int64(n) {
		t.Fatalf("peer: read %d (want %d) err=%v", m, n, err)
	}

	a, b := net.Pipe()
	defer func() { _ = b.Close() }()
	ca := Wrap(a, Config{DropProbability: 1})
	if _, err := ca.Read(make([]byte, 1)); !errors.Is(err, ErrInjected) {
		t.Fatalf("read: %v", err)
	}
}

func TestStallHonoursDeadlines(t *testing.T) {
	t.Parallel()
	a, b := net.Pipe()
	defer func() { _ = b.Close() }()
	ca := Wrap(a, Config{StallProbability: 1})
	defer func() { _ = ca.Close() }()

	_ = ca.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	start := time.Now()
	if _, err := ca.Read(make([]byte, 1)); !isTimeout(err) {
		t.Fatalf("read: %v", err)
	}
	_ = ca.SetDeadline(time.Now().Add(20 * time.Millisecond))
	if _, err := ca.Write([]byte{1}); !isTimeout(err) {
		t.Fatalf("write: %v", err)
	}
	if el := time.Since(start); el > time.Second {
		t.Fatalf("stalls took %v", el)
	}
	// An already expired deadline fails immediately.
	if _, err := ca.Read(make([]byte, 1)); !isTimeout(err) {
		t.Fatalf("expired: %v", err)
	}
	// Setting a deadline wakes a stall that started without one.
	_ = ca.SetDeadline(time.Time{})
	errc := make(chan error, 1)
	go func() { _, err := ca.Read(make([]byte, 1)); errc <- err }()
	time.Sleep(10 * time.Millisecond)
	_ = ca.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
	select {
	case err := <-errc:
		if !isTimeout(err) {
			t.Fatalf("woken stall: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stall not woken by deadline change")
	}
	if ca.Stats().Stalls != 4 {
		t.Fatalf("stats: %+v", ca.Stats())
	}
}

func TestDelayHonoursDeadline(t *testing.T) {
	t.Parallel()
	a, b := net.Pipe()
	defer func() { _ = b.Close() }()
	ca := Wrap(a, Config{MaxDelay: time.Hour, Seed: 11})
	defer func() { _ = ca.Close() }()
	_ = ca.SetWriteDeadline(time.Now().Add(20 * time.Millisecond))
	start := time.Now()
	if _, err := ca.Write([]byte{1}); !isTimeout(err) {
		t.Fatalf("write: %v", err)
	}
	if el := time.Since(start); el > time.Second {
		t.Fatalf("delay took %v", el)
	}
	if ca.Stats().Delays != 1 {
		t.Fatalf("stats: %+v", ca.Stats())
	}
}

func TestCloseUnblocksStall(t *testing.T) {
	t.Parallel()
	a, b := net.Pipe()
	defer func() { _ = b.Close() }()
	ca := Wrap(a, Config{StallProbability: 1})
	errc := make(chan error, 2)
	go func() { _, err := ca.Read(make([]byte, 1)); errc <- err }()
	go func() { _, err := ca.Write([]byte{1}); errc <- err }()
	time.Sleep(10 * time.Millisecond)
	if err := ca.Close(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case err := <-errc:
			if !errors.Is(err, net.ErrClosed) {
				t.Fatalf("got %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("close did not unblock stall")
		}
	}
	if err := ca.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if _, err := ca.Read(make([]byte, 1)); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("read after close: %v", err)
	}
}

func TestListenerAndDialer(t *testing.T) {
	t.Parallel()
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{Seed: 21, ChunkReads: true, ChunkWrites: true, MaxDelay: 100 * time.Microsecond}
	ln := WrapListener(inner, cfg)
	defer func() { _ = ln.Close() }()

	accepted := make(chan *Conn, 2)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				close(accepted)
				return
			}
			cc := c.(*Conn)
			accepted <- cc
			go func() { _, _ = io.Copy(cc, cc); _ = cc.Close() }()
		}
	}()

	dial := Dialer(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var seeds []int64
	for range 2 {
		c, err := dial(ctx, "tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		data := randBytes(t, 16<<10)
		if got := transfer(t, c, c, data); !bytes.Equal(got, data) {
			t.Fatal("echo mismatch")
		}
		seeds = append(seeds, c.(*Conn).Seed())
		_ = c.Close()
		srvConn := <-accepted
		seeds = append(seeds, srvConn.Seed())
	}
	// Per-connection seeds are deterministic and distinct.
	want := []int64{deriveSeed(21, 1), deriveSeed(21, 1), deriveSeed(21, 2), deriveSeed(21, 2)}
	for i := range want {
		if seeds[i] != want[i] {
			t.Fatalf("seeds %v want %v", seeds, want)
		}
	}
	if seeds[0] == seeds[2] {
		t.Fatal("seeds not distinct")
	}

	_ = ln.Close()
	if _, ok := <-accepted; ok {
		t.Fatal("expected accept loop to end")
	}
	// Dial errors are returned unchanged.
	if _, err := dial(ctx, "tcp", ln.Addr().String()); err == nil {
		t.Fatal("dial to closed listener succeeded")
	}
	// Seed 0 picks a time-based base seed.
	if WrapListener(inner, Config{}).(*listener).base == 0 {
		t.Fatal("zero base seed")
	}
	if _, err := Dialer(Config{})(ctx, "tcp", ln.Addr().String()); err == nil {
		t.Fatal("dial to closed listener succeeded")
	}
}

func TestDeterministicDecisions(t *testing.T) {
	t.Parallel()
	run := func() Stats {
		a, b := net.Pipe()
		defer func() { _ = b.Close() }()
		ca := Wrap(a, Config{Seed: 1234, StallProbability: 0.5})
		defer func() { _ = ca.Close() }()
		_ = ca.SetDeadline(time.Now().Add(-time.Second))
		for range 50 {
			_, _ = ca.Read(make([]byte, 1))
		}
		return ca.Stats()
	}
	s1, s2 := run(), run()
	if s1 != s2 || s1.Stalls == 0 || s1.Stalls == 50 {
		t.Fatalf("non-deterministic or degenerate: %+v vs %+v", s1, s2)
	}
	if deriveSeed(0, 0) == 0 {
		t.Fatal("derived seed must be non-zero")
	}
}
