// SPDX-License-Identifier: MIT

package mms

import (
	"strings"
	"testing"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/pdu"
	"github.com/otfabric/go-mms/internal/presentation"
	"github.com/otfabric/go-mms/internal/session"
)

// garbage is a service body no modelled service decodes.
var garbage = []byte{0xff, 0xff}

func mustB(t *testing.T) func([]byte, error) []byte {
	return func(b []byte, err error) []byte {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
}

func TestDecodePDU_Shapes(t *testing.T) {
	must := mustB(t)
	d := pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "v"}
	// Read by list name.
	p, err := DecodePDU(must(pdu.MarshalReadRequestByListName(5, d)))
	if err != nil || p.ListName == nil || p.ListName.ItemID != "v" || p.InvokeID != 5 {
		t.Errorf("read by list = %+v, %v", p, err)
	}
	// Read with an index and an index range.
	p, err = DecodePDU(must(pdu.MarshalReadRequestWithAccess(6, []pdu.VariableSpecWire{
		{Name: d, AlternateAccess: []pdu.AccessSelectorWire{{HasIndex: true, Index: 2}}},
		{Name: d, AlternateAccess: []pdu.AccessSelectorWire{{IndexRange: &pdu.IndexRangeWire{LowIndex: 1, NumberOfElements: 3}}}},
	})))
	if err != nil || len(p.Variables) != 2 || *p.Variables[0].Access[0].Index != 2 || p.Variables[1].Access[0].IndexRange.Count != 3 {
		t.Errorf("read with access = %+v, %v", p, err)
	}
	// A write response with a failure, a reject, a cancel response.
	p, err = DecodePDU(must(codec.MarshalConfirmedResponse(7, asn1util.TagNumWrite, true,
		must(pdu.MarshalWriteResponse([]pdu.WriteResult{{Success: true}, {Code: 3}})))))
	if err != nil || len(p.WriteResults) != 2 || p.WriteResults[0] != nil || p.WriteResults[1].Code != DataAccessErrorCode(3) {
		t.Errorf("write failure = %+v, %v", p, err)
	}
	p, err = DecodePDU(codec.MarshalRejectPDU(8, 1, 4))
	if err != nil || p.Reject == nil || p.Reject.Type != 1 || p.Reject.Reason != 4 || p.InvokeID != 8 {
		t.Errorf("reject = %+v, %v", p, err)
	}
	p, err = DecodePDU(codec.MarshalCancelResponse(9))
	if err != nil || p.Kind != "cancel-response" || len(p.Raw) == 0 {
		t.Errorf("cancel = %+v, %v", p, err)
	}
	// A service this decoder does not model keeps its content.
	p, err = DecodePDU(must(codec.MarshalConfirmedResponse(1, asn1util.TagNumStatus, true, []byte{0x80, 0x01, 0x00})))
	if err != nil || p.Service != "Status" || len(p.Raw) == 0 || p.DecodeError != "" {
		t.Errorf("status = %+v, %v", p, err)
	}
	// An information report naming a list.
	rep := must(pdu.MarshalInformationReport(&pdu.InformationReportWire{ListName: &d, Values: []*pdu.DataValue{{Tag: pdu.TagDataBoolean, Bool: true}}}))
	if p, err := DecodePDU(rep); err != nil || p.ListName == nil || len(p.Values) != 1 {
		t.Errorf("report by list = %+v, %v", p, err)
	}
}

func TestDecodePDU_Malformed(t *testing.T) {
	must := mustB(t)
	req := func(tag int, body []byte) []byte { return must(codec.MarshalConfirmedRequest(1, tag, true, body)) }
	resp := func(tag int, body []byte) []byte { return must(codec.MarshalConfirmedResponse(1, tag, true, body)) }
	badScope := must(pdu.EncodeObjectName(pdu.ObjectNameWire{Scope: 2, ItemID: "x"}))
	badScope[0] = 0x83 // [3]: not an ObjectName alternative
	for name, b := range map[string][]byte{
		"read request":     req(asn1util.TagNumRead, garbage),
		"write request":    req(asn1util.TagNumWrite, garbage),
		"name list":        req(asn1util.TagNumGetNameList, garbage),
		"GetVAA":           req(asn1util.TagNumGetVariableAccessAttributes, garbage),
		"GetNVLAttributes": req(asn1util.TagNumGetNamedVariableListAttrs, garbage),
		"read response":    resp(asn1util.TagNumRead, garbage),
		"write response":   resp(asn1util.TagNumWrite, garbage),
		"write bad code":   resp(asn1util.TagNumWrite, must(pdu.MarshalWriteResponse([]pdu.WriteResult{{Code: 99}}))),
		"name list resp":   resp(asn1util.TagNumGetNameList, garbage),
		"identify resp":    resp(asn1util.TagNumIdentify, garbage),
		"GetVAA resp":      resp(asn1util.TagNumGetVariableAccessAttributes, garbage),
		"GetNVL resp":      resp(asn1util.TagNumGetNamedVariableListAttrs, garbage),
		"report":           berutil.EncodeTLV(asn1util.TagUnconfirmed, berutil.EncodeTLV(0xa0, garbage)),
		"initiate request": berutil.EncodeTLV(0xa8, garbage),
		"initiate resp":    berutil.EncodeTLV(0xa9, garbage),
		"name list scope":  req(asn1util.TagNumGetNameList, []byte{0xa0, 0x03, 0x80, 0x01, 0x00, 0xa1, 0x02, 0x85, 0x00}),
		"GetVAA scope":     req(asn1util.TagNumGetVariableAccessAttributes, berutil.EncodeTLV(0xa0, badScope)),
	} {
		p, err := DecodePDU(b)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if p.DecodeError == "" || len(p.Raw) == 0 {
			t.Errorf("%s: no decode error: %+v", name, p)
		}
	}
	for name, b := range map[string][]byte{
		"confirmed request":  berutil.EncodeTLV(0xa0, garbage),
		"confirmed response": berutil.EncodeTLV(0xa1, garbage),
		"confirmed error":    berutil.EncodeTLV(0xa2, garbage),
		"reject":             berutil.EncodeTLV(0xa4, garbage),
	} {
		if _, err := DecodePDU(b); err == nil {
			t.Errorf("%s decoded", name)
		}
	}
}

