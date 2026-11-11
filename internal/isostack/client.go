// SPDX-License-Identifier: MIT

// Package isostack orchestrates the ISO upper-layer protocol stack
// (session → presentation → ACSE) for MMS associations.
//
// This package composes internal/session, internal/presentation, and
// internal/acse into a single codec layer. It does not own the transport
// connection — that responsibility belongs to the caller (via go-tpkt /
// go-cotp or a mock transport in tests).
//
// This package is internal — users interact with mms.Client.
package isostack

import (
	"bytes"
	"errors"
	"fmt"
	"slices"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/presentation"
	"github.com/otfabric/go-mms/internal/session"
)

// Contexts are the presentation context identifiers of an association:
// the ones the initiator proposed and the responder accepted (ISO
// 8823-1). Every data-phase PPDU names one of them.
type Contexts struct {
	ACSE int
	MMS  int
}

// DefaultContexts are the identifiers this implementation proposes as
// an initiator.
var DefaultContexts = Contexts{ACSE: presentation.ContextIDACSE, MMS: presentation.ContextIDMMS}

// Params holds all ISO upper-layer parameters needed for association.
type Params struct {
	CallingSessionSelector      []byte
	CalledSessionSelector       []byte
	CallingPresentationSelector []byte
	CalledPresentationSelector  []byte
	ACSE                        acse.AARQParams
}

// AssociateResult holds the parsed result of an accepted ISO association
// (Session ACCEPT + Presentation CPA + ACSE AARE).
type AssociateResult struct {
	ACSEResult int    // always acse.ResultAccepted when no error is returned
	MmsPayload []byte // inner MMS Initiate Response bytes
	// ResponderAuth reports responder-acse-requirements {authentication}.
	ResponderAuth bool
}

// Rejection describes an association the responder refused: a Session
// REFUSE (with a presentation CPR), or an ACSE AARE with a rejecting
// result. Fields that the refusal did not carry are -1.
type Rejection struct {
	// SessionReason is the REFUSE Reason Code (ISO 8327-1).
	SessionReason int
	// ProviderReason is the CPR provider-reason (ISO 8823-1).
	ProviderReason int
	// ACSEResult, DiagSource and Diagnostic come from the AARE
	// (ISO 8650-1); DiagSource is acse.DiagSourceUser or
	// acse.DiagSourceProvider.
	ACSEResult int
	DiagSource int
	Diagnostic int
	// MmsPayload is the AARE user-information, normally an
	// Initiate-ErrorPDU; nil when absent.
	MmsPayload []byte
}

func (r *Rejection) Error() string {
	return fmt.Sprintf("isostack: association rejected (session reason %d, presentation provider-reason %d, ACSE result %d, diagnostic %d/%d)",
		r.SessionReason, r.ProviderReason, r.ACSEResult, r.DiagSource, r.Diagnostic)
}

// Transient reports whether the refusal describes a temporary condition:
// session congestion, presentation temporary-congestion, or ACSE
// rejected (transient).
func (r *Rejection) Transient() bool {
	return (r.SessionReason >= 0 && session.ReasonTransient(byte(r.SessionReason))) ||
		r.ProviderReason == presentation.ProviderReasonTemporaryCongestion ||
		r.ACSEResult == acse.ResultRejectedTransient
}

func newRejection() *Rejection {
	return &Rejection{SessionReason: -1, ProviderReason: -1, ACSEResult: -1, DiagSource: -1, Diagnostic: -1}
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

	cn, err := session.EncodeConnect(session.ConnectParams{
		CallingSelector: p.CallingSessionSelector,
		CalledSelector:  p.CalledSessionSelector,
	}, cp)
	if err != nil {
		return nil, fmt.Errorf("isostack: encode CONNECT: %w", err)
	}
	return cn, nil
}

