// SPDX-License-Identifier: MIT

package mms

import (
	"testing"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/pdu"
)

func TestWriteObject_SendConfirmedClosed(t *testing.T) {
	client, srv, _, ctx := associateMockClient(t)
	defer closeMockClient(t, client, srv, ctx)

	name := ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "v"}
	// Invalid OID fails inside MarshalWriteRequest after valueToDataValue succeeds.
	if _, err := client.WriteObject(ctx, name, NewObjectIdentifier([]int{1})); err == nil {
		t.Fatal("expected marshal write error")
	}

	client.mu.Lock()
	client.closed = true
	client.mu.Unlock()
	if _, err := client.WriteObject(ctx, name, NewInteger(1)); err == nil {
		t.Fatal("expected ErrClosed")
	}
}

func TestReadNamedVariableList_DataValueConversionError(t *testing.T) {
	client, srv, _, ctx := associateMockClient(t)
	defer closeMockClient(t, client, srv, ctx)
	list := ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "L"}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		// Array containing AccessError with invalid code → dataValueToValue fails.
		body, _ := pdu.MarshalReadResponse([]*pdu.AccessResult{
			{Data: &pdu.DataValue{
				Tag: pdu.TagDataArray,
				Elements: []*pdu.DataValue{
					{Tag: pdu.TagDataAccessError, ErrCode: 99999},
				},
			}},
		})
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumRead, true, body)
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.ReadNamedVariableList(ctx, list); err == nil {
		t.Fatal("expected conversion error")
	}
}

func TestWriteNamedVariableList_SendConfirmedClosed(t *testing.T) {
	client, srv, _, ctx := associateMockClient(t)
	defer closeMockClient(t, client, srv, ctx)
	list := ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "L"}

	if _, err := client.WriteNamedVariableList(ctx, list, []*Value{NewObjectIdentifier([]int{1})}); err == nil {
		t.Fatal("expected marshal write NVL error")
	}

	client.mu.Lock()
	client.closed = true
	client.mu.Unlock()
	if _, err := client.WriteNamedVariableList(ctx, list, []*Value{NewInteger(1)}); err == nil {
		t.Fatal("expected ErrClosed")
	}
}

func TestGetNameList_Edges(t *testing.T) {
	client, srv, _, ctx := associateMockClient(t)
	defer closeMockClient(t, client, srv, ctx)

	if _, err := client.GetNameList(ctx, NameListRequest{
		ObjectClass: ObjectClass(99),
		Scope:       ObjectScopeVMD,
	}); err == nil {
		t.Fatal("unknown object class")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		srv.sendIdentifyResponse(ctx, id)
	}()
	if _, err := client.GetNameList(ctx, NameListRequest{
		ObjectClass: ObjectClassNamedVariable,
		Scope:       ObjectScopeAssociation,
	}); err == nil {
		t.Fatal("wrong service")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumGetNameList, true, []byte{0xff})
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.GetNameList(ctx, NameListRequest{
		ObjectClass: ObjectClassDomain,
		Scope:       ObjectScopeVMD,
	}); err == nil {
		t.Fatal("unmarshal error")
	}

	// Happy path association scope (covers remaining switch arm used in request).
	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		srv.sendGetNameListResponse(ctx, id, []string{"aa1"}, false)
	}()
	res, err := client.GetNameList(ctx, NameListRequest{
		ObjectClass: ObjectClassNamedVariable,
		Scope:       ObjectScopeAssociation,
	})
	if err != nil || len(res.Names) != 1 {
		t.Fatalf("%v %v", res, err)
	}
}

func TestGetNameListAll_Edges(t *testing.T) {
	client, srv, _, ctx := associateMockClient(t)
	defer closeMockClient(t, client, srv, ctx)

	if _, err := client.GetNameListAll(ctx, NameListRequest{
		ObjectClass: ObjectClass(99),
	}); err == nil {
		t.Fatal("propagated GetNameList error")
	}

	// MoreFollows with empty page ends pagination.
	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		srv.sendGetNameListResponse(ctx, id, nil, true)
	}()
	all, err := client.GetNameListAll(ctx, NameListRequest{
		ObjectClass: ObjectClassDomain,
		Scope:       ObjectScopeVMD,
	})
	if err != nil || len(all) != 0 {
		t.Fatalf("%v %v", all, err)
	}
}

func TestGetVariableAccessAttributes_Edges(t *testing.T) {
	client, srv, _, ctx := associateMockClient(t)
	defer closeMockClient(t, client, srv, ctx)
	name := ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "v"}

	if _, err := client.GetVariableAccessAttributes(ctx, ObjectName{Scope: ObjectScopeDomain, ItemID: "v"}); err == nil {
		t.Fatal("bad name")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		srv.sendIdentifyResponse(ctx, id)
	}()
	if _, err := client.GetVariableAccessAttributes(ctx, name); err == nil {
		t.Fatal("wrong service")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumGetVariableAccessAttributes, true, []byte{0xff})
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.GetVariableAccessAttributes(ctx, name); err == nil {
		t.Fatal("unmarshal")
	}

	// ObjectIdentifier type tag 15 is decoded on the wire but not mapped by typeSpecFromWire.
	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		ts := berutil.EncodeTLV(0x8f, nil) // [15] NULL
		srv.sendGetVarAccessResponse(ctx, id, false, ts)
	}()
	if _, err := client.GetVariableAccessAttributes(ctx, name); err == nil {
		t.Fatal("typeSpecFromWire")
	}

	client.mu.Lock()
	client.closed = true
	client.mu.Unlock()
	if _, err := client.GetVariableAccessAttributes(ctx, name); err == nil {
		t.Fatal("closed")
	}
}

