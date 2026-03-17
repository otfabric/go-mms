package pdu

import (
	"encoding/asn1"
	"fmt"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
)

// TypeSpecWire is the internal representation of an MMS TypeSpecification.
type TypeSpecWire struct {
	Tag int // context-specific tag identifying the type

	Size        int  // bit width or max length, depending on type
	FormatWidth int  // float: total format width in bits
	ExpWidth    int  // float: exponent width in bits
	BinTimeFull bool // binarytime: true = 6-byte form
	Count       int  // array: number of elements
	Element     *TypeSpecWire
	Components  []StructComponentWire
	TypeName    *ObjectNameWire // typeName [0]: the referenced named type
}

// StructComponentWire is the internal representation of a structure component.
type StructComponentWire struct {
	Name string
	Type TypeSpecWire
}

const maxTypeSpecNestingDepth = 32

// TypeSpec context-specific tag numbers.
const (
	tsTagTypeName        = 0
	tsTagArray           = 1
	tsTagStructure       = 2
	tsTagBoolean         = 3
	tsTagBitString       = 4
	tsTagInteger         = 5
	tsTagUnsigned        = 6
	tsTagFloat           = 7
	tsTagOctetString     = 9
	tsTagVisibleString   = 10
	tsTagGeneralizedTime = 11
	tsTagBinaryTime      = 12
	tsTagBCD             = 13
	tsTagObjID           = 15
	tsTagMmsString       = 16
	tsTagUTCTime         = 17
)

// MarshalGetVarAccessRequest builds a ConfirmedRequestPdu for the
// MMS GetVariableAccessAttributes service.
//
//	GetVariableAccessAttributesRequest ::= CHOICE {
//	  name    [0] EXPLICIT ObjectName
//	  address [1] EXPLICIT Address
//	}
func MarshalGetVarAccessRequest(invokeID codec.InvokeID, name ObjectNameWire) ([]byte, error) {
	objNameBytes, err := EncodeObjectName(name)
	if err != nil {
		return nil, fmt.Errorf("pdu: getvaraccess request: %w", err)
	}
	// Wrap in [0] EXPLICIT (name choice)
	payload := asn1util.WrapConstructed(0, objNameBytes)
	return marshalConfirmedLegacy(invokeID, asn1util.TagServiceGetVariableAccessAttributes, payload)
}

// GetVarAccessResult is the internal result of a GetVariableAccessAttributes response.
type GetVarAccessResult struct {
	Deletable bool
	TypeSpec  TypeSpecWire
}

// UnmarshalGetVarAccessResponse parses a GetVariableAccessAttributes response.
//
//	GetVariableAccessAttributesResponse ::= SEQUENCE {
//	  mmsDeletable     [0] IMPLICIT BOOLEAN
//	  address          [1] EXPLICIT Address  -- OPTIONAL
//	  typeSpecification [2] EXPLICIT TypeSpecification
//	}
func UnmarshalGetVarAccessResponse(serviceData asn1.RawValue) (*GetVarAccessResult, error) {
	content := serviceData.Bytes
	if len(content) == 0 {
		return nil, fmt.Errorf("pdu: getvaraccess response: empty content")
	}

	offset := 0

	// mmsDeletable [0] IMPLICIT BOOLEAN
	tag, inner, n, err := berutil.DecodeTLVAt(content, offset)
	if err != nil {
		return nil, fmt.Errorf("pdu: getvaraccess response: mmsDeletable: %w", err)
	}
	offset += n
	if tag != 0x80 {
		return nil, fmt.Errorf("pdu: getvaraccess response: expected mmsDeletable [0] (0x80), got 0x%02x", tag)
	}
	deletable := len(inner) == 1 && inner[0] != 0

	// Skip optional address [1] EXPLICIT
	if offset < len(content) {
		nextTag := content[offset]
		if nextTag == 0xa1 {
			_, _, n, err := berutil.DecodeTLVAt(content, offset)
			if err != nil {
				return nil, fmt.Errorf("pdu: getvaraccess response: address: %w", err)
			}
			offset += n
		}
	}

	// typeSpecification [2] EXPLICIT TypeSpecification
	if offset >= len(content) {
		return nil, fmt.Errorf("pdu: getvaraccess response: missing typeSpecification")
	}
	tag, inner, n, err = berutil.DecodeTLVAt(content, offset)
	if err != nil {
		return nil, fmt.Errorf("pdu: getvaraccess response: typeSpec: %w", err)
	}
	offset += n
	if tag != 0xa2 {
		return nil, fmt.Errorf("pdu: getvaraccess response: expected typeSpecification [2] (0xa2), got 0x%02x", tag)
	}

	ts, err := DecodeTypeSpec(inner)
	if err != nil {
		return nil, fmt.Errorf("pdu: getvaraccess response: %w", err)
	}

	if offset != len(content) {
		return nil, fmt.Errorf("pdu: getvaraccess response: %d trailing bytes", len(content)-offset)
	}

	return &GetVarAccessResult{Deletable: deletable, TypeSpec: ts}, nil
}

