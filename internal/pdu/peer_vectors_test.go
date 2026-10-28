// SPDX-License-Identifier: MIT

package pdu

import (
	"bytes"
	"encoding/hex"
	"testing"
	"time"
)

// Vectors in this file were captured from libiec61850 v1.6.1 (the
// mms-interop reference peer). Unlike the golden files, which pin go-mms's
// own output, they pin go-mms against an independent implementation.

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// ReadJournal-Response body (inside [65]) for one entry: entryID 3,
// occurrenceTime 2024-01-01T00:00:03Z, interop/boolean = false and the
// ReasonCode bit-string with bit 2 set.
const libiec61850JournalResponseHex = "a04a304880080300000000000000a1023000a238800600000bb83912a22ea12c3016800f696e7465726f702f626f6f6c65616ea1038301003012800a526561736f6e436f6465a10484020120"

func libiec61850JournalEntry() JournalEntryWire {
	return JournalEntryWire{
		EntryID:        []byte{3, 0, 0, 0, 0, 0, 0, 0},
		OccurrenceTime: time.Date(2024, 1, 1, 0, 0, 3, 0, time.UTC),
		Variables: []JournalVariableWire{
			{Tag: "interop/boolean", Value: &DataValue{Tag: TagDataBoolean, Bool: false}},
			{Tag: "ReasonCode", Value: &DataValue{Tag: TagDataBitString, Bytes: []byte{0x20}, BitLen: 7}},
		},
	}
}

func TestPeerVector_ReadJournalResponse_Decode(t *testing.T) {
	entries, more, err := UnmarshalReadJournalResponse(mustHex(t, libiec61850JournalResponseHex))
	if err != nil {
		t.Fatalf("decode libiec61850 journal response: %v", err)
	}
	if more {
		t.Error("moreFollows = true, want false (absent)")
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	want := libiec61850JournalEntry()
	got := entries[0]
	if !bytes.Equal(got.EntryID, want.EntryID) || !got.OccurrenceTime.Equal(want.OccurrenceTime) {
		t.Errorf("entry = %x @ %v, want %x @ %v", got.EntryID, got.OccurrenceTime, want.EntryID, want.OccurrenceTime)
	}
	if len(got.Variables) != 2 || got.Variables[0].Tag != "interop/boolean" || got.Variables[1].Tag != "ReasonCode" {
		t.Fatalf("variables = %+v", got.Variables)
	}
	if rc := got.Variables[1].Value; rc.BitLen != 7 || !bytes.Equal(rc.Bytes, []byte{0x20}) {
		t.Errorf("ReasonCode = %x/%d bits, want 20/7", rc.Bytes, rc.BitLen)
	}
}

// originatingApplication is mandatory in JournalEntry; go-mms must emit
// it exactly as libiec61850 does.
func TestPeerVector_ReadJournalResponse_Encode(t *testing.T) {
	got, err := MarshalReadJournalResponse([]JournalEntryWire{libiec61850JournalEntry()}, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := mustHex(t, libiec61850JournalResponseHex); !bytes.Equal(got, want) {
		t.Errorf("journal response\n got %x\nwant %x", got, want)
	}
}

// FileDelete-Request ::= [76] IMPLICIT FileName: the GraphicString is the
// direct content of [76], as mms_client_files.c encodes it.
func TestPeerVector_FileDeleteRequest(t *testing.T) {
	got, err := MarshalFileDeleteRequest(1, "DELETE.ME")
	if err != nil {
		t.Fatal(err)
	}
	want := mustHex(t, "a0110201"+"01"+"bf4c0b"+"1909"+hex.EncodeToString([]byte("DELETE.ME")))
	if !bytes.Equal(got, want) {
		t.Errorf("FileDelete request\n got %x\nwant %x", got, want)
	}

	_, service, err := DecodeConfirmedRequestContent(got[2:])
	if err != nil {
		t.Fatal(err)
	}
	name, err := UnmarshalFileDeleteRequest(service.Bytes)
	if err != nil || name != "DELETE.ME" {
		t.Errorf("server decode = %q, %v", name, err)
	}
}
