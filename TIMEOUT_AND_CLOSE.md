# Timeout, Cancellation, and Shutdown Semantics

This document describes how go-mms handles context cancellation, timeouts, and connection shutdown.

## Client shutdown

### Close

`Close(ctx)` performs an orderly shutdown:

1. Marks client as closed (idempotent — second call returns nil)
2. Cancels all pending requests via `tracker.CancelAll(ErrClosed)`
3. Sends a ConcludeRequest and waits for ConcludeResponse (or `ctx` done)
4. Cancels the reader loop
5. Waits for the reader loop to exit
6. Closes the transport

If `ctx` ends during conclude, the shutdown still completes — the conclude is abandoned, the transport is closed, and `Close` returns the conclude error.

### Abort

`Abort(ctx)` performs an immediate shutdown:

1. Marks client as closed
2. Sends an ABRT PDU (best-effort, bounded by `ctx`: when `ctx` is done the transport is closed, which fails a stuck send)
3. Cancels all pending requests via `CancelAll(ErrClosed)`
4. Cancels the reader loop and waits for exit
5. Closes the transport

No ConcludeRequest/ConcludeResponse handshake occurs.

### Idempotency

Both `Close` and `Abort` are safe to call multiple times, including concurrently. The second call is a no-op.

### Detecting the end of an association

`Client.Done()` returns a channel closed when the association has ended: after `Close` or `Abort`, when the peer concludes, releases or aborts, or when the connection is lost or fails to decode. `Client.Err()` returns nil while the association is up and afterwards the reason it ended (always matching `ErrClosed`). Pending and later requests fail with an error matching `ErrClosed`.

Reconnecting is left to the caller; `mms.Retry` with a `RetryPolicy` (or `iso.WithRetry` on `iso.Dial`) provides backoff with jitter. See the `Retry` doc comment for a reconnect loop.

## Context cancellation and timeouts

### During association (Dial / NewClient)

`ctx` bounds the whole association. With `iso.Dial`, it also bounds the TCP connect and the TLS and COTP handshakes. The ISO transport maps a deadline to the connection's read/write deadline and cancellation to an immediate deadline, so both a deadline and a cancel-only context unblock promptly.

With `iso.WithRetry`, one attempt covers TCP connect, TLS and COTP handshakes and the MMS association; attempts are retried with backoff until the policy gives up or `ctx` is done. Errors `mms.IsRetryable` classifies as permanent (rejected association, authentication failure, invalid argument, `context.Canceled`) are returned at once.

### Request timeout

`DialOptions.RequestTimeout`, when positive, bounds every confirmed request; an earlier deadline on the request context still wins. A request whose deadline expires fails with an error matching both `ErrInvokeTimeout` and `context.DeadlineExceeded`. A cancelled request context yields `context.Canceled`. Zero (default) means requests wait until their context is done or the association ends.

### During confirmed request send

Request writes are detached from the request context. Abandoning a PDU half-written would corrupt the stream for every other request on the association, so:

- The write runs on its own goroutine, serialized with other writes; the caller returns as soon as `ctx` is done.
- A write that has not started when `ctx` ends is skipped.
- A write in progress completes in the background; its response, if any, is discarded as a late response.
- A write that **fails** leaves the association unusable: the transport is closed, the reader stops and all pending requests fail with `ErrClosed`.

A request's context ending mid-write therefore no longer tears down the association. In the ISO transport, a write is still bounded by its context's deadline (stalled peer) but not interrupted by cancellation.

### While waiting for response

`sendConfirmed` selects on the response channel, `ctx.Done()` and the reader's exit. On `ctx` done the invoke ID is removed from the tracker and the context error is returned (`ErrInvokeTimeout` + `context.DeadlineExceeded`, or `context.Canceled`). If the reader exits, the request fails with the reader's error (matching `ErrClosed`).

### Late responses

If a response arrives after the request was cancelled, `tracker.Complete` returns false (the ID was already removed by `Cancel`). The late response is discarded with a log entry (debug during shutdown, warn otherwise).

### Server-side sends

`ServerConn.SendInformationReport`, `SendUnsolicitedStatus` and `Server.Broadcast` use the same detached-write rule: `ctx` bounds how long the caller waits, an unstarted write is skipped, and a write in progress completes in the background.

## Full-duplex transport and deadlines

The client reads (reader goroutine) and writes (requests) concurrently on one connection. go-cotp (v1.1.0+) arms read and write deadlines per direction, so one direction's deadline never aborts the other. In `transport/iso`:

- **Receive** passes its context to go-cotp. A context that ends before any octet of the next TSDU arrived returns the context error and leaves the connection usable; one that ends mid-TSDU aborts the connection (the error matches the context error, and later I/O fails with `ErrClosed` at the MMS layer).
- **Send** passes go-cotp a context that keeps the caller's deadline but not its cancellation: a write is bounded by the deadline (a stalled peer) but never abandoned half-written because a request was cancelled. A write cut by the deadline aborts the connection.

