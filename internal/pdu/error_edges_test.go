// SPDX-License-Identifier: MIT

package pdu

import (
	"testing"

	"github.com/otfabric/go-mms/internal/berutil"
)

func TestDecodeConfirmedError_Edges(t *testing.T) {
	badID := berutil.EncodeTLV(0x80, nil)
	svc := berutil.EncodeTLV(0xa2, berutil.EncodeTLV(0xa0, berutil.EncodeTLV(0x80, []byte{1})))
	if _, err := DecodeConfirmedError(append(badID, svc...)); err == nil {
		t.Fatal("bad invokeID")
	}
	if _, err := DecodeServiceError([]byte{0xa0, 0x05}); err == nil {
		t.Fatal("truncated service error")
	}
	// ISO 9506-2 clause 7.4.1: errorClass is mandatory and the CHOICE
	// has exactly one primitive alternative with a non-empty INTEGER
	// (rewritten from the earlier lenient expectations, MMS-PDU-10).
	se := &ServiceErrorWire{}
	for name, in := range map[string][]byte{
		"empty":          nil,
		"truncated":      {0x80, 0x05},
		"trailing":       append(berutil.EncodeTLV(0x80, []byte{1}), 0x00),
		"code too large": berutil.EncodeTLV(0x81, []byte{1, 2, 3, 4, 5}),
		"empty code":     berutil.EncodeTLV(0x82, nil),
		"class 13":       berutil.EncodeTLV(0x8d, []byte{1}),
		"constructed":    berutil.EncodeTLV(0xa0, nil),
	} {
		if err := parseErrorClass(in, se); err == nil {
			t.Errorf("%s: parseErrorClass accepted %x", name, in)
		}
	}
	if err := parseErrorClass(berutil.EncodeTLV(0x82, []byte{4}), se); err != nil || se.ErrorClass != 2 || se.ErrorCode != 4 {
		t.Fatalf("%+v %v", se, err)
	}
}

func TestDecodeRejectPDU_Edges(t *testing.T) {
	badID := berutil.EncodeTLV(0x80, nil)
	reason := berutil.EncodeTLV(0x81, []byte{1})
	if _, err := DecodeRejectPDU(append(badID, reason...)); err == nil {
		t.Fatal("bad invokeID")
	}
	if _, err := DecodeRejectPDU(berutil.EncodeTLV(0x85, []byte{1, 2, 3, 4, 5})); err == nil {
		t.Fatal("bad reason integer")
	}
	// An empty pdu-error reason is not an INTEGER (MMS-PDU-25).
	if _, err := DecodeRejectPDU([]byte{0x85, 0x00}); err == nil {
		t.Fatal("empty pdu-error reason accepted")
	}
	// cancel-requestPDU [6] is IMPLICIT NULL without the cancel CBB.
	rj, err := DecodeRejectPDU([]byte{0x86, 0x00})
	if err != nil || rj.RejectType != 6 || rj.HasInvokeID {
		t.Fatalf("%+v %v", rj, err)
	}
}
