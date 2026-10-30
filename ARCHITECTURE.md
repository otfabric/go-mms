# Architecture

go-mms is a pure Go MMS client and server. The only direct dependency is
[go-cotp](https://github.com/otfabric/go-cotp), used by `transport/iso`;
go-tpkt comes in through it.

Details live elsewhere: public API in [API.md](API.md), locks and goroutines
in [RACE_NOTES.md](RACE_NOTES.md), shutdown order in
[TIMEOUT_AND_CLOSE.md](TIMEOUT_AND_CLOSE.md), memory ownership of values in
[OWNERSHIP.md](OWNERSHIP.md), bounds in [LIMITS.md](LIMITS.md).

## Layers

The diagram follows the import graph (`go list -f '{{.Imports}}' ./...`).

```text
application
    |
transport/iso            Dial, DialTCP, Listen, NewListener; TLS, retry
    |        \
    |         go-cotp -> go-tpkt
    v
mms (root package)       Client, Server, ServerConn, Value, Transport interfaces
    |
    +-- internal/isostack     session -> presentation -> ACSE, as one codec
    |       +-- internal/session        ISO 8327-1 SPDUs
    |       +-- internal/presentation   ISO 8823-1 PDUs
    |       +-- internal/acse           association control
    +-- internal/pdu          MMS PDU construction and parsing
    |       +-- internal/codec          MMS marshal/unmarshal wrappers
    |       |       +-- internal/asn1util, internal/berutil
    +-- internal/invoke       invoke ID allocation, request/response tracking
    +-- internal/serverconn   state of one server association
    +-- internal/servermodel  domain, variable and list registry
```

`transport/iso` imports the root package, not the other way round: the root
package sees a connection only through the `Transport` interface (`Send`,
`Receive`, `Close`, payload above COTP). `TLSTransport` and
`RemoteAddrTransport` are optional extensions found by type assertion.
`TransportListener` is the listening counterpart, implemented by
`iso.Listener`. Tests can therefore replace TCP; `internal/chaos` wraps a
connection to inject faults for the e2e chaos tests.

`internal/apisurface` and `internal/chaos` are test support, imported only by
`e2e` and `interop`. `internal/transport` declares an interface that nothing
imports.

## Goroutines and ownership

| Goroutine | Started by | Ends when |
|---|---|---|
| `Client.readerLoop` (one per client) | client setup, after association | its context is cancelled or the connection fails; `Client.Done` is then closed |
| detached request write | `Client.send`, per request | the write ends, or at once if it is abandoned before it starts |
| detached response write | `serverconn.Conn.sendData`, per send | same as the client write |
| per-association `Serve` | `Server.ListenAndServe` | its context ends or the association ends; it closes the transport |
| abort of one connection | `Server.Shutdown` | the abort finishes (bounded to one second); `Shutdown` waits for all of them |
| accept loop (one per listener) | first `iso.Listener.Accept` | the listener closes |
| handshake, plus a watcher | accept loop, per TCP connection | the handshake ends or times out |

`Serve` blocks, so a caller without `ListenAndServe` runs it on its own
goroutine. Who closes the transport, and in what order, is in
[TIMEOUT_AND_CLOSE.md](TIMEOUT_AND_CLOSE.md).

## Platforms

There is no platform-specific code and no cgo: the only build tag in the
repository is `interop`, on the interop tests. `make cross` builds the module
without cgo for the platforms in `CROSS_PLATFORMS`.
