// SPDX-License-Identifier: MIT

package pdu

import (
	"encoding/asn1"
	"testing"

	"github.com/otfabric/go-mms/internal/berutil"
)

func TestDecodeObjectNameAt_Edges(t *testing.T) {
	if _, _, err := DecodeObjectNameAt([]byte{0x80, 0x05}, 0); err == nil {
		t.Fatal("truncated TLV")
	}
	if _, _, err := DecodeObjectNameAt([]byte{0xff, 0x00}, 0); err == nil {
		t.Fatal("bad tag")
	}
	name, n, err := DecodeObjectNameAt(append([]byte{0x00}, berutil.EncodeTLV(0x80, []byte("v"))...), 1)
	if err != nil || name.ItemID != "v" || n != 3 {
		t.Fatalf("got name=%+v n=%d err=%v", name, n, err)
	}
}

func TestDecodeDomainSpecificName_Edges(t *testing.T) {
	if _, err := decodeDomainSpecificName([]byte{0x02, 0x01, 0x01}); err == nil {
		t.Fatal("domainId wrong tag")
	}
	if _, err := decodeDomainSpecificName([]byte{0x1a, 0x05}); err == nil {
		t.Fatal("truncated after domainId")
	}
	badItem := append(berutil.EncodeTLV(tagVisibleString, []byte("D")), berutil.EncodeTLV(0x02, []byte{1})...)
	if _, err := decodeDomainSpecificName(badItem); err == nil {
		t.Fatal("itemId wrong tag")
	}
	trail := append(
		berutil.EncodeTLV(tagVisibleString, []byte("D")),
		berutil.EncodeTLV(tagVisibleString, []byte("I"))...,
	)
	trail = append(trail, 0x00)
	if _, err := decodeDomainSpecificName(trail); err == nil {
		t.Fatal("trailing bytes")
	}
}

func TestMarshalReadRequest_EncodeError(t *testing.T) {
	if _, err := MarshalReadRequest(1, []ObjectNameWire{{Scope: 99, ItemID: "x"}}); err == nil {
		t.Fatal("expected encode error")
	}
	if _, err := encodeListOfVariable([]ObjectNameWire{{Scope: 99, ItemID: "x"}}); err == nil {
		t.Fatal("encodeListOfVariable")
	}
}

func TestUnmarshalReadResponse_Edges(t *testing.T) {
	if _, err := UnmarshalReadResponse(asn1.RawValue{}); err == nil {
		t.Fatal("empty")
	}
	// Truncated optional variableAccessSpecification [0].
	if _, err := UnmarshalReadResponse(asn1.RawValue{Bytes: []byte{0xa0, 0x05}}); err == nil {
		t.Fatal("truncated varspec")
	}
	// Complete varspec, missing listOfAccessResult.
	varspec := berutil.EncodeTLV(0xa0, nil)
	if _, err := UnmarshalReadResponse(asn1.RawValue{Bytes: varspec}); err == nil {
		t.Fatal("missing list")
	}
	// Wrong list tag.
	if _, err := UnmarshalReadResponse(asn1.RawValue{Bytes: berutil.EncodeTLV(0xa2, nil)}); err == nil {
		t.Fatal("wrong list tag")
	}
	// Trailing bytes after list.
	list := berutil.EncodeTLV(0xa1, nil)
	junk := append(append([]byte{}, list...), 0x00)
	if _, err := UnmarshalReadResponse(asn1.RawValue{Bytes: junk}); err == nil {
		t.Fatal("trailing")
	}
	// Truncated list TLV.
	if _, err := UnmarshalReadResponse(asn1.RawValue{Bytes: []byte{0xa1, 0x05}}); err == nil {
		t.Fatal("truncated list")
	}
}

func TestMarshalReadWriteWithAccess_Edges(t *testing.T) {
	badName := VariableSpecWire{Name: ObjectNameWire{Scope: 99, ItemID: "x"}}
	if _, err := MarshalReadRequestWithAccess(1, []VariableSpecWire{badName}); err == nil {
		t.Fatal("read with access encode name")
	}
	emptyAA := VariableSpecWire{
		Name:            ObjectNameWire{Scope: ScopeVMD, ItemID: "v"},
		AlternateAccess: []AccessSelectorWire{{}},
	}
	if _, err := MarshalReadRequestWithAccess(1, []VariableSpecWire{emptyAA}); err == nil {
		t.Fatal("read with access empty AA")
	}
	if _, err := encodeListOfVariableWithAccess([]VariableSpecWire{badName}); err == nil {
		t.Fatal("encodeList name")
	}
	if _, err := encodeListOfVariableWithAccess([]VariableSpecWire{emptyAA}); err == nil {
		t.Fatal("encodeList AA")
	}

	ok := VariableSpecWire{Name: ObjectNameWire{Scope: ScopeVMD, ItemID: "v"}}
	if _, err := MarshalWriteRequestWithAccess(1, []VariableSpecWire{ok}, nil); err == nil {
		t.Fatal("count mismatch")
	}
	if _, err := MarshalWriteRequestWithAccess(1, []VariableSpecWire{badName}, []*DataValue{{Tag: TagDataInteger, Int: 1}}); err == nil {
		t.Fatal("write encode name")
	}
	if _, err := MarshalWriteRequestWithAccess(1, []VariableSpecWire{ok}, []*DataValue{{Tag: 0xfe}}); err == nil {
		t.Fatal("write marshal data")
	}
	if _, err := MarshalWriteRequestWithAccess(1, []VariableSpecWire{emptyAA}, []*DataValue{{Tag: TagDataInteger, Int: 1}}); err == nil {
		t.Fatal("write empty AA")
	}
}

func TestMarshalByListName_Edges(t *testing.T) {
	bad := ObjectNameWire{Scope: 99, ItemID: "L"}
	if _, err := MarshalReadRequestByListName(1, bad); err == nil {
		t.Fatal("read by list name")
	}
	if _, err := MarshalReadRequestByListNameWithSpec(1, bad, true); err == nil {
		t.Fatal("read by list name with spec")
	}
	if _, err := MarshalReadRequestByListNameWithSpec(1, bad, false); err == nil {
		t.Fatal("read by list name with spec false")
	}
	if _, err := MarshalWriteRequestByListName(1, bad, []*DataValue{{Tag: TagDataInteger, Int: 1}}); err == nil {
		t.Fatal("write by list name encode")
	}
	ok := ObjectNameWire{Scope: ScopeVMD, ItemID: "L"}
	if _, err := MarshalWriteRequestByListName(1, ok, []*DataValue{{Tag: 0xfe}}); err == nil {
		t.Fatal("write by list name data")
	}
}
