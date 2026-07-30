// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/pdu"
	"github.com/otfabric/go-mms/internal/servermodel"
)

func TestHandleIdentify_Edges(t *testing.T) {
	s := NewServer(ServerOptions{})
	ctx := context.Background()
	if _, _, _, err := s.handleIdentify(ctx); err == nil {
		t.Fatal("nil handler")
	}
	s.HandleIdentify(func(context.Context, IdentifyRequest) (*ServerIdentity, error) {
		return nil, errors.New("boom")
	})
	if _, _, _, err := s.handleIdentify(ctx); err == nil {
		t.Fatal("handler error")
	}
	s.HandleIdentify(func(context.Context, IdentifyRequest) (*ServerIdentity, error) {
		return &ServerIdentity{Vendor: "V", Model: "M", Revision: "1"}, nil
	})
	_, ok, payload, err := s.handleIdentify(ctx)
	if err != nil || !ok || len(payload) == 0 {
		t.Fatalf("success: ok=%v len=%d err=%v", ok, len(payload), err)
	}
}

func TestHandleStatus_Edges(t *testing.T) {
	s := NewServer(ServerOptions{})
	ctx := context.Background()
	if _, _, _, err := s.handleStatus(ctx, []byte{0x00}); err == nil {
		t.Fatal("nil handler")
	}
	s.HandleStatus(func(_ context.Context, req StatusRequest) (*ServerStatus, error) {
		if req.ExtendedDerivation {
			return nil, errors.New("ext fail")
		}
		return &ServerStatus{Logical: VMDLogicalStatusStateChangesAllowed, Physical: VMDPhysicalStatusOperational}, nil
	})
	if _, _, _, err := s.handleStatus(ctx, nil); err == nil {
		t.Fatal("bad body length")
	}
	if _, _, _, err := s.handleStatus(ctx, []byte{0x01}); err == nil {
		t.Fatal("handler error on extended")
	}
	if _, _, _, err := s.handleStatus(ctx, []byte{0x00}); err != nil {
		t.Fatalf("success: %v", err)
	}
}

