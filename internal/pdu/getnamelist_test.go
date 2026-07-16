// SPDX-License-Identifier: MIT

package pdu

import (
	"encoding/asn1"
	"testing"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
)

func TestMarshalGetNameListRequestVMD(t *testing.T) {
	b, err := MarshalGetNameListRequest(1, 0, ScopeVMD, "", "")
	if err != nil {
		t.Fatalf("MarshalGetNameListRequest: %v", err)
	}
	if b[0] != asn1util.TagConfirmedRequest {
		t.Fatalf("outer tag = 0x%02x, want 0x%02x", b[0], asn1util.TagConfirmedRequest)
	}
}

func TestMarshalGetNameListRequestDomain(t *testing.T) {
	b, err := MarshalGetNameListRequest(2, 0, ScopeDomain, "TestDomain", "")
	if err != nil {
		t.Fatalf("MarshalGetNameListRequest: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("empty request")
	}
}

func TestMarshalGetNameListRequestWithContinuation(t *testing.T) {
	b, err := MarshalGetNameListRequest(3, 0, ScopeDomain, "TestDomain", "LastName")
	if err != nil {
		t.Fatalf("MarshalGetNameListRequest: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("empty request")
	}
}

func TestUnmarshalGetNameListResponse(t *testing.T) {
	// Build: listOfIdentifier [0] { "Name1", "Name2" }, moreFollows [1] FALSE
	n1 := berutil.EncodeTLV(tagVisibleString, []byte("Name1"))
	n2 := berutil.EncodeTLV(tagVisibleString, []byte("Name2"))
	list := berutil.EncodeTLV(0xa0, append(n1, n2...))
	moreFalse := berutil.EncodeTLV(0x81, []byte{0x00})

	content := append(list, moreFalse...)
	serviceData := asn1.RawValue{
		Class:      asn1.ClassContextSpecific,
		Tag:        1,
		IsCompound: true,
		Bytes:      content,
	}

	result, err := UnmarshalGetNameListResponse(serviceData)
	if err != nil {
		t.Fatalf("UnmarshalGetNameListResponse: %v", err)
	}
	if len(result.Names) != 2 {
		t.Fatalf("names = %d, want 2", len(result.Names))
	}
	if result.Names[0] != "Name1" || result.Names[1] != "Name2" {
		t.Errorf("names = %v, want [Name1, Name2]", result.Names)
	}
	if result.MoreFollows {
		t.Error("moreFollows should be false")
	}
}

func TestUnmarshalGetNameListResponseMoreFollows(t *testing.T) {
	// Build: listOfIdentifier [0] { "A" }, no moreFollows (defaults to TRUE)
	list := berutil.EncodeTLV(0xa0, berutil.EncodeTLV(tagVisibleString, []byte("A")))
	serviceData := asn1.RawValue{
		Class:      asn1.ClassContextSpecific,
		Tag:        1,
		IsCompound: true,
		Bytes:      list,
	}

	result, err := UnmarshalGetNameListResponse(serviceData)
	if err != nil {
		t.Fatalf("UnmarshalGetNameListResponse: %v", err)
	}
	if !result.MoreFollows {
		t.Error("moreFollows should default to true when absent")
	}
}

func TestDecodeIdentifierList_TooLong(t *testing.T) {
	longID := make([]byte, maxIdentifierLen+1)
	for i := range longID {
		longID[i] = 'x'
	}
	data := berutil.EncodeTLV(tagVisibleString, longID)
	_, err := decodeIdentifierList(data)
	if err == nil {
		t.Fatal("expected error for identifier exceeding length limit")
	}
}

func TestDecodeIdentifierList_TooMany(t *testing.T) {
	single := berutil.EncodeTLV(tagVisibleString, []byte("n"))
	var data []byte
	for i := 0; i < maxIdentifiers+1; i++ {
		data = append(data, single...)
	}
	_, err := decodeIdentifierList(data)
	if err == nil {
		t.Fatal("expected error for too many identifiers")
	}
}

func TestUnmarshalGetNameListResponseEmpty(t *testing.T) {
	list := berutil.EncodeTLV(0xa0, nil)
	moreFalse := berutil.EncodeTLV(0x81, []byte{0x00})

	serviceData := asn1.RawValue{
		Class:      asn1.ClassContextSpecific,
		Tag:        1,
		IsCompound: true,
		Bytes:      append(list, moreFalse...),
	}

	result, err := UnmarshalGetNameListResponse(serviceData)
	if err != nil {
		t.Fatalf("UnmarshalGetNameListResponse: %v", err)
	}
	if len(result.Names) != 0 {
		t.Errorf("names = %d, want 0", len(result.Names))
	}
}
