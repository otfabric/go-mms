// SPDX-License-Identifier: MIT

package pdu

import (
	"bytes"
	"encoding/asn1"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/otfabric/go-mms/internal/codec"
)

func rawCompound(tag int, content []byte) asn1.RawValue {
	return asn1.RawValue{Class: 2, Tag: tag, IsCompound: true, Bytes: content}
}

func hx(t testing.TB, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatalf("hex %q: %v", s, err)
	}
	return b
}

// TestSpec_9506_2_8_4_3_ConcludeErrorPDU: conclude-ErrorPDU is
// [13] IMPLICIT Conclude-ErrorPDU and Conclude-ErrorPDU ::= ServiceError,
// a SEQUENCE, so the tag is 0xad (ISO 9506-2 clauses 7 and 8.4.3;
// MMS-PDU-01).
func TestSpec_9506_2_8_4_3_ConcludeErrorPDU(t *testing.T) {
	kind, content, err := DecodePdu(hx(t, "ad05a003890101"))
	if err != nil || kind != PduConcludeError {
		t.Fatalf("DecodePdu = %v, %v", kind, err)
	}
	se, err := DecodeConcludeError(content)
	if err != nil || se.ErrorClass != 9 || se.ErrorCode != 1 {
		t.Fatalf("DecodeConcludeError = %+v, %v; want conclude/further-communication-required", se, err)
	}
	if got := codec.MarshalConcludeError(9, 1); !bytes.Equal(got, hx(t, "ad05a003890101")) {
		t.Fatalf("MarshalConcludeError = %x", got)
	}
	// The primitive form 0x8d is not an MMSpdu alternative.
	if _, _, err := DecodePdu(hx(t, "8d00")); err == nil {
		t.Fatal("primitive 0x8d accepted as Conclude-ErrorPDU")
	}
	if _, err := DecodeConcludeError(nil); err == nil {
		t.Fatal("empty Conclude-ErrorPDU accepted")
	}
}

// TestSpec_X690_8_1_3_BEREnvelopes: Confirmed-Response envelopes in the
// BER forms DER forbids decode to the same invokeID and service
// (X.690 §8.1.3.5 NOTE 2, §8.1.3.6, §8.3.2; MMS-PDU-06).
func TestSpec_X690_8_1_3_BEREnvelopes(t *testing.T) {
	for _, in := range []string{
		"a1 07 020101 82020000",
		"a1 8107 020101 82020000",
		"a1 820007 020101 82020000",
		"a1 08 02020001 82020000",
		"a1 80 020101 82020000 0000",
		"a1 80 020101 a2800000 0000",
	} {
		kind, content, err := DecodePdu(hx(t, in))
		if err != nil || kind != PduConfirmedResponse {
			t.Errorf("DecodePdu(%s) = %v, %v", in, kind, err)
			continue
		}
		id, svc, err := codec.UnmarshalConfirmedResponse(content)
		if err != nil || id != 1 || svc.Tag != 2 {
			t.Errorf("envelope(%s) = %d, %+v, %v", in, id, svc, err)
		}
	}
	// A primitive element cannot use the indefinite form (§8.1.3.2 a).
	if _, _, err := DecodePdu(hx(t, "8b800000")); err == nil {
		t.Error("indefinite primitive Conclude-Request accepted")
	}
}

