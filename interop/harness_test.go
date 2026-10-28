//go:build interop

// SPDX-License-Identifier: MIT

// Package interop contains interoperability tests for go-mms.
//
// Tests in this package start mms-interop adapter containers (or local
// binaries), wait for the JSON readiness event, exercise the go-mms API,
// and assert results. They own the full lifecycle: start, wait, run, teardown.
//
// Run with:
//
//	go test -tags=interop ./interop/...
//
// Environment variables (all optional):
//
//	MMS_INTEROP_STACK     Peer stack: libiec61850 (default), dscsystems or iec61850-rust
//	MMS_INTEROP_IMAGE     Adapter image for the selected stack (overrides the per-stack variable)
//	LIBIEC61850_IMAGE     Image for libiec61850 (default: mms-interop-libiec61850:local)
//	DSCSYSTEMS_IMAGE      Image for dscsystems (default: mms-interop-dscsystems:local)
//	IEC61850RUST_IMAGE    Image for iec61850-rust (default: mms-interop-iec61850-rust:local)
//	MMS_SERVER_BINARY     Path to the stack's <stack>-mms-server binary (skips Docker when set)
//	MMS_CLIENT_BINARY     Path to the stack's <stack>-mms-client binary (skips Docker when set)
//	MMS_FIXTURE           Path to fixture file (default: testdata/interop.json)
package interop

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	defaultFixturePath  = "testdata/interop.json"
	readyTimeout        = 30 * time.Second
	clientResultTimeout = 60 * time.Second
)

// ---------------------------------------------------------------------------
// Common types
// ---------------------------------------------------------------------------

type readyEvent struct {
	Event   string `json:"event"`
	Address string `json:"address"`
	Fixture string `json:"fixture"`
	Adapter string `json:"adapter"`
	Version string `json:"version"`
}

type adapterReady struct {
	addr    string
	fixture string
	adapter string
	version string
}

// validateAdapterMeta asserts that the ready event carries the expected fixture
// and adapter identifiers and a non-empty version string. In CI (env CI set) a
// "dev" version is rejected so accidental use of a local image is caught early.
func validateAdapterMeta(t *testing.T, m adapterReady, wantFixture, wantAdapter string) {
	t.Helper()
	if m.fixture != wantFixture {
		t.Errorf("adapter fixture: got %q, want %q", m.fixture, wantFixture)
	}
	if m.adapter != wantAdapter {
		t.Errorf("adapter name: got %q, want %q", m.adapter, wantAdapter)
	}
	if m.version == "" {
		t.Error("adapter version is empty")
	}
	if os.Getenv("CI") != "" && m.version == "dev" {
		t.Errorf("adapter version is %q in CI; pin a released image digest", m.version)
	}
}

type serverHandle struct {
	addr string
	stop func()
	kill func() // stops the peer process (and its container) at once
}

// clientResult is one JSON Line emitted by the MMS client adapter.
type clientResult struct {
	Operation string      `json:"operation"`
	OK        bool        `json:"ok"`
	Error     string      `json:"error,omitempty"`
	Target    string      `json:"target,omitempty"`
	Value     interface{} `json:"value,omitempty"`
	Values    interface{} `json:"values,omitempty"`
	Names     []string    `json:"names,omitempty"`
	Type      string      `json:"type,omitempty"`
}

