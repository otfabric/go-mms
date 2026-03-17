package mms

import "log/slog"

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

	// PDUHook, when non-nil, is called for every PDU sent or received.
	// direction is "send" or "recv". raw is the raw PDU bytes. This hook
	// is intended for packet capture and replay tooling.
	PDUHook func(direction string, raw []byte)
}

// TransportOptions configures the TPKT/COTP transport layer.
type TransportOptions struct {
	LocalTSelector  []byte
	RemoteTSelector []byte
}

// ISOOptions configures the ISO upper layers (session, presentation, ACSE).
type ISOOptions struct {
	LocalAPTitle    APTitle
	RemoteAPTitle   APTitle
	LocalAEQualifer int
	RemoteAEQualifer int
	LocalPSelector  []byte
	RemotePSelector []byte
	LocalSSelector  []byte
	RemoteSSelector []byte
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
