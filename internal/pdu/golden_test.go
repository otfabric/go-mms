package pdu

import (
	"bytes"
	"encoding/hex"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/codec"
)

const goldenDir = "testdata/golden"

var updateGoldenFlag = flag.Bool("update-golden", false, "update golden fixture files")

func goldenPath(name string) string {
	return filepath.Join(goldenDir, name+".hex")
}

func loadGolden(t *testing.T, name string) []byte {
	t.Helper()
	hexData, err := os.ReadFile(goldenPath(name))
	if err != nil {
		t.Fatalf("load golden %s: %v", name, err)
	}
	data, err := hex.DecodeString(strings.TrimSpace(string(hexData)))
	if err != nil {
		t.Fatalf("decode golden %s: %v", name, err)
	}
	return data
}

func updateGolden(t *testing.T, name string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(goldenDir, 0o755); err != nil {
		t.Fatalf("create golden dir: %v", err)
	}
	err := os.WriteFile(goldenPath(name), []byte(hex.EncodeToString(data)+"\n"), 0o644)
	if err != nil {
		t.Fatalf("update golden %s: %v", name, err)
	}
}

func TestGoldenFixtures(t *testing.T) {
	// Fixed timestamp for deterministic output.
	fixedTime := time.Date(2024, 1, 15, 12, 30, 0, 0, time.UTC)

	fixtures := []struct {
		name    string
		produce func() ([]byte, error)
	}{
		{
			name: "initiate_request",
			produce: func() ([]byte, error) {
				return MarshalInitiateRequest(DefaultInitiateRequest(65000, 5, 5, 10))
			},
		},
		{
			name: "identify_request",
			produce: func() ([]byte, error) {
				return MarshalIdentifyRequest(1)
			},
		},
		{
			name: "status_request",
			produce: func() ([]byte, error) {
				return MarshalStatusRequest(1, false)
			},
		},
		{
			name: "getnamelist_request_vmd",
			produce: func() ([]byte, error) {
				return MarshalGetNameListRequest(1, 0, ScopeVMD, "", "")
			},
		},
		{
			name: "getnamelist_request_domain",
			produce: func() ([]byte, error) {
				return MarshalGetNameListRequest(2, 0, ScopeDomain, "testDomain", "")
			},
		},
		{
			name: "getnamelist_response",
			produce: func() ([]byte, error) {
				return MarshalGetNameListResponse([]string{"var1", "var2", "var3"}, false)
			},
		},
		{
			name: "getvaraccess_request",
			produce: func() ([]byte, error) {
				return MarshalGetVarAccessRequest(1, ObjectNameWire{
					Scope:    ScopeDomain,
					DomainID: "testDomain",
					ItemID:   "testVar",
				})
			},
		},
		{
			name: "getvaraccess_response",
			produce: func() ([]byte, error) {
				return MarshalGetVarAccessResponse(false, TypeSpecWire{
					Tag:  tsTagInteger,
					Size: 32,
				})
			},
		},
		{
			name: "read_request",
			produce: func() ([]byte, error) {
				return MarshalReadRequest(1, []ObjectNameWire{
					{Scope: ScopeDomain, DomainID: "testDomain", ItemID: "var1"},
					{Scope: ScopeDomain, DomainID: "testDomain", ItemID: "var2"},
				})
			},
		},
		{
			name: "read_response",
			produce: func() ([]byte, error) {
				return MarshalReadResponse([]*AccessResult{
					{Data: &DataValue{Tag: TagDataInteger, Int: 42}},
					{Data: &DataValue{Tag: TagDataBoolean, Bool: true}},
				})
			},
		},
		{
			name: "write_request",
			produce: func() ([]byte, error) {
				return MarshalWriteRequest(1,
					[]ObjectNameWire{
						{Scope: ScopeDomain, DomainID: "testDomain", ItemID: "var1"},
					},
					[]*DataValue{
						{Tag: TagDataInteger, Int: 100},
					},
				)
			},
		},
		{
			name: "write_response",
			produce: func() ([]byte, error) {
				return MarshalWriteResponse([]int{0})
			},
		},
		{
			name: "define_nvl_request",
			produce: func() ([]byte, error) {
				return MarshalDefineNamedVarListRequest(1,
					ObjectNameWire{Scope: ScopeDomain, DomainID: "testDomain", ItemID: "myList"},
					[]VariableSpecWire{
						{Name: ObjectNameWire{Scope: ScopeDomain, DomainID: "testDomain", ItemID: "v1"}},
						{Name: ObjectNameWire{Scope: ScopeDomain, DomainID: "testDomain", ItemID: "v2"}},
					},
				)
			},
		},
		{
			name: "get_nvl_attrs_request",
			produce: func() ([]byte, error) {
				return MarshalGetNamedVarListAttrsRequest(1, ObjectNameWire{
					Scope:    ScopeDomain,
					DomainID: "testDomain",
					ItemID:   "myList",
				})
			},
		},
		{
			name: "get_nvl_attrs_response",
			produce: func() ([]byte, error) {
				return MarshalGetNVLAttrsResponse(true, []VariableSpecWire{
					{Name: ObjectNameWire{Scope: ScopeDomain, DomainID: "testDomain", ItemID: "v1"}},
					{Name: ObjectNameWire{Scope: ScopeDomain, DomainID: "testDomain", ItemID: "v2"}},
				})
			},
		},
		{
			name: "delete_nvl_request",
			produce: func() ([]byte, error) {
				return MarshalDeleteNamedVarListRequest(1, []ObjectNameWire{
					{Scope: ScopeDomain, DomainID: "testDomain", ItemID: "myList"},
				})
			},
		},
		{
			name: "delete_nvl_response",
			produce: func() ([]byte, error) {
				return MarshalDeleteNVLResponse(1, 1)
			},
		},
		{
			name: "file_open_request",
			produce: func() ([]byte, error) {
				return MarshalFileOpenRequest(1, "test.dat", 0)
			},
		},
		{
			name: "file_read_request",
			produce: func() ([]byte, error) {
				return MarshalFileReadRequest(1, 42)
			},
		},
		{
			name: "file_close_request",
			produce: func() ([]byte, error) {
				return MarshalFileCloseRequest(1, 42)
			},
		},
		{
			name: "file_directory_request",
			produce: func() ([]byte, error) {
				return MarshalFileDirectoryRequest(1, "*.dat", "")
			},
		},
		{
			name: "file_read_response",
			produce: func() ([]byte, error) {
				return MarshalFileReadResponse([]byte("test data"), true)
			},
		},
		{
			name: "file_directory_response",
			produce: func() ([]byte, error) {
				return MarshalFileDirectoryResponse([]FileDirectoryEntry{
					{FileName: "file1.dat", Size: 1024, LastModified: fixedTime},
					{FileName: "file2.dat", Size: 2048, LastModified: fixedTime},
				}, false)
			},
		},
		{
			name: "file_open_response",
			produce: func() ([]byte, error) {
				return MarshalFileOpenResponse(1, 4096, fixedTime)
			},
		},
		{
			name: "confirmed_error",
			produce: func() ([]byte, error) {
				return codec.MarshalConfirmedError(1, 1, 0), nil
			},
		},
		{
			name: "reject_pdu",
			produce: func() ([]byte, error) {
				return codec.MarshalRejectPDU(1, 1, 0), nil
			},
		},
		{
			name: "information_report",
			produce: func() ([]byte, error) {
				return MarshalInformationReport(&InformationReportWire{
					Variables: []ObjectNameWire{
						{Scope: ScopeDomain, DomainID: "testDomain", ItemID: "rptVar"},
					},
					Values: []*DataValue{
						{Tag: TagDataInteger, Int: 99},
					},
				})
			},
		},
	}

	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			data, err := f.produce()
			if err != nil {
				t.Fatalf("produce %s: %v", f.name, err)
			}

			if *updateGoldenFlag {
				updateGolden(t, f.name, data)
				t.Logf("updated golden file: %s", goldenPath(f.name))
				return
			}

			expected := loadGolden(t, f.name)
			if !bytes.Equal(data, expected) {
				t.Errorf("golden mismatch for %s:\n  got:  %s\n  want: %s",
					f.name, hex.EncodeToString(data), hex.EncodeToString(expected))
			}
		})
	}
}
