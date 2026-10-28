//go:build interop

// SPDX-License-Identifier: MIT

// Tests in this file cover the go-mms client direction against a stack
// that packages only IEC 61850 IED adapters (peerStack.iedOnly):
//
//	go-mms client → <stack>-ied-server
//
// The IED server serves the mms-interop IEC 61850 fixture (revision
// iec61850-v2: interop.icd and values.json) built into the image. go-mms
// reads it as plain MMS: logical device InteropLD of IED InteropIED is
// domain InteropIEDInteropLD, and data attribute LN.DO.DA with functional
// constraint FC is variable LN$FC$DO$DA. The server direction has no
// counterpart: these stacks ship no MMS client adapter.
package interop

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"
)

// iedFixtureRevision is the IEC 61850 fixture the IED adapters announce.
const iedFixtureRevision = "iec61850-v2"

const iedValuesPath = "testdata/iec61850/values.json"

type iedServerHandle struct {
	addr   string
	domain mms.DomainID // <IED name><LD inst>
}

// startIEDServer launches <stack>-ied-server and waits for its ready
// event. It skips unless the selected stack is IED-only.
func startIEDServer(t *testing.T) *iedServerHandle {
	t.Helper()
	peer := stack(t)
	if !peer.iedOnly {
		t.Skipf("%s: has an MMS contract adapter; IED server tests run only for IED-only stacks", peer.name)
	}
	port := freePort(t)

	cmdCtx, cmdCancel := context.WithCancel(context.Background())
	var cmd *exec.Cmd
	if binary := os.Getenv("IED_SERVER_BINARY"); binary != "" {
		cmd = exec.CommandContext(cmdCtx, binary, "--port", port)
	} else {
		containerName := dockerContainerName(t)
		t.Cleanup(func() { dockerStop(containerName) })
		cmd = exec.CommandContext(cmdCtx, "docker", "run", "--rm",
			"--name", containerName,
			"-p", port+":"+port,
			"--entrypoint", peer.iedServerCommand(),
			peer.image(),
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
		t.Fatalf("start IED server: %v", err)
	}
	stop := func() {
		cmdCancel()
		_ = cmd.Wait()
	}

	type iedReady struct {
		readyEvent
		IEDName string `json:"ied_name"`
	}
	ready := make(chan iedReady, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			var ev iedReady
			if json.Unmarshal(scanner.Bytes(), &ev) == nil && ev.Event == "ready" {
				ready <- ev
				break
			}
		}
		_, _ = io.Copy(io.Discard, stdout)
		close(ready)
	}()

	select {
	case ev, ok := <-ready:
		if !ok {
			stop()
			t.Fatal("IED server exited before emitting readiness event")
		}
		validateAdapterMeta(t, adapterReady{fixture: ev.Fixture, adapter: ev.Adapter, version: ev.Version},
			iedFixtureRevision, peer.name)
		if ev.IEDName != "InteropIED" {
			t.Errorf("ied_name = %q, want InteropIED", ev.IEDName)
		}
		t.Cleanup(stop)
		return &iedServerHandle{
			addr:   "127.0.0.1:" + port,
			domain: mms.DomainID(ev.IEDName + "InteropLD"),
		}
	case <-time.After(readyTimeout):
		stop()
		t.Fatal("timed out waiting for IED server readiness")
		return nil
	}
}

// dialIED starts the IED server and associates with it. The client is
// aborted at cleanup: noConclude peers never answer Close.
func dialIED(t *testing.T) (*iedServerHandle, *mms.Client, context.Context) {
	t.Helper()
	h := startIEDServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	c := dial(t, ctx, h.addr)
	t.Cleanup(func() { _ = c.Abort(context.Background()) })
	return h, c, ctx
}

func (h *iedServerHandle) name(item string) mms.ObjectName {
	return mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: h.domain, ItemID: mms.ItemID(item)}
}

// iedValues is the values.json fixture: initial data attribute values
// keyed by IEC 61850 reference (LD/LN.DO.DA).
type iedValues struct {
	Values   map[string]interface{} `json:"values"`
	Writable []string               `json:"writable"`
}

func loadIEDValues(t *testing.T) iedValues {
	t.Helper()
	b, err := os.ReadFile(iedValuesPath)
	if err != nil {
		t.Fatal(err)
	}
	var v iedValues
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%s: %v", iedValuesPath, err)
	}
	return v
}

