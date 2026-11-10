// SPDX-License-Identifier: MIT

package isostack

import (
	"errors"
	"fmt"
	"slices"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/presentation"
	"github.com/otfabric/go-mms/internal/session"
)

// AssociateRequest holds the parsed result of a client's association request
// (Session CONNECT → Presentation CP → ACSE AARQ).
type AssociateRequest struct {
	MmsPayload []byte        // MMS Initiate Request bytes
	Auth       acse.AuthInfo // ACSE authentication information from AARQ
	AARQ       *acse.AARQInfo

	// Contexts are the presentation context ids the client chose for
	// ACSE and MMS; Negotiation is the answer to every proposed context.
	Contexts    Contexts
	Negotiation presentation.Negotiation

	// SessionVersion is the session version selected for the ACCEPT.
	SessionVersion byte
}

// RefusedError is returned by DecodeAssociateRequest when the request
// was decoded far enough to be answered but cannot be accepted. Response
// is the refusal to send before closing the transport: a Session REFUSE
// carrying a presentation CPR and, where the ACSE was reached, an AARE
// with result rejected-permanent.
type RefusedError struct {
	Reason   string
	Response []byte
}

func (e *RefusedError) Error() string { return "isostack: association refused: " + e.Reason }

// Reject describes an AARE rejection: the result, the
// result-source-diagnostic and the MMS payload (an Initiate-ErrorPDU, or
// nil).
type Reject struct {
	Result     int
	DiagSource int
	Diagnostic int
	MmsPayload []byte
}

// DecodeAssociateRequest parses an incoming association request from a
// client: Session CONNECT → Presentation CP → ACSE AARQ, and negotiates
// the session version and the presentation contexts.
//
// A CONNECT that cannot be parsed returns a plain error: there is
// nothing to answer. Every later failure returns a *RefusedError whose
// Response the caller sends:
//   - no common session version: REFUSE reason 132 (ISO 8327-1);
//   - duplex functional unit not proposed: REFUSE reason 134 (rejection
//     by the SPM, implementation restriction: MMS needs full duplex,
//     ISO 9506-2 24.7);
//   - undecodable CP, or ACSE/MMS context not acceptable: REFUSE with a
//     CPR carrying the result list and provider-reason
//     reason-not-specified (ISO 8823-1);
//   - undecodable AARQ: AARE rejected-permanent, acse-service-provider
//     no-reason-given; protocol-version without version1: acse-service-
//     provider no-common-acse-version; application-context-name other
//     than MMS 1.0.9506.2.3 (ISO 9506-2 Annex A.7): acse-service-user
//     application-context-name-not-supported; no MMS user-information:
//     acse-service-user no-reason-given (ISO 8650-1).
func DecodeAssociateRequest(data []byte) (*AssociateRequest, error) {
	spdu, err := session.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("isostack: parse session CONNECT: %w", err)
	}
	if spdu.Type != session.SpduConnect {
		return nil, fmt.Errorf("isostack: expected CONNECT, got %s", spdu.Type)
	}

	version := session.NegotiateVersion(spdu.Version)
	if version == 0 {
		return nil, refuseSession(session.ReasonVersionNotSupported, fmt.Sprintf("no common session version in %#x", spdu.Version))
	}
	if spdu.HasRequirements && spdu.Requirements&session.FUDuplex == 0 {
		return nil, refuseSession(session.ReasonSPMRestriction, fmt.Sprintf("duplex functional unit not proposed (requirements %#04x)", spdu.Requirements))
	}

	ppdu, err := presentation.Parse(spdu.UserData)
	if err != nil || ppdu.Kind != presentation.PpduCP {
		reason := "undecodable presentation CP"
		if err == nil {
			reason = "expected CP, got " + ppdu.Kind.String()
		}
		return nil, refusePresentation(nil, reason)
	}
	neg := presentation.Negotiate(ppdu.Contexts)
	if neg.ACSE == 0 || neg.MMS == 0 {
		return nil, refusePresentation(neg.Results, fmt.Sprintf("no acceptable ACSE and MMS presentation contexts in %d proposed", len(ppdu.Contexts)))
	}
	req := &AssociateRequest{
		Contexts:       Contexts{ACSE: neg.ACSE, MMS: neg.MMS},
		Negotiation:    neg,
		SessionVersion: version,
	}
	if ppdu.ContextID != neg.ACSE {
		return nil, req.refuse(Reject{Result: acse.ResultRejectedPerm, DiagSource: acse.DiagSourceProvider, Diagnostic: acse.DiagProviderNoReasonGiven},
			fmt.Sprintf("AARQ on presentation context %d, ACSE context is %d", ppdu.ContextID, neg.ACSE))
	}

	apdu, err := acse.Parse(ppdu.UserData)
	if err != nil || apdu.Type != acse.ApduAARQ {
		reason := "undecodable AARQ"
		if err == nil {
			reason = "expected AARQ, got " + apdu.Type.String()
		}
		return nil, req.refuse(Reject{Result: acse.ResultRejectedPerm, DiagSource: acse.DiagSourceProvider, Diagnostic: acse.DiagProviderNoReasonGiven}, reason)
	}
	req.AARQ = apdu.AARQ
	req.Auth = apdu.Auth
	req.MmsPayload = apdu.UserData
	switch {
	case !apdu.AARQ.ProtocolVersionOK:
		return nil, req.refuse(Reject{Result: acse.ResultRejectedPerm, DiagSource: acse.DiagSourceProvider, Diagnostic: acse.DiagProviderNoCommonACSEVersion},
			"AARQ protocol-version without version1")
	case !apdu.AARQ.IsMMS():
		return nil, req.refuse(Reject{Result: acse.ResultRejectedPerm, DiagSource: acse.DiagSourceUser, Diagnostic: acse.DiagUserAppContextNotSupported},
			fmt.Sprintf("application-context-name %x is not MMS", apdu.AARQ.AppContext))
	case len(apdu.UserData) == 0:
		return nil, req.refuse(Reject{Result: acse.ResultRejectedPerm, DiagSource: acse.DiagSourceUser, Diagnostic: acse.DiagUserNoReasonGiven},
			"AARQ without MMS user-information")
	}
	return req, nil
}

