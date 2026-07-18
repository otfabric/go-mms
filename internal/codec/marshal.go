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

// MarshalMmsPduBareSequence marshals a struct into an MMS PDU, stripping the
// 0x30 SEQUENCE header that encoding/asn1 adds, so the outer context-specific
// tag is IMPLICIT (it replaces the universal SEQUENCE tag). Use this for all
// MMS PDU types whose ASN.1 definition uses an IMPLICIT context tag, such as
// InitiateRequestPDU [8] and InitiateResponsePDU [9].
func MarshalMmsPduBareSequence(pduTag byte, payload any) ([]byte, error) {
	content, err := asn1.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("codec: marshal PDU content: %w", err)
	}
	if len(content) < 2 || content[0] != 0x30 {
		return nil, fmt.Errorf("codec: expected SEQUENCE (0x30) from asn1.Marshal, got 0x%02x", content[0])
	}
	_, inner, _, err := berutil.DecodeTLVAt(content, 0)
	if err != nil {
		return nil, fmt.Errorf("codec: strip SEQUENCE header: %w", err)
	}
	tagNum := asn1util.TagNumber(pduTag)
	return asn1util.WrapConstructed(tagNum, inner), nil
}

// MarshalConcludeRequest produces a ConcludeRequestPDU (context 11, NULL).
func MarshalConcludeRequest() []byte {
	return asn1util.WrapPrimitive(11, nil)
}
