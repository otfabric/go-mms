# Error Taxonomy

go-mms uses Go's standard error model with sentinel values and typed errors.
Use `errors.Is` for category checks and `errors.As` to extract structured
detail. **Never compare error strings**: messages are for humans and may
change between releases (v1.2.0 added the ISO 9506-2 code name to
`ServiceError.Error()`, for example).

## Classification

Errors are classified by attaching sentinel *kinds* without changing the
message. An internal wrapper exposes them through `Unwrap() []error`, so a
single error can match several sentinels and still match everything in its
original chain (transport errors, `context` errors, typed errors):

| Situation | Matches |
|-----------|---------|
| `NewClient` / `iso.Dial` association failure of any kind | `ErrAssociationFailed` (plus the cause, e.g. `ErrConnectionRejected`, `ErrDecodeFailed`, `*ProtocolError`, `context.DeadlineExceeded`) |
| Peer rejected the ACSE association | `ErrConnectionRejected` and `ErrAssociationFailed` |
| Invalid negotiated MMS version in the Initiate response | `ErrNegotiationFailed`, `ErrAssociationFailed`, `*ProtocolError` (phase `mms`) |
| Request after `Close`/`Abort`, or after the association ended | `ErrClosed` |
| Pending request failed because the connection was lost | `ErrClosed` (plus the transport error) |
| Connection ended by an undecodable PDU | `ErrClosed` and `ErrDecodeFailed` |
| Connection ended by a PDU larger than the negotiated maximum | `ErrClosed` and `ErrInvalidPDU` |
| Request write failed | `ErrClosed` (the transport is closed; other pending requests fail too) |
| Request deadline (context or `DialOptions.RequestTimeout`) | `ErrInvokeTimeout` and `context.DeadlineExceeded` |
| Request context cancelled | `context.Canceled` |
| Invalid caller argument or option | `ErrInvalidArgument` (nothing is sent) |
| Malformed response to a request | `ErrDecodeFailed` |
| ConfirmedErrorPDU | `*ServiceError`, `ErrServiceRejected` |
| RejectPDU | `*RejectError`, `*ProtocolError` (phase `mms`), `ErrProtocol` |
| `Server.Serve` association failure | `ErrAssociationFailed` (or `*AuthenticationError`) |
| `Server.Serve` association ended abnormally | `ErrClosed` |
| `Serve` / `ListenAndServe` after `Server.Shutdown` | `ErrServerClosed` |

`Client.Err()` returns the reason the association ended; it always matches
`ErrClosed`.

## Sentinel Errors

