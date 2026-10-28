// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"encoding/asn1"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

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
	serviceErrorClassVMD      = int(ErrorClassVMDState)
	serviceErrorClassResource = int(ErrorClassResource)
	serviceErrorClassService  = int(ErrorClassService)
	serviceErrorClassAccess   = int(ErrorClassAccess)

	// Confirmed service-error codes within their class.
	// Access-class codes match the mms-extended.asn1 AccessError ENUMERATED:
	//   other(0), objectaccessunsupported(1), objectnonexistent(2),
	//   objectaccessdenied(3), objectinvalidated(4)
	svcErrOther               = 0 // generic "other"
	svcErrObjectAccessUnsupp  = 1 // access: object-access-unsupported
	svcErrObjectNonExistent   = 2 // access: object-non-existent
	svcErrObjectAccessDenied  = 3 // access: object-access-denied
	svcErrServiceUnsupported  = 4 // vmd-state: other (no handler)
	svcErrServiceNotSupported = 5 // vmd-state: service-not-supported
)

var (
	errServiceUnsupported = &serverconn.ServiceError{ErrorClass: serviceErrorClassVMD, ErrorCode: svcErrServiceUnsupported}
	errObjectNonExistent  = &serverconn.ServiceError{ErrorClass: serviceErrorClassAccess, ErrorCode: svcErrObjectNonExistent}
	errAccessDenied       = &serverconn.ServiceError{ErrorClass: serviceErrorClassAccess, ErrorCode: svcErrObjectAccessDenied}
	errInvalidRequest     = &serverconn.ServiceError{ErrorClass: serviceErrorClassService, ErrorCode: svcErrOther}
	errUnsupportedFeature = &serverconn.ServiceError{ErrorClass: serviceErrorClassVMD, ErrorCode: svcErrServiceNotSupported}
	errAccessUnsupported  = &serverconn.ServiceError{ErrorClass: serviceErrorClassAccess, ErrorCode: svcErrObjectAccessUnsupp}
	errResourceOther      = &serverconn.ServiceError{ErrorClass: serviceErrorClassResource, ErrorCode: svcErrOther}
)

// Per-variable data-access error codes for Read/Write result lists.
// These use the public DataAccessErrorCode constants and are independent
// of the ConfirmedErrorPDU service-error codes above.
const (
	wireErrObjectUndefined   = DataAccessErrorObjectUndefined
	wireErrObjectNonExistent = DataAccessErrorObjectNonExistent
	wireErrAccessDenied      = DataAccessErrorObjectAccessDenied
	wireErrTempUnavail       = DataAccessErrorTemporarilyUnavailable
	wireErrTypeInconsistent  = DataAccessErrorTypeInconsistent
)

// callbackErrCode maps an error from a Variable Read or Write callback to
// the wire DataAccessError code: the code of a wrapped [*DataAccessError]
// when it is valid, temporarily-unavailable otherwise.
func callbackErrCode(err error) int {
	var dae *DataAccessError
	if errors.As(err, &dae) {
		if wire, encErr := encodeDataAccessError(dae.Code); encErr == nil {
			return wire
		}
	}
	return wireErrCode(wireErrTempUnavail)
}

// wireErrCode converts a DataAccessErrorCode to its validated MMS wire integer.
// It panics on DataAccessErrorNone or any out-of-range code, making accidental
// sentinel serialization a development-time crash rather than a silent wire error.
func wireErrCode(code DataAccessErrorCode) int {
	wire, err := encodeDataAccessError(code)
	if err != nil {
		panic(fmt.Sprintf("mms: server: invalid DataAccessErrorCode in response: %v", err))
	}
	return wire
}

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

	metrics      Metrics
	assocTimeout time.Duration

	connsMu   sync.RWMutex
	conns     map[*ServerConn]struct{}
	listeners map[TransportListener]struct{}
	shutdown  bool
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
	busy      chan struct{} // held while a confirmed request is handled
	remote    string
	conn      *serverconn.Conn
	authToken any
	frsmTable *frsmTable

	assocNVLs     map[string]*servermodel.NVLEntry
	assocNVLOrder []string

	assocVars     map[string]*servermodel.VarEntry
	assocVarOrder []string
}

