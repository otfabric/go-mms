// SPDX-License-Identifier: MIT

package pdu

import (
	"encoding/asn1"
	"fmt"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/codec"
)

// InitRequestDetail carries the MMS version and service support bitmasks
// proposed by the client during association.
type InitRequestDetail struct {
	ProposedVersion          int            `asn1:"tag:0,implicit"`
	ProposedParamCBB         asn1.BitString `asn1:"tag:1,implicit"`
	ServicesSupportedCalling asn1.BitString `asn1:"tag:2,implicit"`
}

// InitiateRequest represents the MMS Initiate-RequestPDU.
// All fields use context-specific implicit tags matching ISO 9506.
type InitiateRequest struct {
	LocalDetailCalling               int               `asn1:"tag:0,implicit,optional"`
	ProposedMaxServOutstandingCall   int               `asn1:"tag:1,implicit"`
	ProposedMaxServOutstandingCalled int               `asn1:"tag:2,implicit"`
	ProposedDataStructureNesting     int               `asn1:"tag:3,implicit,optional"`
	InitRequestDetail                InitRequestDetail `asn1:"tag:4,implicit"`
}

// InitResponseDetail carries the negotiated MMS version and service
// support bitmasks from the server.
type InitResponseDetail struct {
	NegotiatedVersion       int            `asn1:"tag:0,implicit"`
	NegotiatedParamCBB      asn1.BitString `asn1:"tag:1,implicit"`
	ServicesSupportedCalled asn1.BitString `asn1:"tag:2,implicit"`
}

// InitiateResponse represents the MMS Initiate-ResponsePDU.
type InitiateResponse struct {
	LocalDetailCalled                  int                `asn1:"tag:0,implicit,optional"`
	NegotiatedMaxServOutstandingCall   int                `asn1:"tag:1,implicit"`
	NegotiatedMaxServOutstandingCalled int                `asn1:"tag:2,implicit"`
	NegotiatedDataStructureNesting     int                `asn1:"tag:3,implicit,optional"`
	InitResponseDetail                 InitResponseDetail `asn1:"tag:4,implicit"`
}

// DefaultInitiateRequest returns a standard InitiateRequest with common defaults.
func DefaultInitiateRequest(maxPDU, maxOutCalling, maxOutCalled, nestingLevel int) InitiateRequest {
	if maxPDU <= 0 {
		maxPDU = 65000
	}
	if maxOutCalling <= 0 {
		maxOutCalling = 5
	}
	if maxOutCalled <= 0 {
		maxOutCalled = 5
	}
	if nestingLevel <= 0 {
		nestingLevel = 10
	}

	paramCBB := asn1.BitString{
		Bytes:     []byte{0xf1, 0x00},
		BitLength: 11,
	}

	servicesBits := []byte{0xee, 0x1c, 0x00, 0x00, 0x04, 0x08, 0x00, 0x00, 0x79, 0xef, 0x18}
	servicesCalling := asn1.BitString{
		Bytes:     servicesBits,
		BitLength: 85,
	}

	return InitiateRequest{
		LocalDetailCalling:               maxPDU,
		ProposedMaxServOutstandingCall:   maxOutCalling,
		ProposedMaxServOutstandingCalled: maxOutCalled,
		ProposedDataStructureNesting:     nestingLevel,
		InitRequestDetail: InitRequestDetail{
			ProposedVersion:          1,
			ProposedParamCBB:         paramCBB,
			ServicesSupportedCalling: servicesCalling,
		},
	}
}

// MarshalInitiateRequest encodes an InitiateRequest into a complete
// MMS PDU (with the 0xa8 outer tag).
func MarshalInitiateRequest(req InitiateRequest) ([]byte, error) {
	return codec.MarshalMmsPdu(asn1util.TagInitiateRequest, req)
}

// UnmarshalInitiateResponse decodes the inner content of an
// InitiateResponsePdu (after the 0xa9 outer tag has been stripped).
func UnmarshalInitiateResponse(content []byte) (*InitiateResponse, error) {
	var resp InitiateResponse
	rest, err := asn1.Unmarshal(content, &resp)
	if err != nil {
		return nil, fmt.Errorf("pdu: unmarshal initiate response: %w", err)
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("pdu: unmarshal initiate response: %d trailing bytes", len(rest))
	}
	return &resp, nil
}
