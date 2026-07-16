// SPDX-License-Identifier: MIT

package mms

import (
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// New value types: GeneralizedTime, BCD, ObjectIdentifier
// ---------------------------------------------------------------------------

func TestGeneralizedTimeValue(t *testing.T) {
	ts := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	v := NewGeneralizedTime(ts)

	if v.Type() != ValueTypeGeneralizedTime {
		t.Fatalf("Type() = %v, want GeneralizedTime", v.Type())
	}

	got, ok := v.GeneralizedTime()
	if !ok {
		t.Fatal("GeneralizedTime() ok=false")
	}
	if !got.Equal(ts) {
		t.Errorf("GeneralizedTime() = %v, want %v", got, ts)
	}

	// Wrong-type accessor should return ok=false.
	if _, ok := v.UTCTime(); ok {
		t.Error("UTCTime() on GeneralizedTime should return ok=false")
	}
	if _, ok := v.Int64(); ok {
		t.Error("Int64() on GeneralizedTime should return ok=false")
	}
}

func TestGeneralizedTimeCloneEqual(t *testing.T) {
	ts := time.Date(2024, 6, 15, 8, 30, 0, 0, time.UTC)
	v := NewGeneralizedTime(ts)
	c := v.Clone()

	if !v.Equal(c) {
		t.Error("Clone should Equal original")
	}
	if !c.Equal(v) {
		t.Error("Equal should be symmetric")
	}

	other := NewGeneralizedTime(ts.Add(time.Hour))
	if v.Equal(other) {
		t.Error("different times should not be equal")
	}
}

func TestGeneralizedTimeString(t *testing.T) {
	ts := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	v := NewGeneralizedTime(ts)
	s := v.String()
	if s == "" {
		t.Error("String() should not be empty")
	}
	if !strings.Contains(s, "2024") {
		t.Errorf("String() = %q, expected to contain year", s)
	}
}

func TestBCDValue(t *testing.T) {
	v := NewBCD(42)

	if v.Type() != ValueTypeBCD {
		t.Fatalf("Type() = %v, want BCD", v.Type())
	}

	got, ok := v.BCD()
	if !ok {
		t.Fatal("BCD() ok=false")
	}
	if got != 42 {
		t.Errorf("BCD() = %d, want 42", got)
	}

	if _, ok := v.Int64(); ok {
		t.Error("Int64() on BCD should return ok=false")
	}
}

func TestBCDCloneEqual(t *testing.T) {
	v := NewBCD(99)
	c := v.Clone()

	if !v.Equal(c) {
		t.Error("Clone should Equal original")
	}

	other := NewBCD(100)
	if v.Equal(other) {
		t.Error("different BCD values should not be equal")
	}
}

func TestBCDString(t *testing.T) {
	v := NewBCD(42)
	s := v.String()
	if s != "BCD(42)" {
		t.Errorf("String() = %q, want %q", s, "BCD(42)")
	}
}

func TestObjectIdentifierValue(t *testing.T) {
	oid := []int{1, 2, 840, 10003}
	v := NewObjectIdentifier(oid)

	if v.Type() != ValueTypeObjectIdentifier {
		t.Fatalf("Type() = %v, want ObjectIdentifier", v.Type())
	}

	got, ok := v.ObjectIdentifier()
	if !ok {
		t.Fatal("ObjectIdentifier() ok=false")
	}
	if len(got) != 4 {
		t.Fatalf("ObjectIdentifier() len = %d, want 4", len(got))
	}
	expected := []int{1, 2, 840, 10003}
	for i, want := range expected {
		if got[i] != want {
			t.Errorf("OID[%d] = %d, want %d", i, got[i], want)
		}
	}
}

func TestObjectIdentifierCopyIsolation(t *testing.T) {
	oid := []int{1, 3, 6, 1}
	v := NewObjectIdentifier(oid)

	// Mutating original should not affect value.
	oid[0] = 99
	got, _ := v.ObjectIdentifier()
	if got[0] != 1 {
		t.Error("constructor should copy input: mutation of original affected stored value")
	}

	// Mutating returned slice should not affect value.
	got[1] = 99
	got2, _ := v.ObjectIdentifier()
	if got2[1] != 3 {
		t.Error("accessor should copy output: mutation of returned slice affected stored value")
	}
}

func TestObjectIdentifierCloneEqual(t *testing.T) {
	v := NewObjectIdentifier([]int{1, 2, 840, 10003})
	c := v.Clone()

	if !v.Equal(c) {
		t.Error("Clone should Equal original")
	}
	if !c.Equal(v) {
		t.Error("Equal should be symmetric")
	}

	other := NewObjectIdentifier([]int{1, 2, 840, 99999})
	if v.Equal(other) {
		t.Error("different OIDs should not be equal")
	}

	shorter := NewObjectIdentifier([]int{1, 2})
	if v.Equal(shorter) {
		t.Error("different-length OIDs should not be equal")
	}
}

func TestObjectIdentifierString(t *testing.T) {
	v := NewObjectIdentifier([]int{1, 2, 840})
	s := v.String()
	if !strings.Contains(s, "OID") {
		t.Errorf("String() = %q, expected to contain OID", s)
	}
	if !strings.Contains(s, "840") {
		t.Errorf("String() = %q, expected to contain arc value", s)
	}
}

func TestObjectIdentifierNil(t *testing.T) {
	v := NewObjectIdentifier(nil)
	got, ok := v.ObjectIdentifier()
	if !ok {
		t.Fatal("ObjectIdentifier() ok=false for nil OID")
	}
	if len(got) != 0 {
		t.Errorf("expected empty OID, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// Selector builders
// ---------------------------------------------------------------------------

func TestSelectComponent(t *testing.T) {
	s := SelectComponent("foo")
	if s.Component != "foo" {
		t.Errorf("Component = %q, want %q", s.Component, "foo")
	}
	if s.Index != nil {
		t.Error("Index should be nil")
	}
	if s.IndexRange != nil {
		t.Error("IndexRange should be nil")
	}
}

func TestSelectIndex(t *testing.T) {
	s := SelectIndex(5)
	if s.Index == nil || *s.Index != 5 {
		t.Errorf("Index = %v, want 5", s.Index)
	}
	if s.Component != "" {
		t.Error("Component should be empty")
	}
	if s.IndexRange != nil {
		t.Error("IndexRange should be nil")
	}
}

func TestSelectRange(t *testing.T) {
	s := SelectRange(2, 3)
	if s.IndexRange == nil {
		t.Fatal("IndexRange should not be nil")
	}
	if s.IndexRange.Start != 2 {
		t.Errorf("Start = %d, want 2", s.IndexRange.Start)
	}
	if s.IndexRange.Count != 3 {
		t.Errorf("Count = %d, want 3", s.IndexRange.Count)
	}
	if s.Component != "" {
		t.Error("Component should be empty")
	}
	if s.Index != nil {
		t.Error("Index should be nil")
	}
}

// ---------------------------------------------------------------------------
// Value.Get — path traversal
// ---------------------------------------------------------------------------

func TestValueGet(t *testing.T) {
	inner1 := NewStructure([]*Value{NewInteger(10), NewInteger(20)})
	inner2 := NewStructure([]*Value{NewInteger(30), NewInteger(40)})
	arr := NewArray([]*Value{inner1, inner2})
	root := NewStructure([]*Value{NewInteger(1), arr})

	t.Run("index into structure", func(t *testing.T) {
		v, err := root.Get(SelectIndex(0))
		if err != nil {
			t.Fatal(err)
		}
		got, ok := v.Int64()
		if !ok || got != 1 {
			t.Errorf("got %d/%v, want 1/true", got, ok)
		}
	})

	t.Run("index into structure then array", func(t *testing.T) {
		v, err := root.Get(SelectIndex(1), SelectIndex(0))
		if err != nil {
			t.Fatal(err)
		}
		if v.Type() != ValueTypeStructure {
			t.Fatalf("Type() = %v, want Structure", v.Type())
		}
		elems, ok := v.Structure()
		if !ok || len(elems) != 2 {
			t.Fatal("expected 2-element structure")
		}
		first, ok := elems[0].Int64()
		if !ok || first != 10 {
			t.Errorf("inner1[0] = %d, want 10", first)
		}
	})

	t.Run("index range on array", func(t *testing.T) {
		v, err := root.Get(SelectIndex(1), SelectRange(0, 1))
		if err != nil {
			t.Fatal(err)
		}
		if v.Type() != ValueTypeArray {
			t.Fatalf("Type() = %v, want Array", v.Type())
		}
		elems, ok := v.ArrayElements()
		if !ok || len(elems) != 1 {
			t.Fatalf("expected 1-element array, got %d", len(elems))
		}
	})

	t.Run("deep traversal array then structure element", func(t *testing.T) {
		v, err := root.Get(SelectIndex(1), SelectIndex(1), SelectIndex(0))
		if err != nil {
			t.Fatal(err)
		}
		got, ok := v.Int64()
		if !ok || got != 30 {
			t.Errorf("got %d/%v, want 30/true", got, ok)
		}
	})
}

func TestValueGet_Errors(t *testing.T) {
	inner := NewStructure([]*Value{NewInteger(10)})
	arr := NewArray([]*Value{inner})
	root := NewStructure([]*Value{NewInteger(1), arr})

	t.Run("out of bounds on structure", func(t *testing.T) {
		_, err := root.Get(SelectIndex(99))
		if err == nil {
			t.Fatal("expected error for out-of-bounds index")
		}
	})

	t.Run("out of bounds on array", func(t *testing.T) {
		_, err := root.Get(SelectIndex(1), SelectIndex(99))
		if err == nil {
			t.Fatal("expected error for out-of-bounds array index")
		}
	})

	t.Run("negative index on structure", func(t *testing.T) {
		_, err := root.Get(SelectIndex(-1))
		if err == nil {
			t.Fatal("expected error for negative index")
		}
	})

	t.Run("index on scalar", func(t *testing.T) {
		_, err := root.Get(SelectIndex(0), SelectIndex(0))
		if err == nil {
			t.Fatal("expected error for index on non-composite type")
		}
	})

	t.Run("range on non-array", func(t *testing.T) {
		_, err := root.Get(SelectRange(0, 1))
		if err == nil {
			t.Fatal("expected error for range on structure")
		}
	})

	t.Run("range out of bounds", func(t *testing.T) {
		_, err := root.Get(SelectIndex(1), SelectRange(0, 99))
		if err == nil {
			t.Fatal("expected error for range exceeding array bounds")
		}
	})

	t.Run("component on value requires TypeSpec", func(t *testing.T) {
		_, err := root.Get(SelectComponent("name"))
		if err == nil {
			t.Fatal("expected error for component access on Value")
		}
	})

	t.Run("nil value at selector", func(t *testing.T) {
		var nilVal *Value
		_, err := nilVal.Get(SelectIndex(0))
		if err == nil {
			t.Fatal("expected error for nil value")
		}
	})

	t.Run("no field set", func(t *testing.T) {
		_, err := root.Get(AccessSelector{})
		if err == nil {
			t.Fatal("expected error for empty selector")
		}
	})
}

func TestValueGet_EmptySelectors(t *testing.T) {
	v := NewInteger(42)
	got, err := v.Get()
	if err != nil {
		t.Fatal(err)
	}
	if got != v {
		t.Error("Get with no selectors should return the same value")
	}
}

// ---------------------------------------------------------------------------
// TypeSpec.Resolve — type-level path traversal
// ---------------------------------------------------------------------------

func TestTypeSpecResolve(t *testing.T) {
	ts := TypeSpec{
		Type: ValueTypeStructure,
		Elements: []TypeSpecElement{
			{Name: "name", Type: TypeSpec{Type: ValueTypeVisibleString, Size: 32}},
			{Name: "items", Type: TypeSpec{
				Type: ValueTypeArray, Count: 5,
				Element: &TypeSpec{
					Type: ValueTypeStructure,
					Elements: []TypeSpecElement{
						{Name: "x", Type: TypeSpec{Type: ValueTypeInteger, Size: 32}},
						{Name: "y", Type: TypeSpec{Type: ValueTypeFloat, FormatWidth: 32, ExponentWidth: 8}},
					},
				},
			}},
		},
	}

	t.Run("resolve component name", func(t *testing.T) {
		resolved, err := ts.Resolve(SelectComponent("name"))
		if err != nil {
			t.Fatal(err)
		}
		if resolved.Type != ValueTypeVisibleString {
			t.Errorf("Type = %v, want VisibleString", resolved.Type)
		}
		if resolved.Size != 32 {
			t.Errorf("Size = %d, want 32", resolved.Size)
		}
	})

	t.Run("resolve component items", func(t *testing.T) {
		resolved, err := ts.Resolve(SelectComponent("items"))
		if err != nil {
			t.Fatal(err)
		}
		if resolved.Type != ValueTypeArray {
			t.Errorf("Type = %v, want Array", resolved.Type)
		}
		if resolved.Count != 5 {
			t.Errorf("Count = %d, want 5", resolved.Count)
		}
	})

	t.Run("resolve items then index into element", func(t *testing.T) {
		resolved, err := ts.Resolve(SelectComponent("items"), SelectIndex(0))
		if err != nil {
			t.Fatal(err)
		}
		if resolved.Type != ValueTypeStructure {
			t.Fatalf("Type = %v, want Structure", resolved.Type)
		}
		if len(resolved.Elements) != 2 {
			t.Fatalf("Elements = %d, want 2", len(resolved.Elements))
		}
		if resolved.Elements[0].Name != "x" {
			t.Errorf("Elements[0].Name = %q, want x", resolved.Elements[0].Name)
		}
	})

	t.Run("resolve items then index then component x", func(t *testing.T) {
		resolved, err := ts.Resolve(SelectComponent("items"), SelectIndex(0), SelectComponent("x"))
		if err != nil {
			t.Fatal(err)
		}
		if resolved.Type != ValueTypeInteger {
			t.Errorf("Type = %v, want Integer", resolved.Type)
		}
	})

	t.Run("resolve items then range", func(t *testing.T) {
		resolved, err := ts.Resolve(SelectComponent("items"), SelectRange(1, 3))
		if err != nil {
			t.Fatal(err)
		}
		if resolved.Type != ValueTypeArray {
			t.Errorf("Type = %v, want Array", resolved.Type)
		}
		if resolved.Count != 3 {
			t.Errorf("Count = %d, want 3", resolved.Count)
		}
		if resolved.Element == nil {
			t.Fatal("Element should not be nil")
		}
		if resolved.Element.Type != ValueTypeStructure {
			t.Errorf("Element.Type = %v, want Structure", resolved.Element.Type)
		}
	})

	t.Run("empty selectors", func(t *testing.T) {
		resolved, err := ts.Resolve()
		if err != nil {
			t.Fatal(err)
		}
		if resolved != &ts {
			t.Error("Resolve with no selectors should return the same TypeSpec")
		}
	})
}

func TestTypeSpecResolve_Errors(t *testing.T) {
	ts := TypeSpec{
		Type: ValueTypeStructure,
		Elements: []TypeSpecElement{
			{Name: "name", Type: TypeSpec{Type: ValueTypeVisibleString, Size: 32}},
			{Name: "items", Type: TypeSpec{
				Type: ValueTypeArray, Count: 5,
				Element: &TypeSpec{Type: ValueTypeInteger, Size: 32},
			}},
		},
	}

	t.Run("component not found", func(t *testing.T) {
		_, err := ts.Resolve(SelectComponent("z"))
		if err == nil {
			t.Fatal("expected error for unknown component")
		}
		if !strings.Contains(err.Error(), `"z"`) {
			t.Errorf("error should mention component name: %v", err)
		}
	})

	t.Run("index on scalar", func(t *testing.T) {
		_, err := ts.Resolve(SelectComponent("name"), SelectIndex(0))
		if err == nil {
			t.Fatal("expected error for index on non-composite type")
		}
	})

	t.Run("index out of bounds on structure", func(t *testing.T) {
		_, err := ts.Resolve(SelectIndex(99))
		if err == nil {
			t.Fatal("expected error for out-of-bounds index")
		}
	})

	t.Run("range on non-array", func(t *testing.T) {
		_, err := ts.Resolve(SelectRange(0, 1))
		if err == nil {
			t.Fatal("expected error for range on structure")
		}
	})

	t.Run("nil TypeSpec", func(t *testing.T) {
		var nilTS *TypeSpec
		_, err := nilTS.Resolve(SelectIndex(0))
		if err == nil {
			t.Fatal("expected error for nil TypeSpec")
		}
	})

	t.Run("no field set", func(t *testing.T) {
		_, err := ts.Resolve(AccessSelector{})
		if err == nil {
			t.Fatal("expected error for empty selector")
		}
	})

	t.Run("range on array with nil element", func(t *testing.T) {
		nilElem := TypeSpec{Type: ValueTypeArray, Count: 5, Element: nil}
		_, err := nilElem.Resolve(SelectRange(0, 2))
		if err == nil {
			t.Fatal("expected error for range on array with nil element type")
		}
	})
}

// ---------------------------------------------------------------------------
// DefaultValue for new types
// ---------------------------------------------------------------------------

func TestTypeSpecDefaultValue_NewTypes(t *testing.T) {
	tests := []struct {
		name string
		ts   TypeSpec
		typ  ValueType
	}{
		{"GeneralizedTime", TypeSpec{Type: ValueTypeGeneralizedTime}, ValueTypeGeneralizedTime},
		{"BCD", TypeSpec{Type: ValueTypeBCD}, ValueTypeBCD},
		{"ObjectIdentifier", TypeSpec{Type: ValueTypeObjectIdentifier}, ValueTypeObjectIdentifier},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := tt.ts.DefaultValue()
			if v == nil {
				t.Fatal("DefaultValue returned nil")
			}
			if v.Type() != tt.typ {
				t.Errorf("type = %v, want %v", v.Type(), tt.typ)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Cross-type Equal comparisons for new types
// ---------------------------------------------------------------------------

func TestEqualCrossType_NewTypes(t *testing.T) {
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	if NewGeneralizedTime(ts).Equal(NewUTCTime(ts)) {
		t.Error("GeneralizedTime should not equal UTCTime even with same time")
	}
	if NewBCD(42).Equal(NewInteger(42)) {
		t.Error("BCD should not equal Integer even with same numeric value")
	}
	if NewObjectIdentifier([]int{1, 2}).Equal(NewArray([]*Value{NewInteger(1), NewInteger(2)})) {
		t.Error("ObjectIdentifier should not equal Array")
	}
}