// TestSpec_X690_8_6_InitiateBitStringPadding: unused BIT STRING bits are
// only required to be zero in DER/CER (X.690 §11.2.1); an
// Initiate-Response with non-zero padding and non-minimal INTEGERs
// decodes (MMS-PDU-06).
func TestSpec_X690_8_6_InitiateBitStringPadding(t *testing.T) {
	for name, in := range map[string]string{
		"padding 1f":          "800300fde8810105820105830105a40e800101810305f100820403ee1c1f",
		"padding zero":        "800300fde8810105820105830105a40e800101810305f100820403ee1c18",
		"non-minimal integer": "80040000fde881020005820105830105a40f80020001810305f100820403ee1c1f",
		"explicit SEQUENCE":   "301e800300fde8810105820105830105a40e800101810305f100820403ee1c1f",
	} {
		resp, err := UnmarshalInitiateResponse(hx(t, in))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if resp.LocalDetailCalled != 65000 || resp.NegotiatedMaxServOutstandingCall != 5 || resp.InitResponseDetail.NegotiatedVersion != 1 ||
			resp.InitResponseDetail.ServicesSupportedCalled.BitLength != 21 || resp.InitResponseDetail.ServicesSupportedCalled.Bytes[2] != 0x18 {
			t.Errorf("%s: %+v", name, resp)
		}
	}
	req, err := UnmarshalInitiateRequest(hx(t, "800300fde8810105820105830105a40e800101810305f107820403ee1c1f"))
	if err != nil || req.InitRequestDetail.ProposedParamCBB.Bytes[1] != 0x00 {
		t.Fatalf("UnmarshalInitiateRequest = %+v, %v", req, err)
	}
	st, err := UnmarshalStatusResponse(rawCompound(0, hx(t, "80020000 810100")))
	if err != nil || st.VMDLogicalStatus != 0 || st.VMDPhysicalStatus != 0 {
		t.Fatalf("UnmarshalStatusResponse = %+v, %v", st, err)
	}
	if got := maskBitStringPadding([]byte{0x08, 0xff}); !bytes.Equal(got, []byte{0x08, 0xff}) {
		t.Errorf("invalid unused-bit count changed: %x", got)
	}
	if got := normalizeInitiate([]byte{0x30, 0x05}); !bytes.Equal(got, []byte{0x30, 0x05}) {
		t.Errorf("malformed SEQUENCE changed: %x", got)
	}
	if got := normalizeFields([]byte{0x80, 0x05}, statusRules, nil); !bytes.Equal(got, []byte{0x80, 0x05}) {
		t.Errorf("malformed fields changed: %x", got)
	}
}

// TestSpec_9506_2_7_4_1_ServiceError covers the ServiceError SEQUENCE
// (clause 7.4.1) and the Confirmed-ErrorPDU (clause 7.4; MMS-PDU-10).
func TestSpec_9506_2_7_4_1_ServiceError(t *testing.T) {
	ce, err := DecodeConfirmedError(hx(t, "800101 a20a a003870102 820378797a"))
	if err != nil || ce.ErrorClass != 7 || ce.ErrorCode != 2 || ce.Detail.AdditionalDescription == nil || *ce.Detail.AdditionalDescription != "xyz" {
		t.Fatalf("additionalDescription: %+v, %v", ce, err)
	}
	ce, err = DecodeConfirmedError(hx(t, "800101 810100 a205a003820101"))
	if err != nil || ce.ModifierPosition == nil || *ce.ModifierPosition != 0 || ce.ErrorClass != 2 {
		t.Fatalf("modifierPosition: %+v, %v", ce, err)
	}
	ce, err = DecodeConfirmedError(hx(t, "800101 a20d a003820101 810107 a303860103"))
	if err != nil || ce.Detail.AdditionalCode == nil || *ce.Detail.AdditionalCode != 7 || ce.Detail.ServiceSpecific == nil ||
		ce.Detail.ServiceSpecific.Kind != 6 || !ce.Detail.ServiceSpecific.HasValue || ce.Detail.ServiceSpecific.Value != 3 {
		t.Fatalf("serviceSpecificInfo deleteNVL: %+v, %v", ce, err)
	}
	for name, in := range map[string]string{
		"no errorClass":             "800101 a2028100",
		"constructed class":         "800101 a204a002a000",
		"empty code":                "800101 a204a0028000",
		"class 13":                  "800101 a205a0038d0101",
		"duplicate errorClass":      "800101 a20aa003800101a003800101",
		"fields out of order":       "800101 a20ba003800101820141810100",
		"universal in ServiceError": "800101 a207a003800101 0500",
		"bad description":           "800101 a209a003800101820201ff",
		"bad additionalCode":        "800101 a207a0038001018100",
		"specific two alternatives": "800101 a20da003800101a306800101800102",
		"specific universal":        "800101 a209a003800101a3020500",
		"invokeID not first":        "a205a003800101 800101",
		"universal invokeID":        "020101 a205a003800101",
		"missing serviceError":      "800101",
		"modifierPosition twice":    "800101 810100 810100 a205a003800101",
		"after serviceError":        "800101 a205a003800101 810100",
		"bad modifierPosition":      "800101 8100 a205a003800101",
		"empty":                     "",
	} {
		if _, err := DecodeConfirmedError(hx(t, in)); err == nil {
			t.Errorf("%s: DecodeConfirmedError(%s) accepted", name, in)
		}
	}
}

