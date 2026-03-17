package mms

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/codec"
)

// loopbackPair creates a pair of connected transports for in-process testing.
func loopbackPair() (client, server Transport) {
	c2s := make(chan []byte, 16)
	s2c := make(chan []byte, 16)
	cl := &chanTransport{send: c2s, recv: s2c}
	sr := &chanTransport{send: s2c, recv: c2s}
	return cl, sr
}

type chanTransport struct {
	send chan []byte
	recv chan []byte

	mu     sync.Mutex
	closed bool
}

func (t *chanTransport) Send(_ context.Context, data []byte) error {
	t.mu.Lock()
	if t.closed {
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
	case data := <-t.recv:
		if data == nil {
			return nil, errors.New("transport closed")
		}
		return data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (t *chanTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed {
		t.closed = true
		close(t.send)
	}
	return nil
}

// mockTLSTransport wraps chanTransport and implements TLSTransport
// with a fake peer certificate. Used for white-box testing of
// mechanism classification when TLS state is present.
type mockTLSTransport struct {
	chanTransport
}

func (m *mockTLSTransport) TLSConnectionState() *tls.ConnectionState {
	return &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{
			{Subject: pkix.Name{CommonName: "mock-peer"}},
		},
	}
}

func (m *mockTLSTransport) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9999}
}

func testServer(t *testing.T) *Server {
	t.Helper()
	srv := NewServer(ServerOptions{
		Logger: slog.New(slog.NewTextHandler(newTestWriter(t), &slog.HandlerOptions{Level: slog.LevelDebug})),
		MMS: ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
	})

	srv.HandleIdentify(func(_ context.Context, _ IdentifyRequest) (*ServerIdentity, error) {
		return &ServerIdentity{Vendor: "TestVendor", Model: "TestModel", Revision: "1.0"}, nil
	})

	srv.HandleStatus(func(_ context.Context, _ StatusRequest) (*ServerStatus, error) {
		return &ServerStatus{
			Logical:  VMDLogicalStatusStateChangesAllowed,
			Physical: VMDPhysicalStatusOperational,
		}, nil
	})

	if err := srv.RegisterDomain("testDomain"); err != nil {
		t.Fatalf("register domain: %v", err)
	}

	var temperature float64 = 21.5
	var mu sync.Mutex

	if err := srv.RegisterVariable(Variable{
		Name:     ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "temperature"},
		TypeSpec: TypeSpec{Type: ValueTypeFloat, FormatWidth: 32, ExponentWidth: 8},
		Read: func(_ context.Context) (*Value, error) {
			mu.Lock()
			defer mu.Unlock()
			return NewFloat(temperature), nil
		},
		Write: func(_ context.Context, v *Value) error {
			f, ok := v.Float64()
			if !ok {
				return errors.New("expected float")
			}
			mu.Lock()
			temperature = f
			mu.Unlock()
			return nil
		},
	}); err != nil {
		t.Fatalf("register variable: %v", err)
	}

	if err := srv.RegisterVariable(Variable{
		Name:     ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "counter"},
		TypeSpec: TypeSpec{Type: ValueTypeInteger, Size: 32},
		Read: func(_ context.Context) (*Value, error) {
			return NewInteger(42), nil
		},
	}); err != nil {
		t.Fatalf("register variable: %v", err)
	}

	if err := srv.RegisterVariable(Variable{
		Name:     ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "label"},
		TypeSpec: TypeSpec{Type: ValueTypeVisibleString, Size: 64},
		Read: func(_ context.Context) (*Value, error) {
			return NewVisibleString("hello"), nil
		},
	}); err != nil {
		t.Fatalf("register variable: %v", err)
	}

	return srv
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

func (w *testWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.done {
		w.t.Helper()
		w.t.Log(string(p))
	}
	return len(p), nil
}

