// SPDX-License-Identifier: MIT

package pdu

import (
	"testing"

	"github.com/otfabric/go-mms/internal/berutil"
)

func TestDecodeTypeSpec_MorePrimitives(t *testing.T) {
	cases := []struct {
		name string
		ts   []byte
		tag  int
		size int
	}{
		{"bitstring", berutil.EncodeTLV(0x84, []byte{16}), tsTagBitString, 16},
		{"unsigned", berutil.EncodeTLV(0x86, []byte{32}), tsTagUnsigned, 32},
		{"octetstring", berutil.EncodeTLV(0x89, []byte{8}), tsTagOctetString, 8},
		{"generalizedTime", berutil.EncodeTLV(0x8b, nil), tsTagGeneralizedTime, 0},
		{"bcd", berutil.EncodeTLV(0x8d, []byte{8}), tsTagBCD, 8},
		{"objID", berutil.EncodeTLV(0x8f, nil), tsTagObjID, 0},
		{"mmsString", berutil.EncodeTLV(0x90, []byte{32}), tsTagMmsString, 32},
		{"binaryTimeShort", berutil.EncodeTLV(0x8c, []byte{0x00}), tsTagBinaryTime, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := buildTypeSpecResponse(false, tc.ts)
			result, err := UnmarshalGetVarAccessResponse(resp)
			if err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if result.TypeSpec.Tag != tc.tag {
				t.Fatalf("tag=%d want %d", result.TypeSpec.Tag, tc.tag)
			}
			if tc.size != 0 && result.TypeSpec.Size != tc.size {
				t.Fatalf("size=%d want %d", result.TypeSpec.Size, tc.size)
			}
		})
	}
}

func TestDecodeTypeSpec_TypeName(t *testing.T) {
	name, err := EncodeObjectName(ObjectNameWire{Scope: ScopeVMD, ItemID: "MyType"})
	if err != nil {
		t.Fatal(err)
	}
	ts := berutil.EncodeTLV(0xa0, name) // [0] EXPLICIT ObjectName
	resp := buildTypeSpecResponse(false, ts)
	result, err := UnmarshalGetVarAccessResponse(resp)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if result.TypeSpec.Tag != tsTagTypeName || result.TypeSpec.TypeName == nil {
		t.Fatalf("got %+v", result.TypeSpec)
	}
	if result.TypeSpec.TypeName.ItemID != "MyType" {
		t.Fatalf("ItemID=%q", result.TypeSpec.TypeName.ItemID)
	}
}

func TestDecodeTypeSpec_Errors(t *testing.T) {
	if _, err := DecodeTypeSpec([]byte{0xff}); err == nil {
		t.Fatal("expected decode error")
	}
	if _, err := DecodeTypeSpec(berutil.EncodeTLV(0x9f, nil)); err == nil {
		t.Fatal("expected unknown tag error")
	}
	// Float must be constructed.
	if _, err := DecodeTypeSpec(berutil.EncodeTLV(0x87, []byte{32, 8})); err == nil {
		t.Fatal("expected float constructed error")
	}
	// Array must be constructed.
	if _, err := DecodeTypeSpec(berutil.EncodeTLV(0x81, []byte{1})); err == nil {
		t.Fatal("expected array constructed error")
	}
	// Structure must be constructed.
	if _, err := DecodeTypeSpec(berutil.EncodeTLV(0x82, []byte{1})); err == nil {
		t.Fatal("expected structure constructed error")
	}
	// Bad integer size content.
	if _, err := DecodeTypeSpec(berutil.EncodeTLV(0x85, nil)); err == nil {
		t.Fatal("expected integer size error")
	}
}

func TestDecodeTypeSpec_NestingDepth(t *testing.T) {
	// Build deeply nested arrays exceeding maxTypeSpecNestingDepth.
	inner := berutil.EncodeTLV(0x83, nil) // boolean
	ts := inner
	for i := 0; i < maxTypeSpecNestingDepth+2; i++ {
		num := berutil.EncodeTLV(0x81, []byte{1})
		elem := berutil.EncodeTLV(0xa2, ts)
		ts = berutil.EncodeTLV(0xa1, append(num, elem...))
	}
	if _, err := DecodeTypeSpec(ts); err == nil {
		t.Fatal("expected nesting depth error")
	}
}

func TestDefaultInitiateRequest_ZeroDefaults(t *testing.T) {
	req := DefaultInitiateRequest(0, 0, 0, 0)
	if req.LocalDetailCalling != 65000 {
		t.Fatalf("MaxPDU=%d", req.LocalDetailCalling)
	}
	if req.ProposedMaxServOutstandingCall != 5 || req.ProposedMaxServOutstandingCalled != 5 {
		t.Fatalf("outstanding=%d/%d", req.ProposedMaxServOutstandingCall, req.ProposedMaxServOutstandingCalled)
	}
	if req.ProposedDataStructureNesting != 10 {
		t.Fatalf("nesting=%d", req.ProposedDataStructureNesting)
	}
}

