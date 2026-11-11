// SPDX-License-Identifier: MIT

package e2e

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

// gatedServer serves "fast" at once and blocks a Read of "slow" until
// release is closed (or the test ends).
type gatedServer struct {
	addr     string
	entered  chan struct{}
	release  chan struct{}
	openOnce sync.Once
}

func startGatedServer(t *testing.T, opts mms.ServerOptions) *gatedServer {
	t.Helper()
	g := &gatedServer{entered: make(chan struct{}, 64), release: make(chan struct{})}
	srv := mms.NewServer(opts)
	if err := srv.RegisterDomain(dom); err != nil {
		t.Fatal(err)
	}
	ts := mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 32}
	register := func(item string, read func(context.Context) (*mms.Value, error)) {
		if err := srv.RegisterVariable(mms.Variable{Name: domName(item), TypeSpec: ts, Read: read}); err != nil {
			t.Fatal(err)
		}
	}
	register("fast", func(context.Context) (*mms.Value, error) { return mms.NewInteger(1), nil })
	register("slow", func(ctx context.Context) (*mms.Value, error) {
		g.entered <- struct{}{}
		select {
		case <-g.release:
		case <-ctx.Done():
		}
		return mms.NewInteger(2), nil
	})
	g.addr = serve(t, srv)
	// Registered after serve, so it runs before serve's cleanup cancels
	// the server: a handler still blocked would hold the shutdown.
	t.Cleanup(func() { g.open() })
	return g
}

func (g *gatedServer) open() { g.openOnce.Do(func() { close(g.release) }) }

func (g *gatedServer) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-g.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the slow Read handler was not entered")
	}
}

// cutProxy forwards a TCP connection between a client and addr. Once
// armed, the next server-to-client TPKT is cut in half and the
// client side is half-closed (CloseWrite): the client sees EOF in the
// middle of a TPKT while its own writes still reach the proxy.
type cutProxy struct {
	addr  string
	armed atomic.Bool
	cuts  atomic.Int32
	wg    sync.WaitGroup
}

func startCutProxy(t *testing.T, upstream string) *cutProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &cutProxy{addr: ln.Addr().String()}
	var mu sync.Mutex
	var conns []net.Conn
	track := func(c net.Conn) {
		mu.Lock()
		conns = append(conns, c)
		mu.Unlock()
	}
	p.wg.Go(func() {
		for {
			down, err := ln.Accept()
			if err != nil {
				return
			}
			up, err := net.Dial("tcp", upstream)
			if err != nil {
				_ = down.Close()
				continue
			}
			track(down)
			track(up)
			// The client never half-closes: when its side ends, both do.
			p.wg.Go(func() { _, _ = io.Copy(up, down); _ = up.Close(); _ = down.Close() })
			p.wg.Go(func() { p.downstream(down.(*net.TCPConn), up) })
		}
	})
	t.Cleanup(func() {
		_ = ln.Close()
		mu.Lock()
		for _, c := range conns {
			_ = c.Close()
		}
		mu.Unlock()
		p.wg.Wait()
	})
	return p
}

func (p *cutProxy) downstream(down *net.TCPConn, up net.Conn) {
	buf := make([]byte, 64<<10)
	for {
		n, err := up.Read(buf)
		if n > 0 {
			if p.armed.CompareAndSwap(true, false) {
				// Half of the first TPKT (RFC 1006: length in octets
				// 2..3); the segment may carry more PDUs after it.
				cut := n
				if n >= 4 {
					cut = min(n, int(binary.BigEndian.Uint16(buf[2:4])))
				}
				_, _ = down.Write(buf[:max(1, cut/2)])
				_ = down.CloseWrite()
				p.cuts.Add(1)
				_, _ = io.Copy(io.Discard, up)
				return
			}
			if _, werr := down.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			_ = down.CloseWrite()
			return
		}
	}
}

