// SPDX-License-Identifier: MIT

package pdu

import (
	"bytes"
	"runtime"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/berutil"
)

// defaultMaxPDU is the MMS PDU size both sides propose by default.
const defaultMaxPDU = 65000

// largest returns build(n) for the largest n whose encoding fits the
// default PDU size, and that n.
func largest(t *testing.T, build func(n int) []byte) ([]byte, int) {
	t.Helper()
	lo, hi := 1, 2
	for len(build(hi)) <= defaultMaxPDU {
		lo, hi = hi, 2*hi
	}
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if len(build(mid)) <= defaultMaxPDU {
			lo = mid
		} else {
			hi = mid
		}
	}
	b := build(lo)
	if len(b) > defaultMaxPDU {
		t.Fatalf("one unit takes %d octets", len(b))
	}
	return b, lo
}

// allocated returns the octets fn allocates on the heap.
func allocated(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// must returns b; a test input that does not encode is a bug in the test.
func must(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}

// What each top-level decoder allocates is linear in its input. Each
// case is the input that fits the default PDU size and decodes to the
// most values per octet; the budget is a fixed number of octets per
// input octet, so a cost that grows faster than the input exceeds it.
func TestDecoderAllocationBudget(t *testing.T) {
	emptyStruct := []byte{TagDataStructure, 0x00}
	name := ObjectNameWire{Scope: ScopeVMD, ItemID: "a"}
	vars := func(n int) []VariableSpecWire {
		out := make([]VariableSpecWire, n)
		for i := range out {
			out[i].Name = name
		}
		return out
	}
	names := func(n int) []ObjectNameWire {
		out := make([]ObjectNameWire, n)
		for i := range out {
			out[i] = name
		}
		return out
	}
	values := func(n int) []*DataValue {
		out := make([]*DataValue, n)
		for i := range out {
			out[i] = &DataValue{Tag: TagDataStructure}
		}
		return out
	}
	for _, tc := range []struct {
		name   string
		build  func(n int) []byte
		decode func(b []byte) (int, error) // the number of values decoded
		budget int                         // octets allocated per input octet
	}{
		{"Data element", func(n int) []byte {
			return berutil.EncodeTLV(TagDataArray, bytes.Repeat(emptyStruct, n))
		}, func(b []byte) (int, error) {
			v, _, err := UnmarshalDataElement(b, 0)
			if err != nil {
				return 0, err
			}
			return len(v.Elements), nil
		}, 160},
		{"access results", func(n int) []byte {
			return bytes.Repeat(emptyStruct, n)
		}, func(b []byte) (int, error) {
			vs, err := UnmarshalAccessResults(b)
			return len(vs), err
		}, 160},
		{"Read response", func(n int) []byte {
			return berutil.EncodeTLV(tagReadListOfAccessResult, bytes.Repeat(emptyStruct, n))
		}, func(b []byte) (int, error) {
			vs, err := UnmarshalReadResponse(serviceResponse(4, b))
			return len(vs), err
		}, 160},
		{"Write response", func(n int) []byte {
			return bytes.Repeat([]byte{0x81, 0x00}, n)
		}, func(b []byte) (int, error) {
			items, err := UnmarshalWriteResponse(serviceResponse(5, b))
			return len(items), err
		}, 64},
		{"GetNameList response", func(n int) []byte {
			list := make([]string, n)
			for i := range list {
				list[i] = "a"
			}
			return must(MarshalGetNameListResponse(list, true))
		}, func(b []byte) (int, error) {
			r, err := UnmarshalGetNameListResponse(serviceResponse(1, b))
			if err != nil {
				return 0, err
			}
			return len(r.Names), nil
		}, 40},
		{"GetVariableAccessAttributes response", func(n int) []byte {
			comps := make([]StructComponentWire, n)
			for i := range comps {
				comps[i] = StructComponentWire{Name: "a", Type: TypeSpecWire{Tag: tsTagBoolean}}
			}
			return must(MarshalGetVarAccessResponse(false, TypeSpecWire{Tag: tsTagStructure, Components: comps}))
		}, func(b []byte) (int, error) {
			r, err := UnmarshalGetVarAccessResponse(serviceResponse(6, b))
			if err != nil {
				return 0, err
			}
			return len(r.TypeSpec.Components), nil
		}, 80},
		{"GetNamedVariableListAttributes response", func(n int) []byte {
			return must(MarshalGetNVLAttrsResponse(false, vars(n)))
		}, func(b []byte) (int, error) {
			r, err := UnmarshalGetNamedVarListAttrsResponse(serviceResponse(12, b))
			if err != nil {
				return 0, err
			}
			return len(r.Variables), nil
		}, 64},
		{"FileRead response", func(n int) []byte {
			return must(MarshalFileReadResponse(make([]byte, n), false))
		}, func(b []byte) (int, error) {
			r, err := UnmarshalFileReadResponse(b)
			if err != nil {
				return 0, err
			}
			return len(r.Data), nil
		}, 2},
		{"FileDirectory response", func(n int) []byte {
			return must(MarshalFileDirectoryResponse(make([]FileDirectoryEntry, n), true))
		}, func(b []byte) (int, error) {
			entries, _, err := UnmarshalFileDirectoryResponse(b)
			return len(entries), err
		}, 32},
		{"ReadJournal response", func(n int) []byte {
			entries := make([]JournalEntryWire, n)
			for i := range entries {
				entries[i] = JournalEntryWire{EntryID: []byte{1}, OccurrenceTime: time.Unix(0, 0), HasAnnotation: true}
			}
			return must(MarshalReadJournalResponse(entries, true))
		}, func(b []byte) (int, error) {
			entries, _, err := UnmarshalReadJournalResponse(b)
			return len(entries), err
		}, 24},
		{"Read request", func(n int) []byte {
			return must(requestBody(MarshalReadRequestWithAccess(1, vars(n))))
		}, func(b []byte) (int, error) {
			r, err := UnmarshalReadRequestParsed(b)
			if err != nil {
				return 0, err
			}
			return len(r.Variables), nil
		}, 64},
		{"Write request", func(n int) []byte {
			return must(requestBody(MarshalWriteRequestWithAccess(1, vars(n), values(n))))
		}, func(b []byte) (int, error) {
			r, err := UnmarshalWriteRequestParsed(b)
			if err != nil {
				return 0, err
			}
			return len(r.Variables) + len(r.Values), nil
		}, 80},
		{"DefineNamedVariableList request", func(n int) []byte {
			return must(requestBody(MarshalDefineNamedVarListRequest(1, name, vars(n))))
		}, func(b []byte) (int, error) {
			r, err := UnmarshalDefineNVLRequest(b)
			if err != nil {
				return 0, err
			}
			return len(r.Variables), nil
		}, 64},
		{"ReadJournal request", func(n int) []byte {
			journal := must(EncodeObjectName(ObjectNameWire{Scope: ScopeDomain, DomainID: "d", ItemID: "j"}))
			list := bytes.Repeat([]byte{0x1a, 0x01, 'a'}, n)
			return append(berutil.EncodeTLV(0xa0, journal), berutil.EncodeTLV(0xa4, list)...)
		}, func(b []byte) (int, error) {
			r, err := UnmarshalReadJournalRequest(b)
			if err != nil {
				return 0, err
			}
			return len(r.ListOfVariables), nil
		}, 40},
		{"InformationReport", func(n int) []byte {
			return must(pduContent(must(MarshalInformationReport(&InformationReportWire{Variables: names(n), Values: values(n)}))))
		}, func(b []byte) (int, error) {
			r, err := UnmarshalInformationReport(b)
			if err != nil {
				return 0, err
			}
			return len(r.Variables) + len(r.Values), nil
		}, 64},
		{"PDU of indefinite-length elements", func(n int) []byte {
			inner := bytes.Repeat([]byte{0x30, 0x80, 0x00, 0x00}, n)
			return append(append([]byte{0xa1, 0x80}, inner...), 0x00, 0x00)
		}, func(b []byte) (int, error) {
			_, content, err := DecodePdu(b)
			return len(content) / 2, err
		}, 2},
		{"Confirmed-Response envelope", func(n int) []byte {
			return append([]byte{0x02, 0x01, 0x01}, berutil.EncodeTLV(0xa4, make([]byte, n))...)
		}, func(b []byte) (int, error) {
			r, err := DecodeConfirmedResponse(b)
			if err != nil {
				return 0, err
			}
			return len(r.ServiceData.Bytes), nil
		}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, n := largest(t, tc.build)
			var got int
			var err error
			alloc := allocated(func() { got, err = tc.decode(b) })
			if err != nil || got < n {
				t.Fatalf("%d units in %d octets: %d decoded, %v", n, len(b), got, err)
			}
			t.Logf("%d units in %d octets: %d octets allocated, %.1f per input octet", n, len(b), alloc, float64(alloc)/float64(len(b)))
			if limit := uint64(tc.budget * len(b)); alloc > limit {
				t.Errorf("%d units in %d octets allocated %d octets, budget %d", n, len(b), alloc, limit)
			}
		})
	}
}
