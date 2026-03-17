package pdu

import (
	"testing"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
)

// --- helpers ---

// extractServiceBody builds a confirmed request PDU and returns
// just the service body bytes (stripping the outer PDU and invoke ID).
func extractServiceBody(t *testing.T, pdu []byte) []byte {
	t.Helper()
	_, content, err := codec.UnwrapPdu(pdu)
	if err != nil {
		t.Fatalf("unwrap PDU: %v", err)
	}
	_, svc, err := codec.UnmarshalConfirmedRequest(content)
	if err != nil {
		t.Fatalf("unmarshal confirmed request: %v", err)
	}
	return svc.Bytes
}

// --- GetNameList roundtrip ---

func TestGetNameListRequestRoundtrip_VMD(t *testing.T) {
	pdu, err := MarshalGetNameListRequest(1, 0, ScopeVMD, "", "")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := extractServiceBody(t, pdu)

	req, err := UnmarshalGetNameListRequest(body)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.ObjectClass != 0 {
		t.Errorf("ObjectClass = %d, want 0", req.ObjectClass)
	}
	if req.Scope != ScopeVMD {
		t.Errorf("Scope = %d, want %d", req.Scope, ScopeVMD)
	}
	if req.DomainID != "" {
		t.Errorf("DomainID = %q, want empty", req.DomainID)
	}
	if req.ContinueAfter != "" {
		t.Errorf("ContinueAfter = %q, want empty", req.ContinueAfter)
	}
}

func TestGetNameListRequestRoundtrip_Domain(t *testing.T) {
	pdu, err := MarshalGetNameListRequest(2, 0, ScopeDomain, "MyDomain", "")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := extractServiceBody(t, pdu)

	req, err := UnmarshalGetNameListRequest(body)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.Scope != ScopeDomain {
		t.Errorf("Scope = %d, want %d", req.Scope, ScopeDomain)
	}
	if req.DomainID != "MyDomain" {
		t.Errorf("DomainID = %q, want %q", req.DomainID, "MyDomain")
	}
}

func TestGetNameListRequestRoundtrip_AA(t *testing.T) {
	pdu, err := MarshalGetNameListRequest(3, 0, ScopeAssociation, "", "")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := extractServiceBody(t, pdu)

	req, err := UnmarshalGetNameListRequest(body)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.Scope != ScopeAssociation {
		t.Errorf("Scope = %d, want %d", req.Scope, ScopeAssociation)
	}
}

func TestGetNameListRequestRoundtrip_ContinueAfter(t *testing.T) {
	pdu, err := MarshalGetNameListRequest(4, 0, ScopeDomain, "D1", "LastItem")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := extractServiceBody(t, pdu)

	req, err := UnmarshalGetNameListRequest(body)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.ContinueAfter != "LastItem" {
		t.Errorf("ContinueAfter = %q, want %q", req.ContinueAfter, "LastItem")
	}
}

func TestMarshalGetNameListResponse(t *testing.T) {
	data, err := MarshalGetNameListResponse([]string{"Name1", "Name2", "Name3"}, false)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty output")
	}

	// Decode the list
	tag, listContent, n, err := berutil.DecodeTLVAt(data, 0)
	if err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if tag != 0xa0 {
		t.Fatalf("list tag = 0x%02x, want 0xa0", tag)
	}

	var names []string
	off := 0
	for off < len(listContent) {
		tag, content, nn, err := berutil.DecodeTLVAt(listContent, off)
		if err != nil {
			t.Fatalf("decode name: %v", err)
		}
		off += nn
		if tag != tagVisibleString {
			t.Fatalf("name tag = 0x%02x, want 0x%02x", tag, tagVisibleString)
		}
		names = append(names, string(content))
	}
	if len(names) != 3 || names[0] != "Name1" || names[1] != "Name2" || names[2] != "Name3" {
		t.Errorf("names = %v, want [Name1, Name2, Name3]", names)
	}

	// Check moreFollows = false
	tag, content, _, err := berutil.DecodeTLVAt(data, n)
	if err != nil {
		t.Fatalf("decode moreFollows: %v", err)
	}
	if tag != 0x81 {
		t.Fatalf("moreFollows tag = 0x%02x, want 0x81", tag)
	}
	if len(content) != 1 || content[0] != 0x00 {
		t.Errorf("moreFollows = %v, want [0x00]", content)
	}
}

