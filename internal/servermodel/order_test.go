// SPDX-License-Identifier: MIT

package servermodel

import (
	"fmt"
	"slices"
	"testing"
)

func TestInsertSorted(t *testing.T) {
	var names []string
	for i := range 500 { // 7919 is prime: a fixed, unsorted insertion order
		names = append(names, fmt.Sprintf("LN%d$ST$x%d", (i*7919)%50, i))
	}
	var order []string
	for _, n := range names {
		order = InsertSorted(order, n)
		if !slices.IsSorted(order) {
			t.Fatalf("not sorted after inserting %q", n)
		}
	}
	want := slices.Clone(names)
	slices.Sort(want)
	if !slices.Equal(order, want) {
		t.Fatal("InsertSorted result differs from a sort of the inputs")
	}
	if got := InsertSorted(nil, "a"); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("insert into nil: %v", got)
	}
}

// BenchmarkRegisterVariables registers a model the size of a large
// substation IED (tens of thousands of variables in one domain).
func BenchmarkRegisterVariables(b *testing.B) {
	const n = 30000
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("LN%05d$ST$stVal", (i*7919)%n)
	}
	for b.Loop() {
		r := NewRegistry()
		if err := r.RegisterDomain("LD"); err != nil {
			b.Fatal(err)
		}
		for _, id := range ids {
			if err := r.RegisterVariable(&VarEntry{Domain: "LD", ItemID: id, Scope: 1}); err != nil {
				b.Fatal(err)
			}
		}
	}
}
