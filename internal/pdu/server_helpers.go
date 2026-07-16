// SPDX-License-Identifier: MIT

package pdu

import (
	"fmt"

	"github.com/otfabric/go-mms/internal/berutil"
)

// ---- Server-side request unmarshalers ----

// GetNameListRequest is the server-side parsed GetNameList request.
type GetNameListRequest struct {
	ObjectClass   int
	Scope         int
	DomainID      string
	ContinueAfter string
}

// UnmarshalGetNameListRequest parses the service body of a GetNameList request.
//
//	GetNameListRequest ::= SEQUENCE {
//	  objectClass   [0] EXPLICIT ObjectClass
//	  objectScope   [1] EXPLICIT ObjectScope
//	  continueAfter [2] IMPLICIT Identifier OPTIONAL
//	}
func UnmarshalGetNameListRequest(data []byte) (*GetNameListRequest, error) {
	offset := 0

	// objectClass [0] EXPLICIT { basicObjectClass [0] IMPLICIT INTEGER }
	tag, content, n, err := berutil.DecodeTLVAt(data, offset)
	if err != nil {
		return nil, fmt.Errorf("getnamelist request: objectClass: %w", err)
	}
	offset += n
	if tag != 0xa0 {
		return nil, fmt.Errorf("getnamelist request: expected objectClass [0] (0xa0), got 0x%02x", tag)
	}
	tag, inner, innerN, err := berutil.DecodeTLVAt(content, 0)
	if err != nil {
		return nil, fmt.Errorf("getnamelist request: objectClass inner: %w", err)
	}
	if tag != 0x80 {
		return nil, fmt.Errorf("getnamelist request: expected basicObjectClass [0] (0x80), got 0x%02x", tag)
	}
	if innerN != len(content) {
		return nil, fmt.Errorf("getnamelist request: objectClass: %d trailing bytes in explicit wrapper", len(content)-innerN)
	}
	objectClass, err := berutil.DecodeInteger(inner)
	if err != nil {
		return nil, fmt.Errorf("getnamelist request: objectClass value: %w", err)
	}

	// objectScope [1] EXPLICIT CHOICE
	if offset >= len(data) {
		return nil, fmt.Errorf("getnamelist request: missing objectScope")
	}
	tag, content, n, err = berutil.DecodeTLVAt(data, offset)
	if err != nil {
		return nil, fmt.Errorf("getnamelist request: objectScope: %w", err)
	}
	offset += n
	if tag != 0xa1 {
		return nil, fmt.Errorf("getnamelist request: expected objectScope [1] (0xa1), got 0x%02x", tag)
	}

	var scope int
	var domainID string
	if len(content) == 0 {
		return nil, fmt.Errorf("getnamelist request: empty objectScope wrapper")
	}
	scopeTag, scopeInner, scopeN, err := berutil.DecodeTLVAt(content, 0)
	if err != nil {
		return nil, fmt.Errorf("getnamelist request: objectScope inner: %w", err)
	}
	if scopeN != len(content) {
		return nil, fmt.Errorf("getnamelist request: objectScope: %d trailing bytes in explicit wrapper", len(content)-scopeN)
	}
	switch scopeTag {
	case 0x80: // vmdSpecific
		scope = ScopeVMD
	case 0x81: // domainSpecific
		scope = ScopeDomain
		domainID = string(scopeInner)
	case 0x82: // aaSpecific
		scope = ScopeAssociation
	default:
		return nil, fmt.Errorf("getnamelist request: unknown scope tag 0x%02x", scopeTag)
	}

	// continueAfter [2] IMPLICIT Identifier OPTIONAL
	var continueAfter string
	if offset < len(data) {
		tag, content, n, err = berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, fmt.Errorf("getnamelist request: continueAfter: %w", err)
		}
		offset += n
		if tag == 0x82 {
			continueAfter = string(content)
		}
	}
	if offset != len(data) {
		return nil, fmt.Errorf("getnamelist request: %d trailing bytes", len(data)-offset)
	}

	return &GetNameListRequest{
		ObjectClass:   objectClass,
		Scope:         scope,
		DomainID:      domainID,
		ContinueAfter: continueAfter,
	}, nil
}