func TestMarshalGetNameListResponse_MoreFollowsTrue(t *testing.T) {
	data, err := MarshalGetNameListResponse([]string{"A"}, true)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// When moreFollows=true, the field should be omitted (DEFAULT TRUE)
	_, _, n, err := berutil.DecodeTLVAt(data, 0)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if n != len(data) {
		t.Errorf("expected no moreFollows field when true; got %d trailing bytes", len(data)-n)
	}
}

// --- GetVarAccess roundtrip ---

func TestGetVarAccessRequestRoundtrip(t *testing.T) {
	name := ObjectNameWire{Scope: ScopeDomain, DomainID: "dom1", ItemID: "var1"}
	pdu, err := MarshalGetVarAccessRequest(1, name)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := extractServiceBody(t, pdu)

	got, err := UnmarshalGetVarAccessRequest(body)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Scope != ScopeDomain || got.DomainID != "dom1" || got.ItemID != "var1" {
		t.Errorf("got %+v, want domain dom1/var1", got)
	}
}

func TestGetVarAccessRequestRoundtrip_VMD(t *testing.T) {
	name := ObjectNameWire{Scope: ScopeVMD, ItemID: "vmdVar"}
	pdu, err := MarshalGetVarAccessRequest(2, name)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := extractServiceBody(t, pdu)

	got, err := UnmarshalGetVarAccessRequest(body)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Scope != ScopeVMD || got.ItemID != "vmdVar" {
		t.Errorf("got %+v, want VMD vmdVar", got)
	}
}

func TestMarshalGetVarAccessResponse(t *testing.T) {
	ts := TypeSpecWire{Tag: tsTagBoolean}
	data, err := MarshalGetVarAccessResponse(true, ts)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty output")
	}

	// Verify deletable [0]
	tag, content, n, err := berutil.DecodeTLVAt(data, 0)
	if err != nil {
		t.Fatalf("decode deletable: %v", err)
	}
	if tag != 0x80 {
		t.Fatalf("deletable tag = 0x%02x, want 0x80", tag)
	}
	if len(content) != 1 || content[0] != 0xff {
		t.Errorf("deletable = 0x%02x, want 0xff", content[0])
	}

	// Verify typeSpecification [2]
	tag, _, _, err = berutil.DecodeTLVAt(data, n)
	if err != nil {
		t.Fatalf("decode typeSpec: %v", err)
	}
	if tag != 0xa2 {
		t.Fatalf("typeSpec tag = 0x%02x, want 0xa2", tag)
	}
}

// --- Read request roundtrip ---

func TestReadRequestRoundtrip(t *testing.T) {
	vars := []ObjectNameWire{
		{Scope: ScopeDomain, DomainID: "D1", ItemID: "V1"},
		{Scope: ScopeDomain, DomainID: "D1", ItemID: "V2"},
	}
	pdu, err := MarshalReadRequest(1, vars)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := extractServiceBody(t, pdu)

	got, err := UnmarshalReadRequest(body)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].DomainID != "D1" || got[0].ItemID != "V1" {
		t.Errorf("got[0] = %+v, want D1/V1", got[0])
	}
	if got[1].DomainID != "D1" || got[1].ItemID != "V2" {
		t.Errorf("got[1] = %+v, want D1/V2", got[1])
	}
}

// --- Write request roundtrip ---