// DecodeTypeSpec decodes an MMS TypeSpecification from BER-encoded data.
func DecodeTypeSpec(data []byte) (TypeSpecWire, error) {
	return decodeTypeSpecWithDepth(data, 0)
}

func decodeTypeSpecWithDepth(data []byte, depth int) (TypeSpecWire, error) {
	if depth > maxTypeSpecNestingDepth {
		return TypeSpecWire{}, fmt.Errorf("pdu: typespec nesting depth %d exceeds maximum %d", depth, maxTypeSpecNestingDepth)
	}
	tag, content, err := berutil.DecodeTLV(data)
	if err != nil {
		return TypeSpecWire{}, fmt.Errorf("typespec: %w", err)
	}
	return decodeTypeSpecFromTLVWithDepth(tag, content, depth)
}

func decodeTypeSpecFromTLVWithDepth(tag byte, content []byte, depth int) (TypeSpecWire, error) {
	tagNum := int(tag & 0x1f)
	isConstructed := tag&0x20 != 0

	switch tagNum {
	case tsTagBoolean: // [3] NULL
		return TypeSpecWire{Tag: tsTagBoolean}, nil

	case tsTagBitString: // [4] Integer32
		size, err := berutil.DecodeInteger(content)
		if err != nil {
			return TypeSpecWire{}, fmt.Errorf("typespec bitstring size: %w", err)
		}
		return TypeSpecWire{Tag: tsTagBitString, Size: size}, nil

	case tsTagInteger: // [5] Unsigned8
		size, err := berutil.DecodeInteger(content)
		if err != nil {
			return TypeSpecWire{}, fmt.Errorf("typespec integer size: %w", err)
		}
		return TypeSpecWire{Tag: tsTagInteger, Size: size}, nil

	case tsTagUnsigned: // [6] Unsigned8
		size, err := berutil.DecodeInteger(content)
		if err != nil {
			return TypeSpecWire{}, fmt.Errorf("typespec unsigned size: %w", err)
		}
		return TypeSpecWire{Tag: tsTagUnsigned, Size: size}, nil

	case tsTagFloat: // [7] SEQUENCE { formatWidth, exponentWidth }
		if !isConstructed {
			return TypeSpecWire{}, fmt.Errorf("typespec float: expected constructed")
		}
		return decodeFloatTypeSpec(content)

	case tsTagOctetString: // [9] Integer32
		size, err := berutil.DecodeInteger(content)
		if err != nil {
			return TypeSpecWire{}, fmt.Errorf("typespec octetstring size: %w", err)
		}
		return TypeSpecWire{Tag: tsTagOctetString, Size: size}, nil

	case tsTagVisibleString: // [10] Integer32
		size, err := berutil.DecodeInteger(content)
		if err != nil {
			return TypeSpecWire{}, fmt.Errorf("typespec visiblestring size: %w", err)
		}
		return TypeSpecWire{Tag: tsTagVisibleString, Size: size}, nil

	case tsTagGeneralizedTime: // [11] NULL
		return TypeSpecWire{Tag: tsTagGeneralizedTime}, nil

	case tsTagBinaryTime: // [12] BOOLEAN (false=4-byte, true=6-byte)
		full := len(content) == 1 && content[0] != 0
		return TypeSpecWire{Tag: tsTagBinaryTime, BinTimeFull: full}, nil

	case tsTagBCD: // [13] Unsigned8
		size, err := berutil.DecodeInteger(content)
		if err != nil {
			return TypeSpecWire{}, fmt.Errorf("typespec bcd size: %w", err)
		}
		return TypeSpecWire{Tag: tsTagBCD, Size: size}, nil

	case tsTagObjID: // [15] NULL
		return TypeSpecWire{Tag: tsTagObjID}, nil

	case tsTagMmsString: // [16] Integer32
		size, err := berutil.DecodeInteger(content)
		if err != nil {
			return TypeSpecWire{}, fmt.Errorf("typespec mmsstring size: %w", err)
		}
		return TypeSpecWire{Tag: tsTagMmsString, Size: size}, nil

	case tsTagUTCTime: // [17] NULL
		return TypeSpecWire{Tag: tsTagUTCTime}, nil

	case tsTagArray: // [1] SEQUENCE { packed?, numberOfElements, elementType }
		if !isConstructed {
			return TypeSpecWire{}, fmt.Errorf("typespec array: expected constructed")
		}
		return decodeArrayTypeSpecWithDepth(content, depth)

	case tsTagStructure: // [2] SEQUENCE { packed?, components }
		if !isConstructed {
			return TypeSpecWire{}, fmt.Errorf("typespec structure: expected constructed")
		}
		return decodeStructureTypeSpecWithDepth(content, depth)

	case tsTagTypeName: // [0] EXPLICIT ObjectName
		name, err := DecodeObjectName(content)
		if err != nil {
			return TypeSpecWire{}, fmt.Errorf("typespec typeName: %w", err)
		}
		return TypeSpecWire{Tag: tsTagTypeName, TypeName: &name}, nil

	default:
		return TypeSpecWire{}, fmt.Errorf("typespec: unknown tag [%d] (0x%02x)", tagNum, tag)
	}
}

