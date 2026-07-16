// SPDX-License-Identifier: MIT

package mms

import (
	"log/slog"
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

	// Logger, when non-nil, enables structured logging. When nil (default),
	// no logging is emitted. The logger is used for connection lifecycle
	// events at Info level and request/response summaries at Debug level.
	Logger *slog.Logger

	// RawHook, when non-nil, is called for every raw ISO upper-layer
	// payload sent to or received from the transport. direction is "send"
	// or "recv". raw contains the session SPDU bytes (which embed
	// presentation, ACSE, and MMS data).
	//
	// This hook operates at the COTP user-data level, not at the MMS PDU
	// level specifically. It is intended for packet capture and replay
	// tooling.
	RawHook func(direction string, raw []byte)
}

// TransportOptions configures the TPKT/COTP transport layer.
type TransportOptions struct {
	LocalTSelector  []byte
	RemoteTSelector []byte
}

// ISOOptions configures the ISO upper layers (session, presentation, ACSE).
type ISOOptions struct {
	LocalAPTitle      APTitle
	RemoteAPTitle     APTitle
	LocalAEQualifier  int
	RemoteAEQualifier int
	LocalPSelector    []byte
	RemotePSelector   []byte
	LocalSSelector    []byte
	RemoteSSelector   []byte

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
	// requests this client proposes. Zero means use the library default (5).
	MaxOutstandingCalling int

	// MaxOutstandingCalled is the maximum number of outstanding
	// requests the server is expected to support. Zero means use the
	// library default (5).
	MaxOutstandingCalled int

	// DataStructureNestingLevel is the proposed maximum nesting level
	// for structured data. Zero means use the library default (10).
	DataStructureNestingLevel int
}
