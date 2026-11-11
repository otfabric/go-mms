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

	// Annotation is the entryForm annotation [3] MMSString alternative
	// (ISO 9506-2 clause 23.8.1 EntryContent); HasAnnotation selects it
	// instead of data [2]. The two forms are exclusive.
	HasAnnotation bool
	Annotation    string
}

// EncodeEntryTime encodes t as an IEC 61850-8-1 EntryTime: a TimeOfDay
// of exactly six octets (8-1 clause 8.1.3.8, "The size … shall be six (6)
// octets"). The generic TimeOfDay encoder emits the four-octet form for
// times below 2^28 ms after the Unix epoch, which a 61850 peer rejects;
// times before the 1984-01-01 epoch of the six-octet form are clamped to
// it, so time.Unix(0, 0) keeps meaning "from the oldest entry".
func EncodeEntryTime(t time.Time) ([]byte, error) {
	ms := t.UnixMilli()
	if ms < binaryTimeMinMs {
		ms = binaryTimeMinMs
	}
	return encodeBinaryTimeChecked(ms)
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

	startBin, err := EncodeEntryTime(start)
	if err != nil {
		return nil, fmt.Errorf("start time: %w", err)
	}
	stopBin, err := EncodeEntryTime(stop)
	if err != nil {
		return nil, fmt.Errorf("stop time: %w", err)
	}
	rangeStart := berutil.EncodeTLV(0xa1, berutil.EncodeTLV(0x80, startBin))
	rangeStop := berutil.EncodeTLV(0xa2, berutil.EncodeTLV(0x80, stopBin))

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

	afterBin, err := EncodeEntryTime(afterTime)
	if err != nil {
		return nil, fmt.Errorf("after time: %w", err)
	}
	timeBin := berutil.EncodeTLV(0x80, afterBin)
	entrySpec := berutil.EncodeTLV(0x81, afterID)
	startAfter := berutil.EncodeTLV(0xa5, append(timeBin, entrySpec...))

	payload := make([]byte, 0, len(nameBytes)+len(startAfter))
	payload = append(payload, nameBytes...)
	payload = append(payload, startAfter...)

	return MarshalConfirmedRequest(invokeID, asn1util.TagNumReadJournal, true, payload)
}

