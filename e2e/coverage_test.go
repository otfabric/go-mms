// SPDX-License-Identifier: MIT

package e2e

import (
	"sort"
	"strings"
	"testing"

	"github.com/otfabric/go-mms/internal/apisurface"
)

// apiCoverage maps every exported method of Client, Server and ServerConn,
// and every exported package function, to the e2e tests that exercise it.
// TestAPICoverage requires each mapped test to exist and to call the method
// directly in its body, so new API cannot ship without an end-to-end test.
// Value constructors are covered by valueCases instead (one row each).
var apiCoverage = map[string][]string{
	"Client.Abort":                          {"TestClient_Abort"},
	"Client.Close":                          {"TestClient_Close", "TestServer_Connections"},
	"Client.Done":                           {"TestClient_Done_Err_Reconnect"},
	"Client.Err":                            {"TestClient_Done_Err_Reconnect"},
	"Client.DefineNamedVariableList":        {"TestClient_NamedVariableLists", "TestClient_DeleteAllNVLs"},
	"Client.DeleteAllDomainNVLs":            {"TestClient_DeleteAllNVLs"},
	"Client.DeleteAllVMDNVLs":               {"TestClient_DeleteAllNVLs"},
	"Client.DeleteNamedVariableList":        {"TestClient_NamedVariableLists", "TestServer_RegisterNamedVariableList"},
	"Client.DownloadFile":                   {"TestClient_DownloadFile", "TestClient_ObtainFile"},
	"Client.FileClose":                      {"TestClient_FileOpenReadClose"},
	"Client.FileDelete":                     {"TestClient_FileDelete"},
	"Client.FileDirectory":                  {"TestClient_FileDirectory", "TestClient_FileDirectoryAll"},
	"Client.FileDirectoryAll":               {"TestClient_FileDirectoryAll"},
	"Client.FileOpen":                       {"TestClient_FileOpenReadClose", "TestClient_FileOpen_Errors"},
	"Client.FileRead":                       {"TestClient_FileOpenReadClose"},
	"Client.FileReadAll":                    {"TestClient_FileReadAll", "TestClient_FileOpen_Errors"},
	"Client.FileRename":                     {"TestClient_FileRename"},
	"Client.GetNameList":                    {"TestClient_GetNameList", "TestClient_GetNameList_Paged"},
	"Client.GetNameListAll":                 {"TestClient_GetNameListAll", "TestServerConn_RegisterVariable"},
	"Client.GetNamedVariableListAttributes": {"TestClient_NamedVariableLists"},
	"Client.GetVariableAccessAttributes":    {"TestClient_GetVariableAccessAttributes", "TestValues_RoundTrip"},
	"Client.Identify":                       {"TestClient_Identify", "TestServer_HandleIdentify_HandleStatus"},
	"Client.Negotiated":                     {"TestClient_Negotiated", "TestClient_NewClient"},
	"Client.ObtainFile":                     {"TestClient_ObtainFile"},
	"Client.OnInformationReport":            {"TestClient_OnInformationReport", "TestServer_Broadcast"},
	"Client.Read":                           {"TestClient_Read", "TestValues_RoundTrip", "TestValues_DataAccessError"},
	"Client.ReadArrayElement":               {"TestClient_ArrayAccess"},
	"Client.ReadArrayRange":                 {"TestClient_ArrayAccess"},
	"Client.ReadByIndex":                    {"TestClient_ArrayAccess"},
	"Client.ReadComponent":                  {"TestClient_ReadComponent_WriteComponent"},
	"Client.ReadJournalStartAfter":          {"TestClient_ReadJournalStartAfter"},
	"Client.ReadJournalTimeRange":           {"TestClient_ReadJournalTimeRange", "TestClient_ReadJournalStartAfter"},
	"Client.ReadMultiple":                   {"TestClient_ReadMultiple"},
	"Client.ReadNamedVariableList":          {"TestClient_NamedVariableLists"},
	"Client.ReadObject":                     {"TestClient_ReadObject_WriteObject", "TestServerConn_RegisterVariable"},
	"Client.ReadVariables":                  {"TestClient_ReadVariables_WriteVariables"},
	"Client.Status":                         {"TestClient_Status", "TestServer_HandleIdentify_HandleStatus"},
	"Client.StatusWithOptions":              {"TestClient_Status"},
	"Client.Write":                          {"TestClient_Write", "TestValues_RoundTrip", "TestValues_Unencodable"},
	"Client.WriteArrayElement":              {"TestClient_ArrayAccess"},
	"Client.WriteComponent":                 {"TestClient_ReadComponent_WriteComponent"},
	"Client.WriteNamedVariableList":         {"TestClient_NamedVariableLists"},
	"Client.WriteObject":                    {"TestClient_ReadObject_WriteObject", "TestServerConn_RegisterVariable"},
	"Client.WriteVariables":                 {"TestClient_ReadVariables_WriteVariables"},

	"Server.Broadcast":                 {"TestServer_Broadcast", "TestClient_OnInformationReport"},
	"Server.Connections":               {"TestServer_Connections"},
	"Server.HandleIdentify":            {"TestServer_HandleIdentify_HandleStatus"},
	"Server.HandleStatus":              {"TestServer_HandleIdentify_HandleStatus"},
	"Server.ListenAndServe":            {"TestServer_ListenAndServe"},
	"Server.RegisterDomain":            {"TestServer_RegisterDomain_RegisterVariable"},
	"Server.RegisterNamedVariableList": {"TestServer_RegisterNamedVariableList"},
	"Server.RegisterVariable":          {"TestServer_RegisterDomain_RegisterVariable"},
	"Server.Serve":                     {"TestServer_Serve"},
	"Server.SetVariableRead":           {"TestServer_SetVariableRead"},
	"Server.Shutdown":                  {"TestServer_Shutdown", "TestServer_Shutdown_InFlight"},

	"ServerConn.Abort":                 {"TestServerConn_Abort"},
	"ServerConn.AuthToken":             {"TestServerConn_AuthToken"},
	"ServerConn.RegisterVariable":      {"TestServerConn_RegisterVariable"},
	"ServerConn.SendInformationReport": {"TestServerConn_SendInformationReport", "TestClient_OnInformationReport"},
	"ServerConn.SendUnsolicitedStatus": {"TestServerConn_SendUnsolicitedStatus"},

	"DefaultRetryPolicy":    {"TestClient_Done_Err_Reconnect"},
	"IsRetryable":           {"TestClient_Done_Err_Reconnect"},
	"NewClient":             {"TestClient_NewClient"},
	"Retry":                 {"TestClient_Done_Err_Reconnect"},
	"NewDataAccessError":    {"TestValues_DataAccessError"},
	"NewServer":             {"TestServer_ListenAndServe", "TestServer_RegisterDomain_RegisterVariable"},
	"SelectComponent":       {"TestClient_ReadVariables_WriteVariables"},
	"SelectIndex":           {"TestClient_ReadVariables_WriteVariables"},
	"SelectRange":           {"TestClient_ReadVariables_WriteVariables"},
	"ServerConnFromContext": {"TestServerConn_RegisterVariable", "TestServerConn_SendInformationReport"},
}

