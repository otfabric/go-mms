# go-mms Interoperability

`go-mms` owns its interoperability assertions. Tests in `interop/` consume the adapter images published by [iec61850-interop](https://github.com/otfabric/iec61850-interop) and assert `go-mms` behaviour against a live, independent implementation.

## Architecture

```
iec61850-interop
  adapter images: libiec61850, dscsystems, iec61850-rust
  (same contract: <stack>-mms-server / <stack>-mms-client, fixture, JSON Lines)
  IEC 61850 IED adapter image: iec61850bean
  (<stack>-ied-server serving the iec61850-v3 IEC 61850 fixture)
         |
    go-mms/interop/
      harness_test.go                        lifecycle helpers
      stack_test.go                          peer stacks, quirks and skips (MMS_INTEROP_STACK)
      fixture_server_test.go                 go-mms server model built from the fixture
      libiec61850_client_test.go             TestClient_* (core variable access)
      libiec61850_client_services_test.go    TestClient_* (files, journals, NVLs, reports, ...)
      libiec61850_server_test.go             TestServer_* (core variable access)
      libiec61850_server_services_test.go    TestServer_* (the adapter's service sequence)
      ied_server_test.go                     TestIEDServer_* (IED-only stacks: go-mms client → <stack>-ied-server)
      coverage_test.go                       API and service coverage guard
```

The suite is stack-parametrized: one test body per scenario, run against the
stack selected by `MMS_INTEROP_STACK`. (The file names predate the other
stacks.) Each test:
1. Starts the stack's adapter container with `docker run`.
2. Waits for the readiness event (`{"event":"ready",...}`) and checks its
   fixture revision and adapter name.
3. Exercises the `go-mms` API under test.
4. Asserts results.
5. Tears the container down.

No pre-running containers. No manual steps. Tests are gated behind `-tags=interop`.

## Running

```bash
# Build adapter images locally (in iec61850-interop)
cd ../iec61850-interop && make build-libiec61850 build-dscsystems build-iec61850-rust build-iec61850bean

# One stack (default libiec61850)
MMS_INTEROP_STACK=dscsystems make interop

# Every stack
make interop-all

# Published images (CI pins the iec61850-interop v1.0.0 digests in .github/workflows/interop.yml)
MMS_INTEROP_STACK=iec61850-rust IEC61850RUST_IMAGE=ghcr.io/otfabric/iec61850-interop-iec61850-rust:v1.0.0 make interop
```

CI (`.github/workflows/interop.yml`) runs the full matrix, one job per stack,
against images pinned from the iec61850-interop release. Bump the images and
the fixture copies (`interop/testdata/interop.json`,
`interop/testdata/iec61850/values.json`) together when iec61850-interop publishes
new adapters.

## Environment variables

| Variable | Description | Default |
|----------|-------------|---------|
| `MMS_INTEROP_STACK` | Peer stack: `libiec61850`, `dscsystems`, `iec61850-rust`, `iec61850bean` | `libiec61850` |
| `MMS_INTEROP_IMAGE` | Image for the selected stack (overrides the per-stack variable) | — |
| `LIBIEC61850_IMAGE` | libiec61850 adapter image | `iec61850-interop-libiec61850:local` |
| `DSCSYSTEMS_IMAGE` | dscsystems adapter image | `iec61850-interop-dscsystems:local` |
| `IEC61850RUST_IMAGE` | iec61850-rust adapter image | `iec61850-interop-iec61850-rust:local` |
| `IEC61850BEAN_IMAGE` | iec61850bean adapter image | `iec61850-interop-iec61850bean:local` |
| `MMS_SERVER_BINARY` | Path to `<stack>-mms-server` binary (skips Docker) | — |
| `MMS_CLIENT_BINARY` | Path to `<stack>-mms-client` binary (skips Docker) | — |
| `IED_SERVER_BINARY` | Path to `<stack>-ied-server` binary (skips Docker) | — |
| `MMS_FIXTURE` | Path to fixture JSON file | `testdata/interop.json` |

## Test naming

| Prefix | Go role | Adapter counterpart |
|--------|---------|---------------------|
| `TestClient_` | MMS client | `<stack>-mms-server` |
| `TestServer_` | MMS server | `<stack>-mms-client` |
| `TestIEDServer_` | MMS client | `<stack>-ied-server` (IED-only stacks) |

## Peer stacks

Peer limitations are never hidden as failures or silent passes. Each stack
in `interop/stack_test.go` declares:

- **quirks** — documented behaviours a test adapts its expectation to, so
  the rest of the scenario still runs;
- **skips** — tests the peer cannot run, each with a reason (`go test -v`
  prints it). Client-adapter steps answered `"error":"unsupported"` are
  skipped with that reason too.

Both mirror `knownLimitations` in iec61850-interop `fixtures/capabilities.json`.
`TestStackSkipsNameRealTests` fails when a skip names a test that does not
exist.

| Stack | Upstream | Result | Quirks | Skipped |
|-------|----------|--------|--------|---------|
| libiec61850 | libiec61850 1.6.2.1-89-g6e265c54 | 120 pass | VMD named-list reports only; FileOpen ignores the seek | — |
| dscsystems | dscsystems/go-iec61850 6bf3f37 (GPL, isolated container) | 112 pass, 8 skip | client drops FileOpen `lastModified`; no write to a named variable list | index-range alternate access (server refuses >1 element); client has no alternate access, association-specific NVLs or NVL write |
| iec61850bean | beanit iec61850bean 1.9.0 (IED adapters only) | 14 pass (`TestIEDServer_*`); the 118 MMS contract tests skip | no Identify (no response); no Conclude (no response, `Close` ends at its deadline); a read of an unknown name disconnects | MMS contract suite: the image has no `<stack>-mms-server` / `-mms-client` |
| iec61850-rust | csp0924/iec61850-rust (d4e8952) | 120 pass | — (its known limitations match libiec61850's and are asserted: ObtainFile role reversal answered file-other, domain/VMD NVL deletion object-access-unsupported, client abort closes the transport, in-memory file store) | — |

Skipped tests per stack:

| Stack | Test | Reason |
|-------|------|--------|
| dscsystems | `TestClient_ReadArrayRange` | server refuses index-range alternate access of more than one element |
| dscsystems | `TestClient_ReadVariables_WriteVariables_AlternateAccess` | same |
| dscsystems | `TestServer_ReadArrayElements`, `TestServer_WriteArrayElements`, `TestServer_ReadComponent`, `TestServer_WriteComponent` | client reads and writes whole variables only (`unsupported`) |
| dscsystems | `TestServer_NVL_DomainScope`, `TestServer_NVL_AssociationScope` | client cannot write a list / has no association-specific names (`unsupported`) |

## Coverage guard

`TestInteropCoverage` (in `interop/coverage_test.go`, no adapter needed, runs
with `-tags=interop`) fails when:

- an exported `Client`, `Server` or `ServerConn` method has no interop test
  and no entry in `interopAPIExempt` with a reason;
- a mapped test does not exist or the method is never called from the
  interop sources;
- a confirmed service the server dispatches in `server.go` has no
  `TestServer_` test.

Adding a public method or a server service therefore needs an interop test
or a written exemption. `Client.Done` and `Client.Err` map to
`TestClient_PeerDisconnect`, `Server.Shutdown` to `TestServer_Shutdown`. The in-process counterpart is `e2e/coverage_test.go`
(`TestAPICoverage`, part of `go test ./...`), which requires every exported
method, constructor and `Value` constructor to be exercised by an `e2e` test
between a go-mms client and a go-mms server.

## MMS compatibility matrix

Black-box bidirectional interoperability against a pinned libiec61850 adapter through the independently versioned iec61850-interop infrastructure.

**Key:** ✓ covered · ◐ covered, peer limitation asserted · — not tested · n/a not applicable

The go-mms→libIEC column is the go-mms client against `libiec61850-mms-server`; libIEC→go-mms is the libiec61850 client against the go-mms server.

| Capability | go-mms→libIEC | libIEC→go-mms | Notes |
|-----------|:---:|:---:|-------|
| Associate / conclude | ✓ | ✓ | |
| Negotiated parameters | ✓ | n/a | `Client.Negotiated` |
| Abort (client-initiated) | ✓ | ✓ | |
| Abort (server-initiated) | n/a | ✓ | `ServerConn.Abort`; the peer observes the connection loss |
| Reconnect after close | ✓ | ✓ | |
| Peer disconnect detection | ✓ | n/a | `TestClient_PeerDisconnect`: the adapter is killed; `Client.Done` closes, `Client.Err` and later requests match `ErrClosed` |
| Graceful server shutdown | n/a | ✓ | `TestServer_Shutdown`: `Server.Shutdown` ends the shared libiec61850 client run; `ListenAndServe` returns `ErrServerClosed`, no association left |
| Identify | ✓ | ✓ | |
| Status | ✓ | ✓ | logical/physical from the fixture |
| UnsolicitedStatus | n/a | — | sent after each report; the libiec61850 client API has no handler for it |
| GetNameList (domains) | ✓ | ✓ | |
| GetNameList (variables in domain) | ✓ | ✓ | including `GetNameListAll` across pages |
| GetNameList (VMD variables) | n/a | ✓ | |
| GetNameList (journals) | ✓ | ✓ | |
| GetNameList (unknown domain, negative) | ✓ | ✓ | ServiceError Access / object-non-existent |
| Read boolean | ✓ | ✓ | |
| Read integer | ✓ | ✓ | |
| Read float32 | ✓ | ✓ | |
| Read unsigned | ✓ | ✓ | |
| Read visible-string | ✓ | ✓ | |
| Read octet-string | ✓ | ✓ | |
| Read bit-string | ✓ | ✓ | |
| Read utc-time | ✓ | ✓ | |
| Read array | ✓ | ✓ | |
| Read structure | ✓ | ✓ | |
| Write (writable variable) | ✓ | ✓ | |
| Write + read-back | ✓ | ✓ | |
| GetVariableAccessAttributes | ✓ | ✓ | scalars, arrays and named structures |
| Multi-variable read | ✓ | ✓ | |
| Multi-variable write | ✓ | ✓ | |
| ReadObject / WriteObject, ReadVariables / WriteVariables | ✓ | n/a | client helpers; with alternate access |
| Read/write structure component | ✓ | ✓ | alternate access by component name |
| Read/write array element and range | ✓ | ✓ | alternate access by index and index range |
| Named variable list, domain scope (define, attributes, read, write, delete) | ✓ | ✓ | |
| Named variable list, VMD scope | ✓ | ✓ | |
| Named variable list, association scope | ✓ | ✓ | |
| Named variable list registered by the server | n/a | ✓ | `Server.RegisterNamedVariableList` |
| Named variable list read with `SpecificationWithResult` | ✓ | n/a | |
| Named variable list, unknown (negative) | ✓ | n/a | |
| Delete all domain / VMD lists | ◐ | n/a | libiec61850 implements only `scopeOfDelete` specific |
| Read unknown domain (negative) | ✓ | ✓ | object-non-existent; server stays connected |
| Read unknown variable (negative) | ✓ | ✓ | object-non-existent; server stays connected |
| Write wrong type (negative) | ✓ | ✓ | value unchanged |
| Write read-only (negative) | ✓ | ✓ | ObjectAccessDenied; value unchanged |
| InformationReport, VMD named-list form | ✓ | ✓ | `Server.Broadcast`, `Client.OnInformationReport` |
| InformationReport, listOfVariable form | ◐ | ✓ | libiec61850 still encodes the server-side report with a wrong length, so the adapter does not send it |
| FileDirectory (root, subdirectory, paging) | ✓ | ✓ | go-mms issue #2 |
| FileDirectory naming conventions | ✓ | ✓ | libiec61850 server flattens into full `/` paths; the go-mms fixture server lists per level with `COMTRADE\` as a zero-size entry (see API.md) |
| FileDirectory entry without lastModified | — | ✓ | libiec61850 server always sends it; go-mms omits it when the provider has no time |
| FileOpen without lastModified | — | ◐ | libiec61850 client reports an uninitialised value when the field is absent |
| FileOpen / FileRead / FileClose (multi-chunk) | ✓ | ✓ | |
| FileOpen initial position | ◐ | ✓ | libiec61850 server ignores `initialPosition` |
| FileOpen non-existent (negative) | ✓ | ✓ | |
| FileReadAll / DownloadFile | ✓ | n/a | |
| FileDelete | ✓ | ✓ | |
| FileRename | ✓ | ✓ | the adapter build enables `MMS_RENAME_FILE_SERVICE` |
| ObtainFile | ◐ | ✓ | libiec61850 server pulls the file from the client (role reversal), which go-mms does not serve |
| ReadJournal, time range | ✓ | ✓ | including entry content and reason code |
| ReadJournal, start after entry | ✓ | ✓ | |
| ReadJournal, unknown journal (negative) | ✓ | ✓ | |
| Cancel | — | — | the go-mms client does not send Cancel |

The ◐ rows are listed with the test that pins the peer behaviour in the iec61850-interop [`COVERAGE.md`](https://github.com/otfabric/iec61850-interop/blob/main/COVERAGE.md).

## Related test suites

The interop suite complements the in-process quality gates (all without
Docker):

| Target | What it runs |
|--------|--------------|
| `make test` / `make test-race` | Unit and `e2e` tests (go-mms client ↔ go-mms server) |
| `make coverage` | `scripts/coverage-gate.sh`: fails below `COVERAGE_MIN` (default 90%) per package or in total |
| `make fuzz` / `make fuzz-short` | `scripts/fuzz.sh`: every `Fuzz*` target, discovered with `go test -list`, for `FUZZTIME` (default 15s; 2s for `fuzz-short`) |
| `make chaos` | `TestChaos_*` / `TestStress_*` under `-race` through `internal/chaos` fault-injecting connections (delays, chunked I/O, bandwidth limit, drops, resets, stalls); `CHAOS_DURATION` (default 30s) and `CHAOS_SEED` (0 = random; the seed is logged for replay), passed as `MMS_CHAOS_DURATION` / `MMS_CHAOS_SEED` |

CI runs the coverage gate, a 2s fuzz pass and a 5s chaos run in the
`quality` job of `.github/workflows/ci.yml`.

## Fixture

`interop/testdata/interop.json` is a synchronized copy of the canonical fixture from iec61850-interop. It must be updated alongside the pinned adapter image version when the fixture contract changes.
