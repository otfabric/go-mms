// SPDX-License-Identifier: MIT

package codec

import (
	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
)

// MarshalConfirmedResponse builds a ConfirmedResponsePdu wrapping an
// invoke ID and a pre-encoded service response. The tagNum/constructed
// pair describes the service CHOICE tag; extended tags (>30) are
// encoded automatically using multi-byte BER tag form.
func MarshalConfirmedResponse(invokeID InvokeID, tagNum int, constructed bool, servicePayload []byte) ([]byte, error) {
	invokeIDBytes := berutil.EncodeTLV(0x02, berutil.EncodeInt(int(invokeID)))

	serviceBytes := asn1util.WrapContextTag(tagNum, constructed, servicePayload)

	content := make([]byte, 0, len(invokeIDBytes)+len(serviceBytes))
	content = append(content, invokeIDBytes...)
	content = append(content, serviceBytes...)

	return asn1util.WrapConstructed(1, content), nil // context 1 = ConfirmedResponse
}

// MarshalConfirmedError builds a ConfirmedErrorPdu.
//
//	ConfirmedErrorPDU ::= SEQUENCE {
//	    invokeID       [0] IMPLICIT Unsigned32,
//	    serviceError   [2] IMPLICIT ServiceError
//	}
//
//	ServiceError ::= SEQUENCE {
//	    errorClass     [0] IMPLICIT CHOICE { ... }
//	}
func MarshalConfirmedError(invokeID InvokeID, errorClass, errorCode int) []byte {
	invokeIDTLV := berutil.EncodeTLV(0x80, berutil.EncodeUint32(uint32(invokeID)))

	errorCodeTLV := asn1util.WrapPrimitive(errorClass, berutil.EncodeInt(errorCode))
	errorClassTLV := berutil.EncodeTLV(0xa0, errorCodeTLV)
	serviceErrorTLV := berutil.EncodeTLV(0xa2, errorClassTLV)

	content := make([]byte, 0, len(invokeIDTLV)+len(serviceErrorTLV))
	content = append(content, invokeIDTLV...)
	content = append(content, serviceErrorTLV...)

	return asn1util.WrapConstructed(2, content) // context 2 = ConfirmedError
}

// MarshalRejectPDU builds a RejectPDU.
//
//	RejectPDU ::= SEQUENCE {
//	    originalInvokeID  [0] IMPLICIT Unsigned32 OPTIONAL,
//	    rejectReason      CHOICE { ... }
//	}
func MarshalRejectPDU(invokeID InvokeID, rejectType, rejectReason int) []byte {
	invokeIDTLV := berutil.EncodeTLV(0x80, berutil.EncodeUint32(uint32(invokeID)))
	reasonTLV := asn1util.WrapPrimitive(rejectType, berutil.EncodeInt(rejectReason))

	content := make([]byte, 0, len(invokeIDTLV)+len(reasonTLV))
	content = append(content, invokeIDTLV...)
	content = append(content, reasonTLV...)

	return asn1util.WrapConstructed(4, content) // context 4 = Reject
}

// MarshalConcludeResponse produces a ConcludeResponsePDU (context 12, NULL).
func MarshalConcludeResponse() []byte {
	return asn1util.WrapPrimitive(12, nil)
}

// MarshalCancelError builds a CancelErrorPDU (MMSpdu [7], constructed).
//
//	Cancel-ErrorPDU ::= SEQUENCE {
//	    originalInvokeID   [0] IMPLICIT Unsigned32,
//	    serviceError       [1] IMPLICIT ServiceError
//	}
func MarshalCancelError(originalInvokeID InvokeID, errorClass, errorCode int) []byte {
	invokeIDTLV := berutil.EncodeTLV(0x80, berutil.EncodeUint32(uint32(originalInvokeID)))

	errorCodeTLV := asn1util.WrapPrimitive(errorClass, berutil.EncodeInt(errorCode))
	errorClassTLV := berutil.EncodeTLV(0xa0, errorCodeTLV)
	serviceErrorTLV := berutil.EncodeTLV(0xa1, errorClassTLV)

	content := make([]byte, 0, len(invokeIDTLV)+len(serviceErrorTLV))
	content = append(content, invokeIDTLV...)
	content = append(content, serviceErrorTLV...)

	return asn1util.WrapConstructed(7, content)
}

// MarshalCancelResponse builds a CancelResponsePDU (MMSpdu [6], primitive).
// The content is the original invoke ID being cancelled.
func MarshalCancelResponse(originalInvokeID InvokeID) []byte {
	return asn1util.WrapPrimitive(6, berutil.EncodeUint32(uint32(originalInvokeID)))
}

// ServiceErrorFields are the optional components of an ISO 9506-2
// clause 7.4.1 ServiceError beyond errorClass.
type ServiceErrorFields struct {
	AdditionalCode        *int
	AdditionalDescription string
	// HasSpecific selects serviceSpecificInfo [3]; SpecificKind is the
	// CHOICE alternative's tag number and SpecificContent its contents
	// (encoded primitive).
	HasSpecific     bool
	SpecificKind    int
	SpecificContent []byte
}

// MarshalServiceError encodes the contents of a ServiceError SEQUENCE:
// errorClass [0] { [class] IMPLICIT INTEGER code }, then the optional
// additionalCode [1], additionalDescription [2] and serviceSpecificInfo
// [3] in that order.
func MarshalServiceError(errorClass, errorCode int, f ServiceErrorFields) []byte {
	out := berutil.EncodeTLV(0xa0, asn1util.WrapPrimitive(errorClass, berutil.EncodeInt(errorCode)))
	if f.AdditionalCode != nil {
		out = append(out, berutil.EncodeTLV(0x81, berutil.EncodeInt(*f.AdditionalCode))...)
	}
	if f.AdditionalDescription != "" {
		out = append(out, berutil.EncodeTLV(0x82, []byte(f.AdditionalDescription))...)
	}
	if f.HasSpecific {
		out = append(out, berutil.EncodeTLV(0xa3, asn1util.WrapPrimitive(f.SpecificKind, f.SpecificContent))...)
	}
	return out
}

// MarshalConfirmedErrorFields builds a Confirmed-ErrorPDU whose
// ServiceError carries the optional components in f.
func MarshalConfirmedErrorFields(invokeID InvokeID, errorClass, errorCode int, f ServiceErrorFields) []byte {
	content := berutil.EncodeTLV(0x80, berutil.EncodeUint32(uint32(invokeID)))
	content = append(content, berutil.EncodeTLV(0xa2, MarshalServiceError(errorClass, errorCode, f))...)
	return asn1util.WrapConstructed(2, content)
}

// MarshalRejectPDUNoInvokeID builds a RejectPDU without the OPTIONAL
// originalInvokeID, for a PDU whose invokeID could not be decoded
// (ISO 9506-2 clause 8.6).
func MarshalRejectPDUNoInvokeID(rejectType, rejectReason int) []byte {
	return asn1util.WrapConstructed(4, asn1util.WrapPrimitive(rejectType, berutil.EncodeInt(rejectReason)))
}

// MarshalConcludeError builds a Conclude-ErrorPDU: MMSpdu
// conclude-ErrorPDU [13] IMPLICIT ServiceError (ISO 9506-2 clause 8.4.3).
func MarshalConcludeError(errorClass, errorCode int) []byte {
	return asn1util.WrapConstructed(13, MarshalServiceError(errorClass, errorCode, ServiceErrorFields{}))
}
