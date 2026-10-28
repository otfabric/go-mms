// SPDX-License-Identifier: MIT

package e2e

import (
	"bytes"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"
)

// ---------------------------------------------------------------------------
// Association
// ---------------------------------------------------------------------------

func TestClient_Identify(t *testing.T) {
	m := startModel(t)
	id, err := dial(t, m.addr).Identify(timeout(t))
	if err != nil {
		t.Fatal(err)
	}
	if *id != (mms.ServerIdentity{Vendor: "otfabric", Model: "go-mms-e2e", Revision: "1"}) {
		t.Errorf("identity = %+v", id)
	}
}

func TestClient_Status(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)
	st, err := c.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Logical != mms.VMDLogicalStatusNoStateChanges || st.Physical != mms.VMDPhysicalStatusPartiallyOperational {
		t.Errorf("Status = %+v", st)
	}
	st, err = c.StatusWithOptions(ctx, mms.ClientStatusRequest{ExtendedDerivation: true})
	if err != nil {
		t.Fatal(err)
	}
	if st.Logical != mms.VMDLogicalStatusLimited || st.Physical != mms.VMDPhysicalStatusNeedsCommissioning {
		t.Errorf("StatusWithOptions(extended) = %+v", st)
	}
}

func TestClient_Negotiated(t *testing.T) {
	m := startModel(t, modelOptions{maxPDU: 8000})
	c := dial(t, m.addr, mms.DialOptions{MMS: mms.MMSOptions{MaxPDUSize: 4096, DataStructureNestingLevel: 4}})
	n := c.Negotiated()
	if n.MaxPDUSize != 4096 || n.NestingLevel != 4 || n.MaxOutCalling < 1 || n.MaxOutCalled < 1 || n.ServerVersion < 1 {
		t.Errorf("client-limited Negotiated = %+v", n)
	}
	if got := dial(t, m.addr).Negotiated().MaxPDUSize; got != 8000 {
		t.Errorf("server-limited MaxPDUSize = %d, want 8000", got)
	}
}

func TestClient_Close(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)
	eventually(t, "server connection", func() bool { return len(m.srv.Connections()) == 1 })
	if err := c.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := c.Identify(ctx); !errors.Is(err, mms.ErrClosed) {
		t.Errorf("Identify after Close: %v, want ErrClosed", err)
	}
	eventually(t, "server to drop the connection", func() bool { return len(m.srv.Connections()) == 0 })
}

func TestClient_Abort(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)
	eventually(t, "server connection", func() bool { return len(m.srv.Connections()) == 1 })
	if err := c.Abort(ctx); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if _, err := c.Identify(ctx); !errors.Is(err, mms.ErrClosed) {
		t.Errorf("Identify after Abort: %v, want ErrClosed", err)
	}
	eventually(t, "server to drop the connection", func() bool { return len(m.srv.Connections()) == 0 })
}

// ---------------------------------------------------------------------------
// Directory services
// ---------------------------------------------------------------------------

func TestClient_GetNameList(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)

	var wantVars []string
	for _, vc := range valueCases() {
		wantVars = append(wantVars, vc.name)
	}
	wantVars = append(wantVars, "readonly", "unavailable")
	sort.Strings(wantVars)

	cases := []struct {
		name string
		req  mms.NameListRequest
		want []string
	}{
		{"domains", mms.NameListRequest{ObjectClass: mms.ObjectClassDomain}, []string{auxDom, dom}},
		{"domain variables", mms.NameListRequest{ObjectClass: mms.ObjectClassNamedVariable, Scope: mms.ObjectScopeDomain, DomainID: dom}, wantVars},
		{"vmd variables", mms.NameListRequest{ObjectClass: mms.ObjectClassNamedVariable}, []string{"vmdvar"}},
		{"domain lists", mms.NameListRequest{ObjectClass: mms.ObjectClassNamedVariableList, Scope: mms.ObjectScopeDomain, DomainID: dom}, []string{"static"}},
		{"journals", mms.NameListRequest{ObjectClass: mms.ObjectClassJournal, Scope: mms.ObjectScopeDomain, DomainID: dom}, []string{journal}},
	}
	for _, tc := range cases {
		res, err := c.GetNameList(ctx, tc.req)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		got := append([]string(nil), res.Names...)
		sort.Strings(got)
		if res.MoreFollows || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s = %v (more=%v), want %v", tc.name, res.Names, res.MoreFollows, tc.want)
		}
	}

	for _, class := range []mms.ObjectClass{mms.ObjectClassNamedVariable, mms.ObjectClassNamedVariableList} {
		_, err := c.GetNameList(ctx, mms.NameListRequest{ObjectClass: class, Scope: mms.ObjectScopeDomain, DomainID: "nope"})
		if se := serviceError(t, err); se.Class != mms.ErrorClassAccess || se.Code != 2 {
			t.Errorf("%v in unknown domain = %v, want access/object-non-existent", class, se)
		}
	}
}