// NewServer creates a new MMS server with the given options.
func NewServer(opts ServerOptions) *Server {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(discardHandler{})
	}
	assocTimeout := opts.AssociationTimeout
	if assocTimeout == 0 {
		assocTimeout = defaultAssociationTimeout
	}
	return &Server{
		logger:          logger.With("role", RoleServer.String()),
		mmsOpts:         opts.MMS.withDefaults(),
		registry:        servermodel.NewRegistry(),
		authenticator:   opts.Authenticate,
		fileProvider:    opts.FileProvider,
		journalProvider: opts.JournalProvider,
		metrics:         metricsOrNop(opts.Metrics),
		assocTimeout:    assocTimeout,
		conns:           make(map[*ServerConn]struct{}),
		listeners:       make(map[TransportListener]struct{}),
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
//
// For association-scoped variables, use [ServerConn.RegisterVariable]
// instead — association-scope variables are per-connection and cannot
// be registered on the shared server model.
func (s *Server) RegisterVariable(v Variable) error {
	if err := validateObjectName(v.Name); err != nil {
		return invalidArgf("mms: register variable: %w", err)
	}
	if v.Name.Scope == ObjectScopeAssociation {
		return invalidArgf("mms: register variable %q: use ServerConn.RegisterVariable for association-scope variables", v.Name.ItemID)
	}
	if _, err := typeSpecToWire(v.TypeSpec); err != nil {
		return invalidArgf("mms: register variable %q: invalid type spec: %w", v.Name.ItemID, err)
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

// SetVariableRead replaces the read handler of a previously registered
// domain-scoped variable. This is intended for post-registration
// configuration, such as installing control-model-specific read handlers
// on CO attributes after the server model has been fully built.
//
// Returns an error if no domain-scoped variable with the given domain
// and itemID exists.
func (s *Server) SetVariableRead(domain, itemID string, fn func(context.Context) (*Value, error)) error {
	entry, ok := s.registry.LookupVariable(int(ObjectScopeDomain), domain, itemID)
	if !ok {
		return invalidArgf("mms: SetVariableRead: variable %s/%s not found", domain, itemID)
	}
	entry.ReadFunc = fn
	return nil
}

// RegisterNamedVariableList adds a static named variable list to the
// server's MMS object model. The list's domain (if domain-scoped) must
// already be registered via [Server.RegisterDomain].
func (s *Server) RegisterNamedVariableList(nvl NamedVariableList) error {
	if err := validateObjectName(nvl.Name); err != nil {
		return invalidArgf("mms: register NVL: %w", err)
	}
	if len(nvl.Variables) == 0 {
		return invalidArgf("mms: register NVL %q: no variables", nvl.Name.ItemID)
	}

	vars := make([]servermodel.NVLVariable, len(nvl.Variables))
	for i, v := range nvl.Variables {
		if err := validateObjectName(v.Name); err != nil {
			return invalidArgf("mms: register NVL %q: variable [%d]: %w", nvl.Name.ItemID, i, err)
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
// until the client concludes, releases or disconnects, or an error
// occurs. Serve returns nil after an orderly Conclude or Release.
//
// The corresponding [ServerConn] is registered for the duration of
// Serve and can be obtained via [Server.Connections].
//
// The caller owns the transport, with three exceptions in which Serve
// closes it to unblock a pending read: ctx is cancelled (Serve then
// returns an error matching ctx.Err()), the association is not
// established within [ServerOptions.AssociationTimeout], or
// [Server.Shutdown] aborts the association. [Server.ListenAndServe]
// closes every accepted transport when its Serve returns.
//
// Serve blocks until the connection is complete. For serving multiple
// connections, call Serve in a goroutine per accepted transport.
func (s *Server) Serve(ctx context.Context, conn Transport) error {
	remote := remoteAddrOf(conn)
	logger := s.logger
	if remote != "" {
		logger = logger.With("remote_addr", remote)
	}
	if s.isShutdown() {
		return ErrServerClosed
	}

	sc := &ServerConn{busy: make(chan struct{}, 1), remote: remote, frsmTable: newFRSMTable()}
	c := serverconn.New(conn, logger, s.handler(sc, logger), serverconn.MMSOptions{
		MaxPDUSize:                s.mmsOpts.MaxPDUSize,
		MaxOutstandingCalling:     s.mmsOpts.MaxOutstandingCalling,
		MaxOutstandingCalled:      s.mmsOpts.MaxOutstandingCalled,
		DataStructureNestingLevel: s.mmsOpts.DataStructureNestingLevel,
	})
	sc.conn = c

	// Cancelling ctx closes the transport so a blocked Receive returns.
	stopOnCancel := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopOnCancel()

	authToken, err := s.associate(ctx, conn, c, logger)
	if err != nil {
		if ctx.Err() != nil {
			err = errors.Join(ctx.Err(), err)
		}
		logger.Info("mms: association failed", "error", err)
		s.metrics.ObserveConnection(ConnectionEvent{Role: RoleServer, State: ConnectionRejected, RemoteAddr: remote, Err: err})
		return err
	}
	sc.authToken = authToken

	if err := s.registerConn(sc); err != nil {
		_ = sc.Abort(ctx)
		return err
	}
	logger.Info("mms: association established")
	s.metrics.ObserveConnection(ConnectionEvent{Role: RoleServer, State: ConnectionEstablished, RemoteAddr: remote})
	defer func() {
		if s.fileProvider != nil {
			sc.frsmTable.closeAll(ctx, s.fileProvider)
		}
		s.unregisterConn(sc)
	}()

	svcCtx := context.WithValue(ctx, serverConnCtxKey{}, sc)
	err = c.Serve(svcCtx)
	if err != nil {
		err = classify(fmt.Errorf("mms: serve: %w", err), ErrClosed)
		if ctx.Err() != nil {
			err = errors.Join(ctx.Err(), err)
		}
		logger.Info("mms: association ended", "error", err)
	} else {
		logger.Info("mms: association ended")
	}
	s.metrics.ObserveConnection(ConnectionEvent{Role: RoleServer, State: ConnectionClosed, RemoteAddr: remote, Err: err})
	return err
}

// defaultAssociationTimeout is used when ServerOptions.AssociationTimeout
// is zero.
const defaultAssociationTimeout = 30 * time.Second

// associate receives the AARQ, runs the authenticator and sends the AARE,
// bounded by the association timeout. On timeout the transport is closed.
func (s *Server) associate(ctx context.Context, conn Transport, c *serverconn.Conn, logger *slog.Logger) (any, error) {
	actx := ctx
	if s.assocTimeout > 0 {
		var cancel context.CancelFunc
		actx, cancel = context.WithTimeout(ctx, s.assocTimeout)
		defer cancel()
		stop := context.AfterFunc(actx, func() { _ = conn.Close() })
		defer stop()
	}

	authInfo, err := c.ReceiveAssociation(actx)
	if err != nil {
		if deadlinePassed(actx) && !errors.Is(err, context.DeadlineExceeded) {
			err = errors.Join(context.DeadlineExceeded, err)
		}
		return nil, classify(fmt.Errorf("mms: receive association: %w", err), ErrAssociationFailed)
	}

	var authToken any
	if s.authenticator != nil {
		authCtx := s.buildAuthContext(conn, authInfo)
		result, authErr := s.authenticator(actx, authCtx)
		if authErr != nil {
			logger.Warn("mms: authenticator error", "error", authErr)
			_ = c.RejectAssociation(actx)
			return nil, &AuthenticationError{Reason: authErr.Error()}
		}
		if !result.Accept {
			logger.Warn("mms: authentication rejected")
			_ = c.RejectAssociation(actx)
			return nil, &AuthenticationError{Reason: "authenticator rejected"}
		}
		authToken = result.Token
	}

	if err := c.AcceptAssociation(actx); err != nil {
		return nil, classify(fmt.Errorf("mms: accept association: %w", err), ErrAssociationFailed)
	}
	return authToken, nil
}

// deadlinePassed reports whether ctx ended by its deadline, including a
// deadline that has passed before ctx's timer fired (a transport deadline
// taken from ctx can expire first).
func deadlinePassed(ctx context.Context) bool {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return true
	}
	dl, ok := ctx.Deadline()
	return ok && !time.Now().Before(dl)
}

// handler returns the per-connection confirmed-request handler: it
// dispatches the request, maps handler errors to service errors, and
// reports the outcome to the logger and Metrics.
func (s *Server) handler(sc *ServerConn, logger *slog.Logger) serverconn.ServiceHandler {
	return func(ctx context.Context, invokeID codec.InvokeID, serviceTag int, body []byte) (int, bool, []byte, error) {
		sc.busy <- struct{}{}
		defer func() { <-sc.busy }()

		start := time.Now()
		respTag, constructed, payload, err := s.dispatch(ctx, invokeID, serviceTag, body)
		err = toWireServiceError(err)
		svc := serviceFromTag(serviceTag)
		elapsed := time.Since(start)
		if err != nil {
			logger.Debug("mms: request failed", "service", svc.String(), "invoke_id", invokeID, "duration", elapsed, "error", err)
		} else {
			logger.Debug("mms: request", "service", svc.String(), "invoke_id", invokeID, "duration", elapsed)
		}
		s.metrics.ObserveRequest(RequestEvent{Role: RoleServer, Service: svc, InvokeID: InvokeID(invokeID), Duration: elapsed, Err: err})
		return respTag, constructed, payload, err
	}
}

// toWireServiceError converts a [*ServiceError] returned by a user
// handler or provider into the ConfirmedErrorPDU it describes. Other
// errors pass through unchanged.
func toWireServiceError(err error) error {
	if err == nil {
		return nil
	}
	var wire *serverconn.ServiceError
	if errors.As(err, &wire) {
		return err
	}
	var se *ServiceError
	if errors.As(err, &se) {
		return &serverconn.ServiceError{ErrorClass: int(se.Class), ErrorCode: se.Code}
	}
	return err
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

func (s *Server) registerConn(sc *ServerConn) error {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	if s.shutdown {
		return ErrServerClosed
	}
	s.conns[sc] = struct{}{}
	return nil
}

func (s *Server) isShutdown() bool {
	s.connsMu.RLock()
	defer s.connsMu.RUnlock()
	return s.shutdown
}

// Shutdown stops the server gracefully: it closes the listeners of every
// running [Server.ListenAndServe] (which then return [ErrServerClosed]),
// lets each association finish the request it is handling, and then
// aborts it (ACSE ABRT and transport close). MMS has no server-initiated
// orderly release, so idle associations are aborted right away.
//
// If ctx is done before all in-flight requests finish, the remaining
// associations are aborted immediately and Shutdown returns ctx.Err().
// After Shutdown, [Server.Serve] and [Server.ListenAndServe] return
// [ErrServerClosed]. Shutdown is safe to call more than once.
func (s *Server) Shutdown(ctx context.Context) error {
	s.connsMu.Lock()
	s.shutdown = true
	lns := make([]TransportListener, 0, len(s.listeners))
	for ln := range s.listeners {
		lns = append(lns, ln)
	}
	conns := make([]*ServerConn, 0, len(s.conns))
	for sc := range s.conns {
		conns = append(conns, sc)
	}
	s.connsMu.Unlock()

	for _, ln := range lns {
		_ = ln.Close()
	}

	var wg sync.WaitGroup
	for _, sc := range conns {
		wg.Add(1)
		go func(sc *ServerConn) {
			defer wg.Done()
			select {
			case sc.busy <- struct{}{}:
				defer func() { <-sc.busy }()
			case <-ctx.Done():
			}
			abortCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			defer cancel()
			_ = sc.Abort(abortCtx)
		}(sc)
	}
	wg.Wait()
	s.logger.Info("mms: server shut down", "aborted", len(conns))
	return ctx.Err()
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
		return invalidArgf("mms: association NVL %q already defined", entry.ItemID)
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

// RegisterVariable adds an association-scoped variable to this
// connection. Association-scope variables are visible only to this
// client and are automatically discarded when the connection closes.
//
// This is typically called from an [Authenticator] or connection setup
// callback after the association is established. The variable is
// readable/writable via the standard MMS Read/Write services using
// [ObjectScopeAssociation] addressing.
func (sc *ServerConn) RegisterVariable(v Variable) error {
	if v.Name.ItemID == "" {
		return invalidArgf("mms: register association variable: empty ItemID")
	}
	entry := &servermodel.VarEntry{
		ItemID:    string(v.Name.ItemID),
		Scope:     int(ObjectScopeAssociation),
		Deletable: v.Deletable,
		TypeSpec:  v.TypeSpec,
		ReadFunc:  v.Read,
		WriteFunc: v.Write,
	}
	return sc.registerAssocVar(entry)
}

func (sc *ServerConn) registerAssocVar(entry *servermodel.VarEntry) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.assocVars == nil {
		sc.assocVars = make(map[string]*servermodel.VarEntry)
	}
	if _, exists := sc.assocVars[entry.ItemID]; exists {
		return invalidArgf("mms: association variable %q already defined", entry.ItemID)
	}
	sc.assocVars[entry.ItemID] = entry
	sc.assocVarOrder = append(sc.assocVarOrder, entry.ItemID)
	sort.Strings(sc.assocVarOrder)
	return nil
}

func (sc *ServerConn) lookupAssocVar(itemID string) (*servermodel.VarEntry, bool) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	e, ok := sc.assocVars[itemID]
	return e, ok
}

func (sc *ServerConn) listAssocVars(continueAfter string, maxNames int) servermodel.NameListResult {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	if maxNames <= 0 {
		maxNames = 100
	}
	sorted := sc.assocVarOrder
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

// Abort sends a protocol-level MMS Abort PDU (ACSE ABRT wrapped in a
// Session ABORT SPDU) to the connected client and closes the connection.
// The send is best-effort: the connection is closed even if it fails.
// Returns [ErrServerConnClosed] if the connection was already closed.
func (sc *ServerConn) Abort(ctx context.Context) error {
	sc.mu.RLock()
	if sc.closed {
		sc.mu.RUnlock()
		return ErrServerConnClosed
	}
	sc.mu.RUnlock()

	// Close even if the send is stuck behind a slow peer.
	stop := context.AfterFunc(ctx, func() { _ = sc.conn.Close() })
	sendErr := sc.conn.SendAbort(ctx)
	stop()
	closeErr := sc.conn.Close()
	if sendErr != nil {
		return sendErr
	}
	return closeErr
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
		return invalidArgf("mms: marshal unsolicited status: %w", err)
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
		return invalidArgf("mms: marshal info report: %w", err)
	}
	mmsPdu, err := pdu.MarshalInformationReport(wire)
	if err != nil {
		return invalidArgf("mms: marshal info report PDU: %w", err)
	}
	return sc.conn.SendUnconfirmed(ctx, mmsPdu)
}

// Broadcast sends an InformationReport to all currently connected clients.
// Errors from individual connections are logged but do not stop delivery
// to remaining connections.
func (s *Server) Broadcast(ctx context.Context, req *InformationReportRequest) error {
	wire, err := infoReportRequestToWire(req)
	if err != nil {
		return invalidArgf("mms: marshal info report: %w", err)
	}
	mmsPdu, err := pdu.MarshalInformationReport(wire)
	if err != nil {
		return invalidArgf("mms: marshal info report PDU: %w", err)
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
			s.logger.Warn("mms: broadcast send failed", "remote_addr", sc.remote, "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func infoReportRequestToWire(req *InformationReportRequest) (*pdu.InformationReportWire, error) {
	if req == nil {
		return nil, invalidArgf("nil InformationReportRequest")
	}

	hasListName := req.ListName != nil
	hasVars := len(req.Variables) > 0

	switch {
	case hasListName && hasVars:
		return nil, invalidArgf("InformationReportRequest: ListName and Variables are mutually exclusive")
	case !hasListName && !hasVars:
		return nil, invalidArgf("InformationReportRequest: one of ListName or Variables is required")
	case !hasListName && len(req.Variables) != len(req.Values):
		return nil, invalidArgf("InformationReportRequest: len(Variables)=%d != len(Values)=%d", len(req.Variables), len(req.Values))
	}

	if len(req.Values) == 0 {
		return nil, invalidArgf("InformationReportRequest: at least one Value is required")
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
			return nil, invalidArgf("value [%d]: %w", i, err)
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
	s.connsMu.Lock()
	if s.shutdown {
		s.connsMu.Unlock()
		_ = ln.Close()
		return ErrServerClosed
	}
	s.listeners[ln] = struct{}{}
	s.connsMu.Unlock()
	defer func() {
		s.connsMu.Lock()
		delete(s.listeners, ln)
		s.connsMu.Unlock()
		_ = ln.Close()
	}()

	s.logger.Info("mms: server listening", "addr", ln.Addr().String())
	for {
		conn, err := ln.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if s.isShutdown() {
				return ErrServerClosed
			}
			if isTemporary(err) {
				s.logger.Warn("mms: temporary accept error, retrying", "error", err)
				continue
			}
			return fmt.Errorf("mms: accept: %w", err)
		}

		go func(c Transport) {
			defer func() { _ = c.Close() }()
			// Serve logs and reports the outcome itself.
			_ = s.Serve(ctx, c)
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

	payload, err := pdu.MarshalIdentifyResponse(result.Vendor, result.Model, result.Revision)
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
		return 0, false, nil, errInvalidRequest
	}
	req := StatusRequest{ExtendedDerivation: body[0] != 0}
	result, err := h(ctx, req)
	if err != nil {
		return 0, false, nil, err
	}

	payload, err := pdu.MarshalStatusResponse(int(result.Logical), int(result.Physical))
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

	if req.Scope == pdu.ScopeDomain && !s.registry.HasDomain(req.DomainID) {
		return 0, false, nil, errObjectNonExistent
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
	case req.ObjectClass == int(ObjectClassNamedVariable) && req.Scope == pdu.ScopeAssociation:
		sc, _ := ctx.Value(serverConnCtxKey{}).(*ServerConn)
		if sc == nil {
			return 0, false, nil, errUnsupportedFeature
		}
		result = sc.listAssocVars(req.ContinueAfter, 0)
	default:
		return 0, false, nil, errUnsupportedFeature
	}

	respBytes, err := fitList(len(result.Names), s.responseBudget(ctx), func(k int, truncated bool) ([]byte, error) {
		return pdu.MarshalGetNameListResponse(result.Names[:k], truncated || result.MoreFollows)
	})
	if err != nil {
		if errors.Is(err, errResponseTooLarge) {
			return 0, false, nil, err
		}
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

	respBytes, err := fitList(len(names), s.responseBudget(ctx), func(k int, truncated bool) ([]byte, error) {
		return pdu.MarshalGetNameListResponse(names[:k], truncated)
	})
	if err != nil {
		if errors.Is(err, errResponseTooLarge) {
			return 0, false, nil, err
		}
		return 0, false, nil, fmt.Errorf("marshal getnamelist response: %w", err)
	}
	return asn1util.TagNumGetNameList, true, respBytes, nil
}

// --- GetVariableAccessAttributes ---

func (s *Server) handleGetVarAccess(ctx context.Context, body []byte) (int, bool, []byte, error) {
	wireName, err := pdu.UnmarshalGetVarAccessRequest(body)
	if err != nil {
		return 0, false, nil, errInvalidRequest
	}

	entry, ok := s.lookupVariable(ctx, wireName.Scope, wireName.DomainID, wireName.ItemID)
	if !ok {
		return 0, false, nil, errObjectNonExistent
	}

	ts, ok := entry.TypeSpec.(TypeSpec)
	if !ok {
		return 0, false, nil, errAccessDenied
	}

	wireTS, err := typeSpecToWire(ts)
	if err != nil {
		return 0, false, nil, errAccessUnsupported
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
		entry, ok := s.lookupVariable(ctx, wn.Scope, wn.DomainID, wn.ItemID)
		if !ok {
			accessResults = append(accessResults, &pdu.AccessResult{IsError: true, ErrorCode: wireErrCode(wireErrObjectNonExistent)})
			continue
		}

		readFn, ok := entry.ReadFunc.(func(context.Context) (*Value, error))
		if !ok || readFn == nil {
			accessResults = append(accessResults, &pdu.AccessResult{IsError: true, ErrorCode: wireErrCode(wireErrAccessDenied)})
			continue
		}

		val, err := readFn(ctx)
		if err != nil {
			accessResults = append(accessResults, &pdu.AccessResult{IsError: true, ErrorCode: callbackErrCode(err)})
			continue
		}

		if len(spec.AlternateAccess) > 0 {
			ts := extractTypeSpec(entry.TypeSpec)
			val = applyAlternateAccessRead(val, ts, spec.AlternateAccess)
			if val == nil {
				accessResults = append(accessResults, &pdu.AccessResult{IsError: true, ErrorCode: wireErrCode(wireErrObjectUndefined)})
				continue
			}
		}

		dv, err := valueToDataValue(val)
		if err != nil {
			accessResults = append(accessResults, &pdu.AccessResult{IsError: true, ErrorCode: wireErrCode(wireErrTempUnavail)})
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

	var results []pdu.WriteResult
	for i, spec := range specs {
		if i >= len(wireData) {
			results = append(results, pdu.WriteResult{Code: wireErrCode(wireErrTypeInconsistent)})
			continue
		}
		wn := spec.Name
		entry, ok := s.lookupVariable(ctx, wn.Scope, wn.DomainID, wn.ItemID)
		if !ok {
			results = append(results, pdu.WriteResult{Code: wireErrCode(wireErrObjectNonExistent)})
			continue
		}

		writeFn, ok := entry.WriteFunc.(func(context.Context, *Value) error)
		if !ok || writeFn == nil {
			results = append(results, pdu.WriteResult{Code: wireErrCode(wireErrAccessDenied)})
			continue
		}

		val, err := dataValueToValue(wireData[i])
		if err != nil {
			results = append(results, pdu.WriteResult{Code: wireErrCode(wireErrTypeInconsistent)})
			continue
		}

		if len(spec.AlternateAccess) > 0 {
			readFn, readOK := entry.ReadFunc.(func(context.Context) (*Value, error))
			if !readOK || readFn == nil {
				results = append(results, pdu.WriteResult{Code: wireErrCode(wireErrAccessDenied)})
				continue
			}
			currentVal, readErr := readFn(ctx)
			if readErr != nil {
				results = append(results, pdu.WriteResult{Code: wireErrCode(wireErrTempUnavail)})
				continue
			}
			ts := extractTypeSpec(entry.TypeSpec)
			val = applyAlternateAccessWrite(currentVal, ts, spec.AlternateAccess, val)
			if val == nil {
				results = append(results, pdu.WriteResult{Code: wireErrCode(wireErrTypeInconsistent)})
				continue
			}
		}

		if err := writeFn(ctx, val); err != nil {
			results = append(results, pdu.WriteResult{Code: callbackErrCode(err)})
			continue
		}
		results = append(results, pdu.WriteResult{Success: true})
	}

	respBytes, err := pdu.MarshalWriteResponse(results)
	if err != nil {
		return 0, false, nil, fmt.Errorf("marshal write response: %w", err)
	}
	return asn1util.TagNumWrite, true, respBytes, nil
}

// lookupVariable resolves a variable by scope. For association scope,
// it checks the per-connection storage first. For VMD/domain scope,
// it uses the shared registry.
func (s *Server) lookupVariable(ctx context.Context, scope int, domain, itemID string) (*servermodel.VarEntry, bool) {
	if scope == pdu.ScopeAssociation {
		if sc, _ := ctx.Value(serverConnCtxKey{}).(*ServerConn); sc != nil {
			return sc.lookupAssocVar(itemID)
		}
		return nil, false
	}
	return s.registry.LookupVariable(scope, domain, itemID)
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

	return asn1util.TagNumDefineNamedVariableList, false, nil, nil
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
	case 2: // domain-scope: delete all deletable NVLs in the specified domain
		if req.DomainName == "" {
			return 0, false, nil, errInvalidRequest
		}
		matched, deleted = s.registry.DeleteAllDomainNVLs(req.DomainName)
	case 3: // VMD-scope: delete all deletable VMD-scoped NVLs
		matched, deleted = s.registry.DeleteAllVMDNVLs()
	default:
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
	case ValueTypeGeneralizedTime:
		return pdu.TypeSpecWire{Tag: 11}, nil
	case ValueTypeBCD:
		return pdu.TypeSpecWire{Tag: 13, Size: ts.Size}, nil
	case ValueTypeObjectIdentifier:
		return pdu.TypeSpecWire{Tag: 15}, nil
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
