# Testing

Set-up and the pull request gate are in [CONTRIBUTING.md](CONTRIBUTING.md).
`make help` lists every target. The Makefile sets `GOWORK=off`.

## Tiers

| Tier | Command | Prerequisites | Runs in CI |
|---|---|---|---|
| Unit and e2e | `make test` | Go | `ci.yml` |
| Race detector | `make test-race` | Go, cgo for `-race` | `ci.yml` |
| End-to-end only | `make e2e` | Go | as part of the test jobs |
| Coverage gate | `make coverage` (alias `make coverage-check`) | Go | `ci.yml`, job `quality` |
| Fuzz | `make fuzz`, `make fuzz-short`, `make fuzz-ber`, `make fuzz-pdu` | Go | `ci.yml`, 10 s per target |
| Chaos and stress | `make chaos` | Go | `ci.yml`, job `quality`, `CHAOS_DURATION=5s` |
| Interop | `make interop`, `make interop-all` | Docker and adapter images | `interop.yml`, one job per stack |
| Benchmarks | `make bench` | Go | no |
| Build for other platforms | `make cross`, `make test-armv7` | Go | no |
| 32-bit platform | `GOARCH=386 go test ./...` | Go on linux/amd64 (386 binaries run natively) | `nightly.yml`, job `test-386` |
| All local gates | `make check` | golangci-lint, govulncheck | no, CI runs its parts |

`make cross` and `make test-armv7` only compile. The 32-bit tests run in
`nightly.yml` on linux/386, where an `int` is 32 bits.

## When CI runs

| Event | Workflow | What runs |
|---|---|---|
| Pull request | `pr.yml` | `docs.yml` (docsync, otfabric-policy) always; `ci.yml` and `interop.yml` unless every changed file is documentation; then `OT Fabric Required Checks` |
| Push to a branch | none | nothing; dispatch `ci.yml` or `interop.yml` to test a branch without a pull request |
| Push to `main` | `docs.yml` | docsync and otfabric-policy; the merged commit was tested as the pull request's merge commit |
| Nightly, by hand | `nightly.yml`, `self-interop.yml` | long fuzz and chaos; this commit against the latest published go-mms |
| Release | `release.yml` | the shared release workflow tests the release commit again |

Documentation means an added or modified `*.md` file outside `.github/`,
`testdata/` and `vendor/`, other than `AGENTS.md` and `CLAUDE.md`. A deleted
or renamed document, or any other file, runs everything. `OT Fabric Required
Checks` fails unless every job that applies succeeded; its summary lists
them.

## End-to-end suite

`e2e/` runs the go-mms client against the go-mms server over real TCP through
`transport/iso`. `TestAPICoverage` (`e2e/coverage_test.go`) keeps the suite
covering every exported `Client`, `Server` and `ServerConn` method. A peer
written by this project proves self-consistency, not interoperability.

## Chaos

The chaos and stress tests in `e2e/chaos_test.go` use `internal/chaos` to
inject faults. Under `go test` they run for 500 ms with seed 1.

| Variable | Meaning |
|---|---|
| `MMS_CHAOS_DURATION` | run time of each timed test, a Go duration; `make chaos` sets it from `CHAOS_DURATION` (default 30s). Six tests run one after the other, so keep `CHAOS_TIMEOUT` (default 30m) above six times this value |
| `MMS_CHAOS_SEED` | seed; `0` picks a time-based seed; `make chaos` sets it from `CHAOS_SEED` (default 0) |

Each run logs its seed. Replay a failure with
`make chaos CHAOS_SEED=<seed>`. [RACE_NOTES.md](RACE_NOTES.md) lists what each
test exercises.

## Interop

The interop suite has the build tag `interop`. Its peers, images, environment
variables and results are in [INTEROP.md](INTEROP.md) and
[interop/README.md](interop/README.md). `TestInteropCoverage`
(`interop/coverage_test.go`) fails when an exported method has no interop test
or written exemption.

A missing Docker or image fails the test rather than skipping it. `t.Skip` in
the suite is used only where the selected peer does not support the scenario,
so a green run of one stack does not cover every test. Read the skips in the
output.

`MMS_INTEROP_REQUIRE_CALLS=1` turns the skip for a client adapter that answers
`unsupported` into a failure, so an adapter image that loses a call cannot pass
unnoticed. `interop.yml` sets it for every stack except dscsystems, whose client
has six documented gaps.

## Goroutine leaks

Packages that start goroutines (root, `transport/iso`, `internal/serverconn`,
and `e2e`) run `goleak.VerifyTestMain` in `leak_main_test.go`: a goroutine left
running after the tests fails the package. `go.uber.org/goleak` is a test-only
dependency.

## Lifecycle contract matrix

The rows of the contract test matrix (otfabric/engineering
standards/lifecycle.md) and the tests that hold them. Tests in the root
package unless a directory is given. They wait for events through channels or
`testing/synctest`, not `time.Sleep`.

