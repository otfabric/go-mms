// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"encoding/asn1"
	"fmt"
	"strings"
	"time"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/isostack"
	"github.com/otfabric/go-mms/internal/pdu"
	"github.com/otfabric/go-mms/internal/session"
)

// RejectionError reports an association the server refused: a Session
// REFUSE (ISO 8327-1) with its presentation CPR (ISO 8823-1), or an ACSE
// AARE with result rejected (ISO 8650-1), with the Initiate-ErrorPDU it
// carried (ISO 9506-2 8.2). Fields the refusal did not carry are -1.
//
// It matches [ErrConnectionRejected], and also [ErrAuthenticationFailed]
// when the AARE diagnostic is one of the acse-service-user
// authentication diagnostics (11 to 14). [IsRetryable] reports Transient.
// The decoded Initiate-ErrorPDU is reachable with [errors.As] as a
// *[ServiceError].
type RejectionError struct {
	// SessionReason is the REFUSE Reason Code (ISO 8327-1: 0 no reason,
	// 1 user congestion, 2 rejection by the user with data, 129+ by the
	// session provider).
	SessionReason int
	// ProviderReason is the CPR provider-reason (ISO 8823-1).
	ProviderReason int
	// ACSEResult is the AARE result: 1 rejected-permanent,
	// 2 rejected-transient.
	ACSEResult int
	// DiagnosticSource is 1 (acse-service-user) or 2 (acse-service-
	// provider); Diagnostic is the result-source-diagnostic value.
	DiagnosticSource int
	Diagnostic       int
	// InitiateError is the decoded Initiate-ErrorPDU, nil when absent.
	InitiateError *ServiceError
	// Transient reports a temporary refusal (session congestion,
	// presentation temporary-congestion, ACSE rejected-transient); a
	// later attempt may succeed.
	Transient bool
}

func (e *RejectionError) Error() string {
	var parts []string
	if e.SessionReason >= 0 {
		parts = append(parts, fmt.Sprintf("session reason %d", e.SessionReason))
	}
	if e.ProviderReason >= 0 {
		parts = append(parts, fmt.Sprintf("presentation provider-reason %d", e.ProviderReason))
	}
	if e.ACSEResult >= 0 {
		parts = append(parts, fmt.Sprintf("ACSE result %d", e.ACSEResult))
	}
	if e.DiagnosticSource > 0 {
		src := "acse-service-user"
		if e.DiagnosticSource == acse.DiagSourceProvider {
			src = "acse-service-provider"
		}
		parts = append(parts, fmt.Sprintf("diagnostic %s %d", src, e.Diagnostic))
	}
	if e.InitiateError != nil {
		parts = append(parts, fmt.Sprintf("initiate error class=%s code=%d", e.InitiateError.Class, e.InitiateError.Code))
	}
	return "mms: association rejected: " + strings.Join(parts, ", ")
}

// Unwrap returns the sentinels the rejection matches and the
// Initiate-ErrorPDU.
func (e *RejectionError) Unwrap() []error {
	errs := []error{ErrConnectionRejected}
	if e.DiagnosticSource == acse.DiagSourceUser && e.Diagnostic >= acse.DiagUserAuthMechNameNotRecognized && e.Diagnostic <= acse.DiagUserAuthRequired {
		errs = append(errs, ErrAuthenticationFailed)
	}
	if e.InitiateError != nil {
		errs = append(errs, e.InitiateError)
	}
	return errs
}

// rejectionError converts an isostack rejection.
func rejectionError(r *isostack.Rejection) *RejectionError {
	e := &RejectionError{
		SessionReason:    r.SessionReason,
		ProviderReason:   r.ProviderReason,
		ACSEResult:       r.ACSEResult,
		DiagnosticSource: r.DiagSource,
		Diagnostic:       r.Diagnostic,
		InitiateError:    initiateError(r.MmsPayload),
		Transient:        r.Transient(),
	}
	return e
}

