// SPDX-License-Identifier: MIT

package mms

import (
	"math"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/pdu"
)

func TestTypeSpecFromWire_AllTags(t *testing.T) {
	cases := []struct {
		name string
		wire pdu.TypeSpecWire
		want ValueType
	}{
		{"bool", pdu.TypeSpecWire{Tag: 3}, ValueTypeBoolean},
		{"bitstring", pdu.TypeSpecWire{Tag: 4, Size: 16}, ValueTypeBitString},
		{"integer", pdu.TypeSpecWire{Tag: 5, Size: 32}, ValueTypeInteger},
		{"unsigned", pdu.TypeSpecWire{Tag: 6, Size: 16}, ValueTypeUnsigned},
		{"float", pdu.TypeSpecWire{Tag: 7, FormatWidth: 32, ExpWidth: 8}, ValueTypeFloat},
		{"oid", pdu.TypeSpecWire{Tag: 8}, ValueTypeObjectIdentifier},
		{"octet", pdu.TypeSpecWire{Tag: 9, Size: 8}, ValueTypeOctetString},
		{"visible", pdu.TypeSpecWire{Tag: 10, Size: 32}, ValueTypeVisibleString},
		{"gentime", pdu.TypeSpecWire{Tag: 11}, ValueTypeGeneralizedTime},
		{"bintime", pdu.TypeSpecWire{Tag: 12}, ValueTypeBinaryTime},
		{"bcd", pdu.TypeSpecWire{Tag: 13, Size: 8}, ValueTypeBCD},
		{"mmsstring", pdu.TypeSpecWire{Tag: 16, Size: 64}, ValueTypeMmsString},
		{"utctime", pdu.TypeSpecWire{Tag: 17}, ValueTypeUTCTime},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts, err := typeSpecFromWire(tc.wire)
			if err != nil {
				t.Fatal(err)
			}
			if ts.Type != tc.want {
				t.Fatalf("got %s want %s", ts.Type, tc.want)
			}
		})
	}

	// Named type with/without name + bad scope
	ts, err := typeSpecFromWire(pdu.TypeSpecWire{Tag: 0})
	if err != nil || ts.Type != ValueTypeNamedType || ts.TypeName != nil {
		t.Fatalf("named empty: %+v err=%v", ts, err)
	}
	name := pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "t"}
	ts, err = typeSpecFromWire(pdu.TypeSpecWire{Tag: 0, TypeName: &name})
	if err != nil || ts.TypeName == nil || ts.TypeName.ItemID != "t" {
		t.Fatalf("named: %+v err=%v", ts, err)
	}
	bad := pdu.ObjectNameWire{Scope: 99, ItemID: "x"}
	if _, err := typeSpecFromWire(pdu.TypeSpecWire{Tag: 0, TypeName: &bad}); err == nil {
		t.Fatal("expected bad typeName scope error")
	}

	// Array / structure nesting + error propagation
	elem := pdu.TypeSpecWire{Tag: 5, Size: 32}
	ts, err = typeSpecFromWire(pdu.TypeSpecWire{Tag: 1, Count: 4, Element: &elem})
	if err != nil || ts.Type != ValueTypeArray || ts.Count != 4 || ts.Element == nil {
		t.Fatalf("array: %+v err=%v", ts, err)
	}
	ts, err = typeSpecFromWire(pdu.TypeSpecWire{Tag: 1, Count: 2})
	if err != nil || ts.Element != nil {
		t.Fatalf("array no elem: %+v err=%v", ts, err)
	}
	badElem := pdu.TypeSpecWire{Tag: 99}
	if _, err := typeSpecFromWire(pdu.TypeSpecWire{Tag: 1, Count: 1, Element: &badElem}); err == nil {
		t.Fatal("expected array element error")
	}

	ts, err = typeSpecFromWire(pdu.TypeSpecWire{
		Tag: 2,
		Components: []pdu.StructComponentWire{
			{Name: "a", Type: pdu.TypeSpecWire{Tag: 3}},
			{Name: "b", Type: pdu.TypeSpecWire{Tag: 5, Size: 8}},
		},
	})
	if err != nil || len(ts.Elements) != 2 || ts.Elements[0].Name != "a" {
		t.Fatalf("structure: %+v err=%v", ts, err)
	}
	if _, err := typeSpecFromWire(pdu.TypeSpecWire{
		Tag:        2,
		Components: []pdu.StructComponentWire{{Name: "x", Type: pdu.TypeSpecWire{Tag: 99}}},
	}); err == nil {
		t.Fatal("expected structure component error")
	}

	if _, err := typeSpecFromWire(pdu.TypeSpecWire{Tag: 99}); err == nil {
		t.Fatal("expected unsupported tag")
	}
}

