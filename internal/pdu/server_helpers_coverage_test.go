// SPDX-License-Identifier: MIT

package pdu

import (
	"math"
	"testing"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
)

func TestUnmarshalGetNameListRequest_Errors(t *testing.T) {
	if _, err := UnmarshalGetNameListRequest(nil); err == nil {
		t.Fatal("empty")
	}
	if _, err := UnmarshalGetNameListRequest(berutil.EncodeTLV(0xa1, nil)); err == nil {
		t.Fatal("wrong objectClass tag")
	}
	// Truncated objectClass inner.
	if _, err := UnmarshalGetNameListRequest(berutil.EncodeTLV(0xa0, []byte{0x80, 0x05})); err == nil {
		t.Fatal("objectClass inner TLV")
	}
	// Wrong inner tag.
	if _, err := UnmarshalGetNameListRequest(berutil.EncodeTLV(0xa0, berutil.EncodeTLV(0x81, []byte{1}))); err == nil {
		t.Fatal("wrong basicObjectClass tag")
	}
	// Trailing in explicit wrapper.
	inner := append(berutil.EncodeTLV(0x80, []byte{0}), 0x00)
	if _, err := UnmarshalGetNameListRequest(berutil.EncodeTLV(0xa0, inner)); err == nil {
		t.Fatal("objectClass trailing")
	}
	// Empty integer.
	if _, err := UnmarshalGetNameListRequest(berutil.EncodeTLV(0xa0, berutil.EncodeTLV(0x80, nil))); err == nil {
		t.Fatal("objectClass value")
	}

	objClass := berutil.EncodeTLV(0xa0, berutil.EncodeTLV(0x80, []byte{0}))
	if _, err := UnmarshalGetNameListRequest(objClass); err == nil {
		t.Fatal("missing objectScope")
	}
	// Truncated scope.
	if _, err := UnmarshalGetNameListRequest(append(objClass, 0xa1, 0x05)); err == nil {
		t.Fatal("scope TLV")
	}
	if _, err := UnmarshalGetNameListRequest(append(objClass, berutil.EncodeTLV(0xa2, nil)...)); err == nil {
		t.Fatal("wrong scope tag")
	}
	if _, err := UnmarshalGetNameListRequest(append(objClass, berutil.EncodeTLV(0xa1, nil)...)); err == nil {
		t.Fatal("empty scope wrapper")
	}
	if _, err := UnmarshalGetNameListRequest(append(objClass, berutil.EncodeTLV(0xa1, []byte{0x80, 0x05})...)); err == nil {
		t.Fatal("scope inner TLV")
	}
	scopeTrail := append(berutil.EncodeTLV(0x80, nil), 0x00)
	if _, err := UnmarshalGetNameListRequest(append(objClass, berutil.EncodeTLV(0xa1, scopeTrail)...)); err == nil {
		t.Fatal("scope trailing")
	}
	if _, err := UnmarshalGetNameListRequest(append(objClass, berutil.EncodeTLV(0xa1, berutil.EncodeTLV(0x83, nil))...)); err == nil {
		t.Fatal("unknown scope")
	}

	// continueAfter truncated + trailing after valid continueAfter.
	okScope := append(objClass, berutil.EncodeTLV(0xa1, berutil.EncodeTLV(0x80, nil))...)
	if _, err := UnmarshalGetNameListRequest(append(okScope, 0x82, 0x05)); err == nil {
		t.Fatal("continueAfter TLV")
	}
	cont := append(okScope, berutil.EncodeTLV(0x82, []byte("x"))...)
	if _, err := UnmarshalGetNameListRequest(append(cont, 0x00)); err == nil {
		t.Fatal("trailing after continueAfter")
	}
}

func TestUnmarshalGetVarAccessRequest_Errors(t *testing.T) {
	if _, err := UnmarshalGetVarAccessRequest([]byte{0xff}); err == nil {
		t.Fatal("TLV")
	}
	if _, err := UnmarshalGetVarAccessRequest(berutil.EncodeTLV(0xa1, nil)); err == nil {
		t.Fatal("address not supported")
	}
	name, _ := EncodeObjectName(ObjectNameWire{Scope: ScopeVMD, ItemID: "v"})
	trail := append(berutil.EncodeTLV(0xa0, name), 0x00)
	if _, err := UnmarshalGetVarAccessRequest(trail); err == nil {
		t.Fatal("trailing")
	}
}