| Sentinel | When returned | Check with |
|----------|---------------|------------|
| `ErrClosed` | Any client method after `Close`/`Abort`, after the peer concluded, released or aborted the association, or after the connection was lost or failed to decode; pending requests fail with it. `Server.Serve` when an established association ends abnormally | `errors.Is(err, mms.ErrClosed)` |
| `ErrInvokeTimeout` | A confirmed request's deadline expired (also matches `context.DeadlineExceeded`) | `errors.Is(err, mms.ErrInvokeTimeout)` |
| `ErrConnectionRejected` | `NewClient`/`Dial` when the peer rejects the ACSE association | `errors.Is(err, mms.ErrConnectionRejected)` |
| `ErrAssociationFailed` | `NewClient`/`Dial` when the association fails for any reason; `Server.Serve` when receiving or accepting the association fails (including `ServerOptions.AssociationTimeout`) | `errors.Is(err, mms.ErrAssociationFailed)` |
| `ErrNegotiationFailed` | `NewClient`/`Dial` when MMS Initiate parameter negotiation fails | `errors.Is(err, mms.ErrNegotiationFailed)` |
| `ErrInvalidPDU` | A received PDU exceeds the negotiated maximum PDU size (the client then closes the association) | `errors.Is(err, mms.ErrInvalidPDU)` |
| `ErrDecodeFailed` | PDU decoding failed; also unwrapped from `*DecodeError` | `errors.Is(err, mms.ErrDecodeFailed)` |
| `ErrUnsupported` | Requested feature or operation is not supported | `errors.Is(err, mms.ErrUnsupported)` |
| `ErrServiceRejected` | Peer returned a ConfirmedErrorPDU; also unwrapped from `*ServiceError` | `errors.Is(err, mms.ErrServiceRejected)` |
| `ErrDataAccess` | Per-variable access error in Read/Write; also unwrapped from `*DataAccessError` | `errors.Is(err, mms.ErrDataAccess)` |
| `ErrProtocol` | Protocol-level error; also unwrapped from `*ProtocolError` and `*RejectError` | `errors.Is(err, mms.ErrProtocol)` |
| `ErrServerConnClosed` | `ServerConn` methods after the connection is closed | `errors.Is(err, mms.ErrServerConnClosed)` |
| `ErrAuthenticationFailed` | Server-side `Serve` when the authenticator rejects the client; also unwrapped from `*AuthenticationError` | `errors.Is(err, mms.ErrAuthenticationFailed)` |
| `ErrFileAccessDenied` | `FileProvider`/`JournalProvider` implementations signal permission errors; server maps it to MMS error class "file", code "file-access-denied" | `errors.Is(err, mms.ErrFileAccessDenied)` |
| `ErrServerClosed` | `Server.Serve` and `Server.ListenAndServe` after `Server.Shutdown` | `errors.Is(err, mms.ErrServerClosed)` |
| `ErrInvalidArgument` | A caller-supplied argument or option is invalid (empty names, nil values, mismatched slices, values that cannot be encoded, bad selectors); the request is not sent | `errors.Is(err, mms.ErrInvalidArgument)` |

Timeouts and cancellation also match the standard `context.DeadlineExceeded`
and `context.Canceled`.

### Phases

`ProtocolError.Phase` is one of the constants `PhaseSession`
(`"session"`), `PhasePresentation` (`"presentation"`), `PhaseACSE`
(`"acse"`) or `PhaseMMS` (`"mms"`).

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
- **`CodeName()`** returns the ISO 9506-2 name of `Code` within `Class`
  (e.g. `"object-non-existent"` for class Access, code 2), or `""` when the
  code is not defined for the class and for class Others. `Error()` includes
  it, e.g. `mms: service error: class=Access code=2 (object-non-existent) invokeID=7`.
  Switch on `Class` and `Code`, not on the message.

**Server side.** An Identify or Status handler (`HandleIdentify`,
`HandleStatus`) that returns a `*ServiceError` (possibly wrapped) has its
`Class` and `Code` sent in the ConfirmedErrorPDU; other handler errors are
sent as class Access, code 1. `FileProvider` and `JournalProvider` errors
are mapped by kind (`fs.ErrNotExist`, `fs.ErrPermission`,
`ErrFileAccessDenied`, other). A Status request with a malformed body is
answered with class Service, code other (was class Resource).

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

**Server side.** A `Variable.Read` or `Variable.Write` callback that
returns a `*DataAccessError` (possibly wrapped) with a valid code (0–11) has
that code sent in the access result. Any other callback error is sent as
`DataAccessErrorTemporarilyUnavailable`.

#### `DataAccessErrorCode` values

| Constant | Value | Meaning |
|----------|-------|---------|
| `DataAccessErrorNone` | -1 | No error (Go-only sentinel; never sent on the wire) |
| `DataAccessErrorObjectInvalidated` | 0 | Object has been invalidated |
| `DataAccessErrorHardwareFault` | 1 | Hardware fault |
| `DataAccessErrorTemporarilyUnavailable` | 2 | Temporarily unavailable |
| `DataAccessErrorObjectAccessDenied` | 3 | Access denied |
| `DataAccessErrorObjectUndefined` | 4 | Object does not exist |
| `DataAccessErrorInvalidAddress` | 5 | Invalid address |
| `DataAccessErrorTypeUnsupported` | 6 | Type unsupported |
| `DataAccessErrorTypeInconsistent` | 7 | Type inconsistent |
| `DataAccessErrorObjectAttributeInconsistent` | 8 | Object attribute inconsistent |
| `DataAccessErrorObjectAccessUnsupported` | 9 | Object access unsupported |
| `DataAccessErrorObjectNonExistent` | 10 | Object non-existent |
| `DataAccessErrorObjectValueInvalid` | 11 | Object value invalid |

