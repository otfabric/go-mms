# Go↔C Interop Testing

## Overview

The `interop/` directory contains a harness for testing go-mms against the
C reference implementation (libIEC61850).

The `sources/mms/` tree contains the MMS-layer source extracted from
libIEC61850 (ASN.1/BER codec, ISO stack, MMS client/server). It does
**not** include the full library build system or example programs, so
interop testing requires a separate full libIEC61850 checkout.

### MMS services available in the C reference

Based on the source files in `sources/mms/iso_mms/server/`:

| Service | C server file | Go client method |
|---------|--------------|-----------------|
| Identify | `mms_identify_service.c` | `Client.Identify` |
| Status | `mms_status_service.c` | `Client.Status` |
| GetNameList | `mms_get_namelist_service.c` | `Client.GetNameList` |
| GetVariableAccessAttributes | `mms_get_var_access_service.c` | `Client.GetVariableAccessAttributes` |
| Read | `mms_read_service.c` | `Client.Read` / `Client.ReadMultiple` |
| Write | `mms_write_service.c` | `Client.Write` / `Client.WriteVariables` |
| InformationReport | `mms_information_report.c` | `Client.OnInformationReport` |
| NamedVariableList | `mms_named_variable_list_service.c` | `Client.DefineNamedVariableList` |
| File services | `mms_file_service.c` | `Client.FileOpen` / `Client.FileRead` / ... |
| Journal | `mms_journal_service.c` | `Client.ReadJournal` |

## Current status

| Scenario | Status | Notes |
|----------|--------|-------|
| Go client → C server: Identify | 🔲 Ready | Test implemented |
| Go client → C server: Status | 🔲 Ready | Test implemented |
| Go client → C server: GetNameList | 🔲 Ready | Test implemented |
| Go client → C server: Read | 🔲 Ready | Test implemented |
| Go client → C server: GetVariableAccessAttributes | 🔲 Ready | Test implemented |
| Go client → C server: Write | 📋 Planned | Not yet implemented |
| Go client → C server: InformationReport | 📋 Planned | Not yet implemented |
| Go client → C server: File services | 📋 Planned | Not yet implemented |
| Go server → C client | 📋 Planned | Not yet implemented |

## Prerequisites

- Go 1.21+
- GCC or Clang
- CMake 3.10+

## Quick start

1. Clone and build the full libIEC61850:
   ```bash
   git clone https://github.com/mz-automation/libIEC61850.git /tmp/libIEC61850
   cd /tmp/libIEC61850 && mkdir -p build && cd build && cmake .. && make
   ```

2. Start the C server:
   ```bash
   ./interop/start_c_server.sh
   ```

3. Run interop tests:
   ```bash
   go test -tags interop -v ./interop/...
   ```

4. Stop the C server:
   ```bash
   ./interop/stop_c_server.sh
   ```

## Custom server address

Set `MMS_INTEROP_ADDR` to override the default `localhost:102`:

```bash
MMS_INTEROP_ADDR=192.168.1.100:102 go test -tags interop -v ./interop/...
```

## Custom server binary

Set `INTEROP_SERVER_BIN` to use a different server binary:

```bash
INTEROP_SERVER_BIN=/path/to/my/server ./interop/start_c_server.sh
```

## Adding new scenarios

Add test functions to `interop/interop_test.go` using the standard Go testing
framework. All tests are gated behind the `interop` build tag so they don't
run during normal `go test ./...`.
