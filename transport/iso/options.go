// SPDX-License-Identifier: MIT

package iso

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"time"

	mms "github.com/otfabric/go-mms"
)

// Options configures the ISO transport layer for both client and server.
type Options struct {
	callingTSelector []byte
	calledTSelector  []byte
	tlsConfig        *tls.Config
	mmsDialOpts      mms.DialOptions
	hasMmsDialOpts   bool
	logger           *slog.Logger
	retry            mms.RetryPolicy
	dialer           func(ctx context.Context, network, addr string) (net.Conn, error)
	handshakeTimeout time.Duration
}

// Option configures an ISO transport connection.
type Option func(*Options)

// WithCallingTSelector sets the calling TSAP selector (COTP CR parameter 0xC1).
// This typically identifies the local endpoint.
func WithCallingTSelector(sel []byte) Option {
	return func(o *Options) {
		o.callingTSelector = append([]byte(nil), sel...)
	}
}

// WithCalledTSelector sets the called TSAP selector (COTP CR parameter 0xC2).
// This typically identifies the remote endpoint.
func WithCalledTSelector(sel []byte) Option {
	return func(o *Options) {
		o.calledTSelector = append([]byte(nil), sel...)
	}
}

// WithClientDialOptions sets the MMS dial options used by the convenience
// [Dial] function. These options are passed to [mms.NewClient] after the
// transport is established. Ignored by [DialTCP] and [Listen].
func WithClientDialOptions(opts mms.DialOptions) Option {
	return func(o *Options) {
		o.mmsDialOpts = opts
		o.hasMmsDialOpts = true
	}
}

// WithTLSConfig enables TLS on the transport connection.
//
// For clients ([DialTCP], [Dial]): the config is used to perform a TLS
// handshake after the TCP connection is established, before the COTP
// handshake. Set ServerName to the expected server hostname for
// certificate verification.
//
// For servers ([Listen], [NewListener]): the config is used to accept
// TLS connections. Include server certificates and optionally set
// ClientAuth to request client certificates.
//
// The standard port for MMS over TLS is 3782 (vs 102 for plaintext).
func WithTLSConfig(cfg *tls.Config) Option {
	return func(o *Options) {
		o.tlsConfig = cfg
	}
}

// WithLogger sets a structured logger for the ISO transport layer.
// Used by [Listener] to log per-connection handshake failures and by
// [DialTCP] and [Dial] to log failed attempts retried under [WithRetry].
func WithLogger(l *slog.Logger) Option {
	return func(o *Options) {
		o.logger = l
	}
}

func applyOptions(opts []Option) Options {
	var o Options
	for _, fn := range opts {
		fn(&o)
	}
	return o
}

// WithRetry makes [DialTCP] and [Dial] retry failed attempts according to
// p (exponential backoff with jitter, honouring the context). For [Dial]
// one attempt covers TCP connect, TLS and COTP handshakes and the MMS
// association; errors that cannot succeed on retry, such as a rejected
// association, are returned at once (see [mms.IsRetryable]). The default
// is a single attempt. Ignored by [Listen].
func WithRetry(p mms.RetryPolicy) Option {
	return func(o *Options) {
		o.retry = p
	}
}

// WithDialer replaces the function [DialTCP] and [Dial] use to open the
// TCP connection (by default a zero [net.Dialer]'s DialContext). Use it
// for proxies, custom socket options, or fault injection in tests. The
// returned connection is owned by the transport. Ignored by [Listen].
func WithDialer(dial func(ctx context.Context, network, addr string) (net.Conn, error)) Option {
	return func(o *Options) {
		o.dialer = dial
	}
}

// WithHandshakeTimeout bounds the TLS and COTP handshakes of each
// connection accepted by a [Listener]; a peer that does not complete them
// in time is disconnected. Zero means 10s; a negative value disables the
// limit. Handshakes run concurrently, so a slow peer never delays other
// connections. For clients, bound the handshake with the context passed
// to [DialTCP] or [Dial].
func WithHandshakeTimeout(d time.Duration) Option {
	return func(o *Options) {
		o.handshakeTimeout = d
	}
}
