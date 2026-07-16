// SPDX-License-Identifier: MIT

package pdu

import (
	"fmt"
	"time"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
)

// Defensive limit for journal entry decoders.
const maxJournalEntries = 10000

// JournalEntryWire is a single journal entry on the wire.
type JournalEntryWire struct {
	EntryID        []byte
	OccurrenceTime time.Time
	Variables      []JournalVariableWire
}

// JournalVariableWire is a single variable inside a journal entry.
type JournalVariableWire struct {
	Tag   string
	Value *DataValue
}

// --- Client-side request marshalers ---

// MarshalReadJournalTimeRange builds a ReadJournal request with a time
// range specification.
//
//	ReadJournalRequest ::= [65] IMPLICIT SEQUENCE {
//	    journalName              [0] IMPLICIT ObjectName,
//	    rangeStartSpecification  [1] IMPLICIT SEQUENCE {
//	        startingTime [0] IMPLICIT TimeOfDay
//	    },
//	    rangeStopSpecification   [2] IMPLICIT SEQUENCE {
//	        endingTime   [0] IMPLICIT TimeOfDay
//	    }
//	}
func MarshalReadJournalTimeRange(invokeID codec.InvokeID, domain, journal string, start, stop time.Time) ([]byte, error) {
	nameBytes, err := encodeJournalName(domain, journal)
	if err != nil {
		return nil, err
	}

	startTime := berutil.EncodeTLV(0x80, encodeBinaryTime(start.UnixMilli()))
	rangeStart := berutil.EncodeTLV(0xa1, startTime)

	stopTime := berutil.EncodeTLV(0x80, encodeBinaryTime(stop.UnixMilli()))
	rangeStop := berutil.EncodeTLV(0xa2, stopTime)

	payload := make([]byte, 0, len(nameBytes)+len(rangeStart)+len(rangeStop))
	payload = append(payload, nameBytes...)
	payload = append(payload, rangeStart...)
	payload = append(payload, rangeStop...)

	return MarshalConfirmedRequest(invokeID, asn1util.TagNumReadJournal, true, payload)
}

// MarshalReadJournalStartAfter builds a ReadJournal request with a
// start-after specification for continuation/paging.
//
//	ReadJournalRequest ::= [65] IMPLICIT SEQUENCE {
//	    journalName          [0] IMPLICIT ObjectName,
//	    entryToStartAfter    [5] IMPLICIT SEQUENCE {
//	        timeSpecification  [0] IMPLICIT TimeOfDay,
//	        entrySpecification [1] IMPLICIT OCTET STRING
//	    }
//	}
func MarshalReadJournalStartAfter(invokeID codec.InvokeID, domain, journal string, afterTime time.Time, afterID []byte) ([]byte, error) {
	nameBytes, err := encodeJournalName(domain, journal)
	if err != nil {
		return nil, err
	}

	timeBin := berutil.EncodeTLV(0x80, encodeBinaryTime(afterTime.UnixMilli()))
	entrySpec := berutil.EncodeTLV(0x81, afterID)
	startAfter := berutil.EncodeTLV(0xa5, append(timeBin, entrySpec...))

	payload := make([]byte, 0, len(nameBytes)+len(startAfter))
	payload = append(payload, nameBytes...)
	payload = append(payload, startAfter...)

	return MarshalConfirmedRequest(invokeID, asn1util.TagNumReadJournal, true, payload)
}

func encodeJournalName(domain, journal string) ([]byte, error) {
	name, err := EncodeObjectName(ObjectNameWire{
		Scope:    ScopeDomain,
		DomainID: domain,
		ItemID:   journal,
	})
	if err != nil {
		return nil, fmt.Errorf("pdu: journal name: %w", err)
	}
	return berutil.EncodeTLV(0xa0, name), nil
}

// --- Server-side request unmarshalers ---

