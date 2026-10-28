// SPDX-License-Identifier: MIT

package mms

import (
	"fmt"
	"time"

	"github.com/otfabric/go-mms/internal/pdu"
)

// Metrics receives operational events from a [Client] or [Server]. Set it
// with [DialOptions.Metrics] or [ServerOptions.Metrics]; when nil, events
// are dropped ([NopMetrics]).
//
// go-mms has no metrics dependency. Adapt this interface to the metrics
// library of your choice (Prometheus, OpenTelemetry, expvar, ...); see
// OBSERVABILITY.md for a Prometheus example.
//
// Methods are called synchronously on the goroutine that completed the
// operation, so implementations must be safe for concurrent use and should
// return quickly. Embed [NopMetrics] in your implementation so that methods
// added to this interface in later minor releases do not break your build.
type Metrics interface {
	// ObserveRequest is called once per confirmed service request: by the
	// client when the response (or error) is delivered to the caller, and
	// by the server when the response (or error PDU) has been sent.
	ObserveRequest(RequestEvent)

	// ObserveConnection is called when an association is established,
	// rejected or ends.
	ObserveConnection(ConnectionEvent)
}

// NopMetrics is a [Metrics] implementation that discards all events. Embed
// it in custom implementations to stay forward compatible.
type NopMetrics struct{}

// ObserveRequest implements [Metrics].
func (NopMetrics) ObserveRequest(RequestEvent) {}

// ObserveConnection implements [Metrics].
func (NopMetrics) ObserveConnection(ConnectionEvent) {}

// Role says which side of an association reported an event.
type Role int

// Roles.
const (
	RoleClient Role = iota
	RoleServer
)

// String returns "client" or "server".
func (r Role) String() string {
	switch r {
	case RoleClient:
		return "client"
	case RoleServer:
		return "server"
	default:
		return fmt.Sprintf("Role(%d)", int(r))
	}
}

// Service identifies an MMS confirmed service, for example in a
// [RequestEvent]. Its String method returns the ISO 9506 service name
// ("Read", "GetNameList", ...), suitable as a metrics label.
type Service int

// Confirmed services handled by go-mms.
const (
	ServiceStatus                         = Service(pdu.ServiceStatus)
	ServiceGetNameList                    = Service(pdu.ServiceGetNameList)
	ServiceIdentify                       = Service(pdu.ServiceIdentify)
	ServiceRead                           = Service(pdu.ServiceRead)
	ServiceWrite                          = Service(pdu.ServiceWrite)
	ServiceGetVariableAccessAttributes    = Service(pdu.ServiceGetVariableAccessAttrs)
	ServiceDefineNamedVariableList        = Service(pdu.ServiceDefineNamedVariableList)
	ServiceGetNamedVariableListAttributes = Service(pdu.ServiceGetNamedVariableListAttrs)
	ServiceDeleteNamedVariableList        = Service(pdu.ServiceDeleteNamedVariableList)
	ServiceFileOpen                       = Service(pdu.ServiceFileOpen)
	ServiceFileRead                       = Service(pdu.ServiceFileRead)
	ServiceFileClose                      = Service(pdu.ServiceFileClose)
	ServiceFileRename                     = Service(pdu.ServiceFileRename)
	ServiceFileDelete                     = Service(pdu.ServiceFileDelete)
	ServiceFileDirectory                  = Service(pdu.ServiceFileDirectory)
	ServiceObtainFile                     = Service(pdu.ServiceObtainFile)
	ServiceReadJournal                    = Service(pdu.ServiceReadJournal)
	ServiceUnknown                        = Service(pdu.ServiceUnknown)
)

// String returns the ISO 9506 name of the service.
func (s Service) String() string {
	if s < 0 || s > ServiceUnknown {
		return fmt.Sprintf("Service(%d)", int(s))
	}
	return pdu.ServiceKind(s).String()
}

// serviceFromTag maps a ConfirmedServiceRequest CHOICE tag to a Service.
func serviceFromTag(tag int) Service {
	return Service(pdu.ClassifyServiceTag(tag))
}

// RequestEvent describes one completed confirmed request.
type RequestEvent struct {
	Role     Role
	Service  Service
	InvokeID InvokeID
	// Duration is the time from sending the request to receiving the
	// response (client) or from receiving the request to sending the
	// response (server).
	Duration time.Duration
	// Err is nil on success. On the client it is the error returned to the
	// caller (inspect it with errors.Is / errors.As, e.g. [*ServiceError]).
	// On the server it is the handler error that was turned into a
	// ConfirmedErrorPDU, or the error that prevented sending the response.
	Err error
}

// ConnectionState is the association state reported in a
// [ConnectionEvent].
type ConnectionState int

// Connection states.
const (
	// ConnectionEstablished: the association was accepted.
	ConnectionEstablished ConnectionState = iota
	// ConnectionRejected: the association attempt failed or was rejected
	// (for example by the server's Authenticator). Err holds the reason.
	ConnectionRejected
	// ConnectionClosed: an established association ended. Err is nil after
	// an orderly Conclude or Release and holds the cause otherwise.
	ConnectionClosed
)

// String returns "established", "rejected" or "closed".
func (s ConnectionState) String() string {
	switch s {
	case ConnectionEstablished:
		return "established"
	case ConnectionRejected:
		return "rejected"
	case ConnectionClosed:
		return "closed"
	default:
		return fmt.Sprintf("ConnectionState(%d)", int(s))
	}
}

// ConnectionEvent describes an association state change.
type ConnectionEvent struct {
	Role  Role
	State ConnectionState
	// RemoteAddr is the peer address when the transport implements
	// [RemoteAddrTransport], otherwise "".
	RemoteAddr string
	Err        error
}

func metricsOrNop(m Metrics) Metrics {
	if m == nil {
		return NopMetrics{}
	}
	return m
}

func remoteAddrOf(t Transport) string {
	if ra, ok := t.(RemoteAddrTransport); ok {
		if a := ra.RemoteAddr(); a != nil {
			return a.String()
		}
	}
	return ""
}

// metricsSink returns the configured Metrics or NopMetrics. Clients built
// directly in tests may have no metrics set.
func (c *Client) metricsSink() Metrics {
	return metricsOrNop(c.metrics)
}