// DecodeAssociateResponse parses the responder's answer to
// EncodeAssociateRequest. An accepted association (Session ACCEPT →
// Presentation CPA → ACSE AARE accepted) returns the MMS payload. A
// refusal — Session REFUSE with a CPR, or an AARE with a rejecting
// result — returns a *Rejection error carrying the reasons and the
// Initiate-ErrorPDU, if any.
//
// The ACCEPT must select session version 2 (the only one proposed) and
// keep the duplex functional unit when it states them (ISO 8327-1;
// ISO 9506-2 24.7); the CPA must accept both proposed contexts with BER
// (ISO 8823-1); an AARE application-context-name, when present, must be
// the MMS context (ISO 9506-2 Annex A.7).
func DecodeAssociateResponse(data []byte) (*AssociateResult, error) {
	spdu, err := session.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("isostack: session: %w", err)
	}
	switch spdu.Type {
	case session.SpduAccept:
	case session.SpduRefuse:
		return nil, decodeRefuse(spdu)
	default:
		return nil, fmt.Errorf("isostack: expected Session ACCEPT, got %s", spdu.Type)
	}
	if spdu.Version != 0 && spdu.Version != session.Version2 {
		return nil, fmt.Errorf("isostack: Session ACCEPT selected version %#x, proposed version 2", spdu.Version)
	}
	if spdu.HasRequirements && spdu.Requirements&session.FUDuplex == 0 {
		return nil, fmt.Errorf("isostack: Session ACCEPT without the duplex functional unit (requirements %#04x)", spdu.Requirements)
	}

	ppdu, err := presentation.Parse(spdu.UserData)
	if err != nil {
		return nil, fmt.Errorf("isostack: presentation: %w", err)
	}
	if ppdu.Kind != presentation.PpduCPA {
		return nil, fmt.Errorf("isostack: expected Presentation CPA, got %s", ppdu.Kind)
	}
	if err := checkContextResults(ppdu); err != nil {
		return nil, err
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
	if apdu.AARE.Result != acse.ResultAccepted {
		// AC + CPA + AARE-reject, as libiec61850-style responders and
		// earlier go-mms servers send it.
		r := newRejection()
		fillAARE(r, apdu.AARE)
		return nil, r
	}
	if apdu.AARE.AppContext != nil && !bytes.Equal(apdu.AARE.AppContext, acse.MMSApplicationContext) {
		return nil, fmt.Errorf("isostack: AARE application-context-name %x is not MMS (1.0.9506.2.3)", apdu.AARE.AppContext)
	}

	return &AssociateResult{
		ACSEResult:    apdu.AARE.Result,
		MmsPayload:    apdu.AARE.UserData,
		ResponderAuth: apdu.AARE.ResponderAuth,
	}, nil
}

// checkContextResults verifies the CPA result list: one entry per
// proposed context (ACSE, MMS), both accepted. A responder that omits
// the list is tolerated.
func checkContextResults(p *presentation.ParsedPPDU) error {
	if !p.HasResults {
		return nil
	}
	if len(p.Results) != 2 {
		return fmt.Errorf("isostack: CPA has %d context results for 2 proposed contexts", len(p.Results))
	}
	for i, r := range p.Results {
		if r.Result != presentation.ResultAcceptance {
			name := [...]string{"ACSE", "MMS"}[i]
			return fmt.Errorf("isostack: CPA rejected the %s presentation context (result %d, provider-reason %d)", name, r.Result, r.ProviderReason)
		}
	}
	return nil
}

func fillAARE(r *Rejection, a *acse.ParsedAARE) {
	r.ACSEResult = a.Result
	if a.DiagSource != 0 {
		r.DiagSource = a.DiagSource
		r.Diagnostic = a.Diagnostic
	}
	r.MmsPayload = a.UserData
}

// decodeRefuse extracts what a Session REFUSE says: the reason code and,
// for a rejection by the SS-user, the CPR and the AARE inside it. A part
// that cannot be decoded is left out; the refusal itself stands.
func decodeRefuse(spdu *session.ParsedSpdu) error {
	r := newRejection()
	if spdu.HasReason {
		r.SessionReason = int(spdu.Reason)
	}
	if len(spdu.UserData) == 0 {
		return r
	}
	ppdu, err := presentation.Parse(spdu.UserData)
	if err != nil || ppdu.Kind != presentation.PpduCPR {
		return r
	}
	r.ProviderReason = ppdu.ProviderReason
	if len(ppdu.UserData) == 0 {
		return r
	}
	if apdu, err := acse.Parse(ppdu.UserData); err == nil && apdu.Type == acse.ApduAARE {
		fillAARE(r, apdu.AARE)
	}
	return r
}

// EncodeDataRequest wraps an MMS PDU in presentation user-data on the
// default MMS context inside a session DATA SPDU.
func EncodeDataRequest(mmsPayload []byte) []byte {
	return EncodeData(DefaultContexts, mmsPayload)
}

// EncodeData wraps an MMS PDU in presentation user-data on the
// association's MMS context inside a session DATA SPDU.
func EncodeData(c Contexts, mmsPayload []byte) []byte {
	pres := presentation.EncodeUserData(c.MMS, mmsPayload)
	return session.EncodeData(pres)
}

// ErrPeerReleased reports that the peer ended the session (FINISH or
// ABORT) while data was expected.
var ErrPeerReleased = errors.New("isostack: session ended by peer")

// DecodeDataResponse parses session DATA → presentation user-data on the
// default MMS context and returns the inner MMS PDU payload.
func DecodeDataResponse(data []byte) ([]byte, error) {
	return DecodeData(DefaultContexts, data)
}

