// SPDX-License-Identifier: MIT

package mms

import (
	"testing"
	"time"
)

func TestObjectClassString(t *testing.T) {
	tests := []struct {
		c    ObjectClass
		want string
	}{
		{ObjectClassNamedVariable, "NamedVariable"},
		{ObjectClassNamedVariableList, "NamedVariableList"},
		{ObjectClassDomain, "Domain"},
		{ObjectClass(99), "ObjectClass(99)"},
	}
	for _, tt := range tests {
		if got := tt.c.String(); got != tt.want {
			t.Errorf("ObjectClass(%d).String() = %q, want %q", int(tt.c), got, tt.want)
		}
	}
}

func TestValueTypeString(t *testing.T) {
	tests := []struct {
		vt   ValueType
		want string
	}{
		{ValueTypeBoolean, "Boolean"},
		{ValueTypeInteger, "Integer"},
		{ValueTypeFloat, "Float"},
		{ValueTypeStructure, "Structure"},
		{ValueTypeDataAccessError, "DataAccessError"},
		{ValueType(99), "ValueType(99)"},
	}
	for _, tt := range tests {
		if got := tt.vt.String(); got != tt.want {
			t.Errorf("ValueType(%d).String() = %q, want %q", int(tt.vt), got, tt.want)
		}
	}
}

func TestDataAccessErrorCodeString(t *testing.T) {
	tests := []struct {
		c    DataAccessErrorCode
		want string
	}{
		{DataAccessErrorNone, "None"},
		{DataAccessErrorObjectUndefined, "ObjectUndefined"},
		{DataAccessErrorCode(99), "DataAccessErrorCode(99)"},
	}
	for _, tt := range tests {
		if got := tt.c.String(); got != tt.want {
			t.Errorf("DataAccessErrorCode(%d).String() = %q, want %q", int(tt.c), got, tt.want)
		}
	}
}

func TestErrorClassString(t *testing.T) {
	tests := []struct {
		c    ErrorClass
		want string
	}{
		{ErrorClassVMDState, "VMDState"},
		{ErrorClassAccess, "Access"},
		{ErrorClass(99), "ErrorClass(99)"},
	}
	for _, tt := range tests {
		if got := tt.c.String(); got != tt.want {
			t.Errorf("ErrorClass(%d).String() = %q, want %q", int(tt.c), got, tt.want)
		}
	}
}

func TestVMDLogicalStatusString(t *testing.T) {
	if got := VMDLogicalStatusStateChangesAllowed.String(); got != "StateChangesAllowed" {
		t.Errorf("got %q, want StateChangesAllowed", got)
	}
}

func TestVMDLogicalStatusStringAll(t *testing.T) {
	tests := []struct {
		s    VMDLogicalStatus
		want string
	}{
		{VMDLogicalStatusStateChangesAllowed, "StateChangesAllowed"},
		{VMDLogicalStatusNoStateChanges, "NoStateChanges"},
		{VMDLogicalStatusLimited, "Limited"},
		{VMDLogicalStatus(99), "VMDLogicalStatus(99)"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("VMDLogicalStatus(%d).String() = %q, want %q", int(tt.s), got, tt.want)
		}
	}
}

func TestVMDPhysicalStatusString(t *testing.T) {
	if got := VMDPhysicalStatusOperational.String(); got != "Operational" {
		t.Errorf("got %q, want Operational", got)
	}
}

func TestVMDPhysicalStatusStringAll(t *testing.T) {
	tests := []struct {
		s    VMDPhysicalStatus
		want string
	}{
		{VMDPhysicalStatusOperational, "Operational"},
		{VMDPhysicalStatusPartiallyOperational, "PartiallyOperational"},
		{VMDPhysicalStatusInoperable, "Inoperable"},
		{VMDPhysicalStatusNeedsCommissioning, "NeedsCommissioning"},
		{VMDPhysicalStatus(99), "VMDPhysicalStatus(99)"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("VMDPhysicalStatus(%d).String() = %q, want %q", int(tt.s), got, tt.want)
		}
	}
}

