package mms

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/invoke"
	"github.com/otfabric/go-mms/internal/isostack"
	"github.com/otfabric/go-mms/internal/pdu"
)

// Client is an MMS client connection to a remote MMS server.
//
// A Client is created via [NewClient] (or the convenience iso.Dial from
// the transport/iso subpackage) and must be closed with [Client.Close]
// when no longer needed.
//
// Close is idempotent: calling it multiple times is safe and subsequent
// calls return nil.
//
// Concurrency: the Client serializes confirmed service sends internally
// via a request mutex (one send at a time). A background reader loop
// dispatches confirmed responses by invoke ID and delivers unconfirmed
// PDUs (InformationReport) to registered handlers.
type Client struct {
	mu     sync.Mutex
	closed bool
	logger *slog.Logger
	opts   DialOptions

	sendMu  sync.Mutex // serializes writes to transport
	conn    Transport
	tracker *invoke.Tracker

	readerCancel context.CancelFunc
	readerDone   chan struct{} // closed when reader loop exits
	concludeCh   chan struct{} // receives signal when ConcludeResponse arrives

	reportMu      sync.RWMutex
	reportHandler InformationReportHandler

	// Negotiated parameters from MMS Initiate handshake.
	maxPDUSize    int
	maxOutCalling int
	maxOutCalled  int
	nestingLevel  int
	serverVersion int
}

// NewClient creates a Client using an already-established [Transport]
// connection. This is the low-level entry point for creating clients
// with custom or pre-established transports.
//
// NewClient performs the full ISO upper-layer association and MMS
// Initiate handshake over the provided transport.
func NewClient(ctx context.Context, conn Transport, opts DialOptions) (*Client, error) {
	return newClient(ctx, conn, opts)
}

func newClient(ctx context.Context, conn Transport, opts DialOptions) (*Client, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(discardHandler{})
	}

	c := &Client{
		logger:     logger,
		opts:       opts,
		conn:       conn,
		readerDone: make(chan struct{}),
		concludeCh: make(chan struct{}, 1),
	}

	if err := c.associate(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}

	c.tracker = invoke.NewTracker(0)

	readerCtx, cancel := context.WithCancel(context.Background())
	c.readerCancel = cancel
	go c.readerLoop(readerCtx)

	logger.Info("mms: connected",
		"max_pdu_size", c.maxPDUSize,
		"max_outstanding_calling", c.maxOutCalling,
		"max_outstanding_called", c.maxOutCalled,
	)

	return c, nil
}

func (c *Client) associate(ctx context.Context) error {
	mmsOpts := c.opts.MMS
	initReq := pdu.DefaultInitiateRequest(
		mmsOpts.MaxPDUSize,
		mmsOpts.MaxOutstandingCalling,
		mmsOpts.MaxOutstandingCalled,
		mmsOpts.DataStructureNestingLevel,
	)

	initBytes, err := pdu.MarshalInitiateRequest(initReq)
	if err != nil {
		return fmt.Errorf("mms: marshal initiate request: %w", err)
	}

	isoParams := c.buildISOParams()
	assocReq, err := isostack.EncodeAssociateRequest(isoParams, initBytes)
	if err != nil {
		return fmt.Errorf("mms: encode association request: %w", err)
	}

	if err := c.sendRaw(ctx, assocReq); err != nil {
		return fmt.Errorf("mms: send association request: %w", err)
	}

	respData, err := c.receiveRaw(ctx)
	if err != nil {
		return fmt.Errorf("mms: receive association response: %w", err)
	}

	assocResult, err := isostack.DecodeAssociateResponse(respData)
	if err != nil {
		return fmt.Errorf("mms: %w", err)
	}

	if assocResult.ACSEResult != acse.ResultAccepted {
		return fmt.Errorf("mms: association rejected (result=%d): %w",
			assocResult.ACSEResult, ErrConnectionRejected)
	}

	if len(assocResult.MmsPayload) == 0 {
		return &ProtocolError{
			Phase:   "mms",
			Message: "association accepted but no MMS Initiate Response",
		}
	}

	kind, content, err := pdu.DecodePdu(assocResult.MmsPayload)
	if err != nil {
		return fmt.Errorf("mms: decode initiate response: %w", err)
	}
	if kind != pdu.PduInitiateResponse {
		return &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("expected InitiateResponse, got %s", kind),
		}
	}

	initResp, err := pdu.UnmarshalInitiateResponse(content)
	if err != nil {
		return fmt.Errorf("mms: %w", err)
	}

	if err := c.applyNegotiatedParams(initReq, initResp); err != nil {
		return err
	}
	return nil
}

// applyNegotiatedParams stores the result of MMS Initiate negotiation.
//
// Negotiation policy (pragmatic interop defaults, not strict spec):
//   - maxPDUSize: server's LocalDetailCalled, falling back to our proposed value
//   - maxOutstanding: min(proposed, negotiated), floor of 1
//   - nestingLevel: server's negotiated value, falling back to our proposed value
//   - version: server's negotiated version, must be > 0
//
// This policy mirrors common MMS client behavior. The server side will
// need its own negotiation logic when implemented in Phase 7.
func (c *Client) applyNegotiatedParams(req pdu.InitiateRequest, resp *pdu.InitiateResponse) error {
	c.maxPDUSize = resp.LocalDetailCalled
	if c.maxPDUSize <= 0 {
		c.maxPDUSize = req.LocalDetailCalling
	}

	c.maxOutCalling = min(req.ProposedMaxServOutstandingCall, resp.NegotiatedMaxServOutstandingCall)
	if c.maxOutCalling <= 0 {
		c.maxOutCalling = 1
	}

	c.maxOutCalled = min(req.ProposedMaxServOutstandingCalled, resp.NegotiatedMaxServOutstandingCalled)
	if c.maxOutCalled <= 0 {
		c.maxOutCalled = 1
	}

	c.nestingLevel = resp.NegotiatedDataStructureNesting
	if c.nestingLevel <= 0 {
		c.nestingLevel = req.ProposedDataStructureNesting
	}

	c.serverVersion = resp.InitResponseDetail.NegotiatedVersion
	if c.serverVersion <= 0 {
		return &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("invalid negotiated version %d", c.serverVersion),
		}
	}

	c.logger.Debug("mms: negotiated",
		"max_pdu_size", c.maxPDUSize,
		"max_outstanding_calling", c.maxOutCalling,
		"max_outstanding_called", c.maxOutCalled,
		"nesting_level", c.nestingLevel,
		"version", c.serverVersion,
	)

	return nil
}

func (c *Client) buildISOParams() isostack.Params {
	iso := c.opts.ISO
	return isostack.Params{
		CallingSessionSelector:      iso.LocalSSelector,
		CalledSessionSelector:       iso.RemoteSSelector,
		CallingPresentationSelector: iso.LocalPSelector,
		CalledPresentationSelector:  iso.RemotePSelector,
		ACSE: acse.AARQParams{
			CalledAPTitle:      iso.RemoteAPTitle,
			CalledAEQualifier:  iso.RemoteAEQualifier,
			CallingAPTitle:     iso.LocalAPTitle,
			CallingAEQualifier: iso.LocalAEQualifier,
			Password:           append([]byte(nil), iso.Password...),
		},
	}
}

// Close performs a hard shutdown of the MMS connection.
//
// Shutdown sequence:
//  1. Marks the client as closed — all new service calls return [ErrClosed].
//  2. Cancels all pending confirmed requests with [ErrClosed].
//  3. Sends a ConcludeRequest and waits for the ConcludeResponse.
//  4. Stops the background reader loop and closes the transport.
//
// This is NOT a graceful drain: pending requests are aborted immediately
// and their callers receive [ErrClosed]. Confirmed responses that arrive
// during the shutdown window (after CancelAll but before the reader
// exits) are discarded as unknown invoke IDs.
//
// Close is idempotent: the first call performs the conclude handshake;
// subsequent calls return nil.
func (c *Client) Close(ctx context.Context) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	c.tracker.CancelAll(ErrClosed)

	err := c.conclude(ctx)

	c.readerCancel()
	<-c.readerDone

	closeErr := c.conn.Close()
	if err == nil {
		err = closeErr
	}

	c.logger.Info("mms: closed")
	return err
}

// Abort performs a hard, immediate association abort without the
// graceful ConcludeRequest/ConcludeResponse exchange. Use this for
// protocol desync recovery, emergency teardown, or test tooling.
//
// After Abort returns, the Client is closed and all pending requests
// are cancelled with [ErrClosed].
func (c *Client) Abort(_ context.Context) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	// Best-effort send of the protocol abort PDU (ACSE ABRT wrapped in
	// session/presentation layers). Ignored if the peer is already gone.
	abortPDU := isostack.EncodeAbort()
	c.sendMu.Lock()
	_ = c.conn.Send(context.Background(), abortPDU)
	c.sendMu.Unlock()

	c.tracker.CancelAll(ErrClosed)
	c.readerCancel()
	<-c.readerDone

	err := c.conn.Close()
	c.logger.Info("mms: aborted")
	return err
}

// Negotiated returns the MMS parameters that were negotiated during
// the association establishment handshake.
func (c *Client) Negotiated() NegotiatedParameters {
	return NegotiatedParameters{
		MaxPDUSize:    c.maxPDUSize,
		MaxOutCalling: c.maxOutCalling,
		MaxOutCalled:  c.maxOutCalled,
		NestingLevel:  c.nestingLevel,
		ServerVersion: c.serverVersion,
	}
}

