# Error Taxonomy

go-mms uses Go's standard error model with sentinel values and typed errors.
All errors returned by the library wrap one of the sentinel values or a typed
error struct that can be inspected with `errors.As`. Use `errors.Is` for
category checks and `errors.As` to extract structured detail.

## Error Categories

### Closed connection

Operation attempted on a client or server connection that has already been
closed (via `Close` or `Abort`).

### Association rejection

The remote peer rejected the ISO/ACSE association request during connection
setup.

### Association failure

The association handshake failed for a reason other than explicit rejection
(e.g., transport error during setup).

### Negotiation failure

The MMS Initiate handshake completed but parameter negotiation failed
(e.g., invalid negotiated version).

### Protocol violation

Wire-level protocol error at any ISO stack layer (session, presentation,
ACSE, or MMS). Covers unexpected PDU types, invoke ID mismatches, malformed
responses, and reject PDUs from the peer.

### Decode failure

A received PDU could not be parsed. The `*DecodeError` typed error carries
the byte offset and tag where parsing failed.

### Invalid PDU

A PDU is structurally invalid (reserved for malformed PDU detection).

### Remote service error

The remote peer returned a `ConfirmedErrorPDU`, indicating the requested
MMS service was rejected at the application level. The `*ServiceError`
typed error carries the error class, error code, and invoke ID.

### Per-variable data access error

An individual variable within a multi-variable Read or Write response
reported an access error. This is distinct from a `ServiceError` — the
overall Read/Write succeeded, but one or more variables had an access-level
failure (e.g., object undefined, access denied).

### Unsupported

The requested operation or feature is not supported by the library.

### Invoke timeout

A confirmed request timed out waiting for a response from the peer.

### Authentication failure

The server-side authenticator rejected the client's credentials during
association establishment. The `*AuthenticationError` typed error carries
the rejection reason.

### Server connection closed

An operation was attempted on a `ServerConn` that has already been closed
(the `Serve` call that created it has returned).

### File access denied

A `FileProvider` or `JournalProvider` implementation reported a permission
error. Used server-side to map provider errors to MMS service error codes.

### Timeout / cancellation

Context deadline exceeded or cancelled. These use the standard
`context.DeadlineExceeded` and `context.Canceled` errors; no custom
sentinel is needed.

## Sentinel Errors

| Sentinel | Category | When returned | Check with |
|----------|----------|---------------|------------|
| `ErrClosed` | Closed connection | Any client method after `Close`/`Abort`; pending requests cancelled during shutdown | `errors.Is(err, mms.ErrClosed)` |
| `ErrInvokeTimeout` | Invoke timeout | Confirmed request timed out waiting for peer response | `errors.Is(err, mms.ErrInvokeTimeout)` |
| `ErrConnectionRejected` | Association rejection | `NewClient`/`Dial` when the peer rejects the ACSE association | `errors.Is(err, mms.ErrConnectionRejected)` |
| `ErrAssociationFailed` | Association failure | `NewClient`/`Dial` when the association handshake fails | `errors.Is(err, mms.ErrAssociationFailed)` |
| `ErrNegotiationFailed` | Negotiation failure | `NewClient`/`Dial` when MMS Initiate parameter negotiation fails | `errors.Is(err, mms.ErrNegotiationFailed)` |
| `ErrInvalidPDU` | Invalid PDU | A received PDU is structurally invalid | `errors.Is(err, mms.ErrInvalidPDU)` |
| `ErrDecodeFailed` | Decode failure | PDU decoding failed; also unwrapped from `*DecodeError` | `errors.Is(err, mms.ErrDecodeFailed)` |
| `ErrUnsupported` | Unsupported | Requested feature or operation is not supported | `errors.Is(err, mms.ErrUnsupported)` |
| `ErrServiceRejected` | Remote service error | Peer returned a ConfirmedErrorPDU; also unwrapped from `*ServiceError` | `errors.Is(err, mms.ErrServiceRejected)` |
| `ErrDataAccess` | Data access error | Per-variable access error in Read/Write; also unwrapped from `*DataAccessError` | `errors.Is(err, mms.ErrDataAccess)` |
| `ErrProtocol` | Protocol violation | Protocol-level error; also unwrapped from `*ProtocolError` | `errors.Is(err, mms.ErrProtocol)` |
| `ErrServerConnClosed` | Server connection closed | `ServerConn.SendInformationReport` after the connection is closed | `errors.Is(err, mms.ErrServerConnClosed)` |
| `ErrAuthenticationFailed` | Authentication failure | Server-side `Serve` when the authenticator rejects the client; also unwrapped from `*AuthenticationError` | `errors.Is(err, mms.ErrAuthenticationFailed)` |
| `ErrFileAccessDenied` | File access denied | `FileProvider`/`JournalProvider` implementations signal permission errors; server maps it to MMS error class "file", code "file-access-denied" | `errors.Is(err, mms.ErrFileAccessDenied)` |

## Typed Errors

### `*ServiceError`

Returned when the remote peer sends a ConfirmedErrorPDU in response to a
confirmed service request (Read, Write, GetNameList, etc.).