func TestObjectScopeStringAll(t *testing.T) {
	tests := []struct {
		s    ObjectScope
		want string
	}{
		{ObjectScopeVMD, "VMD"},
		{ObjectScopeDomain, "Domain"},
		{ObjectScopeAssociation, "Association"},
		{ObjectScope(99), "ObjectScope(99)"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("ObjectScope(%d).String() = %q, want %q", int(tt.s), got, tt.want)
		}
	}
}

func TestShallowCompatibleNil(t *testing.T) {
	ts := TypeSpec{Type: ValueTypeBoolean}
	if ts.ShallowCompatible(nil) {
		t.Error("ShallowCompatible(nil) should be false")
	}
}

func TestShallowCompatibleWrongType(t *testing.T) {
	ts := TypeSpec{Type: ValueTypeBoolean}
	v := NewInteger(1)
	if ts.ShallowCompatible(v) {
		t.Error("ShallowCompatible(wrong type) should be false")
	}
}

func TestShallowCompatibleStructure(t *testing.T) {
	ts := TypeSpec{
		Type: ValueTypeStructure,
		Elements: []TypeSpecElement{
			{Name: "a", Type: TypeSpec{Type: ValueTypeBoolean}},
			{Name: "b", Type: TypeSpec{Type: ValueTypeInteger}},
		},
	}

	match := NewStructure([]*Value{NewBoolean(true), NewInteger(1)})
	if !ts.ShallowCompatible(match) {
		t.Error("ShallowCompatible(matching struct) should be true")
	}

	mismatch := NewStructure([]*Value{NewBoolean(true)})
	if ts.ShallowCompatible(mismatch) {
		t.Error("ShallowCompatible(wrong count struct) should be false")
	}
}

func TestShallowCompatibleArray(t *testing.T) {
	elemType := TypeSpec{Type: ValueTypeInteger}
	ts := TypeSpec{
		Type:    ValueTypeArray,
		Count:   3,
		Element: &elemType,
	}

	match := NewArray([]*Value{NewInteger(1), NewInteger(2), NewInteger(3)})
	if !ts.ShallowCompatible(match) {
		t.Error("ShallowCompatible(matching array) should be true")
	}

	mismatch := NewArray([]*Value{NewInteger(1), NewInteger(2)})
	if ts.ShallowCompatible(mismatch) {
		t.Error("ShallowCompatible(wrong count array) should be false")
	}
}

func TestDefaultValueAllTypes(t *testing.T) {
	scalar := []ValueType{
		ValueTypeBoolean, ValueTypeInteger, ValueTypeUnsigned, ValueTypeFloat,
		ValueTypeBitString, ValueTypeOctetString, ValueTypeVisibleString,
		ValueTypeMmsString, ValueTypeUTCTime, ValueTypeBinaryTime,
		ValueTypeGeneralizedTime, ValueTypeBCD, ValueTypeObjectIdentifier,
		ValueTypeReal, ValueTypeBooleanArray,
	}
	for _, vt := range scalar {
		ts := TypeSpec{Type: vt}
		v := ts.DefaultValue()
		if v == nil {
			t.Errorf("DefaultValue for %s should not be nil", vt)
			continue
		}
		if v.Type() != vt {
			t.Errorf("DefaultValue type = %s, want %s", v.Type(), vt)
		}
	}
}

func TestDefaultValueBitStringWithSize(t *testing.T) {
	ts := TypeSpec{Type: ValueTypeBitString, Size: 16}
	v := ts.DefaultValue()
	if v == nil {
		t.Fatal("DefaultValue should not be nil")
	}
	bits, ok := v.BitString()
	if !ok || len(bits) != 2 {
		t.Errorf("BitString = (%d bytes, %v), want 2 bytes", len(bits), ok)
	}
}