// TestSpec_9506_2_8_6_RejectPDU: originalInvokeID [0] OPTIONAL first,
// then exactly one non-empty rejectReason (clause 8.6; MMS-PDU-25).
func TestSpec_9506_2_8_6_RejectPDU(t *testing.T) {
	rj, err := DecodeRejectPDU(hx(t, "850101"))
	if err != nil || rj.HasInvokeID || rj.RejectType != 5 || rj.RejectReason != 1 {
		t.Fatalf("no invokeID: %+v, %v", rj, err)
	}
	for name, in := range map[string]string{
		"duplicate reason":       "800101 810101 810102",
		"invokeID after reason":  "810101 800101",
		"duplicate invokeID":     "800101 800102 810101",
		"empty confirmed reason": "800101 8100",
		"no reason":              "800101",
	} {
		if _, err := DecodeRejectPDU(hx(t, in)); err == nil {
			t.Errorf("%s: DecodeRejectPDU(%s) accepted", name, in)
		}
	}
}

// TestSpec_9506_2_7_InvokeIDTagPerPDU: the invokeID of a
// Confirmed-ResponsePDU is an untagged INTEGER, that of a
// Confirmed-ErrorPDU and RejectPDU is [0] IMPLICIT (clause 7; MMS-PDU-25).
func TestSpec_9506_2_7_InvokeIDTagPerPDU(t *testing.T) {
	if id, err := ExtractInvokeIDFor(PduConfirmedResponse, hx(t, "020107")); err != nil || id != 7 {
		t.Errorf("response: %d, %v", id, err)
	}
	if id, err := ExtractInvokeIDFor(PduConfirmedError, hx(t, "800107")); err != nil || id != 7 {
		t.Errorf("error: %d, %v", id, err)
	}
	for _, tc := range []struct {
		kind PduKind
		in   string
	}{
		{PduConfirmedResponse, "800107"},
		{PduConfirmedError, "020107"},
		{PduReject, "020107"},
		{PduReject, "850101"},
		{PduReject, ""},
		{PduConfirmedError, "80050080000000"},
		{PduConfirmedError, "8000"},
	} {
		if _, err := ExtractInvokeIDFor(tc.kind, hx(t, tc.in)); err == nil {
			t.Errorf("ExtractInvokeIDFor(%s, %s) accepted", tc.kind, tc.in)
		}
	}
}

func TestRecoverInvokeID(t *testing.T) {
	for in, want := range map[string]uint32{
		"a120020107a200":   7,
		"a28105800109a200": 9,
		"a40a800103":       3,
		"a1 820100 020102": 2,
	} {
		_, id, ok := RecoverInvokeID(hx(t, in))
		if !ok || id != want {
			t.Errorf("RecoverInvokeID(%s) = %d, %v; want %d", in, id, ok, want)
		}
	}
	for _, in := range []string{"", "a1", "a0200201 07", "a320020107", "a120800107", "a185", "a1200201", "ff00"} {
		if _, _, ok := RecoverInvokeID(hx(t, in)); ok {
			t.Errorf("RecoverInvokeID(%s) recovered an ID", in)
		}
	}
}

// FuzzDecodeServiceError checks the ServiceError decoder never panics and
// that a decoded error class is one of [0]..[12].
func FuzzDecodeServiceError(f *testing.F) {
	for _, s := range []string{"a003890101", "a003870102820378797a", "a003820101810107a303860103", "a002a000", "8100"} {
		f.Add(hx(f, s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		se, err := DecodeServiceError(data)
		if err == nil && (se.ErrorClass < 0 || se.ErrorClass > 12) {
			t.Fatalf("DecodeServiceError(%x) class %d", data, se.ErrorClass)
		}
		_, _ = DecodeConfirmedError(data)
		_, _ = DecodeRejectPDU(data)
	})
}

// FuzzRecoverInvokeID checks RecoverInvokeID never panics and only
// reports IDs within Unsigned32.
func FuzzRecoverInvokeID(f *testing.F) {
	for _, s := range []string{"a120020107a200", "a28105800109a200", "a40a800103", "a1820100020102", "a1"} {
		f.Add(hx(f, s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if _, id, ok := RecoverInvokeID(data); ok && id > codec.MaxInvokeID {
			t.Fatalf("RecoverInvokeID(%x) = %d", data, id)
		}
	})
}
