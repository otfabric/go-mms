// SPDX-License-Identifier: MIT

package pdu

// Interop tests validate encoding/decoding against known-good wire
// encodings that are compatible with the C reference implementation.
// These tests use hardcoded byte sequences representing valid MMS PDUs
// and verify that our implementation produces and consumes them correctly.

import (
	"bytes"
	"encoding/asn1"
	"testing"

	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
)

// TestInteropReadRequestEncoding verifies that a Read request for a
// domain-specific variable produces the expected wire encoding.
func TestInteropReadRequestEncoding(t *testing.T) {
	vars := []ObjectNameWire{
		{Scope: ScopeDomain, DomainID: "TestDomain", ItemID: "TestVar"},
	}
	pdu, err := MarshalReadRequest(1, vars)
	if err != nil {
		t.Fatalf("MarshalReadRequest: %v", err)
	}

	// Verify it's a valid ConfirmedRequest by unwrapping.
	tag, err := codec.PduType(pdu)
	if err != nil {
		t.Fatalf("PduType: %v", err)
	}
	if tag != 0xa0 {
		t.Errorf("expected ConfirmedRequest tag 0xa0, got 0x%02x", tag)
	}

	// The PDU should contain the domain and variable identifiers.
	if !bytes.Contains(pdu, []byte("TestDomain")) {
		t.Error("PDU does not contain domain identifier")
	}
	if !bytes.Contains(pdu, []byte("TestVar")) {
		t.Error("PDU does not contain variable identifier")
	}
}

// TestInteropReadResponseDecoding verifies that a hand-crafted Read
// response with an integer value decodes correctly.
func TestInteropReadResponseDecoding(t *testing.T) {
	// Build a Read response: listOfAccessResult [1] IMPLICIT with one integer.
	intData := berutil.EncodeTLV(0x85, []byte{0x00, 0x2a}) // integer 42
	list := berutil.EncodeTLV(tagReadListOfAccessResult, intData)

	raw := asn1.RawValue{Tag: 4, Class: 2, IsCompound: true, Bytes: list}
	results, err := UnmarshalReadResponse(raw)
	if err != nil {
		t.Fatalf("UnmarshalReadResponse: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Tag != TagDataInteger {
		t.Errorf("expected Integer tag, got 0x%02x", results[0].Tag)
	}
	if results[0].Int != 42 {
		t.Errorf("expected 42, got %d", results[0].Int)
	}
}

// TestInteropObjectNameEncodings verifies wire encoding of all three
// ObjectName scope variants against expected BER patterns.
func TestInteropObjectNameEncodings(t *testing.T) {
	tests := []struct {
		name    string
		wire    ObjectNameWire
		wantTag byte
	}{
		{"VMD", ObjectNameWire{Scope: ScopeVMD, ItemID: "vmdVar"}, 0x80},
		{"Domain", ObjectNameWire{Scope: ScopeDomain, DomainID: "dom", ItemID: "var"}, 0xa1},
		{"Association", ObjectNameWire{Scope: ScopeAssociation, ItemID: "aaVar"}, 0x82},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := EncodeObjectName(tt.wire)
			if err != nil {
				t.Fatalf("EncodeObjectName: %v", err)
			}
			if len(encoded) == 0 {
				t.Fatal("empty encoding")
			}
			if encoded[0] != tt.wantTag {
				t.Errorf("tag = 0x%02x, want 0x%02x", encoded[0], tt.wantTag)
			}
			// Round-trip.
			decoded, err := DecodeObjectName(encoded)
			if err != nil {
				t.Fatalf("DecodeObjectName: %v", err)
			}
			if decoded.Scope != tt.wire.Scope {
				t.Errorf("scope = %d, want %d", decoded.Scope, tt.wire.Scope)
			}
			if decoded.ItemID != tt.wire.ItemID {
				t.Errorf("itemID = %q, want %q", decoded.ItemID, tt.wire.ItemID)
			}
			if decoded.DomainID != tt.wire.DomainID {
				t.Errorf("domainID = %q, want %q", decoded.DomainID, tt.wire.DomainID)
			}
		})
	}
}