func TestWriteRequestRoundtrip(t *testing.T) {
	vars := []ObjectNameWire{
		{Scope: ScopeDomain, DomainID: "D1", ItemID: "V1"},
	}
	values := []*DataValue{{Tag: TagDataInteger, Int: 42}}
	pdu, err := MarshalWriteRequest(1, vars, values)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := extractServiceBody(t, pdu)

	gotVars, gotValues, err := UnmarshalWriteRequest(body)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(gotVars) != 1 {
		t.Fatalf("vars = %d, want 1", len(gotVars))
	}
	if gotVars[0].DomainID != "D1" || gotVars[0].ItemID != "V1" {
		t.Errorf("var = %+v, want D1/V1", gotVars[0])
	}
	if len(gotValues) != 1 {
		t.Fatalf("values = %d, want 1", len(gotValues))
	}
	if gotValues[0].Tag != TagDataInteger || gotValues[0].Int != 42 {
		t.Errorf("value = %+v, want int 42", gotValues[0])
	}
}

func TestWriteRequestRoundtrip_Multiple(t *testing.T) {
	vars := []ObjectNameWire{
		{Scope: ScopeDomain, DomainID: "D", ItemID: "A"},
		{Scope: ScopeDomain, DomainID: "D", ItemID: "B"},
	}
	values := []*DataValue{
		{Tag: TagDataBoolean, Bool: true},
		{Tag: TagDataInteger, Int: -7},
	}
	pdu, err := MarshalWriteRequest(2, vars, values)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := extractServiceBody(t, pdu)

	gotVars, gotValues, err := UnmarshalWriteRequest(body)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(gotVars) != 2 {
		t.Fatalf("vars = %d, want 2", len(gotVars))
	}
	if len(gotValues) != 2 {
		t.Fatalf("values = %d, want 2", len(gotValues))
	}
	if !gotValues[0].Bool {
		t.Error("values[0] should be true")
	}
	if gotValues[1].Int != -7 {
		t.Errorf("values[1].Int = %d, want -7", gotValues[1].Int)
	}
}

// --- MarshalReadResponse ---

func TestMarshalReadResponse(t *testing.T) {
	results := []*AccessResult{
		{IsError: false, Data: &DataValue{Tag: TagDataBoolean, Bool: true}},
		{IsError: true, ErrorCode: 1},
	}
	data, err := MarshalReadResponse(results)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty output")
	}

	// Should be a SEQUENCE
	tag, _, _, err := berutil.DecodeTLVAt(data, 0)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tag != tagSequence {
		t.Fatalf("tag = 0x%02x, want 0x%02x", tag, tagSequence)
	}
}

func TestMarshalReadResponse_Empty(t *testing.T) {
	data, err := MarshalReadResponse(nil)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty (empty SEQUENCE)")
	}
}

// --- MarshalWriteResponse ---

func TestMarshalWriteResponse(t *testing.T) {
	results := []int{0, 1, 0, 2}
	data, err := MarshalWriteResponse(results)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty output")
	}

	offset := 0
	for i, code := range results {
		tag, _, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			t.Fatalf("result[%d]: %v", i, err)
		}
		offset += n
		if code == 0 {
			if tag != 0x81 {
				t.Errorf("result[%d]: tag = 0x%02x, want 0x81 (success)", i, tag)
			}
		} else {
			if tag != 0x80 {
				t.Errorf("result[%d]: tag = 0x%02x, want 0x80 (failure)", i, tag)
			}
		}
	}
	if offset != len(data) {
		t.Errorf("%d trailing bytes", len(data)-offset)
	}
}

func TestMarshalWriteResponse_AllSuccess(t *testing.T) {
	data, err := MarshalWriteResponse([]int{0, 0, 0})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty output")
	}
}

// --- EncodeTypeSpec ---