// DecodeData parses session DATA → presentation user-data on the
// association's MMS context and returns the inner MMS PDU payload.
func DecodeData(c Contexts, data []byte) ([]byte, error) {
	spdu, err := session.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("isostack: session: %w", err)
	}

	switch spdu.Type {
	case session.SpduData:
		// Normal data transfer
	case session.SpduFinish:
		return nil, fmt.Errorf("%w: received Session FINISH (server-initiated release)", ErrPeerReleased)
	case session.SpduAbort, session.SpduAbortAccept:
		return nil, fmt.Errorf("%w: received Session ABORT (%s)", ErrPeerReleased, DescribeAbort(spdu.UserData))
	default:
		return nil, fmt.Errorf("isostack: unexpected SPDU type %s during data transfer", spdu.Type)
	}

	ppdu, err := presentation.Parse(spdu.UserData)
	if err != nil {
		return nil, fmt.Errorf("isostack: presentation: %w", err)
	}
	if ppdu.ContextID != c.MMS {
		return nil, fmt.Errorf("isostack: expected MMS context (%d) in data response, got context %d",
			c.MMS, ppdu.ContextID)
	}

	return ppdu.UserData, nil
}

// DescribeAbort names the source of an abort from the ABORT SPDU user
// data: an ARU carrying an ACSE ABRT, or an ARP (ISO 8823-1, ISO
// 8650-1). It never fails; undecodable data is described as such.
func DescribeAbort(userData []byte) string {
	if len(userData) == 0 {
		return "no user data"
	}
	p, err := presentation.ParseAbort(userData)
	if err != nil {
		return "undecodable abort PPDU"
	}
	if p.Kind == presentation.PpduARP {
		return fmt.Sprintf("presentation provider abort, reason %d", p.ProviderReason)
	}
	apdu, err := acse.Parse(p.UserData)
	if err != nil || apdu.Type != acse.ApduABRT {
		return "user abort"
	}
	src := "acse-service-user"
	if apdu.AbortSource == acse.AbortSourceProvider {
		src = "acse-service-provider"
	}
	return "ABRT source " + src
}

// EncodeReleaseRequest builds a release request: Session FINISH wrapping
// Presentation user-data (ACSE context) wrapping ACSE RLRQ
// (ISO 9506-2 Annex A.1.2.1: A-RELEASE after Conclude).
func EncodeReleaseRequest() []byte {
	return slices.Clone(releaseRequest)
}

var releaseRequest = mustSPDU(session.EncodeFinish(
	presentation.EncodeUserData(presentation.ContextIDACSE, acse.EncodeRLRQ())))

// DecodeReleaseResponse checks the answer to EncodeReleaseRequest: a
// Session DISCONNECT carrying an ACSE RLRE on the ACSE context. It
// returns the SPDU type with an error when the data is something else.
func DecodeReleaseResponse(data []byte) (session.SpduType, error) {
	spdu, err := session.Parse(data)
	if err != nil {
		return 0, fmt.Errorf("isostack: session: %w", err)
	}
	if spdu.Type != session.SpduDisconnect {
		return spdu.Type, fmt.Errorf("isostack: expected Session DISCONNECT, got %s", spdu.Type)
	}
	if err := expectACSE(spdu.UserData, DefaultContexts.ACSE, acse.ApduRLRE); err != nil {
		return spdu.Type, err
	}
	return spdu.Type, nil
}

// expectACSE checks that userData is a presentation user-data PPDU on
// the ACSE context carrying an ACSE APDU of type want.
func expectACSE(userData []byte, acseCtx int, want acse.ApduType) error {
	ppdu, err := presentation.Parse(userData)
	if err != nil {
		return fmt.Errorf("isostack: presentation: %w", err)
	}
	if ppdu.Kind != presentation.PpduUserData || ppdu.ContextID != acseCtx {
		return fmt.Errorf("isostack: expected user data on ACSE context %d, got %s on context %d", acseCtx, ppdu.Kind, ppdu.ContextID)
	}
	apdu, err := acse.Parse(ppdu.UserData)
	if err != nil {
		return fmt.Errorf("isostack: acse: %w", err)
	}
	if apdu.Type != want {
		return fmt.Errorf("isostack: expected ACSE %s, got %s", want, apdu.Type)
	}
	return nil
}

// EncodeAbort builds a user abort: Session ABORT wrapping a Presentation
// ARU-PPDU (ACSE context) wrapping an ACSE ABRT with source
// acse-service-user (ISO 9506-2 Annex A.3: M-U-ABORT maps to
// A-U-ABORT; ISO 8823-1 ARU-PPDU).
func EncodeAbort() []byte {
	return slices.Clone(abortRequest)
}

// EncodeAbortOn builds the same user abort on the association's ACSE
// context.
func EncodeAbortOn(c Contexts) []byte {
	if c.ACSE == presentation.ContextIDACSE {
		return EncodeAbort()
	}
	return mustSPDU(session.EncodeAbort(
		presentation.EncodeARU(c.ACSE, acse.EncodeABRT(acse.AbortSourceUser))))
}

var abortRequest = mustSPDU(session.EncodeAbort(
	presentation.EncodeARU(presentation.ContextIDACSE, acse.EncodeABRT(acse.AbortSourceUser))))

// mustSPDU is for SPDUs built from constant or bounded content: an
// encoding error there is a programming error that every test run
// catches.
func mustSPDU(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}
