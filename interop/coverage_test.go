//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/otfabric/go-mms/internal/apisurface"
)

// interopAPICoverage maps every exported method of Client, Server and
// ServerConn to the interop tests that exercise it against libiec61850.
// TestInteropCoverage fails when a method is added without an entry here,
// so new API cannot ship without interop coverage or a written exemption.
var interopAPICoverage = map[string][]string{
	"Client.Abort":                          {"TestClient_Abort"},
	"Client.Close":                          {"TestClient_Conclude"},
	"Client.Done":                           {"TestClient_PeerDisconnect"},
	"Client.Err":                            {"TestClient_PeerDisconnect"},
	"Client.DefineNamedVariableList":        {"TestClient_NVL", "TestClient_NVL_VMDScope", "TestClient_NVL_AssociationScope"},
	"Client.DeleteAllDomainNVLs":            {"TestClient_DeleteAllNVLs_UnsupportedByPeer"},
	"Client.DeleteAllVMDNVLs":               {"TestClient_DeleteAllNVLs_UnsupportedByPeer"},
	"Client.DeleteNamedVariableList":        {"TestClient_NVL", "TestClient_NVL_VMDScope", "TestClient_NVL_AssociationScope"},
	"Client.DownloadFile":                   {"TestClient_DownloadFile"},
	"Client.FileClose":                      {"TestClient_FileOpenReadClose"},
	"Client.FileDelete":                     {"TestClient_FileDelete"},
	"Client.FileDirectory":                  {"TestClient_FileDirectory", "TestClient_FileDirectory_Subdirectory"},
	"Client.FileDirectoryAll":               {"TestClient_FileDirectoryAll_Paginated"},
	"Client.FileOpen":                       {"TestClient_FileOpenReadClose", "TestClient_FileOpen_InitialPosition", "TestClient_FileOpen_NonExistent"},
	"Client.FileRead":                       {"TestClient_FileOpenReadClose"},
	"Client.FileReadAll":                    {"TestClient_FileReadAll_MultiChunk"},
	"Client.FileRename":                     {"TestClient_FileRename"},
	"Client.GetNameList":                    {"TestClient_GetNameList_Domains", "TestClient_GetNameList_Variables", "TestClient_GetNameList_Journals"},
	"Client.GetNameListAll":                 {"TestClient_GetNameListAll_Variables"},
	"Client.GetNamedVariableListAttributes": {"TestClient_NVL_Attributes"},
	"Client.GetVariableAccessAttributes":    {"TestClient_GetVariableAccessAttributes", "TestClient_GetVariableAccessAttributes_Composite"},
	"Client.Identify":                       {"TestClient_Identify"},
	"Client.Negotiated":                     {"TestClient_Negotiated"},
	"Client.ObtainFile":                     {"TestClient_ObtainFile"},
	"Client.OnInformationReport":            {"TestClient_InformationReport"},
	"Client.Read":                           {"TestClient_Read_Boolean", "TestClient_Read_Structure", "TestClient_Read_UnknownVariable"},
	"Client.ReadArrayElement":               {"TestClient_ReadArrayElement"},
	"Client.ReadArrayRange":                 {"TestClient_ReadArrayRange"},
	"Client.ReadByIndex":                    {"TestClient_ReadArrayElement"},
	"Client.ReadComponent":                  {"TestClient_ReadComponent"},
	"Client.ReadJournalStartAfter":          {"TestClient_ReadJournalStartAfter"},
	"Client.ReadJournalTimeRange":           {"TestClient_ReadJournalTimeRange", "TestClient_ReadJournal_Unknown"},
	"Client.ReadMultiple":                   {"TestClient_ReadMultiple"},
	"Client.ReadNamedVariableList":          {"TestClient_NVL", "TestClient_NVL_SpecificationWithResult"},
	"Client.ReadObject":                     {"TestClient_ReadObject_WriteObject"},
	"Client.ReadVariables":                  {"TestClient_ReadVariables_WriteVariables_AlternateAccess"},
	"Client.Status":                         {"TestClient_Status"},
	"Client.StatusWithOptions":              {"TestClient_Status"},
	"Client.Write":                          {"TestClient_Write_Float32", "TestClient_Write_ReadOnly", "TestClient_Write_WrongType"},
	"Client.WriteArrayElement":              {"TestClient_WriteArrayElement"},
	"Client.WriteComponent":                 {"TestClient_WriteComponent"},
	"Client.WriteNamedVariableList":         {"TestClient_WriteNamedVariableList"},
	"Client.WriteObject":                    {"TestClient_ReadObject_WriteObject"},
	"Client.WriteVariables":                 {"TestClient_ReadVariables_WriteVariables_AlternateAccess"},

	"Server.Broadcast":                 {"TestServer_InformationReport"},
	"Server.HandleIdentify":            {"TestServer_Identify"},
	"Server.HandleStatus":              {"TestServer_Status"},
	"Server.ListenAndServe":            {"TestServer_Identify"},
	"Server.RegisterDomain":            {"TestServer_GetNameList_Domains"},
	"Server.RegisterNamedVariableList": {"TestServer_NVL_ServerRegistered"},
	"Server.RegisterVariable":          {"TestServer_Read_Boolean", "TestServer_Write_Float32", "TestServer_Write_ReadOnly"},
	"Server.Shutdown":                  {"TestServer_Shutdown"},

	"ServerConn.Abort":                 {"TestServer_ServerConnAbort"},
	"ServerConn.SendInformationReport": {"TestServer_InformationReport"},
	"ServerConn.SendUnsolicitedStatus": {"TestServer_InformationReport"},
}