func TestDecodeTSDU_Layers(t *testing.T) {
	must := mustB(t)
	// A connect without user data.
	if got, err := DecodeTSDU(must(session.EncodeConnect(session.ConnectParams{}, nil))); err != nil || got.Session != "connect" || got.Presentation != "" {
		t.Errorf("bare connect = %+v, %v", got, err)
	}
	// An abort carrying an empty ARU-PPDU (ISO 8823-1), and one whose
	// presentation PDU does not decode.
	if got, err := DecodeTSDU(must(session.EncodeAbort([]byte{0xa0, 0x00}))); err != nil || got.Session != "abort" || got.Presentation != "ARU" || got.ACSE != nil {
		t.Errorf("abort with ARU = %+v, %v", got, err)
	}
	if got, err := DecodeTSDU(must(session.EncodeAbort([]byte{0xff}))); err != nil || got.Session != "abort" || got.Presentation != "" {
		t.Errorf("abort with junk = %+v, %v", got, err)
	}
	// ACSE that does not decode, an MMS PDU that does not decode.
	if _, err := DecodeTSDU(session.EncodeData(presentation.EncodeUserData(presentation.ContextIDACSE, garbage))); err == nil || !strings.Contains(err.Error(), "acse") {
		t.Errorf("bad ACSE = %v", err)
	}
	if got, err := DecodeTSDU(session.EncodeData(presentation.EncodeUserData(presentation.ContextIDMMS, garbage))); err == nil || got.Presentation != "user-data" {
		t.Errorf("bad MMS = %+v, %v", got, err)
	}
	// An AARE carrying the Initiate response.
	init := must(codec.MarshalMmsPduBareSequence(asn1util.TagInitiateResponse, pdu.InitiateResponse{NegotiatedMaxServOutstandingCall: 1, NegotiatedMaxServOutstandingCalled: 1}))
	got, err := DecodeTSDU(must(session.EncodeAccept(session.ConnectParams{}, presentation.EncodeCPA(nil, acse.EncodeAARE(0, init)))))
	if err != nil || got.PDU == nil || got.PDU.Kind != "initiate-response" {
		t.Errorf("AARE with initiate = %+v, %v", got, err)
	}
	// Names with a scope the decoder does not know.
	bad := pdu.ObjectNameWire{Scope: 7, ItemID: "x"}
	var p CapturedPDU
	if err := p.setVariables(nil, &bad); err == nil {
		t.Error("a list name of an unknown scope")
	}
	if _, err := capturedVariables([]pdu.VariableSpecWire{{Name: bad}}); err == nil {
		t.Error("a variable of an unknown scope")
	}
	// Selector conversion keeps a component.
	if s := accessSelectorFromWire(pdu.AccessSelectorWire{Component: "x"}); s.Component != "x" {
		t.Errorf("component = %+v", s)
	}
}

// TestDecodePDU_Captured: shapes seen in public captures.
func TestDecodePDU_Captured(t *testing.T) {
	must := mustB(t)
	// Every ISO 9506-2 service is named, an undefined tag is numbered.
	for tag, want := range map[int]string{44: "Kill", 19: "TakeControl", 63: "GetAlarmSummary", 77: "FileDirectory", 200: "Service200"} {
		p, err := DecodePDU(must(codec.MarshalConfirmedRequest(1, tag, true, []byte{0x80, 0x00})))
		if err != nil || p.Service != want || len(p.Raw) == 0 {
			t.Errorf("tag %d = %+v, %v", tag, p, err)
		}
	}
	// A confirmed response without a service (sent by some stacks in reply
	// to a conclude): its invoke ID is kept, with a decode error.
	p, err := DecodePDU([]byte{0xa1, 0x03, 0x02, 0x01, 0x07})
	if err != nil || !p.HasInvokeID || p.InvokeID != 7 || p.DecodeError == "" || p.Kind != "confirmed-response" {
		t.Errorf("response without service = %+v, %v", p, err)
	}
	// A GetVariableAccessAttributes response with a primitive address [1].
	resp := []byte{0x80, 0x01, 0x00, 0x81, 0x01, 0x05, 0xa2, 0x03, 0x83, 0x01, 0x00}
	p, err = DecodePDU(must(codec.MarshalConfirmedResponse(2, asn1util.TagNumGetVariableAccessAttributes, true, resp)))
	if err != nil || p.DecodeError != "" || p.TypeSpec == nil || p.TypeSpec.Type != ValueTypeBoolean {
		t.Errorf("GetVAA with address = %+v, %v", p, err)
	}
}
