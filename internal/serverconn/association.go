// SPDX-License-Identifier: MIT

package serverconn

import (
	"context"
	"encoding/asn1"
	"errors"
	"fmt"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/isostack"
	"github.com/otfabric/go-mms/internal/pdu"
)

// MMS Initiate version this server implements: ISO 9506:2003 major
// version 1 (ISO 9506-2 24.11). IEC 61850-8-1 Ed2 Tables 109/110 print
// "2.1", which an Integer16 cannot carry; every deployed stack sends 1.
const mmsVersion = 1

// ServiceSupportBits is the length of the servicesSupportedCalled bit
// string (ISO 9506-2 ServiceSupportOptions up to cancel(84)). Bits 85
// and above belong to MMS minor version 2+ and shall not be present
// (IEC 61850-8-1 Table 111 c10).
const ServiceSupportBits = 85

// ServiceSupportOptions bit numbers (ISO 9506-2 8.2) used by the server.
// A confirmed service's bit equals its ConfirmedServiceRequest tag.
const (
	ServiceStatus            = 0
	ServiceIdentify          = 2
	ServiceInformationReport = 79
	ServiceConclude          = 83
	ServiceCancel            = 84
)

// DefaultParameterCBB is the parameter CBB the server supports: str1,
// str2, vnam, valt, vlis (ISO 9506-2 ParameterSupportOptions bits 0, 1,
// 2, 3, 7; IEC 61850-8-1 Table 112). The c4 bits (cei and the
// 9506-1 Annex options) shall not be present.
var DefaultParameterCBB = asn1.BitString{Bytes: []byte{0xf1, 0x00}, BitLength: 11}

// ServiceBits builds a servicesSupportedCalled bit string of
// ServiceSupportBits bits with the given service bits set.
func ServiceBits(services ...int) asn1.BitString {
	b := make([]byte, (ServiceSupportBits+7)/8)
	for _, s := range services {
		if s >= 0 && s < ServiceSupportBits {
			b[s/8] |= 0x80 >> (s % 8)
		}
	}
	return asn1.BitString{Bytes: b, BitLength: ServiceSupportBits}
}

// mandatoryServices are the server's mandatory services in IEC 61850-8-1
// Table 111 (status, identify, conclude, cancel). They are advertised
// when MMSOptions.ServicesSupportedCalled is empty.
var mandatoryServices = ServiceBits(ServiceStatus, ServiceIdentify, ServiceConclude, ServiceCancel)

// maskBits returns proposed AND supported over supported's length (bits
// the proposal does not carry are unproposed).
func maskBits(proposed, supported asn1.BitString) asn1.BitString {
	n := supported.BitLength
	out := make([]byte, (n+7)/8)
	for i := range out {
		if i < len(proposed.Bytes) && i < len(supported.Bytes) {
			out[i] = proposed.Bytes[i] & supported.Bytes[i]
		}
	}
	if pl := proposed.BitLength; pl < n {
		for bit := pl; bit < n; bit++ {
			out[bit/8] &^= 0x80 >> (bit % 8)
		}
	}
	if n%8 != 0 {
		out[len(out)-1] &= 0xff << (8 - n%8)
	}
	return asn1.BitString{Bytes: out, BitLength: n}
}

// InitiateError reports an Initiate-Request whose parameters the server
// cannot accept (IEC 61850-8-1 Table 13). The association was refused
// with an Initiate-ErrorPDU of class initiate and this code.
type InitiateError struct {
	Code   int
	Reason string
}

func (e *InitiateError) Error() string {
	return fmt.Sprintf("serverconn: initiate error %d: %s", e.Code, e.Reason)
}

// ErrRefused reports that ReceiveAssociation answered the request with a
// refusal before returning.
var ErrRefused = errors.New("serverconn: association refused")