// A small PDU forces the server to page; ContinueAfter resumes the list.
func TestClient_GetNameList_Paged(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr, mms.DialOptions{MMS: mms.MMSOptions{MaxPDUSize: 512}})
	ctx := timeout(t)
	req := mms.NameListRequest{ObjectClass: mms.ObjectClassNamedVariable, Scope: mms.ObjectScopeDomain, DomainID: auxDom}
	var all []string
	for pages := 1; ; pages++ {
		res, err := c.GetNameList(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, res.Names...)
		if !res.MoreFollows {
			if pages < 2 {
				t.Errorf("expected several pages at MaxPDUSize 512, got %d", pages)
			}
			break
		}
		req.ContinueAfter = res.Names[len(res.Names)-1]
	}
	assertAuxNames(t, all)
}

func TestClient_GetNameListAll(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr, mms.DialOptions{MMS: mms.MMSOptions{MaxPDUSize: 512}})
	all, err := c.GetNameListAll(timeout(t), mms.NameListRequest{ObjectClass: mms.ObjectClassNamedVariable, Scope: mms.ObjectScopeDomain, DomainID: auxDom})
	if err != nil {
		t.Fatal(err)
	}
	assertAuxNames(t, all)
}

func assertAuxNames(t *testing.T, names []string) {
	t.Helper()
	if len(names) != auxCount {
		t.Fatalf("got %d names, want %d", len(names), auxCount)
	}
	for i, n := range names {
		if n != auxItem(i) {
			t.Fatalf("name[%d] = %q, want %q", i, n, auxItem(i))
		}
	}
}

func TestClient_GetVariableAccessAttributes(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)
	attrs, err := c.GetVariableAccessAttributes(ctx, domName("NewStructure"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(attrs.TypeSpec, recordType) {
		t.Errorf("TypeSpec = %+v, want %+v", attrs.TypeSpec, recordType)
	}
	if attrs.Deletable {
		t.Error("Deletable = true")
	}
	if _, err := c.GetVariableAccessAttributes(ctx, vmdName("vmdvar")); err != nil {
		t.Errorf("VMD scope: %v", err)
	}
	if _, err := c.GetVariableAccessAttributes(ctx, domName("nope")); err == nil {
		t.Error("unknown variable: expected error")
	}
}

// ---------------------------------------------------------------------------
// Variable access
// ---------------------------------------------------------------------------

func TestClient_Read(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)
	got, err := c.Read(ctx, mms.ReadRequest{DomainID: dom, ItemID: "NewStructure"})
	if err != nil {
		t.Fatal(err)
	}
	mustEqual(t, "NewStructure", got.Value, record(7, true, "rec", 10, 20, 30))

	_, err = c.Read(ctx, mms.ReadRequest{DomainID: dom, ItemID: "nope"})
	if code := dataAccessCode(t, err); code != mms.DataAccessErrorObjectNonExistent {
		t.Errorf("unknown variable code = %v, want ObjectNonExistent", code)
	}
	_, err = c.Read(ctx, mms.ReadRequest{DomainID: "nope", ItemID: "x"})
	if code := dataAccessCode(t, err); code != mms.DataAccessErrorObjectNonExistent {
		t.Errorf("unknown domain code = %v, want ObjectNonExistent", code)
	}
}

