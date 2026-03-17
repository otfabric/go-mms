package pdu

import (
	"encoding/asn1"
	"fmt"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
)

// ObjectNameWire is the internal representation of an MMS ObjectName.
//
// When Scope is ScopeDomain, both DomainID and ItemID are set.
// When Scope is ScopeVMD or ScopeAssociation, only ItemID is used.
type ObjectNameWire struct {
	Scope    int // ScopeVMD=0, ScopeDomain=1, ScopeAssociation=2
	DomainID string
	ItemID   string
}

// BER tags used in Read request/response encoding.
const (
	tagVisibleString byte = 0x1a // UNIVERSAL 26
	tagInteger       byte = 0x02 // UNIVERSAL 2
	tagSequence      byte = 0x30 // UNIVERSAL 16, constructed
	tagListOfVar     byte = 0xa0 // [0] CONSTRUCTED (listOfVariable)
	tagVarListName   byte = 0xa1 // [1] CONSTRUCTED (variableListName)
)

// EncodeObjectName encodes an ObjectName for the wire.
//
//	ObjectName ::= CHOICE {
//	  vmdSpecific      [0] IMPLICIT Identifier
//	  domainSpecific   [1] CONSTRUCTED { domainId, itemId }
//	  aaSpecific       [2] IMPLICIT Identifier
//	}
func EncodeObjectName(n ObjectNameWire) ([]byte, error) {
	switch n.Scope {
	case ScopeVMD:
		return berutil.EncodeTLV(0x80, []byte(n.ItemID)), nil // [0] IMPLICIT Identifier
	case ScopeDomain:
		domain := berutil.EncodeTLV(tagVisibleString, []byte(n.DomainID))
		item := berutil.EncodeTLV(tagVisibleString, []byte(n.ItemID))
		content := make([]byte, 0, len(domain)+len(item))
		content = append(content, domain...)
		content = append(content, item...)
		return berutil.EncodeTLV(0xa1, content), nil // [1] CONSTRUCTED
	case ScopeAssociation:
		return berutil.EncodeTLV(0x82, []byte(n.ItemID)), nil // [2] IMPLICIT Identifier
	default:
		return nil, fmt.Errorf("pdu: invalid ObjectName scope %d", n.Scope)
	}
}

// DecodeObjectName decodes an ObjectName from wire encoding.
func DecodeObjectName(data []byte) (ObjectNameWire, error) {
	tag, content, err := berutil.DecodeTLV(data)
	if err != nil {
		return ObjectNameWire{}, fmt.Errorf("decode object name: %w", err)
	}
	return decodeObjectNameFromTLV(tag, content)
}

// DecodeObjectNameAt decodes an ObjectName starting at offset, returning
// the decoded name and the number of bytes consumed.
func DecodeObjectNameAt(data []byte, offset int) (ObjectNameWire, int, error) {
	tag, content, n, err := berutil.DecodeTLVAt(data, offset)
	if err != nil {
		return ObjectNameWire{}, 0, fmt.Errorf("decode object name: %w", err)
	}
	name, err := decodeObjectNameFromTLV(tag, content)
	if err != nil {
		return ObjectNameWire{}, 0, err
	}
	return name, n, nil
}

func decodeObjectNameFromTLV(tag byte, content []byte) (ObjectNameWire, error) {
	switch tag {
	case 0x80: // vmdSpecific [0] IMPLICIT Identifier
		return ObjectNameWire{Scope: ScopeVMD, ItemID: string(content)}, nil
	case 0xa1: // domainSpecific [1] CONSTRUCTED
		return decodeDomainSpecificName(content)
	case 0x82: // aaSpecific [2] IMPLICIT Identifier
		return ObjectNameWire{Scope: ScopeAssociation, ItemID: string(content)}, nil
	default:
		return ObjectNameWire{}, fmt.Errorf("unexpected ObjectName tag 0x%02x", tag)
	}
}

