// SPDX-License-Identifier: MIT

package pdu

import (
	"fmt"

	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
)

// ServiceErrorWire holds the fields of an ISO 9506-2 clause 7.4.1
// ServiceError:
//
//	ServiceError ::= SEQUENCE {
//	    errorClass            [0] CHOICE { vmd-state [0] IMPLICIT INTEGER, ..., others [12] IMPLICIT INTEGER },
//	    additionalCode        [1] IMPLICIT INTEGER OPTIONAL,
//	    additionalDescription [2] IMPLICIT VisibleString OPTIONAL,
//	    serviceSpecificInfo   [3] CHOICE { obtainFile [0] ..., ... } OPTIONAL
//	}
type ServiceErrorWire struct {
	ErrorClass            int
	ErrorCode             int
	AdditionalCode        *int
	AdditionalDescription *string
	// ServiceSpecific is set when serviceSpecificInfo is present.
	ServiceSpecific *ServiceSpecificWire
}

// ServiceSpecificWire is the serviceSpecificInfo [3] CHOICE of a
// ServiceError: Kind is the alternative's tag number (0 obtainFile ...
// 6 deleteNamedVariableList, 9 fileRename ...), Raw its contents
// octets. HasValue and Value carry the INTEGER/ENUMERATED/Unsigned32
// value of a primitive alternative (for example numberDeleted of
// DeleteNamedVariableList-Error).
type ServiceSpecificWire struct {
	Kind     int
	Raw      []byte
	HasValue bool
	Value    int
}

// ConfirmedError holds the parsed fields of a ConfirmedErrorPDU.
type ConfirmedError struct {
	InvokeID   codec.InvokeID
	ErrorClass int
	ErrorCode  int
	// ModifierPosition is the optional modifierPosition [1].
	ModifierPosition *uint32
	// Detail is the complete ServiceError; ErrorClass and ErrorCode
	// repeat Detail's.
	Detail ServiceErrorWire
}

// DecodeConfirmedError parses the inner content of a ConfirmedErrorPDU
// (after the 0xa2 outer tag is stripped). ISO 9506-2 clause 7.4:
//
//	Confirmed-ErrorPDU ::= SEQUENCE {
//	    invokeID         [0] IMPLICIT Unsigned32,
//	    modifierPosition [1] IMPLICIT Unsigned32 OPTIONAL,
//	    serviceError     [2] IMPLICIT ServiceError
//	}
//
// The SEQUENCE order is enforced and every component may occur once.
func DecodeConfirmedError(content []byte) (*ConfirmedError, error) {
	if len(content) == 0 {
		return nil, fmt.Errorf("pdu: confirmed error: empty content")
	}

	result := &ConfirmedError{}
	offset := 0
	stage := 0 // 0 expect invokeID, 1 modifierPosition or serviceError, 2 done

	for offset < len(content) {
		tag, inner, n, err := berutil.DecodeTLVAt(content, offset)
		if err != nil {
			return nil, fmt.Errorf("pdu: confirmed error field: %w", err)
		}
		offset += n

		switch {
		case tag == 0x80 && stage == 0: // invokeID
			id, err := berutil.DecodeUnsigned(inner)
			if err != nil {
				return nil, fmt.Errorf("pdu: confirmed error invokeID: %w", err)
			}
			result.InvokeID = id
			stage = 1
		case tag == 0x81 && stage == 1 && result.ModifierPosition == nil: // modifierPosition
			pos, err := berutil.DecodeUnsigned(inner)
			if err != nil {
				return nil, fmt.Errorf("pdu: confirmed error modifierPosition: %w", err)
			}
			result.ModifierPosition = &pos
		case tag == 0xa2 && stage == 1: // serviceError
			se, err := DecodeServiceError(inner)
			if err != nil {
				return nil, err
			}
			result.Detail = se
			result.ErrorClass = se.ErrorClass
			result.ErrorCode = se.ErrorCode
			stage = 2
		case stage == 0:
			return nil, fmt.Errorf("pdu: confirmed error: missing invokeID [0], got tag 0x%02x", tag)
		default:
			return nil, fmt.Errorf("pdu: confirmed error: unexpected tag 0x%02x", tag)
		}
	}

	switch stage {
	case 0:
		return nil, fmt.Errorf("pdu: confirmed error: missing invokeID")
	case 1:
		return nil, fmt.Errorf("pdu: confirmed error: missing serviceError")
	}
	return result, nil
}

