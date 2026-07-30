// SPDX-License-Identifier: MIT

package isostack

import (
	"testing"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/presentation"
	"github.com/otfabric/go-mms/internal/session"
)

func TestDecodeAssociateRequest_Edges(t *testing.T) {
	if _, err := DecodeAssociateRequest([]byte{0xff}); err == nil {
		t.Fatal("session parse")
	}
	// CONNECT with garbage presentation.
	bad := session.EncodeConnect(session.ConnectParams{}, []byte{0xff})
	if _, err := DecodeAssociateRequest(bad); err == nil {
		t.Fatal("presentation parse")
	}
	// CONNECT wrapping user-data (not CP).
	pres := presentation.EncodeUserData(presentation.ContextIDACSE, []byte{0x01})
	wrongKind := session.EncodeConnect(session.ConnectParams{}, pres)
	if _, err := DecodeAssociateRequest(wrongKind); err == nil {
		t.Fatal("CP kind")
	}
	// CP with non-AARQ ACSE (AARE).
	aare := acse.EncodeAARE(acse.ResultAccepted, nil)
	cp := presentation.EncodeCP(presentation.ConnectParams{}, aare)
	wrongAPDU := session.EncodeConnect(session.ConnectParams{}, cp)
	if _, err := DecodeAssociateRequest(wrongAPDU); err == nil {
		t.Fatal("AARQ type")
	}
	// CP with garbage ACSE.
	cpBad := presentation.EncodeCP(presentation.ConnectParams{}, []byte{0xff})
	badACSE := session.EncodeConnect(session.ConnectParams{}, cpBad)
	if _, err := DecodeAssociateRequest(badACSE); err == nil {
		t.Fatal("ACSE parse")
	}
}

func TestDecodeAssociateResponse_Edges(t *testing.T) {
	// CPA with wrong context (MMS instead of ACSE): rebuild CPA-like PPDU
	// using EncodeUserData payload shape inside a CPA shell is awkward —
	// inject via EncodeCPA then swap by building ACCEPT with MMS-context CPA.
	// EncodeCPA always uses ACSE; craft normal-mode with MMS context ID.
	mmsCtx := encodeFullyEncodedDataForTest(presentation.ContextIDMMS, acse.EncodeAARE(acse.ResultAccepted, nil))
	cpaWrongCtx := wrapCPA(mmsCtx)
	badCtx := session.EncodeAccept(session.ConnectParams{}, cpaWrongCtx)
	if _, err := DecodeAssociateResponse(badCtx); err == nil {
		t.Fatal("wrong context")
	}

	// CPA with non-AARE ACSE (AARQ).
	aarq, err := acse.EncodeAARQ(acse.AARQParams{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cpa := presentation.EncodeCPA(nil, aarq)
	wrongAPDU := session.EncodeAccept(session.ConnectParams{}, cpa)
	if _, err := DecodeAssociateResponse(wrongAPDU); err == nil {
		t.Fatal("AARE type")
	}

	// CPA with garbage ACSE.
	cpaBad := presentation.EncodeCPA(nil, []byte{0xff})
	badACSE := session.EncodeAccept(session.ConnectParams{}, cpaBad)
	if _, err := DecodeAssociateResponse(badACSE); err == nil {
		t.Fatal("ACSE parse")
	}
}

func TestDecodeDataResponse_PresentationError(t *testing.T) {
	data := session.EncodeData([]byte{0xff})
	if _, err := DecodeDataResponse(data); err == nil {
		t.Fatal("presentation parse")
	}
}

func TestDecodeReleaseRequest_ParseError(t *testing.T) {
	if err := DecodeReleaseRequest([]byte{0xff}); err == nil {
		t.Fatal("session parse")
	}
}

// Minimal CPA (0x31) with result-list + fully-encoded-data payload.
func wrapCPA(fullyEncoded []byte) []byte {
	resultList := berutil.EncodeTLV(0xa5, berutil.EncodeTLV(0x30, berutil.EncodeTLV(0x80, []byte{0})))
	modeSelector := berutil.EncodeTLV(0xa0, []byte{0x80, 0x01, 0x01})
	normalMode := append(resultList, fullyEncoded...)
	inner := append(modeSelector, berutil.EncodeTLV(0xa2, normalMode)...)
	return berutil.EncodeTLV(0x31, inner)
}

func encodeFullyEncodedDataForTest(contextID int, payload []byte) []byte {
	return presentation.EncodeUserData(contextID, payload)
}