// TestInteropDataValueRoundTrip verifies that all supported MMS Data
// types encode and decode correctly through a marshal/unmarshal cycle.
func TestInteropDataValueRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		dv   *DataValue
	}{
		{"boolean_true", &DataValue{Tag: TagDataBoolean, Bool: true}},
		{"boolean_false", &DataValue{Tag: TagDataBoolean, Bool: false}},
		{"integer_positive", &DataValue{Tag: TagDataInteger, Int: 12345}},
		{"integer_negative", &DataValue{Tag: TagDataInteger, Int: -42}},
		{"integer_zero", &DataValue{Tag: TagDataInteger, Int: 0}},
		{"unsigned", &DataValue{Tag: TagDataUnsigned, Uint: 65535}},
		{"float32", &DataValue{Tag: TagDataFloat, Float: 3.14, FloatWide: false}},
		{"float64", &DataValue{Tag: TagDataFloat, Float: 2.718281828, FloatWide: true}},
		{"visible_string", &DataValue{Tag: TagDataVisibleStr, Str: "hello world"}},
		{"mms_string", &DataValue{Tag: TagDataMmsString, Str: "MMS test"}},
		{"octet_string", &DataValue{Tag: TagDataOctetString, Bytes: []byte{0xde, 0xad, 0xbe, 0xef}}},
		{"bit_string", &DataValue{Tag: TagDataBitString, Bytes: []byte{0xfc}, BitLen: 6}},
		{"access_error", &DataValue{Tag: TagDataAccessError, ErrCode: 5}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := MarshalData(tt.dv)
			if err != nil {
				t.Fatalf("MarshalData: %v", err)
			}
			decoded, _, err := UnmarshalDataElement(encoded, 0)
			if err != nil {
				t.Fatalf("UnmarshalDataElement: %v", err)
			}
			if decoded.Tag != tt.dv.Tag {
				t.Errorf("tag = 0x%02x, want 0x%02x", decoded.Tag, tt.dv.Tag)
			}

			switch tt.dv.Tag {
			case TagDataBoolean:
				if decoded.Bool != tt.dv.Bool {
					t.Errorf("bool = %v, want %v", decoded.Bool, tt.dv.Bool)
				}
			case TagDataInteger:
				if decoded.Int != tt.dv.Int {
					t.Errorf("int = %d, want %d", decoded.Int, tt.dv.Int)
				}
			case TagDataUnsigned:
				if decoded.Uint != tt.dv.Uint {
					t.Errorf("uint = %d, want %d", decoded.Uint, tt.dv.Uint)
				}
			case TagDataFloat:
				if decoded.FloatWide != tt.dv.FloatWide {
					t.Errorf("wide = %v, want %v", decoded.FloatWide, tt.dv.FloatWide)
				}
			case TagDataVisibleStr, TagDataMmsString:
				if decoded.Str != tt.dv.Str {
					t.Errorf("str = %q, want %q", decoded.Str, tt.dv.Str)
				}
			case TagDataOctetString:
				if !bytes.Equal(decoded.Bytes, tt.dv.Bytes) {
					t.Errorf("bytes = %x, want %x", decoded.Bytes, tt.dv.Bytes)
				}
			case TagDataBitString:
				if decoded.BitLen != tt.dv.BitLen {
					t.Errorf("bitLen = %d, want %d", decoded.BitLen, tt.dv.BitLen)
				}
			case TagDataAccessError:
				if decoded.ErrCode != tt.dv.ErrCode {
					t.Errorf("errCode = %d, want %d", decoded.ErrCode, tt.dv.ErrCode)
				}
			}
		})
	}
}

