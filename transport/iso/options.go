// SPDX-License-Identifier: MIT

package iso

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"time"

	mms "github.com/otfabric/go-mms"
)

// Options configures the ISO transport layer for both client and server.
type Options struct {
	callingTSelector []byte
	calledTSelector  []byte
	maxTPDULength    int
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

// Default client transport parameters, the values libiec61850 and most
// IEC 61850 clients send and IEDs expect (IEC 61850-8-1 mandates none; it
// limits a T-selector to 4 octets, §6.2.3.2.2).
const (
	// DefaultMaxTPDULength is the TPDU size a client proposes in the CR
	// (parameter 0xC0 = 0x0A, 1024 octets).
	DefaultMaxTPDULength = 1024
	// MaxTSelectorLength is the longest T-selector a client sends
	// (IEC 61850-8-1 §6.2.3.2.2).
	MaxTSelectorLength = 4
)

// defaultTSelector is the calling and called T-selector a client sends
// when none is configured: 00 01.
var defaultTSelector = [2]byte{0x00, 0x01}

// WithCallingTSelector sets the calling TSAP selector (COTP CR parameter
// 0xC1), at most [MaxTSelectorLength] octets.
//
// A client sends 00 01 when no calling selector is configured (here or in
// [mms.TransportOptions]); a non-nil empty sel omits the parameter.
func WithCallingTSelector(sel []byte) Option {
	return func(o *Options) {
		o.callingTSelector = cloneSelector(sel)
	}
}

// WithCalledTSelector sets the called TSAP selector (COTP CR parameter
// 0xC2), at most [MaxTSelectorLength] octets for a client.
//
// A client sends 00 01 when no called selector is configured (here or in
// [mms.TransportOptions]); a non-nil empty sel omits the parameter.
//
// On a [Listener] it makes the selector strict: a CR whose called selector
// is absent or differs byte for byte is refused with a Disconnect Request
// (reason 3, address unknown). Without it a Listener accepts any called
// selector, as other IEC 61850 servers do.
func WithCalledTSelector(sel []byte) Option {
	return func(o *Options) {
		o.calledTSelector = cloneSelector(sel)
	}
}

// WithMaxTPDULength sets the maximum TPDU size in octets. A client
// proposes it in the CR (default [DefaultMaxTPDULength]); 65531 omits the
// 0xC0 parameter, the RFC 1006 default that earlier go-mms versions sent.
// On a [Listener] it caps the size accepted from a CR (default: whatever
// the peer proposes, up to 65531). Valid values are 128..65531.
func WithMaxTPDULength(n int) Option {
	return func(o *Options) {
		o.maxTPDULength = n
	}
}

// cloneSelector copies sel, keeping nil (not configured) apart from a
// non-nil empty selector (configured to be omitted).
func cloneSelector(sel []byte) []byte {
	if sel == nil {
		return nil
	}
	return append(make([]byte, 0, len(sel)), sel...)
}

// clientTSelector returns the selector a client sends: sel when
// configured, an omitted parameter for a non-nil empty sel, otherwise the
// default 00 01.
func clientTSelector(sel []byte, what string) ([]byte, error) {
	switch {
	case sel == nil:
		return defaultTSelector[:], nil
	case len(sel) == 0:
		return nil, nil
	case len(sel) > MaxTSelectorLength:
		return nil, fmt.Errorf("iso: %s T-selector of %d octets, IEC 61850-8-1 allows at most %d", what, len(sel), MaxTSelectorLength)
	}
	return sel, nil
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
// for a destination allowlist checked against the resolved IP (for
// example a [net.Dialer] with a Control hook), a SOCKS or HTTP proxy, a
// jump host, a source address, custom socket options, or an in-memory
// connection and fault injection in tests. A [*net.Dialer]'s or a proxy's
// DialContext fits as a method value. With [WithTLS], the TLS handshake
// runs over the connection it returns.
//
// It is called once per attempt (see [WithRetry]) with network "tcp" and
// the address given to [DialTCP] or [Dial]. The context carries the
// caller's deadline and cancellation; a custom dialer must honour it.
// Like [net.Dialer], it returns a non-nil connection exactly when the
// error is nil: a connection returned together with an error is closed,
// and a nil connection with a nil error fails the dial.
// Its error is wrapped, so [errors.Is] reaches it, for example an
// allowlist's refusal. The returned connection is owned and closed by
// the transport. Nil keeps the default. Ignored by [Listen].
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
