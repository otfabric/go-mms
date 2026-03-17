package session

import (
	"bytes"
	"testing"
)

func TestEncodeDecodeConnect(t *testing.T) {
	params := ConnectParams{
		CallingSelector: []byte{0x00, 0x01},
		CalledSelector:  []byte{0x00, 0x01},
	}
	userData := []byte{0xAA, 0xBB, 0xCC, 0xDD}
	encoded := EncodeConnect(params, userData)

	if encoded[0] != SIConnect {
		t.Fatalf("SI byte = 0x%02x, want 0x%02x", encoded[0], SIConnect)
	}

	parsed, err := Parse(encoded)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Type != SpduConnect {
		t.Errorf("Type = %s, want CONNECT", parsed.Type)
	}
	if !bytes.Equal(parsed.CallingSelector, []byte{0x00, 0x01}) {
		t.Errorf("CallingSelector = %x, want 0001", parsed.CallingSelector)
	}
	if !bytes.Equal(parsed.CalledSelector, []byte{0x00, 0x01}) {
		t.Errorf("CalledSelector = %x, want 0001", parsed.CalledSelector)
	}
	if !bytes.Equal(parsed.UserData, userData) {
		t.Errorf("UserData = %x, want %x", parsed.UserData, userData)
	}
}

func TestEncodeDecodeAccept(t *testing.T) {
	params := ConnectParams{
		CalledSelector: []byte{0x00, 0x01},
	}
	userData := []byte{0x01, 0x02, 0x03}
	encoded := EncodeAccept(params, userData)

	if encoded[0] != SIAccept {
		t.Fatalf("SI byte = 0x%02x, want 0x%02x", encoded[0], SIAccept)
	}

	parsed, err := Parse(encoded)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Type != SpduAccept {
		t.Errorf("Type = %s, want ACCEPT", parsed.Type)
	}
	if !bytes.Equal(parsed.UserData, userData) {
		t.Errorf("UserData = %x, want %x", parsed.UserData, userData)
	}
}

func TestEncodeDecodeData(t *testing.T) {
	payload := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	encoded := EncodeData(payload)

	if !bytes.Equal(encoded[:4], dataSpduHeader) {
		t.Fatalf("DATA header = %x, want %x", encoded[:4], dataSpduHeader)
	}

	parsed, err := Parse(encoded)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Type != SpduData {
		t.Errorf("Type = %s, want DATA", parsed.Type)
	}
	if !bytes.Equal(parsed.UserData, payload) {
		t.Errorf("UserData = %x, want %x", parsed.UserData, payload)
	}
}

func TestEncodeDecodeFinish(t *testing.T) {
	userData := []byte{0x01, 0x02}
	encoded := EncodeFinish(userData)

	if encoded[0] != SIFinish {
		t.Fatalf("SI byte = 0x%02x, want 0x%02x", encoded[0], SIFinish)
	}

	parsed, err := Parse(encoded)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Type != SpduFinish {
		t.Errorf("Type = %s, want FINISH", parsed.Type)
	}
	if !bytes.Equal(parsed.UserData, userData) {
		t.Errorf("UserData = %x, want %x", parsed.UserData, userData)
	}
}

func TestEncodeDecodeDisconnect(t *testing.T) {
	userData := []byte{0xFF}
	encoded := EncodeDisconnect(userData)

	if encoded[0] != SIDisconnect {
		t.Fatalf("SI byte = 0x%02x, want 0x%02x", encoded[0], SIDisconnect)
	}

	parsed, err := Parse(encoded)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Type != SpduDisconnect {
		t.Errorf("Type = %s, want DISCONNECT", parsed.Type)
	}
	if !bytes.Equal(parsed.UserData, userData) {
		t.Errorf("UserData = %x, want %x", parsed.UserData, userData)
	}
}

func TestEncodeDecodeAbort(t *testing.T) {
	userData := []byte{0xAB, 0xCD}
	encoded := EncodeAbort(userData)

	if encoded[0] != SIAbort {
		t.Fatalf("SI byte = 0x%02x, want 0x%02x", encoded[0], SIAbort)
	}

	parsed, err := Parse(encoded)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Type != SpduAbort {
		t.Errorf("Type = %s, want ABORT", parsed.Type)
	}
	if !bytes.Equal(parsed.UserData, userData) {
		t.Errorf("UserData = %x, want %x", parsed.UserData, userData)
	}
}

func TestConnectWireFormat(t *testing.T) {
	params := ConnectParams{
		CallingSelector: []byte{0x00, 0x01},
		CalledSelector:  []byte{0x00, 0x01},
	}
	encoded := EncodeConnect(params, []byte{0xAA, 0xBB, 0xCC, 0xDD})

	// Verify: SI=0x0d, then PGI 5 (conn/accept item) is present
	if encoded[0] != 0x0d {
		t.Errorf("SI = 0x%02x, want 0x0d", encoded[0])
	}
	// PGI 5 should start at offset 2
	if encoded[2] != 0x05 {
		t.Errorf("PGI at offset 2 = 0x%02x, want 0x05 (Connection/Accept Item)", encoded[2])
	}
}

