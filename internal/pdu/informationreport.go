package pdu

import (
	"fmt"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
)

// InformationReport tags within UnconfirmedPDU (0xa3).
//
// BER layout:
//
//	0xa3 (UnconfirmedPDU) {
//	  0xa0 (InformationReport [0] in UnconfirmedService CHOICE) {
//	    VariableAccessSpecification CHOICE:
//	      [0] listOfVariable — SEQUENCE OF { variableSpecification, ... }
//	      [1] variableListName — ObjectName
//	    [0] listOfAccessResult — SEQUENCE OF AccessResult
//	  }
//	}
const (
	tagInfoReport         byte = 0xa0 // [0] IMPLICIT InformationReport within UnconfirmedService
	tagListOfVariable     byte = 0xa0 // [0] IMPLICIT SEQUENCE OF within VariableAccessSpec
	tagVariableListName   byte = 0xa1 // [1] variableListName within VariableAccessSpec
	tagListOfAccessResult byte = 0xa0 // [0] IMPLICIT SEQUENCE OF AccessResult
	tagSequenceInfoReport byte = 0x30 // UNIVERSAL SEQUENCE
	tagVarSpecName        byte = 0xa0 // [0] EXPLICIT ObjectName within VariableSpecification
)

// InformationReportWire is the internal representation of an MMS
// InformationReport for encoding and decoding.
type InformationReportWire struct {
	// If non-nil, the report references a named variable list.
	ListName *ObjectNameWire

	// Individual variable specifications (when ListName is nil).
	Variables []ObjectNameWire

	// Access result values, one per variable (or per list member).
	Values []*DataValue
}

// MarshalInformationReport encodes an InformationReport as a complete
// MMS UnconfirmedPDU (tag 0xa3).
func MarshalInformationReport(r *InformationReportWire) ([]byte, error) {
	var content []byte

	if r.ListName != nil {
		nameBytes, err := EncodeObjectName(*r.ListName)
		if err != nil {
			return nil, fmt.Errorf("pdu: info report list name: %w", err)
		}
		content = append(content, berutil.EncodeTLV(tagVariableListName, nameBytes)...)
	} else {
		var entries []byte
		for i, v := range r.Variables {
			nameBytes, err := EncodeObjectName(v)
			if err != nil {
				return nil, fmt.Errorf("pdu: info report variable [%d]: %w", i, err)
			}
			varSpec := berutil.EncodeTLV(tagVarSpecName, nameBytes)
			entry := berutil.EncodeTLV(tagSequenceInfoReport, varSpec)
			entries = append(entries, entry...)
		}
		content = append(content, berutil.EncodeTLV(tagListOfVariable, entries)...)
	}

	var resultEntries []byte
	for i, dv := range r.Values {
		b, err := MarshalData(dv)
		if err != nil {
			return nil, fmt.Errorf("pdu: info report value [%d]: %w", i, err)
		}
		resultEntries = append(resultEntries, b...)
	}
	content = append(content, berutil.EncodeTLV(tagListOfAccessResult, resultEntries)...)

	infoReport := berutil.EncodeTLV(tagInfoReport, content)
	return berutil.EncodeTLV(asn1util.TagUnconfirmed, infoReport), nil
}

// UnmarshalInformationReport decodes an InformationReport from the
// content of an UnconfirmedPDU (after stripping the 0xa3 outer tag).
func UnmarshalInformationReport(unconfirmedContent []byte) (*InformationReportWire, error) {
	if len(unconfirmedContent) == 0 {
		return nil, fmt.Errorf("pdu: empty UnconfirmedPDU content")
	}

	tag, irContent, err := berutil.DecodeTLV(unconfirmedContent)
	if err != nil {
		return nil, fmt.Errorf("pdu: info report outer: %w", err)
	}
	if tag != tagInfoReport {
		return nil, fmt.Errorf("pdu: expected InformationReport tag 0xa0, got 0x%02x", tag)
	}

	r := &InformationReportWire{}
	offset := 0

	if offset >= len(irContent) {
		return nil, fmt.Errorf("pdu: info report truncated (no variableAccessSpec)")
	}

	vasTag, vasContent, vasN, err := berutil.DecodeTLVAt(irContent, offset)
	if err != nil {
		return nil, fmt.Errorf("pdu: info report variableAccessSpec: %w", err)
	}
	offset += vasN

	switch vasTag {
	case tagListOfVariable:
		vars, err := decodeInfoReportListOfVariable(vasContent)
		if err != nil {
			return nil, err
		}
		r.Variables = vars

	case tagVariableListName:
		name, err := DecodeObjectName(vasContent)
		if err != nil {
			return nil, fmt.Errorf("pdu: info report variableListName: %w", err)
		}
		r.ListName = &name

	default:
		return nil, fmt.Errorf("pdu: info report unknown variableAccessSpec tag 0x%02x", vasTag)
	}

	if offset >= len(irContent) {
		return nil, fmt.Errorf("pdu: info report truncated (no listOfAccessResult)")
	}

	resTag, resContent, resN, err := berutil.DecodeTLVAt(irContent, offset)
	if err != nil {
		return nil, fmt.Errorf("pdu: info report listOfAccessResult: %w", err)
	}
	offset += resN

	if resTag != tagListOfAccessResult {
		return nil, fmt.Errorf("pdu: info report expected listOfAccessResult tag 0xa0, got 0x%02x", resTag)
	}

	values, err := UnmarshalAccessResults(resContent)
	if err != nil {
		return nil, fmt.Errorf("pdu: info report values: %w", err)
	}
	r.Values = values

	if offset != len(irContent) {
		return nil, fmt.Errorf("pdu: %d trailing bytes in InformationReport", len(irContent)-offset)
	}

	return r, nil
}

func decodeInfoReportListOfVariable(data []byte) ([]ObjectNameWire, error) {
	var vars []ObjectNameWire
	offset := 0
	for offset < len(data) {
		tag, content, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, fmt.Errorf("pdu: info report variable [%d]: %w", len(vars), err)
		}
		offset += n
		if tag != tagSequenceInfoReport {
			return nil, fmt.Errorf("pdu: info report variable [%d]: expected SEQUENCE (0x30), got 0x%02x", len(vars), tag)
		}
		name, err := decodeVariableSpec(content)
		if err != nil {
			return nil, fmt.Errorf("pdu: info report variable [%d]: %w", len(vars), err)
		}
		vars = append(vars, name)
	}
	return vars, nil
}
