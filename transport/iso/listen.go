// SPDX-License-Identifier: MIT

package iso

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/otfabric/go-cotp"
	mms "github.com/otfabric/go-mms"
)

// acceptPollInterval is the deadline set on the TCP listener between
// context cancellation checks. Kept short so context cancellation is
// responsive, but not so short that it busy-spins.
const acceptPollInterval = 500 * time.Millisecond

// Listener accepts TCP connections, performs the COTP handshake for each,
// and returns [mms.Transport] instances ready for MMS server use.
//
// Listener implements [mms.TransportListener].
type Listener struct {
	ln        net.Listener
	opts      Options
	sourceRef atomic.Uint32
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

// Accept waits for the next TCP connection, performs the COTP handshake
// (receives CR, sends CC), and returns a transport ready for MMS.
//
// Accept blocks until a connection is available, the listener is closed,
// or the context is cancelled. Context cancellation does NOT close the
// listener — the caller may retry Accept with a new context.
//
// If a client connects but the COTP handshake fails (malformed CR, wrong
// class, selector mismatch, etc.), the individual connection is closed
// and Accept retries automatically. Only listener-level or context-level
// errors cause Accept to return an error.
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

		t := newCOTPTransport(conn)
		if err := serverCOTPHandshake(t, l.opts, l.nextSourceRef()); err != nil {
			if l.opts.logger != nil {
				l.opts.logger.Warn("cotp handshake failed, closing connection",
					"remote", conn.RemoteAddr(), "error", err)
			}
			_ = conn.Close()
			continue
		}
		return t, nil
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

func (l *Listener) nextSourceRef() uint16 {
	v := l.sourceRef.Add(1)
	if v == 0 {
		v = l.sourceRef.Add(1)
	}
	return uint16(v)
}

// serverCOTPHandshake reads a COTP Connection Request (CR) from the client
// and responds with a Connection Confirm (CC). Validates the CR contents
// and optionally checks that the called TSAP selector matches the
// listener configuration.
func serverCOTPHandshake(t *cotpTransport, o Options, sourceRef uint16) error {
	crFrame, err := t.readTPDU()
	if err != nil {
		return fmt.Errorf("read CR: %w", err)
	}

	decoded, err := cotp.Decode(crFrame)
	if err != nil {
		return fmt.Errorf("decode CR: %w", err)
	}

	if decoded.CR == nil {
		return fmt.Errorf("expected CR, got COTP %s", decoded.Type)
	}

	cr := decoded.CR

	if cr.ClassOption&0xF0 != 0 {
		sendDR(t, cr.SourceRef, 2) // reason 2 = negotiation failed
		return fmt.Errorf("unsupported COTP class %d (only class 0 is supported)", cr.ClassOption>>4)
	}

	if o.calledTSelector != nil && cr.CalledSelector != nil {
		if !bytes.Equal(o.calledTSelector, cr.CalledSelector) {
			sendDR(t, cr.SourceRef, 3) // reason 3 = address unknown
			return fmt.Errorf("called TSAP selector mismatch: got %x, want %x", cr.CalledSelector, o.calledTSelector)
		}
	}

	cc := &cotp.CC{
		DestinationRef:  cr.SourceRef,
		SourceRef:       sourceRef,
		ClassOption:     0,
		CallingSelector: cr.CallingSelector,
		CalledSelector:  cr.CalledSelector,
	}
	if o.calledTSelector != nil {
		cc.CalledSelector = o.calledTSelector
	}

	ccBytes, err := cc.MarshalBinary()
	if err != nil {
		return fmt.Errorf("marshal CC: %w", err)
	}

	if err := t.sendTPDU(ccBytes); err != nil {
		return fmt.Errorf("send CC: %w", err)
	}

	return nil
}

func sendDR(t *cotpTransport, destRef uint16, reason uint8) {
	dr := &cotp.DR{
		DestinationRef: destRef,
		SourceRef:      0,
		Reason:         reason,
	}
	if raw, err := dr.MarshalBinary(); err == nil {
		_ = t.sendTPDU(raw)
	}
}