func TestUnmarshalReadRequest_Edges(t *testing.T) {
	if _, err := UnmarshalReadRequest([]byte{0xff}); err == nil {
		t.Fatal("propagates parse error")
	}
	// ListName path rejected by Full.
	listName, _ := EncodeObjectName(ObjectNameWire{Scope: ScopeVMD, ItemID: "L"})
	choice := berutil.EncodeTLV(tagVarListName, listName)
	body := berutil.EncodeTLV(tagReadVarAccessSpec, choice)
	if _, err := UnmarshalReadRequestFull(body); err == nil {
		t.Fatal("listName not supported in Full")
	}
	req, err := UnmarshalReadRequestParsed(body)
	if err != nil || req.ListName == nil {
		t.Fatalf("%+v err=%v", req, err)
	}

	// Parsed errors.
	if _, err := UnmarshalReadRequestParsed(nil); err == nil {
		t.Fatal("missing vas")
	}
	if _, err := UnmarshalReadRequestParsed([]byte{0x80, 0x05}); err == nil {
		t.Fatal("specWithResult TLV")
	}
	specTrue := berutil.EncodeTLV(0x80, []byte{0xff})
	if _, err := UnmarshalReadRequestParsed(append(specTrue, 0xa1, 0x05)); err == nil {
		t.Fatal("vas TLV")
	}
	if _, err := UnmarshalReadRequestParsed(append(specTrue, berutil.EncodeTLV(0xa2, nil)...)); err == nil {
		t.Fatal("wrong vas tag")
	}
	// Trailing after vas.
	vas := berutil.EncodeTLV(tagReadVarAccessSpec, berutil.EncodeTLV(tagListOfVar, nil))
	if _, err := UnmarshalReadRequestParsed(append(vas, 0x00)); err == nil {
		t.Fatal("trailing")
	}
	// Empty/truncated choice.
	if _, err := UnmarshalReadRequestParsed(berutil.EncodeTLV(tagReadVarAccessSpec, []byte{0xa0, 0x05})); err == nil {
		t.Fatal("choice TLV")
	}
	choiceTrail := append(berutil.EncodeTLV(tagListOfVar, nil), 0x00)
	if _, err := UnmarshalReadRequestParsed(berutil.EncodeTLV(tagReadVarAccessSpec, choiceTrail)); err == nil {
		t.Fatal("choice trailing")
	}
	if _, err := UnmarshalReadRequestParsed(berutil.EncodeTLV(tagReadVarAccessSpec, berutil.EncodeTLV(0xa2, nil))); err == nil {
		t.Fatal("unknown choice")
	}
	// Bad list name + bad var list.
	if _, err := UnmarshalReadRequestParsed(berutil.EncodeTLV(tagReadVarAccessSpec, berutil.EncodeTLV(tagVarListName, []byte{0xff}))); err == nil {
		t.Fatal("bad list name")
	}
	badList := berutil.EncodeTLV(tagListOfVar, []byte{0xff})
	if _, err := UnmarshalReadRequestParsed(berutil.EncodeTLV(tagReadVarAccessSpec, badList)); err == nil {
		t.Fatal("bad var list")
	}
}

func TestDecodeVarSpecHelpers_Errors(t *testing.T) {
	if _, err := decodeVarSpecListFull([]byte{0xff}); err == nil {
		t.Fatal("TLV")
	}
	if _, err := decodeVarSpecListFull(berutil.EncodeTLV(0xa0, nil)); err == nil {
		t.Fatal("not SEQUENCE")
	}
	if _, err := decodeVarSpecListFull(berutil.EncodeTLV(tagSequence, []byte{0xff})); err == nil {
		t.Fatal("bad wire")
	}

	if _, err := decodeVariableSpecWire([]byte{0xff}); err == nil {
		t.Fatal("TLV")
	}
	if _, err := decodeVariableSpecWire(berutil.EncodeTLV(0xa1, nil)); err == nil {
		t.Fatal("wrong name tag")
	}
	if _, err := decodeVariableSpecWire(berutil.EncodeTLV(0xa0, []byte{0xff})); err == nil {
		t.Fatal("bad object name")
	}
	name, _ := EncodeObjectName(ObjectNameWire{Scope: ScopeVMD, ItemID: "v"})
	nameWrap := asn1util.WrapConstructed(0, name)
	if _, err := decodeVariableSpecWire(append(nameWrap, 0xa5, 0x05)); err == nil {
		t.Fatal("truncated AA")
	}
	if _, err := decodeVariableSpecWire(append(nameWrap, berutil.EncodeTLV(0xa1, nil)...)); err == nil {
		t.Fatal("wrong AA tag")
	}
	if _, err := decodeVariableSpecWire(append(nameWrap, berutil.EncodeTLV(tagAltAccessWrapper, berutil.EncodeTLV(0x04, []byte{1}))...)); err == nil {
		t.Fatal("bad AA content")
	}
	aa, _ := encodeAlternateAccess([]AccessSelectorWire{{Component: "c"}})
	trail := append(append([]byte{}, nameWrap...), berutil.EncodeTLV(tagAltAccessWrapper, aa)...)
	trail = append(trail, 0x00)
	if _, err := decodeVariableSpecWire(trail); err == nil {
		t.Fatal("trailing")
	}
}

