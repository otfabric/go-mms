// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func nvlValueTestSetup(t *testing.T) *Client {
	t.Helper()
	srv := testServer(t)

	if err := srv.RegisterDomain("dom"); err != nil {
		t.Fatal(err)
	}

	temp := 21.5
	var valid = true
	var mu sync.Mutex

	srv.RegisterVariable(Variable{
		Name:     ObjectName{Scope: ObjectScopeDomain, Domain: "dom", ItemID: "temp"},
		TypeSpec: TypeSpec{Type: ValueTypeFloat, FormatWidth: 64, ExponentWidth: 11},
		Read: func(_ context.Context) (*Value, error) {
			mu.Lock()
			defer mu.Unlock()
			return NewFloat(temp), nil
		},
		Write: func(_ context.Context, v *Value) error {
			mu.Lock()
			defer mu.Unlock()
			f, ok := v.Float64()
			if !ok {
				return fmt.Errorf("type mismatch")
			}
			temp = f
			return nil
		},
	})

	srv.RegisterVariable(Variable{
		Name:     ObjectName{Scope: ObjectScopeDomain, Domain: "dom", ItemID: "valid"},
		TypeSpec: TypeSpec{Type: ValueTypeBoolean},
		Read: func(_ context.Context) (*Value, error) {
			mu.Lock()
			defer mu.Unlock()
			return NewBoolean(valid), nil
		},
		Write: func(_ context.Context, v *Value) error {
			mu.Lock()
			defer mu.Unlock()
			b, ok := v.Bool()
			if !ok {
				return fmt.Errorf("type mismatch")
			}
			valid = b
			return nil
		},
	})

	client := connectClientServer(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.DefineNamedVariableList(ctx, DefineNamedVariableListRequest{
		ListName: ObjectName{Scope: ObjectScopeDomain, Domain: "dom", ItemID: "myList"},
		Variables: []VariableSpec{
			{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "dom", ItemID: "temp"}},
			{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "dom", ItemID: "valid"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	return client
}

func TestReadNamedVariableListEndToEnd(t *testing.T) {
	client := nvlValueTestSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	results, err := client.ReadNamedVariableList(ctx, ObjectName{
		Scope: ObjectScopeDomain, Domain: "dom", ItemID: "myList",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	f, ok := results[0].Value.Float64()
	if !ok || f != 21.5 {
		t.Errorf("results[0] = %v/%v, want 21.5", f, ok)
	}

	b, ok := results[1].Value.Bool()
	if !ok || !b {
		t.Errorf("results[1] = %v/%v, want true", b, ok)
	}

	client.Close(ctx)
}

func TestWriteNamedVariableListEndToEnd(t *testing.T) {
	client := nvlValueTestSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.WriteNamedVariableList(ctx, ObjectName{
		Scope: ObjectScopeDomain, Domain: "dom", ItemID: "myList",
	}, []*Value{
		NewFloat(42.0),
		NewBoolean(false),
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := client.ReadNamedVariableList(ctx, ObjectName{
		Scope: ObjectScopeDomain, Domain: "dom", ItemID: "myList",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	f, ok := results[0].Value.Float64()
	if !ok || f != 42.0 {
		t.Errorf("after write: results[0] = %v/%v, want 42.0", f, ok)
	}

	b, ok := results[1].Value.Bool()
	if !ok || b {
		t.Errorf("after write: results[1] = %v/%v, want false", b, ok)
	}

	client.Close(ctx)
}

func TestReadObjectEndToEnd(t *testing.T) {
	client := nvlValueTestSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.ReadObject(ctx, ObjectName{
		Scope: ObjectScopeDomain, Domain: "dom", ItemID: "temp",
	})
	if err != nil {
		t.Fatal(err)
	}

	f, ok := result.Value.Float64()
	if !ok || f != 21.5 {
		t.Errorf("got %v/%v, want 21.5", f, ok)
	}

	client.Close(ctx)
}

func TestWriteObjectEndToEnd(t *testing.T) {
	client := nvlValueTestSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.WriteObject(ctx, ObjectName{
		Scope: ObjectScopeDomain, Domain: "dom", ItemID: "temp",
	}, NewFloat(99.9))
	if err != nil {
		t.Fatal(err)
	}

	result, err := client.ReadObject(ctx, ObjectName{
		Scope: ObjectScopeDomain, Domain: "dom", ItemID: "temp",
	})
	if err != nil {
		t.Fatal(err)
	}

	f, ok := result.Value.Float64()
	if !ok || f != 99.9 {
		t.Errorf("after write: got %v/%v, want 99.9", f, ok)
	}

	client.Close(ctx)
}

func TestReadNVLUnknownList(t *testing.T) {
	client := nvlValueTestSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.ReadNamedVariableList(ctx, ObjectName{
		Scope: ObjectScopeDomain, Domain: "dom", ItemID: "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for non-existent NVL")
	}

	var svcErr *ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("expected ServiceError, got %T: %v", err, err)
	}
	if svcErr.Class != ErrorClassAccess {
		t.Errorf("error class = %s, want Access (object-non-existent)", svcErr.Class)
	}

	client.Close(ctx)
}

func TestWriteNVLUnknownList(t *testing.T) {
	client := nvlValueTestSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.WriteNamedVariableList(ctx, ObjectName{
		Scope: ObjectScopeDomain, Domain: "dom", ItemID: "nonexistent",
	}, []*Value{NewFloat(1.0)})
	if err == nil {
		t.Fatal("expected error for non-existent NVL write")
	}

	var svcErr *ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("expected ServiceError, got %T: %v", err, err)
	}
	if svcErr.Class != ErrorClassAccess {
		t.Errorf("error class = %s, want Access (object-non-existent)", svcErr.Class)
	}

	client.Close(ctx)
}
