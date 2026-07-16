// SPDX-License-Identifier: MIT

package pdu

import (
	"encoding/asn1"
	"testing"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
)

func TestMarshalWriteRequest(t *testing.T) {
	vars := []ObjectNameWire{{Scope: ScopeDomain, DomainID: "TestDomain", ItemID: "TestVar"}}
	values := []*DataValue{{Tag: TagDataInteger, Int: 42}}

	b, err := MarshalWriteRequest(1, vars, values)
	if err != nil {
		t.Fatalf("MarshalWriteRequest: %v", err)
	}
	if b[0] != asn1util.TagConfirmedRequest {
		t.Fatalf("outer tag = 0x%02x, want 0x%02x", b[0], asn1util.TagConfirmedRequest)
	}
}

func TestMarshalWriteRequestMismatch(t *testing.T) {
	vars := []ObjectNameWire{{Scope: ScopeDomain, DomainID: "D", ItemID: "V"}}
	values := []*DataValue{
		{Tag: TagDataInteger, Int: 1},
		{Tag: TagDataInteger, Int: 2},
	}

	_, err := MarshalWriteRequest(1, vars, values)
	if err == nil {
		t.Fatal("expected error for mismatched vars/values count")
	}
}

func TestUnmarshalWriteResponseSuccess(t *testing.T) {
	// Single success: [1] NULL
	content := berutil.EncodeTLV(0x81, nil)

	serviceData := asn1.RawValue{
		Class:      asn1.ClassContextSpecific,
		Tag:        5,
		IsCompound: true,
		Bytes:      content,
	}

	results, err := UnmarshalWriteResponse(serviceData)
	if err != nil {
		t.Fatalf("UnmarshalWriteResponse: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if !results[0].Success {
		t.Error("result[0] should be success")
	}
}

func TestUnmarshalWriteResponseFailure(t *testing.T) {
	// Single failure: [0] DataAccessError = 5 (object-undefined)
	content := berutil.EncodeTLV(0x80, []byte{0x05})

	serviceData := asn1.RawValue{
		Class:      asn1.ClassContextSpecific,
		Tag:        5,
		IsCompound: true,
		Bytes:      content,
	}

	results, err := UnmarshalWriteResponse(serviceData)
	if err != nil {
		t.Fatalf("UnmarshalWriteResponse: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].Success {
		t.Error("result[0] should be failure")
	}
	if results[0].ErrCode != 5 {
		t.Errorf("result[0] error code = %d, want 5", results[0].ErrCode)
	}
}

func TestUnmarshalWriteResponseMultiple(t *testing.T) {
	// Mixed: success, failure, success
	var content []byte
	content = append(content, berutil.EncodeTLV(0x81, nil)...)
	content = append(content, berutil.EncodeTLV(0x80, []byte{0x03})...) // error 3
	content = append(content, berutil.EncodeTLV(0x81, nil)...)

	serviceData := asn1.RawValue{
		Class:      asn1.ClassContextSpecific,
		Tag:        5,
		IsCompound: true,
		Bytes:      content,
	}

	results, err := UnmarshalWriteResponse(serviceData)
	if err != nil {
		t.Fatalf("UnmarshalWriteResponse: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}
	if !results[0].Success {
		t.Error("result[0] should be success")
	}
	if results[1].Success || results[1].ErrCode != 3 {
		t.Errorf("result[1] = %+v, want failure code 3", results[1])
	}
	if !results[2].Success {
		t.Error("result[2] should be success")
	}
}
