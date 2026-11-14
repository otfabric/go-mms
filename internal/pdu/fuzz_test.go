// SPDX-License-Identifier: MIT

package pdu

import (
	"bytes"
	"encoding/asn1"
	"encoding/hex"
	"math"
	"math/big"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
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
	// array with numberOfElements at and beyond the int32 boundary, and negative
	for _, count := range [][]byte{{0x7f, 0xff, 0xff, 0xff}, {0x00, 0x80, 0x00, 0x00, 0x00}, {0xff}} {
		f.Add(berutil.EncodeTLV(0xa1, arrayTypeSpec(count)))
	}
	f.Add([]byte{})
	f.Add([]byte{0xff})
	f.Add([]byte{0x83}) // truncated

	f.Fuzz(func(t *testing.T, data []byte) {
		ts, err := DecodeTypeSpec(data)
		if err != nil {
			return
		}
		// Whatever decodes, an array count is never negative.
		var walk func(TypeSpecWire)
		walk = func(ts TypeSpecWire) {
			if ts.Count < 0 {
				t.Fatalf("array count %d is negative", ts.Count)
			}
			if ts.Element != nil {
				walk(*ts.Element)
			}
			for _, c := range ts.Components {
				walk(c.Type)
			}
		}
		walk(ts)
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
	// generalized time
	f.Add(berutil.EncodeTLV(0x8b, []byte("20240315120000Z")))
	// bcd integer
	f.Add(berutil.EncodeTLV(0x8d, []byte{42}))
	// object identifier (1.2.840.10004)
	oidContent, _ := berutil.EncodeObjectIdentifier([]int{1, 2, 840, 10004})
	f.Add(berutil.EncodeTLV(0x8f, oidContent))
	// empty
	f.Add([]byte{})
	// truncated
	f.Add([]byte{0x85, 0x10})

	f.Fuzz(func(t *testing.T, data []byte) {
		v, n, err := UnmarshalDataElement(data, 0)
		if err != nil {
			return
		}
		if v == nil || n <= 0 || n > len(data) {
			t.Fatalf("UnmarshalDataElement = %v, %d of %d octets, nil error", v, n, len(data))
		}
		if !encodableData(v) {
			return
		}
		reencodes(t, v, MarshalData, decodeOneElement)
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
		vs, err := UnmarshalAccessResults(data)
		if err != nil {
			return
		}
		noNil(t, vs)
		if !encodableData(vs...) {
			return
		}
		reencodes(t, vs, MarshalDataList, UnmarshalAccessResults)
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
		kind, content, err := DecodePdu(data)
		if err != nil {
			return
		}
		// The contents wrapped in the same identifier decode the same.
		again, content2, err := DecodePdu(berutil.EncodeTLV(data[0], content))
		if err != nil || again != kind || !bytes.Equal(content2, content) {
			t.Fatalf("re-wrapped contents decode as %v, % x, %v; want %v, % x", again, content2, err, kind, content)
		}
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
		ce, err := DecodeConfirmedError(data)
		if err != nil {
			return
		}
		if ce.ErrorClass != ce.Detail.ErrorClass || ce.ErrorCode != ce.Detail.ErrorCode {
			t.Fatalf("class %d, code %d do not repeat the ServiceError's %d, %d", ce.ErrorClass, ce.ErrorCode, ce.Detail.ErrorClass, ce.Detail.ErrorCode)
		}
		reencodes(t, ce, func(ce *ConfirmedError) ([]byte, error) {
			f := codec.ServiceErrorFields{AdditionalCode: ce.Detail.AdditionalCode}
			if d := ce.Detail.AdditionalDescription; d != nil {
				f.AdditionalDescription = *d
			}
			if s := ce.Detail.ServiceSpecific; s != nil {
				f.HasSpecific, f.SpecificKind, f.SpecificContent = true, s.Kind, s.Raw
			}
			return pduContent(codec.MarshalConfirmedErrorFields(ce.InvokeID, ce.ErrorClass, ce.ErrorCode, f))
		}, DecodeConfirmedError)
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
		r, err := DecodeRejectPDU(data)
		if err != nil {
			return
		}
		if r.RejectType < 1 || r.RejectType > 11 || !r.HasInvokeID && r.InvokeID != 0 {
			t.Fatalf("DecodeRejectPDU = %+v", r)
		}
		reencodes(t, r, func(r *RejectPDU) ([]byte, error) {
			if r.HasInvokeID {
				return pduContent(codec.MarshalRejectPDU(r.InvokeID, r.RejectType, r.RejectReason))
			}
			return pduContent(codec.MarshalRejectPDUNoInvokeID(r.RejectType, r.RejectReason))
		}, DecodeRejectPDU)
	})
}

// FuzzDecodeConfirmedResponse fuzzes the ConfirmedResponse envelope decoder.
func FuzzDecodeConfirmedResponse(f *testing.F) {
	f.Add([]byte{0x02, 0x01, 0x01, 0xa4, 0x00}) // invokeID=1, Read response (empty)
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		r, err := DecodeConfirmedResponse(data)
		if err != nil {
			return
		}
		if r.ServiceKind != ClassifyServiceTag(r.ServiceData.Tag) {
			t.Fatalf("service kind %v for tag %d", r.ServiceKind, r.ServiceData.Tag)
		}
		reencodes(t, r, func(r *ConfirmedResponse) ([]byte, error) {
			b, err := codec.MarshalConfirmedResponse(r.InvokeID, r.ServiceData.Tag, r.ServiceData.IsCompound, r.ServiceData.Bytes)
			if err != nil {
				return nil, err
			}
			return pduContent(b)
		}, DecodeConfirmedResponse)
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
		vs, err := UnmarshalReadResponse(raw)
		if err != nil {
			return
		}
		noNil(t, vs)
		if !encodableData(vs...) {
			return
		}
		reencodes(t, vs, marshalReadResults, func(b []byte) ([]*DataValue, error) {
			return UnmarshalReadResponse(serviceResponse(4, b))
		})
	})
}