func (c *Client) conclude(ctx context.Context) error {
	concludeReq := codec.MarshalConcludeRequest()
	data := isostack.EncodeDataRequest(concludeReq)

	c.sendMu.Lock()
	sendErr := c.sendRaw(ctx, data)
	c.sendMu.Unlock()
	if sendErr != nil {
		return fmt.Errorf("mms: send conclude: %w", sendErr)
	}

	select {
	case <-c.concludeCh:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("mms: conclude: %w", ctx.Err())
	case <-c.readerDone:
		// The reader loop signals concludeCh then returns (closing
		// readerDone). When both are ready, select picks randomly.
		// Drain concludeCh to avoid a false-negative race.
		select {
		case <-c.concludeCh:
			return nil
		default:
			return &ProtocolError{Phase: "mms", Message: "connection closed before conclude response"}
		}
	}
}

// Identify sends an MMS Identify request and returns the server's identity.
func (c *Client) Identify(ctx context.Context) (*ServerIdentity, error) {
	invokeID := c.nextInvokeID()

	reqBytes, err := pdu.MarshalIdentifyRequest(invokeID)
	if err != nil {
		return nil, fmt.Errorf("mms: marshal identify request: %w", err)
	}

	confirmed, err := c.sendConfirmed(ctx, invokeID, reqBytes)
	if err != nil {
		return nil, err
	}

	if confirmed.ServiceKind != pdu.ServiceIdentify {
		return nil, &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("expected Identify response, got %s", confirmed.ServiceKind),
		}
	}

	identResp, err := pdu.UnmarshalIdentifyResponse(confirmed.ServiceData)
	if err != nil {
		return nil, fmt.Errorf("mms: %w", err)
	}

	c.logger.Debug("mms: identify",
		"invoke_id", invokeID,
		"service", "Identify",
		"vendor", identResp.VendorName,
		"model", identResp.ModelName,
		"revision", identResp.Revision,
	)

	return &ServerIdentity{
		Vendor:   identResp.VendorName,
		Model:    identResp.ModelName,
		Revision: identResp.Revision,
	}, nil
}

// ClientStatusRequest configures the MMS Status request.
// The zero value requests non-extended derivation (the common default).
type ClientStatusRequest struct {
	// ExtendedDerivation requests extended status derivation from the server.
	ExtendedDerivation bool
}

// Status sends an MMS Status request with default parameters
// (ExtendedDerivation = false) and returns the VMD status.
// Use [Client.StatusWithOptions] to control ExtendedDerivation.
func (c *Client) Status(ctx context.Context) (*ServerStatus, error) {
	return c.StatusWithOptions(ctx, ClientStatusRequest{})
}

// StatusWithOptions sends an MMS Status request with the given options.
func (c *Client) StatusWithOptions(ctx context.Context, req ClientStatusRequest) (*ServerStatus, error) {
	invokeID := c.nextInvokeID()

	reqBytes, err := pdu.MarshalStatusRequest(invokeID, req.ExtendedDerivation)
	if err != nil {
		return nil, fmt.Errorf("mms: marshal status request: %w", err)
	}

	confirmed, err := c.sendConfirmed(ctx, invokeID, reqBytes)
	if err != nil {
		return nil, err
	}

	if confirmed.ServiceKind != pdu.ServiceStatus {
		return nil, &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("expected Status response, got %s", confirmed.ServiceKind),
		}
	}

	statusResp, err := pdu.UnmarshalStatusResponse(confirmed.ServiceData)
	if err != nil {
		return nil, fmt.Errorf("mms: %w", err)
	}

	c.logger.Debug("mms: status",
		"invoke_id", invokeID,
		"service", "Status",
		"logical", statusResp.VMDLogicalStatus,
		"physical", statusResp.VMDPhysicalStatus,
	)

	return &ServerStatus{
		Logical:  VMDLogicalStatus(statusResp.VMDLogicalStatus),
		Physical: VMDPhysicalStatus(statusResp.VMDPhysicalStatus),
	}, nil
}

// sendConfirmed sends an MMS PDU as a confirmed request, waits for the
// response, verifies the invoke ID, and handles ConfirmedError and
// Reject PDUs. Returns the parsed confirmed response on success.
func (c *Client) sendConfirmed(ctx context.Context, invokeID codec.InvokeID, mmsPdu []byte) (*pdu.ConfirmedResponse, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrClosed
	}
	c.mu.Unlock()

	_, ch, err := c.tracker.AllocateWithID(invokeID)
	if err != nil {
		return nil, fmt.Errorf("mms: allocate invoke ID %d: %w", invokeID, err)
	}

	data := isostack.EncodeDataRequest(mmsPdu)

	c.sendMu.Lock()
	sendErr := c.sendRaw(ctx, data)
	c.sendMu.Unlock()
	if sendErr != nil {
		c.tracker.Cancel(invokeID, sendErr)
		return nil, fmt.Errorf("mms: send request: %w", sendErr)
	}

	var resp invoke.Response
	select {
	case resp = <-ch:
	case <-ctx.Done():
		c.tracker.Cancel(invokeID, ctx.Err())
		return nil, fmt.Errorf("mms: %w", ctx.Err())
	}

	if resp.Err != nil {
		return nil, resp.Err
	}

	return c.processConfirmedPDU(invokeID, pdu.PduKind(resp.Kind), resp.Data)
}

func (c *Client) processConfirmedPDU(invokeID codec.InvokeID, kind pdu.PduKind, content []byte) (*pdu.ConfirmedResponse, error) {
	switch kind {
	case pdu.PduConfirmedResponse:
		confirmed, err := pdu.DecodeConfirmedResponse(content)
		if err != nil {
			return nil, fmt.Errorf("mms: %w", err)
		}
		if confirmed.InvokeID != invokeID {
			return nil, &ProtocolError{
				Phase:   "mms",
				Message: fmt.Sprintf("confirmed response invoke ID mismatch: expected %d, got %d", invokeID, confirmed.InvokeID),
			}
		}
		return confirmed, nil

	case pdu.PduConfirmedError:
		return nil, c.handleConfirmedError(invokeID, content)

	case pdu.PduReject:
		return nil, c.handleReject(invokeID, content)

	default:
		return nil, &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("unexpected PDU type %s for confirmed request", kind),
		}
	}
}

func (c *Client) handleConfirmedError(expectedID codec.InvokeID, content []byte) error {
	ce, err := pdu.DecodeConfirmedError(content)
	if err != nil {
		return fmt.Errorf("mms: decode confirmed error: %w", err)
	}
	if ce.InvokeID != expectedID {
		return &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("confirmed error invoke ID mismatch: sent %d, received %d", expectedID, ce.InvokeID),
		}
	}
	return &ServiceError{
		Class:    ErrorClass(ce.ErrorClass),
		Code:     ce.ErrorCode,
		InvokeID: InvokeID(ce.InvokeID),
	}
}

func (c *Client) handleReject(expectedID codec.InvokeID, content []byte) error {
	rj, err := pdu.DecodeRejectPDU(content)
	if err != nil {
		return fmt.Errorf("mms: decode reject: %w", err)
	}
	if rj.HasInvokeID && rj.InvokeID != expectedID {
		return &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("reject invoke ID mismatch: sent %d, received %d", expectedID, rj.InvokeID),
		}
	}

	var msg string
	if rj.HasInvokeID {
		msg = fmt.Sprintf("request rejected: type=%d reason=%d invokeID=%d", rj.RejectType, rj.RejectReason, rj.InvokeID)
	} else {
		msg = fmt.Sprintf("request rejected: type=%d reason=%d invokeID=absent", rj.RejectType, rj.RejectReason)
	}
	return &ProtocolError{Phase: "mms", Message: msg}
}

// OnInformationReport registers a handler for incoming InformationReport
// PDUs from the server. Only one handler may be registered at a time;
// subsequent calls replace the previous handler. Passing nil unregisters
// the handler.
func (c *Client) OnInformationReport(handler InformationReportHandler) {
	c.reportMu.Lock()
	c.reportHandler = handler
	c.reportMu.Unlock()
}

func (c *Client) nextInvokeID() codec.InvokeID {
	return c.tracker.NextID()
}

func (c *Client) readerLoop(ctx context.Context) {
	defer close(c.readerDone)

	for {
		rawData, err := c.receiveRaw(ctx)
		if err != nil {
			c.logger.Debug("mms: reader loop stopped", "error", err)
			c.tracker.CancelAll(fmt.Errorf("mms: connection read: %w", err))
			return
		}

		mmsPayload, err := isostack.DecodeDataResponse(rawData)
		if err != nil {
			c.logger.Debug("mms: reader loop decode error", "error", err)
			c.tracker.CancelAll(fmt.Errorf("mms: %w", err))
			return
		}

		kind, content, err := pdu.DecodePdu(mmsPayload)
		if err != nil {
			c.logger.Debug("mms: reader loop PDU error", "error", err)
			c.tracker.CancelAll(fmt.Errorf("mms: decode PDU: %w", err))
			return
		}

		switch kind {
		case pdu.PduConfirmedResponse:
			c.dispatchConfirmed(kind, content)

		case pdu.PduConfirmedError:
			c.dispatchConfirmed(kind, content)

		case pdu.PduReject:
			c.dispatchConfirmed(kind, content)

		case pdu.PduUnconfirmed:
			c.dispatchUnconfirmed(content)

		case pdu.PduConcludeResponse:
			select {
			case c.concludeCh <- struct{}{}:
			default:
			}
			return

		case pdu.PduConcludeError:
			c.logger.Warn("mms: server rejected conclude")
			select {
			case c.concludeCh <- struct{}{}:
			default:
			}
			return

		default:
			c.logger.Warn("mms: reader loop unexpected PDU", "kind", kind)
		}
	}
}