func TestClient_Write(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)
	if _, err := c.Write(ctx, mms.WriteRequest{DomainID: dom, ItemID: "NewInteger", Value: mms.NewInteger(77)}); err != nil {
		t.Fatal(err)
	}
	mustEqual(t, "server value", m.get(dom+"/NewInteger"), mms.NewInteger(77))

	_, err := c.Write(ctx, mms.WriteRequest{DomainID: dom, ItemID: "readonly", Value: mms.NewInteger(1)})
	if code := dataAccessCode(t, err); code != mms.DataAccessErrorObjectAccessDenied {
		t.Errorf("read-only code = %v, want ObjectAccessDenied", code)
	}
	_, err = c.Write(ctx, mms.WriteRequest{DomainID: dom, ItemID: "nope", Value: mms.NewInteger(1)})
	if code := dataAccessCode(t, err); code != mms.DataAccessErrorObjectNonExistent {
		t.Errorf("unknown variable code = %v, want ObjectNonExistent", code)
	}
}

func TestClient_ReadMultiple(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	res, err := c.ReadMultiple(timeout(t), []mms.ObjectName{domName("NewBoolean"), domName("nope"), vmdName("vmdvar"), domName("unavailable")})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 4 {
		t.Fatalf("got %d results", len(res))
	}
	mustEqual(t, "NewBoolean", res[0].Value, mms.NewBoolean(true))
	if res[0].ErrorCode != mms.DataAccessErrorNone {
		t.Errorf("NewBoolean code = %v", res[0].ErrorCode)
	}
	if res[1].Value != nil || res[1].ErrorCode != mms.DataAccessErrorObjectNonExistent {
		t.Errorf("unknown = %+v", res[1])
	}
	mustEqual(t, "vmdvar", res[2].Value, mms.NewInteger(99))
	if res[3].ErrorCode != mms.DataAccessErrorTemporarilyUnavailable {
		t.Errorf("unavailable code = %v", res[3].ErrorCode)
	}
}

func TestClient_ReadVariables_WriteVariables(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)
	vars := []mms.VariableSpec{
		{Name: domName("NewStructure"), AlternateAccess: []mms.AccessSelector{mms.SelectComponent("label")}},
		{Name: domName("NewArray"), AlternateAccess: []mms.AccessSelector{mms.SelectIndex(2)}},
		{Name: domName("NewArray"), AlternateAccess: []mms.AccessSelector{mms.SelectRange(1, 3)}},
		{Name: domName("NewStructure"), AlternateAccess: []mms.AccessSelector{mms.SelectComponent("samples"), mms.SelectIndex(1)}},
		{Name: vmdName("vmdvar")},
	}
	res, err := c.ReadVariables(ctx, vars)
	if err != nil {
		t.Fatal(err)
	}
	want := []*mms.Value{mms.NewVisibleString("rec"), mms.NewInteger(3), intArray(2, 3, 4), mms.NewInteger(20), mms.NewInteger(99)}
	for i, w := range want {
		mustEqual(t, "ReadVariables["+string(rune('0'+i))+"]", res[i].Value, w)
	}

	wres, err := c.WriteVariables(ctx, vars, []*mms.Value{
		mms.NewVisibleString("patched"), mms.NewInteger(-3), intArray(-2, -3, -4), mms.NewInteger(-20), mms.NewInteger(100),
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range wres {
		if !r.Success || r.Index != i {
			t.Errorf("WriteVariables[%d] = %+v", i, r)
		}
	}
	mustEqual(t, "NewStructure", m.get(dom+"/NewStructure"), record(7, true, "patched", 10, -20, 30))
	mustEqual(t, "NewArray", m.get(dom+"/NewArray"), intArray(1, -2, -3, -4, 5))
	mustEqual(t, "vmdvar", m.get("/vmdvar"), mms.NewInteger(100))

	wres, err = c.WriteVariables(ctx, specs(domName("readonly")), []*mms.Value{mms.NewInteger(1)})
	if err != nil {
		t.Fatal(err)
	}
	if wres[0].Success || wres[0].ErrorCode != mms.DataAccessErrorObjectAccessDenied {
		t.Errorf("read-only = %+v", wres[0])
	}
}

func TestClient_ReadComponent_WriteComponent(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)
	got, err := c.ReadComponent(ctx, domName("NewStructure"), "count")
	if err != nil {
		t.Fatal(err)
	}
	mustEqual(t, "count", got.Value, mms.NewInteger(7))
	if err := c.WriteComponent(ctx, domName("NewStructure"), "flag", mms.NewBoolean(false)); err != nil {
		t.Fatal(err)
	}
	mustEqual(t, "NewStructure", m.get(dom+"/NewStructure"), record(7, false, "rec", 10, 20, 30))
	if _, err := c.ReadComponent(ctx, domName("NewStructure"), "nope"); err == nil {
		t.Error("unknown component: expected error")
	}
}

