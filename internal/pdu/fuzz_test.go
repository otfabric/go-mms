package pdu

import (
	"encoding/asn1"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/berutil"
)

// FuzzDecodeTypeSpec fuzzes the TypeSpecification decoder with arbitrary bytes.
func FuzzDecodeTypeSpec(f *testing.F) {
	// boolean [3] 0
	f.Add(berutil.EncodeTLV(0x83, []byte{0}))
	// integer [5] 32 bits
	f.Add(berutil.EncodeTLV(0x85, []byte{32}))
	// float [7] SEQUENCE { formatWidth=32, exponentWidth=8 }
	floatInner := append(berutil.EncodeTLV(0x02, []byte{32}), berutil.EncodeTLV(0x02, []byte{8})...)
	f.Add(berutil.EncodeTLV(0xa7, floatInner))
	// array [1] CONSTRUCTED { count + elementType }
	arrayInner := append(berutil.EncodeTLV(0x80, []byte{10}), berutil.EncodeTLV(0x83, []byte{0})...)
	f.Add(berutil.EncodeTLV(0xa1, arrayInner))
	f.Add([]byte{})
	f.Add([]byte{0xff})
	f.Add([]byte{0x83}) // truncated

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = DecodeTypeSpec(data)
	})
}

// FuzzDecodeObjectName fuzzes the ObjectName decoder.
func FuzzDecodeObjectName(f *testing.F) {
	vmd, _ := EncodeObjectName(ObjectNameWire{Scope: ScopeVMD, ItemID: "test"})
	dom, _ := EncodeObjectName(ObjectNameWire{Scope: ScopeDomain, DomainID: "d", ItemID: "v"})
	aa, _ := EncodeObjectName(ObjectNameWire{Scope: ScopeAssociation, ItemID: "aa"})
	f.Add(vmd)
	f.Add(dom)
	f.Add(aa)
	f.Add([]byte{})
	f.Add([]byte{0xff, 0x00})

	f.Fuzz(func(t *testing.T, data []byte) {
		name, err := DecodeObjectName(data)
		if err != nil {
			return
		}
		reEncoded, err := EncodeObjectName(name)
		if err != nil {
			t.Fatalf("re-encode failed: %v", err)
		}
		name2, err := DecodeObjectName(reEncoded)
		if err != nil {
			t.Fatalf("round-trip decode failed: %v", err)
		}
		if name2.Scope != name.Scope || name2.DomainID != name.DomainID || name2.ItemID != name.ItemID {
			t.Fatalf("round-trip mismatch: %+v vs %+v", name, name2)
		}
	})
}

// FuzzUnmarshalDataElement fuzzes the MMS Data element decoder.
func FuzzUnmarshalDataElement(f *testing.F) {
	// boolean true
	f.Add(berutil.EncodeTLV(0x83, []byte{0xff}))
	// integer 42
	f.Add(berutil.EncodeTLV(0x85, []byte{42}))
	// unsigned 100
	f.Add(berutil.EncodeTLV(0x86, []byte{100}))
	// float32
	f.Add(berutil.EncodeTLV(0x87, []byte{8, 0x42, 0x28, 0x00, 0x00}))
	// visible string
	f.Add(berutil.EncodeTLV(0x8a, []byte("hello")))
	// octet string
	f.Add(berutil.EncodeTLV(0x89, []byte{0xde, 0xad}))
	// bit string: unused=2, data=0xfc
	f.Add(berutil.EncodeTLV(0x84, []byte{2, 0xfc}))
	// empty
	f.Add([]byte{})
	// truncated
	f.Add([]byte{0x85, 0x10})

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = UnmarshalDataElement(data, 0)
	})
}

// FuzzUnmarshalAccessResults fuzzes the AccessResult list decoder.
func FuzzUnmarshalAccessResults(f *testing.F) {
	bool1 := berutil.EncodeTLV(0x83, []byte{0xff})
	int1 := berutil.EncodeTLV(0x85, []byte{42})
	f.Add(append(bool1, int1...))
	f.Add([]byte{})
	f.Add([]byte{0x83, 0x01, 0xff, 0x85}) // truncated second element

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = UnmarshalAccessResults(data)
	})
}

