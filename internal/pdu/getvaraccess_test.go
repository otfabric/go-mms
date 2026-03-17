package pdu

import (
	"encoding/asn1"
	"testing"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
)

func TestMarshalGetVarAccessRequest(t *testing.T) {
	name := ObjectNameWire{Scope: ScopeDomain, DomainID: "D", ItemID: "V"}
	b, err := MarshalGetVarAccessRequest(1, name)
	if err != nil {
		t.Fatalf("MarshalGetVarAccessRequest: %v", err)
	}
	if b[0] != asn1util.TagConfirmedRequest {
		t.Fatalf("outer tag = 0x%02x, want 0x%02x", b[0], asn1util.TagConfirmedRequest)
	}
}

func buildTypeSpecResponse(deletable bool, typeSpecContent []byte) asn1.RawValue {
	var del byte
	if deletable {
		del = 0xff
	}
	mmsDeletable := berutil.EncodeTLV(0x80, []byte{del})
	typeSpec := berutil.EncodeTLV(0xa2, typeSpecContent)

	content := append(mmsDeletable, typeSpec...)
	return asn1.RawValue{
		Class:      asn1.ClassContextSpecific,
		Tag:        6,
		IsCompound: true,
		Bytes:      content,
	}
}

func TestDecodeTypeSpecBoolean(t *testing.T) {
	// TypeSpecification boolean [3] NULL
	ts := berutil.EncodeTLV(0x83, nil)
	resp := buildTypeSpecResponse(false, ts)

	result, err := UnmarshalGetVarAccessResponse(resp)
	if err != nil {
		t.Fatalf("UnmarshalGetVarAccessResponse: %v", err)
	}
	if result.TypeSpec.Tag != tsTagBoolean {
		t.Errorf("tag = %d, want %d (boolean)", result.TypeSpec.Tag, tsTagBoolean)
	}
}

func TestDecodeTypeSpecInteger(t *testing.T) {
	// TypeSpecification integer [5] Unsigned8 = 32 bits
	ts := berutil.EncodeTLV(0x85, []byte{32})
	resp := buildTypeSpecResponse(false, ts)

	result, err := UnmarshalGetVarAccessResponse(resp)
	if err != nil {
		t.Fatalf("UnmarshalGetVarAccessResponse: %v", err)
	}
	if result.TypeSpec.Tag != tsTagInteger {
		t.Errorf("tag = %d, want %d (integer)", result.TypeSpec.Tag, tsTagInteger)
	}
	if result.TypeSpec.Size != 32 {
		t.Errorf("size = %d, want 32", result.TypeSpec.Size)
	}
}

func TestDecodeTypeSpecFloat(t *testing.T) {
	// TypeSpecification floatingpoint [7] SEQUENCE { formatwidth=32, exponentwidth=8 }
	fw := berutil.EncodeTLV(0x02, []byte{32})
	ew := berutil.EncodeTLV(0x02, []byte{8})
	ts := berutil.EncodeTLV(0xa7, append(fw, ew...))
	resp := buildTypeSpecResponse(true, ts)

	result, err := UnmarshalGetVarAccessResponse(resp)
	if err != nil {
		t.Fatalf("UnmarshalGetVarAccessResponse: %v", err)
	}
	if !result.Deletable {
		t.Error("expected deletable = true")
	}
	if result.TypeSpec.Tag != tsTagFloat {
		t.Errorf("tag = %d, want %d (float)", result.TypeSpec.Tag, tsTagFloat)
	}
	if result.TypeSpec.FormatWidth != 32 {
		t.Errorf("formatWidth = %d, want 32", result.TypeSpec.FormatWidth)
	}
	if result.TypeSpec.ExpWidth != 8 {
		t.Errorf("expWidth = %d, want 8", result.TypeSpec.ExpWidth)
	}
}

func TestDecodeTypeSpecVisibleString(t *testing.T) {
	// TypeSpecification visiblestring [10] Integer32 = 64
	ts := berutil.EncodeTLV(0x8a, []byte{64})
	resp := buildTypeSpecResponse(false, ts)

	result, err := UnmarshalGetVarAccessResponse(resp)
	if err != nil {
		t.Fatalf("UnmarshalGetVarAccessResponse: %v", err)
	}
	if result.TypeSpec.Tag != tsTagVisibleString {
		t.Errorf("tag = %d, want %d (visiblestring)", result.TypeSpec.Tag, tsTagVisibleString)
	}
	if result.TypeSpec.Size != 64 {
		t.Errorf("size = %d, want 64", result.TypeSpec.Size)
	}
}

func TestDecodeTypeSpecUTCTime(t *testing.T) {
	ts := berutil.EncodeTLV(0x91, nil) // [17] NULL
	resp := buildTypeSpecResponse(false, ts)

	result, err := UnmarshalGetVarAccessResponse(resp)
	if err != nil {
		t.Fatalf("UnmarshalGetVarAccessResponse: %v", err)
	}
	if result.TypeSpec.Tag != tsTagUTCTime {
		t.Errorf("tag = %d, want %d (utctime)", result.TypeSpec.Tag, tsTagUTCTime)
	}
}

