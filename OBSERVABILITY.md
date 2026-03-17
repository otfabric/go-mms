# Observability

go-mms uses Go's `log/slog` structured logging framework. Logging is
disabled by default and can be enabled at various levels.

## Default Behavior

When no logger is configured, go-mms creates a logger backed by an
internal `discardHandler` — a `slog.Handler` whose `Enabled` method
always returns `false`. This means no log records are allocated, formatted,
or written. There is effectively zero overhead.

## Logging Levels

| Level | What is logged | When to use |
|-------|---------------|-------------|
| Disabled (default) | Nothing | Production with no debugging needs |
| `slog.LevelInfo` | Lifecycle events (connected, closed, aborted, concluded, listening) | Basic monitoring |
| `slog.LevelDebug` | All of the above, plus per-request protocol summaries with invoke IDs and service types | Development |
| `slog.LevelWarn` | Protocol anomalies, authentication rejections, broadcast failures (always emitted when a logger is configured) | Production with a filtered handler |

## Configuration

### Client

Pass a `*slog.Logger` via `DialOptions.Logger`:

```go
logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
    Level: slog.LevelDebug,
}))

client, err := mms.NewClient(ctx, conn, mms.DialOptions{
    Logger: logger,
})
```

### Server

Pass a `*slog.Logger` via `ServerOptions.Logger`:

```go
logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
    Level: slog.LevelDebug,
}))

srv := mms.NewServer(mms.ServerOptions{
    Logger: logger,
    MMS:    mms.ServerMMSOptions{MaxPDUSize: 65000},
})
```

### ISO Transport Layer

The `transport/iso` package has its own `WithLogger` option, used by the
`Listener` to log per-connection handshake failures (TLS and COTP):

```go
ln, err := iso.Listen("tcp", ":102",
    iso.WithLogger(logger),
)
```

## Log Messages

### Client — Info Level

| Message | Fields | When |
|---------|--------|------|
| `mms: connected` | `max_pdu_size`, `max_outstanding_calling`, `max_outstanding_called` | Association established |
| `mms: closed` | — | Graceful close (after ConcludeRequest/Response) |
| `mms: aborted` | — | Hard abort (no conclude handshake) |

### Client — Debug Level

| Message | Fields | When |
|---------|--------|------|
| `mms: negotiated` | `max_pdu_size`, `max_outstanding_calling`, `max_outstanding_called`, `nesting_level`, `version` | After MMS Initiate handshake |
| `mms: identify` | `invoke_id`, `service`, `vendor`, `model`, `revision` | After Identify response |
| `mms: status` | `invoke_id`, `service`, `logical`, `physical` | After Status response |
| `mms: read` | `invoke_id`, `service`, `variables`, `results` | After Read response |
| `mms: write` | `invoke_id`, `service`, `domain_id`, `item_id` | After single Write response |
| `mms: readVariables` | `invoke_id`, `service`, `variables`, `results` | After ReadVariables response |
| `mms: writeVariables` | `invoke_id`, `service`, `variables` | After WriteVariables response |
| `mms: writeObject` | `invoke_id`, `service`, `scope`, `item_id` | After WriteObject response |
| `mms: readNamedVariableList` | `invoke_id`, `service`, `list_name`, `results` | After ReadNamedVariableList response |
| `mms: writeNamedVariableList` | `invoke_id`, `service`, `list_name` | After WriteNamedVariableList response |
| `mms: getnamelist` | `invoke_id`, `service`, `names`, `more_follows` | After GetNameList response |
| `mms: getvaraccess` | `invoke_id`, `service`, `type`, `deletable` | After GetVariableAccessAttributes response |
| `mms: define named variable list` | `invoke_id`, `service`, `list_name`, `variables` | After DefineNamedVariableList response |
| `mms: get named variable list attributes` | `invoke_id`, `service`, `deletable`, `variables` | After GetNamedVariableListAttributes response |
| `mms: delete named variable list` | `invoke_id`, `service`, `matched`, `deleted` | After DeleteNamedVariableList response |
| `mms: reader loop stopped` | `error` | Reader goroutine exits (transport error) |
| `mms: reader loop decode error` | `error` | ISO stack decode failure in reader |
| `mms: reader loop PDU error` | `error` | MMS PDU decode failure in reader |
| `mms: discarding late response during shutdown` | `invoke_id` | Response arrives after Close started |

### Client — Warn Level

| Message | Fields | When |
|---------|--------|------|
| `mms: server rejected conclude` | — | Server sent ConcludeError |
| `mms: reader loop unexpected PDU` | `kind` | Unhandled PDU type in reader |
| `mms: reader cannot extract invoke ID` | `error` | Malformed confirmed response |
| `mms: reader got response for unknown invoke ID` | `invoke_id` | Response with no pending request |
| `mms: reader cannot decode InformationReport` | `error` | Malformed InformationReport |
| `mms: reader cannot convert InformationReport` | `error` | InformationReport conversion failure |

### Client — Error Level

| Message | Fields | When |
|---------|--------|------|
| `mms: InformationReport handler panic` | `panic` | User-registered handler panicked (recovered) |