// FuzzUnmarshalWriteResponse fuzzes the Write response decoder.
func FuzzUnmarshalWriteResponse(f *testing.F) {
	f.Add([]byte{0x81, 0x00})       // success [1] NULL
	f.Add([]byte{0x80, 0x01, 0x05}) // failure [0] DataAccessError
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		raw := asn1.RawValue{Tag: 5, Class: 2, IsCompound: true, Bytes: data}
		items, err := UnmarshalWriteResponse(raw)
		if err != nil {
			return
		}
		reencodes(t, items, func(items []WriteResultItem) ([]byte, error) {
			results := make([]WriteResult, len(items))
			for i, it := range items {
				results[i] = WriteResult{Success: it.Success, Code: it.ErrCode}
			}
			return MarshalWriteResponse(results)
		}, func(b []byte) ([]WriteResultItem, error) {
			return UnmarshalWriteResponse(serviceResponse(5, b))
		})
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
		r, err := UnmarshalGetNameListResponse(raw)
		if err != nil {
			return
		}
		reencodes(t, r, func(r *GetNameListResult) ([]byte, error) {
			return MarshalGetNameListResponse(r.Names, r.MoreFollows)
		}, func(b []byte) (*GetNameListResult, error) {
			return UnmarshalGetNameListResponse(serviceResponse(1, b))
		})
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
		r, err := UnmarshalGetVarAccessResponse(raw)
		if err != nil || r.TypeSpec.Tag == tsTagTypeName {
			// A top-level typeName is decoded but not encoded (see
			// MarshalGetVarAccessResponse).
			return
		}
		reencodes(t, r, func(r *GetVarAccessResult) ([]byte, error) {
			return MarshalGetVarAccessResponse(r.Deletable, r.TypeSpec)
		}, func(b []byte) (*GetVarAccessResult, error) {
			return UnmarshalGetVarAccessResponse(serviceResponse(6, b))
		})
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
		r, err := UnmarshalGetNamedVarListAttrsResponse(raw)
		if err != nil {
			return
		}
		reencodes(t, r, func(r *NamedVarListAttrsResult) ([]byte, error) {
			return MarshalGetNVLAttrsResponse(r.Deletable, r.Variables)
		}, func(b []byte) (*NamedVarListAttrsResult, error) {
			return UnmarshalGetNamedVarListAttrsResponse(serviceResponse(12, b))
		})
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
		r, err := UnmarshalDeleteNamedVarListResponse(raw)
		if err != nil {
			return
		}
		reencodes(t, r, func(r *DeleteNamedVarListResult) ([]byte, error) {
			return MarshalDeleteNVLResponse(r.NumberMatched, r.NumberDeleted)
		}, func(b []byte) (*DeleteNamedVarListResult, error) {
			return UnmarshalDeleteNamedVarListResponse(serviceResponse(13, b))
		})
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
		r, err := UnmarshalFileOpenRequest(data)
		if err != nil {
			return
		}
		reencodes(t, r, func(r *FileOpenRequest) ([]byte, error) {
			return requestBody(MarshalFileOpenRequest(1, r.FileName, r.InitialPosition))
		}, UnmarshalFileOpenRequest)
	})
}

