// SPDX-License-Identifier: MIT

package pdu

import (
	"encoding/asn1"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
)

func parseJournalConfirmedRequest(t *testing.T, data []byte) (codec.InvokeID, []byte) {
	t.Helper()
	var raw asn1.RawValue
	rest, err := asn1.Unmarshal(data, &raw)
	if err != nil {
		t.Fatalf("unmarshal outer: %v", err)
	}
	if len(rest) != 0 {
		t.Fatalf("trailing bytes: %d", len(rest))
	}
	invokeID, serviceRaw, err := codec.UnmarshalConfirmedRequest(raw.Bytes)
	if err != nil {
		t.Fatalf("unmarshal confirmed request: %v", err)
	}
	if serviceRaw.Tag != asn1util.TagNumReadJournal {
		t.Fatalf("service tag = %d, want %d (ReadJournal)", serviceRaw.Tag, asn1util.TagNumReadJournal)
	}
	return invokeID, serviceRaw.Bytes
}

func TestUnmarshalReadJournalRequest_TimeRange(t *testing.T) {
	start := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	stop := time.Date(2025, 6, 15, 11, 0, 0, 0, time.UTC)

	reqBytes, err := MarshalReadJournalTimeRange(1, "TestDomain", "EventLog", start, stop)
	if err != nil {
		t.Fatalf("MarshalReadJournalTimeRange: %v", err)
	}

	invokeID, body := parseJournalConfirmedRequest(t, reqBytes)
	if invokeID != 1 {
		t.Errorf("invokeID = %d, want 1", invokeID)
	}

	req, err := UnmarshalReadJournalRequest(body)
	if err != nil {
		t.Fatalf("UnmarshalReadJournalRequest: %v", err)
	}
	if req.Domain != "TestDomain" {
		t.Errorf("Domain = %q, want TestDomain", req.Domain)
	}
	if req.Journal != "EventLog" {
		t.Errorf("Journal = %q, want EventLog", req.Journal)
	}
	if !req.IsTimeRange {
		t.Error("expected IsTimeRange=true")
	}
	if req.IsStartAfter {
		t.Error("expected IsStartAfter=false")
	}
}

func TestUnmarshalReadJournalRequest_StartAfter(t *testing.T) {
	afterTime := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	afterID := []byte{0x01, 0x02, 0x03}

	reqBytes, err := MarshalReadJournalStartAfter(1, "TestDomain", "EventLog", afterTime, afterID)
	if err != nil {
		t.Fatalf("MarshalReadJournalStartAfter: %v", err)
	}

	_, body := parseJournalConfirmedRequest(t, reqBytes)

	req, err := UnmarshalReadJournalRequest(body)
	if err != nil {
		t.Fatalf("UnmarshalReadJournalRequest: %v", err)
	}
	if !req.IsStartAfter {
		t.Error("expected IsStartAfter=true")
	}
	if req.IsTimeRange {
		t.Error("expected IsTimeRange=false")
	}
	if string(req.AfterID) != string(afterID) {
		t.Errorf("AfterID = %v, want %v", req.AfterID, afterID)
	}
}

func TestMarshalReadJournalResponse_RoundTrip(t *testing.T) {
	entries := []JournalEntryWire{
		{
			EntryID:        []byte{0x01},
			OccurrenceTime: time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
			Variables: []JournalVariableWire{
				{Tag: "Value", Value: &DataValue{Tag: TagDataInteger, Int: 42}},
			},
		},
		{
			EntryID:        []byte{0x02},
			OccurrenceTime: time.Date(2025, 6, 15, 10, 1, 0, 0, time.UTC),
			Variables: []JournalVariableWire{
				{Tag: "Status", Value: &DataValue{Tag: TagDataBoolean, Bool: true}},
			},
		},
	}

	payload, err := MarshalReadJournalResponse(entries, true)
	if err != nil {
		t.Fatalf("MarshalReadJournalResponse: %v", err)
	}

	parsed, moreFollows, err := UnmarshalReadJournalResponse(payload)
	if err != nil {
		t.Fatalf("UnmarshalReadJournalResponse: %v", err)
	}
	if !moreFollows {
		t.Error("expected moreFollows=true")
	}
	if len(parsed) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(parsed))
	}
	if string(parsed[0].EntryID) != string([]byte{0x01}) {
		t.Errorf("entry[0].EntryID = %v, want [0x01]", parsed[0].EntryID)
	}
	if len(parsed[0].Variables) != 1 {
		t.Fatalf("entry[0] variables: got %d, want 1", len(parsed[0].Variables))
	}
	if parsed[0].Variables[0].Tag != "Value" {
		t.Errorf("entry[0].Variables[0].Tag = %q, want Value", parsed[0].Variables[0].Tag)
	}
	if parsed[1].Variables[0].Tag != "Status" {
		t.Errorf("entry[1].Variables[0].Tag = %q, want Status", parsed[1].Variables[0].Tag)
	}
}