func TestDefaultValueStructure(t *testing.T) {
	ts := TypeSpec{
		Type: ValueTypeStructure,
		Elements: []TypeSpecElement{
			{Name: "a", Type: TypeSpec{Type: ValueTypeBoolean}},
			{Name: "b", Type: TypeSpec{Type: ValueTypeInteger}},
		},
	}
	v := ts.DefaultValue()
	if v == nil {
		t.Fatal("DefaultValue should not be nil")
	}
	elems, ok := v.Structure()
	if !ok || len(elems) != 2 {
		t.Fatalf("Structure = %v, %v, want 2 elements", elems, ok)
	}
	if elems[0].Type() != ValueTypeBoolean {
		t.Errorf("elem[0] type = %s, want Boolean", elems[0].Type())
	}
}

func TestDefaultValueStructureWithUnsupported(t *testing.T) {
	ts := TypeSpec{
		Type: ValueTypeStructure,
		Elements: []TypeSpecElement{
			{Name: "a", Type: TypeSpec{Type: ValueType(200)}},
		},
	}
	v := ts.DefaultValue()
	if v != nil {
		t.Error("DefaultValue with unsupported element type should be nil")
	}
}

func TestDefaultValueArray(t *testing.T) {
	elemTS := TypeSpec{Type: ValueTypeInteger}
	ts := TypeSpec{
		Type:    ValueTypeArray,
		Count:   3,
		Element: &elemTS,
	}
	v := ts.DefaultValue()
	if v == nil {
		t.Fatal("DefaultValue should not be nil")
	}
	elems, ok := v.ArrayElements()
	if !ok || len(elems) != 3 {
		t.Fatalf("Array = %v, %v, want 3 elements", elems, ok)
	}
}

func TestDefaultValueArrayNilElement(t *testing.T) {
	ts := TypeSpec{
		Type:  ValueTypeArray,
		Count: 3,
	}
	v := ts.DefaultValue()
	if v == nil {
		t.Fatal("DefaultValue should not be nil for nil Element")
	}
	elems, ok := v.ArrayElements()
	if !ok || len(elems) != 0 {
		t.Errorf("Array with nil Element = %d elems, want 0", len(elems))
	}
}

func TestDefaultValueArrayUnsupportedElement(t *testing.T) {
	badElem := TypeSpec{Type: ValueType(200)}
	ts := TypeSpec{
		Type:    ValueTypeArray,
		Count:   2,
		Element: &badElem,
	}
	v := ts.DefaultValue()
	if v != nil {
		t.Error("DefaultValue with unsupported array element should be nil")
	}
}

func TestDefaultValueUnsupportedType(t *testing.T) {
	ts := TypeSpec{Type: ValueType(200)}
	v := ts.DefaultValue()
	if v != nil {
		t.Error("DefaultValue for unknown type should be nil")
	}
}

func TestResolveIndexOnStructure(t *testing.T) {
	ts := TypeSpec{
		Type: ValueTypeStructure,
		Elements: []TypeSpecElement{
			{Name: "a", Type: TypeSpec{Type: ValueTypeBoolean}},
			{Name: "b", Type: TypeSpec{Type: ValueTypeInteger}},
		},
	}
	got, err := ts.Resolve(SelectIndex(1))
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != ValueTypeInteger {
		t.Errorf("Resolve(1) = %s, want Integer", got.Type)
	}
}

