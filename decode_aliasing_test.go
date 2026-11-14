// SPDX-License-Identifier: MIT

package mms

import (
	"bytes"
	"testing"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/isostack"
	"github.com/otfabric/go-mms/internal/pdu"
)

// pdusWithRaw are MMS PDUs whose decoded form carries Raw: services the
// decoder does not model, a cancel PDU, and a modelled service that does
// not decode.
func pdusWithRaw(t *testing.T) map[string][]byte {
	must := mustB(t)
	return map[string][]byte{
		"request of an unmodelled service":  must(codec.MarshalConfirmedRequest(1, 44, true, []byte{0x1a, 0x01, 0x41})),
		"response of an unmodelled service": must(codec.MarshalConfirmedResponse(1, asn1util.TagNumStatus, true, []byte{0x80, 0x01, 0x00})),
		"cancel response":                   codec.MarshalCancelResponse(9),
		"modelled service that fails":       must(codec.MarshalConfirmedRequest(1, asn1util.TagNumGetNameList, true, []byte{0xa0, 0x03, 0x80, 0x01, 0x00, 0xa1, 0x02, 0x85, 0x00})),
	}
}

// Raw of a decoded PDU is a copy (OWNERSHIP.md): changing the decoded
// buffer afterwards does not change it.
func TestDecodeResultsDoNotAliasInput(t *testing.T) {
	for name, b := range pdusWithRaw(t) {
		in := bytes.Clone(b)
		p, err := DecodePDU(in)
		if err != nil || len(p.Raw) == 0 {
			t.Fatalf("%s: %+v, %v", name, p, err)
		}
		want := bytes.Clone(p.Raw)
		for i := range in {
			in[i] ^= 0xff
		}
		if !bytes.Equal(p.Raw, want) {
			t.Errorf("DecodePDU, %s: Raw changed from % x to % x with the input", name, want, p.Raw)
		}

		tsdu := isostack.EncodeDataRequest(b)
		c, err := DecodeTSDU(tsdu)
		if err != nil || c.PDU == nil || len(c.PDU.Raw) == 0 {
			t.Fatalf("DecodeTSDU, %s: %+v, %v", name, c, err)
		}
		want = bytes.Clone(c.PDU.Raw)
		for i := range tsdu {
			tsdu[i] ^= 0xff
		}
		if !bytes.Equal(c.PDU.Raw, want) {
			t.Errorf("DecodeTSDU, %s: Raw changed from % x to % x with the input", name, want, c.PDU.Raw)
		}
	}
}

// ServiceSpecific.Raw of a ServiceError is a copy of the received octets.
func TestServiceErrorServiceSpecificIsCopy(t *testing.T) {
	raw := []byte{0x01, 0x02}
	se := serviceErrorFromWire(pdu.ServiceErrorWire{ErrorClass: 2, ErrorCode: 1, ServiceSpecific: &pdu.ServiceSpecificWire{Kind: 9, Raw: raw}}, 1)
	raw[0] = 0xff
	if se.ServiceSpecific == nil || !bytes.Equal(se.ServiceSpecific.Raw, []byte{0x01, 0x02}) {
		t.Fatalf("ServiceSpecific = %+v after the received octets changed", se.ServiceSpecific)
	}
}
