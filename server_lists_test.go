// SPDX-License-Identifier: MIT

package mms

import (
	"slices"
	"testing"

	"github.com/otfabric/go-mms/internal/servermodel"
)

// assocLists is an association with three lists and three variables.
func assocLists(t *testing.T) *ServerConn {
	t.Helper()
	sc := &ServerConn{}
	for _, n := range []string{"a", "b", "c"} {
		if err := sc.defineAssocNVL(&servermodel.NVLEntry{ItemID: n, Scope: 2, Deletable: true}); err != nil {
			t.Fatal(err)
		}
		if err := sc.registerAssocVar(&servermodel.VarEntry{ItemID: n, Scope: 2}); err != nil {
			t.Fatal(err)
		}
	}
	return sc
}

// The association-scoped name lists hand out copies: changing a result
// does not change the association, and the association changing does not
// change a result.
func TestAssocListResultsAreCopies(t *testing.T) {
	for name, list := range map[string]func(*ServerConn) servermodel.NameListResult{
		"nvls": func(sc *ServerConn) servermodel.NameListResult { return sc.listAssocNVLs("", 0) },
		"vars": func(sc *ServerConn) servermodel.NameListResult { return sc.listAssocVars("", 0) },
		"page": func(sc *ServerConn) servermodel.NameListResult { return sc.listAssocNVLs("", 2) },
	} {
		sc := assocLists(t)
		want := slices.Clone(list(sc).Names)
		first := list(sc)
		for i := range first.Names {
			first.Names[i] = "changed"
		}
		if got := list(sc).Names; !slices.Equal(got, want) {
			t.Errorf("%s: after changing a result the association lists %q, want %q", name, got, want)
		}
	}
	sc := assocLists(t)
	page := sc.listAssocNVLs("", 0)
	snapshot := slices.Clone(page.Names)
	sc.deleteAssocNVL("a") // shifts the order in place
	if !slices.Equal(page.Names, snapshot) {
		t.Errorf("a page changed from %q to %q when a list was deleted", snapshot, page.Names)
	}
}
