// SPDX-License-Identifier: MIT

package mms

import (
	"testing"

	"github.com/otfabric/go-mms/internal/pdu"
)

func TestApplyAlternateAccessReadWrite_Edges(t *testing.T) {
	arrTS := &TypeSpec{Type: ValueTypeArray, Count: 3, Element: &TypeSpec{Type: ValueTypeInteger, Size: 32}}
	arr := NewArray([]*Value{NewInteger(1), NewInteger(2), NewInteger(3)})

	strucTS := &TypeSpec{
		Type: ValueTypeStructure,
		Elements: []TypeSpecElement{
			{Name: "a", Type: TypeSpec{Type: ValueTypeInteger, Size: 32}},
			{Name: "b", Type: TypeSpec{Type: ValueTypeBoolean}},
		},
	}
	struc := NewStructure([]*Value{NewInteger(10), NewBoolean(true)})

	// Empty selectors on write returns writeVal.
	wv := NewInteger(9)
	if got := applyAlternateAccessWrite(arr, arrTS, nil, wv); got != wv {
		t.Fatal("empty selectors should return writeVal")
	}

	// Index out of bounds → nil
	if applyAlternateAccessRead(arr, arrTS, []pdu.AccessSelectorWire{{HasIndex: true, Index: 9}}) != nil {
		t.Fatal("expected nil for OOB index read")
	}
	if applyAlternateAccessWrite(arr, arrTS, []pdu.AccessSelectorWire{{HasIndex: true, Index: 9}}, wv) != nil {
		t.Fatal("expected nil for OOB index write")
	}

	// Unknown component → nil
	if applyAlternateAccessRead(struc, strucTS, []pdu.AccessSelectorWire{{Component: "missing"}}) != nil {
		t.Fatal("expected nil for missing component")
	}
	if resolveComponentIndex(nil, "a") != -1 {
		t.Fatal("nil typespec should resolve -1")
	}

	// Range patch success + failures
	rangeSel := []pdu.AccessSelectorWire{{IndexRange: &pdu.IndexRangeWire{LowIndex: 1, NumberOfElements: 2}}}
	newRange := NewArray([]*Value{NewInteger(8), NewInteger(9)})
	patched := applyAlternateAccessWrite(arr, arrTS, rangeSel, newRange)
	if patched == nil {
		t.Fatal("range write failed")
	}
	elems, _ := patched.ArrayElements()
	if i, ok := elems[1].Int64(); !ok || i != 8 {
		t.Fatalf("elems[1]=%v", elems[1])
	}

	if applyAlternateAccessWrite(arr, arrTS, rangeSel, NewInteger(1)) != nil {
		t.Fatal("range write with non-array should fail")
	}
	if applyAlternateAccessWrite(NewInteger(1), arrTS, rangeSel, newRange) != nil {
		t.Fatal("range write on non-array current should fail")
	}
	badRange := []pdu.AccessSelectorWire{{IndexRange: &pdu.IndexRangeWire{LowIndex: 0, NumberOfElements: 10}}}
	if applyAlternateAccessWrite(arr, arrTS, badRange, newRange) != nil {
		t.Fatal("OOB range should fail")
	}

	// Component write leaf
	compSel := []pdu.AccessSelectorWire{{Component: "b"}}
	patched = applyAlternateAccessWrite(struc, strucTS, compSel, NewBoolean(false))
	if patched == nil {
		t.Fatal("component write failed")
	}
	se, _ := patched.Structure()
	if b, ok := se[1].Bool(); !ok || b {
		t.Fatalf("want false, got %v", se[1])
	}

	// Wrong type for selector default branch
	if applyAlternateAccessRead(arr, arrTS, []pdu.AccessSelectorWire{{}}) != nil {
		t.Fatal("empty selector should yield nil")
	}

	// Mid-chain nil short-circuit on read.
	if applyAlternateAccessRead(arr, arrTS, []pdu.AccessSelectorWire{
		{HasIndex: true, Index: 9},
		{Component: "x"},
	}) != nil {
		t.Fatal("nil after first selector should short-circuit")
	}
}

func TestPatchValue_Edges(t *testing.T) {
	arrTS := &TypeSpec{Type: ValueTypeArray, Count: 3, Element: &TypeSpec{Type: ValueTypeInteger, Size: 32}}
	arr := NewArray([]*Value{NewInteger(1), NewInteger(2), NewInteger(3)})
	if patchValue(arr, arrTS, nil, NewInteger(1)) {
		t.Fatal("empty selectors")
	}
	if patchValue(nil, arrTS, []pdu.AccessSelectorWire{{HasIndex: true, Index: 0}}, NewInteger(1)) {
		t.Fatal("nil value")
	}
	if patchValue(arr, arrTS, []pdu.AccessSelectorWire{{}}, NewInteger(1)) {
		t.Fatal("empty selector")
	}
}