Values 0–11 match the ISO 9506-2 `DataAccessError` wire encoding. The
go-mms server answers a Read or Write of an unknown domain or variable with
`ObjectNonExistent`, as libiec61850 does, and an alternate access that does
not match the variable's type with `ObjectUndefined`. The
deprecated aliases `DataAccessErrorTemporarilyUnavail`,
`DataAccessErrorObjectAccessUnsup`, `DataAccessErrorTypeMismatch`
(= `TypeUnsupported`) and `DataAccessErrorObjectExists`
(= `ObjectAttributeInconsistent`) remain for compatibility.

### `*ProtocolError`

Returned when a wire-level protocol violation is detected at any layer
of the ISO stack.

| Field | Type | Description |
|-------|------|-------------|
| `Phase` | `string` | ISO stack layer: `PhaseSession`, `PhasePresentation`, `PhaseACSE` or `PhaseMMS` |
| `Message` | `string` | Human-readable description |

- **Unwraps to:** `ErrProtocol`
- **Check with:** `errors.As(err, &pe)` and/or `errors.Is(err, mms.ErrProtocol)`

Common `*ProtocolError` situations:
- Unexpected PDU type in a confirmed response
- Invoke ID mismatch between request and response
- Peer sent a Reject PDU (returned as `*RejectError`, which unwraps to a `*ProtocolError`)
- Missing MMS Initiate Response in accepted association
- Pagination stall in `GetNameListAll`
- Connection closed before conclude response

### `*RejectError`

Returned when the peer answers a confirmed request with an MMS RejectPDU.

| Field | Type | Description |
|-------|------|-------------|
| `Type` | `RejectType` | Which PDU kind was rejected (the rejectReason CHOICE alternative) |
| `Reason` | `int` | Reject code within `Type` (e.g. 1 = unrecognized-service for `RejectConfirmedRequest`) |
| `InvokeID` | `InvokeID` | Invoke ID echoed by the peer, valid when `HasInvokeID` |
| `HasInvokeID` | `bool` | Whether the RejectPDU carried an invoke ID |

- **Unwraps to:** `*ProtocolError` with `Phase == PhaseMMS`, and through it
  `ErrProtocol`. Code written for earlier releases that checked
  `errors.Is(err, mms.ErrProtocol)` or `errors.As(err, &pe)` keeps working.
- **Check with:** `errors.As(err, &re)`

`RejectType` values (`String()` returns the ISO 9506-2 name):

| Constant | Value | Name |
|----------|------:|------|
| `RejectConfirmedRequest` | 1 | `confirmed-requestPDU` |
| `RejectConfirmedResponse` | 2 | `confirmed-responsePDU` |
| `RejectConfirmedError` | 3 | `confirmed-errorPDU` |
| `RejectUnconfirmed` | 4 | `unconfirmedPDU` |
| `RejectPDUError` | 5 | `pdu-error` |
| `RejectCancelRequest` | 6 | `cancel-requestPDU` |
| `RejectCancelResponse` | 7 | `cancel-responsePDU` |
| `RejectCancelError` | 8 | `cancel-errorPDU` |
| `RejectConcludeRequest` | 9 | `conclude-requestPDU` |
| `RejectConcludeResponse` | 10 | `conclude-responsePDU` |
| `RejectConcludeError` | 11 | `conclude-errorPDU` |

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
*RejectError         → *ProtocolError (phase mms) → ErrProtocol
*AuthenticationError → ErrAuthenticationFailed
```

This means `errors.Is` works transitively — checking for the sentinel
matches both the bare sentinel and the typed error that wraps it.

Transport and context errors are wrapped with `fmt.Errorf("mms: ...: %w", err)`,
preserving the original error for `errors.Is` checks (e.g.,
`context.DeadlineExceeded`, `context.Canceled`, `io.EOF`). Sentinel kinds
(see [Classification](#classification)) are added on top without changing
the message.

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
    fmt.Printf("service error: class=%s code=%d (%s) invokeID=%d\n",
        se.Class, se.Code, se.CodeName(), se.InvokeID)
    if se.Class == mms.ErrorClassAccess && se.Code == 2 { // object-non-existent
        // ...
    }
}
```

