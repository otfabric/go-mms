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

// TestDataAccessErrorWireValues verifies that every DataAccessErrorCode
// constant exactly matches the corresponding MMS data-access-error
// ENUMERATED wire value (ISO 9506-2). This is a regression guard: any
// accidental shift in the constants will be caught here.
func TestDataAccessErrorWireValues(t *testing.T) {
	tests := []struct {
		name string
		code DataAccessErrorCode
		wire int
	}{
		{"object-invalidated", DataAccessErrorObjectInvalidated, 0},
		{"hardware-fault", DataAccessErrorHardwareFault, 1},
		{"temporarily-unavailable", DataAccessErrorTemporarilyUnavailable, 2},
		{"object-access-denied", DataAccessErrorObjectAccessDenied, 3},
		{"object-undefined", DataAccessErrorObjectUndefined, 4},
		{"invalid-address", DataAccessErrorInvalidAddress, 5},
		{"type-unsupported", DataAccessErrorTypeUnsupported, 6},
		{"type-inconsistent", DataAccessErrorTypeInconsistent, 7},
		{"object-attribute-inconsistent", DataAccessErrorObjectAttributeInconsistent, 8},
		{"object-access-unsupported", DataAccessErrorObjectAccessUnsupported, 9},
		{"object-non-existent", DataAccessErrorObjectNonExistent, 10},
		{"object-value-invalid", DataAccessErrorObjectValueInvalid, 11},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Constant value must equal the MMS wire value.
			if int(tt.code) != tt.wire {
				t.Errorf("constant value = %d, want wire value %d", int(tt.code), tt.wire)
			}
			// encodeDataAccessError must produce the same wire value.
			got, err := encodeDataAccessError(tt.code)
			if err != nil {
				t.Fatalf("encodeDataAccessError: %v", err)
			}
			if got != tt.wire {
				t.Errorf("encodeDataAccessError = %d, want %d", got, tt.wire)
			}
			// Round-trip: decode the wire value back to the constant.
			decoded, err := decodeDataAccessError(tt.wire)
			if err != nil {
				t.Fatalf("decodeDataAccessError(%d): %v", tt.wire, err)
			}
			if decoded != tt.code {
				t.Errorf("decodeDataAccessError(%d) = %v, want %v", tt.wire, decoded, tt.code)
			}
		})
	}
}

func TestDataAccessErrorEncodeRejectsNone(t *testing.T) {
	_, err := encodeDataAccessError(DataAccessErrorNone)
	if err == nil {
		t.Fatal("expected error encoding DataAccessErrorNone, got nil")
	}
}

func TestDataAccessErrorDecodeRejectsOutOfRange(t *testing.T) {
	for _, wire := range []int{-1, 12, 100} {
		if _, err := decodeDataAccessError(wire); err == nil {
			t.Errorf("expected error for out-of-range wire value %d, got nil", wire)
		}
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
