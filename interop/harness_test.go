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
//	LIBIEC61850_IMAGE     Docker image for the libiec61850 adapter (default: mms-interop-libiec61850:local)
//	MMS_SERVER_BINARY     Path to libiec61850-mms-server binary (skips Docker when set)
//	MMS_CLIENT_BINARY     Path to libiec61850-mms-client binary (skips Docker when set)
//	MMS_FIXTURE           Path to fixture file (default: testdata/interop.json)
package interop

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
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
	defaultLibIECImage  = "mms-interop-libiec61850:local"
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
}

// clientResult is one JSON Line emitted by the libiec61850 MMS client adapter.
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

// dial opens a go-mms client connection to addr and fails the test on error.
func dial(t *testing.T, ctx context.Context, addr string) *mms.Client {
	t.Helper()
	client, err := iso.Dial(ctx, addr)
	if err != nil {
		t.Fatalf("iso.Dial %s: %v", addr, err)
	}
	return client
}

// ---------------------------------------------------------------------------
// libiec61850 MMS server adapter (client-direction tests)
// ---------------------------------------------------------------------------

// startAdapter launches the libiec61850-mms-server container (or binary) and
// waits for the JSON readiness event. It returns a serverHandle whose addr
// field is ready to dial.
func startAdapter(t *testing.T) *serverHandle {
	t.Helper()

	port := freePort(t)
	fixtureAbs := fixturePath(t)

	ctx, cancel := context.WithTimeout(context.Background(), readyTimeout)

	var cmd *exec.Cmd
	if binary := os.Getenv("MMS_SERVER_BINARY"); binary != "" {
		cmd = exec.CommandContext(ctx, binary,
			"--fixture", fixtureAbs,
			"--port", port,
		)
	} else {
		image := getEnv("LIBIEC61850_IMAGE", defaultLibIECImage)
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm",
			"-p", port+":"+port,
			"-v", fixtureAbs+":/fixtures/mms/interop.json:ro",
			image,
			"--fixture", "/fixtures/mms/interop.json",
			"--port", port,
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
		t.Fatalf("start adapter: %v", err)
	}

	ready := make(chan adapterReady, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			var ev readyEvent
			if json.Unmarshal([]byte(line), &ev) == nil && ev.Event == "ready" {
				ready <- adapterReady{
					addr:    fmt.Sprintf("localhost:%s", port),
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

	stop := func() {
		cancel()
		_ = cmd.Wait()
	}

	select {
	case m, ok := <-ready:
		if !ok {
			stop()
			t.Fatal("adapter exited before emitting readiness event")
		}
		validateAdapterMeta(t, m, "mms-v1", "libiec61850")
		t.Cleanup(stop)
		return &serverHandle{addr: m.addr, stop: stop}
	case <-ctx.Done():
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

	ctx, cancel := context.WithTimeout(context.Background(), clientResultTimeout)

	var cmd *exec.Cmd
	if binary := os.Getenv("MMS_CLIENT_BINARY"); binary != "" {
		cmd = exec.CommandContext(ctx, binary,
			"--host", "127.0.0.1",
			"--port", fmt.Sprintf("%d", port),
		)
	} else {
		image := getEnv("LIBIEC61850_IMAGE", defaultLibIECImage)
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm",
			"--add-host=host.docker.internal:host-gateway",
			"--entrypoint", "libiec61850-mms-client",
			image,
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
	Identity struct {
		Vendor   string `json:"vendor"`
		Model    string `json:"model"`
		Revision string `json:"revision"`
	} `json:"identity"`
	Domains map[string]fixtureDomain `json:"domains"`
}

type fixtureDomain struct {
	Variables map[string]fixtureVar `json:"variables"`
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
	port int
}

// startGoMmsServer reads the fixture and starts a fixture-backed go-mms server
// on an ephemeral port bound to all interfaces.
func startGoMmsServer(t *testing.T) *goMmsServer {
	t.Helper()

	fix, err := loadFixture(fixturePath(t))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	srv := mms.NewServer(mms.ServerOptions{})

	srv.HandleIdentify(func(_ context.Context, _ mms.IdentifyRequest) (*mms.ServerIdentity, error) {
		return &mms.ServerIdentity{
			Vendor:   fix.Identity.Vendor,
			Model:    fix.Identity.Model,
			Revision: fix.Identity.Revision,
		}, nil
	})

	srv.HandleStatus(func(_ context.Context, _ mms.StatusRequest) (*mms.ServerStatus, error) {
		return &mms.ServerStatus{
			Logical:  mms.VMDLogicalStatusStateChangesAllowed,
			Physical: mms.VMDPhysicalStatusOperational,
		}, nil
	})

	for domainName, domain := range fix.Domains {
		if err := srv.RegisterDomain(domainName); err != nil {
			t.Fatalf("RegisterDomain %q: %v", domainName, err)
		}
		for varName, v := range domain.Variables {
			entry, err := buildVariable(mms.DomainID(domainName), mms.ItemID(varName), v)
			if err != nil {
				t.Fatalf("build variable %s/%s: %v", domainName, varName, err)
			}
			if err := srv.RegisterVariable(entry); err != nil {
				t.Fatalf("RegisterVariable %s/%s: %v", domainName, varName, err)
			}
		}
	}

	ln, err := iso.Listen("0.0.0.0:0")
	if err != nil {
		t.Fatalf("iso.Listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.ListenAndServe(ctx, ln) }()
	t.Cleanup(cancel)

	return &goMmsServer{port: port}
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
			compSpec, err := parseScalarTypeSpec(ct, 0)
			if err != nil {
				return mms.TypeSpec{}, nil, fmt.Errorf("structure[%d] type: %w", i, err)
			}
			tsElems = append(tsElems, mms.TypeSpecElement{Type: compSpec})
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
