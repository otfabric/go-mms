// SPDX-License-Identifier: MIT

package servermodel

import "testing"

func TestRegistryDomainLifecycle(t *testing.T) {
	r := NewRegistry()

	if err := r.RegisterDomain("alpha"); err != nil {
		t.Fatalf("register alpha: %v", err)
	}
	if err := r.RegisterDomain("beta"); err != nil {
		t.Fatalf("register beta: %v", err)
	}

	// Duplicate should fail.
	if err := r.RegisterDomain("alpha"); err == nil {
		t.Fatal("expected duplicate domain error")
	}

	// Empty name should fail.
	if err := r.RegisterDomain(""); err == nil {
		t.Fatal("expected empty domain error")
	}

	if !r.HasDomain("alpha") {
		t.Error("alpha not found")
	}
	if r.HasDomain("gamma") {
		t.Error("gamma unexpectedly found")
	}
}

func TestRegistryVariableLifecycle(t *testing.T) {
	r := NewRegistry()
	if err := r.RegisterDomain("dom"); err != nil {
		t.Fatal(err)
	}

	entry := &VarEntry{Domain: "dom", ItemID: "temp", Scope: 1}
	if err := r.RegisterVariable(entry); err != nil {
		t.Fatalf("register variable: %v", err)
	}

	// Duplicate should fail.
	if err := r.RegisterVariable(entry); err == nil {
		t.Fatal("expected duplicate variable error")
	}

	// Domain-scoped with empty domain should fail.
	if err := r.RegisterVariable(&VarEntry{ItemID: "x", Scope: 1}); err == nil {
		t.Fatal("expected empty domain error")
	}

	// Unknown domain should fail.
	if err := r.RegisterVariable(&VarEntry{Domain: "nope", ItemID: "x", Scope: 1}); err == nil {
		t.Fatal("expected unknown domain error")
	}

	// Empty ItemID should fail.
	if err := r.RegisterVariable(&VarEntry{Domain: "dom", Scope: 1}); err == nil {
		t.Fatal("expected empty ItemID error")
	}

	v, ok := r.LookupVariable(1, "dom", "temp")
	if !ok || v.ItemID != "temp" {
		t.Errorf("lookup failed: ok=%v, v=%v", ok, v)
	}

	_, ok = r.LookupVariable(1, "dom", "missing")
	if ok {
		t.Error("lookup of missing variable succeeded")
	}
}

func TestRegistryListDomains(t *testing.T) {
	r := NewRegistry()
	for _, name := range []string{"gamma", "alpha", "beta"} {
		if err := r.RegisterDomain(name); err != nil {
			t.Fatal(err)
		}
	}

	result := r.ListDomains("", 0)
	if len(result.Names) != 3 {
		t.Fatalf("count = %d, want 3", len(result.Names))
	}
	if result.Names[0] != "alpha" || result.Names[1] != "beta" || result.Names[2] != "gamma" {
		t.Errorf("order = %v, want [alpha beta gamma]", result.Names)
	}
	if result.MoreFollows {
		t.Error("unexpected MoreFollows")
	}
}

func TestRegistryPagination(t *testing.T) {
	r := NewRegistry()
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		if err := r.RegisterDomain(name); err != nil {
			t.Fatal(err)
		}
	}

	// Page of 2
	result := r.ListDomains("", 2)
	if len(result.Names) != 2 {
		t.Fatalf("page 1: count = %d, want 2", len(result.Names))
	}
	if !result.MoreFollows {
		t.Error("page 1: expected MoreFollows")
	}

	// Continue after "b"
	result = r.ListDomains("b", 2)
	if len(result.Names) != 2 {
		t.Fatalf("page 2: count = %d, want 2", len(result.Names))
	}
	if result.Names[0] != "c" || result.Names[1] != "d" {
		t.Errorf("page 2: names = %v", result.Names)
	}

	// Continue after "d"
	result = r.ListDomains("d", 2)
	if len(result.Names) != 1 || result.Names[0] != "e" {
		t.Errorf("page 3: names = %v", result.Names)
	}
	if result.MoreFollows {
		t.Error("page 3: unexpected MoreFollows")
	}
}

