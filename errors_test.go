// SPDX-License-Identifier: MIT

package mms

import (
	"errors"
	"strings"
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

func TestProtocolErrorUnwraps(t *testing.T) {
	err := &ProtocolError{Phase: "session", Message: "unexpected SPDU"}
	if !errors.Is(err, ErrProtocol) {
		t.Error("ProtocolError should unwrap to ErrProtocol")
	}
	msg := err.Error()
	if msg == "" {
		t.Error("ProtocolError.Error() should not be empty")
	}
}

func TestDecodeErrorError(t *testing.T) {
	e := &DecodeError{Offset: 10, Tag: 0x42, Message: "bad tag"}
	s := e.Error()
	if s == "" {
		t.Fatal("empty error string")
	}
	if !strings.Contains(s, "offset 10") {
		t.Fatal("missing offset")
	}
	if !strings.Contains(s, "0x42") {
		t.Fatal("missing tag")
	}
	if !strings.Contains(s, "bad tag") {
		t.Fatal("missing message")
	}
}

func TestDataAccessErrorError(t *testing.T) {
	e := &DataAccessError{Code: DataAccessErrorObjectUndefined}
	s := e.Error()
	if s == "" {
		t.Fatal("empty error string")
	}
	if !errors.Is(e, ErrDataAccess) {
		t.Fatal("should unwrap to ErrDataAccess")
	}
}

func TestAuthenticationErrorError(t *testing.T) {
	e := &AuthenticationError{Reason: "bad password"}
	s := e.Error()
	if !strings.Contains(s, "bad password") {
		t.Fatal("missing reason")
	}
	if !errors.Is(e, ErrAuthenticationFailed) {
		t.Fatal("should unwrap to ErrAuthenticationFailed")
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
		ErrProtocol,
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
