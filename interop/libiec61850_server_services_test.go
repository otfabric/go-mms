//go:build interop

// SPDX-License-Identifier: MIT

// Tests in this file extend the go-mms server direction
//
//	libiec61850-mms-client → go-mms server
//
// to status, journals and VMD name lists, alternate access, named variable
// lists in every scope, file services, journals, InformationReport /
// UnsolicitedStatus delivery and abort. All tests assert on the shared
// client adapter run (see sharedClientRun).
package interop

import (
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	mms "github.com/otfabric/go-mms"
)

// okResult returns the result for operation/target and fails the test if
// it is missing or reports an error.
func okResult(t *testing.T, op, target string) clientResult {
	t.Helper()
	r, ok := findResult(sharedClientRun(t).results, op, target)
	if !ok {
		t.Fatalf("no %q result for target %q from client", op, target)
	}
	skipUnsupported(t, r)
	if !r.OK {
		t.Fatalf("%s %q failed: %s", op, target, r.Error)
	}
	return r
}

// failResult returns the error for operation/target and fails the test if
// the operation unexpectedly succeeded.
func failResult(t *testing.T, op, target string) string {
	t.Helper()
	r, ok := findResult(sharedClientRun(t).results, op, target)
	if !ok {
		t.Fatalf("no %q result for target %q from client", op, target)
	}
	skipUnsupported(t, r)
	if r.OK {
		t.Fatalf("%s %q succeeded, want failure", op, target)
	}
	return r.Error
}

func asObject(t *testing.T, v interface{}) map[string]interface{} {
	t.Helper()
	m, ok := v.(map[string]interface{})
	if !ok {
		t.Fatalf("value is %T, want object", v)
	}
	return m
}

func asList(t *testing.T, v interface{}) []interface{} {
	t.Helper()
	l, ok := v.([]interface{})
	if !ok {
		t.Fatalf("value is %T, want array", v)
	}
	return l
}

func assertJSON(t *testing.T, what string, got, want interface{}) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func fnv1a64(data []byte) string {
	h := uint64(0xcbf29ce484222325)
	for _, b := range data {
		h ^= uint64(b)
		h *= 0x100000001b3
	}
	return fmt.Sprintf("%016x", h)
}

// ---------------------------------------------------------------------------
// VMD services
// ---------------------------------------------------------------------------

func TestServer_Status(t *testing.T) {
	fix := mustLoadFixture(t)
	want := map[string]interface{}{"logical": float64(fix.Status.Logical), "physical": float64(fix.Status.Physical)}
	assertJSON(t, "status", okResult(t, "status", "").Value, want)
	assertJSON(t, "status (extended)", okResult(t, "status", "extended").Value, want)
}

func TestServer_GetNameList_Journals(t *testing.T) {
	assertJSON(t, "journals", okResult(t, "get-name-list", "interop/journals").Names, []string{"log1"})
}

func TestServer_GetNameList_VMDVariables(t *testing.T) {
	if names := okResult(t, "get-name-list", "vmd/variables").Names; len(names) != 0 {
		t.Errorf("VMD variables = %v, want none (fixture has domain variables only)", names)
	}
}

func TestServer_GetNameList_UnknownDomain(t *testing.T) {
	if msg := failResult(t, "get-name-list", "unknown-domain/variables"); msg != "object-non-existent" {
		t.Errorf("error = %q, want object-non-existent", msg)
	}
}

func TestServer_GetVariableAccessAttributes_Composite(t *testing.T) {
	assertJSON(t, "array-rw type", okResult(t, "get-var-access-attr", "interop/array-rw").Value,
		map[string]interface{}{"type": "array", "count": float64(4), "element": map[string]interface{}{"type": "integer"}})
	assertJSON(t, "structure-rw type", okResult(t, "get-var-access-attr", "interop/structure-rw").Value,
		map[string]interface{}{"type": "structure", "components": []interface{}{
			map[string]interface{}{"name": "flag", "spec": map[string]interface{}{"type": "boolean"}},
			map[string]interface{}{"name": "count", "spec": map[string]interface{}{"type": "integer"}},
		}})
}