// DecodeServiceError parses the contents of a ServiceError SEQUENCE
// (ISO 9506-2 clause 7.4.1). errorClass is mandatory and must be exactly
// one primitive alternative [0]..[12] with a non-empty INTEGER; the
// optional components must appear in order and at most once.
func DecodeServiceError(data []byte) (ServiceErrorWire, error) {
	var se ServiceErrorWire
	offset := 0
	next := 0 // lowest component tag number still allowed
	for offset < len(data) {
		tag, inner, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return ServiceErrorWire{}, fmt.Errorf("pdu: service error field: %w", err)
		}
		offset += n
		num := int(tag & 0x1f)
		if tag&0xc0 != 0x80 || num > 3 || num < next {
			return ServiceErrorWire{}, fmt.Errorf("pdu: service error: unexpected tag 0x%02x", tag)
		}
		if next == 0 && num != 0 {
			return ServiceErrorWire{}, fmt.Errorf("pdu: service error: missing errorClass [0]")
		}
		next = num + 1
		switch tag {
		case 0xa0: // errorClass
			if err := parseErrorClass(inner, &se); err != nil {
				return ServiceErrorWire{}, err
			}
		case 0x81: // additionalCode
			v, err := berutil.DecodeInteger(inner)
			if err != nil {
				return ServiceErrorWire{}, fmt.Errorf("pdu: service error additionalCode: %w", err)
			}
			se.AdditionalCode = &v
		case 0x82: // additionalDescription
			d, err := decodeVisibleString(inner)
			if err != nil {
				return ServiceErrorWire{}, fmt.Errorf("pdu: service error additionalDescription: %w", err)
			}
			se.AdditionalDescription = &d
		case 0xa3: // serviceSpecificInfo
			spec, err := parseServiceSpecific(inner)
			if err != nil {
				return ServiceErrorWire{}, err
			}
			se.ServiceSpecific = spec
		default:
			return ServiceErrorWire{}, fmt.Errorf("pdu: service error: tag 0x%02x has the wrong form", tag)
		}
	}
	if next == 0 {
		return ServiceErrorWire{}, fmt.Errorf("pdu: service error: missing errorClass [0]")
	}
	return se, nil
}

// parseErrorClass parses the CHOICE inside errorClass [0]: exactly one
// primitive context alternative [0] vmd-state .. [12] others, each an
// IMPLICIT INTEGER.
func parseErrorClass(data []byte, se *ServiceErrorWire) error {
	if len(data) == 0 {
		return fmt.Errorf("pdu: error class: empty CHOICE")
	}
	tag, inner, n, err := berutil.DecodeTLVAt(data, 0)
	if err != nil {
		return fmt.Errorf("pdu: error class: %w", err)
	}
	if n != len(data) {
		return fmt.Errorf("pdu: error class: %d trailing bytes", len(data)-n)
	}
	if tag < 0x80 || tag > 0x8c {
		return fmt.Errorf("pdu: error class: tag 0x%02x is not a class [0]..[12]", tag)
	}
	val, err := berutil.DecodeInteger(inner)
	if err != nil {
		return fmt.Errorf("pdu: error code: %w", err)
	}
	se.ErrorClass = int(tag) - 0x80
	se.ErrorCode = val
	return nil
}

// parseServiceSpecific parses serviceSpecificInfo [3]: a CHOICE with
// exactly one context-tagged alternative (ISO 9506-2 clause 7.4.1).
func parseServiceSpecific(data []byte) (*ServiceSpecificWire, error) {
	el, n, err := berutil.DecodeElement(data, 0)
	if err != nil {
		return nil, fmt.Errorf("pdu: service error serviceSpecificInfo: %w", err)
	}
	if n != len(data) || el.Class != 0x80 {
		return nil, fmt.Errorf("pdu: service error serviceSpecificInfo: not one context alternative")
	}
	spec := &ServiceSpecificWire{Kind: el.Tag, Raw: el.Content}
	if !el.Constructed && len(el.Content) > 0 {
		if v, err := berutil.DecodeInteger(el.Content); err == nil {
			spec.Value, spec.HasValue = v, true
		}
	}
	return spec, nil
}