// mmsItem maps IEC 61850 reference LD/LN.DO.DA[.sub] with functional
// constraint fc to its MMS item ID LN$FC$DO$DA[$sub].
func mmsItem(t *testing.T, ref, fc string) string {
	t.Helper()
	_, rest, ok := strings.Cut(ref, "/")
	parts := strings.Split(rest, ".")
	if !ok || len(parts) < 3 {
		t.Fatalf("bad reference %q", ref)
	}
	return strings.Join(append([]string{parts[0], fc}, parts[1:]...), "$")
}

// seededAttributes are the values.json references the IED adapters set
// at start, with their functional constraint. GGIO1.DPS1.stVal and
// MMTR1.TotVAh.actVal are in values.json but not seeded by the
// iec61850bean v0.3.0 adapter; their types are checked in
// TestIEDServer_GetVariableAccessAttributes.
var seededAttributes = []struct{ ref, fc string }{
	{"InteropLD/LLN0.Mod.stVal", "ST"},
	{"InteropLD/LLN0.Mod.ctlModel", "CF"},
	{"InteropLD/LLN0.Mod.d", "DC"},
	{"InteropLD/LLN0.Beh.stVal", "ST"},
	{"InteropLD/GGIO1.SPS1.stVal", "ST"},
	{"InteropLD/GGIO1.SPS1.d", "DC"},
	{"InteropLD/GGIO1.SPCSO1.stVal", "ST"},
	{"InteropLD/GGIO1.SPCSO1.ctlModel", "CF"},
	{"InteropLD/GGIO1.SPCSO2.stVal", "ST"},
	{"InteropLD/GGIO1.SPCSO2.ctlModel", "CF"},
	{"InteropLD/GGIO1.SPCSO3.stVal", "ST"},
	{"InteropLD/GGIO1.SPCSO3.ctlModel", "CF"},
	{"InteropLD/MMXU1.TotW.mag.f", "MX"},
	{"InteropLD/GGIO1.SetInt1.setVal", "SP"},
}

// valueMatches compares a read value with a values.json entry.
func valueMatches(v *mms.Value, want interface{}) bool {
	switch w := want.(type) {
	case bool:
		got, ok := v.Bool()
		return ok && got == w
	case string:
		got, ok := v.VisibleString()
		return ok && got == w
	case float64:
		if got, ok := v.Int64(); ok {
			return float64(got) == w
		}
		if got, ok := v.Float32(); ok {
			return float64(got) == w
		}
	}
	return false
}

func TestIEDServer_Associate(t *testing.T) {
	_, c, _ := dialIED(t)
	n := c.Negotiated()
	t.Logf("negotiated: %+v", n)
	if n.MaxPDUSize <= 0 || n.ServerVersion < 1 || n.MaxOutCalling < 1 || n.MaxOutCalled < 1 {
		t.Errorf("negotiated parameters = %+v", n)
	}
}

func TestIEDServer_GetNameList_Domains(t *testing.T) {
	h, c, ctx := dialIED(t)
	names, err := c.GetNameListAll(ctx, mms.NameListRequest{ObjectClass: mms.ObjectClassDomain})
	if err != nil {
		t.Fatalf("GetNameListAll (domains): %v", err)
	}
	if len(names) != 1 || names[0] != string(h.domain) {
		t.Errorf("domains = %v, want [%s]", names, h.domain)
	}
}

func TestIEDServer_GetNameList_Variables(t *testing.T) {
	h, c, ctx := dialIED(t)
	names, err := c.GetNameListAll(ctx, mms.NameListRequest{
		ObjectClass: mms.ObjectClassNamedVariable, Scope: mms.ObjectScopeDomain, DomainID: h.domain,
	})
	if err != nil {
		t.Fatalf("GetNameListAll (variables): %v", err)
	}
	have := make(map[string]bool, len(names))
	for _, n := range names {
		have[n] = true
	}
	// Every logical node, functional constraint and data object above a
	// seeded attribute is a variable too.
	for _, a := range seededAttributes {
		parts := strings.Split(mmsItem(t, a.ref, a.fc), "$")
		for i := range parts {
			if prefix := strings.Join(parts[:i+1], "$"); !have[prefix] {
				t.Errorf("variable %q missing from %d names", prefix, len(names))
			}
		}
	}
}

