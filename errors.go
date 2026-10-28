// SPDX-License-Identifier: MIT

package mms

import (
	"errors"
	"fmt"
)

// Sentinel errors for major failure categories.
//
// Use [errors.Is] to test against these values. All errors returned by the
// library wrap one of these sentinels or a typed error struct that can be
// inspected with [errors.As].
var (
	ErrClosed               = errors.New("mms: connection closed")
	ErrInvokeTimeout        = errors.New("mms: invoke timeout")
	ErrConnectionRejected   = errors.New("mms: connection rejected")
	ErrAssociationFailed    = errors.New("mms: association failed")
	ErrNegotiationFailed    = errors.New("mms: negotiation failed")
	ErrInvalidPDU           = errors.New("mms: invalid PDU")
	ErrDecodeFailed         = errors.New("mms: decode failed")
	ErrUnsupported          = errors.New("mms: unsupported")
	ErrServiceRejected      = errors.New("mms: service rejected")
	ErrDataAccess           = errors.New("mms: data access error")
	ErrProtocol             = errors.New("mms: protocol error")
	ErrServerConnClosed     = errors.New("mms: server connection closed")
	ErrAuthenticationFailed = errors.New("mms: authentication failed")

	// ErrServerClosed is returned by [Server.Serve] and
	// [Server.ListenAndServe] after [Server.Shutdown].
	ErrServerClosed = errors.New("mms: server closed")

	// ErrInvalidArgument is wrapped by errors caused by a caller-supplied
	// argument or option (empty names, nil values, mismatched slices,
	// values that cannot be encoded). The request is not sent.
	ErrInvalidArgument = errors.New("mms: invalid argument")
)

// Phases reported in [ProtocolError.Phase].
const (
	PhaseSession      = "session"
	PhasePresentation = "presentation"
	PhaseACSE         = "acse"
	PhaseMMS          = "mms"
)

// kindError attaches sentinel kinds to an error without changing its
// message. errors.Is and errors.As see both the original chain and the
// kinds.
type kindError struct {
	err   error
	kinds []error
}

func (e *kindError) Error() string { return e.err.Error() }

func (e *kindError) Unwrap() []error {
	return append([]error{e.err}, e.kinds...)
}

// classify returns err annotated with the given sentinel kinds. Kinds
// that err already matches are not added twice. A nil err stays nil.
func classify(err error, kinds ...error) error {
	if err == nil {
		return nil
	}
	var add []error
	for _, k := range kinds {
		if !errors.Is(err, k) {
			add = append(add, k)
		}
	}
	if len(add) == 0 {
		return err
	}
	return &kindError{err: err, kinds: add}
}

// invalidArgf formats an error that wraps [ErrInvalidArgument].
func invalidArgf(format string, args ...any) error {
	return classify(fmt.Errorf(format, args...), ErrInvalidArgument)
}

// decodeErrorf formats an error that wraps [ErrDecodeFailed].
func decodeErrorf(format string, args ...any) error {
	return classify(fmt.Errorf(format, args...), ErrDecodeFailed)
}

// invalidPDUf formats an error that wraps [ErrInvalidPDU].
func invalidPDUf(format string, args ...any) error {
	return classify(fmt.Errorf(format, args...), ErrInvalidPDU)
}

// ServiceError represents a remote MMS service error received via
// ConfirmedErrorPDU. Use [errors.As] to extract it from wrapped errors.
type ServiceError struct {
	Class    ErrorClass
	Code     int
	InvokeID InvokeID
}

func (e *ServiceError) Error() string {
	if name := e.CodeName(); name != "" {
		return fmt.Sprintf("mms: service error: class=%s code=%d (%s) invokeID=%d", e.Class, e.Code, name, e.InvokeID)
	}
	return fmt.Sprintf("mms: service error: class=%s code=%d invokeID=%d", e.Class, e.Code, e.InvokeID)
}

