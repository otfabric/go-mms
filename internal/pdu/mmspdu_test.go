package pdu

import "testing"

func TestClassifyPdu_AllTags(t *testing.T) {
	tests := []struct {
		tag  byte
		want PduKind
	}{
		{0xa0, PduConfirmedRequest},
		{0xa1, PduConfirmedResponse},
		{0xa2, PduConfirmedError},
		{0xa3, PduUnconfirmed},
		{0xa4, PduReject},
		{0xa8, PduInitiateRequest},
		{0xa9, PduInitiateResponse},
		{0xaa, PduInitiateError},
		{0x8b, PduConcludeRequest},
		{0x8c, PduConcludeResponse},
	}
	for _, tt := range tests {
		// Build minimal TLV: tag + length 0.
		data := []byte{tt.tag, 0x00}
		got, err := ClassifyPdu(data)
		if err != nil {
			t.Errorf("ClassifyPdu(0x%02x): %v", tt.tag, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ClassifyPdu(0x%02x) = %v, want %v", tt.tag, got, tt.want)
		}
	}
}

func TestClassifyPdu_UnknownTag(t *testing.T) {
	data := []byte{0xff, 0x00}
	_, err := ClassifyPdu(data)
	if err == nil {
		t.Error("expected error for unknown tag 0xff")
	}
}

func TestClassifyPdu_EmptyData(t *testing.T) {
	_, err := ClassifyPdu(nil)
	if err == nil {
		t.Error("expected error for empty data")
	}
}

func TestPduKindString(t *testing.T) {
	if PduConfirmedRequest.String() != "ConfirmedRequest" {
		t.Errorf("got %q", PduConfirmedRequest.String())
	}
	if PduInitiateResponse.String() != "InitiateResponse" {
		t.Errorf("got %q", PduInitiateResponse.String())
	}
}
