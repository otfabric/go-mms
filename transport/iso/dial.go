// SPDX-License-Identifier: MIT

package iso

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"

	"github.com/otfabric/go-cotp"
	mms "github.com/otfabric/go-mms"
)

// DialTCP establishes a TCP connection to addr, performs the TP0 handshake
// via go-cotp, and returns a [mms.Transport] ready for MMS communication.
//
// addr is a host:port string (e.g. "10.0.0.1:102"). The standard MMS port
// is 102 for plaintext.
//
// Use [WithCallingTSelector] and [WithCalledTSelector] to configure TSAP
// selectors. If not set, selectors are omitted from the COTP CR.
//
// TPDU size follows go-cotp RFC 1006 defaults (omitted/default size path).
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

	cotpConn, err := cotp.Connect(ctx, conn, cotp.ClientConfig{
		LocalSelector:  o.callingTSelector,
		RemoteSelector: o.calledTSelector,
		// MaxTPDULength 0 → go-cotp default (omit-size / RFC1006Compat path).
	})
	if err != nil {
		return nil, fmt.Errorf("iso: cotp connect %s: %w", addr, err)
	}
	return wrapCOTP(cotpConn, conn), nil
}

// Dial is a convenience function that establishes a TCP+TP0 connection and
// creates an MMS client in one call.
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
