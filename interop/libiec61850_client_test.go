//go:build interop

// SPDX-License-Identifier: MIT

// Tests in this file cover the go-mms client direction:
//
//	go-mms client → libiec61850-mms-server
//
// The adapter container is started and torn down by each test.
package interop

import (
	"context"
	"errors"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"
)

func TestClient_Identify(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	id, err := c.Identify(ctx)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}
	t.Logf("Identify: vendor=%q model=%q revision=%q", id.Vendor, id.Model, id.Revision)
	if id.Vendor == "" {
		t.Error("expected non-empty vendor")
	}
	if id.Model == "" {
		t.Error("expected non-empty model")
	}
}

func TestClient_GetNameList_Domains(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	result, err := c.GetNameList(ctx, mms.NameListRequest{
		ObjectClass: mms.ObjectClassDomain,
		Scope:       mms.ObjectScopeVMD,
	})
	if err != nil {
		t.Fatalf("GetNameList (domains): %v", err)
	}
	found := false
	for _, n := range result.Names {
		if n == "interop" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected domain 'interop' in %v", result.Names)
	}
}

func TestClient_GetNameList_Variables(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	result, err := c.GetNameList(ctx, mms.NameListRequest{
		ObjectClass: mms.ObjectClassNamedVariable,
		Scope:       mms.ObjectScopeDomain,
		DomainID:    "interop",
	})
	if err != nil {
		t.Fatalf("GetNameList (variables): %v", err)
	}
	want := []string{"boolean", "integer", "unsigned", "float32", "visible-string",
		"octet-string", "bit-string", "utc-time", "array", "structure"}
	nameSet := make(map[string]bool, len(result.Names))
	for _, n := range result.Names {
		nameSet[n] = true
	}
	for _, w := range want {
		if !nameSet[w] {
			t.Errorf("expected variable %q in domain 'interop', got %v", w, result.Names)
		}
	}
}

func TestClient_Read_Boolean(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	rr, err := c.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "boolean"})
	if err != nil {
		t.Fatalf("Read boolean: %v", err)
	}
	b, ok := rr.Value.Bool()
	if !ok {
		t.Fatalf("expected boolean value, got type %s", rr.Value.Type())
	}
	if !b {
		t.Errorf("expected boolean=true, got false")
	}
}

func TestClient_Read_Integer(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	rr, err := c.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "integer"})
	if err != nil {
		t.Fatalf("Read integer: %v", err)
	}
	iv, ok := rr.Value.Int64()
	if !ok {
		t.Fatalf("expected integer value, got type %s", rr.Value.Type())
	}
	if iv != -123 {
		t.Errorf("expected integer=-123, got %d", iv)
	}
}

func TestClient_Read_Float32(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	rr, err := c.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "float32"})
	if err != nil {
		t.Fatalf("Read float32: %v", err)
	}
	fv, ok := rr.Value.Float64()
	if !ok {
		t.Fatalf("expected float value, got type %s", rr.Value.Type())
	}
	if fv < 21.49 || fv > 21.51 {
		t.Errorf("expected float32≈21.5, got %f", fv)
	}
}

func TestClient_Write_Float32(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	_, err := c.Write(ctx, mms.WriteRequest{
		DomainID: "interop",
		ItemID:   "float32",
		Value:    mms.NewFloat(99.0),
	})
	if err != nil {
		t.Fatalf("Write float32: %v", err)
	}
	rr, err := c.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "float32"})
	if err != nil {
		t.Fatalf("Read float32 after write: %v", err)
	}
	fv, ok := rr.Value.Float64()
	if !ok {
		t.Fatalf("expected float value, got type %s", rr.Value.Type())
	}
	if fv < 98.9 || fv > 99.1 {
		t.Errorf("expected float32≈99.0 after write, got %f", fv)
	}
}

