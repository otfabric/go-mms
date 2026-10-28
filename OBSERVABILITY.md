# Observability

go-mms reports through two independent channels, both off by default:

- **Logging** via Go's `log/slog` (`DialOptions.Logger`,
  `ServerOptions.Logger`, `iso.WithLogger`).
- **Metrics** via the `Metrics` interface (`DialOptions.Metrics`,
  `ServerOptions.Metrics`). go-mms has no metrics dependency; you adapt
  the interface to Prometheus, OpenTelemetry, expvar, etc.

## Metrics

```go
type Metrics interface {
    ObserveRequest(RequestEvent)
    ObserveConnection(ConnectionEvent)
}
```

| Type | Fields / values |
|------|-----------------|
| `RequestEvent` | `Role`, `Service`, `InvokeID`, `Duration`, `Err` |
| `ConnectionEvent` | `Role`, `State`, `RemoteAddr`, `Err` |
| `Role` | `RoleClient` (`"client"`), `RoleServer` (`"server"`) |
| `ConnectionState` | `ConnectionEstablished`, `ConnectionRejected`, `ConnectionClosed` (`"established"`, `"rejected"`, `"closed"`) |
| `Service` | `ServiceStatus`, `ServiceGetNameList`, `ServiceIdentify`, `ServiceRead`, `ServiceWrite`, `ServiceGetVariableAccessAttributes`, `ServiceDefineNamedVariableList`, `ServiceGetNamedVariableListAttributes`, `ServiceDeleteNamedVariableList`, `ServiceFileOpen`, `ServiceFileRead`, `ServiceFileClose`, `ServiceFileRename`, `ServiceFileDelete`, `ServiceFileDirectory`, `ServiceObtainFile`, `ServiceReadJournal`, `ServiceUnknown`; `String()` returns the ISO 9506 name (`"Read"`, `"GetNameList"`, ...) |

Semantics:

- `ObserveRequest` is called once per confirmed request: on the client when
  the response or error is delivered to the caller (`Duration` = send to
  response), on the server when the response or error PDU has been sent
  (`Duration` = request received to response sent). `Err` is nil on
  success; on the client it is the error returned to the caller, on the
  server the handler error turned into a ConfirmedErrorPDU or the error
  that prevented sending the response.
- `ObserveConnection` is called when an association is established,
  rejected (`Err` holds the reason, e.g. an `Authenticator` rejection) or
  closed (`Err` nil after an orderly Conclude/Release, and on the
  client after `Close`/`Abort`; the cause otherwise). `RemoteAddr` is set when the
  transport implements `RemoteAddrTransport`.
- Methods run synchronously on the goroutine that completed the operation:
  implementations must be safe for concurrent use and return quickly.
- Nil means `NopMetrics`. Embed `NopMetrics` in your implementation so
  methods added in later minor releases do not break your build.

Use `Service.String()`, `Role.String()` and `ConnectionState.String()` as
labels; they are low-cardinality. Do not use `RemoteAddr`, `InvokeID` or
error messages as labels. For an error label, classify with `errors.Is`
(see [ERRORS.md](ERRORS.md)).

### Example: Prometheus adapter

Example only — go-mms does not depend on `prometheus/client_golang`; copy
this into your application.

```go
import (
    "errors"

    mms "github.com/otfabric/go-mms"
    "github.com/prometheus/client_golang/prometheus"
)

type promMetrics struct {
    mms.NopMetrics // forward compatibility
    requests    *prometheus.CounterVec
    duration    *prometheus.HistogramVec
    connections *prometheus.CounterVec
}

func newPromMetrics(reg prometheus.Registerer) *promMetrics {
    m := &promMetrics{
        requests: prometheus.NewCounterVec(prometheus.CounterOpts{
            Name: "mms_requests_total",
            Help: "Confirmed MMS requests by role, service and outcome.",
        }, []string{"role", "service", "result"}),
        duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
            Name:    "mms_request_duration_seconds",
            Help:    "Confirmed MMS request duration.",
            Buckets: prometheus.DefBuckets,
        }, []string{"role", "service"}),
        connections: prometheus.NewCounterVec(prometheus.CounterOpts{
            Name: "mms_connection_events_total",
            Help: "MMS association state changes.",
        }, []string{"role", "state"}),
    }
    reg.MustRegister(m.requests, m.duration, m.connections)
    return m
}

func (m *promMetrics) ObserveRequest(e mms.RequestEvent) {
    role, svc := e.Role.String(), e.Service.String()
    m.requests.WithLabelValues(role, svc, result(e.Err)).Inc()
    m.duration.WithLabelValues(role, svc).Observe(e.Duration.Seconds())
}

func (m *promMetrics) ObserveConnection(e mms.ConnectionEvent) {
    m.connections.WithLabelValues(e.Role.String(), e.State.String()).Inc()
}

func result(err error) string {
    switch {
    case err == nil:
        return "ok"
    case errors.Is(err, mms.ErrServiceRejected):
        return "service_error"
    case errors.Is(err, mms.ErrInvokeTimeout):
        return "timeout"
    case errors.Is(err, mms.ErrClosed):
        return "closed"
    default:
        return "error"
    }
}

// Wiring:
//   m := newPromMetrics(prometheus.DefaultRegisterer)
//   client, err := iso.Dial(ctx, addr, iso.WithClientDialOptions(mms.DialOptions{Metrics: m}))
//   srv := mms.NewServer(mms.ServerOptions{Metrics: m})
```

