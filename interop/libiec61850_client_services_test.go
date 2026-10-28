//go:build interop

// SPDX-License-Identifier: MIT

// Tests in this file extend the go-mms client direction
//
//	go-mms client → libiec61850-mms-server
//
// beyond plain variable access: status, association lifecycle, name lists,
// alternate access, named variable lists in every scope, InformationReport,
// file services and journals.
package interop

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

// session starts a fresh libiec61850 server adapter and connects to it.
// Every session gets its own adapter process and file store.
func session(t *testing.T, opts ...iso.Option) (context.Context, *mms.Client) {
	t.Helper()
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	c := dial(t, ctx, h.addr, opts...)
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return ctx, c
}

func dv(item string) mms.ObjectName {
	return mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "interop", ItemID: mms.ItemID(item)}
}

func readInts(t *testing.T, v *mms.Value) []int64 {
	t.Helper()
	if v == nil {
		t.Fatal("no value (failed access result)")
	}
	elems, ok := v.ArrayElements()
	if !ok {
		elems, ok = v.Structure()
	}
	if !ok {
		t.Fatalf("value is not an array or structure: %s", v.Type())
	}
	out := make([]int64, len(elems))
	for i, e := range elems {
		n, ok := e.Int64()
		if !ok {
			t.Fatalf("element %d is %s, want integer", i, e.Type())
		}
		out[i] = n
	}
	return out
}

func equalInts(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Association lifecycle and VMD services
// ---------------------------------------------------------------------------

func TestClient_Status(t *testing.T) {
	ctx, c := session(t)
	fix := mustLoadFixture(t)

	st, err := c.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if int(st.Logical) != fix.Status.Logical || int(st.Physical) != fix.Status.Physical {
		t.Errorf("Status = %v/%v, want %d/%d", st.Logical, st.Physical, fix.Status.Logical, fix.Status.Physical)
	}

	st, err = c.StatusWithOptions(ctx, mms.ClientStatusRequest{ExtendedDerivation: true})
	if err != nil {
		t.Fatalf("StatusWithOptions(extended): %v", err)
	}
	if int(st.Logical) != fix.Status.Logical || int(st.Physical) != fix.Status.Physical {
		t.Errorf("extended Status = %v/%v, want %d/%d", st.Logical, st.Physical, fix.Status.Logical, fix.Status.Physical)
	}
}

func TestClient_Negotiated(t *testing.T) {
	const pdu = 4096
	ctx, c := session(t, iso.WithClientDialOptions(mms.DialOptions{MMS: mms.MMSOptions{MaxPDUSize: pdu}}))

	n := c.Negotiated()
	if n.MaxPDUSize != pdu {
		t.Errorf("negotiated MaxPDUSize = %d, want %d (the smaller of client and server)", n.MaxPDUSize, pdu)
	}
	if n.MaxOutCalling < 1 || n.MaxOutCalled < 1 || n.NestingLevel < 1 {
		t.Errorf("negotiated parameters out of range: %+v", n)
	}
	if n.ServerVersion < 1 {
		t.Errorf("negotiated ServerVersion = %d, want >= 1", n.ServerVersion)
	}
	if _, err := c.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "boolean"}); err != nil {
		t.Fatalf("Read with %d-byte PDU: %v", pdu, err)
	}
}

func TestClient_Conclude(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dial(t, ctx, h.addr)
	if err := c.Close(ctx); err != nil {
		t.Fatalf("Close (Conclude): %v", err)
	}
	if _, err := c.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "boolean"}); err == nil {
		t.Fatal("Read after Close succeeded")
	}
}

func TestClient_Abort(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dial(t, ctx, h.addr)
	if err := c.Abort(ctx); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if _, err := c.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "boolean"}); err == nil {
		t.Fatal("Read after Abort succeeded")
	}

	c2 := dial(t, ctx, h.addr)
	defer c2.Close(ctx)
	if _, err := c2.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "boolean"}); err != nil {
		t.Fatalf("server unusable after a peer aborted: %v", err)
	}
}

// TestClient_PeerDisconnect stops the libiec61850 server under an open
// association: Done is closed, Err reports ErrClosed, and requests fail
// with ErrClosed instead of hanging.
func TestClient_PeerDisconnect(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dial(t, ctx, h.addr)
	defer func() { _ = c.Abort(context.Background()) }()
	if err := c.Err(); err != nil {
		t.Fatalf("Err on a live association = %v", err)
	}
	select {
	case <-c.Done():
		t.Fatal("Done closed on a live association")
	default:
	}

	h.kill()
	select {
	case <-c.Done():
	case <-ctx.Done():
		t.Fatal("Done not closed after the peer went away")
	}
	if err := c.Err(); !errors.Is(err, mms.ErrClosed) {
		t.Errorf("Err = %v, want ErrClosed", err)
	}
	if _, err := c.Identify(ctx); !errors.Is(err, mms.ErrClosed) {
		t.Errorf("Identify after disconnect = %v, want ErrClosed", err)
	}
}

