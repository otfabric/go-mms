// SPDX-License-Identifier: MIT

package pdu

import (
	"encoding/asn1"
	"fmt"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
)

// ObjectScope values for GetNameList request.
const (
	ScopeVMD         = 0
	ScopeDomain      = 1
	ScopeAssociation = 2
)

// Defensive limits for identifier list decoders.
const (
	maxIdentifiers   = 100000
	maxIdentifierLen = 1024
)

// GetNameListResult is the internal representation of a GetNameList response.
type GetNameListResult struct {
	Names       []string
	MoreFollows bool
}

// MarshalGetNameListRequest builds a ConfirmedRequestPdu for the
// MMS GetNameList service.
//
//	GetNameListRequest ::= SEQUENCE {
//	  objectClass   [0] EXPLICIT ObjectClass
//	  objectScope   [1] EXPLICIT ObjectScope
//	  continueAfter [2] IMPLICIT Identifier  -- OPTIONAL
//	}
func MarshalGetNameListRequest(invokeID codec.InvokeID, objectClass int, scope int, domainID string, continueAfter string) ([]byte, error) {
	// objectClass [0] EXPLICIT { basicObjectClass [0] IMPLICIT INTEGER }
	classInt := berutil.EncodeTLV(0x80, encodeUnsignedInt(uint64(objectClass)))
	objClass := berutil.EncodeTLV(0xa0, classInt)

	// objectScope [1] EXPLICIT CHOICE
	var scopeContent []byte
	switch scope {
	case ScopeVMD:
		scopeContent = berutil.EncodeTLV(0x80, nil) // vmdSpecific [0] NULL
	case ScopeDomain:
		scopeContent = berutil.EncodeTLV(0x81, []byte(domainID)) // domainSpecific [1] IMPLICIT Identifier
	case ScopeAssociation:
		scopeContent = berutil.EncodeTLV(0x82, nil) // aaSpecific [2] NULL
	default:
		return nil, fmt.Errorf("pdu: invalid scope %d", scope)
	}
	objScope := berutil.EncodeTLV(0xa1, scopeContent)

	payload := make([]byte, 0, len(objClass)+len(objScope)+32)
	payload = append(payload, objClass...)
	payload = append(payload, objScope...)

	if continueAfter != "" {
		payload = append(payload, berutil.EncodeTLV(0x82, []byte(continueAfter))...) // [2] IMPLICIT Identifier
	}

	return marshalConfirmedLegacy(invokeID, asn1util.TagServiceGetNameList, payload)
}

// UnmarshalGetNameListResponse parses a GetNameList response from the
// service RawValue inside a ConfirmedResponsePdu.
//
//	GetNameListResponse ::= SEQUENCE {
//	  listOfIdentifier [0] IMPLICIT SEQUENCE OF Identifier
//	  moreFollows      [1] IMPLICIT BOOLEAN  -- OPTIONAL, DEFAULT TRUE
//	}
func UnmarshalGetNameListResponse(serviceData asn1.RawValue) (*GetNameListResult, error) {
	content := serviceData.Bytes
	if len(content) == 0 {
		return nil, fmt.Errorf("pdu: getnamelist response: empty content")
	}

	offset := 0

	// listOfIdentifier [0] IMPLICIT SEQUENCE OF
	tag, listContent, n, err := berutil.DecodeTLVAt(content, offset)
	if err != nil {
		return nil, fmt.Errorf("pdu: getnamelist response: list: %w", err)
	}
	offset += n
	if tag != 0xa0 {
		return nil, fmt.Errorf("pdu: getnamelist response: expected [0] SEQUENCE OF (0xa0), got 0x%02x", tag)
	}

	names, err := decodeIdentifierList(listContent)
	if err != nil {
		return nil, fmt.Errorf("pdu: getnamelist response: %w", err)
	}

	moreFollows := true // default TRUE per ASN.1 spec
	if offset < len(content) {
		tag, boolContent, n, err := berutil.DecodeTLVAt(content, offset)
		if err != nil {
			return nil, fmt.Errorf("pdu: getnamelist response: moreFollows: %w", err)
		}
		offset += n
		if tag != 0x81 {
			return nil, fmt.Errorf("pdu: getnamelist response: expected [1] BOOLEAN (0x81), got 0x%02x", tag)
		}
		if len(boolContent) == 1 {
			moreFollows = boolContent[0] != 0
		}
	}

	if offset != len(content) {
		return nil, fmt.Errorf("pdu: getnamelist response: %d trailing bytes", len(content)-offset)
	}

	return &GetNameListResult{Names: names, MoreFollows: moreFollows}, nil
}

func decodeIdentifierList(data []byte) ([]string, error) {
	var names []string
	offset := 0
	for offset < len(data) {
		tag, content, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, fmt.Errorf("identifier [%d]: %w", len(names), err)
		}
		offset += n
		if tag != tagVisibleString {
			return nil, fmt.Errorf("identifier [%d]: expected VisibleString (0x1a), got 0x%02x", len(names), tag)
		}
		if len(content) > maxIdentifierLen {
			return nil, fmt.Errorf("pdu: identifier length %d exceeds maximum %d", len(content), maxIdentifierLen)
		}
		names = append(names, string(content))
		if len(names) > maxIdentifiers {
			return nil, fmt.Errorf("pdu: identifier count %d exceeds maximum %d", len(names), maxIdentifiers)
		}
	}
	return names, nil
}