// ReceiveAssociation reads the client's CONNECT, validates the
// session/presentation/ACSE layers and the MMS Initiate-Request and
// negotiates the MMS parameters. It returns the ACSE authentication info
// and does NOT send the acceptance: the caller must subsequently call
// AcceptAssociation or RejectAssociation.
//
// A request that cannot be accepted is answered here and an error
// wrapping ErrRefused is returned: the session/presentation/ACSE
// refusals of isostack.DecodeAssociateRequest; an undecodable or
// non-Initiate MMS payload with AARE rejected-permanent,
// acse-service-user no-reason-given; an Initiate-Request whose
// parameters cannot be negotiated with an Initiate-ErrorPDU (see
// negotiate). Only a frame that is not a session CONNECT at all goes
// unanswered.
func (c *Conn) ReceiveAssociation(ctx context.Context) (acse.AuthInfo, error) {
	var noAuth acse.AuthInfo

	data, err := c.transport.Receive(ctx)
	if err != nil {
		return noAuth, fmt.Errorf("serverconn: receive association: %w", err)
	}

	assocReq, err := isostack.DecodeAssociateRequest(data)
	if err != nil {
		if re, ok := errors.AsType[*isostack.RefusedError](err); ok {
			return noAuth, c.sendRefusal(ctx, re.Response, err)
		}
		return noAuth, fmt.Errorf("serverconn: %w", err)
	}

	kind, initContent, err := pdu.DecodePdu(assocReq.MmsPayload)
	if err == nil && kind != pdu.PduInitiateRequest {
		err = fmt.Errorf("expected InitiateRequest, got %s", kind)
	}
	var initReq *pdu.InitiateRequest
	if err == nil {
		initReq, err = pdu.UnmarshalInitiateRequest(initContent)
	}
	if err != nil {
		resp, rerr := assocReq.EncodeReject(isostack.Reject{
			Result: acse.ResultRejectedPerm, DiagSource: acse.DiagSourceUser, Diagnostic: acse.DiagUserNoReasonGiven,
		})
		return noAuth, c.sendRefusal(ctx, resp, errors.Join(fmt.Errorf("serverconn: decode MMS initiate: %w", err), rerr))
	}

	hasLocalDetail, hasNesting := pdu.InitiatePresence(initContent)
	negotiated, ierr := c.negotiate(*initReq, hasLocalDetail, hasNesting)
	if ierr != nil {
		resp, rerr := assocReq.EncodeReject(isostack.Reject{
			Result: acse.ResultRejectedPerm, DiagSource: acse.DiagSourceUser, Diagnostic: acse.DiagUserNoReasonGiven,
			MmsPayload: pdu.MarshalInitiateError(pdu.ErrorClassInitiate, ierr.Code),
		})
		return noAuth, c.sendRefusal(ctx, resp, errors.Join(ierr, rerr))
	}

	c.pendingReq = assocReq
	c.pendingInitReq = initReq
	c.pendingNegotiated = negotiated
	c.pendingNesting = hasNesting
	return assocReq.Auth, nil
}

// sendRefusal sends resp (when non-nil) and returns cause wrapped with
// ErrRefused.
func (c *Conn) sendRefusal(ctx context.Context, resp []byte, cause error) error {
	err := fmt.Errorf("%w: %w", ErrRefused, cause)
	if resp == nil {
		return err
	}
	if serr := c.transport.Send(ctx, resp); serr != nil {
		return errors.Join(err, fmt.Errorf("serverconn: send refusal: %w", serr))
	}
	return err
}

// AcceptAssociation sends the acceptance (Session ACCEPT, CPA, AARE
// accepted) with the Initiate-ResponsePDU carrying the negotiated
// parameters and the server's own servicesSupportedCalled and
// parameter CBB (IEC 61850-8-1 8.1.3.4.1, Tables 111/112). Must be called
// after ReceiveAssociation.
func (c *Conn) AcceptAssociation(ctx context.Context) error {
	if c.pendingInitReq == nil || c.pendingReq == nil {
		return fmt.Errorf("serverconn: no pending association to accept")
	}
	req, negotiated := c.pendingReq, c.pendingNegotiated
	nesting := 0
	if c.pendingNesting {
		nesting = negotiated.DataStructureNestingLevel
	}
	c.clearPending()

	initResp := pdu.InitiateResponse{
		LocalDetailCalled:                  negotiated.MaxPDUSize,
		NegotiatedMaxServOutstandingCall:   negotiated.MaxOutstandingCalling,
		NegotiatedMaxServOutstandingCalled: negotiated.MaxOutstandingCalled,
		// dataStructureNestingLevel is OPTIONAL in both PDUs (ISO 9506-2
		// 8.2); it is answered only when proposed.
		NegotiatedDataStructureNesting: nesting,
		InitResponseDetail: pdu.InitResponseDetail{
			NegotiatedVersion:       mmsVersion,
			NegotiatedParamCBB:      negotiated.ParameterCBB,
			ServicesSupportedCalled: negotiated.ServicesSupportedCalled,
		},
	}

	initRespBytes, err := codec.MarshalMmsPduBareSequence(asn1util.TagInitiateResponse, initResp)
	if err != nil {
		return fmt.Errorf("serverconn: marshal initiate response: %w", err)
	}

	resp, err := req.EncodeAssociateResponse(isostack.ResponseParams{
		RespondingAPTitle:     c.mmsOpts.RespondingAPTitle,
		RespondingAEQualifier: c.mmsOpts.RespondingAEQualifier,
	}, initRespBytes)
	if err != nil {
		return fmt.Errorf("serverconn: %w", err)
	}
	if err := c.transport.Send(ctx, resp); err != nil {
		return fmt.Errorf("serverconn: send association response: %w", err)
	}

	c.mmsOpts = negotiated
	c.contexts = req.Contexts

	c.logger.Info("mms: association accepted",
		"max_pdu_size", negotiated.MaxPDUSize,
		"max_outstanding_calling", negotiated.MaxOutstandingCalling,
		"acse_context", req.Contexts.ACSE,
		"mms_context", req.Contexts.MMS,
	)

	return nil
}