func decodeDomainSpecificName(data []byte) (ObjectNameWire, error) {
	offset := 0
	tag, content, n, err := berutil.DecodeTLVAt(data, offset)
	if err != nil {
		return ObjectNameWire{}, fmt.Errorf("domainSpecific domainId: %w", err)
	}
	offset += n
	if tag != tagVisibleString {
		return ObjectNameWire{}, fmt.Errorf("domainSpecific domainId: expected VisibleString, got 0x%02x", tag)
	}
	domainID := string(content)

	tag, content, n, err = berutil.DecodeTLVAt(data, offset)
	if err != nil {
		return ObjectNameWire{}, fmt.Errorf("domainSpecific itemId: %w", err)
	}
	offset += n
	if tag != tagVisibleString {
		return ObjectNameWire{}, fmt.Errorf("domainSpecific itemId: expected VisibleString, got 0x%02x", tag)
	}
	if offset != len(data) {
		return ObjectNameWire{}, fmt.Errorf("domainSpecific: %d trailing bytes", len(data)-offset)
	}
	return ObjectNameWire{Scope: ScopeDomain, DomainID: domainID, ItemID: string(content)}, nil
}

// MarshalReadRequest builds a complete ConfirmedRequestPdu for the
// MMS Read service with a list of domain-specific variables.
func MarshalReadRequest(invokeID codec.InvokeID, vars []ObjectNameWire) ([]byte, error) {
	varSpec, err := encodeListOfVariable(vars)
	if err != nil {
		return nil, fmt.Errorf("pdu: read request: %w", err)
	}
	return marshalConfirmedLegacy(invokeID, asn1util.TagServiceRead, varSpec)
}

// UnmarshalReadResponse parses a Read response from the service
// RawValue inside a ConfirmedResponsePdu. Returns the list of
// AccessResult values (each is either a Data value or a DataAccessError).
func UnmarshalReadResponse(serviceData asn1.RawValue) ([]*DataValue, error) {
	content := serviceData.Bytes
	if len(content) == 0 {
		return nil, fmt.Errorf("pdu: read response: empty content")
	}

	offset := 0

	// Skip optional variableAccessSpecification [0]
	if offset < len(content) && content[offset] == tagListOfVar {
		_, _, n, err := berutil.DecodeTLVAt(content, offset)
		if err != nil {
			return nil, fmt.Errorf("pdu: read response: skip varspec: %w", err)
		}
		offset += n
	}

	// Parse listOfAccessResult SEQUENCE OF
	if offset >= len(content) {
		return nil, fmt.Errorf("pdu: read response: missing access result list")
	}

	tag, listContent, n, err := berutil.DecodeTLVAt(content, offset)
	if err != nil {
		return nil, fmt.Errorf("pdu: read response: list: %w", err)
	}
	offset += n
	if tag != tagSequence {
		return nil, fmt.Errorf("pdu: read response: expected SEQUENCE OF (0x30), got 0x%02x", tag)
	}

	if offset != len(content) {
		return nil, fmt.Errorf("pdu: read response: %d trailing bytes", len(content)-offset)
	}

	return UnmarshalAccessResults(listContent)
}

func encodeListOfVariable(vars []ObjectNameWire) ([]byte, error) {
	var inner []byte
	for i, v := range vars {
		nameBytes, err := EncodeObjectName(v)
		if err != nil {
			return nil, fmt.Errorf("variable [%d]: %w", i, err)
		}
		seq := berutil.EncodeTLV(tagSequence, nameBytes)
		inner = append(inner, seq...)
	}
	return berutil.EncodeTLV(tagListOfVar, inner), nil
}

// MarshalReadRequestWithAccess builds a ConfirmedRequestPdu for the
// MMS Read service with variables that may include alternate access.
func MarshalReadRequestWithAccess(invokeID codec.InvokeID, vars []VariableSpecWire) ([]byte, error) {
	varSpec, err := encodeListOfVariableWithAccess(vars)
	if err != nil {
		return nil, fmt.Errorf("pdu: read request: %w", err)
	}
	return marshalConfirmedLegacy(invokeID, asn1util.TagServiceRead, varSpec)
}