func decodeFloatTypeSpec(data []byte) (TypeSpecWire, error) {
	offset := 0

	// formatWidth: UNIVERSAL INTEGER
	tag, content, n, err := berutil.DecodeTLVAt(data, offset)
	if err != nil {
		return TypeSpecWire{}, fmt.Errorf("float formatWidth: %w", err)
	}
	offset += n
	if tag != tagInteger {
		return TypeSpecWire{}, fmt.Errorf("float formatWidth: expected INTEGER (0x02), got 0x%02x", tag)
	}
	formatWidth, err := berutil.DecodeInteger(content)
	if err != nil {
		return TypeSpecWire{}, fmt.Errorf("float formatWidth value: %w", err)
	}

	// exponentWidth: UNIVERSAL INTEGER
	tag, content, n, err = berutil.DecodeTLVAt(data, offset)
	if err != nil {
		return TypeSpecWire{}, fmt.Errorf("float exponentWidth: %w", err)
	}
	offset += n
	if tag != tagInteger {
		return TypeSpecWire{}, fmt.Errorf("float exponentWidth: expected INTEGER (0x02), got 0x%02x", tag)
	}
	if offset != len(data) {
		return TypeSpecWire{}, fmt.Errorf("float: %d trailing bytes", len(data)-offset)
	}
	expWidth, err := berutil.DecodeInteger(content)
	if err != nil {
		return TypeSpecWire{}, fmt.Errorf("float exponentWidth value: %w", err)
	}

	return TypeSpecWire{Tag: tsTagFloat, FormatWidth: formatWidth, ExpWidth: expWidth}, nil
}

func decodeArrayTypeSpecWithDepth(data []byte, depth int) (TypeSpecWire, error) {
	offset := 0

	// Skip optional packed [0] IMPLICIT BOOLEAN
	if offset < len(data) && data[offset] == 0x80 {
		_, _, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return TypeSpecWire{}, fmt.Errorf("array packed: %w", err)
		}
		offset += n
	}

	// numberOfElements [1] IMPLICIT Unsigned32
	if offset >= len(data) {
		return TypeSpecWire{}, fmt.Errorf("array: missing numberOfElements")
	}
	tag, content, n, err := berutil.DecodeTLVAt(data, offset)
	if err != nil {
		return TypeSpecWire{}, fmt.Errorf("array numberOfElements: %w", err)
	}
	offset += n
	if tag != 0x81 {
		return TypeSpecWire{}, fmt.Errorf("array numberOfElements: expected [1] (0x81), got 0x%02x", tag)
	}
	count, err := berutil.DecodeInteger(content)
	if err != nil {
		return TypeSpecWire{}, fmt.Errorf("array numberOfElements value: %w", err)
	}

	// elementType [2] EXPLICIT TypeSpecification
	if offset >= len(data) {
		return TypeSpecWire{}, fmt.Errorf("array: missing elementType")
	}
	tag, content, n, err = berutil.DecodeTLVAt(data, offset)
	if err != nil {
		return TypeSpecWire{}, fmt.Errorf("array elementType: %w", err)
	}
	offset += n
	if tag != 0xa2 {
		return TypeSpecWire{}, fmt.Errorf("array elementType: expected [2] (0xa2), got 0x%02x", tag)
	}
	if offset != len(data) {
		return TypeSpecWire{}, fmt.Errorf("array: %d trailing bytes", len(data)-offset)
	}

	elemType, err := decodeTypeSpecWithDepth(content, depth+1)
	if err != nil {
		return TypeSpecWire{}, fmt.Errorf("array elementType: %w", err)
	}

	return TypeSpecWire{Tag: tsTagArray, Count: count, Element: &elemType}, nil
}

