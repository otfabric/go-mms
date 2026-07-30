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

	if err := parseServiceError([]byte{0xa0, 0x05}, &ConfirmedError{}); err == nil {
		t.Fatal("truncated service error")
	}
	if err := parseServiceError(berutil.EncodeTLV(0xa1, nil), &ConfirmedError{}); err != nil {
		t.Fatal(err)
	}

	if err := parseErrorClass(nil, &ConfirmedError{}); err != nil {
		t.Fatal(err)
	}
	if err := parseErrorClass([]byte{0x80, 0x05}, &ConfirmedError{}); err == nil {
		t.Fatal("truncated error class")
	}
	trail := append(berutil.EncodeTLV(0x80, []byte{1}), 0x00)
	if err := parseErrorClass(trail, &ConfirmedError{}); err == nil {
		t.Fatal("trailing")
	}
	// Inner present but empty INTEGER content → DecodeInteger error.
	// Use a length>0 body that DecodeInteger rejects: too large (>4 bytes).
	if err := parseErrorClass(berutil.EncodeTLV(0x81, []byte{1, 2, 3, 4, 5}), &ConfirmedError{}); err == nil {
		t.Fatal("error code too large")
	}
	r := &ConfirmedError{}
	if err := parseErrorClass(berutil.EncodeTLV(0x82, nil), r); err != nil || r.ErrorClass != 2 {
		t.Fatalf("%+v %v", r, err)
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
	rj, err := DecodeRejectPDU([]byte{0x85, 0x00})
	if err != nil || rj.RejectType != 5 || rj.HasInvokeID {
		t.Fatalf("%+v %v", rj, err)
	}
}
