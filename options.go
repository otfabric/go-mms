// SPDX-License-Identifier: MIT

package mms

import (
	"log/slog"
	"time"
)

// DialOptions configures the MMS client connection.
//
// Options are grouped by protocol layer to avoid a flat, cross-layer
// catch-all struct. The exact grouping may evolve, but separation by
// responsibility is maintained.
type DialOptions struct {
	Transport TransportOptions
	ISO       ISOOptions
	MMS       MMSOptions

	// Logger, when non-nil, enables structured MMS logging. When nil
	// (default), a discard handler is used (zero overhead). Lifecycle
	// events at Info; request/response summaries at Debug.
	// Does not configure transport/iso logging — use iso.WithLogger
	// separately (see OBSERVABILITY.md).
	Logger *slog.Logger

	// RawHook, when non-nil, is called for every raw ISO upper-layer
	// payload sent to or received from the transport. direction is
	// [RawHookSend] ("send") or [RawHookRecv] ("recv"). raw contains the
	// session SPDU bytes (which embed presentation, ACSE, and MMS data).
	//
	// This hook operates at the COTP user-data level, not at the MMS PDU
	// level specifically. It is intended for packet capture and replay
	// tooling.
	RawHook func(direction string, raw []byte)

	// Metrics, when non-nil, receives request and connection events (see
	// [Metrics]). Nil means no metrics.
	Metrics Metrics

	// RequestTimeout, when positive, bounds every confirmed request whose
	// context has no earlier deadline. A request that times out fails with
	// an error matching both [ErrInvokeTimeout] and
	// [context.DeadlineExceeded]. Zero means no default timeout: requests
	// wait until their context is done or the connection is lost.
	RequestTimeout time.Duration

	// AllowedServices, when non-nil, lists the confirmed services the
	// client may send. A request for any other service is refused before
	// it reaches the transport: nothing is sent, and the call returns an
	// error matching [ErrServiceNotAllowed] (the [Metrics] request event
	// carries the same error). Nil allows every service; an empty non-nil
	// slice refuses every confirmed request. Association control
	// (Initiate, Conclude, Abort) is not a confirmed service and is never
	// refused. Use it to make a client read-only, for example
	// {ServiceIdentify, ServiceStatus, ServiceGetNameList, ServiceRead,
	// ServiceGetVariableAccessAttributes, ServiceGetNamedVariableListAttributes}.
	AllowedServices []Service

	// ReleaseTimeout bounds the A-RELEASE that [Client.Close] performs
	// after the Conclude-Response: Session FINISH carrying an ACSE RLRQ,
	// answered by DISCONNECT carrying an RLRE (ISO 9506-2 Annex A.1.2.1).
	// Zero means 2s. A server that closes the transport or does not
	// answer in time still ends the association without an error. A
	// negative value skips the A-RELEASE: Close only concludes and closes
	// the transport (the behaviour before v1.8.0).
	ReleaseTimeout time.Duration
}

// Directions passed to [DialOptions.RawHook].
const (
	RawHookSend = "send"
	RawHookRecv = "recv"
)

// TransportOptions configures the TPKT/COTP transport layer. It is read
// by transport/iso.Dial.
//
// Each selector left nil is sent as 00 01; a non-nil empty slice omits it
// from the COTP CR. See transport/iso.WithCallingTSelector.
type TransportOptions struct {
	LocalTSelector  []byte // calling T-selector (0xC1)
	RemoteTSelector []byte // called T-selector (0xC2)
}

// ISOOptions configures the ISO upper layers (session, presentation, ACSE).
//
// A client sends the addressing that libiec61850 and most IEC 61850
// clients send, and that IEDs are configured for, unless set otherwise
// (IEC 61850-8-1 Table 132 makes these SCL address parameters but
// mandates no value):
//
//	S-selector      00 01 (calling and called)
//	P-selector      00 00 00 01 (calling and called)
//	AP-title        1.1.1.999 (calling), 1.1.1.999.1 (called)
//	AE-qualifier    12 (calling and called)
//
// A nil selector or AP-title takes the default; a non-nil empty one is
// omitted from the request. An AE-qualifier is sent only with its
// AP-title: it is 12 when the AP-title is the default and the qualifier is
// zero, and the configured value otherwise.
type ISOOptions struct {
	LocalAPTitle      APTitle
	RemoteAPTitle     APTitle
	LocalAEQualifier  int
	RemoteAEQualifier int
	LocalPSelector    []byte
	RemotePSelector   []byte
	LocalSSelector    []byte
	RemoteSSelector   []byte
	// Selectors are at most 16 octets (ISO 8327-1 PI 51/52; IEC 61850-8-1
	// SCL allows 8); a longer one fails NewClient with
	// [ErrInvalidArgument] before anything is sent.

	// Password, when non-nil, includes ACSE password authentication
	// in the association request (AARQ). The server must be configured
	// to accept password-based authentication.
	//
	// SECURITY: The password is embedded in the AARQ PDU and transmitted
	// as part of the ISO upper-layer association request. Without TLS,
	// the password travels in the clear over the wire — this is
	// plaintext-equivalent and offers no confidentiality against network
	// observers. ACSE password authentication should normally be
	// combined with TLS transport security ([transport/iso.WithTLSConfig]).
	// The library does not enforce this combination; it is the caller's
	// responsibility to ensure adequate transport security.
	//
	// The slice is copied internally; the caller may reuse or modify
	// the original after passing it.
	Password []byte
}

// MMSOptions configures the MMS protocol negotiation parameters.
type MMSOptions struct {
	// MaxPDUSize is the maximum PDU size in bytes proposed during MMS
	// initiation. Zero means use the library default (65000).
	MaxPDUSize int

	// MaxOutstandingCalling is the maximum number of outstanding
	// requests this client proposes during Initiate. Zero means use the
	// library default (5). The negotiated value is exposed via
	// Client.Negotiated() but is not enforced as a runtime pending-request
	// limit today.
	MaxOutstandingCalling int

	// MaxOutstandingCalled is the maximum number of outstanding
	// requests the peer is expected to support. Zero means use the
	// library default (5). Negotiated only; not a runtime client cap.
	MaxOutstandingCalled int

	// DataStructureNestingLevel is the proposed maximum nesting level
	// for structured data. Zero means use the library default (10).
	DataStructureNestingLevel int
}