func TestHandleGetNameList_Edges(t *testing.T) {
	s := NewServer(ServerOptions{})
	ctx := context.Background()
	if err := s.RegisterDomain("d"); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterVariable(Variable{
		Name:     ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "v1"},
		TypeSpec: TypeSpec{Type: ValueTypeInteger, Size: 32},
		Read:     func(context.Context) (*Value, error) { return NewInteger(1), nil },
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterVariable(Variable{
		Name:     ObjectName{Scope: ObjectScopeVMD, ItemID: "vv"},
		TypeSpec: TypeSpec{Type: ValueTypeBoolean},
		Read:     func(context.Context) (*Value, error) { return NewBoolean(true), nil },
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterNamedVariableList(NamedVariableList{
		Name: ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "nvl"},
		Variables: []VariableSpec{
			{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "v1"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterNamedVariableList(NamedVariableList{
		Name: ObjectName{Scope: ObjectScopeVMD, ItemID: "vnvl"},
		Variables: []VariableSpec{
			{Name: ObjectName{Scope: ObjectScopeVMD, ItemID: "vv"}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := s.handleGetNameList(ctx, []byte{0xff}); err == nil {
		t.Fatal("bad body")
	}

	cases := []struct {
		name  string
		cls   int
		scope int
		dom   string
	}{
		{"domains", int(ObjectClassDomain), pdu.ScopeVMD, ""},
		{"domain vars", int(ObjectClassNamedVariable), pdu.ScopeDomain, "d"},
		{"vmd vars", int(ObjectClassNamedVariable), pdu.ScopeVMD, ""},
		{"domain nvls", int(ObjectClassNamedVariableList), pdu.ScopeDomain, "d"},
		{"vmd nvls", int(ObjectClassNamedVariableList), pdu.ScopeVMD, ""},
	}
	for _, tc := range cases {
		pduBytes, err := pdu.MarshalGetNameListRequest(1, tc.cls, tc.scope, tc.dom, "")
		if err != nil {
			t.Fatalf("%s marshal: %v", tc.name, err)
		}
		if _, _, _, err := s.handleGetNameList(ctx, extractConfirmedBody(t, pduBytes)); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}

	// Unsupported combo.
	bad, _ := pdu.MarshalGetNameListRequest(1, int(ObjectClassDomain), pdu.ScopeDomain, "d", "")
	if _, _, _, err := s.handleGetNameList(ctx, extractConfirmedBody(t, bad)); err == nil {
		t.Fatal("unsupported combo")
	}

	// Association scope without / with ServerConn.
	aaNVL, _ := pdu.MarshalGetNameListRequest(1, int(ObjectClassNamedVariableList), pdu.ScopeAssociation, "", "")
	if _, _, _, err := s.handleGetNameList(ctx, extractConfirmedBody(t, aaNVL)); err == nil {
		t.Fatal("aa nvl without conn")
	}
	aaVar, _ := pdu.MarshalGetNameListRequest(1, int(ObjectClassNamedVariable), pdu.ScopeAssociation, "", "")
	if _, _, _, err := s.handleGetNameList(ctx, extractConfirmedBody(t, aaVar)); err == nil {
		t.Fatal("aa var without conn")
	}
	sc := &ServerConn{}
	ctxSC := context.WithValue(ctx, serverConnCtxKey{}, sc)
	if _, _, _, err := s.handleGetNameList(ctxSC, extractConfirmedBody(t, aaNVL)); err != nil {
		t.Fatalf("aa nvl: %v", err)
	}
	if _, _, _, err := s.handleGetNameList(ctxSC, extractConfirmedBody(t, aaVar)); err != nil {
		t.Fatalf("aa var: %v", err)
	}
}

func TestHandleGetNameListJournal_Edges(t *testing.T) {
	s := NewServer(ServerOptions{})
	ctx := context.Background()
	reqPDU, err := pdu.MarshalGetNameListRequest(1, int(ObjectClassJournal), pdu.ScopeDomain, "d", "")
	if err != nil {
		t.Fatal(err)
	}
	body := extractConfirmedBody(t, reqPDU)
	if _, _, _, err := s.handleGetNameList(ctx, body); err == nil {
		t.Fatal("journal without provider")
	}

	jp := newMemJournalProvider()
	jp.addJournal("d", "j1")
	jp.addJournal("d", "j2")
	s.journalProvider = jp
	if _, _, _, err := s.handleGetNameList(ctx, body); err != nil {
		t.Fatalf("list journals: %v", err)
	}

	// ContinueAfter success + unknown token.
	cont, _ := pdu.MarshalGetNameListRequest(1, int(ObjectClassJournal), pdu.ScopeDomain, "d", "j1")
	if _, _, _, err := s.handleGetNameList(ctx, extractConfirmedBody(t, cont)); err != nil {
		t.Fatalf("continue: %v", err)
	}
	badCont, _ := pdu.MarshalGetNameListRequest(1, int(ObjectClassJournal), pdu.ScopeDomain, "d", "nope")
	if _, _, _, err := s.handleGetNameList(ctx, extractConfirmedBody(t, badCont)); err == nil {
		t.Fatal("bad continueAfter")
	}

	// Provider error.
	s.journalProvider = &failListJournalProvider{err: errors.New("list fail")}
	if _, _, _, err := s.handleGetNameList(ctx, body); err == nil {
		t.Fatal("provider error")
	}
}

type failListJournalProvider struct{ err error }

func (p *failListJournalProvider) ListJournals(context.Context, string) ([]string, error) {
	return nil, p.err
}
func (p *failListJournalProvider) ReadTimeRange(context.Context, string, string, time.Time, time.Time, int) (*JournalResult, error) {
	return nil, p.err
}
func (p *failListJournalProvider) ReadStartAfter(context.Context, string, string, []byte, time.Time, int) (*JournalResult, error) {
	return nil, p.err
}

func TestHandleGetVarAccess_Edges(t *testing.T) {
	s := NewServer(ServerOptions{})
	ctx := context.Background()
	if err := s.RegisterDomain("d"); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterVariable(Variable{
		Name:     ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "ok"},
		TypeSpec: TypeSpec{Type: ValueTypeInteger, Size: 32},
		Read:     func(context.Context) (*Value, error) { return NewInteger(1), nil },
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.handleGetVarAccess(ctx, []byte{0xff}); err == nil {
		t.Fatal("bad body")
	}
	miss, _ := pdu.MarshalGetVarAccessRequest(1, pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "nope"})
	if _, _, _, err := s.handleGetVarAccess(ctx, extractConfirmedBody(t, miss)); err == nil {
		t.Fatal("missing")
	}
	ok, _ := pdu.MarshalGetVarAccessRequest(1, pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "ok"})
	if _, _, _, err := s.handleGetVarAccess(ctx, extractConfirmedBody(t, ok)); err != nil {
		t.Fatalf("success: %v", err)
	}

	// Wrong TypeSpec concrete type.
	if err := s.registry.RegisterVariable(&servermodel.VarEntry{
		Domain: "d", ItemID: "badType", Scope: int(ObjectScopeDomain),
		TypeSpec: "not-TypeSpec",
	}); err != nil {
		t.Fatal(err)
	}
	badType, _ := pdu.MarshalGetVarAccessRequest(1, pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "badType"})
	if _, _, _, err := s.handleGetVarAccess(ctx, extractConfirmedBody(t, badType)); err == nil {
		t.Fatal("bad typespec type")
	}

	// typeSpecToWire failure (named type with nil name).
	if err := s.registry.RegisterVariable(&servermodel.VarEntry{
		Domain: "d", ItemID: "named", Scope: int(ObjectScopeDomain),
		TypeSpec: TypeSpec{Type: ValueTypeNamedType},
	}); err != nil {
		t.Fatal(err)
	}
	named, _ := pdu.MarshalGetVarAccessRequest(1, pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "named"})
	if _, _, _, err := s.handleGetVarAccess(ctx, extractConfirmedBody(t, named)); err == nil {
		t.Fatal("named type wire fail")
	}
}

func TestHandleRead_Edges(t *testing.T) {
	s := NewServer(ServerOptions{})
	ctx := context.Background()
	if err := s.RegisterDomain("d"); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterVariable(Variable{
		Name:     ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "ok"},
		TypeSpec: TypeSpec{Type: ValueTypeInteger, Size: 32},
		Read:     func(context.Context) (*Value, error) { return NewInteger(7), nil },
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterVariable(Variable{
		Name: ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "arr"},
		TypeSpec: TypeSpec{
			Type: ValueTypeArray, Count: 2,
			Element: &TypeSpec{Type: ValueTypeInteger, Size: 32},
		},
		Read: func(context.Context) (*Value, error) {
			return NewArray([]*Value{NewInteger(1), NewInteger(2)}), nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.registry.RegisterVariable(&servermodel.VarEntry{
		Domain: "d", ItemID: "noRead", Scope: int(ObjectScopeDomain),
		TypeSpec: TypeSpec{Type: ValueTypeInteger, Size: 32},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.registry.RegisterVariable(&servermodel.VarEntry{
		Domain: "d", ItemID: "failRead", Scope: int(ObjectScopeDomain),
		TypeSpec: TypeSpec{Type: ValueTypeInteger, Size: 32},
		ReadFunc: func(context.Context) (*Value, error) { return nil, errors.New("r") },
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.registry.RegisterVariable(&servermodel.VarEntry{
		Domain: "d", ItemID: "badVal", Scope: int(ObjectScopeDomain),
		TypeSpec: TypeSpec{Type: ValueTypeInteger, Size: 32},
		ReadFunc: func(context.Context) (*Value, error) {
			return &Value{typ: ValueTypeNamedType}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterNamedVariableList(NamedVariableList{
		Name: ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "L"},
		Variables: []VariableSpec{
			{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "ok"}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := s.handleRead(ctx, []byte{0xff}); err == nil {
		t.Fatal("bad body")
	}

	// Missing NVL.
	missNVL, _ := pdu.MarshalReadRequestByListName(1, pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "nope"})
	if _, _, _, err := s.handleRead(ctx, extractConfirmedBody(t, missNVL)); err == nil {
		t.Fatal("missing nvl")
	}

	// Multi-path variable list: ok, missing, noRead, failRead, badVal, bad AA.
	vars := []pdu.VariableSpecWire{
		{Name: pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "ok"}},
		{Name: pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "missing"}},
		{Name: pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "noRead"}},
		{Name: pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "failRead"}},
		{Name: pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "badVal"}},
		{
			Name:            pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "arr"},
			AlternateAccess: []pdu.AccessSelectorWire{{HasIndex: true, Index: 9}},
		},
		{
			Name:            pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "arr"},
			AlternateAccess: []pdu.AccessSelectorWire{{HasIndex: true, Index: 0}},
		},
	}
	readPDU, err := pdu.MarshalReadRequestWithAccess(1, vars)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.handleRead(ctx, extractConfirmedBody(t, readPDU)); err != nil {
		t.Fatalf("multi read: %v", err)
	}

	// SpecWithResult via list name.
	specPDU, err := pdu.MarshalReadRequestByListNameWithSpec(1, pdu.ObjectNameWire{
		Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "L",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.handleRead(ctx, extractConfirmedBody(t, specPDU)); err != nil {
		t.Fatalf("specWithResult: %v", err)
	}
}

func TestHandleWrite_Edges(t *testing.T) {
	s := NewServer(ServerOptions{})
	ctx := context.Background()
	if err := s.RegisterDomain("d"); err != nil {
		t.Fatal(err)
	}
	var last *Value
	if err := s.RegisterVariable(Variable{
		Name:     ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "w"},
		TypeSpec: TypeSpec{Type: ValueTypeInteger, Size: 32},
		Read:     func(context.Context) (*Value, error) { return NewInteger(0), nil },
		Write: func(_ context.Context, v *Value) error {
			last = v
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterVariable(Variable{
		Name: ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "arr"},
		TypeSpec: TypeSpec{
			Type: ValueTypeArray, Count: 2,
			Element: &TypeSpec{Type: ValueTypeInteger, Size: 32},
		},
		Read: func(context.Context) (*Value, error) {
			return NewArray([]*Value{NewInteger(1), NewInteger(2)}), nil
		},
		Write: func(context.Context, *Value) error { return nil },
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.registry.RegisterVariable(&servermodel.VarEntry{
		Domain: "d", ItemID: "ro", Scope: int(ObjectScopeDomain),
		TypeSpec: TypeSpec{Type: ValueTypeInteger, Size: 32},
		ReadFunc: func(context.Context) (*Value, error) { return NewInteger(1), nil },
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.registry.RegisterVariable(&servermodel.VarEntry{
		Domain: "d", ItemID: "dae", Scope: int(ObjectScopeDomain),
		TypeSpec: TypeSpec{Type: ValueTypeInteger, Size: 32},
		ReadFunc: func(context.Context) (*Value, error) { return NewInteger(1), nil },
		WriteFunc: func(context.Context, *Value) error {
			return &DataAccessError{Code: DataAccessErrorObjectAccessDenied}
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.registry.RegisterVariable(&servermodel.VarEntry{
		Domain: "d", ItemID: "temp", Scope: int(ObjectScopeDomain),
		TypeSpec:  TypeSpec{Type: ValueTypeInteger, Size: 32},
		ReadFunc:  func(context.Context) (*Value, error) { return NewInteger(1), nil },
		WriteFunc: func(context.Context, *Value) error { return errors.New("temp") },
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.registry.RegisterVariable(&servermodel.VarEntry{
		Domain: "d", ItemID: "aaRO", Scope: int(ObjectScopeDomain),
		TypeSpec: TypeSpec{
			Type: ValueTypeArray, Count: 2,
			Element: &TypeSpec{Type: ValueTypeInteger, Size: 32},
		},
		WriteFunc: func(context.Context, *Value) error { return nil },
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.registry.RegisterVariable(&servermodel.VarEntry{
		Domain: "d", ItemID: "aaFailRead", Scope: int(ObjectScopeDomain),
		TypeSpec: TypeSpec{
			Type: ValueTypeArray, Count: 2,
			Element: &TypeSpec{Type: ValueTypeInteger, Size: 32},
		},
		ReadFunc:  func(context.Context) (*Value, error) { return nil, errors.New("r") },
		WriteFunc: func(context.Context, *Value) error { return nil },
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterNamedVariableList(NamedVariableList{
		Name: ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "WL"},
		Variables: []VariableSpec{
			{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "w"}},
			{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "w"}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := s.handleWrite(ctx, []byte{0xff}); err == nil {
		t.Fatal("bad body")
	}
	missNVL, _ := pdu.MarshalWriteRequestByListName(1, pdu.ObjectNameWire{
		Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "nope",
	}, []*pdu.DataValue{{Tag: pdu.TagDataInteger, Int: 1}})
	if _, _, _, err := s.handleWrite(ctx, extractConfirmedBody(t, missNVL)); err == nil {
		t.Fatal("missing nvl")
	}

	// Fewer values than NVL members → type inconsistent for second.
	shortNVL, err := pdu.MarshalWriteRequestByListName(1, pdu.ObjectNameWire{
		Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "WL",
	}, []*pdu.DataValue{{Tag: pdu.TagDataInteger, Int: 5}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.handleWrite(ctx, extractConfirmedBody(t, shortNVL)); err != nil {
		t.Fatalf("short nvl write: %v", err)
	}

	vars := []pdu.VariableSpecWire{
		{Name: pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "w"}},
		{Name: pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "missing"}},
		{Name: pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "ro"}},
		{Name: pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "dae"}},
		{Name: pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "temp"}},
		{
			Name:            pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "aaRO"},
			AlternateAccess: []pdu.AccessSelectorWire{{HasIndex: true, Index: 0}},
		},
		{
			Name:            pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "aaFailRead"},
			AlternateAccess: []pdu.AccessSelectorWire{{HasIndex: true, Index: 0}},
		},
		{
			Name:            pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "arr"},
			AlternateAccess: []pdu.AccessSelectorWire{{HasIndex: true, Index: 9}},
		},
		{
			Name:            pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "arr"},
			AlternateAccess: []pdu.AccessSelectorWire{{HasIndex: true, Index: 0}},
		},
	}
	vals := []*pdu.DataValue{
		{Tag: pdu.TagDataInteger, Int: 11},
		{Tag: pdu.TagDataInteger, Int: 1},
		{Tag: pdu.TagDataInteger, Int: 1},
		{Tag: pdu.TagDataInteger, Int: 1},
		{Tag: pdu.TagDataInteger, Int: 1},
		{Tag: pdu.TagDataInteger, Int: 1},
		{Tag: pdu.TagDataInteger, Int: 1},
		{Tag: pdu.TagDataInteger, Int: 1},
		{Tag: pdu.TagDataInteger, Int: 99},
	}
	writePDU, err := pdu.MarshalWriteRequestWithAccess(1, vars, vals)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.handleWrite(ctx, extractConfirmedBody(t, writePDU)); err != nil {
		t.Fatalf("multi write: %v", err)
	}
	if last == nil {
		t.Fatal("expected successful write to w")
	}

	// dataValueToValue failure via invalid access-error wire code.
	badDV, err := pdu.MarshalWriteRequest(1,
		[]pdu.ObjectNameWire{{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "w"}},
		[]*pdu.DataValue{{Tag: pdu.TagDataAccessError, ErrCode: 99999}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.handleWrite(ctx, extractConfirmedBody(t, badDV)); err != nil {
		t.Fatalf("bad data value: %v", err)
	}
}
