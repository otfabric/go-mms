package mms

import (
	"testing"
	"time"
)

func TestValueClone_Nil(t *testing.T) {
	var v *Value
	if got := v.Clone(); got != nil {
		t.Errorf("Clone(nil) = %v, want nil", got)
	}
}

func TestValueClone_Scalar(t *testing.T) {
	original := NewInteger(42)
	cloned := original.Clone()

	if !cloned.Equal(original) {
		t.Error("cloned value should equal original")
	}

	// Verify independence
	original.intVal = 99
	i, ok := cloned.Int64()
	if !ok || i != 42 {
		t.Errorf("clone should be independent, got %d", i)
	}
}

func TestValueClone_DeepStructure(t *testing.T) {
	original := NewStructure([]*Value{
		NewFloat(1.5),
		NewArray([]*Value{NewInteger(10), NewInteger(20)}),
	})

	cloned := original.Clone()
	if !cloned.Equal(original) {
		t.Error("cloned structure should equal original")
	}

	// Mutate original's nested array element
	original.elementsVal[1].elementsVal[0].intVal = 999
	arr, _ := cloned.ArrayElements()
	if arr != nil {
		t.Error("clone should not have ArrayElements at top level")
	}
	elems, ok := cloned.Structure()
	if !ok || len(elems) != 2 {
		t.Fatal("expected structure with 2 elements")
	}
	nested, ok := elems[1].ArrayElements()
	if !ok || len(nested) != 2 {
		t.Fatal("expected nested array with 2 elements")
	}
	v, ok := nested[0].Int64()
	if !ok || v != 10 {
		t.Errorf("nested clone value should be 10, got %d", v)
	}
}

func TestValueClone_ByteSlice(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03}
	original := NewOctetString(data)
	cloned := original.Clone()

	data[0] = 0xff
	original.bytesVal[1] = 0xfe

	got, ok := cloned.OctetString()
	if !ok {
		t.Fatal("expected OctetString")
	}
	if got[0] != 0x01 || got[1] != 0x02 || got[2] != 0x03 {
		t.Errorf("clone should be independent, got %v", got)
	}
}

