// SPDX-License-Identifier: MIT

package servermodel

import (
	"errors"
	"testing"
)

func TestDefinePeerNVLLimit(t *testing.T) {
	r := NewRegistry()
	if err := r.RegisterDomain("d"); err != nil {
		t.Fatal(err)
	}
	// Lists not marked PeerDefined are not counted nor limited.
	for _, n := range []string{"a1", "a2", "a3"} {
		if err := r.DefinePeerNVL(&NVLEntry{ItemID: n, Scope: 0, Deletable: true}, 1); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.DefinePeerNVL(&NVLEntry{ItemID: "p1", Scope: 1, Domain: "d", Deletable: true, PeerDefined: true}, 2); err != nil {
		t.Fatal(err)
	}
	if err := r.DefinePeerNVL(&NVLEntry{ItemID: "p2", Scope: 0, Deletable: true, PeerDefined: true}, 2); err != nil {
		t.Fatal(err)
	}
	err := r.DefinePeerNVL(&NVLEntry{ItemID: "p3", Scope: 0, Deletable: true, PeerDefined: true}, 2)
	if !errors.Is(err, ErrNVLLimit) {
		t.Fatalf("over limit: %v", err)
	}
	// A duplicate reports the duplicate, not the limit.
	if err := r.DefinePeerNVL(&NVLEntry{ItemID: "p2", Scope: 0, Deletable: true, PeerDefined: true}, 2); err == nil || errors.Is(err, ErrNVLLimit) {
		t.Fatalf("duplicate: %v", err)
	}
	if n := r.PeerNVLCount(); n != 2 {
		t.Fatalf("count = %d", n)
	}
	// No limit when maxPeer <= 0.
	if err := r.DefinePeerNVL(&NVLEntry{ItemID: "p3", Scope: 0, Deletable: true, PeerDefined: true}, 0); err != nil {
		t.Fatal(err)
	}
	if !r.DeleteNVL(0, "", "a1") || r.PeerNVLCount() != 3 {
		t.Fatalf("deleting an unmarked list changed the count: %d", r.PeerNVLCount())
	}
	if !r.DeleteNVL(0, "", "p3") || r.PeerNVLCount() != 2 {
		t.Fatalf("after DeleteNVL: %d", r.PeerNVLCount())
	}
	if _, del := r.DeleteAllDomainNVLs("d"); del != 1 || r.PeerNVLCount() != 1 {
		t.Fatalf("after DeleteAllDomainNVLs: %d", r.PeerNVLCount())
	}
	if _, del := r.DeleteAllVMDNVLs(); del != 3 || r.PeerNVLCount() != 0 {
		t.Fatalf("after DeleteAllVMDNVLs: del=%d count=%d", del, r.PeerNVLCount())
	}
}