func FuzzUnmarshalFileOpenResponse(f *testing.F) {
	seed, _ := MarshalFileOpenResponse(1, 1024, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	f.Add(seed)
	f.Add([]byte{})
	f.Add([]byte{0x80, 0x01, 0x01}) // frsmID only, no attributes

	f.Fuzz(func(t *testing.T, data []byte) {
		r, err := UnmarshalFileOpenResponse(data)
		if err != nil {
			return
		}
		reencodes(t, r, func(r *FileOpenResult) ([]byte, error) {
			return MarshalFileOpenResponse(r.FrsmID, r.Size, r.LastModified)
		}, UnmarshalFileOpenResponse)
	})
}

func FuzzUnmarshalFileReadResponse(f *testing.F) {
	seed, _ := MarshalFileReadResponse([]byte("hello world"), false)
	f.Add(seed)
	seedMore, _ := MarshalFileReadResponse([]byte{0xde, 0xad}, true)
	f.Add(seedMore)
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		r, err := UnmarshalFileReadResponse(data)
		if err != nil {
			return
		}
		reencodes(t, r, func(r *FileReadResult) ([]byte, error) {
			return MarshalFileReadResponse(r.Data, r.MoreFollows)
		}, UnmarshalFileReadResponse)
	})
}

func FuzzUnmarshalFileDirectoryRequest(f *testing.F) {
	fileSpec := berutil.EncodeTLV(0xa0, berutil.EncodeTLV(0x19, []byte("*.dat")))
	contAfter := berutil.EncodeTLV(0xa1, berutil.EncodeTLV(0x19, []byte("last.dat")))
	f.Add(append(fileSpec, contAfter...))
	f.Add(fileSpec)
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		r, err := UnmarshalFileDirectoryRequest(data)
		if err != nil {
			return
		}
		reencodes(t, r, func(r *FileDirectoryRequest) ([]byte, error) {
			return requestBody(MarshalFileDirectoryRequest(1, r.FileSpec, r.ContinueAfter))
		}, UnmarshalFileDirectoryRequest)
	})
}