func TestValueDataValueRoundTrip(t *testing.T) {
	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	values := []*Value{
		NewBoolean(true),
		NewInteger(-42),
		NewUnsigned(99),
		NewFloat(math.Pi),               // wide float64
		NewFloat(float64(float32(1.5))), // fits float32
		NewBitStringWithLength([]byte{0xA0}, 4),
		NewOctetString([]byte{1, 2, 3}),
		NewVisibleString("hi"),
		NewMmsString("mms"),
		NewUTCTimeWithQuality(now, 0x0A),
		NewBinaryTime(123456),
		NewGeneralizedTime(now),
		NewBCD(42),
		NewObjectIdentifier([]int{1, 3, 9999}),
		NewReal(2.5),
		NewBooleanArray([]byte{0xF0}, 4),
		NewArray([]*Value{NewInteger(1), NewInteger(2)}),
		NewStructure([]*Value{NewBoolean(false), NewInteger(7)}),
	}
	for i, v := range values {
		dv, err := valueToDataValue(v)
		if err != nil {
			t.Fatalf("[%d] to wire: %v", i, err)
		}
		back, err := dataValueToValue(dv)
		if err != nil {
			t.Fatalf("[%d] from wire: %v", i, err)
		}
		if !v.Equal(back) {
			t.Fatalf("[%d] round-trip mismatch: %v vs %v", i, v, back)
		}
	}

	if _, err := valueToDataValue(nil); err == nil {
		t.Fatal("nil value")
	}
	if _, err := valueToDataValue(&Value{typ: ValueTypeDataAccessError, accessErr: DataAccessErrorObjectNonExistent}); err == nil {
		t.Fatal("access error not writable")
	}
	if _, err := valueToDataValue(&Value{typ: ValueTypeNamedType}); err == nil {
		t.Fatal("named type unsupported")
	}
	if _, err := valuesToDataValues([]*Value{NewInteger(1), nil}); err == nil {
		t.Fatal("nil element")
	}

	if _, err := dataValueToValue(&pdu.DataValue{Tag: 0xFF}); err == nil {
		t.Fatal("unknown tag")
	}
	v, err := dataValueToValue(&pdu.DataValue{Tag: pdu.TagDataAccessError, ErrCode: int(DataAccessErrorObjectNonExistent)})
	if err != nil || v.typ != ValueTypeDataAccessError {
		t.Fatalf("access err value: %v %v", v, err)
	}
	if _, err := dataValueToValue(&pdu.DataValue{Tag: pdu.TagDataAccessError, ErrCode: 99999}); err == nil {
		t.Fatal("bad access err code")
	}
	if _, err := dataValuesToValues([]*pdu.DataValue{{Tag: 0xFF}}); err == nil {
		t.Fatal("bad element in list")
	}
}