func TestUnmarshalWriteRequest_Edges(t *testing.T) {
	if _, _, err := UnmarshalWriteRequestFull([]byte{0xff}); err == nil {
		t.Fatal("parse error")
	}
	// ListName rejected by Full.
	listName, _ := EncodeObjectName(ObjectNameWire{Scope: ScopeVMD, ItemID: "L"})
	body := append(berutil.EncodeTLV(tagVarListName, listName), berutil.EncodeTLV(tagWriteListOfData, berutil.EncodeTLV(TagDataInteger, []byte{1}))...)
	if _, _, err := UnmarshalWriteRequestFull(body); err == nil {
		t.Fatal("listName Full")
	}
	req, err := UnmarshalWriteRequestParsed(body)
	if err != nil || req.ListName == nil {
		t.Fatalf("%+v err=%v", req, err)
	}

	if _, err := UnmarshalWriteRequestParsed(nil); err == nil {
		t.Fatal("missing vas")
	}
	if _, err := UnmarshalWriteRequestParsed([]byte{0xa0, 0x05}); err == nil {
		t.Fatal("vas TLV")
	}
	if _, err := UnmarshalWriteRequestParsed(berutil.EncodeTLV(0xa2, nil)); err == nil {
		t.Fatal("wrong vas tag")
	}
	if _, err := UnmarshalWriteRequestParsed(berutil.EncodeTLV(tagVarListName, []byte{0xff})); err == nil {
		t.Fatal("bad list name")
	}
	if _, err := UnmarshalWriteRequestParsed(berutil.EncodeTLV(tagListOfVar, []byte{0xff})); err == nil {
		t.Fatal("bad var list")
	}

	name, _ := EncodeObjectName(ObjectNameWire{Scope: ScopeVMD, ItemID: "v"})
	entry := berutil.EncodeTLV(tagSequence, asn1util.WrapConstructed(0, name))
	list := berutil.EncodeTLV(tagListOfVar, entry)
	if _, err := UnmarshalWriteRequestParsed(list); err == nil {
		t.Fatal("missing data")
	}
	if _, err := UnmarshalWriteRequestParsed(append(list, 0xa0, 0x05)); err == nil {
		t.Fatal("data TLV")
	}
	if _, err := UnmarshalWriteRequestParsed(append(list, berutil.EncodeTLV(0xa1, nil)...)); err == nil {
		t.Fatal("wrong data tag")
	}
	data := berutil.EncodeTLV(tagWriteListOfData, berutil.EncodeTLV(TagDataInteger, []byte{1}))
	if _, err := UnmarshalWriteRequestParsed(append(append(list, data...), 0x00)); err == nil {
		t.Fatal("trailing")
	}
	if _, err := UnmarshalWriteRequestParsed(append(list, berutil.EncodeTLV(tagWriteListOfData, []byte{0xff})...)); err == nil {
		t.Fatal("bad data element")
	}
	// Count mismatch: two vars, one value.
	list2 := berutil.EncodeTLV(tagListOfVar, append(entry, entry...))
	if _, err := UnmarshalWriteRequestParsed(append(list2, data...)); err == nil {
		t.Fatal("count mismatch")
	}
}