func TestDefineNamedVariableList_Edges(t *testing.T) {
	client, srv, _, ctx := associateMockClient(t)
	defer closeMockClient(t, client, srv, ctx)
	list := ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "L"}
	okVar := VariableSpec{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "v"}}

	// Empty alternate-access selector fails at marshal time.
	if err := client.DefineNamedVariableList(ctx, DefineNamedVariableListRequest{
		ListName: list,
		Variables: []VariableSpec{{
			Name:            okVar.Name,
			AlternateAccess: []AccessSelector{{}},
		}},
	}); err == nil {
		t.Fatal("marshal empty selector")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		srv.sendIdentifyResponse(ctx, id)
	}()
	if err := client.DefineNamedVariableList(ctx, DefineNamedVariableListRequest{
		ListName:  list,
		Variables: []VariableSpec{okVar},
	}); err == nil {
		t.Fatal("wrong service")
	}

	client.mu.Lock()
	client.closed = true
	client.mu.Unlock()
	if err := client.DefineNamedVariableList(ctx, DefineNamedVariableListRequest{
		ListName:  list,
		Variables: []VariableSpec{okVar},
	}); err == nil {
		t.Fatal("closed")
	}
}

func TestGetNamedVariableListAttributes_Edges(t *testing.T) {
	client, srv, _, ctx := associateMockClient(t)
	defer closeMockClient(t, client, srv, ctx)
	list := ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "L"}

	if _, err := client.GetNamedVariableListAttributes(ctx, ObjectName{Scope: ObjectScopeDomain, ItemID: "L"}); err == nil {
		t.Fatal("bad name")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		srv.sendIdentifyResponse(ctx, id)
	}()
	if _, err := client.GetNamedVariableListAttributes(ctx, list); err == nil {
		t.Fatal("wrong service")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumGetNamedVariableListAttrs, true, []byte{0xff})
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.GetNamedVariableListAttributes(ctx, list); err == nil {
		t.Fatal("unmarshal")
	}

	client.mu.Lock()
	client.closed = true
	client.mu.Unlock()
	if _, err := client.GetNamedVariableListAttributes(ctx, list); err == nil {
		t.Fatal("closed")
	}
}

func TestDeleteNamedVariableList_Edges(t *testing.T) {
	client, srv, _, ctx := associateMockClient(t)
	defer closeMockClient(t, client, srv, ctx)
	list := ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "L"}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		srv.sendIdentifyResponse(ctx, id)
	}()
	if _, err := client.DeleteNamedVariableList(ctx, []ObjectName{list}); err == nil {
		t.Fatal("wrong service")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumDeleteNamedVariableList, true, []byte{0xff})
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.DeleteNamedVariableList(ctx, []ObjectName{list}); err == nil {
		t.Fatal("unmarshal")
	}

	client.mu.Lock()
	client.closed = true
	client.mu.Unlock()
	if _, err := client.DeleteNamedVariableList(ctx, []ObjectName{list}); err == nil {
		t.Fatal("closed")
	}
}

func TestDeleteAllDomainNVLs_Edges(t *testing.T) {
	client, srv, _, ctx := associateMockClient(t)
	defer closeMockClient(t, client, srv, ctx)

	if _, err := client.DeleteAllDomainNVLs(ctx, ""); err == nil {
		t.Fatal("empty domain")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		srv.sendIdentifyResponse(ctx, id)
	}()
	if _, err := client.DeleteAllDomainNVLs(ctx, "d"); err == nil {
		t.Fatal("wrong service")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumDeleteNamedVariableList, true, []byte{0xff})
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.DeleteAllDomainNVLs(ctx, "d"); err == nil {
		t.Fatal("unmarshal")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		srv.sendDeleteNamedVarListResponse(ctx, id, 2, 2)
	}()
	res, err := client.DeleteAllDomainNVLs(ctx, "d")
	if err != nil || res.NumberDeleted != 2 {
		t.Fatalf("%v %v", res, err)
	}

	client.mu.Lock()
	client.closed = true
	client.mu.Unlock()
	if _, err := client.DeleteAllDomainNVLs(ctx, "d"); err == nil {
		t.Fatal("closed")
	}
}

func TestDeleteAllVMDNVLs_Edges(t *testing.T) {
	client, srv, _, ctx := associateMockClient(t)
	defer closeMockClient(t, client, srv, ctx)

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		srv.sendIdentifyResponse(ctx, id)
	}()
	if _, err := client.DeleteAllVMDNVLs(ctx); err == nil {
		t.Fatal("wrong service")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumDeleteNamedVariableList, true, []byte{0xff})
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.DeleteAllVMDNVLs(ctx); err == nil {
		t.Fatal("unmarshal")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		srv.sendDeleteNamedVarListResponse(ctx, id, 1, 1)
	}()
	res, err := client.DeleteAllVMDNVLs(ctx)
	if err != nil || res.NumberMatched != 1 {
		t.Fatalf("%v %v", res, err)
	}

	client.mu.Lock()
	client.closed = true
	client.mu.Unlock()
	if _, err := client.DeleteAllVMDNVLs(ctx); err == nil {
		t.Fatal("closed")
	}
}
