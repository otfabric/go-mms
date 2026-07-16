// SPDX-License-Identifier: MIT

package pdu

import (
	"fmt"

	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
)

// ConfirmedError holds the parsed fields of a ConfirmedErrorPDU.
type ConfirmedError struct {
	InvokeID   codec.InvokeID
	ErrorClass int
	ErrorCode  int
}

// DecodeConfirmedError parses the inner content of a ConfirmedErrorPDU
// (after the 0xa2 outer tag is stripped).
//
// Wire format:
//
//	ConfirmedErrorPDU ::= SEQUENCE {
//	    invokeID [0] IMPLICIT Unsigned32,
//	    serviceError [2] IMPLICIT ServiceError
//	}
//
//	ServiceError ::= SEQUENCE {
//	    errorClass [0] IMPLICIT CHOICE { ... }
//	}
func DecodeConfirmedError(content []byte) (*ConfirmedError, error) {
	if len(content) == 0 {
		return nil, fmt.Errorf("pdu: confirmed error: empty content")
	}

	result := &ConfirmedError{}
	offset := 0
	hasInvokeID := false

	for offset < len(content) {
		tag, inner, n, err := berutil.DecodeTLVAt(content, offset)
		if err != nil {
			return nil, fmt.Errorf("pdu: confirmed error field: %w", err)
		}
		offset += n

		switch tag {
		case 0x80: // invokeID
			id, err := berutil.DecodeInteger(inner)
			if err != nil {
				return nil, fmt.Errorf("pdu: confirmed error invokeID: %w", err)
			}
			result.InvokeID = codec.InvokeID(id)
			hasInvokeID = true
		case 0xa2: // serviceError
			if err := parseServiceError(inner, result); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("pdu: confirmed error: unexpected tag 0x%02x", tag)
		}
	}

	if !hasInvokeID {
		return nil, fmt.Errorf("pdu: confirmed error: missing invokeID")
	}

	return result, nil
}

func parseServiceError(data []byte, result *ConfirmedError) error {
	offset := 0
	for offset < len(data) {
		tag, inner, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return fmt.Errorf("pdu: service error field: %w", err)
		}
		offset += n

		if tag == 0xa0 { // errorClass
			return parseErrorClass(inner, result)
		}
	}
	return nil
}

// parseErrorClass parses the CHOICE inside errorClass [0].
// The tag identifies the error class (0x80=vmd-state, 0x81=app-ref, etc.)
// and the inner value is the error code INTEGER.
func parseErrorClass(data []byte, result *ConfirmedError) error {
	if len(data) == 0 {
		return nil
	}
	tag, inner, n, err := berutil.DecodeTLVAt(data, 0)
	if err != nil {
		return fmt.Errorf("pdu: error class: %w", err)
	}
	if n != len(data) {
		return fmt.Errorf("pdu: error class: %d trailing bytes", len(data)-n)
	}
	result.ErrorClass = int(tag) - 0x80
	if len(inner) > 0 {
		val, err := berutil.DecodeInteger(inner)
		if err != nil {
			return fmt.Errorf("pdu: error code: %w", err)
		}
		result.ErrorCode = val
	}
	return nil
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

	for offset < len(content) {
		tag, inner, n, err := berutil.DecodeTLVAt(content, offset)
		if err != nil {
			return nil, fmt.Errorf("pdu: reject field: %w", err)
		}
		offset += n

		switch {
		case tag == 0x80: // originalInvokeID
			id, err := berutil.DecodeInteger(inner)
			if err != nil {
				return nil, fmt.Errorf("pdu: reject invokeID: %w", err)
			}
			result.InvokeID = codec.InvokeID(id)
			result.HasInvokeID = true
		case tag >= 0x81 && tag <= 0x8b: // rejectReason CHOICE
			result.RejectType = int(tag) - 0x80
			if len(inner) > 0 {
				val, err := berutil.DecodeInteger(inner)
				if err != nil {
					return nil, fmt.Errorf("pdu: reject reason: %w", err)
				}
				result.RejectReason = val
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
