// SPDX-License-Identifier: MIT

package mms

import (
	"log/slog"
	"math"
	"time"
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

	// Metrics, when non-nil, receives request and connection events (see
	// [Metrics]). Nil means no metrics.
	Metrics Metrics

	// AssociationTimeout bounds the time [Server.Serve] waits for a new
	// connection to complete the association (AARQ received,
	// Authenticate run, AARE sent). A peer that connects and stays silent
	// is disconnected after it. Zero means 30s; a negative value disables
	// the limit.
	AssociationTimeout time.Duration

	// ReleaseTimeout bounds how long Serve waits, after answering a
	// Conclude-Request, for the client's A-RELEASE (Session FINISH
	// carrying an ACSE RLRQ), which it answers with DISCONNECT + RLRE
	// (ISO 9506-2 Annex A.1.2.2). A client that closes the transport
	// instead ends the association normally. Zero means 2s; a negative
	// value does not wait (Serve returns right after the
	// Conclude-Response, the behaviour before v1.8.0).
	ReleaseTimeout time.Duration

	// WriteTimeout bounds every write of the server to an association:
	// confirmed responses, Rejects, ServiceErrors, association refusals,
	// aborts and the release response, and, together with the deadline
	// of ctx, the unsolicited sends ([ServerConn.SendInformationReport],
	// [ServerConn.SendUnsolicitedStatus], [Server.Broadcast]). A peer that
	// stops reading cannot hold the association's writer longer: a write
	// it cuts closes the transport and ends the association. Zero means
	// 10s; a negative value disables the limit.
	WriteTimeout time.Duration

	// RespondingAPTitle and RespondingAEQualifier, when the AP-title is
	// non-empty, are sent in the AARE as responding-AP-title [4] and
	// responding-AE-qualifier [5] (ISO 8650-1; ISO 9506-2 Annex A.1.1
	// maps them to the Responding Application Reference). Nil sends
	// neither.
	RespondingAPTitle     APTitle
	RespondingAEQualifier int

	// MaxPeerNVLs bounds the named variable lists that peers define
	// with VMD or domain scope (DefineNamedVariableList) in the
	// server's shared object model. Such lists outlive the defining
	// association; a DeleteNamedVariableList frees their slots. Lists
	// registered with [Server.RegisterNamedVariableList] do not count.
	// Zero means 4096; a negative value refuses every VMD- or
	// domain-scope definition by a peer.
	MaxPeerNVLs int

	// MaxAssociationNVLs bounds the association-scope named variable
	// lists one association may define. Zero means 256; a negative
	// value refuses every association-scope definition.
	MaxAssociationNVLs int

	// MaxNVLMembers bounds the members of one peer-defined named
	// variable list. Zero means 1000; a negative value refuses every
	// DefineNamedVariableList.
	MaxNVLMembers int

	// MaxOpenFiles bounds the files (FRSMs) one association may hold
	// open through FileOpen at a time; FileClose and the end of the
	// association free them. Zero means 32; a negative value refuses
	// every FileOpen. Only used when FileProvider is set.
	MaxOpenFiles int

	// MaxAssociations bounds the associations the server holds at a
	// time, counted from the AARQ (so including the one the
	// Authenticator is deciding on) until [Server.Serve] returns. The
	// AARQ of one more is answered with AARE rejected-transient and an
	// Initiate-ErrorPDU resource capability-unavailable (ISO 9506-2
	// clause 7.4.1), logged at Warn level ("mms: limit reached"), and its
	// Serve returns an error that matches [ErrLimitReached] and
	// [ErrAssociationFailed]. A connection that has not sent its AARQ is
	// not counted; [ServerOptions.AssociationTimeout] bounds it. Zero
	// means 100: libiec61850's MMS server accepts 100 connections by
	// default (its IEC 61850 server 5), and a device serves a few
	// clients (SCADA, gateway, engineering tool). A negative value
	// refuses every association. Before v1.11.0 there was no limit;
	// math.MaxInt keeps that behaviour.
	MaxAssociations int
}

// Defaults of the peer-definition and open-file limits in
// [ServerOptions].
const (
	defaultMaxPeerNVLs        = 4096
	defaultMaxAssociationNVLs = 256
	defaultMaxNVLMembers      = 1000
	defaultMaxOpenFiles       = 32
	defaultMaxAssociations    = 100
)

// serverLimits holds the resolved limits of [ServerOptions]; a value
// < 0 refuses the operation, otherwise it is the maximum.
type serverLimits struct {
	peerNVLs        int
	associationNVLs int
	nvlMembers      int
	openFiles       int
}

func limitOrDefault(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

func (o *ServerOptions) limits() serverLimits {
	return serverLimits{
		peerNVLs:        limitOrDefault(o.MaxPeerNVLs, defaultMaxPeerNVLs),
		associationNVLs: limitOrDefault(o.MaxAssociationNVLs, defaultMaxAssociationNVLs),
		nvlMembers:      limitOrDefault(o.MaxNVLMembers, defaultMaxNVLMembers),
		openFiles:       limitOrDefault(o.MaxOpenFiles, defaultMaxOpenFiles),
	}
}

// limitOf resolves a limit option for which a negative value means no
// bound: zero is def, a negative value is math.MaxInt.
func limitOf(v, def int) int {
	switch {
	case v == 0:
		return def
	case v < 0:
		return math.MaxInt
	default:
		return v
	}
}

// limitOf64 is limitOf for an int64 option (no bound: math.MaxInt64).
func limitOf64(v, def int64) int64 {
	switch {
	case v == 0:
		return def
	case v < 0:
		return math.MaxInt64
	default:
		return v
	}
}

// defaultMaxPDUSize is the MMS PDU size a client proposes when
// MMSOptions.MaxPDUSize is zero.
const defaultMaxPDUSize = 65000

// pduSizeLimiter is implemented by a Transport that carries MMS PDUs up to
// a size: those of transport/iso, whose TSDUs are bounded.
type pduSizeLimiter interface {
	MaxMMSPDU() int
}

// transportPDULimit returns n, or the largest MMS PDU t carries when t
// says and that is smaller, so that neither side proposes or supports a
// PDU size its transport does not carry.
func transportPDULimit(t Transport, n int) int {
	if l, ok := t.(pduSizeLimiter); ok {
		if m := l.MaxMMSPDU(); m > 0 && m < n {
			return m
		}
	}
	return n
}

// ServerMMSOptions configures MMS protocol negotiation parameters for
// the server side of the association.
type ServerMMSOptions struct {
	// MaxPDUSize is the maximum PDU size the server supports.
	// Zero means use the library default (65000).
	MaxPDUSize int

	// MaxOutstandingCalling is proposed during Initiate as the max
	// outstanding requests the server accepts from the client.
	// Zero means 5. The server serves confirmed requests one at a time,
	// so it never has more than one outstanding; a go-mms client keeps
	// to the negotiated value.
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
