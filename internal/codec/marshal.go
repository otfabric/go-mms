// SPDX-License-Identifier: MIT

package codec

import (
	"encoding/asn1"
	"fmt"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
)

// MarshalMmsPdu marshals a PDU payload and wraps it in the appropriate
// top-level MMS PDU tag. The pduTag must be one of the asn1util.Tag*
// constants (e.g., TagInitiateRequest, TagConfirmedRequest).
func MarshalMmsPdu(pduTag byte, payload any) ([]byte, error) {
	content, err := asn1.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("codec: marshal PDU content: %w", err)
	}

	tagNum := asn1util.TagNumber(pduTag)
	if asn1util.IsConstructed(pduTag) {
		return asn1util.WrapConstructed(tagNum, content), nil
	}
	return asn1util.WrapPrimitive(tagNum, content), nil
}

// MarshalConfirmedRequest builds a ConfirmedRequestPdu wrapping an
// invoke ID and a pre-encoded service request. The tagNum/constructed
// pair describes the service CHOICE tag; extended tags (>30) are
// encoded automatically using multi-byte BER tag form.
func MarshalConfirmedRequest(invokeID InvokeID, tagNum int, constructed bool, servicePayload []byte) ([]byte, error) {
	invokeIDBytes, err := asn1.Marshal(int(invokeID))
	if err != nil {
		return nil, fmt.Errorf("codec: marshal invoke ID: %w", err)
	}

	serviceBytes := asn1util.WrapContextTag(tagNum, constructed, servicePayload)

	content := make([]byte, 0, len(invokeIDBytes)+len(serviceBytes))
	content = append(content, invokeIDBytes...)
	content = append(content, serviceBytes...)

	return asn1util.WrapConstructed(0, content), nil
}

// tagUniversalSequence is the BER tag for a universal SEQUENCE (0x30).
const tagUniversalSequence byte = 0x30

// MarshalSequenceContent marshals a struct using encoding/asn1 and returns
// the SEQUENCE fields without the 0x30 wrapper. This produces the bare
// content suitable for IMPLICIT sequence tags, where the context-specific
// tag replaces the universal SEQUENCE tag.
//
// It validates that asn1.Marshal produced exactly one complete SEQUENCE TLV.
func MarshalSequenceContent(value any) ([]byte, error) {
	encoded, err := asn1.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("codec: marshal sequence: %w", err)
	}
	tag, inner, consumed, err := berutil.DecodeTLVAt(encoded, 0)
	if err != nil {
		return nil, fmt.Errorf("codec: decode SEQUENCE header: %w", err)
	}
	if tag != tagUniversalSequence {
		return nil, fmt.Errorf("codec: expected SEQUENCE (0x30) from asn1.Marshal, got 0x%02x", tag)
	}
	if consumed != len(encoded) {
		return nil, fmt.Errorf("codec: %d trailing bytes after SEQUENCE", len(encoded)-consumed)
	}
	return inner, nil
}

// MarshalMmsPduBareSequence marshals a struct into an MMS PDU using
// MarshalSequenceContent, so the outer context-specific tag is IMPLICIT
// (it replaces the universal SEQUENCE tag). Use this for all MMS PDU types
// whose ASN.1 definition uses an IMPLICIT context tag, such as
// InitiateRequestPDU [8] and InitiateResponsePDU [9].
func MarshalMmsPduBareSequence(pduTag byte, payload any) ([]byte, error) {
	inner, err := MarshalSequenceContent(payload)
	if err != nil {
		return nil, err
	}
	return asn1util.WrapConstructed(asn1util.TagNumber(pduTag), inner), nil
}

// MarshalConcludeRequest produces a ConcludeRequestPDU (context 11, NULL).
func MarshalConcludeRequest() []byte {
	return asn1util.WrapPrimitive(11, nil)
}
