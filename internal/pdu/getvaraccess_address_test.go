// SPDX-License-Identifier: MIT

package pdu

import (
	"encoding/asn1"
	"testing"

	"github.com/otfabric/go-mms/internal/berutil"
)

// TestUnmarshalGetVarAccessResponse_Address covers the optional address [1]
// (ISO 9506-2, GetVariableAccessAttributes-Response): constructed as the
// standard says, primitive as some servers send it, and damaged.
func TestUnmarshalGetVarAccessResponse_Address(t *testing.T) {
	deletable := berutil.EncodeTLV(0x80, []byte{0x00})
	typeSpec := berutil.EncodeTLV(0xa2, berutil.EncodeTLV(0x83, nil))
	build := func(addr []byte) asn1.RawValue {
		content := append(append(append([]byte{}, deletable...), addr...), typeSpec...)
		return asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 6, IsCompound: true, Bytes: content}
	}

	for name, addr := range map[string][]byte{
		"constructed": berutil.EncodeTLV(0xa1, berutil.EncodeTLV(0x80, []byte{0x01})),
		"primitive":   berutil.EncodeTLV(0x81, []byte{0x01, 0x02}),
	} {
		t.Run(name, func(t *testing.T) {
			res, err := UnmarshalGetVarAccessResponse(build(addr))
			if err != nil {
				t.Fatalf("UnmarshalGetVarAccessResponse: %v", err)
			}
			if res.TypeSpec.Tag != tsTagBoolean {
				t.Errorf("tag = %d, want %d (boolean)", res.TypeSpec.Tag, tsTagBoolean)
			}
		})
	}

	t.Run("damaged", func(t *testing.T) {
		// Primitive address claiming more bytes than present.
		raw := asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 6, IsCompound: true,
			Bytes: append(append([]byte{}, deletable...), 0x81, 0x05, 0x01)}
		if _, err := UnmarshalGetVarAccessResponse(raw); err == nil {
			t.Fatal("expected an error for a truncated address")
		}
	})
}
