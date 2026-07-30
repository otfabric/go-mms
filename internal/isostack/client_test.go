// SPDX-License-Identifier: MIT

package isostack

import (
	"bytes"
	"encoding/asn1"
	"testing"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/presentation"
	"github.com/otfabric/go-mms/internal/session"
)

func defaultParams() Params {
	return Params{
		CallingSessionSelector:      []byte{0x00, 0x01},
		CalledSessionSelector:       []byte{0x00, 0x01},
		CallingPresentationSelector: []byte{0x00, 0x00, 0x00, 0x01},
		CalledPresentationSelector:  []byte{0x00, 0x00, 0x00, 0x01},
		ACSE: acse.AARQParams{
			CalledAPTitle:      asn1.ObjectIdentifier{1, 1, 1, 1},
			CalledAEQualifier:  12,
			CallingAPTitle:     asn1.ObjectIdentifier{1, 1, 1, 1},
			CallingAEQualifier: 12,
		},
	}
}

func TestAssociateRoundTrip(t *testing.T) {
	params := defaultParams()
	mmsInitReq := []byte{0xa8, 0x04, 0x01, 0x02, 0x03, 0x04}

	reqBytes, err := EncodeAssociateRequest(params, mmsInitReq)
	if err != nil {
		t.Fatalf("EncodeAssociateRequest: %v", err)
	}
	if reqBytes[0] != session.SIConnect {
		t.Fatalf("request SI = 0x%02x, want 0x%02x (CONNECT)", reqBytes[0], session.SIConnect)
	}

	mmsInitResp := []byte{0xa9, 0x03, 0x01, 0x02, 0x03}
	serverResp := buildMockAssociateResponse(t, mmsInitResp)

	result, err := DecodeAssociateResponse(serverResp)
	if err != nil {
		t.Fatalf("DecodeAssociateResponse: %v", err)
	}
	if result.ACSEResult != acse.ResultAccepted {
		t.Errorf("ACSEResult = %d, want %d (accepted)", result.ACSEResult, acse.ResultAccepted)
	}
	if !bytes.Equal(result.MmsPayload, mmsInitResp) {
		t.Errorf("MmsPayload = %x, want %x", result.MmsPayload, mmsInitResp)
	}
}

func TestDataRoundTrip(t *testing.T) {
	mmsPayload := []byte{0xa0, 0x07, 0x02, 0x01, 0x01, 0x82, 0x02, 0xAA, 0xBB}

	dataBytes := EncodeDataRequest(mmsPayload)
	if dataBytes[0] != session.SIData {
		t.Fatalf("data SI = 0x%02x, want 0x%02x (DATA)", dataBytes[0], session.SIData)
	}

	decoded, err := DecodeDataResponse(dataBytes)
	if err != nil {
		t.Fatalf("DecodeDataResponse: %v", err)
	}
	if !bytes.Equal(decoded, mmsPayload) {
		t.Errorf("decoded = %x, want %x", decoded, mmsPayload)
	}
}

func TestReleaseRequest(t *testing.T) {
	rel := EncodeReleaseRequest()
	if rel[0] != session.SIFinish {
		t.Fatalf("release SI = 0x%02x, want 0x%02x (FINISH)", rel[0], session.SIFinish)
	}

	spdu, err := session.Parse(rel)
	if err != nil {
		t.Fatalf("session.Parse: %v", err)
	}
	if spdu.Type != session.SpduFinish {
		t.Errorf("SPDU type = %s, want FINISH", spdu.Type)
	}
	if len(spdu.UserData) == 0 {
		t.Fatal("FINISH should carry user data")
	}
}

func TestAbort(t *testing.T) {
	ab := EncodeAbort()
	if ab[0] != session.SIAbort {
		t.Fatalf("abort SI = 0x%02x, want 0x%02x (ABORT)", ab[0], session.SIAbort)
	}

	spdu, err := session.Parse(ab)
	if err != nil {
		t.Fatalf("session.Parse: %v", err)
	}
	if spdu.Type != session.SpduAbort {
		t.Errorf("SPDU type = %s, want ABORT", spdu.Type)
	}
}

func TestDecodeAssociateResponseWrongSpdu(t *testing.T) {
	data := session.EncodeData([]byte{0x01})
	_, err := DecodeAssociateResponse(data)
	if err == nil {
		t.Error("expected error for non-ACCEPT SPDU")
	}
}

func TestDecodeDataResponseFinish(t *testing.T) {
	rlrq := acse.EncodeRLRQ()
	finish := session.EncodeFinish(rlrq)
	_, err := DecodeDataResponse(finish)
	if err == nil {
		t.Error("expected error for FINISH during data transfer")
	}
}

func TestDecodeDataResponseWrongContext(t *testing.T) {
	// Build data with ACSE context instead of MMS context
	pres := presentation.EncodeUserData(presentation.ContextIDACSE, []byte{0x01})
	data := session.EncodeData(pres)
	_, err := DecodeDataResponse(data)
	if err == nil {
		t.Error("expected error for wrong presentation context in data response")
	}
}

func TestDecodeAssociateResponse_MoreErrors(t *testing.T) {
	if _, err := DecodeAssociateResponse([]byte{0xff}); err == nil {
		t.Fatal("expected session parse error")
	}
	// ACCEPT with garbage presentation.
	bad := session.EncodeAccept(session.ConnectParams{CalledSelector: []byte{0x01}}, []byte{0xff})
	if _, err := DecodeAssociateResponse(bad); err == nil {
		t.Fatal("expected presentation parse error")
	}
	// ACCEPT wrapping non-CPA presentation user-data (DATA-style).
	pres := presentation.EncodeUserData(presentation.ContextIDACSE, []byte{0x01})
	wrongKind := session.EncodeAccept(session.ConnectParams{CalledSelector: []byte{0x01}}, pres)
	if _, err := DecodeAssociateResponse(wrongKind); err == nil {
		t.Fatal("expected CPA kind error")
	}
}

func TestDecodeDataResponse_AbortAndUnexpected(t *testing.T) {
	if _, err := DecodeDataResponse(EncodeAbort()); err == nil {
		t.Fatal("expected abort error")
	}
	if _, err := DecodeDataResponse(session.EncodeConnect(session.ConnectParams{}, []byte{1})); err == nil {
		t.Fatal("expected unexpected SPDU error")
	}
	if _, err := DecodeDataResponse([]byte{0x01}); err == nil {
		t.Fatal("expected session parse error")
	}
}

func buildMockAssociateResponse(t *testing.T, mmsPayload []byte) []byte {
	t.Helper()

	aare := acse.EncodeAARE(acse.ResultAccepted, mmsPayload)
	cpa := presentation.EncodeCPA([]byte{0x00, 0x00, 0x00, 0x01}, aare)

	return session.EncodeAccept(session.ConnectParams{
		CalledSelector: []byte{0x00, 0x01},
	}, cpa)
}