// MarshalWriteRequestWithAccess builds a ConfirmedRequestPdu for the
// MMS Write service with variables that may include alternate access.
func MarshalWriteRequestWithAccess(invokeID codec.InvokeID, vars []VariableSpecWire, values []*DataValue) ([]byte, error) {
	if len(vars) != len(values) {
		return nil, fmt.Errorf("pdu: write request: %d variables but %d values", len(vars), len(values))
	}

	varSpec, err := encodeListOfVariableWithAccess(vars)
	if err != nil {
		return nil, fmt.Errorf("pdu: write request: %w", err)
	}

	dataContent, err := MarshalDataList(values)
	if err != nil {
		return nil, fmt.Errorf("pdu: write request: marshal data: %w", err)
	}
	dataList := berutil.EncodeTLV(tagSequence, dataContent)

	payload := make([]byte, 0, len(varSpec)+len(dataList))
	payload = append(payload, varSpec...)
	payload = append(payload, dataList...)

	return marshalConfirmedLegacy(invokeID, asn1util.TagServiceWrite, payload)
}

func encodeListOfVariableWithAccess(vars []VariableSpecWire) ([]byte, error) {
	var inner []byte
	for i, v := range vars {
		nameBytes, err := EncodeObjectName(v.Name)
		if err != nil {
			return nil, fmt.Errorf("variable [%d]: %w", i, err)
		}

		var seqContent []byte
		seqContent = append(seqContent, nameBytes...)

		if len(v.AlternateAccess) > 0 {
			aaBytes, err := encodeAlternateAccess(v.AlternateAccess)
			if err != nil {
				return nil, fmt.Errorf("variable [%d] alternate access: %w", i, err)
			}
			seqContent = append(seqContent, berutil.EncodeTLV(tagAltAccessWrapper, aaBytes)...)
		}

		inner = append(inner, berutil.EncodeTLV(tagSequence, seqContent)...)
	}
	return berutil.EncodeTLV(tagListOfVar, inner), nil
}

// MarshalReadRequestByListName builds a ConfirmedRequestPdu for the
// MMS Read service using the variableListName CHOICE (tag [1]).
func MarshalReadRequestByListName(invokeID codec.InvokeID, listName ObjectNameWire) ([]byte, error) {
	nameBytes, err := EncodeObjectName(listName)
	if err != nil {
		return nil, fmt.Errorf("pdu: read by list name: %w", err)
	}
	varSpec := berutil.EncodeTLV(tagVarListName, nameBytes)
	return marshalConfirmedLegacy(invokeID, asn1util.TagServiceRead, varSpec)
}

// MarshalReadRequestByListNameWithSpec builds a ConfirmedRequestPdu for
// the MMS Read service using variableListName with specificationWithResult.
func MarshalReadRequestByListNameWithSpec(invokeID codec.InvokeID, listName ObjectNameWire, specWithResult bool) ([]byte, error) {
	var payload []byte
	if specWithResult {
		payload = append(payload, berutil.EncodeTLV(0x80, []byte{0xff})...)
	}
	nameBytes, err := EncodeObjectName(listName)
	if err != nil {
		return nil, fmt.Errorf("pdu: read by list name: %w", err)
	}
	payload = append(payload, berutil.EncodeTLV(tagVarListName, nameBytes)...)
	return marshalConfirmedLegacy(invokeID, asn1util.TagServiceRead, payload)
}

// MarshalWriteRequestByListName builds a ConfirmedRequestPdu for the
// MMS Write service using the variableListName CHOICE (tag [1]).
func MarshalWriteRequestByListName(invokeID codec.InvokeID, listName ObjectNameWire, values []*DataValue) ([]byte, error) {
	nameBytes, err := EncodeObjectName(listName)
	if err != nil {
		return nil, fmt.Errorf("pdu: write by list name: %w", err)
	}
	varSpec := berutil.EncodeTLV(tagVarListName, nameBytes)

	dataContent, err := MarshalDataList(values)
	if err != nil {
		return nil, fmt.Errorf("pdu: write by list name: marshal data: %w", err)
	}
	dataList := berutil.EncodeTLV(tagSequence, dataContent)

	payload := make([]byte, 0, len(varSpec)+len(dataList))
	payload = append(payload, varSpec...)
	payload = append(payload, dataList...)

	return marshalConfirmedLegacy(invokeID, asn1util.TagServiceWrite, payload)
}
