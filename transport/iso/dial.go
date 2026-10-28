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
// Use [WithCallingTSelector] and [WithCalledTSelector] to configure TSAP
// selectors. If not set, selectors are omitted from the COTP CR.
//
// TPDU size follows go-cotp RFC 1006 defaults (omitted/default size path).
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

	shim := newSplitDeadlineConn(conn)
	cotpConn, err := cotp.Connect(ctx, shim, cotp.ClientConfig{
		LocalSelector:  o.callingTSelector,
		RemoteSelector: o.calledTSelector,
		// MaxTPDULength 0 → go-cotp default (omit-size / RFC1006Compat path).
	})
	if err != nil {
		return nil, fmt.Errorf("iso: cotp connect %s: %w", addr, err)
	}
	return wrapCOTP(cotpConn, conn, shim), nil
}

// Dial is a convenience function that establishes a TCP+TP0 connection and
// creates an MMS client in one call.
//
// It combines [DialTCP] with [mms.NewClient]. Use [WithClientDialOptions]
// to pass MMS-level configuration; TSAP selectors from [mms.DialOptions]
// are used automatically if ISO-level selectors are not set explicitly.
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
