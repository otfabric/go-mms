// SPDX-License-Identifier: MIT

package iso

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/otfabric/go-cotp"
	mms "github.com/otfabric/go-mms"
)

// defaultHandshakeTimeout bounds TLS+COTP handshakes of accepted
// connections when WithHandshakeTimeout is not set.
const defaultHandshakeTimeout = 10 * time.Second

// maxPendingHandshakes limits connections that are handshaking or waiting
// to be returned by Accept. Further connections wait in the kernel
// backlog.
const maxPendingHandshakes = 64

// Listener accepts TCP connections, performs the TP0 handshake for each,
// and returns [mms.Transport] instances ready for MMS server use.
//
// Handshakes run concurrently in the background, bounded by
// [WithHandshakeTimeout], so a peer that connects and stays silent never
// blocks other connections.
//
// Listener implements [mms.TransportListener].
type Listener struct {
	ln   net.Listener
	opts Options

	start     sync.Once
	ready     chan mms.Transport // handshaken transports, handed to Accept
	pending   chan struct{}      // semaphore of in-flight handshakes
	done      chan struct{}      // closed when the accept loop stops
	err       error              // why the accept loop stopped; set before done is closed
	closeOnce sync.Once
	closed    chan struct{}
}

// Listen creates a new [Listener] bound to the given TCP address.
// addr follows [net.Listen] conventions (e.g. ":102", "0.0.0.0:102").
func Listen(addr string, opts ...Option) (*Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("iso: listen %s: %w", addr, err)
	}
	return NewListener(ln, opts...), nil
}

// NewListener wraps an existing [net.Listener] with COTP handshake support.
// This is useful when the caller manages the TCP listener directly (e.g.
// for testing, custom bind logic, or fault injection).
func NewListener(ln net.Listener, opts ...Option) *Listener {
	return &Listener{
		ln:      ln,
		opts:    applyOptions(opts),
		ready:   make(chan mms.Transport),
		pending: make(chan struct{}, maxPendingHandshakes),
		done:    make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

// Accept returns the next connection that completed the TCP, optional
// TLS, and TP0 handshakes.
//
// Accept blocks until a connection is available, the listener is closed,
// or the context is cancelled. Context cancellation does NOT close the
// listener — the caller may retry Accept with a new context.
//
// Connections whose handshake fails or times out are closed and logged
// (see [WithLogger]); they never cause Accept to return. Only
// listener-level or context-level errors do.
func (l *Listener) Accept(ctx context.Context) (mms.Transport, error) {
	l.start.Do(func() { go l.acceptLoop() })
	select {
	case tr := <-l.ready:
		return tr, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-l.done:
		return nil, l.err
	}
}

func (l *Listener) acceptLoop() {
	var tempDelay time.Duration
	for {
		conn, err := l.ln.Accept()
		if err != nil {
			if isTemporary(err) {
				// Back off on resource exhaustion (EMFILE and the like).
				tempDelay = min(max(2*tempDelay, 5*time.Millisecond), time.Second)
				select {
				case <-time.After(tempDelay):
					continue
				case <-l.closed:
				}
			}
			l.err = fmt.Errorf("iso: accept: %w", err)
			close(l.done)
			return
		}
		tempDelay = 0
		select {
		case l.pending <- struct{}{}:
		case <-l.closed:
			_ = conn.Close()
			l.err = fmt.Errorf("iso: accept: %w", net.ErrClosed)
			close(l.done)
			return
		}
		go l.handshake(conn)
	}
}

func isTemporary(err error) bool {
	type temporary interface{ Temporary() bool }
	var t temporary
	return errors.As(err, &t) && t.Temporary()
}

// handshake runs the TLS and COTP handshakes for conn and hands the
// transport to Accept.
func (l *Listener) handshake(conn net.Conn) {
	defer func() { <-l.pending }()

	timeout := l.opts.handshakeTimeout
	if timeout == 0 {
		timeout = defaultHandshakeTimeout
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
		_ = conn.SetDeadline(time.Now().Add(timeout))
	}
	stop := context.AfterFunc(ctx, func() {
		// Unblock handshake reads that do not watch ctx.
		_ = conn.SetDeadline(time.Unix(1, 0))
	})
	defer stop()
	go func() {
		select {
		case <-l.closed:
			cancel()
		case <-ctx.Done():
		}
	}()

	tr, err := l.handshakeConn(ctx, conn)
	if err != nil {
		return
	}
	if !stop() {
		// The deadline fired after the handshake completed.
		_ = tr.Close()
		return
	}
	_ = conn.SetDeadline(time.Time{})

	select {
	case l.ready <- tr:
	case <-l.closed:
		_ = tr.Close()
	}
}

func (l *Listener) handshakeConn(ctx context.Context, conn net.Conn) (mms.Transport, error) {
	if l.opts.tlsConfig != nil {
		tlsConn := tls.Server(conn, l.opts.tlsConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			l.logWarn("iso: tls handshake failed, closing connection", conn, err)
			_ = conn.Close()
			return nil, err
		}
		conn = tlsConn
	}

	cotpConn, err := cotp.Accept(ctx, conn, cotp.ServerConfig{
		LocalSelector: l.opts.calledTSelector,
		// MaxTPDULength 0 → go-cotp default (RFC1006 omit/default path).
	})
	if err != nil {
		l.logWarn("iso: cotp handshake failed, closing connection", conn, err)
		_ = conn.Close() // go-cotp normally closed it already
		return nil, err
	}
	return &cotpTransport{cotp: cotpConn, raw: conn}, nil
}

func (l *Listener) logWarn(msg string, conn net.Conn, err error) {
	if l.opts.logger != nil {
		l.opts.logger.Warn(msg, "remote_addr", conn.RemoteAddr().String(), "error", err)
	}
}

// Close closes the underlying TCP listener and any connection still
// handshaking or waiting for Accept. A blocked [Listener.Accept] returns
// an error. Close is idempotent.
func (l *Listener) Close() error {
	err := net.ErrClosed
	l.closeOnce.Do(func() {
		close(l.closed)
		err = l.ln.Close()
	})
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

// Addr returns the listener's network address.
func (l *Listener) Addr() net.Addr {
	return l.ln.Addr()
}
