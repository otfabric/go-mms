// SPDX-License-Identifier: MIT

package codec

import (
	"encoding/asn1"
	"fmt"

	"github.com/otfabric/go-mms/internal/asn1util"
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
// bytes along with the tag byte. It validates that the tag matches the
// expected top-level PDU tag format.
func UnwrapPdu(data []byte) (tag byte, content []byte, err error) {
	raw, rest, err := asn1util.UnmarshalRaw(data)
	if err != nil {
		return 0, nil, fmt.Errorf("codec: unwrap PDU: %w", err)
	}
	if len(rest) != 0 {
		return 0, nil, fmt.Errorf("codec: unwrap PDU: %d trailing bytes", len(rest))
	}

	// Reconstruct the tag byte from the parsed RawValue.
	tag = byte(raw.Class<<6) | byte(raw.Tag&0x1f)
	if raw.IsCompound {
		tag |= asn1util.ConstructedFlag
	}

	return tag, raw.Bytes, nil
}

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

func unmarshalConfirmedEnvelope(content []byte) (InvokeID, asn1.RawValue, error) {
	var invokeInt int
	rest, err := asn1.Unmarshal(content, &invokeInt)
	if err != nil {
		return 0, asn1.RawValue{}, fmt.Errorf("codec: unmarshal invoke ID: %w", err)
	}

	var serviceRaw asn1.RawValue
	tail, err := asn1.Unmarshal(rest, &serviceRaw)
	if err != nil {
		return 0, asn1.RawValue{}, fmt.Errorf("codec: unmarshal service: %w", err)
	}
	if len(tail) != 0 {
		return 0, asn1.RawValue{}, fmt.Errorf("codec: %d trailing bytes after confirmed envelope", len(tail))
	}

	return InvokeID(invokeInt), serviceRaw, nil
}