func connectClientServer(t *testing.T, srv *Server) *Client {
	t.Helper()

	clientConn, serverConn := loopbackPair()
	ctx := context.Background()

	go func() {
		_ = srv.Serve(ctx, serverConn)
	}()

	client, err := NewClient(ctx, clientConn, DialOptions{
		Logger: slog.New(slog.NewTextHandler(newTestWriter(t), &slog.HandlerOptions{Level: slog.LevelDebug})),
		MMS: MMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	return client
}

// waitForConnections polls srv.Connections() until it reaches the expected
// count or the context deadline expires.
func waitForConnections(t *testing.T, srv *Server, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if len(srv.Connections()) == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d connections, have %d", want, len(srv.Connections()))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// --- Integration tests ---

func TestServerIdentify(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ident, err := client.Identify(ctx)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}
	if ident.Vendor != "TestVendor" {
		t.Errorf("Vendor = %q, want %q", ident.Vendor, "TestVendor")
	}
	if ident.Model != "TestModel" {
		t.Errorf("Model = %q, want %q", ident.Model, "TestModel")
	}
	if ident.Revision != "1.0" {
		t.Errorf("Revision = %q, want %q", ident.Revision, "1.0")
	}
}

func TestServerStatus(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	status, err := client.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Logical != VMDLogicalStatusStateChangesAllowed {
		t.Errorf("Logical = %d, want %d", status.Logical, VMDLogicalStatusStateChangesAllowed)
	}
	if status.Physical != VMDPhysicalStatusOperational {
		t.Errorf("Physical = %d, want %d", status.Physical, VMDPhysicalStatusOperational)
	}
}

func TestServerGetNameListDomains(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.GetNameList(ctx, NameListRequest{
		ObjectClass: ObjectClassDomain,
		Scope:       ObjectScopeVMD,
	})
	if err != nil {
		t.Fatalf("GetNameList: %v", err)
	}
	if len(result.Names) != 1 || result.Names[0] != "testDomain" {
		t.Errorf("Names = %v, want [testDomain]", result.Names)
	}
	if result.MoreFollows {
		t.Errorf("MoreFollows = true, want false")
	}
}

func TestServerGetNameListVariables(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.GetNameList(ctx, NameListRequest{
		ObjectClass: ObjectClassNamedVariable,
		Scope:       ObjectScopeDomain,
		DomainID:    "testDomain",
	})
	if err != nil {
		t.Fatalf("GetNameList: %v", err)
	}
	if len(result.Names) != 3 {
		t.Fatalf("Names count = %d, want 3, got %v", len(result.Names), result.Names)
	}
	expected := []string{"counter", "label", "temperature"}
	for i, name := range expected {
		if result.Names[i] != name {
			t.Errorf("Names[%d] = %q, want %q", i, result.Names[i], name)
		}
	}
}

func TestServerGetVariableAccessAttributes(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.GetVariableAccessAttributes(ctx, ObjectName{
		Scope:  ObjectScopeDomain,
		Domain: "testDomain",
		ItemID: "temperature",
	})
	if err != nil {
		t.Fatalf("GetVariableAccessAttributes: %v", err)
	}
	if result.TypeSpec.Type != ValueTypeFloat {
		t.Errorf("TypeSpec.Type = %v, want ValueTypeFloat", result.TypeSpec.Type)
	}
	if result.TypeSpec.FormatWidth != 32 {
		t.Errorf("FormatWidth = %d, want 32", result.TypeSpec.FormatWidth)
	}
}

func TestServerRead(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rr, err := client.Read(ctx, ReadRequest{
		DomainID: "testDomain",
		ItemID:   "temperature",
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	f, ok := rr.Value.Float64()
	if !ok {
		t.Fatal("expected float value")
	}
	if f != 21.5 {
		t.Errorf("value = %f, want 21.5", f)
	}
}

func TestServerReadInteger(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rr, err := client.Read(ctx, ReadRequest{
		DomainID: "testDomain",
		ItemID:   "counter",
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	i, ok := rr.Value.Int64()
	if !ok {
		t.Fatal("expected int value")
	}
	if i != 42 {
		t.Errorf("value = %d, want 42", i)
	}
}

func TestServerWrite(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Write(ctx, WriteRequest{
		DomainID: "testDomain",
		ItemID:   "temperature",
		Value:    NewFloat(99.9),
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	rr, err := client.Read(ctx, ReadRequest{
		DomainID: "testDomain",
		ItemID:   "temperature",
	})
	if err != nil {
		t.Fatalf("Read after write: %v", err)
	}
	f, ok := rr.Value.Float64()
	if !ok {
		t.Fatal("expected float value after write")
	}
	if f != 99.9 {
		t.Errorf("value after write = %f, want 99.9", f)
	}
}

func TestServerConclude(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Identify(ctx)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}

	if err := client.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestServerMultipleSequentialRequests(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ident, err := client.Identify(ctx)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}
	if ident.Vendor != "TestVendor" {
		t.Errorf("Vendor = %q", ident.Vendor)
	}

	status, err := client.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Logical != VMDLogicalStatusStateChangesAllowed {
		t.Errorf("Logical = %d", status.Logical)
	}

	nl, err := client.GetNameList(ctx, NameListRequest{
		ObjectClass: ObjectClassDomain,
		Scope:       ObjectScopeVMD,
	})
	if err != nil {
		t.Fatalf("GetNameList: %v", err)
	}
	if len(nl.Names) == 0 {
		t.Error("no domain names returned")
	}

	rr, err := client.Read(ctx, ReadRequest{DomainID: "testDomain", ItemID: "temperature"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	f, ok := rr.Value.Float64()
	if !ok {
		t.Fatal("expected float")
	}
	if f != 21.5 {
		t.Errorf("temperature = %f", f)
	}

	if _, err := client.Write(ctx, WriteRequest{DomainID: "testDomain", ItemID: "temperature", Value: NewFloat(50.0)}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	rr, err = client.Read(ctx, ReadRequest{DomainID: "testDomain", ItemID: "temperature"})
	if err != nil {
		t.Fatalf("Read after write: %v", err)
	}
	f, ok = rr.Value.Float64()
	if !ok {
		t.Fatal("expected float")
	}
	if f != 50.0 {
		t.Errorf("temperature after write = %f", f)
	}
}

func TestServerConcurrentClients(t *testing.T) {
	srv := testServer(t)

	const numClients = 5
	var wg sync.WaitGroup
	errCh := make(chan error, numClients)

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			client := connectClientServer(t, srv)
			defer client.Close(context.Background())

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			ident, err := client.Identify(ctx)
			if err != nil {
				errCh <- fmt.Errorf("client %d identify: %w", id, err)
				return
			}
			if ident.Vendor != "TestVendor" {
				errCh <- fmt.Errorf("client %d: vendor = %q", id, ident.Vendor)
				return
			}

			rr, err := client.Read(ctx, ReadRequest{DomainID: "testDomain", ItemID: "counter"})
			if err != nil {
				errCh <- fmt.Errorf("client %d read: %w", id, err)
				return
			}
			v, ok := rr.Value.Int64()
			if !ok || v != 42 {
				errCh <- fmt.Errorf("client %d: counter = %v", id, rr.Value)
			}
		}(i)
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}

func TestServerReadNonExistentVariable(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rr, err := client.Read(ctx, ReadRequest{
		DomainID: "testDomain",
		ItemID:   "nonexistent",
	})
	if err != nil {
		t.Logf("Read non-existent returned error (expected): %v", err)
		return
	}
	if rr.Value != nil && rr.Value.Type() == ValueTypeDataAccessError {
		t.Logf("Read non-existent returned data access error (expected): %v", rr.Value)
		return
	}
	t.Fatalf("expected error or data access error value, got: %v", rr.Value)
}

func TestServerWriteReadOnly(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Write(ctx, WriteRequest{
		DomainID: "testDomain",
		ItemID:   "counter",
		Value:    NewInteger(100),
	})
	if err != nil {
		t.Logf("Write to read-only returned error (expected): %v", err)
		return
	}
	t.Fatal("expected error writing to read-only variable")
}

// --- Negative interop tests ---

func TestServerGetNameListUnsupportedObjectClass(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.GetNameList(ctx, NameListRequest{
		ObjectClass: ObjectClassJournal,
		Scope:       ObjectScopeVMD,
	})
	if err == nil {
		t.Fatal("expected error for unsupported journal object class")
	}
	t.Logf("Unsupported ObjectClassJournal: %v", err)
}

func TestServerGetNameListDomainScopeForDomains(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.GetNameList(ctx, NameListRequest{
		ObjectClass: ObjectClassDomain,
		Scope:       ObjectScopeDomain,
		DomainID:    "testDomain",
	})
	if err == nil {
		t.Fatal("expected error for domain-scoped domain list")
	}
	t.Logf("Domain+Domain scope: %v", err)
}

func TestServerReadNonExistentDomain(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rr, err := client.Read(ctx, ReadRequest{
		DomainID: "nosuchDomain",
		ItemID:   "temperature",
	})
	if err != nil {
		t.Logf("Read from non-existent domain returned error (expected): %v", err)
		return
	}
	if rr.Value != nil && rr.Value.Type() == ValueTypeDataAccessError {
		t.Logf("Read non-existent domain returned data access error: %v", rr.Value)
		return
	}
	t.Fatalf("expected error, got: %v", rr.Value)
}

func TestServerGetVarAccessNonExistent(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.GetVariableAccessAttributes(ctx, ObjectName{
		Scope:  ObjectScopeDomain,
		Domain: "testDomain",
		ItemID: "nosuch",
	})
	if err == nil {
		t.Fatal("expected error for non-existent variable")
	}
	t.Logf("GetVarAccess non-existent: %v", err)
}

func TestServerStatusExtendedDerivation(t *testing.T) {
	var gotExtended bool
	srv := NewServer(ServerOptions{
		Logger: slog.New(slog.NewTextHandler(newTestWriter(t), &slog.HandlerOptions{Level: slog.LevelDebug})),
		MMS:    ServerMMSOptions{MaxPDUSize: 65000},
	})
	srv.HandleStatus(func(_ context.Context, req StatusRequest) (*ServerStatus, error) {
		gotExtended = req.ExtendedDerivation
		return &ServerStatus{
			Logical:  VMDLogicalStatusStateChangesAllowed,
			Physical: VMDPhysicalStatusOperational,
		}, nil
	})

	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	// The client's default Status call sends extendedDerivation=false
	if gotExtended {
		t.Error("expected ExtendedDerivation=false")
	}
}

func TestServerRegisterVariableUnsupportedTypeSpec(t *testing.T) {
	srv := NewServer(ServerOptions{MMS: ServerMMSOptions{MaxPDUSize: 65000}})
	if err := srv.RegisterDomain("d"); err != nil {
		t.Fatal(err)
	}

	err := srv.RegisterVariable(Variable{
		Name:     ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "x"},
		TypeSpec: TypeSpec{Type: ValueType(255)},
		Read:     func(_ context.Context) (*Value, error) { return nil, nil },
	})
	if err == nil {
		t.Fatal("expected error for unsupported TypeSpec")
	}
	t.Logf("Register unsupported TypeSpec: %v", err)
}

func TestServerRegisterNamedTypeNilTypeName(t *testing.T) {
	srv := NewServer(ServerOptions{MMS: ServerMMSOptions{MaxPDUSize: 65000}})
	if err := srv.RegisterDomain("d"); err != nil {
		t.Fatal(err)
	}

	err := srv.RegisterVariable(Variable{
		Name:     ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "x"},
		TypeSpec: TypeSpec{Type: ValueTypeNamedType, TypeName: nil},
		Read:     func(_ context.Context) (*Value, error) { return nil, nil },
	})
	if err == nil {
		t.Fatal("expected error for NamedType with nil TypeName")
	}
	t.Logf("Register NamedType nil: %v", err)
}

func TestServerNoIdentifyHandler(t *testing.T) {
	srv := NewServer(ServerOptions{
		Logger: slog.New(slog.NewTextHandler(newTestWriter(t), &slog.HandlerOptions{Level: slog.LevelDebug})),
		MMS:    ServerMMSOptions{MaxPDUSize: 65000},
	})

	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Identify(ctx)
	if err == nil {
		t.Fatal("expected error when no Identify handler is registered")
	}
	t.Logf("No Identify handler: %v", err)
}

func TestServerNoStatusHandler(t *testing.T) {
	srv := NewServer(ServerOptions{
		Logger: slog.New(slog.NewTextHandler(newTestWriter(t), &slog.HandlerOptions{Level: slog.LevelDebug})),
		MMS:    ServerMMSOptions{MaxPDUSize: 65000},
	})

	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Status(ctx)
	if err == nil {
		t.Fatal("expected error when no Status handler is registered")
	}
	t.Logf("No Status handler: %v", err)
}

// --- Phase 8: Named Variable List tests ---

func TestServerDefineAndGetNVLAttributes(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	listName := ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "myList"}
	vars := []VariableSpec{
		{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "temperature"}},
		{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "counter"}},
	}

	err := client.DefineNamedVariableList(ctx, DefineNamedVariableListRequest{
		ListName:  listName,
		Variables: vars,
	})
	if err != nil {
		t.Fatalf("DefineNamedVariableList: %v", err)
	}

	attrs, err := client.GetNamedVariableListAttributes(ctx, listName)
	if err != nil {
		t.Fatalf("GetNamedVariableListAttributes: %v", err)
	}
	if !attrs.Deletable {
		t.Error("expected Deletable=true")
	}
	if len(attrs.Variables) != 2 {
		t.Fatalf("Variables count = %d, want 2", len(attrs.Variables))
	}
	if attrs.Variables[0].Name.ItemID != "temperature" {
		t.Errorf("Variables[0].Name.ItemID = %q, want %q", attrs.Variables[0].Name.ItemID, "temperature")
	}
	if attrs.Variables[1].Name.ItemID != "counter" {
		t.Errorf("Variables[1].Name.ItemID = %q, want %q", attrs.Variables[1].Name.ItemID, "counter")
	}
}

func TestServerDeleteNVL(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	listName := ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "deleteMe"}
	err := client.DefineNamedVariableList(ctx, DefineNamedVariableListRequest{
		ListName: listName,
		Variables: []VariableSpec{
			{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "temperature"}},
		},
	})
	if err != nil {
		t.Fatalf("DefineNamedVariableList: %v", err)
	}

	result, err := client.DeleteNamedVariableList(ctx, []ObjectName{listName})
	if err != nil {
		t.Fatalf("DeleteNamedVariableList: %v", err)
	}
	if result.NumberMatched != 1 {
		t.Errorf("NumberMatched = %d, want 1", result.NumberMatched)
	}
	if result.NumberDeleted != 1 {
		t.Errorf("NumberDeleted = %d, want 1", result.NumberDeleted)
	}

	_, err = client.GetNamedVariableListAttributes(ctx, listName)
	if err == nil {
		t.Fatal("expected error getting deleted NVL")
	}
	t.Logf("Get deleted NVL: %v", err)
}

func TestServerGetNameListNVL(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.DefineNamedVariableList(ctx, DefineNamedVariableListRequest{
		ListName: ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "list1"},
		Variables: []VariableSpec{
			{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "temperature"}},
		},
	})
	if err != nil {
		t.Fatalf("Define list1: %v", err)
	}

	err = client.DefineNamedVariableList(ctx, DefineNamedVariableListRequest{
		ListName: ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "list2"},
		Variables: []VariableSpec{
			{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "counter"}},
		},
	})
	if err != nil {
		t.Fatalf("Define list2: %v", err)
	}

	nl, err := client.GetNameList(ctx, NameListRequest{
		ObjectClass: ObjectClassNamedVariableList,
		Scope:       ObjectScopeDomain,
		DomainID:    "testDomain",
	})
	if err != nil {
		t.Fatalf("GetNameList NVL: %v", err)
	}
	if len(nl.Names) != 2 {
		t.Fatalf("Names = %v, want 2 entries", nl.Names)
	}
	if nl.Names[0] != "list1" || nl.Names[1] != "list2" {
		t.Errorf("Names = %v, want [list1 list2]", nl.Names)
	}
}

