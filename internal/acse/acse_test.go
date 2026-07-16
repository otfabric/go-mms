// SPDX-License-Identifier: MIT

package acse

import (
	"bytes"
	"encoding/asn1"
	"testing"
)

func TestEncodeParseAARQRoundTrip(t *testing.T) {
	params := AARQParams{
		CalledAPTitle:      asn1.ObjectIdentifier{1, 1, 1, 1},
		CalledAEQualifier:  12,
		CallingAPTitle:     asn1.ObjectIdentifier{1, 1, 1, 1},
		CallingAEQualifier: 12,
	}
	mmsPayload := []byte{0xa8, 0x04, 0x01, 0x02, 0x03, 0x04} // mock MMS Initiate

	aarq, err := EncodeAARQ(params, mmsPayload)
	if err != nil {
		t.Fatalf("EncodeAARQ: %v", err)
	}

	if aarq[0] != TagAARQ {
		t.Fatalf("AARQ tag = 0x%02x, want 0x%02x", aarq[0], TagAARQ)
	}

	parsed, err := Parse(aarq)
	if err != nil {
		t.Fatalf("Parse AARQ: %v", err)
	}
	if parsed.Type != ApduAARQ {
		t.Errorf("Type = %s, want AARQ", parsed.Type)
	}
	if !bytes.Equal(parsed.UserData, mmsPayload) {
		t.Errorf("UserData = %x, want %x", parsed.UserData, mmsPayload)
	}
}

func TestEncodeParseAARERoundTrip(t *testing.T) {
	mmsPayload := []byte{0xa9, 0x03, 0x01, 0x02, 0x03} // mock MMS Initiate Response

	aare := EncodeAARE(ResultAccepted, mmsPayload)

	if aare[0] != TagAARE {
		t.Fatalf("AARE tag = 0x%02x, want 0x%02x", aare[0], TagAARE)
	}

	parsed, err := Parse(aare)
	if err != nil {
		t.Fatalf("Parse AARE: %v", err)
	}
	if parsed.Type != ApduAARE {
		t.Errorf("Type = %s, want AARE", parsed.Type)
	}
	if parsed.AARE == nil {
		t.Fatal("AARE field is nil")
	}
	if parsed.AARE.Result != ResultAccepted {
		t.Errorf("Result = %d, want %d (accepted)", parsed.AARE.Result, ResultAccepted)
	}
	if !bytes.Equal(parsed.AARE.UserData, mmsPayload) {
		t.Errorf("UserData = %x, want %x", parsed.AARE.UserData, mmsPayload)
	}
}

func TestEncodeParseAARERejected(t *testing.T) {
	aare := EncodeAARE(ResultRejectedPerm, nil)
	parsed, err := Parse(aare)
	if err != nil {
		t.Fatalf("Parse AARE: %v", err)
	}
	if parsed.AARE.Result != ResultRejectedPerm {
		t.Errorf("Result = %d, want %d (rejected-permanent)", parsed.AARE.Result, ResultRejectedPerm)
	}
}

func TestRLRQRoundTrip(t *testing.T) {
	rlrq := EncodeRLRQ()
	if rlrq[0] != TagRLRQ {
		t.Fatalf("RLRQ tag = 0x%02x, want 0x%02x", rlrq[0], TagRLRQ)
	}

	parsed, err := Parse(rlrq)
	if err != nil {
		t.Fatalf("Parse RLRQ: %v", err)
	}
	if parsed.Type != ApduRLRQ {
		t.Errorf("Type = %s, want RLRQ", parsed.Type)
	}
}

func TestRLRERoundTrip(t *testing.T) {
	rlre := EncodeRLRE()
	if rlre[0] != TagRLRE {
		t.Fatalf("RLRE tag = 0x%02x, want 0x%02x", rlre[0], TagRLRE)
	}
	if len(rlre) != 2 || rlre[1] != 0x00 {
		t.Fatalf("RLRE = %x, want 6300", rlre)
	}

	parsed, err := Parse(rlre)
	if err != nil {
		t.Fatalf("Parse RLRE: %v", err)
	}
	if parsed.Type != ApduRLRE {
		t.Errorf("Type = %s, want RLRE", parsed.Type)
	}
}

