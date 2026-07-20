// SPDX-License-Identifier: MIT

package pdu

import (
	"encoding/asn1"
	"fmt"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
)

// Write response per-variable result tags.
const (
	tagWriteFailure byte = 0x80 // [0] IMPLICIT DataAccessError
	tagWriteSuccess byte = 0x81 // [1] IMPLICIT NULL
)

// WriteResultItem is the per-variable outcome from a Write response.
type WriteResultItem struct {
	Success bool
	ErrCode int // DataAccessError code, valid when !Success
}

// MarshalWriteRequest builds a complete ConfirmedRequestPdu for the
// MMS Write service with domain-specific variables and their values.
func MarshalWriteRequest(invokeID codec.InvokeID, vars []ObjectNameWire, values []*DataValue) ([]byte, error) {
	if len(vars) != len(values) {
		return nil, fmt.Errorf("pdu: write request: %d variables but %d values", len(vars), len(values))
	}

	varSpec, err := encodeListOfVariable(vars)
	if err != nil {
		return nil, fmt.Errorf("pdu: write request: %w", err)
	}

	dataContent, err := MarshalDataList(values)
	if err != nil {
		return nil, fmt.Errorf("pdu: write request: marshal data: %w", err)
	}
	// WriteRequest.listOfData [0] IMPLICIT SEQUENCE OF per the MMS ASN.1 definition.
	// The surrounding context-specific tag is IMPLICIT and replaces the universal
	// SEQUENCE tag, so tagWriteListOfData (0xa0) is used rather than 0x30.
	dataList := berutil.EncodeTLV(tagWriteListOfData, dataContent)

	payload := make([]byte, 0, len(varSpec)+len(dataList))
	payload = append(payload, varSpec...)
	payload = append(payload, dataList...)

	return marshalConfirmedLegacy(invokeID, asn1util.TagServiceWrite, payload)
}

// UnmarshalWriteResponse parses a Write response from the service
// RawValue inside a ConfirmedResponsePdu. Returns per-variable results.
func UnmarshalWriteResponse(serviceData asn1.RawValue) ([]WriteResultItem, error) {
	content := serviceData.Bytes
	if len(content) == 0 {
		return nil, fmt.Errorf("pdu: write response: empty content")
	}

	var results []WriteResultItem
	offset := 0
	for offset < len(content) {
		tag, inner, n, err := berutil.DecodeTLVAt(content, offset)
		if err != nil {
			return nil, fmt.Errorf("pdu: write response item [%d]: %w", len(results), err)
		}
		offset += n

		switch tag {
		case tagWriteSuccess:
			results = append(results, WriteResultItem{Success: true})
		case tagWriteFailure:
			code, err := berutil.DecodeInteger(inner)
			if err != nil {
				return nil, fmt.Errorf("pdu: write response failure code: %w", err)
			}
			results = append(results, WriteResultItem{ErrCode: code})
		default:
			return nil, fmt.Errorf("pdu: write response: unexpected tag 0x%02x", tag)
		}
	}

	if offset != len(content) {
		return nil, fmt.Errorf("pdu: %d trailing bytes in write response", len(content)-offset)
	}

	return results, nil
}