// FuzzDecodePdu fuzzes the top-level PDU classifier/decoder.
func FuzzDecodePdu(f *testing.F) {
	f.Add([]byte{0xa1, 0x00}) // ConfirmedResponse, empty
	f.Add([]byte{0xa0, 0x00}) // ConfirmedRequest, empty
	f.Add([]byte{0xa2, 0x00}) // ConfirmedError, empty
	f.Add([]byte{0xa4, 0x00}) // Reject, empty
	f.Add([]byte{0xa8, 0x00}) // InitiateRequest
	f.Add([]byte{0xa9, 0x00}) // InitiateResponse
	f.Add([]byte{})
	f.Add([]byte{0xff})

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = DecodePdu(data)
	})
}

// FuzzDecodeConfirmedError fuzzes the ConfirmedError decoder.
func FuzzDecodeConfirmedError(f *testing.F) {
	invokeID := berutil.EncodeTLV(0x80, []byte{0x01})
	errClass := berutil.EncodeTLV(0x80, []byte{0x01}) // vmd-state, code=1
	svcErr := berutil.EncodeTLV(0xa0, errClass)
	serviceError := berutil.EncodeTLV(0xa2, svcErr)
	f.Add(append(invokeID, serviceError...))
	f.Add([]byte{})
	f.Add([]byte{0x80, 0x01, 0x00}) // invokeID only

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = DecodeConfirmedError(data)
	})
}

// FuzzDecodeRejectPDU fuzzes the RejectPDU decoder.
func FuzzDecodeRejectPDU(f *testing.F) {
	invokeID := berutil.EncodeTLV(0x80, []byte{0x01})
	reason := berutil.EncodeTLV(0x81, []byte{0x02})
	f.Add(append(invokeID, reason...))
	f.Add([]byte{})
	f.Add(reason) // no invokeID

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = DecodeRejectPDU(data)
	})
}

// FuzzDecodeConfirmedResponse fuzzes the ConfirmedResponse envelope decoder.
func FuzzDecodeConfirmedResponse(f *testing.F) {
	f.Add([]byte{0x02, 0x01, 0x01, 0xa4, 0x00}) // invokeID=1, Read response (empty)
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = DecodeConfirmedResponse(data)
	})
}

// FuzzUnmarshalReadResponse fuzzes the Read response decoder.
func FuzzUnmarshalReadResponse(f *testing.F) {
	boolVal := berutil.EncodeTLV(0x83, []byte{0xff})
	listContent := berutil.EncodeTLV(0x30, boolVal)

	f.Add(asn1.RawValue{Tag: 4, Class: 2, IsCompound: true, Bytes: listContent}.Bytes)
	f.Add([]byte{})
	f.Add([]byte{0x30, 0x00}) // empty list

	f.Fuzz(func(t *testing.T, data []byte) {
		raw := asn1.RawValue{Tag: 4, Class: 2, IsCompound: true, Bytes: data}
		_, _ = UnmarshalReadResponse(raw)
	})
}

// FuzzUnmarshalWriteResponse fuzzes the Write response decoder.
func FuzzUnmarshalWriteResponse(f *testing.F) {
	f.Add([]byte{0x81, 0x00})       // success [1] NULL
	f.Add([]byte{0x80, 0x01, 0x05}) // failure [0] DataAccessError
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		raw := asn1.RawValue{Tag: 5, Class: 2, IsCompound: true, Bytes: data}
		_, _ = UnmarshalWriteResponse(raw)
	})
}

// FuzzUnmarshalGetNameListResponse fuzzes the GetNameList response decoder.
func FuzzUnmarshalGetNameListResponse(f *testing.F) {
	id := berutil.EncodeTLV(0x1a, []byte("name1"))
	list := berutil.EncodeTLV(0xa0, berutil.EncodeTLV(0x30, id))
	f.Add(list)
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		raw := asn1.RawValue{Tag: 1, Class: 2, IsCompound: true, Bytes: data}
		_, _ = UnmarshalGetNameListResponse(raw)
	})
}

// FuzzUnmarshalGetVarAccessResponse fuzzes the GetVariableAccessAttributes response decoder.
func FuzzUnmarshalGetVarAccessResponse(f *testing.F) {
	deletable := berutil.EncodeTLV(0x80, []byte{0x00})
	typeSpec := berutil.EncodeTLV(0xa2, berutil.EncodeTLV(0x85, []byte{32}))
	f.Add(append(deletable, typeSpec...))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		raw := asn1.RawValue{Tag: 6, Class: 2, IsCompound: true, Bytes: data}
		_, _ = UnmarshalGetVarAccessResponse(raw)
	})
}