func decodeStructureTypeSpecWithDepth(data []byte, depth int) (TypeSpecWire, error) {
	offset := 0

	// Skip optional packed [0] IMPLICIT BOOLEAN
	if offset < len(data) && data[offset] == 0x80 {
		_, _, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return TypeSpecWire{}, fmt.Errorf("structure packed: %w", err)
		}
		offset += n
	}

	// components [1] IMPLICIT SEQUENCE OF StructComponent
	if offset >= len(data) {
		return TypeSpecWire{}, fmt.Errorf("structure: missing components")
	}
	tag, content, n, err := berutil.DecodeTLVAt(data, offset)
	if err != nil {
		return TypeSpecWire{}, fmt.Errorf("structure components: %w", err)
	}
	offset += n
	if tag != 0xa1 {
		return TypeSpecWire{}, fmt.Errorf("structure components: expected [1] (0xa1), got 0x%02x", tag)
	}
	if offset != len(data) {
		return TypeSpecWire{}, fmt.Errorf("structure: %d trailing bytes", len(data)-offset)
	}

	components, err := decodeStructComponentsWithDepth(content, depth)
	if err != nil {
		return TypeSpecWire{}, err
	}

	return TypeSpecWire{Tag: tsTagStructure, Components: components}, nil
}

func decodeStructComponentsWithDepth(data []byte, depth int) ([]StructComponentWire, error) {
	var components []StructComponentWire
	offset := 0
	for offset < len(data) {
		// Each component is a SEQUENCE
		tag, content, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, fmt.Errorf("component [%d]: %w", len(components), err)
		}
		offset += n
		if tag != tagSequence {
			return nil, fmt.Errorf("component [%d]: expected SEQUENCE (0x30), got 0x%02x", len(components), tag)
		}

		comp, err := decodeStructComponentWithDepth(content, depth)
		if err != nil {
			return nil, fmt.Errorf("component [%d]: %w", len(components), err)
		}
		components = append(components, comp)
	}
	return components, nil
}

func decodeStructComponentWithDepth(data []byte, depth int) (StructComponentWire, error) {
	offset := 0
	var name string

	// Optional componentName [0] IMPLICIT Identifier
	if offset < len(data) && data[offset] == 0x80 {
		_, content, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return StructComponentWire{}, fmt.Errorf("componentName: %w", err)
		}
		offset += n
		name = string(content)
	}

	// componentType [1] EXPLICIT TypeSpecification
	if offset >= len(data) {
		return StructComponentWire{}, fmt.Errorf("missing componentType")
	}
	tag, content, n, err := berutil.DecodeTLVAt(data, offset)
	if err != nil {
		return StructComponentWire{}, fmt.Errorf("componentType: %w", err)
	}
	offset += n
	if tag != 0xa1 {
		return StructComponentWire{}, fmt.Errorf("componentType: expected [1] (0xa1), got 0x%02x", tag)
	}
	if offset != len(data) {
		return StructComponentWire{}, fmt.Errorf("structComponent: %d trailing bytes", len(data)-offset)
	}

	ts, err := decodeTypeSpecWithDepth(content, depth+1)
	if err != nil {
		return StructComponentWire{}, fmt.Errorf("componentType: %w", err)
	}

	return StructComponentWire{Name: name, Type: ts}, nil
}
