// SPDX-License-Identifier: MIT

package isostack

import (
	"bytes"
	"testing"

	"github.com/otfabric/go-mms/internal/session"
)

func TestDecodeAssociateRequest(t *testing.T) {
	mmsPayload := []byte{0xa8, 0x04, 0x01, 0x02, 0x03, 0x04}
	params := defaultParams()
	req, err := EncodeAssociateRequest(params, mmsPayload)
	if err != nil {
		t.Fatalf("EncodeAssociateRequest: %v", err)
	}

	result, err := DecodeAssociateRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(result.MmsPayload, mmsPayload) {
		t.Fatalf("payload mismatch: got %x, want %x", result.MmsPayload, mmsPayload)
	}
}

func TestEncodeAssociateResponse(t *testing.T) {
	mmsPayload := []byte{0x04, 0x05}
	resp := encodeAccept(t, mmsPayload)
	if len(resp) == 0 {
		t.Fatal("empty")
	}
	spdu, err := session.Parse(resp)
	if err != nil {
		t.Fatal(err)
	}
	if spdu.Type != session.SpduAccept {
		t.Fatalf("got SPDU type %s, want ACCEPT", spdu.Type)
	}
}

// ISO 8327-1 / ISO 8823-1 (MMS-STK-03): a rejected association is a
// Session REFUSE carrying a CPR, not an ACCEPT carrying a CPA.
func TestEncodeAssociateReject(t *testing.T) {
	resp := encodeReject(t)
	spdu, err := session.Parse(resp)
	if err != nil {
		t.Fatal(err)
	}
	if spdu.Type != session.SpduRefuse || spdu.Reason != session.ReasonUserData {
		t.Fatalf("got SPDU type %s reason %d, want REFUSE reason 2", spdu.Type, spdu.Reason)
	}
	if spdu.UserData[0] != 0x30 {
		t.Fatalf("PPDU tag %02x, want CPR 0x30", spdu.UserData[0])
	}
}

func TestDecodeReleaseRequest(t *testing.T) {
	req := EncodeReleaseRequest()
	err := DecodeReleaseRequest(req)
	if err != nil {
		t.Fatal(err)
	}
}

func TestEncodeReleaseResponse(t *testing.T) {
	resp := EncodeReleaseResponse()
	if len(resp) == 0 {
		t.Fatal("empty")
	}
	spdu, err := session.Parse(resp)
	if err != nil {
		t.Fatal(err)
	}
	if spdu.Type != session.SpduDisconnect {
		t.Fatalf("got SPDU type %s, want DISCONNECT", spdu.Type)
	}
}

func TestDecodeAssociateRequestBadSpdu(t *testing.T) {
	data := session.EncodeData([]byte{0x01})
	_, err := DecodeAssociateRequest(data)
	if err == nil {
		t.Error("expected error for non-CONNECT SPDU")
	}
}

func TestDecodeReleaseRequestBadSpdu(t *testing.T) {
	data := session.EncodeData([]byte{0x01})
	err := DecodeReleaseRequest(data)
	if err == nil {
		t.Error("expected error for non-FINISH SPDU")
	}
}