func TestABRTRoundTrip(t *testing.T) {
	abrt := EncodeABRT(0) // source = user
	if abrt[0] != TagABRT {
		t.Fatalf("ABRT tag = 0x%02x, want 0x%02x", abrt[0], TagABRT)
	}

	parsed, err := Parse(abrt)
	if err != nil {
		t.Fatalf("Parse ABRT: %v", err)
	}
	if parsed.Type != ApduABRT {
		t.Errorf("Type = %s, want ABRT", parsed.Type)
	}
}

func TestRLRQWireBytes(t *testing.T) {
	expected := []byte{0x62, 0x03, 0x80, 0x01, 0x00}
	got := EncodeRLRQ()
	if !bytes.Equal(got, expected) {
		t.Errorf("RLRQ = %x, want %x", got, expected)
	}
}

func TestRLREWireBytes(t *testing.T) {
	expected := []byte{0x63, 0x00}
	got := EncodeRLRE()
	if !bytes.Equal(got, expected) {
		t.Errorf("RLRE = %x, want %x", got, expected)
	}
}

func TestABRTWireBytes(t *testing.T) {
	expected := []byte{0x64, 0x03, 0x80, 0x01, 0x00}
	got := EncodeABRT(0)
	if !bytes.Equal(got, expected) {
		t.Errorf("ABRT(user) = %x, want %x", got, expected)
	}

	expected = []byte{0x64, 0x03, 0x80, 0x01, 0x01}
	got = EncodeABRT(1)
	if !bytes.Equal(got, expected) {
		t.Errorf("ABRT(provider) = %x, want %x", got, expected)
	}
}

func TestAARQContainsAppContextName(t *testing.T) {
	params := AARQParams{
		CalledAPTitle:  asn1.ObjectIdentifier{1, 1, 1, 1},
		CallingAPTitle: asn1.ObjectIdentifier{1, 1, 1, 1},
	}
	aarq, err := EncodeAARQ(params, []byte{0x01})
	if err != nil {
		t.Fatalf("EncodeAARQ: %v", err)
	}

	// [1] (0xa1) application-context-name should be present
	found := false
	for i := 2; i < len(aarq)-1; i++ {
		if aarq[i] == 0xa1 {
			found = true
			break
		}
	}
	if !found {
		t.Error("AARQ should contain application-context-name (tag 0xa1)")
	}
}

func TestAARQNoAPTitles(t *testing.T) {
	params := AARQParams{}
	aarq, err := EncodeAARQ(params, []byte{0x01})
	if err != nil {
		t.Fatalf("EncodeAARQ: %v", err)
	}

	parsed, err := Parse(aarq)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Type != ApduAARQ {
		t.Errorf("Type = %s, want AARQ", parsed.Type)
	}
	if !bytes.Equal(parsed.UserData, []byte{0x01}) {
		t.Errorf("UserData = %x, want 01", parsed.UserData)
	}
}

func TestParseTooShort(t *testing.T) {
	_, err := Parse(nil)
	if err == nil {
		t.Error("expected error for nil input")
	}
	_, err = Parse([]byte{0x60})
	if err == nil {
		t.Error("expected error for 1-byte input")
	}
}

func TestParseUnknownTag(t *testing.T) {
	_, err := Parse([]byte{0x70, 0x00})
	if err == nil {
		t.Error("expected error for unknown APDU tag")
	}
}

func TestApduTypeString(t *testing.T) {
	tests := []struct {
		t    ApduType
		want string
	}{
		{ApduAARQ, "AARQ"},
		{ApduAARE, "AARE"},
		{ApduRLRQ, "RLRQ"},
		{ApduRLRE, "RLRE"},
		{ApduABRT, "ABRT"},
		{ApduType(0x99), "ApduType(0x99)"},
	}
	for _, tt := range tests {
		if got := tt.t.String(); got != tt.want {
			t.Errorf("ApduType(0x%02x).String() = %q, want %q", byte(tt.t), got, tt.want)
		}
	}
}