func (c *Client) dispatchConfirmed(kind pdu.PduKind, content []byte) {
	invokeID, err := pdu.ExtractInvokeID(content)
	if err != nil {
		c.logger.Warn("mms: reader cannot extract invoke ID", "error", err)
		return
	}

	resp := invoke.Response{
		InvokeID: invokeID,
		Kind:     int(kind),
		Data:     content,
	}

	if !c.tracker.Complete(invokeID, resp) {
		c.mu.Lock()
		closing := c.closed
		c.mu.Unlock()
		if closing {
			c.logger.Debug("mms: discarding late response during shutdown", "invoke_id", invokeID)
		} else {
			c.logger.Warn("mms: reader got response for unknown invoke ID", "invoke_id", invokeID)
		}
	}
}

func (c *Client) dispatchUnconfirmed(content []byte) {
	report, err := pdu.UnmarshalInformationReport(content)
	if err != nil {
		c.logger.Warn("mms: reader cannot decode InformationReport", "error", err)
		return
	}

	indication, err := c.infoReportToIndication(report)
	if err != nil {
		c.logger.Warn("mms: reader cannot convert InformationReport", "error", err)
		return
	}

	c.reportMu.RLock()
	handler := c.reportHandler
	c.reportMu.RUnlock()

	if handler != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					c.logger.Error("mms: InformationReport handler panic", "panic", r)
				}
			}()
			handler(indication)
		}()
	}
}

func (c *Client) infoReportToIndication(r *pdu.InformationReportWire) (*InformationReportIndication, error) {
	ind := &InformationReportIndication{}

	if r.ListName != nil {
		n, err := wireNameToObjectName(*r.ListName)
		if err != nil {
			return nil, fmt.Errorf("list name: %w", err)
		}
		ind.ListName = &n
	}

	for i, v := range r.Variables {
		on, err := wireNameToObjectName(v)
		if err != nil {
			return nil, fmt.Errorf("variable [%d]: %w", i, err)
		}
		ind.Variables = append(ind.Variables, on)
	}

	for i, dv := range r.Values {
		val, err := dataValueToValue(dv)
		if err != nil {
			return nil, fmt.Errorf("value [%d]: %w", i, err)
		}
		ind.Values = append(ind.Values, val)
	}

	return ind, nil
}

func (c *Client) sendRaw(ctx context.Context, data []byte) error {
	if c.opts.RawHook != nil {
		c.opts.RawHook("send", data)
	}
	return c.conn.Send(ctx, data)
}

func (c *Client) receiveRaw(ctx context.Context) ([]byte, error) {
	data, err := c.conn.Receive(ctx)
	if err != nil {
		return nil, err
	}
	if c.maxPDUSize > 0 && len(data) > c.maxPDUSize {
		return nil, fmt.Errorf("mms: received PDU size %d exceeds negotiated max %d", len(data), c.maxPDUSize)
	}
	if c.opts.RawHook != nil {
		c.opts.RawHook("recv", data)
	}
	return data, nil
}

// ReadRequest specifies what to read from the MMS server.
// Set DomainID and ItemID to read a single named variable.
type ReadRequest struct {
	DomainID DomainID
	ItemID   ItemID
}

// ReadResult holds the result of an MMS Read operation.
type ReadResult struct {
	Value *Value
}

// AccessResult represents the outcome for a single variable in a
// multi-variable Read response.
type AccessResult struct {
	// Value holds the read data on success. Nil when ErrorCode is non-zero.
	Value *Value
	// ErrorCode is non-zero when the server reported a per-variable data
	// access error instead of returning a value.
	ErrorCode DataAccessErrorCode
}

// Read sends an MMS Read request for a single named variable and
// returns the read value. If the server reports a per-variable data
// access error, Read returns a [*DataAccessError].
func (c *Client) Read(ctx context.Context, req ReadRequest) (*ReadResult, error) {
	if req.DomainID == "" {
		return nil, fmt.Errorf("mms: read: empty DomainID")
	}
	if req.ItemID == "" {
		return nil, fmt.Errorf("mms: read: empty ItemID")
	}

	results, err := c.ReadMultiple(ctx, []ObjectName{
		{Scope: ObjectScopeDomain, Domain: req.DomainID, ItemID: req.ItemID},
	})
	if err != nil {
		return nil, err
	}
	r := results[0]
	if r.ErrorCode != 0 {
		return nil, &DataAccessError{Code: r.ErrorCode}
	}
	return &ReadResult{Value: r.Value}, nil
}

// ReadMultiple sends an MMS Read request for multiple named variables
// in a single PDU. Returns one [AccessResult] per variable in the
// same order as the input. Each result is either a value or a
// per-variable [DataAccessErrorCode].
//
// All three naming scopes are supported via [ObjectName.Scope]:
// VMD-specific, domain-specific, and association-specific.
func (c *Client) ReadMultiple(ctx context.Context, variables []ObjectName) ([]AccessResult, error) {
	for i, v := range variables {
		if err := validateObjectName(v); err != nil {
			return nil, fmt.Errorf("mms: read: variable [%d]: %w", i, err)
		}
	}

	invokeID := c.nextInvokeID()

	wireVars := make([]pdu.ObjectNameWire, len(variables))
	for i, v := range variables {
		wireVars[i] = objectNameToWire(v)
	}

	reqBytes, err := pdu.MarshalReadRequest(invokeID, wireVars)
	if err != nil {
		return nil, fmt.Errorf("mms: marshal read request: %w", err)
	}

	confirmed, err := c.sendConfirmed(ctx, invokeID, reqBytes)
	if err != nil {
		return nil, err
	}

	if confirmed.ServiceKind != pdu.ServiceRead {
		return nil, &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("expected Read response, got %s", confirmed.ServiceKind),
		}
	}

	dataValues, err := pdu.UnmarshalReadResponse(confirmed.ServiceData)
	if err != nil {
		return nil, fmt.Errorf("mms: %w", err)
	}

	if len(dataValues) != len(variables) {
		return nil, &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("read response: expected %d results, got %d", len(variables), len(dataValues)),
		}
	}

	results := make([]AccessResult, len(dataValues))
	for i, dv := range dataValues {
		if dv.Tag == pdu.TagDataAccessError {
			results[i] = AccessResult{ErrorCode: DataAccessErrorCode(dv.ErrCode)}
		} else {
			val, err := dataValueToValue(dv)
			if err != nil {
				return nil, fmt.Errorf("mms: read result [%d]: %w", i, err)
			}
			results[i] = AccessResult{Value: val}
		}
	}

	c.logger.Debug("mms: read",
		"invoke_id", invokeID,
		"service", "Read",
		"variables", len(variables),
		"results", len(results),
	)

	return results, nil
}

// WriteRequest specifies what to write to the MMS server.
type WriteRequest struct {
	DomainID DomainID
	ItemID   ItemID
	Value    *Value
}

// WriteResult holds the result of an MMS Write operation.
type WriteResult struct{}

// Write sends an MMS Write request for a single named variable.
// If the server reports a per-variable data access error, Write
// returns a [*DataAccessError].
func (c *Client) Write(ctx context.Context, req WriteRequest) (*WriteResult, error) {
	if req.DomainID == "" {
		return nil, fmt.Errorf("mms: write: empty DomainID")
	}
	if req.ItemID == "" {
		return nil, fmt.Errorf("mms: write: empty ItemID")
	}
	if req.Value == nil {
		return nil, fmt.Errorf("mms: write: nil Value")
	}

	invokeID := c.nextInvokeID()

	wireVar := pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: string(req.DomainID), ItemID: string(req.ItemID)}

	dv, err := valueToDataValue(req.Value)
	if err != nil {
		return nil, fmt.Errorf("mms: marshal write value: %w", err)
	}

	reqBytes, err := pdu.MarshalWriteRequest(invokeID, []pdu.ObjectNameWire{wireVar}, []*pdu.DataValue{dv})
	if err != nil {
		return nil, fmt.Errorf("mms: marshal write request: %w", err)
	}

	confirmed, err := c.sendConfirmed(ctx, invokeID, reqBytes)
	if err != nil {
		return nil, err
	}

	if confirmed.ServiceKind != pdu.ServiceWrite {
		return nil, &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("expected Write response, got %s", confirmed.ServiceKind),
		}
	}

	items, err := pdu.UnmarshalWriteResponse(confirmed.ServiceData)
	if err != nil {
		return nil, fmt.Errorf("mms: %w", err)
	}

	if len(items) != 1 {
		return nil, &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("write response: expected 1 result, got %d", len(items)),
		}
	}

	item := items[0]
	if !item.Success {
		return nil, &DataAccessError{Code: DataAccessErrorCode(item.ErrCode)}
	}

	c.logger.Debug("mms: write",
		"invoke_id", invokeID,
		"service", "Write",
		"domain_id", req.DomainID,
		"item_id", req.ItemID,
	)

	return &WriteResult{}, nil
}