// ---------------------------------------------------------------------------
// Name lists and object access
// ---------------------------------------------------------------------------

func TestClient_GetNameListAll_Variables(t *testing.T) {
	ctx, c := session(t, iso.WithClientDialOptions(mms.DialOptions{MMS: mms.MMSOptions{MaxPDUSize: 512}}))
	fix := mustLoadFixture(t)

	names, err := c.GetNameListAll(ctx, mms.NameListRequest{
		ObjectClass: mms.ObjectClassNamedVariable,
		Scope:       mms.ObjectScopeDomain,
		DomainID:    "interop",
	})
	if err != nil {
		t.Fatalf("GetNameListAll: %v", err)
	}
	got := map[string]bool{}
	for _, n := range names {
		got[n] = true
	}
	for name := range fix.Domains["interop"].Variables {
		if !got[name] {
			t.Errorf("variable %q missing from GetNameListAll (%d names)", name, len(names))
		}
	}
}

func TestClient_GetNameList_Journals(t *testing.T) {
	ctx, c := session(t)

	res, err := c.GetNameList(ctx, mms.NameListRequest{
		ObjectClass: mms.ObjectClassJournal,
		Scope:       mms.ObjectScopeDomain,
		DomainID:    "interop",
	})
	if err != nil {
		t.Fatalf("GetNameList(journals): %v", err)
	}
	if len(res.Names) != 1 || res.Names[0] != "log1" {
		t.Errorf("journals = %v, want [log1]", res.Names)
	}
}

func TestClient_GetNameList_UnknownDomain(t *testing.T) {
	ctx, c := session(t)

	_, err := c.GetNameList(ctx, mms.NameListRequest{
		ObjectClass: mms.ObjectClassNamedVariable,
		Scope:       mms.ObjectScopeDomain,
		DomainID:    "unknown-domain",
	})
	var se *mms.ServiceError
	if !errors.As(err, &se) || se.Class != mms.ErrorClassAccess || se.Code != 2 {
		t.Fatalf("GetNameList(unknown domain) = %v, want access/object-non-existent", err)
	}
}

func TestClient_ReadObject_WriteObject(t *testing.T) {
	ctx, c := session(t)

	if _, err := c.WriteObject(ctx, dv("unsigned"), mms.NewUnsigned(789)); err != nil {
		t.Fatalf("WriteObject: %v", err)
	}
	rr, err := c.ReadObject(ctx, dv("unsigned"))
	if err != nil {
		t.Fatalf("ReadObject: %v", err)
	}
	if u, _ := rr.Value.Uint64(); u != 789 {
		t.Errorf("unsigned = %d after WriteObject, want 789", u)
	}
}

func TestClient_GetVariableAccessAttributes_Composite(t *testing.T) {
	ctx, c := session(t)

	arr, err := c.GetVariableAccessAttributes(ctx, dv("array-rw"))
	if err != nil {
		t.Fatalf("GetVariableAccessAttributes(array-rw): %v", err)
	}
	if arr.TypeSpec.Type != mms.ValueTypeArray || arr.TypeSpec.Count != 4 ||
		arr.TypeSpec.Element == nil || arr.TypeSpec.Element.Type != mms.ValueTypeInteger {
		t.Errorf("array-rw type = %+v, want array[4] of integer", arr.TypeSpec)
	}

	st, err := c.GetVariableAccessAttributes(ctx, dv("structure-rw"))
	if err != nil {
		t.Fatalf("GetVariableAccessAttributes(structure-rw): %v", err)
	}
	if st.TypeSpec.Type != mms.ValueTypeStructure || len(st.TypeSpec.Elements) != 2 {
		t.Fatalf("structure-rw type = %+v, want 2-component structure", st.TypeSpec)
	}
	if st.TypeSpec.Elements[0].Name != "flag" || st.TypeSpec.Elements[0].Type.Type != mms.ValueTypeBoolean ||
		st.TypeSpec.Elements[1].Name != "count" || st.TypeSpec.Elements[1].Type.Type != mms.ValueTypeInteger {
		t.Errorf("structure-rw components = %+v, want flag:boolean, count:integer", st.TypeSpec.Elements)
	}
}

// ---------------------------------------------------------------------------
// Alternate access
// ---------------------------------------------------------------------------

func TestClient_ReadArrayElement(t *testing.T) {
	ctx, c := session(t)

	rr, err := c.ReadArrayElement(ctx, dv("array"), 2)
	if err != nil {
		t.Fatalf("ReadArrayElement: %v", err)
	}
	if n, _ := rr.Value.Int64(); n != 3 {
		t.Errorf("array[2] = %v, want 3", rr.Value)
	}

	rr, err = c.ReadByIndex(ctx, dv("array"), 4)
	if err != nil {
		t.Fatalf("ReadByIndex: %v", err)
	}
	if n, _ := rr.Value.Int64(); n != 5 {
		t.Errorf("array[4] = %v, want 5", rr.Value)
	}
}

