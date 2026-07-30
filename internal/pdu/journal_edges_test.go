// SPDX-License-Identifier: MIT

package pdu

import (
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/berutil"
)

func TestJournalMarshal_ErrorPaths(t *testing.T) {
	now := time.Unix(1, 0).UTC()
	if _, err := MarshalReadJournalTimeRange(1, "D", "", now, now); err == nil {
		t.Fatal("empty journal time-range")
	}
	if _, err := MarshalReadJournalStartAfter(1, "D", "", now, []byte{1}); err == nil {
		t.Fatal("empty journal start-after")
	}
	if _, err := encodeJournalName("D", ""); err == nil {
		t.Fatal("encodeJournalName empty")
	}

	// Unsupported Data tag fails marshalJournalVariable → entry → response.
	bad := []JournalEntryWire{{
		EntryID:        []byte{1},
		OccurrenceTime: now,
		Variables: []JournalVariableWire{
			{Tag: "x", Value: &DataValue{Tag: 0xfe}},
		},
	}}
	if _, err := MarshalReadJournalResponse(bad, false); err == nil {
		t.Fatal("expected marshal variable error")
	}
	if _, err := marshalJournalEntry(bad[0]); err == nil {
		t.Fatal("expected entry marshal error")
	}
	if _, err := marshalJournalVariable(bad[0].Variables[0]); err == nil {
		t.Fatal("expected variable marshal error")
	}
}

func TestUnmarshalReadJournalRequest_Edges(t *testing.T) {
	name := encodeDomainObjectName(t, "D", "J")
	nameWrapped := berutil.EncodeTLV(0xa0, name)

	if _, err := UnmarshalReadJournalRequest([]byte{0xa0, 0x05}); err == nil {
		t.Fatal("truncated TLV")
	}
	if _, err := UnmarshalReadJournalRequest(berutil.EncodeTLV(0xa0, []byte{0xff})); err == nil {
		t.Fatal("bad journalName")
	}
	if _, err := UnmarshalReadJournalRequest(berutil.EncodeTLV(0xa9, []byte{1})); err == nil {
		t.Fatal("unexpected tag")
	}

	// rangeStart with bad time / missing time / truncated.
	badStart := append(nameWrapped, berutil.EncodeTLV(0xa1, []byte{0xff})...)
	if _, err := UnmarshalReadJournalRequest(badStart); err == nil {
		t.Fatal("bad rangeStart TLV")
	}
	emptyStart := append(nameWrapped, berutil.EncodeTLV(0xa1, nil)...)
	emptyStart = append(emptyStart, berutil.EncodeTLV(0xa2, berutil.EncodeTLV(0x80, encodeBinaryTime(1)))...)
	if _, err := UnmarshalReadJournalRequest(emptyStart); err == nil {
		t.Fatal("missing time in rangeStart")
	}
	badBin := append(nameWrapped, berutil.EncodeTLV(0xa1, berutil.EncodeTLV(0x80, []byte{1, 2, 3}))...)
	badBin = append(badBin, berutil.EncodeTLV(0xa2, berutil.EncodeTLV(0x80, encodeBinaryTime(1)))...)
	if _, err := UnmarshalReadJournalRequest(badBin); err == nil {
		t.Fatal("bad binary time in rangeStart")
	}

	// Only start, no stop.
	onlyStart := append(nameWrapped, berutil.EncodeTLV(0xa1, berutil.EncodeTLV(0x80, encodeBinaryTime(1)))...)
	if _, err := UnmarshalReadJournalRequest(onlyStart); err == nil {
		t.Fatal("time-range requires stop")
	}

	// Bad rangeStop.
	okStart := berutil.EncodeTLV(0xa1, berutil.EncodeTLV(0x80, encodeBinaryTime(1)))
	badStop := append(append([]byte{}, nameWrapped...), okStart...)
	badStop = append(badStop, berutil.EncodeTLV(0xa2, berutil.EncodeTLV(0x80, []byte{9}))...)
	if _, err := UnmarshalReadJournalRequest(badStop); err == nil {
		t.Fatal("bad rangeStop")
	}

	// startAfter: truncated, unexpected tag, bad binary time.
	if _, err := UnmarshalReadJournalRequest(append(nameWrapped, berutil.EncodeTLV(0xa5, []byte{0x80, 0x05})...)); err == nil {
		t.Fatal("truncated startAfter")
	}
	badSATag := berutil.EncodeTLV(0xa5, berutil.EncodeTLV(0x82, []byte{1}))
	if _, err := UnmarshalReadJournalRequest(append(nameWrapped, badSATag...)); err == nil {
		t.Fatal("unexpected startAfter tag")
	}
	badSATime := berutil.EncodeTLV(0xa5, append(
		berutil.EncodeTLV(0x80, []byte{1}),
		berutil.EncodeTLV(0x81, []byte{1})...,
	))
	if _, err := UnmarshalReadJournalRequest(append(nameWrapped, badSATime...)); err == nil {
		t.Fatal("bad startAfter time")
	}
}