func TestClient_ArrayAccess(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)
	arr := domName("NewArray")

	// ReadArrayElement is a deprecated alias of ReadByIndex; the
	// coverage guard requires a direct call in this test.
	got, err := c.ReadArrayElement(ctx, arr, 0) //nolint:staticcheck
	if err != nil {
		t.Fatal(err)
	}
	mustEqual(t, "ReadArrayElement(0)", got.Value, mms.NewInteger(1))
	got, err = c.ReadByIndex(ctx, arr, 4)
	if err != nil {
		t.Fatal(err)
	}
	mustEqual(t, "ReadByIndex(4)", got.Value, mms.NewInteger(5))
	got, err = c.ReadArrayRange(ctx, arr, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	mustEqual(t, "ReadArrayRange(1,2)", got.Value, intArray(2, 3))

	if err := c.WriteArrayElement(ctx, arr, 3, mms.NewInteger(40)); err != nil {
		t.Fatal(err)
	}
	mustEqual(t, "NewArray", m.get(dom+"/NewArray"), intArray(1, 2, 3, 40, 5))
	if _, err := c.ReadByIndex(ctx, arr, 5); err == nil {
		t.Error("index out of range: expected error")
	}
}

func TestClient_ReadObject_WriteObject(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)
	for _, name := range []mms.ObjectName{domName("NewInteger"), vmdName("vmdvar")} {
		if _, err := c.WriteObject(ctx, name, mms.NewInteger(-5)); err != nil {
			t.Fatalf("WriteObject %v: %v", name, err)
		}
		got, err := c.ReadObject(ctx, name)
		if err != nil {
			t.Fatalf("ReadObject %v: %v", name, err)
		}
		mustEqual(t, string(name.ItemID), got.Value, mms.NewInteger(-5))
	}
}

// ---------------------------------------------------------------------------
// Named variable lists
// ---------------------------------------------------------------------------

func TestClient_NamedVariableLists(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	other := dial(t, m.addr)
	ctx := timeout(t)
	members := []mms.VariableSpec{
		{Name: domName("NewBoolean")},
		{Name: domName("NewStructure"), AlternateAccess: []mms.AccessSelector{mms.SelectComponent("count")}},
		{Name: vmdName("vmdvar")},
	}

	for _, list := range []mms.ObjectName{domName("mylist"), vmdName("vmdlist"), aaName("aalist")} {
		t.Run(list.Scope.String(), func(t *testing.T) {
			if err := c.DefineNamedVariableList(ctx, mms.DefineNamedVariableListRequest{ListName: list, Variables: members}); err != nil {
				t.Fatalf("Define: %v", err)
			}
			if err := c.DefineNamedVariableList(ctx, mms.DefineNamedVariableListRequest{ListName: list, Variables: members}); err == nil {
				t.Error("redefine: expected error")
			}
			attrs, err := c.GetNamedVariableListAttributes(ctx, list)
			if err != nil {
				t.Fatalf("Attributes: %v", err)
			}
			if !attrs.Deletable || !reflect.DeepEqual(attrs.Variables, members) {
				t.Errorf("Attributes = %+v", attrs)
			}

			want := []*mms.Value{mms.NewBoolean(true), mms.NewInteger(7), mms.NewInteger(99)}
			for _, opt := range []mms.ReadNamedVariableListOptions{{}, {SpecificationWithResult: true}} {
				res, err := c.ReadNamedVariableList(ctx, list, opt)
				if err != nil {
					t.Fatalf("Read %+v: %v", opt, err)
				}
				for i, w := range want {
					mustEqual(t, "member", res[i].Value, w)
				}
			}

			wres, err := c.WriteNamedVariableList(ctx, list, []*mms.Value{mms.NewBoolean(false), mms.NewInteger(8), mms.NewInteger(98)})
			if err != nil {
				t.Fatalf("Write: %v", err)
			}
			for i, r := range wres {
				if !r.Success {
					t.Errorf("Write[%d] = %+v", i, r)
				}
			}
			mustEqual(t, "NewStructure", m.get(dom+"/NewStructure"), record(8, true, "rec", 10, 20, 30))
			// Restore for the next scope.
			m.set(dom+"/NewBoolean", mms.NewBoolean(true))
			m.set(dom+"/NewStructure", record(7, true, "rec", 10, 20, 30))
			m.set("/vmdvar", mms.NewInteger(99))

			_, otherErr := other.GetNamedVariableListAttributes(ctx, list)
			if visible := otherErr == nil; visible != (list.Scope != mms.ObjectScopeAssociation) {
				t.Errorf("visible to another association = %v (%v)", visible, otherErr)
			}

			del, err := c.DeleteNamedVariableList(ctx, []mms.ObjectName{list})
			if err != nil {
				t.Fatalf("Delete: %v", err)
			}
			if del.NumberMatched != 1 || del.NumberDeleted != 1 {
				t.Errorf("Delete = %+v", del)
			}
			if _, err := c.GetNamedVariableListAttributes(ctx, list); err == nil {
				t.Error("list still exists after delete")
			}
		})
	}

	attrs, err := c.GetNamedVariableListAttributes(ctx, domName("static"))
	if err != nil {
		t.Fatal(err)
	}
	if attrs.Deletable {
		t.Error("server-registered list is deletable")
	}
	del, err := c.DeleteNamedVariableList(ctx, []mms.ObjectName{domName("static")})
	if err != nil {
		t.Fatal(err)
	}
	if del.NumberDeleted != 0 {
		t.Errorf("deleted a non-deletable list: %+v", del)
	}
	if _, err := c.ReadNamedVariableList(ctx, domName("nope")); err == nil {
		t.Error("unknown list: expected error")
	}
}