// ---------------------------------------------------------------------------
// Utility helpers
// ---------------------------------------------------------------------------

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// freePort returns an unused TCP port. When MMS_INTEROP_PORT is set that
// value is used directly; otherwise an ephemeral port is chosen.
func freePort(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("MMS_INTEROP_PORT"); p != "" {
		return p
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	p := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return fmt.Sprintf("%d", p)
}

func fixturePath(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(getEnv("MMS_FIXTURE", defaultFixturePath))
	if err != nil {
		t.Fatalf("fixture path: %v", err)
	}
	return p
}

// dial opens an MMS connection to addr, retrying for up to 5 seconds to
// absorb any latency between the Docker container's "ready" event and the
// TCP listener becoming fully available (especially under QEMU emulation).
func dial(t *testing.T, ctx context.Context, addr string, opts ...iso.Option) *mms.Client {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		client, err := iso.Dial(ctx, addr, opts...)
		if err == nil {
			return client
		}
		if time.Now().After(deadline) {
			t.Fatalf("iso.Dial %s: %v", addr, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// ---------------------------------------------------------------------------
// libiec61850 MMS server adapter (client-direction tests)
// ---------------------------------------------------------------------------

// startAdapter launches the libiec61850-mms-server container (or binary) and
// waits for the JSON readiness event. It returns a serverHandle whose addr
// field is ready to dial.
// dockerContainerName returns a unique Docker container name for the given test.
func dockerContainerName(t *testing.T) string {
	t.Helper()
	name := fmt.Sprintf("interop-%d-%s", os.Getpid(), t.Name())
	return strings.NewReplacer("/", "_", " ", "_", "(", "", ")", "").Replace(name)
}

// dockerStop sends docker stop to the named container; errors are ignored
// because the container may have already exited.
func dockerStop(name string) {
	exec.Command("docker", "stop", "-t", "2", name).Run() //nolint:errcheck
}

func startAdapter(t *testing.T) *serverHandle {
	t.Helper()
	skipForStack(t)
	peer := stack(t)

	port := freePort(t)
	fixtureAbs := fixturePath(t)

	// cmdCtx controls the Docker/binary process lifetime; it is cancelled
	// via t.Cleanup so the process outlives the readiness-wait timeout.
	cmdCtx, cmdCancel := context.WithCancel(context.Background())

	var cmd *exec.Cmd
	stopContainer := func() {}
	if binary := os.Getenv("MMS_SERVER_BINARY"); binary != "" {
		cmd = exec.CommandContext(cmdCtx, binary,
			"--fixture", fixtureAbs,
			"--port", port,
		)
	} else {
		containerName := dockerContainerName(t)
		stopContainer = func() { dockerStop(containerName) }
		t.Cleanup(func() { dockerStop(containerName) })
		cmd = exec.CommandContext(cmdCtx, "docker", "run", "--rm",
			"--name", containerName,
			"-p", port+":"+port,
			"-v", fixtureAbs+":/fixtures/mms/interop.json:ro",
			"--entrypoint", peer.serverCommand(),
			peer.image(),
			"--fixture", "/fixtures/mms/interop.json",
			"--port", port,
		)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cmdCancel()
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		cmdCancel()
		t.Fatalf("start adapter: %v", err)
	}

	stop := func() {
		cmdCancel()
		_ = cmd.Wait()
	}

	ready := make(chan adapterReady, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			var ev readyEvent
			if json.Unmarshal([]byte(line), &ev) == nil && ev.Event == "ready" {
				ready <- adapterReady{
					addr:    fmt.Sprintf("127.0.0.1:%s", port),
					fixture: ev.Fixture,
					adapter: ev.Adapter,
					version: ev.Version,
				}
				break
			}
		}
		_ = scanner.Err()
		_, _ = io.Copy(io.Discard, stdout)
		close(ready)
	}()

	// Use a separate short-lived context only for waiting for the ready event.
	startCtx, startCancel := context.WithTimeout(context.Background(), readyTimeout)
	defer startCancel()

	select {
	case m, ok := <-ready:
		if !ok {
			stop()
			t.Fatal("adapter exited before emitting readiness event")
		}
		validateAdapterMeta(t, m, mustLoadFixture(t).Revision, peer.name)
		t.Cleanup(stop)
		kill := func() {
			stopContainer()
			stop()
		}
		return &serverHandle{addr: m.addr, stop: stop, kill: kill}
	case <-startCtx.Done():
		stop()
		t.Fatal("timed out waiting for adapter readiness")
		return nil
	}
}

// ---------------------------------------------------------------------------
// libiec61850 MMS client adapter (server-direction tests)
// ---------------------------------------------------------------------------

// startClientAdapter launches the libiec61850-mms-client container (or binary)
// pointing at a go-mms server on the given port. It returns a channel that
// receives parsed JSON Lines from the adapter's stdout.
func startClientAdapter(t *testing.T, port int) <-chan clientResult {
	t.Helper()
	peer := stack(t)

	ctx, cancel := context.WithTimeout(context.Background(), clientResultTimeout)

	var cmd *exec.Cmd
	if binary := os.Getenv("MMS_CLIENT_BINARY"); binary != "" {
		cmd = exec.CommandContext(ctx, binary,
			"--host", "127.0.0.1",
			"--port", fmt.Sprintf("%d", port),
		)
	} else {
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm",
			"--add-host=host.docker.internal:host-gateway",
			"--entrypoint", peer.clientCommand(),
			peer.image(),
			"--host", "host.docker.internal",
			"--port", fmt.Sprintf("%d", port),
		)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start client adapter: %v", err)
	}

	results := make(chan clientResult, 32)
	go func() {
		defer close(results)
		defer cancel()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			var r clientResult
			if err := json.Unmarshal([]byte(line), &r); err != nil {
				t.Logf("unparseable client line: %q: %v", line, err)
				continue
			}
			results <- r
		}
		_ = scanner.Err()
		_, _ = io.Copy(io.Discard, stdout)
		_ = cmd.Wait()
	}()

	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
	})

	return results
}

