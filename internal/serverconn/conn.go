// SPDX-License-Identifier: MIT

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
	"sync/atomic"
	"time"

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
// Fields carries the optional ServiceError components (ISO 9506-2
// clause 7.4.1).
type ServiceError struct {
	ErrorClass int
	ErrorCode  int
	Fields     codec.ServiceErrorFields
}

// RejectError indicates the handler wants the request answered with a
// RejectPDU carrying the request's invokeID as originalInvokeID
// (ISO 9506-2 clause 8.6). Type is the rejectReason alternative
// (RejectConfirmedRequest for a confirmed request) and Reason its value.
type RejectError struct {
	Type   int
	Reason int
}

func (e *RejectError) Error() string {
	return fmt.Sprintf("reject: type=%d reason=%d", e.Type, e.Reason)
}

// RejectPDU rejectReason alternatives and values used by the server
// (ISO 9506-2 clause 8.6).
const (
	RejectConfirmedRequest = 1
	RejectPDUError         = 5
	RejectCancelRequest    = 6

	// confirmed-requestPDU reasons.
	ReasonOther                = 0
	ReasonUnrecognizedService  = 1
	ReasonInvalidInvokeID      = 3
	ReasonInvalidArgument      = 4
	ReasonMaxRecursionExceeded = 8
	ReasonValueOutOfRange      = 9

	// pdu-error reasons.
	ReasonUnknownPDUType = 0
	ReasonInvalidPDU     = 1

	// cancel-requestPDU reasons.
	ReasonCancelOther           = 0
	ReasonCancelInvalidInvokeID = 1
)

func (e *ServiceError) Error() string {
	return fmt.Sprintf("service error: class=%d code=%d", e.ErrorClass, e.ErrorCode)
}

// Conn manages one MMS association.
type Conn struct {
	sendMu    sync.Mutex
	transport Transport
	logger    *slog.Logger
	handler   ServiceHandler
	mmsOpts   MMSOptions
	contexts  isostack.Contexts

	pendingReq        *isostack.AssociateRequest
	pendingInitReq    *pdu.InitiateRequest
	pendingNegotiated MMSOptions
	pendingNesting    bool
}

// MMSOptions are the negotiated MMS parameters for this connection.
type MMSOptions struct {
	MaxPDUSize                int
	MaxOutstandingCalling     int
	MaxOutstandingCalled      int
	DataStructureNestingLevel int

	// ServicesSupportedCalled is the server's servicesSupportedCalled
	// (IEC 61850-8-1 Table 111). Empty means the Table 111 mandatory
	// services only.
	ServicesSupportedCalled asn1.BitString
	// ParameterCBB is the parameter CBB the server supports; the answer
	// is the proposal AND this (Table 112). Empty means
	// DefaultParameterCBB.
	ParameterCBB asn1.BitString

	// ReleaseTimeout bounds the wait for the client's A-RELEASE (FINISH)
	// after a Conclude-Response. Zero means DefaultReleaseTimeout; a
	// negative value does not wait.
	ReleaseTimeout time.Duration

	// RespondingAPTitle / RespondingAEQualifier are sent in the AARE
	// when the AP-title is non-empty (ISO 8650-1).
	RespondingAPTitle     []int
	RespondingAEQualifier int
}

// New creates a new server connection.
func New(t Transport, logger *slog.Logger, handler ServiceHandler, opts MMSOptions) *Conn {
	return &Conn{
		transport: t,
		logger:    logger,
		handler:   handler,
		mmsOpts:   opts,
		contexts:  isostack.DefaultContexts,
	}
}

// Serve runs the request/response loop until the client disconnects
// or an unrecoverable error occurs.
func (c *Conn) Serve(ctx context.Context) error {
	for {
		data, err := c.transport.Receive(ctx)
		if err != nil {
			return fmt.Errorf("serverconn: receive: %w", err)
		}

		mmsPayload, err := isostack.DecodeData(c.contexts, data)
		if err != nil {
			if isFinish(data) {
				return c.handleUnexpectedRelease(ctx)
			}
			return fmt.Errorf("serverconn: decode data: %w", err)
		}

		if err := c.dispatchPDU(ctx, mmsPayload); err != nil {
			if errors.Is(err, errConcluded) {
				return nil
			}
			return err
		}
	}
}

// errConcluded ends Serve after a Conclude exchange.
var errConcluded = errors.New("serverconn: concluded")