func TestPatchComponent_Edges(t *testing.T) {
	strucTS := &TypeSpec{
		Type: ValueTypeStructure,
		Elements: []TypeSpecElement{
			{Name: "a", Type: TypeSpec{Type: ValueTypeInteger, Size: 32}},
			{Name: "inner", Type: TypeSpec{
				Type: ValueTypeStructure,
				Elements: []TypeSpecElement{
					{Name: "x", Type: TypeSpec{Type: ValueTypeBoolean}},
				},
			}},
		},
	}
	struc := NewStructure([]*Value{
		NewInteger(10),
		NewStructure([]*Value{NewBoolean(true)}),
	})

	if patchComponent(NewInteger(1), strucTS, "a", nil, NewInteger(2)) {
		t.Fatal("non-structure")
	}
	if patchComponent(struc, strucTS, "missing", nil, NewInteger(2)) {
		t.Fatal("missing component")
	}
	// Type mismatch at leaf.
	if patchComponent(struc, strucTS, "a", nil, NewBoolean(true)) {
		t.Fatal("shallow incompatible")
	}
	// Nested component write.
	ok := patchComponent(struc, strucTS, "inner", []pdu.AccessSelectorWire{{Component: "x"}}, NewBoolean(false))
	if !ok {
		t.Fatal("nested component write")
	}
	inner, _ := struc.elementsVal[1].Structure()
	if b, ok := inner[0].Bool(); !ok || b {
		t.Fatalf("inner.x=%v", inner[0])
	}
	// Component index beyond value length (name in short TypeSpec vs longer? use mismatched lengths).
	shortVal := NewStructure([]*Value{NewInteger(1)}) // only one element
	wideTS := &TypeSpec{
		Type: ValueTypeStructure,
		Elements: []TypeSpecElement{
			{Name: "a", Type: TypeSpec{Type: ValueTypeInteger, Size: 32}},
			{Name: "b", Type: TypeSpec{Type: ValueTypeBoolean}},
		},
	}
	if patchComponent(shortVal, wideTS, "b", nil, NewBoolean(true)) {
		t.Fatal("index beyond value length")
	}
	// Nil typespec → resolve fails.
	if patchComponent(struc, nil, "a", nil, NewInteger(1)) {
		t.Fatal("nil typespec")
	}
}

func TestPatchIndex_Edges(t *testing.T) {
	arrTS := &TypeSpec{Type: ValueTypeArray, Count: 2, Element: &TypeSpec{Type: ValueTypeInteger, Size: 32}}
	arr := NewArray([]*Value{NewInteger(1), NewInteger(2)})
	strucTS := &TypeSpec{
		Type: ValueTypeStructure,
		Elements: []TypeSpecElement{
			{Name: "a", Type: TypeSpec{Type: ValueTypeInteger, Size: 32}},
			{Name: "b", Type: TypeSpec{Type: ValueTypeBoolean}},
		},
	}
	struc := NewStructure([]*Value{NewInteger(10), NewBoolean(true)})

	if patchIndex(NewInteger(1), arrTS, 0, nil, NewInteger(9)) {
		t.Fatal("non-array/structure")
	}
	if patchIndex(arr, arrTS, -1, nil, NewInteger(9)) || patchIndex(arr, arrTS, 9, nil, NewInteger(9)) {
		t.Fatal("OOB array")
	}
	if patchIndex(arr, arrTS, 0, nil, NewBoolean(true)) {
		t.Fatal("type mismatch")
	}
	if !patchIndex(arr, arrTS, 0, nil, NewInteger(99)) {
		t.Fatal("array leaf write")
	}
	// Nested: array of structures → index then component.
	nestedTS := &TypeSpec{
		Type:  ValueTypeArray,
		Count: 1,
		Element: &TypeSpec{
			Type: ValueTypeStructure,
			Elements: []TypeSpecElement{
				{Name: "f", Type: TypeSpec{Type: ValueTypeInteger, Size: 32}},
			},
		},
	}
	nested := NewArray([]*Value{NewStructure([]*Value{NewInteger(1)})})
	if !patchIndex(nested, nestedTS, 0, []pdu.AccessSelectorWire{{Component: "f"}}, NewInteger(7)) {
		t.Fatal("nested index→component")
	}
	// Structure by index leaf + nested.
	if !patchIndex(struc, strucTS, 1, nil, NewBoolean(false)) {
		t.Fatal("structure index leaf")
	}
	if patchIndex(struc, strucTS, 0, nil, NewBoolean(true)) {
		t.Fatal("structure type mismatch")
	}
	// Nil TypeSpec skips compatibility check.
	if !patchIndex(arr, nil, 1, nil, NewInteger(5)) {
		t.Fatal("nil ts array write")
	}
}

