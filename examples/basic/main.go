// SPDX-License-Identifier: MIT

// Command basic demonstrates a typical go-mms client session:
// connect to an MMS server, identify it, browse its name list,
// read a variable, write a value, and disconnect.
//
// GetNameListAll handles pagination automatically. For manual control,
// use [mms.Client.GetNameList] with ContinueAfter set to the last name
// from the previous page when MoreFollows is true.
//
// Features not shown here but available in the API:
//   - Alternate access: [mms.Client.ReadComponent], [mms.Client.ReadByIndex], [mms.Client.ReadArrayRange]
//   - Named variable lists: [mms.Client.DefineNamedVariableList], [mms.Client.ReadNamedVariableList]
//   - Information reports: [mms.Client.OnInformationReport]
//   - File services: [mms.Client.FileOpen], [mms.Client.FileRead], [mms.Client.FileClose]
//
// Usage:
//
//	go run . -addr 10.0.0.1:102 -domain MyDomain -var MyVariable
//	go run . -addr 10.0.0.1:102 -domain MyDomain -var MyVariable -write -write-val 42
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:102", "MMS server address (host:port)")
	domain := flag.String("domain", "", "MMS domain to browse")
	varName := flag.String("var", "", "variable name to read/write")
	doWrite := flag.Bool("write", false, "write -write-val to the variable")
	writeVal := flag.Int("write-val", 0, "integer value to write (requires -write)")
	verbose := flag.Bool("v", false, "enable verbose logging")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))
	if *verbose {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Printf("Connecting to %s...\n", *addr)
	client, err := iso.Dial(ctx, *addr,
		iso.WithCalledTSelector([]byte{0x00, 0x01}),
		iso.WithClientDialOptions(mms.DialOptions{
			ISO: mms.ISOOptions{
				LocalAPTitle:  mms.APTitle{1, 1, 1, 1},
				RemoteAPTitle: mms.APTitle{1, 1, 1, 1},
			},
			MMS: mms.MMSOptions{
				MaxPDUSize: 65000,
			},
			Logger: logger,
		}),
	)
	if err != nil {
		log.Fatalf("Dial: %v", err)
	}
	defer func() {
		if err := client.Close(ctx); err != nil {
			log.Printf("Close: %v", err)
		}
	}()

	// Identify
	id, err := client.Identify(ctx)
	if err != nil {
		log.Fatalf("Identify: %v", err)
	}
	fmt.Printf("Server: %s %s (rev %s)\n", id.Vendor, id.Model, id.Revision)

	// Status
	status, err := client.Status(ctx)
	if err != nil {
		log.Fatalf("Status: %v", err)
	}
	fmt.Printf("Status: logical=%s physical=%s\n", status.Logical, status.Physical)

	// Browse names (VMD-level domains or domain-level variables)
	if *domain == "" {
		names, err := client.GetNameListAll(ctx, mms.NameListRequest{
			ObjectClass: mms.ObjectClassDomain,
			Scope:       mms.ObjectScopeVMD,
		})
		if err != nil {
			log.Fatalf("GetNameList (domains): %v", err)
		}
		fmt.Printf("Domains (%d):\n", len(names))
		for _, name := range names {
			fmt.Printf("  %s\n", name)
		}
	} else {
		names, err := client.GetNameListAll(ctx, mms.NameListRequest{
			ObjectClass: mms.ObjectClassNamedVariable,
			Scope:       mms.ObjectScopeDomain,
			DomainID:    mms.DomainID(*domain),
		})
		if err != nil {
			log.Fatalf("GetNameList (variables): %v", err)
		}
		fmt.Printf("Variables in %s (%d):\n", *domain, len(names))
		for _, name := range names {
			fmt.Printf("  %s\n", name)
		}
	}

	if *varName == "" || *domain == "" {
		return
	}

	// Read
	result, err := client.Read(ctx, mms.ReadRequest{
		DomainID: mms.DomainID(*domain),
		ItemID:   mms.ItemID(*varName),
	})
	if err != nil {
		log.Fatalf("Read: %v", err)
	}
	fmt.Printf("Read %s/%s: type=%s value=%v\n", *domain, *varName, result.Value.Type(), formatValue(result.Value))

	// Write (optional)
	if *doWrite {
		_, err := client.Write(ctx, mms.WriteRequest{
			DomainID: mms.DomainID(*domain),
			ItemID:   mms.ItemID(*varName),
			Value:    mms.NewInteger(int64(*writeVal)),
		})
		if err != nil {
			log.Fatalf("Write: %v", err)
		}
		fmt.Printf("Wrote %d to %s/%s\n", *writeVal, *domain, *varName)
	}
}

func formatValue(v *mms.Value) string {
	switch v.Type() {
	case mms.ValueTypeBoolean:
		b, _ := v.Bool()
		return fmt.Sprintf("%v", b)
	case mms.ValueTypeInteger:
		i, _ := v.Int64()
		return fmt.Sprintf("%d", i)
	case mms.ValueTypeUnsigned:
		u, _ := v.Uint64()
		return fmt.Sprintf("%d", u)
	case mms.ValueTypeFloat:
		f, _ := v.Float64()
		return fmt.Sprintf("%g", f)
	case mms.ValueTypeVisibleString:
		s, _ := v.VisibleString()
		return s
	case mms.ValueTypeMmsString:
		s, _ := v.MmsString()
		return s
	default:
		return fmt.Sprintf("(%s)", v.Type())
	}
}
