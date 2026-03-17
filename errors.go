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
	ErrClosed             = errors.New("mms: connection closed")
	ErrInvokeTimeout      = errors.New("mms: invoke timeout")
	ErrConnectionRejected = errors.New("mms: connection rejected")
	ErrAssociationFailed  = errors.New("mms: association failed")
	ErrNegotiationFailed  = errors.New("mms: negotiation failed")
	ErrInvalidPDU         = errors.New("mms: invalid PDU")
	ErrDecodeFailed       = errors.New("mms: decode failed")
	ErrUnsupported        = errors.New("mms: unsupported")
	ErrServiceRejected    = errors.New("mms: service rejected")
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

// ProtocolError indicates a protocol-level violation at a specific ISO
// stack layer.
type ProtocolError struct {
	Phase   string // "session", "presentation", "acse", "mms"
	Message string
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("mms: protocol error [%s]: %s", e.Phase, e.Message)
}