func TestRegistryListDomainVariables(t *testing.T) {
	r := NewRegistry()
	if err := r.RegisterDomain("proc"); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"z_var", "a_var", "m_var"} {
		if err := r.RegisterVariable(&VarEntry{Domain: "proc", ItemID: name, Scope: 1}); err != nil {
			t.Fatal(err)
		}
	}

	result := r.ListDomainVariables("proc", "", 0)
	if len(result.Names) != 3 {
		t.Fatalf("count = %d, want 3", len(result.Names))
	}
	if result.Names[0] != "a_var" || result.Names[1] != "m_var" || result.Names[2] != "z_var" {
		t.Errorf("names = %v", result.Names)
	}
}

func TestRegistryVMDVariables(t *testing.T) {
	r := NewRegistry()

	if err := r.RegisterVariable(&VarEntry{ItemID: "vmd_b", Scope: 0}); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterVariable(&VarEntry{ItemID: "vmd_a", Scope: 0}); err != nil {
		t.Fatal(err)
	}

	result := r.ListVMDVariables("", 0)
	if len(result.Names) != 2 {
		t.Fatalf("count = %d, want 2", len(result.Names))
	}
	if result.Names[0] != "vmd_a" || result.Names[1] != "vmd_b" {
		t.Errorf("names = %v", result.Names)
	}
}

func TestDefineAndLookupNVL(t *testing.T) {
	r := NewRegistry()
	if err := r.RegisterDomain("dom1"); err != nil {
		t.Fatal(err)
	}

	domNVL := &NVLEntry{
		Domain:    "dom1",
		ItemID:    "nvl_a",
		Scope:     1,
		Deletable: true,
		Variables: []NVLVariable{
			{Scope: 1, DomainID: "dom1", ItemID: "var1"},
		},
	}
	if err := r.DefineNVL(domNVL); err != nil {
		t.Fatalf("define domain NVL: %v", err)
	}

	got, ok := r.LookupNVL(1, "dom1", "nvl_a")
	if !ok {
		t.Fatal("domain NVL not found")
	}
	if got.ItemID != "nvl_a" || got.Domain != "dom1" {
		t.Errorf("lookup got %+v", got)
	}
	if len(got.Variables) != 1 || got.Variables[0].ItemID != "var1" {
		t.Errorf("variables = %+v", got.Variables)
	}

	vmdNVL := &NVLEntry{
		ItemID:    "vmd_list",
		Scope:     0,
		Deletable: true,
		Variables: []NVLVariable{
			{Scope: 0, ItemID: "vmd_var1"},
		},
	}
	if err := r.DefineNVL(vmdNVL); err != nil {
		t.Fatalf("define VMD NVL: %v", err)
	}

	got, ok = r.LookupNVL(0, "", "vmd_list")
	if !ok {
		t.Fatal("VMD NVL not found")
	}
	if got.ItemID != "vmd_list" || got.Scope != 0 {
		t.Errorf("lookup got %+v", got)
	}

	if err := r.DefineNVL(domNVL); err == nil {
		t.Error("expected error for duplicate NVL")
	}

	if err := r.DefineNVL(&NVLEntry{Domain: "noexist", ItemID: "x", Scope: 1}); err == nil {
		t.Error("expected error for unregistered domain")
	}

	if err := r.DefineNVL(&NVLEntry{Domain: "dom1", Scope: 1}); err == nil {
		t.Error("expected error for empty ItemID")
	}

	if err := r.DefineNVL(&NVLEntry{ItemID: "x", Scope: 1}); err == nil {
		t.Error("expected error for domain-scoped NVL with empty domain")
	}

	_, ok = r.LookupNVL(1, "dom1", "nonexistent")
	if ok {
		t.Error("lookup of nonexistent NVL should return false")
	}
}

