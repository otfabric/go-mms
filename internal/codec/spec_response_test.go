// SPDX-License-Identifier: MIT

package codec

import (
	"encoding/hex"
	"errors"
	"testing"
)

// TestSpec_9506_2_7_4_1_ServiceErrorEncoding: ServiceError is errorClass
// [0], then the optional additionalCode [1], additionalDescription [2]
// and serviceSpecificInfo [3], in that order (ISO 9506-2 clause 7.4.1).
func TestSpec_9506_2_7_4_1_ServiceErrorEncoding(t *testing.T) {
	code := 7
	for _, tc := range []struct {
		f    ServiceErrorFields
		want string
	}{
		{ServiceErrorFields{}, "a003870101"},
		{ServiceErrorFields{AdditionalCode: &code}, "a003870101810107"},
		{ServiceErrorFields{AdditionalDescription: "x"}, "a003870101820178"},
		{ServiceErrorFields{HasSpecific: true, SpecificKind: 6, SpecificContent: []byte{2}}, "a003870101a303860102"},
		{ServiceErrorFields{AdditionalCode: &code, AdditionalDescription: "x", HasSpecific: true, SpecificKind: 6, SpecificContent: []byte{2}},
			"a003870101810107820178a303860102"},
	} {
		if got := hex.EncodeToString(MarshalServiceError(7, 1, tc.f)); got != tc.want {
			t.Errorf("MarshalServiceError(%+v) = %s, want %s", tc.f, got, tc.want)
		}
	}
	// Confirmed-ErrorPDU [2] { invokeID [0], serviceError [2] }.
	if got := hex.EncodeToString(MarshalConfirmedErrorFields(5, 7, 1, ServiceErrorFields{AdditionalDescription: "x"})); got != "a20d800105a208a003870101820178" {
		t.Errorf("MarshalConfirmedErrorFields = %s", got)
	}
	// RejectPDU [4] without originalInvokeID: pdu-error [5] invalid-pdu (1).
	if got := hex.EncodeToString(MarshalRejectPDUNoInvokeID(5, 1)); got != "a403850101" {
		t.Errorf("MarshalRejectPDUNoInvokeID = %s", got)
	}
}

// TestEnvelopeError: the envelope error keeps the invokeID it could read
// and unwraps to its cause (ISO 9506-2 clause 8.6 originalInvokeID).
func TestEnvelopeError(t *testing.T) {
	_, _, err := UnmarshalConfirmedRequest([]byte{0x02, 0x01, 0x05})
	var envErr *EnvelopeError
	if !errors.As(err, &envErr) || !envErr.HasInvokeID || envErr.InvokeID != 5 || envErr.InvalidInvokeID {
		t.Fatalf("err = %#v", err)
	}
	if envErr.Error() == "" || errors.Unwrap(envErr) == nil {
		t.Error("EnvelopeError has no message or cause")
	}
	_, _, err = UnmarshalConfirmedRequest([]byte{0x02, 0x05, 0x00, 0x80, 0x00, 0x00, 0x00, 0x81, 0x00})
	if !errors.As(err, &envErr) || !envErr.InvalidInvokeID {
		t.Fatalf("2^31: err = %v, want InvalidInvokeID", err)
	}
}

// TestSpec_9506_2_7_1_ServiceChoiceContextClass: the service of a
// Confirmed-Request/Response is a CHOICE of context-specific alternatives
// (ISO 9506-2 clauses 7.1, 7.2); a universal or application tag is not
// one of them.
func TestSpec_9506_2_7_1_ServiceChoiceContextClass(t *testing.T) {
	for _, svc := range [][]byte{{0x30, 0x00}, {0x61, 0x00}, {0x04, 0x00}} {
		_, _, err := UnmarshalConfirmedResponse(append([]byte{0x02, 0x01, 0x07}, svc...))
		var envErr *EnvelopeError
		if !errors.As(err, &envErr) || !envErr.HasInvokeID || envErr.InvokeID != 7 {
			t.Errorf("service %x: err = %v, want EnvelopeError with invokeID 7", svc, err)
		}
	}
	if _, _, err := UnmarshalConfirmedResponse([]byte{0x02, 0x01, 0x07, 0xa4, 0x00}); err != nil {
		t.Errorf("context-specific service: %v", err)
	}
}
