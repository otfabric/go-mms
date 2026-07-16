// SPDX-License-Identifier: MIT

package iso

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"github.com/otfabric/go-cotp"
	mms "github.com/otfabric/go-mms"
)

// acceptPollInterval is the deadline set on the TCP listener between
// context cancellation checks. Kept short so context cancellation is
// responsive, but not so short that it busy-spins.
const acceptPollInterval = 500 * time.Millisecond

// Listener accepts TCP connections, performs the TP0 handshake for each,
// and returns [mms.Transport] instances ready for MMS server use.
//
// Listener implements [mms.TransportListener].
type Listener struct {
	ln   net.Listener
	opts Options
}

// Listen creates a new [Listener] bound to the given TCP address.
// addr follows [net.Listen] conventions (e.g. ":102", "0.0.0.0:102").
func Listen(addr string, opts ...Option) (*Listener, error) {
	o := applyOptions(opts)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("iso: listen %s: %w", addr, err)
	}

	return &Listener{ln: ln, opts: o}, nil
}

// NewListener wraps an existing [net.Listener] with COTP handshake support.
// This is useful when the caller manages the TCP listener directly (e.g.
// for testing or custom bind logic).
func NewListener(ln net.Listener, opts ...Option) *Listener {
	return &Listener{ln: ln, opts: applyOptions(opts)}
}

// Accept waits for the next TCP connection, performs the TP0 handshake
// via go-cotp Accept, and returns a transport ready for MMS.
//
// Accept blocks until a connection is available, the listener is closed,
// or the context is cancelled. Context cancellation does NOT close the
// listener — the caller may retry Accept with a new context.
//
// If a client connects but the COTP handshake fails, the individual
// connection is closed (by go-cotp) and Accept retries automatically.
// Only listener-level or context-level errors cause Accept to return.
func (l *Listener) Accept(ctx context.Context) (mms.Transport, error) {
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if dl, ok := l.ln.(*net.TCPListener); ok {
			_ = dl.SetDeadline(time.Now().Add(acceptPollInterval))
		}

		conn, err := l.ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return nil, fmt.Errorf("iso: accept: %w", err)
		}

		if l.opts.tlsConfig != nil {
			tlsConn := tls.Server(conn, l.opts.tlsConfig)
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				if l.opts.logger != nil {
					l.opts.logger.Warn("tls handshake failed, closing connection",
						"remote", conn.RemoteAddr(), "error", err)
				}
				_ = conn.Close()
				continue
			}
			conn = tlsConn
		}

		cotpConn, err := cotp.Accept(ctx, conn, cotp.ServerConfig{
			LocalSelector: l.opts.calledTSelector,
			// MaxTPDULength 0 → go-cotp default (RFC1006 omit/default path).
		})
		if err != nil {
			if l.opts.logger != nil {
				l.opts.logger.Warn("cotp handshake failed, closing connection",
					"remote", conn.RemoteAddr(), "error", err)
			}
			// go-cotp already closed conn on failure.
			continue
		}
		return wrapCOTP(cotpConn, conn), nil
	}
}

// Close closes the underlying TCP listener. Any blocked [Accept] call will
// return an error on the next poll iteration.
func (l *Listener) Close() error {
	return l.ln.Close()
}

// Addr returns the listener's network address.
func (l *Listener) Addr() net.Addr {
	return l.ln.Addr()
}