// initiateError decodes an Initiate-ErrorPDU, or returns nil.
func initiateError(mmsPayload []byte) *ServiceError {
	kind, content, err := pdu.DecodePdu(mmsPayload)
	if err == nil && kind == pdu.PduInitiateError {
		if class, code, derr := pdu.DecodeServiceError(content); derr == nil {
			return &ServiceError{Class: ErrorClass(class), Code: code}
		}
	}
	return nil
}

// maxSelectorLen bounds the session and presentation selectors
// (ISO 8327-1 PI 51/52: at most 16 octets; ISO 8823-1 selectors are
// bounded to 16 octets by the ISPs). IEC 61850-8-1 SCL OSI-SSEL/OSI-PSEL
// allow 8 octets.
const maxSelectorLen = 16

// validateISOOptions rejects selectors that no peer accepts before
// anything is sent (MMS-STK-18).
func validateISOOptions(o ISOOptions) error {
	for name, sel := range map[string][]byte{
		"LocalSSelector":  o.LocalSSelector,
		"RemoteSSelector": o.RemoteSSelector,
		"LocalPSelector":  o.LocalPSelector,
		"RemotePSelector": o.RemotePSelector,
	} {
		if len(sel) > maxSelectorLen {
			return invalidArgf("mms: ISOOptions.%s is %d octets, at most %d allowed", name, len(sel), maxSelectorLen)
		}
	}
	return nil
}

// checkNegotiated validates an Initiate-Response against the request
// (ISO 9506-2 8.2; IEC 61850-8-1 Tables 109/110: negotiated values are 1
// or greater and never exceed the proposal) and stores the result:
//
//   - version: 1 <= negotiated <= proposed;
//   - max-services-outstanding calling/called: 1 <= negotiated <=
//     proposed;
//   - dataStructureNestingLevel: <= proposed when present, the proposal
//     when absent;
//   - localDetailCalled is the server's own maximum, not a negotiated
//     value: requests larger than it are refused locally, responses
//     larger than this client's localDetailCalling are invalid, and
//     MaxPDUSize reports the smaller of the two.
func (c *Client) checkNegotiated(req pdu.InitiateRequest, resp *pdu.InitiateResponse, content []byte) error {
	fail := func(format string, args ...any) error {
		return classify(&ProtocolError{Phase: PhaseMMS, Message: fmt.Sprintf(format, args...)}, ErrNegotiationFailed)
	}
	v := resp.InitResponseDetail.NegotiatedVersion
	if v < 1 || v > req.InitRequestDetail.ProposedVersion {
		return fail("negotiated version %d, proposed %d", v, req.InitRequestDetail.ProposedVersion)
	}
	if n := resp.NegotiatedMaxServOutstandingCall; n < 1 || n > req.ProposedMaxServOutstandingCall {
		return fail("negotiated max-services-outstanding-calling %d, proposed %d", n, req.ProposedMaxServOutstandingCall)
	}
	if n := resp.NegotiatedMaxServOutstandingCalled; n < 1 || n > req.ProposedMaxServOutstandingCalled {
		return fail("negotiated max-services-outstanding-called %d, proposed %d", n, req.ProposedMaxServOutstandingCalled)
	}
	hasLocalDetail, hasNesting := pdu.InitiatePresence(content)
	nesting := req.ProposedDataStructureNesting
	if hasNesting {
		if resp.NegotiatedDataStructureNesting > req.ProposedDataStructureNesting {
			return fail("negotiated dataStructureNestingLevel %d, proposed %d", resp.NegotiatedDataStructureNesting, req.ProposedDataStructureNesting)
		}
		nesting = resp.NegotiatedDataStructureNesting
	}
	c.recvLimit = req.LocalDetailCalling
	c.sendLimit = 0
	c.maxPDUSize = req.LocalDetailCalling
	if hasLocalDetail && resp.LocalDetailCalled > 0 {
		c.sendLimit = resp.LocalDetailCalled
		c.maxPDUSize = min(req.LocalDetailCalling, resp.LocalDetailCalled)
	}
	c.maxOutCalling = resp.NegotiatedMaxServOutstandingCall
	c.maxOutCalled = resp.NegotiatedMaxServOutstandingCalled
	c.nestingLevel = nesting
	c.serverVersion = v
	c.servicesSupported = cloneBits(resp.InitResponseDetail.ServicesSupportedCalled)
	c.parameterCBB = cloneBits(resp.InitResponseDetail.NegotiatedParamCBB)

	c.logger.Debug("mms: negotiated",
		"max_pdu_size", c.maxPDUSize,
		"send_limit", c.sendLimit,
		"max_outstanding_calling", c.maxOutCalling,
		"max_outstanding_called", c.maxOutCalled,
		"nesting_level", c.nestingLevel,
		"version", c.serverVersion,
	)
	return nil
}