### OpenTelemetry

The same shape maps onto OpenTelemetry metrics: an `Int64Counter` for
requests and connection events and a `Float64Histogram` for
`RequestEvent.Duration`, with `role`/`service`/`state` as attributes. go-mms
does not create spans; `ObserveRequest` is called after the request
completes, so it can record a span retroactively (start time =
now − `Duration`) if per-request traces are needed.

## Logging

### Default Behavior

When no logger is configured, go-mms creates a logger backed by an
internal `discardHandler` — a `slog.Handler` whose `Enabled` method
always returns `false`. This means no log records are allocated, formatted,
or written. There is effectively zero overhead.

### Logging Levels

| Level | What is logged | When to use |
|-------|---------------|-------------|
| Disabled (default) | Nothing | Production with no debugging needs |
| `slog.LevelInfo` | Lifecycle events (connected, closed, aborted, concluded, listening) | Basic monitoring |
| `slog.LevelDebug` | All of the above, plus one `mms: request` / `mms: request failed` line per confirmed request (client and server) with service, invoke ID and duration, and per-service protocol summaries (client) | Development |
| `slog.LevelWarn` | Protocol anomalies, authentication rejections, broadcast failures (always emitted when a logger is configured) | Production with a filtered handler |

### Configuration

Every log message is prefixed `mms:` (client, server and the internal
server connection) or `iso:` (`transport/iso`). Loggers carry a `role`
attribute (`"client"` or `"server"`); per-association loggers also carry
`remote_addr` when the transport implements `RemoteAddrTransport`.

#### Client

Pass a `*slog.Logger` via `DialOptions.Logger`:

```go
logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
    Level: slog.LevelDebug,
}))

client, err := mms.NewClient(ctx, conn, mms.DialOptions{
    Logger: logger,
})
```

#### Server

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

#### ISO Transport Layer (independent of MMS logger)

MMS and ISO loggers are **separate**. Setting `DialOptions.Logger` or
`ServerOptions.Logger` does **not** enable ISO transport logging, and
`iso.WithLogger` does **not** enable MMS protocol logging.

| Option | Affects |
|--------|---------|
| `DialOptions.Logger` / `ServerOptions.Logger` | MMS association lifecycle, request summaries, server accept/serve |
| `iso.WithLogger` | ISO listener: TLS/COTP handshake failures and timeouts; `iso.Dial`/`iso.DialTCP`: attempts retried under `iso.WithRetry` |

Nil MMS logger → silent discard handler. Nil / omitted `iso.WithLogger` →
silent transport.

```go
ln, err := iso.Listen(":102",
    iso.WithLogger(logger),
)
```

### Log Messages

All client messages carry `role=client` (and `remote_addr` when known);
server messages carry `role=server`, and per-association ones
`remote_addr`.

#### Client — Info Level

| Message | Fields | When |
|---------|--------|------|
| `mms: connected` | `max_pdu_size`, `max_outstanding_calling`, `max_outstanding_called` | Association established |
| `mms: closed` | — | Graceful close (after ConcludeRequest/Response) |
| `mms: aborted` | — | Hard abort (no conclude handshake) |
| `mms: association ended by peer` | `error` | Peer released or aborted the association |

#### Client — Debug Level

| Message | Fields | When |
|---------|--------|------|
| `mms: request` | `service`, `invoke_id`, `duration` | Confirmed request succeeded |
| `mms: request failed` | `service`, `invoke_id`, `duration`, `error` | Confirmed request failed |
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
| `mms: delete all domain NVLs` | `invoke_id`, `service`, `domain`, `matched`, `deleted` | After `DeleteAllDomainNVLs` |
| `mms: delete all VMD NVLs` | `invoke_id`, `service`, `matched`, `deleted` | After `DeleteAllVMDNVLs` |
| `mms: reader loop stopped` | `error` | Reader goroutine exits (transport error) |
| `mms: reader loop decode error` | `error` | ISO stack decode failure in reader |
| `mms: reader loop PDU size error` | `error` | Received PDU exceeds the negotiated size |
| `mms: reader loop PDU error` | `error` | MMS PDU decode failure in reader |
| `mms: discarding late response during shutdown` | `invoke_id` | Response arrives after Close started |

