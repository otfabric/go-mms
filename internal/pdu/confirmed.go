// SPDX-License-Identifier: MIT

package pdu

import (
	"encoding/asn1"
	"fmt"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
)

// ServiceKind identifies which MMS confirmed service is in the PDU.
type ServiceKind int

const (
	ServiceStatus ServiceKind = iota
	ServiceGetNameList
	ServiceIdentify
	ServiceRead
	ServiceWrite
	ServiceGetVariableAccessAttrs
	ServiceDefineNamedVariableList
	ServiceGetNamedVariableListAttrs
	ServiceDeleteNamedVariableList
	ServiceFileOpen
	ServiceFileRead
	ServiceFileClose
	ServiceFileRename
	ServiceFileDelete
	ServiceFileDirectory
	ServiceObtainFile
	ServiceReadJournal
	ServiceUnknown
)

var serviceKindNames = [...]string{
	"Status",
	"GetNameList",
	"Identify",
	"Read",
	"Write",
	"GetVariableAccessAttributes",
	"DefineNamedVariableList",
	"GetNamedVariableListAttributes",
	"DeleteNamedVariableList",
	"FileOpen",
	"FileRead",
	"FileClose",
	"FileRename",
	"FileDelete",
	"FileDirectory",
	"ObtainFile",
	"ReadJournal",
	"Unknown",
}

func (s ServiceKind) String() string {
	if int(s) >= 0 && int(s) < len(serviceKindNames) {
		return serviceKindNames[s]
	}
	return fmt.Sprintf("ServiceKind(%d)", int(s))
}

// ConfirmedResponse holds the parsed envelope of a ConfirmedResponsePdu.
type ConfirmedResponse struct {
	InvokeID    codec.InvokeID
	ServiceKind ServiceKind
	ServiceData asn1.RawValue
}

// ExtractInvokeID reads the invoke ID from the first TLV in a
// ConfirmedResponse (tag 0x02), ConfirmedError (tag 0x80), or Reject
// (tag 0x80) PDU content without fully parsing the rest. Used by the
// reader loop for dispatch.
func ExtractInvokeID(content []byte) (codec.InvokeID, error) {
	if len(content) == 0 {
		return 0, fmt.Errorf("pdu: extract invoke ID: empty content")
	}
	tag, inner, _, err := berutil.DecodeTLVAt(content, 0)
	if err != nil {
		return 0, fmt.Errorf("pdu: extract invoke ID: %w", err)
	}
	switch tag {
	case 0x02, 0x80:
		// invokeID is Unsigned32 (ISO 9506-2): IDs >= 2^31 carry a
		// 0x00 pad, negative encodings are rejected instead of wrapped.
		id, err := berutil.DecodeUnsigned(inner)
		if err != nil {
			return 0, fmt.Errorf("pdu: extract invoke ID value: %w", err)
		}
		return id, nil
	default:
		return 0, fmt.Errorf("pdu: extract invoke ID: unexpected tag 0x%02x", tag)
	}
}

// ExtractInvokeIDFor reads the invoke ID with the tag the PDU kind
// defines (ISO 9506-2 clause 7): the untagged INTEGER (0x02) of a
// Confirmed-ResponsePDU, the [0] IMPLICIT Unsigned32 (0x80) of a
// Confirmed-ErrorPDU or RejectPDU. A value above 2^31-1 is outside
// Unsigned32 and rejected.
func ExtractInvokeIDFor(kind PduKind, content []byte) (codec.InvokeID, error) {
	want := byte(0x80)
	if kind == PduConfirmedResponse || kind == PduConfirmedRequest {
		want = 0x02
	}
	if len(content) == 0 || content[0] != want {
		return 0, fmt.Errorf("pdu: extract invoke ID: %s does not start with tag 0x%02x", kind, want)
	}
	id, err := ExtractInvokeID(content)
	if err != nil {
		return 0, err
	}
	if id > codec.MaxInvokeID {
		return 0, fmt.Errorf("pdu: extract invoke ID: %d outside Unsigned32", id)
	}
	return id, nil
}

