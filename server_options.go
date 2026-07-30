// SPDX-License-Identifier: MIT

package mms

import (
	"log/slog"
)

// ServerOptions configures the MMS server.
type ServerOptions struct {
	MMS ServerMMSOptions

	// Logger, when non-nil, enables structured MMS/server logging.
	// When nil (default), a discard handler is used. Does not configure
	// transport/iso logging — use iso.WithLogger on the listener
	// separately (see OBSERVABILITY.md).
	Logger *slog.Logger

	// Authenticate is called during association establishment to
	// accept or reject peers. If nil, all associations are accepted.
	//
	// See [Authenticator] for callback semantics and examples.
	Authenticate Authenticator

	// FileProvider, if set, enables MMS file services (FileOpen,
	// FileRead, FileClose, FileDelete, FileDirectory). When nil,
	// file service requests are rejected with service-unsupported.
	FileProvider FileProvider

	// JournalProvider, if set, enables MMS journal services
	// (ReadJournal). When nil, journal service requests are
	// rejected with service-unsupported.
	JournalProvider JournalProvider
}

// ServerMMSOptions configures MMS protocol negotiation parameters for
// the server side of the association.
type ServerMMSOptions struct {
	// MaxPDUSize is the maximum PDU size the server supports.
	// Zero means use the library default (65000).
	MaxPDUSize int

	// MaxOutstandingCalling is proposed during Initiate as the max
	// outstanding requests the server accepts from the client.
	// Zero means 5. Negotiated; not a separate pending-queue cap
	// (confirmed requests are handled serially per connection).
	MaxOutstandingCalling int

	// MaxOutstandingCalled is proposed as the max outstanding
	// requests the server can issue. Zero means 5. Negotiated only.
	MaxOutstandingCalled int

	// DataStructureNestingLevel is the maximum nesting depth.
	// Zero means 10.
	DataStructureNestingLevel int
}

func (o *ServerMMSOptions) withDefaults() ServerMMSOptions {
	out := *o
	if out.MaxPDUSize <= 0 {
		out.MaxPDUSize = 65000
	}
	if out.MaxOutstandingCalling <= 0 {
		out.MaxOutstandingCalling = 5
	}
	if out.MaxOutstandingCalled <= 0 {
		out.MaxOutstandingCalled = 5
	}
	if out.DataStructureNestingLevel <= 0 {
		out.DataStructureNestingLevel = 10
	}
	return out
}
