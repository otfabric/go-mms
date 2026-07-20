// SPDX-License-Identifier: MIT

package pdu

import (
	"encoding/asn1"
	"fmt"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
)

// MarshalDefineNamedVarListRequest builds a ConfirmedRequestPdu for
// the MMS DefineNamedVariableList service.
//
//	DefineNamedVariableListRequest ::= SEQUENCE {
//	  variableListName ObjectName
//	  listOfVariable   [0] IMPLICIT SEQUENCE OF SEQUENCE {
//	    variableSpecification VariableSpecification
//	  }
//	}
func MarshalDefineNamedVarListRequest(invokeID codec.InvokeID, listName ObjectNameWire, variables []VariableSpecWire) ([]byte, error) {
	nameBytes, err := EncodeObjectName(listName)
	if err != nil {
		return nil, fmt.Errorf("pdu: define named var list: list name: %w", err)
	}

	var varEntries []byte
	for i, v := range variables {
		objName, err := EncodeObjectName(v.Name)
		if err != nil {
			return nil, fmt.Errorf("pdu: define named var list: variable [%d]: %w", i, err)
		}
		varSpec := asn1util.WrapConstructed(0, objName)
		var seqContent []byte
		seqContent = append(seqContent, varSpec...)
		if len(v.AlternateAccess) > 0 {
			aaBytes, err := encodeAlternateAccess(v.AlternateAccess)
			if err != nil {
				return nil, fmt.Errorf("pdu: define named var list: variable [%d] alternate access: %w", i, err)
			}
			seqContent = append(seqContent, berutil.EncodeTLV(tagAltAccessWrapper, aaBytes)...)
		}
		entry := berutil.EncodeTLV(tagSequence, seqContent)
		varEntries = append(varEntries, entry...)
	}
	listOfVar := berutil.EncodeTLV(0xa0, varEntries) // [0] IMPLICIT SEQUENCE OF

	payload := make([]byte, 0, len(nameBytes)+len(listOfVar))
	payload = append(payload, nameBytes...)
	payload = append(payload, listOfVar...)

	return marshalConfirmedLegacy(invokeID, asn1util.TagServiceDefineNamedVariableList, payload)
}

// MarshalGetNamedVarListAttrsRequest builds a ConfirmedRequestPdu for
// the MMS GetNamedVariableListAttributes service.
//
// The request is simply an ObjectName identifying the list.
func MarshalGetNamedVarListAttrsRequest(invokeID codec.InvokeID, listName ObjectNameWire) ([]byte, error) {
	payload, err := EncodeObjectName(listName)
	if err != nil {
		return nil, fmt.Errorf("pdu: get named var list attrs: %w", err)
	}
	return marshalConfirmedLegacy(invokeID, asn1util.TagServiceGetNamedVariableListAttrs, payload)
}

// NamedVarListAttrsResult is the internal result of a
// GetNamedVariableListAttributes response.
type NamedVarListAttrsResult struct {
	Deletable bool
	Variables []VariableSpecWire
}

// UnmarshalGetNamedVarListAttrsResponse parses a
// GetNamedVariableListAttributes response.
//
//	GetNamedVariableListAttributesResponse ::= SEQUENCE {
//	  mmsDeletable   [0] IMPLICIT BOOLEAN
//	  listOfVariable [1] IMPLICIT SEQUENCE OF SEQUENCE {
//	    variableSpecification VariableSpecification
//	  }
//	}
func UnmarshalGetNamedVarListAttrsResponse(serviceData asn1.RawValue) (*NamedVarListAttrsResult, error) {
	content := serviceData.Bytes
	if len(content) == 0 {
		return nil, fmt.Errorf("pdu: getnamedvarlistattrs response: empty content")
	}

	offset := 0

	// mmsDeletable [0] IMPLICIT BOOLEAN
	tag, inner, n, err := berutil.DecodeTLVAt(content, offset)
	if err != nil {
		return nil, fmt.Errorf("pdu: getnamedvarlistattrs response: mmsDeletable: %w", err)
	}
	offset += n
	if tag != 0x80 {
		return nil, fmt.Errorf("pdu: getnamedvarlistattrs response: expected [0] (0x80), got 0x%02x", tag)
	}
	deletable := len(inner) == 1 && inner[0] != 0

	// listOfVariable [1] IMPLICIT SEQUENCE OF
	if offset >= len(content) {
		return nil, fmt.Errorf("pdu: getnamedvarlistattrs response: missing listOfVariable")
	}
	tag, inner, n, err = berutil.DecodeTLVAt(content, offset)
	if err != nil {
		return nil, fmt.Errorf("pdu: getnamedvarlistattrs response: listOfVariable: %w", err)
	}
	offset += n
	if tag != 0xa1 {
		return nil, fmt.Errorf("pdu: getnamedvarlistattrs response: expected [1] (0xa1), got 0x%02x", tag)
	}

	variables, err := decodeVariableList(inner)
	if err != nil {
		return nil, fmt.Errorf("pdu: getnamedvarlistattrs response: %w", err)
	}

	if offset != len(content) {
		return nil, fmt.Errorf("pdu: getnamedvarlistattrs response: %d trailing bytes", len(content)-offset)
	}

	return &NamedVarListAttrsResult{Deletable: deletable, Variables: variables}, nil
}