func refuseSession(reason byte, msg string) error {
	return &RefusedError{Reason: msg, Response: mustSPDU(session.EncodeRefuse(reason, nil))}
}

func refusePresentation(results []presentation.ContextResult, msg string) error {
	cpr := presentation.EncodeCPR(nil, results, presentation.ProviderReasonNotSpecified, 0, nil)
	return &RefusedError{Reason: msg, Response: mustSPDU(session.EncodeRefuse(session.ReasonUserData, cpr))}
}

func (r *AssociateRequest) refuse(rj Reject, msg string) error {
	b, err := r.EncodeReject(rj)
	if err != nil {
		return fmt.Errorf("isostack: %s: %w", msg, err)
	}
	return &RefusedError{Reason: msg, Response: b}
}

// ResponseParams are the server's own AARE fields.
type ResponseParams struct {
	RespondingAPTitle     []int
	RespondingAEQualifier int
}

// EncodeAssociateResponse builds the server's acceptance: Session ACCEPT
// (negotiated version, duplex) → Presentation CPA (the negotiated result
// list, on the client's ACSE context) → ACSE AARE accepted with the MMS
// payload on the client's MMS context. responder-acse-requirements
// {authentication} is echoed when the AARQ selected the authentication
// functional unit and carried an authentication value that the caller
// accepted (ISO 8650-1).
func (r *AssociateRequest) EncodeAssociateResponse(p ResponseParams, mmsPayload []byte) ([]byte, error) {
	aare := acse.EncodeAAREWith(acse.AAREParams{
		Result:                acse.ResultAccepted,
		DiagSource:            acse.DiagSourceUser,
		MMSContextID:          r.Contexts.MMS,
		RespondingAPTitle:     p.RespondingAPTitle,
		RespondingAEQualifier: p.RespondingAEQualifier,
		ResponderAuth:         r.AARQ != nil && r.AARQ.AuthRequested && r.Auth.Mechanism != acse.AuthNone,
	}, mmsPayload)
	cpa := presentation.EncodeCPAResults(nil, r.Negotiation.Results, r.Contexts.ACSE, aare)
	b, err := session.EncodeAccept(session.ConnectParams{Version: r.SessionVersion, Requirements: session.FUDuplex}, cpa)
	if err != nil {
		return nil, fmt.Errorf("isostack: encode ACCEPT: %w", err)
	}
	return b, nil
}