func TestEncodeTypeSpec(t *testing.T) {
	tests := []struct {
		name string
		ts   TypeSpecWire
	}{
		{"Boolean", TypeSpecWire{Tag: tsTagBoolean}},
		{"BitString", TypeSpecWire{Tag: tsTagBitString, Size: 32}},
		{"Integer", TypeSpecWire{Tag: tsTagInteger, Size: 32}},
		{"Unsigned", TypeSpecWire{Tag: tsTagUnsigned, Size: 16}},
		{"Float", TypeSpecWire{Tag: tsTagFloat, FormatWidth: 32, ExpWidth: 8}},
		{"OctetString", TypeSpecWire{Tag: tsTagOctetString, Size: 255}},
		{"VisibleString", TypeSpecWire{Tag: tsTagVisibleString, Size: 64}},
		{"MmsString", TypeSpecWire{Tag: tsTagMmsString, Size: 128}},
		{"UTCTime", TypeSpecWire{Tag: tsTagUTCTime}},
		{"BinaryTime_Short", TypeSpecWire{Tag: tsTagBinaryTime, BinTimeFull: false}},
		{"BinaryTime_Full", TypeSpecWire{Tag: tsTagBinaryTime, BinTimeFull: true}},
		{"Array", TypeSpecWire{
			Tag:     tsTagArray,
			Count:   5,
			Element: &TypeSpecWire{Tag: tsTagInteger, Size: 8},
		}},
		{"Structure", TypeSpecWire{
			Tag: tsTagStructure,
			Components: []StructComponentWire{
				{Name: "field1", Type: TypeSpecWire{Tag: tsTagBoolean}},
				{Name: "field2", Type: TypeSpecWire{Tag: tsTagInteger, Size: 32}},
			},
		}},
		{"TypeName", TypeSpecWire{
			Tag:      tsTagTypeName,
			TypeName: &ObjectNameWire{Scope: ScopeDomain, DomainID: "D", ItemID: "MyType"},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := EncodeTypeSpec(tt.ts)
			if err != nil {
				t.Fatalf("EncodeTypeSpec: %v", err)
			}
			if len(data) == 0 {
				t.Fatal("expected non-empty output")
			}
			// Verify valid BER
			_, _, _, err = berutil.DecodeTLVAt(data, 0)
			if err != nil {
				t.Fatalf("invalid BER: %v", err)
			}
		})
	}
}

func TestEncodeTypeSpec_UnsupportedTag(t *testing.T) {
	_, err := EncodeTypeSpec(TypeSpecWire{Tag: 999})
	if err == nil {
		t.Fatal("expected error for unsupported tag")
	}
}

func TestEncodeTypeSpec_TypeNameNil(t *testing.T) {
	_, err := EncodeTypeSpec(TypeSpecWire{Tag: tsTagTypeName, TypeName: nil})
	if err == nil {
		t.Fatal("expected error for nil TypeName")
	}
}

// --- DefineNVL roundtrip ---

func TestDefineNVLRequestRoundtrip(t *testing.T) {
	listName := ObjectNameWire{Scope: ScopeDomain, DomainID: "D1", ItemID: "NVL1"}
	vars := []VariableSpecWire{
		{Name: ObjectNameWire{Scope: ScopeDomain, DomainID: "D1", ItemID: "V1"}},
		{Name: ObjectNameWire{Scope: ScopeDomain, DomainID: "D1", ItemID: "V2"}},
	}

	pdu, err := MarshalDefineNamedVarListRequest(1, listName, vars)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := extractServiceBody(t, pdu)

	req, err := UnmarshalDefineNVLRequest(body)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.ListName.DomainID != "D1" || req.ListName.ItemID != "NVL1" {
		t.Errorf("ListName = %+v, want D1/NVL1", req.ListName)
	}
	if len(req.Variables) != 2 {
		t.Fatalf("Variables = %d, want 2", len(req.Variables))
	}
	if req.Variables[0].Name.ItemID != "V1" {
		t.Errorf("Variables[0] = %+v, want V1", req.Variables[0].Name)
	}
	if req.Variables[1].Name.ItemID != "V2" {
		t.Errorf("Variables[1] = %+v, want V2", req.Variables[1].Name)
	}
}