func TestDecodeTimeField_Direct(t *testing.T) {
	if _, err := decodeTimeField([]byte{0x80, 0x05}); err == nil {
		t.Fatal("truncated")
	}
	if _, err := decodeTimeField(berutil.EncodeTLV(0x81, []byte{1})); err == nil {
		t.Fatal("missing 0x80")
	}
	if _, err := decodeTimeField(berutil.EncodeTLV(0x80, []byte{1, 2})); err == nil {
		t.Fatal("bad binary time")
	}
	wantMS := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC).UnixMilli()
	got, err := decodeTimeField(berutil.EncodeTLV(0x80, encodeBinaryTime(wantMS)))
	if err != nil || got.UnixMilli() != wantMS {
		t.Fatalf("got %v wantMS=%d err=%v", got, wantMS, err)
	}
}

func TestUnmarshalReadJournalResponse_Edges(t *testing.T) {
	if _, _, err := UnmarshalReadJournalResponse([]byte{0xa0, 0x05}); err == nil {
		t.Fatal("truncated")
	}
	if _, _, err := UnmarshalReadJournalResponse(berutil.EncodeTLV(0xa9, []byte{1})); err == nil {
		t.Fatal("unexpected tag")
	}
	// moreFollows empty content → false
	payload := append(berutil.EncodeTLV(0xa0, nil), berutil.EncodeTLV(0x81, nil)...)
	_, more, err := UnmarshalReadJournalResponse(payload)
	if err != nil || more {
		t.Fatalf("more=%v err=%v", more, err)
	}
}

func TestParseJournalEntries_Edges(t *testing.T) {
	if _, err := parseJournalEntries([]byte{0x30, 0x05}); err == nil {
		t.Fatal("truncated entry")
	}
	if _, err := parseJournalEntries(berutil.EncodeTLV(0x31, nil)); err == nil {
		t.Fatal("wrong entry tag")
	}
}

func TestParseJournalEntry_Edges(t *testing.T) {
	if _, err := parseJournalEntry([]byte{0x80, 0x05}); err == nil {
		t.Fatal("truncated")
	}
	if _, err := parseJournalEntry(berutil.EncodeTLV(0x81, []byte{1})); err == nil {
		t.Fatal("unexpected tag")
	}
	// entryContent present but invalid (no time/data) with no entryID.
	if _, err := parseJournalEntry(berutil.EncodeTLV(0xa2, nil)); err == nil {
		t.Fatal("missing entryID after empty content")
	}
	// entryID empty bytes still counts as missing (len==0).
	if _, err := parseJournalEntry(berutil.EncodeTLV(0x80, nil)); err == nil {
		t.Fatal("empty entryID")
	}
}

func TestParseEntryContent_Edges(t *testing.T) {
	var e JournalEntryWire
	if err := parseEntryContent([]byte{0x80, 0x05}, &e); err == nil {
		t.Fatal("truncated")
	}
	if err := parseEntryContent(berutil.EncodeTLV(0x80, []byte{1}), &e); err == nil {
		t.Fatal("bad binary time")
	}
	if err := parseEntryContent(berutil.EncodeTLV(0x81, []byte{1}), &e); err == nil {
		t.Fatal("unexpected tag")
	}
	// Bad journal data inside 0xa2.
	occ := berutil.EncodeTLV(0x80, encodeBinaryTime(1))
	badData := berutil.EncodeTLV(0xa2, []byte{0xa1, 0x05})
	if err := parseEntryContent(append(occ, badData...), &e); err == nil {
		t.Fatal("bad journal data")
	}
}

func TestParseJournalDataAndVariables_Edges(t *testing.T) {
	if _, err := parseJournalData([]byte{0xa1, 0x05}); err == nil {
		t.Fatal("truncated journal data")
	}
	// No 0xa1 → empty vars, nil error.
	got, err := parseJournalData(berutil.EncodeTLV(0xa2, nil))
	if err != nil || got != nil {
		t.Fatalf("got %v err=%v", got, err)
	}

	if _, err := parseJournalVariables([]byte{0x30, 0x05}); err == nil {
		t.Fatal("truncated variable seq")
	}
	if _, err := parseJournalVariables(berutil.EncodeTLV(0x31, nil)); err == nil {
		t.Fatal("wrong variable tag")
	}

	if _, err := parseJournalVariable([]byte{0x80, 0x05}); err == nil {
		t.Fatal("truncated variable field")
	}
	if _, err := parseJournalVariable(berutil.EncodeTLV(0x82, []byte{1})); err == nil {
		t.Fatal("unexpected variable tag")
	}
	badVal := append(
		berutil.EncodeTLV(0x80, []byte("t")),
		berutil.EncodeTLV(0xa1, []byte{0xfe, 0x01, 0x00})...,
	)
	if _, err := parseJournalVariable(badVal); err == nil {
		t.Fatal("bad valueSpec")
	}
}