// dispatchPDU handles one MMS PDU received after the association is
// established. A PDU that cannot be processed is answered with a RejectPDU
// (ISO 9506-2 clause 8.6) instead of being dropped, so the peer's request
// does not wait for its timeout; the association continues.
func (c *Conn) dispatchPDU(ctx context.Context, mmsPayload []byte) error {
	if c.mmsOpts.MaxPDUSize > 0 && len(mmsPayload) > c.mmsOpts.MaxPDUSize {
		c.logger.Warn("mms: PDU exceeds negotiated size", "size", len(mmsPayload), "max", c.mmsOpts.MaxPDUSize)
		return c.rejectOversized(ctx, mmsPayload)
	}

	kind, content, err := pdu.DecodePdu(mmsPayload)
	if err != nil {
		c.logger.Warn("mms: malformed PDU", "error", err)
		reason := ReasonInvalidPDU
		if errors.Is(err, pdu.ErrUnknownPduType) {
			reason = ReasonUnknownPDUType
		}
		return c.sendData(ctx, codec.MarshalRejectPDUNoInvokeID(RejectPDUError, reason))
	}

	switch kind {
	case pdu.PduConcludeRequest:
		if err := c.handleConclude(ctx); err != nil {
			return err
		}
		return errConcluded
	case pdu.PduConfirmedRequest:
		return c.handleConfirmedRequest(ctx, content)
	case pdu.PduCancelRequest:
		return c.handleCancelRequest(ctx, content)
	default:
		c.logger.Warn("mms: unexpected PDU kind", "kind", kind)
		return nil
	}
}

// rejectOversized answers a PDU larger than the negotiated
// localDetailCalled. For a Confirmed-Request whose invokeID can be read,
// IEC 61850-8-1 clause 8.1.3.5 (Write) and 8.1.3.6.1 (DefineNamedVariableList)
// require Reject confirmed-requestPDU value-out-of-range (ISO 9506-2
// clause 8.6); otherwise Reject pdu-error invalid-pdu without an invokeID.
func (c *Conn) rejectOversized(ctx context.Context, mmsPayload []byte) error {
	if kind, content, err := pdu.DecodePdu(mmsPayload); err == nil && kind == pdu.PduConfirmedRequest {
		invokeID, _, err := codec.UnmarshalConfirmedRequest(content)
		if envErr, ok := errors.AsType[*codec.EnvelopeError](err); ok && envErr.HasInvokeID {
			invokeID, err = envErr.InvokeID, nil
		}
		if err == nil {
			return c.sendData(ctx, codec.MarshalRejectPDU(invokeID, RejectConfirmedRequest, ReasonValueOutOfRange))
		}
	}
	return c.sendData(ctx, codec.MarshalRejectPDUNoInvokeID(RejectPDUError, ReasonInvalidPDU))
}

func (c *Conn) handleConfirmedRequest(ctx context.Context, content []byte) error {
	invokeID, serviceRaw, err := codec.UnmarshalConfirmedRequest(content)
	if err != nil {
		c.logger.Warn("mms: malformed confirmed request", "error", err)
		return c.rejectEnvelope(ctx, err)
	}

	respTag, constructed, respPayload, err := c.handler(ctx, invokeID, serviceRaw.Tag, serviceRaw.Bytes)
	if err != nil {
		if rejErr, ok := errors.AsType[*RejectError](err); ok {
			return c.sendData(ctx, codec.MarshalRejectPDU(invokeID, rejErr.Type, rejErr.Reason))
		}
		if svcErr, ok := errors.AsType[*ServiceError](err); ok {
			return c.sendData(ctx, codec.MarshalConfirmedErrorFields(invokeID, svcErr.ErrorClass, svcErr.ErrorCode, svcErr.Fields))
		}
		return c.sendError(ctx, invokeID, 7, 1) // access: object-access-unsupported
	}

	respPdu, err := codec.MarshalConfirmedResponse(invokeID, respTag, constructed, respPayload)
	if err != nil {
		return fmt.Errorf("serverconn: marshal response: %w", err)
	}

	// A response that does not fit the negotiated PDU size is answered
	// with ServiceError service pdu-size (IEC 61850-8-1 clauses
	// 8.1.3.4.3.2, 8.1.3.4.3.5 and Table 17: value 3 of ISO 9506:1990).
	if c.mmsOpts.MaxPDUSize > 0 && len(respPdu) > c.mmsOpts.MaxPDUSize {
		c.logger.Warn("mms: response exceeds negotiated PDU size",
			"invoke_id", invokeID, "size", len(respPdu), "max", c.mmsOpts.MaxPDUSize)
		return c.sendError(ctx, invokeID, ErrorClassService, ErrorCodePDUSize)
	}

	return c.sendData(ctx, respPdu)
}