func TestDefineNVLRequestRoundtrip_AAScope(t *testing.T) {
	listName := ObjectNameWire{Scope: ScopeAssociation, ItemID: "AAList"}
	vars := []VariableSpecWire{
		{Name: ObjectNameWire{Scope: ScopeDomain, DomainID: "D", ItemID: "X"}},
	}

	pdu, err := MarshalDefineNamedVarListRequest(2, listName, vars)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := extractServiceBody(t, pdu)

	req, err := UnmarshalDefineNVLRequest(body)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.ListName.Scope != ScopeAssociation || req.ListName.ItemID != "AAList" {
		t.Errorf("ListName = %+v, want AA/AAList", req.ListName)
	}
}

// --- GetNVLAttrs roundtrip ---

func TestGetNVLAttrsRequestRoundtrip(t *testing.T) {
	listName := ObjectNameWire{Scope: ScopeDomain, DomainID: "D1", ItemID: "MyList"}
	pdu, err := MarshalGetNamedVarListAttrsRequest(1, listName)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := extractServiceBody(t, pdu)

	req, err := UnmarshalGetNVLAttrsRequest(body)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.ListName.DomainID != "D1" || req.ListName.ItemID != "MyList" {
		t.Errorf("ListName = %+v, want D1/MyList", req.ListName)
	}
}

func TestMarshalGetNVLAttrsResponse(t *testing.T) {
	vars := []VariableSpecWire{
		{Name: ObjectNameWire{Scope: ScopeDomain, DomainID: "D", ItemID: "V1"}},
		{Name: ObjectNameWire{Scope: ScopeDomain, DomainID: "D", ItemID: "V2"}},
	}
	data, err := MarshalGetNVLAttrsResponse(true, vars)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty output")
	}

	// Verify deletable = true
	tag, content, _, err := berutil.DecodeTLVAt(data, 0)
	if err != nil {
		t.Fatalf("decode deletable: %v", err)
	}
	if tag != 0x80 {
		t.Fatalf("tag = 0x%02x, want 0x80", tag)
	}
	if len(content) != 1 || content[0] != 0xff {
		t.Errorf("deletable = 0x%02x, want 0xff", content[0])
	}
}

func TestMarshalGetNVLAttrsResponse_NotDeletable(t *testing.T) {
	vars := []VariableSpecWire{
		{Name: ObjectNameWire{Scope: ScopeVMD, ItemID: "V1"}},
	}
	data, err := MarshalGetNVLAttrsResponse(false, vars)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	tag, content, _, err := berutil.DecodeTLVAt(data, 0)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tag != 0x80 || content[0] != 0x00 {
		t.Errorf("deletable = 0x%02x, want 0x00", content[0])
	}
}

// --- DeleteNVL roundtrip ---

func TestDeleteNVLRequestRoundtrip(t *testing.T) {
	names := []ObjectNameWire{
		{Scope: ScopeDomain, DomainID: "D", ItemID: "List1"},
		{Scope: ScopeDomain, DomainID: "D", ItemID: "List2"},
	}
	pdu, err := MarshalDeleteNamedVarListRequest(1, names)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := extractServiceBody(t, pdu)

	req, err := UnmarshalDeleteNVLRequest(body)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(req.ListNames) != 2 {
		t.Fatalf("ListNames = %d, want 2", len(req.ListNames))
	}
	if req.ListNames[0].ItemID != "List1" {
		t.Errorf("ListNames[0] = %+v, want List1", req.ListNames[0])
	}
	if req.ListNames[1].ItemID != "List2" {
		t.Errorf("ListNames[1] = %+v, want List2", req.ListNames[1])
	}
	if req.ScopeOfDelete != 0 {
		t.Errorf("ScopeOfDelete = %d, want 0", req.ScopeOfDelete)
	}
}

