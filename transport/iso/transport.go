// SPDX-License-Identifier: MIT

package iso

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/otfabric/go-cotp"
)

// cotpTransport implements mms.Transport over an established *cotp.Conn.
// Session SPDUs are transferred as TSDUs; TPKT/COTP framing is owned by go-cotp.
//
// MMS uses the connection full duplex: a reader goroutine waits in Receive
// while requests are sent. go-cotp arms conn.SetDeadline (both directions)
// for every TSDU read or write and clears it afterwards, so a write whose
// context expires would abort a concurrent read and vice versa. The
// transport therefore hands go-cotp a [splitDeadlineConn] that ignores
// deadlines once the handshake is done, and applies each context as a
// read or write deadline itself.
type cotpTransport struct {
	mu   sync.Mutex
	cotp *cotp.Conn
	raw  net.Conn // same conn passed to Connect/Accept; used for TLSConnectionState
}

func wrapCOTP(c *cotp.Conn, raw net.Conn, shim *splitDeadlineConn) *cotpTransport {
	shim.detach()
	return &cotpTransport{cotp: c, raw: raw}
}

// splitDeadlineConn is the net.Conn given to go-cotp. It forwards
// deadlines during the COTP handshake and ignores them afterwards, when
// cotpTransport owns per-direction deadlines.
type splitDeadlineConn struct {
	net.Conn
	detached atomic.Bool
}

func newSplitDeadlineConn(c net.Conn) *splitDeadlineConn {
	return &splitDeadlineConn{Conn: c}
}

func (c *splitDeadlineConn) detach() { c.detached.Store(true) }

func (c *splitDeadlineConn) SetDeadline(t time.Time) error {
	if c.detached.Load() {
		return nil
	}
	return c.Conn.SetDeadline(t)
}

func (c *splitDeadlineConn) SetReadDeadline(t time.Time) error {
	if c.detached.Load() {
		return nil
	}
	return c.Conn.SetReadDeadline(t)
}

func (c *splitDeadlineConn) SetWriteDeadline(t time.Time) error {
	if c.detached.Load() {
		return nil
	}
	return c.Conn.SetWriteDeadline(t)
}

// armDirection applies ctx to one direction of the connection: its
// deadline, and an immediate deadline when ctx is cancelled. The returned
// function clears it.
func armDirection(ctx context.Context, set func(time.Time) error) func() {
	if dl, ok := ctx.Deadline(); ok {
		_ = set(dl)
	}
	stop := context.AfterFunc(ctx, func() { _ = set(time.Unix(1, 0)) })
	return func() {
		stop()
		_ = set(time.Time{})
	}
}

// ctxIOError prefers the context's error when ctx ended an I/O operation.
func ctxIOError(ctx context.Context, err error) error {
	if err != nil && ctx.Err() != nil {
		return fmt.Errorf("%w: %w", ctx.Err(), err)
	}
	return err
}

func (t *cotpTransport) isClosed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cotp == nil
}

// Send writes one Session SPDU as a COTP TSDU.
func (t *cotpTransport) Send(ctx context.Context, data []byte) error {
	t.mu.Lock()
	c := t.cotp
	t.mu.Unlock()
	if c == nil {
		return net.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Cancellation is checked before the write but does not interrupt it:
	// abandoning a TSDU half-written would corrupt the stream and end the
	// association for every other request. A context deadline still
	// bounds the write (a slow or stalled peer).
	if dl, ok := ctx.Deadline(); ok {
		_ = t.raw.SetWriteDeadline(dl)
		defer func() { _ = t.raw.SetWriteDeadline(time.Time{}) }()
	}
	return ctxIOError(ctx, c.WriteTSDU(context.Background(), data))
}

// Receive reads one Session SPDU (reassembled TSDU).
func (t *cotpTransport) Receive(ctx context.Context) ([]byte, error) {
	t.mu.Lock()
	c := t.cotp
	t.mu.Unlock()
	if c == nil {
		return nil, net.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	disarm := armDirection(ctx, t.raw.SetReadDeadline)
	data, err := c.ReadTSDU(context.Background())
	disarm()
	return data, ctxIOError(ctx, err)
}

// Close closes the TP0 connection. Idempotent; returns nil on success.
func (t *cotpTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cotp == nil {
		return nil
	}
	err := t.cotp.Close()
	t.cotp = nil
	// Local Close and a peer that already dropped the link are both
	// success: the transport is gone. go-cotp reports the latter as
	// ErrDisconnected (often wrapping io.EOF), not ErrClosed.
	// A connection that already failed reports that failure again on
	// Close; it was returned by the Send or Receive that hit it.
	var netErr net.Error
	if err == nil ||
		(errors.As(err, &netErr) && netErr.Timeout()) ||
		errors.Is(err, cotp.ErrClosed) ||
		errors.Is(err, cotp.ErrDisconnected) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// RemoteAddr returns the remote network address.
func (t *cotpTransport) RemoteAddr() net.Addr {
	t.mu.Lock()
	c := t.cotp
	raw := t.raw
	t.mu.Unlock()
	if c != nil {
		return c.RemoteAddr()
	}
	if raw != nil {
		return raw.RemoteAddr()
	}
	return nil
}

// TLSConnectionState returns the TLS connection state if the underlying
// connection uses TLS. Returns nil if the transport is not TLS-secured.
func (t *cotpTransport) TLSConnectionState() *tls.ConnectionState {
	t.mu.Lock()
	raw := t.raw
	t.mu.Unlock()
	if tc, ok := raw.(*tls.Conn); ok {
		cs := tc.ConnectionState()
		return &cs
	}
	return nil
}
