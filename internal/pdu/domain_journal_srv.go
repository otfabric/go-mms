// SPDX-License-Identifier: MIT

package pdu

import (
	"fmt"
	"time"

	"github.com/otfabric/go-mms/internal/berutil"
)

// DomainStateReady is the ready(2) DomainState of ISO 9506-1 clause 11.
const DomainStateReady = 2

// UnmarshalGetDomainAttributesRequest decodes the body of
// GetDomainAttributes-Request ::= Identifier (ISO 9506-2 clause 11.13),
// sent as [37] IMPLICIT, so body is the domain name itself.
func UnmarshalGetDomainAttributesRequest(body []byte) (string, error) {
	if len(body) == 0 {
		return "", fmt.Errorf("pdu: get-domain-attributes request: empty domain name")
	}
	if len(body) > maxIdentifierLen {
		return "", fmt.Errorf("pdu: get-domain-attributes request: domain name length %d exceeds %d", len(body), maxIdentifierLen)
	}
	return string(body), nil
}

// MarshalGetDomainAttributesResponse encodes the body of
// GetDomainAttributes-Response (ISO 9506-2 clause 11.13) for a domain
// with no capabilities and no program invocations:
//
//	GetDomainAttributes-Response ::= SEQUENCE {
//	  listOfCapabilities       [0] IMPLICIT SEQUENCE OF MMSString,
//	  state                    [1] IMPLICIT DomainState,
//	  mmsDeletable             [2] IMPLICIT BOOLEAN,
//	  sharable                 [3] IMPLICIT BOOLEAN,
//	  listOfProgramInvocations [4] IMPLICIT SEQUENCE OF Identifier,
//	  uploadInProgress         [5] IMPLICIT Integer8 }
func MarshalGetDomainAttributesResponse(state int, deletable, sharable bool) []byte {
	b := func(v bool) []byte {
		if v {
			return []byte{0xff}
		}
		return []byte{0x00}
	}
	out := berutil.EncodeTLV(0xa0, nil)
	out = append(out, berutil.EncodeTLV(0x81, berutil.EncodeInt(state))...)
	out = append(out, berutil.EncodeTLV(0x82, b(deletable))...)
	out = append(out, berutil.EncodeTLV(0x83, b(sharable))...)
	out = append(out, berutil.EncodeTLV(0xa4, nil)...)
	return append(out, berutil.EncodeTLV(0x85, []byte{0x00})...)
}

// InitializeJournalRequest is a decoded InitializeJournal-Request (ISO
// 9506-2 clause 23.4):
//
//	InitializeJournal-Request ::= SEQUENCE {
//	  journalName        [0] ObjectName,
//	  limitSpecification [1] IMPLICIT SEQUENCE {
//	    limitingTime  [0] IMPLICIT TimeOfDay,
//	    limitingEntry [1] IMPLICIT OCTET STRING OPTIONAL } OPTIONAL }
type InitializeJournalRequest struct {
	Name ObjectNameWire

	HasLimit      bool
	LimitingTime  time.Time
	LimitingEntry []byte // nil when absent
}

// UnmarshalInitializeJournalRequest decodes an InitializeJournal-Request
// body.
func UnmarshalInitializeJournalRequest(data []byte) (*InitializeJournalRequest, error) {
	req := &InitializeJournalRequest{}
	offset := 0
	tag, inner, n, err := berutil.DecodeTLVAt(data, offset)
	if err != nil {
		return nil, fmt.Errorf("pdu: initialize-journal request: %w", err)
	}
	if tag != 0xa0 {
		return nil, fmt.Errorf("pdu: initialize-journal request: expected journalName [0] (0xa0), got 0x%02x", tag)
	}
	if req.Name, err = DecodeObjectName(inner); err != nil {
		return nil, fmt.Errorf("pdu: initialize-journal journalName: %w", err)
	}
	offset += n
	if offset == len(data) {
		return req, nil
	}
	tag, inner, n, err = berutil.DecodeTLVAt(data, offset)
	if err != nil {
		return nil, fmt.Errorf("pdu: initialize-journal limitSpecification: %w", err)
	}
	if tag != 0xa1 || offset+n != len(data) {
		return nil, fmt.Errorf("pdu: initialize-journal request: unexpected tag 0x%02x or trailing bytes", tag)
	}
	req.HasLimit = true
	ltag, lt, ln, err := berutil.DecodeTLVAt(inner, 0)
	if err != nil || ltag != 0x80 {
		return nil, fmt.Errorf("pdu: initialize-journal limitingTime: missing or malformed")
	}
	if req.LimitingTime, err = decodeJournalTime(lt); err != nil {
		return nil, fmt.Errorf("pdu: initialize-journal limitingTime: %w", err)
	}
	if ln == len(inner) {
		return req, nil
	}
	etag, entry, err := berutil.DecodeTLVExact(inner[ln:])
	if err != nil || etag != 0x81 {
		return nil, fmt.Errorf("pdu: initialize-journal limitingEntry: missing or malformed")
	}
	req.LimitingEntry = append([]byte{}, entry...)
	return req, nil
}
