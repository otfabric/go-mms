package pdu

import (
	"testing"

	"github.com/otfabric/go-mms/internal/berutil"
)

func TestDecodeConfirmedError(t *testing.T) {
	// Build a ConfirmedError:
	// invokeID [0] IMPLICIT = 5
	// serviceError [2] IMPLICIT:
	//   errorClass [0] IMPLICIT:
	//     [7] access: INTEGER = 4 (object-access-denied)
	invokeID := berutil.EncodeTLV(0x80, []byte{0x05})
	errorClassChoice := berutil.EncodeTLV(0x87, []byte{0x04}) // [7] access, errorCode=4
	errorClass := berutil.EncodeTLV(0xa0, errorClassChoice)
	serviceError := berutil.EncodeTLV(0xa2, errorClass)

	content := make([]byte, 0, len(invokeID)+len(serviceError))
	content = append(content, invokeID...)
	content = append(content, serviceError...)

	ce, err := DecodeConfirmedError(content)
	if err != nil {
		t.Fatalf("DecodeConfirmedError: %v", err)
	}
	if ce.InvokeID != 5 {
		t.Errorf("InvokeID = %d, want 5", ce.InvokeID)
	}
	if ce.ErrorClass != 7 { // access
		t.Errorf("ErrorClass = %d, want 7 (access)", ce.ErrorClass)
	}
	if ce.ErrorCode != 4 { // object-access-denied
		t.Errorf("ErrorCode = %d, want 4", ce.ErrorCode)
	}
}

func TestDecodeRejectPDU(t *testing.T) {
	// Build a Reject:
	// originalInvokeID [0] IMPLICIT = 10
	// rejectReason [1] confirmedRequestPDU: INTEGER = 1 (unrecognized-service)
	invokeID := berutil.EncodeTLV(0x80, []byte{0x0a})
	reason := berutil.EncodeTLV(0x81, []byte{0x01}) // [1] confirmedRequest, reason=1

	content := make([]byte, 0, len(invokeID)+len(reason))
	content = append(content, invokeID...)
	content = append(content, reason...)

	rj, err := DecodeRejectPDU(content)
	if err != nil {
		t.Fatalf("DecodeRejectPDU: %v", err)
	}
	if !rj.HasInvokeID {
		t.Error("HasInvokeID should be true")
	}
	if rj.InvokeID != 10 {
		t.Errorf("InvokeID = %d, want 10", rj.InvokeID)
	}
	if rj.RejectType != 1 {
		t.Errorf("RejectType = %d, want 1 (confirmedRequest)", rj.RejectType)
	}
	if rj.RejectReason != 1 {
		t.Errorf("RejectReason = %d, want 1 (unrecognized-service)", rj.RejectReason)
	}
}

func TestDecodeRejectPDUNoInvokeID(t *testing.T) {
	// Reject with only rejectReason, no invokeID
	reason := berutil.EncodeTLV(0x85, []byte{0x00}) // [5] pduError, reason=0

	rj, err := DecodeRejectPDU(reason)
	if err != nil {
		t.Fatalf("DecodeRejectPDU: %v", err)
	}
	if rj.HasInvokeID {
		t.Error("HasInvokeID should be false")
	}
	if rj.RejectType != 5 {
		t.Errorf("RejectType = %d, want 5 (pduError)", rj.RejectType)
	}
}

func TestDecodeConfirmedErrorStrict(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
	}{
		{"empty content", nil},
		{"empty content zero len", []byte{}},
		{"missing invokeID", berutil.EncodeTLV(0xa2,
			berutil.EncodeTLV(0xa0,
				berutil.EncodeTLV(0x87, []byte{0x04})))},
		{"unexpected tag", append(
			berutil.EncodeTLV(0x80, []byte{0x01}),
			berutil.EncodeTLV(0xcc, []byte{0x00})...)},
	}
	for _, tt := range tests {
		_, err := DecodeConfirmedError(tt.content)
		if err == nil {
			t.Errorf("%s: expected error", tt.name)
		}
	}
}

func TestDecodeRejectPDUStrict(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
	}{
		{"empty content", nil},
		{"empty content zero len", []byte{}},
		{"missing rejectReason", berutil.EncodeTLV(0x80, []byte{0x01})},
		{"unexpected tag", append(
			berutil.EncodeTLV(0x80, []byte{0x01}),
			berutil.EncodeTLV(0xdd, []byte{0x00})...)},
	}
	for _, tt := range tests {
		_, err := DecodeRejectPDU(tt.content)
		if err == nil {
			t.Errorf("%s: expected error", tt.name)
		}
	}
}

func TestDecodeConfirmedErrorTrailingBytes(t *testing.T) {
	// Construct valid errorClass with trailing garbage
	errorChoice := berutil.EncodeTLV(0x87, []byte{0x04})
	errorChoiceWithTrailing := append(errorChoice, 0xff) // trailing byte
	errorClass := berutil.EncodeTLV(0xa0, errorChoiceWithTrailing)
	serviceError := berutil.EncodeTLV(0xa2, errorClass)
	invokeID := berutil.EncodeTLV(0x80, []byte{0x05})
	content := append(invokeID, serviceError...)

	_, err := DecodeConfirmedError(content)
	if err == nil {
		t.Error("expected error for trailing bytes in errorClass")
	}
}