// interopAPIExempt lists methods with no wire behaviour of their own, or
// none that libiec61850 can exercise. Each is covered by the e2e suite.
var interopAPIExempt = map[string]string{
	"Server.Connections":          "server-local accessor; no wire behaviour",
	"Server.Serve":                "exercised on every connection through ListenAndServe",
	"Server.SetVariableRead":      "swaps a local read handler; the wire path is RegisterVariable's",
	"ServerConn.AuthToken":        "server-local accessor; libiec61850 sends no authentication value",
	"ServerConn.RegisterVariable": "libiec61850 has no API to address association-specific variables",
}

// interopServiceCoverage maps every confirmed service the server dispatches
// to the server-direction tests that drive it from the libiec61850 client.
var interopServiceCoverage = map[string][]string{
	"TagNumIdentify":                    {"TestServer_Identify"},
	"TagNumStatus":                      {"TestServer_Status"},
	"TagNumGetNameList":                 {"TestServer_GetNameList_Domains", "TestServer_GetNameList_Journals", "TestServer_GetNameList_VMDVariables", "TestServer_GetNameList_UnknownDomain"},
	"TagNumGetVariableAccessAttributes": {"TestServer_GetVariableAccessAttributes", "TestServer_GetVariableAccessAttributes_Composite"},
	"TagNumRead":                        {"TestServer_Read_Boolean", "TestServer_ReadArrayElements", "TestServer_ReadComponent"},
	"TagNumWrite":                       {"TestServer_Write_Float32", "TestServer_WriteArrayElements", "TestServer_WriteComponent"},
	"TagNumDefineNamedVariableList":     {"TestServer_NVL_DomainScope", "TestServer_NVL_VMDScope", "TestServer_NVL_AssociationScope"},
	"TagNumGetNamedVariableListAttrs":   {"TestServer_NVL_DomainScope", "TestServer_NVL_ServerRegistered"},
	"TagNumDeleteNamedVariableList":     {"TestServer_NVL_DomainScope", "TestServer_NVL_VMDScope"},
	"TagNumFileOpen":                    {"TestServer_FileOpenReadClose", "TestServer_FileOpen_InitialPosition", "TestServer_FileOpen_NonExistent", "TestServer_FileOpen_NoLastModified"},
	"TagNumFileRead":                    {"TestServer_FileOpenReadClose", "TestServer_FileRead_MultiChunk"},
	"TagNumFileClose":                   {"TestServer_FileOpenReadClose"},
	"TagNumFileRename":                  {"TestServer_FileDelete_Rename_Obtain"},
	"TagNumFileDelete":                  {"TestServer_FileDelete_Rename_Obtain"},
	"TagNumFileDirectory":               {"TestServer_FileDirectory", "TestServer_FileDirectory_Subdirectory"},
	"TagNumObtainFile":                  {"TestServer_FileDelete_Rename_Obtain"},
	"TagNumReadJournal":                 {"TestServer_ReadJournal_TimeRange", "TestServer_ReadJournal_StartAfter", "TestServer_ReadJournal_Unknown"},
}