func TestClient_ReadArrayRange(t *testing.T) {
	ctx, c := session(t)

	rr, err := c.ReadArrayRange(ctx, dv("array"), 1, 3)
	if err != nil {
		t.Fatalf("ReadArrayRange: %v", err)
	}
	if got := readInts(t, rr.Value); !equalInts(got, []int64{2, 3, 4}) {
		t.Errorf("array[1:4] = %v, want [2 3 4]", got)
	}
}

func TestClient_ReadComponent(t *testing.T) {
	ctx, c := session(t)

	rr, err := c.ReadComponent(ctx, dv("structure-rw"), "count")
	if err != nil {
		t.Fatalf("ReadComponent: %v", err)
	}
	if n, _ := rr.Value.Int64(); n != 7 {
		t.Errorf("structure-rw.count = %v, want 7", rr.Value)
	}
}

func TestClient_WriteArrayElement(t *testing.T) {
	ctx, c := session(t)

	if err := c.WriteArrayElement(ctx, dv("array-rw"), 1, mms.NewInteger(21)); err != nil {
		t.Fatalf("WriteArrayElement: %v", err)
	}
	rr, err := c.ReadObject(ctx, dv("array-rw"))
	if err != nil {
		t.Fatalf("ReadObject: %v", err)
	}
	if got := readInts(t, rr.Value); !equalInts(got, []int64{10, 21, 30, 40}) {
		t.Errorf("array-rw = %v after element write, want [10 21 30 40]", got)
	}
}

func TestClient_WriteComponent(t *testing.T) {
	ctx, c := session(t)

	if err := c.WriteComponent(ctx, dv("structure-rw"), "count", mms.NewInteger(99)); err != nil {
		t.Fatalf("WriteComponent: %v", err)
	}
	rr, err := c.ReadObject(ctx, dv("structure-rw"))
	if err != nil {
		t.Fatalf("ReadObject: %v", err)
	}
	comps, ok := rr.Value.Structure()
	if !ok || len(comps) != 2 {
		t.Fatalf("structure-rw = %v, want 2 components", rr.Value)
	}
	if b, _ := comps[0].Bool(); b {
		t.Errorf("structure-rw.flag changed by a write to count")
	}
	if n, _ := comps[1].Int64(); n != 99 {
		t.Errorf("structure-rw.count = %v, want 99", comps[1])
	}
}

func TestClient_ReadVariables_WriteVariables_AlternateAccess(t *testing.T) {
	ctx, c := session(t)

	wres, err := c.WriteVariables(ctx,
		[]mms.VariableSpec{
			{Name: dv("array-rw"), AlternateAccess: []mms.AccessSelector{mms.SelectRange(2, 2)}},
			{Name: dv("structure-rw"), AlternateAccess: []mms.AccessSelector{mms.SelectComponent("flag")}},
		},
		[]*mms.Value{
			mms.NewArray([]*mms.Value{mms.NewInteger(33), mms.NewInteger(44)}),
			mms.NewBoolean(true),
		})
	if err != nil {
		t.Fatalf("WriteVariables: %v", err)
	}
	for _, r := range wres {
		if !r.Success {
			t.Errorf("WriteVariables[%d] failed: %v", r.Index, r.ErrorCode)
		}
	}

	rres, err := c.ReadVariables(ctx, []mms.VariableSpec{
		{Name: dv("array-rw"), AlternateAccess: []mms.AccessSelector{mms.SelectRange(2, 2)}},
		{Name: dv("structure-rw"), AlternateAccess: []mms.AccessSelector{mms.SelectComponent("flag")}},
		{Name: dv("array-rw"), AlternateAccess: []mms.AccessSelector{mms.SelectIndex(0)}},
	})
	if err != nil {
		t.Fatalf("ReadVariables: %v", err)
	}
	if len(rres) != 3 {
		t.Fatalf("ReadVariables returned %d results, want 3", len(rres))
	}
	if got := readInts(t, rres[0].Value); !equalInts(got, []int64{33, 44}) {
		t.Errorf("array-rw[2:4] = %v, want [33 44]", got)
	}
	if b, _ := rres[1].Value.Bool(); !b {
		t.Errorf("structure-rw.flag = %v, want true", rres[1].Value)
	}
	if n, _ := rres[2].Value.Int64(); n != 10 {
		t.Errorf("array-rw[0] = %v, want 10", rres[2].Value)
	}
}

// ---------------------------------------------------------------------------
// Named variable lists
// ---------------------------------------------------------------------------

func nvlMembers() []mms.VariableSpec {
	return []mms.VariableSpec{{Name: dv("boolean")}, {Name: dv("integer")}}
}

