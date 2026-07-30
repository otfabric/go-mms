// SPDX-License-Identifier: MIT

package pdu

import (
	"encoding/asn1"
	"testing"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
)

func TestDecodeVariableSpec_Errors(t *testing.T) {
	if _, err := decodeVariableSpec([]byte{0xff}); err == nil {
		t.Fatal("expected TLV error")
	}
	if _, err := decodeVariableSpec(berutil.EncodeTLV(0xa1, nil)); err == nil {
		t.Fatal("expected non-name tag error")
	}
	// Success path (name only).
	name, err := EncodeObjectName(ObjectNameWire{Scope: ScopeVMD, ItemID: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeVariableSpec(asn1util.WrapConstructed(0, name))
	if err != nil || got.ItemID != "ok" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestDecodeVariableSpecFull_WithAlternateAccess(t *testing.T) {
	name, err := EncodeObjectName(ObjectNameWire{Scope: ScopeDomain, DomainID: "d", ItemID: "v"})
	if err != nil {
		t.Fatal(err)
	}
	nameWrap := asn1util.WrapConstructed(0, name)
	aaInner, err := encodeAlternateAccess([]AccessSelectorWire{{Component: "comp"}})
	if err != nil {
		t.Fatal(err)
	}
	aaWrap := berutil.EncodeTLV(tagAltAccessWrapper, aaInner)
	data := append(append([]byte{}, nameWrap...), aaWrap...)

	spec, err := decodeVariableSpecFull(data)
	if err != nil {
		t.Fatalf("decodeVariableSpecFull: %v", err)
	}
	if spec.Name.ItemID != "v" {
		t.Fatalf("name=%+v", spec.Name)
	}
	if len(spec.AlternateAccess) != 1 || spec.AlternateAccess[0].Component != "comp" {
		t.Fatalf("aa=%+v", spec.AlternateAccess)
	}
}

func TestDecodeVariableSpecFull_Errors(t *testing.T) {
	if _, err := decodeVariableSpecFull([]byte{0xff}); err == nil {
		t.Fatal("expected TLV error")
	}
	if _, err := decodeVariableSpecFull(berutil.EncodeTLV(0xa1, nil)); err == nil {
		t.Fatal("expected non-name tag")
	}
	name, _ := EncodeObjectName(ObjectNameWire{Scope: ScopeVMD, ItemID: "x"})
	nameWrap := asn1util.WrapConstructed(0, name)
	// Unexpected second tag.
	junk := append(append([]byte{}, nameWrap...), berutil.EncodeTLV(0xa1, []byte{1})...)
	if _, err := decodeVariableSpecFull(junk); err == nil {
		t.Fatal("expected unexpected tag error")
	}
	badName := asn1util.WrapConstructed(0, []byte{0xff})
	if _, err := decodeVariableSpecFull(badName); err == nil {
		t.Fatal("expected object name error")
	}
	// Truncated alternate-access TLV after name.
	truncAA := append(append([]byte{}, nameWrap...), 0xa5, 0x05)
	if _, err := decodeVariableSpecFull(truncAA); err == nil {
		t.Fatal("expected truncated AA TLV error")
	}
	// Bad alternate-access content (unknown selector tag inside wrapper).
	badAA := append(append([]byte{}, nameWrap...), berutil.EncodeTLV(tagAltAccessWrapper, berutil.EncodeTLV(0x04, []byte{1}))...)
	if _, err := decodeVariableSpecFull(badAA); err == nil {
		t.Fatal("expected AA decode error")
	}
	// Trailing bytes after a valid AA selector.
	aaInner, err := encodeAlternateAccess([]AccessSelectorWire{{Component: "c"}})
	if err != nil {
		t.Fatal(err)
	}
	trail := append(append([]byte{}, nameWrap...), berutil.EncodeTLV(tagAltAccessWrapper, aaInner)...)
	trail = append(trail, 0x00)
	if _, err := decodeVariableSpecFull(trail); err == nil {
		t.Fatal("expected trailing bytes error")
	}
}

func TestDecodeVariableList_Errors(t *testing.T) {
	if _, err := decodeVariableList([]byte{0xff}); err == nil {
		t.Fatal("expected TLV error")
	}
	if _, err := decodeVariableList(berutil.EncodeTLV(0xa0, nil)); err == nil {
		t.Fatal("expected non-SEQUENCE tag")
	}
	// SEQUENCE with bad variableSpec inside.
	bad := berutil.EncodeTLV(tagSequence, []byte{0xff})
	if _, err := decodeVariableList(bad); err == nil {
		t.Fatal("expected nested decode error")
	}
	// Empty list is OK.
	specs, err := decodeVariableList(nil)
	if err != nil || len(specs) != 0 {
		t.Fatalf("empty list: %v %v", specs, err)
	}
}

func TestMarshalDefineNamedVarList_WithAlternateAccess(t *testing.T) {
	listName := ObjectNameWire{Scope: ScopeDomain, DomainID: "dom", ItemID: "list1"}
	vars := []VariableSpecWire{{
		Name: ObjectNameWire{Scope: ScopeDomain, DomainID: "dom", ItemID: "temp"},
		AlternateAccess: []AccessSelectorWire{
			{Component: "mag"},
		},
	}}
	b, err := MarshalDefineNamedVarListRequest(1, listName, vars)
	if err != nil {
		t.Fatalf("MarshalDefineNamedVarListRequest: %v", err)
	}
	if b[0] != asn1util.TagConfirmedRequest {
		t.Fatalf("tag=0x%02x", b[0])
	}
}

func TestMarshalDefineNamedVarList_Errors(t *testing.T) {
	_, err := MarshalDefineNamedVarListRequest(1, ObjectNameWire{Scope: 99, ItemID: "x"}, nil)
	if err == nil {
		t.Fatal("expected bad list name error")
	}
	listName := ObjectNameWire{Scope: ScopeVMD, ItemID: "L"}
	_, err = MarshalDefineNamedVarListRequest(1, listName, []VariableSpecWire{
		{Name: ObjectNameWire{Scope: 99, ItemID: "bad"}},
	})
	if err == nil {
		t.Fatal("expected bad variable name error")
	}
	// Empty access selector fails encodeAlternateAccess.
	_, err = MarshalDefineNamedVarListRequest(1, listName, []VariableSpecWire{{
		Name:            ObjectNameWire{Scope: ScopeVMD, ItemID: "v"},
		AlternateAccess: []AccessSelectorWire{{}},
	}})
	if err == nil {
		t.Fatal("expected alternate access encode error")
	}
}

func TestMarshalGetNamedVarListAttrsRequest_Error(t *testing.T) {
	if _, err := MarshalGetNamedVarListAttrsRequest(1, ObjectNameWire{Scope: 99, ItemID: "x"}); err == nil {
		t.Fatal("expected bad scope error")
	}
}

func TestMarshalDeleteNamedVarListRequest_Error(t *testing.T) {
	if _, err := MarshalDeleteNamedVarListRequest(1, []ObjectNameWire{
		{Scope: ScopeVMD, ItemID: "ok"},
		{Scope: 99, ItemID: "bad"},
	}); err == nil {
		t.Fatal("expected bad name error")
	}
}

func TestUnmarshalGetNamedVarListAttrsResponse_Errors(t *testing.T) {
	rv := func(b []byte) asn1.RawValue {
		return asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 12, IsCompound: true, Bytes: b}
	}
	if _, err := UnmarshalGetNamedVarListAttrsResponse(rv(nil)); err == nil {
		t.Fatal("empty")
	}
	if _, err := UnmarshalGetNamedVarListAttrsResponse(rv([]byte{0xff})); err == nil {
		t.Fatal("bad deletable TLV")
	}
	if _, err := UnmarshalGetNamedVarListAttrsResponse(rv(berutil.EncodeTLV(0x81, []byte{0xff}))); err == nil {
		t.Fatal("wrong deletable tag")
	}
	delOnly := berutil.EncodeTLV(0x80, []byte{0x00})
	if _, err := UnmarshalGetNamedVarListAttrsResponse(rv(delOnly)); err == nil {
		t.Fatal("missing listOfVariable")
	}
	// Truncated listOfVariable TLV.
	trunc := append(delOnly, 0xa1, 0x05)
	if _, err := UnmarshalGetNamedVarListAttrsResponse(rv(trunc)); err == nil {
		t.Fatal("truncated list")
	}
	// Wrong list tag.
	wrongList := append(delOnly, berutil.EncodeTLV(0xa2, nil)...)
	if _, err := UnmarshalGetNamedVarListAttrsResponse(rv(wrongList)); err == nil {
		t.Fatal("wrong list tag")
	}
	// Bad variable entry inside list.
	badEntry := append(delOnly, berutil.EncodeTLV(0xa1, berutil.EncodeTLV(tagSequence, []byte{0xff}))...)
	if _, err := UnmarshalGetNamedVarListAttrsResponse(rv(badEntry)); err == nil {
		t.Fatal("bad variable entry")
	}
	// Trailing bytes after valid response.
	name, _ := EncodeObjectName(ObjectNameWire{Scope: ScopeVMD, ItemID: "v"})
	entry := berutil.EncodeTLV(tagSequence, asn1util.WrapConstructed(0, name))
	ok := append(berutil.EncodeTLV(0x80, []byte{0xff}), berutil.EncodeTLV(0xa1, entry)...)
	trail := append(append([]byte{}, ok...), 0x00)
	if _, err := UnmarshalGetNamedVarListAttrsResponse(rv(trail)); err == nil {
		t.Fatal("trailing")
	}
	res, err := UnmarshalGetNamedVarListAttrsResponse(rv(ok))
	if err != nil || !res.Deletable || len(res.Variables) != 1 {
		t.Fatalf("deletable true: %+v err=%v", res, err)
	}
	falseDel := append(berutil.EncodeTLV(0x80, []byte{0x00}), berutil.EncodeTLV(0xa1, entry)...)
	res, err = UnmarshalGetNamedVarListAttrsResponse(rv(falseDel))
	if err != nil || res.Deletable || len(res.Variables) != 1 || res.Variables[0].Name.ItemID != "v" {
		t.Fatalf("deletable false: %+v err=%v", res, err)
	}
}

func TestUnmarshalDeleteNamedVarListResponse_Errors(t *testing.T) {
	rv := func(b []byte) asn1.RawValue {
		return asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 13, IsCompound: true, Bytes: b}
	}
	if _, err := UnmarshalDeleteNamedVarListResponse(rv(nil)); err == nil {
		t.Fatal("empty")
	}
	if _, err := UnmarshalDeleteNamedVarListResponse(rv([]byte{0xff})); err == nil {
		t.Fatal("bad matched TLV")
	}
	if _, err := UnmarshalDeleteNamedVarListResponse(rv(berutil.EncodeTLV(0x81, []byte{1}))); err == nil {
		t.Fatal("wrong matched tag")
	}
	// Empty INTEGER content fails DecodeUnsigned.
	badVal := berutil.EncodeTLV(0x80, nil)
	if _, err := UnmarshalDeleteNamedVarListResponse(rv(badVal)); err == nil {
		t.Fatal("bad matched value")
	}
	matchedOnly := berutil.EncodeTLV(0x80, []byte{0x01})
	if _, err := UnmarshalDeleteNamedVarListResponse(rv(matchedOnly)); err == nil {
		t.Fatal("missing deleted")
	}
	wrongDel := append(matchedOnly, berutil.EncodeTLV(0x82, []byte{0x01})...)
	if _, err := UnmarshalDeleteNamedVarListResponse(rv(wrongDel)); err == nil {
		t.Fatal("wrong deleted tag")
	}
	badDelVal := append(matchedOnly, berutil.EncodeTLV(0x81, nil)...)
	if _, err := UnmarshalDeleteNamedVarListResponse(rv(badDelVal)); err == nil {
		t.Fatal("bad deleted value")
	}
	ok := append(berutil.EncodeTLV(0x80, []byte{0x02}), berutil.EncodeTLV(0x81, []byte{0x01})...)
	trail := append(append([]byte{}, ok...), 0x00)
	if _, err := UnmarshalDeleteNamedVarListResponse(rv(trail)); err == nil {
		t.Fatal("trailing")
	}
	res, err := UnmarshalDeleteNamedVarListResponse(rv(ok))
	if err != nil || res.NumberMatched != 2 || res.NumberDeleted != 1 {
		t.Fatalf("%+v err=%v", res, err)
	}
}