func TestClient_DeleteAllNVLs(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)
	for _, list := range []mms.ObjectName{domName("l1"), domName("l2"), vmdName("v1"), vmdName("v2"), vmdName("v3")} {
		if err := c.DefineNamedVariableList(ctx, mms.DefineNamedVariableListRequest{ListName: list, Variables: specs(domName("NewBoolean"))}); err != nil {
			t.Fatal(err)
		}
	}
	del, err := c.DeleteAllDomainNVLs(ctx, dom)
	if err != nil {
		t.Fatal(err)
	}
	if del.NumberMatched != 3 || del.NumberDeleted != 2 {
		t.Errorf("DeleteAllDomainNVLs = %+v, want matched 3 (incl. static) deleted 2", del)
	}
	del, err = c.DeleteAllVMDNVLs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if del.NumberMatched != 3 || del.NumberDeleted != 3 {
		t.Errorf("DeleteAllVMDNVLs = %+v", del)
	}
	names, err := c.GetNameListAll(ctx, mms.NameListRequest{ObjectClass: mms.ObjectClassNamedVariableList, Scope: mms.ObjectScopeDomain, DomainID: dom})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"static"}) {
		t.Errorf("remaining domain lists = %v", names)
	}
}

// ---------------------------------------------------------------------------
// File services
// ---------------------------------------------------------------------------

func TestClient_FileDirectory(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)
	res, err := c.FileDirectory(ctx, mms.FileDirectoryRequest{FileSpec: "data"})
	if err != nil {
		t.Fatal(err)
	}
	want := []mms.FileDirectoryEntry{
		{Name: "data/large.bin", Size: 200_000, LastModified: t0.Add(2 * time.Hour)},
		{Name: "data/small.bin", Size: 1000, LastModified: t0.Add(time.Hour)},
	}
	if res.MoreFollows || !reflect.DeepEqual(res.Entries, want) {
		t.Errorf("FileDirectory(data) = %+v", res)
	}
	res, err = c.FileDirectory(ctx, mms.FileDirectoryRequest{ContinueAfter: "readme.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Entries) != 1 || res.Entries[0].Name != "rename.me" {
		t.Errorf("FileDirectory(after readme.txt) = %+v", res.Entries)
	}
	if _, err := c.FileDirectory(ctx, mms.FileDirectoryRequest{FileSpec: "nope"}); err == nil {
		t.Error("unknown directory: expected error")
	}
}

