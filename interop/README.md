# go-mms interoperability tests

This package contains interoperability tests for `go-mms` against independent MMS implementations provided by [mms-interop](https://github.com/otfabric/mms-interop).

## Test directions

| File | Go role | Adapter counterpart |
|------|---------|---------------------|
| `libiec61850_client_test.go` | MMS client | `libiec61850-mms-server` |
| `libiec61850_server_test.go` | MMS server | `libiec61850-mms-client` |

Tests start adapter containers, wait for the readiness event, exercise the `go-mms` API, and assert results. No pre-running containers are required.

## Running

```bash
# Build the adapter images first (in mms-interop)
cd ../mms-interop && make build-libiec61850 ADAPTER_VERSION=v0.2.0

# Run all interop tests
LIBIEC61850_IMAGE=mms-interop-libiec61850:local make interop

# Or using the published v0.2.0 image (CI pins the digest in interop.yml)
LIBIEC61850_IMAGE=ghcr.io/otfabric/mms-interop-libiec61850:v0.2.0 make interop
```

## Environment variables

| Variable | Description | Default |
|----------|-------------|---------|
| `LIBIEC61850_IMAGE` | Docker image for the libiec61850 adapter | `mms-interop-libiec61850:local` |
| `MMS_SERVER_BINARY` | Path to `libiec61850-mms-server` binary (skips Docker) | — |
| `MMS_CLIENT_BINARY` | Path to `libiec61850-mms-client` binary (skips Docker) | — |
| `MMS_FIXTURE` | Path to the fixture JSON file | `testdata/interop.json` |

## Fixture

`testdata/interop.json` defines the MMS domain, variable types and initial values that both the adapter server and the `go-mms` server fixture expose. It is versioned alongside the pinned adapter image.
