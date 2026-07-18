// SPDX-License-Identifier: MIT

package pdu

import (
	"encoding/asn1"
	"testing"

	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
)

// TestReadRequestWireStructure verifies each tagging layer of an encoded
// ReadRequest PDU, ensuring the exact structural hierarchy is correct.
//
//	0xa0  ConfirmedRequestPDU
//	  invokeID INTEGER
//	  0xa4  Read service [4] CONSTRUCTED
//	    0xa1  variableAccessSpecification [1] EXPLICIT
//	      0xa0  listOfVariable [0] CONSTRUCTED
//	        0x30  VariableSpecification SEQUENCE
//	          0xa0  name [0] EXPLICIT ObjectName
//	            0xa1  domain-specific ObjectName [1] CONSTRUCTED
func TestReadRequestWireStructure(t *testing.T) {
	vars := []ObjectNameWire{{Scope: ScopeDomain, DomainID: "interop", ItemID: "boolean"}}
	b, err := MarshalReadRequest(codec.InvokeID(1), vars)
	if err != nil {
		t.Fatalf("MarshalReadRequest: %v", err)
	}

	// Outer: ConfirmedRequestPDU
	outerTag, outerContent, outerN, err := berutil.DecodeTLVAt(b, 0)
	if err != nil {
		t.Fatalf("outer TLV: %v", err)
	}
	if outerN != len(b) {
		t.Fatalf("trailing bytes after outer PDU")
	}
	if outerTag != 0xa0 {
		t.Fatalf("outer tag = 0x%02x, want 0xa0 (ConfirmedRequestPDU)", outerTag)
	}

	// Skip invokeID (INTEGER)
	_, _, n, err := berutil.DecodeTLVAt(outerContent, 0)
	if err != nil {
		t.Fatalf("invokeID TLV: %v", err)
	}
	offset := n

	// Read service CHOICE [4] = 0xa4
	svcTag, svcContent, svcN, err := berutil.DecodeTLVAt(outerContent, offset)
	if err != nil {
		t.Fatalf("service TLV: %v", err)
	}
	offset += svcN
	if svcTag != 0xa4 {
		t.Fatalf("service tag = 0x%02x, want 0xa4 (Read)", svcTag)
	}
	if offset != len(outerContent) {
		t.Fatalf("trailing bytes after service TLV")
	}

	// variableAccessSpecification [1] EXPLICIT = 0xa1
	vaTag, vaContent, vaN, err := berutil.DecodeTLVAt(svcContent, 0)
	if err != nil {
		t.Fatalf("varAccessSpec TLV: %v", err)
	}
	if vaTag != tagReadVarAccessSpec {
		t.Fatalf("variableAccessSpecification tag = 0x%02x, want 0xa1 (tagReadVarAccessSpec)", vaTag)
	}
	if vaN != len(svcContent) {
		t.Fatalf("trailing bytes inside Read service body")
	}

	// listOfVariable [0] = 0xa0
	lovTag, lovContent, lovN, err := berutil.DecodeTLVAt(vaContent, 0)
	if err != nil {
		t.Fatalf("listOfVariable TLV: %v", err)
	}
	if lovTag != tagListOfVar {
		t.Fatalf("listOfVariable tag = 0x%02x, want 0xa0 (tagListOfVar)", lovTag)
	}
	if lovN != len(vaContent) {
		t.Fatalf("trailing bytes inside variableAccessSpecification")
	}

	// VariableSpecification SEQUENCE = 0x30
	vsTag, vsContent, vsN, err := berutil.DecodeTLVAt(lovContent, 0)
	if err != nil {
		t.Fatalf("VariableSpecification TLV: %v", err)
	}
	if vsTag != tagSequence {
		t.Fatalf("VariableSpecification tag = 0x%02x, want 0x30 (SEQUENCE)", vsTag)
	}
	if vsN != len(lovContent) {
		t.Fatalf("trailing bytes in listOfVariable")
	}

	// name [0] EXPLICIT ObjectName = 0xa0
	nameTag, nameContent, nameN, err := berutil.DecodeTLVAt(vsContent, 0)
	if err != nil {
		t.Fatalf("name TLV: %v", err)
	}
	if nameTag != tagVarSpecName {
		t.Fatalf("name tag = 0x%02x, want 0xa0 (tagVarSpecName)", nameTag)
	}
	if nameN != len(vsContent) {
		t.Fatalf("trailing bytes in VariableSpecification")
	}

	// domain-specific ObjectName [1] CONSTRUCTED = 0xa1
	onTag, _, _, err := berutil.DecodeTLVAt(nameContent, 0)
	if err != nil {
		t.Fatalf("ObjectName TLV: %v", err)
	}
	if onTag != 0xa1 {
		t.Fatalf("ObjectName tag = 0x%02x, want 0xa1 (domain-specific)", onTag)
	}
}