// defaultReleaseTimeout is used when DialOptions.ReleaseTimeout is zero.
const defaultReleaseTimeout = 2 * time.Second

// release completes the orderly release after a Conclude-Response
// (ISO 9506-2 Annex A.1.2.1): A-RELEASE.request (Session FINISH with an
// ACSE RLRQ) and A-RELEASE.confirm (DISCONNECT with an RLRE), bounded by
// ctx and DialOptions.ReleaseTimeout. The reader loop has exited, so the
// response is read here.
//
// A peer that closes the transport or aborts instead, or that does not
// answer within the timeout, still ends the association: Close succeeds.
// An answer other than DISCONNECT + RLRE is answered with an abort.
func (c *Client) release(ctx context.Context) error {
	timeout := c.opts.ReleaseTimeout
	if timeout < 0 {
		return nil
	}
	if timeout == 0 {
		timeout = defaultReleaseTimeout
	}
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// A stalled peer must not hold Close past the release timeout.
	stop := context.AfterFunc(rctx, func() { _ = c.conn.Close() })
	defer stop()

	c.sendMu.Lock()
	err := c.sendRaw(rctx, isostack.EncodeReleaseRequest())
	c.sendMu.Unlock()
	if err == nil {
		var data []byte
		if data, err = c.receiveRaw(rctx); err == nil {
			var typ session.SpduType
			typ, err = isostack.DecodeReleaseResponse(data)
			switch {
			case err == nil:
				c.logger.Debug("mms: released")
				return nil
			case typ == session.SpduAbort || typ == session.SpduAbortAccept:
				c.logger.Warn("mms: peer aborted the release", "error", err)
				return nil
			}
			c.logger.Warn("mms: invalid release response; aborting", "error", err)
			c.sendMu.Lock()
			_ = c.sendRaw(rctx, isostack.EncodeAbort())
			c.sendMu.Unlock()
			return nil
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("mms: release: %w", ctxErr)
	}
	c.logger.Debug("mms: release not confirmed", "error", err)
	return nil
}

// concludeError builds Close's error for a Conclude-ErrorPDU and aborts
// the association (ISO 9506-2 24.3: a failed Conclude leads to
// M-U-ABORT). content is the ServiceError.
func (c *Client) concludeError(content []byte) error {
	class, code, err := pdu.DecodeServiceError(content)
	if err != nil {
		return &ProtocolError{Phase: PhaseMMS, Message: "undecodable Conclude-ErrorPDU: " + err.Error()}
	}
	return fmt.Errorf("mms: conclude refused: %w", &ServiceError{Class: ErrorClass(class), Code: code})
}

func (c *Client) abortAfterConclude(ctx context.Context) {
	stop := context.AfterFunc(ctx, func() { _ = c.conn.Close() })
	defer stop()
	c.sendMu.Lock()
	_ = c.sendRaw(ctx, isostack.EncodeAbort())
	c.sendMu.Unlock()
}

func cloneBits(b asn1.BitString) asn1.BitString {
	return asn1.BitString{Bytes: append([]byte(nil), b.Bytes...), BitLength: b.BitLength}
}