// UnmarshalGetVarAccessRequest parses the service body of a
// GetVariableAccessAttributes request, extracting the ObjectName.
func UnmarshalGetVarAccessRequest(data []byte) (ObjectNameWire, error) {
	// The service body is CHOICE { name [0] EXPLICIT ObjectName, address [1] ... }
	tag, content, n, err := berutil.DecodeTLVAt(data, 0)
	if err != nil {
		return ObjectNameWire{}, fmt.Errorf("getvaraccess request: %w", err)
	}
	if tag != 0xa0 {
		return ObjectNameWire{}, fmt.Errorf("getvaraccess request: expected name [0] (0xa0), got 0x%02x (address not supported)", tag)
	}
	if n != len(data) {
		return ObjectNameWire{}, fmt.Errorf("getvaraccess request: %d trailing bytes", len(data)-n)
	}
	return DecodeObjectName(content)
}

// UnmarshalReadRequest parses the service body of a Read request,
// returning the list of variable names (without alternate access).
func UnmarshalReadRequest(data []byte) ([]ObjectNameWire, error) {
	specs, err := UnmarshalReadRequestFull(data)
	if err != nil {
		return nil, err
	}
	vars := make([]ObjectNameWire, len(specs))
	for i, s := range specs {
		vars[i] = s.Name
	}
	return vars, nil
}

// ReadRequestWire holds the parsed read request, which uses either
// listOfVariable or variableListName addressing.
type ReadRequestWire struct {
	SpecWithResult bool
	Variables      []VariableSpecWire // set when listOfVariable
	ListName       *ObjectNameWire    // set when variableListName
}

// UnmarshalReadRequestFull parses the service body of a Read request,
// returning variable specs including any alternate access.
func UnmarshalReadRequestFull(data []byte) ([]VariableSpecWire, error) {
	req, err := UnmarshalReadRequestParsed(data)
	if err != nil {
		return nil, err
	}
	if req.ListName != nil {
		return nil, fmt.Errorf("read request: variableListName not supported in this path")
	}
	return req.Variables, nil
}

// UnmarshalReadRequestParsed parses a Read request into the full
// wire representation, supporting both listOfVariable and variableListName.
func UnmarshalReadRequestParsed(data []byte) (*ReadRequestWire, error) {
	offset := 0
	result := &ReadRequestWire{}

	// Optional specificationWithResult [0] IMPLICIT BOOLEAN
	if offset < len(data) && data[offset] == 0x80 {
		_, content, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, fmt.Errorf("read request: specificationWithResult: %w", err)
		}
		offset += n
		if len(content) > 0 && content[0] != 0 {
			result.SpecWithResult = true
		}
	}

	if offset >= len(data) {
		return nil, fmt.Errorf("read request: missing variable access specification")
	}
	tag, content, n, err := berutil.DecodeTLVAt(data, offset)
	if err != nil {
		return nil, fmt.Errorf("read request: varAccessSpec: %w", err)
	}
	offset += n

	if offset != len(data) {
		return nil, fmt.Errorf("read request: %d trailing bytes", len(data)-offset)
	}

	switch tag {
	case tagListOfVar:
		specs, err := decodeVarSpecListFull(content)
		if err != nil {
			return nil, err
		}
		result.Variables = specs
		return result, nil
	case tagVarListName:
		name, err := DecodeObjectName(content)
		if err != nil {
			return nil, fmt.Errorf("read request: variableListName: %w", err)
		}
		result.ListName = &name
		return result, nil
	default:
		return nil, fmt.Errorf("read request: expected listOfVariable [0] or variableListName [1], got 0x%02x", tag)
	}
}