// TestWriteRequestWireStructure verifies that listOfData uses the IMPLICIT
// tag 0xa0 and does NOT contain an additional 0x30 SEQUENCE wrapper.
func TestWriteRequestWireStructure(t *testing.T) {
	vars := []ObjectNameWire{{Scope: ScopeDomain, DomainID: "interop", ItemID: "integer"}}
	values := []*DataValue{{Tag: TagDataInteger, Int: 42}}

	b, err := MarshalWriteRequest(codec.InvokeID(1), vars, values)
	if err != nil {
		t.Fatalf("MarshalWriteRequest: %v", err)
	}

	// Outer: ConfirmedRequestPDU
	_, outerContent, outerN, err := berutil.DecodeTLVAt(b, 0)
	if err != nil {
		t.Fatalf("outer TLV: %v", err)
	}
	if outerN != len(b) {
		t.Fatalf("trailing bytes after outer PDU")
	}

	// Skip invokeID
	_, _, n, err := berutil.DecodeTLVAt(outerContent, 0)
	if err != nil {
		t.Fatalf("invokeID TLV: %v", err)
	}
	offset := n

	// Write service CHOICE [5] = 0xa5
	svcTag, svcContent, svcN, err := berutil.DecodeTLVAt(outerContent, offset)
	if err != nil {
		t.Fatalf("service TLV: %v", err)
	}
	offset += svcN
	if svcTag != 0xa5 {
		t.Fatalf("service tag = 0x%02x, want 0xa5 (Write)", svcTag)
	}
	if offset != len(outerContent) {
		t.Fatalf("trailing bytes after Write service TLV")
	}

	// listOfVariable [0] = 0xa0
	_, _, lovN, err := berutil.DecodeTLVAt(svcContent, 0)
	if err != nil {
		t.Fatalf("listOfVariable TLV: %v", err)
	}

	// listOfData [0] IMPLICIT = 0xa0 (comes after listOfVariable)
	lodTag, lodContent, lodN, err := berutil.DecodeTLVAt(svcContent, lovN)
	if err != nil {
		t.Fatalf("listOfData TLV: %v", err)
	}
	if lodTag != tagWriteListOfData {
		t.Fatalf("listOfData tag = 0x%02x, want 0xa0 (tagWriteListOfData)", lodTag)
	}
	if lodN+lovN != len(svcContent) {
		t.Fatalf("trailing bytes in Write service body")
	}

	// listOfData content must start directly with a data element, NOT with 0x30.
	if len(lodContent) == 0 {
		t.Fatal("listOfData is empty")
	}
	if lodContent[0] == 0x30 {
		t.Fatalf("listOfData starts with 0x30 (legacy SEQUENCE wrapper); should be a data element tag")
	}
}

// TestReadRequestDecodeRoundTrip verifies that MarshalReadRequest +
// UnmarshalReadRequestParsed round-trips correctly with the canonical encoding.
func TestReadRequestDecodeRoundTrip(t *testing.T) {
	want := []ObjectNameWire{
		{Scope: ScopeDomain, DomainID: "interop", ItemID: "boolean"},
		{Scope: ScopeDomain, DomainID: "interop", ItemID: "integer"},
	}
	b, err := MarshalReadRequest(codec.InvokeID(7), want)
	if err != nil {
		t.Fatalf("MarshalReadRequest: %v", err)
	}

	// Extract Read service body (skip ConfirmedRequestPDU + invokeID)
	_, outerContent, _, err := berutil.DecodeTLVAt(b, 0)
	if err != nil {
		t.Fatalf("outer TLV: %v", err)
	}
	_, _, n, err := berutil.DecodeTLVAt(outerContent, 0)
	if err != nil {
		t.Fatalf("invokeID TLV: %v", err)
	}
	_, svcContent, _, err := berutil.DecodeTLVAt(outerContent, n)
	if err != nil {
		t.Fatalf("service TLV: %v", err)
	}

	req, err := UnmarshalReadRequestParsed(svcContent)
	if err != nil {
		t.Fatalf("UnmarshalReadRequestParsed: %v", err)
	}
	if len(req.Variables) != len(want) {
		t.Fatalf("got %d variables, want %d", len(req.Variables), len(want))
	}
	for i, w := range want {
		got := req.Variables[i].Name
		if got.DomainID != w.DomainID || got.ItemID != w.ItemID {
			t.Errorf("variable[%d]: got {%s,%s}, want {%s,%s}", i, got.DomainID, got.ItemID, w.DomainID, w.ItemID)
		}
	}
}