// clientRun is the outcome of one libiec61850 client adapter run against
// the fixture-backed go-mms server.
type clientRun struct {
	results []clientResult
	files   []string // file store contents after the run

	shutdownErr error // Server.Shutdown after the run
	servedErr   error // ListenAndServe's result after Shutdown
	connsAfter  int   // associations left after Shutdown
}

var sharedRun struct {
	once sync.Once
	run  clientRun
}

// sharedClientRun runs the client adapter once per test binary; the
// adapter's operation sequence is fixed, so every server-direction test
// asserts on the same run. The server and adapter are bound to the first
// caller's lifetime, but the run is complete before that test returns.
func sharedClientRun(t *testing.T) clientRun {
	t.Helper()
	skipForStack(t)
	sharedRun.once.Do(func() {
		srv := startGoMmsServer(t)
		sharedRun.run.results = collectResults(t, startClientAdapter(t, srv.port))
		sharedRun.run.files = srv.files.names()

		// End the run with a graceful shutdown.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		sharedRun.run.shutdownErr = srv.srv.Shutdown(ctx)
		select {
		case sharedRun.run.servedErr = <-srv.served:
		case <-ctx.Done():
			sharedRun.run.servedErr = ctx.Err()
		}
		sharedRun.run.connsAfter = len(srv.srv.Connections())
	})
	if len(sharedRun.run.results) == 0 {
		t.Fatal("client adapter produced no results")
	}
	return sharedRun.run
}

// collectResults drains the results channel into a slice and logs each result.
func collectResults(t *testing.T, ch <-chan clientResult) []clientResult {
	t.Helper()
	var out []clientResult
	for r := range ch {
		t.Logf("client: op=%q ok=%v target=%q error=%q", r.Operation, r.OK, r.Target, r.Error)
		out = append(out, r)
	}
	return out
}

// findResult returns the first result matching operation and target.
func findResult(results []clientResult, operation, target string) (clientResult, bool) {
	for _, r := range results {
		if r.Operation == operation && r.Target == target {
			return r, true
		}
	}
	return clientResult{}, false
}

// findOp returns the first result matching operation (any target).
func findOp(results []clientResult, operation string) (clientResult, bool) {
	for _, r := range results {
		if r.Operation == operation {
			return r, true
		}
	}
	return clientResult{}, false
}

// ---------------------------------------------------------------------------
// Fixture-backed go-mms server (server-direction tests)
// ---------------------------------------------------------------------------

type fixtureFile struct {
	Revision string `json:"revision"`
	Identity struct {
		Vendor   string `json:"vendor"`
		Model    string `json:"model"`
		Revision string `json:"revision"`
	} `json:"identity"`
	Status struct {
		Logical  int `json:"logical"`
		Physical int `json:"physical"`
	} `json:"status"`
	Domains            map[string]fixtureDomain `json:"domains"`
	InformationReports []fixtureReport          `json:"informationReports"`
	Files              map[string]fixtureStored `json:"files"`
}

type fixtureDomain struct {
	Variables map[string]fixtureVar     `json:"variables"`
	Journals  map[string]fixtureJournal `json:"journals"`
}

type fixtureJournal struct {
	Entries []fixtureJournalEntry `json:"entries"`
}