func TestIEDServer_DataSet(t *testing.T) {
	h, c, ctx := dialIED(t)
	lists, err := c.GetNameListAll(ctx, mms.NameListRequest{
		ObjectClass: mms.ObjectClassNamedVariableList, Scope: mms.ObjectScopeDomain, DomainID: h.domain,
	})
	if err != nil {
		t.Fatalf("GetNameListAll (data sets): %v", err)
	}
	if len(lists) != 1 || lists[0] != "LLN0$dsInterop" {
		t.Fatalf("data sets = %v, want [LLN0$dsInterop]", lists)
	}
	attrs, err := c.GetNamedVariableListAttributes(ctx, h.name("LLN0$dsInterop"))
	if err != nil {
		t.Fatalf("GetNamedVariableListAttributes: %v", err)
	}
	if attrs.Deletable {
		t.Error("fixture data set reported deletable")
	}
	var members []string
	for _, v := range attrs.Variables {
		if v.Name.Scope != mms.ObjectScopeDomain || v.Name.Domain != h.domain {
			t.Errorf("member %+v not in domain %s", v.Name, h.domain)
		}
		members = append(members, string(v.Name.ItemID))
	}
	if strings.Join(members, ",") != "GGIO1$ST$SPS1$stVal,LLN0$ST$Mod$stVal" {
		t.Errorf("members = %v", members)
	}
	// Read the members by name (the server does not read a data set by
	// its list name).
	results, err := c.ReadVariables(ctx, attrs.Variables)
	if err != nil {
		t.Fatalf("ReadVariables: %v", err)
	}
	if len(results) != 2 || !valueMatches(results[0].Value, false) || !valueMatches(results[1].Value, 1.0) {
		t.Errorf("member values = %+v", results)
	}
}

func TestIEDServer_GetVariableAccessAttributes(t *testing.T) {
	h, c, ctx := dialIED(t)
	leaves := []struct {
		item string
		typ  mms.ValueType
		size int
	}{
		{"LLN0$ST$Mod$stVal", mms.ValueTypeInteger, 32},       // ENUMERATED as INT32
		{"GGIO1$ST$SPS1$stVal", mms.ValueTypeBoolean, 0},      // BOOLEAN
		{"GGIO1$ST$DPS1$stVal", mms.ValueTypeBitString, -2},   // Dbpos: BIT STRING (SIZE(2))
		{"MMTR1$ST$TotVAh$actVal", mms.ValueTypeUnsigned, 32}, // INT32U
		{"GGIO1$SP$SetInt1$setVal", mms.ValueTypeInteger, 32}, // INT32
	}
	for _, l := range leaves {
		a, err := c.GetVariableAccessAttributes(ctx, h.name(l.item))
		if err != nil {
			t.Errorf("GetVariableAccessAttributes(%s): %v", l.item, err)
			continue
		}
		if a.Deletable || a.TypeSpec.Type != l.typ || a.TypeSpec.Size != l.size {
			t.Errorf("%s: deletable=%v type=%v size=%d, want %v size %d",
				l.item, a.Deletable, a.TypeSpec.Type, a.TypeSpec.Size, l.typ, l.size)
		}
	}

	a, err := c.GetVariableAccessAttributes(ctx, h.name("MMXU1$MX$TotW$mag$f"))
	if err != nil {
		t.Fatalf("GetVariableAccessAttributes(TotW.mag.f): %v", err)
	}
	if a.TypeSpec.Type != mms.ValueTypeFloat || a.TypeSpec.FormatWidth != 32 || a.TypeSpec.ExponentWidth != 8 {
		t.Errorf("TotW.mag.f type = %+v, want FLOAT32", a.TypeSpec)
	}

	// A data object is a structure of its attributes: SPS[ST] is
	// {stVal BOOLEAN, q Quality, t Timestamp}.
	a, err = c.GetVariableAccessAttributes(ctx, h.name("GGIO1$ST$SPS1"))
	if err != nil {
		t.Fatalf("GetVariableAccessAttributes(SPS1): %v", err)
	}
	want := []struct {
		name string
		typ  mms.ValueType
	}{{"stVal", mms.ValueTypeBoolean}, {"q", mms.ValueTypeBitString}, {"t", mms.ValueTypeUTCTime}}
	if a.TypeSpec.Type != mms.ValueTypeStructure || len(a.TypeSpec.Elements) != len(want) {
		t.Fatalf("SPS1 type = %+v", a.TypeSpec)
	}
	for i, w := range want {
		if e := a.TypeSpec.Elements[i]; e.Name != w.name || e.Type.Type != w.typ {
			t.Errorf("SPS1 element %d = %s %v, want %s %v", i, e.Name, e.Type.Type, w.name, w.typ)
		}
	}
	if a.TypeSpec.Elements[1].Type.Size != -13 {
		t.Errorf("q size = %d, want -13 (BIT STRING (SIZE(13)))", a.TypeSpec.Elements[1].Type.Size)
	}
}