func TestClient_FileDirectoryAll(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr, mms.DialOptions{MMS: mms.MMSOptions{MaxPDUSize: 512}})
	ctx := timeout(t)
	first, err := c.FileDirectory(ctx, mms.FileDirectoryRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !first.MoreFollows {
		t.Errorf("expected paging at MaxPDUSize 512, got %d entries", len(first.Entries))
	}
	all, err := c.FileDirectoryAll(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != len(m.files.files) {
		t.Errorf("FileDirectoryAll = %d entries, want %d", len(all), len(m.files.files))
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].Name >= all[i].Name {
			t.Fatalf("entries out of order or duplicated at %d: %q, %q", i, all[i-1].Name, all[i].Name)
		}
	}
}

// Devices differ in directory conventions and some omit the optional
// lastModified. The client must hand names through unchanged and report
// a missing time as the zero time; the server must omit lastModified
// rather than send year 0001.
func TestClient_FileDirectory_DeviceConventions(t *testing.T) {
	cases := []struct {
		name     string
		listings map[string][]mms.FileEntry
		open     string
	}{
		{
			// One listing of the whole store, full paths with '/'.
			name: "flattened",
			listings: map[string][]mms.FileEntry{"": {
				{Name: "COMTRADE/REC02.CFG", Size: 3, LastModified: t0},
				{Name: "REC01.CFG", Size: 3},
			}},
			open: "REC01.CFG",
		},
		{
			// Top level only; a subdirectory is a zero-size entry ending
			// in '\', listed separately.
			name: "per-level",
			listings: map[string][]mms.FileEntry{
				"":           {{Name: "COMTRADE\\", Size: 0}, {Name: "REC01.CFG", Size: 3, LastModified: t0}},
				"COMTRADE\\": {{Name: "COMTRADE\\REC02.CFG", Size: 3}},
			},
			open: "COMTRADE\\REC02.CFG",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := &memFiles{files: map[string]*memFile{tc.open: {data: []byte("cfg")}}, open: map[*memHandle]bool{}}
			srv := mms.NewServer(mms.ServerOptions{FileProvider: deviceStore{files, tc.listings}})
			rec := &rawRecorder{}
			c := dial(t, serve(t, srv), mms.DialOptions{RawHook: rec.hook})
			ctx := timeout(t)

			for spec, want := range tc.listings {
				res, err := c.FileDirectory(ctx, mms.FileDirectoryRequest{FileSpec: spec})
				if err != nil {
					t.Fatalf("FileDirectory(%q): %v", spec, err)
				}
				var got []mms.FileEntry
				for _, e := range res.Entries {
					got = append(got, mms.FileEntry(e))
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("FileDirectory(%q) = %+v, want %+v", spec, got, want)
				}
			}
			open, err := c.FileOpen(ctx, tc.open)
			if err != nil {
				t.Fatalf("FileOpen(%q): %v", tc.open, err)
			}
			if open.Size != 3 || !open.LastModified.IsZero() {
				t.Errorf("FileOpen(%q) = %+v, want size 3 and no lastModified", tc.open, open)
			}

			rec.mu.Lock()
			defer rec.mu.Unlock()
			for _, frame := range rec.recv {
				if bytes.Contains(frame, []byte("00010101")) {
					t.Errorf("response carries a year-0001 lastModified: % x", frame)
				}
			}
		})
	}
}

func TestClient_FileOpenReadClose(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)
	open, err := c.FileOpen(ctx, "data/large.bin")
	if err != nil {
		t.Fatal(err)
	}
	if open.Size != 200_000 || !open.LastModified.Equal(t0.Add(2*time.Hour)) {
		t.Errorf("FileOpen = %+v", open)
	}
	var data []byte
	chunks := 0
	for {
		rd, err := c.FileRead(ctx, open.FrsmID)
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, rd.Data...)
		chunks++
		if !rd.MoreFollows {
			break
		}
	}
	if !bytes.Equal(data, pattern(200_000)) {
		t.Errorf("content mismatch (%d bytes)", len(data))
	}
	if chunks < 4 {
		t.Errorf("200 kB read in %d chunks; expected PDU-sized chunks", chunks)
	}
	if err := c.FileClose(ctx, open.FrsmID); err != nil {
		t.Fatal(err)
	}
	if n := m.files.openCount(); n != 0 {
		t.Errorf("%d provider handles still open", n)
	}
	if err := c.FileClose(ctx, open.FrsmID); err == nil {
		t.Error("double close: expected error")
	}
}