func TestClient_Write_ReadOnly(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	rrBefore, err := c.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "octet-string"})
	if err != nil {
		t.Fatalf("pre-write read: %v", err)
	}
	originalBytes, ok := rrBefore.Value.OctetString()
	if !ok {
		t.Fatalf("expected OctetString before write, got %s", rrBefore.Value.Type())
	}

	_, writeErr := c.Write(ctx, mms.WriteRequest{
		DomainID: "interop",
		ItemID:   "octet-string",
		Value:    mms.NewOctetString([]byte{0xde, 0xad}),
	})
	if writeErr == nil {
		t.Fatal("expected error writing to read-only variable, got nil")
	}
	t.Logf("write rejected (expected): %v", writeErr)

	var dae *mms.DataAccessError
	if !errors.As(writeErr, &dae) {
		t.Errorf("expected *DataAccessError, got %T: %v", writeErr, writeErr)
	} else if dae.Code != mms.DataAccessErrorObjectAccessDenied {
		t.Errorf("error code = %s, want ObjectAccessDenied", dae.Code)
	}

	rrAfter, err := c.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "octet-string"})
	if err != nil {
		t.Fatalf("post-write read failed (server disconnected?): %v", err)
	}
	afterBytes, ok := rrAfter.Value.OctetString()
	if !ok {
		t.Fatalf("expected OctetString after failed write, got %s", rrAfter.Value.Type())
	}
	if string(afterBytes) != string(originalBytes) {
		t.Errorf("value changed after rejected write: before=%x after=%x", originalBytes, afterBytes)
	}
}

func TestClient_Reconnect(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for round := 1; round <= 2; round++ {
		c := dial(t, ctx, h.addr)
		rr, err := c.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "boolean"})
		if err != nil {
			c.Close(ctx)
			t.Fatalf("round %d: Read boolean: %v", round, err)
		}
		b, _ := rr.Value.Bool()
		t.Logf("round %d: boolean=%v", round, b)
		if err := c.Close(ctx); err != nil {
			t.Logf("round %d: Close: %v (non-fatal)", round, err)
		}
	}
}

func TestClient_Read_Unsigned(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	rr, err := c.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "unsigned"})
	if err != nil {
		t.Fatalf("Read unsigned: %v", err)
	}
	uv, ok := rr.Value.Uint64()
	if !ok {
		t.Fatalf("expected unsigned value, got type %s", rr.Value.Type())
	}
	if uv != 456 {
		t.Errorf("expected unsigned=456, got %d", uv)
	}
}

func TestClient_Read_VisibleString(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	rr, err := c.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "visible-string"})
	if err != nil {
		t.Fatalf("Read visible-string: %v", err)
	}
	sv, ok := rr.Value.VisibleString()
	if !ok {
		t.Fatalf("expected visible-string value, got type %s", rr.Value.Type())
	}
	if sv != "interop" {
		t.Errorf("expected visible-string=%q, got %q", "interop", sv)
	}
}

func TestClient_Write_VisibleString(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	_, err := c.Write(ctx, mms.WriteRequest{
		DomainID: "interop",
		ItemID:   "visible-string",
		Value:    mms.NewVisibleString("hello"),
	})
	if err != nil {
		t.Fatalf("Write visible-string: %v", err)
	}
	rr, err := c.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "visible-string"})
	if err != nil {
		t.Fatalf("Read visible-string after write: %v", err)
	}
	sv, ok := rr.Value.VisibleString()
	if !ok {
		t.Fatalf("expected visible-string, got %s", rr.Value.Type())
	}
	if sv != "hello" {
		t.Errorf("expected visible-string=%q after write, got %q", "hello", sv)
	}
}

func TestClient_Read_OctetString(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	rr, err := c.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "octet-string"})
	if err != nil {
		t.Fatalf("Read octet-string: %v", err)
	}
	bv, ok := rr.Value.OctetString()
	if !ok {
		t.Fatalf("expected octet-string value, got type %s", rr.Value.Type())
	}
	want := []byte{0xde, 0xad, 0xbe, 0xef}
	if len(bv) != len(want) {
		t.Fatalf("expected %d bytes, got %d: %x", len(want), len(bv), bv)
	}
	for i := range want {
		if bv[i] != want[i] {
			t.Errorf("byte[%d]: want %02x got %02x", i, want[i], bv[i])
		}
	}
}