func TestDecodeFloatTypeSpec_Errors(t *testing.T) {
	if _, err := decodeFloatTypeSpec(nil); err == nil {
		t.Fatal("empty")
	}
	// Wrong tag for formatWidth.
	if _, err := decodeFloatTypeSpec(berutil.EncodeTLV(0x04, []byte{32})); err == nil {
		t.Fatal("bad formatWidth tag")
	}
	// Missing exponentWidth.
	onlyFmt := berutil.EncodeTLV(0x02, []byte{32})
	if _, err := decodeFloatTypeSpec(onlyFmt); err == nil {
		t.Fatal("missing exponent")
	}
	// Wrong tag for exponentWidth.
	badExp := append(berutil.EncodeTLV(0x02, []byte{32}), berutil.EncodeTLV(0x04, []byte{8})...)
	if _, err := decodeFloatTypeSpec(badExp); err == nil {
		t.Fatal("bad exponent tag")
	}
	// Trailing bytes.
	trail := append(
		append(berutil.EncodeTLV(0x02, []byte{32}), berutil.EncodeTLV(0x02, []byte{8})...),
		0x00,
	)
	if _, err := decodeFloatTypeSpec(trail); err == nil {
		t.Fatal("trailing")
	}
	ok := append(berutil.EncodeTLV(0x02, []byte{32}), berutil.EncodeTLV(0x02, []byte{8})...)
	ts, err := decodeFloatTypeSpec(ok)
	if err != nil || ts.FormatWidth != 32 || ts.ExpWidth != 8 {
		t.Fatalf("%+v %v", ts, err)
	}
}

func TestDecodeStructureAndArrayTypeSpec_Errors(t *testing.T) {
	if _, err := decodeStructureTypeSpecWithDepth(nil, 0); err == nil {
		t.Fatal("structure missing components")
	}
	if _, err := decodeStructureTypeSpecWithDepth(berutil.EncodeTLV(0xa2, nil), 0); err == nil {
		t.Fatal("wrong components tag")
	}
	// packed + wrong components tag
	packed := append(berutil.EncodeTLV(0x80, []byte{0xff}), berutil.EncodeTLV(0x81, nil)...)
	if _, err := decodeStructureTypeSpecWithDepth(packed, 0); err == nil {
		t.Fatal("structure after packed wrong tag")
	}
	// trailing after components
	trail := append(berutil.EncodeTLV(0xa1, nil), 0x00)
	if _, err := decodeStructureTypeSpecWithDepth(trail, 0); err == nil {
		t.Fatal("structure trailing")
	}
	// component not SEQUENCE
	comps := berutil.EncodeTLV(0xa1, berutil.EncodeTLV(0x04, []byte{1}))
	if _, err := decodeStructureTypeSpecWithDepth(comps, 0); err == nil {
		t.Fatal("component not sequence")
	}

	if _, err := decodeArrayTypeSpecWithDepth(nil, 0); err == nil {
		t.Fatal("array missing count")
	}
	if _, err := decodeArrayTypeSpecWithDepth(berutil.EncodeTLV(0x82, []byte{1}), 0); err == nil {
		t.Fatal("wrong count tag")
	}
	countOnly := berutil.EncodeTLV(0x81, []byte{1})
	if _, err := decodeArrayTypeSpecWithDepth(countOnly, 0); err == nil {
		t.Fatal("missing elementType")
	}
	badElemTag := append(berutil.EncodeTLV(0x81, []byte{1}), berutil.EncodeTLV(0xa3, berutil.EncodeTLV(0x83, nil))...)
	if _, err := decodeArrayTypeSpecWithDepth(badElemTag, 0); err == nil {
		t.Fatal("wrong element tag")
	}
	arrTrail := append(
		append(berutil.EncodeTLV(0x81, []byte{1}), berutil.EncodeTLV(0xa2, berutil.EncodeTLV(0x83, nil))...),
		0x00,
	)
	if _, err := decodeArrayTypeSpecWithDepth(arrTrail, 0); err == nil {
		t.Fatal("array trailing")
	}
	// packed + valid array
	packedArr := append(
		berutil.EncodeTLV(0x80, []byte{0x00}),
		append(berutil.EncodeTLV(0x81, []byte{2}), berutil.EncodeTLV(0xa2, berutil.EncodeTLV(0x83, nil))...)...,
	)
	ts, err := decodeArrayTypeSpecWithDepth(packedArr, 0)
	if err != nil || ts.Count != 2 || ts.Element == nil {
		t.Fatalf("%+v %v", ts, err)
	}

	// component missing type / wrong type tag / trailing
	if _, err := decodeStructComponentWithDepth(nil, 0); err == nil {
		t.Fatal("missing componentType")
	}
	if _, err := decodeStructComponentWithDepth(berutil.EncodeTLV(0xa2, berutil.EncodeTLV(0x83, nil)), 0); err == nil {
		t.Fatal("wrong componentType tag")
	}
	namedTrail := append(
		append(berutil.EncodeTLV(0x80, []byte("c")), berutil.EncodeTLV(0xa1, berutil.EncodeTLV(0x83, nil))...),
		0x00,
	)
	if _, err := decodeStructComponentWithDepth(namedTrail, 0); err == nil {
		t.Fatal("component trailing")
	}
	okComp, err := decodeStructComponentWithDepth(
		append(berutil.EncodeTLV(0x80, []byte("temp")), berutil.EncodeTLV(0xa1, berutil.EncodeTLV(0x83, nil))...),
		0,
	)
	if err != nil || okComp.Name != "temp" || okComp.Type.Tag != tsTagBoolean {
		t.Fatalf("%+v %v", okComp, err)
	}
}