func TestParseAAREMissingResult(t *testing.T) {
	// AARE with only app-context-name, no result field
	inner := []byte{0xa1, 0x02, 0x06, 0x00} // [1] app-context (empty OID)
	aare := append([]byte{TagAARE, byte(len(inner))}, inner...)
	_, err := Parse(aare)
	if err == nil {
		t.Error("expected error for AARE missing result")
	}
}

func TestParseExternalMissingPayload(t *testing.T) {
	// EXTERNAL with indirect-reference but no single-ASN1-type
	extContent := []byte{0x02, 0x01, 0x03} // indirect-reference = 3
	external := append([]byte{0x28, byte(len(extContent))}, extContent...)
	userInfo := append([]byte{0xbe, byte(len(external))}, external...)
	aarqContent := append([]byte{0xa1, 0x02, 0x06, 0x00}, userInfo...) // app-context + user-info
	aarq := append([]byte{TagAARQ, byte(len(aarqContent))}, aarqContent...)
	_, err := Parse(aarq)
	if err == nil {
		t.Error("expected error for EXTERNAL missing single-ASN1-type")
	}
}

func TestParseABRTMissingSource(t *testing.T) {
	abrt := []byte{TagABRT, 0x00} // empty ABRT
	_, err := Parse(abrt)
	if err == nil {
		t.Error("expected error for ABRT missing abort-source")
	}
}

func TestParseRLRQBadField(t *testing.T) {
	// RLRQ with unexpected field tag
	inner := []byte{0xA1, 0x01, 0x00} // [1] instead of [0]
	rlrq := append([]byte{TagRLRQ, byte(len(inner))}, inner...)
	_, err := Parse(rlrq)
	if err == nil {
		t.Error("expected error for RLRQ with unexpected field tag")
	}
}

// --- ACSE auth negative tests ---

// buildAARQ constructs an AARQ with the given inner TLV fields.
func buildAARQ(fields ...[]byte) []byte {
	var content []byte
	// Always include app-context-name (required)
	content = append(content, 0xa1, 0x02, 0x06, 0x00)
	for _, f := range fields {
		content = append(content, f...)
	}
	return append([]byte{TagAARQ, byte(len(content))}, content...)
}

// tlv is a test helper that builds a simple TLV.
func tlv(tag byte, value []byte) []byte {
	return append([]byte{tag, byte(len(value))}, value...)
}

func TestAARQPasswordAuthRoundTrip(t *testing.T) {
	params := AARQParams{Password: []byte("secret")}
	aarq, err := EncodeAARQ(params, []byte{0x01})
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := Parse(aarq)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Auth.Mechanism != AuthPassword {
		t.Errorf("mechanism = %v, want AuthPassword", parsed.Auth.Mechanism)
	}
	if string(parsed.Auth.Password) != "secret" {
		t.Errorf("password = %q, want secret", parsed.Auth.Password)
	}
}

func TestAARQAuthValueWithoutMechanism(t *testing.T) {
	// auth-value [12] present but no mechanism-name [11]
	authVal := tlv(0xac, tlv(0x80, []byte("pw")))
	aarq := buildAARQ(authVal)
	_, err := Parse(aarq)
	if err == nil {
		t.Fatal("expected error for auth-value without mechanism")
	}
	t.Logf("got expected error: %v", err)
}

func TestAARQPasswordMechanismWithoutAuthValue(t *testing.T) {
	// mechanism-name [11] = password OID, but no auth-value [12]
	mech := tlv(0x8b, authMechPasswordOID)
	aarq := buildAARQ(mech)
	_, err := Parse(aarq)
	if err == nil {
		t.Fatal("expected error for password mechanism without auth-value")
	}
	t.Logf("got expected error: %v", err)
}

