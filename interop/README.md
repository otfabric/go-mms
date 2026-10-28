# go-mms interoperability tests

This package contains interoperability tests for `go-mms` against independent MMS implementations provided by [iec61850-interop](https://github.com/otfabric/iec61850-interop).

## Test directions

| File | Go role | Adapter counterpart |
|------|---------|---------------------|
| `libiec61850_client_test.go` | MMS client | `libiec61850-mms-server` |
| `libiec61850_server_test.go` | MMS server | `libiec61850-mms-client` |
| `ied_server_test.go` | MMS client | `<stack>-ied-server` (IED-only stacks such as iec61850bean) |

Tests start adapter containers, wait for the readiness event, exercise the `go-mms` API, and assert results. No pre-running containers are required.

## Running

```bash
# Build the adapter images first (in iec61850-interop)
cd ../iec61850-interop && make build-libiec61850 build-dscsystems build-iec61850-rust build-iec61850bean

# Run all interop tests
LIBIEC61850_IMAGE=iec61850-interop-libiec61850:local make interop

# Another peer stack, or all of them (see INTEROP.md#peer-stacks)
MMS_INTEROP_STACK=dscsystems make interop
make interop-all

# Or using the published v1.0.1 images (CI pins the digests in interop.yml)
LIBIEC61850_IMAGE=ghcr.io/otfabric/iec61850-interop-libiec61850:v1.0.1 make interop
```

## Environment variables

| Variable | Description | Default |
|----------|-------------|---------|
| `LIBIEC61850_IMAGE` | Docker image for the libiec61850 adapter | `iec61850-interop-libiec61850:local` |
| `MMS_SERVER_BINARY` | Path to `libiec61850-mms-server` binary (skips Docker) | — |
| `MMS_CLIENT_BINARY` | Path to `libiec61850-mms-client` binary (skips Docker) | — |
| `MMS_FIXTURE` | Path to the fixture JSON file | `testdata/interop.json` |
| `IEC61850BEAN_IMAGE` | Docker image for the iec61850bean IED adapter | `iec61850-interop-iec61850bean:local` |
| `IED_SERVER_BINARY` | Path to `<stack>-ied-server` binary (skips Docker) | — |

## Fixture

`testdata/interop.json` defines the MMS domain, variable types and initial values that both the adapter server and the `go-mms` server fixture expose. It is versioned alongside the pinned adapter image.
