// Package isostack orchestrates the ISO upper-layer protocol stack
// (session → presentation → ACSE) for client-side MMS connections.
//
// This package composes internal/session, internal/presentation, and
// internal/acse into a single codec layer. It does not own the transport
// connection — that responsibility belongs to the caller (via go-tpkt /
// go-cotp or a mock transport in tests).
//
// This package is internal — users interact with mms.Client.
package isostack

import (
	"fmt"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/presentation"
	"github.com/otfabric/go-mms/internal/session"
)

// Params holds all ISO upper-layer parameters needed for association.
type Params struct {
	CallingSessionSelector      []byte
	CalledSessionSelector       []byte
	CallingPresentationSelector []byte
	CalledPresentationSelector  []byte
	ACSE                        acse.AARQParams
}

// AssociateResult holds the parsed result of a full ISO association response
// (Session ACCEPT + Presentation CPA + ACSE AARE).
type AssociateResult struct {
	ACSEResult int    // 0=accepted, 1=rejected-permanent, 2=rejected-transient
	MmsPayload []byte // inner MMS Initiate Response bytes
}

// EncodeAssociateRequest builds the full ISO upper-layer association
// request: Session CONNECT wrapping Presentation CP wrapping ACSE AARQ
// wrapping the given MMS Initiate Request payload.
//
// The returned bytes are ready to be sent as COTP user data.
func EncodeAssociateRequest(p Params, mmsPayload []byte) ([]byte, error) {
	aarq, err := acse.EncodeAARQ(p.ACSE, mmsPayload)
	if err != nil {
		return nil, fmt.Errorf("isostack: encode AARQ: %w", err)
	}

	cp := presentation.EncodeCP(presentation.ConnectParams{
		CallingSelector: p.CallingPresentationSelector,
		CalledSelector:  p.CalledPresentationSelector,
	}, aarq)

	cn := session.EncodeConnect(session.ConnectParams{
		CallingSelector: p.CallingSessionSelector,
		CalledSelector:  p.CalledSessionSelector,
	}, cp)

	return cn, nil
}

// DecodeAssociateResponse parses a full ISO association response
// (Session ACCEPT → Presentation CPA → ACSE AARE) from the raw
// bytes received as COTP user data.
func DecodeAssociateResponse(data []byte) (*AssociateResult, error) {
	spdu, err := session.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("isostack: session: %w", err)
	}
	if spdu.Type != session.SpduAccept {
		return nil, fmt.Errorf("isostack: expected Session ACCEPT, got %s", spdu.Type)
	}

	ppdu, err := presentation.Parse(spdu.UserData)
	if err != nil {
		return nil, fmt.Errorf("isostack: presentation: %w", err)
	}
	if ppdu.Kind != presentation.PpduCPA {
		return nil, fmt.Errorf("isostack: expected Presentation CPA, got %s", ppdu.Kind)
	}
	if ppdu.ContextID != presentation.ContextIDACSE {
		return nil, fmt.Errorf("isostack: expected ACSE context (%d) in association response, got context %d",
			presentation.ContextIDACSE, ppdu.ContextID)
	}

	apdu, err := acse.Parse(ppdu.UserData)
	if err != nil {
		return nil, fmt.Errorf("isostack: acse: %w", err)
	}
	if apdu.Type != acse.ApduAARE {
		return nil, fmt.Errorf("isostack: expected ACSE AARE, got %s", apdu.Type)
	}

	return &AssociateResult{
		ACSEResult: apdu.AARE.Result,
		MmsPayload: apdu.AARE.UserData,
	}, nil
}

// EncodeDataRequest wraps an MMS PDU in presentation user-data inside
// a session DATA SPDU, ready to be sent as COTP user data.
func EncodeDataRequest(mmsPayload []byte) []byte {
	pres := presentation.EncodeUserData(presentation.ContextIDMMS, mmsPayload)
	return session.EncodeData(pres)
}

// DecodeDataResponse parses session DATA → presentation user-data
// and returns the inner MMS PDU payload.
func DecodeDataResponse(data []byte) ([]byte, error) {
	spdu, err := session.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("isostack: session: %w", err)
	}

	switch spdu.Type {
	case session.SpduData:
		// Normal data transfer
	case session.SpduFinish:
		return nil, fmt.Errorf("isostack: received Session FINISH (server-initiated release)")
	case session.SpduAbort:
		return nil, fmt.Errorf("isostack: received Session ABORT")
	default:
		return nil, fmt.Errorf("isostack: unexpected SPDU type %s during data transfer", spdu.Type)
	}

	ppdu, err := presentation.Parse(spdu.UserData)
	if err != nil {
		return nil, fmt.Errorf("isostack: presentation: %w", err)
	}
	if ppdu.ContextID != presentation.ContextIDMMS {
		return nil, fmt.Errorf("isostack: expected MMS context (%d) in data response, got context %d",
			presentation.ContextIDMMS, ppdu.ContextID)
	}

	return ppdu.UserData, nil
}

// EncodeReleaseRequest builds a release request: Session FINISH wrapping
// Presentation user-data (ACSE context) wrapping ACSE RLRQ.
func EncodeReleaseRequest() []byte {
	rlrq := acse.EncodeRLRQ()
	pres := presentation.EncodeUserData(presentation.ContextIDACSE, rlrq)
	return session.EncodeFinish(pres)
}

// EncodeAbort builds an abort: Session ABORT wrapping Presentation
// user-data (ACSE context) wrapping ACSE ABRT.
func EncodeAbort() []byte {
	abrt := acse.EncodeABRT(0)
	pres := presentation.EncodeUserData(presentation.ContextIDACSE, abrt)
	return session.EncodeAbort(pres)
}