// FuzzUnmarshalGetNamedVarListAttrsResponse fuzzes the named var list attrs decoder.
func FuzzUnmarshalGetNamedVarListAttrsResponse(f *testing.F) {
	deletable := berutil.EncodeTLV(0x80, []byte{0x00})
	objName, _ := EncodeObjectName(ObjectNameWire{Scope: ScopeDomain, DomainID: "d", ItemID: "v"})
	varSpec := berutil.EncodeTLV(0xa0, objName)
	entry := berutil.EncodeTLV(0x30, varSpec)
	listOfVar := berutil.EncodeTLV(0xa1, entry)
	f.Add(append(deletable, listOfVar...))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		raw := asn1.RawValue{Tag: 12, Class: 2, IsCompound: true, Bytes: data}
		_, _ = UnmarshalGetNamedVarListAttrsResponse(raw)
	})
}

// FuzzUnmarshalDeleteNamedVarListResponse fuzzes the delete response decoder.
func FuzzUnmarshalDeleteNamedVarListResponse(f *testing.F) {
	matched := berutil.EncodeTLV(0x02, []byte{0x01})
	deleted := berutil.EncodeTLV(0x02, []byte{0x01})
	f.Add(append(matched, deleted...))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		raw := asn1.RawValue{Tag: 13, Class: 2, IsCompound: true, Bytes: data}
		_, _ = UnmarshalDeleteNamedVarListResponse(raw)
	})
}

// --- File service decoders ---

func FuzzUnmarshalFileOpenRequest(f *testing.F) {
	nameBytes := berutil.EncodeTLV(0xa0, berutil.EncodeTLV(0x19, []byte("test.dat")))
	posBytes := berutil.EncodeTLV(0x81, berutil.EncodeUint32(0))
	f.Add(append(nameBytes, posBytes...))
	f.Add([]byte{})
	f.Add([]byte{0xff})

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = UnmarshalFileOpenRequest(data)
	})
}

func FuzzUnmarshalFileOpenResponse(f *testing.F) {
	seed, _ := MarshalFileOpenResponse(1, 1024, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	f.Add(seed)
	f.Add([]byte{})
	f.Add([]byte{0x80, 0x01, 0x01}) // frsmID only, no attributes

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = UnmarshalFileOpenResponse(data)
	})
}

func FuzzUnmarshalFileReadResponse(f *testing.F) {
	seed, _ := MarshalFileReadResponse([]byte("hello world"), false)
	f.Add(seed)
	seedMore, _ := MarshalFileReadResponse([]byte{0xde, 0xad}, true)
	f.Add(seedMore)
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = UnmarshalFileReadResponse(data)
	})
}

func FuzzUnmarshalFileDirectoryRequest(f *testing.F) {
	fileSpec := berutil.EncodeTLV(0xa0, berutil.EncodeTLV(0x19, []byte("*.dat")))
	contAfter := berutil.EncodeTLV(0xa1, berutil.EncodeTLV(0x19, []byte("last.dat")))
	f.Add(append(fileSpec, contAfter...))
	f.Add(fileSpec)
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = UnmarshalFileDirectoryRequest(data)
	})
}

func FuzzUnmarshalFileDirectoryResponse(f *testing.F) {
	seed, _ := MarshalFileDirectoryResponse([]FileDirectoryEntry{
		{FileName: "test.dat", Size: 100, LastModified: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
	}, false)
	f.Add(seed)
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = UnmarshalFileDirectoryResponse(data)
	})
}

// --- Journal decoders ---

func FuzzUnmarshalReadJournalRequest(f *testing.F) {
	seed, _ := MarshalReadJournalTimeRange(1, "dom", "jrn",
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC))
	_ = seed
	nameBytes, _ := EncodeObjectName(ObjectNameWire{Scope: ScopeDomain, DomainID: "dom", ItemID: "jrn"})
	journalName := berutil.EncodeTLV(0xa0, nameBytes)
	startTime := berutil.EncodeTLV(0xa1, berutil.EncodeTLV(0x80, make([]byte, 6)))
	stopTime := berutil.EncodeTLV(0xa2, berutil.EncodeTLV(0x80, make([]byte, 6)))
	body := append(journalName, startTime...)
	body = append(body, stopTime...)
	f.Add(body)
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = UnmarshalReadJournalRequest(data)
	})
}

