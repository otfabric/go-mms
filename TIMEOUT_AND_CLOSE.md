# Timeout, Cancellation, and Shutdown Semantics

This document describes how go-mms handles context cancellation, timeouts, and connection shutdown.

## Client shutdown

### Close

`Close(ctx)` performs an orderly shutdown:

1. Marks client as closed (idempotent — second call returns nil)
2. Cancels all pending requests via `tracker.CancelAll(ErrClosed)`
3. Sends a ConcludeRequest and waits for ConcludeResponse (or `ctx` done). The send is bounded by `ctx`: when `ctx` is done the transport is closed, which fails a request write stuck behind a peer that stopped reading and releases the write lock
4. Releases the association (ISO 9506-2 Annex A.1.2.1): sends a Session FINISH carrying an ACSE RLRQ and waits for the DISCONNECT carrying the RLRE, bounded by `ctx` and `DialOptions.ReleaseTimeout` (zero → 2 s; negative skips the release, the behaviour before v1.8.0). A server that closes the transport, aborts, or does not answer in time still yields a successful `Close`; any other answer is aborted. The transport is closed when the release timeout expires, so a stalled peer cannot hold `Close`
5. Cancels the reader loop
6. Waits for the reader loop to exit
7. Closes the transport

If `ctx` ends during conclude or release, the shutdown still completes — the exchange is abandoned, the transport is closed, and `Close` returns an error matching the context error.

A Conclude-ErrorPDU makes `Close` abort the association (ISO 9506-2 §24.3: a failed Conclude leads to M-U-ABORT) and return an error carrying the `*ServiceError` (class `conclude`, e.g. `further-communication-required`).

### Abort

`Abort(ctx)` performs an immediate shutdown:

1. Marks client as closed
2. Sends a user abort (best-effort, bounded by `ctx`: when `ctx` is done the transport is closed, which fails a stuck send): Session ABORT carrying an ARU-PPDU with an ACSE ABRT, source acse-service-user (ISO 8823-1, ISO 8650-1)
3. Cancels all pending requests via `CancelAll(ErrClosed)`
4. Cancels the reader loop and waits for exit
5. Closes the transport

No ConcludeRequest/ConcludeResponse handshake occurs.

### Idempotency

Both `Close` and `Abort` are safe to call multiple times, including concurrently. The second call is a no-op.

### Detecting the end of an association

`Client.Done()` returns a channel closed when the association has ended: after `Close` or `Abort`, when the peer concludes, releases or aborts, or when the connection is lost or fails to decode. `Client.Err()` returns nil while the association is up and afterwards the reason it ended (always matching `ErrClosed`). Pending and later requests fail with an error matching `ErrClosed`. When the reader stops on its own (connection lost, undecodable or oversized PDU, peer release, or a Conclude-Response or Conclude-ErrorPDU that no `Close` asked for, which also matches `ErrProtocol`) it also closes the transport, so a write stalled on the dead connection fails and `Close` does not wait for its context. A request is not written once the reader has stopped, nor once `Close` or `Abort` has started: no request follows the Conclude-Request (ISO 9506-2 clause 8.4).

`Close` and `Abort` wait for the reader goroutine to stop; when their `ctx` is done they close the transport, which ends that wait. Called from the `InformationReport` handler, which runs on the reader, they do not wait for it: `Abort` returns at once, and `Close` when its `ctx` is done (it cannot read the Conclude-Response); the reader stops when the handler returns.

Reconnecting is left to the caller; `mms.Retry` with a `RetryPolicy` (or `iso.WithRetry` on `iso.Dial`) provides backoff with jitter. See the `Retry` doc comment for a reconnect loop.

## Context cancellation and timeouts

### During association (Dial / NewClient)

`ctx` bounds the whole association. With `iso.Dial`, it also bounds the TCP connect and the TLS and COTP handshakes. The ISO transport maps a deadline to the connection's read/write deadline and cancellation to an immediate deadline, so both a deadline and a cancel-only context unblock promptly.

With `iso.WithRetry`, one attempt covers TCP connect, TLS and COTP handshakes and the MMS association; attempts are retried with backoff until the policy gives up or `ctx` is done. Errors `mms.IsRetryable` classifies as permanent (rejected association, authentication failure, invalid argument, `context.Canceled`) are returned at once.

### Request timeout

`DialOptions.RequestTimeout`, when positive, bounds every confirmed request; an earlier deadline on the request context still wins. A request whose deadline expires fails with an error matching both `ErrInvokeTimeout` and `context.DeadlineExceeded`. A cancelled request context yields `context.Canceled`. Zero (default) means requests wait until their context is done or the association ends.

### Waiting for an outstanding-request slot