type fixtureJournalEntry struct {
	EntryID        uint64      `json:"entryId"`
	OccurrenceTime string      `json:"occurrenceTime"`
	Tag            string      `json:"tag"`
	Type           string      `json:"type"`
	Value          interface{} `json:"value"`
	ReasonCode     int         `json:"reasonCode"`
}

// entryIDBytes is the wire form of a fixture entryId: 8 little-endian bytes,
// as libiec61850 writes its uint64 entry IDs.
func entryIDBytes(id uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, id)
	return b
}

func (e fixtureJournalEntry) time(t *testing.T) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339Nano, e.OccurrenceTime)
	if err != nil {
		t.Fatalf("journal entry %d time %q: %v", e.EntryID, e.OccurrenceTime, err)
	}
	return tm
}

type fixtureReport struct {
	VMDList   string   `json:"vmdList"`
	Variables []string `json:"variables"`
}

type fixtureStored struct {
	Content      *string `json:"content"`
	Hex          *string `json:"hex"`
	Size         *int    `json:"size"`
	LastModified string  `json:"lastModified"`
}

// bytes returns the content of a fixture file. Generated files (size only)
// hold byte i = i % 251.
func (f fixtureStored) bytes() ([]byte, error) {
	switch {
	case f.Content != nil:
		return []byte(*f.Content), nil
	case f.Hex != nil:
		return hex.DecodeString(*f.Hex)
	case f.Size != nil:
		b := make([]byte, *f.Size)
		for i := range b {
			b[i] = byte(i % 251)
		}
		return b, nil
	}
	return nil, fmt.Errorf("file has no content, hex or size")
}

// modTime is the zero time when the fixture gives no lastModified.
func (f fixtureStored) modTime() time.Time {
	t, _ := time.Parse(time.RFC3339, f.LastModified)
	return t
}

// modMs is lastModified as a libiec61850 client reports it: milliseconds
// since the epoch, 0 when the server omitted it.
func (f fixtureStored) modMs() int64 {
	if f.LastModified == "" {
		return 0
	}
	return f.modTime().UnixMilli()
}

func mustLoadFixture(t *testing.T) *fixtureFile {
	t.Helper()
	fix, err := loadFixture(fixturePath(t))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	return fix
}

func (f *fixtureFile) file(t *testing.T, name string) ([]byte, fixtureStored) {
	t.Helper()
	stored, ok := f.Files[name]
	if !ok {
		t.Fatalf("fixture has no file %q", name)
	}
	data, err := stored.bytes()
	if err != nil {
		t.Fatalf("fixture file %q: %v", name, err)
	}
	return data, stored
}

type fixtureVar struct {
	Type        string      `json:"type"`
	Size        int         `json:"size"`
	Value       interface{} `json:"value"`
	Writable    bool        `json:"writable"`
	ElementType string      `json:"elementType"`
	Count       int         `json:"count"`
}

type goMmsServer struct {
	port   int
	files  *fixtureFileStore
	srv    *mms.Server
	served <-chan error // ListenAndServe's result
}

