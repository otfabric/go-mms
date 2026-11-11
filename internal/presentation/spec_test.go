// SPDX-License-Identifier: MIT

package presentation

import (
	"bytes"
	"testing"

	"github.com/otfabric/go-mms/internal/berutil"
)

func ctxDef(id int, abs []byte, ts ...[]byte) []byte {
	var tsl []byte
	for _, t := range ts {
		tsl = berutil.AppendTLV(tsl, 0x06, t)
	}
	c := berutil.EncodeTLV(0x02, berutil.EncodeInt(id))
	c = berutil.AppendTLV(c, 0x06, abs)
	c = berutil.AppendTLV(c, 0x30, tsl)
	return berutil.EncodeTLV(0x30, c)
}

func cpWith(defs ...[]byte) []byte {
	var list []byte
	for _, d := range defs {
		list = append(list, d...)
	}
	normal := berutil.EncodeTLV(0xa4, list)
	normal = append(normal, encodeFullyEncodedData(1, []byte{0x60, 0x00})...)
	inner := berutil.EncodeTLV(0xa0, []byte{0x80, 0x01, 0x01})
	inner = berutil.AppendTLV(inner, 0xa2, normal)
	return berutil.EncodeTLV(0x31, inner)
}

// ISO 8823-1 / ISO 9506-2 Annex A.5 (MMS-STK-07): the responder answers
// every proposed context in proposal order and uses the identifiers the
// initiator chose.
func TestSpec_8823_ContextNegotiation(t *testing.T) {
	unknown := []byte{0x2a, 0x03}
	p, err := Parse(cpWith(ctxDef(1, oidACSE, oidBER), ctxDef(5, oidMMS, oidBER), ctxDef(7, unknown, oidBER), ctxDef(9, oidMMS, []byte{0x51, 0x02})))
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != PpduCP || len(p.Contexts) != 4 {
		t.Fatalf("kind %s contexts %d", p.Kind, len(p.Contexts))
	}
	n := Negotiate(p.Contexts)
	if n.ACSE != 1 || n.MMS != 5 {
		t.Fatalf("ACSE %d MMS %d", n.ACSE, n.MMS)
	}
	want := []ContextResult{
		{Result: ResultAcceptance, TransferSyntax: oidBER, ProviderReason: -1},
		{Result: ResultAcceptance, TransferSyntax: oidBER, ProviderReason: -1},
		{Result: ResultProviderRejection, ProviderReason: ContextReasonAbstractSyntaxNotSupported},
		{Result: ResultProviderRejection, ProviderReason: ContextReasonTransferSyntaxesNotSupported},
	}
	if len(n.Results) != len(want) {
		t.Fatalf("%d results", len(n.Results))
	}
	for i := range want {
		if n.Results[i].Result != want[i].Result || n.Results[i].ProviderReason != want[i].ProviderReason || !bytes.Equal(n.Results[i].TransferSyntax, want[i].TransferSyntax) {
			t.Fatalf("result %d = %+v, want %+v", i, n.Results[i], want[i])
		}
	}

	cpa := EncodeCPAResults(nil, n.Results, n.ACSE, []byte{0x61, 0x00})
	// Third entry: provider-rejection with abstract-syntax-not-supported.
	if !bytes.Contains(cpa, []byte{0x30, 0x06, 0x80, 0x01, 0x02, 0x82, 0x01, 0x01}) {
		t.Fatalf("CPA %x lacks the rejected third context", cpa)
	}
	pa, err := Parse(cpa)
	if err != nil || pa.Kind != PpduCPA || len(pa.Results) != 4 || pa.ContextID != 1 {
		t.Fatalf("CPA parse %+v %v", pa, err)
	}

	// A second MMS context with BER is refused (only one per syntax).
	n = Negotiate([]ContextDef{{ID: 3, AbstractSyntax: oidMMS, TransferSyntaxes: [][]byte{oidBER}}, {ID: 5, AbstractSyntax: oidMMS, TransferSyntaxes: [][]byte{oidBER}}})
	if n.MMS != 3 || n.Results[1].Result != ResultProviderRejection {
		t.Fatalf("duplicate MMS: %+v", n)
	}
}

func TestSpec_8823_ContextListMalformed(t *testing.T) {
	for _, defs := range [][]byte{
		berutil.EncodeTLV(0x31, nil),                                         // not a SEQUENCE
		berutil.EncodeTLV(0x30, berutil.EncodeTLV(0x06, oidMMS)),             // no identifier
		berutil.EncodeTLV(0x30, berutil.EncodeTLV(0x02, nil)),                // bad INTEGER
		berutil.EncodeTLV(0x30, berutil.EncodeTLV(0x30, []byte{0x06, 0x05})), // truncated TS
	} {
		if _, err := Parse(cpWith(defs)); err == nil {
			t.Errorf("malformed definition %x accepted", defs)
		}
	}
	for _, res := range [][]byte{
		berutil.EncodeTLV(0x31, nil),
		berutil.EncodeTLV(0x30, berutil.EncodeTLV(0x81, oidBER)), // no result
		berutil.EncodeTLV(0x30, berutil.EncodeTLV(0x80, nil)),
	} {
		if _, err := parseResultList(res); err == nil {
			t.Errorf("malformed result %x accepted", res)
		}
	}
}

