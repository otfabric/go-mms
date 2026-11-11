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
type ServiceError struct {
	ErrorClass int
	ErrorCode  int
}

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

		if c.mmsOpts.MaxPDUSize > 0 && len(mmsPayload) > c.mmsOpts.MaxPDUSize {
			c.logger.Warn("mms: PDU exceeds negotiated size", "size", len(mmsPayload), "max", c.mmsOpts.MaxPDUSize)
			continue
		}

		kind, content, err := pdu.DecodePdu(mmsPayload)
		if err != nil {
			c.logger.Warn("mms: malformed PDU", "error", err)
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
			c.logger.Warn("mms: unexpected PDU kind", "kind", kind)
		}
	}
}

func (c *Conn) handleConfirmedRequest(ctx context.Context, content []byte) error {
	invokeID, serviceRaw, err := codec.UnmarshalConfirmedRequest(content)
	if err != nil {
		c.logger.Warn("mms: malformed confirmed request", "error", err)
		rejectPdu := codec.MarshalRejectPDU(0, 1, 0) // confirmed-request-pdu, other
		return c.sendData(ctx, rejectPdu)
	}

	respTag, constructed, respPayload, err := c.handler(ctx, invokeID, serviceRaw.Tag, serviceRaw.Bytes)
	if err != nil {
		if svcErr, ok := errors.AsType[*ServiceError](err); ok {
			return c.sendError(ctx, invokeID, svcErr.ErrorClass, svcErr.ErrorCode)
		}
		return c.sendError(ctx, invokeID, 7, 1) // access: object-access-unsupported
	}

	respPdu, err := codec.MarshalConfirmedResponse(invokeID, respTag, constructed, respPayload)
	if err != nil {
		return fmt.Errorf("serverconn: marshal response: %w", err)
	}

	if c.mmsOpts.MaxPDUSize > 0 && len(respPdu) > c.mmsOpts.MaxPDUSize {
		c.logger.Warn("mms: response exceeds negotiated PDU size",
			"invoke_id", invokeID, "size", len(respPdu), "max", c.mmsOpts.MaxPDUSize)
		return c.sendError(ctx, invokeID, errorClassResource, errorCodeOther)
	}

	return c.sendData(ctx, respPdu)
}

// ServiceError class resource (3), code other (0): sent when a response
// does not fit the negotiated PDU size.
const (
	errorClassResource = 3
	errorCodeOther     = 0
)

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
// ctx bounds how long the caller waits, not the write: abandoning a PDU
// half-written would corrupt the stream and end the association. A write
// that has not started when ctx is done is skipped; one in progress
// completes in the background.
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
		if !state.CompareAndSwap(pending, writing) {
			done <- nil
			return
		}
		done <- c.transport.Send(context.WithoutCancel(ctx), isostack.EncodeData(c.contexts, mmsPdu))
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		state.CompareAndSwap(pending, abandoned)
		return ctx.Err()
	}
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
	if err != nil {
		c.logger.Warn("mms: malformed cancel request", "error", err)
		return nil
	}
	cancelErr := codec.MarshalCancelError(codec.InvokeID(invokeID), 10, 1) // cancel: invoke-id-unknown
	return c.sendData(ctx, cancelErr)
}
