# go-mms — Current Status

Snapshot of what is implemented and how it is verified, as of v1.2.0.
Release history is in [RELEASE.md](RELEASE.md); the per-service matrix with
test coverage is in [COMPLIANCE.md](COMPLIANCE.md).

## Implemented

### Protocol stack

| Layer | Package | Notes |
|-------|---------|-------|
| TCP + optional TLS, TPKT/COTP class 0 | `transport/iso` (via go-cotp) | `Dial`, `DialTCP`, `Listen`; retry (`WithRetry`), custom dialer, concurrent accept-side handshakes with timeout |
| Session (ISO 8327-1) | `internal/session` | CONNECT/ACCEPT/DATA/FINISH/RELEASE |
| Presentation (ISO 8823-1) | `internal/presentation` | Context negotiation |
| ACSE (ISO 8650-1) | `internal/acse` | AARQ/AARE/ABRT, password authentication, AP-title/AE-qualifier |
| MMS (ISO 9506-1/2) | `internal/pdu`, `internal/codec`, root package | Hand-written BER |

### MMS services (client and server unless noted)

| Group | Services |
|-------|----------|
| Association | Initiate, Conclude, Abort; Cancel (server answers CancelError) |
| Identity & status | Identify, Status, UnsolicitedStatus (server send only) |
| Variable access | Read, Write (single, multi-variable, alternate access, NVL by name) |
| Named variable lists | DefineNamedVariableList, GetNamedVariableListAttributes, DeleteNamedVariableList (server: all four `scopeOfDelete`; client: 0, 2, 3) |
| Discovery | GetNameList (all scopes, paginated), GetVariableAccessAttributes |
| Reports | InformationReport (send, broadcast, receive) |
| Files | FileOpen, FileRead, FileClose, FileDirectory, FileDelete, FileRename, ObtainFile (provider-delegated), DownloadFile helper |
| Journals | ReadJournal (time range, start after) |
| Errors | ConfirmedError, Reject |

All MMS data types, including GeneralizedTime, BCD, ObjectIdentifier,
Real, BooleanArray and UTCTime with TimeQuality.

Not implemented (see [KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md)):
Semaphore, Event and Program Invocation services, ScatteredAccess,
client-sent Cancel, client UnsolicitedStatus callback, segmented
(role-reversal) ObtainFile.

### Operability (v1.2.0)

| Area | API | Docs |
|------|-----|------|
| Error classification | Sentinels incl. `ErrServerClosed`, `ErrInvalidArgument`; `*RejectError`, `ServiceError.CodeName`, `Phase*` constants | [ERRORS.md](ERRORS.md) |
| Metrics | `Metrics`, `NopMetrics`, `RequestEvent`, `ConnectionEvent`; `DialOptions.Metrics`, `ServerOptions.Metrics` | [OBSERVABILITY.md](OBSERVABILITY.md) |
| Structured logging | `log/slog`; `role`, `remote_addr`, per-request `mms: request` debug lines | [OBSERVABILITY.md](OBSERVABILITY.md) |
| Timeouts | `DialOptions.RequestTimeout`, `ServerOptions.AssociationTimeout`, `iso.WithHandshakeTimeout` | [TIMEOUT_AND_CLOSE.md](TIMEOUT_AND_CLOSE.md), [LIMITS.md](LIMITS.md) |
| Reconnect / retry | `Client.Done`, `Client.Err`, `RetryPolicy`, `Retry`, `IsRetryable`, `iso.WithRetry` | [TIMEOUT_AND_CLOSE.md](TIMEOUT_AND_CLOSE.md) |
| Graceful shutdown | `Server.Shutdown` | [TIMEOUT_AND_CLOSE.md](TIMEOUT_AND_CLOSE.md) |

## Verification

| Suite | Command | Scope |
|-------|---------|-------|
| Unit and end-to-end tests | `make test`, `make test-race` | ~1,000 unit tests; `e2e/` exercises every exported `Client`, `Server`, `ServerConn` method and `Value` constructor between a go-mms client and server (`TestAPICoverage` guard) |
| Coverage gate | `make coverage` | `scripts/coverage-gate.sh`: fails when any package or the total is below `COVERAGE_MIN` (default 90%); examples excluded |
| Fuzzing | `make fuzz` (15s per target), `make fuzz-short` (2s) | `scripts/fuzz.sh` discovers every `Fuzz*` target (57: PDU, BER, codec, ISO stack, value codec, server dispatch and session); `fuzz-ber`, `fuzz-pdu` restrict packages |
| Chaos | `make chaos` (`CHAOS_DURATION`, default 30s; `CHAOS_SEED`, 0 = random) | `TestChaos_*`, `TestStress_*` under `-race` through `internal/chaos` (delays, chunked I/O, bandwidth limit, drops, resets, stalls); seed logged for replay |
| Live interop | `make interop` (`-tags=interop`, Docker) | 120 tests, both directions against libiec61850 via mms-interop; `TestInteropCoverage` guard. See [INTEROP.md](INTEROP.md) |
| Static checks | `make vet`, `make lint`, `make vuln`, `make test-armv7` | go vet, golangci-lint, govulncheck, 32-bit build of `internal/pdu` |

`make check` runs fmt, tidy, vet, lint, vuln, test, test-race, coverage,
fuzz-short and test-armv7. CI (`.github/workflows/ci.yml`) runs the shared
Go workflow on Go 1.23–1.26 plus a `quality` job (coverage gate, 2s fuzz
pass, 5s chaos run); `.github/workflows/interop.yml` runs the interop
suite against a digest-pinned adapter image.

## Open items

- Testing against physical IEDs has not been formally established;
  interop is against libiec61850 only.
- Remaining protocol gaps are tracked in [COMPLIANCE.md](COMPLIANCE.md)
  (Remaining Gaps) and [KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md).