// EncodeReject builds the refusal of an association whose ACSE was
// reached: Session REFUSE (reason 2, rejection by the SS-user with user
// data) → Presentation CPR (the result list, no provider-reason: the
// presentation user rejected) → ACSE AARE with the given result,
// diagnostic and MMS payload (ISO 8327-1, ISO 8823-1, ISO 8650-1;
// IEC 61850-8-1 Table 3 makes these mandatory).
func (r *AssociateRequest) EncodeReject(rj Reject) ([]byte, error) {
	aare := acse.EncodeAAREWith(acse.AAREParams{
		Result:       rj.Result,
		DiagSource:   rj.DiagSource,
		Diagnostic:   rj.Diagnostic,
		MMSContextID: r.Contexts.MMS,
	}, rj.MmsPayload)
	cpr := presentation.EncodeCPR(nil, r.Negotiation.Results, -1, r.Contexts.ACSE, aare)
	b, err := session.EncodeRefuse(session.ReasonUserData, cpr)
	if err != nil {
		return nil, fmt.Errorf("isostack: encode REFUSE: %w", err)
	}
	return b, nil
}

// ErrNotRelease reports that a FINISH did not carry an RLRQ.
var ErrNotRelease = errors.New("isostack: FINISH without ACSE RLRQ")

// DecodeReleaseRequest parses a FINISH SPDU whose user data is an ACSE
// RLRQ on the default ACSE context.
func DecodeReleaseRequest(data []byte) error {
	return DecodeReleaseRequestOn(DefaultContexts, data)
}

// DecodeReleaseRequestOn parses a FINISH SPDU whose user data is an
// ACSE RLRQ on the association's ACSE context (ISO 9506-2 Annex
// A.1.2.2: A-RELEASE.indication). A FINISH with other user data returns
// an error wrapping ErrNotRelease.
func DecodeReleaseRequestOn(c Contexts, data []byte) error {
	spdu, err := session.Parse(data)
	if err != nil {
		return fmt.Errorf("isostack: parse session: %w", err)
	}
	if spdu.Type != session.SpduFinish {
		return fmt.Errorf("isostack: expected FINISH, got %s", spdu.Type)
	}
	if err := expectACSE(spdu.UserData, c.ACSE, acse.ApduRLRQ); err != nil {
		return fmt.Errorf("%w: %w", ErrNotRelease, err)
	}
	return nil
}

// EncodeReleaseResponse builds a DISCONNECT SPDU with RLRE on the
// default ACSE context.
func EncodeReleaseResponse() []byte {
	return slices.Clone(releaseResponse)
}

// EncodeReleaseResponseOn builds a DISCONNECT SPDU with RLRE on the
// association's ACSE context (ISO 9506-2 Annex A.1.2.2:
// A-RELEASE.response).
func EncodeReleaseResponseOn(c Contexts) []byte {
	if c.ACSE == presentation.ContextIDACSE {
		return EncodeReleaseResponse()
	}
	return mustSPDU(session.EncodeDisconnect(presentation.EncodeUserData(c.ACSE, acse.EncodeRLRE())))
}

var releaseResponse = mustSPDU(session.EncodeDisconnect(
	presentation.EncodeUserData(presentation.ContextIDACSE, acse.EncodeRLRE())))