func TestMarshalReadJournalResponse_Empty(t *testing.T) {
	payload, err := MarshalReadJournalResponse(nil, false)
	if err != nil {
		t.Fatalf("MarshalReadJournalResponse: %v", err)
	}

	parsed, moreFollows, err := UnmarshalReadJournalResponse(payload)
	if err != nil {
		t.Fatalf("UnmarshalReadJournalResponse: %v", err)
	}
	if moreFollows {
		t.Error("expected moreFollows=false")
	}
	if len(parsed) != 0 {
		t.Errorf("expected 0 entries, got %d", len(parsed))
	}
}

func TestUnmarshalReadJournalRequest_MissingName(t *testing.T) {
	_, err := UnmarshalReadJournalRequest([]byte{})
	if err == nil {
		t.Fatal("expected error for empty request body")
	}
}

func TestUnmarshalReadJournalRequest_MissingRange(t *testing.T) {
	// journalName only, no range specification at all.
	name := encodeDomainObjectName(t, "D", "J")
	nameWrapped := berutil.EncodeTLV(0xa0, name)
	_, err := UnmarshalReadJournalRequest(nameWrapped)
	if err == nil {
		t.Fatal("expected error for request with no range specification")
	}
}

func TestUnmarshalReadJournalRequest_BothRangeAndStartAfter(t *testing.T) {
	name := encodeDomainObjectName(t, "D", "J")
	nameWrapped := berutil.EncodeTLV(0xa0, name)

	startTime := berutil.EncodeTLV(0x80, encodeBinaryTime(time.Now().UnixMilli()))
	rangeStart := berutil.EncodeTLV(0xa1, startTime)
	rangeStop := berutil.EncodeTLV(0xa2, startTime)

	entryAfterTime := berutil.EncodeTLV(0x80, encodeBinaryTime(time.Now().UnixMilli()))
	entryAfterID := berutil.EncodeTLV(0x81, []byte{0x01})
	startAfter := berutil.EncodeTLV(0xa5, append(entryAfterTime, entryAfterID...))

	var body []byte
	body = append(body, nameWrapped...)
	body = append(body, rangeStart...)
	body = append(body, rangeStop...)
	body = append(body, startAfter...)

	_, err := UnmarshalReadJournalRequest(body)
	if err == nil {
		t.Fatal("expected error when both range and start-after present")
	}
}

func TestUnmarshalReadJournalRequest_StartAfterMissingID(t *testing.T) {
	name := encodeDomainObjectName(t, "D", "J")
	nameWrapped := berutil.EncodeTLV(0xa0, name)

	entryAfterTime := berutil.EncodeTLV(0x80, encodeBinaryTime(time.Now().UnixMilli()))
	// No entrySpecification [0x81]
	startAfter := berutil.EncodeTLV(0xa5, entryAfterTime)

	body := append(nameWrapped, startAfter...)
	_, err := UnmarshalReadJournalRequest(body)
	if err == nil {
		t.Fatal("expected error for start-after missing entrySpecification")
	}
}

func TestUnmarshalReadJournalRequest_StartAfterMissingTime(t *testing.T) {
	name := encodeDomainObjectName(t, "D", "J")
	nameWrapped := berutil.EncodeTLV(0xa0, name)

	entryAfterID := berutil.EncodeTLV(0x81, []byte{0x01})
	// No timeSpecification [0x80]
	startAfter := berutil.EncodeTLV(0xa5, entryAfterID)

	body := append(nameWrapped, startAfter...)
	_, err := UnmarshalReadJournalRequest(body)
	if err == nil {
		t.Fatal("expected error for start-after missing timeSpecification")
	}
}

func TestJournalEntryMissingEntryID(t *testing.T) {
	fakeContent := []byte{0xa2, 0x00}
	entrySeq := append([]byte{0x30, byte(len(fakeContent))}, fakeContent...)
	listPayload := append([]byte{0xa0, byte(len(entrySeq))}, entrySeq...)

	_, _, err := UnmarshalReadJournalResponse(listPayload)
	if err == nil {
		t.Fatal("expected error for entry missing entryID")
	}
}

