// SPDX-License-Identifier: MIT

package pdu

import (
	"bytes"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

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

// TestSpec_9506_2_14_2_2_9_AlternateAccess: inside selectAlternateAccess
// the nested AlternateAccess is untagged (universal SEQUENCE OF, 0x30);
// [5] is the named alternative of a SEQUENCE OF element, and several
// elements are sibling selections, not a path (MMS-PDU-05).
func TestSpec_9506_2_14_2_2_9_AlternateAccess(t *testing.T) {
	golden := []struct {
		chain []AccessSelectorWire
		wire  string
	}{
		{[]AccessSelectorWire{{Component: "a"}, {Component: "b"}}, "a008 800161 3003 810162"},
		{[]AccessSelectorWire{{HasIndex: true, Index: 2}, {Component: "q"}}, "a008 810102 3003 810171"},
		{[]AccessSelectorWire{{Component: "a"}, {HasIndex: true, Index: 1}, {Component: "c"}}, "a00f 800161 300a a008 810101 3003 810163"},
		{[]AccessSelectorWire{{Component: "q"}}, "810171"},
	}
	for _, g := range golden {
		enc, err := encodeAlternateAccess(g.chain)
		if err != nil || !bytes.Equal(enc, hx(t, g.wire)) {
			t.Errorf("encode %+v = %x, %v; want %s", g.chain, enc, err, g.wire)
		}
		dec, err := decodeAlternateAccess(hx(t, g.wire))
		if err != nil || len(dec) != len(g.chain) {
			t.Errorf("decode %s = %+v, %v", g.wire, dec, err)
			continue
		}
		for i := range dec {
			if dec[i].Component != g.chain[i].Component || dec[i].HasIndex != g.chain[i].HasIndex || dec[i].Index != g.chain[i].Index {
				t.Errorf("decode %s level %d = %+v", g.wire, i, dec[i])
			}
		}
	}
	// go-mms up to v1.7.2 wrapped the nested chain in [5]; accepted.
	if dec, err := decodeAlternateAccess(hx(t, "a008 800161 a503 810162")); err != nil || len(dec) != 2 || dec[1].Component != "b" {
		t.Errorf("legacy [5] nesting: %+v, %v", dec, err)
	}
	for name, in := range map[string]string{
		"named":              "a506 80016e 810171",
		"sibling selections": "810161 810162",
		"allElements":        "8400",
		"nested allElements": "a006 8300 3002 8400",
		"nested siblings":    "a00b 800161 3006 810162 810163",
	} {
		if _, err := decodeAlternateAccess(hx(t, in)); !errors.Is(err, ErrUnsupportedAlternateAccess) {
			t.Errorf("%s: decode(%s) = %v, want ErrUnsupportedAlternateAccess", name, in, err)
		}
	}
	for name, in := range map[string]string{
		"missing nested":   "a003 800161",
		"wrong nested tag": "a008 800161 a103 810162",
		"trailing":         "a00a 800161 3003 810162 0500",
		"bad sibling":      "810161 81",
		"empty":            "",
	} {
		if _, err := decodeAlternateAccess(hx(t, in)); err == nil || errors.Is(err, ErrUnsupportedAlternateAccess) {
			t.Errorf("%s: decode(%s) = %v, want a decode error", name, in, err)
		}
	}
}

// TestSpec_9506_2_DeleteNamedVariableListRequest: listOfVariableListName
// and domainName are OPTIONAL, scopeOfDelete is 0..3 (MMS-PDU-09).
func TestSpec_9506_2_DeleteNamedVariableListRequest(t *testing.T) {
	ok := []struct {
		in     string
		scope  int
		names  int
		domain string
	}{
		{"800103", 3, 0, ""},
		{"8001028203444f4d", 2, 0, "DOM"},
		{"a10580034e564c", 0, 1, ""},
		{"800100a10580034e564c", 0, 1, ""},
		{"800102a1008203444f4d", 2, 0, "DOM"},
		{"800101", 1, 0, ""},
	}
	for _, tc := range ok {
		r, err := UnmarshalDeleteNVLRequest(hx(t, tc.in))
		if err != nil || r.ScopeOfDelete != tc.scope || len(r.ListNames) != tc.names || r.DomainName != tc.domain {
			t.Errorf("%s = %+v, %v", tc.in, r, err)
		}
	}
	for _, in := range []string{"800104a100", "8001ff", "800102", "a100800103", "8200", "8203444f4d a100", "0500"} {
		if _, err := UnmarshalDeleteNVLRequest(hx(t, in)); err == nil {
			t.Errorf("%s accepted", in)
		}
	}
}

// TestSpec_8_1_8_1_3_8_EntryTimeSixOctets: an EntryTime is a TimeOfDay of
// exactly six octets (IEC 61850-8-1 clause 8.1.3.8); times before the
// 1984 epoch are clamped to it (MMS-PDU-29).
func TestSpec_8_1_8_1_3_8_EntryTimeSixOctets(t *testing.T) {
	for _, tm := range []time.Time{time.Unix(0, 0), time.Unix(1000, 0), epoch1984, time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)} {
		b, err := EncodeEntryTime(tm)
		if err != nil || len(b) != 6 {
			t.Fatalf("EncodeEntryTime(%v) = %x, %v; want 6 octets", tm, b, err)
		}
	}
	if b, _ := EncodeEntryTime(time.Unix(0, 0)); !bytes.Equal(b, make([]byte, 6)) {
		t.Errorf("Unix epoch = %x, want the 1984 epoch 000000000000", b)
	}
	req, err := MarshalReadJournalTimeRange(1, "D", "J", time.Unix(0, 0), time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(req, hx(t, "a1088006000000000000")) {
		t.Errorf("startingTime not 6 octets: %x", req)
	}
	req, err = MarshalReadJournalStartAfter(1, "D", "J", time.Unix(5, 0), []byte{1})
	if err != nil || !bytes.Contains(req, hx(t, "8006000000000000")) {
		t.Errorf("timeSpecification not 6 octets: %x, %v", req, err)
	}
	if _, err := marshalJournalEntry(JournalEntryWire{EntryID: []byte{1}, OccurrenceTime: time.Unix(0, 0)}); err != nil {
		t.Fatal(err)
	}
}

// TestSpec_9506_2_23_8_1_JournalAnnotation: entryForm is a CHOICE of
// data [2] and annotation [3] MMSString, explicitly tagged (ISO 9506-2
// clause 23.8.1); IEC 61850-8-1 Table 125 makes annotation mandatory for
// a client (MMS-PDU-13).
func TestSpec_9506_2_23_8_1_JournalAnnotation(t *testing.T) {
	entry := func(content string) []byte {
		c := hx(t, "8006000000000000"+content)
		inner := append(hx(t, "800101a1023000"), append([]byte{0xa2, byte(len(c))}, c...)...)
		list := append([]byte{0x30, byte(len(inner))}, inner...)
		return append([]byte{0xa0, byte(len(list))}, list...)
	}
	for _, s := range []string{"a3051a03616263", "a3050c03616263"} {
		got, _, err := UnmarshalReadJournalResponse(entry(s))
		if err != nil || len(got) != 1 || !got[0].HasAnnotation || got[0].Annotation != "abc" || len(got[0].Variables) != 0 {
			t.Errorf("%s = %+v, %v", s, got, err)
		}
	}
	for _, s := range []string{
		"",                   // no entryForm
		"a200a3051a03616263", // data and annotation
		"a3041a03616263",     // inner string overruns [3]
		"a3058003616263",     // not an MMSString
	} {
		if _, _, err := UnmarshalReadJournalResponse(entry(s)); err == nil {
			t.Errorf("entry content %q accepted", s)
		}
	}
	wire, err := MarshalReadJournalResponse([]JournalEntryWire{{EntryID: []byte{1}, OccurrenceTime: epoch1984, HasAnnotation: true, Annotation: "abc"}}, false)
	if err != nil || !bytes.Contains(wire, hx(t, "a3051a03616263")) {
		t.Fatalf("annotation encode = %x, %v", wire, err)
	}
	got, _, err := UnmarshalReadJournalResponse(wire)
	if err != nil || !got[0].HasAnnotation || got[0].Annotation != "abc" {
		t.Fatalf("round trip = %+v, %v", got, err)
	}
	if _, err := MarshalReadJournalResponse([]JournalEntryWire{{EntryID: []byte{1}, OccurrenceTime: epoch1984, HasAnnotation: true,
		Variables: []JournalVariableWire{{Tag: "x", Value: &DataValue{Tag: TagDataBoolean, Bool: true}}}}}, false); err == nil {
		t.Error("data and annotation encoded together")
	}
}

// TestSpec_9506_2_7_5_7_FileNameSequence: FileName is a SEQUENCE OF
// GraphicString (ISO 9506-2 clause 7.5.7) of at most 255 octets
// (IEC 61850-8-1 clause 23.1) (MMS-PDU-14).
func TestSpec_9506_2_7_5_7_FileNameSequence(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"1903646972 1903612e62", "dir/a.b"},
		{"a00a 1903646972 1903612e62", "dir/a.b"},
		{"1903612e62", "a.b"},
	} {
		got, err := decodeFileName(hx(t, tc.in))
		if err != nil || got != tc.want {
			t.Errorf("%s = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	long := append([]byte{0x19, 0x81, 0xff}, bytes.Repeat([]byte{'a'}, 255)...)
	if got, err := decodeFileName(long); err != nil || len(got) != 255 {
		t.Errorf("255-octet name: %d, %v", len(got), err)
	}
	// 128 + "/" + 127 = 256 octets.
	split := append(append([]byte{0x19, 0x81, 0x80}, bytes.Repeat([]byte{'a'}, 128)...), append([]byte{0x19, 0x7f}, bytes.Repeat([]byte{'b'}, 127)...)...)
	for _, in := range [][]byte{split, hx(t, "1903646972 1a03612e62"), hx(t, "a000"), hx(t, "1903646972 19")} {
		if _, err := decodeFileName(in); err == nil {
			t.Errorf("%x accepted", in)
		}
	}
	tooLong := strings.Repeat("a", 256)
	if _, err := MarshalFileOpenRequest(1, tooLong, 0); err == nil {
		t.Error("FileOpen with a 256-octet name encoded")
	}
	if _, err := MarshalFileRenameRequest(1, "a", tooLong); err == nil {
		t.Error("FileRename with a 256-octet name encoded")
	}
	if _, err := MarshalFileDeleteRequest(1, tooLong); err == nil {
		t.Error("FileDelete with a 256-octet name encoded")
	}
	if _, err := MarshalFileDirectoryRequest(1, "", tooLong); err == nil {
		t.Error("FileDirectory with a 256-octet name encoded")
	}
	if _, err := MarshalObtainFileRequest(1, tooLong, "b"); err == nil {
		t.Error("ObtainFile with a 256-octet name encoded")
	}
}

// TestSpec_9506_2_14_9_GetVarAccessResponse: typeDescription is a
// TypeDescription with no typeName alternative (MMS-PDU-17); each
// TypeSpecification alternative is context-specific with a fixed form,
// decoded exactly, and the optional [3]/[4] after typeDescription are
// tolerated (ISO 9506-2 clauses 14.2.1 and 14.9; MMS-PDU-18).
func TestSpec_9506_2_14_9_GetVarAccessResponse(t *testing.T) {
	if _, err := MarshalGetVarAccessResponse(false, TypeSpecWire{Tag: tsTagTypeName, TypeName: &ObjectNameWire{Scope: 0, ItemID: "T"}}); !errors.Is(err, ErrTypeNameNotDescription) {
		t.Errorf("typeName typeDescription: %v", err)
	}
	named := TypeSpecWire{Tag: tsTagStructure, Components: []StructComponentWire{{Name: "a", Type: TypeSpecWire{Tag: tsTagTypeName, TypeName: &ObjectNameWire{Scope: 0, ItemID: "T"}}}}}
	if _, err := MarshalGetVarAccessResponse(false, named); err != nil {
		t.Errorf("nested typeName (a TypeSpecification): %v", err)
	}
	for _, in := range []string{"8300", "a003800154", "a706020120020108"} {
		if _, err := DecodeTypeSpec(hx(t, in)); err != nil {
			t.Errorf("DecodeTypeSpec(%s): %v", in, err)
		}
	}
	for _, in := range []string{"300102", "a300", "8700", "c30100", "830000", "820100"} {
		if _, err := DecodeTypeSpec(hx(t, in)); err == nil {
			t.Errorf("DecodeTypeSpec(%s) accepted", in)
		}
	}
	for _, in := range []string{"800100a2028300830141", "800100a20283008402686d", "800100a2028300830141840100"} {
		if _, err := UnmarshalGetVarAccessResponse(asn1.RawValue{Bytes: hx(t, in)}); err != nil {
			t.Errorf("GVAA response %s: %v", in, err)
		}
	}
	for _, in := range []string{"800100a2028300840100830141", "800100a2028300850100"} {
		if _, err := UnmarshalGetVarAccessResponse(asn1.RawValue{Bytes: hx(t, in)}); err == nil {
			t.Errorf("GVAA response %s accepted", in)
		}
	}
}

// TestSpec_X690_8_6_2_BitStringEncode: the initial octet gives the unused
// bits of the last octet (X.690 clause 8.6.2), so the contents must hold
// exactly ceil(bitLen/8) octets; unused bits are sent as zero (MMS-PDU-19).
func TestSpec_X690_8_6_2_BitStringEncode(t *testing.T) {
	for _, tc := range []struct {
		data   string
		bitLen int
		want   string
	}{
		{"ff", 3, "05e0"},
		{"", 0, "00"},
		{"ffff", 0, "00ffff"},
		{"ffff", 13, "03fff8"},
		{"aa", 8, "00aa"},
	} {
		got, err := encodeBitString(hx(t, tc.data), tc.bitLen)
		if err != nil || hex.EncodeToString(got) != tc.want {
			t.Errorf("(%s, %d) = %x, %v; want %s", tc.data, tc.bitLen, got, err, tc.want)
		}
	}
	for _, tc := range []struct {
		data   string
		bitLen int
	}{{"e0ff", 3}, {"", 3}, {"ff", -1}, {"ff", 9}} {
		if got, err := encodeBitString(hx(t, tc.data), tc.bitLen); err == nil {
			t.Errorf("(%s, %d) = %x, want error", tc.data, tc.bitLen, got)
		}
	}
	// Received padding bits are cleared (X.690 clause 11.2.1 leaves them
	// to a BER sender), so the value equals its re-encoding.
	if dv, _, err := UnmarshalDataElement(hx(t, "84020530"), 0); err != nil || dv.BitLen != 3 || !bytes.Equal(dv.Bytes, []byte{0x20}) {
		t.Errorf("padded bit string = %+v, %v", dv, err)
	}
	if _, err := MarshalData(&DataValue{Tag: TagDataBitString, Bytes: []byte{0xe0, 0xff}, BitLen: 3}); err == nil {
		t.Error("MarshalData: bit string length mismatch encoded")
	}
	if _, err := MarshalData(&DataValue{Tag: TagDataBooleanArray, Bytes: []byte{0xe0, 0xff}, BitLen: 3}); err == nil {
		t.Error("MarshalData: boolean array length mismatch encoded")
	}
}

// TestSpec_9506_2_14_4_2_BCDNotNegative: bcd [13] "shall not be negative"
// (ISO 9506-2 clause 14.4.2) on encode and decode (MMS-PDU-21).
func TestSpec_9506_2_14_4_2_BCDNotNegative(t *testing.T) {
	if _, err := MarshalData(&DataValue{Tag: TagDataBCD, Int: -1}); err == nil {
		t.Error("negative BCD encoded")
	}
	if _, _, err := UnmarshalDataElement(hx(t, "8d01ff"), 0); err == nil {
		t.Error("negative BCD decoded")
	}
	if dv, _, err := UnmarshalDataElement(hx(t, "8d0109"), 0); err != nil || dv.Int != 9 {
		t.Errorf("BCD 9 = %+v, %v", dv, err)
	}
}

// TestSpec_X680_46_GeneralizedTime: every GeneralizedTime form of X.680
// clause 46 is accepted; local time is read as UTC (MMS-PDU-22).
func TestSpec_X680_46_GeneralizedTime(t *testing.T) {
	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	for in, want := range map[string]time.Time{
		"20240101120000Z":     base,
		"20240101120000.123Z": base.Add(123 * time.Millisecond),
		"20240101120000,5Z":   base.Add(500 * time.Millisecond),
		"20240101120000":      base,
		"20240101120000+0100": base.Add(-time.Hour),
		"20240101120000-01":   base.Add(time.Hour),
		"202401011200Z":       base,
		"2024010112Z":         base,
		"2024010112.5Z":       base.Add(30 * time.Minute),
		"202401011230.5":      base.Add(30*time.Minute + 30*time.Second),
	} {
		got, err := ParseGeneralizedTime(in)
		if err != nil || !got.Equal(want) {
			t.Errorf("%s = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "2024", "20241301120000Z", "20240230120000Z", "20240101250000Z", "20240101120000.Z",
		"20240101120000X", "20240101120000+01000", "0000010100+01", "99991231230000-01", "20240101120000+2400", "2024010112000", "20240101120060Z", "2024010112a0Z"} {
		if _, err := ParseGeneralizedTime(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
	dv, _, err := UnmarshalDataElement(append([]byte{0x8b, 19}, "20240101120000+0100"...), 0)
	if err != nil || !dv.Time.Equal(base.Add(-time.Hour)) {
		t.Errorf("Data generalized-time = %+v, %v", dv, err)
	}
}

// TestSpec_9506_2_23_4_InitializeJournalRequest and GetDomainAttributes
// (ISO 9506-2 clauses 23.4 and 11.13; MMS-PDU-30).
func TestSpec_9506_2_23_4_InitializeJournalRequest(t *testing.T) {
	name := "a00ba1091a01441a044a524e4c"
	r, err := UnmarshalInitializeJournalRequest(hx(t, name))
	if err != nil || r.HasLimit || r.Name.DomainID != "D" || r.Name.ItemID != "JRNL" {
		t.Fatalf("name only = %+v, %v", r, err)
	}
	r, err = UnmarshalInitializeJournalRequest(hx(t, name+"a10b8006000000000000810109"))
	if err != nil || !r.HasLimit || !r.LimitingTime.Equal(epoch1984) || !bytes.Equal(r.LimitingEntry, []byte{9}) {
		t.Fatalf("with limit = %+v, %v", r, err)
	}
	for _, bad := range []string{"", "a100", "8000", name + "a100", name + "a2028100", name + "a1028100", name + "a1088006000000000000a100",
		name + "a1088006ffffffff0000", name + "a10b8006000000000000820109", name + "a1088006000000000000" + "00"} {
		if _, err := UnmarshalInitializeJournalRequest(hx(t, bad)); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	if n, err := UnmarshalGetDomainAttributesRequest([]byte("LD1")); err != nil || n != "LD1" {
		t.Errorf("domain = %q, %v", n, err)
	}
	for _, bad := range [][]byte{nil, bytes.Repeat([]byte{'a'}, maxIdentifierLen+1)} {
		if _, err := UnmarshalGetDomainAttributesRequest(bad); err == nil {
			t.Errorf("domain name of %d octets accepted", len(bad))
		}
	}
	if got := hex.EncodeToString(MarshalGetDomainAttributesResponse(DomainStateReady, true, false)); got != "a0008101028201ff830100a400850100" {
		t.Errorf("response = %s", got)
	}
}
