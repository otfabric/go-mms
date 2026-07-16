// SPDX-License-Identifier: MIT

package pdu

import (
	"encoding/asn1"
	"encoding/hex"
	"testing"
)

func TestMarshalInitiateRequest_RoundTrip(t *testing.T) {
	req := DefaultInitiateRequest(65000, 5, 5, 10)
	data, err := MarshalInitiateRequest(req)
	if err != nil {
		t.Fatalf("MarshalInitiateRequest: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("encoded data is empty")
	}
	if data[0] != 0xa8 {
		t.Fatalf("expected outer tag 0xa8, got 0x%02x", data[0])
	}

	kind, content, err := DecodePdu(data)
	if err != nil {
		t.Fatalf("DecodePdu: %v", err)
	}
	if kind != PduInitiateRequest {
		t.Fatalf("kind = %v, want InitiateRequest", kind)
	}

	// Parse content back into struct to verify round-trip.
	var decoded InitiateRequest
	_, err = asn1.Unmarshal(content, &decoded)
	if err != nil {
		t.Fatalf("asn1.Unmarshal content: %v", err)
	}

	if decoded.LocalDetailCalling != 65000 {
		t.Errorf("LocalDetailCalling = %d, want 65000", decoded.LocalDetailCalling)
	}
	if decoded.ProposedMaxServOutstandingCall != 5 {
		t.Errorf("ProposedMaxServOutstandingCall = %d, want 5", decoded.ProposedMaxServOutstandingCall)
	}
	if decoded.ProposedMaxServOutstandingCalled != 5 {
		t.Errorf("ProposedMaxServOutstandingCalled = %d, want 5", decoded.ProposedMaxServOutstandingCalled)
	}
	if decoded.ProposedDataStructureNesting != 10 {
		t.Errorf("ProposedDataStructureNesting = %d, want 10", decoded.ProposedDataStructureNesting)
	}
	if decoded.InitRequestDetail.ProposedVersion != 1 {
		t.Errorf("ProposedVersion = %d, want 1", decoded.InitRequestDetail.ProposedVersion)
	}
	if decoded.InitRequestDetail.ServicesSupportedCalling.BitLength != 85 {
		t.Errorf("ServicesSupportedCalling.BitLength = %d, want 85", decoded.InitRequestDetail.ServicesSupportedCalling.BitLength)
	}

	t.Logf("Encoded InitiateRequest (%d bytes): %s", len(data), hex.EncodeToString(data))
}

func TestUnmarshalInitiateResponse(t *testing.T) {
	// Build a synthetic InitiateResponse, marshal it, wrap it as a PDU,
	// then decode to verify the full round-trip.
	resp := InitiateResponse{
		LocalDetailCalled:                  32000,
		NegotiatedMaxServOutstandingCall:   5,
		NegotiatedMaxServOutstandingCalled: 5,
		NegotiatedDataStructureNesting:     10,
		InitResponseDetail: InitResponseDetail{
			NegotiatedVersion:  1,
			NegotiatedParamCBB: asn1.BitString{Bytes: []byte{0xf1, 0x00}, BitLength: 11},
			ServicesSupportedCalled: asn1.BitString{
				Bytes:     []byte{0xee, 0x1c, 0x00, 0x00, 0x04, 0x08, 0x00, 0x00, 0x79, 0xef, 0x18},
				BitLength: 85,
			},
		},
	}

	content, err := asn1.Marshal(resp)
	if err != nil {
		t.Fatalf("asn1.Marshal response: %v", err)
	}

	decoded, err := UnmarshalInitiateResponse(content)
	if err != nil {
		t.Fatalf("UnmarshalInitiateResponse: %v", err)
	}

	if decoded.LocalDetailCalled != 32000 {
		t.Errorf("LocalDetailCalled = %d, want 32000", decoded.LocalDetailCalled)
	}
	if decoded.NegotiatedMaxServOutstandingCall != 5 {
		t.Errorf("NegotiatedMaxServOutstandingCall = %d, want 5", decoded.NegotiatedMaxServOutstandingCall)
	}
	if decoded.InitResponseDetail.NegotiatedVersion != 1 {
		t.Errorf("NegotiatedVersion = %d, want 1", decoded.InitResponseDetail.NegotiatedVersion)
	}
}

func TestInitiateRequest_FieldTagValues(t *testing.T) {
	req := DefaultInitiateRequest(1024, 2, 3, 4)
	data, err := MarshalInitiateRequest(req)
	if err != nil {
		t.Fatalf("MarshalInitiateRequest: %v", err)
	}

	// The first byte must be 0xa8 (context 8, constructed).
	if data[0] != 0xa8 {
		t.Errorf("outer tag = 0x%02x, want 0xa8", data[0])
	}

	// Verify we can classify it.
	kind, err := ClassifyPdu(data)
	if err != nil {
		t.Fatalf("ClassifyPdu: %v", err)
	}
	if kind != PduInitiateRequest {
		t.Errorf("ClassifyPdu = %v, want InitiateRequest", kind)
	}
}
