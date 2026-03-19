// Package serverconn manages per-connection server state for one MMS
// association. It handles the request→dispatch→response pipeline with
// serialized confirmed request handling.
package serverconn

import (
	"context"
	"encoding/asn1"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/isostack"
	"github.com/otfabric/go-mms/internal/pdu"
)

// Transport matches the public mms.Transport interface.
type Transport interface {
	Send(ctx context.Context, data []byte) error
	Receive(ctx context.Context) ([]byte, error)
	Close() error
}

// ServiceHandler is called for each confirmed service request.
// serviceTag is the CHOICE tag number from the request. The handler
// returns the response tag number, whether the response is constructed,
// the response payload, or an error.
type ServiceHandler func(ctx context.Context, invokeID codec.InvokeID, serviceTag int, serviceBody []byte) (respTag int, constructed bool, respPayload []byte, err error)

// ServiceError indicates the handler wants to produce a ConfirmedErrorPDU.
type ServiceError struct {
	ErrorClass int
	ErrorCode  int
}

func (e *ServiceError) Error() string {
	return fmt.Sprintf("service error: class=%d code=%d", e.ErrorClass, e.ErrorCode)
}

// Conn manages one MMS association.
type Conn struct {
	sendMu         sync.Mutex
	transport      Transport
	logger         *slog.Logger
	handler        ServiceHandler
	mmsOpts        MMSOptions
	pendingInitReq *pdu.InitiateRequest
}

// MMSOptions are the negotiated MMS parameters for this connection.
type MMSOptions struct {
	MaxPDUSize                int
	MaxOutstandingCalling     int
	MaxOutstandingCalled      int
	DataStructureNestingLevel int
}

// New creates a new server connection.
func New(t Transport, logger *slog.Logger, handler ServiceHandler, opts MMSOptions) *Conn {
	return &Conn{
		transport: t,
		logger:    logger,
		handler:   handler,
		mmsOpts:   opts,
	}
}

// ReceiveAssociation reads the client's CONNECT, validates the MMS
// Initiate Request, and returns the ACSE authentication info. It does
// NOT send the AARE — the caller must subsequently call AcceptAssociation
// or RejectAssociation.
func (c *Conn) ReceiveAssociation(ctx context.Context) (acse.AuthInfo, error) {
	var noAuth acse.AuthInfo

	data, err := c.transport.Receive(ctx)
	if err != nil {
		return noAuth, fmt.Errorf("serverconn: receive association: %w", err)
	}

	assocReq, err := isostack.DecodeAssociateRequest(data)
	if err != nil {
		return noAuth, fmt.Errorf("serverconn: %w", err)
	}

	kind, initContent, err := pdu.DecodePdu(assocReq.MmsPayload)
	if err != nil {
		return noAuth, fmt.Errorf("serverconn: decode MMS initiate: %w", err)
	}
	if kind != pdu.PduInitiateRequest {
		return noAuth, fmt.Errorf("serverconn: expected InitiateRequest, got %s", kind)
	}

	var initReq pdu.InitiateRequest
	rest, err := asn1.Unmarshal(initContent, &initReq)
	if err != nil {
		return noAuth, fmt.Errorf("serverconn: unmarshal initiate request: %w", err)
	}
	if len(rest) != 0 {
		return noAuth, fmt.Errorf("serverconn: initiate request: %d trailing bytes", len(rest))
	}

	c.pendingInitReq = &initReq
	return assocReq.Auth, nil
}