func TestClient_Read_BitString(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	rr, err := c.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "bit-string"})
	if err != nil {
		t.Fatalf("Read bit-string: %v", err)
	}
	bits, ok := rr.Value.BitString()
	if !ok {
		t.Fatalf("expected bit-string value, got type %s", rr.Value.Type())
	}
	bitLen, _ := rr.Value.BitStringLength()
	if bitLen != 5 {
		t.Fatalf("expected bitLen=5, got %d", bitLen)
	}
	wantBits := []bool{true, false, true, true, false}
	for i, wb := range wantBits {
		got := (bits[i/8]>>(7-uint(i%8)))&1 == 1
		if got != wb {
			t.Errorf("bit[%d]: want %v got %v", i, wb, got)
		}
	}
}

func TestClient_Read_UTCTime(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	rr, err := c.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "utc-time"})
	if err != nil {
		t.Fatalf("Read utc-time: %v", err)
	}
	tv, ok := rr.Value.UTCTime()
	if !ok {
		t.Fatalf("expected utc-time value, got type %s", rr.Value.Type())
	}
	const wantEpochSec = int64(1704067200)
	if tv.Unix() != wantEpochSec {
		t.Errorf("expected epoch=%d, got %d (%s)", wantEpochSec, tv.Unix(), tv)
	}
}

func TestClient_Read_Array(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	rr, err := c.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "array"})
	if err != nil {
		t.Fatalf("Read array: %v", err)
	}
	elems, ok := rr.Value.ArrayElements()
	if !ok {
		t.Fatalf("expected array value, got type %s", rr.Value.Type())
	}
	want := []int64{1, 2, 3, 4, 5}
	if len(elems) != len(want) {
		t.Fatalf("expected %d elements, got %d", len(want), len(elems))
	}
	for i, e := range elems {
		iv, ok := e.Int64()
		if !ok {
			t.Errorf("element[%d]: expected integer, got %s", i, e.Type())
			continue
		}
		if iv != want[i] {
			t.Errorf("element[%d]: want %d, got %d", i, want[i], iv)
		}
	}
}

func TestClient_Read_Structure(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	rr, err := c.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "structure"})
	if err != nil {
		t.Fatalf("Read structure: %v", err)
	}
	comps, ok := rr.Value.Structure()
	if !ok {
		t.Fatalf("expected structure value, got type %s", rr.Value.Type())
	}
	if len(comps) != 2 {
		t.Fatalf("expected 2 structure components, got %d", len(comps))
	}
	b, ok := comps[0].Bool()
	if !ok {
		t.Fatalf("component[0]: expected boolean, got %s", comps[0].Type())
	}
	if !b {
		t.Errorf("component[0]: expected true")
	}
	iv, ok := comps[1].Int64()
	if !ok {
		t.Fatalf("component[1]: expected integer, got %s", comps[1].Type())
	}
	if iv != 42 {
		t.Errorf("component[1]: expected 42, got %d", iv)
	}
}

func TestClient_GetVariableAccessAttributes(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	attrs, err := c.GetVariableAccessAttributes(ctx, mms.ObjectName{
		Scope:  mms.ObjectScopeDomain,
		Domain: "interop",
		ItemID: "boolean",
	})
	if err != nil {
		t.Fatalf("GetVariableAccessAttributes boolean: %v", err)
	}
	if attrs.TypeSpec.Type != mms.ValueTypeBoolean {
		t.Errorf("expected TypeSpec.Type=Boolean, got %s", attrs.TypeSpec.Type)
	}
}

