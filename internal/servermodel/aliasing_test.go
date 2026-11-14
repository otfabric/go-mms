// SPDX-License-Identifier: MIT

package servermodel

import (
	"fmt"
	"slices"
	"sync"
	"testing"
)

// aliasRegistry has three names in every list.
func aliasRegistry(t *testing.T) *Registry {
	t.Helper()
	r := NewRegistry()
	for _, d := range []string{"d1", "d2", "d3"} {
		if err := r.RegisterDomain(d); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []string{"a", "b", "c"} {
		for _, e := range []error{
			r.RegisterVariable(&VarEntry{Domain: "d1", ItemID: n, Scope: 1}),
			r.RegisterVariable(&VarEntry{ItemID: n, Scope: 0}),
			r.DefineNVL(&NVLEntry{Domain: "d1", ItemID: n, Scope: 1, Deletable: true}),
			r.DefineNVL(&NVLEntry{ItemID: n, Scope: 0, Deletable: true}),
		} {
			if e != nil {
				t.Fatal(e)
			}
		}
	}
	return r
}

// A page of names is the caller's: changing it does not change the
// registry, and the registry changing does not change the page.
func TestListResultsAreCopies(t *testing.T) {
	for name, list := range map[string]func(*Registry, string, int) NameListResult{
		"domains":        (*Registry).ListDomains,
		"domain vars":    func(r *Registry, after string, n int) NameListResult { return r.ListDomainVariables("d1", after, n) },
		"vmd vars":       (*Registry).ListVMDVariables,
		"domain nvls":    func(r *Registry, after string, n int) NameListResult { return r.ListDomainNVLs("d1", after, n) },
		"vmd nvls":       (*Registry).ListVMDNVLs,
		"page of a list": func(r *Registry, _ string, _ int) NameListResult { return r.ListVMDNVLs("", 2) },
	} {
		r := aliasRegistry(t)
		first := list(r, "", 0)
		want := slices.Clone(list(r, "", 0).Names)
		for i := range first.Names {
			first.Names[i] = "changed"
		}
		if got := list(r, "", 0).Names; !slices.Equal(got, want) {
			t.Errorf("%s: after changing a result, the registry lists %q, want %q", name, got, want)
		}
		page := list(r, "", 0)
		snapshot := slices.Clone(page.Names)
		// Removing the first list shifts the sorted order in place.
		r.DeleteNVL(1, "d1", "a")
		r.DeleteNVL(0, "", "a")
		if !slices.Equal(page.Names, snapshot) {
			t.Errorf("%s: a page changed from %q to %q when the registry changed", name, snapshot, page.Names)
		}
	}
}

// One association lists named variable lists while another defines and
// deletes them: run with -race.
func TestListWhileDefiningAndDeleting(t *testing.T) {
	r := NewRegistry()
	for i := range 50 {
		if err := r.DefineNVL(&NVLEntry{ItemID: fmt.Sprintf("base%02d", i), Scope: 0, Deletable: true}); err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		<-start
		for i := range 2000 {
			name := fmt.Sprintf("tmp%04d", i)
			_ = r.DefineNVL(&NVLEntry{ItemID: name, Scope: 0, Deletable: true})
			r.DeleteNVL(0, "", name)
		}
	})
	wg.Go(func() {
		<-start
		for range 2000 {
			total := 0
			for _, n := range r.ListVMDNVLs("", 0).Names {
				total += len(n)
			}
			if total == 0 {
				t.Error("empty list")
				return
			}
		}
	})
	close(start)
	wg.Wait()
}