// CodeName returns the ISO 9506-2 name of Code within Class, such as
// "object-non-existent" for class Access code 2. It returns "" when the
// code is not defined for the class (and for class Others, whose codes
// are plain integers).
func (e *ServiceError) CodeName() string {
	if int(e.Class) < 0 || int(e.Class) >= len(serviceErrorCodeNames) {
		return ""
	}
	names := serviceErrorCodeNames[e.Class]
	if e.Code < 0 || e.Code >= len(names) {
		return ""
	}
	return names[e.Code]
}

// serviceErrorCodeNames lists the ServiceError errorClass codes of
// ISO 9506-2 per class, indexed by ErrorClass and then by code.
var serviceErrorCodeNames = [...][]string{
	ErrorClassVMDState:       {"other", "vmd-state-conflict", "vmd-operational-problem", "domain-transfer-problem", "state-machine-id-invalid"},
	ErrorClassAppReference:   {"other", "application-unreachable", "connection-lost", "application-reference-invalid", "context-unsupported"},
	ErrorClassDefinition:     {"other", "object-undefined", "invalid-address", "type-unsupported", "type-inconsistent", "object-exists", "object-attribute-inconsistent"},
	ErrorClassResource:       {"other", "memory-unavailable", "processor-resource-unavailable", "mass-storage-unavailable", "capability-unavailable", "capability-unknown"},
	ErrorClassService:        {"other", "primitives-out-of-sequence", "object-state-conflict", "pdu-size", "continuation-invalid", "object-constraint-conflict"},
	ErrorClassServicePreempt: {"other", "timeout", "deadlock", "cancel"},
	ErrorClassTimeResolution: {"other", "unsupportable-time-resolution"},
	ErrorClassAccess:         {"other", "object-access-unsupported", "object-non-existent", "object-access-denied", "object-invalidated"},
	ErrorClassInitiate:       {"other", "version-incompatible", "max-segment-insufficient", "max-services-outstanding-calling-insufficient", "max-services-outstanding-called-insufficient", "service-CBB-insufficient", "parameter-CBB-insufficient", "nesting-level-insufficient"},
	ErrorClassConclude:       {"other", "further-communication-required"},
	ErrorClassCancel:         {"other", "invoke-id-unknown", "cancel-not-possible"},
	ErrorClassFile:           {"other", "filename-ambiguous", "file-busy", "filename-syntax-error", "content-type-invalid", "position-invalid", "file-access-denied", "file-non-existent", "duplicate-filename", "insufficient-space-in-filestore"},
	ErrorClassOthers:         nil,
}

// RejectType identifies which kind of PDU the peer rejected in an MMS
// RejectPDU (the rejectReason CHOICE alternative of ISO 9506-2).
type RejectType int

// RejectPDU rejectReason alternatives.
const (
	RejectConfirmedRequest  RejectType = 1
	RejectConfirmedResponse RejectType = 2
	RejectConfirmedError    RejectType = 3
	RejectUnconfirmed       RejectType = 4
	RejectPDUError          RejectType = 5
	RejectCancelRequest     RejectType = 6
	RejectCancelResponse    RejectType = 7
	RejectCancelError       RejectType = 8
	RejectConcludeRequest   RejectType = 9
	RejectConcludeResponse  RejectType = 10
	RejectConcludeError     RejectType = 11
)

var rejectTypeNames = [...]string{
	RejectConfirmedRequest:  "confirmed-requestPDU",
	RejectConfirmedResponse: "confirmed-responsePDU",
	RejectConfirmedError:    "confirmed-errorPDU",
	RejectUnconfirmed:       "unconfirmedPDU",
	RejectPDUError:          "pdu-error",
	RejectCancelRequest:     "cancel-requestPDU",
	RejectCancelResponse:    "cancel-responsePDU",
	RejectCancelError:       "cancel-errorPDU",
	RejectConcludeRequest:   "conclude-requestPDU",
	RejectConcludeResponse:  "conclude-responsePDU",
	RejectConcludeError:     "conclude-errorPDU",
}

// String returns the ISO 9506-2 name of the reject type.
func (t RejectType) String() string {
	if t > 0 && int(t) < len(rejectTypeNames) {
		return rejectTypeNames[t]
	}
	return fmt.Sprintf("RejectType(%d)", int(t))
}