The client keeps at most the negotiated max-services-outstanding-calling confirmed requests outstanding (ISO 9506-2 clause 8.2). A request made at the limit waits for a slot, first come first served; the wait is part of the request and bounded by the same context (and `RequestTimeout`). A request whose context ends while it waits is never sent. `Close`, `Abort` and a lost association end the waiters with `ErrClosed`. With `MMSOptions.FailFastOutstanding` a request at the limit fails at once with `ErrMaxOutstanding` instead.

### During confirmed request send

Request writes are detached from the cancellation of the request context, not from its deadline. Abandoning a PDU half-written would corrupt the stream for every other request on the association, so:

- The write runs on its own goroutine, serialized with other writes; the caller returns as soon as `ctx` is done.
- A write that has not started when `ctx` ends is skipped, also when its deadline passes while it waits for an earlier write: nothing of it is on the wire, and the association stays usable.
- A write in progress continues in the background; its response, if any, is discarded as a late response.
- The transport's `Send` receives a context with the request's deadline (the earlier of the caller's deadline and `RequestTimeout`) and without its cancellation, so a write to a peer that stopped reading ends at that deadline.
- A write that **fails**, or that the deadline cuts, leaves the association unusable: the transport is closed, the reader stops, `Done` closes and all pending and queued requests fail with `ErrClosed`.

A request's context being cancelled mid-write therefore does not tear down the association; its deadline passing mid-write does. A request without any deadline (no deadline on `ctx` and `RequestTimeout` zero) has an unbounded write: against a peer that stopped reading it stays blocked until `Close`, `Abort` or the connection ends. Set `RequestTimeout` or a deadline to bound it.

`ServerConn.SendInformationReport` and the other unsolicited sends of the server follow the same rule: the deadline of `ctx` bounds the write, and a write that fails or is cut ends the association.

### While waiting for response

`sendConfirmed` selects on the response channel, `ctx.Done()` and the reader's exit. On `ctx` done the context error is returned (`ErrInvokeTimeout` + `context.DeadlineExceeded`, or `context.Canceled`). If the reader exits, the request fails with the reader's error (matching `ErrClosed`).

A request that was sent stays outstanding at the server until the server answers it (ISO 9506-2 clause 6.3.1), so its invoke ID and its outstanding-request slot are kept after the caller returns. A request whose write was skipped frees its slot at once.

### Late responses

A response to a request whose caller gave up completes the kept invoke ID: the response is discarded and the slot frees for the next request. A server that never answers such a request keeps its slot until the association ends. A response for an invoke ID that is not outstanding is discarded with a log entry (debug during shutdown, warn otherwise).

### Server-side sends

`ServerConn.SendInformationReport`, `SendUnsolicitedStatus` and `Server.Broadcast` use the same detached-write rule: `ctx` bounds how long the caller waits, an unstarted write is skipped, and a write in progress completes in the background.

## Full-duplex transport and deadlines

The client reads (reader goroutine) and writes (requests) concurrently on one connection. go-cotp (v1.4.0) arms read and write deadlines per direction, so one direction's deadline never aborts the other. In `transport/iso`:

- **Receive** passes its context to go-cotp. A context that ends before any octet of the next TSDU arrived returns the context error and leaves the connection usable; one that ends mid-TSDU aborts the connection (the error matches the context error, and later I/O fails with `ErrClosed` at the MMS layer).
- **Send** passes go-cotp a context that keeps the caller's deadline but not its cancellation: a write is bounded by the deadline (a stalled peer) but never abandoned half-written because a request was cancelled. A write cut by the deadline aborts the connection.

An I/O error caused by the context always matches `context.Canceled` or `context.DeadlineExceeded`, also when the connection deadline fires a moment before the context's own timer.

## Server shutdown

### Accepting connections (`iso.Listener`)

`iso.Listener` runs TLS and COTP handshakes in the background, concurrently, up to 64 at a time (further connections wait in the kernel backlog). Each handshake is bounded by `iso.WithHandshakeTimeout` (zero → 10 s, negative disables). A peer that connects and stays silent is closed after the timeout and never blocks `Accept` for other peers. Failed handshakes are logged (with `iso.WithLogger`) and do not make `Accept` return. Temporary accept errors (e.g. `EMFILE`) are retried with backoff from 5 ms up to 1 s. Cancelling the `Accept` context does not close the listener; `Close` does, and also closes connections still handshaking or waiting for `Accept`.

### Per-connection lifecycle (`Server.Serve`)

1. `ReceiveAssociation` — reads the CONNECT/CP/AARQ and the Initiate-Request and negotiates. A request that cannot be accepted is answered with a Session REFUSE (ISO 8327-1) carrying a presentation CPR (ISO 8823-1) and, when the ACSE was reached, an AARE rejected-permanent with its diagnostic and, for Initiate parameters, an Initiate-ErrorPDU (IEC 61850-8-1 Tables 3 and 13), before `Serve` returns
2. Authenticator (if configured); a refusal is sent the same way, with diagnostic authentication-required (14), authentication-mechanism-name-not-recognized (11) or authentication-failure (13) and an Initiate-ErrorPDU access/object-access-denied
3. `AcceptAssociation` — sends the AARE

   Steps 1–3 are bounded by `ServerOptions.AssociationTimeout` (zero → 30 s, negative disables); on expiry the transport is closed and `Serve` returns an error matching `ErrAssociationFailed` and `context.DeadlineExceeded`.
4. `Serve` loop — reads and handles requests (serially per connection) until error or ConcludeRequest
5. On ConcludeRequest, sends the Conclude-Response, then waits up to `ServerOptions.ReleaseTimeout` (zero → 2 s, negative does not wait) for the client's FINISH + RLRQ and answers DISCONNECT + RLRE (ISO 9506-2 Annex A.1.2.2). A client that closes the transport, aborts or stays silent ends the association normally (nil); any other PDU after the Conclude-Response is aborted. A FINISH before any Conclude is aborted and `Serve` returns an error (A-RELEASE is valid only after Conclude)
6. Deferred cleanup: `frsmTable.closeAll` (with a 10-second bounded context), then connection deregistration

`Serve` closes the transport when its `ctx` is cancelled, so a blocked read returns; `Serve` then returns an error matching `ctx.Err()`. It also closes it on association timeout and when `Shutdown` aborts the association. Otherwise the caller owns the transport.

### ListenAndServe

`ListenAndServe` owns the listener and closes it on return. Each accepted transport is served in its own goroutine with the `ListenAndServe` context and closed when its `Serve` returns. `ListenAndServe` returns only after every goroutine it started has returned.

When the context is cancelled:
1. The accept loop exits
2. Every `Serve` goroutine sees the cancelled context and closes its transport, ending the association without a protocol-level abort
3. `ListenAndServe` returns `ctx.Err()` once those goroutines have returned

After `Shutdown` it returns `ErrServerClosed` once `Shutdown` has ended its connections. On a fatal accept error it ends its connections as a cancelled context does and returns the error.

`iso.Listener.Close` closes the TCP listener and every connection still handshaking or waiting for `Accept`, and returns when the accept loop and the handshake goroutines have stopped.

### Graceful shutdown (`Server.Shutdown`)

`Shutdown(ctx)`:

1. Marks the server shut down and closes the listeners of every running `ListenAndServe`, which return `ErrServerClosed`
2. Ends every connection that is not associated yet (its transport is closed; its `Serve` returns an error matching `ErrServerClosed`), whatever `AssociationTimeout` is
3. For each association, waits for the request it is handling (if any) to finish, then aborts it (ACSE ABRT and transport close, 1 s bound). MMS has no server-initiated orderly release, so idle associations are aborted right away
4. Returns when every `Serve` has returned
5. If `ctx` ends first, remaining associations are aborted immediately and `Shutdown` returns `ctx.Err()` without waiting further

After `Shutdown`, `Serve` and `ListenAndServe` return `ErrServerClosed`. `Shutdown` is safe to call more than once.

### File handle cleanup

`frsmTable.closeAll` uses a fresh 10-second timeout context (independent of the request context) to ensure file handles are properly closed even during shutdown.

## Tested scenarios

| Scenario | Test | Result |
|----------|------|--------|
| Close during in-flight request | `TestCloseDuringInFlightRequest` | The request held by the server fails with `ErrClosed`; `Close` returns nil once the server answers; `Serve` returns nil |
| Context cancellation during request | `TestContextCancellationDuringRequest` | Caller gets `context.Canceled`; the association stays up and serves the next request |
| Abort during in-flight request | `TestAbortDuringInFlightRequest` | The request fails with `ErrClosed`; `Abort` returns nil; `Serve` returns |
| Double Close (concurrent) | `TestDoubleCloseConcurrent` | Both return nil; `Done` closed; no goroutine left (goleak) |
| Close with timeout | `TestCloseWithTimeout` | Returns `context.DeadlineExceeded` at the deadline; `Done` closed; a second `Close` returns nil |
| Association-scoped objects | `TestAssociationScopeIsolation` | A list defined by one association is not listed or readable by another |
| Close already closed | `TestCloseAlreadyClosed` | Returns nil |
| Close with a request queued behind a write | `TestCloseWritesNoQueuedRequest`, `TestCloseWhileConcurrentRequests` | The queued request is not written; every request fails with `ErrClosed`; the release completes |
| Close while a request write is stalled | `TestCloseBoundedByContextWhileWriteStalled` | Returns `context.DeadlineExceeded` when `ctx` ends; transport closed |
| Reader stops on an undecodable PDU while a write is stalled | `TestReaderExitClosesTransport` | Transport closed at once; `Err` matches `ErrDecodeFailed`; `Close` returns promptly |
| Abort already closed | `TestClientAbort` | Returns nil |
| Conclude PDU without a Conclude-Request | `TestConcludeWithoutRequestEndsAssociation` | `Done` closed, `Err` matches `ErrClosed` and `ErrProtocol`, transport closed, a later request is not written |
| Report handler calls `Abort` or `Close` | `TestReportHandlerEndsAssociation` | `Abort` returns at once, `Close` at its context's deadline; the reader stops afterwards |
| Peer goes away | `TestClient_Done_Err_Reconnect`, `TestClient_PeerDisconnect` (interop) | `Done` closed, `Err` matches `ErrClosed`, reconnect succeeds |
| Request timeout | `TestClient_RequestTimeout`, `TestClient_RequestTimeout_Loopback` | `ErrInvokeTimeout` + `context.DeadlineExceeded`; association stays up |
| Write failure | `TestClient_SendFailureClosesAssociation` | Association closed, pending requests get `ErrClosed` |
| Write deadline | `TestRequestWriteCarriesDeadline`, `TestSendUnconfirmedWriteCarriesDeadline` (`internal/serverconn`) | `Send` receives the caller's deadline or the one `RequestTimeout` sets |
| Expired before the write | `TestExpiredRequestIsNotWritten`, `TestSendUnconfirmedExpiredIsNotWritten` (`internal/serverconn`) | Write not started; association stays up |
| Write stalled past its deadline | `TestStalledWriteEndsAssociation` | `Done` closes, `Err` matches `ErrClosed`, queued requests fail, no goroutine left (goleak) |
| Context ends before write starts | `TestClient_SendAbandonedBeforeWrite` | Write skipped |
| Dial with retry | `TestDial_WithRetry`, `TestDial_RetriesDialFailure` | Retries until the peer is up; permanent errors not retried |
| Silent peer during association | `TestServer_AssociationTimeout`, `TestServer_AssociationTimeout_Loopback` | Disconnected after `AssociationTimeout` |
| Silent peer during COTP handshake | `TestListener_SlowHandshake`, `TestListener_HandshakeTimeoutLogged` | Other peers accepted; slow peer closed after the handshake timeout |
| Graceful shutdown | `TestServer_Shutdown`, `TestServer_Shutdown_InFlight`, `TestServer_Shutdown_StopsListenAndServe`, `TestServer_Shutdown` (interop) | In-flight request completes; `ListenAndServe` returns `ErrServerClosed`; no association left |
| Shutdown while a connection is associating | `TestShutdownWaitsForAssociatingServe` | `Shutdown` returns after that `Serve` returned `ErrServerClosed`, for every `AssociationTimeout` |
| Shutdown twice | `TestShutdownTwice` | Both return nil; `Serve` and `ListenAndServe` afterwards return `ErrServerClosed` |
| `ListenAndServe` return | `TestListenAndServeJoinsConnections` | Returns after its connection goroutines, on cancel, `Shutdown` and a fatal accept error |
| Pending handshakes and `Listener.Close` | `TestListenerPendingHandshakeBound` (`transport/iso`) | At most 64 handshakes; `Close` returns after the handshake goroutines stopped |
| Temporary accept errors | `TestListener_TemporaryAcceptErrors` | Retried with backoff |
| Orderly release | `TestSpec_9506_A12_OrderlyRelease` | CONNECT, DATA, FINISH sent; ACCEPT, DATA, DISCONNECT received; `Serve` returns nil |
| Release peer closes, silent, aborts, answers junk | `TestSpec_9506_A12_ReleaseTolerance` | `Close` returns nil within `ReleaseTimeout`; junk is aborted; `ctx` ending the release is an error |
| Conclude-Error | `TestSpec_9506_24_3_ConcludeErrorAborts` | Abort sent; `Close` returns the `*ServiceError` |
| Server release wait | `TestSpec_9506_A12_ServerRelease`, `TestSpec_9506_A12_ReleaseWaitBounded` (`internal/serverconn`) | DN + RLRE after FINISH; bounded by `ReleaseTimeout` and `ctx`; non-RLRQ aborted |
| Release/abort races | `TestChaos_ReleaseAbortRaces`, `TestChaos_ReleaseRaces` (`internal/serverconn`) | No deadlock, no goroutine leak (goleak) |

## Known limitations

- **Write cancellation**: a request write in progress is not interrupted by cancellation, only by the request's deadline; the caller returns, and the write continues in the background until it completes or the deadline passes. A request without a deadline has an unbounded write. Custom `Transport` implementations decide how they honour the deadline of the context passed to `Send`.
- **Cancel PDU**: the client never sends an MMS Cancel; abandoning a request only stops waiting for it.