// startGoMmsServer reads the fixture and starts a fixture-backed go-mms server
// on an ephemeral port bound to all interfaces.
func startGoMmsServer(t *testing.T) *goMmsServer {
	t.Helper()

	fix, err := loadFixture(fixturePath(t))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	files, err := newFixtureFileStore(fix)
	if err != nil {
		t.Fatalf("fixture files: %v", err)
	}
	journals, err := newFixtureJournals(fix)
	if err != nil {
		t.Fatalf("fixture journals: %v", err)
	}

	srv := mms.NewServer(mms.ServerOptions{FileProvider: files, JournalProvider: journals})

	srv.HandleIdentify(func(_ context.Context, _ mms.IdentifyRequest) (*mms.ServerIdentity, error) {
		return &mms.ServerIdentity{
			Vendor:   fix.Identity.Vendor,
			Model:    fix.Identity.Model,
			Revision: fix.Identity.Revision,
		}, nil
	})

	srv.HandleStatus(func(_ context.Context, _ mms.StatusRequest) (*mms.ServerStatus, error) {
		return &mms.ServerStatus{
			Logical:  mms.VMDLogicalStatus(fix.Status.Logical),
			Physical: mms.VMDPhysicalStatus(fix.Status.Physical),
		}, nil
	})

	vars := make(map[string]mms.Variable)
	readRef := func(ref string) (*mms.Value, error) {
		v, ok := vars[ref]
		if !ok {
			return nil, fmt.Errorf("unknown variable %q", ref)
		}
		return v.Read(context.Background())
	}
	for domainName, domain := range fix.Domains {
		if err := srv.RegisterDomain(domainName); err != nil {
			t.Fatalf("RegisterDomain %q: %v", domainName, err)
		}
		for varName, v := range domain.Variables {
			entry, err := buildVariable(mms.DomainID(domainName), mms.ItemID(varName), v)
			if err != nil {
				t.Fatalf("build variable %s/%s: %v", domainName, varName, err)
			}
			if varName == "report-trigger" {
				store := entry.Write
				entry.Write = func(ctx context.Context, val *mms.Value) error {
					if err := store(ctx, val); err != nil {
						return err
					}
					return onReportTrigger(ctx, srv, fix, val, readRef)
				}
			}
			vars[domainName+"/"+varName] = entry
			if err := srv.RegisterVariable(entry); err != nil {
				t.Fatalf("RegisterVariable %s/%s: %v", domainName, varName, err)
			}
		}
	}

	// Named-list reports refer to VMD-scoped lists the client can inspect.
	for _, r := range fix.InformationReports {
		if r.VMDList == "" {
			continue
		}
		nvl := mms.NamedVariableList{Name: mms.ObjectName{Scope: mms.ObjectScopeVMD, ItemID: mms.ItemID(r.VMDList)}}
		for _, ref := range r.Variables {
			dom, item, _ := strings.Cut(ref, "/")
			nvl.Variables = append(nvl.Variables, mms.VariableSpec{Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: mms.DomainID(dom), ItemID: mms.ItemID(item)}})
		}
		if err := srv.RegisterNamedVariableList(nvl); err != nil {
			t.Fatalf("RegisterNamedVariableList %s: %v", r.VMDList, err)
		}
	}

	ln, err := iso.Listen("0.0.0.0:0")
	if err != nil {
		t.Fatalf("iso.Listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- srv.ListenAndServe(ctx, ln) }()
	t.Cleanup(cancel)

	// Wait until the server goroutine is actually accepting TCP connections.
	// Without this probe, a fast CI runner can start the Docker client before
	// ListenAndServe has entered its accept loop, causing all operations to
	// return "connection-lost" (COTP handshake times out).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
		if err == nil {
			c.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	return &goMmsServer{port: port, files: files, srv: srv, served: served}
}

func loadFixture(path string) (*fixtureFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var fix fixtureFile
	if err := json.Unmarshal(data, &fix); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &fix, nil
}

func buildVariable(domain mms.DomainID, itemID mms.ItemID, v fixtureVar) (mms.Variable, error) {
	name := mms.ObjectName{
		Scope:  mms.ObjectScopeDomain,
		Domain: domain,
		ItemID: itemID,
	}
	typeSpec, initial, err := parseTypeAndValue(v)
	if err != nil {
		return mms.Variable{}, err
	}
	var mu sync.Mutex
	current := initial
	variable := mms.Variable{
		Name:     name,
		TypeSpec: typeSpec,
		Read: func(_ context.Context) (*mms.Value, error) {
			mu.Lock()
			defer mu.Unlock()
			return current, nil
		},
	}
	if v.Writable {
		variable.Write = func(_ context.Context, val *mms.Value) error {
			if val == nil || val.Type() != typeSpec.Type {
				return fmt.Errorf("type mismatch: got %v, want %v", val.Type(), typeSpec.Type)
			}
			mu.Lock()
			defer mu.Unlock()
			current = val
			return nil
		}
	}
	return variable, nil
}

func parseTypeAndValue(v fixtureVar) (mms.TypeSpec, *mms.Value, error) {
	switch v.Type {
	case "boolean":
		b, ok := v.Value.(bool)
		if !ok {
			return mms.TypeSpec{}, nil, fmt.Errorf("boolean: expected bool, got %T", v.Value)
		}
		return mms.TypeSpec{Type: mms.ValueTypeBoolean}, mms.NewBoolean(b), nil

	case "integer":
		n, ok := v.Value.(float64)
		if !ok {
			return mms.TypeSpec{}, nil, fmt.Errorf("integer: expected number, got %T", v.Value)
		}
		size := v.Size
		if size == 0 {
			size = 32
		}
		return mms.TypeSpec{Type: mms.ValueTypeInteger, Size: size}, mms.NewInteger(int64(n)), nil

	case "unsigned":
		n, ok := v.Value.(float64)
		if !ok {
			return mms.TypeSpec{}, nil, fmt.Errorf("unsigned: expected number, got %T", v.Value)
		}
		size := v.Size
		if size == 0 {
			size = 32
		}
		return mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: size}, mms.NewUnsigned(uint64(n)), nil

	case "float32":
		n, ok := v.Value.(float64)
		if !ok {
			return mms.TypeSpec{}, nil, fmt.Errorf("float32: expected number, got %T", v.Value)
		}
		ts := mms.TypeSpec{Type: mms.ValueTypeFloat, FormatWidth: 32, ExponentWidth: 8}
		return ts, mms.NewFloat(n), nil

	case "visible-string":
		s, ok := v.Value.(string)
		if !ok {
			return mms.TypeSpec{}, nil, fmt.Errorf("visible-string: expected string, got %T", v.Value)
		}
		return mms.TypeSpec{Type: mms.ValueTypeVisibleString, Size: v.Size}, mms.NewVisibleString(s), nil

	case "octet-string":
		s, ok := v.Value.(string)
		if !ok {
			return mms.TypeSpec{}, nil, fmt.Errorf("octet-string: expected string, got %T", v.Value)
		}
		decoded, err := hex.DecodeString(s)
		if err != nil {
			return mms.TypeSpec{}, nil, fmt.Errorf("octet-string hex: %w", err)
		}
		ts := mms.TypeSpec{Type: mms.ValueTypeOctetString, Size: len(decoded)}
		return ts, mms.NewOctetString(decoded), nil

	case "bit-string":
		s, ok := v.Value.(string)
		if !ok {
			return mms.TypeSpec{}, nil, fmt.Errorf("bit-string: expected string, got %T", v.Value)
		}
		bitLen := len(s)
		byteLen := (bitLen + 7) / 8
		bits := make([]byte, byteLen)
		for i, ch := range s {
			if ch == '1' {
				bits[i/8] |= 1 << (7 - uint(i%8))
			}
		}
		ts := mms.TypeSpec{Type: mms.ValueTypeBitString, Size: bitLen}
		return ts, mms.NewBitStringWithLength(bits, bitLen), nil

	case "utc-time":
		s, ok := v.Value.(string)
		if !ok {
			return mms.TypeSpec{}, nil, fmt.Errorf("utc-time: expected string, got %T", v.Value)
		}
		var tm time.Time
		var parseErr error
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000Z", time.RFC3339} {
			tm, parseErr = time.Parse(layout, s)
			if parseErr == nil {
				break
			}
		}
		if parseErr != nil {
			return mms.TypeSpec{}, nil, fmt.Errorf("utc-time parse %q: %w", s, parseErr)
		}
		return mms.TypeSpec{Type: mms.ValueTypeUTCTime}, mms.NewUTCTime(tm.UTC()), nil

	case "array":
		if v.ElementType == "" {
			return mms.TypeSpec{}, nil, fmt.Errorf("array: missing elementType")
		}
		elemSpec, err := parseScalarTypeSpec(v.ElementType, 0)
		if err != nil {
			return mms.TypeSpec{}, nil, fmt.Errorf("array elementType: %w", err)
		}
		rawElems, ok := v.Value.([]interface{})
		if !ok {
			return mms.TypeSpec{}, nil, fmt.Errorf("array: expected JSON array, got %T", v.Value)
		}
		count := len(rawElems)
		if v.Count > 0 {
			count = v.Count
		}
		elems := make([]*mms.Value, 0, len(rawElems))
		for i, raw := range rawElems {
			n, ok := raw.(float64)
			if !ok {
				return mms.TypeSpec{}, nil, fmt.Errorf("array[%d]: expected number, got %T", i, raw)
			}
			var elem *mms.Value
			switch v.ElementType {
			case "integer":
				elem = mms.NewInteger(int64(n))
			case "unsigned":
				elem = mms.NewUnsigned(uint64(n))
			case "float32":
				elem = mms.NewFloat(n)
			default:
				return mms.TypeSpec{}, nil, fmt.Errorf("array: unsupported element type %q", v.ElementType)
			}
			elems = append(elems, elem)
		}
		ts := mms.TypeSpec{Type: mms.ValueTypeArray, Count: count, Element: &elemSpec}
		return ts, mms.NewArray(elems), nil

	case "structure":
		rawComps, ok := v.Value.([]interface{})
		if !ok {
			return mms.TypeSpec{}, nil, fmt.Errorf("structure: expected JSON array, got %T", v.Value)
		}
		var tsElems []mms.TypeSpecElement
		var vals []*mms.Value
		for i, raw := range rawComps {
			comp, ok := raw.(map[string]interface{})
			if !ok {
				return mms.TypeSpec{}, nil, fmt.Errorf("structure[%d]: expected object, got %T", i, raw)
			}
			ct, _ := comp["type"].(string)
			cv := comp["value"]
			name, _ := comp["name"].(string)
			if name == "" {
				name = fmt.Sprintf("comp%d", i)
			}
			compSpec, err := parseScalarTypeSpec(ct, 0)
			if err != nil {
				return mms.TypeSpec{}, nil, fmt.Errorf("structure[%d] type: %w", i, err)
			}
			tsElems = append(tsElems, mms.TypeSpecElement{Name: name, Type: compSpec})
			val, err := makeScalarValue(ct, cv)
			if err != nil {
				return mms.TypeSpec{}, nil, fmt.Errorf("structure[%d] value: %w", i, err)
			}
			vals = append(vals, val)
		}
		ts := mms.TypeSpec{Type: mms.ValueTypeStructure, Elements: tsElems}
		return ts, mms.NewStructure(vals), nil

	default:
		return mms.TypeSpec{}, nil, fmt.Errorf("unsupported variable type %q", v.Type)
	}
}