func TestServerDefineNVLDuplicate(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	listName := ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "dupList"}
	defineReq := DefineNamedVariableListRequest{
		ListName: listName,
		Variables: []VariableSpec{
			{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "temperature"}},
		},
	}

	if err := client.DefineNamedVariableList(ctx, defineReq); err != nil {
		t.Fatalf("First define: %v", err)
	}

	err := client.DefineNamedVariableList(ctx, defineReq)
	if err == nil {
		t.Fatal("expected error for duplicate NVL define")
	}
	t.Logf("Duplicate define: %v", err)
}

func TestServerDeleteNonExistentNVL(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.DeleteNamedVariableList(ctx, []ObjectName{
		{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "noSuchList"},
	})
	if err != nil {
		t.Fatalf("DeleteNamedVariableList: %v", err)
	}
	if result.NumberMatched != 0 {
		t.Errorf("NumberMatched = %d, want 0", result.NumberMatched)
	}
	if result.NumberDeleted != 0 {
		t.Errorf("NumberDeleted = %d, want 0", result.NumberDeleted)
	}
}

func TestServerNVLFullLifecycle(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	listName := ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "lifecycle"}
	vars := []VariableSpec{
		{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "temperature"}},
		{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "counter"}},
		{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "label"}},
	}

	// Define
	err := client.DefineNamedVariableList(ctx, DefineNamedVariableListRequest{
		ListName:  listName,
		Variables: vars,
	})
	if err != nil {
		t.Fatalf("Define: %v", err)
	}

	// List
	nl, err := client.GetNameList(ctx, NameListRequest{
		ObjectClass: ObjectClassNamedVariableList,
		Scope:       ObjectScopeDomain,
		DomainID:    "testDomain",
	})
	if err != nil {
		t.Fatalf("GetNameList: %v", err)
	}
	found := false
	for _, n := range nl.Names {
		if n == "lifecycle" {
			found = true
		}
	}
	if !found {
		t.Errorf("NVL 'lifecycle' not in name list: %v", nl.Names)
	}

	// GetAttributes
	attrs, err := client.GetNamedVariableListAttributes(ctx, listName)
	if err != nil {
		t.Fatalf("GetAttrs: %v", err)
	}
	if len(attrs.Variables) != 3 {
		t.Errorf("Variables count = %d, want 3", len(attrs.Variables))
	}

	// Delete
	delResult, err := client.DeleteNamedVariableList(ctx, []ObjectName{listName})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if delResult.NumberDeleted != 1 {
		t.Errorf("NumberDeleted = %d, want 1", delResult.NumberDeleted)
	}

	// Verify gone
	_, err = client.GetNamedVariableListAttributes(ctx, listName)
	if err == nil {
		t.Fatal("expected error after delete")
	}

	// Verify gone from name list
	nl, err = client.GetNameList(ctx, NameListRequest{
		ObjectClass: ObjectClassNamedVariableList,
		Scope:       ObjectScopeDomain,
		DomainID:    "testDomain",
	})
	if err != nil {
		t.Fatalf("GetNameList after delete: %v", err)
	}
	for _, n := range nl.Names {
		if n == "lifecycle" {
			t.Error("NVL 'lifecycle' still in name list after delete")
		}
	}
}