// ---------------------------------------------------------------------------
// Alternate access
// ---------------------------------------------------------------------------

func TestServer_ReadArrayElements(t *testing.T) {
	assertJSON(t, "array[1:4]", okResult(t, "read-array-elements", "interop/array[1:4]").Value,
		[]interface{}{float64(2), float64(3), float64(4)})
	assertJSON(t, "array[2]", okResult(t, "read-array-elements", "interop/array[2]").Value, float64(3))
}

func TestServer_ReadComponent(t *testing.T) {
	assertJSON(t, "structure-rw$count", okResult(t, "read-component", "interop/structure-rw$count").Value, float64(7))
}

func TestServer_WriteArrayElements(t *testing.T) {
	okResult(t, "write-array-elements", "interop/array-rw[1]")
	okResult(t, "write-array-elements", "interop/array-rw[2:4]")
	assertJSON(t, "array-rw after writes", okResult(t, "read", "interop/array-rw").Value,
		[]interface{}{float64(10), float64(21), float64(33), float64(44)})
}

func TestServer_WriteComponent(t *testing.T) {
	okResult(t, "write-component", "interop/structure-rw$count")
	assertJSON(t, "structure-rw after write", okResult(t, "read", "interop/structure-rw").Value,
		[]interface{}{false, float64(99)})
}

// ---------------------------------------------------------------------------
// Named variable lists
// ---------------------------------------------------------------------------

var nvlMemberRefs = []interface{}{"interop/boolean", "interop/integer"}

// nvlWrittenValues returns interop/boolean and interop/integer after the
// client's write-nvl step: (true, 77), or (false, 0) from write-multiple
// when the peer client cannot write a list.
func nvlWrittenValues(t *testing.T) []interface{} {
	if stack(t).quirks.noWriteNVL {
		return []interface{}{false, float64(0)}
	}
	return []interface{}{true, float64(77)}
}

func TestServer_NVL_DomainScope(t *testing.T) {
	okResult(t, "define-nvl", "interop/svclist")
	assertJSON(t, "domain lists", okResult(t, "get-name-list", "interop/nvl").Names, []string{"svclist"})
	assertJSON(t, "svclist attributes", okResult(t, "get-nvl-attrs", "interop/svclist").Value,
		map[string]interface{}{"deletable": true, "variables": nvlMemberRefs})
	// specificationWithResult; values written by write-multiple earlier in the run
	assertJSON(t, "svclist (spec with result)", okResult(t, "read-nvl", "interop/svclist?spec").Value,
		[]interface{}{false, float64(0)})
	okResult(t, "write-nvl", "interop/svclist")
	assertJSON(t, "svclist after write", okResult(t, "read-nvl", "interop/svclist").Value,
		[]interface{}{true, float64(77)})
	okResult(t, "delete-nvl", "interop/svclist")
}

func TestServer_NVL_VMDScope(t *testing.T) {
	okResult(t, "define-nvl", "vmd/vmdlist")
	assertJSON(t, "vmdlist attributes", okResult(t, "get-nvl-attrs", "vmd/vmdlist").Value,
		map[string]interface{}{"deletable": true, "variables": nvlMemberRefs})
	assertJSON(t, "vmdlist", okResult(t, "read-nvl", "vmd/vmdlist").Value, nvlWrittenValues(t))
	okResult(t, "delete-nvl", "vmd/vmdlist")
}