func parseScalarTypeSpec(typeName string, size int) (mms.TypeSpec, error) {
	switch typeName {
	case "boolean":
		return mms.TypeSpec{Type: mms.ValueTypeBoolean}, nil
	case "integer":
		if size == 0 {
			size = 32
		}
		return mms.TypeSpec{Type: mms.ValueTypeInteger, Size: size}, nil
	case "unsigned":
		if size == 0 {
			size = 32
		}
		return mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: size}, nil
	case "float32":
		return mms.TypeSpec{Type: mms.ValueTypeFloat, FormatWidth: 32, ExponentWidth: 8}, nil
	case "visible-string":
		return mms.TypeSpec{Type: mms.ValueTypeVisibleString, Size: size}, nil
	case "octet-string":
		return mms.TypeSpec{Type: mms.ValueTypeOctetString, Size: size}, nil
	case "bit-string":
		return mms.TypeSpec{Type: mms.ValueTypeBitString, Size: size}, nil
	case "utc-time":
		return mms.TypeSpec{Type: mms.ValueTypeUTCTime}, nil
	default:
		return mms.TypeSpec{}, fmt.Errorf("unsupported scalar type %q", typeName)
	}
}

func makeScalarValue(typeName string, raw interface{}) (*mms.Value, error) {
	switch typeName {
	case "boolean":
		b, ok := raw.(bool)
		if !ok {
			return nil, fmt.Errorf("expected bool, got %T", raw)
		}
		return mms.NewBoolean(b), nil
	case "integer":
		n, ok := raw.(float64)
		if !ok {
			return nil, fmt.Errorf("expected number, got %T", raw)
		}
		return mms.NewInteger(int64(n)), nil
	case "unsigned":
		n, ok := raw.(float64)
		if !ok {
			return nil, fmt.Errorf("expected number, got %T", raw)
		}
		return mms.NewUnsigned(uint64(n)), nil
	case "float32":
		n, ok := raw.(float64)
		if !ok {
			return nil, fmt.Errorf("expected number, got %T", raw)
		}
		return mms.NewFloat(n), nil
	default:
		return nil, fmt.Errorf("unsupported scalar type %q", typeName)
	}
}