### Checking for a per-variable data access error

```go
_, err := client.Read(ctx, mms.ReadRequest{DomainID: "D", ItemID: "V"})
var dae *mms.DataAccessError
if errors.As(err, &dae) {
    fmt.Printf("data access error: %s\n", dae.Code)
    switch dae.Code {
    case mms.DataAccessErrorObjectNonExistent, mms.DataAccessErrorObjectUndefined:
        // variable does not exist on the server (servers use either code)
    }
}
```

### Checking for timeout / cancellation

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

_, err := client.Read(ctx, mms.ReadRequest{DomainID: "D", ItemID: "V"})
if errors.Is(err, mms.ErrInvokeTimeout) { // also matches context.DeadlineExceeded
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

### Checking for a Reject PDU

```go
_, err := client.Identify(ctx)
var re *mms.RejectError
if errors.As(err, &re) {
    fmt.Printf("rejected: %s reason=%d\n", re.Type, re.Reason)
}
```

### Checking for an invalid argument

```go
_, err := client.Read(ctx, mms.ReadRequest{DomainID: "", ItemID: "V"})
if errors.Is(err, mms.ErrInvalidArgument) {
    // caller bug; nothing was sent and the association is unaffected
}
```

### Detecting a lost association

```go
<-client.Done()
err := client.Err() // matches mms.ErrClosed; wraps the cause
```

### Checking for association rejection during connect

```go
client, err := mms.NewClient(ctx, transport, opts)
if errors.Is(err, mms.ErrConnectionRejected) {
    log.Println("server rejected the association")
} else if errors.Is(err, mms.ErrAssociationFailed) {
    log.Println("association failed:", err)
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

### Server shutdown

```go
go func() { _ = srv.Shutdown(shutdownCtx) }()
if err := srv.ListenAndServe(ctx, ln); !errors.Is(err, mms.ErrServerClosed) {
    log.Fatal(err)
}
```

### Server-side service error from a handler

```go
srv.HandleIdentify(func(ctx context.Context, _ mms.IdentifyRequest) (*mms.ServerIdentity, error) {
    // Sent on the wire as class Access, code 3 (object-access-denied).
    return nil, &mms.ServiceError{Class: mms.ErrorClassAccess, Code: 3}
})
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
├── errors.Is(err, mms.ErrInvalidArgument)
│   └── Caller bug — nothing was sent; fix the arguments
├── errors.Is(err, mms.ErrClosed)
│   └── Association ended — client.Err() has the cause; reconnect (mms.Retry) or exit
├── errors.Is(err, mms.ErrInvokeTimeout)  (also context.DeadlineExceeded)
│   └── Request timed out — retry or widen deadline; the association stays up
├── errors.Is(err, context.Canceled)
│   └── Caller cancelled — usually intentional
├── errors.Is(err, mms.ErrServiceRejected)
│   └── errors.As(err, &se) → inspect se.Class, se.Code
├── errors.Is(err, mms.ErrDataAccess)
│   └── errors.As(err, &dae) → inspect dae.Code
├── errors.Is(err, mms.ErrProtocol)
│   ├── errors.As(err, &re) → RejectPDU: inspect re.Type, re.Reason
│   └── errors.As(err, &pe) → inspect pe.Phase, pe.Message
├── errors.Is(err, mms.ErrDecodeFailed)
│   └── errors.As(err, &de) → inspect de.Offset, de.Tag
├── errors.Is(err, mms.ErrConnectionRejected)
│   └── Peer rejected association — check credentials / config
├── errors.Is(err, mms.ErrAssociationFailed)
│   └── Association could not be established — mms.IsRetryable(err) says whether to retry
└── other
    └── Transport-level or unexpected error — inspect err.Error()
```
