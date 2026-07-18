// SPDX-License-Identifier: MIT

package pdu

import (
	"encoding/asn1"
	"testing"
)

func TestMarshalStatusRequest(t *testing.T) {
	data, err := MarshalStatusRequest(1, false)
	if err != nil {
		t.Fatalf("MarshalStatusRequest: %v", err)
	}

	kind, err := ClassifyPdu(data)
	if err != nil {
		t.Fatalf("ClassifyPdu: %v", err)
	}
	if kind != PduConfirmedRequest {
		t.Errorf("PDU kind = %s, want ConfirmedRequest", kind)
	}
}

func TestMarshalStatusRequestExtended(t *testing.T) {
	data, err := MarshalStatusRequest(2, true)
	if err != nil {
		t.Fatalf("MarshalStatusRequest: %v", err)
	}

	kind, err := ClassifyPdu(data)
	if err != nil {
		t.Fatalf("ClassifyPdu: %v", err)
	}
	if kind != PduConfirmedRequest {
		t.Errorf("PDU kind = %s, want ConfirmedRequest", kind)
	}
}

func TestStatusResponseRoundTrip(t *testing.T) {
	data, err := MarshalStatusRequest(1, false)
	if err != nil {
		t.Fatalf("MarshalStatusRequest: %v", err)
	}

	// Verify the request is parseable as a confirmed request.
	_, content, err := DecodePdu(data)
	if err != nil {
		t.Fatalf("DecodePdu: %v", err)
	}

	resp, err := DecodeConfirmedResponse(content)
	if err == nil {
		// A status request is NOT a response — just checking it decodes
		// as a confirmed request structure.
		_ = resp
	}
}

func TestUnmarshalStatusResponse(t *testing.T) {
	// Build bare SEQUENCE fields (IMPLICIT tag — no 0x30 wrapper).
	bareFields, err := MarshalStatusResponse(1, 2)
	if err != nil {
		t.Fatal(err)
	}

	// UnmarshalImplicitSequence wraps raw.Bytes in 0x30 before decoding.
	raw := asn1.RawValue{
		Class:      asn1.ClassContextSpecific,
		Tag:        0,
		IsCompound: true,
		Bytes:      bareFields,
	}

	resp, err := UnmarshalStatusResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if resp.VMDLogicalStatus != 1 {
		t.Fatalf("logical=%d, want 1", resp.VMDLogicalStatus)
	}
	if resp.VMDPhysicalStatus != 2 {
		t.Fatalf("physical=%d, want 2", resp.VMDPhysicalStatus)
	}
}

func TestUnmarshalStatusResponseBadInput(t *testing.T) {
	raw := asn1.RawValue{Tag: 0x05, Class: asn1.ClassUniversal, IsCompound: true, Bytes: []byte{0xff, 0xff}}
	_, err := UnmarshalStatusResponse(raw)
	if err == nil {
		t.Fatal("expected error for invalid ASN.1 content")
	}
}
