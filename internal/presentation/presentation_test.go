// SPDX-License-Identifier: MIT

package presentation

import (
	"bytes"
	"testing"

	"github.com/otfabric/go-mms/internal/berutil"
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

func TestParseCPorCPA_Edges(t *testing.T) {
	// Truncated field inside CP SET.
	bad := berutil.EncodeTLV(0x31, []byte{0xa0, 0x05})
	if _, err := parseCPorCPA(bad); err == nil {
		t.Fatal("expected truncated CP field error")
	}

	// Unknown top-level field is skipped (interop).
	mode := berutil.EncodeTLV(0xa0, []byte{0x80, 0x01, 0x01})
	unknown := berutil.EncodeTLV(0xa9, []byte{0x01})
	normal := berutil.EncodeTLV(0xa2, berutil.EncodeTLV(0xa4, nil)) // context-def list only
	ok := berutil.EncodeTLV(0x31, append(append(mode, unknown...), normal...))
	parsed, err := parseCPorCPA(ok)
	if err != nil || parsed.Kind != PpduCP {
		t.Fatalf("skip unknown: %+v err=%v", parsed, err)
	}

	// Normal-mode parse error propagates (truncated inside 0xa2).
	badNormal := berutil.EncodeTLV(0x31, berutil.EncodeTLV(0xa2, []byte{0x81, 0x05}))
	if _, err := parseCPorCPA(badNormal); err == nil {
		t.Fatal("expected normal-mode field error")
	}
}

func TestParseNormalMode_Edges(t *testing.T) {
	result := &ParsedPPDU{Kind: PpduCP}
	var def, res bool

	if err := parseNormalMode([]byte{0xff}, result, &def, &res); err == nil {
		t.Fatal("expected TLV error")
	}

	// Selectors + unknown + context lists, no user-data → success.
	body := append(
		berutil.EncodeTLV(0x81, []byte{0x01}),
		berutil.EncodeTLV(0x82, []byte{0x02})...,
	)
	body = append(body, berutil.EncodeTLV(0x83, []byte{0x03})...)
	body = append(body, berutil.EncodeTLV(0xa9, []byte{0x00})...) // unknown skip
	body = append(body, berutil.EncodeTLV(0xa4, nil)...)
	body = append(body, berutil.EncodeTLV(0xa5, nil)...)
	if err := parseNormalMode(body, result, &def, &res); err != nil {
		t.Fatal(err)
	}
	if !def || !res {
		t.Fatalf("def=%v res=%v", def, res)
	}

	// User-data path with transfer-syntax OID skipped.
	pdvInner := append(
		berutil.EncodeTLV(0x02, []byte{0x03}),                // context-id
		berutil.EncodeTLV(0x06, []byte{0x2a, 0x86, 0x48})..., // transfer-syntax OID
	)
	pdvInner = append(pdvInner, berutil.EncodeTLV(0xa0, []byte{0xab})...)
	pdv := berutil.EncodeTLV(0x30, pdvInner)
	userData := berutil.EncodeTLV(0x61, pdv)
	result = &ParsedPPDU{}
	def, res = false, false
	if err := parseNormalMode(userData, result, &def, &res); err != nil {
		t.Fatal(err)
	}
	if result.ContextID != 3 || !bytes.Equal(result.UserData, []byte{0xab}) {
		t.Fatalf("%+v", result)
	}
}

func TestParseUserDataAndPdvList_Edges(t *testing.T) {
	// Outer decode ok, pdv-list fails.
	bad := berutil.EncodeTLV(0x61, []byte{0xff})
	if _, err := parseUserData(bad); err == nil {
		t.Fatal("expected pdv-list error via user-data")
	}

	result := &ParsedPPDU{}
	if err := parsePdvList([]byte{0xff}, result); err == nil {
		t.Fatal("pdv outer")
	}

	// Truncated field inside SEQUENCE.
	trunc := berutil.EncodeTLV(0x30, []byte{0x02, 0x05})
	if err := parsePdvList(trunc, result); err == nil {
		t.Fatal("truncated field")
	}

	// Bad INTEGER for context-id (empty content).
	badInt := berutil.EncodeTLV(0x30, berutil.EncodeTLV(0x02, nil))
	if err := parsePdvList(badInt, result); err == nil {
		t.Fatal("bad context-id integer")
	}

	// Missing context-id.
	onlyData := berutil.EncodeTLV(0x30, berutil.EncodeTLV(0xa0, []byte{0x01}))
	if err := parsePdvList(onlyData, result); err == nil {
		t.Fatal("missing context-id")
	}

	// Success with transfer-syntax + data.
	inner := append(
		berutil.EncodeTLV(0x06, []byte{0x51}),
		berutil.EncodeTLV(0x02, []byte{0x01})...,
	)
	inner = append(inner, berutil.EncodeTLV(0xa0, []byte{0x11, 0x22})...)
	ok := berutil.EncodeTLV(0x30, inner)
	result = &ParsedPPDU{}
	if err := parsePdvList(ok, result); err != nil {
		t.Fatal(err)
	}
	if result.ContextID != 1 || !bytes.Equal(result.UserData, []byte{0x11, 0x22}) {
		t.Fatalf("%+v", result)
	}

	// User-data success path wrapping the same.
	ud := berutil.EncodeTLV(0x61, ok)
	parsed, err := parseUserData(ud)
	if err != nil || parsed.ContextID != 1 {
		t.Fatalf("%+v err=%v", parsed, err)
	}
}