// --- InformationReport tests ---

func TestServerInformationReport(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	received := make(chan *InformationReportIndication, 1)
	client.OnInformationReport(func(r *InformationReportIndication) {
		received <- r
	})

	waitForConnections(t, srv, 1, 2*time.Second)

	conns := srv.Connections()
	err := conns[0].SendInformationReport(ctx, &InformationReportRequest{
		Variables: []ObjectName{
			{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "temperature"},
		},
		Values: []*Value{NewFloat(42.5)},
	})
	if err != nil {
		t.Fatalf("SendInformationReport: %v", err)
	}

	select {
	case report := <-received:
		if report.ListName != nil {
			t.Error("expected nil ListName for list-of-variable style")
		}
		if len(report.Variables) != 1 {
			t.Fatalf("expected 1 variable, got %d", len(report.Variables))
		}
		if string(report.Variables[0].ItemID) != "temperature" {
			t.Errorf("Variable ItemID = %q, want temperature", report.Variables[0].ItemID)
		}
		if len(report.Values) != 1 {
			t.Fatalf("expected 1 value, got %d", len(report.Values))
		}
		f, ok := report.Values[0].Float64()
		if !ok {
			t.Fatal("expected float value")
		}
		if f < 42.4 || f > 42.6 {
			t.Errorf("value = %f, want ~42.5", f)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for InformationReport")
	}

	client.Close(ctx)
}

func TestServerInformationReportNamedList(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	received := make(chan *InformationReportIndication, 1)
	client.OnInformationReport(func(r *InformationReportIndication) {
		received <- r
	})

	waitForConnections(t, srv, 1, 2*time.Second)
	conns := srv.Connections()

	listName := ObjectName{Scope: ObjectScopeVMD, ItemID: "myReport"}
	err := conns[0].SendInformationReport(ctx, &InformationReportRequest{
		ListName: &listName,
		Values:   []*Value{NewInteger(100), NewVisibleString("hello")},
	})
	if err != nil {
		t.Fatalf("SendInformationReport: %v", err)
	}

	select {
	case report := <-received:
		if report.ListName == nil {
			t.Fatal("expected non-nil ListName")
		}
		if string(report.ListName.ItemID) != "myReport" {
			t.Errorf("ListName ItemID = %q, want myReport", report.ListName.ItemID)
		}
		if len(report.Values) != 2 {
			t.Fatalf("expected 2 values, got %d", len(report.Values))
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for InformationReport")
	}

	client.Close(ctx)
}

func TestServerBroadcast(t *testing.T) {
	srv := testServer(t)

	clientConn1, serverConn1 := loopbackPair()
	clientConn2, serverConn2 := loopbackPair()
	ctx := context.Background()

	go func() { _ = srv.Serve(ctx, serverConn1) }()
	go func() { _ = srv.Serve(ctx, serverConn2) }()

	opts := DialOptions{
		Logger: slog.New(slog.NewTextHandler(newTestWriter(t), &slog.HandlerOptions{Level: slog.LevelDebug})),
		MMS: MMSOptions{
			MaxPDUSize:            65000,
			MaxOutstandingCalling: 5,
			MaxOutstandingCalled:  5,
		},
	}

	client1, err := NewClient(ctx, clientConn1, opts)
	if err != nil {
		t.Fatalf("NewClient 1: %v", err)
	}
	client2, err := NewClient(ctx, clientConn2, opts)
	if err != nil {
		t.Fatalf("NewClient 2: %v", err)
	}

	received1 := make(chan *InformationReportIndication, 1)
	received2 := make(chan *InformationReportIndication, 1)
	client1.OnInformationReport(func(r *InformationReportIndication) { received1 <- r })
	client2.OnInformationReport(func(r *InformationReportIndication) { received2 <- r })

	tctx, tcancel := context.WithTimeout(ctx, 5*time.Second)
	defer tcancel()

	waitForConnections(t, srv, 2, 2*time.Second)

	err = srv.Broadcast(tctx, &InformationReportRequest{
		Variables: []ObjectName{
			{Scope: ObjectScopeVMD, ItemID: "event"},
		},
		Values: []*Value{NewBoolean(true)},
	})
	if err != nil {
		t.Fatalf("Broadcast: %v", err)
	}

	for i, ch := range []chan *InformationReportIndication{received1, received2} {
		select {
		case r := <-ch:
			if len(r.Values) != 1 {
				t.Errorf("client %d: expected 1 value, got %d", i+1, len(r.Values))
			}
		case <-tctx.Done():
			t.Fatalf("client %d: timeout waiting for broadcast", i+1)
		}
	}

	client1.Close(tctx)
	client2.Close(tctx)
}

func TestInfoReportConcurrentWithConfirmed(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	received := make(chan *InformationReportIndication, 10)
	client.OnInformationReport(func(r *InformationReportIndication) {
		received <- r
	})

	waitForConnections(t, srv, 1, 2*time.Second)
	sc := srv.Connections()[0]

	var wg sync.WaitGroup

	// Send InformationReports from server in a goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			_ = sc.SendInformationReport(ctx, &InformationReportRequest{
				Variables: []ObjectName{
					{Scope: ObjectScopeVMD, ItemID: "event"},
				},
				Values: []*Value{NewInteger(int64(i))},
			})
		}
	}()

	// Run confirmed service calls concurrently.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			ident, err := client.Identify(ctx)
			if err != nil {
				t.Errorf("Identify %d: %v", i, err)
				return
			}
			if ident.Vendor != "TestVendor" {
				t.Errorf("Identify %d: vendor = %q", i, ident.Vendor)
			}
		}
	}()

	wg.Wait()

	reportCount := 0
	deadline := time.After(2 * time.Second)
	for reportCount < 5 {
		select {
		case <-received:
			reportCount++
		case <-deadline:
			goto done
		}
	}
done:
	if reportCount != 5 {
		t.Errorf("received %d reports, want 5", reportCount)
	}

	client.Close(ctx)
}

func TestInfoReportNoHandler(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	waitForConnections(t, srv, 1, 2*time.Second)

	err := srv.Connections()[0].SendInformationReport(ctx, &InformationReportRequest{
		Variables: []ObjectName{
			{Scope: ObjectScopeVMD, ItemID: "ignored"},
		},
		Values: []*Value{NewBoolean(false)},
	})
	if err != nil {
		t.Fatalf("SendInformationReport: %v", err)
	}

	time.Sleep(20 * time.Millisecond) // allow unhandled report to be processed
	ident, err := client.Identify(ctx)
	if err != nil {
		t.Fatalf("Identify after unhandled report: %v", err)
	}
	if ident.Vendor != "TestVendor" {
		t.Errorf("vendor = %q, want TestVendor", ident.Vendor)
	}

	client.Close(ctx)
}

