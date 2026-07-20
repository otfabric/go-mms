// SPDX-License-Identifier: MIT

package pdu

import (
	"encoding/asn1"
	"fmt"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
)

// StatusResponse holds the parsed fields of an MMS Status response.
type StatusResponse struct {
	VMDLogicalStatus  int
	VMDPhysicalStatus int
}

type statusResponseASN1 struct {
	VMDLogicalStatus  int `asn1:"tag:0,implicit"`
	VMDPhysicalStatus int `asn1:"tag:1,implicit"`
}

// MarshalStatusRequest builds a complete ConfirmedRequestPdu for the
// MMS Status service. The request body is a BOOLEAN indicating whether
// extended derivation is requested.
func MarshalStatusRequest(invokeID codec.InvokeID, extendedDerivation bool) ([]byte, error) {
	val := byte(0x00)
	if extendedDerivation {
		val = 0xff
	}
	return marshalConfirmedLegacy(invokeID, asn1util.TagServiceStatus, []byte{val})
}

// MarshalStatusResponse encodes a Status response payload as bare SEQUENCE
// fields. The surrounding context-specific tag is IMPLICIT (it replaces the
// universal SEQUENCE tag), so no 0x30 wrapper is emitted.
func MarshalStatusResponse(logical, physical int) ([]byte, error) {
	return marshalBareSequence(statusResponseASN1{
		VMDLogicalStatus:  logical,
		VMDPhysicalStatus: physical,
	})
}

// UnmarshalStatusResponse parses a Status response from the service RawValue
// inside a ConfirmedResponsePDU. The context-specific tag is IMPLICIT, so
// raw.Bytes contains the SEQUENCE fields directly without a 0x30 header.
func UnmarshalStatusResponse(serviceData asn1.RawValue) (*StatusResponse, error) {
	var resp statusResponseASN1
	if err := codec.UnmarshalImplicitSequence(serviceData, &resp); err != nil {
		return nil, fmt.Errorf("pdu: unmarshal status response: %w", err)
	}
	return &StatusResponse{
		VMDLogicalStatus:  resp.VMDLogicalStatus,
		VMDPhysicalStatus: resp.VMDPhysicalStatus,
	}, nil
}

// MarshalUnsolicitedStatus builds a complete UnconfirmedPDU (0xa3)
// containing an UnsolicitedStatus service ([1] in UnconfirmedService
// CHOICE).
//
// BER layout:
//
//	0xa3 (UnconfirmedPDU) {
//	  0xa1 (UnsolicitedStatus [1] in UnconfirmedService CHOICE) {
//	    [0] IMPLICIT INTEGER — vmdLogicalStatus
//	    [1] IMPLICIT INTEGER — vmdPhysicalStatus
//	  }
//	}
func MarshalUnsolicitedStatus(logical, physical int) ([]byte, error) {
	content := berutil.EncodeTLV(0x80, berutil.EncodeInt(logical))
	content = append(content, berutil.EncodeTLV(0x81, berutil.EncodeInt(physical))...)

	unsolicited := berutil.EncodeTLV(0xa1, content) // [1] UnsolicitedStatus
	return berutil.EncodeTLV(asn1util.TagUnconfirmed, unsolicited), nil
}