func decodeVarSpecListFull(data []byte) ([]VariableSpecWire, error) {
	var specs []VariableSpecWire
	offset := 0
	for offset < len(data) {
		tag, content, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, fmt.Errorf("varSpec [%d]: %w", len(specs), err)
		}
		offset += n
		if tag != tagSequence {
			return nil, fmt.Errorf("varSpec [%d]: expected SEQUENCE (0x30), got 0x%02x", len(specs), tag)
		}
		spec, err := decodeVariableSpecWire(content)
		if err != nil {
			return nil, fmt.Errorf("varSpec [%d]: %w", len(specs), err)
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

func decodeVariableSpecWire(data []byte) (VariableSpecWire, error) {
	name, consumed, err := DecodeObjectNameAt(data, 0)
	if err != nil {
		return VariableSpecWire{}, err
	}

	spec := VariableSpecWire{Name: name}

	if consumed < len(data) {
		tag, content, n, err := berutil.DecodeTLVAt(data, consumed)
		if err != nil {
			return VariableSpecWire{}, fmt.Errorf("alternate access: %w", err)
		}
		consumed += n
		if tag != tagAltAccessWrapper {
			return VariableSpecWire{}, fmt.Errorf("expected alternateAccess [5] (0xa5), got 0x%02x", tag)
		}
		selectors, err := decodeAlternateAccess(content)
		if err != nil {
			return VariableSpecWire{}, fmt.Errorf("alternate access: %w", err)
		}
		spec.AlternateAccess = selectors
	}

	if consumed != len(data) {
		return VariableSpecWire{}, fmt.Errorf("%d trailing bytes after variable spec", len(data)-consumed)
	}

	return spec, nil
}

// UnmarshalWriteRequest parses the service body of a Write request.
func UnmarshalWriteRequest(data []byte) ([]ObjectNameWire, []*DataValue, error) {
	specs, values, err := UnmarshalWriteRequestFull(data)
	if err != nil {
		return nil, nil, err
	}
	vars := make([]ObjectNameWire, len(specs))
	for i, s := range specs {
		vars[i] = s.Name
	}
	return vars, values, nil
}

// WriteRequestWire holds the parsed write request, which uses either
// listOfVariable or variableListName addressing.
type WriteRequestWire struct {
	Variables []VariableSpecWire // set when listOfVariable
	ListName  *ObjectNameWire    // set when variableListName
	Values    []*DataValue
}

// UnmarshalWriteRequestFull parses the service body of a Write request,
// returning variable specs including any alternate access.
func UnmarshalWriteRequestFull(data []byte) ([]VariableSpecWire, []*DataValue, error) {
	req, err := UnmarshalWriteRequestParsed(data)
	if err != nil {
		return nil, nil, err
	}
	if req.ListName != nil {
		return nil, nil, fmt.Errorf("write request: variableListName not supported in this path")
	}
	return req.Variables, req.Values, nil
}

// UnmarshalWriteRequestParsed parses a Write request into the full
// wire representation, supporting both listOfVariable and variableListName.
func UnmarshalWriteRequestParsed(data []byte) (*WriteRequestWire, error) {
	offset := 0

	if offset >= len(data) {
		return nil, fmt.Errorf("write request: missing variable access specification")
	}
	tag, content, n, err := berutil.DecodeTLVAt(data, offset)
	if err != nil {
		return nil, fmt.Errorf("write request: varAccessSpec: %w", err)
	}
	offset += n

	var result WriteRequestWire

	switch tag {
	case tagListOfVar:
		specs, err := decodeVarSpecListFull(content)
		if err != nil {
			return nil, fmt.Errorf("write request: %w", err)
		}
		result.Variables = specs
	case tagVarListName:
		name, err := DecodeObjectName(content)
		if err != nil {
			return nil, fmt.Errorf("write request: variableListName: %w", err)
		}
		result.ListName = &name
	default:
		return nil, fmt.Errorf("write request: expected listOfVariable [0] or variableListName [1], got 0x%02x", tag)
	}

	// listOfData: SEQUENCE OF Data
	if offset >= len(data) {
		return nil, fmt.Errorf("write request: missing data list")
	}
	tag, content, n, err = berutil.DecodeTLVAt(data, offset)
	if err != nil {
		return nil, fmt.Errorf("write request: dataList: %w", err)
	}
	offset += n
	if tag != tagSequence {
		return nil, fmt.Errorf("write request: expected SEQUENCE OF (0x30), got 0x%02x", tag)
	}
	if offset != len(data) {
		return nil, fmt.Errorf("write request: %d trailing bytes", len(data)-offset)
	}

	var values []*DataValue
	dataOffset := 0
	for dataOffset < len(content) {
		dv, consumed, err := UnmarshalDataElement(content, dataOffset)
		if err != nil {
			return nil, fmt.Errorf("write request: data [%d]: %w", len(values), err)
		}
		dataOffset += consumed
		values = append(values, dv)
	}
	result.Values = values

	if result.ListName == nil && len(result.Variables) != len(values) {
		return nil, fmt.Errorf("write request: variable count (%d) != value count (%d)", len(result.Variables), len(values))
	}

	return &result, nil
}

// ---- Server-side response marshalers ----

// AccessResult represents a per-variable result in a Read response.
type AccessResult struct {
	IsError   bool
	ErrorCode int
	Data      *DataValue
}

// MarshalGetNameListResponse encodes a GetNameList response body.
//
//	GetNameListResponse ::= SEQUENCE {
//	  listOfIdentifier [0] IMPLICIT SEQUENCE OF Identifier
//	  moreFollows      [1] IMPLICIT BOOLEAN DEFAULT TRUE
//	}
func MarshalGetNameListResponse(names []string, moreFollows bool) ([]byte, error) {
	var identifiers []byte
	for _, name := range names {
		identifiers = append(identifiers, berutil.EncodeTLV(tagVisibleString, []byte(name))...)
	}
	listTLV := berutil.EncodeTLV(0xa0, identifiers) // [0] IMPLICIT SEQUENCE OF

	var result []byte
	result = append(result, listTLV...)

	if !moreFollows {
		result = append(result, berutil.EncodeTLV(0x81, []byte{0x00})...) // [1] IMPLICIT BOOLEAN
	}

	return result, nil
}

// MarshalReadResponse encodes a Read response body.
//
//	ReadResponse ::= SEQUENCE {
//	  variableAccessSpecification  -- skipped (optional)
//	  listOfAccessResult   SEQUENCE OF AccessResult
//	}
func MarshalReadResponse(results []*AccessResult) ([]byte, error) {
	var inner []byte
	for _, r := range results {
		if r.IsError {
			encoded := berutil.EncodeTLV(TagDataAccessError, encodeSmallInt(r.ErrorCode))
			inner = append(inner, encoded...)
		} else {
			encoded, err := MarshalData(r.Data)
			if err != nil {
				return nil, fmt.Errorf("read response: marshal data: %w", err)
			}
			inner = append(inner, encoded...)
		}
	}
	return berutil.EncodeTLV(tagSequence, inner), nil // wrap in SEQUENCE OF
}

// MarshalReadResponseWithSpec encodes a Read response body that includes
// the variableAccessSpecification (for specificationWithResult=true).
//
//	ReadResponse ::= SEQUENCE {
//	  variableAccessSpecification VariableAccessSpecification OPTIONAL
//	  listOfAccessResult          SEQUENCE OF AccessResult
//	}
func MarshalReadResponseWithSpec(listName *ObjectNameWire, specs []VariableSpecWire, results []*AccessResult) ([]byte, error) {
	var body []byte

	if listName != nil {
		nameBytes, err := EncodeObjectName(*listName)
		if err != nil {
			return nil, fmt.Errorf("read response: variableListName: %w", err)
		}
		body = append(body, berutil.EncodeTLV(tagVarListName, nameBytes)...)
	} else if len(specs) > 0 {
		var varEntries []byte
		for i, s := range specs {
			nameBytes, err := EncodeObjectName(s.Name)
			if err != nil {
				return nil, fmt.Errorf("read response: variable [%d]: %w", i, err)
			}
			varSpec := berutil.EncodeTLV(0xa0, nameBytes)
			var seqContent []byte
			seqContent = append(seqContent, varSpec...)
			if len(s.AlternateAccess) > 0 {
				aaBytes, err := encodeAlternateAccess(s.AlternateAccess)
				if err != nil {
					return nil, fmt.Errorf("read response: variable [%d] alternate access: %w", i, err)
				}
				seqContent = append(seqContent, berutil.EncodeTLV(tagAltAccessWrapper, aaBytes)...)
			}
			varEntries = append(varEntries, berutil.EncodeTLV(tagSequence, seqContent)...)
		}
		body = append(body, berutil.EncodeTLV(tagListOfVar, varEntries)...)
	}

	var inner []byte
	for _, r := range results {
		if r.IsError {
			inner = append(inner, berutil.EncodeTLV(TagDataAccessError, encodeSmallInt(r.ErrorCode))...)
		} else {
			encoded, err := MarshalData(r.Data)
			if err != nil {
				return nil, fmt.Errorf("read response: marshal data: %w", err)
			}
			inner = append(inner, encoded...)
		}
	}
	body = append(body, berutil.EncodeTLV(tagSequence, inner)...)

	return body, nil
}

// MarshalWriteResponse encodes a Write response body.
//
//	WriteResponse ::= SEQUENCE OF CHOICE {
//	  failure [0] IMPLICIT DataAccessError
//	  success [1] IMPLICIT NULL
//	}
func MarshalWriteResponse(results []int) ([]byte, error) {
	var encoded []byte
	for _, code := range results {
		if code == 0 {
			encoded = append(encoded, berutil.EncodeTLV(tagWriteSuccess, nil)...) // [1] IMPLICIT NULL
		} else {
			encoded = append(encoded, berutil.EncodeTLV(tagWriteFailure, encodeSmallInt(code))...) // [0] IMPLICIT DataAccessError
		}
	}
	return encoded, nil
}

// MarshalGetVarAccessResponse encodes a GetVariableAccessAttributes response body.
//
//	GetVariableAccessAttributesResponse ::= SEQUENCE {
//	  mmsDeletable      [0] IMPLICIT BOOLEAN
//	  address           [1] EXPLICIT Address OPTIONAL  -- omitted
//	  typeSpecification [2] EXPLICIT TypeSpecification
//	}
func MarshalGetVarAccessResponse(deletable bool, ts TypeSpecWire) ([]byte, error) {
	delByte := byte(0x00)
	if deletable {
		delByte = 0xff
	}
	delTLV := berutil.EncodeTLV(0x80, []byte{delByte}) // [0] IMPLICIT BOOLEAN

	tsBytes, err := EncodeTypeSpec(ts)
	if err != nil {
		return nil, fmt.Errorf("getvaraccess response: typespec: %w", err)
	}
	tsTLV := berutil.EncodeTLV(0xa2, tsBytes) // [2] EXPLICIT TypeSpecification

	var result []byte
	result = append(result, delTLV...)
	result = append(result, tsTLV...)
	return result, nil
}

// EncodeTypeSpec encodes a TypeSpecWire into BER format.
func EncodeTypeSpec(ts TypeSpecWire) ([]byte, error) {
	switch ts.Tag {
	case tsTagBoolean:
		return berutil.EncodeTLV(0x83, nil), nil // [3] NULL
	case tsTagBitString:
		return berutil.EncodeTLV(0x84, encodeSmallInt(ts.Size)), nil // [4] Integer32
	case tsTagInteger:
		return berutil.EncodeTLV(0x85, encodeSmallInt(ts.Size)), nil // [5] Unsigned8
	case tsTagUnsigned:
		return berutil.EncodeTLV(0x86, encodeSmallInt(ts.Size)), nil // [6] Unsigned8
	case tsTagFloat:
		fmtWidth := berutil.EncodeTLV(0x02, encodeSmallInt(ts.FormatWidth))
		expWidth := berutil.EncodeTLV(0x02, encodeSmallInt(ts.ExpWidth))
		inner := append(fmtWidth, expWidth...)
		return berutil.EncodeTLV(0xa7, inner), nil // [7] CONSTRUCTED
	case tsTagOctetString:
		return berutil.EncodeTLV(0x89, encodeSmallInt(ts.Size)), nil // [9] Integer32
	case tsTagVisibleString:
		return berutil.EncodeTLV(0x8a, encodeSmallInt(ts.Size)), nil // [10] Integer32
	case tsTagMmsString:
		return berutil.EncodeTLV(0x90, encodeSmallInt(ts.Size)), nil // [16] Integer32
	case tsTagUTCTime:
		return berutil.EncodeTLV(0x91, nil), nil // [17] NULL
	case tsTagBinaryTime:
		b := byte(0x00)
		if ts.BinTimeFull {
			b = 0xff
		}
		return berutil.EncodeTLV(0x8c, []byte{b}), nil // [12] BOOLEAN
	case tsTagArray:
		var inner []byte
		countTLV := berutil.EncodeTLV(0x81, encodeSmallInt(ts.Count)) // numberOfElements [1]
		inner = append(inner, countTLV...)
		if ts.Element != nil {
			elemBytes, err := EncodeTypeSpec(*ts.Element)
			if err != nil {
				return nil, fmt.Errorf("array elementType: %w", err)
			}
			elemTLV := berutil.EncodeTLV(0xa2, elemBytes) // elementType [2] EXPLICIT
			inner = append(inner, elemTLV...)
		}
		return berutil.EncodeTLV(0xa1, inner), nil // [1] CONSTRUCTED
	case tsTagStructure:
		var compsInner []byte
		for _, comp := range ts.Components {
			var compInner []byte
			if comp.Name != "" {
				compInner = append(compInner, berutil.EncodeTLV(0x80, []byte(comp.Name))...) // componentName [0]
			}
			elemBytes, err := EncodeTypeSpec(comp.Type)
			if err != nil {
				return nil, fmt.Errorf("structure component %q: %w", comp.Name, err)
			}
			compInner = append(compInner, berutil.EncodeTLV(0xa1, elemBytes)...)   // componentType [1] EXPLICIT
			compsInner = append(compsInner, berutil.EncodeTLV(0x30, compInner)...) // SEQUENCE
		}
		inner := berutil.EncodeTLV(0xa1, compsInner) // components [1] IMPLICIT SEQUENCE OF
		return berutil.EncodeTLV(0xa2, inner), nil   // [2] CONSTRUCTED
	case tsTagTypeName:
		if ts.TypeName != nil {
			nameBytes, err := EncodeObjectName(*ts.TypeName)
			if err != nil {
				return nil, fmt.Errorf("typeName: %w", err)
			}
			return berutil.EncodeTLV(0xa0, nameBytes), nil // [0] EXPLICIT
		}
		return nil, fmt.Errorf("typespec typeName: nil TypeName")
	default:
		return nil, fmt.Errorf("typespec: unsupported tag %d", ts.Tag)
	}
}

func encodeSmallInt(v int) []byte {
	return berutil.EncodeInt(v)
}

// ---- Named Variable List server-side helpers ----

// DefineNVLRequest is the server-side parsed DefineNamedVariableList request.
type DefineNVLRequest struct {
	ListName  ObjectNameWire
	Variables []VariableSpecWire
}

// UnmarshalDefineNVLRequest parses the service body of a DefineNamedVariableList request.
func UnmarshalDefineNVLRequest(data []byte) (*DefineNVLRequest, error) {
	offset := 0

	// variableListName: ObjectName
	listName, n, err := DecodeObjectNameAt(data, offset)
	if err != nil {
		return nil, fmt.Errorf("define-nvl request: listName: %w", err)
	}
	offset += n

	// listOfVariable [0] IMPLICIT SEQUENCE OF SEQUENCE { variableSpecification }
	if offset >= len(data) {
		return nil, fmt.Errorf("define-nvl request: missing listOfVariable")
	}
	tag, content, n, err := berutil.DecodeTLVAt(data, offset)
	if err != nil {
		return nil, fmt.Errorf("define-nvl request: listOfVariable: %w", err)
	}
	offset += n
	if tag != 0xa0 {
		return nil, fmt.Errorf("define-nvl request: expected listOfVariable [0] (0xa0), got 0x%02x", tag)
	}
	if offset != len(data) {
		return nil, fmt.Errorf("define-nvl request: %d trailing bytes", len(data)-offset)
	}

	vars, err := decodeDefineNVLVarList(content)
	if err != nil {
		return nil, fmt.Errorf("define-nvl request: %w", err)
	}

	return &DefineNVLRequest{ListName: listName, Variables: vars}, nil
}

func decodeDefineNVLVarList(data []byte) ([]VariableSpecWire, error) {
	var vars []VariableSpecWire
	offset := 0
	for offset < len(data) {
		tag, content, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, fmt.Errorf("variable [%d]: %w", len(vars), err)
		}
		offset += n
		if tag != tagSequence {
			return nil, fmt.Errorf("variable [%d]: expected SEQUENCE (0x30), got 0x%02x", len(vars), tag)
		}
		spec, err := decodeVariableSpecFull(content)
		if err != nil {
			return nil, fmt.Errorf("variable [%d]: %w", len(vars), err)
		}
		vars = append(vars, spec)
	}
	return vars, nil
}

// MarshalDefineNVLResponse builds a DefineNamedVariableList response (empty body).
func MarshalDefineNVLResponse() ([]byte, error) {
	return nil, nil
}

// GetNVLAttrsRequest is the server-side parsed request for GetNamedVariableListAttributes.
type GetNVLAttrsRequest struct {
	ListName ObjectNameWire
}

// UnmarshalGetNVLAttrsRequest parses the service body of a GetNamedVariableListAttributes request.
func UnmarshalGetNVLAttrsRequest(data []byte) (*GetNVLAttrsRequest, error) {
	listName, n, err := DecodeObjectNameAt(data, 0)
	if err != nil {
		return nil, fmt.Errorf("get-nvl-attrs request: listName: %w", err)
	}
	if n != len(data) {
		return nil, fmt.Errorf("get-nvl-attrs request: %d trailing bytes", len(data)-n)
	}
	return &GetNVLAttrsRequest{ListName: listName}, nil
}

// MarshalGetNVLAttrsResponse builds a GetNamedVariableListAttributes response.
func MarshalGetNVLAttrsResponse(deletable bool, variables []VariableSpecWire) ([]byte, error) {
	var content []byte

	delVal := byte(0x00)
	if deletable {
		delVal = 0xff
	}
	content = berutil.AppendTLV(content, 0x80, []byte{delVal})

	var entries []byte
	for i, v := range variables {
		nameBytes, err := EncodeObjectName(v.Name)
		if err != nil {
			return nil, fmt.Errorf("variable [%d]: %w", i, err)
		}
		varSpec := berutil.EncodeTLV(0xa0, nameBytes)
		var seqContent []byte
		seqContent = append(seqContent, varSpec...)
		if len(v.AlternateAccess) > 0 {
			aaBytes, err := encodeAlternateAccess(v.AlternateAccess)
			if err != nil {
				return nil, fmt.Errorf("variable [%d] alternate access: %w", i, err)
			}
			seqContent = append(seqContent, berutil.EncodeTLV(tagAltAccessWrapper, aaBytes)...)
		}
		entries = append(entries, berutil.EncodeTLV(tagSequence, seqContent)...)
	}
	content = berutil.AppendTLV(content, 0xa1, entries)

	return content, nil
}

// DeleteNVLRequest is the server-side parsed DeleteNamedVariableList request.
type DeleteNVLRequest struct {
	ScopeOfDelete int // 0=specific (default), 1=aa-specific, 2=domain, 3=vmd
	ListNames     []ObjectNameWire
	DomainName    string
}

// UnmarshalDeleteNVLRequest parses the service body of a DeleteNamedVariableList request.
func UnmarshalDeleteNVLRequest(data []byte) (*DeleteNVLRequest, error) {
	offset := 0
	req := &DeleteNVLRequest{ScopeOfDelete: 0}

	// scopeOfDelete [0] IMPLICIT INTEGER OPTIONAL (default 0)
	if offset < len(data) {
		tag, content, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, fmt.Errorf("delete-nvl request: %w", err)
		}
		if tag == 0x80 {
			scope, err := berutil.DecodeInteger(content)
			if err != nil {
				return nil, fmt.Errorf("delete-nvl request: scopeOfDelete: %w", err)
			}
			req.ScopeOfDelete = scope
			offset += n
		}
	}

	// listOfVariableListName [1] IMPLICIT SEQUENCE OF ObjectName
	if offset >= len(data) {
		return nil, fmt.Errorf("delete-nvl request: missing listOfVariableListName")
	}
	tag, content, n, err := berutil.DecodeTLVAt(data, offset)
	if err != nil {
		return nil, fmt.Errorf("delete-nvl request: listOfNames: %w", err)
	}
	offset += n
	if tag != 0xa1 {
		return nil, fmt.Errorf("delete-nvl request: expected [1] (0xa1), got 0x%02x", tag)
	}

	nameOff := 0
	for nameOff < len(content) {
		name, consumed, err := DecodeObjectNameAt(content, nameOff)
		if err != nil {
			return nil, fmt.Errorf("delete-nvl request: name [%d]: %w", len(req.ListNames), err)
		}
		nameOff += consumed
		req.ListNames = append(req.ListNames, name)
	}

	// domainName [2] IMPLICIT Identifier OPTIONAL
	if offset < len(data) {
		tag, content, n, err = berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, fmt.Errorf("delete-nvl request: domainName: %w", err)
		}
		if tag == 0x82 {
			req.DomainName = string(content)
			offset += n
		}
	}

	if offset != len(data) {
		return nil, fmt.Errorf("delete-nvl request: %d trailing bytes", len(data)-offset)
	}

	return req, nil
}

// MarshalDeleteNVLResponse builds a DeleteNamedVariableList response.
func MarshalDeleteNVLResponse(numberMatched, numberDeleted int) ([]byte, error) {
	var content []byte
	content = berutil.AppendTLV(content, 0x02, encodeSmallInt(numberMatched))
	content = berutil.AppendTLV(content, 0x02, encodeSmallInt(numberDeleted))
	return content, nil
}