func FuzzUnmarshalFileDirectoryResponse(f *testing.F) {
	seed, _ := MarshalFileDirectoryResponse([]FileDirectoryEntry{
		{FileName: "test.dat", Size: 100, LastModified: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
	}, false)
	f.Add(seed)
	f.Add([]byte{})
	if explicit, err := hex.DecodeString(fileDirectoryExplicitHex); err == nil {
		f.Add(explicit)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		entries, more, err := UnmarshalFileDirectoryResponse(data)
		if err != nil {
			return
		}
		reencodes(t, page[FileDirectoryEntry]{entries, more}, func(p page[FileDirectoryEntry]) ([]byte, error) {
			return MarshalFileDirectoryResponse(p.items, p.more)
		}, func(b []byte) (page[FileDirectoryEntry], error) {
			entries, more, err := UnmarshalFileDirectoryResponse(b)
			return page[FileDirectoryEntry]{entries, more}, err
		})
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
		r, err := UnmarshalReadJournalRequest(data)
		if err != nil {
			return
		}
		// rangeStart and rangeStop are CHOICEs (ISO 9506-2 clause 23.7.1).
		switch {
		case r.Journal == "",
			r.HasStartTime && r.HasStartEntry,
			r.HasStopTime && r.HasNumberOfEntries,
			r.IsTimeRange != (r.HasStartTime && r.HasStopTime),
			r.NumberOfEntries < 0,
			!r.HasNumberOfEntries && r.NumberOfEntries != 0,
			!r.HasStartEntry && r.StartEntry != nil:
			t.Fatalf("UnmarshalReadJournalRequest = %+v", r)
		}
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
		{EntryID: []byte{0x02}, OccurrenceTime: time.Date(2024, 1, 1, 12, 0, 1, 0, time.UTC), HasAnnotation: true, Annotation: "note"},
	}, false)
	f.Add(seed)
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		entries, more, err := UnmarshalReadJournalResponse(data)
		if err != nil {
			return
		}
		for _, e := range entries {
			for _, v := range e.Variables {
				if !encodableData(v.Value) {
					return
				}
			}
		}
		reencodes(t, page[JournalEntryWire]{entries, more}, func(p page[JournalEntryWire]) ([]byte, error) {
			return MarshalReadJournalResponse(p.items, p.more)
		}, func(b []byte) (page[JournalEntryWire], error) {
			entries, more, err := UnmarshalReadJournalResponse(b)
			return page[JournalEntryWire]{entries, more}, err
		})
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
		r, err := UnmarshalGetNameListRequest(data)
		if err != nil || r.ObjectClass < 0 {
			// The decoder keeps any INTEGER as the object class;
			// MarshalGetNameListRequest encodes only one that is not
			// negative.
			return
		}
		reencodes(t, r, func(r *GetNameListRequest) ([]byte, error) {
			return requestBody(MarshalGetNameListRequest(1, r.ObjectClass, r.Scope, r.DomainID, r.ContinueAfter))
		}, UnmarshalGetNameListRequest)
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
	// AlternateAccess chains at and beyond the depth limit.
	f.Add(readRequestWithSelection(maxAlternateAccessDepth))
	f.Add(readRequestWithSelection(maxAlternateAccessDepth + 1))

	f.Fuzz(func(t *testing.T, data []byte) {
		r, err := UnmarshalReadRequestParsed(data)
		if err != nil {
			return
		}
		if r.ListName != nil && r.Variables != nil {
			t.Fatalf("both a list name and variables: %+v", r)
		}
		reencodes(t, r, func(r *ReadRequestWire) ([]byte, error) {
			if r.ListName != nil {
				return requestBody(MarshalReadRequestByListNameWithSpec(1, *r.ListName, r.SpecWithResult))
			}
			return requestBody(MarshalReadRequestWithAccess(1, r.Variables))
		}, UnmarshalReadRequestParsed)
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
		r, err := UnmarshalWriteRequestParsed(data)
		if err != nil {
			return
		}
		if r.ListName != nil && r.Variables != nil {
			t.Fatalf("both a list name and variables: %+v", r)
		}
		noNil(t, r.Values)
		if !encodableData(r.Values...) {
			return
		}
		reencodes(t, r, func(r *WriteRequestWire) ([]byte, error) {
			if r.ListName != nil {
				return requestBody(MarshalWriteRequestByListName(1, *r.ListName, r.Values))
			}
			return requestBody(MarshalWriteRequestWithAccess(1, r.Variables, r.Values))
		}, UnmarshalWriteRequestParsed)
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
		r, err := UnmarshalDefineNVLRequest(data)
		if err != nil {
			return
		}
		reencodes(t, r, func(r *DefineNVLRequest) ([]byte, error) {
			return requestBody(MarshalDefineNamedVarListRequest(1, r.ListName, r.Variables))
		}, UnmarshalDefineNVLRequest)
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
		r, err := UnmarshalInformationReport(data)
		if err != nil {
			return
		}
		if r.ListName != nil && r.Variables != nil {
			t.Fatalf("both a list name and variables: %+v", r)
		}
		noNil(t, r.Values)
		if !encodableData(r.Values...) {
			return
		}
		reencodes(t, r, func(r *InformationReportWire) ([]byte, error) {
			b, err := MarshalInformationReport(r)
			if err != nil {
				return nil, err
			}
			return pduContent(b)
		}, UnmarshalInformationReport)
	})
}

