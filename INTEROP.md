# go-mms Interoperability

`go-mms` owns its interoperability assertions. Tests in `interop/` consume the adapter images published by [mms-interop](https://github.com/otfabric/mms-interop) and assert `go-mms` behaviour against a live, independent implementation.

## Architecture

```
mms-interop
  libiec61850 adapter images
         |
    go-mms/interop/
      harness_test.go              lifecycle helpers
      libiec61850_client_test.go   TestClient_*
      libiec61850_server_test.go   TestServer_*
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
cd ../mms-interop && make build

# Run against local images
LIBIEC61850_IMAGE=mms-interop-libiec61850:local make interop

# Run against published images (version tag)
LIBIEC61850_IMAGE=ghcr.io/otfabric/mms-interop-libiec61850:v0.1.0 make interop

# Run against published images (CI — digest pinned)
LIBIEC61850_IMAGE=ghcr.io/otfabric/mms-interop-libiec61850@sha256:<digest> make interop
```

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

## MMS compatibility matrix

Black-box bidirectional interoperability against a pinned libiec61850 adapter through the independently versioned mms-interop infrastructure.

**Key:** ✓ covered · — not yet tested · n/a not applicable

| Capability | go-mms→libIEC | libIEC→go-mms | Notes |
|-----------|:---:|:---:|-------|
| Associate / conclude | ✓ | ✓ | |
| Reconnect after close | ✓ | ✓ | |
| Identify | ✓ | ✓ | |
| Status | ✓ | ✓ | |
| GetNameList (domains) | ✓ | ✓ | |
| GetNameList (variables in domain) | ✓ | ✓ | |
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
| GetVariableAccessAttributes | ✓ | ✓ | |
| Multi-variable read | ✓ | ✓ | |
| Multi-variable write | ✓ | ✓ | |
| Named variable list (define) | ✓ | ✓ | |
| Named variable list (read) | ✓ | ✓ | |
| Named variable list (delete) | ✓ | ✓ | |
| Read unknown domain (negative) | ✓ | ✓ | server stays connected |
| Read unknown variable (negative) | ✓ | ✓ | server stays connected |
| Write wrong type (negative) | ✓ | ✓ | value unchanged |
| Write read-only (negative) | ✓ | ✓ | ObjectAccessDenied; value unchanged |
| InformationReport | — | — | |
| File services | — | — | |
| Journal services | — | — | |

## Fixture

`interop/testdata/interop.json` is a synchronized copy of the canonical fixture from mms-interop. It must be updated alongside the pinned adapter image version when the fixture contract changes.