func TestVariableSpecAndObjectNameWire(t *testing.T) {
	idx := 2
	vs := VariableSpec{
		Name: ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "v"},
		AlternateAccess: []AccessSelector{
			{Component: "a"},
			{Index: &idx},
			{IndexRange: &IndexRange{Start: 1, Count: 3}},
		},
	}
	w := variableSpecToWire(vs)
	back, err := variableSpecFromWire(w)
	if err != nil {
		t.Fatal(err)
	}
	if back.Name.ItemID != "v" || len(back.AlternateAccess) != 3 {
		t.Fatalf("%+v", back)
	}
	if back.AlternateAccess[0].Component != "a" || back.AlternateAccess[1].Index == nil || *back.AlternateAccess[1].Index != 2 {
		t.Fatalf("selectors: %+v", back.AlternateAccess)
	}
	if back.AlternateAccess[2].IndexRange == nil || back.AlternateAccess[2].IndexRange.Count != 3 {
		t.Fatalf("range: %+v", back.AlternateAccess[2])
	}
	if _, err := variableSpecFromWire(pdu.VariableSpecWire{Name: pdu.ObjectNameWire{Scope: 99, ItemID: "x"}}); err == nil {
		t.Fatal("bad scope")
	}

	for _, scope := range []ObjectScope{ObjectScopeVMD, ObjectScopeDomain, ObjectScopeAssociation} {
		on := ObjectName{Scope: scope, Domain: "dom", ItemID: "item"}
		wn := objectNameToWire(on)
		got, err := objectNameFromWire(wn)
		if err != nil {
			t.Fatal(err)
		}
		if got.Scope != scope || got.ItemID != "item" {
			t.Fatalf("scope %v: %+v", scope, got)
		}
		if _, err := wireNameToObjectName(wn); err != nil {
			t.Fatal(err)
		}
		ws, err := objectScopeToWire(scope)
		if err != nil {
			t.Fatal(err)
		}
		if objectScopeToWireUnchecked(scope) != ws {
			t.Fatal("unchecked mismatch")
		}
	}
	if _, err := objectScopeFromWire(42); err == nil {
		t.Fatal("bad wire scope")
	}
	if _, err := objectScopeToWire(ObjectScope(99)); err == nil {
		t.Fatal("bad public scope")
	}
	if objectScopeToWireUnchecked(ObjectScope(99)) != pdu.ScopeVMD {
		t.Fatal("unchecked default VMD")
	}
}

func TestExtractTypeSpecAndTypeSpecToWire(t *testing.T) {
	ts := TypeSpec{Type: ValueTypeInteger, Size: 32}
	if extractTypeSpec(ts) == nil || extractTypeSpec(&ts) == nil {
		t.Fatal("extract TypeSpec")
	}
	if extractTypeSpec("nope") != nil {
		t.Fatal("extract wrong type")
	}

	all := []TypeSpec{
		{Type: ValueTypeBoolean},
		{Type: ValueTypeInteger, Size: 16},
		{Type: ValueTypeUnsigned, Size: 32},
		{Type: ValueTypeFloat, FormatWidth: 32, ExponentWidth: 8},
		{Type: ValueTypeBitString, Size: 8},
		{Type: ValueTypeOctetString, Size: 4},
		{Type: ValueTypeVisibleString, Size: 10},
		{Type: ValueTypeMmsString, Size: 20},
		{Type: ValueTypeUTCTime},
		{Type: ValueTypeBinaryTime},
		{Type: ValueTypeArray, Count: 3, Element: &TypeSpec{Type: ValueTypeBoolean}},
		{Type: ValueTypeArray, Count: 1},
		{Type: ValueTypeStructure, Elements: []TypeSpecElement{{Name: "x", Type: TypeSpec{Type: ValueTypeBoolean}}}},
		{Type: ValueTypeNamedType, TypeName: &ObjectName{Scope: ObjectScopeVMD, ItemID: "T"}},
	}
	for i, in := range all {
		w, err := typeSpecToWire(in)
		if err != nil {
			t.Fatalf("[%d] toWire: %v", i, err)
		}
		back, err := typeSpecFromWire(w)
		if err != nil {
			t.Fatalf("[%d] fromWire: %v", i, err)
		}
		if back.Type != in.Type {
			t.Fatalf("[%d] type %s vs %s", i, back.Type, in.Type)
		}
	}
	if _, err := typeSpecToWire(TypeSpec{Type: ValueTypeNamedType}); err == nil {
		t.Fatal("nil TypeName")
	}
	if _, err := typeSpecToWire(TypeSpec{Type: ValueTypeNamedType, TypeName: &ObjectName{}}); err == nil {
		t.Fatal("invalid object name")
	}
	if _, err := typeSpecToWire(TypeSpec{Type: ValueTypeReal}); err == nil {
		t.Fatal("unsupported Real")
	}
	if _, err := typeSpecToWire(TypeSpec{
		Type:     ValueTypeStructure,
		Elements: []TypeSpecElement{{Name: "bad", Type: TypeSpec{Type: ValueTypeReal}}},
	}); err == nil {
		t.Fatal("structure element error")
	}
	if _, err := typeSpecToWire(TypeSpec{
		Type:    ValueTypeArray,
		Count:   1,
		Element: &TypeSpec{Type: ValueTypeReal},
	}); err == nil {
		t.Fatal("array element error")
	}
}