func TestJournalEntryMissingOccurrenceTime(t *testing.T) {
	// Entry with entryID but no occurrenceTime in content.
	entryID := berutil.EncodeTLV(0x80, []byte{0x01})
	// entryContent with only data block, no time.
	journalVars := berutil.EncodeTLV(0xa1, []byte{})
	dataBlock := berutil.EncodeTLV(0xa2, journalVars)
	entryContent := berutil.EncodeTLV(0xa2, dataBlock)

	entryInner := append(entryID, entryContent...)
	entrySeq := berutil.EncodeTLV(0x30, entryInner)
	listPayload := berutil.EncodeTLV(0xa0, entrySeq)

	_, _, err := UnmarshalReadJournalResponse(listPayload)
	if err == nil {
		t.Fatal("expected error for entry missing occurrenceTime")
	}
}

func TestJournalEntryMissingDataBlock(t *testing.T) {
	entryID := berutil.EncodeTLV(0x80, []byte{0x01})
	// entryContent with only time, no data block.
	occTime := berutil.EncodeTLV(0x80, encodeBinaryTime(time.Now().UnixMilli()))
	entryContent := berutil.EncodeTLV(0xa2, occTime)

	entryInner := append(entryID, entryContent...)
	entrySeq := berutil.EncodeTLV(0x30, entryInner)
	listPayload := berutil.EncodeTLV(0xa0, entrySeq)

	_, _, err := UnmarshalReadJournalResponse(listPayload)
	if err == nil {
		t.Fatal("expected error for entry missing data block")
	}
}

func TestJournalVariableMissingTag(t *testing.T) {
	// Variable with only valueSpec, no variableTag.
	boolVal := berutil.EncodeTLV(TagDataBoolean, []byte{0xff})
	valueSpec := berutil.EncodeTLV(0xa1, boolVal)
	varSeq := berutil.EncodeTLV(0x30, valueSpec)

	journalVars := berutil.EncodeTLV(0xa1, varSeq)
	dataBlock := berutil.EncodeTLV(0xa2, journalVars)
	occTime := berutil.EncodeTLV(0x80, encodeBinaryTime(time.Now().UnixMilli()))
	entryContent := berutil.EncodeTLV(0xa2, append(occTime, dataBlock...))
	entryID := berutil.EncodeTLV(0x80, []byte{0x01})
	entryInner := append(entryID, entryContent...)
	entrySeq := berutil.EncodeTLV(0x30, entryInner)
	listPayload := berutil.EncodeTLV(0xa0, entrySeq)

	_, _, err := UnmarshalReadJournalResponse(listPayload)
	if err == nil {
		t.Fatal("expected error for variable missing tag")
	}
}

func TestJournalVariableMissingValue(t *testing.T) {
	// Variable with only variableTag, no valueSpec.
	varTag := berutil.EncodeTLV(0x80, []byte("TestVar"))
	varSeq := berutil.EncodeTLV(0x30, varTag)

	journalVars := berutil.EncodeTLV(0xa1, varSeq)
	dataBlock := berutil.EncodeTLV(0xa2, journalVars)
	occTime := berutil.EncodeTLV(0x80, encodeBinaryTime(time.Now().UnixMilli()))
	entryContent := berutil.EncodeTLV(0xa2, append(occTime, dataBlock...))
	entryID := berutil.EncodeTLV(0x80, []byte{0x01})
	entryInner := append(entryID, entryContent...)
	entrySeq := berutil.EncodeTLV(0x30, entryInner)
	listPayload := berutil.EncodeTLV(0xa0, entrySeq)

	_, _, err := UnmarshalReadJournalResponse(listPayload)
	if err == nil {
		t.Fatal("expected error for variable missing value")
	}
}

func TestParseJournalEntries_TooMany(t *testing.T) {
	now := time.Now().UTC()
	entries := make([]JournalEntryWire, maxJournalEntries+1)
	for i := range entries {
		entries[i] = JournalEntryWire{
			EntryID:        []byte{byte(i), byte(i >> 8)},
			OccurrenceTime: now,
			Variables: []JournalVariableWire{
				{Tag: "v", Value: &DataValue{Tag: TagDataBoolean, Bool: true}},
			},
		}
	}

	payload, err := MarshalReadJournalResponse(entries, false)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	_, _, err = UnmarshalReadJournalResponse(payload)
	if err == nil {
		t.Fatal("expected error for too many journal entries")
	}
}

func encodeDomainObjectName(t *testing.T, domain, item string) []byte {
	t.Helper()
	b, err := EncodeObjectName(ObjectNameWire{
		Scope:    ScopeDomain,
		DomainID: domain,
		ItemID:   item,
	})
	if err != nil {
		t.Fatalf("EncodeObjectName: %v", err)
	}
	return b
}