func TestServerConnRemovedAfterClose(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	waitForConnections(t, srv, 1, 2*time.Second)

	client.Close(ctx)

	waitForConnections(t, srv, 0, 2*time.Second)
}

func TestInfoReportHandlerPanicDoesNotKillClient(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	panicked := make(chan struct{}, 1)
	client.OnInformationReport(func(_ *InformationReportIndication) {
		panicked <- struct{}{}
		panic("test panic in handler")
	})

	waitForConnections(t, srv, 1, 2*time.Second)
	sc := srv.Connections()[0]

	err := sc.SendInformationReport(ctx, &InformationReportRequest{
		Variables: []ObjectName{{Scope: ObjectScopeVMD, ItemID: "x"}},
		Values:    []*Value{NewInteger(1)},
	})
	if err != nil {
		t.Fatalf("SendInformationReport: %v", err)
	}

	select {
	case <-panicked:
	case <-ctx.Done():
		t.Fatal("timeout waiting for panic")
	}

	ident, err := client.Identify(ctx)
	if err != nil {
		t.Fatalf("Identify after panic: %v", err)
	}
	if ident.Vendor != "TestVendor" {
		t.Errorf("vendor = %q, want TestVendor", ident.Vendor)
	}

	client.Close(ctx)
}

func TestInfoReportRequestValidation(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	waitForConnections(t, srv, 1, 2*time.Second)
	sc := srv.Connections()[0]

	tests := []struct {
		name string
		req  *InformationReportRequest
	}{
		{"nil request", nil},
		{"both ListName and Variables", &InformationReportRequest{
			ListName:  &ObjectName{Scope: ObjectScopeVMD, ItemID: "list"},
			Variables: []ObjectName{{Scope: ObjectScopeVMD, ItemID: "x"}},
			Values:    []*Value{NewInteger(1)},
		}},
		{"neither ListName nor Variables", &InformationReportRequest{
			Values: []*Value{NewInteger(1)},
		}},
		{"Variables/Values length mismatch", &InformationReportRequest{
			Variables: []ObjectName{{Scope: ObjectScopeVMD, ItemID: "x"}},
			Values:    []*Value{NewInteger(1), NewInteger(2)},
		}},
		{"empty Values with ListName", &InformationReportRequest{
			ListName: &ObjectName{Scope: ObjectScopeVMD, ItemID: "list"},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sc.SendInformationReport(ctx, tt.req)
			if err == nil {
				t.Fatal("expected validation error")
			}
			t.Logf("validation error: %v", err)
		})
	}

	client.Close(ctx)
}

func TestServerConnSendAfterClose(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	waitForConnections(t, srv, 1, 2*time.Second)
	sc := srv.Connections()[0]

	client.Close(ctx)
	waitForConnections(t, srv, 0, 2*time.Second)

	err := sc.SendInformationReport(ctx, &InformationReportRequest{
		Variables: []ObjectName{{Scope: ObjectScopeVMD, ItemID: "x"}},
		Values:    []*Value{NewInteger(1)},
	})
	if !errors.Is(err, ErrServerConnClosed) {
		t.Fatalf("expected ErrServerConnClosed, got: %v", err)
	}
}

func TestAuthenticatorPasswordAccept(t *testing.T) {
	var gotAuth *AuthContext
	srv := NewServer(ServerOptions{
		Logger: slog.New(slog.NewTextHandler(newTestWriter(t), &slog.HandlerOptions{Level: slog.LevelDebug})),
		MMS: ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
		Authenticate: func(_ context.Context, auth *AuthContext) (AuthResult, error) {
			gotAuth = auth
			if auth.Mechanism != AuthMechanismACSEPassword {
				return AuthResult{}, fmt.Errorf("expected password auth")
			}
			if string(auth.Password) != "secret" {
				return AuthResult{}, fmt.Errorf("wrong password")
			}
			return AuthResult{Accept: true, Token: "operator-1"}, nil
		},
	})

	srv.HandleIdentify(func(_ context.Context, _ IdentifyRequest) (*ServerIdentity, error) {
		return &ServerIdentity{Vendor: "Auth", Model: "Test", Revision: "1.0"}, nil
	})

	clientConn, serverConn := loopbackPair()
	ctx := context.Background()

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve(ctx, serverConn)
	}()

	client, err := NewClient(ctx, clientConn, DialOptions{
		Logger: slog.New(slog.NewTextHandler(newTestWriter(t), &slog.HandlerOptions{Level: slog.LevelDebug})),
		ISO:    ISOOptions{Password: []byte("secret")},
		MMS: MMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	id, err := client.Identify(ctx)
	if err != nil {
		t.Fatalf("identify: %v", err)
	}
	if id.Vendor != "Auth" {
		t.Errorf("vendor = %q, want Auth", id.Vendor)
	}

	if gotAuth.Mechanism != AuthMechanismACSEPassword {
		t.Errorf("mechanism = %v, want AuthMechanismACSEPassword", gotAuth.Mechanism)
	}
	if string(gotAuth.Password) != "secret" {
		t.Errorf("password = %q, want secret", string(gotAuth.Password))
	}

	waitForConnections(t, srv, 1, 2*time.Second)
	sc := srv.Connections()[0]
	if sc.AuthToken() != "operator-1" {
		t.Errorf("auth token = %v, want operator-1", sc.AuthToken())
	}

	client.Close(ctx)
}

func TestAuthenticatorRejectsConnection(t *testing.T) {
	srv := NewServer(ServerOptions{
		Logger: slog.New(slog.NewTextHandler(newTestWriter(t), &slog.HandlerOptions{Level: slog.LevelDebug})),
		MMS: ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
		Authenticate: func(_ context.Context, _ *AuthContext) (AuthResult, error) {
			return AuthResult{Accept: false}, nil
		},
	})

	clientConn, serverConn := loopbackPair()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve(ctx, serverConn)
	}()

	_, err := NewClient(ctx, clientConn, DialOptions{
		Logger: slog.New(slog.NewTextHandler(newTestWriter(t), &slog.HandlerOptions{Level: slog.LevelDebug})),
		MMS: MMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
	})
	if err == nil {
		t.Fatal("expected error when auth rejects")
	}
	t.Logf("got expected client error: %v", err)

	select {
	case sErr := <-serveErr:
		if sErr == nil {
			t.Fatal("expected serve to return error")
		}
		var authErr *AuthenticationError
		if !errors.As(sErr, &authErr) {
			t.Fatalf("expected AuthenticationError, got %T: %v", sErr, sErr)
		}
		if !errors.Is(sErr, ErrAuthenticationFailed) {
			t.Fatalf("expected ErrAuthenticationFailed sentinel, got: %v", sErr)
		}
		t.Logf("serve returned typed AuthenticationError: %v", sErr)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for serve to return")
	}
}

func TestAuthenticatorNoAuthAcceptsAll(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	id, err := client.Identify(ctx)
	if err != nil {
		t.Fatalf("identify: %v", err)
	}
	if id.Vendor != "TestVendor" {
		t.Errorf("vendor = %q, want TestVendor", id.Vendor)
	}

	client.Close(ctx)
}