#### Client — Warn Level

| Message | Fields | When |
|---------|--------|------|
| `mms: association failed` | `error` | `NewClient` association failed |
| `mms: server rejected conclude` | — | Server sent ConcludeError |
| `mms: reader loop unexpected PDU` | `kind` | Unhandled PDU type in reader |
| `mms: reader cannot extract invoke ID` | `error` | Malformed confirmed response |
| `mms: reader got response for unknown invoke ID` | `invoke_id` | Response with no pending request |
| `mms: reader cannot decode InformationReport` | `error` | Malformed InformationReport |
| `mms: reader cannot convert InformationReport` | `error` | InformationReport conversion failure |

#### Client — Error Level

| Message | Fields | When |
|---------|--------|------|
| `mms: InformationReport handler panic` | `panic` | User-registered handler panicked (recovered) |

#### Server — Info Level

| Message | Fields | When |
|---------|--------|------|
| `mms: server listening` | `addr` | ListenAndServe started |
| `mms: association accepted` | `max_pdu_size`, `max_outstanding_calling` | MMS Initiate accepted |
| `mms: association established` | — | Association registered; requests are served |
| `mms: association failed` | `error` | Association not established (including `AssociationTimeout`, authentication) |
| `mms: concluded` | — | Client sent ConcludeRequest, response sent |
| `mms: released` | — | Client sent session Release, response sent |
| `mms: association ended` | `error` (absent after orderly end) | Per-connection Serve returned |
| `mms: server shut down` | `aborted` | `Server.Shutdown` finished |

#### Server — Debug Level

| Message | Fields | When |
|---------|--------|------|
| `mms: request` | `service`, `invoke_id`, `duration` | Response sent |
| `mms: request failed` | `service`, `invoke_id`, `duration`, `error` | Handler error turned into a ConfirmedErrorPDU, or response not sent |

#### Server — Warn Level

| Message | Fields | When |
|---------|--------|------|
| `mms: PDU exceeds negotiated size` | `size`, `max` | Oversized PDU received (skipped) |
| `mms: malformed PDU` | `error` | PDU decode failure |
| `mms: unexpected PDU kind` | `kind` | Unhandled PDU type |
| `mms: malformed confirmed request` | `error` | Confirmed request decode failure (Reject sent) |
| `mms: response exceeds negotiated PDU size` | `invoke_id`, `size`, `max` | Response too large; Resource error sent instead |
| `mms: malformed cancel request` | `error` | CancelRequest decode failure |
| `mms: authenticator error` | `error` | Authenticator returned an error |
| `mms: authentication rejected` | — | Authenticator returned `Accept=false` |
| `mms: broadcast send failed` | `remote_addr`, `error` | Broadcast to a connection failed |
| `mms: temporary accept error, retrying` | `error` | Transient accept error in ListenAndServe |

#### ISO Transport — Warn Level

| Message | Fields | When |
|---------|--------|------|
| `iso: tls handshake failed, closing connection` | `remote_addr`, `error` | TLS handshake failed or timed out on accept |
| `iso: cotp handshake failed, closing connection` | `remote_addr`, `error` | COTP handshake failed or timed out on accept |
| `iso: dial attempt failed, retrying` | `addr`, `attempt`, `delay`, `error` | `Dial`/`DialTCP` attempt failed under `WithRetry` |

### Log Fields

| Field | Type | Description |
|-------|------|-------------|
| `role` | string | `client` or `server` (on every MMS log line) |
| `remote_addr` | string | Peer address (per-association loggers, ISO listener) |
| `duration` | duration | Confirmed request duration (`mms: request`, `mms: request failed`) |
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
| `domain` | string | Domain name (`mms: delete all domain NVLs`) |
| `addr` | string | Listener address, or dial target (`iso: dial attempt failed, retrying`) |
| `attempt` | int | 1-based number of the failed dial attempt |
| `delay` | duration | Wait before the next dial attempt |
| `aborted` | int | Associations aborted by `Server.Shutdown` |
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
The `direction` parameter is `mms.RawHookSend` (`"send"`) or
`mms.RawHookRecv` (`"recv"`).

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
2. Use `Metrics` rather than logs for request rates, latencies and error
   counts in production.
3. Use `slog.LevelInfo` for basic lifecycle monitoring (connect/close/abort).
4. Use `slog.LevelDebug` during development to see per-request invoke IDs,
   durations and service details.
5. Use `RawHook` for wire-level protocol analysis and packet capture replay.
6. **Avoid blocking in `RawHook` callbacks or `Metrics` methods** — they run
   synchronously in the send/receive path and will stall the connection.
7. Set `iso.WithLogger` separately from the MMS logger if you need
   visibility into TLS/COTP handshake failures on the server accept path
   or retried dial attempts (MMS logger does not inherit into ISO).