// RecoverInvokeID reads the invoke ID of a Confirmed-Response,
// Confirmed-Error or Reject PDU whose outer element did not decode (for
// example a wrong outer length), so the request it answers can be failed
// and the PDU rejected instead of ending the association. It only reads
// the first inner element.
func RecoverInvokeID(mmsPDU []byte) (PduKind, codec.InvokeID, bool) {
	if len(mmsPDU) < 2 {
		return 0, 0, false
	}
	kind, err := classifyTag(mmsPDU[0])
	if err != nil || (kind != PduConfirmedResponse && kind != PduConfirmedError && kind != PduReject) {
		return 0, 0, false
	}
	off := 2
	if l := mmsPDU[1]; l > 0x80 {
		off += int(l & 0x7f)
	}
	if off >= len(mmsPDU) {
		return 0, 0, false
	}
	el, _, err := berutil.DecodeElement(mmsPDU, off)
	if err != nil {
		return 0, 0, false
	}
	id, err := ExtractInvokeIDFor(kind, el.Full)
	if err != nil {
		return 0, 0, false
	}
	return kind, id, true
}

// DecodeConfirmedResponse parses the inner content of a
// ConfirmedResponsePdu (after the 0xa1 outer tag is stripped).
func DecodeConfirmedResponse(content []byte) (*ConfirmedResponse, error) {
	invokeID, serviceRaw, err := codec.UnmarshalConfirmedResponse(content)
	if err != nil {
		return nil, fmt.Errorf("pdu: %w", err)
	}

	kind := classifyServiceByTagNum(serviceRaw.Tag)

	return &ConfirmedResponse{
		InvokeID:    invokeID,
		ServiceKind: kind,
		ServiceData: serviceRaw,
	}, nil
}

// ClassifyServiceTag maps a BER tag number to a ServiceKind.
// Exported for use by the root package's test mock server dispatch.
func ClassifyServiceTag(tagNum int) ServiceKind {
	return classifyServiceByTagNum(tagNum)
}

func classifyServiceByTagNum(tagNum int) ServiceKind {
	switch tagNum {
	case asn1util.TagNumStatus:
		return ServiceStatus
	case asn1util.TagNumGetNameList:
		return ServiceGetNameList
	case asn1util.TagNumIdentify:
		return ServiceIdentify
	case asn1util.TagNumRead:
		return ServiceRead
	case asn1util.TagNumWrite:
		return ServiceWrite
	case asn1util.TagNumGetVariableAccessAttributes:
		return ServiceGetVariableAccessAttrs
	case asn1util.TagNumDefineNamedVariableList:
		return ServiceDefineNamedVariableList
	case asn1util.TagNumGetNamedVariableListAttrs:
		return ServiceGetNamedVariableListAttrs
	case asn1util.TagNumDeleteNamedVariableList:
		return ServiceDeleteNamedVariableList
	case asn1util.TagNumFileOpen:
		return ServiceFileOpen
	case asn1util.TagNumFileRead:
		return ServiceFileRead
	case asn1util.TagNumFileClose:
		return ServiceFileClose
	case asn1util.TagNumFileRename:
		return ServiceFileRename
	case asn1util.TagNumFileDelete:
		return ServiceFileDelete
	case asn1util.TagNumFileDirectory:
		return ServiceFileDirectory
	case asn1util.TagNumObtainFile:
		return ServiceObtainFile
	case asn1util.TagNumReadJournal:
		return ServiceReadJournal
	default:
		return ServiceUnknown
	}
}

// MarshalConfirmedRequest builds a complete ConfirmedRequestPdu for a
// given service. tagNum is the CHOICE tag number and constructed
// indicates whether the service body is constructed. servicePayload
// is the pre-encoded content for the service (e.g., nil for Identify).
func MarshalConfirmedRequest(invokeID codec.InvokeID, tagNum int, constructed bool, servicePayload []byte) ([]byte, error) {
	return codec.MarshalConfirmedRequest(invokeID, tagNum, constructed, servicePayload)
}

// marshalConfirmedLegacy converts a single-byte tag constant to
// (tagNum, constructed) and delegates to MarshalConfirmedRequest.
func marshalConfirmedLegacy(invokeID codec.InvokeID, tag byte, payload []byte) ([]byte, error) {
	return MarshalConfirmedRequest(invokeID, asn1util.TagNumber(tag), asn1util.IsConstructed(tag), payload)
}
