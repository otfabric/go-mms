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
)

// ServiceError represents a remote MMS service error received via
// ConfirmedErrorPDU. Use [errors.As] to extract it from wrapped errors.
type ServiceError struct {
	Class    ErrorClass
	Code     int
	InvokeID InvokeID
}

func (e *ServiceError) Error() string {
	return fmt.Sprintf("mms: service error: class=%s code=%d invokeID=%d", e.Class, e.Code, e.InvokeID)
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

// ProtocolError indicates a protocol-level violation at a specific ISO
// stack layer.
type ProtocolError struct {
	Phase   string // "session", "presentation", "acse", "mms"
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