An I/O error caused by the context always matches `context.Canceled` or `context.DeadlineExceeded`, also when the connection deadline fires a moment before the context's own timer.

## Server shutdown

### Accepting connections (`iso.Listener`)

`iso.Listener` runs TLS and COTP handshakes in the background, concurrently, up to 64 at a time (further connections wait in the kernel backlog). Each handshake is bounded by `iso.WithHandshakeTimeout` (zero → 10 s, negative disables). A peer that connects and stays silent is closed after the timeout and never blocks `Accept` for other peers. Failed handshakes are logged (with `iso.WithLogger`) and do not make `Accept` return. Temporary accept errors (e.g. `EMFILE`) are retried with backoff from 5 ms up to 1 s. Cancelling the `Accept` context does not close the listener; `Close` does, and also closes connections still handshaking or waiting for `Accept`.

### Per-connection lifecycle (`Server.Serve`)

1. `ReceiveAssociation` — reads the AARQ
2. Authenticator (if configured)
3. `AcceptAssociation` — sends the AARE

   Steps 1–3 are bounded by `ServerOptions.AssociationTimeout` (zero → 30 s, negative disables); on expiry the transport is closed and `Serve` returns an error matching `ErrAssociationFailed` and `context.DeadlineExceeded`.
4. `Serve` loop — reads and handles requests (serially per connection) until error, ConcludeRequest or session Release
5. On ConcludeRequest/Release, sends the response and returns nil
6. Deferred cleanup: `frsmTable.closeAll` (with a 10-second bounded context), then connection deregistration

`Serve` closes the transport when its `ctx` is cancelled, so a blocked read returns; `Serve` then returns an error matching `ctx.Err()`. It also closes it on association timeout and when `Shutdown` aborts the association. Otherwise the caller owns the transport.

### ListenAndServe

`ListenAndServe` owns the listener and closes it on return. Each accepted transport is served in its own goroutine with the `ListenAndServe` context and closed when its `Serve` returns.

When the context is cancelled:
1. The accept loop exits and `ListenAndServe` returns `ctx.Err()`
2. Every `Serve` goroutine sees the cancelled context and closes its transport, ending the association without a protocol-level abort

### Graceful shutdown (`Server.Shutdown`)

`Shutdown(ctx)`:

1. Marks the server shut down and closes the listeners of every running `ListenAndServe`, which return `ErrServerClosed`
2. For each association, waits for the request it is handling (if any) to finish, then aborts it (ACSE ABRT and transport close, 1 s bound). MMS has no server-initiated orderly release, so idle associations are aborted right away
3. If `ctx` ends first, remaining associations are aborted immediately and `Shutdown` returns `ctx.Err()`

After `Shutdown`, `Serve` and `ListenAndServe` return `ErrServerClosed`. `Shutdown` is safe to call more than once.

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
| Peer goes away | `TestClient_Done_Err_Reconnect`, `TestClient_PeerDisconnect` (interop) | `Done` closed, `Err` matches `ErrClosed`, reconnect succeeds |
| Request timeout | `TestClient_RequestTimeout`, `TestClient_RequestTimeout_Loopback` | `ErrInvokeTimeout` + `context.DeadlineExceeded`; association stays up |
| Write failure | `TestClient_SendFailureClosesAssociation` | Association closed, pending requests get `ErrClosed` |
| Context ends before write starts | `TestClient_SendAbandonedBeforeWrite` | Write skipped |
| Dial with retry | `TestDial_WithRetry`, `TestDial_RetriesDialFailure` | Retries until the peer is up; permanent errors not retried |
| Silent peer during association | `TestServer_AssociationTimeout`, `TestServer_AssociationTimeout_Loopback` | Disconnected after `AssociationTimeout` |
| Silent peer during COTP handshake | `TestListener_SlowHandshake`, `TestListener_HandshakeTimeoutLogged` | Other peers accepted; slow peer closed after the handshake timeout |
| Graceful shutdown | `TestServer_Shutdown`, `TestServer_Shutdown_InFlight`, `TestServer_Shutdown_StopsListenAndServe`, `TestServer_Shutdown` (interop) | In-flight request completes; `ListenAndServe` returns `ErrServerClosed`; no association left |
| Temporary accept errors | `TestListener_TemporaryAcceptErrors` | Retried with backoff |

## Known limitations

- **Write cancellation**: a request write in progress is not interrupted by cancellation (only by its context deadline in `transport/iso`); the caller returns, but the write completes in the background. Custom `Transport` implementations decide how they honour the context passed to `Send`.
- **Cancel PDU**: the client never sends an MMS Cancel; abandoning a request only stops waiting for it.
