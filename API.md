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

Per-variable write handling is set via `Variable.Write` at registration time.

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
)

// Typed errors (use errors.As).
type ServiceError struct {
    Class    ErrorClass
    Code     int
    InvokeID InvokeID
}

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
| `DataAccessErrorTypeMismatch` | `DataAccessErrorTypeUnsupported` |
| `DataAccessErrorObjectExists` | `DataAccessErrorObjectAttributeInconsistent` |
| `VMDPhysicalStatusPartiallyOper` | `VMDPhysicalStatusPartiallyOperational` |