// rejectEnvelope answers a Confirmed-Request whose envelope could not be
// decoded: with originalInvokeID when it was read (reason
// invalid-argument), without it otherwise (reason invalid-invokeID when
// the invokeID is outside Unsigned32, pdu-error invalid-pdu when it is
// unreadable). ISO 9506-2 clause 8.6.
func (c *Conn) rejectEnvelope(ctx context.Context, err error) error {
	envErr, _ := errors.AsType[*codec.EnvelopeError](err)
	switch {
	case envErr != nil && envErr.HasInvokeID:
		return c.sendData(ctx, codec.MarshalRejectPDU(envErr.InvokeID, RejectConfirmedRequest, ReasonInvalidArgument))
	case envErr != nil && envErr.InvalidInvokeID:
		return c.sendData(ctx, codec.MarshalRejectPDUNoInvokeID(RejectConfirmedRequest, ReasonInvalidInvokeID))
	default:
		return c.sendData(ctx, codec.MarshalRejectPDUNoInvokeID(RejectPDUError, ReasonInvalidPDU))
	}
}

// ServiceError class service (4), code pdu-size (3) (IEC 61850-8-1
// Table 17): sent when a response does not fit the negotiated PDU size.
const (
	ErrorClassService = 4
	ErrorCodePDUSize  = 3
)

// NestingLevel returns the negotiated dataStructureNestingLevel, or the
// locally configured level before the association is accepted.
func (c *Conn) NestingLevel() int {
	return c.mmsOpts.DataStructureNestingLevel
}

// MaxPDUSize returns the negotiated maximum MMS PDU size, or the locally
// configured maximum before the association is accepted.
func (c *Conn) MaxPDUSize() int {
	return c.mmsOpts.MaxPDUSize
}

func (c *Conn) sendError(ctx context.Context, invokeID codec.InvokeID, errorClass, errorCode int) error {
	errPdu := codec.MarshalConfirmedError(invokeID, errorClass, errorCode)
	return c.sendData(ctx, errPdu)
}

func (c *Conn) sendData(ctx context.Context, mmsPdu []byte) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	data := isostack.EncodeData(c.contexts, mmsPdu)
	return c.transport.Send(ctx, data)
}

// SendUnconfirmed sends a pre-encoded MMS unconfirmed PDU (e.g.
// InformationReport) over the connection. Safe for concurrent use with
// the Serve loop.
//
// Cancelling ctx ends the caller's wait, not the write: abandoning a PDU
// half-written would corrupt the stream. A write that has not started when
// ctx is done is skipped; one in progress continues in the background,
// bounded by the deadline of ctx when it has one. A write that fails, or
// that the deadline cuts, leaves the stream unusable, so the transport is
// closed, which ends Serve.
func (c *Conn) SendUnconfirmed(ctx context.Context, mmsPdu []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	const (
		pending int32 = iota
		writing
		abandoned
	)
	var state atomic.Int32
	done := make(chan error, 1)
	go func() {
		c.sendMu.Lock()
		defer c.sendMu.Unlock()
		// A write whose context ended while it waited is skipped: nothing
		// of it is on the wire and the stream stays usable.
		if err := contextEnded(ctx); err != nil {
			done <- err
			return
		}
		if !state.CompareAndSwap(pending, writing) {
			done <- nil
			return
		}
		wctx := context.WithoutCancel(ctx)
		if dl, ok := ctx.Deadline(); ok {
			var cancel context.CancelFunc
			wctx, cancel = context.WithDeadline(wctx, dl)
			defer cancel()
		}
		err := c.transport.Send(wctx, isostack.EncodeData(c.contexts, mmsPdu))
		if err != nil {
			_ = c.transport.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		state.CompareAndSwap(pending, abandoned)
		return ctx.Err()
	}
}

// contextEnded returns ctx.Err(), or context.DeadlineExceeded once the
// deadline of ctx has passed, also before its timer has fired.
func contextEnded(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if dl, ok := ctx.Deadline(); ok && !time.Now().Before(dl) {
		return context.DeadlineExceeded
	}
	return nil
}

// Close closes the transport, ending Serve.
func (c *Conn) Close() error {
	return c.transport.Close()
}

// handleCancelRequest handles CancelRequestPDU. Since requests are
// processed synchronously, there is never an in-flight request to
// cancel — respond with CancelError (invokeID-unknown).
func (c *Conn) handleCancelRequest(ctx context.Context, content []byte) error {
	invokeID, err := berutil.DecodeUnsigned(content)
	if err != nil || invokeID > codec.MaxInvokeID {
		// Cancel-RequestPDU ::= Unsigned32; a malformed one is answered
		// with Reject cancel-requestPDU (ISO 9506-2 clause 8.6).
		c.logger.Warn("mms: malformed cancel request", "error", err)
		reason := ReasonCancelOther
		if err == nil {
			reason = ReasonCancelInvalidInvokeID
		}
		return c.sendData(ctx, codec.MarshalRejectPDUNoInvokeID(RejectCancelRequest, reason))
	}
	cancelErr := codec.MarshalCancelError(codec.InvokeID(invokeID), 10, 1) // cancel: invoke-id-unknown
	return c.sendData(ctx, cancelErr)
}
