// SPDX-License-Identifier: MIT

// Command loopback demonstrates a fully runnable in-process MMS
// client-server session using channel-based transports.
//
// This is the recommended starting point for understanding go-mms
// since it runs without any external dependencies or network.
// It exercises: Identify, Status, GetNameList, Read, Write.
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
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	srv := mms.NewServer(mms.ServerOptions{
		Logger: logger,
		MMS:    mms.ServerMMSOptions{MaxPDUSize: 65000},
	})

	srv.HandleIdentify(func(_ context.Context, _ mms.IdentifyRequest) (*mms.ServerIdentity, error) {
		return &mms.ServerIdentity{Vendor: "OTfabric", Model: "go-mms loopback", Revision: "dev"}, nil
	})
	srv.HandleStatus(func(_ context.Context, _ mms.StatusRequest) (*mms.ServerStatus, error) {
		return &mms.ServerStatus{
			Logical:  mms.VMDLogicalStatusStateChangesAllowed,
			Physical: mms.VMDPhysicalStatusOperational,
		}, nil
	})

	if err := srv.RegisterDomain("demo"); err != nil {
		log.Fatal(err)
	}

	var (
		temp = 21.5
		mu   sync.Mutex
	)
	if err := srv.RegisterVariable(mms.Variable{
		Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "demo", ItemID: "temperature"},
		TypeSpec: mms.TypeSpec{Type: mms.ValueTypeFloat, FormatWidth: 32, ExponentWidth: 8},
		Read: func(_ context.Context) (*mms.Value, error) {
			mu.Lock()
			defer mu.Unlock()
			return mms.NewFloat(temp), nil
		},
		Write: func(_ context.Context, v *mms.Value) error {
			f, ok := v.Float64()
			if !ok {
				return errors.New("expected float")
			}
			mu.Lock()
			temp = f
			mu.Unlock()
			return nil
		},
	}); err != nil {
		log.Fatal(err)
	}

	clientConn, serverConn := channelPair()
	ctx := context.Background()

	go func() {
		if err := srv.Serve(ctx, serverConn); err != nil {
			logger.Info("server done", "error", err)
		}
	}()

	client, err := mms.NewClient(ctx, clientConn, mms.DialOptions{
		Logger: logger,
		MMS:    mms.MMSOptions{MaxPDUSize: 65000},
	})
	if err != nil {
		log.Fatalf("NewClient: %v", err)
	}
	defer func() {
		if err := client.Close(ctx); err != nil {
			log.Printf("Close: %v", err)
		}
	}()

	ident, err := client.Identify(ctx)
	if err != nil {
		log.Fatalf("Identify: %v", err)
	}
	fmt.Printf("Identify: %s / %s / %s\n", ident.Vendor, ident.Model, ident.Revision)

	status, err := client.Status(ctx)
	if err != nil {
		log.Fatalf("Status: %v", err)
	}
	fmt.Printf("Status: logical=%s physical=%s\n", status.Logical, status.Physical)

	nl, err := client.GetNameList(ctx, mms.NameListRequest{
		ObjectClass: mms.ObjectClassDomain,
		Scope:       mms.ObjectScopeVMD,
	})
	if err != nil {
		log.Fatalf("GetNameList: %v", err)
	}
	fmt.Printf("Domains: %v\n", nl.Names)

	vars, err := client.GetNameListAll(ctx, mms.NameListRequest{
		ObjectClass: mms.ObjectClassNamedVariable,
		Scope:       mms.ObjectScopeDomain,
		DomainID:    "demo",
	})
	if err != nil {
		log.Fatalf("GetNameList (variables): %v", err)
	}
	fmt.Printf("Variables in demo: %v\n", vars)

	rr, err := client.Read(ctx, mms.ReadRequest{DomainID: "demo", ItemID: "temperature"})
	if err != nil {
		log.Fatalf("Read: %v", err)
	}
	f, _ := rr.Value.Float64()
	fmt.Printf("Read temperature: %v\n", f)

	if _, err := client.Write(ctx, mms.WriteRequest{DomainID: "demo", ItemID: "temperature", Value: mms.NewFloat(99.9)}); err != nil {
		log.Fatalf("Write: %v", err)
	}
	fmt.Println("Write temperature: 99.9")

	rr, err = client.Read(ctx, mms.ReadRequest{DomainID: "demo", ItemID: "temperature"})
	if err != nil {
		log.Fatalf("Read: %v", err)
	}
	f, _ = rr.Value.Float64()
	fmt.Printf("Read temperature: %v\n", f)

	fmt.Println("Done.")
}

func channelPair() (mms.Transport, mms.Transport) {
	c2s := make(chan []byte, 16)
	s2c := make(chan []byte, 16)
	return &chanTransport{send: c2s, recv: s2c}, &chanTransport{send: s2c, recv: c2s}
}

type chanTransport struct {
	send chan []byte
	recv chan []byte
	mu   sync.Mutex
	done bool
}

func (t *chanTransport) Send(_ context.Context, data []byte) error {
	t.mu.Lock()
	if t.done {
		t.mu.Unlock()
		return errors.New("transport closed")
	}
	t.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	t.send <- cp
	return nil
}

func (t *chanTransport) Receive(ctx context.Context) ([]byte, error) {
	select {
	case d := <-t.recv:
		if d == nil {
			return nil, errors.New("closed")
		}
		return d, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (t *chanTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.done {
		t.done = true
		close(t.send)
	}
	return nil
}