| Row | Contract | Tests |
|---|---|---|
| L1 | Close is idempotent | `TestCloseAlreadyClosed`, `TestDoubleClose`, `TestCloseWithTimeout`, `TestShutdownTwice`, `TestTransportCloseIdempotent` (`transport/iso`) |
| L2 | Concurrent Close | `TestDoubleCloseConcurrent`: both return nil, `Done` closed, no goroutine left |
| L3 | Close during receive | `TestCloseDuringInFlightRequest`, `TestReaderExitClosesTransport`, `TestConcludeWithoutRequestEndsAssociation`, `TestConcurrentCloseReceive` (`transport/iso`) |
| L4 | Close during a stalled write | `TestCloseBoundedByContextWhileWriteStalled` (goleak), `TestStalledWriteEndsAssociation`, `TestCloseWritesNoQueuedRequest`, `TestConcurrentCloseSend` (`transport/iso`) |
| L5 | Cancel during connect or association | `TestServer_AssociationTimeout`, `TestDialContextCancellation`, `TestShutdownWaitsForAssociatingServe`, `TestListener_SlowHandshake` (`e2e`) |
| L6 | Cancel during a request | `TestContextCancellationDuringRequest`, `TestClient_RequestTimeout_Loopback`, `OutstandingWaitHonoursContext` (`negotiated_limits_test.go`) |
| L7 | Late response after cancel | `AbandonedRequestStaysOutstanding` (`negotiated_limits_test.go`), `TestContextCancellationDuringRequest`, `TestLateResponseAfterCancel` (`internal/invoke`) |
| L8 | Reconnect | not applicable: the library does not reconnect; dial retry is `RetryPolicy` (`TestDial_WithRetry` in `e2e`, `TestBackoffMultiplierValues`) |
| L9 | A callback re-enters the server | `TestReadCallbackRegistersHandlers`, `TestBlockedCallbackDoesNotStopOtherAssociations`, `TestSetVariableReadWhileReading` |
| L10 | A callback calls Close | `TestReportHandlerEndsAssociation` |
| L11 | A slow callback | not covered: the InformationReport handler runs on the reader goroutine ([KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md)) |
| L12 | Bounded queues and outstanding requests | `OutstandingCallingLimit`, `FailFast` (`negotiated_limits_test.go`), `TestCloseWhileConcurrentRequests`, `TestListenerPendingHandshakeBound` (`transport/iso`), `TestChaos_OutstandingLimit` (`e2e`) |
| L13 | Session isolation | `TestAssociationScopeIsolation`, `TestAssocListResultsAreCopies`, `TestServerConcurrentClients`, `TestStress_Concurrent` (`e2e`) |
| L14 | A failed constructor releases its resources | `TestDialerConnWithErrorClosed` (`transport/iso`); goleak in every `TestMain` |
| L15 | Server shutdown | `TestServer_Shutdown`, `TestServer_Shutdown_StopsListenAndServe`, `TestShutdownWaitsForAssociatingServe`, `TestListenAndServeJoinsConnections`, `TestListenerPendingHandshakeBound` (`transport/iso`), `TestServer_Shutdown_InFlight` (`e2e`) |
| L16 | Reconnect loop | not applicable: no reconnect loop |

## Extended tier

`.github/workflows/nightly.yml` runs every Fuzz target for 2 minutes, split
over three jobs (`make fuzz FUZZTIME=2m FUZZ_SHARD=<i>/3`), the chaos suite
under the race detector with a random seed, 5 minutes for each of its six timed
tests (`make chaos CHAOS_DURATION=5m CHAOS_TIMEOUT=45m`), and the unit and e2e
tests on linux/386 (`GOARCH=386 go test ./...`, job `test-386`).

## Coverage policy

`scripts/coverage-gate.sh` fails when the total or any package with statements
is below `COVER_PKG_MIN` (90, `make coverage-pkg-check`). `ci.yml` applies the same floor to the
shared workflow with `coverage-min: "90"`. Do not lower it.

The only exclusion is `examples/`: example programs are not library code.

## Fuzz regression

`scripts/fuzz.sh` finds targets with `go test -list`, so a new target needs no
registration. `make fuzz` uses `FUZZTIME` (default 15s); `make fuzz-short` uses
2 s and is part of `make check`. `FUZZ_SHARD=i/n` runs the i-th of every n
targets in discovery order, as the nightly jobs do.

When a target fails, Go writes the input to `testdata/fuzz/<Target>/` in the
package. Keep that file in the fix: `go test` then replays it on every run
(see `testdata/fuzz/FuzzValueCodec/`). Run one target with:

```bash
go test -run='^$' -fuzz='^FuzzValueCodec$' -fuzztime=30s .
```

The script retries once when the fuzz coordinator times out without a failing
input. A real failure is never retried.

A fuzz target asserts an invariant, not only that the decoder returns. A decoder
target in `internal/pdu` re-encodes what decodes, decodes that encoding, and
checks that encoding the result again gives the same octets
(`internal/pdu/invariant_test.go`). Where the package has no encoder for the
decoded form, the target checks that the decoded value is consistent, for
example that a CHOICE has one alternative set.

## Allocation budgets

`TestDecoderAllocationBudget` (`internal/pdu/alloc_budget_test.go`) decodes,
for each top-level decoder, the input that fits the default PDU size (65,000
octets) and decodes to the most values per octet, and fails when the heap
allocation exceeds a fixed number of octets per input octet. A cost that grows
faster than the input exceeds the budget. `TestUnmarshalReadRequestSelectionBudget`
does the same for the deepest AlternateAccess chain, and
`numeric_bounds_test.go` for `TypeSpec.DefaultValue`.

## Golden data

`internal/pdu/testdata/golden` and `internal/codec/testdata` hold expected
encodings for the tests in those packages.
