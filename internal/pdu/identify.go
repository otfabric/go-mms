// SPDX-License-Identifier: MIT

package pdu

import (
	"encoding/asn1"
	"fmt"

	"github.com/otfabric/go-mms/internal/asn1util"
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

// UnmarshalIdentifyResponse parses an Identify response from the
// service RawValue inside a ConfirmedResponsePdu.
func UnmarshalIdentifyResponse(serviceData asn1.RawValue) (*IdentifyResponse, error) {
	var resp identifyResponseASN1
	err := codec.UnmarshalInner(serviceData, &resp)
	if err != nil {
		return nil, fmt.Errorf("pdu: unmarshal identify response: %w", err)
	}
	return &IdentifyResponse{
		VendorName: resp.VendorName,
		ModelName:  resp.ModelName,
		Revision:   resp.Revision,
	}, nil
}
