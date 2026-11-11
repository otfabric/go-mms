// SPDX-License-Identifier: MIT

package acse

import (
	"encoding/asn1"
	"testing"

	"github.com/otfabric/go-mms/internal/berutil"
)

func TestMarshalOID_Error(t *testing.T) {
	// Invalid OID: first arc must be 0–2.
	if _, err := marshalOID(asn1.ObjectIdentifier{3, 1}); err == nil {
		t.Fatal("expected marshalOID error")
	}
}

func TestEncodeAARQ_OIDErrors(t *testing.T) {
	bad := asn1.ObjectIdentifier{3, 999}
	if _, err := EncodeAARQ(AARQParams{CalledAPTitle: bad}, nil); err == nil {
		t.Fatal("called AP-title")
	}
	if _, err := EncodeAARQ(AARQParams{CallingAPTitle: bad}, nil); err == nil {
		t.Fatal("calling AP-title")
	}
}

func TestParseAARE_Edges(t *testing.T) {
	if _, err := parseAARE([]byte{0xff}); err == nil {
		t.Fatal("field TLV")
	}
	// Bad result inner TLV.
	badResult := berutil.EncodeTLV(0xa2, []byte{0x02, 0x05})
	if _, err := parseAARE(badResult); err == nil {
		t.Fatal("result TLV")
	}
	// Empty INTEGER.
	emptyInt := berutil.EncodeTLV(0xa2, berutil.EncodeTLV(0x02, nil))
	if _, err := parseAARE(emptyInt); err == nil {
		t.Fatal("result value")
	}
	// Missing result.
	if _, err := parseAARE(berutil.EncodeTLV(0xa1, nil)); err == nil {
		t.Fatal("missing result")
	}
	// Invalid result value.
	badVal := berutil.EncodeTLV(0xa2, berutil.EncodeTLV(0x02, []byte{9}))
	if _, err := parseAARE(badVal); err == nil {
		t.Fatal("invalid result")
	}
	// Bad user-information.
	okResult := berutil.EncodeTLV(0xa2, berutil.EncodeTLV(0x02, []byte{0}))
	badUI := append(okResult, berutil.EncodeTLV(0xbe, []byte{0xff})...)
	if _, err := parseAARE(badUI); err == nil {
		t.Fatal("bad user-info")
	}
	// Unknown field skipped + accepted result.
	skip := append(berutil.EncodeTLV(0xa9, []byte{1}), okResult...)
	aare, err := parseAARE(skip)
	if err != nil || aare.Result != ResultAccepted {
		t.Fatalf("%+v err=%v", aare, err)
	}
}

func TestParseAARQ_Edges(t *testing.T) {
	if _, _, err := parseAARQ([]byte{0xff}); err == nil {
		t.Fatal("field TLV")
	}
	// Bad calling AP-title OID.
	badTitle := berutil.EncodeTLV(0xa6, []byte{0xff})
	if _, _, err := parseAARQ(badTitle); err == nil {
		t.Fatal("calling AP-title")
	}
	// Bad AE-qualifier.
	badAE := berutil.EncodeTLV(0xa7, []byte{0xff})
	if _, _, err := parseAARQ(badAE); err == nil {
		t.Fatal("AE qualifier")
	}
	// Bad mechanism OID bytes.
	mech := berutil.EncodeTLV(0x8b, []byte{0xff, 0xff, 0xff})
	if _, _, err := parseAARQ(mech); err == nil {
		t.Fatal("mechanism OID")
	}
	// Bad user-information.
	if _, _, err := parseAARQ(berutil.EncodeTLV(0xbe, []byte{0xff})); err == nil {
		t.Fatal("user-info")
	}
}

func TestParseExternalPayload_Edges(t *testing.T) {
	if _, err := parseExternalPayload(nil); err == nil {
		t.Fatal("empty")
	}
	if _, err := parseExternalPayload([]byte{0xff}); err == nil {
		t.Fatal("TLV")
	}
	if _, err := parseExternalPayload(berutil.EncodeTLV(0x30, nil)); err == nil {
		t.Fatal("wrong tag")
	}
	if _, err := parseExternalPayload(berutil.EncodeTLV(0x28, []byte{0xa0, 0x05})); err == nil {
		t.Fatal("truncated field")
	}
	// Missing 0xa0.
	if _, err := parseExternalPayload(berutil.EncodeTLV(0x28, berutil.EncodeTLV(0x02, []byte{3}))); err == nil {
		t.Fatal("missing single-ASN1")
	}
	ok := berutil.EncodeTLV(0x28, append(
		berutil.EncodeTLV(0x02, []byte{3}),
		berutil.EncodeTLV(0xa0, []byte{0xab})...,
	))
	got, err := parseExternalPayload(ok)
	if err != nil || string(got) != "\xab" {
		t.Fatalf("%x %v", got, err)
	}
}

// ISO 8650-1 RLRQ/RLRE/ABRT are extensible SEQUENCEs: optional and
// unknown trailing elements are accepted (MMS-STK-16); malformed values
// are not.
func TestParseReleaseAndABRT_Edges(t *testing.T) {
	r := &ParsedAPDU{Type: ApduRLRQ, Reason: -1}
	if err := parseRelease(nil, r); err != nil || r.Reason != -1 {
		t.Fatal(err)
	}
	if err := parseRelease([]byte{0xff}, r); err == nil {
		t.Fatal("RLRQ TLV")
	}
	if err := parseRelease(berutil.EncodeTLV(0x80, nil), r); err == nil {
		t.Fatal("RLRQ empty reason")
	}
	if err := parseRelease(berutil.EncodeTLV(0xbe, []byte{0xff}), r); err == nil {
		t.Fatal("RLRQ bad user-information")
	}
	if err := parseRelease(append(berutil.EncodeTLV(0x80, []byte{1}), berutil.EncodeTLV(0xad, nil)...), r); err != nil || r.Reason != 1 {
		t.Fatalf("RLRQ with aso-qualifier: %v reason %d", err, r.Reason)
	}

	a := &ParsedAPDU{Type: ApduABRT, AbortSource: -1, AbortDiagnostic: -1}
	if err := parseABRT(nil, a); err == nil {
		t.Fatal("ABRT empty")
	}
	if err := parseABRT([]byte{0xff}, a); err == nil {
		t.Fatal("ABRT TLV")
	}
	if err := parseABRT(berutil.EncodeTLV(0x81, []byte{1}), a); err == nil {
		t.Fatal("ABRT without source")
	}
	if err := parseABRT(berutil.EncodeTLV(0x80, nil), a); err == nil {
		t.Fatal("ABRT empty source")
	}
	if err := parseABRT(append(berutil.EncodeTLV(0x80, []byte{0}), berutil.EncodeTLV(0xbe, []byte{0xff})...), a); err == nil {
		t.Fatal("ABRT bad user-information")
	}
	a = &ParsedAPDU{Type: ApduABRT, AbortSource: -1, AbortDiagnostic: -1}
	if err := parseABRT(append(berutil.EncodeTLV(0x80, []byte{0}), 0xa9, 0x00), a); err != nil || a.AbortSource != 0 {
		t.Fatal(err)
	}
}
