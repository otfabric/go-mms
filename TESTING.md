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
| All local gates | `make check` | golangci-lint, govulncheck | no, CI runs its parts |

`make cross` and `make test-armv7` only compile. No test runs on those
targets here.

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
| `MMS_CHAOS_DURATION` | run time, a Go duration; `make chaos` sets it from `CHAOS_DURATION` (default 30s) |
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

## Extended tier

`.github/workflows/nightly.yml` runs every Fuzz target for 2 minutes
(`make fuzz FUZZTIME=2m`) and the chaos suite for 10 minutes under the race
detector with a random seed (`make chaos CHAOS_DURATION=10m`).

## Coverage policy

`scripts/coverage-gate.sh` fails when the total or any package with statements
is below `COVER_PKG_MIN` (90, `make coverage-pkg-check`). `ci.yml` applies the same floor to the
shared workflow with `coverage-min: "90"`. Do not lower it.

The only exclusion is `examples/`: example programs are not library code.

## Fuzz regression

`scripts/fuzz.sh` finds targets with `go test -list`, so a new target needs no
registration. `make fuzz` uses `FUZZTIME` (default 15s); `make fuzz-short` uses
2 s and is part of `make check`.

When a target fails, Go writes the input to `testdata/fuzz/<Target>/` in the
package. Keep that file in the fix: `go test` then replays it on every run
(see `testdata/fuzz/FuzzValueCodec/`). Run one target with:

```bash
go test -run='^$' -fuzz='^FuzzValueCodec$' -fuzztime=30s .
```

The script retries once when the fuzz coordinator times out without a failing
input. A real failure is never retried.

## Golden data

`internal/pdu/testdata/golden` and `internal/codec/testdata` hold expected
encodings for the tests in those packages.