| Field | Type | Description |
|-------|------|-------------|
| `Class` | `ErrorClass` | MMS error class (VMDState, ApplicationReference, Definition, Resource, Service, Access, File, etc.) |
| `Code` | `int` | Error code within the class |
| `InvokeID` | `InvokeID` | Correlation ID of the failed request |

- **Unwraps to:** `ErrServiceRejected`
- **Check with:** `errors.As(err, &se)` and/or `errors.Is(err, mms.ErrServiceRejected)`

### `*DecodeError`

Returned when a received PDU cannot be decoded (malformed BER/ASN.1).

| Field | Type | Description |
|-------|------|-------------|
| `Offset` | `int` | Byte offset in the PDU where the error was detected |
| `Tag` | `byte` | The BER tag at the error location |
| `Message` | `string` | Human-readable description |

- **Unwraps to:** `ErrDecodeFailed`
- **Check with:** `errors.As(err, &de)` and/or `errors.Is(err, mms.ErrDecodeFailed)`

### `*DataAccessError`

Returned by single-variable convenience methods (`Read`, `Write`,
`ReadObject`, `WriteObject`, `ReadComponent`, `WriteComponent`,
`ReadByIndex`, `ReadArrayElement`, `WriteArrayElement`,
`ReadArrayRange`) when the server reports a per-variable access error
within an otherwise successful response.

| Field | Type | Description |
|-------|------|-------------|
| `Code` | `DataAccessErrorCode` | One of the `DataAccessError*` constants (see below) |

- **Unwraps to:** `ErrDataAccess`
- **Check with:** `errors.As(err, &dae)` and/or `errors.Is(err, mms.ErrDataAccess)`

#### `DataAccessErrorCode` values

| Constant | Value | Meaning |
|----------|-------|---------|
| `DataAccessErrorNone` | 0 | No error |
| `DataAccessErrorObjectInvalidated` | 1 | Object has been invalidated |
| `DataAccessErrorHardwareFault` | 2 | Hardware fault |
| `DataAccessErrorTemporarilyUnavail` | 3 | Temporarily unavailable |
| `DataAccessErrorObjectAccessDenied` | 4 | Access denied |
| `DataAccessErrorObjectUndefined` | 5 | Object does not exist |
| `DataAccessErrorInvalidAddress` | 6 | Invalid address |
| `DataAccessErrorTypeMismatch` | 7 | Type mismatch |
| `DataAccessErrorTypeInconsistent` | 8 | Type inconsistent |
| `DataAccessErrorObjectExists` | 9 | Object already exists |
| `DataAccessErrorObjectAccessUnsup` | 10 | Object access unsupported |

### `*ProtocolError`

Returned when a wire-level protocol violation is detected at any layer
of the ISO stack.

| Field | Type | Description |
|-------|------|-------------|
| `Phase` | `string` | ISO stack layer: `"session"`, `"presentation"`, `"acse"`, or `"mms"` |
| `Message` | `string` | Human-readable description |

- **Unwraps to:** `ErrProtocol`
- **Check with:** `errors.As(err, &pe)` and/or `errors.Is(err, mms.ErrProtocol)`

Common `*ProtocolError` situations:
- Unexpected PDU type in a confirmed response
- Invoke ID mismatch between request and response
- Peer sent a Reject PDU
- Missing MMS Initiate Response in accepted association
- Pagination stall in `GetNameListAll`
- Connection closed before conclude response

### `*AuthenticationError`

Returned server-side by `Server.Serve` when the configured authenticator
rejects the client during association establishment.

| Field | Type | Description |
|-------|------|-------------|
| `Reason` | `string` | Human-readable rejection reason |

- **Unwraps to:** `ErrAuthenticationFailed`
- **Check with:** `errors.As(err, &ae)` and/or `errors.Is(err, mms.ErrAuthenticationFailed)`

## `ErrorClass` values

The `ErrorClass` type categorizes MMS service errors from
ConfirmedErrorPDU responses (the `Class` field of `*ServiceError`).

| Constant | Value | Meaning |
|----------|-------|---------|
| `ErrorClassVMDState` | 0 | VMD state error |
| `ErrorClassAppReference` | 1 | Application reference error |
| `ErrorClassDefinition` | 2 | Definition error |
| `ErrorClassResource` | 3 | Resource error |
| `ErrorClassService` | 4 | Service error |
| `ErrorClassServicePreempt` | 5 | Service preempted |
| `ErrorClassTimeResolution` | 6 | Time resolution error |
| `ErrorClassAccess` | 7 | Access error |
| `ErrorClassInitiate` | 8 | Initiate error |
| `ErrorClassConclude` | 9 | Conclude error |
| `ErrorClassCancel` | 10 | Cancel error |
| `ErrorClassFile` | 11 | File error |
| `ErrorClassOthers` | 12 | Other error |

## Error Wrapping Model

Every typed error implements `Unwrap()` returning its sentinel:

```
*ServiceError        → ErrServiceRejected
*DecodeError         → ErrDecodeFailed
*DataAccessError     → ErrDataAccess
*ProtocolError       → ErrProtocol
*AuthenticationError → ErrAuthenticationFailed
```