// --- Rejection tests: malformed legacy layouts must be refused ---

// TestReadRequestRejectsMissingVarAccessWrapper verifies that a Read request
// body lacking the [1] EXPLICIT variableAccessSpecification wrapper is rejected.
func TestReadRequestRejectsMissingVarAccessWrapper(t *testing.T) {
	// Encode listOfVariable directly (tag 0xa0) without the 0xa1 outer wrapper.
	vars := []ObjectNameWire{{Scope: ScopeDomain, DomainID: "d", ItemID: "v"}}
	varSpec, err := encodeListOfVariable(vars)
	if err != nil {
		t.Fatal(err)
	}
	// Pass bare 0xa0 body (listOfVariable) without the required 0xa1 wrapper.
	_, err = UnmarshalReadRequestParsed(varSpec)
	if err == nil {
		t.Fatal("expected error for missing variableAccessSpecification [1] wrapper, got nil")
	}
	t.Logf("correctly rejected: %v", err)
}

// TestReadRequestRejectsUnknownVarAccessChoice verifies that an unknown
// CHOICE tag inside variableAccessSpecification is rejected.
func TestReadRequestRejectsUnknownVarAccessChoice(t *testing.T) {
	// Wrap a garbage inner tag (0xa5) in the correct 0xa1 outer wrapper.
	inner := berutil.EncodeTLV(0xa5, []byte{0x01})
	wrapper := berutil.EncodeTLV(tagReadVarAccessSpec, inner)

	_, err := UnmarshalReadRequestParsed(wrapper)
	if err == nil {
		t.Fatal("expected error for unknown VariableAccessSpecification CHOICE tag, got nil")
	}
	t.Logf("correctly rejected: %v", err)
}

// TestReadResponseRejectsUniversalSequence verifies that a Read response with
// UNIVERSAL SEQUENCE (0x30) as the listOfAccessResult tag is rejected.
// Only 0xa1 ([1] IMPLICIT) is accepted.
func TestReadResponseRejectsUniversalSequence(t *testing.T) {
	boolData := berutil.EncodeTLV(TagDataBoolean, []byte{0xff})
	legacySeq := berutil.EncodeTLV(0x30, boolData) // wrong: legacy UNIVERSAL SEQUENCE

	serviceData := asn1.RawValue{
		Class:      asn1.ClassContextSpecific,
		Tag:        4,
		IsCompound: true,
		Bytes:      legacySeq,
	}
	_, err := UnmarshalReadResponse(serviceData)
	if err == nil {
		t.Fatal("expected error for UNIVERSAL SEQUENCE (0x30) listOfAccessResult, got nil")
	}
	t.Logf("correctly rejected: %v", err)
}

// TestWriteRequestRejectsUniversalSequenceForData verifies that a Write request
// with UNIVERSAL SEQUENCE (0x30) for listOfData is rejected.
// Only 0xa0 ([0] IMPLICIT) is accepted.
func TestWriteRequestRejectsUniversalSequenceForData(t *testing.T) {
	vars := []ObjectNameWire{{Scope: ScopeDomain, DomainID: "d", ItemID: "v"}}
	varSpec, err := encodeListOfVariable(vars)
	if err != nil {
		t.Fatal(err)
	}

	// Build a write body with listOfData as 0x30 (legacy/incorrect encoding).
	intData := berutil.EncodeTLV(TagDataInteger, []byte{0x01})
	legacyDataList := berutil.EncodeTLV(0x30, intData) // wrong: should be 0xa0

	body := append(varSpec, legacyDataList...)
	_, _, err = UnmarshalWriteRequest(body)
	if err == nil {
		t.Fatal("expected error for UNIVERSAL SEQUENCE (0x30) listOfData, got nil")
	}
	t.Logf("correctly rejected: %v", err)
}

// TestVariableSpecRejectsUnwrappedObjectName verifies that a VariableSpecification
// whose ObjectName is not wrapped in the name [0] EXPLICIT tag is rejected.
func TestVariableSpecRejectsUnwrappedObjectName(t *testing.T) {
	// Encode a domain-specific ObjectName directly, bypassing the name [0] wrapper.
	bare, err := EncodeObjectName(ObjectNameWire{Scope: ScopeDomain, DomainID: "d", ItemID: "v"})
	if err != nil {
		t.Fatal(err)
	}
	// Place the raw ObjectName directly in VariableSpecification SEQUENCE
	// content without the name [0] EXPLICIT wrapper.
	_, err = decodeVariableSpecWire(bare)
	if err == nil {
		t.Fatal("expected error for ObjectName without name [0] wrapper, got nil")
	}
	t.Logf("correctly rejected: %v", err)
}