// The list behind the fixture's named-list report is registered with
// Server.RegisterNamedVariableList and must not be deletable by clients.
func TestServer_NVL_ServerRegistered(t *testing.T) {
	fix := mustLoadFixture(t)
	// Live values, as observed by the vmdlist read immediately before.
	values := map[string]interface{}{}
	for i, v := range asList(t, okResult(t, "read-nvl", "vmd/vmdlist").Value) {
		values[nvlMemberRefs[i].(string)] = v
	}
	for _, rep := range fix.InformationReports {
		if rep.VMDList == "" {
			continue
		}
		target := "vmd/" + rep.VMDList
		refs := []interface{}{}
		want := []interface{}{}
		for _, ref := range rep.Variables {
			refs = append(refs, ref)
			want = append(want, values[ref])
		}
		assertJSON(t, target+" attributes", okResult(t, "get-nvl-attrs", target).Value,
			map[string]interface{}{"deletable": false, "variables": refs})
		assertJSON(t, target, okResult(t, "read-nvl", target).Value, want)
	}
}

func TestServer_NVL_AssociationScope(t *testing.T) {
	okResult(t, "define-nvl", "aa/aalist")
	assertJSON(t, "association lists", okResult(t, "get-name-list", "aa/nvl").Names, []string{"aalist"})
	assertJSON(t, "aalist attributes", okResult(t, "get-nvl-attrs", "aa/aalist").Value,
		map[string]interface{}{"deletable": true, "variables": nvlMemberRefs})
	assertJSON(t, "aalist", okResult(t, "read-nvl", "aa/aalist").Value, []interface{}{true, float64(77)})
	okResult(t, "write-nvl", "aa/aalist")
	okResult(t, "delete-nvl", "aa/aalist")
}

// ---------------------------------------------------------------------------
// File services
// ---------------------------------------------------------------------------

// directoryListing converts a file-directory value to name → entry.
func directoryListing(t *testing.T, v interface{}) map[string]map[string]interface{} {
	t.Helper()
	out := map[string]map[string]interface{}{}
	for _, e := range asList(t, v) {
		m := asObject(t, e)
		out[fmt.Sprint(m["name"])] = m
	}
	return out
}

