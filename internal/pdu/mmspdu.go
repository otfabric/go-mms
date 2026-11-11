// SPDX-License-Identifier: MIT

package pdu

import (
	"errors"
	"fmt"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/codec"
)

// PduKind identifies the top-level MMS PDU type.
type PduKind int

const (
	PduConfirmedRequest PduKind = iota
	PduConfirmedResponse
	PduConfirmedError
	PduUnconfirmed
	PduReject
	PduInitiateRequest
	PduInitiateResponse
	PduInitiateError
	PduConcludeRequest
	PduConcludeResponse
	PduConcludeError
	PduCancelRequest
	PduCancelResponse
	PduCancelError
)

var pduKindNames = [...]string{
	"ConfirmedRequest",
	"ConfirmedResponse",
	"ConfirmedError",
	"Unconfirmed",
	"Reject",
	"InitiateRequest",
	"InitiateResponse",
	"InitiateError",
	"ConcludeRequest",
	"ConcludeResponse",
	"ConcludeError",
	"CancelRequest",
	"CancelResponse",
	"CancelError",
}

func (k PduKind) String() string {
	if int(k) >= 0 && int(k) < len(pduKindNames) {
		return pduKindNames[k]
	}
	return fmt.Sprintf("PduKind(%d)", int(k))
}

// ErrUnknownPduType reports a top-level tag that is no MMSpdu CHOICE
// alternative (ISO 9506-2 clause 7, MMSpdu); a receiver answers it with
// Reject pdu-error unknown-pdu-type (clause 8.6).
var ErrUnknownPduType = errors.New("pdu: unknown MMS PDU type")

// ClassifyPdu identifies the MMS PDU type from raw BER-encoded data
// by inspecting the first tag byte.
func ClassifyPdu(data []byte) (PduKind, error) {
	tag, err := codec.PduType(data)
	if err != nil {
		return 0, err
	}
	return classifyTag(tag)
}

func classifyTag(tag byte) (PduKind, error) {
	switch tag {
	case asn1util.TagConfirmedRequest:
		return PduConfirmedRequest, nil
	case asn1util.TagConfirmedResponse:
		return PduConfirmedResponse, nil
	case asn1util.TagConfirmedError:
		return PduConfirmedError, nil
	case asn1util.TagUnconfirmed:
		return PduUnconfirmed, nil
	case asn1util.TagReject:
		return PduReject, nil
	case asn1util.TagInitiateRequest:
		return PduInitiateRequest, nil
	case asn1util.TagInitiateResponse:
		return PduInitiateResponse, nil
	case asn1util.TagInitiateError:
		return PduInitiateError, nil
	case asn1util.TagConcludeRequest:
		return PduConcludeRequest, nil
	case asn1util.TagConcludeResponse:
		return PduConcludeResponse, nil
	case asn1util.TagConcludeError:
		return PduConcludeError, nil
	case asn1util.TagCancelRequest:
		return PduCancelRequest, nil
	case asn1util.TagCancelResponse:
		return PduCancelResponse, nil
	case asn1util.TagCancelError:
		return PduCancelError, nil
	default:
		return 0, fmt.Errorf("%w: tag 0x%02x", ErrUnknownPduType, tag)
	}
}

// DecodePdu strips the outer MMS PDU tag and returns the kind and
// inner content bytes for further processing.
func DecodePdu(data []byte) (kind PduKind, content []byte, err error) {
	// An identifier that is no MMSpdu alternative is reported as such
	// even when the rest does not parse (Reject unknown-pdu-type).
	if len(data) > 0 {
		if _, cerr := classifyTag(data[0]); cerr != nil {
			return 0, nil, cerr
		}
	}
	tag, content, err := codec.UnwrapPdu(data)
	if err != nil {
		return 0, nil, err
	}
	kind, err = classifyTag(tag)
	if err != nil {
		return 0, nil, err
	}
	return kind, content, nil
}