// ReadVariables sends an MMS Read request for multiple variables,
// each optionally qualified with alternate access selectors for
// component, array-element, or array-range addressing.
func (c *Client) ReadVariables(ctx context.Context, variables []VariableSpec) ([]AccessResult, error) {
	if len(variables) == 0 {
		return nil, fmt.Errorf("mms: read: no variables specified")
	}
	for i, v := range variables {
		if err := validateObjectName(v.Name); err != nil {
			return nil, fmt.Errorf("mms: read: variable [%d]: %w", i, err)
		}
		if err := validateAccessSelectors(v.AlternateAccess); err != nil {
			return nil, fmt.Errorf("mms: read: variable [%d]: %w", i, err)
		}
	}

	invokeID := c.nextInvokeID()

	wireVars := make([]pdu.VariableSpecWire, len(variables))
	for i, v := range variables {
		wireVars[i] = variableSpecToWire(v)
	}

	reqBytes, err := pdu.MarshalReadRequestWithAccess(invokeID, wireVars)
	if err != nil {
		return nil, fmt.Errorf("mms: marshal read request: %w", err)
	}

	confirmed, err := c.sendConfirmed(ctx, invokeID, reqBytes)
	if err != nil {
		return nil, err
	}

	if confirmed.ServiceKind != pdu.ServiceRead {
		return nil, &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("expected Read response, got %s", confirmed.ServiceKind),
		}
	}

	dataValues, err := pdu.UnmarshalReadResponse(confirmed.ServiceData)
	if err != nil {
		return nil, fmt.Errorf("mms: %w", err)
	}

	if len(dataValues) != len(variables) {
		return nil, &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("read response count mismatch: requested %d, got %d", len(variables), len(dataValues)),
		}
	}

	results := make([]AccessResult, len(dataValues))
	for i, dv := range dataValues {
		if dv.Tag == pdu.TagDataAccessError {
			results[i] = AccessResult{ErrorCode: DataAccessErrorCode(dv.ErrCode)}
		} else {
			val, err := dataValueToValue(dv)
			if err != nil {
				return nil, fmt.Errorf("mms: read result [%d]: %w", i, err)
			}
			results[i] = AccessResult{Value: val}
		}
	}

	c.logger.Debug("mms: readVariables",
		"invoke_id", invokeID,
		"service", "Read",
		"variables", len(variables),
		"results", len(results),
	)

	return results, nil
}

// WriteVariables sends an MMS Write request for multiple variables,
// each optionally qualified with alternate access selectors.
// Returns per-variable results preserving partial-success semantics.
func (c *Client) WriteVariables(ctx context.Context, variables []VariableSpec, values []*Value) ([]WriteAccessResult, error) {
	if len(variables) != len(values) {
		return nil, fmt.Errorf("mms: write: %d variables but %d values", len(variables), len(values))
	}
	for i, v := range variables {
		if err := validateObjectName(v.Name); err != nil {
			return nil, fmt.Errorf("mms: write: variable [%d]: %w", i, err)
		}
		if err := validateAccessSelectors(v.AlternateAccess); err != nil {
			return nil, fmt.Errorf("mms: write: variable [%d]: %w", i, err)
		}
	}
	for i, val := range values {
		if val == nil {
			return nil, fmt.Errorf("mms: write: nil value at index %d", i)
		}
	}

	invokeID := c.nextInvokeID()

	wireVars := make([]pdu.VariableSpecWire, len(variables))
	for i, v := range variables {
		wireVars[i] = variableSpecToWire(v)
	}

	wireValues := make([]*pdu.DataValue, len(values))
	for i, val := range values {
		dv, err := valueToDataValue(val)
		if err != nil {
			return nil, fmt.Errorf("mms: marshal write value [%d]: %w", i, err)
		}
		wireValues[i] = dv
	}

	reqBytes, err := pdu.MarshalWriteRequestWithAccess(invokeID, wireVars, wireValues)
	if err != nil {
		return nil, fmt.Errorf("mms: marshal write request: %w", err)
	}

	confirmed, err := c.sendConfirmed(ctx, invokeID, reqBytes)
	if err != nil {
		return nil, err
	}

	if confirmed.ServiceKind != pdu.ServiceWrite {
		return nil, &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("expected Write response, got %s", confirmed.ServiceKind),
		}
	}

	items, err := pdu.UnmarshalWriteResponse(confirmed.ServiceData)
	if err != nil {
		return nil, fmt.Errorf("mms: %w", err)
	}

	if len(items) != len(variables) {
		return nil, &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("write response count mismatch: sent %d, got %d", len(variables), len(items)),
		}
	}

	results := make([]WriteAccessResult, len(items))
	for i, item := range items {
		results[i] = WriteAccessResult{
			Index:     i,
			Success:   item.Success,
			ErrorCode: DataAccessErrorCode(item.ErrCode),
		}
	}

	c.logger.Debug("mms: writeVariables",
		"invoke_id", invokeID,
		"service", "Write",
		"variables", len(variables),
	)

	return results, nil
}

// ReadComponent reads a single structure component by name.
func (c *Client) ReadComponent(ctx context.Context, name ObjectName, component string) (*ReadResult, error) {
	results, err := c.ReadVariables(ctx, []VariableSpec{{
		Name:            name,
		AlternateAccess: []AccessSelector{{Component: component}},
	}})
	if err != nil {
		return nil, err
	}
	r := results[0]
	if r.ErrorCode != 0 {
		return nil, &DataAccessError{Code: r.ErrorCode}
	}
	return &ReadResult{Value: r.Value}, nil
}

// WriteComponent writes a single structure component by name.
func (c *Client) WriteComponent(ctx context.Context, name ObjectName, component string, value *Value) error {
	results, err := c.WriteVariables(ctx,
		[]VariableSpec{{Name: name, AlternateAccess: []AccessSelector{{Component: component}}}},
		[]*Value{value},
	)
	if err != nil {
		return err
	}
	if !results[0].Success {
		return &DataAccessError{Code: results[0].ErrorCode}
	}
	return nil
}

// ReadByIndex reads a single element from an array or structure variable
// using a zero-based integer index. In MMS, index-based alternate access
// applies to both ordered structure elements and array elements.
// For arrays, this is equivalent to [Client.ReadArrayElement].
// ReadByIndex is preferred over ReadArrayElement.
func (c *Client) ReadByIndex(ctx context.Context, name ObjectName, index int) (*ReadResult, error) {
	results, err := c.ReadVariables(ctx, []VariableSpec{{
		Name:            name,
		AlternateAccess: []AccessSelector{{Index: &index}},
	}})
	if err != nil {
		return nil, err
	}
	r := results[0]
	if r.ErrorCode != 0 {
		return nil, &DataAccessError{Code: r.ErrorCode}
	}
	return &ReadResult{Value: r.Value}, nil
}

// Deprecated: ReadArrayElement is identical to [Client.ReadByIndex].
// Use ReadByIndex instead.
func (c *Client) ReadArrayElement(ctx context.Context, name ObjectName, index int) (*ReadResult, error) {
	results, err := c.ReadVariables(ctx, []VariableSpec{{
		Name:            name,
		AlternateAccess: []AccessSelector{{Index: &index}},
	}})
	if err != nil {
		return nil, err
	}
	r := results[0]
	if r.ErrorCode != 0 {
		return nil, &DataAccessError{Code: r.ErrorCode}
	}
	return &ReadResult{Value: r.Value}, nil
}

// WriteArrayElement writes a single array element by zero-based index.
func (c *Client) WriteArrayElement(ctx context.Context, name ObjectName, index int, value *Value) error {
	results, err := c.WriteVariables(ctx,
		[]VariableSpec{{Name: name, AlternateAccess: []AccessSelector{{Index: &index}}}},
		[]*Value{value},
	)
	if err != nil {
		return err
	}
	if !results[0].Success {
		return &DataAccessError{Code: results[0].ErrorCode}
	}
	return nil
}

// ReadArrayRange reads a contiguous range of array elements.
func (c *Client) ReadArrayRange(ctx context.Context, name ObjectName, start, count int) (*ReadResult, error) {
	results, err := c.ReadVariables(ctx, []VariableSpec{{
		Name:            name,
		AlternateAccess: []AccessSelector{{IndexRange: &IndexRange{Start: start, Count: count}}},
	}})
	if err != nil {
		return nil, err
	}
	r := results[0]
	if r.ErrorCode != 0 {
		return nil, &DataAccessError{Code: r.ErrorCode}
	}
	return &ReadResult{Value: r.Value}, nil
}

// ReadObject reads a single variable by [ObjectName], supporting
// all three scopes (VMD, domain, association).
func (c *Client) ReadObject(ctx context.Context, name ObjectName) (*ReadResult, error) {
	results, err := c.ReadMultiple(ctx, []ObjectName{name})
	if err != nil {
		return nil, err
	}
	r := results[0]
	if r.ErrorCode != 0 {
		return nil, &DataAccessError{Code: r.ErrorCode}
	}
	return &ReadResult{Value: r.Value}, nil
}