// ISO 8823-1 CPR-PPDU (MMS-STK-03): normal-mode SEQUENCE with the result
// list, the provider-reason and the user data.
func TestSpec_8823_CPR(t *testing.T) {
	accept := ContextResult{Result: ResultAcceptance, TransferSyntax: oidBER, ProviderReason: -1}
	cpr := EncodeCPR([]byte{0, 0, 0, 1}, []ContextResult{accept, accept}, -1, 1, []byte{0x61, 0x00})
	if cpr[0] != 0x30 {
		t.Fatalf("CPR tag %02x", cpr[0])
	}
	p, err := Parse(cpr)
	if err != nil || p.Kind != PpduCPR || len(p.Results) != 2 || p.ProviderReason != -1 || !bytes.Equal(p.UserData, []byte{0x61, 0x00}) {
		t.Fatalf("CPR %+v %v", p, err)
	}
	cpr = EncodeCPR(nil, nil, ProviderReasonNotSpecified, 1, nil)
	if !bytes.Equal(cpr, []byte{0x30, 0x03, 0x8a, 0x01, 0x00}) {
		t.Fatalf("provider CPR %x", cpr)
	}
	p, err = Parse(cpr)
	if err != nil || p.ProviderReason != 0 || p.HasResults {
		t.Fatalf("provider CPR %+v %v", p, err)
	}
	for _, bad := range [][]byte{{0x30, 0x02, 0x8a, 0x00}, {0x30, 0x02, 0xa5, 0x01}, {0x30, 0x02, 0x61, 0x00}, {0x30, 0x05, 0xa5, 0x03, 0x31, 0x01, 0x00}} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("CPR %x accepted", bad)
		}
	}
}

// ISO 8823-1 ARU-PPDU (MMS-STK-08): an A-U-ABORT goes as [0] { user-data }.
func TestSpec_8823_ARU(t *testing.T) {
	aru := EncodeARU(1, []byte{0x64, 0x03, 0x80, 0x01, 0x00})
	want := []byte{0xa0, 0x0e, 0x61, 0x0c, 0x30, 0x0a, 0x02, 0x01, 0x01, 0xa0, 0x05, 0x64, 0x03, 0x80, 0x01, 0x00}
	if !bytes.Equal(aru, want) {
		t.Fatalf("ARU %x want %x", aru, want)
	}
	p, err := ParseAbort(aru)
	if err != nil || p.Kind != PpduARU || p.ContextID != 1 || !bytes.Equal(p.UserData, want[11:]) {
		t.Fatalf("ARU %+v %v", p, err)
	}
	// With the optional context identifier list.
	withList := berutil.EncodeTLV(0xa0, append(berutil.EncodeTLV(0xa0, nil), want[2:]...))
	if p, err := ParseAbort(withList); err != nil || p.ContextID != 1 {
		t.Fatalf("ARU with list %+v %v", p, err)
	}
	// ARP: provider-reason 1.
	p, err = ParseAbort([]byte{0x30, 0x03, 0x80, 0x01, 0x01})
	if err != nil || p.Kind != PpduARP || p.ProviderReason != 1 {
		t.Fatalf("ARP %+v %v", p, err)
	}
	for _, bad := range [][]byte{nil, {0x61, 0x00}, {0x30, 0x02, 0x80, 0x00}, {0xa0, 0x02, 0x61, 0x00}, {0xa0, 0x01, 0x61}} {
		if _, err := ParseAbort(bad); err == nil {
			t.Errorf("abort %x accepted", bad)
		}
	}
}

// ISO 8823-1 PDV-list (MMS-STK-17): octet-aligned data is accepted, the
// arbitrary encoding and unknown tags are refused, and several PDV-lists
// are all returned.
func TestSpec_8823_PDVEncodings(t *testing.T) {
	p, err := Parse([]byte{0x61, 0x0a, 0x30, 0x08, 0x02, 0x01, 0x03, 0x81, 0x03, 0xa1, 0x01, 0x00})
	if err != nil || p.ContextID != 3 || !bytes.Equal(p.UserData, []byte{0xa1, 0x01, 0x00}) {
		t.Fatalf("octet-aligned %+v %v", p, err)
	}
	if _, err := Parse([]byte{0x61, 0x09, 0x30, 0x07, 0x02, 0x01, 0x03, 0x82, 0x02, 0x00, 0xff}); err == nil {
		t.Fatal("arbitrary accepted")
	}
	if _, err := Parse([]byte{0x61, 0x08, 0x30, 0x06, 0x02, 0x01, 0x03, 0x85, 0x01, 0x00}); err == nil {
		t.Fatal("unknown PDV tag accepted")
	}
	two := append(encodeFullyEncodedData(1, []byte{0x01})[2:], encodeFullyEncodedData(3, []byte{0x02})[2:]...)
	p, err = Parse(berutil.EncodeTLV(0x61, two))
	if err != nil || len(p.PDVs) != 2 || p.PDVs[1].ContextID != 3 || p.ContextID != 1 {
		t.Fatalf("two PDVs %+v %v", p, err)
	}
	if _, err := Parse([]byte{0x61, 0x00}); err == nil {
		t.Fatal("empty fully-encoded-data accepted")
	}
	if _, err := Parse([]byte{0x61, 0x02, 0x31, 0x00}); err == nil {
		t.Fatal("non-SEQUENCE PDV-list accepted")
	}
}

func TestPpduKindStringNew(t *testing.T) {
	for k, w := range map[PpduKind]string{PpduCPR: "CPR", PpduARU: "ARU", PpduARP: "ARP"} {
		if k.String() != w {
			t.Errorf("%d: %s", k, k.String())
		}
	}
}