// RejectError is returned when the peer answers a confirmed request
// with an MMS RejectPDU. Reason is the reject code within Type (for
// example 1 = unrecognized-service for [RejectConfirmedRequest]).
//
// RejectError unwraps to a [*ProtocolError] in phase [PhaseMMS], so
// errors.Is(err, [ErrProtocol]) and errors.As with *ProtocolError keep
// matching as in earlier releases.
type RejectError struct {
	Type        RejectType
	Reason      int
	InvokeID    InvokeID
	HasInvokeID bool
}

func (e *RejectError) message() string {
	if e.HasInvokeID {
		return fmt.Sprintf("request rejected: type=%d reason=%d invokeID=%d", int(e.Type), e.Reason, e.InvokeID)
	}
	return fmt.Sprintf("request rejected: type=%d reason=%d invokeID=absent", int(e.Type), e.Reason)
}

func (e *RejectError) Error() string {
	return fmt.Sprintf("mms: protocol error [%s]: %s (%s)", PhaseMMS, e.message(), e.Type)
}

func (e *RejectError) Unwrap() error {
	return &ProtocolError{Phase: PhaseMMS, Message: e.message()}
}

func (e *ServiceError) Unwrap() error { return ErrServiceRejected }

// DecodeError indicates a failure to decode a protocol data unit.
// Use [errors.As] to extract it from wrapped errors.
type DecodeError struct {
	Offset  int
	Tag     byte
	Message string
}

func (e *DecodeError) Error() string {
	return fmt.Sprintf("mms: decode error at offset %d (tag 0x%02x): %s", e.Offset, e.Tag, e.Message)
}

func (e *DecodeError) Unwrap() error { return ErrDecodeFailed }

// DataAccessError is returned when an MMS read or write operation
// encounters a per-variable access error from the server. This is
// distinct from [ServiceError] (ConfirmedErrorPDU) — a DataAccessError
// is a normal MMS access-result outcome within a successful response.
type DataAccessError struct {
	Code DataAccessErrorCode
}

func (e *DataAccessError) Error() string {
	return fmt.Sprintf("mms: data access error: %s", e.Code)
}

func (e *DataAccessError) Unwrap() error { return ErrDataAccess }

// encodeDataAccessError converts a DataAccessErrorCode to its MMS wire
// integer value. Returns an error if code is DataAccessErrorNone or outside
// the known range 0–11, preventing accidental serialization of sentinels
// or future enum additions before they are mapped.
func encodeDataAccessError(code DataAccessErrorCode) (int, error) {
	if code == DataAccessErrorNone || int(code) < 0 || int(code) > int(DataAccessErrorObjectValueInvalid) {
		return 0, fmt.Errorf("mms: cannot encode DataAccessErrorCode %v as MMS wire value", code)
	}
	return int(code), nil
}

// decodeDataAccessError converts an MMS wire integer to a DataAccessErrorCode.
// Returns an error for values outside the known range 0–11.
func decodeDataAccessError(wire int) (DataAccessErrorCode, error) {
	if wire < 0 || wire > int(DataAccessErrorObjectValueInvalid) {
		return DataAccessErrorNone, fmt.Errorf("mms: unknown data-access-error wire value %d", wire)
	}
	return DataAccessErrorCode(wire), nil
}

// ProtocolError indicates a protocol-level violation at a specific ISO
// stack layer.
type ProtocolError struct {
	Phase   string // one of PhaseSession, PhasePresentation, PhaseACSE, PhaseMMS
	Message string
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("mms: protocol error [%s]: %s", e.Phase, e.Message)
}

func (e *ProtocolError) Unwrap() error { return ErrProtocol }

// AuthenticationError indicates an authentication failure during
// association establishment. Use [errors.As] to extract it and
// [errors.Is] with [ErrAuthenticationFailed] to classify.
type AuthenticationError struct {
	// Reason describes why authentication failed.
	Reason string
}

func (e *AuthenticationError) Error() string {
	return fmt.Sprintf("mms: authentication failed: %s", e.Reason)
}

func (e *AuthenticationError) Unwrap() error { return ErrAuthenticationFailed }