func TestMarshalDefineNVLResponse(t *testing.T) {
	data, err := MarshalDefineNVLResponse()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if data != nil {
		t.Errorf("expected nil body, got %d bytes", len(data))
	}
}

func TestMarshalDeleteNVLResponse(t *testing.T) {
	data, err := MarshalDeleteNVLResponse(3, 2)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty output")
	}

	// Decode numberMatched
	tag, content, n, err := berutil.DecodeTLVAt(data, 0)
	if err != nil {
		t.Fatalf("decode matched: %v", err)
	}
	if tag != 0x02 {
		t.Fatalf("matched tag = 0x%02x, want 0x02", tag)
	}
	matched, _ := berutil.DecodeInteger(content)
	if matched != 3 {
		t.Errorf("matched = %d, want 3", matched)
	}

	// Decode numberDeleted
	tag, content, _, err = berutil.DecodeTLVAt(data, n)
	if err != nil {
		t.Fatalf("decode deleted: %v", err)
	}
	if tag != 0x02 {
		t.Fatalf("deleted tag = 0x%02x, want 0x02", tag)
	}
	deleted, _ := berutil.DecodeInteger(content)
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}
}

// --- MarshalReadRequestByListName ---

func TestMarshalReadRequestByListName(t *testing.T) {
	name := ObjectNameWire{Scope: ScopeDomain, DomainID: "dom", ItemID: "list1"}
	data, err := MarshalReadRequestByListName(1, name)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty output")
	}
	if data[0] != asn1util.TagConfirmedRequest {
		t.Fatalf("outer tag = 0x%02x, want 0x%02x", data[0], asn1util.TagConfirmedRequest)
	}

	// Roundtrip: extract service body and parse as a read request with variableListName
	body := extractServiceBody(t, data)
	req, err := UnmarshalReadRequestParsed(body)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.ListName == nil {
		t.Fatal("expected ListName to be set")
	}
	if req.ListName.DomainID != "dom" || req.ListName.ItemID != "list1" {
		t.Errorf("ListName = %+v, want dom/list1", *req.ListName)
	}
}

func TestMarshalReadRequestByListNameWithSpec(t *testing.T) {
	name := ObjectNameWire{Scope: ScopeDomain, DomainID: "dom", ItemID: "list1"}
	data, err := MarshalReadRequestByListNameWithSpec(1, name, true)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty output")
	}

	body := extractServiceBody(t, data)
	req, err := UnmarshalReadRequestParsed(body)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !req.SpecWithResult {
		t.Error("expected SpecWithResult = true")
	}
	if req.ListName == nil {
		t.Fatal("expected ListName to be set")
	}
	if req.ListName.DomainID != "dom" || req.ListName.ItemID != "list1" {
		t.Errorf("ListName = %+v, want dom/list1", *req.ListName)
	}
}

func TestMarshalReadRequestByListNameWithSpec_False(t *testing.T) {
	name := ObjectNameWire{Scope: ScopeAssociation, ItemID: "aaList"}
	data, err := MarshalReadRequestByListNameWithSpec(2, name, false)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	body := extractServiceBody(t, data)
	req, err := UnmarshalReadRequestParsed(body)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.SpecWithResult {
		t.Error("expected SpecWithResult = false")
	}
	if req.ListName == nil {
		t.Fatal("expected ListName to be set")
	}
	if req.ListName.Scope != ScopeAssociation || req.ListName.ItemID != "aaList" {
		t.Errorf("ListName = %+v, want AA/aaList", *req.ListName)
	}
}

// --- MarshalWriteRequestByListName ---