// TestInteropCoverage needs no peer binaries: it checks the manifests above
// against the library source and the interop tests.
func TestInteropCoverage(t *testing.T) {
	methods, err := apisurface.Methods("..", "Client", "Server", "ServerConn")
	if err != nil {
		t.Fatal(err)
	}
	tests, err := apisurface.TestFuncs(".")
	if err != nil {
		t.Fatal(err)
	}
	src := interopSource(t)

	exported := map[string]bool{}
	for _, m := range methods {
		exported[m] = true
		mapped, covered := interopAPICoverage[m]
		_, exempt := interopAPIExempt[m]
		switch {
		case covered && exempt:
			t.Errorf("%s is both covered and exempt", m)
		case !covered && !exempt:
			t.Errorf("%s has no interop test: add it to interopAPICoverage (or interopAPIExempt with a reason)", m)
		case covered:
			requireTests(t, m, mapped, tests)
			name := m[strings.IndexByte(m, '.')+1:]
			if !strings.Contains(src, "."+name+"(") {
				t.Errorf("%s is mapped to interop tests but never called from interop code", m)
			}
		}
	}
	for m := range interopAPICoverage {
		if !exported[m] {
			t.Errorf("interopAPICoverage lists %s, which is not an exported method", m)
		}
	}
	for m := range interopAPIExempt {
		if !exported[m] {
			t.Errorf("interopAPIExempt lists %s, which is not an exported method", m)
		}
	}

	services := dispatchedServices(t)
	for _, svc := range services {
		mapped, ok := interopServiceCoverage[svc]
		if !ok {
			t.Errorf("server dispatches %s but no server-direction interop test is mapped to it", svc)
			continue
		}
		requireTests(t, svc, mapped, tests)
	}
	for svc := range interopServiceCoverage {
		if !contains(services, svc) {
			t.Errorf("interopServiceCoverage lists %s, which the server does not dispatch", svc)
		}
	}
}

func requireTests(t *testing.T, subject string, names []string, tests map[string]bool) {
	t.Helper()
	if len(names) == 0 {
		t.Errorf("%s maps to no tests", subject)
	}
	for _, n := range names {
		if !tests[n] {
			t.Errorf("%s maps to %s, which does not exist", subject, n)
		}
	}
}

// interopSource concatenates the interop test sources, minus this file.
func interopSource(t *testing.T) string {
	t.Helper()
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, f := range files {
		if f == "coverage_test.go" {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
	}
	return b.String()
}

var dispatchCase = regexp.MustCompile(`(?m)^\s*case asn1util\.(TagNum\w+):`)

// dispatchedServices returns the confirmed-service tags handled by the
// server's dispatch switch.
func dispatchedServices(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "server.go"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range dispatchCase.FindAllStringSubmatch(string(data), -1) {
		out = append(out, m[1])
	}
	if len(out) == 0 {
		t.Fatal("no dispatch cases found in server.go")
	}
	sort.Strings(out)
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