func encodeJournalName(domain, journal string) ([]byte, error) {
	if journal == "" {
		return nil, fmt.Errorf("pdu: journal name: empty journal identifier")
	}
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

// ReadJournalRequest holds the parsed ReadJournal request
// (ISO 9506-2 clause 23.2):
//
//	ReadJournal-Request ::= SEQUENCE {
//	  journalName             [0] ObjectName,
//	  rangeStartSpecification [1] CHOICE { startingTime [0] IMPLICIT TimeOfDay,
//	                                       startingEntry [1] IMPLICIT OCTET STRING } OPTIONAL,
//	  rangeStopSpecification  [2] CHOICE { endingTime [0] IMPLICIT TimeOfDay,
//	                                       numberOfEntries [1] IMPLICIT Integer32 } OPTIONAL,
//	  listOfVariables         [4] IMPLICIT SEQUENCE OF VisibleString OPTIONAL,
//	  entryToStartAfter       [5] IMPLICIT SEQUENCE { timeSpecification [0] IMPLICIT TimeOfDay,
//	                                                  entrySpecification [1] IMPLICIT OCTET STRING } OPTIONAL
//	}
//
// IEC 61850-8-1 Table 124 makes every option mandatory for a server; the
// start and stop specifications are independent (MMS-PDU-12).
type ReadJournalRequest struct {
	Domain  string
	Journal string

	// IsTimeRange is set when both startingTime and endingTime are present.
	IsTimeRange bool
	StartTime   time.Time // startingTime, valid when HasStartTime
	StopTime    time.Time // endingTime, valid when HasStopTime

	HasStartTime       bool
	HasStartEntry      bool
	StartEntry         []byte // startingEntry
	HasStopTime        bool
	HasNumberOfEntries bool
	NumberOfEntries    int
	ListOfVariables    []string // nil when absent

	// Start-after (both set when IsStartAfter is true)
	IsStartAfter bool
	AfterTime    time.Time
	AfterID      []byte
}

// UnmarshalReadJournalRequest decodes a ReadJournal request body. The
// components must appear in tag order, each at most once.
func UnmarshalReadJournalRequest(data []byte) (*ReadJournalRequest, error) {
	req := &ReadJournalRequest{}
	offset := 0
	next := 0
	hasName := false
	for offset < len(data) {
		tag, inner, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, fmt.Errorf("pdu: read-journal request: %w", err)
		}
		offset += n
		num := int(tag & 0x1f)
		if tag&0xe0 != 0xa0 || num < next || num == 3 || num > 5 {
			return nil, fmt.Errorf("pdu: unexpected tag 0x%02x in UnmarshalReadJournalRequest", tag)
		}
		next = num + 1
		switch tag {
		case 0xa0: // journalName
			name, nameErr := DecodeObjectName(inner)
			if nameErr != nil {
				return nil, fmt.Errorf("pdu: read-journal journalName: %w", nameErr)
			}
			req.Domain = name.DomainID
			req.Journal = name.ItemID
			hasName = true
		case 0xa1: // rangeStartSpecification
			if err := decodeRangeStart(inner, req); err != nil {
				return nil, fmt.Errorf("pdu: read-journal rangeStart: %w", err)
			}
		case 0xa2: // rangeStopSpecification
			if err := decodeRangeStop(inner, req); err != nil {
				return nil, fmt.Errorf("pdu: read-journal rangeStop: %w", err)
			}
		case 0xa4: // listOfVariables
			vars, err := decodeVisibleStringList(inner)
			if err != nil {
				return nil, fmt.Errorf("pdu: read-journal listOfVariables: %w", err)
			}
			req.ListOfVariables = vars
		default: // 0xa5 entryToStartAfter
			if err := decodeStartAfter(inner, req); err != nil {
				return nil, fmt.Errorf("pdu: read-journal startAfter: %w", err)
			}
			req.IsStartAfter = true
		}
	}
	if !hasName || req.Journal == "" {
		return nil, fmt.Errorf("pdu: read-journal request: missing journalName")
	}
	req.IsTimeRange = req.HasStartTime && req.HasStopTime
	return req, nil
}

// decodeRangeStart decodes the rangeStartSpecification CHOICE.
func decodeRangeStart(data []byte, req *ReadJournalRequest) error {
	tag, inner, err := berutil.DecodeTLVExact(data)
	if err != nil {
		return err
	}
	switch tag {
	case 0x80: // startingTime
		t, err := decodeJournalTime(inner)
		if err != nil {
			return err
		}
		req.StartTime, req.HasStartTime = t, true
	case 0x81: // startingEntry
		req.StartEntry, req.HasStartEntry = append([]byte{}, inner...), true
	default:
		return fmt.Errorf("unexpected tag 0x%02x", tag)
	}
	return nil
}

// decodeRangeStop decodes the rangeStopSpecification CHOICE.
func decodeRangeStop(data []byte, req *ReadJournalRequest) error {
	tag, inner, err := berutil.DecodeTLVExact(data)
	if err != nil {
		return err
	}
	switch tag {
	case 0x80: // endingTime
		t, err := decodeJournalTime(inner)
		if err != nil {
			return err
		}
		req.StopTime, req.HasStopTime = t, true
	case 0x81: // numberOfEntries Integer32
		n, err := berutil.DecodeInt32(inner)
		if err != nil {
			return err
		}
		if n < 0 {
			return fmt.Errorf("negative numberOfEntries %d", n)
		}
		req.NumberOfEntries, req.HasNumberOfEntries = int(n), true
	default:
		return fmt.Errorf("unexpected tag 0x%02x", tag)
	}
	return nil
}

