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

	errorCodeTLV := berutil.EncodeTLV(byte(0x80+errorClass), berutil.EncodeInt(errorCode))
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
	reasonTLV := berutil.EncodeTLV(byte(0x80+rejectType), berutil.EncodeInt(rejectReason))

	content := make([]byte, 0, len(invokeIDTLV)+len(reasonTLV))
	content = append(content, invokeIDTLV...)
	content = append(content, reasonTLV...)

	return asn1util.WrapConstructed(4, content) // context 4 = Reject
}

// MarshalConcludeResponse produces a ConcludeResponsePDU (context 12, NULL).
func MarshalConcludeResponse() []byte {
	return asn1util.WrapPrimitive(12, nil)
}

// MarshalCancelError builds a CancelErrorPDU (context 8, constructed).
//
//	CancelErrorPDU ::= SEQUENCE {
//	    originalInvokeID   [0] IMPLICIT Unsigned32,
//	    serviceError       [2] IMPLICIT ServiceError
//	}
func MarshalCancelError(originalInvokeID InvokeID, errorClass, errorCode int) []byte {
	invokeIDTLV := berutil.EncodeTLV(0x80, berutil.EncodeUint32(uint32(originalInvokeID)))

	errorCodeTLV := berutil.EncodeTLV(byte(0x80+errorClass), berutil.EncodeInt(errorCode))
	errorClassTLV := berutil.EncodeTLV(0xa0, errorCodeTLV)
	serviceErrorTLV := berutil.EncodeTLV(0xa2, errorClassTLV)

	content := make([]byte, 0, len(invokeIDTLV)+len(serviceErrorTLV))
	content = append(content, invokeIDTLV...)
	content = append(content, serviceErrorTLV...)

	return asn1util.WrapConstructed(8, content)
}

// MarshalCancelResponse builds a CancelResponsePDU (context 7, primitive).
// The content is the original invoke ID being cancelled.
func MarshalCancelResponse(originalInvokeID InvokeID) []byte {
	return asn1util.WrapPrimitive(7, berutil.EncodeUint32(uint32(originalInvokeID)))
}
