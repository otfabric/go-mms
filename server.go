package mms

import (
	"context"
	"encoding/asn1"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/pdu"
	"github.com/otfabric/go-mms/internal/serverconn"
	"github.com/otfabric/go-mms/internal/servermodel"
)

// ConfirmedErrorPDU service-error constants.
//
// These are used in ConfirmedErrorPDU responses. They are conceptually
// separate from per-variable DataAccessErrorCode values even though some
// numeric values may coincide. This separation is deliberate to keep the
// confirmed-service error domain and per-variable access-error domain
// independent.
const (
	// serviceErrorClassVMD is the error class for VMD-state errors.
	serviceErrorClassVMD = 0
	// serviceErrorClassService is the error class for service-level errors.
	serviceErrorClassService = 4
	// serviceErrorClassAccess is the error class for access/object errors.
	serviceErrorClassAccess = 7

	// Confirmed service-error codes within their class.
	svcErrOther               = 0 // generic "other"
	svcErrObjectNonExistent   = 0 // access: object-non-existent
	svcErrObjectAccessDenied  = 1 // access: object-access-denied
	svcErrServiceUnsupported  = 4 // vmd-state: other (no handler)
	svcErrServiceNotSupported = 5 // vmd-state: service-not-supported
)

var (
	errServiceUnsupported = &serverconn.ServiceError{ErrorClass: serviceErrorClassVMD, ErrorCode: svcErrServiceUnsupported}
	errObjectNonExistent  = &serverconn.ServiceError{ErrorClass: serviceErrorClassAccess, ErrorCode: svcErrObjectNonExistent}
	errAccessDenied       = &serverconn.ServiceError{ErrorClass: serviceErrorClassAccess, ErrorCode: svcErrObjectAccessDenied}
	errInvalidRequest     = &serverconn.ServiceError{ErrorClass: serviceErrorClassService, ErrorCode: svcErrOther}
	errUnsupportedFeature = &serverconn.ServiceError{ErrorClass: serviceErrorClassVMD, ErrorCode: svcErrServiceNotSupported}
)

// Per-variable data-access error codes for Read/Write result lists.
// These use the public DataAccessErrorCode constants and are independent
// of the ConfirmedErrorPDU service-error codes above.
const (
	wireErrObjectUndefined  = int(DataAccessErrorObjectUndefined)
	wireErrAccessDenied     = int(DataAccessErrorObjectAccessDenied)
	wireErrTempUnavail      = int(DataAccessErrorTemporarilyUnavailable)
	wireErrTypeInconsistent = int(DataAccessErrorTypeInconsistent)
)

// Server is a generic MMS server that accepts associations and serves
// confirmed MMS services via registered handlers and a variable registry.
//
// Create a Server with [NewServer], register handlers and variables,
// then call [Server.Serve] to handle a single transport connection or
// [Server.ListenAndServe] with a [TransportListener] for an accept loop.
//
// Active server connections can be iterated via [Server.Connections] and
// used to send unconfirmed PDUs such as InformationReport.
type Server struct {
	mu              sync.RWMutex
	logger          *slog.Logger
	mmsOpts         ServerMMSOptions
	registry        *servermodel.Registry
	authenticator   Authenticator
	fileProvider    FileProvider
	journalProvider JournalProvider

	identifyHandler func(context.Context, IdentifyRequest) (*ServerIdentity, error)
	statusHandler   func(context.Context, StatusRequest) (*ServerStatus, error)

	connsMu sync.RWMutex
	conns   map[*ServerConn]struct{}
}

// ServerConn represents an active server-side MMS association. It can
// be used to send unsolicited PDUs (e.g. InformationReport) to the
// connected client.
//
// A ServerConn is valid for the duration of the [Server.Serve] call
// that created it. After Serve returns, the ServerConn is marked as
// closed and any subsequent [ServerConn.SendInformationReport] calls
// return [ErrServerConnClosed].
type ServerConn struct {
	mu        sync.RWMutex
	closed    bool
	conn      *serverconn.Conn
	authToken any
	frsmTable *frsmTable

	assocNVLs     map[string]*servermodel.NVLEntry
	assocNVLOrder []string
}

// NewServer creates a new MMS server with the given options.
func NewServer(opts ServerOptions) *Server {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(discardHandler{})
	}
	return &Server{
		logger:          logger,
		mmsOpts:         opts.MMS.withDefaults(),
		registry:        servermodel.NewRegistry(),
		authenticator:   opts.Authenticate,
		fileProvider:    opts.FileProvider,
		journalProvider: opts.JournalProvider,
		conns:           make(map[*ServerConn]struct{}),
	}
}