func TestIEDServer_Read_FixtureValues(t *testing.T) {
	h, c, ctx := dialIED(t)
	values := loadIEDValues(t)
	for _, a := range seededAttributes {
		want, ok := values.Values[a.ref]
		if !ok {
			t.Errorf("%s not in %s", a.ref, iedValuesPath)
			continue
		}
		item := mmsItem(t, a.ref, a.fc)
		r, err := c.Read(ctx, mms.ReadRequest{DomainID: h.domain, ItemID: mms.ItemID(item)})
		if err != nil {
			t.Errorf("Read(%s): %v", item, err)
			continue
		}
		if !valueMatches(r.Value, want) {
			t.Errorf("%s = %v, want %v", item, r.Value, want)
		}
	}

	// Dbpos is a 2-bit BIT STRING whatever its value.
	r, err := c.ReadObject(ctx, h.name("GGIO1$ST$DPS1$stVal"))
	if err != nil {
		t.Fatalf("Read(DPS1.stVal): %v", err)
	}
	if n, ok := r.Value.BitStringLength(); !ok || n != 2 {
		t.Errorf("DPS1.stVal = %v, want a 2-bit BIT STRING", r.Value)
	}
}

func TestIEDServer_ReadMultiple(t *testing.T) {
	h, c, ctx := dialIED(t)
	results, err := c.ReadMultiple(ctx, []mms.ObjectName{
		h.name("LLN0$ST$Mod$stVal"), h.name("MMXU1$MX$TotW$mag$f"), h.name("LLN0$DC$Mod$d"),
	})
	if err != nil {
		t.Fatalf("ReadMultiple: %v", err)
	}
	want := []interface{}{1.0, 1234.5, "mode"}
	if len(results) != len(want) {
		t.Fatalf("ReadMultiple returned %d results", len(results))
	}
	for i, w := range want {
		if results[i].ErrorCode != mms.DataAccessErrorNone || !valueMatches(results[i].Value, w) {
			t.Errorf("result %d = %+v, want %v", i, results[i], w)
		}
	}
}

func TestIEDServer_Read_Structure(t *testing.T) {
	h, c, ctx := dialIED(t)
	r, err := c.ReadObject(ctx, h.name("GGIO1$ST$SPS1"))
	if err != nil {
		t.Fatalf("Read(SPS1): %v", err)
	}
	fields, ok := r.Value.Structure()
	if !ok || len(fields) != 3 {
		t.Fatalf("SPS1 = %v, want a 3-member structure", r.Value)
	}
	if !valueMatches(fields[0], false) {
		t.Errorf("SPS1.stVal = %v, want false", fields[0])
	}
	if n, ok := fields[1].BitStringLength(); !ok || n != 13 {
		t.Errorf("SPS1.q = %v, want a 13-bit quality", fields[1])
	}
	if _, ok := fields[2].UTCTime(); !ok {
		t.Errorf("SPS1.t = %v, want a UtcTime", fields[2])
	}
}

func TestIEDServer_Write_SetPoint(t *testing.T) {
	h, c, ctx := dialIED(t)
	values := loadIEDValues(t)
	ref := "InteropLD/GGIO1.SetInt1.setVal"
	writable := false
	for _, w := range values.Writable {
		writable = writable || w == ref
	}
	if !writable {
		t.Fatalf("%s is not writable in %s", ref, iedValuesPath)
	}
	item := mms.ItemID(mmsItem(t, ref, "SP"))
	if _, err := c.Write(ctx, mms.WriteRequest{DomainID: h.domain, ItemID: item, Value: mms.NewInteger(77)}); err != nil {
		t.Fatalf("Write(%s): %v", item, err)
	}
	r, err := c.Read(ctx, mms.ReadRequest{DomainID: h.domain, ItemID: item})
	if err != nil {
		t.Fatalf("Read(%s): %v", item, err)
	}
	if !valueMatches(r.Value, 77.0) {
		t.Errorf("%s = %v after write, want 77", item, r.Value)
	}
}

