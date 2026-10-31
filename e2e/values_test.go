// SPDX-License-Identifier: MIT

package e2e

import (
	"reflect"
	"testing"

	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/internal/chaos"
)

// Every value type survives read, write and read-back over the wire, and
// every type specification survives GetVariableAccessAttributes.
func TestValues_RoundTrip(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)

	for _, vc := range valueCases() {
		t.Run(vc.name, func(t *testing.T) {
			req := mms.ReadRequest{DomainID: dom, ItemID: mms.ItemID(vc.name)}
			got, err := c.Read(ctx, req)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			mustEqual(t, "initial", got.Value, vc.initial)

			attrs, err := c.GetVariableAccessAttributes(ctx, domName(vc.name))
			if err != nil {
				t.Fatalf("GetVariableAccessAttributes: %v", err)
			}
			if !reflect.DeepEqual(attrs.TypeSpec, vc.ts) {
				t.Errorf("TypeSpec = %+v, want %+v", attrs.TypeSpec, vc.ts)
			}

			if _, err := c.Write(ctx, mms.WriteRequest{DomainID: dom, ItemID: mms.ItemID(vc.name), Value: vc.write}); err != nil {
				t.Fatalf("Write: %v", err)
			}
			mustEqual(t, "server value", m.get(dom+"/"+vc.name), vc.write)
			got, err = c.Read(ctx, req)
			if err != nil {
				t.Fatalf("Read after write: %v", err)
			}
			mustEqual(t, "read-back", got.Value, vc.write)
		})
	}
}

// Values without a wire representation fail locally instead of being
// silently mangled.
func TestValues_Unencodable(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)
	for name, v := range map[string]*mms.Value{
		"NewBinaryTime":       mms.NewBinaryTime(1 << 32), // 1970-02-19: before the 1984 epoch, longer than a time of day
		"NewObjectIdentifier": mms.NewObjectIdentifier([]int{3, 1}),
	} {
		before := m.get(dom + "/" + name)
		if _, err := c.Write(ctx, mms.WriteRequest{DomainID: dom, ItemID: mms.ItemID(name), Value: v}); err == nil {
			t.Errorf("Write %s %v: expected error", name, v)
		}
		mustEqual(t, name+" after rejected write", m.get(dom+"/"+name), before)
	}
	if _, err := c.Read(ctx, mms.ReadRequest{DomainID: dom, ItemID: "NewBoolean"}); err != nil {
		t.Errorf("client unusable after local encode failure: %v", err)
	}
}

// A server Read handler can return a DataAccessError value; the client
// surfaces it as *mms.DataAccessError.
func TestValues_DataAccessError(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	_, err := c.Read(timeout(t), mms.ReadRequest{DomainID: dom, ItemID: "unavailable"})
	if code := dataAccessCode(t, err); code != mms.DataAccessErrorTemporarilyUnavailable {
		t.Errorf("code = %v, want TemporarilyUnavailable", code)
	}
	if v := mms.NewDataAccessError(mms.DataAccessErrorObjectNonExistent); v.Type() != mms.ValueTypeDataAccessError {
		t.Errorf("NewDataAccessError type = %v", v.Type())
	}
}

// An MmsString that is not valid UTF-8 cannot leave either side, and the
// failed encode neither hangs the caller nor breaks the association.
func TestMmsStringInvalidUTF8(t *testing.T) {
	cs := startChaosServer(t, chaos.Config{}, mms.ServerOptions{})
	c := dial(t, cs.addr)
	bad := mms.NewMmsString("\xa5")

	if _, err := c.WriteObject(timeout(t), domName("v0"), bad); err == nil {
		t.Error("WriteObject of invalid UTF-8 succeeded")
	}

	cs.mu.Lock()
	cs.vals["v1"] = bad
	cs.mu.Unlock()
	if v, err := c.ReadObject(timeout(t), domName("v1")); err == nil {
		t.Errorf("ReadObject of invalid UTF-8 = %v", v)
	}

	if _, err := c.ReadObject(timeout(t), domName("v2")); err != nil {
		t.Errorf("association unusable after the rejected values: %v", err)
	}
}