func TestAuthenticatorNoneNilFields(t *testing.T) {
	var gotAuth *AuthContext
	srv := NewServer(ServerOptions{
		Logger: slog.New(slog.NewTextHandler(newTestWriter(t), &slog.HandlerOptions{Level: slog.LevelDebug})),
		MMS:    ServerMMSOptions{MaxPDUSize: 65000},
		Authenticate: func(_ context.Context, auth *AuthContext) (AuthResult, error) {
			gotAuth = auth
			return AuthResult{Accept: true}, nil
		},
	})
	srv.HandleIdentify(func(_ context.Context, _ IdentifyRequest) (*ServerIdentity, error) {
		return &ServerIdentity{Vendor: "V", Model: "M", Revision: "1"}, nil
	})

	client := connectClientServer(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := client.Identify(ctx); err != nil {
		t.Fatal(err)
	}

	if gotAuth.Mechanism != AuthMechanismNone {
		t.Errorf("mechanism = %v, want AuthMechanismNone", gotAuth.Mechanism)
	}
	if gotAuth.Password != nil {
		t.Errorf("password should be nil, got %q", gotAuth.Password)
	}
	if gotAuth.CallingApplication != nil {
		t.Errorf("CallingApplication should be nil, got %+v", gotAuth.CallingApplication)
	}
	if gotAuth.PeerCertificate != nil {
		t.Error("PeerCertificate should be nil for non-TLS")
	}
	if gotAuth.MechanismOID != nil {
		t.Errorf("MechanismOID should be nil, got %v", gotAuth.MechanismOID)
	}

	client.Close(ctx)
}

func TestAuthenticatorPasswordWithAPTitle(t *testing.T) {
	var gotAuth *AuthContext
	srv := NewServer(ServerOptions{
		Logger: slog.New(slog.NewTextHandler(newTestWriter(t), &slog.HandlerOptions{Level: slog.LevelDebug})),
		MMS:    ServerMMSOptions{MaxPDUSize: 65000},
		Authenticate: func(_ context.Context, auth *AuthContext) (AuthResult, error) {
			gotAuth = auth
			if auth.Mechanism != AuthMechanismACSEPassword {
				return AuthResult{}, fmt.Errorf("wrong mechanism")
			}
			return AuthResult{Accept: true, Token: "session-42"}, nil
		},
	})
	srv.HandleIdentify(func(_ context.Context, _ IdentifyRequest) (*ServerIdentity, error) {
		return &ServerIdentity{Vendor: "V"}, nil
	})

	clientConn, serverConn := loopbackPair()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() { _ = srv.Serve(ctx, serverConn) }()

	client, err := NewClient(ctx, clientConn, DialOptions{
		Logger: slog.New(slog.NewTextHandler(newTestWriter(t), &slog.HandlerOptions{Level: slog.LevelDebug})),
		ISO: ISOOptions{
			Password:         []byte("pass123"),
			LocalAPTitle:     APTitle{1, 3, 9999, 13, 1},
			LocalAEQualifier: 7,
		},
		MMS: MMSOptions{MaxPDUSize: 65000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Identify(ctx); err != nil {
		t.Fatal(err)
	}

	if gotAuth.Mechanism != AuthMechanismACSEPassword {
		t.Errorf("mechanism = %v", gotAuth.Mechanism)
	}
	if string(gotAuth.Password) != "pass123" {
		t.Errorf("password = %q", gotAuth.Password)
	}
	wantOID := APTitle{2, 2, 3, 1}
	if !gotAuth.MechanismOID.Equal(wantOID) {
		t.Errorf("MechanismOID = %v, want %v", gotAuth.MechanismOID, wantOID)
	}
	if gotAuth.CallingApplication == nil {
		t.Fatal("CallingApplication should not be nil")
	}
	wantAPTitle := APTitle{1, 3, 9999, 13, 1}
	if !gotAuth.CallingApplication.APTitle.Equal(wantAPTitle) {
		t.Errorf("APTitle = %v, want %v", gotAuth.CallingApplication.APTitle, wantAPTitle)
	}
	if gotAuth.CallingApplication.AEQualifier == nil || *gotAuth.CallingApplication.AEQualifier != 7 {
		t.Errorf("AEQualifier = %v, want 7", gotAuth.CallingApplication.AEQualifier)
	}

	waitForConnections(t, srv, 1, 2*time.Second)
	sc := srv.Connections()[0]
	if sc.AuthToken() != "session-42" {
		t.Errorf("AuthToken = %v, want session-42", sc.AuthToken())
	}

	client.Close(ctx)
}

func TestAuthenticatorRejectsViaError(t *testing.T) {
	srv := NewServer(ServerOptions{
		Logger: slog.New(slog.NewTextHandler(newTestWriter(t), &slog.HandlerOptions{Level: slog.LevelDebug})),
		MMS:    ServerMMSOptions{MaxPDUSize: 65000},
		Authenticate: func(_ context.Context, _ *AuthContext) (AuthResult, error) {
			return AuthResult{}, fmt.Errorf("backend lookup failed")
		},
	})

	clientConn, serverConn := loopbackPair()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ctx, serverConn) }()

	_, err := NewClient(ctx, clientConn, DialOptions{
		Logger: slog.New(slog.NewTextHandler(newTestWriter(t), &slog.HandlerOptions{Level: slog.LevelDebug})),
		MMS:    MMSOptions{MaxPDUSize: 65000},
	})
	if err == nil {
		t.Fatal("expected error")
	}

	select {
	case sErr := <-serveErr:
		var authErr *AuthenticationError
		if !errors.As(sErr, &authErr) {
			t.Fatalf("expected AuthenticationError, got %T: %v", sErr, sErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestAuthenticatorTokenNilWhenNoAuthenticator(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	waitForConnections(t, srv, 1, 2*time.Second)
	sc := srv.Connections()[0]
	if sc.AuthToken() != nil {
		t.Errorf("AuthToken should be nil, got %v", sc.AuthToken())
	}

	client.Close(ctx)
}

func TestAuthContextDefensiveCopyPassword(t *testing.T) {
	pw := []byte("original")
	var gotAuth *AuthContext
	srv := NewServer(ServerOptions{
		Logger: slog.New(slog.NewTextHandler(newTestWriter(t), &slog.HandlerOptions{Level: slog.LevelDebug})),
		MMS:    ServerMMSOptions{MaxPDUSize: 65000},
		Authenticate: func(_ context.Context, auth *AuthContext) (AuthResult, error) {
			gotAuth = auth
			return AuthResult{Accept: true}, nil
		},
	})
	srv.HandleIdentify(func(_ context.Context, _ IdentifyRequest) (*ServerIdentity, error) {
		return &ServerIdentity{Vendor: "V"}, nil
	})

	clientConn, serverConn := loopbackPair()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { _ = srv.Serve(ctx, serverConn) }()

	client, err := NewClient(ctx, clientConn, DialOptions{
		Logger: slog.New(slog.NewTextHandler(newTestWriter(t), &slog.HandlerOptions{Level: slog.LevelDebug})),
		ISO:    ISOOptions{Password: pw},
		MMS:    MMSOptions{MaxPDUSize: 65000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Identify(ctx); err != nil {
		t.Fatal(err)
	}

	// Mutate the original password slice after connection
	pw[0] = 'X'

	if string(gotAuth.Password) != "original" {
		t.Errorf("AuthContext.Password mutated: got %q, want %q", gotAuth.Password, "original")
	}
	client.Close(ctx)
}

func TestAuthContextDefensiveCopyAPTitle(t *testing.T) {
	apTitle := APTitle{1, 3, 9999, 13, 1}
	var gotAuth *AuthContext
	srv := NewServer(ServerOptions{
		Logger: slog.New(slog.NewTextHandler(newTestWriter(t), &slog.HandlerOptions{Level: slog.LevelDebug})),
		MMS:    ServerMMSOptions{MaxPDUSize: 65000},
		Authenticate: func(_ context.Context, auth *AuthContext) (AuthResult, error) {
			gotAuth = auth
			return AuthResult{Accept: true}, nil
		},
	})
	srv.HandleIdentify(func(_ context.Context, _ IdentifyRequest) (*ServerIdentity, error) {
		return &ServerIdentity{Vendor: "V"}, nil
	})

	clientConn, serverConn := loopbackPair()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { _ = srv.Serve(ctx, serverConn) }()

	client, err := NewClient(ctx, clientConn, DialOptions{
		Logger: slog.New(slog.NewTextHandler(newTestWriter(t), &slog.HandlerOptions{Level: slog.LevelDebug})),
		ISO: ISOOptions{
			LocalAPTitle:     apTitle,
			LocalAEQualifier: 7,
		},
		MMS: MMSOptions{MaxPDUSize: 65000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Identify(ctx); err != nil {
		t.Fatal(err)
	}

	if gotAuth.CallingApplication == nil {
		t.Fatal("CallingApplication is nil")
	}

	// Mutate the auth context's APTitle — should not affect the original
	gotAuth.CallingApplication.APTitle[0] = 999

	if apTitle[0] != 1 {
		t.Errorf("source APTitle mutated: got %v, want [1 3 9999 13 1]", apTitle)
	}

	// Verify AEQualifier is an independent copy
	if gotAuth.CallingApplication.AEQualifier == nil {
		t.Fatal("AEQualifier is nil")
	}
	original := *gotAuth.CallingApplication.AEQualifier
	*gotAuth.CallingApplication.AEQualifier = 999
	// Re-read — since it's a copy the original int on the server side is independent.
	// The important property is the *int in AuthContext is not shared with
	// the internal acse.AuthInfo. We verify the value was correctly set.
	if original != 7 {
		t.Errorf("AEQualifier = %d, want 7", original)
	}

	client.Close(ctx)
}

func TestAuthRemoteAddrOnPlaintextISO(t *testing.T) {
	var gotAuth *AuthContext
	srv := NewServer(ServerOptions{
		Logger: slog.New(slog.NewTextHandler(newTestWriter(t), &slog.HandlerOptions{Level: slog.LevelDebug})),
		MMS:    ServerMMSOptions{MaxPDUSize: 65000},
		Authenticate: func(_ context.Context, auth *AuthContext) (AuthResult, error) {
			gotAuth = auth
			return AuthResult{Accept: true}, nil
		},
	})
	srv.HandleIdentify(func(_ context.Context, _ IdentifyRequest) (*ServerIdentity, error) {
		return &ServerIdentity{Vendor: "V"}, nil
	})

	// chanTransport does not implement RemoteAddrTransport, so
	// AuthContext.RemoteAddr should be nil. This verifies the
	// nil-safety path.
	client := connectClientServer(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.Identify(ctx); err != nil {
		t.Fatal(err)
	}

	if gotAuth.RemoteAddr != nil {
		t.Errorf("RemoteAddr should be nil for chanTransport, got %v", gotAuth.RemoteAddr)
	}
	client.Close(ctx)
}

func TestBuildAuthContextUnknownACSEPlusTLS(t *testing.T) {
	// Verify that when an unknown ACSE mechanism is present AND TLS
	// peer certificates exist, the mechanism is AuthMechanismUnknown
	// (ACSE takes priority) but PeerCertificate is still populated.
	srv := NewServer(ServerOptions{MMS: ServerMMSOptions{MaxPDUSize: 65000}})

	mockConn := &mockTLSTransport{}
	ai := acse.AuthInfo{
		Mechanism:    acse.AuthUnknown,
		MechanismOID: APTitle{2, 99, 1}, // fictitious OID
	}

	ac := srv.BuildAuthContextForTest(mockConn, ai)

	if ac.Mechanism != AuthMechanismUnknown {
		t.Errorf("Mechanism = %v, want AuthMechanismUnknown", ac.Mechanism)
	}
	wantOID := APTitle{2, 99, 1}
	if !ac.MechanismOID.Equal(wantOID) {
		t.Errorf("MechanismOID = %v, want %v", ac.MechanismOID, wantOID)
	}
	if ac.PeerCertificate == nil {
		t.Fatal("PeerCertificate should be populated from TLS")
	}
	if ac.PeerCertificate.Subject.CommonName != "mock-peer" {
		t.Errorf("peer CN = %q, want mock-peer", ac.PeerCertificate.Subject.CommonName)
	}
}

func TestAuthMechanismString(t *testing.T) {
	tests := []struct {
		m    AuthMechanism
		want string
	}{
		{AuthMechanismUnknown, "Unknown"},
		{AuthMechanismNone, "None"},
		{AuthMechanismACSEPassword, "ACSEPassword"},
		{AuthMechanismTLSCertificate, "TLSCertificate"},
		{AuthMechanism(99), "AuthMechanism(?)"},
	}
	for _, tt := range tests {
		if got := tt.m.String(); got != tt.want {
			t.Errorf("AuthMechanism(%d).String() = %q, want %q", int(tt.m), got, tt.want)
		}
	}
}

func TestAuthContextHelpers(t *testing.T) {
	t.Run("nil context", func(t *testing.T) {
		var ac AuthContext
		if ac.HasTLSCertificate() {
			t.Error("HasTLSCertificate() should be false for zero value")
		}
		if ac.HasCallingApplication() {
			t.Error("HasCallingApplication() should be false for zero value")
		}
	})

	t.Run("with TLS cert", func(t *testing.T) {
		ac := AuthContext{
			PeerCertificate: &x509.Certificate{},
		}
		if !ac.HasTLSCertificate() {
			t.Error("HasTLSCertificate() should be true")
		}
		if ac.HasCallingApplication() {
			t.Error("HasCallingApplication() should be false")
		}
	})

	t.Run("with calling application", func(t *testing.T) {
		ac := AuthContext{
			CallingApplication: &ApplicationReference{
				APTitle: APTitle{1, 2, 3},
			},
		}
		if ac.HasTLSCertificate() {
			t.Error("HasTLSCertificate() should be false")
		}
		if !ac.HasCallingApplication() {
			t.Error("HasCallingApplication() should be true")
		}
	})

	t.Run("both present", func(t *testing.T) {
		ac := AuthContext{
			PeerCertificate:    &x509.Certificate{},
			CallingApplication: &ApplicationReference{APTitle: APTitle{1, 2}},
		}
		if !ac.HasTLSCertificate() {
			t.Error("HasTLSCertificate() should be true")
		}
		if !ac.HasCallingApplication() {
			t.Error("HasCallingApplication() should be true")
		}
	})
}

// --- RegisterNamedVariableList unit tests ---

func TestRegisterNamedVariableList(t *testing.T) {
	t.Run("valid NVL", func(t *testing.T) {
		s := NewServer(ServerOptions{MMS: ServerMMSOptions{MaxPDUSize: 65000}})
		if err := s.RegisterDomain("testDomain"); err != nil {
			t.Fatal(err)
		}

		err := s.RegisterNamedVariableList(NamedVariableList{
			Name: ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "ds1"},
			Variables: []VariableSpec{
				{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "var1"}},
			},
		})
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
	})

	t.Run("multiple variables", func(t *testing.T) {
		s := NewServer(ServerOptions{MMS: ServerMMSOptions{MaxPDUSize: 65000}})
		if err := s.RegisterDomain("testDomain"); err != nil {
			t.Fatal(err)
		}

		err := s.RegisterNamedVariableList(NamedVariableList{
			Name: ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "ds2"},
			Variables: []VariableSpec{
				{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "var1"}},
				{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "var2"}},
				{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "var3"}},
			},
		})
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
	})

	t.Run("empty variables", func(t *testing.T) {
		s := NewServer(ServerOptions{MMS: ServerMMSOptions{MaxPDUSize: 65000}})
		if err := s.RegisterDomain("testDomain"); err != nil {
			t.Fatal(err)
		}

		err := s.RegisterNamedVariableList(NamedVariableList{
			Name:      ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "ds3"},
			Variables: nil,
		})
		if err == nil {
			t.Fatal("expected error for empty variables")
		}
	})

	t.Run("empty ItemID", func(t *testing.T) {
		s := NewServer(ServerOptions{MMS: ServerMMSOptions{MaxPDUSize: 65000}})
		if err := s.RegisterDomain("testDomain"); err != nil {
			t.Fatal(err)
		}

		err := s.RegisterNamedVariableList(NamedVariableList{
			Name: ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: ""},
			Variables: []VariableSpec{
				{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "var1"}},
			},
		})
		if err == nil {
			t.Fatal("expected error for empty ItemID")
		}
	})

	t.Run("variable with empty ItemID", func(t *testing.T) {
		s := NewServer(ServerOptions{MMS: ServerMMSOptions{MaxPDUSize: 65000}})
		if err := s.RegisterDomain("testDomain"); err != nil {
			t.Fatal(err)
		}

		err := s.RegisterNamedVariableList(NamedVariableList{
			Name: ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "ds5"},
			Variables: []VariableSpec{
				{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: ""}},
			},
		})
		if err == nil {
			t.Fatal("expected error for variable with empty ItemID")
		}
	})

	t.Run("duplicate NVL", func(t *testing.T) {
		s := NewServer(ServerOptions{MMS: ServerMMSOptions{MaxPDUSize: 65000}})
		if err := s.RegisterDomain("testDomain"); err != nil {
			t.Fatal(err)
		}

		nvl := NamedVariableList{
			Name: ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "ds6"},
			Variables: []VariableSpec{
				{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "var1"}},
			},
		}

		if err := s.RegisterNamedVariableList(nvl); err != nil {
			t.Fatalf("first register: %v", err)
		}
		err := s.RegisterNamedVariableList(nvl)
		if err == nil {
			t.Fatal("expected error for duplicate NVL")
		}
	})

	t.Run("domain scope missing domain", func(t *testing.T) {
		s := NewServer(ServerOptions{MMS: ServerMMSOptions{MaxPDUSize: 65000}})

		err := s.RegisterNamedVariableList(NamedVariableList{
			Name: ObjectName{Scope: ObjectScopeDomain, Domain: "", ItemID: "ds7"},
			Variables: []VariableSpec{
				{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "testDomain", ItemID: "var1"}},
			},
		})
		if err == nil {
			t.Fatal("expected error for domain scope with empty Domain")
		}
	})
}

