// SPDX-License-Identifier: MIT

package pdu

import (
	"encoding/asn1"
	"testing"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
)

func TestMarshalDefineNamedVarListRequest(t *testing.T) {
	listName := ObjectNameWire{Scope: ScopeDomain, DomainID: "D", ItemID: "MyList"}
	vars := []VariableSpecWire{
		{Name: ObjectNameWire{Scope: ScopeDomain, DomainID: "D", ItemID: "V1"}},
		{Name: ObjectNameWire{Scope: ScopeDomain, DomainID: "D", ItemID: "V2"}},
	}

	b, err := MarshalDefineNamedVarListRequest(1, listName, vars)
	if err != nil {
		t.Fatalf("MarshalDefineNamedVarListRequest: %v", err)
	}
	if b[0] != asn1util.TagConfirmedRequest {
		t.Fatalf("outer tag = 0x%02x, want 0x%02x", b[0], asn1util.TagConfirmedRequest)
	}
}

func TestMarshalDefineNamedVarListRequestAA(t *testing.T) {
	listName := ObjectNameWire{Scope: ScopeAssociation, ItemID: "MyAAList"}
	vars := []VariableSpecWire{
		{Name: ObjectNameWire{Scope: ScopeDomain, DomainID: "D", ItemID: "V1"}},
	}

	b, err := MarshalDefineNamedVarListRequest(2, listName, vars)
	if err != nil {
		t.Fatalf("MarshalDefineNamedVarListRequest: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("empty request")
	}
}

func TestMarshalGetNamedVarListAttrsRequest(t *testing.T) {
	listName := ObjectNameWire{Scope: ScopeDomain, DomainID: "D", ItemID: "MyList"}
	b, err := MarshalGetNamedVarListAttrsRequest(1, listName)
	if err != nil {
		t.Fatalf("MarshalGetNamedVarListAttrsRequest: %v", err)
	}
	if b[0] != asn1util.TagConfirmedRequest {
		t.Fatalf("outer tag = 0x%02x, want 0x%02x", b[0], asn1util.TagConfirmedRequest)
	}
}

func TestUnmarshalGetNamedVarListAttrsResponse(t *testing.T) {
	// mmsDeletable [0] TRUE
	del := berutil.EncodeTLV(0x80, []byte{0xff})

	// listOfVariable [1] { SEQUENCE { varSpec [0] { domainSpecific [1] { "D", "V1" } } } }
	domainName := berutil.EncodeTLV(tagVisibleString, []byte("D"))
	itemName := berutil.EncodeTLV(tagVisibleString, []byte("V1"))
	domSpec := berutil.EncodeTLV(0xa1, append(domainName, itemName...))
	varSpec := berutil.EncodeTLV(0xa0, domSpec) // name [0] EXPLICIT ObjectName
	entry := berutil.EncodeTLV(tagSequence, varSpec)
	listOfVar := berutil.EncodeTLV(0xa1, entry) // [1] IMPLICIT SEQUENCE OF

	content := append(del, listOfVar...)
	serviceData := asn1.RawValue{
		Class:      asn1.ClassContextSpecific,
		Tag:        12,
		IsCompound: true,
		Bytes:      content,
	}

	result, err := UnmarshalGetNamedVarListAttrsResponse(serviceData)
	if err != nil {
		t.Fatalf("UnmarshalGetNamedVarListAttrsResponse: %v", err)
	}
	if !result.Deletable {
		t.Error("expected deletable = true")
	}
	if len(result.Variables) != 1 {
		t.Fatalf("variables = %d, want 1", len(result.Variables))
	}
	if result.Variables[0].Name.DomainID != "D" || result.Variables[0].Name.ItemID != "V1" {
		t.Errorf("variable = %+v, want {D, V1}", result.Variables[0])
	}
}

func TestMarshalDeleteNamedVarListRequest(t *testing.T) {
	names := []ObjectNameWire{
		{Scope: ScopeDomain, DomainID: "D", ItemID: "List1"},
	}

	b, err := MarshalDeleteNamedVarListRequest(1, names)
	if err != nil {
		t.Fatalf("MarshalDeleteNamedVarListRequest: %v", err)
	}
	if b[0] != asn1util.TagConfirmedRequest {
		t.Fatalf("outer tag = 0x%02x, want 0x%02x", b[0], asn1util.TagConfirmedRequest)
	}
}

func TestUnmarshalDeleteNamedVarListResponse(t *testing.T) {
	// Wire format: numberMatched [0] IMPLICIT = 0x80, numberDeleted [1] IMPLICIT = 0x81
	matched := berutil.EncodeTLV(0x80, []byte{0x01}) // 1
	deleted := berutil.EncodeTLV(0x81, []byte{0x01}) // 1

	content := append(matched, deleted...)
	serviceData := asn1.RawValue{
		Class:      asn1.ClassContextSpecific,
		Tag:        13,
		IsCompound: true,
		Bytes:      content,
	}

	result, err := UnmarshalDeleteNamedVarListResponse(serviceData)
	if err != nil {
		t.Fatalf("UnmarshalDeleteNamedVarListResponse: %v", err)
	}
	if result.NumberMatched != 1 {
		t.Errorf("numberMatched = %d, want 1", result.NumberMatched)
	}
	if result.NumberDeleted != 1 {
		t.Errorf("numberDeleted = %d, want 1", result.NumberDeleted)
	}
}

func TestMarshalDeleteNVLDomainScopeRequest(t *testing.T) {
	b, err := MarshalDeleteNVLDomainScopeRequest(1, "testDomain")
	if err != nil {
		t.Fatalf("MarshalDeleteNVLDomainScopeRequest: %v", err)
	}
	if b[0] != asn1util.TagConfirmedRequest {
		t.Fatalf("outer tag = 0x%02x, want 0x%02x", b[0], asn1util.TagConfirmedRequest)
	}

	body := extractServiceBody(t, b)
	req, err := UnmarshalDeleteNVLRequest(body)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.ScopeOfDelete != 2 {
		t.Errorf("ScopeOfDelete = %d, want 2", req.ScopeOfDelete)
	}
	if req.DomainName != "testDomain" {
		t.Errorf("DomainName = %q, want testDomain", req.DomainName)
	}
}

func TestMarshalDeleteNVLVMDScopeRequest(t *testing.T) {
	b, err := MarshalDeleteNVLVMDScopeRequest(1)
	if err != nil {
		t.Fatalf("MarshalDeleteNVLVMDScopeRequest: %v", err)
	}
	if b[0] != asn1util.TagConfirmedRequest {
		t.Fatalf("outer tag = 0x%02x, want 0x%02x", b[0], asn1util.TagConfirmedRequest)
	}

	body := extractServiceBody(t, b)
	req, err := UnmarshalDeleteNVLRequest(body)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.ScopeOfDelete != 3 {
		t.Errorf("ScopeOfDelete = %d, want 3", req.ScopeOfDelete)
	}
}