func TestMarshalWriteRequestByListName(t *testing.T) {
	name := ObjectNameWire{Scope: ScopeDomain, DomainID: "dom", ItemID: "list1"}
	vals := []*DataValue{
		{Tag: TagDataBoolean, Bool: true},
		{Tag: TagDataInteger, Int: 99},
	}
	data, err := MarshalWriteRequestByListName(1, name, vals)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty output")
	}
	if data[0] != asn1util.TagConfirmedRequest {
		t.Fatalf("outer tag = 0x%02x, want 0x%02x", data[0], asn1util.TagConfirmedRequest)
	}

	// Roundtrip through the parsed write request
	body := extractServiceBody(t, data)
	req, err := UnmarshalWriteRequestParsed(body)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.ListName == nil {
		t.Fatal("expected ListName to be set")
	}
	if req.ListName.DomainID != "dom" || req.ListName.ItemID != "list1" {
		t.Errorf("ListName = %+v, want dom/list1", *req.ListName)
	}
	if len(req.Values) != 2 {
		t.Fatalf("values = %d, want 2", len(req.Values))
	}
	if !req.Values[0].Bool {
		t.Error("values[0] should be true")
	}
	if req.Values[1].Int != 99 {
		t.Errorf("values[1].Int = %d, want 99", req.Values[1].Int)
	}
}

// --- ExtractInvokeID ---