// FuzzDecodeGeneralizedTime fuzzes the GeneralizedTime data element
// decoder via round-trip: encode a valid GeneralizedTime TLV, then
// decode arbitrary mutations.
func FuzzDecodeGeneralizedTime(f *testing.F) {
	f.Add([]byte("20240315120000Z"))
	f.Add([]byte("20000101000000Z"))
	f.Add([]byte("19991231235959Z"))
	f.Add([]byte(""))
	f.Add([]byte("not-a-time"))
	f.Add([]byte("2024031512000"))  // truncated
	f.Add([]byte("20240315120000")) // missing Z

	f.Fuzz(func(t *testing.T, content []byte) {
		data := berutil.EncodeTLV(TagDataGenTime, content)
		dv, _, err := UnmarshalDataElement(data, 0)
		if err != nil {
			return
		}
		encoded, err := MarshalData(dv)
		if err != nil {
			t.Fatalf("re-encode failed: %v", err)
		}
		dv2, _, err := UnmarshalDataElement(encoded, 0)
		if err != nil {
			t.Fatalf("re-decode failed: %v", err)
		}
		if !dv.Time.Equal(dv2.Time) {
			t.Fatalf("round-trip mismatch: %v vs %v", dv.Time, dv2.Time)
		}
	})
}

// FuzzDecodeBCD fuzzes the BCD (binary-coded decimal) data element
// decoder via round-trip.
func FuzzDecodeBCD(f *testing.F) {
	f.Add([]byte{0})
	f.Add([]byte{42})
	f.Add([]byte{0x7f})
	f.Add([]byte{0x80})             // -128
	f.Add([]byte{0xff})             // -1
	f.Add([]byte{0x00, 0x80})       // 128
	f.Add([]byte{0x7f, 0xff})       // 32767
	f.Add([]byte{0x00, 0x00, 0x01}) // 1 with leading zero
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, content []byte) {
		data := berutil.EncodeTLV(TagDataBCD, content)
		dv, _, err := UnmarshalDataElement(data, 0)
		if err != nil {
			return
		}
		encoded, err := MarshalData(dv)
		if err != nil {
			t.Fatalf("re-encode failed: %v", err)
		}
		dv2, _, err := UnmarshalDataElement(encoded, 0)
		if err != nil {
			t.Fatalf("re-decode failed: %v", err)
		}
		if dv.Int != dv2.Int {
			t.Fatalf("round-trip mismatch: %d vs %d", dv.Int, dv2.Int)
		}
	})
}