// HandleIdentify registers a handler for the MMS Identify service.
func (s *Server) HandleIdentify(h func(context.Context, IdentifyRequest) (*ServerIdentity, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.identifyHandler = h
}

// HandleStatus registers a handler for the MMS Status service.
func (s *Server) HandleStatus(h func(context.Context, StatusRequest) (*ServerStatus, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statusHandler = h
}

// RegisterDomain adds a domain to the server's MMS object model.
func (s *Server) RegisterDomain(name string) error {
	return s.registry.RegisterDomain(name)
}

// RegisterVariable adds a named variable to the server's MMS object model.
// The variable's domain (if domain-scoped) must already be registered.
// The TypeSpec is validated at registration time; unsupported type
// specifications are rejected early rather than at request time.
func (s *Server) RegisterVariable(v Variable) error {
	if err := validateObjectName(v.Name); err != nil {
		return fmt.Errorf("mms: register variable: %w", err)
	}
	if _, err := typeSpecToWire(v.TypeSpec); err != nil {
		return fmt.Errorf("mms: register variable %q: invalid type spec: %w", v.Name.ItemID, err)
	}
	entry := &servermodel.VarEntry{
		Domain:    string(v.Name.Domain),
		ItemID:    string(v.Name.ItemID),
		Scope:     int(v.Name.Scope),
		Deletable: v.Deletable,
		TypeSpec:  v.TypeSpec,
		ReadFunc:  v.Read,
		WriteFunc: v.Write,
	}
	return s.registry.RegisterVariable(entry)
}

// RegisterNamedVariableList adds a static named variable list to the
// server's MMS object model. The list's domain (if domain-scoped) must
// already be registered via [Server.RegisterDomain].
func (s *Server) RegisterNamedVariableList(nvl NamedVariableList) error {
	if err := validateObjectName(nvl.Name); err != nil {
		return fmt.Errorf("mms: register NVL: %w", err)
	}
	if len(nvl.Variables) == 0 {
		return fmt.Errorf("mms: register NVL %q: no variables", nvl.Name.ItemID)
	}

	vars := make([]servermodel.NVLVariable, len(nvl.Variables))
	for i, v := range nvl.Variables {
		if err := validateObjectName(v.Name); err != nil {
			return fmt.Errorf("mms: register NVL %q: variable [%d]: %w", nvl.Name.ItemID, i, err)
		}
		nv := servermodel.NVLVariable{
			Scope:    int(v.Name.Scope),
			DomainID: string(v.Name.Domain),
			ItemID:   string(v.Name.ItemID),
		}
		for _, sel := range v.AlternateAccess {
			sm := servermodel.AccessSelectorModel{
				Component: sel.Component,
			}
			if sel.Index != nil {
				sm.HasIndex = true
				sm.Index = *sel.Index
			}
			if sel.IndexRange != nil {
				sm.IndexRange = &servermodel.IndexRangeModel{
					LowIndex:         sel.IndexRange.Start,
					NumberOfElements: sel.IndexRange.Count,
				}
			}
			nv.AlternateAccess = append(nv.AlternateAccess, sm)
		}
		vars[i] = nv
	}

	entry := &servermodel.NVLEntry{
		Domain:    string(nvl.Name.Domain),
		ItemID:    string(nvl.Name.ItemID),
		Scope:     int(nvl.Name.Scope),
		Deletable: nvl.Deletable,
		Variables: vars,
	}
	return s.registry.DefineNVL(entry)
}

// Serve handles a single MMS association over the provided transport.
// It performs the ISO handshake, then enters a request/response loop
// until the client disconnects or an error occurs.
//
// The corresponding [ServerConn] is registered for the duration of
// Serve and can be obtained via [Server.Connections].
//
// Serve does not close the transport — the caller owns transport
// lifecycle. [ListenAndServe] handles this automatically with a
// deferred Close in the per-connection goroutine.
//
// Serve blocks until the connection is complete. For serving multiple
// connections, call Serve in a goroutine per accepted transport.
func (s *Server) Serve(ctx context.Context, conn Transport) error {
	c := serverconn.New(conn, s.logger, s.dispatch, serverconn.MMSOptions{
		MaxPDUSize:                s.mmsOpts.MaxPDUSize,
		MaxOutstandingCalling:     s.mmsOpts.MaxOutstandingCalling,
		MaxOutstandingCalled:      s.mmsOpts.MaxOutstandingCalled,
		DataStructureNestingLevel: s.mmsOpts.DataStructureNestingLevel,
	})

	authInfo, err := c.ReceiveAssociation(ctx)
	if err != nil {
		return err
	}

	var authToken any
	if s.authenticator != nil {
		authCtx := s.buildAuthContext(conn, authInfo)
		result, authErr := s.authenticator(ctx, authCtx)
		if authErr != nil {
			s.logger.Warn("authentication error", "error", authErr)
			_ = c.RejectAssociation(ctx)
			return &AuthenticationError{Reason: authErr.Error()}
		}
		if !result.Accept {
			s.logger.Warn("authentication rejected")
			_ = c.RejectAssociation(ctx)
			return &AuthenticationError{Reason: "authenticator rejected"}
		}
		authToken = result.Token
	}

	if err := c.AcceptAssociation(ctx); err != nil {
		return err
	}

	sc := &ServerConn{conn: c, authToken: authToken, frsmTable: newFRSMTable()}
	s.registerConn(sc)
	defer func() {
		if s.fileProvider != nil {
			sc.frsmTable.closeAll(ctx, s.fileProvider)
		}
		s.unregisterConn(sc)
	}()

	svcCtx := context.WithValue(ctx, serverConnCtxKey{}, sc)
	return c.Serve(svcCtx)
}

func (s *Server) buildAuthContext(conn Transport, authInfo acse.AuthInfo) *AuthContext {
	ac := &AuthContext{}

	// TLS peer certificate
	if tt, ok := conn.(TLSTransport); ok {
		if state := tt.TLSConnectionState(); state != nil && len(state.PeerCertificates) > 0 {
			ac.PeerCertificate = state.PeerCertificates[0]
		}
	}

	// Remote address
	if ra, ok := conn.(RemoteAddrTransport); ok {
		ac.RemoteAddr = ra.RemoteAddr()
	}

	// ACSE mechanism and authentication material
	switch authInfo.Mechanism {
	case acse.AuthPassword:
		ac.Mechanism = AuthMechanismACSEPassword
		ac.MechanismOID = copyOID(authInfo.MechanismOID)
		ac.Password = append([]byte(nil), authInfo.Password...)
	case acse.AuthUnknown:
		ac.Mechanism = AuthMechanismUnknown
		ac.MechanismOID = copyOID(authInfo.MechanismOID)
	default:
		if ac.PeerCertificate != nil {
			ac.Mechanism = AuthMechanismTLSCertificate
		} else {
			ac.Mechanism = AuthMechanismNone
		}
	}

	// Calling application reference (defensive copy of all fields)
	if len(authInfo.CallingAPTitle) > 0 {
		var aeq *int
		if authInfo.CallingAEQualifier != nil {
			v := *authInfo.CallingAEQualifier
			aeq = &v
		}
		ac.CallingApplication = &ApplicationReference{
			APTitle:     copyOID(authInfo.CallingAPTitle),
			AEQualifier: aeq,
		}
	}

	return ac
}

func copyOID(oid asn1.ObjectIdentifier) asn1.ObjectIdentifier {
	if oid == nil {
		return nil
	}
	cp := make(asn1.ObjectIdentifier, len(oid))
	copy(cp, oid)
	return cp
}

func (s *Server) registerConn(sc *ServerConn) {
	s.connsMu.Lock()
	s.conns[sc] = struct{}{}
	s.connsMu.Unlock()
}

func (s *Server) unregisterConn(sc *ServerConn) {
	sc.mu.Lock()
	sc.closed = true
	sc.mu.Unlock()

	s.connsMu.Lock()
	delete(s.conns, sc)
	s.connsMu.Unlock()
}

// Connections returns a snapshot of currently active server connections.
// Each [ServerConn] can be used to send unsolicited PDUs.
func (s *Server) Connections() []*ServerConn {
	s.connsMu.RLock()
	defer s.connsMu.RUnlock()
	result := make([]*ServerConn, 0, len(s.conns))
	for sc := range s.conns {
		result = append(result, sc)
	}
	return result
}

// AuthToken returns the opaque security token stored by the
// [Authenticator] during association establishment. Returns nil if no
// authenticator was configured or the authenticator did not set a token.
//
// Upper layers (e.g. go-iec61850) use this to retrieve the principal
// or session context associated with the authenticated peer.
func (sc *ServerConn) AuthToken() any {
	return sc.authToken
}

func (sc *ServerConn) defineAssocNVL(entry *servermodel.NVLEntry) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.assocNVLs == nil {
		sc.assocNVLs = make(map[string]*servermodel.NVLEntry)
	}
	if _, exists := sc.assocNVLs[entry.ItemID]; exists {
		return fmt.Errorf("mms: association NVL %q already defined", entry.ItemID)
	}
	sc.assocNVLs[entry.ItemID] = entry
	sc.assocNVLOrder = append(sc.assocNVLOrder, entry.ItemID)
	sort.Strings(sc.assocNVLOrder)
	return nil
}

func (sc *ServerConn) lookupAssocNVL(itemID string) (*servermodel.NVLEntry, bool) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	e, ok := sc.assocNVLs[itemID]
	return e, ok
}

