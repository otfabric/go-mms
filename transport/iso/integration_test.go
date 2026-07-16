// SPDX-License-Identifier: MIT

package iso_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

// testServer creates a standard MMS server configured for integration tests.
func testServer(t *testing.T) *mms.Server {
	t.Helper()
	srv := mms.NewServer(mms.ServerOptions{
		Logger: slog.New(slog.NewTextHandler(newTestWriter(t), &slog.HandlerOptions{Level: slog.LevelDebug})),
		MMS: mms.ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
	})

	srv.HandleIdentify(func(_ context.Context, _ mms.IdentifyRequest) (*mms.ServerIdentity, error) {
		return &mms.ServerIdentity{Vendor: "GoMMS", Model: "TestServer", Revision: "9.0"}, nil
	})
	srv.HandleStatus(func(_ context.Context, _ mms.StatusRequest) (*mms.ServerStatus, error) {
		return &mms.ServerStatus{
			Logical:  mms.VMDLogicalStatusStateChangesAllowed,
			Physical: mms.VMDPhysicalStatusOperational,
		}, nil
	})

	if err := srv.RegisterDomain("testDomain"); err != nil {
		t.Fatal(err)
	}

	temperature := 42.5
	var mu sync.Mutex
	if err := srv.RegisterVariable(mms.Variable{
		Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "testDomain", ItemID: "temperature"},
		TypeSpec: mms.TypeSpec{Type: mms.ValueTypeFloat, FormatWidth: 32, ExponentWidth: 8},
		Read: func(_ context.Context) (*mms.Value, error) {
			mu.Lock()
			defer mu.Unlock()
			return mms.NewFloat(temperature), nil
		},
		Write: func(_ context.Context, v *mms.Value) error {
			f, ok := v.Float64()
			if !ok {
				return &mms.DataAccessError{Code: mms.DataAccessErrorTypeMismatch}
			}
			mu.Lock()
			temperature = f
			mu.Unlock()
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	if err := srv.RegisterVariable(mms.Variable{
		Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "testDomain", ItemID: "counter"},
		TypeSpec: mms.TypeSpec{Type: mms.ValueTypeInteger, Size: 32},
		Read: func(_ context.Context) (*mms.Value, error) {
			return mms.NewInteger(100), nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	return srv
}

func TestTCPIdentify(t *testing.T) {
	srv := testServer(t)
	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go srv.ListenAndServe(ctx, ln)

	client, err := iso.Dial(ctx, ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close(ctx)

	id, err := client.Identify(ctx)
	if err != nil {
		t.Fatalf("identify: %v", err)
	}
	if id.Vendor != "GoMMS" {
		t.Errorf("vendor = %q, want %q", id.Vendor, "GoMMS")
	}
	if id.Model != "TestServer" {
		t.Errorf("model = %q, want %q", id.Model, "TestServer")
	}
}

func TestTCPStatus(t *testing.T) {
	srv := testServer(t)
	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go srv.ListenAndServe(ctx, ln)

	client, err := iso.Dial(ctx, ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close(ctx)

	status, err := client.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Logical != mms.VMDLogicalStatusStateChangesAllowed {
		t.Errorf("logical = %v, want StateChangesAllowed", status.Logical)
	}
	if status.Physical != mms.VMDPhysicalStatusOperational {
		t.Errorf("physical = %v, want Operational", status.Physical)
	}
}

func TestTCPReadWrite(t *testing.T) {
	srv := testServer(t)
	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go srv.ListenAndServe(ctx, ln)

	client, err := iso.Dial(ctx, ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close(ctx)

	rr, err := client.Read(ctx, mms.ReadRequest{
		DomainID: "testDomain",
		ItemID:   "temperature",
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	f, ok := rr.Value.Float64()
	if !ok {
		t.Fatal("expected float value")
	}
	if f != 42.5 {
		t.Errorf("temperature = %v, want 42.5", f)
	}

	_, err = client.Write(ctx, mms.WriteRequest{
		DomainID: "testDomain",
		ItemID:   "temperature",
		Value:    mms.NewFloat(99.9),
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	rr, err = client.Read(ctx, mms.ReadRequest{
		DomainID: "testDomain",
		ItemID:   "temperature",
	})
	if err != nil {
		t.Fatalf("read after write: %v", err)
	}
	f, ok = rr.Value.Float64()
	if !ok {
		t.Fatal("expected float value")
	}
	want := float64(float32(99.9))
	if f < want-0.01 || f > want+0.01 {
		t.Errorf("temperature = %v, want ~%v", f, want)
	}
}

func TestTCPGetNameList(t *testing.T) {
	srv := testServer(t)
	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go srv.ListenAndServe(ctx, ln)

	client, err := iso.Dial(ctx, ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close(ctx)

	result, err := client.GetNameList(ctx, mms.NameListRequest{
		ObjectClass: mms.ObjectClassDomain,
		Scope:       mms.ObjectScopeVMD,
	})
	if err != nil {
		t.Fatalf("getnamelist domains: %v", err)
	}
	if len(result.Names) != 1 || result.Names[0] != "testDomain" {
		t.Errorf("domains = %v, want [testDomain]", result.Names)
	}

	result, err = client.GetNameList(ctx, mms.NameListRequest{
		ObjectClass: mms.ObjectClassNamedVariable,
		Scope:       mms.ObjectScopeDomain,
		DomainID:    "testDomain",
	})
	if err != nil {
		t.Fatalf("getnamelist vars: %v", err)
	}
	if len(result.Names) != 2 {
		t.Errorf("variable count = %d, want 2", len(result.Names))
	}
}

func TestTCPMultipleSequentialServices(t *testing.T) {
	srv := testServer(t)
	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go srv.ListenAndServe(ctx, ln)

	client, err := iso.Dial(ctx, ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close(ctx)

	if _, err := client.Identify(ctx); err != nil {
		t.Fatalf("identify: %v", err)
	}
	if _, err := client.Status(ctx); err != nil {
		t.Fatalf("status: %v", err)
	}
	if _, err := client.GetNameList(ctx, mms.NameListRequest{
		ObjectClass: mms.ObjectClassDomain,
		Scope:       mms.ObjectScopeVMD,
	}); err != nil {
		t.Fatalf("getnamelist: %v", err)
	}
	if _, err := client.Read(ctx, mms.ReadRequest{
		DomainID: "testDomain",
		ItemID:   "counter",
	}); err != nil {
		t.Fatalf("read: %v", err)
	}
}

func TestTCPConcurrentClients(t *testing.T) {
	srv := testServer(t)
	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	go srv.ListenAndServe(ctx, ln)

	const numClients = 3
	var wg sync.WaitGroup
	errs := make(chan error, numClients)

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := iso.Dial(ctx, ln.Addr().String())
			if err != nil {
				errs <- err
				return
			}
			defer c.Close(ctx)

			id, err := c.Identify(ctx)
			if err != nil {
				errs <- err
				return
			}
			if id.Vendor != "GoMMS" {
				errs <- err
				return
			}

			_, err = c.Read(ctx, mms.ReadRequest{
				DomainID: "testDomain",
				ItemID:   "counter",
			})
			if err != nil {
				errs <- err
				return
			}
		}()
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("client error: %v", err)
	}
}

func TestTCPNVLLifecycle(t *testing.T) {
	srv := testServer(t)
	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go srv.ListenAndServe(ctx, ln)

	client, err := iso.Dial(ctx, ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close(ctx)

	err = client.DefineNamedVariableList(ctx, mms.DefineNamedVariableListRequest{
		ListName: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "testDomain", ItemID: "myList"},
		Variables: []mms.VariableSpec{
			{Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "testDomain", ItemID: "temperature"}},
			{Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "testDomain", ItemID: "counter"}},
		},
	})
	if err != nil {
		t.Fatalf("define NVL: %v", err)
	}

	attrs, err := client.GetNamedVariableListAttributes(ctx, mms.ObjectName{
		Scope:  mms.ObjectScopeDomain,
		Domain: "testDomain",
		ItemID: "myList",
	})
	if err != nil {
		t.Fatalf("get NVL attrs: %v", err)
	}
	if len(attrs.Variables) != 2 {
		t.Errorf("variable count = %d, want 2", len(attrs.Variables))
	}

	delResult, err := client.DeleteNamedVariableList(ctx, []mms.ObjectName{
		{Scope: mms.ObjectScopeDomain, Domain: "testDomain", ItemID: "myList"},
	})
	if err != nil {
		t.Fatalf("delete NVL: %v", err)
	}
	if delResult.NumberDeleted != 1 {
		t.Errorf("deleted = %d, want 1", delResult.NumberDeleted)
	}
}

func TestTCPConclude(t *testing.T) {
	srv := testServer(t)
	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go srv.ListenAndServe(ctx, ln)

	client, err := iso.Dial(ctx, ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := client.Identify(ctx); err != nil {
		t.Fatalf("identify: %v", err)
	}

	if err := client.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestTCPWithTSAPSelectors(t *testing.T) {
	srv := testServer(t)
	ln, err := iso.Listen("127.0.0.1:0",
		iso.WithCalledTSelector([]byte{0x00, 0x01}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go srv.ListenAndServe(ctx, ln)

	client, err := iso.Dial(ctx, ln.Addr().String(),
		iso.WithCallingTSelector([]byte{0x00, 0x02}),
		iso.WithCalledTSelector([]byte{0x00, 0x01}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close(ctx)

	id, err := client.Identify(ctx)
	if err != nil {
		t.Fatalf("identify: %v", err)
	}
	if id.Vendor != "GoMMS" {
		t.Errorf("vendor = %q, want GoMMS", id.Vendor)
	}
}

type testWriter struct {
	t    *testing.T
	mu   sync.Mutex
	done bool
}

func newTestWriter(t *testing.T) *testWriter {
	w := &testWriter{t: t}
	t.Cleanup(func() {
		w.mu.Lock()
		w.done = true
		w.mu.Unlock()
	})
	return w
}

func (w *testWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.done {
		w.t.Helper()
		w.t.Log(string(p))
	}
	return len(p), nil
}
