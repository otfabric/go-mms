// SPDX-License-Identifier: MIT

package iso

import (
	"crypto/tls"
	"log/slog"

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
// Used by [Listener] to log per-connection handshake failures.
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
