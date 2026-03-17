# Timeout, Cancellation, and Shutdown Semantics

This document describes how go-mms handles context cancellation, timeouts, and connection shutdown.

## Client shutdown

### Close

`Close(ctx)` performs an orderly shutdown:

1. Marks client as closed (idempotent — second call returns nil)
2. Cancels all pending requests via `tracker.CancelAll(ErrClosed)`
3. Sends a ConcludeRequest and waits for ConcludeResponse (or `ctx` timeout)
4. Cancels the reader loop
5. Waits for the reader loop to exit
6. Closes the transport

If `ctx` times out during conclude, the shutdown still completes — the conclude is skipped and the transport is closed.

### Abort

`Abort(ctx)` performs an immediate shutdown:

1. Marks client as closed
2. Sends an ABRT PDU (best-effort)
3. Cancels all pending requests via `CancelAll(ErrClosed)`
4. Cancels the reader loop and waits for exit
5. Closes the transport

No ConcludeRequest/ConcludeResponse handshake occurs.

### Idempotency

Both `Close` and `Abort` are safe to call multiple times, including concurrently. The second call is a no-op.

## Context cancellation during requests

### During association (Dial)

Context propagates to `sendRaw` and `receiveRaw`. With a deadline, transport-level `SetDeadline` ensures prompt cancellation. Without a deadline (cancel-only), the transport blocks on the TCP read until data arrives or the connection is closed.

### During confirmed request send

Context propagates to `conn.Send`. Transport-level errors (timeout, cancellation) are returned to the caller.

### While waiting for response

`sendConfirmed` uses `select` on the response channel and `ctx.Done()`. On cancellation, the invoke ID is removed from the tracker and `ctx.Err()` is returned.

### Late responses

If a response arrives after the request was cancelled, `tracker.Complete` returns false (the ID was already removed by `Cancel`). The late response is silently discarded with a debug log entry.

## Server shutdown

### Per-connection lifecycle

1. `ReceiveAssociation` — blocks on transport read
2. `AcceptAssociation` — sends response
3. `Serve` loop — reads and handles requests until error or ConcludeRequest
4. On ConcludeRequest, sends ConcludeResponse and returns
5. Deferred cleanup: `frsmTable.closeAll` (with a 10-second bounded context), then connection deregistration

### ListenAndServe shutdown

When the context is cancelled:
1. Accept loop exits
2. Existing connections continue until their `Serve` returns
3. Connections end when their transport fails (e.g. client disconnects or context propagation)

### File handle cleanup

`frsmTable.closeAll` uses a fresh 10-second timeout context (independent of the request context) to ensure file handles are properly closed even during shutdown.

## Tested scenarios

| Scenario | Test | Result |
|----------|------|--------|
| Close during in-flight request | `TestCloseDuringInFlightRequest` | Caller gets `ErrClosed` or completes |
| Context cancellation during request | `TestContextCancellationDuringRequest` | Caller gets `context.Canceled` |
| Abort during in-flight request | `TestAbortDuringInFlightRequest` | Caller gets error or completes |
| Double Close (concurrent) | `TestDoubleCloseConcurrent` | No deadlock, no panic |
| Close with timeout | `TestCloseWithTimeout` | Conclude timeout handled gracefully |
| Close already closed | `TestCloseAlreadyClosed` | Returns nil |
| Abort already closed | `TestClientAbort` | Returns nil |

## Known limitations

- **Cancel-only contexts without deadlines**: The ISO transport checks `ctx.Err()` only after `ReadFrame` returns. Cancellation without a deadline may not unblock until data arrives or the connection is closed. Use `context.WithTimeout` or `context.WithDeadline` for prompt cancellation.
- **Server connection shutdown**: Existing connections are not forcibly closed when the server context is cancelled. Connections end naturally when their next transport read fails.
