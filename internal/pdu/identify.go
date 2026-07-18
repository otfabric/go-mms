// SPDX-License-Identifier: MIT

package pdu

import (
	"encoding/asn1"
	"fmt"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
)

// IdentifyResponse holds the parsed fields of an MMS Identify response.
type IdentifyResponse struct {
	VendorName string
	ModelName  string
	Revision   string
}

// identifyResponseASN1 is the internal struct matching the wire format
// of the Identify response body (inside the context 2 constructed tag).
type identifyResponseASN1 struct {
	VendorName string `asn1:"tag:0,implicit,ia5"`
	ModelName  string `asn1:"tag:1,implicit,ia5"`
	Revision   string `asn1:"tag:2,implicit,ia5"`
}

// MarshalIdentifyRequest builds a complete ConfirmedRequestPdu for the
// MMS Identify service. The Identify request has an empty body
// (context 2, primitive, length 0).
func MarshalIdentifyRequest(invokeID codec.InvokeID) ([]byte, error) {
	return marshalConfirmedLegacy(invokeID, asn1util.TagServiceIdentify, nil)
}

// MarshalIdentifyResponse encodes an Identify response payload as bare SEQUENCE
// fields. The surrounding context-specific tag is IMPLICIT (it replaces the
// universal SEQUENCE tag), so no 0x30 wrapper is emitted.
func MarshalIdentifyResponse(vendor, model, revision string) ([]byte, error) {
	resp := identifyResponseASN1{
		VendorName: vendor,
		ModelName:  model,
		Revision:   revision,
	}
	return marshalBareSequence(resp)
}

// UnmarshalIdentifyResponse parses an Identify response from the service
// RawValue inside a ConfirmedResponsePDU. The context-specific tag is IMPLICIT,
// so raw.Bytes contains the SEQUENCE fields directly without a 0x30 header.
func UnmarshalIdentifyResponse(serviceData asn1.RawValue) (*IdentifyResponse, error) {
	var resp identifyResponseASN1
	if err := codec.UnmarshalImplicitSequence(serviceData, &resp); err != nil {
		return nil, fmt.Errorf("pdu: unmarshal identify response: %w", err)
	}
	return &IdentifyResponse{
		VendorName: resp.VendorName,
		ModelName:  resp.ModelName,
		Revision:   resp.Revision,
	}, nil
}

// marshalBareSequence marshals a struct into bare SEQUENCE fields by stripping
// the 0x30 wrapper that encoding/asn1 adds. This produces the content suitable
// for IMPLICIT sequence tags in MMS PDUs.
func marshalBareSequence(v any) ([]byte, error) {
	content, err := asn1.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(content) < 2 || content[0] != 0x30 {
		return nil, fmt.Errorf("pdu: expected SEQUENCE from asn1.Marshal, got 0x%02x", content[0])
	}
	_, inner, _, err := berutil.DecodeTLVAt(content, 0)
	return inner, err
}