// ReadJournalRequest holds the parsed ReadJournal request.
type ReadJournalRequest struct {
	Domain  string
	Journal string

	// Time range (both set when IsTimeRange is true)
	IsTimeRange bool
	StartTime   time.Time
	StopTime    time.Time

	// Start-after (both set when IsStartAfter is true)
	IsStartAfter bool
	AfterTime    time.Time
	AfterID      []byte
}

// UnmarshalReadJournalRequest decodes a ReadJournal request body.
func UnmarshalReadJournalRequest(data []byte) (*ReadJournalRequest, error) {
	req := &ReadJournalRequest{}
	offset := 0
	for offset < len(data) {
		tag, inner, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, fmt.Errorf("pdu: read-journal request: %w", err)
		}
		offset += n
		switch tag {
		case 0xa0: // journalName
			name, nameErr := DecodeObjectName(inner)
			if nameErr != nil {
				return nil, fmt.Errorf("pdu: read-journal journalName: %w", nameErr)
			}
			req.Domain = name.DomainID
			req.Journal = name.ItemID
		case 0xa1: // rangeStartSpecification
			t, tErr := decodeTimeField(inner)
			if tErr != nil {
				return nil, fmt.Errorf("pdu: read-journal rangeStart: %w", tErr)
			}
			req.StartTime = t
			req.IsTimeRange = true
		case 0xa2: // rangeStopSpecification
			t, tErr := decodeTimeField(inner)
			if tErr != nil {
				return nil, fmt.Errorf("pdu: read-journal rangeStop: %w", tErr)
			}
			req.StopTime = t
		case 0xa5: // entryToStartAfter
			if err := decodeStartAfter(inner, req); err != nil {
				return nil, fmt.Errorf("pdu: read-journal startAfter: %w", err)
			}
			req.IsStartAfter = true
		default:
			return nil, fmt.Errorf("pdu: unexpected tag 0x%02x in UnmarshalReadJournalRequest", tag)
		}
	}
	if req.Journal == "" {
		return nil, fmt.Errorf("pdu: read-journal request: missing journalName")
	}

	// Enforce exactly one valid mode.
	switch {
	case req.IsTimeRange && req.IsStartAfter:
		return nil, fmt.Errorf("pdu: read-journal request: both time-range and start-after present")
	case req.IsTimeRange:
		if req.StartTime.IsZero() || req.StopTime.IsZero() {
			return nil, fmt.Errorf("pdu: read-journal request: time-range requires both start and stop")
		}
	case req.IsStartAfter:
		// decodeStartAfter already validates required fields
	default:
		return nil, fmt.Errorf("pdu: read-journal request: missing range specification")
	}

	return req, nil
}

func decodeTimeField(data []byte) (time.Time, error) {
	offset := 0
	for offset < len(data) {
		tag, inner, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return time.Time{}, err
		}
		offset += n
		if tag == 0x80 { // TimeOfDay
			ms, msErr := decodeBinaryTime(inner)
			if msErr != nil {
				return time.Time{}, msErr
			}
			return time.UnixMilli(ms).UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("missing time value")
}

func decodeStartAfter(data []byte, req *ReadJournalRequest) error {
	hasTime := false
	hasEntry := false
	offset := 0
	for offset < len(data) {
		tag, inner, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return err
		}
		offset += n
		switch tag {
		case 0x80: // timeSpecification
			ms, msErr := decodeBinaryTime(inner)
			if msErr != nil {
				return msErr
			}
			req.AfterTime = time.UnixMilli(ms).UTC()
			hasTime = true
		case 0x81: // entrySpecification
			req.AfterID = append([]byte(nil), inner...)
			hasEntry = true
		default:
			return fmt.Errorf("pdu: unexpected tag 0x%02x in decodeStartAfter", tag)
		}
	}
	if !hasTime {
		return fmt.Errorf("missing timeSpecification")
	}
	if !hasEntry {
		return fmt.Errorf("missing entrySpecification")
	}
	return nil
}

// --- Server-side response marshalers ---

