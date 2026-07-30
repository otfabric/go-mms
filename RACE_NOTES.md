# Race and Concurrency Notes

## Race detector status

All tests pass clean under `go test -race -count=5 ./...`. No data races detected.

## Concurrency model

### Client

- **sendMu**: Serializes outbound writes (`sendRaw`) so PDU bytes do not interleave. Wait-for-response is outside this lock, so concurrent callers may have multiple confirmed requests outstanding.
- **mu**: Protects `closed` state and `readerCancel`/`readerDone`.
- **invoke tracker**: Thread-safe map of pending request channels. `Allocate`, `Complete`, `Cancel`, and `CancelAll` are internally synchronized. Created with no `maxPending` cap; negotiated MaxOutstanding* is not enforced here.
- **readerLoop**: Single goroutine dispatches incoming PDUs. Confirmed responses are routed by invoke ID. Unconfirmed reports are dispatched to the report handler.

### Server

- **Per-connection goroutine**: Each accepted connection runs in its own goroutine with its own `serverconn.Conn`.
- **frsmTable**: Per-connection file handle table. Protected by its own mutex. `closeAll` runs during deferred cleanup with a bounded 10-second context.
- **Connection registry**: `sync.Map` for thread-safe add/remove/iterate.

### Invoke tracker

- Buffered channels (capacity 1) prevent sends from blocking.
- `CancelAll` atomically swaps the pending map, then sends errors to all channels. Concurrent `Complete` calls on the old map are safe — they find the map empty and return false.
- `AllocateWithID` rejects collisions immediately.

## Stress tests

| Test | What it exercises | Goroutines |
|------|-------------------|:--:|
| `TestTrackerConcurrentStress` | Allocate + Complete + Cancel + CancelAll racing | 50+ |
| `TestAllocateWithIDConcurrent` | ID collisions under contention | 50 |
| `TestConcurrentClientRequests` | 20 concurrent Identify requests | 20 |
| `TestConcurrentReadWrite` | Mixed GetNameList + Status requests | 20 |
| `TestCloseWhileConcurrentRequests` | Close during 10 concurrent requests | 11 |
| `TestDoubleCloseConcurrent` | Two concurrent Close calls | 2 |
| `TestAbortDuringInFlightRequest` | Abort while request in flight | 2 |

## Known non-issues

- **Late response after cancel**: Tracker silently discards late responses (invoke ID already removed). Logged at debug level.
- **Report handler during shutdown**: Reports can be delivered during the shutdown window between Close being called and the reader loop exiting. Handlers should be non-blocking.
- **No flaky tests**: All tests pass deterministically under repeated runs with the race detector.