// WriteObject writes a single variable by [ObjectName], supporting
// all three scopes (VMD, domain, association).
func (c *Client) WriteObject(ctx context.Context, name ObjectName, value *Value) (*WriteResult, error) {
	if err := validateObjectName(name); err != nil {
		return nil, fmt.Errorf("mms: write: %w", err)
	}
	if value == nil {
		return nil, fmt.Errorf("mms: write: nil Value")
	}

	invokeID := c.nextInvokeID()

	wireVar := objectNameToWire(name)
	dv, err := valueToDataValue(value)
	if err != nil {
		return nil, fmt.Errorf("mms: marshal write value: %w", err)
	}

	reqBytes, err := pdu.MarshalWriteRequest(invokeID, []pdu.ObjectNameWire{wireVar}, []*pdu.DataValue{dv})
	if err != nil {
		return nil, fmt.Errorf("mms: marshal write request: %w", err)
	}

	confirmed, err := c.sendConfirmed(ctx, invokeID, reqBytes)
	if err != nil {
		return nil, err
	}

	if confirmed.ServiceKind != pdu.ServiceWrite {
		return nil, &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("expected Write response, got %s", confirmed.ServiceKind),
		}
	}

	items, err := pdu.UnmarshalWriteResponse(confirmed.ServiceData)
	if err != nil {
		return nil, fmt.Errorf("mms: %w", err)
	}

	if len(items) != 1 {
		return nil, &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("write response: expected 1 result, got %d", len(items)),
		}
	}

	if !items[0].Success {
		return nil, &DataAccessError{Code: DataAccessErrorCode(items[0].ErrCode)}
	}

	c.logger.Debug("mms: writeObject",
		"invoke_id", invokeID,
		"service", "Write",
		"scope", name.Scope,
		"item_id", name.ItemID,
	)

	return &WriteResult{}, nil
}

// ReadNamedVariableList reads all values from a named variable list
// in a single PDU using the variableListName addressing form.
func (c *Client) ReadNamedVariableList(ctx context.Context, listName ObjectName, opts ...ReadNamedVariableListOptions) ([]AccessResult, error) {
	if err := validateObjectName(listName); err != nil {
		return nil, fmt.Errorf("mms: read NVL: %w", err)
	}

	var opt ReadNamedVariableListOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	invokeID := c.nextInvokeID()

	var reqBytes []byte
	var err error
	if opt.SpecificationWithResult {
		reqBytes, err = pdu.MarshalReadRequestByListNameWithSpec(invokeID, objectNameToWire(listName), true)
	} else {
		reqBytes, err = pdu.MarshalReadRequestByListName(invokeID, objectNameToWire(listName))
	}
	if err != nil {
		return nil, fmt.Errorf("mms: marshal read NVL request: %w", err)
	}

	confirmed, err := c.sendConfirmed(ctx, invokeID, reqBytes)
	if err != nil {
		return nil, err
	}

	if confirmed.ServiceKind != pdu.ServiceRead {
		return nil, &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("expected Read response, got %s", confirmed.ServiceKind),
		}
	}

	dataValues, err := pdu.UnmarshalReadResponse(confirmed.ServiceData)
	if err != nil {
		return nil, fmt.Errorf("mms: %w", err)
	}

	results := make([]AccessResult, len(dataValues))
	for i, dv := range dataValues {
		if dv.Tag == pdu.TagDataAccessError {
			results[i] = AccessResult{ErrorCode: DataAccessErrorCode(dv.ErrCode)}
		} else {
			val, err := dataValueToValue(dv)
			if err != nil {
				return nil, fmt.Errorf("mms: read NVL result [%d]: %w", i, err)
			}
			results[i] = AccessResult{Value: val}
		}
	}

	c.logger.Debug("mms: readNamedVariableList",
		"invoke_id", invokeID,
		"service", "Read",
		"list_name", listName.ItemID,
		"results", len(results),
	)

	return results, nil
}

// WriteNamedVariableList writes all values to a named variable list
// in a single PDU using the variableListName addressing form.
// Returns per-variable results preserving partial-success semantics.
func (c *Client) WriteNamedVariableList(ctx context.Context, listName ObjectName, values []*Value) ([]WriteAccessResult, error) {
	if err := validateObjectName(listName); err != nil {
		return nil, fmt.Errorf("mms: write NVL: %w", err)
	}
	for i, val := range values {
		if val == nil {
			return nil, fmt.Errorf("mms: write NVL: nil value at index %d", i)
		}
	}

	invokeID := c.nextInvokeID()

	wireValues := make([]*pdu.DataValue, len(values))
	for i, val := range values {
		dv, err := valueToDataValue(val)
		if err != nil {
			return nil, fmt.Errorf("mms: write NVL: marshal value [%d]: %w", i, err)
		}
		wireValues[i] = dv
	}

	reqBytes, err := pdu.MarshalWriteRequestByListName(invokeID, objectNameToWire(listName), wireValues)
	if err != nil {
		return nil, fmt.Errorf("mms: marshal write NVL request: %w", err)
	}

	confirmed, err := c.sendConfirmed(ctx, invokeID, reqBytes)
	if err != nil {
		return nil, err
	}

	if confirmed.ServiceKind != pdu.ServiceWrite {
		return nil, &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("expected Write response, got %s", confirmed.ServiceKind),
		}
	}

	items, err := pdu.UnmarshalWriteResponse(confirmed.ServiceData)
	if err != nil {
		return nil, fmt.Errorf("mms: %w", err)
	}

	if len(items) != len(values) {
		return nil, &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("write NVL response count mismatch: sent %d, got %d", len(values), len(items)),
		}
	}

	results := make([]WriteAccessResult, len(items))
	for i, item := range items {
		results[i] = WriteAccessResult{
			Index:     i,
			Success:   item.Success,
			ErrorCode: DataAccessErrorCode(item.ErrCode),
		}
	}

	c.logger.Debug("mms: writeNamedVariableList",
		"invoke_id", invokeID,
		"service", "Write",
		"list_name", listName.ItemID,
	)

	return results, nil
}

// validateAccessSelectors validates an alternate access selector chain.
func validateAccessSelectors(selectors []AccessSelector) error {
	for i, sel := range selectors {
		kinds := 0
		if sel.Component != "" {
			kinds++
		}
		if sel.Index != nil {
			kinds++
		}
		if sel.IndexRange != nil {
			kinds++
		}
		if kinds == 0 {
			return fmt.Errorf("selector [%d]: no field set (need exactly one of Component, Index, or IndexRange)", i)
		}
		if kinds > 1 {
			return fmt.Errorf("selector [%d]: multiple fields set (need exactly one of Component, Index, or IndexRange)", i)
		}
		if sel.Index != nil && *sel.Index < 0 {
			return fmt.Errorf("selector [%d]: negative index %d", i, *sel.Index)
		}
		if sel.IndexRange != nil {
			if sel.IndexRange.Start < 0 {
				return fmt.Errorf("selector [%d]: negative range start %d", i, sel.IndexRange.Start)
			}
			if sel.IndexRange.Count <= 0 {
				return fmt.Errorf("selector [%d]: range count must be > 0, got %d", i, sel.IndexRange.Count)
			}
		}
	}
	return nil
}

func variableSpecToWire(v VariableSpec) pdu.VariableSpecWire {
	w := pdu.VariableSpecWire{Name: objectNameToWire(v.Name)}
	for _, sel := range v.AlternateAccess {
		ws := pdu.AccessSelectorWire{Component: sel.Component}
		if sel.Index != nil {
			ws.HasIndex = true
			ws.Index = *sel.Index
		}
		if sel.IndexRange != nil {
			ws.IndexRange = &pdu.IndexRangeWire{
				LowIndex:         sel.IndexRange.Start,
				NumberOfElements: sel.IndexRange.Count,
			}
		}
		w.AlternateAccess = append(w.AlternateAccess, ws)
	}
	return w
}

func variableSpecFromWire(w pdu.VariableSpecWire) (VariableSpec, error) {
	on, err := objectNameFromWire(w.Name)
	if err != nil {
		return VariableSpec{}, err
	}
	vs := VariableSpec{Name: on}
	for _, ws := range w.AlternateAccess {
		sel := AccessSelector{Component: ws.Component}
		if ws.HasIndex {
			idx := ws.Index
			sel.Index = &idx
		}
		if ws.IndexRange != nil {
			sel.IndexRange = &IndexRange{
				Start: ws.IndexRange.LowIndex,
				Count: ws.IndexRange.NumberOfElements,
			}
		}
		vs.AlternateAccess = append(vs.AlternateAccess, sel)
	}
	return vs, nil
}

// NameListRequest specifies the scope and filter for a GetNameList operation.
type NameListRequest struct {
	ObjectClass   ObjectClass
	Scope         ObjectScope // zero value = ObjectScopeVMD
	DomainID      DomainID    // required when Scope is ObjectScopeDomain
	ContinueAfter string      // continuation token; empty for first request
}

// NameListResult holds the result of a GetNameList operation.
type NameListResult struct {
	Names       []string
	MoreFollows bool
}