// MarshalReadJournalResponse encodes a ReadJournal response.
//
//	ReadJournalResponse ::= SEQUENCE {
//	    listOfJournalEntry [0] IMPLICIT SEQUENCE OF JournalEntry,
//	    moreFollows        [1] IMPLICIT BOOLEAN OPTIONAL
//	}
func MarshalReadJournalResponse(entries []JournalEntryWire, moreFollows bool) ([]byte, error) {
	var entriesBytes []byte
	for _, e := range entries {
		eb, err := marshalJournalEntry(e)
		if err != nil {
			return nil, err
		}
		entriesBytes = append(entriesBytes, eb...)
	}

	listBytes := berutil.EncodeTLV(0xa0, entriesBytes)
	payload := listBytes
	if moreFollows {
		payload = append(payload, berutil.EncodeTLV(0x81, []byte{0xff})...)
	}
	return payload, nil
}

func marshalJournalEntry(e JournalEntryWire) ([]byte, error) {
	entryIDBytes := berutil.EncodeTLV(0x80, e.EntryID)

	occurrenceTimeBytes := berutil.EncodeTLV(0x80, encodeBinaryTime(e.OccurrenceTime.UnixMilli()))

	var varBytes []byte
	for _, v := range e.Variables {
		vb, err := marshalJournalVariable(v)
		if err != nil {
			return nil, err
		}
		varBytes = append(varBytes, vb...)
	}
	journalVars := berutil.EncodeTLV(0xa1, varBytes)
	dataBody := berutil.EncodeTLV(0xa2, journalVars)
	entryContent := berutil.EncodeTLV(0xa2, append(occurrenceTimeBytes, dataBody...))

	entryInner := make([]byte, 0, len(entryIDBytes)+len(entryContent))
	entryInner = append(entryInner, entryIDBytes...)
	entryInner = append(entryInner, entryContent...)

	return berutil.EncodeTLV(0x30, entryInner), nil
}

func marshalJournalVariable(v JournalVariableWire) ([]byte, error) {
	tagBytes := berutil.EncodeTLV(0x80, []byte(v.Tag))

	valueBytes, err := MarshalData(v.Value)
	if err != nil {
		return nil, fmt.Errorf("pdu: journal variable %q: %w", v.Tag, err)
	}
	valueSpec := berutil.EncodeTLV(0xa1, valueBytes)

	inner := append(tagBytes, valueSpec...)
	return berutil.EncodeTLV(0x30, inner), nil
}

// --- Client-side response unmarshalers ---

// UnmarshalReadJournalResponse decodes a ReadJournal response.
func UnmarshalReadJournalResponse(data []byte) ([]JournalEntryWire, bool, error) {
	var entries []JournalEntryWire
	moreFollows := false
	offset := 0
	for offset < len(data) {
		tag, inner, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, false, fmt.Errorf("pdu: read-journal response: %w", err)
		}
		offset += n
		switch tag {
		case 0xa0: // listOfJournalEntry
			parsed, parseErr := parseJournalEntries(inner)
			if parseErr != nil {
				return nil, false, parseErr
			}
			entries = parsed
		case 0x81: // moreFollows
			if len(inner) > 0 {
				moreFollows = inner[0] != 0
			}
		default:
			return nil, false, fmt.Errorf("pdu: unexpected tag 0x%02x in UnmarshalReadJournalResponse", tag)
		}
	}
	return entries, moreFollows, nil
}

func parseJournalEntries(data []byte) ([]JournalEntryWire, error) {
	var entries []JournalEntryWire
	offset := 0
	for offset < len(data) {
		tag, inner, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, fmt.Errorf("pdu: journal entry: %w", err)
		}
		if tag != 0x30 {
			return nil, fmt.Errorf("pdu: journal entry: expected SEQUENCE (0x30), got 0x%02x", tag)
		}
		offset += n

		e, eErr := parseJournalEntry(inner)
		if eErr != nil {
			return nil, eErr
		}
		entries = append(entries, e)
		if len(entries) > maxJournalEntries {
			return nil, fmt.Errorf("pdu: journal entries %d exceeds maximum %d", len(entries), maxJournalEntries)
		}
	}
	return entries, nil
}

