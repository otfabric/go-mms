// SPDX-License-Identifier: MIT

package presentation

import (
	"bytes"
	"testing"
)

func TestEncodeParseCPRoundTrip(t *testing.T) {
	params := ConnectParams{
		CallingSelector: []byte{0x00, 0x00, 0x00, 0x01},
		CalledSelector:  []byte{0x00, 0x00, 0x00, 0x01},
	}
	acsePayload := []byte{0x60, 0x03, 0x01, 0x02, 0x03} // mock AARQ

	cp := EncodeCP(params, acsePayload)

	if cp[0] != 0x31 {
		t.Fatalf("CP tag = 0x%02x, want 0x31", cp[0])
	}

	parsed, err := Parse(cp)
	if err != nil {
		t.Fatalf("Parse CP: %v", err)
	}
	if parsed.Kind != PpduCP {
		t.Errorf("Kind = %s, want CP", parsed.Kind)
	}
	if parsed.ContextID != ContextIDACSE {
		t.Errorf("ContextID = %d, want %d (ACSE)", parsed.ContextID, ContextIDACSE)
	}
	if !bytes.Equal(parsed.UserData, acsePayload) {
		t.Errorf("UserData = %x, want %x", parsed.UserData, acsePayload)
	}
}

func TestEncodeParseCPARoundTrip(t *testing.T) {
	respondingSel := []byte{0x00, 0x00, 0x00, 0x01}
	acsePayload := []byte{0x61, 0x05, 0x01, 0x02, 0x03, 0x04, 0x05} // mock AARE

	cpa := EncodeCPA(respondingSel, acsePayload)

	if cpa[0] != 0x31 {
		t.Fatalf("CPA tag = 0x%02x, want 0x31", cpa[0])
	}

	parsed, err := Parse(cpa)
	if err != nil {
		t.Fatalf("Parse CPA: %v", err)
	}
	if parsed.Kind != PpduCPA {
		t.Errorf("Kind = %s, want CPA", parsed.Kind)
	}
	if !bytes.Equal(parsed.UserData, acsePayload) {
		t.Errorf("UserData = %x, want %x", parsed.UserData, acsePayload)
	}
}

func TestEncodeParseUserDataRoundTrip(t *testing.T) {
	mmsPayload := []byte{0xa0, 0x05, 0x02, 0x01, 0x01, 0x82, 0x00}

	userData := EncodeUserData(ContextIDMMS, mmsPayload)

	if userData[0] != 0x61 {
		t.Fatalf("user-data tag = 0x%02x, want 0x61", userData[0])
	}

	parsed, err := Parse(userData)
	if err != nil {
		t.Fatalf("Parse user-data: %v", err)
	}
	if parsed.Kind != PpduUserData {
		t.Errorf("Kind = %s, want UserData", parsed.Kind)
	}
	if parsed.ContextID != ContextIDMMS {
		t.Errorf("ContextID = %d, want %d (MMS)", parsed.ContextID, ContextIDMMS)
	}
	if !bytes.Equal(parsed.UserData, mmsPayload) {
		t.Errorf("UserData = %x, want %x", parsed.UserData, mmsPayload)
	}
}

func TestEncodeUserDataACSEContext(t *testing.T) {
	acsePayload := []byte{0x62, 0x03, 0x80, 0x01, 0x00} // mock RLRQ

	userData := EncodeUserData(ContextIDACSE, acsePayload)
	parsed, err := Parse(userData)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.ContextID != ContextIDACSE {
		t.Errorf("ContextID = %d, want %d (ACSE)", parsed.ContextID, ContextIDACSE)
	}
	if !bytes.Equal(parsed.UserData, acsePayload) {
		t.Errorf("UserData mismatch")
	}
}

func TestCPContainsContextList(t *testing.T) {
	cp := EncodeCP(ConnectParams{}, []byte{0x01})

	found := false
	for i := 0; i < len(cp)-1; i++ {
		if cp[i] == 0xa4 {
			found = true
			break
		}
	}
	if !found {
		t.Error("CP should contain presentation-context-definition-list (tag 0xa4)")
	}
}

func TestCPAContainsResultList(t *testing.T) {
	cpa := EncodeCPA(nil, []byte{0x01})

	found := false
	for i := 0; i < len(cpa)-1; i++ {
		if cpa[i] == 0xa5 {
			found = true
			break
		}
	}
	if !found {
		t.Error("CPA should contain context-definition-result-list (tag 0xa5)")
	}
}

func TestParseTooShort(t *testing.T) {
	_, err := Parse(nil)
	if err == nil {
		t.Error("expected error for nil input")
	}
	_, err = Parse([]byte{0x31})
	if err == nil {
		t.Error("expected error for 1-byte input")
	}
}

func TestParseUnknownTag(t *testing.T) {
	_, err := Parse([]byte{0xFF, 0x00})
	if err == nil {
		t.Error("expected error for unknown PPDU tag")
	}
}

func TestCPNoSelectors(t *testing.T) {
	cp := EncodeCP(ConnectParams{}, []byte{0xAA})
	parsed, err := Parse(cp)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Kind != PpduCP {
		t.Errorf("Kind = %s, want CP", parsed.Kind)
	}
	if !bytes.Equal(parsed.UserData, []byte{0xAA}) {
		t.Errorf("UserData = %x, want AA", parsed.UserData)
	}
}

func TestParseUserDataTrailingBytes(t *testing.T) {
	userData := EncodeUserData(ContextIDMMS, []byte{0x01})
	userData = append(userData, 0xff)
	_, err := Parse(userData)
	if err == nil {
		t.Fatal("expected error for trailing bytes after user-data PPDU")
	}
}

func TestParseCPTrailingBytes(t *testing.T) {
	cp := EncodeCP(ConnectParams{}, []byte{0x01})
	cp = append(cp, 0xff)
	_, err := Parse(cp)
	if err == nil {
		t.Fatal("expected error for trailing bytes after CP PPDU")
	}
}

func TestParseCPATrailingBytes(t *testing.T) {
	cpa := EncodeCPA(nil, []byte{0x01})
	cpa = append(cpa, 0xff)
	_, err := Parse(cpa)
	if err == nil {
		t.Fatal("expected error for trailing bytes after CPA PPDU")
	}
}

func TestPpduKindString(t *testing.T) {
	tests := []struct {
		k    PpduKind
		want string
	}{
		{PpduCP, "CP"},
		{PpduCPA, "CPA"},
		{PpduUserData, "UserData"},
		{PpduKind(99), "PpduKind(99)"},
	}
	for _, tt := range tests {
		if got := tt.k.String(); got != tt.want {
			t.Errorf("PpduKind(%d).String() = %q, want %q", int(tt.k), got, tt.want)
		}
	}
}
