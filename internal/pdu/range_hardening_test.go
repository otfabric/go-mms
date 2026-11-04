// SPDX-License-Identifier: MIT

package pdu

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
)

// Invoke IDs are Unsigned32: IDs from 2^31 arrive with a 0x00 pad octet
// and must be accepted; negative encodings must be rejected instead of
// wrapping onto another pending request.
func TestExtractInvokeID_Unsigned32(t *testing.T) {
	for _, tc := range []struct {
		in   []byte
		want uint32
		ok   bool
	}{
		{[]byte{0x02, 0x01, 0x05}, 5, true},
		{[]byte{0x80, 0x05, 0x00, 0x80, 0x00, 0x00, 0x00}, 1 << 31, true},
		{[]byte{0x02, 0x05, 0x00, 0xff, 0xff, 0xff, 0xff}, math.MaxUint32, true},
		{[]byte{0x02, 0x01, 0xff}, 0, false},
		{[]byte{0x02, 0x05, 0x01, 0x00, 0x00, 0x00, 0x00}, 0, false},
	} {
		got, err := ExtractInvokeID(tc.in)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("ExtractInvokeID(%x) = %d, %v; want %d ok=%v", tc.in, got, err, tc.want, tc.ok)
		}
	}
}

func TestDecodeConfirmedErrorAndReject_InvokeIDRange(t *testing.T) {
	serviceErr := berutil.EncodeTLV(0xa2, berutil.EncodeTLV(0xa0, berutil.EncodeTLV(0x80, []byte{0x01})))
	high := berutil.EncodeTLV(0x80, []byte{0x00, 0x80, 0x00, 0x00, 0x01})
	neg := berutil.EncodeTLV(0x80, []byte{0xff})

	ce, err := DecodeConfirmedError(append(append([]byte(nil), high...), serviceErr...))
	if err != nil || ce.InvokeID != 1<<31+1 {
		t.Fatalf("confirmed error, high ID: %+v, %v", ce, err)
	}
	if _, err := DecodeConfirmedError(append(append([]byte(nil), neg...), serviceErr...)); err == nil {
		t.Fatal("confirmed error with negative invoke ID accepted")
	}

	reason := []byte{0x81, 0x01, 0x01}
	rj, err := DecodeRejectPDU(append(append([]byte(nil), high...), reason...))
	if err != nil || rj.InvokeID != 1<<31+1 || !rj.HasInvokeID {
		t.Fatalf("reject, high ID: %+v, %v", rj, err)
	}
	if _, err := DecodeRejectPDU(append(append([]byte(nil), neg...), reason...)); err == nil {
		t.Fatal("reject with negative invoke ID accepted")
	}
}

func TestEncodeAlternateAccess_RejectsOutOfRange(t *testing.T) {
	for name, sels := range map[string][]AccessSelectorWire{
		"negative index":          {{HasIndex: true, Index: -1}},
		"negative nested index":   {{HasIndex: true, Index: -1}, {Component: "x"}},
		"negative low index":      {{IndexRange: &IndexRangeWire{LowIndex: -1, NumberOfElements: 1}}},
		"negative count":          {{IndexRange: &IndexRangeWire{LowIndex: 0, NumberOfElements: -2}}},
		"negative nested count":   {{IndexRange: &IndexRangeWire{LowIndex: 0, NumberOfElements: -2}}, {Component: "x"}},
		"negative nested in rest": {{Component: "x"}, {HasIndex: true, Index: -3}},
	} {
		if _, err := encodeAlternateAccess(sels); err == nil || !strings.Contains(err.Error(), "Unsigned32") {
			t.Errorf("%s: err = %v, want Unsigned32 range error", name, err)
		}
	}
	// The largest Unsigned32 index still encodes and decodes.
	if math.MaxInt == math.MaxInt64 {
		wide := int64(math.MaxUint32)
		maxIdx := int(wide)
		b, err := encodeAlternateAccess([]AccessSelectorWire{{HasIndex: true, Index: maxIdx}})
		if err != nil {
			t.Fatal(err)
		}
		got, err := decodeAlternateAccess(b)
		if err != nil || len(got) != 1 || got[0].Index != maxIdx {
			t.Fatalf("decode = %+v, %v", got, err)
		}
	}
}

func TestMarshalGetNameListRequest_NegativeClass(t *testing.T) {
	if _, err := MarshalGetNameListRequest(1, -1, 0, "", ""); err == nil {
		t.Fatal("negative object class encoded")
	}
}

func TestFileSizeRange(t *testing.T) {
	if _, err := MarshalFileOpenResponse(1, -1, time.Time{}); err == nil {
		t.Error("negative file size encoded")
	}
	if _, err := MarshalFileOpenResponse(1, math.MaxUint32+1, time.Time{}); err == nil {
		t.Error("file size over Unsigned32 encoded")
	}
	if _, err := MarshalFileOpenResponse(1, math.MaxUint32, time.Time{}); err != nil {
		t.Errorf("file size 2^32-1: %v", err)
	}
	_, err := MarshalFileDirectoryResponse([]FileDirectoryEntry{{FileName: "a", Size: -5}}, false)
	if err == nil || !strings.Contains(err.Error(), `"a"`) {
		t.Errorf("directory entry with negative size: %v", err)
	}
}

// FrsmID is Integer32: a five-octet value must not be truncated.
func TestFileFrsmIDRange(t *testing.T) {
	if _, err := UnmarshalFileReadRequest([]byte{0x00, 0x80, 0x00, 0x00, 0x00}); err == nil {
		t.Error("FileRead frsmID 2^31 accepted")
	}
	if _, err := UnmarshalFileCloseRequest([]byte{0x00, 0x80, 0x00, 0x00, 0x00}); err == nil {
		t.Error("FileClose frsmID 2^31 accepted")
	}
	if v, err := UnmarshalFileReadRequest([]byte{0x80, 0x00, 0x00, 0x00}); err != nil || v != math.MinInt32 {
		t.Errorf("FileRead frsmID MinInt32 = %d, %v", v, err)
	}
}

// Context tags above 30 need the multi-byte form; WrapConstructed used to
// write the long-form marker without the tag number.
func TestWrapHighTagNumbers(t *testing.T) {
	for _, n := range []int{30, 31, 72, 127, 128, 0x4000} {
		for _, b := range [][]byte{asn1util.WrapConstructed(n, []byte{0x05, 0x00}), asn1util.WrapPrimitive(n, []byte{0x01})} {
			raw, rest, err := asn1util.UnmarshalRaw(b)
			if err != nil || len(rest) != 0 || raw.Tag != n {
				t.Errorf("tag %d: %x decodes to tag %d, %v", n, b, raw.Tag, err)
			}
		}
	}
}