func parseJournalEntry(data []byte) (JournalEntryWire, error) {
	var e JournalEntryWire
	offset := 0
	for offset < len(data) {
		tag, inner, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return e, fmt.Errorf("pdu: journal entry field: %w", err)
		}
		offset += n
		switch tag {
		case 0x80: // entryID
			e.EntryID = append([]byte(nil), inner...)
		case 0xa2: // entryContent
			if err := parseEntryContent(inner, &e); err != nil {
				return e, err
			}
		default:
			return e, fmt.Errorf("pdu: unexpected tag 0x%02x in parseJournalEntry", tag)
		}
	}
	if len(e.EntryID) == 0 {
		return e, fmt.Errorf("pdu: journal entry: missing entryID")
	}
	return e, nil
}

func parseEntryContent(data []byte, e *JournalEntryWire) error {
	hasTime := false
	hasData := false
	offset := 0
	for offset < len(data) {
		tag, inner, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return fmt.Errorf("pdu: entry content: %w", err)
		}
		offset += n
		switch tag {
		case 0x80: // occurrenceTime
			ms, msErr := decodeBinaryTime(inner)
			if msErr != nil {
				return fmt.Errorf("pdu: journal occurrenceTime: %w", msErr)
			}
			e.OccurrenceTime = time.UnixMilli(ms).UTC()
			hasTime = true
		case 0xa2: // data -> journalVariables
			vars, vErr := parseJournalData(inner)
			if vErr != nil {
				return vErr
			}
			e.Variables = vars
			hasData = true
		default:
			return fmt.Errorf("pdu: unexpected tag 0x%02x in parseEntryContent", tag)
		}
	}
	if !hasTime {
		return fmt.Errorf("pdu: journal entry: missing occurrenceTime")
	}
	if !hasData {
		return fmt.Errorf("pdu: journal entry: missing data block")
	}
	return nil
}

func parseJournalData(data []byte) ([]JournalVariableWire, error) {
	offset := 0
	for offset < len(data) {
		tag, inner, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, fmt.Errorf("pdu: journal data: %w", err)
		}
		offset += n
		if tag == 0xa1 { // journalVariables
			return parseJournalVariables(inner)
		}
	}
	return nil, nil
}

func parseJournalVariables(data []byte) ([]JournalVariableWire, error) {
	var vars []JournalVariableWire
	offset := 0
	for offset < len(data) {
		tag, inner, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, fmt.Errorf("pdu: journal variable: %w", err)
		}
		if tag != 0x30 {
			return nil, fmt.Errorf("pdu: journal variable: expected SEQUENCE (0x30), got 0x%02x", tag)
		}
		offset += n

		v, vErr := parseJournalVariable(inner)
		if vErr != nil {
			return nil, vErr
		}
		vars = append(vars, v)
	}
	return vars, nil
}

func parseJournalVariable(data []byte) (JournalVariableWire, error) {
	var v JournalVariableWire
	hasTag := false
	hasValue := false
	offset := 0
	for offset < len(data) {
		tag, inner, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return v, fmt.Errorf("pdu: journal variable field: %w", err)
		}
		offset += n
		switch tag {
		case 0x80: // variableTag
			v.Tag = string(inner)
			hasTag = true
		case 0xa1: // valueSpec
			dv, _, dvErr := UnmarshalDataElement(inner, 0)
			if dvErr != nil {
				return v, fmt.Errorf("pdu: journal variable value: %w", dvErr)
			}
			v.Value = dv
			hasValue = true
		default:
			return v, fmt.Errorf("pdu: unexpected tag 0x%02x in parseJournalVariable", tag)
		}
	}
	if !hasTag {
		return v, fmt.Errorf("pdu: journal variable: missing tag")
	}
	if !hasValue {
		return v, fmt.Errorf("pdu: journal variable: missing value")
	}
	return v, nil
}