// decodeVisibleStringList decodes SEQUENCE OF VisibleString contents.
func decodeVisibleStringList(data []byte) ([]string, error) {
	out := []string{}
	for off := 0; off < len(data); {
		tag, inner, n, err := berutil.DecodeTLVAt(data, off)
		if err != nil {
			return nil, err
		}
		off += n
		if tag != 0x1a {
			return nil, fmt.Errorf("expected VisibleString (0x1a), got 0x%02x", tag)
		}
		v, err := decodeVisibleString(inner)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func decodeJournalTime(inner []byte) (time.Time, error) {
	ms, err := decodeBinaryTime(inner)
	if err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(ms).UTC(), nil
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

// emptyOriginatingApplication is originatingApplication [1] wrapping an
// ApplicationReference with all of its optional fields omitted.
var emptyOriginatingApplication = []byte{0xa1, 0x02, 0x30, 0x00}

//	JournalEntry ::= SEQUENCE {
//	    entryIdentification    [0] IMPLICIT OCTET STRING,
//	    originatingApplication [1] ApplicationReference,
//	    entryContent           [2] IMPLICIT EntryContent
//	}
func marshalJournalEntry(e JournalEntryWire) ([]byte, error) {
	entryIDBytes := berutil.EncodeTLV(0x80, e.EntryID)

	occurrenceBin, err := EncodeEntryTime(e.OccurrenceTime)
	if err != nil {
		return nil, fmt.Errorf("occurrence time: %w", err)
	}
	occurrenceTimeBytes := berutil.EncodeTLV(0x80, occurrenceBin)

	var form []byte
	if e.HasAnnotation {
		// entryForm annotation [3] MMSString (ISO 9506-2 clause 23.8.1),
		// an explicit tag; MMSString is VisibleString unless the char
		// CBB is negotiated (clause 7.5.6), which go-mms never proposes.
		if len(e.Variables) > 0 {
			return nil, fmt.Errorf("pdu: journal entry: data and annotation are exclusive entryForm alternatives")
		}
		form = berutil.EncodeTLV(0xa3, berutil.EncodeTLV(0x1a, []byte(e.Annotation)))
	} else {
		var varBytes []byte
		for _, v := range e.Variables {
			vb, err := marshalJournalVariable(v)
			if err != nil {
				return nil, err
			}
			varBytes = append(varBytes, vb...)
		}
		form = berutil.EncodeTLV(0xa2, berutil.EncodeTLV(0xa1, varBytes))
	}
	entryContent := berutil.EncodeTLV(0xa2, append(occurrenceTimeBytes, form...))

	entryInner := make([]byte, 0, len(entryIDBytes)+len(emptyOriginatingApplication)+len(entryContent))
	entryInner = append(entryInner, entryIDBytes...)
	entryInner = append(entryInner, emptyOriginatingApplication...)
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
		case 0xa1: // originatingApplication: not exposed
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
	forms := 0
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
			forms++
		case 0xa3: // annotation [3] MMSString
			text, aErr := decodeAnnotation(inner)
			if aErr != nil {
				return aErr
			}
			e.Annotation, e.HasAnnotation = text, true
			forms++
		default:
			return fmt.Errorf("pdu: unexpected tag 0x%02x in parseEntryContent", tag)
		}
	}
	if !hasTime {
		return fmt.Errorf("pdu: journal entry: missing occurrenceTime")
	}
	// entryForm is a CHOICE of data [2] and annotation [3]: exactly one.
	if forms != 1 {
		return fmt.Errorf("pdu: journal entry: want exactly one entryForm (data or annotation), got %d", forms)
	}
	return nil
}

// decodeAnnotation decodes the annotation [3] alternative of entryForm:
// an explicit tag around MMSString, which is VisibleString (0x1a), or
// UTF8String (0x0c) when the char CBB is in use (ISO 9506-2 clauses 7.5.6
// and 23.8.1).
func decodeAnnotation(data []byte) (string, error) {
	tag, inner, err := berutil.DecodeTLVExact(data)
	if err != nil {
		return "", fmt.Errorf("pdu: journal annotation: %w", err)
	}
	switch tag {
	case 0x1a, 0x0c:
		return string(inner), nil
	}
	return "", fmt.Errorf("pdu: journal annotation: unexpected tag 0x%02x", tag)
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