// FuzzDecodeReal fuzzes the ASN.1 REAL data element decoder.
func FuzzDecodeReal(f *testing.F) {
	f.Add(encodeASN1Real(0))
	f.Add(encodeASN1Real(1.0))
	f.Add(encodeASN1Real(-1.0))
	f.Add(encodeASN1Real(3.14159265358979))
	f.Add(encodeASN1Real(math.MaxFloat64))
	f.Add(encodeASN1Real(math.SmallestNonzeroFloat64))
	f.Add(encodeASN1Real(math.Inf(1)))
	f.Add(encodeASN1Real(math.Inf(-1)))
	f.Add([]byte{0x42}) // NaN
	f.Add([]byte{0x43}) // -0
	f.Add([]byte{})
	// exponents whose product with the base shift overflows int64
	f.Add(realWithExponent(false, 0x40, 0, 0, 0, 0, 0, 0, 0))
	f.Add(realWithExponent(true, 0xc0, 0, 0, 0, 0, 0, 0, 0))
	// a zero mantissa with a large exponent; a full mantissa whose
	// product is subnormal
	f.Add([]byte{0x81, 0x7f, 0xff, 0x00})
	f.Add([]byte{0x81, 0xfb, 0x8f, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})

	f.Fuzz(func(t *testing.T, content []byte) {
		data := berutil.EncodeTLV(TagDataReal, content)
		dv, _, err := UnmarshalDataElement(data, 0)
		if err != nil {
			return
		}
		if want, ok := realReference(content); ok && math.Float64bits(dv.Float) != math.Float64bits(want) {
			t.Fatalf("% x decodes as %v, want %v", content, dv.Float, want)
		}
		encoded, err := MarshalData(dv)
		if err != nil {
			t.Fatalf("re-encode failed: %v", err)
		}
		dv2, _, err := UnmarshalDataElement(encoded, 0)
		if err != nil {
			t.Fatalf("re-decode failed: %v", err)
		}
		if math.IsNaN(dv.Float) && math.IsNaN(dv2.Float) {
			return
		}
		if dv.Float != dv2.Float {
			t.Fatalf("round-trip Real mismatch: %v vs %v", dv.Float, dv2.Float)
		}
	})
}

// realReference is the value of binary REAL content, M × 2^F × B^E
// (X.690 §8.5), computed apart from the decoder: the exponent in a
// big.Int, the scaling with math.Ldexp. ok is false for the other forms.
func realReference(content []byte) (v float64, ok bool) {
	if len(content) < 2 || content[0]&0x80 == 0 {
		return 0, false
	}
	info := content[0]
	shift := [...]int64{1, 3, 4, 0}[(info>>4)&0x03]
	if shift == 0 {
		return 0, false
	}
	rest := content[1:]
	n := int(info&0x03) + 1
	if info&0x03 == 0x03 {
		n, rest = int(rest[0]), rest[1:]
	}
	if n == 0 || len(rest) <= n || len(rest)-n > 8 {
		return 0, false
	}
	e := new(big.Int).SetBytes(rest[:n])
	if rest[0]&0x80 != 0 {
		e.Sub(e, new(big.Int).Lsh(big.NewInt(1), uint(8*n)))
	}
	e.Mul(e, big.NewInt(shift))
	e.Add(e, big.NewInt(int64((info>>2)&0x03)))
	// Beyond ±2^20 every non-zero mantissa gives ±Inf or zero.
	if bound := big.NewInt(1 << 20); e.Cmp(bound) > 0 {
		e = bound
	} else if e.Cmp(new(big.Int).Neg(bound)) < 0 {
		e = new(big.Int).Neg(bound)
	}
	var m uint64
	for _, b := range rest[n:] {
		m = m<<8 | uint64(b)
	}
	v = math.Ldexp(float64(m), int(e.Int64()))
	if info&0x40 != 0 {
		v = -v
	}
	return v, true
}