func TestClient_FileOpen_Errors(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)

	open, err := c.FileOpen(ctx, "data/small.bin", mms.FileOpenOptions{InitialPosition: 990})
	if err != nil {
		t.Fatal(err)
	}
	data, err := c.FileReadAll(ctx, open.FrsmID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, pattern(1000)[990:]) {
		t.Errorf("InitialPosition 990 read % x", data)
	}
	if err := c.FileClose(ctx, open.FrsmID); err != nil {
		t.Fatal(err)
	}

	openEOF, err := c.FileOpen(ctx, "data/small.bin", mms.FileOpenOptions{InitialPosition: 1000})
	if err != nil {
		t.Fatalf("position at EOF: %v", err)
	}
	atEnd, err := c.FileReadAll(ctx, openEOF.FrsmID)
	if err != nil {
		t.Fatal(err)
	}
	if len(atEnd) != 0 {
		t.Errorf("read at EOF = %d bytes, want 0", len(atEnd))
	}
	if err := c.FileClose(ctx, openEOF.FrsmID); err != nil {
		t.Fatal(err)
	}

	_, err = c.FileOpen(ctx, "data/small.bin", mms.FileOpenOptions{InitialPosition: 1001})
	if se := serviceError(t, err); se.Class != mms.ErrorClassFile || se.Code != 5 {
		t.Errorf("position beyond EOF = %v, want file/position-invalid", se)
	}
	_, err = c.FileOpen(ctx, "nope")
	if se := serviceError(t, err); se.Class != mms.ErrorClassFile || se.Code != 7 {
		t.Errorf("missing file = %v, want file/non-existent", se)
	}
	if n := m.files.openCount(); n != 0 {
		t.Errorf("%d provider handles leaked", n)
	}
}

func TestClient_FileReadAll(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr, mms.DialOptions{MMS: mms.MMSOptions{MaxPDUSize: 1024}})
	ctx := timeout(t)
	open, err := c.FileOpen(ctx, "data/large.bin")
	if err != nil {
		t.Fatal(err)
	}
	data, err := c.FileReadAll(ctx, open.FrsmID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, pattern(200_000)) {
		t.Errorf("content mismatch (%d bytes)", len(data))
	}
	if err := c.FileClose(ctx, open.FrsmID); err != nil {
		t.Fatal(err)
	}
}

func TestClient_DownloadFile(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)
	data, open, err := c.DownloadFile(ctx, "readme.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "go-mms end-to-end\n" || open.Size != int64(len(data)) {
		t.Errorf("DownloadFile = %q, %+v", data, open)
	}
	if n := m.files.openCount(); n != 0 {
		t.Errorf("%d provider handles still open", n)
	}
	if _, _, err := c.DownloadFile(ctx, "nope"); err == nil {
		t.Error("missing file: expected error")
	}
}

func TestClient_FileDelete(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)
	if err := c.FileDelete(ctx, "delete.me"); err != nil {
		t.Fatal(err)
	}
	if m.files.has("delete.me") {
		t.Error("file still present")
	}
	err := c.FileDelete(ctx, "readme.txt")
	if se := serviceError(t, err); se.Class != mms.ErrorClassFile || se.Code != 6 {
		t.Errorf("protected file = %v, want file/access-denied", se)
	}
	err = c.FileDelete(ctx, "delete.me")
	if se := serviceError(t, err); se.Class != mms.ErrorClassFile || se.Code != 7 {
		t.Errorf("missing file = %v, want file/non-existent", se)
	}
}

func TestClient_FileRename(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)
	if err := c.FileRename(ctx, "rename.me", "renamed.txt"); err != nil {
		t.Fatal(err)
	}
	if m.files.has("rename.me") || !m.files.has("renamed.txt") {
		t.Error("rename not applied")
	}
	if err := c.FileRename(ctx, "rename.me", "x"); err == nil {
		t.Error("missing source: expected error")
	}
}

func TestClient_ObtainFile(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)
	if err := c.ObtainFile(ctx, "readme.txt", "copy.txt"); err != nil {
		t.Fatal(err)
	}
	data, _, err := c.DownloadFile(ctx, "copy.txt")
	if err != nil || string(data) != "go-mms end-to-end\n" {
		t.Errorf("obtained copy = %q, %v", data, err)
	}
	if err := c.ObtainFile(ctx, "nope", "x"); err == nil {
		t.Error("missing source: expected error")
	}
}