func TestMarshalReadResponse_Errors(t *testing.T) {
	if _, err := MarshalReadResponse([]*AccessResult{{Data: &DataValue{Tag: 0xFF}}}); err == nil {
		t.Fatal("bad data")
	}
	if _, err := MarshalReadResponseWithSpec(&ObjectNameWire{Scope: 99, ItemID: "L"}, nil, nil); err == nil {
		t.Fatal("bad list name")
	}
	if _, err := MarshalReadResponseWithSpec(nil, []VariableSpecWire{{Name: ObjectNameWire{Scope: 99, ItemID: "v"}}}, nil); err == nil {
		t.Fatal("bad var name")
	}
	if _, err := MarshalReadResponseWithSpec(nil, []VariableSpecWire{{
		Name:            ObjectNameWire{Scope: ScopeVMD, ItemID: "v"},
		AlternateAccess: []AccessSelectorWire{{}},
	}}, nil); err == nil {
		t.Fatal("bad AA")
	}
	if _, err := MarshalReadResponseWithSpec(nil, nil, []*AccessResult{{Data: &DataValue{Tag: 0xFF}}}); err == nil {
		t.Fatal("bad result data")
	}
	// Success with AA.
	b, err := MarshalReadResponseWithSpec(nil, []VariableSpecWire{{
		Name:            ObjectNameWire{Scope: ScopeVMD, ItemID: "v"},
		AlternateAccess: []AccessSelectorWire{{Component: "c"}},
	}}, []*AccessResult{{Data: &DataValue{Tag: TagDataInteger, Int: 1}}})
	if err != nil || len(b) == 0 {
		t.Fatalf("%v %v", b, err)
	}
}

func TestMarshalGetVarAccessAndEncodeTypeSpec_Errors(t *testing.T) {
	if _, err := MarshalGetVarAccessResponse(true, TypeSpecWire{Tag: 99}); err == nil {
		t.Fatal("bad typespec")
	}
	if _, err := EncodeTypeSpec(TypeSpecWire{Tag: 99}); err == nil {
		t.Fatal("unsupported tag")
	}
	if _, err := EncodeTypeSpec(TypeSpecWire{Tag: tsTagTypeName}); err == nil {
		t.Fatal("nil typeName")
	}
	if _, err := EncodeTypeSpec(TypeSpecWire{Tag: tsTagTypeName, TypeName: &ObjectNameWire{Scope: 99, ItemID: "t"}}); err == nil {
		t.Fatal("bad typeName")
	}
	badElem := TypeSpecWire{Tag: 99}
	if _, err := EncodeTypeSpec(TypeSpecWire{Tag: tsTagArray, Count: 1, Element: &badElem}); err == nil {
		t.Fatal("bad array element")
	}
	if _, err := EncodeTypeSpec(TypeSpecWire{
		Tag:        tsTagStructure,
		Components: []StructComponentWire{{Name: "x", Type: TypeSpecWire{Tag: 99}}},
	}); err == nil {
		t.Fatal("bad structure component")
	}
	// Named type success.
	b, err := EncodeTypeSpec(TypeSpecWire{Tag: tsTagTypeName, TypeName: &ObjectNameWire{Scope: ScopeVMD, ItemID: "T"}})
	if err != nil || len(b) == 0 {
		t.Fatal(err)
	}
}

