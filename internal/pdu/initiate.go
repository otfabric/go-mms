// SPDX-License-Identifier: MIT

package pdu

import (
	"encoding/asn1"
	"fmt"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
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

// MarshalInitiateRequest encodes an InitiateRequest into a complete MMS PDU
// (outer tag 0xa8). InitiateRequestPDU uses an IMPLICIT context tag, so the
// universal SEQUENCE (0x30) added by encoding/asn1 is stripped and the fields
// are placed directly inside the 0xa8 wrapper.
func MarshalInitiateRequest(req InitiateRequest) ([]byte, error) {
	return codec.MarshalMmsPduBareSequence(asn1util.TagInitiateRequest, req)
}

// UnmarshalInitiateRequest decodes the inner content of an
// InitiateRequestPDU (after the 0xa8 outer tag has been stripped).
//
// ISO 9506 defines InitiateRequestPDU as [8] IMPLICIT SEQUENCE, so the outer
// 0xa8 tag replaces the SEQUENCE tag and the content bytes are the SEQUENCE
// fields directly (no 0x30 header). A 0x30 wrapper is reconstructed so that
// Go's encoding/asn1 can parse the struct.
//
// Some implementations (e.g. libiec61850's IedServer stack) encode an explicit
// inner SEQUENCE (0x30) wrapper despite the IMPLICIT tag. Both forms are
// accepted: when the content already begins with 0x30 it is used as-is.
func UnmarshalInitiateRequest(content []byte) (*InitiateRequest, error) {
	var req InitiateRequest
	seq := wrapSequenceIfNeeded(content)
	rest, err := asn1.Unmarshal(seq, &req)
	if err != nil {
		return nil, fmt.Errorf("pdu: unmarshal initiate request: %w", err)
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("pdu: unmarshal initiate request: %d trailing bytes", len(rest))
	}
	return &req, nil
}

// UnmarshalInitiateResponse decodes the inner content of an
// InitiateResponsePDU (after the 0xa9 outer tag has been stripped).
//
// ISO 9506 defines InitiateResponsePDU as [9] IMPLICIT SEQUENCE, so the outer
// 0xa9 tag replaces the SEQUENCE tag and the content bytes are the SEQUENCE
// fields directly (no 0x30 header). A 0x30 wrapper is reconstructed so that
// Go's encoding/asn1 can parse the struct.
//
// Some implementations (e.g. libiec61850's IedServer stack) encode an explicit
// inner SEQUENCE (0x30) wrapper despite the IMPLICIT tag. Both forms are
// accepted: when the content already begins with 0x30 it is used as-is.
func UnmarshalInitiateResponse(content []byte) (*InitiateResponse, error) {
	var resp InitiateResponse
	seq := wrapSequenceIfNeeded(content)
	rest, err := asn1.Unmarshal(seq, &resp)
	if err != nil {
		return nil, fmt.Errorf("pdu: unmarshal initiate response: %w", err)
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("pdu: unmarshal initiate response: %d trailing bytes", len(rest))
	}
	return &resp, nil
}

// wrapSequenceIfNeeded reconstructs the 0x30 SEQUENCE TLV wrapper that Go's
// encoding/asn1 requires when unmarshalling a struct. If the content already
// begins with a SEQUENCE tag (0x30) — as emitted by some implementations that
// include an explicit inner SEQUENCE despite an IMPLICIT outer tag — the bytes
// are returned as-is to avoid double-wrapping.
func wrapSequenceIfNeeded(content []byte) []byte {
	if len(content) > 0 && content[0] == 0x30 {
		return content
	}
	return berutil.EncodeTLV(0x30, content)
}