func TestClient_ReadMultiple(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	results, err := c.ReadMultiple(ctx, []mms.ObjectName{
		{Scope: mms.ObjectScopeDomain, Domain: "interop", ItemID: "boolean"},
		{Scope: mms.ObjectScopeDomain, Domain: "interop", ItemID: "integer"},
	})
	if err != nil {
		t.Fatalf("ReadMultiple: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	b, ok := results[0].Value.Bool()
	if !ok {
		t.Fatalf("result[0]: expected boolean, got %s", results[0].Value.Type())
	}
	if !b {
		t.Errorf("result[0]: expected boolean=true")
	}
	iv, ok := results[1].Value.Int64()
	if !ok {
		t.Fatalf("result[1]: expected integer, got %s", results[1].Value.Type())
	}
	if iv != -123 {
		t.Errorf("result[1]: expected integer=-123, got %d", iv)
	}
}

func TestClient_WriteMultiple(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	results, err := c.WriteVariables(ctx,
		[]mms.VariableSpec{
			{Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "interop", ItemID: "boolean"}},
			{Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "interop", ItemID: "integer"}},
		},
		[]*mms.Value{mms.NewBoolean(false), mms.NewInteger(0)},
	)
	if err != nil {
		t.Fatalf("WriteVariables: %v", err)
	}
	for i, r := range results {
		if !r.Success {
			t.Errorf("WriteVariables[%d]: failed with %s", i, r.ErrorCode)
		}
	}
	rr, err := c.ReadMultiple(ctx, []mms.ObjectName{
		{Scope: mms.ObjectScopeDomain, Domain: "interop", ItemID: "boolean"},
		{Scope: mms.ObjectScopeDomain, Domain: "interop", ItemID: "integer"},
	})
	if err != nil {
		t.Fatalf("ReadMultiple after WriteVariables: %v", err)
	}
	b, _ := rr[0].Value.Bool()
	if b {
		t.Errorf("expected boolean=false after write, got true")
	}
	iv, _ := rr[1].Value.Int64()
	if iv != 0 {
		t.Errorf("expected integer=0 after write, got %d", iv)
	}
}

func TestClient_NVL(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	listName := mms.ObjectName{
		Scope:  mms.ObjectScopeDomain,
		Domain: "interop",
		ItemID: "testlist",
	}
	err := c.DefineNamedVariableList(ctx, mms.DefineNamedVariableListRequest{
		ListName: listName,
		Variables: []mms.VariableSpec{
			{Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "interop", ItemID: "boolean"}},
			{Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "interop", ItemID: "integer"}},
		},
	})
	if err != nil {
		t.Fatalf("DefineNamedVariableList: %v", err)
	}

	results, err := c.ReadNamedVariableList(ctx, listName)
	if err != nil {
		t.Fatalf("ReadNamedVariableList: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 NVL results, got %d", len(results))
	}
	b, ok := results[0].Value.Bool()
	if !ok {
		t.Fatalf("NVL[0]: expected boolean, got %s", results[0].Value.Type())
	}
	if !b {
		t.Errorf("NVL[0]: expected boolean=true")
	}
	iv, ok := results[1].Value.Int64()
	if !ok {
		t.Fatalf("NVL[1]: expected integer, got %s", results[1].Value.Type())
	}
	if iv != -123 {
		t.Errorf("NVL[1]: expected integer=-123, got %d", iv)
	}

	res, err := c.DeleteNamedVariableList(ctx, []mms.ObjectName{listName})
	if err != nil {
		t.Fatalf("DeleteNamedVariableList: %v", err)
	}
	if res.NumberDeleted != 1 {
		t.Errorf("expected NumberDeleted=1, got %d", res.NumberDeleted)
	}
}

func TestClient_Read_UnknownDomain(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	_, err := c.Read(ctx, mms.ReadRequest{DomainID: "unknown-domain", ItemID: "boolean"})
	if err == nil {
		t.Fatal("expected error reading from unknown domain, got nil")
	}
	t.Logf("Read unknown domain: %v (expected)", err)

	rr, err := c.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "boolean"})
	if err != nil {
		t.Fatalf("follow-up read after unknown-domain error: %v", err)
	}
	if _, ok := rr.Value.Bool(); !ok {
		t.Errorf("follow-up read: expected boolean value")
	}
}

func TestClient_Read_UnknownVariable(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	_, err := c.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "nonexistent"})
	if err == nil {
		t.Fatal("expected error reading unknown variable, got nil")
	}
	t.Logf("Read unknown variable: %v (expected)", err)

	rr, err := c.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "boolean"})
	if err != nil {
		t.Fatalf("follow-up read after unknown-variable error: %v", err)
	}
	if _, ok := rr.Value.Bool(); !ok {
		t.Errorf("follow-up read: expected boolean value")
	}
}

