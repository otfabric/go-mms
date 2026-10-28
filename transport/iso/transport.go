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
	"time"

	"github.com/otfabric/go-cotp"
)

// cotpTransport implements mms.Transport over an established *cotp.Conn.
// Session SPDUs are transferred as TSDUs; TPKT/COTP framing is owned by go-cotp.
//
// MMS uses the connection full duplex: a reader goroutine waits in Receive
// while requests are sent. go-cotp (v1.1.0+) arms read and write deadlines
// per direction, so a context ending on one side never disturbs the other.
type cotpTransport struct {
	mu   sync.Mutex
	cotp *cotp.Conn
	raw  net.Conn // same conn passed to Connect/Accept; used for TLSConnectionState
}

// ctxIOError makes an I/O error that ctx ended match ctx's error. go-cotp
// already returns the context error in most cases; the connection
// deadline taken from ctx can also fire just before ctx's own timer, so a
// passed deadline counts as context.DeadlineExceeded.
func ctxIOError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if cerr := ctxDoneErr(ctx); cerr != nil && !errors.Is(err, cerr) {
		return fmt.Errorf("%w: %w", cerr, err)
	}
	return err
}

// ctxDoneErr returns ctx.Err(), or context.DeadlineExceeded once ctx's
// deadline has passed even if its timer has not fired yet.
func ctxDoneErr(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if dl, ok := ctx.Deadline(); ok && !time.Now().Before(dl) {
		return context.DeadlineExceeded
	}
	return nil
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
	// abandoning a TSDU half-written would end the association for every
	// other request (go-cotp aborts the connection). A context deadline
	// still bounds the write (a slow or stalled peer).
	wctx := context.WithoutCancel(ctx)
	if dl, ok := ctx.Deadline(); ok {
		var cancel context.CancelFunc
		wctx, cancel = context.WithDeadline(wctx, dl)
		defer cancel()
	}
	return ctxIOError(ctx, c.WriteTSDU(wctx, data))
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
	// A context that ends before any octet of the TSDU arrived leaves the
	// connection usable; one that ends mid-TSDU aborts it (go-cotp).
	data, err := c.ReadTSDU(ctx)
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
