package isostack

import (
	"fmt"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/presentation"
	"github.com/otfabric/go-mms/internal/session"
)

// AssociateRequest holds the parsed result of a client's association request
// (Session CONNECT → Presentation CP → ACSE AARQ).
type AssociateRequest struct {
	MmsPayload []byte        // MMS Initiate Request bytes
	Auth       acse.AuthInfo // ACSE authentication information from AARQ
}

// DecodeAssociateRequest parses an incoming association request from a
// client: Session CONNECT → Presentation CP → ACSE AARQ.
// Returns the MMS payload (Initiate Request) from the AARQ user-information.
func DecodeAssociateRequest(data []byte) (*AssociateRequest, error) {
	spdu, err := session.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("isostack: parse session CONNECT: %w", err)
	}
	if spdu.Type != session.SpduConnect {
		return nil, fmt.Errorf("isostack: expected CONNECT, got %s", spdu.Type)
	}

	ppdu, err := presentation.Parse(spdu.UserData)
	if err != nil {
		return nil, fmt.Errorf("isostack: parse presentation CP: %w", err)
	}
	if ppdu.Kind != presentation.PpduCP {
		return nil, fmt.Errorf("isostack: expected CP, got %s", ppdu.Kind)
	}

	apdu, err := acse.Parse(ppdu.UserData)
	if err != nil {
		return nil, fmt.Errorf("isostack: parse ACSE AARQ: %w", err)
	}
	if apdu.Type != acse.ApduAARQ {
		return nil, fmt.Errorf("isostack: expected AARQ, got %s", apdu.Type)
	}

	return &AssociateRequest{
		MmsPayload: apdu.UserData,
		Auth:       apdu.Auth,
	}, nil
}

// EncodeAssociateResponse builds the server's association response:
// Session ACCEPT → Presentation CPA → ACSE AARE with the given MMS payload.
func EncodeAssociateResponse(mmsPayload []byte) []byte {
	aare := acse.EncodeAARE(acse.ResultAccepted, mmsPayload)
	cpa := presentation.EncodeCPA(nil, aare)
	return session.EncodeAccept(session.ConnectParams{}, cpa)
}

// EncodeAssociateReject builds a rejection response for the association.
func EncodeAssociateReject() []byte {
	aare := acse.EncodeAARE(acse.ResultRejectedPerm, nil)
	cpa := presentation.EncodeCPA(nil, aare)
	return session.EncodeAccept(session.ConnectParams{}, cpa)
}

// DecodeReleaseRequest parses a FINISH SPDU containing RLRQ.
func DecodeReleaseRequest(data []byte) error {
	spdu, err := session.Parse(data)
	if err != nil {
		return fmt.Errorf("isostack: parse session: %w", err)
	}
	if spdu.Type != session.SpduFinish {
		return fmt.Errorf("isostack: expected FINISH, got %s", spdu.Type)
	}
	return nil
}

// EncodeReleaseResponse builds a DISCONNECT SPDU with RLRE.
func EncodeReleaseResponse() []byte {
	rlre := acse.EncodeRLRE()
	userData := presentation.EncodeUserData(presentation.ContextIDACSE, rlre)
	return session.EncodeDisconnect(userData)
}