// FuzzDecodeBooleanArray fuzzes the BooleanArray data element decoder.
func FuzzDecodeBooleanArray(f *testing.F) {
	f.Add(mustBitString([]byte{0xff}, 8))
	f.Add(mustBitString([]byte{0xaa, 0x55}, 16))
	f.Add(mustBitString([]byte{0xf0}, 4))
	f.Add(mustBitString(nil, 0))
	f.Add([]byte{0x00})

	f.Fuzz(func(t *testing.T, content []byte) {
		data := berutil.EncodeTLV(TagDataBooleanArray, content)
		dv, _, err := UnmarshalDataElement(data, 0)
		if err != nil {
			return
		}
		encoded, err := MarshalData(dv)
		if err != nil {
			t.Fatalf("re-encode failed: %v", err)
		}
		dv2, _, err := UnmarshalDataElement(encoded, 0)
		if err != nil {
			t.Fatalf("re-decode failed: %v", err)
		}
		if dv.BitLen != dv2.BitLen {
			t.Fatalf("round-trip BooleanArray bitLen mismatch: %d vs %d", dv.BitLen, dv2.BitLen)
		}
	})
}

// FuzzDecodeObjectIdentifier fuzzes the ObjectIdentifier data element
// decoder via round-trip.
func FuzzDecodeObjectIdentifier(f *testing.F) {
	for _, oid := range [][]int{
		{1, 2, 840, 10004},
		{2, 16, 840, 1, 101, 2, 1},
		{0, 0},
		{1, 3, 6, 1, 4, 1, 311},
	} {
		content, err := berutil.EncodeObjectIdentifier(oid)
		if err == nil {
			f.Add(content)
		}
	}
	f.Add([]byte{})
	f.Add([]byte{0x2a, 0x86, 0x48}) // partial OID (1.2.840 prefix)

	f.Fuzz(func(t *testing.T, content []byte) {
		data := berutil.EncodeTLV(TagDataObjId, content)
		dv, _, err := UnmarshalDataElement(data, 0)
		if err != nil {
			return
		}
		encoded, err := MarshalData(dv)
		if err != nil {
			t.Fatalf("re-encode failed: %v", err)
		}
		dv2, _, err := UnmarshalDataElement(encoded, 0)
		if err != nil {
			t.Fatalf("re-decode failed: %v", err)
		}
		if len(dv.OID) != len(dv2.OID) {
			t.Fatalf("round-trip OID length mismatch: %v vs %v", dv.OID, dv2.OID)
		}
		for i := range dv.OID {
			if dv.OID[i] != dv2.OID[i] {
				t.Fatalf("round-trip OID arc %d mismatch: %v vs %v", i, dv.OID, dv2.OID)
			}
		}
	})
}

func mustBitString(data []byte, bitLen int) []byte {
	b, err := encodeBitString(data, bitLen)
	if err != nil {
		panic(err)
	}
	return b
}

// FuzzUnmarshalInitializeJournalRequest fuzzes the InitializeJournal
// request decoder (ISO 9506-2 clause 23.4): no panic, and a decoded
// limitingEntry is only set with a limit.
func FuzzUnmarshalInitializeJournalRequest(f *testing.F) {
	f.Add([]byte{0xa0, 0x0b, 0xa1, 0x09, 0x1a, 0x01, 'D', 0x1a, 0x04, 'J', 'R', 'N', 'L'})
	f.Add([]byte{0xa0, 0x03, 0x80, 0x01, 'J', 0xa1, 0x0b, 0x80, 0x06, 0, 0, 0, 0, 0, 0, 0x81, 0x01, 0x09})
	f.Add([]byte{0xa0, 0x03, 0x80, 0x01, 'J', 0xa1, 0x08, 0x80, 0x06, 0, 0, 0, 0, 0, 0})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		req, err := UnmarshalInitializeJournalRequest(data)
		if err == nil && !req.HasLimit && req.LimitingEntry != nil {
			t.Fatalf("limitingEntry without limitSpecification: %+v", req)
		}
		_, _ = UnmarshalGetDomainAttributesRequest(data)
	})
}
