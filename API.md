# go-mms Public API Reference

Public API of package `github.com/otfabric/go-mms` (package name: `mms`).

See [pkg.go.dev](https://pkg.go.dev/github.com/otfabric/go-mms) for the full generated reference.
Compatibility policy: [COMPATIBILITY.md](COMPATIBILITY.md).
Error taxonomy: [ERRORS.md](ERRORS.md).

---

## Table of contents

- [Client](#client)
- [Server](#server)
- [Values and types](#values-and-types)
- [Error types](#error-types)
- [Metrics](#metrics)
- [Retry](#retry)
- [Transport interfaces](#transport-interfaces)
- [transport/iso](#transportiso)
- [Identity, auth, and options](#identity-auth-and-options)
- [Deprecated symbols](#deprecated-symbols)

---

## Client

```go
func NewClient(ctx context.Context, conn Transport, opts DialOptions) (*Client, error)
```

Creates an MMS client over an already-open transport connection. The transport is
owned by the client after this call. Performs the ISO/ACSE/MMS Initiate exchange.

### Association

```go
func (c *Client) Close(ctx context.Context) error
func (c *Client) Abort(ctx context.Context) error
func (c *Client) Done() <-chan struct{}
func (c *Client) Err() error
```

`Close` performs a graceful MMS Conclude exchange. `Abort` tears down the connection
immediately without waiting for a response; the ABRT send is bounded by `ctx`. Both
methods are idempotent.

`Done` is closed when the association has ended (local `Close`/`Abort`, peer
conclude/release/abort, connection lost or undecodable). `Err` returns nil while the
association is up and afterwards why it ended (matches `ErrClosed`). See
[TIMEOUT_AND_CLOSE.md](TIMEOUT_AND_CLOSE.md).

### Identity and status

```go
func (c *Client) Identify(ctx context.Context) (*ServerIdentity, error)
func (c *Client) Status(ctx context.Context) (*ServerStatus, error)
func (c *Client) StatusWithOptions(ctx context.Context, req ClientStatusRequest) (*ServerStatus, error)
func (c *Client) Negotiated() NegotiatedParameters
```

### Read and write

```go
func (c *Client) Read(ctx context.Context, req ReadRequest) (*ReadResult, error)
func (c *Client) ReadObject(ctx context.Context, name ObjectName) (*ReadResult, error)
func (c *Client) ReadMultiple(ctx context.Context, variables []ObjectName) ([]AccessResult, error)
func (c *Client) ReadVariables(ctx context.Context, variables []VariableSpec) ([]AccessResult, error)
func (c *Client) ReadComponent(ctx context.Context, name ObjectName, component string) (*ReadResult, error)
func (c *Client) ReadByIndex(ctx context.Context, name ObjectName, index int) (*ReadResult, error)
func (c *Client) ReadArrayRange(ctx context.Context, name ObjectName, start, count int) (*ReadResult, error)

func (c *Client) Write(ctx context.Context, req WriteRequest) (*WriteResult, error)
func (c *Client) WriteObject(ctx context.Context, name ObjectName, value *Value) (*WriteResult, error)
func (c *Client) WriteVariables(ctx context.Context, variables []VariableSpec, values []*Value) ([]WriteAccessResult, error)
func (c *Client) WriteComponent(ctx context.Context, name ObjectName, component string, value *Value) error
func (c *Client) WriteArrayElement(ctx context.Context, name ObjectName, index int, value *Value) error
```

`ReadArrayElement` is a deprecated alias for `ReadByIndex`. Use `ReadByIndex` instead.

`ReadObject` supports VMD- and association-scoped `ObjectName` values.
`Read` is the simplified domain/item form; prefer `ReadObject` when full `ObjectName`
flexibility is needed.

### Named variable lists (NVL)

```go
func (c *Client) DefineNamedVariableList(ctx context.Context, req DefineNamedVariableListRequest) error
func (c *Client) GetNamedVariableListAttributes(ctx context.Context, listName ObjectName) (*NamedVariableListAttributes, error)
func (c *Client) DeleteNamedVariableList(ctx context.Context, listNames []ObjectName) (*DeleteNamedVariableListResult, error)
func (c *Client) DeleteAllDomainNVLs(ctx context.Context, domain string) (*DeleteNamedVariableListResult, error)
func (c *Client) DeleteAllVMDNVLs(ctx context.Context) (*DeleteNamedVariableListResult, error)
func (c *Client) ReadNamedVariableList(ctx context.Context, listName ObjectName, opts ...ReadNamedVariableListOptions) ([]AccessResult, error)
func (c *Client) WriteNamedVariableList(ctx context.Context, listName ObjectName, values []*Value) ([]WriteAccessResult, error)
```

### Model discovery

```go
func (c *Client) GetNameList(ctx context.Context, req NameListRequest) (*NameListResult, error)
func (c *Client) GetNameListAll(ctx context.Context, req NameListRequest) ([]string, error)
func (c *Client) GetVariableAccessAttributes(ctx context.Context, name ObjectName) (*VariableAccessAttributes, error)
```

`GetNameListAll` paginates automatically, collecting all results. `GetNameList` exposes
the low-level continuation cursor.

### Information reports

```go
func (c *Client) OnInformationReport(handler InformationReportHandler)
```

Registers a callback invoked for every `UnconfirmedPDU`/`InformationReport` received.
The callback is called from the client's internal reader goroutine; it must return quickly
and must not call back into the same `Client`.

### File services

```go
func (c *Client) FileOpen(ctx context.Context, fileName string, opts ...FileOpenOptions) (*FileOpenResult, error)
func (c *Client) FileRead(ctx context.Context, frsmID int32) (*FileReadResult, error)
func (c *Client) FileReadAll(ctx context.Context, frsmID int32) ([]byte, error)
func (c *Client) FileClose(ctx context.Context, frsmID int32) error
func (c *Client) FileDirectory(ctx context.Context, req FileDirectoryRequest) (*FileDirectoryResult, error)
func (c *Client) FileDirectoryAll(ctx context.Context, fileSpec string) ([]FileDirectoryEntry, error)
func (c *Client) FileDelete(ctx context.Context, fileName string) error
func (c *Client) FileRename(ctx context.Context, currentName, newName string) error
func (c *Client) ObtainFile(ctx context.Context, sourceFile, destinationFile string) error
func (c *Client) DownloadFile(ctx context.Context, fileName string) ([]byte, *FileOpenResult, error)
```

`FileOpen` takes an optional [FileOpenOptions]; the zero value starts at
position 0. `FileDirectory` pages with `FileDirectoryRequest.ContinueAfter`;
`FileDirectoryAll` walks the pages. `DownloadFile` is Open → ReadAll → Close
and returns the bytes plus the FileOpen metadata.

`ObtainFile` requests the server to copy `sourceFile` to `destinationFile` using its
own `FileProvider`. The MMS segmented role-reversal protocol (server calling back to
the client's file services) is not implemented; see [KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md).

#### Directory listings differ between servers

MMS treats a file name as an opaque string and has no directory entry
type, so servers organise listings in their own way. go-mms returns names
exactly as received. Conventions seen on devices:

- **Flattened.** One listing of the whole store, every file by its full path
  with `/` separators (`COMTRADE/REC02.CFG`). libiec61850 servers do this.
- **Per level.** Only the requested level. A subdirectory appears as an
  entry of size 0 whose name ends in `\` (`COMTRADE\`); list it with another
  `FileDirectory` call using that name.

Code that walks a store should therefore treat a zero-size entry ending in
`/` or `\` as a directory to list, and pass names back to the server
unchanged rather than normalising separators.

`lastModified` is optional in MMS and some devices omit it, for some or all
entries. `FileDirectoryEntry.LastModified` and `FileOpenResult.LastModified`
are then the zero time. On the server side, a provider that leaves
`FileEntry.LastModified` or `FileAttributes.LastModified` zero has the field
omitted from the response.

### Journal services

```go
func (c *Client) ReadJournalTimeRange(ctx context.Context, domain, journal string, start, stop time.Time) (*JournalResult, error)
func (c *Client) ReadJournalStartAfter(ctx context.Context, domain, journal string, afterTime time.Time, afterID []byte) (*JournalResult, error)
```

When `JournalResult.MoreFollows` is true, call `ReadJournalStartAfter` with the
last entry's `OccurrenceTime` and `EntryID` to retrieve the next page.

---

## Server

```go
func NewServer(opts ServerOptions) *Server
```

Creates an MMS server. Call `ListenAndServe` to accept connections, or `Serve` to
handle a single already-accepted transport connection.

```go
func (s *Server) ListenAndServe(ctx context.Context, ln TransportListener) error
func (s *Server) Serve(ctx context.Context, conn Transport) error
func (s *Server) Shutdown(ctx context.Context) error

// Model registration.
func (s *Server) RegisterDomain(name string) error
func (s *Server) RegisterVariable(v Variable) error
func (s *Server) SetVariableRead(domain, itemID string, fn func(context.Context) (*Value, error)) error
func (s *Server) RegisterNamedVariableList(nvl NamedVariableList) error

// Identify / Status handlers.
func (s *Server) HandleIdentify(h func(context.Context, IdentifyRequest) (*ServerIdentity, error))
func (s *Server) HandleStatus(h func(context.Context, StatusRequest) (*ServerStatus, error))

// Connections and reports.
func (s *Server) Connections() []*ServerConn
func (s *Server) Broadcast(ctx context.Context, req *InformationReportRequest) error
```

Per-variable write handling is set via `Variable.Write` at registration time. A
`Variable.Read`/`Write` callback that returns a `*DataAccessError` with a valid code
has that code sent; other errors are sent as temporarily-unavailable. An Identify or
Status handler that returns a `*ServiceError` has its class and code sent.

`Shutdown` closes the listeners of running `ListenAndServe` calls, lets each
association finish its in-flight request, then aborts it (ACSE ABRT); when `ctx`
ends first, remaining associations are aborted at once and `ctx.Err()` is returned.
Afterwards `Serve` and `ListenAndServe` return `ErrServerClosed`. `Serve` closes its
transport when `ctx` is cancelled, when the association is not established within
`ServerOptions.AssociationTimeout`, or when `Shutdown` aborts it.

### Server file and journal providers

```go
type FileProvider interface {
    List(ctx context.Context, req FileListRequest) (*FileListResult, error)
    Open(ctx context.Context, path string) (FileHandle, FileAttributes, error)
    Read(ctx context.Context, handle FileHandle, maxBytes int) (data []byte, moreFollows bool, err error)
    Close(ctx context.Context, handle FileHandle) error
    Delete(ctx context.Context, path string) error
    Rename(ctx context.Context, currentName, newName string) error
    ObtainFile(ctx context.Context, sourceFile, destinationFile string) error
}

type JournalProvider interface {
    ListJournals(ctx context.Context, domain string) ([]string, error)
    ReadTimeRange(ctx context.Context, domain, journal string,
        start, stop time.Time, maxEntries int) (*JournalResult, error)
    ReadStartAfter(ctx context.Context, domain, journal string,
        afterID []byte, afterTime time.Time, maxEntries int) (*JournalResult, error)
}
```

### Server connection context

```go
func ServerConnFromContext(ctx context.Context) *ServerConn
```

Retrieves the active `*ServerConn` from a handler context. Returns nil when called
outside a server handler.

### ServerConn

```go
func (sc *ServerConn) SendInformationReport(ctx context.Context, req *InformationReportRequest) error
func (sc *ServerConn) SendUnsolicitedStatus(ctx context.Context, status ServerStatus) error
func (sc *ServerConn) RegisterVariable(v Variable) error // association-scope variable
func (sc *ServerConn) Abort(ctx context.Context) error
func (sc *ServerConn) AuthToken() any
```

---

## Values and types

### Value constructors

```go
func NewBoolean(v bool) *Value
func NewInteger(v int64) *Value
func NewUnsigned(v uint64) *Value
func NewFloat(v float64) *Value
func NewReal(v float64) *Value
func NewOctetString(v []byte) *Value
func NewVisibleString(v string) *Value
func NewMmsString(v string) *Value
func NewBinaryTime(epochMillis int64) *Value
func NewBitString(bits []byte, bitLen int) *Value
func NewArray(elements []*Value) *Value
func NewStructure(elements []*Value) *Value
func NewBooleanArray(bits []byte, bitLen int) *Value
func NewDataAccessError(code DataAccessErrorCode) *Value
```

### Value accessors

```go
func (v *Value) Type() ValueType
func (v *Value) Bool() (bool, bool)
func (v *Value) Int() (int64, bool)
func (v *Value) Uint() (uint64, bool)
func (v *Value) Float64() (float64, bool)
func (v *Value) Float32() (float32, bool)
func (v *Value) OctetString() ([]byte, bool)
func (v *Value) VisibleString() (string, bool)
func (v *Value) MmsString() (string, bool)
func (v *Value) BinaryTimeMillis() (int64, bool)
func (v *Value) BitLen() int
func (v *Value) Bits() ([]byte, bool)
func (v *Value) Structure() ([]*Value, bool)
func (v *Value) ArrayElements() ([]*Value, bool)
func (v *Value) Get(selectors ...AccessSelector) (*Value, error)
func (v *Value) DataAccessError() (DataAccessErrorCode, bool)
func (v *Value) Clone() *Value
func (v *Value) Equal(other *Value) bool
func (v *Value) String() string
```

`Equal` compares Float and Real values bit for bit, except that all NaNs are equal
(so `Equal` is reflexive); `+0` and `-0` differ.

Component-name selectors in `Get` require a `TypeSpec` context; use index selectors
(`SelectIndex`) or resolve via `TypeSpec.Resolve` for named component access.

### TypeSpec

```go
type TypeSpec struct {
    Type          ValueType
    Elements      []TypeSpecElement // structure: {Name, Type}
    Count         int               // array element count
    Element       *TypeSpec         // array element type
    Size          int               // bit width / string size
    FormatWidth   int               // floating point
    ExponentWidth int               // floating point
    TypeName      *ObjectName       // named type reference
}

func (ts *TypeSpec) ShallowCompatible(v *Value) bool
func (ts *TypeSpec) DefaultValue() *Value
func (ts *TypeSpec) ChildByName(name string) (*TypeSpec, bool)
func (ts *TypeSpec) ChildByIndex(index int) (*TypeSpec, bool)
func (ts *TypeSpec) Resolve(selectors ...AccessSelector) (*TypeSpec, error)
```

`Resolve` and `Value.Get` errors match `ErrInvalidArgument`.

`ShallowCompatible` checks the top-level type and element count only; it does not
recurse into children.

### ObjectName and addressing

```go
type ObjectName struct {
    Scope    ObjectScope
    Domain   DomainID
    ItemID   ItemID
}

type ObjectScope int
const (
    ObjectScopeVMD        ObjectScope = iota
    ObjectScopeDomain
    ObjectScopeAssociation
)

type DomainID string
type ItemID   string
type InvokeID uint32
```

### Alternate-access selectors

```go
type AccessSelector struct {
    Component  string
    Index      *int
    IndexRange *IndexRange
}

type IndexRange struct { Start, Count int }

func SelectIndex(i int) AccessSelector
func SelectComponent(name string) AccessSelector
func SelectRange(low, count int) AccessSelector
```

### VariableSpec

```go
type VariableSpec struct {
    Name            ObjectName
    AlternateAccess []AccessSelector
}
```

### Server model types

```go
type Variable struct {
    Name      ObjectName
    TypeSpec  TypeSpec
    Deletable bool
    Read      func(ctx context.Context) (*Value, error)
    Write     func(ctx context.Context, v *Value) error
}

type NamedVariableList struct {
    Name      ObjectName
    Deletable bool
    Variables []VariableSpec
}
```

### DataAccessErrorCode constants

```go
const (
    DataAccessErrorNone                        DataAccessErrorCode = -1 // Go-only; never on the wire
    DataAccessErrorObjectInvalidated           DataAccessErrorCode = 0
    DataAccessErrorHardwareFault               DataAccessErrorCode = 1
    DataAccessErrorTemporarilyUnavailable      DataAccessErrorCode = 2
    DataAccessErrorObjectAccessDenied          DataAccessErrorCode = 3
    DataAccessErrorObjectUndefined             DataAccessErrorCode = 4
    DataAccessErrorInvalidAddress              DataAccessErrorCode = 5
    DataAccessErrorTypeUnsupported             DataAccessErrorCode = 6
    DataAccessErrorTypeInconsistent            DataAccessErrorCode = 7
    DataAccessErrorObjectAttributeInconsistent DataAccessErrorCode = 8
    DataAccessErrorObjectAccessUnsupported     DataAccessErrorCode = 9
    DataAccessErrorObjectNonExistent           DataAccessErrorCode = 10
    DataAccessErrorObjectValueInvalid          DataAccessErrorCode = 11
)
```

---

## Error types

See [ERRORS.md](ERRORS.md) for the full taxonomy and `errors.Is` / `errors.As` guidance.

```go
// Sentinel errors (use errors.Is).
var (
    ErrClosed               error  // connection already closed
    ErrInvokeTimeout        error  // confirmed request timed out
    ErrConnectionRejected   error  // peer rejected ISO/ACSE association
    ErrAssociationFailed    error  // association handshake failure
    ErrNegotiationFailed    error  // MMS Initiate negotiation failure
    ErrInvalidPDU           error  // structurally invalid PDU
    ErrDecodeFailed         error  // PDU decode failure
    ErrUnsupported          error  // service not supported by this library
    ErrServiceRejected      error  // remote ConfirmedError response
    ErrDataAccess           error  // per-variable data access error
    ErrProtocol             error  // wire-level protocol error
    ErrServerConnClosed     error  // ServerConn already closed
    ErrAuthenticationFailed error  // authentication failure
    ErrFileAccessDenied     error  // FileProvider/JournalProvider permission error
    ErrServerClosed         error  // Serve/ListenAndServe after Server.Shutdown
    ErrInvalidArgument      error  // invalid caller argument; request not sent
)

// ProtocolError.Phase values.
const (
    PhaseSession      = "session"
    PhasePresentation = "presentation"
    PhaseACSE         = "acse"
    PhaseMMS          = "mms"
)

// Typed errors (use errors.As).
type ServiceError struct {
    Class    ErrorClass
    Code     int
    InvokeID InvokeID
}
func (e *ServiceError) CodeName() string // ISO 9506-2 code name, "" if undefined

type DecodeError struct {
    Offset  int
    Tag     byte
    Message string
}

type DataAccessError struct {
    Code DataAccessErrorCode
}

type ProtocolError struct {
    Phase   string
    Message string
}

type AuthenticationError struct {
    Reason string
}

// MMS RejectPDU; unwraps to *ProtocolError (Phase PhaseMMS) → ErrProtocol.
type RejectError struct {
    Type        RejectType
    Reason      int
    InvokeID    InvokeID
    HasInvokeID bool
}

type RejectType int
const (
    RejectConfirmedRequest  RejectType = 1
    RejectConfirmedResponse RejectType = 2
    RejectConfirmedError    RejectType = 3
    RejectUnconfirmed       RejectType = 4
    RejectPDUError          RejectType = 5
    RejectCancelRequest     RejectType = 6
    RejectCancelResponse    RejectType = 7
    RejectCancelError       RejectType = 8
    RejectConcludeRequest   RejectType = 9
    RejectConcludeResponse  RejectType = 10
    RejectConcludeError     RejectType = 11
)
func (t RejectType) String() string
```

Errors carry sentinel kinds in addition to their chain (e.g. a lost connection matches
`ErrClosed`, a request deadline `ErrInvokeTimeout` and `context.DeadlineExceeded`).
Use `errors.Is`/`errors.As` and the typed fields; do not compare error strings.

---

## Metrics

See [OBSERVABILITY.md](OBSERVABILITY.md).

```go
type Metrics interface {
    ObserveRequest(RequestEvent)
    ObserveConnection(ConnectionEvent)
}

type NopMetrics struct{} // default; embed for forward compatibility

type RequestEvent struct {
    Role     Role
    Service  Service
    InvokeID InvokeID
    Duration time.Duration
    Err      error
}

type ConnectionEvent struct {
    Role       Role
    State      ConnectionState
    RemoteAddr string
    Err        error
}

type Role int            // RoleClient, RoleServer; String() "client"/"server"
type ConnectionState int // ConnectionEstablished, ConnectionRejected, ConnectionClosed
type Service int         // ServiceStatus, ServiceGetNameList, ServiceIdentify, ServiceRead,
                         // ServiceWrite, ServiceGetVariableAccessAttributes,
                         // ServiceDefineNamedVariableList, ServiceGetNamedVariableListAttributes,
                         // ServiceDeleteNamedVariableList, ServiceFileOpen, ServiceFileRead,
                         // ServiceFileClose, ServiceFileRename, ServiceFileDelete,
                         // ServiceFileDirectory, ServiceObtainFile, ServiceReadJournal,
                         // ServiceUnknown; String() is the ISO 9506 service name
```

Set via `DialOptions.Metrics` and `ServerOptions.Metrics`; nil means no metrics.
Methods are called synchronously and must be safe for concurrent use.

---

## Retry

```go
type RetryPolicy struct {
    MaxAttempts    int           // total attempts; 0 or 1 = single attempt; < 0 = until ctx done
    InitialBackoff time.Duration // 0 → 100ms
    MaxBackoff     time.Duration // 0 → 30s
    Multiplier     float64       // < 1 → 2
    Jitter         float64       // [0,1]: delay d becomes random in [d*(1-Jitter), d]
    Retryable      func(err error) bool // nil → IsRetryable
    OnRetry        func(attempt int, err error, delay time.Duration)
}

func DefaultRetryPolicy() RetryPolicy // 5 attempts, 200ms doubling to 10s, 20% jitter
func (p RetryPolicy) Backoff(attempt int) time.Duration // before jitter
func IsRetryable(err error) bool
func Retry(ctx context.Context, p RetryPolicy, fn func(ctx context.Context) error) error
```

The zero `RetryPolicy` performs a single attempt. `IsRetryable` returns false for
`context.Canceled`, `ErrInvalidArgument`, `ErrAuthenticationFailed` and
`ErrConnectionRejected`, true otherwise (including per-attempt deadlines). `Retry`
returns nil on success, otherwise the last error, joined with `ctx.Err()` when the
context ended the retries. Use it with `Client.Done` to reconnect, or use
`iso.WithRetry` for dialing.

---

## Transport interfaces

```go
type Transport interface {
    Send(ctx context.Context, data []byte) error
    Receive(ctx context.Context) ([]byte, error)
    Close() error
}

type TransportListener interface {
    Accept(ctx context.Context) (Transport, error)
    Close() error
    Addr() net.Addr
}

// Optional interface for TLS inspection.
type TLSTransport interface {
    Transport
    TLSConnectionState() *tls.ConnectionState
}

// Optional interface for peer address inspection (also used for log
// remote_addr and ConnectionEvent.RemoteAddr).
type RemoteAddrTransport interface {
    Transport
    RemoteAddr() net.Addr
}
```

The client calls `Receive` on a reader goroutine while requests call `Send`, so
transports must support one concurrent sender and receiver. The `transport/iso`
sub-package implements these interfaces over TCP + TPKT/COTP.

---

## transport/iso

```go
func Dial(ctx context.Context, addr string, opts ...Option) (*mms.Client, error)
func DialTCP(ctx context.Context, addr string, opts ...Option) (mms.Transport, error)
func Listen(addr string, opts ...Option) (*Listener, error)
func NewListener(ln net.Listener, opts ...Option) *Listener

func WithCallingTSelector(sel []byte) Option
func WithCalledTSelector(sel []byte) Option
func WithClientDialOptions(opts mms.DialOptions) Option
func WithTLSConfig(cfg *tls.Config) Option
func WithLogger(l *slog.Logger) Option
func WithRetry(p mms.RetryPolicy) Option
func WithDialer(dial func(ctx context.Context, network, addr string) (net.Conn, error)) Option
func WithHandshakeTimeout(d time.Duration) Option
```

- `WithRetry` makes `Dial`/`DialTCP` retry failed attempts (backoff with jitter,
  honouring `ctx`). For `Dial` one attempt covers TCP connect, TLS and COTP handshakes
  and the MMS association; permanent errors (`mms.IsRetryable`) return at once.
  Default: single attempt. Ignored by `Listen`.
- `WithDialer` replaces the TCP dial function (default: zero `net.Dialer`'s
  `DialContext`) for proxies, socket options or fault injection. Ignored by `Listen`.
- `WithHandshakeTimeout` bounds the TLS and COTP handshakes of each accepted
  connection; 0 → 10s, negative disables. The `Listener` handshakes up to 64
  connections concurrently in the background, so a silent peer never blocks `Accept`.
  For clients, bound the handshake with the `ctx` of `Dial`/`DialTCP`.

---

## Identity, auth, and options

### DialOptions

```go
type DialOptions struct {
    Transport TransportOptions
    ISO       ISOOptions
    MMS       MMSOptions
    Logger    *slog.Logger // nil → silent discard; does not set iso.WithLogger
    RawHook   func(direction string, raw []byte) // direction: RawHookSend or RawHookRecv
    Metrics   Metrics       // nil → no metrics
    RequestTimeout time.Duration // > 0: default bound per confirmed request
}

const (
    RawHookSend = "send"
    RawHookRecv = "recv"
)
```

`RequestTimeout` applies to every confirmed request whose context has no earlier
deadline; on expiry the request fails with `ErrInvokeTimeout` and
`context.DeadlineExceeded`. Zero means no default timeout.

### ISOOptions

```go
type ISOOptions struct {
    LocalAPTitle       APTitle
    RemoteAPTitle      APTitle
    LocalAEQualifier   int
    RemoteAEQualifier  int
    LocalPSelector     []byte
    RemotePSelector    []byte
    LocalSSelector     []byte
    RemoteSSelector    []byte
    Password           []byte // ACSE password in AARQ; use TLS in production
}
```

### MMSOptions

```go
type MMSOptions struct {
    MaxPDUSize                int // default 65000
    MaxOutstandingCalling     int // default 5 (proposed; not runtime-enforced)
    MaxOutstandingCalled      int // default 5 (proposed; not runtime-enforced)
    DataStructureNestingLevel int // default 10
}
```

### ServerOptions

```go
type ServerOptions struct {
    MMS             ServerMMSOptions
    Logger          *slog.Logger // nil → silent discard; does not set iso.WithLogger
    Authenticate    Authenticator
    FileProvider    FileProvider
    JournalProvider JournalProvider
    Metrics         Metrics       // nil → no metrics
    AssociationTimeout time.Duration // AARQ → Authenticate → AARE; 0 → 30s, < 0 disables
}
```

### ServerIdentity / ServerStatus

```go
type ServerIdentity struct {
    Vendor   string
    Model    string
    Revision string
}

type ServerStatus struct {
    Logical  VMDLogicalStatus
    Physical VMDPhysicalStatus
}
```

### Authentication

```go
type Authenticator func(ctx context.Context, auth *AuthContext) (AuthResult, error)

type AuthContext struct {
    Mechanism          AuthMechanism
    MechanismOID       asn1.ObjectIdentifier
    Password           []byte
    CallingApplication *ApplicationReference
    PeerCertificate    *x509.Certificate
    RemoteAddr         net.Addr
}

type AuthResult struct {
    Accept bool
    Token  any // available as ServerConn.AuthToken()
}

type AuthMechanism int
const (
    AuthMechanismUnknown AuthMechanism = iota
    AuthMechanismNone
    AuthMechanismACSEPassword
    AuthMechanismTLSCertificate
)
```

---

## Deprecated symbols

| Symbol | Replacement |
|--------|-------------|
| `ReadArrayElement` | `ReadByIndex` |
| `DataAccessErrorTemporarilyUnavail` | `DataAccessErrorTemporarilyUnavailable` |
| `DataAccessErrorObjectAccessUnsup` | `DataAccessErrorObjectAccessUnsupported` |
| `DataAccessErrorTypeMismatch` | `DataAccessErrorTypeUnsupported` |
| `DataAccessErrorObjectExists` | `DataAccessErrorObjectAttributeInconsistent` |
| `VMDPhysicalStatusPartiallyOper` | `VMDPhysicalStatusPartiallyOperational` |
