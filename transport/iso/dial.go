// SPDX-License-Identifier: MIT

package iso

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync/atomic"

	"github.com/otfabric/go-cotp"
	mms "github.com/otfabric/go-mms"
)

// clientSourceRef allocates unique per-connection COTP source references.
// Starts at 1 and wraps around, skipping 0.
var clientSourceRef atomic.Uint32

func nextClientSourceRef() uint16 {
	v := clientSourceRef.Add(1)
	if v == 0 {
		v = clientSourceRef.Add(1)
	}
	return uint16(v)
}

// DialTCP establishes a TCP connection to addr, performs the TPKT/COTP
// handshake, and returns a [mms.Transport] ready for MMS communication.
//
// addr is a host:port string (e.g. "10.0.0.1:102"). The standard MMS port
// is 102 for plaintext.
//
// Use [WithCallingTSelector] and [WithCalledTSelector] to configure TSAP
// selectors. If not set, selectors are omitted from the COTP CR.
func DialTCP(ctx context.Context, addr string, opts ...Option) (mms.Transport, error) {
	o := applyOptions(opts)

	var d net.Dialer
	tcpConn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("iso: tcp dial %s: %w", addr, err)
	}

	conn := net.Conn(tcpConn)
	if o.tlsConfig != nil {
		tlsConn := tls.Client(tcpConn, o.tlsConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = tcpConn.Close()
			return nil, fmt.Errorf("iso: tls handshake with %s: %w", addr, err)
		}
		conn = tlsConn
	}

	t := newCOTPTransport(conn)
	if err := clientCOTPHandshake(t, o); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("iso: cotp handshake with %s: %w", addr, err)
	}

	return t, nil
}

// Dial is a convenience function that establishes a TCP+TPKT+COTP
// connection and creates an MMS client in one call.
//
// It combines [DialTCP] with [mms.NewClient]. Use [WithClientDialOptions]
// to pass MMS-level configuration; TSAP selectors from [mms.DialOptions]
// are used automatically if ISO-level selectors are not set explicitly.
func Dial(ctx context.Context, addr string, opts ...Option) (*mms.Client, error) {
	o := applyOptions(opts)

	if o.callingTSelector == nil && o.hasMmsDialOpts {
		o.callingTSelector = o.mmsDialOpts.Transport.LocalTSelector
	}
	if o.calledTSelector == nil && o.hasMmsDialOpts {
		o.calledTSelector = o.mmsDialOpts.Transport.RemoteTSelector
	}

	dialOpts := []Option{
		WithCallingTSelector(o.callingTSelector),
		WithCalledTSelector(o.calledTSelector),
	}
	if o.tlsConfig != nil {
		dialOpts = append(dialOpts, WithTLSConfig(o.tlsConfig))
	}

	conn, err := DialTCP(ctx, addr, dialOpts...)
	if err != nil {
		return nil, err
	}

	mmsOpts := o.mmsDialOpts
	client, err := mms.NewClient(ctx, conn, mmsOpts)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return client, nil
}

// clientCOTPHandshake sends a COTP Connection Request (CR) and waits for
// a Connection Confirm (CC) from the server. Validates that the CC
// destination reference matches the CR source reference and that the
// negotiated class is 0.
func clientCOTPHandshake(t *cotpTransport, o Options) error {
	srcRef := nextClientSourceRef()

	cr := &cotp.CR{
		SourceRef:       srcRef,
		ClassOption:     0, // class 0
		CallingSelector: o.callingTSelector,
		CalledSelector:  o.calledTSelector,
	}

	crBytes, err := cr.MarshalBinary()
	if err != nil {
		return fmt.Errorf("marshal CR: %w", err)
	}

	if err := t.sendTPDU(crBytes); err != nil {
		return fmt.Errorf("send CR: %w", err)
	}

	ccFrame, err := t.readTPDU()
	if err != nil {
		return fmt.Errorf("read CC: %w", err)
	}

	decoded, err := cotp.Decode(ccFrame)
	if err != nil {
		return fmt.Errorf("decode CC: %w", err)
	}

	if decoded.CC == nil {
		if decoded.DR != nil {
			return fmt.Errorf("connection refused: DR reason=%d", decoded.DR.Reason)
		}
		return fmt.Errorf("expected CC, got COTP %s", decoded.Type)
	}

	cc := decoded.CC

	if cc.DestinationRef != srcRef {
		return fmt.Errorf("CC destination ref %d does not match CR source ref %d", cc.DestinationRef, srcRef)
	}

	if cc.ClassOption&0xF0 != 0 {
		return fmt.Errorf("CC negotiated unsupported class %d (expected 0)", cc.ClassOption>>4)
	}

	if o.calledTSelector != nil && cc.CalledSelector != nil {
		if !bytes.Equal(o.calledTSelector, cc.CalledSelector) {
			return fmt.Errorf("CC called TSAP selector mismatch: got %x, want %x", cc.CalledSelector, o.calledTSelector)
		}
	}

	return nil
}
