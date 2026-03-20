package iso

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/otfabric/go-cotp"
	"github.com/otfabric/go-tpkt"
)

// cotpTransport implements mms.Transport over a TCP+TPKT+COTP connection.
// It sends and receives COTP DT TPDUs carrying session-layer data.
type cotpTransport struct {
	mu     sync.Mutex
	closed bool
	conn   net.Conn
	reader *tpkt.Reader
	writer *tpkt.Writer
}

func newCOTPTransport(conn net.Conn) *cotpTransport {
	return &cotpTransport{
		conn:   conn,
		reader: tpkt.NewReader(conn),
		writer: tpkt.NewWriter(conn),
	}
}

func (t *cotpTransport) isClosed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}

// Send encodes data as a COTP DT TPDU and writes it as a TPKT frame.
// If the context has a deadline, it is applied to the underlying TCP write.
// Returns [net.ErrClosed] if the transport has been closed.
func (t *cotpTransport) Send(ctx context.Context, data []byte) error {
	if t.isClosed() {
		return net.ErrClosed
	}

	dt := &cotp.DT{EOT: true, UserData: data}
	raw, err := dt.MarshalBinary()
	if err != nil {
		return fmt.Errorf("cotp marshal DT: %w", err)
	}

	if deadline, ok := ctx.Deadline(); ok {
		if err := t.conn.SetWriteDeadline(deadline); err != nil {
			return err
		}
		defer func() { _ = t.conn.SetWriteDeadline(time.Time{}) }()
	}

	if _, err := t.writer.WriteFrame(raw); err != nil {
		return fmt.Errorf("tpkt write: %w", err)
	}
	return nil
}

// Receive reads the next TPKT frame, decodes the COTP DT TPDU, and returns
// the user data (session-layer payload). Non-DT TPDUs (e.g. DR) cause an error.
// If the context has a deadline, it is applied to the underlying TCP read.
// Returns [net.ErrClosed] if the transport has been closed.
func (t *cotpTransport) Receive(ctx context.Context) ([]byte, error) {
	if t.isClosed() {
		return nil, net.ErrClosed
	}

	if deadline, ok := ctx.Deadline(); ok {
		if err := t.conn.SetReadDeadline(deadline); err != nil {
			return nil, err
		}
		defer func() { _ = t.conn.SetReadDeadline(time.Time{}) }()
	}

	frame, err := t.reader.ReadFrame()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("tpkt read: %w", err)
	}

	decoded, err := cotp.Decode(frame)
	if err != nil {
		return nil, fmt.Errorf("cotp decode: %w", err)
	}

	if decoded.DT == nil {
		return nil, fmt.Errorf("unexpected COTP TPDU type %s (expected DT)", decoded.Type)
	}

	cp := make([]byte, len(decoded.DT.UserData))
	copy(cp, decoded.DT.UserData)
	return cp, nil
}

// Close closes the underlying TCP connection. Subsequent Send or Receive
// calls will return an error. Close is safe to call concurrently and is
// idempotent.
func (t *cotpTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	return t.conn.Close()
}

// RemoteAddr returns the remote network address of the underlying connection.
func (t *cotpTransport) RemoteAddr() net.Addr {
	return t.conn.RemoteAddr()
}

// TLSConnectionState returns the TLS connection state if the underlying
// connection uses TLS. Returns nil if the transport is not TLS-secured.
//
// This can be used to extract peer certificates for authentication:
//
//	if state := t.TLSConnectionState(); state != nil {
//	    certs := state.PeerCertificates
//	}
func (t *cotpTransport) TLSConnectionState() *tls.ConnectionState {
	if tc, ok := t.conn.(*tls.Conn); ok {
		cs := tc.ConnectionState()
		return &cs
	}
	return nil
}

// sendTPDU marshals a COTP TPDU and writes it as a TPKT frame.
// Used during the COTP handshake (CR/CC exchange).
func (t *cotpTransport) sendTPDU(raw []byte) error {
	_, err := t.writer.WriteFrame(raw)
	return err
}

// readTPDU reads one TPKT frame and returns the raw COTP TPDU bytes.
// Used during the COTP handshake (CR/CC exchange).
func (t *cotpTransport) readTPDU() ([]byte, error) {
	return t.reader.ReadFrame()
}
