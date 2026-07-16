// SPDX-License-Identifier: MIT

package pdu

import (
	"encoding/asn1"
	"testing"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
)

func TestMarshalReadRequestSingleVariable(t *testing.T) {
	vars := []ObjectNameWire{{Scope: ScopeDomain, DomainID: "TestDomain", ItemID: "TestVar"}}
	b, err := MarshalReadRequest(1, vars)
	if err != nil {
		t.Fatalf("MarshalReadRequest: %v", err)
	}

	// Should be a ConfirmedRequestPdu
	if b[0] != asn1util.TagConfirmedRequest {
		t.Fatalf("outer tag = 0x%02x, want 0x%02x", b[0], asn1util.TagConfirmedRequest)
	}
}

func TestMarshalReadRequestMultipleVariables(t *testing.T) {
	vars := []ObjectNameWire{
		{Scope: ScopeDomain, DomainID: "Domain1", ItemID: "Var1"},
		{Scope: ScopeDomain, DomainID: "Domain2", ItemID: "Var2"},
	}
	b, err := MarshalReadRequest(2, vars)
	if err != nil {
		t.Fatalf("MarshalReadRequest: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("empty read request")
	}
}

func TestUnmarshalReadResponseSingleValue(t *testing.T) {
	// Build a Read response: SEQUENCE OF { boolean true }
	boolData := berutil.EncodeTLV(TagDataBoolean, []byte{0xff})
	seqOf := berutil.EncodeTLV(0x30, boolData)

	serviceData := asn1.RawValue{
		Class:      asn1.ClassContextSpecific,
		Tag:        4,
		IsCompound: true,
		Bytes:      seqOf,
	}

	results, err := UnmarshalReadResponse(serviceData)
	if err != nil {
		t.Fatalf("UnmarshalReadResponse: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if !results[0].Bool {
		t.Error("result[0] should be true")
	}
}

func TestUnmarshalReadResponseMultipleValues(t *testing.T) {
	// Build: SEQUENCE OF { integer 42, visible string "hello", data access error 5 }
	var elements []byte
	b1 := berutil.EncodeTLV(TagDataInteger, []byte{0x2a}) // 42
	elements = append(elements, b1...)
	b2 := berutil.EncodeTLV(TagDataVisibleStr, []byte("hello"))
	elements = append(elements, b2...)
	b3 := berutil.EncodeTLV(TagDataAccessError, []byte{0x05})
	elements = append(elements, b3...)

	seqOf := berutil.EncodeTLV(0x30, elements)

	serviceData := asn1.RawValue{
		Class:      asn1.ClassContextSpecific,
		Tag:        4,
		IsCompound: true,
		Bytes:      seqOf,
	}

	results, err := UnmarshalReadResponse(serviceData)
	if err != nil {
		t.Fatalf("UnmarshalReadResponse: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}
	if results[0].Int != 42 {
		t.Errorf("result[0] int = %d, want 42", results[0].Int)
	}
	if results[1].Str != "hello" {
		t.Errorf("result[1] str = %q, want %q", results[1].Str, "hello")
	}
	if results[2].Tag != TagDataAccessError || results[2].ErrCode != 5 {
		t.Errorf("result[2] = %+v, want access error 5", results[2])
	}
}

func TestUnmarshalReadResponseWithVarSpec(t *testing.T) {
	// Build response with optional variableAccessSpecification [0] present
	varSpec := berutil.EncodeTLV(0xa0, []byte{0x01, 0x02}) // dummy varspec
	boolData := berutil.EncodeTLV(TagDataBoolean, []byte{0x00})
	seqOf := berutil.EncodeTLV(0x30, boolData)

	content := make([]byte, 0, len(varSpec)+len(seqOf))
	content = append(content, varSpec...)
	content = append(content, seqOf...)

	serviceData := asn1.RawValue{
		Class:      asn1.ClassContextSpecific,
		Tag:        4,
		IsCompound: true,
		Bytes:      content,
	}

	results, err := UnmarshalReadResponse(serviceData)
	if err != nil {
		t.Fatalf("UnmarshalReadResponse: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].Bool {
		t.Error("result[0] should be false")
	}
}
