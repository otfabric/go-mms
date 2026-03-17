// Command server-basic demonstrates a minimal MMS server using go-mms.
//
// This example registers a domain with a readable/writable variable
// and shows how to configure identity and status handlers.
//
// In production, accept connections from a go-tpkt/go-cotp listener
// and call [mms.Server.Serve] for each accepted transport connection,
// or use [mms.Server.ListenAndServe] with a [mms.TransportListener].
//
// Features not shown here but available in the server API:
//   - File provider: implement [mms.FileProvider] and pass to [mms.NewServer]
//   - Information reports: use [mms.ServerConn.SendInformationReport]
//   - Named variable lists: register via [mms.Server.RegisterVariable]
//   - Authentication: provide an [mms.Authenticator] callback for association-level auth
//
// Usage:
//
//	go run .
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"sync"

	mms "github.com/otfabric/go-mms"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	srv := mms.NewServer(mms.ServerOptions{
		Logger: logger,
		MMS: mms.ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
	})

	srv.HandleIdentify(func(_ context.Context, _ mms.IdentifyRequest) (*mms.ServerIdentity, error) {
		return &mms.ServerIdentity{
			Vendor:   "OTfabric",
			Model:    "go-mms example server",
			Revision: "dev",
		}, nil
	})

	srv.HandleStatus(func(_ context.Context, _ mms.StatusRequest) (*mms.ServerStatus, error) {
		return &mms.ServerStatus{
			Logical:  mms.VMDLogicalStatusStateChangesAllowed,
			Physical: mms.VMDPhysicalStatusOperational,
		}, nil
	})

	if err := srv.RegisterDomain("process"); err != nil {
		log.Fatalf("RegisterDomain: %v", err)
	}

	var (
		temperature float64 = 21.5
		mu          sync.Mutex
	)

	if err := srv.RegisterVariable(mms.Variable{
		Name: mms.ObjectName{
			Scope:  mms.ObjectScopeDomain,
			Domain: "process",
			ItemID: "temperature",
		},
		TypeSpec: mms.TypeSpec{Type: mms.ValueTypeFloat, FormatWidth: 32, ExponentWidth: 8},
		Read: func(_ context.Context) (*mms.Value, error) {
			mu.Lock()
			defer mu.Unlock()
			return mms.NewFloat(temperature), nil
		},
		Write: func(_ context.Context, v *mms.Value) error {
			f, ok := v.Float64()
			if !ok {
				return errors.New("expected float value")
			}
			mu.Lock()
			temperature = f
			mu.Unlock()
			logger.Info("temperature updated", "value", f)
			return nil
		},
	}); err != nil {
		log.Fatalf("RegisterVariable: %v", err)
	}

	// In production, you would accept connections from a go-tpkt/go-cotp
	// listener and call srv.Serve(ctx, conn) per accepted connection.
	// This example simply prints the server configuration.
	fmt.Println("MMS server configured with:")
	fmt.Println("  Domain: process")
	fmt.Println("  Variable: process/temperature (float32)")
	fmt.Println("")
	fmt.Println("To serve connections, call srv.Serve(ctx, transport) for each")
	fmt.Println("accepted COTP connection from go-tpkt/go-cotp.")
}
