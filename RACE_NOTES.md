# Race and Concurrency Notes

## Race detector status

`make check` runs `go test -race ./...`; CI runs the race detector on Go
1.23–1.26 and a short chaos run (`make chaos CHAOS_DURATION=5s`) under
`-race`. `make chaos` runs the chaos and stress tests for longer with a
random seed; a failure prints `MMS_CHAOS_SEED` to replay it.

## Concurrency model

### Client

- **sendMu**: serializes outbound writes so PDU bytes never interleave.
  Waiting for the response happens outside this lock, so concurrent
  callers can have several confirmed requests outstanding.
- **Detached writes**: a request's write runs on its own goroutine under
  `sendMu`. If the request's context ends first, the caller returns at
  once; a write that has not started is skipped (atomic
  pending/writing/abandoned state), and one that has started completes,
  so a cancelled request never leaves a partial PDU on the wire or tears
  down the association. A failed write closes the association.
- **mu**: protects `closed` and the reader handles.
- **invoke tracker**: thread-safe map of pending request channels
  (`Allocate`, `Complete`, `Cancel`, `CancelAll`). No `maxPending` cap;
  negotiated MaxOutstanding* is not enforced.
- **readerLoop**: one goroutine dispatches incoming PDUs; confirmed
  responses are routed by invoke ID, unconfirmed PDUs go to the report
  handler. `Client.Done` is closed when it exits; `Client.Err` reports
  why.

### Server

- **Per-connection goroutine**: each association runs in its own `Serve`
  call with its own `serverconn.Conn`; `Serve` closes the transport when
  its context ends.
- **busy**: per-connection one-slot semaphore held while a request is
  dispatched. `Server.Shutdown` closes listeners, waits for it per
  connection (bounded by its context), then aborts the association.
- **Unsolicited sends** (`Broadcast`, `SendInformationReport`) use the
  same detached-write pattern as the client.
- **frsmTable**: per-connection file handle table with its own mutex;
  `closeAll` runs on cleanup with a bounded 10-second context.
- **Connection and listener registry**: maps guarded by `connsMu`
  (`sync.RWMutex`).

### transport/iso

- **Listener**: a background accept loop hands connections to
  handshake goroutines (at most 64 pending), each bounded by the
  handshake timeout; finished transports are passed to `Accept` over a
  channel. Close is idempotent and closes connections still
  handshaking or waiting.
- **Per-direction deadlines**: go-cotp v1.0.4 arms and clears a
  both-direction `SetDeadline` around every read and write, which lets
  a write deadline cut a concurrent read. After the handshake the
  transport ignores those calls (`splitDeadlineConn`) and sets read and
  write deadlines separately.

### Invoke tracker

- Buffered channels (capacity 1) prevent sends from blocking.
- `CancelAll` swaps the pending map, then sends errors to all channels;
  concurrent `Complete` calls on the old map find it empty and return
  false.
- `AllocateWithID` rejects collisions immediately.

## Stress and chaos tests

| Test | What it exercises |
|------|-------------------|
| `TestTrackerConcurrentStress` (internal/invoke) | Allocate + Complete + Cancel + CancelAll racing |
| `TestAllocateWithIDConcurrent` (internal/invoke) | ID collisions under contention |
| `TestConcurrentReads`, `TestContextCancellationDuring*`, `TestDoubleClose` | client concurrency and cancellation |
| `TestStress_Concurrent` (e2e) | 4 clients × 4 goroutines sharing one server while it broadcasts reports |
| `TestChaos_LossyButIntact` (e2e) | delays, partial reads, fragmented writes on both sides |
| `TestChaos_DropsAndReconnect` (e2e) | connections dropped mid-PDU; clients reconnect with `Retry`; server releases every association |
| `TestChaos_SlowPeer` (e2e) | bandwidth-limited peer: requests time out, Abort stays bounded |
| `TestServer_Shutdown_InFlight` (e2e) | Shutdown waits for a running handler |

## Known non-issues

- **Late response after cancel**: the tracker discards late responses
  (invoke ID already removed); logged at debug level.
- **Report handler during shutdown**: reports can be delivered between
  Close and the reader loop exiting. Handlers should not block.