func TestClient_NVL_Attributes(t *testing.T) {
	ctx, c := session(t)
	list := dv("attrlist")

	if err := c.DefineNamedVariableList(ctx, mms.DefineNamedVariableListRequest{ListName: list, Variables: nvlMembers()}); err != nil {
		t.Fatalf("DefineNamedVariableList: %v", err)
	}
	attrs, err := c.GetNamedVariableListAttributes(ctx, list)
	if err != nil {
		t.Fatalf("GetNamedVariableListAttributes: %v", err)
	}
	if !attrs.Deletable {
		t.Error("client-defined list reported as not deletable")
	}
	if len(attrs.Variables) != 2 || attrs.Variables[0].Name != dv("boolean") || attrs.Variables[1].Name != dv("integer") {
		t.Errorf("list members = %+v, want interop/boolean, interop/integer", attrs.Variables)
	}

	names, err := c.GetNameList(ctx, mms.NameListRequest{
		ObjectClass: mms.ObjectClassNamedVariableList,
		Scope:       mms.ObjectScopeDomain,
		DomainID:    "interop",
	})
	if err != nil {
		t.Fatalf("GetNameList(named variable lists): %v", err)
	}
	if len(names.Names) != 1 || names.Names[0] != "attrlist" {
		t.Errorf("domain lists = %v, want [attrlist]", names.Names)
	}
}

