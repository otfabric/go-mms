//go:build interop

// SPDX-License-Identifier: MIT

// Tests in this file cover the go-mms server direction:
//
//	libiec61850-mms-client → go-mms server
//
// The go-mms server is started in-process. The libiec61850 MMS client adapter
// runs in a container (or local binary). Its JSON Lines stdout is collected and
// asserted by the test.
package interop

import (
	"fmt"
	"testing"
)

func TestServer_Identify(t *testing.T) {
	srv := startGoMmsServer(t)
	results := collectResults(t, startClientAdapter(t, srv.port))

	r, ok := findOp(results, "identify")
	if !ok {
		t.Fatal("no 'identify' result from client")
	}
	if !r.OK {
		t.Fatalf("identify failed: %s", r.Error)
	}
	m, ok := r.Value.(map[string]interface{})
	if !ok {
		t.Fatalf("identify value is not object: %T", r.Value)
	}
	fix, err := loadFixture(fixturePath(t))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	if v := fmt.Sprintf("%v", m["vendor"]); v != fix.Identity.Vendor {
		t.Errorf("vendor = %q, want %q", v, fix.Identity.Vendor)
	}
	if v := fmt.Sprintf("%v", m["model"]); v != fix.Identity.Model {
		t.Errorf("model = %q, want %q", v, fix.Identity.Model)
	}
}