// AcceptAssociation sends the AARE with result=accepted and the negotiated
// MMS parameters. Must be called after ReceiveAssociation.
func (c *Conn) AcceptAssociation(ctx context.Context) error {
	if c.pendingInitReq == nil {
		return fmt.Errorf("serverconn: no pending association to accept")
	}
	initReq := c.pendingInitReq
	c.pendingInitReq = nil

	negotiated := c.negotiate(*initReq)

	initResp := pdu.InitiateResponse{
		LocalDetailCalled:                  negotiated.MaxPDUSize,
		NegotiatedMaxServOutstandingCall:   negotiated.MaxOutstandingCalling,
		NegotiatedMaxServOutstandingCalled: negotiated.MaxOutstandingCalled,
		NegotiatedDataStructureNesting:     negotiated.DataStructureNestingLevel,
		InitResponseDetail: pdu.InitResponseDetail{
			NegotiatedVersion:       1,
			NegotiatedParamCBB:      initReq.InitRequestDetail.ProposedParamCBB,
			ServicesSupportedCalled: initReq.InitRequestDetail.ServicesSupportedCalling,
		},
	}

	initRespBytes, err := codec.MarshalMmsPdu(asn1util.TagInitiateResponse, initResp)
	if err != nil {
		return fmt.Errorf("serverconn: marshal initiate response: %w", err)
	}

	resp := isostack.EncodeAssociateResponse(initRespBytes)
	if err := c.transport.Send(ctx, resp); err != nil {
		return fmt.Errorf("serverconn: send association response: %w", err)
	}

	c.mmsOpts = negotiated

	c.logger.Info("serverconn: association accepted",
		"max_pdu_size", negotiated.MaxPDUSize,
		"max_outstanding_calling", negotiated.MaxOutstandingCalling,
	)

	return nil
}

// RejectAssociation sends an AARE with result=rejected-permanent.
func (c *Conn) RejectAssociation(ctx context.Context) error {
	c.pendingInitReq = nil
	resp := isostack.EncodeAssociateReject()
	return c.transport.Send(ctx, resp)
}

// Minimum acceptable negotiation values. If the peer proposes less than
// these, we clamp upward to keep the association usable.
const (
	minPDUSize      = 128
	minOutstanding  = 1
	minNestingLevel = 1
)

func (c *Conn) negotiate(req pdu.InitiateRequest) MMSOptions {
	return MMSOptions{
		MaxPDUSize:                clampMin(min(c.mmsOpts.MaxPDUSize, req.LocalDetailCalling), minPDUSize),
		MaxOutstandingCalling:     clampMin(min(c.mmsOpts.MaxOutstandingCalling, req.ProposedMaxServOutstandingCall), minOutstanding),
		MaxOutstandingCalled:      clampMin(min(c.mmsOpts.MaxOutstandingCalled, req.ProposedMaxServOutstandingCalled), minOutstanding),
		DataStructureNestingLevel: clampMin(min(c.mmsOpts.DataStructureNestingLevel, req.ProposedDataStructureNesting), minNestingLevel),
	}
}

func clampMin(val, minimum int) int {
	if val < minimum {
		return minimum
	}
	return val
}

// Serve runs the request/response loop until the client disconnects
// or an unrecoverable error occurs.
func (c *Conn) Serve(ctx context.Context) error {
	for {
		data, err := c.transport.Receive(ctx)
		if err != nil {
			return fmt.Errorf("serverconn: receive: %w", err)
		}

		if c.mmsOpts.MaxPDUSize > 0 && len(data) > c.mmsOpts.MaxPDUSize {
			c.logger.Warn("serverconn: PDU exceeds negotiated size", "size", len(data), "max", c.mmsOpts.MaxPDUSize)
			continue
		}

		mmsPayload, err := isostack.DecodeDataResponse(data)
		if err != nil {
			// Could be FINISH/ABORT
			if rerr := isostack.DecodeReleaseRequest(data); rerr == nil {
				return c.handleRelease(ctx)
			}
			return fmt.Errorf("serverconn: decode data: %w", err)
		}

		kind, content, err := pdu.DecodePdu(mmsPayload)
		if err != nil {
			c.logger.Warn("serverconn: malformed PDU", "error", err)
			continue
		}

		switch kind {
		case pdu.PduConcludeRequest:
			return c.handleConclude(ctx)
		case pdu.PduConfirmedRequest:
			if err := c.handleConfirmedRequest(ctx, content); err != nil {
				return err
			}
		case pdu.PduCancelRequest:
			if err := c.handleCancelRequest(ctx, content); err != nil {
				return err
			}
		default:
			c.logger.Warn("serverconn: unexpected PDU kind", "kind", kind)
		}
	}
}