func TestClient_NVL_SpecificationWithResult(t *testing.T) {
	ctx, c := session(t)
	list := dv("speclist")

	if err := c.DefineNamedVariableList(ctx, mms.DefineNamedVariableListRequest{ListName: list, Variables: nvlMembers()}); err != nil {
		t.Fatalf("DefineNamedVariableList: %v", err)
	}
	res, err := c.ReadNamedVariableList(ctx, list, mms.ReadNamedVariableListOptions{SpecificationWithResult: true})
	if err != nil {
		t.Fatalf("ReadNamedVariableList(specificationWithResult): %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("got %d results, want 2", len(res))
	}
	if b, _ := res[0].Value.Bool(); !b {
		t.Errorf("list[0] = %v, want true", res[0].Value)
	}
}

func TestClient_WriteNamedVariableList(t *testing.T) {
	ctx, c := session(t)
	list := dv("writelist")

	if err := c.DefineNamedVariableList(ctx, mms.DefineNamedVariableListRequest{ListName: list, Variables: nvlMembers()}); err != nil {
		t.Fatalf("DefineNamedVariableList: %v", err)
	}
	wres, err := c.WriteNamedVariableList(ctx, list, []*mms.Value{mms.NewBoolean(false), mms.NewInteger(5)})
	if err != nil {
		t.Fatalf("WriteNamedVariableList: %v", err)
	}
	for _, r := range wres {
		if !r.Success {
			t.Errorf("WriteNamedVariableList[%d] failed: %v", r.Index, r.ErrorCode)
		}
	}
	res, err := c.ReadMultiple(ctx, []mms.ObjectName{dv("boolean"), dv("integer")})
	if err != nil {
		t.Fatalf("ReadMultiple: %v", err)
	}
	if b, _ := res[0].Value.Bool(); b {
		t.Error("boolean still true after list write")
	}
	if n, _ := res[1].Value.Int64(); n != 5 {
		t.Errorf("integer = %v after list write, want 5", res[1].Value)
	}
}

func TestClient_NVL_VMDScope(t *testing.T) {
	ctx, c := session(t)
	list := mms.ObjectName{Scope: mms.ObjectScopeVMD, ItemID: "vmdlist"}

	if err := c.DefineNamedVariableList(ctx, mms.DefineNamedVariableListRequest{ListName: list, Variables: nvlMembers()}); err != nil {
		t.Fatalf("DefineNamedVariableList(VMD): %v", err)
	}
	res, err := c.ReadNamedVariableList(ctx, list)
	if err != nil {
		t.Fatalf("ReadNamedVariableList(VMD): %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("got %d results, want 2", len(res))
	}
	names, err := c.GetNameList(ctx, mms.NameListRequest{ObjectClass: mms.ObjectClassNamedVariableList, Scope: mms.ObjectScopeVMD})
	if err != nil {
		t.Fatalf("GetNameList(VMD lists): %v", err)
	}
	if len(names.Names) != 1 || names.Names[0] != "vmdlist" {
		t.Errorf("VMD lists = %v, want [vmdlist]", names.Names)
	}
	del, err := c.DeleteNamedVariableList(ctx, []mms.ObjectName{list})
	if err != nil {
		t.Fatalf("DeleteNamedVariableList(VMD): %v", err)
	}
	if del.NumberDeleted != 1 {
		t.Errorf("NumberDeleted = %d, want 1", del.NumberDeleted)
	}
}

func TestClient_NVL_AssociationScope(t *testing.T) {
	ctx, c := session(t)
	list := mms.ObjectName{Scope: mms.ObjectScopeAssociation, ItemID: "aalist"}

	if err := c.DefineNamedVariableList(ctx, mms.DefineNamedVariableListRequest{ListName: list, Variables: nvlMembers()}); err != nil {
		t.Fatalf("DefineNamedVariableList(association): %v", err)
	}
	res, err := c.ReadNamedVariableList(ctx, list)
	if err != nil {
		t.Fatalf("ReadNamedVariableList(association): %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("got %d results, want 2", len(res))
	}
	names, err := c.GetNameList(ctx, mms.NameListRequest{ObjectClass: mms.ObjectClassNamedVariableList, Scope: mms.ObjectScopeAssociation})
	if err != nil {
		t.Fatalf("GetNameList(association lists): %v", err)
	}
	if len(names.Names) != 1 || names.Names[0] != "aalist" {
		t.Errorf("association lists = %v, want [aalist]", names.Names)
	}
	del, err := c.DeleteNamedVariableList(ctx, []mms.ObjectName{list})
	if err != nil {
		t.Fatalf("DeleteNamedVariableList(association): %v", err)
	}
	if del.NumberDeleted != 1 {
		t.Errorf("NumberDeleted = %d, want 1", del.NumberDeleted)
	}
}

// libiec61850 implements DeleteNamedVariableList only for scopeOfDelete
// "specific" (an explicit list of names). The bulk forms are answered with
// an error and must leave the association usable.
func TestClient_DeleteAllNVLs_UnsupportedByPeer(t *testing.T) {
	ctx, c := session(t)

	if err := c.DefineNamedVariableList(ctx, mms.DefineNamedVariableListRequest{ListName: dv("bulk"), Variables: nvlMembers()}); err != nil {
		t.Fatalf("DefineNamedVariableList: %v", err)
	}

	for name, call := range map[string]func() (*mms.DeleteNamedVariableListResult, error){
		"DeleteAllDomainNVLs": func() (*mms.DeleteNamedVariableListResult, error) { return c.DeleteAllDomainNVLs(ctx, "interop") },
		"DeleteAllVMDNVLs":    func() (*mms.DeleteNamedVariableListResult, error) { return c.DeleteAllVMDNVLs(ctx) },
	} {
		res, err := call()
		var se *mms.ServiceError
		if !errors.As(err, &se) {
			t.Errorf("%s = %+v, %v; want a ServiceError (peer fixed? update knownLimitations)", name, res, err)
		}
	}

	if _, err := c.ReadNamedVariableList(ctx, dv("bulk")); err != nil {
		t.Fatalf("association unusable after bulk delete: %v", err)
	}
}

// ---------------------------------------------------------------------------
// InformationReport
// ---------------------------------------------------------------------------

// The libiec61850 server adapter sends only the named-list reports of the
// fixture: libiec61850 mis-sizes listOfVariable reports with
// domain-specific names (capabilities.json knownLimitations). The
// listOfVariable form is then covered in the go-mms server → peer client
// direction.
func TestClient_InformationReport(t *testing.T) {
	ctx, c := session(t)
	fix := mustLoadFixture(t)
	if stack(t).quirks.namedListReportsOnly {
		var named []fixtureReport
		for _, r := range fix.InformationReports {
			if r.VMDList != "" {
				named = append(named, r)
			}
		}
		fix.InformationReports = named
	}

	var mu sync.Mutex
	var reports []*mms.InformationReportIndication
	got := make(chan struct{}, 8)
	c.OnInformationReport(func(r *mms.InformationReportIndication) {
		mu.Lock()
		reports = append(reports, r)
		mu.Unlock()
		got <- struct{}{}
	})

	if _, err := c.Write(ctx, mms.WriteRequest{DomainID: "interop", ItemID: "report-trigger", Value: mms.NewInteger(1)}); err != nil {
		t.Fatalf("Write(report-trigger): %v", err)
	}

	for i := 0; i < len(fix.InformationReports); i++ {
		select {
		case <-got:
		case <-time.After(5 * time.Second):
			t.Fatalf("received %d of %d InformationReports", i, len(fix.InformationReports))
		}
	}

	mu.Lock()
	defer mu.Unlock()
	for i, want := range fix.InformationReports {
		r := reports[i]
		if len(r.Values) != len(want.Variables) {
			t.Errorf("report %d: %d values, want %d", i, len(r.Values), len(want.Variables))
			continue
		}
		if n, _ := r.Values[0].Int64(); n != -123 {
			t.Errorf("report %d: integer = %v, want -123", i, r.Values[0])
		}
		if b, _ := r.Values[1].Bool(); !b {
			t.Errorf("report %d: boolean = %v, want true", i, r.Values[1])
		}
		if want.VMDList != "" {
			if r.ListName == nil || r.ListName.Scope != mms.ObjectScopeVMD || string(r.ListName.ItemID) != want.VMDList {
				t.Errorf("report %d: list name = %+v, want VMD %q", i, r.ListName, want.VMDList)
			}
			continue
		}
		if r.ListName != nil {
			t.Errorf("report %d: unexpected list name %+v", i, r.ListName)
		}
		if len(r.Variables) != len(want.Variables) {
			t.Errorf("report %d: variables = %+v, want %v", i, r.Variables, want.Variables)
			continue
		}
		for k, ref := range want.Variables {
			if got := string(r.Variables[k].Domain) + "/" + string(r.Variables[k].ItemID); got != ref {
				t.Errorf("report %d: variable %d = %s, want %s", i, k, got, ref)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// File services
// ---------------------------------------------------------------------------

func directoryByName(entries []mms.FileDirectoryEntry) map[string]mms.FileDirectoryEntry {
	m := make(map[string]mms.FileDirectoryEntry, len(entries))
	for _, e := range entries {
		m[e.Name] = e
	}
	return m
}

// Regression for go-mms issue #2: libiec61850 encodes listOfDirectoryEntry
// with an explicit [0] tag around a SEQUENCE OF.
//
// libiec61850 flattens its store: the root listing holds every file by its
// full '/' path and no entries for directories. It always sends
// lastModified, the file system time when the fixture gives none.
func TestClient_FileDirectory(t *testing.T) {
	ctx, c := session(t)
	fix := mustLoadFixture(t)

	res, err := c.FileDirectory(ctx, mms.FileDirectoryRequest{})
	if err != nil {
		t.Fatalf("FileDirectory: %v", err)
	}
	if res.MoreFollows {
		t.Error("MoreFollows = true for a small file store")
	}
	got := directoryByName(res.Entries)
	if len(got) != len(fix.Files) {
		t.Errorf("FileDirectory returned %d entries, want %d: %+v", len(got), len(fix.Files), res.Entries)
	}
	if _, ok := got["COMTRADE/REC02.CFG"]; !ok {
		t.Error(`root listing lacks the flattened path "COMTRADE/REC02.CFG"`)
	}
	for _, e := range res.Entries {
		if e.Name == "COMTRADE" || strings.HasSuffix(e.Name, "/") || strings.HasSuffix(e.Name, `\`) {
			t.Errorf("root listing has a directory entry %q", e.Name)
		}
	}
	for name := range fix.Files {
		data, stored := fix.file(t, name)
		e, ok := got[name]
		if !ok {
			t.Errorf("file %q missing from directory", name)
			continue
		}
		if e.Size != int64(len(data)) {
			t.Errorf("%s: size = %d, want %d", name, e.Size, len(data))
		}
		switch {
		case stored.LastModified == "":
			if e.LastModified.IsZero() {
				t.Errorf("%s: no lastModified; libiec61850 reports the file system time", name)
			}
		case !e.LastModified.Equal(stored.modTime()):
			t.Errorf("%s: lastModified = %v, want %v", name, e.LastModified, stored.modTime())
		}
	}
}

func TestClient_FileDirectory_Subdirectory(t *testing.T) {
	ctx, c := session(t)

	res, err := c.FileDirectory(ctx, mms.FileDirectoryRequest{FileSpec: "COMTRADE"})
	if err != nil {
		t.Fatalf("FileDirectory(COMTRADE): %v", err)
	}
	if len(res.Entries) != 1 || res.Entries[0].Name != "COMTRADE/REC02.CFG" {
		t.Errorf("COMTRADE listing = %+v, want [COMTRADE/REC02.CFG]", res.Entries)
	}
}

// A small negotiated PDU forces the server to split the listing; the
// client must page through it with continueAfter.
func TestClient_FileDirectoryAll_Paginated(t *testing.T) {
	ctx, c := session(t, iso.WithClientDialOptions(mms.DialOptions{MMS: mms.MMSOptions{MaxPDUSize: 200}}))
	fix := mustLoadFixture(t)

	first, err := c.FileDirectory(ctx, mms.FileDirectoryRequest{})
	if err != nil {
		t.Fatalf("FileDirectory: %v", err)
	}
	if !first.MoreFollows {
		t.Errorf("MoreFollows = false with a %d-byte PDU and %d files; pagination not exercised", c.Negotiated().MaxPDUSize, len(fix.Files))
	}

	all, err := c.FileDirectoryAll(ctx, "")
	if err != nil {
		t.Fatalf("FileDirectoryAll: %v", err)
	}
	var names []string
	for _, e := range all {
		names = append(names, e.Name)
	}
	sort.Strings(names)
	if len(names) != len(fix.Files) {
		t.Errorf("FileDirectoryAll = %v, want %d files", names, len(fix.Files))
	}
	for name := range fix.Files {
		if _, ok := directoryByName(all)[name]; !ok {
			t.Errorf("file %q missing from FileDirectoryAll", name)
		}
	}
}

func TestClient_FileOpenReadClose(t *testing.T) {
	ctx, c := session(t)
	fix := mustLoadFixture(t)
	want, stored := fix.file(t, "REC01.CFG")

	f, err := c.FileOpen(ctx, "REC01.CFG")
	if err != nil {
		t.Fatalf("FileOpen: %v", err)
	}
	if f.Size != int64(len(want)) {
		t.Errorf("FileOpen size = %d, want %d", f.Size, len(want))
	}
	if !f.LastModified.Equal(stored.modTime()) {
		t.Errorf("FileOpen lastModified = %v, want %v", f.LastModified, stored.modTime())
	}

	var got []byte
	for {
		r, err := c.FileRead(ctx, f.FrsmID)
		if err != nil {
			t.Fatalf("FileRead: %v", err)
		}
		got = append(got, r.Data...)
		if !r.MoreFollows {
			break
		}
	}
	if !bytes.Equal(got, want) {
		t.Errorf("content = %q, want %q", got, want)
	}
	if err := c.FileClose(ctx, f.FrsmID); err != nil {
		t.Fatalf("FileClose: %v", err)
	}
	if _, err := c.FileRead(ctx, f.FrsmID); err == nil {
		t.Error("FileRead on a closed frsmID succeeded")
	}
}

// libiec61850 decodes initialPosition and shortens the transfer
// accordingly, but never seeks: it returns size-position bytes from offset
// 0 (capabilities.json knownLimitations). The byte count proves the peer
// decoded go-mms's initialPosition; content semantics are covered by the
// Go↔Go e2e suite and by peers that seek.
func TestClient_FileOpen_InitialPosition(t *testing.T) {
	ctx, c := session(t)
	want, _ := mustLoadFixture(t).file(t, "REC01.DAT")

	f, err := c.FileOpen(ctx, "REC01.DAT", mms.FileOpenOptions{InitialPosition: 4})
	if err != nil {
		t.Fatalf("FileOpen: %v", err)
	}
	got, err := c.FileReadAll(ctx, f.FrsmID)
	if err != nil {
		t.Fatalf("FileReadAll: %v", err)
	}
	if len(got) != len(want)-4 {
		t.Errorf("read %d bytes from offset 4, want %d", len(got), len(want)-4)
	}
	switch {
	case !stack(t).quirks.unseekedFileOpen:
		if !bytes.Equal(got, want[4:]) {
			t.Errorf("content from offset 4 = %x, want %x", got, want[4:])
		}
	case !bytes.Equal(got, want[:len(want)-4]):
		t.Errorf("content from offset 4 = %x; expected the unseeked %x — peer fixed? update knownLimitations", got, want[:len(want)-4])
	}
	if err := c.FileClose(ctx, f.FrsmID); err != nil {
		t.Fatalf("FileClose: %v", err)
	}
}

func TestClient_FileReadAll_MultiChunk(t *testing.T) {
	ctx, c := session(t)
	want, _ := mustLoadFixture(t).file(t, "LARGE.BIN")

	f, err := c.FileOpen(ctx, "LARGE.BIN")
	if err != nil {
		t.Fatalf("FileOpen: %v", err)
	}
	got, err := c.FileReadAll(ctx, f.FrsmID)
	if err != nil {
		t.Fatalf("FileReadAll: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("LARGE.BIN: got %d bytes, want %d (content mismatch)", len(got), len(want))
	}
	if err := c.FileClose(ctx, f.FrsmID); err != nil {
		t.Fatalf("FileClose: %v", err)
	}
}

func TestClient_DownloadFile(t *testing.T) {
	ctx, c := session(t, iso.WithClientDialOptions(mms.DialOptions{MMS: mms.MMSOptions{MaxPDUSize: 1024}}))
	fix := mustLoadFixture(t)
	want, stored := fix.file(t, "LARGE.BIN")

	got, info, err := c.DownloadFile(ctx, "LARGE.BIN")
	if err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("LARGE.BIN: got %d bytes, want %d (content mismatch)", len(got), len(want))
	}
	if info.Size != int64(len(want)) || !info.LastModified.Equal(stored.modTime()) {
		t.Errorf("DownloadFile info = %+v, want size %d, lastModified %v", info, len(want), stored.modTime())
	}
}

func TestClient_FileOpen_NonExistent(t *testing.T) {
	ctx, c := session(t)

	_, err := c.FileOpen(ctx, "NO-SUCH.FILE")
	var se *mms.ServiceError
	if !errors.As(err, &se) {
		t.Fatalf("FileOpen(missing) error = %v, want *mms.ServiceError", err)
	}
	t.Logf("FileOpen(missing): %v", err)
	if _, err := c.ReadObject(ctx, dv("boolean")); err != nil {
		t.Fatalf("association unusable after file error: %v", err)
	}
}

func TestClient_FileDelete(t *testing.T) {
	ctx, c := session(t)

	if err := c.FileDelete(ctx, "DELETE.ME"); err != nil {
		t.Fatalf("FileDelete: %v", err)
	}
	res, err := c.FileDirectory(ctx, mms.FileDirectoryRequest{})
	if err != nil {
		t.Fatalf("FileDirectory: %v", err)
	}
	if _, ok := directoryByName(res.Entries)["DELETE.ME"]; ok {
		t.Error("DELETE.ME still listed after FileDelete")
	}
	if err := c.FileDelete(ctx, "DELETE.ME"); err == nil {
		t.Error("second FileDelete of the same file succeeded")
	}
}

func TestClient_FileRename(t *testing.T) {
	ctx, c := session(t)
	want, _ := mustLoadFixture(t).file(t, "RENAME.ME")

	if err := c.FileRename(ctx, "RENAME.ME", "RENAMED.ME"); err != nil {
		t.Fatalf("FileRename: %v", err)
	}
	got, _, err := c.DownloadFile(ctx, "RENAMED.ME")
	if err != nil {
		t.Fatalf("DownloadFile(RENAMED.ME): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("renamed content = %q, want %q", got, want)
	}
	if _, err := c.FileOpen(ctx, "RENAME.ME"); err == nil {
		t.Error("old name still opens after FileRename")
	}
}

// ObtainFile asks the server to fetch a file from the client. libiec61850
// does this by sending FileOpen/FileRead/FileClose requests back to the
// client, which go-mms does not serve (KNOWN_LIMITATIONS.md). The request
// must fail with a service error and leave the association usable; this
// proves the request encoding is accepted by the peer.
func TestClient_ObtainFile(t *testing.T) {
	ctx, c := session(t)

	octx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err := c.ObtainFile(octx, "local.cfg", "OBTAINED.CFG")
	var se *mms.ServiceError
	if !errors.As(err, &se) || se.Class != mms.ErrorClassFile {
		t.Fatalf("ObtainFile error = %v, want a File-class ServiceError", err)
	}
	if _, err := c.ReadObject(ctx, dv("boolean")); err != nil {
		t.Fatalf("association unusable after ObtainFile: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Journals
// ---------------------------------------------------------------------------

func assertJournalEntries(t *testing.T, got []mms.JournalEntry, want []fixtureJournalEntry) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d journal entries, want %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if !bytes.Equal(g.EntryID, entryIDBytes(w.EntryID)) {
			t.Errorf("entry %d: EntryID = %x, want %x", i, g.EntryID, entryIDBytes(w.EntryID))
		}
		if !g.OccurrenceTime.Equal(w.time(t)) {
			t.Errorf("entry %d: OccurrenceTime = %v, want %v", i, g.OccurrenceTime, w.time(t))
		}
		if len(g.Variables) != 2 {
			t.Errorf("entry %d: %d variables, want data + ReasonCode", i, len(g.Variables))
			continue
		}
		if g.Variables[0].Tag != w.Tag {
			t.Errorf("entry %d: tag = %q, want %q", i, g.Variables[0].Tag, w.Tag)
		}
		want, err := makeScalarValue(w.Type, w.Value)
		if err != nil {
			t.Fatalf("entry %d: %v", i, err)
		}
		if !g.Variables[0].Value.Equal(want) {
			t.Errorf("entry %d: value = %v, want %v", i, g.Variables[0].Value, want)
		}
		if g.Variables[1].Tag != "ReasonCode" {
			t.Errorf("entry %d: second variable %q, want ReasonCode", i, g.Variables[1].Tag)
		}
		if bits, n, _ := reasonCodeBits(g.Variables[1].Value); n != 7 || bits != w.ReasonCode {
			t.Errorf("entry %d: ReasonCode = %d (%d bits), want %d (7 bits)", i, bits, n, w.ReasonCode)
		}
	}
}

// reasonCodeBits decodes libiec61850's ReasonCode bit-string, where bit i
// (MSB-first) carries 1<<i of the reason code.
func reasonCodeBits(v *mms.Value) (int, int, bool) {
	bits, ok := v.BitString()
	n, _ := v.BitStringLength()
	if !ok {
		return 0, 0, false
	}
	code := 0
	for i := 0; i < n; i++ {
		if bits[i/8]&(0x80>>(i%8)) != 0 {
			code |= 1 << i
		}
	}
	return code, n, true
}

func TestClient_ReadJournalTimeRange(t *testing.T) {
	ctx, c := session(t)
	entries := mustLoadFixture(t).Domains["interop"].Journals["log1"].Entries

	res, err := c.ReadJournalTimeRange(ctx, "interop", "log1", entries[0].time(t), entries[len(entries)-1].time(t))
	if err != nil {
		t.Fatalf("ReadJournalTimeRange: %v", err)
	}
	if res.MoreFollows {
		t.Error("MoreFollows = true for a 4-entry journal")
	}
	assertJournalEntries(t, res.Entries, entries)

	res, err = c.ReadJournalTimeRange(ctx, "interop", "log1", entries[1].time(t), entries[2].time(t))
	if err != nil {
		t.Fatalf("ReadJournalTimeRange(sub-range): %v", err)
	}
	assertJournalEntries(t, res.Entries, entries[1:3])
}

func TestClient_ReadJournalStartAfter(t *testing.T) {
	ctx, c := session(t)
	entries := mustLoadFixture(t).Domains["interop"].Journals["log1"].Entries

	res, err := c.ReadJournalStartAfter(ctx, "interop", "log1", entries[1].time(t), entryIDBytes(entries[1].EntryID))
	if err != nil {
		t.Fatalf("ReadJournalStartAfter: %v", err)
	}
	assertJournalEntries(t, res.Entries, entries[2:])
}

func TestClient_ReadJournal_Unknown(t *testing.T) {
	ctx, c := session(t)

	_, err := c.ReadJournalTimeRange(ctx, "interop", "nolog", time.Date(1984, 1, 1, 0, 0, 0, 0, time.UTC), time.Now())
	var se *mms.ServiceError
	if !errors.As(err, &se) {
		t.Fatalf("ReadJournal(unknown) error = %v, want *mms.ServiceError", err)
	}
}
