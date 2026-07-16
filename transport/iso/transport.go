// SPDX-License-Identifier: MIT

package iso

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"sync"

	"github.com/otfabric/go-cotp"
)

// cotpTransport implements mms.Transport over an established *cotp.Conn.
// Session SPDUs are transferred as TSDUs; TPKT/COTP framing is owned by go-cotp.
type cotpTransport struct {
	mu   sync.Mutex
	cotp *cotp.Conn
	raw  net.Conn // same conn passed to Connect/Accept; used for TLSConnectionState
}

func wrapCOTP(c *cotp.Conn, raw net.Conn) *cotpTransport {
	return &cotpTransport{cotp: c, raw: raw}
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
	return c.WriteTSDU(ctx, data)
}

// Receive reads one Session SPDU (reassembled TSDU).
func (t *cotpTransport) Receive(ctx context.Context) ([]byte, error) {
	t.mu.Lock()
	c := t.cotp
	t.mu.Unlock()
	if c == nil {
		return nil, net.ErrClosed
	}
	return c.ReadTSDU(ctx)
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
	if errors.Is(err, cotp.ErrClosed) {
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
