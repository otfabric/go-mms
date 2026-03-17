# Go↔C Interop Harness

This directory contains scripts and test programs for verifying go-mms
against the C reference implementation (libIEC61850).

## C source layout

The `../sources/mms/` tree contains the MMS-layer source extracted from
[libIEC61850](https://github.com/mz-automation/libIEC61850). It includes
the ISO stack (COTP, session, presentation, ACSE) and the MMS client/server
codec, but does **not** include the full library build system or example
programs.

To run interop tests you need the complete libIEC61850 checkout so that
the `server_example_basic_io` binary can be built.

## Prerequisites

- Go 1.22+
- GCC or Clang
- CMake 3.10+
- A full libIEC61850 clone (see below)

## Building the C server

```bash
# Clone the full libIEC61850 repo (if not already present)
git clone https://github.com/mz-automation/libIEC61850.git /tmp/libIEC61850

# Build
cd /tmp/libIEC61850
mkdir -p build && cd build
cmake ..
make -j$(nproc)
```

The server example binary will be at:

```
/tmp/libIEC61850/build/examples/server_example_basic_io/server_example_basic_io
```

## Running interop tests

```bash
# Start C server (set INTEROP_SERVER_BIN if using a non-default path)
./interop/start_c_server.sh

# Run Go client tests against it
go test -tags interop -v ./interop/...

# Stop C server
./interop/stop_c_server.sh
```

## Custom server address

Set `MMS_INTEROP_ADDR` to override the default `localhost:102`:

```bash
MMS_INTEROP_ADDR=192.168.1.100:102 go test -tags interop -v ./interop/...
```