func (sc *ServerConn) deleteAssocNVL(itemID string) bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	entry, ok := sc.assocNVLs[itemID]
	if !ok || !entry.Deletable {
		return false
	}
	delete(sc.assocNVLs, itemID)
	for i, n := range sc.assocNVLOrder {
		if n == itemID {
			sc.assocNVLOrder = append(sc.assocNVLOrder[:i], sc.assocNVLOrder[i+1:]...)
			break
		}
	}
	return true
}

func (sc *ServerConn) listAssocNVLs(continueAfter string, maxNames int) servermodel.NameListResult {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	if maxNames <= 0 {
		maxNames = 100
	}
	sorted := sc.assocNVLOrder
	start := 0
	if continueAfter != "" {
		for i, name := range sorted {
			if name == continueAfter {
				start = i + 1
				break
			}
		}
	}
	remaining := sorted[start:]
	if len(remaining) > maxNames {
		return servermodel.NameListResult{Names: remaining[:maxNames], MoreFollows: true}
	}
	return servermodel.NameListResult{Names: remaining, MoreFollows: false}
}

func (sc *ServerConn) deleteAllAssocNVLs() (matched, deleted int) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	for _, entry := range sc.assocNVLs {
		matched++
		if entry.Deletable {
			deleted++
		}
	}
	if deleted > 0 {
		sc.assocNVLs = make(map[string]*servermodel.NVLEntry)
		sc.assocNVLOrder = nil
	}
	return matched, deleted
}

// Abort sends a protocol-level MMS Abort PDU (ACSE ABRT wrapped in a
// Session ABORT SPDU) to the connected client. This is a best-effort
// send — the caller should close the transport regardless of the error.
// Returns [ErrServerConnClosed] if the connection was already closed.
func (sc *ServerConn) Abort(ctx context.Context) error {
	sc.mu.RLock()
	if sc.closed {
		sc.mu.RUnlock()
		return ErrServerConnClosed
	}
	sc.mu.RUnlock()

	return sc.conn.SendAbort(ctx)
}

// SendUnsolicitedStatus sends an UnsolicitedStatus unconfirmed PDU to
// the connected client. This provides the client with VMD status
// without a preceding Status request.
// Returns [ErrServerConnClosed] if the connection has been closed.
func (sc *ServerConn) SendUnsolicitedStatus(ctx context.Context, status ServerStatus) error {
	sc.mu.RLock()
	if sc.closed {
		sc.mu.RUnlock()
		return ErrServerConnClosed
	}
	sc.mu.RUnlock()

	mmsPdu, err := pdu.MarshalUnsolicitedStatus(int(status.Logical), int(status.Physical))
	if err != nil {
		return fmt.Errorf("mms: marshal unsolicited status: %w", err)
	}
	return sc.conn.SendUnconfirmed(ctx, mmsPdu)
}

// SendInformationReport sends an InformationReport to a specific
// connected client. Returns [ErrServerConnClosed] if the connection
// has been closed.
func (sc *ServerConn) SendInformationReport(ctx context.Context, req *InformationReportRequest) error {
	sc.mu.RLock()
	if sc.closed {
		sc.mu.RUnlock()
		return ErrServerConnClosed
	}
	sc.mu.RUnlock()

	wire, err := infoReportRequestToWire(req)
	if err != nil {
		return fmt.Errorf("mms: marshal info report: %w", err)
	}
	mmsPdu, err := pdu.MarshalInformationReport(wire)
	if err != nil {
		return fmt.Errorf("mms: marshal info report PDU: %w", err)
	}
	return sc.conn.SendUnconfirmed(ctx, mmsPdu)
}