func TestDeleteNVL(t *testing.T) {
	r := NewRegistry()
	if err := r.RegisterDomain("dom1"); err != nil {
		t.Fatal(err)
	}

	deletable := &NVLEntry{
		Domain:    "dom1",
		ItemID:    "del_nvl",
		Scope:     1,
		Deletable: true,
	}
	if err := r.DefineNVL(deletable); err != nil {
		t.Fatal(err)
	}

	if !r.DeleteNVL(1, "dom1", "del_nvl") {
		t.Error("delete of deletable NVL returned false")
	}
	if _, ok := r.LookupNVL(1, "dom1", "del_nvl"); ok {
		t.Error("deleted NVL still found")
	}

	nonDeletable := &NVLEntry{
		Domain:    "dom1",
		ItemID:    "perm_nvl",
		Scope:     1,
		Deletable: false,
	}
	if err := r.DefineNVL(nonDeletable); err != nil {
		t.Fatal(err)
	}
	if r.DeleteNVL(1, "dom1", "perm_nvl") {
		t.Error("delete of non-deletable NVL should return false")
	}
	if _, ok := r.LookupNVL(1, "dom1", "perm_nvl"); !ok {
		t.Error("non-deletable NVL should still exist")
	}

	if r.DeleteNVL(1, "dom1", "no_such_nvl") {
		t.Error("delete of nonexistent NVL should return false")
	}

	vmdDel := &NVLEntry{ItemID: "vmd_del", Scope: 0, Deletable: true}
	if err := r.DefineNVL(vmdDel); err != nil {
		t.Fatal(err)
	}
	if !r.DeleteNVL(0, "", "vmd_del") {
		t.Error("delete of VMD deletable NVL returned false")
	}
	if _, ok := r.LookupNVL(0, "", "vmd_del"); ok {
		t.Error("deleted VMD NVL still found")
	}
}

func TestListNVLs(t *testing.T) {
	r := NewRegistry()
	if err := r.RegisterDomain("dom1"); err != nil {
		t.Fatal(err)
	}

	names := []string{"nvl_c", "nvl_a", "nvl_b", "nvl_d", "nvl_e"}
	for _, name := range names {
		if err := r.DefineNVL(&NVLEntry{Domain: "dom1", ItemID: name, Scope: 1, Deletable: true}); err != nil {
			t.Fatalf("define %s: %v", name, err)
		}
	}

	result := r.ListDomainNVLs("dom1", "", 0)
	if len(result.Names) != 5 {
		t.Fatalf("count = %d, want 5", len(result.Names))
	}
	for i := 1; i < len(result.Names); i++ {
		if result.Names[i] < result.Names[i-1] {
			t.Errorf("not sorted: %v", result.Names)
			break
		}
	}
	if result.MoreFollows {
		t.Error("unexpected MoreFollows for full list")
	}

	result = r.ListDomainNVLs("dom1", "", 2)
	if len(result.Names) != 2 {
		t.Fatalf("page 1: count = %d, want 2", len(result.Names))
	}
	if !result.MoreFollows {
		t.Error("page 1: expected MoreFollows")
	}
	last := result.Names[len(result.Names)-1]

	result = r.ListDomainNVLs("dom1", last, 2)
	if len(result.Names) != 2 {
		t.Fatalf("page 2: count = %d, want 2", len(result.Names))
	}
	last = result.Names[len(result.Names)-1]

	result = r.ListDomainNVLs("dom1", last, 2)
	if len(result.Names) != 1 {
		t.Fatalf("page 3: count = %d, want 1", len(result.Names))
	}
	if result.MoreFollows {
		t.Error("page 3: unexpected MoreFollows")
	}

	vmdNames := []string{"vmd_c", "vmd_a", "vmd_b"}
	for _, name := range vmdNames {
		if err := r.DefineNVL(&NVLEntry{ItemID: name, Scope: 0, Deletable: true}); err != nil {
			t.Fatalf("define VMD %s: %v", name, err)
		}
	}

	result = r.ListVMDNVLs("", 0)
	if len(result.Names) != 3 {
		t.Fatalf("VMD count = %d, want 3", len(result.Names))
	}
	if result.Names[0] != "vmd_a" || result.Names[1] != "vmd_b" || result.Names[2] != "vmd_c" {
		t.Errorf("VMD order = %v, want [vmd_a vmd_b vmd_c]", result.Names)
	}

	result = r.ListVMDNVLs("", 2)
	if len(result.Names) != 2 || !result.MoreFollows {
		t.Errorf("VMD page 1: names=%v, more=%v", result.Names, result.MoreFollows)
	}
	result = r.ListVMDNVLs(result.Names[1], 2)
	if len(result.Names) != 1 || result.MoreFollows {
		t.Errorf("VMD page 2: names=%v, more=%v", result.Names, result.MoreFollows)
	}

	if err := r.RegisterDomain("empty_dom"); err != nil {
		t.Fatal(err)
	}
	result = r.ListDomainNVLs("empty_dom", "", 0)
	if len(result.Names) != 0 {
		t.Errorf("empty domain: count = %d, want 0", len(result.Names))
	}
}