func TestIsTemporary(t *testing.T) {
	if isTemporary(fmt.Errorf("plain error")) {
		t.Fatal("plain error should not be temporary")
	}
	if isTemporary(nil) {
		t.Fatal("nil should not be temporary")
	}
}

type tempError struct{ temp bool }

func (e *tempError) Error() string   { return "temp" }
func (e *tempError) Temporary() bool { return e.temp }

func TestIsTemporaryWithInterface(t *testing.T) {
	if !isTemporary(&tempError{temp: true}) {
		t.Fatal("temporary error should be temporary")
	}
	if isTemporary(&tempError{temp: false}) {
		t.Fatal("non-temporary error should not be temporary")
	}
}

func TestClientAbort(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)

	err := client.Abort(context.Background())
	if err != nil {
		t.Fatalf("Abort: %v", err)
	}

	// Second abort should be a no-op (already closed).
	err = client.Abort(context.Background())
	if err != nil {
		t.Fatalf("second Abort: %v", err)
	}
}

func TestClientNegotiated(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	np := client.Negotiated()
	if np.MaxPDUSize == 0 {
		t.Fatal("MaxPDUSize should be non-zero after association")
	}
	if np.MaxOutCalling == 0 {
		t.Fatal("MaxOutCalling should be non-zero")
	}
	if np.MaxOutCalled == 0 {
		t.Fatal("MaxOutCalled should be non-zero")
	}
	if np.NestingLevel == 0 {
		t.Fatal("NestingLevel should be non-zero")
	}
}