// decodeVisibleString validates VisibleString contents: the graphic
// characters 0x20..0x7e (X.680 clause 41, ISO 646 IRV).
func decodeVisibleString(b []byte) (string, error) {
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return "", fmt.Errorf("character 0x%02x is not in VisibleString", c)
		}
	}
	return string(b), nil
}

// DecodeConcludeError parses the contents of a Conclude-ErrorPDU:
// ISO 9506-2 clause 8.4.3 defines Conclude-ErrorPDU ::= ServiceError,
// carried as MMSpdu conclude-ErrorPDU [13] IMPLICIT (0xad).
func DecodeConcludeError(content []byte) (ServiceErrorWire, error) {
	se, err := DecodeServiceError(content)
	if err != nil {
		return ServiceErrorWire{}, fmt.Errorf("pdu: conclude error: %w", err)
	}
	return se, nil
}

// RejectPDU holds the parsed fields of a RejectPDU.
type RejectPDU struct {
	InvokeID     codec.InvokeID
	HasInvokeID  bool
	RejectType   int // 1=confirmedRequest, 2=confirmedResponse, 5=pduError, etc.
	RejectReason int
}

// DecodeRejectPDU parses the inner content of a RejectPDU
// (after the 0xa4 outer tag is stripped).
//
// Wire format:
//
//	RejectPDU ::= SEQUENCE {
//	    originalInvokeID [0] IMPLICIT Unsigned32 OPTIONAL,
//	    rejectReason CHOICE {
//	        confirmedRequestPDU  [1] IMPLICIT INTEGER,
//	        confirmedResponsePDU [2] IMPLICIT INTEGER,
//	        pduError             [5] IMPLICIT INTEGER,
//	        ...
//	    }
//	}
func DecodeRejectPDU(content []byte) (*RejectPDU, error) {
	if len(content) == 0 {
		return nil, fmt.Errorf("pdu: reject: empty content")
	}

	result := &RejectPDU{}
	offset := 0
	hasReason := false

	// ISO 9506-2 clause 8.6: originalInvokeID [0] OPTIONAL comes first,
	// then exactly one rejectReason alternative; nothing follows it.
	for offset < len(content) {
		tag, inner, n, err := berutil.DecodeTLVAt(content, offset)
		if err != nil {
			return nil, fmt.Errorf("pdu: reject field: %w", err)
		}
		offset += n

		switch {
		case hasReason:
			return nil, fmt.Errorf("pdu: reject: tag 0x%02x after rejectReason", tag)
		case tag == 0x80 && !result.HasInvokeID: // originalInvokeID
			id, err := berutil.DecodeUnsigned(inner)
			if err != nil {
				return nil, fmt.Errorf("pdu: reject invokeID: %w", err)
			}
			result.InvokeID = id
			result.HasInvokeID = true
		case tag >= 0x81 && tag <= 0x8b: // rejectReason CHOICE
			result.RejectType = int(tag) - 0x80
			switch {
			case len(inner) > 0:
				val, err := berutil.DecodeInteger(inner)
				if err != nil {
					return nil, fmt.Errorf("pdu: reject reason: %w", err)
				}
				result.RejectReason = val
			case result.RejectType >= 6 && result.RejectType <= 8:
				// cancel-*PDU [6]..[8] are IMPLICIT NULL when the cancel
				// CBB is not supported (ISO 9506-2 clause 8.6).
			default:
				return nil, fmt.Errorf("pdu: reject reason: empty INTEGER")
			}
			hasReason = true
		default:
			return nil, fmt.Errorf("pdu: reject: unexpected tag 0x%02x", tag)
		}
	}

	if !hasReason {
		return nil, fmt.Errorf("pdu: reject: missing rejectReason")
	}

	return result, nil
}
