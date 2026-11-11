// SPDX-License-Identifier: MIT

package acse

import (
	"bytes"
	"encoding/asn1"
	"testing"
)

// ISO 8650-1 sender-acse-requirements BIT STRING {authentication(0)} in
// canonical form (MMS-STK-21).
func TestSpec_8650_SenderRequirementsCanonical(t *testing.T) {
	b, err := EncodeAARQ(AARQParams{Password: []byte("pw")}, []byte{0xa8, 0x00})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte{0x8a, 0x02, 0x07, 0x80}) {
		t.Fatalf("AARQ %x lacks 8a 02 07 80", b)
	}
	p, err := Parse(b)
	if err != nil || !p.AARQ.AuthRequested || !p.AARQ.IsMMS() || !p.AARQ.ProtocolVersionOK {
		t.Fatalf("parsed %+v %v", p.AARQ, err)
	}
}

// ISO 8650-1 AARQ protocol-version, application-context-name and
// called AP-title are decoded for validation (MMS-STK-12).
func TestSpec_8650_AARQFieldsDecoded(t *testing.T) {
	aarq := buildAARQ(
		tlv(0x80, []byte{0x07, 0x00}), // protocol-version without version1
		tlv(0xa2, []byte{0x06, 0x02, 0x29, 0x01}),
		tlv(0xa3, []byte{0x02, 0x01, 0x05}),
	)
	p, err := Parse(aarq)
	if err != nil {
		t.Fatal(err)
	}
	if p.AARQ.ProtocolVersionOK || p.AARQ.IsMMS() {
		t.Fatalf("%+v", p.AARQ)
	}
	if !p.AARQ.CalledAPTitle.Equal(asn1.ObjectIdentifier{1, 1, 1}) || p.AARQ.CalledAEQualifier == nil || *p.AARQ.CalledAEQualifier != 5 {
		t.Fatalf("called %+v", p.AARQ)
	}
	if _, err := Parse(buildAARQ(tlv(0xa1, []byte{0x02, 0x01, 0x00}))); err == nil {
		t.Fatal("application-context-name that is not an OID accepted")
	}
}

// ISO 8650-1 AARE result-source-diagnostic, responding AP-title and
// responder-acse-requirements (MMS-STK-04, MMS-STK-19).
func TestSpec_8650_AAREDiagnostic(t *testing.T) {
	b := EncodeAAREWith(AAREParams{
		Result: ResultRejectedPerm, DiagSource: DiagSourceUser, Diagnostic: DiagUserAuthFailure,
		RespondingAPTitle: asn1.ObjectIdentifier{1, 3, 9999, 23}, RespondingAEQualifier: 23,
		ResponderAuth: true, MMSContextID: 5,
	}, []byte{0xaa, 0x05, 0xa0, 0x03, 0x87, 0x01, 0x03})
	for _, want := range [][]byte{
		{0xa3, 0x05, 0xa1, 0x03, 0x02, 0x01, 0x0d},
		{0xa4, 0x06, 0x06, 0x04, 0x2b, 0xce, 0x0f, 0x17},
		{0xa5, 0x03, 0x02, 0x01, 0x17},
		{0x88, 0x02, 0x07, 0x80},
		{0x02, 0x01, 0x05, 0xa0, 0x07, 0xaa},
	} {
		if !bytes.Contains(b, want) {
			t.Fatalf("AARE %x lacks %x", b, want)
		}
	}
	p, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	a := p.AARE
	if a.Result != 1 || a.DiagSource != DiagSourceUser || a.Diagnostic != 13 || !a.ResponderAuth ||
		!bytes.Equal(a.AppContext, MMSApplicationContext) || !bytes.Equal(a.UserData, []byte{0xaa, 0x05, 0xa0, 0x03, 0x87, 0x01, 0x03}) {
		t.Fatalf("%+v", a)
	}
	b = EncodeAAREWith(AAREParams{Result: 1, DiagSource: DiagSourceProvider, Diagnostic: DiagProviderNoCommonACSEVersion}, nil)
	if !bytes.Contains(b, []byte{0xa3, 0x05, 0xa2, 0x03, 0x02, 0x01, 0x02}) {
		t.Fatalf("provider diag %x", b)
	}
	if p, _ := Parse(b); p.AARE.DiagSource != DiagSourceProvider || p.AARE.Diagnostic != 2 {
		t.Fatalf("%+v", p.AARE)
	}
	res := []byte{0xa2, 0x03, 0x02, 0x01, 0x00}
	for _, bad := range [][]byte{
		append(res[:5:5], 0xa3, 0x02, 0xa3, 0x00),
		append(res[:5:5], 0xa3, 0x01, 0xa1),
		append(res[:5:5], 0xa3, 0x02, 0xa1, 0x00),
		append(res[:5:5], 0xa3, 0x04, 0xa1, 0x02, 0x02, 0x00),
		append(res[:5:5], 0xa3, 0x05, 0xa4, 0x03, 0x02, 0x01, 0x00),
		append(res[:5:5], 0xa1, 0x02, 0x02, 0x00),
	} {
		if _, err := parseAARE(bad); err == nil {
			t.Errorf("AARE content %x accepted", bad)
		}
	}
}

// X.208 EXTERNAL octet-aligned encoding is accepted (MMS-STK-17).
func TestSpec_8650_ExternalOctetAligned(t *testing.T) {
	got, err := parseExternalPayload([]byte{0x28, 0x07, 0x02, 0x01, 0x03, 0x81, 0x02, 0xa9, 0x00})
	if err != nil || !bytes.Equal(got, []byte{0xa9, 0x00}) {
		t.Fatalf("%x %v", got, err)
	}
	if _, err := parseExternalPayload([]byte{0x28, 0x07, 0x02, 0x01, 0x03, 0x82, 0x02, 0x00, 0x00}); err == nil {
		t.Fatal("arbitrary accepted")
	}
}

// ISO 8650-1 ABRT with abort-diagnostic and RLRQ with user-information
// are valid (MMS-STK-16).
func TestSpec_8650_ABRTAndRLRQExtensions(t *testing.T) {
	p, err := Parse([]byte{0x64, 0x06, 0x80, 0x01, 0x01, 0x81, 0x01, 0x01})
	if err != nil || p.AbortSource != AbortSourceProvider || p.AbortDiagnostic != 1 {
		t.Fatalf("ABRT %+v %v", p, err)
	}
	// user-information is SEQUENCE OF EXTERNAL; an empty one is valid.
	p, err = Parse([]byte{0x62, 0x05, 0x80, 0x01, 0x00, 0xbe, 0x00})
	if err != nil || p.Reason != 0 || p.UserData != nil {
		t.Fatalf("RLRQ with empty user-information: %+v %v", p, err)
	}
	p, err = Parse([]byte{0x62, 0x03, 0x80, 0x01, 0x00})
	if err != nil || p.Reason != 0 {
		t.Fatalf("RLRQ %+v %v", p, err)
	}
	p, err = Parse(EncodeRLRE())
	if err != nil || p.Reason != -1 {
		t.Fatalf("RLRE %+v %v", p, err)
	}
}