func TestPatchRange_Edges(t *testing.T) {
	arr := NewArray([]*Value{NewInteger(1), NewInteger(2), NewInteger(3)})
	if patchRange(NewInteger(1), 0, 1, nil, NewArray([]*Value{NewInteger(9)})) {
		t.Fatal("non-array")
	}
	if patchRange(arr, -1, 1, nil, NewArray([]*Value{NewInteger(9)})) {
		t.Fatal("negative start")
	}
	if patchRange(arr, 0, 2, []pdu.AccessSelectorWire{{HasIndex: true, Index: 0}}, NewArray([]*Value{NewInteger(1), NewInteger(2)})) {
		t.Fatal("remaining selectors")
	}
	if patchRange(arr, 0, 2, nil, NewArray([]*Value{NewInteger(1)})) {
		t.Fatal("wrong element count")
	}
	if !patchRange(arr, 0, 2, nil, NewArray([]*Value{NewInteger(8), NewInteger(9)})) {
		t.Fatal("success")
	}
}

func TestSelectHelpers_Edges(t *testing.T) {
	arrTS := &TypeSpec{Type: ValueTypeArray, Count: 3, Element: &TypeSpec{Type: ValueTypeInteger, Size: 32}}
	arr := NewArray([]*Value{NewInteger(1), NewInteger(2), NewInteger(3)})
	strucTS := &TypeSpec{
		Type: ValueTypeStructure,
		Elements: []TypeSpecElement{
			{Name: "a", Type: TypeSpec{Type: ValueTypeInteger, Size: 32}},
			{Name: "b", Type: TypeSpec{Type: ValueTypeBoolean}},
		},
	}
	struc := NewStructure([]*Value{NewInteger(10), NewBoolean(true)})

	if v, _ := selectComponent(NewInteger(1), strucTS, "a"); v != nil {
		t.Fatal("non-structure component")
	}
	if v, ts := selectComponent(struc, strucTS, "a"); v == nil || ts == nil || ts.Type != ValueTypeInteger {
		t.Fatalf("component a: %v %v", v, ts)
	}
	short := NewStructure([]*Value{NewInteger(1)})
	if v, _ := selectComponent(short, strucTS, "b"); v != nil {
		t.Fatal("component beyond value length")
	}

	if v, _ := selectIndex(NewInteger(1), arrTS, 0); v != nil {
		t.Fatal("non-indexable")
	}
	if v, _ := selectIndex(arr, arrTS, -1); v != nil {
		t.Fatal("neg index")
	}
	if v, ts := selectIndex(arr, arrTS, 1); v == nil || ts == nil {
		t.Fatal("array index")
	}
	if v, _ := selectIndex(arr, nil, 1); v == nil {
		t.Fatal("array index nil ts")
	}
	if v, ts := selectIndex(struc, strucTS, 1); v == nil || ts == nil || ts.Type != ValueTypeBoolean {
		t.Fatalf("structure index: %v %v", v, ts)
	}
	if v, _ := selectIndex(struc, strucTS, 9); v != nil {
		t.Fatal("structure OOB")
	}
	if v, _ := selectIndex(struc, nil, 0); v == nil {
		t.Fatal("structure index nil ts")
	}

	if selectRange(NewInteger(1), 0, 1) != nil {
		t.Fatal("non-array range")
	}
	if selectRange(arr, -1, 1) != nil || selectRange(arr, 0, 10) != nil {
		t.Fatal("OOB range")
	}
	got := selectRange(arr, 1, 2)
	if got == nil {
		t.Fatal("range success")
	}
	ge, _ := got.ArrayElements()
	if len(ge) != 2 {
		t.Fatalf("len=%d", len(ge))
	}

	// Nested write via applyAlternateAccessWrite (index then component).
	nestedTS := &TypeSpec{
		Type:  ValueTypeArray,
		Count: 1,
		Element: &TypeSpec{
			Type: ValueTypeStructure,
			Elements: []TypeSpecElement{
				{Name: "f", Type: TypeSpec{Type: ValueTypeInteger, Size: 32}},
			},
		},
	}
	nested := NewArray([]*Value{NewStructure([]*Value{NewInteger(1)})})
	patched := applyAlternateAccessWrite(nested, nestedTS, []pdu.AccessSelectorWire{
		{HasIndex: true, Index: 0},
		{Component: "f"},
	}, NewInteger(42))
	if patched == nil {
		t.Fatal("nested write")
	}
	read := applyAlternateAccessRead(patched, nestedTS, []pdu.AccessSelectorWire{
		{HasIndex: true, Index: 0},
		{Component: "f"},
	})
	if i, ok := read.Int64(); !ok || i != 42 {
		t.Fatalf("read back %v", read)
	}
	// Structure by index read.
	if applyAlternateAccessRead(struc, strucTS, []pdu.AccessSelectorWire{{HasIndex: true, Index: 0}}) == nil {
		t.Fatal("structure index read")
	}
	if applyAlternateAccessRead(arr, arrTS, []pdu.AccessSelectorWire{
		{IndexRange: &pdu.IndexRangeWire{LowIndex: 0, NumberOfElements: 2}},
	}) == nil {
		t.Fatal("range read")
	}
}