// RejectAssociation refuses the pending association: Session REFUSE →
// CPR → AARE rejected-permanent with result-source-diagnostic
// acse-service-user diagnostic (ISO 8650-1; 13 authentication-failure,
// 14 authentication-required, 11 authentication-mechanism-name-not-
// recognized) → Initiate-ErrorPDU access object-access-denied
// (ISO 9506-2 8.2; IEC 61850-8-1 Table 13 maps an access-denied
// association to ACSI access-violation).
func (c *Conn) RejectAssociation(ctx context.Context, diagnostic int) error {
	req := c.pendingReq
	c.clearPending()
	if req == nil {
		return fmt.Errorf("serverconn: no pending association to reject")
	}
	resp, err := req.EncodeReject(isostack.Reject{
		Result:     acse.ResultRejectedPerm,
		DiagSource: acse.DiagSourceUser,
		Diagnostic: diagnostic,
		MmsPayload: pdu.MarshalInitiateError(pdu.ErrorClassAccess, pdu.AccessErrObjectAccessDenied),
	})
	if err != nil {
		return fmt.Errorf("serverconn: %w", err)
	}
	return c.transport.Send(ctx, resp)
}

func (c *Conn) clearPending() {
	c.pendingReq = nil
	c.pendingInitReq = nil
	c.pendingNegotiated = MMSOptions{}
	c.pendingNesting = false
}

// minLocalDetailCalling is the smallest localDetailCalling the server
// accepts. It leaves room for every answer whose size is fixed: the
// largest is a FileOpen-Response (at most 32 octets of service payload,
// 43 with the confirmed-response framing), and a FileRead-Response needs
// 24 octets of framing around its first octet of file data. Answers whose
// size the application or the peer chooses (names, values) are answered
// with ServiceError service pdu-size when they do not fit.
const minLocalDetailCalling = 64

// negotiate answers an Initiate-Request (ISO 9506-2 8.2, IEC 61850-8-1
// Tables 109/110/13):
//   - negotiated values never exceed the proposal; there is no upward
//     clamp;
//   - an absent localDetailCalling or dataStructureNestingLevel is not a
//     proposal of 0: the server's own value is used;
//   - proposedVersion below 1 is refused with version-incompatible (1);
//     the negotiated version is 1;
//   - a proposed max-services-outstanding of 0 is refused with
//     max-services-outstanding-calling/called-insufficient (3/4), a
//     localDetailCalling below minLocalDetailCalling with
//     max-segment-insufficient (2);
//   - the parameter CBB is proposed AND supported. A proposal missing
//     bits the server needs is not refused (parameter-CBB-insufficient
//     is not sent): the services that need them fail individually.
func (c *Conn) negotiate(req pdu.InitiateRequest, hasLocalDetail, hasNesting bool) (MMSOptions, *InitiateError) {
	switch {
	case req.InitRequestDetail.ProposedVersion < mmsVersion:
		return MMSOptions{}, &InitiateError{Code: pdu.InitiateErrVersionIncompatible,
			Reason: fmt.Sprintf("proposed version %d", req.InitRequestDetail.ProposedVersion)}
	case req.ProposedMaxServOutstandingCall < 1:
		return MMSOptions{}, &InitiateError{Code: pdu.InitiateErrMaxOutstandingCallingInsuff,
			Reason: fmt.Sprintf("proposed max-services-outstanding-calling %d", req.ProposedMaxServOutstandingCall)}
	case req.ProposedMaxServOutstandingCalled < 1:
		return MMSOptions{}, &InitiateError{Code: pdu.InitiateErrMaxOutstandingCalledInsuff,
			Reason: fmt.Sprintf("proposed max-services-outstanding-called %d", req.ProposedMaxServOutstandingCalled)}
	case hasLocalDetail && req.LocalDetailCalling < minLocalDetailCalling:
		return MMSOptions{}, &InitiateError{Code: pdu.InitiateErrMaxSegmentInsufficient,
			Reason: fmt.Sprintf("localDetailCalling %d", req.LocalDetailCalling)}
	}

	out := c.mmsOpts
	if hasLocalDetail {
		out.MaxPDUSize = min(c.mmsOpts.MaxPDUSize, req.LocalDetailCalling)
	}
	out.MaxOutstandingCalling = min(c.mmsOpts.MaxOutstandingCalling, req.ProposedMaxServOutstandingCall)
	out.MaxOutstandingCalled = min(c.mmsOpts.MaxOutstandingCalled, req.ProposedMaxServOutstandingCalled)
	if hasNesting {
		out.DataStructureNestingLevel = min(c.mmsOpts.DataStructureNestingLevel, max(req.ProposedDataStructureNesting, 0))
	}
	supported := c.mmsOpts.ParameterCBB
	if supported.BitLength == 0 {
		supported = DefaultParameterCBB
	}
	out.ParameterCBB = maskBits(req.InitRequestDetail.ProposedParamCBB, supported)
	if out.ServicesSupportedCalled.BitLength == 0 {
		out.ServicesSupportedCalled = mandatoryServices
	}
	return out, nil
}