func TestDataFixedHeader(t *testing.T) {
	encoded := EncodeData(nil)
	expected := []byte{0x01, 0x00, 0x01, 0x00}
	if !bytes.Equal(encoded, expected) {
		t.Errorf("DATA empty = %x, want %x", encoded, expected)
	}
}

func TestParseTooShort(t *testing.T) {
	_, err := Parse(nil)
	if err == nil {
		t.Error("expected error for nil input")
	}
	_, err = Parse([]byte{0x01})
	if err == nil {
		t.Error("expected error for 1-byte input")
	}
}

func TestParseUnknownType(t *testing.T) {
	_, err := Parse([]byte{0xFF, 0x00})
	if err == nil {
		t.Error("expected error for unknown SPDU type")
	}
}

func TestSpduTypeString(t *testing.T) {
	tests := []struct {
		t    SpduType
		want string
	}{
		{SpduConnect, "CONNECT"},
		{SpduAccept, "ACCEPT"},
		{SpduData, "DATA"},
		{SpduFinish, "FINISH"},
		{SpduDisconnect, "DISCONNECT"},
		{SpduAbort, "ABORT"},
		{SpduRefuse, "REFUSE"},
		{SpduType(0x77), "SpduType(0x77)"},
	}
	for _, tt := range tests {
		if got := tt.t.String(); got != tt.want {
			t.Errorf("SpduType(0x%02x).String() = %q, want %q", byte(tt.t), got, tt.want)
		}
	}
}

func TestConnectNoSelectors(t *testing.T) {
	params := ConnectParams{}
	userData := []byte{0x01}
	encoded := EncodeConnect(params, userData)

	parsed, err := Parse(encoded)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(parsed.CallingSelector) != 0 {
		t.Errorf("CallingSelector = %x, want empty", parsed.CallingSelector)
	}
	if len(parsed.CalledSelector) != 0 {
		t.Errorf("CalledSelector = %x, want empty", parsed.CalledSelector)
	}
	if !bytes.Equal(parsed.UserData, userData) {
		t.Errorf("UserData = %x, want %x", parsed.UserData, userData)
	}
}

func TestParseDataInvalidHeader(t *testing.T) {
	// First byte matches DATA SI (0x01) but remaining header bytes are wrong
	bad := []byte{0x01, 0x01, 0x01, 0x00, 0xAA, 0xBB}
	_, err := Parse(bad)
	if err == nil {
		t.Error("expected error for invalid DATA header bytes")
	}
}

func TestParseRefuse(t *testing.T) {
	data := []byte{0x0c, 0x03, 0x01, 0x02, 0x03}
	result, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if result.Type != SpduRefuse {
		t.Fatalf("got type %s, want REFUSE", result.Type)
	}
}

func TestParseRefuseTruncated(t *testing.T) {
	data := []byte{0x0c, 0x05, 0x01, 0x02}
	_, err := Parse(data)
	if err == nil {
		t.Fatal("expected error for truncated REFUSE")
	}
}

func TestConnectTrailingBytesInHeader(t *testing.T) {
	encoded := EncodeConnect(ConnectParams{}, []byte{0x01})
	// Extend LI by 1 and append a trailing byte inside the header area.
	// The parser should detect that the final byte doesn't form a
	// complete PGI (tag+length), breaking out of the loop and hitting
	// the offset != headerEnd check.
	modified := make([]byte, len(encoded)+1)
	copy(modified, encoded)
	modified[1]++
	modified[len(encoded)] = 0xfe
	_, err := Parse(modified)
	if err == nil {
		t.Fatal("expected error for trailing bytes in CONNECT header")
	}
}

func TestAcceptTrailingBytesInHeader(t *testing.T) {
	encoded := EncodeAccept(ConnectParams{}, []byte{0x01})
	modified := make([]byte, len(encoded)+1)
	copy(modified, encoded)
	modified[1]++
	modified[len(encoded)] = 0xfe
	_, err := Parse(modified)
	if err == nil {
		t.Fatal("expected error for trailing bytes in ACCEPT header")
	}
}

func TestFinishTrailingBytes(t *testing.T) {
	encoded := EncodeFinish([]byte{0x01})
	modified := make([]byte, len(encoded)+1)
	copy(modified, encoded)
	modified[1]++
	modified[len(encoded)] = 0xfe
	_, err := Parse(modified)
	if err == nil {
		t.Fatal("expected error for trailing bytes in FINISH SPDU")
	}
}

func TestParseRefuseEmpty(t *testing.T) {
	data := []byte{0x0c, 0x00}
	result, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if result.Type != SpduRefuse {
		t.Fatalf("got type %s, want REFUSE", result.Type)
	}
}
