// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/pdu"
	"github.com/otfabric/go-mms/internal/serverconn"
)

func extractConfirmedBody(t *testing.T, pduBytes []byte) []byte {
	t.Helper()
	_, content, err := codec.UnwrapPdu(pduBytes)
	if err != nil {
		t.Fatalf("unwrap: %v", err)
	}
	_, svc, err := codec.UnmarshalConfirmedRequest(content)
	if err != nil {
		t.Fatalf("confirmed: %v", err)
	}
	return svc.Bytes
}

func TestFileHandlers_NoProviderAndBadBody(t *testing.T) {
	s := NewServer(ServerOptions{})
	ctx := context.Background()
	handlers := []struct {
		name string
		fn   func(context.Context, []byte) (int, bool, []byte, error)
	}{
		{"open", s.handleFileOpen},
		{"read", s.handleFileRead},
		{"close", s.handleFileClose},
		{"delete", s.handleFileDelete},
		{"rename", s.handleFileRename},
		{"obtain", s.handleObtainFile},
		{"dir", s.handleFileDirectory},
	}
	for _, h := range handlers {
		if _, _, _, err := h.fn(ctx, nil); err == nil {
			t.Fatalf("%s: expected unsupported without provider", h.name)
		}
	}

	fp := newMemFileProvider()
	fp.addFile("a.txt", []byte("hi"))
	s.fileProvider = fp

	for _, h := range handlers {
		if _, _, _, err := h.fn(ctx, []byte{0xff}); err == nil {
			t.Fatalf("%s: expected invalid request", h.name)
		}
	}

	// Open without ServerConn closes handle and errors.
	openPDU, err := pdu.MarshalFileOpenRequest(1, "a.txt", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.handleFileOpen(ctx, extractConfirmedBody(t, openPDU)); err == nil {
		t.Fatal("open without conn should fail")
	}

	readPDU, _ := pdu.MarshalFileReadRequest(1, 1)
	if _, _, _, err := s.handleFileRead(ctx, extractConfirmedBody(t, readPDU)); err == nil {
		t.Fatal("read without conn")
	}
	closePDU, _ := pdu.MarshalFileCloseRequest(1, 1)
	if _, _, _, err := s.handleFileClose(ctx, extractConfirmedBody(t, closePDU)); err == nil {
		t.Fatal("close without conn")
	}

	sc := &ServerConn{frsmTable: newFRSMTable()}
	ctxSC := context.WithValue(ctx, serverConnCtxKey{}, sc)
	if _, _, _, err := s.handleFileRead(ctxSC, extractConfirmedBody(t, readPDU)); err == nil {
		t.Fatal("unknown frsm")
	}
	if _, _, _, err := s.handleFileClose(ctxSC, extractConfirmedBody(t, closePDU)); err == nil {
		t.Fatal("unknown frsm close")
	}

	tag, ok, payload, err := s.handleFileOpen(ctxSC, extractConfirmedBody(t, openPDU))
	if err != nil || !ok || tag == 0 || len(payload) == 0 {
		t.Fatalf("open: tag=%d ok=%v err=%v", tag, ok, err)
	}
	var frsmID int32 = 1
	readPDU, _ = pdu.MarshalFileReadRequest(1, frsmID)
	if _, _, _, err := s.handleFileRead(ctxSC, extractConfirmedBody(t, readPDU)); err != nil {
		t.Fatalf("read: %v", err)
	}
	closePDU, _ = pdu.MarshalFileCloseRequest(1, frsmID)
	if _, _, _, err := s.handleFileClose(ctxSC, extractConfirmedBody(t, closePDU)); err != nil {
		t.Fatalf("close: %v", err)
	}

	delPDU, _ := pdu.MarshalFileDeleteRequest(1, "missing.txt")
	_, _, _, err = s.handleFileDelete(ctxSC, extractConfirmedBody(t, delPDU))
	if err == nil {
		t.Fatal("delete missing")
	}
	var se *serverconn.ServiceError
	if !errors.As(err, &se) || se.ErrorCode != fileErrFileNonExistent {
		t.Fatalf("want non-existent file err, got %v", err)
	}

	// Obtain / rename / directory success + obtain error.
	src := "a.txt"
	dst := "b.txt"
	obtainPDU, err := pdu.MarshalObtainFileRequest(1, src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.handleObtainFile(ctxSC, extractConfirmedBody(t, obtainPDU)); err != nil {
		t.Fatalf("obtain: %v", err)
	}
	renPDU, err := pdu.MarshalFileRenameRequest(1, dst, "c.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.handleFileRename(ctxSC, extractConfirmedBody(t, renPDU)); err != nil {
		t.Fatalf("rename: %v", err)
	}
	dirPDU, err := pdu.MarshalFileDirectoryRequest(1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.handleFileDirectory(ctxSC, extractConfirmedBody(t, dirPDU)); err != nil {
		t.Fatalf("dir: %v", err)
	}
}

func TestFileErrorMapping(t *testing.T) {
	cases := []struct {
		err  error
		code int
	}{
		{fs.ErrNotExist, fileErrFileNonExistent},
		{fs.ErrPermission, fileErrFileAccessDenied},
		{ErrFileAccessDenied, fileErrFileAccessDenied},
		{errors.New("other"), fileErrOther},
	}
	for _, tc := range cases {
		err := fileError(tc.err)
		var se *serverconn.ServiceError
		if !errors.As(err, &se) || se.ErrorCode != tc.code {
			t.Fatalf("%v -> %+v want code %d", tc.err, err, tc.code)
		}
	}
}

func TestWireErrCode(t *testing.T) {
	if wireErrCode(DataAccessErrorObjectNonExistent) != int(DataAccessErrorObjectNonExistent) {
		t.Fatal("wire code mismatch")
	}
}

func TestRegisterNVL_WithAlternateAccess(t *testing.T) {
	s := NewServer(ServerOptions{})
	if err := s.RegisterDomain("d"); err != nil {
		t.Fatal(err)
	}
	idx := 1
	err := s.RegisterNamedVariableList(NamedVariableList{
		Name:      ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "nvlAA"},
		Deletable: true,
		Variables: []VariableSpec{{
			Name: ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "arr"},
			AlternateAccess: []AccessSelector{
				{Component: "x"},
				{Index: &idx},
				{IndexRange: &IndexRange{Start: 0, Count: 2}},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := s.registry.LookupNVL(int(ObjectScopeDomain), "d", "nvlAA")
	if !ok || len(entry.Variables) != 1 || len(entry.Variables[0].AlternateAccess) != 3 {
		t.Fatalf("%+v ok=%v", entry, ok)
	}
}

func TestNVLHandlers_Direct(t *testing.T) {
	s := NewServer(ServerOptions{})
	if err := s.RegisterDomain("d"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, _, _, err := s.handleDefineNVL(ctx, []byte{0xff}); err == nil {
		t.Fatal("define bad body")
	}
	if _, _, _, err := s.handleGetNVLAttrs(ctx, []byte{0xff}); err == nil {
		t.Fatal("getattrs bad body")
	}
	if _, _, _, err := s.handleDeleteNVL(ctx, []byte{0xff}); err == nil {
		t.Fatal("delete bad body")
	}

	defPDU, err := pdu.MarshalDefineNamedVarListRequest(1,
		pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "L1"},
		[]pdu.VariableSpecWire{{
			Name: pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "v1"},
			AlternateAccess: []pdu.AccessSelectorWire{
				{Component: "c"},
				{HasIndex: true, Index: 0},
				{IndexRange: &pdu.IndexRangeWire{LowIndex: 0, NumberOfElements: 1}},
			},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.handleDefineNVL(ctx, extractConfirmedBody(t, defPDU)); err != nil {
		t.Fatalf("define: %v", err)
	}
	if _, _, _, err := s.handleDefineNVL(ctx, extractConfirmedBody(t, defPDU)); err == nil {
		t.Fatal("duplicate define")
	}

	getPDU, err := pdu.MarshalGetNamedVarListAttrsRequest(1, pdu.ObjectNameWire{
		Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "L1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.handleGetNVLAttrs(ctx, extractConfirmedBody(t, getPDU)); err != nil {
		t.Fatalf("getattrs: %v", err)
	}
	missPDU, _ := pdu.MarshalGetNamedVarListAttrsRequest(1, pdu.ObjectNameWire{
		Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "nope",
	})
	if _, _, _, err := s.handleGetNVLAttrs(ctx, extractConfirmedBody(t, missPDU)); err == nil {
		t.Fatal("missing nvl")
	}

	// Association-scope define requires ServerConn.
	aaDef, err := pdu.MarshalDefineNamedVarListRequest(1,
		pdu.ObjectNameWire{Scope: pdu.ScopeAssociation, ItemID: "aaL"},
		[]pdu.VariableSpecWire{{
			Name: pdu.ObjectNameWire{Scope: pdu.ScopeAssociation, ItemID: "aaV"},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.handleDefineNVL(ctx, extractConfirmedBody(t, aaDef)); err == nil {
		t.Fatal("aa define without conn")
	}
	sc := &ServerConn{}
	ctxSC := context.WithValue(ctx, serverConnCtxKey{}, sc)
	if _, _, _, err := s.handleDefineNVL(ctxSC, extractConfirmedBody(t, aaDef)); err != nil {
		t.Fatalf("aa define: %v", err)
	}
	// Duplicate AA.
	if _, _, _, err := s.handleDefineNVL(ctxSC, extractConfirmedBody(t, aaDef)); err == nil {
		t.Fatal("duplicate aa define")
	}
	aaGet, _ := pdu.MarshalGetNamedVarListAttrsRequest(1, pdu.ObjectNameWire{
		Scope: pdu.ScopeAssociation, ItemID: "aaL",
	})
	if _, _, _, err := s.handleGetNVLAttrs(ctxSC, extractConfirmedBody(t, aaGet)); err != nil {
		t.Fatalf("aa getattrs: %v", err)
	}

	// resolveNVLMembers via AA + missing.
	members, err := s.resolveNVLMembers(ctxSC, &pdu.ObjectNameWire{Scope: pdu.ScopeAssociation, ItemID: "aaL"})
	if err != nil || len(members) != 1 {
		t.Fatalf("resolve aa: %v %v", members, err)
	}
	if _, err := s.resolveNVLMembers(ctxSC, &pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "nope"}); err == nil {
		t.Fatal("resolve missing")
	}
	members, err = s.resolveNVLMembers(ctx, &pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "L1"})
	if err != nil || len(members) != 1 || len(members[0].AlternateAccess) != 3 {
		t.Fatalf("resolve domain: %+v err=%v", members, err)
	}

	// Delete specific domain + AA names.
	delSpec, err := pdu.MarshalDeleteNamedVarListRequest(1, []pdu.ObjectNameWire{
		{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "L1"},
		{Scope: pdu.ScopeAssociation, ItemID: "aaL"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.handleDeleteNVL(ctxSC, extractConfirmedBody(t, delSpec)); err != nil {
		t.Fatalf("delete specific: %v", err)
	}

	// Re-define AA then delete-all AA scope.
	if _, _, _, err := s.handleDefineNVL(ctxSC, extractConfirmedBody(t, aaDef)); err != nil {
		t.Fatal(err)
	}
	aaDelBody := append(
		berutil.EncodeTLV(0x80, berutil.EncodeInt(1)), // scopeOfDelete=aa-specific
		berutil.EncodeTLV(0xa1, nil)...,               // empty list
	)
	if _, _, _, err := s.handleDeleteNVL(ctx, aaDelBody); err == nil {
		t.Fatal("aa-delete without conn")
	}
	if _, _, _, err := s.handleDeleteNVL(ctxSC, aaDelBody); err != nil {
		t.Fatalf("aa-delete all: %v", err)
	}

	// Domain / VMD scope deletes + unsupported scope.
	domDel, _ := pdu.MarshalDeleteNVLDomainScopeRequest(1, "d")
	if _, _, _, err := s.handleDeleteNVL(ctx, extractConfirmedBody(t, domDel)); err != nil {
		t.Fatalf("domain delete: %v", err)
	}
	emptyDom := append(
		berutil.EncodeTLV(0x80, berutil.EncodeInt(2)),
		berutil.EncodeTLV(0xa1, nil)...,
	)
	if _, _, _, err := s.handleDeleteNVL(ctx, emptyDom); err == nil {
		t.Fatal("domain delete empty name")
	}
	vmdDel, _ := pdu.MarshalDeleteNVLVMDScopeRequest(1)
	if _, _, _, err := s.handleDeleteNVL(ctx, extractConfirmedBody(t, vmdDel)); err != nil {
		t.Fatalf("vmd delete: %v", err)
	}
	badScope := berutil.EncodeTLV(0x80, berutil.EncodeInt(9))
	badScope = append(badScope, berutil.EncodeTLV(0xa1, nil)...)
	if _, _, _, err := s.handleDeleteNVL(ctx, badScope); err == nil {
		t.Fatal("unsupported delete scope")
	}
}