func TestValueEqual_SameType(t *testing.T) {
	tests := []struct {
		name string
		a, b *Value
		want bool
	}{
		{"bool true==true", NewBoolean(true), NewBoolean(true), true},
		{"bool true!=false", NewBoolean(true), NewBoolean(false), false},
		{"int eq", NewInteger(42), NewInteger(42), true},
		{"int neq", NewInteger(42), NewInteger(43), false},
		{"uint eq", NewUnsigned(100), NewUnsigned(100), true},
		{"float eq", NewFloat(3.14), NewFloat(3.14), true},
		{"float neq", NewFloat(3.14), NewFloat(2.71), false},
		{"string eq", NewVisibleString("hello"), NewVisibleString("hello"), true},
		{"string neq", NewVisibleString("hello"), NewVisibleString("world"), false},
		{"octet eq", NewOctetString([]byte{1, 2}), NewOctetString([]byte{1, 2}), true},
		{"octet neq", NewOctetString([]byte{1, 2}), NewOctetString([]byte{3, 4}), false},
		{"utc eq",
			NewUTCTime(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)),
			NewUTCTime(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)),
			true},
		{"array eq",
			NewArray([]*Value{NewInteger(1), NewInteger(2)}),
			NewArray([]*Value{NewInteger(1), NewInteger(2)}),
			true},
		{"array len neq",
			NewArray([]*Value{NewInteger(1)}),
			NewArray([]*Value{NewInteger(1), NewInteger(2)}),
			false},
		{"struct eq",
			NewStructure([]*Value{NewBoolean(true), NewFloat(1.0)}),
			NewStructure([]*Value{NewBoolean(true), NewFloat(1.0)}),
			true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Equal(tt.b); got != tt.want {
				t.Errorf("Equal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValueEqual_DifferentType(t *testing.T) {
	if NewInteger(42).Equal(NewFloat(42)) {
		t.Error("different types should not be equal")
	}
}

func TestValueEqual_Nil(t *testing.T) {
	var a, b *Value
	if !a.Equal(b) {
		t.Error("two nil values should be equal")
	}
	if NewInteger(42).Equal(nil) {
		t.Error("non-nil should not equal nil")
	}
	if a.Equal(NewInteger(42)) {
		t.Error("nil should not equal non-nil")
	}
}

func TestValueString(t *testing.T) {
	tests := []struct {
		name string
		v    *Value
		want string
	}{
		{"nil", nil, "<nil>"},
		{"bool", NewBoolean(true), "true"},
		{"int", NewInteger(-42), "-42"},
		{"uint", NewUnsigned(100), "100"},
		{"float", NewFloat(3.14), "3.14"},
		{"bitstring", NewBitStringWithLength([]byte{0xff}, 5), "BitString(5 bits)"},
		{"octet", NewOctetString([]byte{1, 2, 3}), "OctetString(3 bytes)"},
		{"visible", NewVisibleString("hello"), `"hello"`},
		{"mms", NewMmsString("world"), `MmsString("world")`},
		{"binary_time", NewBinaryTime(12345), "BinaryTime(12345 ms)"},
		{"struct", NewStructure([]*Value{NewInteger(1), NewBoolean(true)}), "{1, true}"},
		{"array", NewArray([]*Value{NewInteger(10), NewInteger(20)}), "[10, 20]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.v.String()
			if got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTypeSpecChildByName(t *testing.T) {
	ts := TypeSpec{
		Type: ValueTypeStructure,
		Elements: []TypeSpecElement{
			{Name: "temperature", Type: TypeSpec{Type: ValueTypeFloat, FormatWidth: 64, ExponentWidth: 11}},
			{Name: "valid", Type: TypeSpec{Type: ValueTypeBoolean}},
		},
	}

	child, ok := ts.ChildByName("temperature")
	if !ok {
		t.Fatal("expected to find 'temperature'")
	}
	if child.Type != ValueTypeFloat {
		t.Errorf("expected Float, got %v", child.Type)
	}

	child, ok = ts.ChildByName("valid")
	if !ok {
		t.Fatal("expected to find 'valid'")
	}
	if child.Type != ValueTypeBoolean {
		t.Errorf("expected Boolean, got %v", child.Type)
	}

	_, ok = ts.ChildByName("nonexistent")
	if ok {
		t.Error("should not find 'nonexistent'")
	}

	intTs := TypeSpec{Type: ValueTypeInteger}
	_, ok = intTs.ChildByName("anything")
	if ok {
		t.Error("non-structure should return false")
	}
}

func TestTypeSpecChildByIndex(t *testing.T) {
	structTs := TypeSpec{
		Type: ValueTypeStructure,
		Elements: []TypeSpecElement{
			{Name: "a", Type: TypeSpec{Type: ValueTypeInteger}},
			{Name: "b", Type: TypeSpec{Type: ValueTypeFloat}},
		},
	}

	child, ok := structTs.ChildByIndex(0)
	if !ok || child.Type != ValueTypeInteger {
		t.Error("expected Integer at index 0")
	}
	child, ok = structTs.ChildByIndex(1)
	if !ok || child.Type != ValueTypeFloat {
		t.Error("expected Float at index 1")
	}
	_, ok = structTs.ChildByIndex(2)
	if ok {
		t.Error("should be out of bounds")
	}

	elemType := TypeSpec{Type: ValueTypeBoolean}
	arrayTs := TypeSpec{
		Type:    ValueTypeArray,
		Count:   5,
		Element: &elemType,
	}
	child, ok = arrayTs.ChildByIndex(3)
	if !ok || child.Type != ValueTypeBoolean {
		t.Error("expected Boolean from array")
	}
	_, ok = arrayTs.ChildByIndex(5)
	if ok {
		t.Error("should be out of bounds for array")
	}
}

func TestTypeSpecShallowCompatible(t *testing.T) {
	ts := TypeSpec{
		Type: ValueTypeStructure,
		Elements: []TypeSpecElement{
			{Name: "a", Type: TypeSpec{Type: ValueTypeInteger}},
			{Name: "b", Type: TypeSpec{Type: ValueTypeFloat}},
		},
	}

	if !ts.ShallowCompatible(NewStructure([]*Value{NewInteger(1), NewFloat(2.0)})) {
		t.Error("should be compatible")
	}
	if ts.ShallowCompatible(NewStructure([]*Value{NewInteger(1)})) {
		t.Error("wrong element count should be incompatible")
	}
	if ts.ShallowCompatible(NewInteger(42)) {
		t.Error("wrong type should be incompatible")
	}
	if ts.ShallowCompatible(nil) {
		t.Error("nil should be incompatible")
	}

	arrTs := TypeSpec{Type: ValueTypeArray, Count: 3, Element: &TypeSpec{Type: ValueTypeInteger}}
	if !arrTs.ShallowCompatible(NewArray([]*Value{NewInteger(1), NewInteger(2), NewInteger(3)})) {
		t.Error("should be compatible with matching array")
	}
	if arrTs.ShallowCompatible(NewArray([]*Value{NewInteger(1)})) {
		t.Error("wrong count should be incompatible")
	}
}

func TestTypeSpecDefaultValue(t *testing.T) {
	tests := []struct {
		name string
		ts   TypeSpec
	}{
		{"bool", TypeSpec{Type: ValueTypeBoolean}},
		{"int", TypeSpec{Type: ValueTypeInteger, Size: 32}},
		{"uint", TypeSpec{Type: ValueTypeUnsigned, Size: 16}},
		{"float", TypeSpec{Type: ValueTypeFloat, FormatWidth: 64, ExponentWidth: 11}},
		{"string", TypeSpec{Type: ValueTypeVisibleString, Size: 255}},
		{"mms", TypeSpec{Type: ValueTypeMmsString}},
		{"octet", TypeSpec{Type: ValueTypeOctetString}},
		{"bitstring", TypeSpec{Type: ValueTypeBitString, Size: 8}},
		{"utc", TypeSpec{Type: ValueTypeUTCTime}},
		{"binary", TypeSpec{Type: ValueTypeBinaryTime}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := tt.ts.DefaultValue()
			if v == nil {
				t.Fatal("DefaultValue returned nil")
			}
			if v.Type() != tt.ts.Type {
				t.Errorf("type mismatch: got %v, want %v", v.Type(), tt.ts.Type)
			}
		})
	}
}

func TestTypeSpecDefaultValue_NestedStructure(t *testing.T) {
	ts := TypeSpec{
		Type: ValueTypeStructure,
		Elements: []TypeSpecElement{
			{Name: "temp", Type: TypeSpec{Type: ValueTypeFloat, FormatWidth: 32, ExponentWidth: 8}},
			{Name: "readings", Type: TypeSpec{
				Type:    ValueTypeArray,
				Count:   3,
				Element: &TypeSpec{Type: ValueTypeInteger, Size: 32},
			}},
		},
	}

	v := ts.DefaultValue()
	if v == nil {
		t.Fatal("DefaultValue returned nil")
	}
	if !ts.ShallowCompatible(v) {
		t.Error("default value should be compatible with its spec")
	}

	elems, ok := v.Structure()
	if !ok || len(elems) != 2 {
		t.Fatal("expected 2-element structure")
	}
	f, ok := elems[0].Float64()
	if !ok || f != 0 {
		t.Errorf("temp default = %v/%v, want 0", f, ok)
	}
	arr, ok := elems[1].ArrayElements()
	if !ok || len(arr) != 3 {
		t.Fatal("expected 3-element array")
	}
	for i, e := range arr {
		v, ok := e.Int64()
		if !ok || v != 0 {
			t.Errorf("array[%d] default = %v/%v, want 0", i, v, ok)
		}
	}
}

func TestValueEqual_BitStringSameBytesButDifferentLen(t *testing.T) {
	a := NewBitStringWithLength([]byte{0xff}, 5)
	b := NewBitStringWithLength([]byte{0xff}, 8)
	if a.Equal(b) {
		t.Error("same bytes but different bit lengths should not be equal")
	}
	c := NewBitStringWithLength([]byte{0xff}, 5)
	if !a.Equal(c) {
		t.Error("same bytes and same bit length should be equal")
	}
}

func TestValueClone_UTCTime(t *testing.T) {
	ts := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	original := NewUTCTime(ts)
	cloned := original.Clone()

	if !cloned.Equal(original) {
		t.Error("cloned UTCTime should equal original")
	}
	got, ok := cloned.UTCTime()
	if !ok || !got.Equal(ts) {
		t.Errorf("clone time = %v, want %v", got, ts)
	}
}

func TestValueClone_BinaryTime(t *testing.T) {
	original := NewBinaryTime(123456789)
	cloned := original.Clone()
	if !cloned.Equal(original) {
		t.Error("cloned BinaryTime should equal original")
	}
	got, ok := cloned.BinaryTime()
	if !ok || got != 123456789 {
		t.Errorf("clone ms = %d, want 123456789", got)
	}
}

func TestValueClone_BitString(t *testing.T) {
	data := []byte{0xab, 0xcd}
	original := NewBitStringWithLength(data, 12)
	cloned := original.Clone()
	if !cloned.Equal(original) {
		t.Error("cloned BitString should equal original")
	}
	data[0] = 0x00
	original.bytesVal[1] = 0x00
	b, ok := cloned.BitString()
	l, _ := cloned.BitStringLength()
	if !ok || l != 12 || b[0] != 0xab || b[1] != 0xcd {
		t.Errorf("clone should be independent, got %x len=%d", b, l)
	}
}

func TestValueClone_NestedStrings(t *testing.T) {
	original := NewStructure([]*Value{
		NewVisibleString("hello"),
		NewMmsString("world"),
	})
	cloned := original.Clone()
	if !cloned.Equal(original) {
		t.Error("cloned structure with strings should equal original")
	}
	original.elementsVal[0].stringVal = "modified"
	s, ok := cloned.elementsVal[0].VisibleString()
	if !ok || s != "hello" {
		t.Errorf("clone should be independent, got %q", s)
	}
}

func TestTypeSpecDefaultValue_ZeroCountArray(t *testing.T) {
	ts := TypeSpec{Type: ValueTypeArray, Count: 0, Element: &TypeSpec{Type: ValueTypeInteger}}
	v := ts.DefaultValue()
	if v == nil {
		t.Fatal("DefaultValue returned nil for zero-count array")
	}
	elems, ok := v.ArrayElements()
	if !ok {
		t.Fatal("expected array type")
	}
	if len(elems) != 0 {
		t.Errorf("expected empty array, got %d elements", len(elems))
	}
}

func TestTypeSpecDefaultValue_NilElementZeroCount(t *testing.T) {
	ts := TypeSpec{Type: ValueTypeArray, Count: 0, Element: nil}
	v := ts.DefaultValue()
	if v == nil {
		t.Fatal("expected empty array, got nil")
	}
	elems, ok := v.ArrayElements()
	if !ok || len(elems) != 0 {
		t.Errorf("expected empty array for nil element with zero count")
	}

	ts2 := TypeSpec{Type: ValueTypeArray, Count: 2, Element: nil}
	v2 := ts2.DefaultValue()
	if v2 == nil {
		t.Fatal("expected empty array for nil element with non-zero count")
	}
	elems2, ok := v2.ArrayElements()
	if !ok || len(elems2) != 0 {
		t.Errorf("expected empty array when Element is nil")
	}
}

func TestNewBitString(t *testing.T) {
	v := NewBitString([]byte{0xFF, 0x80})
	if v.Type() != ValueTypeBitString {
		t.Fatalf("Type() = %v, want BitString", v.Type())
	}
	bs, ok := v.BitString()
	if !ok {
		t.Fatal("BitString() ok=false")
	}
	if len(bs) != 2 {
		t.Fatalf("BitString() len = %d, want 2", len(bs))
	}
	if bs[0] != 0xFF {
		t.Errorf("bs[0] = 0x%02X, want 0xFF", bs[0])
	}
	if bs[1] != 0x80 {
		t.Errorf("bs[1] = 0x%02X, want 0x80", bs[1])
	}
	bitLen, ok := v.BitStringLength()
	if !ok {
		t.Fatal("BitStringLength() ok=false")
	}
	if bitLen != 16 {
		t.Errorf("BitStringLength() = %d, want 16", bitLen)
	}
	if _, ok := v.Int64(); ok {
		t.Error("Int64() on BitString should return ok=false")
	}
}

func TestTypeSpecShallowCompatible_NestedMismatch(t *testing.T) {
	ts := TypeSpec{
		Type: ValueTypeStructure,
		Elements: []TypeSpecElement{
			{Name: "a", Type: TypeSpec{Type: ValueTypeInteger}},
			{Name: "b", Type: TypeSpec{Type: ValueTypeFloat}},
		},
	}
	val := NewStructure([]*Value{NewVisibleString("wrong"), NewBoolean(false)})
	if !ts.ShallowCompatible(val) {
		t.Error("shallow check should pass even with nested type mismatch (correct count)")
	}
}