func TestResolveComponentOnStructure(t *testing.T) {
	ts := TypeSpec{
		Type: ValueTypeStructure,
		Elements: []TypeSpecElement{
			{Name: "alpha", Type: TypeSpec{Type: ValueTypeBoolean}},
			{Name: "beta", Type: TypeSpec{Type: ValueTypeFloat}},
		},
	}
	got, err := ts.Resolve(SelectComponent("beta"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != ValueTypeFloat {
		t.Errorf("Resolve(beta) = %s, want Float", got.Type)
	}
}

func TestResolveComponentNotFound(t *testing.T) {
	ts := TypeSpec{
		Type: ValueTypeStructure,
		Elements: []TypeSpecElement{
			{Name: "x", Type: TypeSpec{Type: ValueTypeBoolean}},
		},
	}
	_, err := ts.Resolve(SelectComponent("notexist"))
	if err == nil {
		t.Error("expected error for unknown component")
	}
}

func TestResolveIndexOnArray(t *testing.T) {
	elemTS := TypeSpec{Type: ValueTypeInteger}
	ts := TypeSpec{
		Type:    ValueTypeArray,
		Count:   5,
		Element: &elemTS,
	}
	got, err := ts.Resolve(SelectIndex(0))
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != ValueTypeInteger {
		t.Errorf("Resolve(0) on array = %s, want Integer", got.Type)
	}
}

func TestResolveIndexOutOfBoundsStructure(t *testing.T) {
	ts := TypeSpec{
		Type: ValueTypeStructure,
		Elements: []TypeSpecElement{
			{Name: "a", Type: TypeSpec{Type: ValueTypeBoolean}},
		},
	}
	_, err := ts.Resolve(SelectIndex(5))
	if err == nil {
		t.Error("expected error for out-of-bounds index on structure")
	}
}

func TestResolveOnNonComposite(t *testing.T) {
	ts := TypeSpec{Type: ValueTypeInteger}
	_, err := ts.Resolve(SelectIndex(0))
	if err == nil {
		t.Error("expected error for index on scalar type")
	}
}

func TestResolveRangeOnArray(t *testing.T) {
	elemTS := TypeSpec{Type: ValueTypeInteger}
	ts := TypeSpec{
		Type:    ValueTypeArray,
		Count:   5,
		Element: &elemTS,
	}
	got, err := ts.Resolve(SelectRange(1, 3))
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != ValueTypeArray {
		t.Errorf("Resolve range = %s, want Array", got.Type)
	}
}

func TestResolveNilTypeSpec(t *testing.T) {
	var ts *TypeSpec
	_, err := ts.Resolve(SelectIndex(0))
	if err == nil {
		t.Error("expected error for nil TypeSpec")
	}
}

func TestValueGetEmptySelectors(t *testing.T) {
	v := NewInteger(42)
	got, err := v.Get()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(v) {
		t.Error("Get() with no selectors should return the value itself")
	}
}

func TestValueGetComponentOnStructure(t *testing.T) {
	s := NewStructure([]*Value{NewBoolean(true), NewInteger(5)})
	_, err := s.Get(SelectComponent("a"))
	if err == nil {
		t.Error("Get(Component) should require TypeSpec context")
	}
}

func TestValueGetComponentOnNonStructure(t *testing.T) {
	v := NewInteger(42)
	_, err := v.Get(SelectComponent("a"))
	if err == nil {
		t.Error("Get(Component) on non-structure should fail")
	}
}

func TestValueGetIndexOnStructure(t *testing.T) {
	s := NewStructure([]*Value{NewBoolean(true), NewInteger(5)})
	got, err := s.Get(SelectIndex(1))
	if err != nil {
		t.Fatal(err)
	}
	i, ok := got.Int64()
	if !ok || i != 5 {
		t.Errorf("Get(1) = %v, want 5", got)
	}
}

func TestValueGetIndexOnArray(t *testing.T) {
	a := NewArray([]*Value{NewInteger(10), NewInteger(20)})
	got, err := a.Get(SelectIndex(1))
	if err != nil {
		t.Fatal(err)
	}
	i, ok := got.Int64()
	if !ok || i != 20 {
		t.Errorf("Get(1) on array = %v, want 20", got)
	}
}

func TestValueGetIndexOutOfBounds(t *testing.T) {
	s := NewStructure([]*Value{NewBoolean(true)})
	_, err := s.Get(SelectIndex(5))
	if err == nil {
		t.Error("expected error for out-of-bounds index")
	}
}

func TestValueGetIndexOnScalar(t *testing.T) {
	v := NewInteger(42)
	_, err := v.Get(SelectIndex(0))
	if err == nil {
		t.Error("expected error for index on scalar")
	}
}

func TestValueGetRangeOnArray(t *testing.T) {
	a := NewArray([]*Value{NewInteger(1), NewInteger(2), NewInteger(3), NewInteger(4)})
	got, err := a.Get(SelectRange(1, 2))
	if err != nil {
		t.Fatal(err)
	}
	elems, ok := got.ArrayElements()
	if !ok || len(elems) != 2 {
		t.Fatalf("Get range = %d elements, want 2", len(elems))
	}
}

func TestValueGetRangeOnNonArray(t *testing.T) {
	v := NewInteger(42)
	_, err := v.Get(SelectRange(0, 1))
	if err == nil {
		t.Error("expected error for range on scalar")
	}
}

func TestValueGetRangeOutOfBounds(t *testing.T) {
	a := NewArray([]*Value{NewInteger(1)})
	_, err := a.Get(SelectRange(0, 5))
	if err == nil {
		t.Error("expected error for out-of-bounds range")
	}
}

func TestValueGetNilValue(t *testing.T) {
	var v *Value
	_, err := v.Get(SelectIndex(0))
	if err == nil {
		t.Error("expected error for Get on nil value")
	}
}

func TestValueGetNoFieldSet(t *testing.T) {
	v := NewInteger(42)
	_, err := v.Get(AccessSelector{})
	if err == nil {
		t.Error("expected error for empty selector")
	}
}

func TestValueUint32Overflow(t *testing.T) {
	v := NewUnsigned(uint64(1) << 40)
	_, ok := v.Uint32()
	if ok {
		t.Error("Uint32() should return false for value exceeding 32 bits")
	}
}

func TestValueGetArrayIndexOutOfBounds(t *testing.T) {
	a := NewArray([]*Value{NewInteger(1)})
	_, err := a.Get(SelectIndex(-1))
	if err == nil {
		t.Error("expected error for negative index")
	}
	_, err = a.Get(SelectIndex(5))
	if err == nil {
		t.Error("expected error for out-of-bounds index")
	}
}

func TestChildByNameNotFound(t *testing.T) {
	ts := TypeSpec{
		Type: ValueTypeStructure,
		Elements: []TypeSpecElement{
			{Name: "x", Type: TypeSpec{Type: ValueTypeBoolean}},
		},
	}
	_, ok := ts.ChildByName("nonexistent")
	if ok {
		t.Error("expected false for nonexistent child")
	}
}

func TestChildByNameFound(t *testing.T) {
	ts := TypeSpec{
		Type: ValueTypeStructure,
		Elements: []TypeSpecElement{
			{Name: "alpha", Type: TypeSpec{Type: ValueTypeBoolean}},
			{Name: "beta", Type: TypeSpec{Type: ValueTypeFloat}},
		},
	}
	child, ok := ts.ChildByName("beta")
	if !ok {
		t.Fatal("expected true for existing child")
	}
	if child.Type != ValueTypeFloat {
		t.Errorf("ChildByName(beta) = %s, want Float", child.Type)
	}
}

func TestChildByNameOnNonStructure(t *testing.T) {
	ts := TypeSpec{Type: ValueTypeInteger}
	_, ok := ts.ChildByName("x")
	if ok {
		t.Error("ChildByName on non-structure should return false")
	}
}

func TestChildByIndexValid(t *testing.T) {
	ts := TypeSpec{
		Type: ValueTypeStructure,
		Elements: []TypeSpecElement{
			{Name: "a", Type: TypeSpec{Type: ValueTypeBoolean}},
			{Name: "b", Type: TypeSpec{Type: ValueTypeInteger}},
		},
	}
	child, ok := ts.ChildByIndex(0)
	if !ok {
		t.Fatal("expected true")
	}
	if child.Type != ValueTypeBoolean {
		t.Errorf("ChildByIndex(0) = %s, want Boolean", child.Type)
	}
}

func TestChildByIndexOutOfBounds(t *testing.T) {
	ts := TypeSpec{
		Type: ValueTypeStructure,
		Elements: []TypeSpecElement{
			{Name: "a", Type: TypeSpec{Type: ValueTypeBoolean}},
		},
	}
	_, ok := ts.ChildByIndex(5)
	if ok {
		t.Error("expected false for out-of-bounds index")
	}
}

func TestChildByIndexOnArray(t *testing.T) {
	elemTS := TypeSpec{Type: ValueTypeInteger}
	ts := TypeSpec{
		Type:    ValueTypeArray,
		Count:   5,
		Element: &elemTS,
	}
	child, ok := ts.ChildByIndex(0)
	if !ok {
		t.Fatal("expected true")
	}
	if child.Type != ValueTypeInteger {
		t.Errorf("ChildByIndex(0) on array = %s, want Integer", child.Type)
	}
}

func TestChildByIndexOnArrayOutOfBounds(t *testing.T) {
	elemTS := TypeSpec{Type: ValueTypeInteger}
	ts := TypeSpec{
		Type:    ValueTypeArray,
		Count:   2,
		Element: &elemTS,
	}
	_, ok := ts.ChildByIndex(5)
	if ok {
		t.Error("expected false for out-of-bounds array index")
	}
}

func TestChildByIndexOnNonComposite(t *testing.T) {
	ts := TypeSpec{Type: ValueTypeInteger}
	_, ok := ts.ChildByIndex(0)
	if ok {
		t.Error("expected false for ChildByIndex on non-composite type")
	}
}

func TestErrorClassStringFallback(t *testing.T) {
	c := ErrorClass(-1)
	got := c.String()
	if got == "" {
		t.Error("ErrorClass(-1).String() should not be empty")
	}
}

func TestValueTypeStringAll(t *testing.T) {
	types := []struct {
		vt   ValueType
		want string
	}{
		{ValueTypeBoolean, "Boolean"},
		{ValueTypeInteger, "Integer"},
		{ValueTypeUnsigned, "Unsigned"},
		{ValueTypeFloat, "Float"},
		{ValueTypeBitString, "BitString"},
		{ValueTypeOctetString, "OctetString"},
		{ValueTypeVisibleString, "VisibleString"},
		{ValueTypeMmsString, "MmsString"},
		{ValueTypeUTCTime, "UTCTime"},
		{ValueTypeBinaryTime, "BinaryTime"},
		{ValueTypeStructure, "Structure"},
		{ValueTypeArray, "Array"},
		{ValueTypeGeneralizedTime, "GeneralizedTime"},
		{ValueTypeBCD, "BCD"},
		{ValueTypeObjectIdentifier, "ObjectIdentifier"},
		{ValueTypeReal, "Real"},
		{ValueTypeBooleanArray, "BooleanArray"},
		{ValueTypeDataAccessError, "DataAccessError"},
	}
	for _, tt := range types {
		if got := tt.vt.String(); got != tt.want {
			t.Errorf("ValueType(%d).String() = %q, want %q", int(tt.vt), got, tt.want)
		}
	}
}

func TestNewUTCTimeCopy(t *testing.T) {
	orig := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	v := NewUTCTime(orig)
	got, ok := v.UTCTime()
	if !ok || !got.Equal(orig) {
		t.Errorf("UTCTime() = %v, want %v", got, orig)
	}
}

func TestSelectorsCreateCorrectFields(t *testing.T) {
	comp := SelectComponent("field")
	if comp.Component != "field" {
		t.Errorf("SelectComponent = %q, want field", comp.Component)
	}
	idx := SelectIndex(5)
	if idx.Index == nil || *idx.Index != 5 {
		t.Error("SelectIndex(5) should set Index to 5")
	}
	rng := SelectRange(2, 4)
	if rng.IndexRange == nil || rng.IndexRange.Start != 2 || rng.IndexRange.Count != 4 {
		t.Error("SelectRange(2,4) should set IndexRange correctly")
	}
}