func TestAARQUnknownMechanismOID(t *testing.T) {
	unknownOID := []byte{0x55, 0x04, 0x03} // BER for 2.5.4.3
	mech := tlv(0x8b, unknownOID)
	authVal := tlv(0xac, tlv(0x80, []byte("data")))
	aarq := buildAARQ(mech, authVal)

	parsed, err := Parse(aarq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed.Auth.Mechanism != AuthUnknown {
		t.Errorf("mechanism = %v, want AuthUnknown", parsed.Auth.Mechanism)
	}
	wantOID := asn1.ObjectIdentifier{2, 5, 4, 3}
	if !parsed.Auth.MechanismOID.Equal(wantOID) {
		t.Errorf("mechanism OID = %v, want %v", parsed.Auth.MechanismOID, wantOID)
	}
}

func TestAARQPasswordWrongInnerTag(t *testing.T) {
	// password mechanism, but auth-value uses wrong CHOICE tag
	mech := tlv(0x8b, authMechPasswordOID)
	authVal := tlv(0xac, tlv(0x81, []byte("pw"))) // 0x81 instead of 0x80
	aarq := buildAARQ(mech, authVal)
	_, err := Parse(aarq)
	if err == nil {
		t.Fatal("expected error for wrong auth-value inner tag")
	}
	t.Logf("got expected error: %v", err)
}

func TestAARQPasswordTrailingBytesInAuthValue(t *testing.T) {
	// password mechanism, auth-value has trailing bytes after the CHOICE
	mech := tlv(0x8b, authMechPasswordOID)
	inner := append(tlv(0x80, []byte("pw")), 0xFF) // trailing byte
	authVal := tlv(0xac, inner)
	aarq := buildAARQ(mech, authVal)
	_, err := Parse(aarq)
	if err == nil {
		t.Fatal("expected error for trailing bytes in auth-value")
	}
	t.Logf("got expected error: %v", err)
}

func TestAARQEmptyAuthValue(t *testing.T) {
	mech := tlv(0x8b, authMechPasswordOID)
	authVal := tlv(0xac, []byte{}) // empty
	aarq := buildAARQ(mech, authVal)
	_, err := Parse(aarq)
	if err == nil {
		t.Fatal("expected error for empty auth-value")
	}
	t.Logf("got expected error: %v", err)
}

func TestAARQMalformedAuthPlusValidUserInfo(t *testing.T) {
	// Malformed auth but valid user-information — should still reject
	mech := tlv(0x8b, authMechPasswordOID)
	authVal := tlv(0xac, []byte{0xFF}) // garbage auth content

	// Valid user-information
	params := AARQParams{}
	goodAARQ, _ := EncodeAARQ(params, []byte{0xDE, 0xAD})
	// Extract user-information from the good AARQ
	// user-info starts after app-context-name
	var userInfo []byte
	offset := 2 // skip AARQ tag+len
	for offset < len(goodAARQ) {
		tag := goodAARQ[offset]
		fieldLen := int(goodAARQ[offset+1])
		if tag == 0xbe {
			userInfo = goodAARQ[offset : offset+2+fieldLen]
			break
		}
		offset += 2 + fieldLen
	}

	aarq := buildAARQ(mech, authVal, userInfo)
	_, err := Parse(aarq)
	if err == nil {
		t.Fatal("expected error for malformed auth despite valid user-info")
	}
	t.Logf("got expected error: %v", err)
}

func TestAARQNoAuth(t *testing.T) {
	params := AARQParams{}
	aarq, err := EncodeAARQ(params, []byte{0x01})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(aarq)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Auth.Mechanism != AuthNone {
		t.Errorf("mechanism = %v, want AuthNone", parsed.Auth.Mechanism)
	}
	if parsed.Auth.MechanismOID != nil {
		t.Errorf("MechanismOID = %v, want nil", parsed.Auth.MechanismOID)
	}
	if parsed.Auth.CallingAPTitle != nil {
		t.Errorf("CallingAPTitle = %v, want nil", parsed.Auth.CallingAPTitle)
	}
	if parsed.Auth.CallingAEQualifier != nil {
		t.Errorf("CallingAEQualifier = %v, want nil", parsed.Auth.CallingAEQualifier)
	}
}

func TestAARQCallingAPTitleRoundTrip(t *testing.T) {
	params := AARQParams{
		CallingAPTitle:     asn1.ObjectIdentifier{1, 3, 9999, 13, 1},
		CallingAEQualifier: 12,
	}
	aarq, err := EncodeAARQ(params, []byte{0x01})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(aarq)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	wantTitle := asn1.ObjectIdentifier{1, 3, 9999, 13, 1}
	if !parsed.Auth.CallingAPTitle.Equal(wantTitle) {
		t.Errorf("CallingAPTitle = %v, want %v", parsed.Auth.CallingAPTitle, wantTitle)
	}
	if parsed.Auth.CallingAEQualifier == nil {
		t.Fatal("CallingAEQualifier is nil, want 12")
	}
	if *parsed.Auth.CallingAEQualifier != 12 {
		t.Errorf("CallingAEQualifier = %d, want 12", *parsed.Auth.CallingAEQualifier)
	}
}

func TestAARQCallingAPTitleWithPasswordAuth(t *testing.T) {
	params := AARQParams{
		CallingAPTitle:     asn1.ObjectIdentifier{1, 1, 1, 1},
		CallingAEQualifier: 5,
		Password:           []byte("pw123"),
	}
	aarq, err := EncodeAARQ(params, []byte{0x01})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(aarq)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Auth.Mechanism != AuthPassword {
		t.Errorf("mechanism = %v, want AuthPassword", parsed.Auth.Mechanism)
	}
	if string(parsed.Auth.Password) != "pw123" {
		t.Errorf("password = %q, want pw123", parsed.Auth.Password)
	}
	wantTitle := asn1.ObjectIdentifier{1, 1, 1, 1}
	if !parsed.Auth.CallingAPTitle.Equal(wantTitle) {
		t.Errorf("CallingAPTitle = %v, want %v", parsed.Auth.CallingAPTitle, wantTitle)
	}
	if parsed.Auth.CallingAEQualifier == nil || *parsed.Auth.CallingAEQualifier != 5 {
		t.Errorf("CallingAEQualifier = %v, want 5", parsed.Auth.CallingAEQualifier)
	}
}

func TestAARQPasswordMechanismOID(t *testing.T) {
	params := AARQParams{Password: []byte("pw")}
	aarq, err := EncodeAARQ(params, []byte{0x01})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(aarq)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	wantOID := asn1.ObjectIdentifier{2, 2, 3, 1}
	if !parsed.Auth.MechanismOID.Equal(wantOID) {
		t.Errorf("MechanismOID = %v, want %v", parsed.Auth.MechanismOID, wantOID)
	}
}

func TestParseRLRQTrailingBytes(t *testing.T) {
	rlrq := EncodeRLRQ()
	rlrq = append(rlrq, 0xff)
	_, err := Parse(rlrq)
	if err == nil {
		t.Fatal("expected error for trailing bytes after RLRQ")
	}
}

func TestParseRLRETrailingBytes(t *testing.T) {
	rlre := EncodeRLRE()
	rlre = append(rlre, 0xff)
	_, err := Parse(rlre)
	if err == nil {
		t.Fatal("expected error for trailing bytes after RLRE")
	}
}

func TestParseABRTTrailingBytes(t *testing.T) {
	abrt := EncodeABRT(0)
	abrt = append(abrt, 0xff)
	_, err := Parse(abrt)
	if err == nil {
		t.Fatal("expected error for trailing bytes after ABRT")
	}
}

func TestAARQCallingAPTitleOnly(t *testing.T) {
	params := AARQParams{
		CallingAPTitle: asn1.ObjectIdentifier{1, 2, 3},
	}
	aarq, err := EncodeAARQ(params, []byte{0x01})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(aarq)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Auth.CallingAPTitle == nil {
		t.Fatal("CallingAPTitle should not be nil")
	}
	if parsed.Auth.CallingAEQualifier == nil {
		t.Fatal("CallingAEQualifier should be set (paired with AP-title)")
	}
	// EncodeAARQ always pairs AP-title with AE-qualifier (default 0)
	if *parsed.Auth.CallingAEQualifier != 0 {
		t.Errorf("CallingAEQualifier = %d, want 0", *parsed.Auth.CallingAEQualifier)
	}
}