func TestObjectScopeToWire(t *testing.T) {
	tests := []struct {
		scope ObjectScope
		ok    bool
	}{
		{ObjectScopeVMD, true},
		{ObjectScopeDomain, true},
		{ObjectScopeAssociation, true},
		{ObjectScope(99), false},
	}
	for _, tt := range tests {
		_, err := objectScopeToWire(tt.scope)
		if tt.ok && err != nil {
			t.Errorf("scope %d: unexpected error: %v", tt.scope, err)
		}
		if !tt.ok && err == nil {
			t.Errorf("scope %d: expected error", tt.scope)
		}
	}
}

func TestHandleReject(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	rejectContent := codec.MarshalRejectPDU(1, 1, 0)
	_, rejectInner, err := codec.UnwrapPdu(rejectContent)
	if err != nil {
		t.Fatalf("unwrap reject: %v", err)
	}

	err = client.handleReject(1, rejectInner)
	if err == nil {
		t.Fatal("expected error from handleReject")
	}
	var pe *ProtocolError
	if !errors.As(err, &pe) {
		t.Fatalf("expected ProtocolError, got %T: %v", err, err)
	}
}

// --- Concurrency / race stress tests ---

func TestConcurrentClientRequests(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	var wg sync.WaitGroup
	const goroutines = 20

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			_, err := client.Identify(ctx)
			if err != nil {
				t.Errorf("Identify: %v", err)
			}
		}()
	}

	wg.Wait()
}

func TestConcurrentReadWrite(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)
	defer client.Close(context.Background())

	var wg sync.WaitGroup
	const goroutines = 10

	for i := 0; i < goroutines; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := client.GetNameList(ctx, NameListRequest{
				ObjectClass: ObjectClassNamedVariable,
				Scope:       ObjectScopeDomain,
				DomainID:    "testDomain",
			})
			if err != nil {
				t.Errorf("GetNameList: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := client.Status(ctx)
			if err != nil {
				t.Errorf("Status: %v", err)
			}
		}()
	}

	wg.Wait()
}

func TestCloseWhileConcurrentRequests(t *testing.T) {
	srv := testServer(t)
	client := connectClientServer(t, srv)

	var wg sync.WaitGroup
	const goroutines = 10

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			client.Identify(ctx) //nolint:errcheck
		}()
	}

	time.Sleep(5 * time.Millisecond)
	client.Close(context.Background()) //nolint:errcheck

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("goroutines stuck after Close")
	}
}

func TestDiscardHandler(t *testing.T) {
	h := discardHandler{}
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("should not be enabled")
	}
	if err := h.Handle(context.Background(), slog.Record{}); err != nil {
		t.Fatal(err)
	}
	h2 := h.WithAttrs(nil)
	if h2 == nil {
		t.Fatal("WithAttrs returned nil")
	}
	h3 := h.WithGroup("test")
	if h3 == nil {
		t.Fatal("WithGroup returned nil")
	}
}