// GetNameList retrieves a list of named objects from the server.
//
// Use [NameListRequest.ContinueAfter] to page through large lists.
// Set it to the last name from the previous result when
// [NameListResult.MoreFollows] is true.
func (c *Client) GetNameList(ctx context.Context, req NameListRequest) (*NameListResult, error) {
	if int(req.ObjectClass) < 0 || int(req.ObjectClass) > int(ObjectClassOperatorStation) {
		return nil, fmt.Errorf("mms: getnamelist: unknown object class %d", int(req.ObjectClass))
	}
	switch req.Scope {
	case ObjectScopeVMD, ObjectScopeAssociation:
		// valid
	case ObjectScopeDomain:
		if req.DomainID == "" {
			return nil, fmt.Errorf("mms: getnamelist: domain scope requires non-empty DomainID")
		}
	default:
		return nil, fmt.Errorf("mms: getnamelist: unknown scope %d", int(req.Scope))
	}

	invokeID := c.nextInvokeID()

	scope := objectScopeToWireUnchecked(req.Scope)
	reqBytes, err := pdu.MarshalGetNameListRequest(
		invokeID, int(req.ObjectClass), scope,
		string(req.DomainID), req.ContinueAfter,
	)
	if err != nil {
		return nil, fmt.Errorf("mms: marshal getnamelist request: %w", err)
	}

	confirmed, err := c.sendConfirmed(ctx, invokeID, reqBytes)
	if err != nil {
		return nil, err
	}

	if confirmed.ServiceKind != pdu.ServiceGetNameList {
		return nil, &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("expected GetNameList response, got %s", confirmed.ServiceKind),
		}
	}

	result, err := pdu.UnmarshalGetNameListResponse(confirmed.ServiceData)
	if err != nil {
		return nil, fmt.Errorf("mms: %w", err)
	}

	c.logger.Debug("mms: getnamelist",
		"invoke_id", invokeID,
		"service", "GetNameList",
		"names", len(result.Names),
		"more_follows", result.MoreFollows,
	)

	return &NameListResult{
		Names:       result.Names,
		MoreFollows: result.MoreFollows,
	}, nil
}

// GetNameListAll retrieves all named objects by automatically handling
// continuation. It repeatedly calls [Client.GetNameList] until
// MoreFollows is false.
//
// As a safety measure, GetNameListAll detects stalled pagination (where
// the server repeats the same continuation token) and returns an error.
//
// Note: this method accumulates all names in memory. For servers with
// very large name lists, prefer using [Client.GetNameList] directly
// with explicit pagination control.
func (c *Client) GetNameListAll(ctx context.Context, req NameListRequest) ([]string, error) {
	var all []string
	prevToken := req.ContinueAfter
	for {
		result, err := c.GetNameList(ctx, req)
		if err != nil {
			return nil, err
		}
		all = append(all, result.Names...)
		if !result.MoreFollows || len(result.Names) == 0 {
			return all, nil
		}
		nextToken := result.Names[len(result.Names)-1]
		if nextToken == prevToken {
			return nil, &ProtocolError{
				Phase:   "mms",
				Message: fmt.Sprintf("getnamelist: pagination stalled (continuation token %q did not advance)", nextToken),
			}
		}
		prevToken = nextToken
		req.ContinueAfter = nextToken
	}
}

// VariableAccessAttributes holds the result of a
// GetVariableAccessAttributes operation.
type VariableAccessAttributes struct {
	Deletable bool
	TypeSpec  TypeSpec
}

// GetVariableAccessAttributes retrieves the type specification and
// attributes of a named variable from the server.
func (c *Client) GetVariableAccessAttributes(ctx context.Context, name ObjectName) (*VariableAccessAttributes, error) {
	if err := validateObjectName(name); err != nil {
		return nil, fmt.Errorf("mms: getvaraccess: %w", err)
	}

	invokeID := c.nextInvokeID()

	wireName := objectNameToWire(name)
	reqBytes, err := pdu.MarshalGetVarAccessRequest(invokeID, wireName)
	if err != nil {
		return nil, fmt.Errorf("mms: marshal getvaraccess request: %w", err)
	}

	confirmed, err := c.sendConfirmed(ctx, invokeID, reqBytes)
	if err != nil {
		return nil, err
	}

	if confirmed.ServiceKind != pdu.ServiceGetVariableAccessAttrs {
		return nil, &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("expected GetVariableAccessAttributes response, got %s", confirmed.ServiceKind),
		}
	}

	result, err := pdu.UnmarshalGetVarAccessResponse(confirmed.ServiceData)
	if err != nil {
		return nil, fmt.Errorf("mms: %w", err)
	}

	ts, err := typeSpecFromWire(result.TypeSpec)
	if err != nil {
		return nil, fmt.Errorf("mms: getvaraccess: convert type spec: %w", err)
	}

	c.logger.Debug("mms: getvaraccess",
		"invoke_id", invokeID,
		"service", "GetVariableAccessAttributes",
		"type", ts.Type.String(),
		"deletable", result.Deletable,
	)

	return &VariableAccessAttributes{
		Deletable: result.Deletable,
		TypeSpec:  ts,
	}, nil
}

// DefineNamedVariableListRequest specifies a named variable list to create.
// Variables may include alternate access selectors for component or
// index-based member addressing.
type DefineNamedVariableListRequest struct {
	ListName  ObjectName
	Variables []VariableSpec
}

// DefineNamedVariableList creates a named variable list on the server.
func (c *Client) DefineNamedVariableList(ctx context.Context, req DefineNamedVariableListRequest) error {
	if err := validateObjectName(req.ListName); err != nil {
		return fmt.Errorf("mms: define named variable list: list name: %w", err)
	}
	if len(req.Variables) == 0 {
		return fmt.Errorf("mms: define named variable list: no variables")
	}
	for i, v := range req.Variables {
		if err := validateObjectName(v.Name); err != nil {
			return fmt.Errorf("mms: define named variable list: variable [%d]: %w", i, err)
		}
	}

	invokeID := c.nextInvokeID()

	wireListName := objectNameToWire(req.ListName)
	wireVars := make([]pdu.VariableSpecWire, len(req.Variables))
	for i, v := range req.Variables {
		wireVars[i] = variableSpecToWire(v)
	}

	reqBytes, err := pdu.MarshalDefineNamedVarListRequest(invokeID, wireListName, wireVars)
	if err != nil {
		return fmt.Errorf("mms: marshal define named var list request: %w", err)
	}

	confirmed, err := c.sendConfirmed(ctx, invokeID, reqBytes)
	if err != nil {
		return err
	}

	if confirmed.ServiceKind != pdu.ServiceDefineNamedVariableList {
		return &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("expected DefineNamedVariableList response, got %s", confirmed.ServiceKind),
		}
	}

	c.logger.Debug("mms: define named variable list",
		"invoke_id", invokeID,
		"service", "DefineNamedVariableList",
		"list_name", req.ListName.ItemID,
		"variables", len(req.Variables),
	)

	return nil
}

// NamedVariableListAttributes holds the result of a
// GetNamedVariableListAttributes operation. Variables preserves
// alternate access selectors when present in the server response.
type NamedVariableListAttributes struct {
	Deletable bool
	Variables []VariableSpec
}

// GetNamedVariableListAttributes retrieves the attributes of a named
// variable list from the server.
func (c *Client) GetNamedVariableListAttributes(ctx context.Context, listName ObjectName) (*NamedVariableListAttributes, error) {
	if err := validateObjectName(listName); err != nil {
		return nil, fmt.Errorf("mms: getnamedvarlistattrs: %w", err)
	}

	invokeID := c.nextInvokeID()

	wireName := objectNameToWire(listName)
	reqBytes, err := pdu.MarshalGetNamedVarListAttrsRequest(invokeID, wireName)
	if err != nil {
		return nil, fmt.Errorf("mms: marshal get named var list attrs request: %w", err)
	}

	confirmed, err := c.sendConfirmed(ctx, invokeID, reqBytes)
	if err != nil {
		return nil, err
	}

	if confirmed.ServiceKind != pdu.ServiceGetNamedVariableListAttrs {
		return nil, &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("expected GetNamedVariableListAttributes response, got %s", confirmed.ServiceKind),
		}
	}

	result, err := pdu.UnmarshalGetNamedVarListAttrsResponse(confirmed.ServiceData)
	if err != nil {
		return nil, fmt.Errorf("mms: %w", err)
	}

	variables := make([]VariableSpec, len(result.Variables))
	for i, wn := range result.Variables {
		vs, err := variableSpecFromWire(wn)
		if err != nil {
			return nil, fmt.Errorf("mms: variable [%d]: %w", i, err)
		}
		variables[i] = vs
	}

	c.logger.Debug("mms: get named variable list attributes",
		"invoke_id", invokeID,
		"service", "GetNamedVariableListAttributes",
		"deletable", result.Deletable,
		"variables", len(variables),
	)

	return &NamedVariableListAttributes{
		Deletable: result.Deletable,
		Variables: variables,
	}, nil
}

// DeleteNamedVariableListResult holds the result of a
// DeleteNamedVariableList operation.
type DeleteNamedVariableListResult struct {
	NumberMatched int
	NumberDeleted int
}