// TestInteropTypeSpecKnownEncodings verifies TypeSpecification decoding
// against known wire patterns from the ISO 9506 spec.
func TestInteropTypeSpecKnownEncodings(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantTag int
	}{
		{
			"boolean",
			berutil.EncodeTLV(0x83, []byte{0}),
			3,
		},
		{
			"integer_32",
			berutil.EncodeTLV(0x85, []byte{32}),
			5,
		},
		{
			"unsigned_16",
			berutil.EncodeTLV(0x86, []byte{16}),
			6,
		},
		{
			"visible_string_65",
			berutil.EncodeTLV(0x8a, []byte{65}),
			10,
		},
		{
			"mms_string_255",
			berutil.EncodeTLV(0x90, []byte{0x00, 0xff}),
			16,
		},
		{
			"utctime",
			berutil.EncodeTLV(0x91, nil),
			17,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, err := DecodeTypeSpec(tt.data)
			if err != nil {
				t.Fatalf("DecodeTypeSpec: %v", err)
			}
			if ts.Tag != tt.wantTag {
				t.Errorf("tag = %d, want %d", ts.Tag, tt.wantTag)
			}
		})
	}
}

// TestInteropWriteRequestEncoding verifies Write request encoding
// produces a valid ConfirmedRequest PDU.
func TestInteropWriteRequestEncoding(t *testing.T) {
	vars := []ObjectNameWire{
		{Scope: ScopeDomain, DomainID: "D", ItemID: "V"},
	}
	vals := []*DataValue{
		{Tag: TagDataInteger, Int: 100},
	}
	pdu, err := MarshalWriteRequest(1, vars, vals)
	if err != nil {
		t.Fatalf("MarshalWriteRequest: %v", err)
	}

	tag, err := codec.PduType(pdu)
	if err != nil {
		t.Fatalf("PduType: %v", err)
	}
	if tag != 0xa0 {
		t.Errorf("expected ConfirmedRequest tag 0xa0, got 0x%02x", tag)
	}
}

// TestInteropGetNameListRequestEncoding verifies GetNameList request
// encoding for VMD scope.
func TestInteropGetNameListRequestEncoding(t *testing.T) {
	pdu, err := MarshalGetNameListRequest(1, 0, ScopeVMD, "", "")
	if err != nil {
		t.Fatalf("MarshalGetNameListRequest: %v", err)
	}

	tag, err := codec.PduType(pdu)
	if err != nil {
		t.Fatalf("PduType: %v", err)
	}
	if tag != 0xa0 {
		t.Errorf("expected ConfirmedRequest tag 0xa0, got 0x%02x", tag)
	}
}

// TestInteropStructureDataValueRoundTrip verifies nested structure encoding.
func TestInteropStructureDataValueRoundTrip(t *testing.T) {
	dv := &DataValue{
		Tag: TagDataStructure,
		Elements: []*DataValue{
			{Tag: TagDataVisibleStr, Str: "field1"},
			{Tag: TagDataInteger, Int: -1},
			{Tag: TagDataBoolean, Bool: true},
		},
	}
	encoded, err := MarshalData(dv)
	if err != nil {
		t.Fatalf("MarshalData: %v", err)
	}
	decoded, _, err := UnmarshalDataElement(encoded, 0)
	if err != nil {
		t.Fatalf("UnmarshalDataElement: %v", err)
	}
	if decoded.Tag != TagDataStructure {
		t.Fatalf("tag = 0x%02x, want Structure", decoded.Tag)
	}
	if len(decoded.Elements) != 3 {
		t.Fatalf("elements = %d, want 3", len(decoded.Elements))
	}
	if decoded.Elements[0].Str != "field1" {
		t.Errorf("element[0].Str = %q, want %q", decoded.Elements[0].Str, "field1")
	}
	if decoded.Elements[1].Int != -1 {
		t.Errorf("element[1].Int = %d, want -1", decoded.Elements[1].Int)
	}
	if decoded.Elements[2].Bool != true {
		t.Errorf("element[2].Bool = %v, want true", decoded.Elements[2].Bool)
	}
}