This means `errors.Is` works transitively — checking for the sentinel
matches both the bare sentinel and the typed error that wraps it.

Transport and context errors are wrapped with `fmt.Errorf("mms: ...: %w", err)`,
preserving the original error for `errors.Is` checks (e.g.,
`context.DeadlineExceeded`, `context.Canceled`, `io.EOF`).

## Usage Examples

### Checking for a closed connection

```go
_, err := client.Identify(ctx)
if errors.Is(err, mms.ErrClosed) {
    log.Println("client was already closed")
}
```

### Checking for a remote service error

```go
_, err := client.Read(ctx, mms.ReadRequest{DomainID: "D", ItemID: "V"})
var se *mms.ServiceError
if errors.As(err, &se) {
    fmt.Printf("service error: class=%s code=%d invokeID=%d\n",
        se.Class, se.Code, se.InvokeID)
}
```

### Checking for a per-variable data access error

```go
_, err := client.Read(ctx, mms.ReadRequest{DomainID: "D", ItemID: "V"})
var dae *mms.DataAccessError
if errors.As(err, &dae) {
    fmt.Printf("data access error: %s\n", dae.Code)
    if dae.Code == mms.DataAccessErrorObjectUndefined {
        // variable does not exist on the server
    }
}
```

### Checking for timeout / cancellation

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

_, err := client.Read(ctx, mms.ReadRequest{DomainID: "D", ItemID: "V"})
if errors.Is(err, context.DeadlineExceeded) {
    log.Println("request timed out")
}
if errors.Is(err, context.Canceled) {
    log.Println("request was cancelled")
}
```

### Checking for a protocol error

```go
_, err := client.Identify(ctx)
var pe *mms.ProtocolError
if errors.As(err, &pe) {
    fmt.Printf("protocol error at %s layer: %s\n", pe.Phase, pe.Message)
}
```

### Checking for association rejection during connect

```go
client, err := mms.NewClient(ctx, transport, opts)
if errors.Is(err, mms.ErrConnectionRejected) {
    log.Println("server rejected the association")
}
```

### Handling per-variable errors in multi-variable reads

```go
results, err := client.ReadMultiple(ctx, []mms.ObjectName{
    {Scope: mms.ObjectScopeDomain, Domain: "D", ItemID: "var1"},
    {Scope: mms.ObjectScopeDomain, Domain: "D", ItemID: "var2"},
    {Scope: mms.ObjectScopeDomain, Domain: "D", ItemID: "var3"},
})
if err != nil {
    // Transport, protocol, or service-level failure — no results at all.
    log.Fatal(err)
}

// ReadMultiple does NOT return a *DataAccessError as err.
// Instead, each AccessResult has its own ErrorCode field.
for i, r := range results {
    if r.ErrorCode != 0 {
        fmt.Printf("variable [%d]: access error %s\n", i, r.ErrorCode)
        continue
    }
    fmt.Printf("variable [%d]: %v\n", i, r.Value)
}
```

### Handling per-variable errors in multi-variable writes

```go
results, err := client.WriteVariables(ctx,
    []mms.VariableSpec{
        {Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "D", ItemID: "v1"}},
        {Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "D", ItemID: "v2"}},
    },
    []*mms.Value{mms.NewInteger(42), mms.NewFloat(3.14)},
)
if err != nil {
    log.Fatal(err)
}

for _, r := range results {
    if !r.Success {
        fmt.Printf("write [%d] failed: %s\n", r.Index, r.ErrorCode)
    }
}
```

### Server-side authentication error handling

```go
err := server.Serve(ctx, conn)
var authErr *mms.AuthenticationError
if errors.As(err, &authErr) {
    log.Printf("client authentication failed: %s", authErr.Reason)
}
```

### Server-side file access denied

```go
// In a FileProvider implementation:
func (p *myProvider) Open(ctx context.Context, name string, opts mms.FileOpenOptions) (mms.FileHandle, uint32, error) {
    if !p.hasPermission(name) {
        return nil, 0, mms.ErrFileAccessDenied
    }
    // ...
}
```

## Decision Tree

```
err from client method
├── errors.Is(err, mms.ErrClosed)
│   └── Client was closed — reconnect or exit
├── errors.Is(err, context.DeadlineExceeded)
│   └── Request timed out — retry or widen deadline
├── errors.Is(err, context.Canceled)
│   └── Caller cancelled — usually intentional
├── errors.Is(err, mms.ErrServiceRejected)
│   └── errors.As(err, &se) → inspect se.Class, se.Code
├── errors.Is(err, mms.ErrDataAccess)
│   └── errors.As(err, &dae) → inspect dae.Code
├── errors.Is(err, mms.ErrProtocol)
│   └── errors.As(err, &pe) → inspect pe.Phase, pe.Message
├── errors.Is(err, mms.ErrDecodeFailed)
│   └── errors.As(err, &de) → inspect de.Offset, de.Tag
├── errors.Is(err, mms.ErrConnectionRejected)
│   └── Peer rejected association — check credentials / config
└── other
    └── Transport-level or unexpected error — inspect err.Error()
```
