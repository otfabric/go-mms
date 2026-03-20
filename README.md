# go-mms

[![Go](https://img.shields.io/badge/Go-1.21%2B-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/otfabric/go-mms)](https://goreportcard.com/report/github.com/otfabric/go-mms)
[![CI](https://github.com/otfabric/go-mms/actions/workflows/ci.yml/badge.svg)](https://github.com/otfabric/go-mms/actions/workflows/ci.yml)
[![Codecov](https://codecov.io/github.com/otfabric/go-mms/graph/badge.svg)](https://app.codecov.io/github.com/otfabric/go-mms)
[![Release](https://img.shields.io/github/v/release/otfabric/go-mms?label=release)](https://github.com/otfabric/go-mms/releases)


A pure-Go implementation of the MMS (Manufacturing Message Specification)
protocol, ISO 9506-1/2, designed as the base transport layer for industrial
automation systems including IEC 61850.

## What is go-mms

- **Pure Go, no CGO** — builds and cross-compiles with the standard toolchain.
- **Client and server** — both sides of the protocol in one library.
- **Full MMS service coverage** — variable access, named variable lists, file
  services, journal services, information reports, and more.
- **Base layer for [go-iec61850](https://github.com/otfabric/go-iec61850)** —
  go-mms handles MMS; IEC 61850 abstractions live in a separate package.

## Features

| Category | Services |
|----------|----------|
| Association | Initiate, Conclude, Abort |
| Identity & Status | Identify, Status |
| Variable Access | Read (single, multi-variable, alternate access), Write (single, multi-variable, alternate access) |
| Named Variable Lists | DefineNamedVariableList, GetNamedVariableListAttributes, DeleteNamedVariableList |
| Information Reports | Send (server), receive (client), broadcast |
| File Services | Open, Read, Close, Directory, Delete, Rename, ObtainFile, DownloadFile |
| Journal Services | ReadJournal (time-range and start-after cursors) |
| Type Introspection | GetVariableAccessAttributes |
| Object Enumeration | GetNameList with automatic pagination |

All services are supported on both client and server unless noted in
[COMPLIANCE.md](COMPLIANCE.md).

## Quick Start

### Installation

```bash
go get github.com/otfabric/go-mms
```

Requires Go 1.21 or later.

### Client

```go
package main

import (
	"context"
	"fmt"
	"log"

	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

func main() {
	ctx := context.Background()

	client, err := iso.Dial(ctx, "10.0.0.1:102",
		iso.WithClientDialOptions(mms.DialOptions{
			MMS: mms.MMSOptions{MaxPDUSize: 65000},
		}),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close(ctx)

	id, err := client.Identify(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Vendor: %s  Model: %s  Rev: %s\n", id.Vendor, id.Model, id.Revision)

	result, err := client.Read(ctx, mms.ReadRequest{
		DomainID: "MyDomain",
		ItemID:   "MyVariable",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Value:", result.Value)
}
```

### Server

```go
package main

import (
	"context"
	"errors"
	"log"

	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

func main() {
	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{MaxPDUSize: 65000},
	})

	srv.HandleIdentify(func(_ context.Context, _ mms.IdentifyRequest) (*mms.ServerIdentity, error) {
		return &mms.ServerIdentity{Vendor: "Acme", Model: "Controller", Revision: "1.0"}, nil
	})

	srv.RegisterDomain("process")
	srv.RegisterVariable(mms.Variable{
		Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "process", ItemID: "temperature"},
		TypeSpec: mms.TypeSpec{Type: mms.ValueTypeFloat, FormatWidth: 32, ExponentWidth: 8},
		Read: func(_ context.Context) (*mms.Value, error) {
			return mms.NewFloat(21.5), nil
		},
		Write: func(_ context.Context, v *mms.Value) error {
			f, ok := v.Float64()
			if !ok {
				return errors.New("expected float")
			}
			_ = f // store the value
			return nil
		},
	})

	ln, err := iso.Listen(":102")
	if err != nil {
		log.Fatal(err)
	}
	log.Fatal(srv.ListenAndServe(context.Background(), ln))
}
```

See [`_examples/`](_examples/) for complete runnable examples.

## Architecture

go-mms implements the full ISO/OSI upper-layer stack required by MMS:

```
TPKT (RFC 1006)
 └─ COTP (ISO 8073)
     └─ Session (ISO 8327-1)
         └─ Presentation (ISO 8823-1)
             └─ ACSE (ISO 8650-1)
                 └─ MMS (ISO 9506-1/2)
```

The lower transport layers (TPKT and COTP) are provided by companion
libraries [go-tpkt](https://github.com/otfabric/go-tpkt) and
[go-cotp](https://github.com/otfabric/go-cotp). The `transport/iso`
subpackage wires them together and exposes `Dial` (client) and `Listen`
(server) entry points.

For custom or test transports, implement the `Transport` interface and
pass it directly to `NewClient` or `Server.Serve`.

## Out of Scope

The following are intentionally not part of go-mms:

- **IEC 61850 abstractions** — data model, SCL, logical nodes, datasets.
  These belong in [go-iec61850](https://github.com/otfabric/go-iec61850).
- **ASN.1 code generation** — BER encoding is hand-written for performance
  and control.
- **Semaphore, Event, and Program Invocation services** — rarely used MMS
  services not required for IEC 61850.
- **GOOSE, Sampled Values, GSE** — non-MMS protocols defined in IEC 61850
  parts 8-1 and 9-2.

## Status

go-mms is **pre-1.0**. The API may change between minor versions.

- **Well-tested**: 630+ unit tests, 42+ integration tests, 200+ negative
  tests, 14 race/concurrency tests, 18 fuzz targets, 20 benchmarks.
- **Hardened**: strict decoder validation, bounds checking on all PDU
  fields, resource limits on negotiated parameters.
- **Interop-verified**: encoding validated against known-good BER patterns
  from a C reference implementation.

## Documentation

- [`_examples/`](_examples/) — runnable client and server examples
- [`COMPLIANCE.md`](COMPLIANCE.md) — service support matrix with test coverage
- [`ERRORS.md`](ERRORS.md) — error taxonomy and handling guide
- [pkg.go.dev](https://pkg.go.dev/github.com/otfabric/go-mms) — API reference

## License

go-mms is proprietary software. See the license terms in your agreement
with OTfabric.