func TestClient_Write_WrongType(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	_, err := c.Write(ctx, mms.WriteRequest{
		DomainID: "interop",
		ItemID:   "integer",
		Value:    mms.NewBoolean(true),
	})
	if err == nil {
		t.Fatal("expected type error writing boolean to integer variable, got nil")
	}
	t.Logf("Write wrong type: %v (expected)", err)

	rr, err := c.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "integer"})
	if err != nil {
		t.Fatalf("follow-up read after wrong-type error: %v", err)
	}
	iv, ok := rr.Value.Int64()
	if !ok {
		t.Errorf("follow-up read: expected integer value")
	}
	if iv != -123 {
		t.Errorf("integer value changed after rejected write: got %d, want -123", iv)
	}
}

func TestClient_Write_Boolean(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	_, err := c.Write(ctx, mms.WriteRequest{
		DomainID: "interop",
		ItemID:   "boolean",
		Value:    mms.NewBoolean(false),
	})
	if err != nil {
		t.Fatalf("Write boolean=false: %v", err)
	}

	rr, err := c.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "boolean"})
	if err != nil {
		t.Fatalf("Read boolean after write: %v", err)
	}
	bv, ok := rr.Value.Bool()
	if !ok {
		t.Fatalf("expected boolean after write, got type %s", rr.Value.Type())
	}
	if bv {
		t.Errorf("boolean after write: want false, got true")
	}
}

func TestClient_Write_Integer(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	const newVal int64 = 9999
	_, err := c.Write(ctx, mms.WriteRequest{
		DomainID: "interop",
		ItemID:   "integer",
		Value:    mms.NewInteger(newVal),
	})
	if err != nil {
		t.Fatalf("Write integer=%d: %v", newVal, err)
	}

	rr, err := c.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "integer"})
	if err != nil {
		t.Fatalf("Read integer after write: %v", err)
	}
	iv, ok := rr.Value.Int64()
	if !ok {
		t.Fatalf("expected integer after write, got type %s", rr.Value.Type())
	}
	if iv != newVal {
		t.Errorf("integer after write: want %d, got %d", newVal, iv)
	}
}

func TestClient_Write_Unsigned(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	const newVal uint64 = 7777
	_, err := c.Write(ctx, mms.WriteRequest{
		DomainID: "interop",
		ItemID:   "unsigned",
		Value:    mms.NewUnsigned(newVal),
	})
	if err != nil {
		t.Fatalf("Write unsigned=%d: %v", newVal, err)
	}

	rr, err := c.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "unsigned"})
	if err != nil {
		t.Fatalf("Read unsigned after write: %v", err)
	}
	uv, ok := rr.Value.Uint64()
	if !ok {
		t.Fatalf("expected unsigned after write, got type %s", rr.Value.Type())
	}
	if uv != newVal {
		t.Errorf("unsigned after write: want %d, got %d", newVal, uv)
	}
}

func TestClient_NVL_Unknown(t *testing.T) {
	h := startAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := dial(t, ctx, h.addr)
	defer c.Close(ctx)

	// Reading a named variable list that doesn't exist should return an error.
	_, err := c.ReadNamedVariableList(ctx, mms.ObjectName{
		Scope:  mms.ObjectScopeDomain,
		Domain: "interop",
		ItemID: "nonexistent-list",
	})
	if err == nil {
		t.Fatal("expected error reading non-existent NVL, got nil")
	}
	t.Logf("ReadNVL nonexistent: %v (expected)", err)

	// The connection must still be usable after the error.
	rr, err := c.Read(ctx, mms.ReadRequest{DomainID: "interop", ItemID: "boolean"})
	if err != nil {
		t.Fatalf("follow-up read after NVL error: %v", err)
	}
	if _, ok := rr.Value.Bool(); !ok {
		t.Errorf("follow-up read: expected boolean value")
	}
}