func decodeVariableList(data []byte) ([]VariableSpecWire, error) {
	var specs []VariableSpecWire
	offset := 0
	for offset < len(data) {
		tag, content, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, fmt.Errorf("variable [%d]: %w", len(specs), err)
		}
		offset += n
		if tag != tagSequence {
			return nil, fmt.Errorf("variable [%d]: expected SEQUENCE (0x30), got 0x%02x", len(specs), tag)
		}

		spec, err := decodeVariableSpecFull(content)
		if err != nil {
			return nil, fmt.Errorf("variable [%d]: %w", len(specs), err)
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

func decodeVariableSpec(data []byte) (ObjectNameWire, error) {
	// VariableSpecification: only supporting name [0] EXPLICIT ObjectName
	tag, content, _, err := berutil.DecodeTLVAt(data, 0)
	if err != nil {
		return ObjectNameWire{}, fmt.Errorf("variableSpec: %w", err)
	}
	if tag != 0xa0 {
		return ObjectNameWire{}, fmt.Errorf("variableSpec: expected name [0] (0xa0), got 0x%02x (only 'name' supported)", tag)
	}
	return DecodeObjectName(content)
}

func decodeVariableSpecFull(data []byte) (VariableSpecWire, error) {
	offset := 0
	tag, content, n, err := berutil.DecodeTLVAt(data, offset)
	if err != nil {
		return VariableSpecWire{}, fmt.Errorf("variableSpec: %w", err)
	}
	offset += n
	if tag != 0xa0 {
		return VariableSpecWire{}, fmt.Errorf("variableSpec: expected name [0] (0xa0), got 0x%02x (only 'name' supported)", tag)
	}
	name, err := DecodeObjectName(content)
	if err != nil {
		return VariableSpecWire{}, err
	}

	spec := VariableSpecWire{Name: name}

	if offset < len(data) {
		tag2, content2, n2, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return VariableSpecWire{}, fmt.Errorf("variableSpec alternateAccess: %w", err)
		}
		offset += n2
		switch tag2 {
		case tagAltAccessWrapper:
			aa, err := decodeAlternateAccess(content2)
			if err != nil {
				return VariableSpecWire{}, fmt.Errorf("variableSpec alternateAccess: %w", err)
			}
			spec.AlternateAccess = aa
		default:
			return VariableSpecWire{}, fmt.Errorf("pdu: unexpected tag 0x%02x in decodeVariableSpecFull", tag2)
		}
	}

	if offset != len(data) {
		return VariableSpecWire{}, fmt.Errorf("pdu: %d trailing bytes in variableSpec", len(data)-offset)
	}

	return spec, nil
}

// MarshalDeleteNamedVarListRequest builds a ConfirmedRequestPdu for
// the MMS DeleteNamedVariableList service.
//
//	DeleteNamedVariableListRequest ::= SEQUENCE {
//	  scopeOfDelete            [0] IMPLICIT INTEGER  -- OPTIONAL, DEFAULT 0
//	  listOfVariableListName   [1] IMPLICIT SEQUENCE OF ObjectName
//	  domainName               [2] IMPLICIT Identifier  -- OPTIONAL
//	}
func MarshalDeleteNamedVarListRequest(invokeID codec.InvokeID, listNames []ObjectNameWire) ([]byte, error) {
	// scopeOfDelete = 0 (specific), which is the default — omit it.
	var nameEntries []byte
	for i, n := range listNames {
		encoded, err := EncodeObjectName(n)
		if err != nil {
			return nil, fmt.Errorf("pdu: delete named var list: name [%d]: %w", i, err)
		}
		nameEntries = append(nameEntries, encoded...)
	}
	listOfNames := berutil.EncodeTLV(0xa1, nameEntries) // [1] IMPLICIT SEQUENCE OF

	return marshalConfirmedLegacy(invokeID, asn1util.TagServiceDeleteNamedVariableList, listOfNames)
}

// MarshalDeleteNVLDomainScopeRequest builds a ConfirmedRequestPdu for
// deleting all deletable NVLs in the specified domain (scopeOfDelete=2).
func MarshalDeleteNVLDomainScopeRequest(invokeID codec.InvokeID, domain string) ([]byte, error) {
	scopeBytes := berutil.EncodeTLV(0x80, berutil.EncodeInt(2))
	listOfNames := berutil.EncodeTLV(0xa1, nil)
	domainName := berutil.EncodeTLV(0x82, []byte(domain))
	var payload []byte
	payload = append(payload, scopeBytes...)
	payload = append(payload, listOfNames...)
	payload = append(payload, domainName...)
	return marshalConfirmedLegacy(invokeID, asn1util.TagServiceDeleteNamedVariableList, payload)
}

// MarshalDeleteNVLVMDScopeRequest builds a ConfirmedRequestPdu for
// deleting all deletable VMD-scoped NVLs (scopeOfDelete=3).
func MarshalDeleteNVLVMDScopeRequest(invokeID codec.InvokeID) ([]byte, error) {
	scopeBytes := berutil.EncodeTLV(0x80, berutil.EncodeInt(3))
	listOfNames := berutil.EncodeTLV(0xa1, nil)
	var payload []byte
	payload = append(payload, scopeBytes...)
	payload = append(payload, listOfNames...)
	return marshalConfirmedLegacy(invokeID, asn1util.TagServiceDeleteNamedVariableList, payload)
}

// DeleteNamedVarListResult is the internal result of a
// DeleteNamedVariableList response.
type DeleteNamedVarListResult struct {
	NumberMatched int
	NumberDeleted int
}

// UnmarshalDeleteNamedVarListResponse parses a DeleteNamedVariableList response.
//
// ISO 9506-2 DeleteNamedVariableList-Response:
//
//	DeleteNamedVariableList-Response ::= SEQUENCE {
//	  numberMatched [0] IMPLICIT Unsigned32,
//	  numberDeleted [1] IMPLICIT Unsigned32
//	}
//
// The wire tags are 0x80 (numberMatched) and 0x81 (numberDeleted).
func UnmarshalDeleteNamedVarListResponse(serviceData asn1.RawValue) (*DeleteNamedVarListResult, error) {
	content := serviceData.Bytes
	if len(content) == 0 {
		return nil, fmt.Errorf("pdu: deletenamelist response: empty content")
	}

	const (
		tagDeleteNVLNumberMatched byte = 0x80 // [0] IMPLICIT Unsigned32
		tagDeleteNVLNumberDeleted byte = 0x81 // [1] IMPLICIT Unsigned32
	)

	offset := 0

	// numberMatched [0] IMPLICIT Unsigned32
	tag, inner, n, err := berutil.DecodeTLVAt(content, offset)
	if err != nil {
		return nil, fmt.Errorf("pdu: deletenamelist response: numberMatched: %w", err)
	}
	offset += n
	if tag != tagDeleteNVLNumberMatched {
		return nil, fmt.Errorf("pdu: deletenamelist response: expected numberMatched [0] (0x80), got 0x%02x", tag)
	}
	matched, err := berutil.DecodeUnsigned(inner)
	if err != nil {
		return nil, fmt.Errorf("pdu: deletenamelist response: numberMatched value: %w", err)
	}

	// numberDeleted [1] IMPLICIT Unsigned32
	tag, inner, n, err = berutil.DecodeTLVAt(content, offset)
	if err != nil {
		return nil, fmt.Errorf("pdu: deletenamelist response: numberDeleted: %w", err)
	}
	offset += n
	if tag != tagDeleteNVLNumberDeleted {
		return nil, fmt.Errorf("pdu: deletenamelist response: expected numberDeleted [1] (0x81), got 0x%02x", tag)
	}
	deleted, err := berutil.DecodeUnsigned(inner)
	if err != nil {
		return nil, fmt.Errorf("pdu: deletenamelist response: numberDeleted value: %w", err)
	}

	if offset != len(content) {
		return nil, fmt.Errorf("pdu: deletenamelist response: %d trailing bytes", len(content)-offset)
	}

	return &DeleteNamedVarListResult{NumberMatched: int(matched), NumberDeleted: int(deleted)}, nil
}
