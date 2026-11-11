// SPDX-License-Identifier: MIT

package codec

import (
	"encoding/asn1"
	"fmt"
	"math"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
)

// PduType identifies the top-level MMS PDU type from raw bytes.
func PduType(data []byte) (byte, error) {
	tag, err := asn1util.PeekTag(data)
	if err != nil {
		return 0, fmt.Errorf("codec: %w", err)
	}
	return tag, nil
}

// UnwrapPdu strips the outer MMS PDU tag and returns the inner content
// bytes along with the tag byte.
//
// MMS PDUs are BER (ISO 9506-2 clause 1, X.690 §8), so the receive side
// accepts what BER allows and DER forbids: non-minimal long-form lengths
// (X.690 §8.1.3.5 NOTE 2) and the indefinite length form for constructed
// encodings (§8.1.3.6). The returned content is rewritten to minimal
// definite lengths (berutil.Canonicalize) when it used either, so the
// definite-only inner decoders see one form. Content that is not a
// well-formed element sequence is returned as received and the service
// decoder reports the fault.
func UnwrapPdu(data []byte) (tag byte, content []byte, err error) {
	el, n, err := berutil.DecodeElement(data, 0)
	if err != nil {
		return 0, nil, fmt.Errorf("codec: unwrap PDU: %w", err)
	}
	if n != len(data) {
		return 0, nil, fmt.Errorf("codec: unwrap PDU: %d trailing bytes", len(data)-n)
	}

	// Every MMS PDU tag fits the single-byte form; a long-form tag
	// number must not be truncated into one of them.
	if el.Tag > 30 {
		return 0, nil, fmt.Errorf("codec: unwrap PDU: tag number %d is not an MMS PDU", el.Tag)
	}

	tag = el.Class | byte(el.Tag)
	if el.Constructed {
		tag |= asn1util.ConstructedFlag
	}
	content = el.Content
	if el.Constructed {
		if canon, _, cerr := berutil.Canonicalize(content); cerr == nil {
			content = canon
		}
	}
	return tag, content, nil
}

// MaxInvokeID is the largest invokeID: Unsigned32 is INTEGER
// (0..2147483647) in ISO 9506-2 (Unsigned32 definition, clause 7.5).
const MaxInvokeID = math.MaxInt32

// EnvelopeError reports a Confirmed-Request/Response envelope that could
// not be decoded. HasInvokeID is set when the invokeID itself was read,
// so a Reject can carry it as originalInvokeID (ISO 9506-2 RejectPDU);
// InvalidInvokeID is set when the invokeID was present but outside
// Unsigned32 (reject reason invalid-invokeID).
type EnvelopeError struct {
	InvokeID        InvokeID
	HasInvokeID     bool
	InvalidInvokeID bool
	Err             error
}

func (e *EnvelopeError) Error() string { return e.Err.Error() }

func (e *EnvelopeError) Unwrap() error { return e.Err }

// InvokeID is the internal type for MMS invoke IDs. It mirrors the
// public mms.InvokeID type without creating an import cycle.
type InvokeID = uint32

// UnmarshalConfirmedResponse parses a ConfirmedResponsePdu and returns
// the invoke ID and the inner service response as a RawValue (for
// CHOICE dispatch by the caller).
func UnmarshalConfirmedResponse(content []byte) (invokeID InvokeID, serviceRaw asn1.RawValue, err error) {
	return unmarshalConfirmedEnvelope(content)
}

// UnmarshalConfirmedRequest parses a ConfirmedRequestPdu and returns
// the invoke ID and the inner service request as a RawValue.
func UnmarshalConfirmedRequest(content []byte) (invokeID InvokeID, serviceRaw asn1.RawValue, err error) {
	return unmarshalConfirmedEnvelope(content)
}

// unmarshalConfirmedEnvelope decodes
//
//	Confirmed-RequestPDU  ::= SEQUENCE { invokeID Unsigned32, ..., service ConfirmedServiceRequest }
//	Confirmed-ResponsePDU ::= SEQUENCE { invokeID Unsigned32, service ConfirmedServiceResponse }
//
// (ISO 9506-2 clause 7.1/7.2). invokeID is an untagged INTEGER (0x02);
// BER non-minimal INTEGER contents are accepted (X.690 §8.3.2 is a
// sender rule only in DER). Service tags above 30 use the multi-octet
// identifier form.
func unmarshalConfirmedEnvelope(content []byte) (InvokeID, asn1.RawValue, error) {
	idEl, n, err := berutil.DecodeElement(content, 0)
	if err != nil {
		return 0, asn1.RawValue{}, &EnvelopeError{Err: fmt.Errorf("codec: unmarshal invoke ID: %w", err)}
	}
	if idEl.Class != asn1util.ClassUniversal || idEl.Constructed || idEl.Tag != 2 {
		return 0, asn1.RawValue{}, &EnvelopeError{Err: fmt.Errorf("codec: unmarshal invoke ID: expected INTEGER (0x02), got 0x%02x", content[0])}
	}
	v := berutil.TrimInteger(idEl.Content)
	if len(v) == 0 {
		return 0, asn1.RawValue{}, &EnvelopeError{Err: fmt.Errorf("codec: unmarshal invoke ID: empty INTEGER")}
	}
	// invokeID is Unsigned32 = INTEGER (0..2147483647): a negative or
	// wider value must not wrap onto another outstanding request.
	if v[0]&0x80 != 0 || len(v) > 4 || (len(v) == 4 && v[0]&0x80 != 0) {
		return 0, asn1.RawValue{}, &EnvelopeError{InvalidInvokeID: true, Err: fmt.Errorf("codec: invoke ID %x out of Unsigned32 range", idEl.Content)}
	}
	var id uint32
	for _, b := range v {
		id = id<<8 | uint32(b)
	}
	invokeID := InvokeID(id)

	if n >= len(content) {
		return 0, asn1.RawValue{}, &EnvelopeError{InvokeID: invokeID, HasInvokeID: true, Err: fmt.Errorf("codec: unmarshal service: missing service")}
	}
	svc, m, err := berutil.DecodeElement(content, n)
	if err != nil {
		return 0, asn1.RawValue{}, &EnvelopeError{InvokeID: invokeID, HasInvokeID: true, Err: fmt.Errorf("codec: unmarshal service: %w", err)}
	}
	if n+m != len(content) {
		return 0, asn1.RawValue{}, &EnvelopeError{InvokeID: invokeID, HasInvokeID: true, Err: fmt.Errorf("codec: %d trailing bytes after confirmed envelope", len(content)-n-m)}
	}

	// ConfirmedServiceRequest / ConfirmedServiceResponse are CHOICEs of
	// context-specific alternatives only (ISO 9506-2 clauses 7.1, 7.2).
	if svc.Class != asn1util.ClassContext {
		return 0, asn1.RawValue{}, &EnvelopeError{InvokeID: invokeID, HasInvokeID: true, Err: fmt.Errorf("codec: service tag 0x%02x is not context-specific", svc.Full[0])}
	}
	serviceRaw := asn1.RawValue{
		Class:      int(svc.Class >> 6),
		Tag:        svc.Tag,
		IsCompound: svc.Constructed,
		Bytes:      svc.Content,
		FullBytes:  svc.Full,
	}
	return invokeID, serviceRaw, nil
}
