// SPDX-License-Identifier: MIT

package e2e

import (
	"bytes"
	"context"
	"sync"
	"testing"

	mms "github.com/otfabric/go-mms"
)

// TestDecode_CapturedAssociation decodes, as a passive trace tool would,
// every ISO upper-layer unit of an association over TCP: both directions,
// from the association to the conclude.
func TestDecode_CapturedAssociation(t *testing.T) {
	srv := mms.NewServer(mms.ServerOptions{})
	if err := srv.RegisterDomain("d"); err != nil {
		t.Fatal(err)
	}
	name := mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "d", ItemID: "v"}
	if err := srv.RegisterVariable(mms.Variable{
		Name: name, TypeSpec: mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 32},
		Read:  func(context.Context) (*mms.Value, error) { return mms.NewInteger(7), nil },
		Write: func(context.Context, *mms.Value) error { return nil },
	}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var units [][]byte
	c := dial(t, serve(t, srv), mms.DialOptions{RawHook: func(_ string, raw []byte) {
		mu.Lock()
		defer mu.Unlock()
		units = append(units, bytes.Clone(raw))
	}})
	ctx := timeout(t)
	if _, err := c.ReadObject(ctx, name); err != nil {
		t.Fatal(err)
	}
	if _, err := c.WriteObject(ctx, name, mms.NewInteger(3)); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(ctx); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	kinds := map[string]bool{}
	for _, u := range units {
		got, err := mms.DecodeTSDU(u)
		if err != nil {
			t.Fatalf("%x: %v", u, err)
		}
		if got.PDU == nil {
			continue
		}
		kinds[got.PDU.Kind+" "+got.PDU.Service] = true
		if got.PDU.Kind == "confirmed-response" && got.PDU.Service == "Read" {
			if n, _ := got.PDU.Values[0].Int64(); n != 7 {
				t.Errorf("read value = %v", got.PDU.Values[0])
			}
		}
	}
	for _, k := range []string{"initiate-request ", "initiate-response ", "confirmed-request Read", "confirmed-response Read",
		"confirmed-request Write", "confirmed-response Write", "conclude-request ", "conclude-response "} {
		if !kinds[k] {
			t.Errorf("no %q in %v", k, kinds)
		}
	}
	// An MMS PDU alone decodes too.
	if p, err := mms.DecodePDU([]byte{0x8b, 0x00}); err != nil || p.Kind != "conclude-request" {
		t.Errorf("DecodePDU(conclude) = %+v, %v", p, err)
	}
}