func TestExtractInvokeID(t *testing.T) {
	pdu, err := MarshalReadRequest(42, []ObjectNameWire{
		{Scope: ScopeVMD, ItemID: "x"},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	_, content, err := codec.UnwrapPdu(pdu)
	if err != nil {
		t.Fatalf("unwrap: %v", err)
	}

	id, err := ExtractInvokeID(content)
	if err != nil {
		t.Fatalf("ExtractInvokeID: %v", err)
	}
	if id != 42 {
		t.Errorf("invoke ID = %d, want 42", id)
	}
}

func TestExtractInvokeID_ContextTag(t *testing.T) {
	// Simulate a ConfirmedError or Reject PDU where the invoke ID uses
	// context tag [0] (0x80) instead of UNIVERSAL INTEGER (0x02).
	content := berutil.EncodeTLV(0x80, berutil.EncodeInt(7))
	id, err := ExtractInvokeID(content)
	if err != nil {
		t.Fatalf("ExtractInvokeID: %v", err)
	}
	if id != 7 {
		t.Errorf("invoke ID = %d, want 7", id)
	}
}

func TestExtractInvokeID_UnexpectedTag(t *testing.T) {
	content := berutil.EncodeTLV(0xa0, []byte{0x01})
	_, err := ExtractInvokeID(content)
	if err == nil {
		t.Fatal("expected error for unexpected tag")
	}
}

func TestExtractInvokeID_Empty(t *testing.T) {
	_, err := ExtractInvokeID(nil)
	if err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestExtractInvokeID_LargeID(t *testing.T) {
	pdu, err := MarshalReadRequest(65535, []ObjectNameWire{
		{Scope: ScopeVMD, ItemID: "y"},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	_, content, err := codec.UnwrapPdu(pdu)
	if err != nil {
		t.Fatalf("unwrap: %v", err)
	}

	id, err := ExtractInvokeID(content)
	if err != nil {
		t.Fatalf("ExtractInvokeID: %v", err)
	}
	if id != 65535 {
		t.Errorf("invoke ID = %d, want 65535", id)
	}
}

// --- ServiceKind.String ---

func TestServiceKindString(t *testing.T) {
	tests := []struct {
		kind ServiceKind
		want string
	}{
		{ServiceStatus, "Status"},
		{ServiceGetNameList, "GetNameList"},
		{ServiceIdentify, "Identify"},
		{ServiceRead, "Read"},
		{ServiceWrite, "Write"},
		{ServiceGetVariableAccessAttrs, "GetVariableAccessAttributes"},
		{ServiceDefineNamedVariableList, "DefineNamedVariableList"},
		{ServiceGetNamedVariableListAttrs, "GetNamedVariableListAttributes"},
		{ServiceDeleteNamedVariableList, "DeleteNamedVariableList"},
		{ServiceFileOpen, "FileOpen"},
		{ServiceFileRead, "FileRead"},
		{ServiceFileClose, "FileClose"},
		{ServiceFileRename, "FileRename"},
		{ServiceFileDelete, "FileDelete"},
		{ServiceFileDirectory, "FileDirectory"},
		{ServiceObtainFile, "ObtainFile"},
		{ServiceReadJournal, "ReadJournal"},
		{ServiceUnknown, "Unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.kind.String()
			if got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestServiceKindString_OutOfRange(t *testing.T) {
	got := ServiceKind(999).String()
	if got != "ServiceKind(999)" {
		t.Errorf("String() = %q, want %q", got, "ServiceKind(999)")
	}
}

// --- MarshalReadResponseWithSpec (already in existing tests; adding coverage variants) ---

func TestMarshalReadResponseWithSpec_ListName(t *testing.T) {
	listName := ObjectNameWire{Scope: ScopeDomain, DomainID: "dom1", ItemID: "nvl1"}
	results := []*AccessResult{
		{IsError: false, Data: &DataValue{Tag: TagDataBoolean, Bool: true}},
	}

	data, err := MarshalReadResponseWithSpec(&listName, nil, results)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty output")
	}
}

func TestMarshalReadResponseWithSpec_ListOfVariable(t *testing.T) {
	specs := []VariableSpecWire{
		{Name: ObjectNameWire{Scope: ScopeDomain, DomainID: "dom1", ItemID: "var1"}},
	}
	results := []*AccessResult{
		{IsError: false, Data: &DataValue{Tag: TagDataBoolean, Bool: true}},
	}

	data, err := MarshalReadResponseWithSpec(nil, specs, results)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty output")
	}
}

func TestMarshalReadResponseWithSpec_WithAlternateAccess(t *testing.T) {
	idx := 2
	specs := []VariableSpecWire{
		{
			Name: ObjectNameWire{Scope: ScopeDomain, DomainID: "dom1", ItemID: "arr1"},
			AlternateAccess: []AccessSelectorWire{
				{HasIndex: true, Index: idx},
			},
		},
	}
	results := []*AccessResult{
		{IsError: false, Data: &DataValue{Tag: TagDataInteger, Int: 42}},
	}

	data, err := MarshalReadResponseWithSpec(nil, specs, results)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty output")
	}
}

func TestMarshalReadResponseWithSpec_NoSpec(t *testing.T) {
	results := []*AccessResult{
		{IsError: false, Data: &DataValue{Tag: TagDataBoolean, Bool: false}},
	}

	data, err := MarshalReadResponseWithSpec(nil, nil, results)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty output for results-only response")
	}
}

func TestMarshalReadResponseWithSpec_ErrorResult(t *testing.T) {
	listName := ObjectNameWire{Scope: ScopeDomain, DomainID: "dom1", ItemID: "nvl1"}
	results := []*AccessResult{
		{IsError: true, ErrorCode: 2},
		{IsError: false, Data: &DataValue{Tag: TagDataInteger, Int: 100}},
	}

	data, err := MarshalReadResponseWithSpec(&listName, nil, results)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty output")
	}
}

func TestMarshalReadResponseWithSpec_MultipleVariables(t *testing.T) {
	specs := []VariableSpecWire{
		{Name: ObjectNameWire{Scope: ScopeDomain, DomainID: "dom1", ItemID: "var1"}},
		{Name: ObjectNameWire{Scope: ScopeDomain, DomainID: "dom1", ItemID: "var2"}},
		{Name: ObjectNameWire{Scope: ScopeDomain, DomainID: "dom1", ItemID: "var3"}},
	}
	results := []*AccessResult{
		{IsError: false, Data: &DataValue{Tag: TagDataBoolean, Bool: true}},
		{IsError: false, Data: &DataValue{Tag: TagDataInteger, Int: 42}},
		{IsError: true, ErrorCode: 1},
	}

	data, err := MarshalReadResponseWithSpec(nil, specs, results)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty output")
	}
}