### Server — Info Level

| Message | Fields | When |
|---------|--------|------|
| `serverconn: association accepted` | `max_pdu_size`, `max_outstanding_calling` | MMS association accepted |
| `serverconn: concluded` | — | Client sent ConcludeRequest, response sent |
| `serverconn: released` | — | Client sent session Release, response sent |
| `server listening` | `addr` | ListenAndServe started |
| `connection closed` | `error` | Per-connection Serve returned |

### Server — Warn Level

| Message | Fields | When |
|---------|--------|------|
| `serverconn: PDU exceeds negotiated size` | `size`, `max` | Oversized PDU received (skipped) |
| `serverconn: malformed PDU` | `error` | PDU decode failure |
| `serverconn: unexpected PDU kind` | `kind` | Unhandled PDU type |
| `serverconn: malformed confirmed request` | `error` | Confirmed request decode failure |
| `authentication error` | `error` | Authenticator returned an error |
| `authentication rejected` | — | Authenticator returned Accept=false |
| `mms: broadcast send failed` | `error` | Broadcast to a connection failed |
| `temporary accept error, retrying` | `error` | Transient accept error in ListenAndServe |

### ISO Transport — Warn Level

| Message | Fields | When |
|---------|--------|------|
| `tls handshake failed, closing connection` | `remote`, `error` | TLS negotiation failed on accept |
| `cotp handshake failed, closing connection` | `remote`, `error` | COTP negotiation failed on accept |

## Log Fields

| Field | Type | Description |
|-------|------|-------------|
| `invoke_id` | int | MMS invoke ID for request/response correlation |
| `service` | string | MMS service name (`Identify`, `Status`, `Read`, `Write`, `GetNameList`, etc.) |
| `max_pdu_size` | int | Negotiated maximum PDU size in bytes |
| `max_outstanding_calling` | int | Negotiated max outstanding calling requests |
| `max_outstanding_called` | int | Negotiated max outstanding called requests |
| `nesting_level` | int | Negotiated data structure nesting depth |
| `version` | int | Negotiated MMS protocol version |
| `variables` | int | Number of variables in a multi-variable request |
| `results` | int | Number of results returned |
| `names` | int | Number of names in a GetNameList response |
| `more_follows` | bool | Whether more names follow (pagination) |
| `domain_id` | string | MMS domain identifier |
| `item_id` | string | MMS item identifier |
| `list_name` | string | Named variable list identifier |
| `scope` | int | Object scope (0=VMD, 1=domain, 2=association) |
| `vendor` | string | Server vendor name from Identify |
| `model` | string | Server model name from Identify |
| `revision` | string | Server revision from Identify |
| `logical` | int | VMD logical status |
| `physical` | int | VMD physical status |
| `type` | string | Variable type description |
| `deletable` | bool | Whether an object is deletable |
| `matched` | int | Number of matched objects in delete |
| `deleted` | int | Number of actually deleted objects |
| `error` | error/string | Error detail for warning/debug messages |
| `kind` | string/int | PDU kind for unexpected PDU warnings |
| `remote` | string | Remote address (ISO transport layer) |
| `addr` | string | Listener address |
| `size` | int | Received PDU size (oversized PDU warning) |
| `max` | int | Negotiated max size (oversized PDU warning) |
| `panic` | any | Recovered panic value |

## Raw Wire Hooks

For wire-level debugging, use the `RawHook` field in `DialOptions` to
inspect raw ISO upper-layer bytes on every send and receive:

```go
client, err := mms.NewClient(ctx, conn, mms.DialOptions{
    RawHook: func(direction string, data []byte) {
        fmt.Printf("[%s] %x\n", direction, data)
    },
})
```

The hook operates at the COTP user-data level (session SPDU bytes that
embed presentation, ACSE, and MMS data), not at the decoded MMS PDU level.
The `direction` parameter is `"send"` or `"recv"`.

**Ownership note:** The `data` slice may be reused after the callback
returns. Copy it if you need to retain the data.

`RawHook` is only available on the client side (`DialOptions`). The server
does not currently expose a raw hook.

## Redaction

No authentication-sensitive data is included in log messages. Password
values from ACSE authentication are handled in the internal ACSE layer
and are never passed to the logger. The `Authenticator` callback's
`AuthContext` (which may contain the password) is not logged by the
library — only the accept/reject outcome appears in logs.

## Best Practices

1. **Leave logging disabled in production** unless actively debugging —
   the discard handler has zero allocation overhead.
2. Use `slog.LevelInfo` for basic lifecycle monitoring (connect/close/abort).
3. Use `slog.LevelDebug` during development to see per-request invoke IDs
   and service details.
4. Use `RawHook` for wire-level protocol analysis and packet capture replay.
5. **Avoid blocking in `RawHook` callbacks** — they run synchronously in
   the send/receive path and will stall the connection.
6. The ISO transport layer has a separate logger (`iso.WithLogger`); set it
   if you need visibility into TLS/COTP handshake failures on the server
   accept path.