// perLevelRoot is the root listing the fixture file store serves for the
// stored names: top-level files plus one "<dir>\" entry per subdirectory.
func perLevelRoot(names []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, n := range names {
		if dir, _, nested := strings.Cut(n, "/"); nested {
			n = dir + `\`
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Regression for go-mms issue #2 in the server direction: libiec61850
// decodes listOfDirectoryEntry only in the explicit form. The listing
// also carries a subdirectory entry (zero size, name ending in '\', no
// lastModified) and a file without lastModified; the libiec61850 client
// must take both as sent.
func TestServer_FileDirectory(t *testing.T) {
	fix := mustLoadFixture(t)
	got := directoryListing(t, okResult(t, "file-directory", "").Value)
	assertJSON(t, "root listing", sortedKeys(got), perLevelRoot(sortedKeys(fix.Files)))
	for name, e := range got {
		if strings.HasSuffix(name, `\`) {
			assertJSON(t, name+" size", e["size"], float64(0))
			assertJSON(t, name+" lastModified", e["lastModified"], float64(0))
			continue
		}
		data, stored := fix.file(t, name)
		assertJSON(t, name+" size", e["size"], float64(len(data)))
		assertJSON(t, name+" lastModified", e["lastModified"], float64(stored.modMs()))
	}
}

func TestServer_FileDirectory_Subdirectory(t *testing.T) {
	for _, spec := range []string{"COMTRADE", `COMTRADE\`} {
		got := directoryListing(t, okResult(t, "file-directory", spec).Value)
		if len(got) != 1 || got["COMTRADE/REC02.CFG"] == nil {
			t.Errorf("%s listing = %v, want [COMTRADE/REC02.CFG]", spec, got)
		}
	}
}

// The server omits lastModified from the FileOpen response when the
// provider has no time, and the libiec61850 client must accept that.
// The value it reports is not asserted: libiec61850 leaves its
// lastModified uninitialised when the field is absent (FileOpen response
// handler in mms_client_connection.c). The omission on the wire is
// asserted by e2e TestClient_FileDirectory_DeviceConventions.
func TestServer_FileOpen_NoLastModified(t *testing.T) {
	data, stored := mustLoadFixture(t).file(t, "NOTIME.TXT")
	if stored.LastModified != "" {
		t.Fatal("fixture NOTIME.TXT must not have lastModified")
	}
	m := asObject(t, okResult(t, "file-read", "NOTIME.TXT").Value)
	assertJSON(t, "size", m["size"], float64(len(data)))
	assertJSON(t, "data", m["data"], hex.EncodeToString(data))
	okResult(t, "file-close", "NOTIME.TXT")
}

func assertFileRead(t *testing.T, target string, want []byte, size int, modMs int64) {
	t.Helper()
	m := asObject(t, okResult(t, "file-read", target).Value)
	assertJSON(t, target+" size", m["size"], float64(size))
	if stack(t).quirks.fileOpenNoLastModified {
		modMs = 0
	}
	assertJSON(t, target+" lastModified", m["lastModified"], float64(modMs))
	assertJSON(t, target+" length", m["length"], float64(len(want)))
	assertJSON(t, target+" fnv1a", m["fnv1a"], fnv1a64(want))
	if d, ok := m["data"]; ok {
		assertJSON(t, target+" data", d, hex.EncodeToString(want))
	}
	okResult(t, "file-close", target)
}

func TestServer_FileOpenReadClose(t *testing.T) {
	data, stored := mustLoadFixture(t).file(t, "REC01.CFG")
	assertFileRead(t, "REC01.CFG", data, len(data), stored.modMs())
}

func TestServer_FileOpen_InitialPosition(t *testing.T) {
	data, stored := mustLoadFixture(t).file(t, "REC01.DAT")
	assertFileRead(t, "REC01.DAT@4", data[4:], len(data), stored.modMs())
}

func TestServer_FileRead_MultiChunk(t *testing.T) {
	data, stored := mustLoadFixture(t).file(t, "LARGE.BIN")
	assertFileRead(t, "LARGE.BIN", data, len(data), stored.modMs())
}

func TestServer_FileOpen_NonExistent(t *testing.T) {
	if e := failResult(t, "file-read", "NO-SUCH.FILE"); e != "file-non-existent" {
		t.Errorf("error = %q, want file-non-existent", e)
	}
}

func TestServer_FileDelete_Rename_Obtain(t *testing.T) {
	okResult(t, "file-delete", "DELETE.ME")
	okResult(t, "file-rename", "RENAME.ME->RENAMED.ME")
	okResult(t, "obtain-file", "REC01.CFG->OBTAINED.CFG")

	fix := mustLoadFixture(t)
	var want []string
	for name := range fix.Files {
		switch name {
		case "DELETE.ME":
		case "RENAME.ME":
			want = append(want, "RENAMED.ME")
		default:
			want = append(want, name)
		}
	}
	want = append(want, "OBTAINED.CFG")
	sort.Strings(want)

	listing := directoryListing(t, okResult(t, "file-directory", "after-changes").Value)
	assertJSON(t, "directory after changes", sortedKeys(listing), perLevelRoot(want))
	assertJSON(t, "file store after run", sharedClientRun(t).files, want)
}

// ---------------------------------------------------------------------------
// Journals
// ---------------------------------------------------------------------------

func assertJournalJSON(t *testing.T, target string, want []fixtureJournalEntry) {
	t.Helper()
	m := asObject(t, okResult(t, "read-journal", target).Value)
	assertJSON(t, target+" moreFollows", m["moreFollows"], false)
	entries := asList(t, m["entries"])
	if len(entries) != len(want) {
		t.Fatalf("%s: %d entries, want %d", target, len(entries), len(want))
	}
	for i, w := range want {
		e := asObject(t, entries[i])
		assertJSON(t, fmt.Sprintf("%s[%d] id", target, i), e["id"], hex.EncodeToString(entryIDBytes(w.EntryID)))
		assertJSON(t, fmt.Sprintf("%s[%d] time", target, i), e["time"], float64(w.time(t).UnixMilli()))

		wantValue := w.Value
		if n, ok := wantValue.(float64); ok {
			wantValue = n
		}
		reason := make([]byte, 7)
		for b := 0; b < 7; b++ {
			reason[b] = '0'
			if w.ReasonCode&(1<<b) != 0 {
				reason[b] = '1'
			}
		}
		assertJSON(t, fmt.Sprintf("%s[%d] variables", target, i), e["variables"], []interface{}{
			map[string]interface{}{"tag": w.Tag, "value": wantValue},
			map[string]interface{}{"tag": "ReasonCode", "value": string(reason)},
		})
	}
}

func TestServer_ReadJournal_TimeRange(t *testing.T) {
	entries := mustLoadFixture(t).Domains["interop"].Journals["log1"].Entries
	assertJournalJSON(t, "interop/log1", entries)
}

func TestServer_ReadJournal_StartAfter(t *testing.T) {
	entries := mustLoadFixture(t).Domains["interop"].Journals["log1"].Entries
	assertJournalJSON(t, "interop/log1@after-2", entries[2:])
}

func TestServer_ReadJournal_Unknown(t *testing.T) {
	if e := failResult(t, "read-journal", "interop/nolog"); e != "object-non-existent" {
		t.Errorf("error = %q, want object-non-existent", e)
	}
}

// ---------------------------------------------------------------------------
// Unconfirmed services and association lifecycle
// ---------------------------------------------------------------------------

// Writing interop/report-trigger makes the go-mms server send every fixture
// InformationReport followed by an UnsolicitedStatus. libiec61850 delivers
// listOfVariable reports per variable and named-list reports as a whole;
// it ignores UnsolicitedStatus, which must not disturb the association.
func TestServer_InformationReport(t *testing.T) {
	okResult(t, "write", "interop/report-trigger")
	fix := mustLoadFixture(t)
	values := map[string]interface{}{"interop/integer": float64(-123), "interop/boolean": true}
	if stack(t).quirks.noWriteNVL {
		// The list write that restores the fixture values is unsupported.
		values = map[string]interface{}{"interop/integer": float64(0), "interop/boolean": false}
	}

	for _, rep := range fix.InformationReports {
		if rep.VMDList != "" {
			var want []interface{}
			for _, ref := range rep.Variables {
				want = append(want, values[ref])
			}
			assertJSON(t, "named-list report", okResult(t, "information-report", "list:"+rep.VMDList).Value, want)
			continue
		}
		for _, ref := range rep.Variables {
			assertJSON(t, "report variable "+ref, okResult(t, "information-report", "var:"+ref).Value, values[ref])
		}
	}
	okResult(t, "read", "interop/boolean#after-reports")
}

func TestServer_Abort(t *testing.T) {
	okResult(t, "read", "interop/boolean#before-abort")
	okResult(t, "abort", "")
}

// Writing reportTriggerAbort makes the go-mms server call ServerConn.Abort;
// libiec61850 must observe the association being torn down.
func TestServer_ServerConnAbort(t *testing.T) {
	okResult(t, "server-abort", "")
}

// TestServer_Shutdown checks the graceful shutdown that ends the shared
// libiec61850 client run: ListenAndServe returns ErrServerClosed and no
// association is left.
func TestServer_Shutdown(t *testing.T) {
	run := sharedClientRun(t)
	if run.shutdownErr != nil {
		t.Errorf("Shutdown = %v", run.shutdownErr)
	}
	if !errors.Is(run.servedErr, mms.ErrServerClosed) {
		t.Errorf("ListenAndServe after Shutdown = %v, want ErrServerClosed", run.servedErr)
	}
	if run.connsAfter != 0 {
		t.Errorf("%d associations left after Shutdown", run.connsAfter)
	}
}
