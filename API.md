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
- [Transport interfaces](#transport-interfaces)
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
```

`Close` performs a graceful MMS Conclude exchange. `Abort` tears down the connection
immediately without waiting for a response. Both methods are idempotent.

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
func (c *Client) FileOpen(ctx context.Context, filename string, initialPosition uint32) (*FileOpenResult, error)
func (c *Client) FileRead(ctx context.Context, handle FileHandle, ...) (*FileReadResult, error)
func (c *Client) FileClose(ctx context.Context, handle FileHandle) error
func (c *Client) FileDirectory(ctx context.Context, fileSpecification string, ...) ([]FileDirectoryEntry, error)
func (c *Client) FileDelete(ctx context.Context, filename string) error
func (c *Client) FileRename(ctx context.Context, currentFilename, newFilename string) error
func (c *Client) ObtainFile(ctx context.Context, sourceFile, destinationFile string) error
func (c *Client) DownloadFile(ctx context.Context, filename string, w io.Writer) error
```

`ObtainFile` requests the server to copy `sourceFile` to `destinationFile` using its
own `FileProvider`. The MMS segmented role-reversal protocol (server calling back to
the client's file services) is not implemented; see [KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md).

### Journal services

```go
func (c *Client) ReadJournalTimeRange(ctx context.Context, domain, name string, from, to time.Time, maxEntries int) (*JournalResult, error)
func (c *Client) ReadJournalStartAfter(ctx context.Context, domain, name string, entrySpec JournalEntry, maxEntries int) (*JournalResult, error)
```

### Low-level MMS transport

```go
func (c *Client) MMS() *mms.Client  // returns the underlying *mms.Client (go-mms)
```

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
func (s *Server) SetVariableRead(fn func(ctx context.Context, name ObjectName) (*Value, error))
func (s *Server) SetVariableWrite(fn func(ctx context.Context, name ObjectName, val *Value) error)
func (s *Server) SetIdentifyHandler(fn func(ctx context.Context) (*ServerIdentity, error))
func (s *Server) SetStatusHandler(fn func(ctx context.Context) (*ServerStatus, error))
func (s *Server) MMS() *mms.Server
```

### Server file and journal providers

```go
type FileProvider interface {
    FileOpen(ctx context.Context, filename string, position uint32) (FileHandle, *FileAttributes, error)
    FileRead(ctx context.Context, handle FileHandle, buf []byte) (int, error)
    FileClose(ctx context.Context, handle FileHandle) error
    FileDirectory(ctx context.Context, spec string) ([]FileEntry, error)
    FileDelete(ctx context.Context, filename string) error
    FileRename(ctx context.Context, current, newName string) error
    ObtainFile(ctx context.Context, sourceFile, destinationFile string) error
    DownloadFile(ctx context.Context, filename string) (io.ReadCloser, *FileAttributes, error)
}

type JournalProvider interface {
    ReadJournal(ctx context.Context, domain, name string, req JournalReadRequest) (*JournalResult, error)
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
func (sc *ServerConn) RemoteAddr() net.Addr
func (sc *ServerConn) LocalAddr() net.Addr
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
func (v *Value) String() string
```

Component-name selectors in `Get` require a `TypeSpec` context; use index selectors
(`SelectIndex`) or resolve via `TypeSpec.Resolve` for named component access.

### TypeSpec

```go
type TypeSpec struct {
    Type     ValueType
    Elements []*TypeSpec  // structure elements
    Element  *TypeSpec    // array element type
    Count    int          // declared array length
    NamedType string      // MMS named type reference
}

func (ts *TypeSpec) ShallowCompatible(v *Value) bool
func (ts *TypeSpec) DefaultValue() *Value
func (ts *TypeSpec) Resolve(v *Value, component string) (*Value, int, error)
```

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
    Index     *int
    Component string
    Range     *IndexRange
}

type IndexRange struct { Start, Count int }

func SelectIndex(i int) AccessSelector
func SelectComponent(name string) AccessSelector
func SelectRange(start, count int) AccessSelector
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
    Name  ItemID
    Value *Value
    Type  *TypeSpec
}

type NamedVariableList struct {
    Name    ObjectName
    Members []VariableSpec
}
```

### DataAccessErrorCode constants

```go
const (
    DataAccessErrorNone                        DataAccessErrorCode = 0
    DataAccessErrorObjectInvalidated           DataAccessErrorCode = 1
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
    ErrClosed             error  // connection already closed
    ErrAssociationRefused error  // peer rejected ISO/ACSE association
    ErrAssociation        error  // association handshake failure
    ErrNegotiation        error  // MMS Initiate negotiation failure
    ErrProtocol           error  // wire-level protocol error
    ErrDecode             error  // PDU decode failure
    ErrUnsupported        error  // service not supported by this library
    ErrService            error  // remote ConfirmedError response
    ErrDataAccess         error  // per-variable data access error
    ErrAuthentication     error  // authentication failure
)

// Typed errors (use errors.As).
type ServiceError struct {
    Class    ErrorClass
    Code     int
    InvokeID InvokeID
}

type DecodeError struct {
    Offset int
    Tag    byte
    Msg    string
}

type DataAccessError struct {
    Code DataAccessErrorCode
}

type ProtocolError struct {
    Phase   string
    Message string
}

type AuthenticationError struct {
    Mechanism AuthMechanism
    Reason    string
}
```

---

## Transport interfaces

```go
type Transport interface {
    Read(b []byte) (int, error)
    Write(b []byte) (int, error)
    Close() error
}

type TransportListener interface {
    Accept(ctx context.Context) (Transport, error)
    Close() error
}

// Optional interface for TLS inspection.
type TLSTransport interface {
    Transport
    ConnectionState() tls.ConnectionState
}

// Optional interface for peer address inspection.
type RemoteAddrTransport interface {
    Transport
    RemoteAddr() net.Addr
    LocalAddr() net.Addr
}
```

The `transport/iso` sub-package provides `iso.Dial` and `iso.Listen` which implement
these interfaces over the full ISO/OSI upper-layer stack.

---

## Identity, auth, and options

### DialOptions

```go
type DialOptions struct {
    Transport TransportOptions
    ISO       ISOOptions
    MMS       MMSOptions
    Logger    *slog.Logger // nil → silent discard; does not set iso.WithLogger
    RawHook   func(direction string, raw []byte)
}
```

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
    LogicalStatus  VMDLogicalStatus
    PhysicalStatus VMDPhysicalStatus
}
```

### Authentication

```go
type Authenticator func(ctx context.Context, auth *AuthContext) (AuthResult, error)

type AuthContext struct {
    Mechanism AuthMechanism
    Value     []byte
    Remote    net.Addr
}

type AuthResult struct {
    Accepted bool
    Reason   string
}

type AuthMechanism int
const (
    AuthMechanismNone     AuthMechanism = 0
    AuthMechanismPassword AuthMechanism = 1
    AuthMechanismCertificate AuthMechanism = 2
)
```

---

## Deprecated symbols

| Symbol | Replacement |
|--------|-------------|
| `ReadArrayElement` | `ReadByIndex` |
| `DataAccessErrorTemporarilyUnavail` | `DataAccessErrorTemporarilyUnavailable` |
| `DataAccessErrorObjectAccessUnsup` | `DataAccessErrorObjectAccessUnsupported` |
| `VMDPhysicalStatusPartiallyOper` | `VMDPhysicalStatusPartiallyOperational` |