// Broadcast sends an InformationReport to all currently connected clients.
// Errors from individual connections are logged but do not stop delivery
// to remaining connections.
func (s *Server) Broadcast(ctx context.Context, req *InformationReportRequest) error {
	wire, err := infoReportRequestToWire(req)
	if err != nil {
		return fmt.Errorf("mms: marshal info report: %w", err)
	}
	mmsPdu, err := pdu.MarshalInformationReport(wire)
	if err != nil {
		return fmt.Errorf("mms: marshal info report PDU: %w", err)
	}

	s.connsMu.RLock()
	snapshot := make([]*ServerConn, 0, len(s.conns))
	for sc := range s.conns {
		snapshot = append(snapshot, sc)
	}
	s.connsMu.RUnlock()

	var firstErr error
	for _, sc := range snapshot {
		sc.mu.RLock()
		closed := sc.closed
		sc.mu.RUnlock()
		if closed {
			continue
		}
		if err := sc.conn.SendUnconfirmed(ctx, mmsPdu); err != nil {
			s.logger.Warn("mms: broadcast send failed", "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func infoReportRequestToWire(req *InformationReportRequest) (*pdu.InformationReportWire, error) {
	if req == nil {
		return nil, fmt.Errorf("nil InformationReportRequest")
	}

	hasListName := req.ListName != nil
	hasVars := len(req.Variables) > 0

	switch {
	case hasListName && hasVars:
		return nil, fmt.Errorf("InformationReportRequest: ListName and Variables are mutually exclusive")
	case !hasListName && !hasVars:
		return nil, fmt.Errorf("InformationReportRequest: one of ListName or Variables is required")
	case !hasListName && len(req.Variables) != len(req.Values):
		return nil, fmt.Errorf("InformationReportRequest: len(Variables)=%d != len(Values)=%d", len(req.Variables), len(req.Values))
	}

	if len(req.Values) == 0 {
		return nil, fmt.Errorf("InformationReportRequest: at least one Value is required")
	}

	wire := &pdu.InformationReportWire{}

	if hasListName {
		wn := objectNameToWire(*req.ListName)
		wire.ListName = &wn
	}

	for _, v := range req.Variables {
		wire.Variables = append(wire.Variables, objectNameToWire(v))
	}

	for i, val := range req.Values {
		dv, err := valueToDataValue(val)
		if err != nil {
			return nil, fmt.Errorf("value [%d]: %w", i, err)
		}
		wire.Values = append(wire.Values, dv)
	}

	return wire, nil
}

// ListenAndServe accepts connections from the given listener and serves
// each one in a new goroutine. It blocks until the context is cancelled
// or the listener returns a fatal (non-temporary) error.
//
// Ownership:
//   - ListenAndServe takes ownership of the listener and closes it on
//     return. The caller must not close the listener separately.
//   - Each accepted transport is closed automatically when its Serve
//     goroutine finishes (via deferred Close).
//
// Each accepted connection is handled by [Server.Serve] in its own
// goroutine. Connection-level serve errors are logged but do not stop
// the accept loop. Temporary accept errors (e.g. transient network
// issues) are logged and retried. Fatal accept errors stop the server.
func (s *Server) ListenAndServe(ctx context.Context, ln TransportListener) error {
	s.logger.Info("server listening", "addr", ln.Addr())
	defer func() { _ = ln.Close() }()

	for {
		conn, err := ln.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if isTemporary(err) {
				s.logger.Warn("temporary accept error, retrying", "error", err)
				continue
			}
			return fmt.Errorf("mms: accept: %w", err)
		}

		go func(c Transport) {
			defer func() { _ = c.Close() }()
			if err := s.Serve(ctx, c); err != nil {
				s.logger.Info("connection closed", "error", err)
			}
		}(conn)
	}
}

func isTemporary(err error) bool {
	type temporary interface{ Temporary() bool }
	var t temporary
	if errors.As(err, &t) {
		return t.Temporary()
	}
	return false
}

// dispatch routes a confirmed service request to the appropriate handler.
// serviceTag is the CHOICE tag number from the confirmed request PDU.
func (s *Server) dispatch(ctx context.Context, invokeID codec.InvokeID, serviceTag int, serviceBody []byte) (int, bool, []byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	switch serviceTag {
	case asn1util.TagNumIdentify:
		return s.handleIdentify(ctx)
	case asn1util.TagNumStatus:
		return s.handleStatus(ctx, serviceBody)
	case asn1util.TagNumGetNameList:
		return s.handleGetNameList(ctx, serviceBody)
	case asn1util.TagNumGetVariableAccessAttributes:
		return s.handleGetVarAccess(ctx, serviceBody)
	case asn1util.TagNumRead:
		return s.handleRead(ctx, serviceBody)
	case asn1util.TagNumWrite:
		return s.handleWrite(ctx, serviceBody)
	case asn1util.TagNumDefineNamedVariableList:
		return s.handleDefineNVL(ctx, serviceBody)
	case asn1util.TagNumGetNamedVariableListAttrs:
		return s.handleGetNVLAttrs(ctx, serviceBody)
	case asn1util.TagNumDeleteNamedVariableList:
		return s.handleDeleteNVL(ctx, serviceBody)
	case asn1util.TagNumFileOpen:
		return s.handleFileOpen(ctx, serviceBody)
	case asn1util.TagNumFileRead:
		return s.handleFileRead(ctx, serviceBody)
	case asn1util.TagNumFileClose:
		return s.handleFileClose(ctx, serviceBody)
	case asn1util.TagNumFileRename:
		return s.handleFileRename(ctx, serviceBody)
	case asn1util.TagNumFileDelete:
		return s.handleFileDelete(ctx, serviceBody)
	case asn1util.TagNumFileDirectory:
		return s.handleFileDirectory(ctx, serviceBody)
	case asn1util.TagNumObtainFile:
		return s.handleObtainFile(ctx, serviceBody)
	case asn1util.TagNumReadJournal:
		return s.handleReadJournal(ctx, serviceBody)
	default:
		return 0, false, nil, errUnsupportedFeature
	}
}

// --- Identify ---

func (s *Server) handleIdentify(ctx context.Context) (int, bool, []byte, error) {
	h := s.identifyHandler
	if h == nil {
		return 0, false, nil, errServiceUnsupported
	}
	result, err := h(ctx, IdentifyRequest{})
	if err != nil {
		return 0, false, nil, err
	}

	type identifyResp struct {
		VendorName string `asn1:"tag:0,implicit,ia5"`
		ModelName  string `asn1:"tag:1,implicit,ia5"`
		Revision   string `asn1:"tag:2,implicit,ia5"`
	}
	payload, err := asn1.Marshal(identifyResp{
		VendorName: result.Vendor,
		ModelName:  result.Model,
		Revision:   result.Revision,
	})
	if err != nil {
		return 0, false, nil, fmt.Errorf("marshal identify response: %w", err)
	}
	return asn1util.TagNumIdentify, true, payload, nil
}

// --- Status ---

//nolint:unparam // TagNumStatus is 0; signature required by dispatch pattern
func (s *Server) handleStatus(ctx context.Context, body []byte) (int, bool, []byte, error) {
	h := s.statusHandler
	if h == nil {
		return 0, false, nil, errServiceUnsupported
	}
	if len(body) != 1 {
		return 0, false, nil, &serverconn.ServiceError{ErrorClass: 1, ErrorCode: 0} // resource: other
	}
	req := StatusRequest{ExtendedDerivation: body[0] != 0}
	result, err := h(ctx, req)
	if err != nil {
		return 0, false, nil, err
	}

	type statusResp struct {
		VMDLogicalStatus  int `asn1:"tag:0,implicit"`
		VMDPhysicalStatus int `asn1:"tag:1,implicit"`
	}
	payload, err := asn1.Marshal(statusResp{
		VMDLogicalStatus:  int(result.Logical),
		VMDPhysicalStatus: int(result.Physical),
	})
	if err != nil {
		return 0, false, nil, fmt.Errorf("marshal status response: %w", err)
	}
	return asn1util.TagNumStatus, true, payload, nil
}

// --- GetNameList ---

// handleGetNameList serves the GetNameList service.
//
// Supported combinations (strict matrix):
//
//	ObjectClassDomain           + ScopeVMD    → list domains
//	ObjectClassNamedVariable    + ScopeDomain → list domain variables
//	ObjectClassNamedVariable    + ScopeVMD    → list VMD-scoped variables
//	ObjectClassNamedVariableList + ScopeDomain → list domain NVLs
//	ObjectClassNamedVariableList + ScopeVMD    → list VMD-scoped NVLs
//
// All other combinations are rejected with a service error.
func (s *Server) handleGetNameList(ctx context.Context, body []byte) (int, bool, []byte, error) {
	req, err := pdu.UnmarshalGetNameListRequest(body)
	if err != nil {
		return 0, false, nil, errInvalidRequest
	}

	// Journal names come from the JournalProvider, not the registry.
	if req.ObjectClass == int(ObjectClassJournal) && req.Scope == pdu.ScopeDomain {
		return s.handleGetNameListJournal(ctx, req)
	}

	var result servermodel.NameListResult
	switch {
	case req.ObjectClass == int(ObjectClassDomain) && req.Scope == pdu.ScopeVMD:
		result = s.registry.ListDomains(req.ContinueAfter, 0)
	case req.ObjectClass == int(ObjectClassNamedVariable) && req.Scope == pdu.ScopeDomain:
		result = s.registry.ListDomainVariables(req.DomainID, req.ContinueAfter, 0)
	case req.ObjectClass == int(ObjectClassNamedVariable) && req.Scope == pdu.ScopeVMD:
		result = s.registry.ListVMDVariables(req.ContinueAfter, 0)
	case req.ObjectClass == int(ObjectClassNamedVariableList) && req.Scope == pdu.ScopeDomain:
		result = s.registry.ListDomainNVLs(req.DomainID, req.ContinueAfter, 0)
	case req.ObjectClass == int(ObjectClassNamedVariableList) && req.Scope == pdu.ScopeVMD:
		result = s.registry.ListVMDNVLs(req.ContinueAfter, 0)
	case req.ObjectClass == int(ObjectClassNamedVariableList) && req.Scope == pdu.ScopeAssociation:
		sc, _ := ctx.Value(serverConnCtxKey{}).(*ServerConn)
		if sc == nil {
			return 0, false, nil, errUnsupportedFeature
		}
		result = sc.listAssocNVLs(req.ContinueAfter, 0)
	default:
		return 0, false, nil, errUnsupportedFeature
	}

	respBytes, err := pdu.MarshalGetNameListResponse(result.Names, result.MoreFollows)
	if err != nil {
		return 0, false, nil, fmt.Errorf("marshal getnamelist response: %w", err)
	}
	return asn1util.TagNumGetNameList, true, respBytes, nil
}

func (s *Server) handleGetNameListJournal(ctx context.Context, req *pdu.GetNameListRequest) (int, bool, []byte, error) {
	if s.journalProvider == nil {
		return 0, false, nil, errServiceUnsupported
	}

	names, err := s.journalProvider.ListJournals(ctx, req.DomainID)
	if err != nil {
		return 0, false, nil, journalError(err)
	}

	if req.ContinueAfter != "" {
		found := false
		for i, n := range names {
			if n == req.ContinueAfter {
				names = names[i+1:]
				found = true
				break
			}
		}
		if !found {
			return 0, false, nil, errInvalidRequest
		}
	}

	respBytes, err := pdu.MarshalGetNameListResponse(names, false)
	if err != nil {
		return 0, false, nil, fmt.Errorf("marshal getnamelist response: %w", err)
	}
	return asn1util.TagNumGetNameList, true, respBytes, nil
}

// --- GetVariableAccessAttributes ---

func (s *Server) handleGetVarAccess(_ context.Context, body []byte) (int, bool, []byte, error) {
	wireName, err := pdu.UnmarshalGetVarAccessRequest(body)
	if err != nil {
		return 0, false, nil, errInvalidRequest
	}

	entry, ok := s.registry.LookupVariable(wireName.Scope, wireName.DomainID, wireName.ItemID)
	if !ok {
		return 0, false, nil, errObjectNonExistent
	}

	ts, ok := entry.TypeSpec.(TypeSpec)
	if !ok {
		return 0, false, nil, errAccessDenied
	}

	wireTS, err := typeSpecToWire(ts)
	if err != nil {
		return 0, false, nil, &serverconn.ServiceError{ErrorClass: 7, ErrorCode: 1}
	}
	respBytes, err := pdu.MarshalGetVarAccessResponse(entry.Deletable, wireTS)
	if err != nil {
		return 0, false, nil, fmt.Errorf("marshal getvaraccess response: %w", err)
	}
	return asn1util.TagNumGetVariableAccessAttributes, true, respBytes, nil
}

// --- Read ---

func (s *Server) handleRead(ctx context.Context, body []byte) (int, bool, []byte, error) {
	req, err := pdu.UnmarshalReadRequestParsed(body)
	if err != nil {
		return 0, false, nil, errInvalidRequest
	}

	var specs []pdu.VariableSpecWire
	if req.ListName != nil {
		resolved, err := s.resolveNVLMembers(ctx, req.ListName)
		if err != nil {
			return 0, false, nil, err
		}
		specs = append(specs, resolved...)
	} else {
		specs = req.Variables
	}

	var accessResults []*pdu.AccessResult
	for _, spec := range specs {
		wn := spec.Name
		entry, ok := s.registry.LookupVariable(wn.Scope, wn.DomainID, wn.ItemID)
		if !ok {
			accessResults = append(accessResults, &pdu.AccessResult{IsError: true, ErrorCode: wireErrObjectUndefined})
			continue
		}

		readFn, ok := entry.ReadFunc.(func(context.Context) (*Value, error))
		if !ok || readFn == nil {
			accessResults = append(accessResults, &pdu.AccessResult{IsError: true, ErrorCode: wireErrAccessDenied})
			continue
		}

		val, err := readFn(ctx)
		if err != nil {
			accessResults = append(accessResults, &pdu.AccessResult{IsError: true, ErrorCode: wireErrTempUnavail})
			continue
		}

		if len(spec.AlternateAccess) > 0 {
			ts := extractTypeSpec(entry.TypeSpec)
			val = applyAlternateAccessRead(val, ts, spec.AlternateAccess)
			if val == nil {
				accessResults = append(accessResults, &pdu.AccessResult{IsError: true, ErrorCode: wireErrObjectUndefined})
				continue
			}
		}

		dv, err := valueToDataValue(val)
		if err != nil {
			accessResults = append(accessResults, &pdu.AccessResult{IsError: true, ErrorCode: wireErrTempUnavail})
			continue
		}
		accessResults = append(accessResults, &pdu.AccessResult{IsError: false, Data: dv})
	}

	var respBytes []byte
	if req.SpecWithResult {
		respBytes, err = pdu.MarshalReadResponseWithSpec(req.ListName, specs, accessResults)
	} else {
		respBytes, err = pdu.MarshalReadResponse(accessResults)
	}
	if err != nil {
		return 0, false, nil, fmt.Errorf("marshal read response: %w", err)
	}
	return asn1util.TagNumRead, true, respBytes, nil
}

// --- Write ---

func (s *Server) handleWrite(ctx context.Context, body []byte) (int, bool, []byte, error) {
	req, err := pdu.UnmarshalWriteRequestParsed(body)
	if err != nil {
		return 0, false, nil, errInvalidRequest
	}

	var specs []pdu.VariableSpecWire
	if req.ListName != nil {
		resolved, err := s.resolveNVLMembers(ctx, req.ListName)
		if err != nil {
			return 0, false, nil, err
		}
		specs = append(specs, resolved...)
	} else {
		specs = req.Variables
	}

	wireData := req.Values

	var results []int // 0=success, >0=data-access-error code
	for i, spec := range specs {
		if i >= len(wireData) {
			results = append(results, wireErrTypeInconsistent)
			continue
		}
		wn := spec.Name
		entry, ok := s.registry.LookupVariable(wn.Scope, wn.DomainID, wn.ItemID)
		if !ok {
			results = append(results, wireErrObjectUndefined)
			continue
		}

		writeFn, ok := entry.WriteFunc.(func(context.Context, *Value) error)
		if !ok || writeFn == nil {
			results = append(results, wireErrAccessDenied)
			continue
		}

		val, err := dataValueToValue(wireData[i])
		if err != nil {
			results = append(results, wireErrTypeInconsistent)
			continue
		}

		if len(spec.AlternateAccess) > 0 {
			readFn, readOK := entry.ReadFunc.(func(context.Context) (*Value, error))
			if !readOK || readFn == nil {
				results = append(results, wireErrAccessDenied)
				continue
			}
			currentVal, readErr := readFn(ctx)
			if readErr != nil {
				results = append(results, wireErrTempUnavail)
				continue
			}
			ts := extractTypeSpec(entry.TypeSpec)
			val = applyAlternateAccessWrite(currentVal, ts, spec.AlternateAccess, val)
			if val == nil {
				results = append(results, wireErrTypeInconsistent)
				continue
			}
		}

		if err := writeFn(ctx, val); err != nil {
			results = append(results, wireErrTempUnavail)
			continue
		}
		results = append(results, 0)
	}

	respBytes, err := pdu.MarshalWriteResponse(results)
	if err != nil {
		return 0, false, nil, fmt.Errorf("marshal write response: %w", err)
	}
	return asn1util.TagNumWrite, true, respBytes, nil
}

// extractTypeSpec returns the TypeSpec stored in a VarEntry.TypeSpec
// field, handling both value and pointer types.
func extractTypeSpec(ts any) *TypeSpec {
	switch v := ts.(type) {
	case *TypeSpec:
		return v
	case TypeSpec:
		return &v
	default:
		return nil
	}
}

// resolveNVLMembers resolves a variableListName to the member variables.
func (s *Server) resolveNVLMembers(ctx context.Context, listName *pdu.ObjectNameWire) ([]pdu.VariableSpecWire, error) {
	var entry *servermodel.NVLEntry
	var ok bool
	if listName.Scope == pdu.ScopeAssociation {
		if sc, _ := ctx.Value(serverConnCtxKey{}).(*ServerConn); sc != nil {
			entry, ok = sc.lookupAssocNVL(listName.ItemID)
		}
	} else {
		entry, ok = s.registry.LookupNVL(listName.Scope, listName.DomainID, listName.ItemID)
	}
	if !ok {
		return nil, errObjectNonExistent
	}
	var members []pdu.VariableSpecWire
	for _, v := range entry.Variables {
		spec := pdu.VariableSpecWire{
			Name: pdu.ObjectNameWire{
				Scope:    v.Scope,
				DomainID: v.DomainID,
				ItemID:   v.ItemID,
			},
		}
		for _, sel := range v.AlternateAccess {
			ws := pdu.AccessSelectorWire{
				Component: sel.Component,
				HasIndex:  sel.HasIndex,
				Index:     sel.Index,
			}
			if sel.IndexRange != nil {
				ws.IndexRange = &pdu.IndexRangeWire{
					LowIndex:         sel.IndexRange.LowIndex,
					NumberOfElements: sel.IndexRange.NumberOfElements,
				}
			}
			spec.AlternateAccess = append(spec.AlternateAccess, ws)
		}
		members = append(members, spec)
	}
	return members, nil
}

// --- DefineNamedVariableList ---

//nolint:unparam // result []byte is always nil; signature matches the handler contract
func (s *Server) handleDefineNVL(ctx context.Context, body []byte) (int, bool, []byte, error) {
	req, err := pdu.UnmarshalDefineNVLRequest(body)
	if err != nil {
		return 0, false, nil, errInvalidRequest
	}

	vars := make([]servermodel.NVLVariable, len(req.Variables))
	for i, v := range req.Variables {
		nv := servermodel.NVLVariable{
			Scope:    v.Name.Scope,
			DomainID: v.Name.DomainID,
			ItemID:   v.Name.ItemID,
		}
		for _, sel := range v.AlternateAccess {
			sm := servermodel.AccessSelectorModel{
				Component: sel.Component,
				HasIndex:  sel.HasIndex,
				Index:     sel.Index,
			}
			if sel.IndexRange != nil {
				sm.IndexRange = &servermodel.IndexRangeModel{
					LowIndex:         sel.IndexRange.LowIndex,
					NumberOfElements: sel.IndexRange.NumberOfElements,
				}
			}
			nv.AlternateAccess = append(nv.AlternateAccess, sm)
		}
		vars[i] = nv
	}

	entry := &servermodel.NVLEntry{
		Domain:    req.ListName.DomainID,
		ItemID:    req.ListName.ItemID,
		Scope:     req.ListName.Scope,
		Deletable: true,
		Variables: vars,
	}

	if req.ListName.Scope == pdu.ScopeAssociation {
		sc, _ := ctx.Value(serverConnCtxKey{}).(*ServerConn)
		if sc == nil {
			return 0, false, nil, errUnsupportedFeature
		}
		if err := sc.defineAssocNVL(entry); err != nil {
			return 0, false, nil, errObjectNonExistent
		}
	} else {
		if err := s.registry.DefineNVL(entry); err != nil {
			return 0, false, nil, errObjectNonExistent
		}
	}

	return asn1util.TagNumDefineNamedVariableList, true, nil, nil
}

// --- GetNamedVariableListAttributes ---

func (s *Server) handleGetNVLAttrs(ctx context.Context, body []byte) (int, bool, []byte, error) {
	req, err := pdu.UnmarshalGetNVLAttrsRequest(body)
	if err != nil {
		return 0, false, nil, errInvalidRequest
	}

	var entry *servermodel.NVLEntry
	var ok bool
	if req.ListName.Scope == pdu.ScopeAssociation {
		if sc, _ := ctx.Value(serverConnCtxKey{}).(*ServerConn); sc != nil {
			entry, ok = sc.lookupAssocNVL(req.ListName.ItemID)
		}
	} else {
		entry, ok = s.registry.LookupNVL(req.ListName.Scope, req.ListName.DomainID, req.ListName.ItemID)
	}
	if !ok {
		return 0, false, nil, errObjectNonExistent
	}

	wireVars := make([]pdu.VariableSpecWire, len(entry.Variables))
	for i, v := range entry.Variables {
		spec := pdu.VariableSpecWire{
			Name: pdu.ObjectNameWire{
				Scope:    v.Scope,
				DomainID: v.DomainID,
				ItemID:   v.ItemID,
			},
		}
		for _, sel := range v.AlternateAccess {
			ws := pdu.AccessSelectorWire{
				Component: sel.Component,
				HasIndex:  sel.HasIndex,
				Index:     sel.Index,
			}
			if sel.IndexRange != nil {
				ws.IndexRange = &pdu.IndexRangeWire{
					LowIndex:         sel.IndexRange.LowIndex,
					NumberOfElements: sel.IndexRange.NumberOfElements,
				}
			}
			spec.AlternateAccess = append(spec.AlternateAccess, ws)
		}
		wireVars[i] = spec
	}

	respBytes, err := pdu.MarshalGetNVLAttrsResponse(entry.Deletable, wireVars)
	if err != nil {
		return 0, false, nil, fmt.Errorf("marshal get-nvl-attrs response: %w", err)
	}
	return asn1util.TagNumGetNamedVariableListAttrs, true, respBytes, nil
}

// --- DeleteNamedVariableList ---

func (s *Server) handleDeleteNVL(ctx context.Context, body []byte) (int, bool, []byte, error) {
	req, err := pdu.UnmarshalDeleteNVLRequest(body)
	if err != nil {
		return 0, false, nil, errInvalidRequest
	}

	var matched, deleted int

	switch req.ScopeOfDelete {
	case 0: // specific list names
		for _, n := range req.ListNames {
			if n.Scope == pdu.ScopeAssociation {
				sc, _ := ctx.Value(serverConnCtxKey{}).(*ServerConn)
				if sc == nil {
					continue
				}
				if _, ok := sc.lookupAssocNVL(n.ItemID); ok {
					matched++
					if sc.deleteAssocNVL(n.ItemID) {
						deleted++
					}
				}
			} else {
				if _, ok := s.registry.LookupNVL(n.Scope, n.DomainID, n.ItemID); ok {
					matched++
					if s.registry.DeleteNVL(n.Scope, n.DomainID, n.ItemID) {
						deleted++
					}
				}
			}
		}
	case 1: // aa-specific: delete all association-scope NVLs for this connection
		sc, _ := ctx.Value(serverConnCtxKey{}).(*ServerConn)
		if sc == nil {
			return 0, false, nil, errUnsupportedFeature
		}
		matched, deleted = sc.deleteAllAssocNVLs()
	default:
		// domain (2) and vmd (3) bulk scope not implemented (matches C reference)
		return 0, false, nil, errUnsupportedFeature
	}

	respBytes, err := pdu.MarshalDeleteNVLResponse(matched, deleted)
	if err != nil {
		return 0, false, nil, fmt.Errorf("marshal delete-nvl response: %w", err)
	}
	return asn1util.TagNumDeleteNamedVariableList, true, respBytes, nil
}

// typeSpecToWire converts a public TypeSpec to an internal wire representation.
// Returns an error for unsupported or invalid type specifications.
func typeSpecToWire(ts TypeSpec) (pdu.TypeSpecWire, error) {
	switch ts.Type {
	case ValueTypeBoolean:
		return pdu.TypeSpecWire{Tag: 3}, nil
	case ValueTypeInteger:
		return pdu.TypeSpecWire{Tag: 5, Size: ts.Size}, nil
	case ValueTypeUnsigned:
		return pdu.TypeSpecWire{Tag: 6, Size: ts.Size}, nil
	case ValueTypeFloat:
		return pdu.TypeSpecWire{Tag: 7, FormatWidth: ts.FormatWidth, ExpWidth: ts.ExponentWidth}, nil
	case ValueTypeBitString:
		return pdu.TypeSpecWire{Tag: 4, Size: ts.Size}, nil
	case ValueTypeOctetString:
		return pdu.TypeSpecWire{Tag: 9, Size: ts.Size}, nil
	case ValueTypeVisibleString:
		return pdu.TypeSpecWire{Tag: 10, Size: ts.Size}, nil
	case ValueTypeMmsString:
		return pdu.TypeSpecWire{Tag: 16, Size: ts.Size}, nil
	case ValueTypeUTCTime:
		return pdu.TypeSpecWire{Tag: 17}, nil
	case ValueTypeBinaryTime:
		return pdu.TypeSpecWire{Tag: 12}, nil
	case ValueTypeStructure:
		comps := make([]pdu.StructComponentWire, len(ts.Elements))
		for i, e := range ts.Elements {
			ct, err := typeSpecToWire(e.Type)
			if err != nil {
				return pdu.TypeSpecWire{}, fmt.Errorf("structure element %q: %w", e.Name, err)
			}
			comps[i] = pdu.StructComponentWire{Name: e.Name, Type: ct}
		}
		return pdu.TypeSpecWire{Tag: 2, Components: comps}, nil
	case ValueTypeArray:
		if ts.Element != nil {
			elem, err := typeSpecToWire(*ts.Element)
			if err != nil {
				return pdu.TypeSpecWire{}, fmt.Errorf("array element: %w", err)
			}
			return pdu.TypeSpecWire{Tag: 1, Count: ts.Count, Element: &elem}, nil
		}
		return pdu.TypeSpecWire{Tag: 1, Count: ts.Count}, nil
	case ValueTypeNamedType:
		if ts.TypeName == nil {
			return pdu.TypeSpecWire{}, fmt.Errorf("named type with nil TypeName")
		}
		if err := validateObjectName(*ts.TypeName); err != nil {
			return pdu.TypeSpecWire{}, fmt.Errorf("named type: %w", err)
		}
		wireScope, err := objectScopeToWire(ts.TypeName.Scope)
		if err != nil {
			return pdu.TypeSpecWire{}, fmt.Errorf("named type scope: %w", err)
		}
		wn := pdu.ObjectNameWire{
			Scope:    wireScope,
			DomainID: string(ts.TypeName.Domain),
			ItemID:   string(ts.TypeName.ItemID),
		}
		return pdu.TypeSpecWire{Tag: 0, TypeName: &wn}, nil
	default:
		return pdu.TypeSpecWire{}, fmt.Errorf("unsupported TypeSpec type %s for wire encoding", ts.Type)
	}
}