// ---------------------------------------------------------------------------
// Journals
// ---------------------------------------------------------------------------

func assertJournalEntry(t *testing.T, e mms.JournalEntry, i int) {
	t.Helper()
	if !bytes.Equal(e.EntryID, []byte{byte(i >> 8), byte(i)}) || !e.OccurrenceTime.Equal(t0.Add(time.Duration(i)*tick)) {
		t.Fatalf("entry %d: id % x at %v", i, e.EntryID, e.OccurrenceTime)
	}
	if len(e.Variables) != 2 || e.Variables[0].Tag != "seq" || !e.Variables[0].Value.Equal(mms.NewInteger(int64(i))) {
		t.Fatalf("entry %d: variables %+v", i, e.Variables)
	}
}

func TestClient_ReadJournalTimeRange(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)
	res, err := c.ReadJournalTimeRange(ctx, dom, journal, t0.Add(10*tick), t0.Add(19*tick))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Entries) != 10 || res.MoreFollows {
		t.Fatalf("got %d entries (more=%v), want 10", len(res.Entries), res.MoreFollows)
	}
	for i, e := range res.Entries {
		assertJournalEntry(t, e, 10+i)
	}
	if _, err := c.ReadJournalTimeRange(ctx, dom, "nope", t0, t0.Add(time.Hour)); err == nil {
		t.Error("unknown journal: expected error")
	}
	if _, err := c.ReadJournalTimeRange(ctx, dom, journal, time.Time{}, t0); err == nil {
		t.Error("start before the TimeOfDay epoch: expected error")
	}
}

// The server truncates to the negotiated PDU size; ReadJournalStartAfter
// pages through the rest.
func TestClient_ReadJournalStartAfter(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr, mms.DialOptions{MMS: mms.MMSOptions{MaxPDUSize: 2048}})
	ctx := timeout(t)
	res, err := c.ReadJournalTimeRange(ctx, dom, journal, t0, t0.Add(journalSize*tick))
	if err != nil {
		t.Fatal(err)
	}
	var all []mms.JournalEntry
	pages := 1
	for {
		all = append(all, res.Entries...)
		if !res.MoreFollows {
			break
		}
		last := res.Entries[len(res.Entries)-1]
		if res, err = c.ReadJournalStartAfter(ctx, dom, journal, last.OccurrenceTime, last.EntryID); err != nil {
			t.Fatal(err)
		}
		pages++
	}
	if pages < 3 {
		t.Errorf("read %d entries in %d pages at MaxPDUSize 2048", len(all), pages)
	}
	if len(all) != journalSize {
		t.Fatalf("got %d entries, want %d", len(all), journalSize)
	}
	for i, e := range all {
		assertJournalEntry(t, e, i)
	}
}

// ---------------------------------------------------------------------------
// Unconfirmed services
// ---------------------------------------------------------------------------

func TestClient_OnInformationReport(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)
	reports := make(chan *mms.InformationReportIndication, 4)
	c.OnInformationReport(func(r *mms.InformationReportIndication) { reports <- r })
	eventually(t, "server connection", func() bool { return len(m.srv.Connections()) == 1 })
	sc := m.srv.Connections()[0]

	vars := []mms.ObjectName{domName("NewInteger"), vmdName("vmdvar")}
	if err := sc.SendInformationReport(ctx, &mms.InformationReportRequest{Variables: vars, Values: []*mms.Value{mms.NewInteger(1), mms.NewInteger(2)}}); err != nil {
		t.Fatal(err)
	}
	list := domName("static")
	if err := m.srv.Broadcast(ctx, &mms.InformationReportRequest{ListName: &list, Values: []*mms.Value{mms.NewBoolean(true), mms.NewInteger(3)}}); err != nil {
		t.Fatal(err)
	}

	r := <-reports
	if r.ListName != nil || !reflect.DeepEqual(r.Variables, vars) || len(r.Values) != 2 || !r.Values[1].Equal(mms.NewInteger(2)) {
		t.Errorf("listOfVariables report = %+v", r)
	}
	r = <-reports
	if r.ListName == nil || *r.ListName != list || len(r.Variables) != 0 || len(r.Values) != 2 || !r.Values[1].Equal(mms.NewInteger(3)) {
		t.Errorf("named-list report = %+v", r)
	}
}
