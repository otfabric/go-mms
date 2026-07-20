// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"sync"
	"testing"
	"time"
)

func altAccessTestSetup(t *testing.T) *Client {
	t.Helper()
	srv := testServer(t)

	if err := srv.RegisterDomain("dom"); err != nil {
		t.Fatal(err)
	}

	if err := srv.RegisterVariable(Variable{
		Name:     ObjectName{Scope: ObjectScopeDomain, Domain: "dom", ItemID: "arr"},
		TypeSpec: TypeSpec{Type: ValueTypeArray, Count: 5, Element: &TypeSpec{Type: ValueTypeInteger, Size: 32}},
		Read: func(_ context.Context) (*Value, error) {
			return NewArray([]*Value{
				NewInteger(10), NewInteger(20), NewInteger(30),
				NewInteger(40), NewInteger(50),
			}), nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	if err := srv.RegisterVariable(Variable{
		Name: ObjectName{Scope: ObjectScopeDomain, Domain: "dom", ItemID: "struc"},
		TypeSpec: TypeSpec{
			Type: ValueTypeStructure,
			Elements: []TypeSpecElement{
				{Name: "temperature", Type: TypeSpec{Type: ValueTypeFloat, FormatWidth: 32, ExponentWidth: 8}},
				{Name: "valid", Type: TypeSpec{Type: ValueTypeBoolean}},
			},
		},
		Read: func(_ context.Context) (*Value, error) {
			return NewStructure([]*Value{
				NewFloat(21.5),
				NewBoolean(true),
			}), nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var writtenValue *Value
	if err := srv.RegisterVariable(Variable{
		Name:     ObjectName{Scope: ObjectScopeDomain, Domain: "dom", ItemID: "wArr"},
		TypeSpec: TypeSpec{Type: ValueTypeArray, Count: 3, Element: &TypeSpec{Type: ValueTypeInteger, Size: 32}},
		Read: func(_ context.Context) (*Value, error) {
			mu.Lock()
			defer mu.Unlock()
			if writtenValue != nil {
				return writtenValue, nil
			}
			return NewArray([]*Value{NewInteger(0), NewInteger(0), NewInteger(0)}), nil
		},
		Write: func(_ context.Context, v *Value) error {
			mu.Lock()
			defer mu.Unlock()
			writtenValue = v
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	var wStruc *Value
	if err := srv.RegisterVariable(Variable{
		Name: ObjectName{Scope: ObjectScopeDomain, Domain: "dom", ItemID: "wStruc"},
		TypeSpec: TypeSpec{
			Type: ValueTypeStructure,
			Elements: []TypeSpecElement{
				{Name: "alpha", Type: TypeSpec{Type: ValueTypeInteger, Size: 32}},
				{Name: "beta", Type: TypeSpec{Type: ValueTypeFloat, FormatWidth: 32, ExponentWidth: 8}},
			},
		},
		Read: func(_ context.Context) (*Value, error) {
			mu.Lock()
			defer mu.Unlock()
			if wStruc != nil {
				return wStruc, nil
			}
			return NewStructure([]*Value{NewInteger(100), NewFloat(1.5)}), nil
		},
		Write: func(_ context.Context, v *Value) error {
			mu.Lock()
			defer mu.Unlock()
			wStruc = v
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	client := connectClientServer(t, srv)
	return client
}

func TestReadArrayElementEndToEnd(t *testing.T) {
	client := altAccessTestSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.ReadArrayElement(ctx, ObjectName{
		Scope: ObjectScopeDomain, Domain: "dom", ItemID: "arr",
	}, 2)
	if err != nil {
		t.Fatal(err)
	}

	i, ok := result.Value.Int64()
	if !ok || i != 30 {
		t.Errorf("got %v/%v, want 30/true", i, ok)
	}

	_ = client.Close(ctx)
}

func TestReadArrayRangeEndToEnd(t *testing.T) {
	client := altAccessTestSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.ReadArrayRange(ctx, ObjectName{
		Scope: ObjectScopeDomain, Domain: "dom", ItemID: "arr",
	}, 1, 3)
	if err != nil {
		t.Fatal(err)
	}

	elems, ok := result.Value.ArrayElements()
	if !ok {
		t.Fatal("expected array value")
	}
	if len(elems) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(elems))
	}

	expected := []int64{20, 30, 40}
	for j, want := range expected {
		got, ok := elems[j].Int64()
		if !ok || got != want {
			t.Errorf("element[%d] = %d/%v, want %d", j, got, ok, want)
		}
	}

	_ = client.Close(ctx)
}

func TestReadStructElementByIndexEndToEnd(t *testing.T) {
	client := altAccessTestSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.ReadByIndex(ctx, ObjectName{
		Scope: ObjectScopeDomain, Domain: "dom", ItemID: "struc",
	}, 1)
	if err != nil {
		t.Fatal(err)
	}

	b, ok := result.Value.Bool()
	if !ok || !b {
		t.Errorf("got %v/%v, want true", b, ok)
	}

	_ = client.Close(ctx)
}

func TestReadVariablesMultipleWithAccess(t *testing.T) {
	client := altAccessTestSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	idx0 := 0
	idx4 := 4
	results, err := client.ReadVariables(ctx, []VariableSpec{
		{
			Name:            ObjectName{Scope: ObjectScopeDomain, Domain: "dom", ItemID: "arr"},
			AlternateAccess: []AccessSelector{{Index: &idx0}},
		},
		{
			Name:            ObjectName{Scope: ObjectScopeDomain, Domain: "dom", ItemID: "arr"},
			AlternateAccess: []AccessSelector{{Index: &idx4}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	v0, ok := results[0].Value.Int64()
	if !ok || v0 != 10 {
		t.Errorf("results[0] = %d/%v, want 10", v0, ok)
	}
	v1, ok := results[1].Value.Int64()
	if !ok || v1 != 50 {
		t.Errorf("results[1] = %d/%v, want 50", v1, ok)
	}

	_ = client.Close(ctx)
}

func TestWriteArrayElementEndToEnd(t *testing.T) {
	client := altAccessTestSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	name := ObjectName{Scope: ObjectScopeDomain, Domain: "dom", ItemID: "wArr"}

	err := client.WriteArrayElement(ctx, name, 1, NewInteger(99))
	if err != nil {
		t.Fatal(err)
	}

	result, err := client.ReadVariables(ctx, []VariableSpec{{Name: name}})
	if err != nil {
		t.Fatal(err)
	}
	elems, ok := result[0].Value.ArrayElements()
	if !ok || len(elems) != 3 {
		t.Fatal("expected 3-element array")
	}
	expected := []int64{0, 99, 0}
	for i, want := range expected {
		got, ok := elems[i].Int64()
		if !ok || got != want {
			t.Errorf("arr[%d] = %d, want %d", i, got, want)
		}
	}
}

func TestWriteArrayRangeVerifyPatching(t *testing.T) {
	client := altAccessTestSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	name := ObjectName{Scope: ObjectScopeDomain, Domain: "dom", ItemID: "wArr"}

	_, err := client.WriteVariables(ctx,
		[]VariableSpec{{Name: name, AlternateAccess: []AccessSelector{{IndexRange: &IndexRange{Start: 0, Count: 2}}}}},
		[]*Value{NewArray([]*Value{NewInteger(77), NewInteger(88)})},
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := client.ReadVariables(ctx, []VariableSpec{{Name: name}})
	if err != nil {
		t.Fatal(err)
	}
	elems, ok := result[0].Value.ArrayElements()
	if !ok || len(elems) != 3 {
		t.Fatal("expected 3-element array")
	}
	expected := []int64{77, 88, 0}
	for i, want := range expected {
		got, ok := elems[i].Int64()
		if !ok || got != want {
			t.Errorf("arr[%d] = %d, want %d", i, got, want)
		}
	}
}

func TestReadComponentByNameEndToEnd(t *testing.T) {
	client := altAccessTestSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	result, err := client.ReadComponent(ctx, ObjectName{
		Scope: ObjectScopeDomain, Domain: "dom", ItemID: "struc",
	}, "temperature")
	if err != nil {
		t.Fatal(err)
	}

	f, ok := result.Value.Float64()
	if !ok || f != 21.5 {
		t.Errorf("got %v/%v, want 21.5", f, ok)
	}

	result, err = client.ReadComponent(ctx, ObjectName{
		Scope: ObjectScopeDomain, Domain: "dom", ItemID: "struc",
	}, "valid")
	if err != nil {
		t.Fatal(err)
	}

	b, ok := result.Value.Bool()
	if !ok || !b {
		t.Errorf("got %v/%v, want true", b, ok)
	}
}

func TestWriteComponentByNameEndToEnd(t *testing.T) {
	client := altAccessTestSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	name := ObjectName{Scope: ObjectScopeDomain, Domain: "dom", ItemID: "wStruc"}

	err := client.WriteComponent(ctx, name, "beta", NewFloat(9.99))
	if err != nil {
		t.Fatal(err)
	}

	result, err := client.ReadVariables(ctx, []VariableSpec{{Name: name}})
	if err != nil {
		t.Fatal(err)
	}
	elems, ok := result[0].Value.Structure()
	if !ok || len(elems) != 2 {
		t.Fatal("expected 2-element structure")
	}
	alpha, _ := elems[0].Int64()
	if alpha != 100 {
		t.Errorf("alpha = %d, want 100 (unchanged)", alpha)
	}
	beta, _ := elems[1].Float64()
	if beta < 9.98 || beta > 10.0 {
		t.Errorf("beta = %v, want ~9.99", beta)
	}
}

func TestReadByIndexEndToEnd(t *testing.T) {
	client := altAccessTestSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	result, err := client.ReadByIndex(ctx, ObjectName{
		Scope: ObjectScopeDomain, Domain: "dom", ItemID: "struc",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := result.Value.Float64()
	if !ok || f != 21.5 {
		t.Errorf("got %v/%v, want 21.5", f, ok)
	}
}

func TestSelectorValidation_NegativeIndex(t *testing.T) {
	client := altAccessTestSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	neg := -1
	_, err := client.ReadVariables(ctx, []VariableSpec{{
		Name:            ObjectName{Scope: ObjectScopeDomain, Domain: "dom", ItemID: "arr"},
		AlternateAccess: []AccessSelector{{Index: &neg}},
	}})
	if err == nil {
		t.Fatal("expected error for negative index")
	}
	t.Logf("expected error: %v", err)
}

func TestSelectorValidation_ZeroCountRange(t *testing.T) {
	client := altAccessTestSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	_, err := client.ReadVariables(ctx, []VariableSpec{{
		Name:            ObjectName{Scope: ObjectScopeDomain, Domain: "dom", ItemID: "arr"},
		AlternateAccess: []AccessSelector{{IndexRange: &IndexRange{Start: 0, Count: 0}}},
	}})
	if err == nil {
		t.Fatal("expected error for zero-count range")
	}
	t.Logf("expected error: %v", err)
}

func TestSelectorValidation_NegativeRangeStart(t *testing.T) {
	client := altAccessTestSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	_, err := client.ReadVariables(ctx, []VariableSpec{{
		Name:            ObjectName{Scope: ObjectScopeDomain, Domain: "dom", ItemID: "arr"},
		AlternateAccess: []AccessSelector{{IndexRange: &IndexRange{Start: -1, Count: 2}}},
	}})
	if err == nil {
		t.Fatal("expected error for negative range start")
	}
	t.Logf("expected error: %v", err)
}

func TestSelectorValidation_ConflictingFields(t *testing.T) {
	client := altAccessTestSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	idx := 0
	_, err := client.ReadVariables(ctx, []VariableSpec{{
		Name: ObjectName{Scope: ObjectScopeDomain, Domain: "dom", ItemID: "arr"},
		AlternateAccess: []AccessSelector{{
			Component: "foo",
			Index:     &idx,
		}},
	}})
	if err == nil {
		t.Fatal("expected error for conflicting selector fields")
	}
	t.Logf("expected error: %v", err)
}

func TestReadArrayElementOutOfBounds(t *testing.T) {
	client := altAccessTestSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.ReadArrayElement(ctx, ObjectName{
		Scope: ObjectScopeDomain, Domain: "dom", ItemID: "arr",
	}, 99)
	if err == nil {
		t.Fatal("expected error for out-of-bounds index")
	}
	t.Logf("out-of-bounds error: %v", err)

	_ = client.Close(ctx)
}

func TestReadVariablesPlainNoAccess(t *testing.T) {
	client := altAccessTestSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	results, err := client.ReadVariables(ctx, []VariableSpec{
		{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "dom", ItemID: "arr"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	elems, ok := results[0].Value.ArrayElements()
	if !ok || len(elems) != 5 {
		t.Errorf("expected array with 5 elements, got %v/%d", ok, len(elems))
	}

	_ = client.Close(ctx)
}