// A status attribute (FC ST) is read-only: the write is refused with a
// data access error and the value is unchanged.
func TestIEDServer_Write_StatusRefused(t *testing.T) {
	h, c, ctx := dialIED(t)
	item := mms.ItemID("GGIO1$ST$SPS1$stVal")
	_, err := c.Write(ctx, mms.WriteRequest{DomainID: h.domain, ItemID: item, Value: mms.NewBoolean(true)})
	var dae *mms.DataAccessError
	if !errors.As(err, &dae) || dae.Code != mms.DataAccessErrorObjectAccessDenied || !errors.Is(err, mms.ErrDataAccess) {
		t.Fatalf("Write(%s) = %v, want DataAccessError object-access-denied", item, err)
	}
	r, err := c.Read(ctx, mms.ReadRequest{DomainID: h.domain, ItemID: item})
	if err != nil {
		t.Fatalf("Read after refused write: %v", err)
	}
	if !valueMatches(r.Value, false) {
		t.Errorf("%s = %v after refused write, want false", item, r.Value)
	}
}

// A request the peer never answers ends at the caller's context deadline
// and leaves the association usable.
func TestIEDServer_UnansweredRequest(t *testing.T) {
	h, c, ctx := dialIED(t)
	if !stack(t).quirks.noIdentify {
		t.Skipf("%s answers Identify", stack(t).name)
	}
	ictx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	start := time.Now()
	_, err := c.Identify(ictx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Identify = %v, want context.DeadlineExceeded", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("Identify returned after %v, want about 2s", d)
	}
	r, err := c.ReadObject(ctx, h.name("LLN0$ST$Mod$stVal"))
	if err != nil || !valueMatches(r.Value, 1.0) {
		t.Errorf("Read after unanswered Identify = %v, %v", r, err)
	}
}

// A read of a name the server does not have fails; a peer that drops the
// connection for it is reported through Done and Err.
func TestIEDServer_Read_UnknownVariable(t *testing.T) {
	h, c, ctx := dialIED(t)
	_, err := c.ReadObject(ctx, h.name("GGIO1$ST$Nope$stVal"))
	if err == nil {
		t.Fatal("Read of an unknown variable succeeded")
	}
	if !stack(t).quirks.disconnectOnUnknownName {
		var dae *mms.DataAccessError
		if !errors.As(err, &dae) {
			t.Errorf("Read = %v, want a DataAccessError", err)
		}
		return
	}
	if !errors.Is(err, mms.ErrClosed) {
		t.Errorf("Read = %v, want ErrClosed", err)
	}
	select {
	case <-c.Done():
		if !errors.Is(c.Err(), mms.ErrClosed) {
			t.Errorf("Err = %v, want ErrClosed", c.Err())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Done not closed after the peer disconnected")
	}
	// The server keeps serving new associations.
	c2 := dial(t, ctx, h.addr)
	defer func() { _ = c2.Abort(context.Background()) }()
	if r, err := c2.ReadObject(ctx, h.name("LLN0$ST$Mod$stVal")); err != nil || !valueMatches(r.Value, 1.0) {
		t.Errorf("Read on a new association = %v, %v", r, err)
	}
}

func TestIEDServer_Abort(t *testing.T) {
	_, c, ctx := dialIED(t)
	if err := c.Abort(ctx); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done not closed after Abort")
	}
	if err := c.Abort(ctx); err != nil {
		t.Errorf("second Abort = %v, want nil", err)
	}
}

// Close sends Conclude. A peer that never answers it bounds Close by its
// context, and the transport is closed either way.
func TestIEDServer_Close(t *testing.T) {
	_, c, ctx := dialIED(t)
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	start := time.Now()
	err := c.Close(cctx)
	if stack(t).quirks.noConclude {
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Close = %v, want context.DeadlineExceeded", err)
		}
	} else if err != nil {
		t.Errorf("Close: %v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("Close returned after %v", d)
	}
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done not closed after Close")
	}
	if err := c.Close(ctx); err != nil {
		t.Errorf("second Close = %v, want nil", err)
	}
}