func TestUnmarshalDefineNVLAndGetAttrs_Errors(t *testing.T) {
	if _, err := UnmarshalDefineNVLRequest([]byte{0xff}); err == nil {
		t.Fatal("listName")
	}
	name, _ := EncodeObjectName(ObjectNameWire{Scope: ScopeVMD, ItemID: "L"})
	if _, err := UnmarshalDefineNVLRequest(name); err == nil {
		t.Fatal("missing list")
	}
	if _, err := UnmarshalDefineNVLRequest(append(name, 0xa0, 0x05)); err == nil {
		t.Fatal("list TLV")
	}
	if _, err := UnmarshalDefineNVLRequest(append(name, berutil.EncodeTLV(0xa1, nil)...)); err == nil {
		t.Fatal("wrong list tag")
	}
	if _, err := UnmarshalDefineNVLRequest(append(append(name, berutil.EncodeTLV(0xa0, nil)...), 0x00)); err == nil {
		t.Fatal("trailing")
	}
	if _, err := UnmarshalDefineNVLRequest(append(name, berutil.EncodeTLV(0xa0, []byte{0xff})...)); err == nil {
		t.Fatal("bad var list")
	}

	if _, err := decodeDefineNVLVarList([]byte{0xff}); err == nil {
		t.Fatal("TLV")
	}
	if _, err := decodeDefineNVLVarList(berutil.EncodeTLV(0xa0, nil)); err == nil {
		t.Fatal("not seq")
	}
	if _, err := decodeDefineNVLVarList(berutil.EncodeTLV(tagSequence, []byte{0xff})); err == nil {
		t.Fatal("bad spec")
	}

	if _, err := UnmarshalGetNVLAttrsRequest([]byte{0xff}); err == nil {
		t.Fatal("bad name")
	}
	if _, err := UnmarshalGetNVLAttrsRequest(append(name, 0x00)); err == nil {
		t.Fatal("trailing")
	}

	if _, err := MarshalGetNVLAttrsResponse(true, []VariableSpecWire{{Name: ObjectNameWire{Scope: 99, ItemID: "v"}}}); err == nil {
		t.Fatal("bad var name")
	}
	if _, err := MarshalGetNVLAttrsResponse(false, []VariableSpecWire{{
		Name:            ObjectNameWire{Scope: ScopeVMD, ItemID: "v"},
		AlternateAccess: []AccessSelectorWire{{}},
	}}); err == nil {
		t.Fatal("bad AA")
	}
	ok, err := MarshalGetNVLAttrsResponse(true, []VariableSpecWire{{
		Name:            ObjectNameWire{Scope: ScopeVMD, ItemID: "v"},
		AlternateAccess: []AccessSelectorWire{{Component: "c"}},
	}})
	if err != nil || len(ok) == 0 {
		t.Fatal(err)
	}
}

func TestUnmarshalDeleteNVLRequest_Errors(t *testing.T) {
	if _, err := UnmarshalDeleteNVLRequest([]byte{0x80, 0x05}); err == nil {
		t.Fatal("scope TLV")
	}
	if _, err := UnmarshalDeleteNVLRequest(berutil.EncodeTLV(0x80, nil)); err == nil {
		// empty int may fail value decode OR missing list — either is fine as error
		t.Fatal("expected error")
	}
	// Bad scope integer then still need list — use empty INTEGER which DecodeInteger rejects.
	scopeBad := berutil.EncodeTLV(0x80, nil)
	if _, err := UnmarshalDeleteNVLRequest(scopeBad); err == nil {
		t.Fatal("scope value / missing list")
	}

	if _, err := UnmarshalDeleteNVLRequest(nil); err == nil {
		t.Fatal("missing list")
	}
	if _, err := UnmarshalDeleteNVLRequest([]byte{0xa1, 0x05}); err == nil {
		t.Fatal("list TLV")
	}
	// Scope parsed, then truncated list TLV.
	if _, err := UnmarshalDeleteNVLRequest(append(berutil.EncodeTLV(0x80, []byte{0}), 0xa1, 0x05)); err == nil {
		t.Fatal("list TLV after scope")
	}
	if _, err := UnmarshalDeleteNVLRequest(berutil.EncodeTLV(0xa2, nil)); err == nil {
		t.Fatal("wrong list tag")
	}
	if _, err := UnmarshalDeleteNVLRequest(berutil.EncodeTLV(0xa1, []byte{0xff})); err == nil {
		t.Fatal("bad name")
	}
	list := berutil.EncodeTLV(0xa1, nil)
	if _, err := UnmarshalDeleteNVLRequest(append(list, 0x82, 0x05)); err == nil {
		t.Fatal("domain TLV")
	}
	// Unknown trailing tag after list.
	if _, err := UnmarshalDeleteNVLRequest(append(list, berutil.EncodeTLV(0x83, []byte("x"))...)); err == nil {
		t.Fatal("trailing")
	}
	// Valid domain scope body.
	body := append(berutil.EncodeTLV(0x80, []byte{2}), list...)
	body = append(body, berutil.EncodeTLV(0x82, []byte("dom"))...)
	req, err := UnmarshalDeleteNVLRequest(body)
	if err != nil || req.ScopeOfDelete != 2 || req.DomainName != "dom" {
		t.Fatalf("%+v err=%v", req, err)
	}
}

func TestMarshalDeleteNVLResponse_Errors(t *testing.T) {
	if _, err := MarshalDeleteNVLResponse(-1, 0); err == nil {
		t.Fatal("negative")
	}
	if _, err := MarshalDeleteNVLResponse(0, int(math.MaxUint32)+1); err == nil {
		t.Fatal("overflow")
	}
}