// A peer that half-closes the connection in the middle of a response
// PDU ends the association: the request in flight and every request
// waiting at the negotiated max-services-outstanding-calling (ISO 9506-2
// clause 8.2) return promptly with ErrClosed, Done
// closes, Err is set, and nothing is left running (goleak in TestMain).
func TestRobustness_HalfCloseMidPDU(t *testing.T) {
	for _, limit := range []int{1, 3} {
		t.Run(fmt.Sprintf("limit=%d", limit), func(t *testing.T) {
			g := startGatedServer(t, mms.ServerOptions{MMS: mms.ServerMMSOptions{MaxOutstandingCalling: limit}})
			p := startCutProxy(t, g.addr)
			c := dial(t, p.addr)
			if got := c.Negotiated().MaxOutCalling; got != limit {
				t.Fatalf("negotiated MaxOutCalling %d, want %d", got, limit)
			}
			ctx := timeout(t)
			if _, err := c.ReadObject(ctx, domName("fast")); err != nil {
				t.Fatalf("ReadObject before the cut: %v", err)
			}

			// One request blocks in the server; the rest fill the
			// remaining slots and wait for one.
			const callers = 8
			errs := make(chan error, callers)
			go func() { _, err := c.ReadObject(ctx, domName("slow")); errs <- err }()
			g.waitEntered(t)
			for range callers - 1 {
				go func() { _, err := c.ReadObject(ctx, domName("fast")); errs <- err }()
			}
			time.Sleep(20 * time.Millisecond) // let the callers queue
			p.armed.Store(true)
			g.open()

			for range callers {
				select {
				case err := <-errs:
					if !errors.Is(err, mms.ErrClosed) {
						t.Errorf("request ended with %v, want ErrClosed", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("a request did not return after the peer half-closed mid-PDU")
				}
			}
			if p.cuts.Load() != 1 {
				t.Fatalf("proxy cut %d responses, want 1", p.cuts.Load())
			}
			select {
			case <-c.Done():
			case <-time.After(5 * time.Second):
				t.Fatal("Done not closed after the peer half-closed mid-PDU")
			}
			if c.Err() == nil {
				t.Error("Err() = nil after a lost association")
			}
			if _, err := c.ReadObject(ctx, domName("fast")); !errors.Is(err, mms.ErrClosed) {
				t.Errorf("ReadObject after the loss = %v, want ErrClosed", err)
			}
		})
	}
}

// Close while a request is outstanding at a negotiated limit of 1 and
// others wait for the slot: the waiting requests end with ErrClosed at
// once, Close completes the Conclude behind the outstanding request
// (the server answers in order) and returns within its context.
func TestLifecycle_CloseWithRequestsWaitingAtLimit(t *testing.T) {
	g := startGatedServer(t, mms.ServerOptions{MMS: mms.ServerMMSOptions{MaxOutstandingCalling: 1}})
	c := dial(t, g.addr)
	ctx := timeout(t)

	const waiters = 4
	errs := make(chan error, waiters+1)
	go func() { _, err := c.ReadObject(ctx, domName("slow")); errs <- err }()
	g.waitEntered(t)
	for range waiters {
		go func() { _, err := c.ReadObject(ctx, domName("fast")); errs <- err }()
	}
	time.Sleep(20 * time.Millisecond)

	closed := make(chan error, 1)
	go func() { closed <- c.Close(ctx) }()
	for range waiters + 1 {
		select {
		case err := <-errs:
			if !errors.Is(err, mms.ErrClosed) {
				t.Errorf("request during Close = %v, want ErrClosed", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("a request did not return after Close")
		}
	}
	g.open() // the server answers the Read, then the Conclude
	select {
	case err := <-closed:
		if err != nil {
			t.Errorf("Close = %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("Close did not return")
	}
	if err := c.Close(ctx); err != nil {
		t.Errorf("second Close = %v, want nil", err)
	}
}

// openSockets counts this process's open socket descriptors, or -1
// where the platform has no /dev/fd. Other descriptors are left out: the
// test binary's own pipes come and go, and go-mms opens only sockets.
func openSockets(t *testing.T) int {
	t.Helper()
	if runtime.GOOS == "windows" {
		return -1
	}
	ents, err := os.ReadDir("/dev/fd")
	if err != nil {
		return -1
	}
	n := 0
	for _, e := range ents {
		if fi, err := os.Stat("/dev/fd/" + e.Name()); err == nil && fi.Mode()&os.ModeSocket != 0 {
			n++
		}
	}
	return n
}

// Associations that end every way (Close, Abort, the peer half-closing
// mid-PDU, a failed dial) release their sockets on both sides; no socket
// descriptor is left open.
func TestRobustness_NoFDLeak(t *testing.T) {
	g := startGatedServer(t, mms.ServerOptions{})
	p := startCutProxy(t, g.addr)
	if openSockets(t) < 0 {
		t.Skip("no /dev/fd on this platform")
	}
	ctx := timeout(t)
	cycle := func(i int) {
		addr := g.addr
		if i%4 == 2 {
			addr = p.addr
		}
		c, err := iso.Dial(ctx, addr)
		if err != nil {
			t.Fatalf("cycle %d: Dial: %v", i, err)
		}
		if _, err := c.ReadObject(ctx, domName("fast")); err != nil {
			t.Fatalf("cycle %d: ReadObject: %v", i, err)
		}
		switch i % 4 {
		case 0:
			if err := c.Close(ctx); err != nil {
				t.Fatalf("cycle %d: Close: %v", i, err)
			}
		case 1:
			_ = c.Abort(ctx)
		case 2:
			p.armed.Store(true)
			_, _ = c.ReadObject(ctx, domName("fast"))
			<-c.Done()
			_ = c.Close(ctx)
		case 3:
			// Then a dial that fails before an association exists.
			_ = c.Close(ctx)
			if _, err := iso.Dial(ctx, "127.0.0.1:1"); err == nil {
				t.Fatalf("cycle %d: Dial to a closed port succeeded", i)
			}
		}
	}
	// Warm up the runtime's poller and the server before counting.
	for i := range 4 {
		cycle(i)
	}
	before := openSockets(t)
	const cycles = 40
	for i := range cycles {
		cycle(i)
	}
	// The server closes its side asynchronously.
	deadline := time.Now().Add(5 * time.Second)
	for {
		after := openSockets(t)
		if after <= before {
			t.Logf("%d open sockets before, %d after %d associations", before, after, cycles)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d open sockets before, %d after %d associations: sockets leak", before, after, cycles)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