func TestAPICoverage(t *testing.T) {
	methods, err := apisurface.Methods("..", "Client", "Server", "ServerConn")
	if err != nil {
		t.Fatal(err)
	}
	funcs, err := apisurface.Funcs("..", "")
	if err != nil {
		t.Fatal(err)
	}
	calls, err := apisurface.TestCalls(".")
	if err != nil {
		t.Fatal(err)
	}

	valueRows := map[string]bool{}
	for _, vc := range valueCases() {
		valueRows[vc.name] = true
	}

	surface := append(append([]string{}, methods...), funcs...)
	seen := map[string]bool{}
	for _, api := range surface {
		seen[api] = true
		tests, mapped := apiCoverage[api]
		if !mapped {
			if valueRows[api] {
				continue
			}
			t.Errorf("%s has no e2e test: map it in apiCoverage (constructors: add a valueCases row)", api)
			continue
		}
		name := api[strings.LastIndexByte(api, '.')+1:]
		for _, test := range tests {
			body, ok := calls[test]
			if !ok {
				t.Errorf("%s maps to %s, which does not exist", api, test)
			} else if !body[name] {
				t.Errorf("%s maps to %s, which does not call %s", api, test, name)
			}
		}
	}

	var stale []string
	for api := range apiCoverage {
		if !seen[api] {
			stale = append(stale, api)
		}
	}
	for name := range valueRows {
		if strings.HasPrefix(name, "New") && !seen[name] && !seen[strings.TrimSuffix(name, "64")] {
			stale = append(stale, "valueCases "+name)
		}
	}
	sort.Strings(stale)
	for _, s := range stale {
		t.Errorf("%s is not part of the exported API", s)
	}
}