func TestServer_GetNameList_Domains(t *testing.T) {
	srv := startGoMmsServer(t)
	results := collectResults(t, startClientAdapter(t, srv.port))

	var found bool
	for _, r := range results {
		if r.Operation == "get-name-list" && r.Target == "" {
			if !r.OK {
				t.Fatalf("get-name-list (domains) failed: %s", r.Error)
			}
			for _, n := range r.Names {
				if n == "interop" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("domain 'interop' not found in get-name-list results")
	}
}

func TestServer_GetNameList_Variables(t *testing.T) {
	srv := startGoMmsServer(t)
	results := collectResults(t, startClientAdapter(t, srv.port))

	var varNames []string
	for _, r := range results {
		if r.Operation == "get-name-list" && r.Target == "interop" {
			if !r.OK {
				t.Fatalf("get-name-list (variables) failed: %s", r.Error)
			}
			varNames = r.Names
			break
		}
	}
	if len(varNames) == 0 {
		t.Fatal("no variable names returned for domain 'interop'")
	}
	want := []string{"boolean", "integer", "float32", "unsigned", "visible-string",
		"octet-string", "bit-string", "utc-time", "array", "structure"}
	nameSet := make(map[string]bool)
	for _, n := range varNames {
		nameSet[n] = true
	}
	for _, w := range want {
		if !nameSet[w] {
			t.Errorf("expected variable %q in interop domain, got %v", w, varNames)
		}
	}
}

func TestServer_Read_Boolean(t *testing.T) {
	srv := startGoMmsServer(t)
	results := collectResults(t, startClientAdapter(t, srv.port))

	r, ok := findResult(results, "read", "interop/boolean")
	if !ok {
		t.Fatal("no 'read' result for interop/boolean")
	}
	if !r.OK {
		t.Fatalf("read boolean failed: %s", r.Error)
	}
	b, ok := r.Value.(bool)
	if !ok {
		t.Fatalf("boolean value is not bool: %T %v", r.Value, r.Value)
	}
	if !b {
		t.Errorf("expected boolean=true, got false")
	}
}

func TestServer_Read_Integer(t *testing.T) {
	srv := startGoMmsServer(t)
	results := collectResults(t, startClientAdapter(t, srv.port))

	r, ok := findResult(results, "read", "interop/integer")
	if !ok {
		t.Fatal("no 'read' result for interop/integer")
	}
	if !r.OK {
		t.Fatalf("read integer failed: %s", r.Error)
	}
	n, ok := r.Value.(float64)
	if !ok {
		t.Fatalf("integer value is not number: %T %v", r.Value, r.Value)
	}
	if int64(n) != -123 {
		t.Errorf("expected integer=-123, got %d", int64(n))
	}
}

func TestServer_Read_Float32(t *testing.T) {
	srv := startGoMmsServer(t)
	results := collectResults(t, startClientAdapter(t, srv.port))

	r, ok := findResult(results, "read", "interop/float32")
	if !ok {
		t.Fatal("no 'read' result for interop/float32")
	}
	if !r.OK {
		t.Fatalf("read float32 failed: %s", r.Error)
	}
	f, ok := r.Value.(float64)
	if !ok {
		t.Fatalf("float32 value is not number: %T %v", r.Value, r.Value)
	}
	if f < 21.4 || f > 21.6 {
		t.Errorf("expected float32≈21.5, got %f", f)
	}
}

func TestServer_Write_Float32(t *testing.T) {
	srv := startGoMmsServer(t)
	results := collectResults(t, startClientAdapter(t, srv.port))

	wr, ok := findResult(results, "write", "interop/float32")
	if !ok {
		t.Fatal("no 'write' result for interop/float32")
	}
	if !wr.OK {
		t.Fatalf("write float32 failed: %s", wr.Error)
	}
	var readbacks []clientResult
	for _, r := range results {
		if r.Operation == "read" && r.Target == "interop/float32" {
			readbacks = append(readbacks, r)
		}
	}
	if len(readbacks) < 2 {
		t.Fatalf("expected at least 2 reads for float32, got %d", len(readbacks))
	}
	f, ok := readbacks[1].Value.(float64)
	if !ok {
		t.Fatalf("readback value is not number: %T", readbacks[1].Value)
	}
	if f < 98.9 || f > 99.1 {
		t.Errorf("expected float32≈99.0 after write, got %f", f)
	}
}

func TestServer_Write_ReadOnly(t *testing.T) {
	srv := startGoMmsServer(t)
	results := collectResults(t, startClientAdapter(t, srv.port))

	wr, ok := findResult(results, "write", "interop/octet-string")
	if !ok {
		t.Fatal("no 'write' result for interop/octet-string")
	}
	if wr.OK {
		t.Fatal("expected write to read-only variable to fail, but ok=true")
	}
	if wr.Error != "object-access-denied" {
		t.Errorf("write error = %q, want %q", wr.Error, "object-access-denied")
	}
	t.Logf("write correctly rejected: %s", wr.Error)

	wrIdx := -1
	for i, r := range results {
		if r.Operation == "write" && r.Target == "interop/octet-string" {
			wrIdx = i
		}
	}
	var postReject bool
	for i, r := range results {
		if i > wrIdx && r.Operation == "read" && r.OK {
			postReject = true
			break
		}
	}
	if !postReject {
		t.Error("server appears disconnected after rejected write: no successful read found afterward")
	}
}

func TestServer_Conclude(t *testing.T) {
	srv := startGoMmsServer(t)
	results := collectResults(t, startClientAdapter(t, srv.port))

	r, ok := findOp(results, "conclude")
	if !ok {
		t.Fatal("no 'conclude' result from client")
	}
	if !r.OK {
		t.Fatalf("conclude failed: %s", r.Error)
	}
}

func TestServer_Reconnect(t *testing.T) {
	srv := startGoMmsServer(t)
	results := collectResults(t, startClientAdapter(t, srv.port))

	var concludes int
	for _, r := range results {
		if r.Operation == "conclude" {
			concludes++
		}
	}
	if concludes < 2 {
		t.Errorf("expected at least 2 conclude operations (reconnect), got %d", concludes)
	}
}

func TestServer_Read_Unsigned(t *testing.T) {
	srv := startGoMmsServer(t)
	results := collectResults(t, startClientAdapter(t, srv.port))

	r, ok := findResult(results, "read", "interop/unsigned")
	if !ok {
		t.Fatal("no 'read' result for interop/unsigned")
	}
	if !r.OK {
		t.Fatalf("read unsigned failed: %s", r.Error)
	}
	n, ok := r.Value.(float64)
	if !ok {
		t.Fatalf("unsigned value is not number: %T", r.Value)
	}
	if uint64(n) != 456 {
		t.Errorf("expected unsigned=456, got %d", uint64(n))
	}
}

func TestServer_Read_VisibleString(t *testing.T) {
	srv := startGoMmsServer(t)
	results := collectResults(t, startClientAdapter(t, srv.port))

	var reads []clientResult
	for _, r := range results {
		if r.Operation == "read" && r.Target == "interop/visible-string" {
			reads = append(reads, r)
		}
	}
	if len(reads) == 0 {
		t.Fatal("no 'read' result for interop/visible-string")
	}
	if !reads[0].OK {
		t.Fatalf("read visible-string failed: %s", reads[0].Error)
	}
	s, ok := reads[0].Value.(string)
	if !ok {
		t.Fatalf("visible-string value is not string: %T", reads[0].Value)
	}
	if s != "interop" {
		t.Errorf("expected visible-string=%q, got %q", "interop", s)
	}
}

func TestServer_Write_VisibleString(t *testing.T) {
	srv := startGoMmsServer(t)
	results := collectResults(t, startClientAdapter(t, srv.port))

	wr, ok := findResult(results, "write", "interop/visible-string")
	if !ok {
		t.Fatal("no 'write' result for interop/visible-string")
	}
	if !wr.OK {
		t.Fatalf("write visible-string failed: %s", wr.Error)
	}
	var reads []clientResult
	for _, r := range results {
		if r.Operation == "read" && r.Target == "interop/visible-string" {
			reads = append(reads, r)
		}
	}
	if len(reads) < 2 {
		t.Fatalf("expected ≥2 reads for visible-string, got %d", len(reads))
	}
	s, ok := reads[1].Value.(string)
	if !ok {
		t.Fatalf("readback value is not string: %T", reads[1].Value)
	}
	if s != "hello" {
		t.Errorf("expected visible-string=%q after write, got %q", "hello", s)
	}
}

func TestServer_Read_OctetString(t *testing.T) {
	srv := startGoMmsServer(t)
	results := collectResults(t, startClientAdapter(t, srv.port))

	r, ok := findResult(results, "read", "interop/octet-string")
	if !ok {
		t.Fatal("no 'read' result for interop/octet-string")
	}
	if !r.OK {
		t.Fatalf("read octet-string failed: %s", r.Error)
	}
	s, ok := r.Value.(string)
	if !ok {
		t.Fatalf("octet-string value is not string: %T", r.Value)
	}
	if s != "deadbeef" {
		t.Errorf("expected octet-string=%q, got %q", "deadbeef", s)
	}
}

func TestServer_Read_BitString(t *testing.T) {
	srv := startGoMmsServer(t)
	results := collectResults(t, startClientAdapter(t, srv.port))

	r, ok := findResult(results, "read", "interop/bit-string")
	if !ok {
		t.Fatal("no 'read' result for interop/bit-string")
	}
	if !r.OK {
		t.Fatalf("read bit-string failed: %s", r.Error)
	}
	s, ok := r.Value.(string)
	if !ok {
		t.Fatalf("bit-string value is not string: %T", r.Value)
	}
	if s != "10110" {
		t.Errorf("expected bit-string=%q, got %q", "10110", s)
	}
}

func TestServer_Read_UTCTime(t *testing.T) {
	srv := startGoMmsServer(t)
	results := collectResults(t, startClientAdapter(t, srv.port))

	r, ok := findResult(results, "read", "interop/utc-time")
	if !ok {
		t.Fatal("no 'read' result for interop/utc-time")
	}
	if !r.OK {
		t.Fatalf("read utc-time failed: %s", r.Error)
	}
	ms, ok := r.Value.(float64)
	if !ok {
		t.Fatalf("utc-time value is not number: %T", r.Value)
	}
	const wantMs = float64(1704067200000)
	if ms != wantMs {
		t.Errorf("expected utc-time=%v ms, got %v", wantMs, ms)
	}
}

func TestServer_Read_Array(t *testing.T) {
	srv := startGoMmsServer(t)
	results := collectResults(t, startClientAdapter(t, srv.port))

	r, ok := findResult(results, "read", "interop/array")
	if !ok {
		t.Fatal("no 'read' result for interop/array")
	}
	if !r.OK {
		t.Fatalf("read array failed: %s", r.Error)
	}
	arr, ok := r.Value.([]interface{})
	if !ok {
		t.Fatalf("array value is not JSON array: %T", r.Value)
	}
	want := []float64{1, 2, 3, 4, 5}
	if len(arr) != len(want) {
		t.Fatalf("expected %d elements, got %d", len(want), len(arr))
	}
	for i, w := range want {
		n, ok := arr[i].(float64)
		if !ok {
			t.Errorf("element[%d]: not a number: %T", i, arr[i])
			continue
		}
		if n != w {
			t.Errorf("element[%d]: want %v got %v", i, w, n)
		}
	}
}

func TestServer_Read_Structure(t *testing.T) {
	srv := startGoMmsServer(t)
	results := collectResults(t, startClientAdapter(t, srv.port))

	r, ok := findResult(results, "read", "interop/structure")
	if !ok {
		t.Fatal("no 'read' result for interop/structure")
	}
	if !r.OK {
		t.Fatalf("read structure failed: %s", r.Error)
	}
	comps, ok := r.Value.([]interface{})
	if !ok {
		t.Fatalf("structure value is not JSON array: %T", r.Value)
	}
	if len(comps) != 2 {
		t.Fatalf("expected 2 components, got %d", len(comps))
	}
	b, ok := comps[0].(bool)
	if !ok {
		t.Fatalf("component[0]: expected bool, got %T", comps[0])
	}
	if !b {
		t.Errorf("component[0]: expected true")
	}
	n, ok := comps[1].(float64)
	if !ok {
		t.Fatalf("component[1]: expected number, got %T", comps[1])
	}
	if int64(n) != 42 {
		t.Errorf("component[1]: expected 42, got %d", int64(n))
	}
}

func TestServer_GetVariableAccessAttributes(t *testing.T) {
	srv := startGoMmsServer(t)
	results := collectResults(t, startClientAdapter(t, srv.port))

	r, ok := findResult(results, "get-var-access-attr", "interop/boolean")
	if !ok {
		t.Fatal("no 'get-var-access-attr' result for interop/boolean")
	}
	if !r.OK {
		t.Fatalf("get-var-access-attr failed: %s", r.Error)
	}
	if r.Type != "boolean" {
		t.Errorf("expected type=%q, got %q", "boolean", r.Type)
	}
}

func TestServer_ReadMultiple(t *testing.T) {
	srv := startGoMmsServer(t)
	results := collectResults(t, startClientAdapter(t, srv.port))

	r, ok := findResult(results, "read-multiple", "interop")
	if !ok {
		t.Fatal("no 'read-multiple' result for interop")
	}
	if !r.OK {
		t.Fatalf("read-multiple failed: %s", r.Error)
	}
	vals, ok := r.Values.([]interface{})
	if !ok {
		t.Fatalf("read-multiple values is not array: %T", r.Values)
	}
	if len(vals) != 2 {
		t.Fatalf("expected 2 values, got %d", len(vals))
	}
	b, ok := vals[0].(bool)
	if !ok {
		t.Fatalf("vals[0]: expected bool, got %T", vals[0])
	}
	if !b {
		t.Errorf("vals[0]: expected boolean=true")
	}
	n, ok := vals[1].(float64)
	if !ok {
		t.Fatalf("vals[1]: expected number, got %T", vals[1])
	}
	if int64(n) != -123 {
		t.Errorf("vals[1]: expected integer=-123, got %d", int64(n))
	}
}

func TestServer_WriteMultiple(t *testing.T) {
	srv := startGoMmsServer(t)
	results := collectResults(t, startClientAdapter(t, srv.port))

	r, ok := findResult(results, "write-multiple", "interop")
	if !ok {
		t.Fatal("no 'write-multiple' result for interop")
	}
	if !r.OK {
		t.Fatalf("write-multiple failed: %s", r.Error)
	}
}

func TestServer_NVL(t *testing.T) {
	srv := startGoMmsServer(t)
	results := collectResults(t, startClientAdapter(t, srv.port))

	dr, ok := findResult(results, "define-nvl", "interop/testlist")
	if !ok {
		t.Fatal("no 'define-nvl' result for interop/testlist")
	}
	if !dr.OK {
		t.Fatalf("define-nvl failed: %s", dr.Error)
	}

	rr, ok := findResult(results, "read-nvl", "interop/testlist")
	if !ok {
		t.Fatal("no 'read-nvl' result for interop/testlist")
	}
	if !rr.OK {
		t.Fatalf("read-nvl failed: %s", rr.Error)
	}
	vals, ok := rr.Values.([]interface{})
	if !ok {
		t.Fatalf("read-nvl values is not array: %T", rr.Values)
	}
	if len(vals) != 2 {
		t.Fatalf("expected 2 NVL values, got %d", len(vals))
	}

	xr, ok := findResult(results, "delete-nvl", "interop/testlist")
	if !ok {
		t.Fatal("no 'delete-nvl' result for interop/testlist")
	}
	if !xr.OK {
		t.Fatalf("delete-nvl failed: %s", xr.Error)
	}
}

func TestServer_Read_UnknownDomain(t *testing.T) {
	srv := startGoMmsServer(t)
	results := collectResults(t, startClientAdapter(t, srv.port))

	r, ok := findResult(results, "read", "unknown-domain/boolean")
	if !ok {
		t.Fatal("no 'read' result for unknown-domain/boolean")
	}
	if r.OK {
		t.Fatal("expected read from unknown domain to fail, got ok=true")
	}
	t.Logf("read unknown domain: %s (expected)", r.Error)
}

func TestServer_Read_UnknownVariable(t *testing.T) {
	srv := startGoMmsServer(t)
	results := collectResults(t, startClientAdapter(t, srv.port))

	r, ok := findResult(results, "read", "interop/nonexistent")
	if !ok {
		t.Fatal("no 'read' result for interop/nonexistent")
	}
	if r.OK {
		t.Fatal("expected read of unknown variable to fail, got ok=true")
	}
	t.Logf("read unknown variable: %s (expected)", r.Error)
}

func TestServer_Write_WrongType(t *testing.T) {
	srv := startGoMmsServer(t)
	results := collectResults(t, startClientAdapter(t, srv.port))

	r, ok := findResult(results, "write", "interop/integer-wrong-type")
	if !ok {
		t.Fatal("no 'write' result for interop/integer-wrong-type")
	}
	if r.OK {
		t.Fatal("expected wrong-type write to fail, got ok=true")
	}
	t.Logf("write wrong type: %s (expected)", r.Error)
}
