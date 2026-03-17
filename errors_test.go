package mms

import (
	"errors"
	"testing"
)

func TestServiceErrorUnwraps(t *testing.T) {
	err := &ServiceError{Class: ErrorClassAccess, Code: 1, InvokeID: 42}
	if !errors.Is(err, ErrServiceRejected) {
		t.Error("ServiceError should unwrap to ErrServiceRejected")
	}
	msg := err.Error()
	if msg == "" {
		t.Error("ServiceError.Error() should not be empty")
	}
}

func TestDecodeErrorUnwraps(t *testing.T) {
	err := &DecodeError{Offset: 10, Tag: 0xa0, Message: "unexpected tag"}
	if !errors.Is(err, ErrDecodeFailed) {
		t.Error("DecodeError should unwrap to ErrDecodeFailed")
	}
}

func TestProtocolErrorMessage(t *testing.T) {
	err := &ProtocolError{Phase: "session", Message: "unexpected SPDU"}
	msg := err.Error()
	if msg == "" {
		t.Error("ProtocolError.Error() should not be empty")
	}
}

func TestSentinelErrors(t *testing.T) {
	sentinels := []error{
		ErrClosed,
		ErrInvokeTimeout,
		ErrConnectionRejected,
		ErrAssociationFailed,
		ErrNegotiationFailed,
		ErrInvalidPDU,
		ErrDecodeFailed,
		ErrUnsupported,
		ErrServiceRejected,
	}
	for _, s := range sentinels {
		if s == nil {
			t.Error("sentinel error should not be nil")
		}
		if s.Error() == "" {
			t.Errorf("sentinel %v should have a non-empty message", s)
		}
	}
}
