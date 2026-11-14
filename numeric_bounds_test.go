// SPDX-License-Identifier: MIT

package mms

import (
	"errors"
	"math"
	"testing"
)

// A TypeSpec can come from a peer. DefaultValue must not panic on it, and
// must not allocate what it describes on its word.
func TestDefaultValueHostileSpecs(t *testing.T) {
	boolean := &TypeSpec{Type: ValueTypeBoolean}
	nested := *boolean
	for i := 0; i < 6; i++ { // 100^6 elements in a few structs
		elem := nested
		nested = TypeSpec{Type: ValueTypeArray, Count: 100, Element: &elem}
	}
	for name, ts := range map[string]*TypeSpec{
		"negative count":      {Type: ValueTypeArray, Count: -1, Element: boolean},
		"minimum count":       {Type: ValueTypeArray, Count: math.MinInt, Element: boolean},
		"maximum count":       {Type: ValueTypeArray, Count: math.MaxInt, Element: boolean},
		"count above budget":  {Type: ValueTypeArray, Count: maxDefaultValueNodes, Element: boolean},
		"nested arrays":       &nested,
		"bit string max size": {Type: ValueTypeBitString, Size: math.MaxInt},
		"bit string too wide": {Type: ValueTypeBitString, Size: 8*maxDefaultValueNodes + 1},
		"struct of big arrays": {Type: ValueTypeStructure, Elements: []TypeSpecElement{
			{Name: "a", Type: TypeSpec{Type: ValueTypeArray, Count: maxDefaultValueNodes / 2, Element: boolean}},
			{Name: "b", Type: TypeSpec{Type: ValueTypeArray, Count: maxDefaultValueNodes / 2, Element: boolean}},
		}},
	} {
		if v := ts.DefaultValue(); v != nil {
			t.Errorf("%s: DefaultValue = %s, want nil", name, v.Type())
		}
	}
	// A refusal costs nothing: no slice of the described size is made first.
	hostile := &TypeSpec{Type: ValueTypeArray, Count: math.MaxInt32, Element: boolean}
	if allocs := testing.AllocsPerRun(20, func() { _ = hostile.DefaultValue() }); allocs > 1 {
		t.Errorf("refusing a hostile array allocates %v times", allocs)
	}
}

// The budget does not get in the way of specs within it.
func TestDefaultValueWithinBudget(t *testing.T) {
	boolean := &TypeSpec{Type: ValueTypeBoolean}
	// The array is one node and each element one more.
	atLimit := &TypeSpec{Type: ValueTypeArray, Count: maxDefaultValueNodes - 1, Element: boolean}
	v := atLimit.DefaultValue()
	if v == nil {
		t.Fatal("an array that exactly spends the budget: nil")
	}
	if elems, _ := v.ArrayElements(); len(elems) != maxDefaultValueNodes-1 {
		t.Fatalf("len = %d", len(elems))
	}
	bits := &TypeSpec{Type: ValueTypeBitString, Size: 13}
	bv := bits.DefaultValue()
	if bv == nil {
		t.Fatal("bit string of 13 bits: nil")
	}
	raw, _ := bv.BitString()
	if n, ok := bv.BitStringLength(); !ok || n != 13 || len(raw) != 2 {
		t.Fatalf("bit string of 13 bits: %d octets, %d bits, ok=%v", len(raw), n, ok)
	}
	empty := &TypeSpec{Type: ValueTypeArray, Count: 5} // element type unknown
	if ev := empty.DefaultValue(); ev == nil {
		t.Fatal("array without element type: nil, want an empty array")
	}
}

// A range selector is a caller's value: a negative count or a sum that
// overflows is an invalid argument, not a panic in make.
func TestValueGetRangeBounds(t *testing.T) {
	arr := NewArray([]*Value{NewInteger(0), NewInteger(1), NewInteger(2), NewInteger(3)})
	for _, tc := range []struct {
		start, count int
		want         int // elements, or -1 for ErrInvalidArgument
	}{
		{0, 4, 4}, {1, 3, 3}, {3, 1, 1}, {4, 0, 0}, {0, 0, 0},
		{0, -1, -1}, {2, -3, -1}, {-1, 2, -1}, {5, 0, -1}, {1, 4, -1},
		{2, math.MaxInt, -1}, {math.MaxInt, math.MaxInt, -1}, {1, math.MinInt, -1},
	} {
		v, err := arr.Get(SelectRange(tc.start, tc.count))
		if tc.want < 0 {
			if !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("Get(range %d from %d) = %v, %v; want ErrInvalidArgument", tc.count, tc.start, v, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("Get(range %d from %d): %v", tc.count, tc.start, err)
			continue
		}
		if elems, _ := v.ArrayElements(); len(elems) != tc.want {
			t.Errorf("Get(range %d from %d): %d elements, want %d", tc.count, tc.start, len(elems), tc.want)
		}
	}
}

// The server's range selection gets its start and count from the peer.
func TestSelectRangeBounds(t *testing.T) {
	arr := NewArray([]*Value{NewInteger(0), NewInteger(1), NewInteger(2)})
	for _, tc := range []struct {
		start, count, want int // want: elements, or -1 for nil
	}{
		{0, 3, 3}, {2, 1, 1}, {3, 0, 0},
		{1, -1, -1}, {-1, 1, -1}, {4, 0, -1}, {1, 3, -1},
		{2, math.MaxInt, -1}, {math.MaxInt, 1, -1},
	} {
		v := selectRange(arr, tc.start, tc.count)
		if tc.want < 0 {
			if v != nil {
				t.Errorf("selectRange(%d, %d) = %v, want nil", tc.start, tc.count, v)
			}
			continue
		}
		if v == nil {
			t.Errorf("selectRange(%d, %d) = nil", tc.start, tc.count)
			continue
		}
		if elems, _ := v.ArrayElements(); len(elems) != tc.want {
			t.Errorf("selectRange(%d, %d): %d elements, want %d", tc.start, tc.count, len(elems), tc.want)
		}
	}
}