// DeleteNamedVariableList deletes one or more named variable lists from
// the server.
func (c *Client) DeleteNamedVariableList(ctx context.Context, listNames []ObjectName) (*DeleteNamedVariableListResult, error) {
	if len(listNames) == 0 {
		return nil, fmt.Errorf("mms: delete named variable list: no list names")
	}
	for i, n := range listNames {
		if err := validateObjectName(n); err != nil {
			return nil, fmt.Errorf("mms: delete named variable list: name [%d]: %w", i, err)
		}
	}

	invokeID := c.nextInvokeID()

	wireNames := make([]pdu.ObjectNameWire, len(listNames))
	for i, n := range listNames {
		wireNames[i] = objectNameToWire(n)
	}

	reqBytes, err := pdu.MarshalDeleteNamedVarListRequest(invokeID, wireNames)
	if err != nil {
		return nil, fmt.Errorf("mms: marshal delete named var list request: %w", err)
	}

	confirmed, err := c.sendConfirmed(ctx, invokeID, reqBytes)
	if err != nil {
		return nil, err
	}

	if confirmed.ServiceKind != pdu.ServiceDeleteNamedVariableList {
		return nil, &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("expected DeleteNamedVariableList response, got %s", confirmed.ServiceKind),
		}
	}

	result, err := pdu.UnmarshalDeleteNamedVarListResponse(confirmed.ServiceData)
	if err != nil {
		return nil, fmt.Errorf("mms: %w", err)
	}

	c.logger.Debug("mms: delete named variable list",
		"invoke_id", invokeID,
		"service", "DeleteNamedVariableList",
		"matched", result.NumberMatched,
		"deleted", result.NumberDeleted,
	)

	return &DeleteNamedVariableListResult{
		NumberMatched: result.NumberMatched,
		NumberDeleted: result.NumberDeleted,
	}, nil
}

// DeleteAllDomainNVLs deletes all deletable named variable lists in the
// specified domain (scopeOfDelete=2). The server iterates all NVLs in
// the domain and deletes those that are marked as deletable.
func (c *Client) DeleteAllDomainNVLs(ctx context.Context, domain string) (*DeleteNamedVariableListResult, error) {
	if domain == "" {
		return nil, fmt.Errorf("mms: delete all domain NVLs: empty domain name")
	}

	invokeID := c.nextInvokeID()

	reqBytes, err := pdu.MarshalDeleteNVLDomainScopeRequest(invokeID, domain)
	if err != nil {
		return nil, fmt.Errorf("mms: marshal delete domain NVLs request: %w", err)
	}

	confirmed, err := c.sendConfirmed(ctx, invokeID, reqBytes)
	if err != nil {
		return nil, err
	}

	if confirmed.ServiceKind != pdu.ServiceDeleteNamedVariableList {
		return nil, &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("expected DeleteNamedVariableList response, got %s", confirmed.ServiceKind),
		}
	}

	result, err := pdu.UnmarshalDeleteNamedVarListResponse(confirmed.ServiceData)
	if err != nil {
		return nil, fmt.Errorf("mms: %w", err)
	}

	c.logger.Debug("mms: delete all domain NVLs",
		"invoke_id", invokeID,
		"service", "DeleteNamedVariableList",
		"domain", domain,
		"matched", result.NumberMatched,
		"deleted", result.NumberDeleted,
	)

	return &DeleteNamedVariableListResult{
		NumberMatched: result.NumberMatched,
		NumberDeleted: result.NumberDeleted,
	}, nil
}

// DeleteAllVMDNVLs deletes all deletable VMD-scoped named variable lists
// (scopeOfDelete=3). The server iterates all VMD-scoped NVLs and deletes
// those that are marked as deletable.
func (c *Client) DeleteAllVMDNVLs(ctx context.Context) (*DeleteNamedVariableListResult, error) {
	invokeID := c.nextInvokeID()

	reqBytes, err := pdu.MarshalDeleteNVLVMDScopeRequest(invokeID)
	if err != nil {
		return nil, fmt.Errorf("mms: marshal delete VMD NVLs request: %w", err)
	}

	confirmed, err := c.sendConfirmed(ctx, invokeID, reqBytes)
	if err != nil {
		return nil, err
	}

	if confirmed.ServiceKind != pdu.ServiceDeleteNamedVariableList {
		return nil, &ProtocolError{
			Phase:   "mms",
			Message: fmt.Sprintf("expected DeleteNamedVariableList response, got %s", confirmed.ServiceKind),
		}
	}

	result, err := pdu.UnmarshalDeleteNamedVarListResponse(confirmed.ServiceData)
	if err != nil {
		return nil, fmt.Errorf("mms: %w", err)
	}

	c.logger.Debug("mms: delete all VMD NVLs",
		"invoke_id", invokeID,
		"service", "DeleteNamedVariableList",
		"matched", result.NumberMatched,
		"deleted", result.NumberDeleted,
	)

	return &DeleteNamedVariableListResult{
		NumberMatched: result.NumberMatched,
		NumberDeleted: result.NumberDeleted,
	}, nil
}

// validateObjectName checks that a public ObjectName has valid field
// combinations: non-empty ItemID, domain scope requires non-empty Domain,
// and scope must be a known value.
func validateObjectName(n ObjectName) error {
	if n.ItemID == "" {
		return fmt.Errorf("empty ItemID")
	}
	switch n.Scope {
	case ObjectScopeVMD, ObjectScopeAssociation:
		// valid
	case ObjectScopeDomain:
		if n.Domain == "" {
			return fmt.Errorf("domain scope requires non-empty Domain")
		}
	default:
		return fmt.Errorf("unknown scope %d", int(n.Scope))
	}
	return nil
}

// objectNameToWire converts a public ObjectName to internal wire format.
func objectNameToWire(n ObjectName) pdu.ObjectNameWire {
	return pdu.ObjectNameWire{
		Scope:    objectScopeToWireUnchecked(n.Scope),
		DomainID: string(n.Domain),
		ItemID:   string(n.ItemID),
	}
}

// objectNameFromWire converts an internal wire ObjectName to public format.
func objectNameFromWire(wn pdu.ObjectNameWire) (ObjectName, error) {
	scope, err := objectScopeFromWire(wn.Scope)
	if err != nil {
		return ObjectName{}, err
	}
	return ObjectName{
		Scope:  scope,
		Domain: DomainID(wn.DomainID),
		ItemID: ItemID(wn.ItemID),
	}, nil
}

// wireNameToObjectName converts a wire name to public ObjectName.
// Returns an error for unknown wire scope values.
func wireNameToObjectName(wn pdu.ObjectNameWire) (ObjectName, error) {
	return objectNameFromWire(wn)
}

// objectScopeFromWire converts a wire scope constant to public ObjectScope.
// Returns an error for unknown wire scope values.
func objectScopeFromWire(s int) (ObjectScope, error) {
	switch s {
	case pdu.ScopeVMD:
		return ObjectScopeVMD, nil
	case pdu.ScopeDomain:
		return ObjectScopeDomain, nil
	case pdu.ScopeAssociation:
		return ObjectScopeAssociation, nil
	default:
		return 0, fmt.Errorf("mms: unknown wire scope %d", s)
	}
}

// objectScopeToWire converts a public ObjectScope to wire constant.
// objectScopeToWireUnchecked converts a public scope to a wire constant.
// Only valid scopes should be passed; callers must validate first via
// validateObjectName or the switch in GetNameList.
func objectScopeToWireUnchecked(s ObjectScope) int {
	switch s {
	case ObjectScopeDomain:
		return pdu.ScopeDomain
	case ObjectScopeAssociation:
		return pdu.ScopeAssociation
	default:
		return pdu.ScopeVMD
	}
}

// objectScopeToWire converts a public ObjectScope to wire constant,
// returning an error for unknown scope values.
func objectScopeToWire(s ObjectScope) (int, error) {
	switch s {
	case ObjectScopeVMD:
		return pdu.ScopeVMD, nil
	case ObjectScopeDomain:
		return pdu.ScopeDomain, nil
	case ObjectScopeAssociation:
		return pdu.ScopeAssociation, nil
	default:
		return 0, fmt.Errorf("mms: unknown scope %d", s)
	}
}

