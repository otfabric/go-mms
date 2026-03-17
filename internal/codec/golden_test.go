package codec

import (
	"bytes"
	"encoding/hex"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestGoldenCodecFixtures(t *testing.T) {
	fixtures := []struct {
		name    string
		produce func() ([]byte, error)
	}{
		{
			name: "confirmed_request_wrap",
			produce: func() ([]byte, error) {
				return MarshalConfirmedRequest(1, 4, true, []byte{0x01, 0x02, 0x03})
			},
		},
		{
			name: "confirmed_response_wrap",
			produce: func() ([]byte, error) {
				return MarshalConfirmedResponse(1, 4, true, []byte{0x01, 0x02, 0x03})
			},
		},
		{
			name: "confirmed_error",
			produce: func() ([]byte, error) {
				return MarshalConfirmedError(1, 1, 0), nil
			},
		},
		{
			name: "reject_pdu",
			produce: func() ([]byte, error) {
				return MarshalRejectPDU(1, 1, 0), nil
			},
		},
		{
			name: "conclude_request",
			produce: func() ([]byte, error) {
				return MarshalConcludeRequest(), nil
			},
		},
		{
			name: "conclude_response",
			produce: func() ([]byte, error) {
				return MarshalConcludeResponse(), nil
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
