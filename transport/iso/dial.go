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

// DialTCP establishes a TCP connection to addr, performs the TP0 handshake
// via go-cotp, and returns a [mms.Transport] ready for MMS communication.
//
// addr is a host:port string (e.g. "10.0.0.1:102"). The standard MMS port
// is 102 for plaintext.
//
// The COTP CR carries a calling and a called T-selector (default 00 01
// each, see [WithCallingTSelector] and [WithCalledTSelector]) and a TPDU
// size (default [DefaultMaxTPDULength], see [WithMaxTPDULength]), as
// IEC 61850 IEDs expect: some refuse a CR without them with a Disconnect
// Request, reason 3 (address unknown).
//
// With [WithRetry], failed attempts are retried with backoff.
func DialTCP(ctx context.Context, addr string, opts ...Option) (mms.Transport, error) {
	o := applyOptions(opts)
	var tr mms.Transport
	err := mms.Retry(ctx, o.retryPolicy(addr), func(ctx context.Context) error {
		var err error
		tr, err = dialOnce(ctx, addr, o)
		return err
	})
	if err != nil {
		return nil, err
	}
	return tr, nil
}

// retryPolicy returns the configured policy with logging of retried
// attempts added.
func (o Options) retryPolicy(addr string) mms.RetryPolicy {
	p := o.retry
	if o.logger == nil {
		return p
	}
	next := p.OnRetry
	logger := o.logger
	p.OnRetry = func(attempt int, err error, delay time.Duration) {
		logger.Warn("iso: dial attempt failed, retrying",
			"addr", addr, "attempt", attempt, "delay", delay, "error", err)
		if next != nil {
			next(attempt, err, delay)
		}
	}
	return p
}

func dialOnce(ctx context.Context, addr string, o Options) (mms.Transport, error) {
	cfg, err := o.clientCOTPConfig()
	if err != nil {
		return nil, err
	}
	dial := o.dialer
	if dial == nil {
		var d net.Dialer
		dial = d.DialContext
	}
	tcpConn, err := dial(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("iso: tcp dial %s: %w", addr, err)
	}

	conn := tcpConn
	if o.tlsConfig != nil {
		tlsConn := tls.Client(tcpConn, o.tlsConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = tcpConn.Close()
			return nil, fmt.Errorf("iso: tls handshake with %s: %w", addr, err)
		}
		conn = tlsConn
	}

	cotpConn, err := cotp.Connect(ctx, conn, cfg)
	if err != nil {
		return nil, fmt.Errorf("iso: cotp connect %s: %w", addr, err)
	}
	return &cotpTransport{cotp: cotpConn, raw: conn}, nil
}

// Dial is a convenience function that establishes a TCP+TP0 connection and
// creates an MMS client in one call.
//
// It combines [DialTCP] with [mms.NewClient]. Use [WithClientDialOptions]
// to pass MMS-level configuration; T-selectors from
// [mms.DialOptions].Transport are used when [WithCallingTSelector] or
// [WithCalledTSelector] are not set, and the defaults when neither is.
// With [WithRetry], failed attempts (connect, handshakes and association)
// are retried with backoff.
func Dial(ctx context.Context, addr string, opts ...Option) (*mms.Client, error) {
	o := applyOptions(opts)

	if o.callingTSelector == nil && o.hasMmsDialOpts {
		o.callingTSelector = o.mmsDialOpts.Transport.LocalTSelector
	}
	if o.calledTSelector == nil && o.hasMmsDialOpts {
		o.calledTSelector = o.mmsDialOpts.Transport.RemoteTSelector
	}

	var client *mms.Client
	err := mms.Retry(ctx, o.retryPolicy(addr), func(ctx context.Context) error {
		conn, err := dialOnce(ctx, addr, o)
		if err != nil {
			return err
		}
		c, err := mms.NewClient(ctx, conn, o.mmsDialOpts)
		if err != nil {
			_ = conn.Close()
			return err
		}
		client = c
		return nil
	})
	if err != nil {
		return nil, err
	}
	return client, nil
}

// clientCOTPConfig returns the CR parameters of a client: the configured
// or default selectors and TPDU size.
func (o Options) clientCOTPConfig() (cotp.ClientConfig, error) {
	calling, err := clientTSelector(o.callingTSelector, "calling")
	if err != nil {
		return cotp.ClientConfig{}, err
	}
	called, err := clientTSelector(o.calledTSelector, "called")
	if err != nil {
		return cotp.ClientConfig{}, err
	}
	maxTPDU := o.maxTPDULength
	if maxTPDU == 0 {
		maxTPDU = DefaultMaxTPDULength
	}
	return cotp.ClientConfig{
		LocalSelector:  calling,
		RemoteSelector: called,
		MaxTPDULength:  maxTPDU,
	}, nil
}
