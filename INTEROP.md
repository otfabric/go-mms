# go-mms Interoperability

`go-mms` owns its interoperability assertions. Tests in `interop/` consume the adapter images published by [mms-interop](https://github.com/otfabric/mms-interop) and assert `go-mms` behaviour against a live, independent implementation.

## Architecture

```
mms-interop
  libiec61850 adapter images
         |
    go-mms/interop/
      harness_test.go                        lifecycle helpers
      fixture_server_test.go                 go-mms server model built from the fixture
      libiec61850_client_test.go             TestClient_* (core variable access)
      libiec61850_client_services_test.go    TestClient_* (files, journals, NVLs, reports, ...)
      libiec61850_server_test.go             TestServer_* (core variable access)
      libiec61850_server_services_test.go    TestServer_* (the adapter's service sequence)
      coverage_test.go                       API and service coverage guard
```

Each test:
1. Starts the adapter container with `docker run`.
2. Waits for the readiness event (`{"event":"ready",...}`) on stdout.
3. Exercises the `go-mms` API under test.
4. Asserts results.
5. Tears the container down.

No pre-running containers. No manual steps. Tests are gated behind `-tags=interop`.

## Running

```bash
# Build adapter images locally (in mms-interop)
cd ../mms-interop && make build-libiec61850 ADAPTER_VERSION=v0.2.0

# Run against local images
LIBIEC61850_IMAGE=mms-interop-libiec61850:local make interop

# Run against the published v0.2.0 image (version tag)
LIBIEC61850_IMAGE=ghcr.io/otfabric/mms-interop-libiec61850:v0.2.0 make interop

# Same image, pinned by digest (what CI uses)
LIBIEC61850_IMAGE=ghcr.io/otfabric/mms-interop-libiec61850@sha256:0ce17d8471eda3a3ef8949a5819b44359a2aeaec23695fee06c5dbea083f0391 make interop
```

CI (`.github/workflows/interop.yml`) pulls that digest from GHCR. Bump the
digest and `interop/testdata/interop.json` together when mms-interop
publishes a new adapter.

## Environment variables

| Variable | Description | Default |
|----------|-------------|---------|
| `LIBIEC61850_IMAGE` | Adapter image | `mms-interop-libiec61850:local` |
| `MMS_SERVER_BINARY` | Path to `libiec61850-mms-server` binary (skips Docker) | — |
| `MMS_CLIENT_BINARY` | Path to `libiec61850-mms-client` binary (skips Docker) | — |
| `MMS_FIXTURE` | Path to fixture JSON file | `testdata/interop.json` |

## Test naming

| Prefix | Go role | Adapter counterpart |
|--------|---------|---------------------|
| `TestClient_` | MMS client | `libiec61850-mms-server` |
| `TestServer_` | MMS server | `libiec61850-mms-client` |

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
or a written exemption. The in-process counterpart is `e2e/coverage_test.go`
(`TestAPICoverage`, part of `go test ./...`), which requires every exported
method, constructor and `Value` constructor to be exercised by an `e2e` test
between a go-mms client and a go-mms server.

## MMS compatibility matrix

Black-box bidirectional interoperability against a pinned libiec61850 adapter through the independently versioned mms-interop infrastructure.

**Key:** ✓ covered · ◐ covered, peer limitation asserted · — not tested · n/a not applicable

The go-mms→libIEC column is the go-mms client against `libiec61850-mms-server`; libIEC→go-mms is the libiec61850 client against the go-mms server.

| Capability | go-mms→libIEC | libIEC→go-mms | Notes |
|-----------|:---:|:---:|-------|
| Associate / conclude | ✓ | ✓ | |
| Negotiated parameters | ✓ | n/a | `Client.Negotiated` |
| Abort (client-initiated) | ✓ | ✓ | |
| Abort (server-initiated) | n/a | ✓ | `ServerConn.Abort`; the peer observes the connection loss |
| Reconnect after close | ✓ | ✓ | |
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

The ◐ rows are listed with the test that pins the peer behaviour in the mms-interop [`COVERAGE.md`](https://github.com/otfabric/mms-interop/blob/main/COVERAGE.md).

## Fixture

`interop/testdata/interop.json` is a synchronized copy of the canonical fixture from mms-interop. It must be updated alongside the pinned adapter image version when the fixture contract changes.