// typeSpecFromWire converts an internal TypeSpecWire to the public TypeSpec.
// Returns an error for wire tags that cannot be mapped to a public TypeSpec.
func typeSpecFromWire(ts pdu.TypeSpecWire) (TypeSpec, error) {
	switch ts.Tag {
	case 3: // boolean
		return TypeSpec{Type: ValueTypeBoolean}, nil
	case 4: // bitstring
		return TypeSpec{Type: ValueTypeBitString, Size: ts.Size}, nil
	case 5: // integer
		return TypeSpec{Type: ValueTypeInteger, Size: ts.Size}, nil
	case 6: // unsigned
		return TypeSpec{Type: ValueTypeUnsigned, Size: ts.Size}, nil
	case 7: // float
		return TypeSpec{Type: ValueTypeFloat, FormatWidth: ts.FormatWidth, ExponentWidth: ts.ExpWidth}, nil
	case 9: // octetstring
		return TypeSpec{Type: ValueTypeOctetString, Size: ts.Size}, nil
	case 10: // visiblestring
		return TypeSpec{Type: ValueTypeVisibleString, Size: ts.Size}, nil
	case 12: // binarytime
		return TypeSpec{Type: ValueTypeBinaryTime}, nil
	case 16: // mmsstring
		return TypeSpec{Type: ValueTypeMmsString, Size: ts.Size}, nil
	case 17: // utctime
		return TypeSpec{Type: ValueTypeUTCTime}, nil
	case 8: // objectidentifier
		return TypeSpec{Type: ValueTypeObjectIdentifier}, nil
	case 11: // generalizedtime
		return TypeSpec{Type: ValueTypeGeneralizedTime}, nil
	case 13: // bcd
		return TypeSpec{Type: ValueTypeBCD, Size: ts.Size}, nil
	case 0: // typeName — reference to a named type
		result := TypeSpec{Type: ValueTypeNamedType}
		if ts.TypeName != nil {
			ref, err := objectNameFromWire(*ts.TypeName)
			if err != nil {
				return TypeSpec{}, fmt.Errorf("typeName: %w", err)
			}
			result.TypeName = &ref
		}
		return result, nil
	case 1: // array
		if ts.Element != nil {
			elemTS, err := typeSpecFromWire(*ts.Element)
			if err != nil {
				return TypeSpec{}, fmt.Errorf("array element: %w", err)
			}
			return TypeSpec{Type: ValueTypeArray, Count: ts.Count, Element: &elemTS}, nil
		}
		return TypeSpec{Type: ValueTypeArray, Count: ts.Count}, nil
	case 2: // structure
		elements := make([]TypeSpecElement, len(ts.Components))
		for i, comp := range ts.Components {
			compTS, err := typeSpecFromWire(comp.Type)
			if err != nil {
				return TypeSpec{}, fmt.Errorf("structure component [%d] %q: %w", i, comp.Name, err)
			}
			elements[i] = TypeSpecElement{
				Name: comp.Name,
				Type: compTS,
			}
		}
		return TypeSpec{Type: ValueTypeStructure, Elements: elements}, nil
	default:
		return TypeSpec{}, fmt.Errorf("unsupported TypeSpecification tag [%d]", ts.Tag)
	}
}

// Value ↔ DataValue conversion helpers.

func valueToDataValue(v *Value) (*pdu.DataValue, error) {
	if v == nil {
		return nil, fmt.Errorf("nil value")
	}
	switch v.typ {
	case ValueTypeBoolean:
		return &pdu.DataValue{Tag: pdu.TagDataBoolean, Bool: v.boolVal}, nil
	case ValueTypeInteger:
		return &pdu.DataValue{Tag: pdu.TagDataInteger, Int: v.intVal}, nil
	case ValueTypeUnsigned:
		return &pdu.DataValue{Tag: pdu.TagDataUnsigned, Uint: v.uintVal}, nil
	case ValueTypeFloat:
		wide := float64(float32(v.floatVal)) != v.floatVal
		return &pdu.DataValue{Tag: pdu.TagDataFloat, Float: v.floatVal, FloatWide: wide}, nil
	case ValueTypeBitString:
		return &pdu.DataValue{Tag: pdu.TagDataBitString, Bytes: copyBytes(v.bytesVal), BitLen: v.bitLen}, nil
	case ValueTypeOctetString:
		return &pdu.DataValue{Tag: pdu.TagDataOctetString, Bytes: copyBytes(v.bytesVal)}, nil
	case ValueTypeVisibleString:
		return &pdu.DataValue{Tag: pdu.TagDataVisibleStr, Str: v.stringVal}, nil
	case ValueTypeMmsString:
		return &pdu.DataValue{Tag: pdu.TagDataMmsString, Str: v.stringVal}, nil
	case ValueTypeUTCTime:
		return &pdu.DataValue{Tag: pdu.TagDataUTCTime, Time: v.timeVal, TimeQuality: v.timeQuality}, nil
	case ValueTypeBinaryTime:
		return &pdu.DataValue{Tag: pdu.TagDataBinaryTime, BinTimeMs: v.binaryTime}, nil
	case ValueTypeArray:
		elems, err := valuesToDataValues(v.elementsVal)
		if err != nil {
			return nil, err
		}
		return &pdu.DataValue{Tag: pdu.TagDataArray, Elements: elems}, nil
	case ValueTypeStructure:
		elems, err := valuesToDataValues(v.elementsVal)
		if err != nil {
			return nil, err
		}
		return &pdu.DataValue{Tag: pdu.TagDataStructure, Elements: elems}, nil
	case ValueTypeGeneralizedTime:
		return &pdu.DataValue{Tag: pdu.TagDataGenTime, Time: v.timeVal}, nil
	case ValueTypeBCD:
		return &pdu.DataValue{Tag: pdu.TagDataBCD, Int: v.intVal}, nil
	case ValueTypeObjectIdentifier:
		oid := make([]int, len(v.oidVal))
		copy(oid, v.oidVal)
		return &pdu.DataValue{Tag: pdu.TagDataObjId, OID: oid}, nil
	case ValueTypeReal:
		return &pdu.DataValue{Tag: pdu.TagDataReal, Float: v.floatVal}, nil
	case ValueTypeBooleanArray:
		return &pdu.DataValue{Tag: pdu.TagDataBooleanArray, Bytes: copyBytes(v.bytesVal), BitLen: v.bitLen}, nil
	case ValueTypeDataAccessError:
		return nil, fmt.Errorf("cannot marshal DataAccessError as writable MMS Data value")
	default:
		return nil, fmt.Errorf("unsupported value type %s", v.typ)
	}
}

func valuesToDataValues(values []*Value) ([]*pdu.DataValue, error) {
	result := make([]*pdu.DataValue, len(values))
	for i, v := range values {
		dv, err := valueToDataValue(v)
		if err != nil {
			return nil, fmt.Errorf("element [%d]: %w", i, err)
		}
		result[i] = dv
	}
	return result, nil
}

func dataValueToValue(dv *pdu.DataValue) (*Value, error) {
	switch dv.Tag {
	case pdu.TagDataBoolean:
		return &Value{typ: ValueTypeBoolean, boolVal: dv.Bool}, nil
	case pdu.TagDataInteger:
		return &Value{typ: ValueTypeInteger, intVal: dv.Int}, nil
	case pdu.TagDataUnsigned:
		return &Value{typ: ValueTypeUnsigned, uintVal: dv.Uint}, nil
	case pdu.TagDataFloat:
		return &Value{typ: ValueTypeFloat, floatVal: dv.Float}, nil
	case pdu.TagDataBitString:
		return &Value{typ: ValueTypeBitString, bytesVal: copyBytes(dv.Bytes), bitLen: dv.BitLen}, nil
	case pdu.TagDataOctetString:
		return &Value{typ: ValueTypeOctetString, bytesVal: copyBytes(dv.Bytes)}, nil
	case pdu.TagDataVisibleStr:
		return &Value{typ: ValueTypeVisibleString, stringVal: dv.Str}, nil
	case pdu.TagDataMmsString:
		return &Value{typ: ValueTypeMmsString, stringVal: dv.Str}, nil
	case pdu.TagDataUTCTime:
		return &Value{typ: ValueTypeUTCTime, timeVal: dv.Time, timeQuality: dv.TimeQuality}, nil
	case pdu.TagDataBinaryTime:
		return &Value{typ: ValueTypeBinaryTime, binaryTime: dv.BinTimeMs}, nil
	case pdu.TagDataArray:
		children, err := dataValuesToValues(dv.Elements)
		if err != nil {
			return nil, err
		}
		return &Value{typ: ValueTypeArray, elementsVal: children}, nil
	case pdu.TagDataStructure:
		children, err := dataValuesToValues(dv.Elements)
		if err != nil {
			return nil, err
		}
		return &Value{typ: ValueTypeStructure, elementsVal: children}, nil
	case pdu.TagDataGenTime:
		return &Value{typ: ValueTypeGeneralizedTime, timeVal: dv.Time}, nil
	case pdu.TagDataBCD:
		return &Value{typ: ValueTypeBCD, intVal: dv.Int}, nil
	case pdu.TagDataObjId:
		oid := make([]int, len(dv.OID))
		copy(oid, dv.OID)
		return &Value{typ: ValueTypeObjectIdentifier, oidVal: oid}, nil
	case pdu.TagDataReal:
		return &Value{typ: ValueTypeReal, floatVal: dv.Float}, nil
	case pdu.TagDataBooleanArray:
		return &Value{typ: ValueTypeBooleanArray, bytesVal: copyBytes(dv.Bytes), bitLen: dv.BitLen}, nil
	case pdu.TagDataAccessError:
		return &Value{typ: ValueTypeDataAccessError, accessErr: DataAccessErrorCode(dv.ErrCode)}, nil
	default:
		return nil, fmt.Errorf("unknown internal data tag 0x%02x", dv.Tag)
	}
}

func dataValuesToValues(dvs []*pdu.DataValue) ([]*Value, error) {
	result := make([]*Value, len(dvs))
	for i, dv := range dvs {
		v, err := dataValueToValue(dv)
		if err != nil {
			return nil, fmt.Errorf("element [%d]: %w", i, err)
		}
		result[i] = v
	}
	return result, nil
}

// discardHandler is a slog.Handler that discards all log records.
type discardHandler struct{}

func (discardHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (d discardHandler) WithAttrs([]slog.Attr) slog.Handler      { return d }
func (d discardHandler) WithGroup(string) slog.Handler           { return d }