func TestDeleteAllDomainNVLs(t *testing.T) {
	r := NewRegistry()
	if err := r.RegisterDomain("dom1"); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"list_a", "list_b", "list_c"} {
		err := r.DefineNVL(&NVLEntry{
			ItemID:    name,
			Scope:     1,
			Domain:    "dom1",
			Deletable: true,
		})
		if err != nil {
			t.Fatalf("define %s: %v", name, err)
		}
	}

	err := r.DefineNVL(&NVLEntry{
		ItemID:    "list_static",
		Scope:     1,
		Domain:    "dom1",
		Deletable: false,
	})
	if err != nil {
		t.Fatalf("define static: %v", err)
	}

	matched, deleted := r.DeleteAllDomainNVLs("dom1")
	if matched != 4 {
		t.Errorf("matched = %d, want 4", matched)
	}
	if deleted != 3 {
		t.Errorf("deleted = %d, want 3", deleted)
	}

	remaining := r.ListDomainNVLs("dom1", "", 0)
	if len(remaining.Names) != 1 || remaining.Names[0] != "list_static" {
		t.Errorf("remaining = %v, want [list_static]", remaining.Names)
	}
}

func TestDeleteAllDomainNVLsEmpty(t *testing.T) {
	r := NewRegistry()
	if err := r.RegisterDomain("empty"); err != nil {
		t.Fatal(err)
	}
	matched, deleted := r.DeleteAllDomainNVLs("empty")
	if matched != 0 || deleted != 0 {
		t.Errorf("got (%d, %d), want (0, 0)", matched, deleted)
	}
}

func TestDeleteAllVMDNVLs(t *testing.T) {
	r := NewRegistry()

	for _, name := range []string{"vmd_a", "vmd_b"} {
		err := r.DefineNVL(&NVLEntry{
			ItemID:    name,
			Scope:     0,
			Deletable: true,
		})
		if err != nil {
			t.Fatalf("define %s: %v", name, err)
		}
	}

	err := r.DefineNVL(&NVLEntry{
		ItemID:    "vmd_static",
		Scope:     0,
		Deletable: false,
	})
	if err != nil {
		t.Fatalf("define static: %v", err)
	}

	matched, deleted := r.DeleteAllVMDNVLs()
	if matched != 3 {
		t.Errorf("matched = %d, want 3", matched)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}

	remaining := r.ListVMDNVLs("", 0)
	if len(remaining.Names) != 1 || remaining.Names[0] != "vmd_static" {
		t.Errorf("remaining = %v, want [vmd_static]", remaining.Names)
	}
}

func TestDeleteAllVMDNVLsEmpty(t *testing.T) {
	r := NewRegistry()
	matched, deleted := r.DeleteAllVMDNVLs()
	if matched != 0 || deleted != 0 {
		t.Errorf("got (%d, %d), want (0, 0)", matched, deleted)
	}
}