func FuzzUnmarshalReadJournalResponse(f *testing.F) {
	seed, _ := MarshalReadJournalResponse([]JournalEntryWire{
		{
			EntryID:        []byte{0x01},
			OccurrenceTime: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			Variables: []JournalVariableWire{
				{Tag: "v1", Value: &DataValue{Tag: TagDataInteger, Int: 42}},
			},
		},
	}, false)
	f.Add(seed)
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = UnmarshalReadJournalResponse(data)
	})
}

// --- Server-side request decoders ---

func FuzzUnmarshalGetNameListRequest(f *testing.F) {
	classInt := berutil.EncodeTLV(0x80, []byte{0x00})
	objClass := berutil.EncodeTLV(0xa0, classInt)
	scopeContent := berutil.EncodeTLV(0x80, nil)
	objScope := berutil.EncodeTLV(0xa1, scopeContent)
	f.Add(append(objClass, objScope...))

	domScope := berutil.EncodeTLV(0x81, []byte("testDomain"))
	domObjScope := berutil.EncodeTLV(0xa1, domScope)
	seed2 := append(objClass, domObjScope...)
	contAfter := berutil.EncodeTLV(0x82, []byte("lastName"))
	seed2 = append(seed2, contAfter...)
	f.Add(seed2)
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = UnmarshalGetNameListRequest(data)
	})
}

func FuzzUnmarshalReadRequestParsed(f *testing.F) {
	nameBytes, _ := EncodeObjectName(ObjectNameWire{Scope: ScopeDomain, DomainID: "d", ItemID: "v"})
	varSpec := berutil.EncodeTLV(0x30, nameBytes)
	listOfVar := berutil.EncodeTLV(0xa0, varSpec)
	f.Add(listOfVar)

	listNameBytes, _ := EncodeObjectName(ObjectNameWire{Scope: ScopeDomain, DomainID: "d", ItemID: "list"})
	varListName := berutil.EncodeTLV(0xa1, listNameBytes)
	f.Add(varListName)
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = UnmarshalReadRequestParsed(data)
	})
}

func FuzzUnmarshalWriteRequestParsed(f *testing.F) {
	nameBytes, _ := EncodeObjectName(ObjectNameWire{Scope: ScopeDomain, DomainID: "d", ItemID: "v"})
	varSpec := berutil.EncodeTLV(0x30, nameBytes)
	listOfVar := berutil.EncodeTLV(0xa0, varSpec)
	dataVal := berutil.EncodeTLV(0x85, []byte{42})
	dataList := berutil.EncodeTLV(0x30, dataVal)
	f.Add(append(listOfVar, dataList...))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = UnmarshalWriteRequestParsed(data)
	})
}

func FuzzUnmarshalDefineNVLRequest(f *testing.F) {
	listNameBytes, _ := EncodeObjectName(ObjectNameWire{Scope: ScopeDomain, DomainID: "d", ItemID: "nvl"})
	varNameBytes, _ := EncodeObjectName(ObjectNameWire{Scope: ScopeDomain, DomainID: "d", ItemID: "v1"})
	varSpecInner := berutil.EncodeTLV(0xa0, varNameBytes)
	varEntry := berutil.EncodeTLV(0x30, varSpecInner)
	listOfVar := berutil.EncodeTLV(0xa0, varEntry)
	f.Add(append(listNameBytes, listOfVar...))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = UnmarshalDefineNVLRequest(data)
	})
}

// --- InformationReport decoder ---

func FuzzUnmarshalInformationReport(f *testing.F) {
	seed, _ := MarshalInformationReport(&InformationReportWire{
		ListName: &ObjectNameWire{Scope: ScopeDomain, DomainID: "d", ItemID: "rpt"},
		Values:   []*DataValue{{Tag: TagDataInteger, Int: 42}},
	})
	if len(seed) > 2 {
		_, content, err := berutil.DecodeTLV(seed)
		if err == nil {
			f.Add(content)
		}
	}

	varSeed, _ := MarshalInformationReport(&InformationReportWire{
		Variables: []ObjectNameWire{
			{Scope: ScopeDomain, DomainID: "d", ItemID: "v1"},
		},
		Values: []*DataValue{{Tag: TagDataBoolean, Bool: true}},
	})
	if len(varSeed) > 2 {
		_, content, err := berutil.DecodeTLV(varSeed)
		if err == nil {
			f.Add(content)
		}
	}
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = UnmarshalInformationReport(data)
	})
}
