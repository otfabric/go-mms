// SPDX-License-Identifier: MIT

package pdu

import (
	"encoding/asn1"
	"encoding/hex"
	"testing"

	"github.com/otfabric/go-mms/internal/berutil"
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

	// InitiateRequestPDU is IMPLICIT, so content contains bare SEQUENCE fields.
	// Reconstruct the 0x30 wrapper that encoding/asn1 needs to parse the struct.
	var decoded InitiateRequest
	_, err = asn1.Unmarshal(berutil.EncodeTLV(0x30, content), &decoded)
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
	// Build a synthetic InitiateResponse, encode it as bare SEQUENCE fields
	// (IMPLICIT tag — no 0x30 wrapper), then decode to verify the round-trip.
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

	// Encode using bare sequence (same form MarshalMmsPduBareSequence produces).
	content, err := marshalBareSequence(resp)
	if err != nil {
		t.Fatalf("marshalBareSequence response: %v", err)
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

func TestUnmarshalInitiateRequest_BareAndWrapped(t *testing.T) {
	req := DefaultInitiateRequest(48000, 3, 4, 8)
	pdu, err := MarshalInitiateRequest(req)
	if err != nil {
		t.Fatalf("MarshalInitiateRequest: %v", err)
	}
	_, content, err := DecodePdu(pdu)
	if err != nil {
		t.Fatalf("DecodePdu: %v", err)
	}

	// Bare IMPLICIT fields (no 0x30).
	decoded, err := UnmarshalInitiateRequest(content)
	if err != nil {
		t.Fatalf("UnmarshalInitiateRequest bare: %v", err)
	}
	if decoded.LocalDetailCalling != 48000 {
		t.Errorf("LocalDetailCalling = %d, want 48000", decoded.LocalDetailCalling)
	}
	if decoded.ProposedMaxServOutstandingCall != 3 || decoded.ProposedMaxServOutstandingCalled != 4 {
		t.Errorf("outstanding = %d/%d, want 3/4",
			decoded.ProposedMaxServOutstandingCall, decoded.ProposedMaxServOutstandingCalled)
	}

	// Explicit inner SEQUENCE (libiec61850-style).
	wrapped := berutil.EncodeTLV(0x30, content)
	decoded2, err := UnmarshalInitiateRequest(wrapped)
	if err != nil {
		t.Fatalf("UnmarshalInitiateRequest wrapped: %v", err)
	}
	if decoded2.LocalDetailCalling != 48000 {
		t.Errorf("wrapped LocalDetailCalling = %d, want 48000", decoded2.LocalDetailCalling)
	}
}

func TestUnmarshalInitiateRequest_Errors(t *testing.T) {
	if _, err := UnmarshalInitiateRequest([]byte{0xff}); err == nil {
		t.Fatal("expected error for invalid ASN.1")
	}
	// Complete SEQUENCE TLV followed by trailing bytes (wrapSequenceIfNeeded
	// leaves an existing 0x30 wrapper as-is, so rest is non-empty).
	req := DefaultInitiateRequest(1000, 1, 1, 1)
	bare, err := marshalBareSequence(req)
	if err != nil {
		t.Fatal(err)
	}
	seq := berutil.EncodeTLV(0x30, bare)
	junk := append(append([]byte{}, seq...), 0x05, 0x00)
	if _, err := UnmarshalInitiateRequest(junk); err == nil {
		t.Fatal("expected trailing-bytes error")
	}
}