func TestDecodeTypeSpecArray(t *testing.T) {
	// Array [1] { numberOfElements [1]=10, elementType [2] EXPLICIT { integer [5] 8 } }
	numElements := berutil.EncodeTLV(0x81, []byte{10}) // [1] IMPLICIT Unsigned32
	innerType := berutil.EncodeTLV(0x85, []byte{8})    // integer [5] 8 bits
	elemType := berutil.EncodeTLV(0xa2, innerType)     // [2] EXPLICIT TypeSpec
	ts := berutil.EncodeTLV(0xa1, append(numElements, elemType...))
	resp := buildTypeSpecResponse(false, ts)

	result, err := UnmarshalGetVarAccessResponse(resp)
	if err != nil {
		t.Fatalf("UnmarshalGetVarAccessResponse: %v", err)
	}
	if result.TypeSpec.Tag != tsTagArray {
		t.Fatalf("tag = %d, want %d (array)", result.TypeSpec.Tag, tsTagArray)
	}
	if result.TypeSpec.Count != 10 {
		t.Errorf("count = %d, want 10", result.TypeSpec.Count)
	}
	if result.TypeSpec.Element == nil {
		t.Fatal("element type is nil")
	}
	if result.TypeSpec.Element.Tag != tsTagInteger || result.TypeSpec.Element.Size != 8 {
		t.Errorf("element = {tag:%d, size:%d}, want {tag:%d, size:8}", result.TypeSpec.Element.Tag, result.TypeSpec.Element.Size, tsTagInteger)
	}
}

func TestDecodeTypeSpecStructure(t *testing.T) {
	// Structure [2] { components [1] { SEQUENCE { name [0] "field1", type [1] { boolean [3] NULL } } } }
	compName := berutil.EncodeTLV(0x80, []byte("field1"))
	boolType := berutil.EncodeTLV(0x83, nil)
	compType := berutil.EncodeTLV(0xa1, boolType)
	comp := berutil.EncodeTLV(0x30, append(compName, compType...))
	components := berutil.EncodeTLV(0xa1, comp) // [1] IMPLICIT SEQUENCE OF
	ts := berutil.EncodeTLV(0xa2, components)
	resp := buildTypeSpecResponse(false, ts)

	result, err := UnmarshalGetVarAccessResponse(resp)
	if err != nil {
		t.Fatalf("UnmarshalGetVarAccessResponse: %v", err)
	}
	if result.TypeSpec.Tag != tsTagStructure {
		t.Fatalf("tag = %d, want %d (structure)", result.TypeSpec.Tag, tsTagStructure)
	}
	if len(result.TypeSpec.Components) != 1 {
		t.Fatalf("components = %d, want 1", len(result.TypeSpec.Components))
	}
	c := result.TypeSpec.Components[0]
	if c.Name != "field1" {
		t.Errorf("component name = %q, want %q", c.Name, "field1")
	}
	if c.Type.Tag != tsTagBoolean {
		t.Errorf("component type tag = %d, want %d (boolean)", c.Type.Tag, tsTagBoolean)
	}
}

func TestDecodeTypeSpecBinaryTime(t *testing.T) {
	// BinaryTime [12] BOOLEAN = TRUE (6-byte form)
	ts := berutil.EncodeTLV(0x8c, []byte{0xff})
	resp := buildTypeSpecResponse(false, ts)

	result, err := UnmarshalGetVarAccessResponse(resp)
	if err != nil {
		t.Fatalf("UnmarshalGetVarAccessResponse: %v", err)
	}
	if result.TypeSpec.Tag != tsTagBinaryTime {
		t.Errorf("tag = %d, want %d", result.TypeSpec.Tag, tsTagBinaryTime)
	}
	if !result.TypeSpec.BinTimeFull {
		t.Error("expected BinTimeFull = true")
	}
}

func TestEncodeDecodeObjectName(t *testing.T) {
	tests := []struct {
		name string
		wire ObjectNameWire
	}{
		{"vmd", ObjectNameWire{Scope: ScopeVMD, ItemID: "myVar"}},
		{"domain", ObjectNameWire{Scope: ScopeDomain, DomainID: "dom1", ItemID: "var1"}},
		{"aa", ObjectNameWire{Scope: ScopeAssociation, ItemID: "aaVar"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := EncodeObjectName(tt.wire)
			if err != nil {
				t.Fatalf("EncodeObjectName: %v", err)
			}
			decoded, err := DecodeObjectName(encoded)
			if err != nil {
				t.Fatalf("DecodeObjectName: %v", err)
			}
			if decoded.Scope != tt.wire.Scope {
				t.Errorf("scope = %d, want %d", decoded.Scope, tt.wire.Scope)
			}
			if decoded.DomainID != tt.wire.DomainID {
				t.Errorf("domainID = %q, want %q", decoded.DomainID, tt.wire.DomainID)
			}
			if decoded.ItemID != tt.wire.ItemID {
				t.Errorf("itemID = %q, want %q", decoded.ItemID, tt.wire.ItemID)
			}
		})
	}
}
