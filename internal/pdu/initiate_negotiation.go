// SPDX-License-Identifier: MIT

package pdu

import (

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
)

// ServiceError errorClass CHOICE alternatives used by the association
// services (ISO 9506-2 ServiceError).
const (
	ErrorClassAccess   = 7
	ErrorClassInitiate = 8
	ErrorClassConclude = 9
)

// ServiceError initiate-class codes (ISO 9506-2 ServiceError
// errorClass initiate; IEC 61850-8-1 Table 13).
const (
	InitiateErrOther                        = 0
	InitiateErrVersionIncompatible          = 1
	InitiateErrMaxSegmentInsufficient       = 2
	InitiateErrMaxOutstandingCallingInsuff  = 3
	InitiateErrMaxOutstandingCalledInsuff   = 4
	InitiateErrServiceCBBInsufficient       = 5
	InitiateErrParameterCBBInsufficient     = 6
	InitiateErrNestingLevelInsufficient     = 7
	AccessErrObjectAccessDenied             = 3
	ConcludeErrFurtherCommunicationRequired = 1
)

// MarshalInitiateError encodes an Initiate-ErrorPDU (MMSpdu [10]
// IMPLICIT ServiceError, ISO 9506-2 8.2): ServiceError ::= SEQUENCE {
// errorClass [0] CHOICE { ... [class] IMPLICIT INTEGER }, ... }.
func MarshalInitiateError(errorClass, errorCode int) []byte {
	code := asn1util.WrapPrimitive(errorClass, berutil.EncodeInt(errorCode))
	return berutil.EncodeTLV(asn1util.TagInitiateError, berutil.EncodeTLV(0xa0, code))
}

// InitiatePresence reports which OPTIONAL fields of an Initiate-Request
// or Initiate-Response content are present: localDetail [0] and
// dataStructureNestingLevel [3] (ISO 9506-2 8.2). encoding/asn1 decodes
// an absent OPTIONAL integer as 0, which is not the same as a proposal
// of 0. The content may carry the explicit inner SEQUENCE some peers
// send.
func InitiatePresence(content []byte) (localDetail, nesting bool) {
	if len(content) > 0 && content[0] == 0x30 {
		if _, inner, err := berutil.DecodeTLV(content); err == nil {
			content = inner
		}
	}
	offset := 0
	for offset < len(content) {
		tag, _, n, err := berutil.DecodeTLVAt(content, offset)
		if err != nil {
			return localDetail, nesting
		}
		offset += n
		switch tag {
		case 0x80:
			localDetail = true
		case 0x83:
			nesting = true
		}
	}
	return localDetail, nesting
}