func (c *Conn) handleConfirmedRequest(ctx context.Context, content []byte) error {
	invokeID, serviceRaw, err := codec.UnmarshalConfirmedRequest(content)
	if err != nil {
		c.logger.Warn("serverconn: malformed confirmed request", "error", err)
		rejectPdu := codec.MarshalRejectPDU(0, 1, 0) // confirmed-request-pdu, other
		return c.sendData(ctx, rejectPdu)
	}

	respTag, constructed, respPayload, err := c.handler(ctx, invokeID, serviceRaw.Tag, serviceRaw.Bytes)
	if err != nil {
		var svcErr *ServiceError
		if errors.As(err, &svcErr) {
			return c.sendError(ctx, invokeID, svcErr.ErrorClass, svcErr.ErrorCode)
		}
		return c.sendError(ctx, invokeID, 7, 1) // access: object-access-denied
	}

	respPdu, err := codec.MarshalConfirmedResponse(invokeID, respTag, constructed, respPayload)
	if err != nil {
		return fmt.Errorf("serverconn: marshal response: %w", err)
	}

	return c.sendData(ctx, respPdu)
}

func (c *Conn) sendError(ctx context.Context, invokeID codec.InvokeID, errorClass, errorCode int) error {
	errPdu := codec.MarshalConfirmedError(invokeID, errorClass, errorCode)
	return c.sendData(ctx, errPdu)
}

func (c *Conn) sendData(ctx context.Context, mmsPdu []byte) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	data := isostack.EncodeDataRequest(mmsPdu)
	return c.transport.Send(ctx, data)
}

// SendUnconfirmed sends a pre-encoded MMS unconfirmed PDU (e.g.
// InformationReport) over the connection. Safe for concurrent use with
// the Serve loop.
func (c *Conn) SendUnconfirmed(ctx context.Context, mmsPdu []byte) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	data := isostack.EncodeDataRequest(mmsPdu)
	return c.transport.Send(ctx, data)
}

func (c *Conn) handleConclude(ctx context.Context) error {
	resp := codec.MarshalConcludeResponse()
	if err := c.sendData(ctx, resp); err != nil {
		return fmt.Errorf("serverconn: send conclude response: %w", err)
	}
	c.logger.Info("serverconn: concluded")
	return nil
}

func (c *Conn) handleRelease(ctx context.Context) error {
	resp := isostack.EncodeReleaseResponse()
	if err := c.transport.Send(ctx, resp); err != nil {
		return fmt.Errorf("serverconn: send release response: %w", err)
	}
	c.logger.Info("serverconn: released")
	return nil
}

// SendAbort sends a protocol-level Abort PDU (Session ABORT / ACSE
// ABRT) to the peer. This is a best-effort send — errors are returned
// but the caller should close the transport regardless.
func (c *Conn) SendAbort(ctx context.Context) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	data := isostack.EncodeAbort()
	return c.transport.Send(ctx, data)
}

// handleCancelRequest handles CancelRequestPDU. Since requests are
// processed synchronously, there is never an in-flight request to
// cancel — respond with CancelError (invokeID-unknown).
func (c *Conn) handleCancelRequest(ctx context.Context, content []byte) error {
	invokeID, err := berutil.DecodeUnsigned(content)
	if err != nil {
		c.logger.Warn("serverconn: malformed cancel request", "error", err)
		return nil
	}
	cancelErr := codec.MarshalCancelError(codec.InvokeID(invokeID), 10, 1) // cancel: invoke-id-unknown
	return c.sendData(ctx, cancelErr)
}
