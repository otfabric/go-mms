// SPDX-License-Identifier: MIT

package mms

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/isostack"
	"github.com/otfabric/go-mms/internal/pdu"
	"github.com/otfabric/go-mms/internal/serverconn"
)

// Spec-verification tests for the MMS PDU layer (ISO 9506-2,
// IEC 61850-8-1 clause 8.1.3.4). Byte vectors are written out so the
// wire form is visible.

func hx(t testing.TB, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex %q: %v", s, err)
	}
	return b
}

// scriptConn feeds queued TSDUs to the server and records what it sends.
type scriptConn struct {
	mu   sync.Mutex
	in   [][]byte
	sent [][]byte
}

func (s *scriptConn) Send(_ context.Context, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, append([]byte(nil), data...))
	return nil
}

func (s *scriptConn) Receive(context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.in) == 0 {
		return nil, io.EOF
	}
	d := s.in[0]
	s.in = s.in[1:]
	return d, nil
}

func (s *scriptConn) Close() error { return nil }

// serverExchange runs the post-Initiate dispatch path of srv over the
// given MMS PDUs (a Conclude-Request is appended) and returns the MMS
// PDUs the server sent, without the final Conclude-Response.
func serverExchange(t *testing.T, srv *Server, nesting int, pdus ...[]byte) [][]byte {
	t.Helper()
	tr := &scriptConn{}
	for _, p := range pdus {
		tr.in = append(tr.in, isostack.EncodeDataRequest(p))
	}
	tr.in = append(tr.in, isostack.EncodeDataRequest(codec.MarshalConcludeRequest()))
	logger := slog.New(discardHandler{})
	sc := &ServerConn{busy: make(chan struct{}, 1), frsmTable: newFRSMTable()}
	opts := serverconn.MMSOptions{MaxPDUSize: 65000, MaxOutstandingCalling: 5, MaxOutstandingCalled: 5, DataStructureNestingLevel: nesting}
	c := serverconn.New(tr, logger, srv.handler(sc, logger), opts)
	sc.conn = c
	if err := c.Serve(context.WithValue(context.Background(), serverConnCtxKey{}, sc)); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var out [][]byte
	for _, f := range tr.sent {
		m, err := isostack.DecodeDataResponse(f)
		if err != nil {
			t.Fatalf("decode sent frame: %v", err)
		}
		out = append(out, m)
	}
	if len(out) == 0 || !bytes.Equal(out[len(out)-1], codec.MarshalConcludeResponse()) {
		t.Fatalf("last PDU is not a Conclude-Response: %x", out)
	}
	return out[:len(out)-1]
}

func confReq(t *testing.T, id codec.InvokeID, tag int, constructed bool, payload []byte) []byte {
	t.Helper()
	b, err := codec.MarshalConfirmedRequest(id, tag, constructed, payload)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func expectPDUs(t *testing.T, got [][]byte, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("server sent %d PDUs %x, want %d", len(got), got, len(want))
	}
	for i, w := range want {
		if !bytes.Equal(got[i], hx(t, w)) {
			t.Errorf("PDU %d = %x, want %s", i, got[i], w)
		}
	}
}

// TestSpec_8_1_8_1_3_4_1_UnsupportedServiceIsRejected: a well-formed
// request for a service the server does not support is answered with
// Reject{originalInvokeID, confirmed-requestPDU unrecognized-service}
// (IEC 61850-8-1 clause 8.1.3.4.1; ISO 9506-2 clause 8.6), never with a
// vmd-state ServiceError code outside 0..4 (MMS-PDU-02).
func TestSpec_8_1_8_1_3_4_1_UnsupportedServiceIsRejected(t *testing.T) {
	srv := NewServer(ServerOptions{})
	cases := map[string][]byte{
		"rename [3]":               confReq(t, 7, 3, true, hx(t, "a003800161")),
		"initializeJournal [67]":   confReq(t, 7, 67, true, hx(t, "a0028000")),
		"getCapabilityList [71]":   confReq(t, 7, 71, true, nil),
		"identify without handler": confReq(t, 7, 2, false, nil),
		"status without handler":   confReq(t, 7, 0, false, []byte{0}),
		"fileOpen without provider": confReq(t, 7, asn1util.TagNumFileOpen, true,
			hx(t, "a0031901418100")),
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			expectPDUs(t, serverExchange(t, srv, 10, req), "a406800107810101")
		})
	}
}

// TestSpec_9506_2_8_6_MalformedRequestsAreRejected: PDUs the server
// cannot process are answered with a RejectPDU instead of being dropped
// (ISO 9506-2 clause 8.6; MMS-PDU-11).
func TestSpec_9506_2_8_6_MalformedRequestsAreRejected(t *testing.T) {
	srv := NewServer(ServerOptions{})
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"unknown PDU type", "ff00", "a403850100"},
		{"truncated PDU", "a005020101a4", "a403850101"},
		{"GetNameList garbage body", "a00702010aa102ffff", "a40680010a810104"},
		{"Read garbage body", "a00702010ba402ffff", "a40680010b810104"},
		{"envelope without service", "a003020101", "a406800101810104"},
		{"malformed cancel", "8501ff", "a403860100"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectPDUs(t, serverExchange(t, srv, 10, hx(t, tc.in)), tc.want)
		})
	}
}

// TestSpec_9506_2_8_6_NestingLevelEnforced: Data nested deeper than the
// negotiated dataStructureNestingLevel (ISO 9506-2 clause 8.2) is
// answered with Reject confirmed-requestPDU max-recursion-exceeded (8)
// (MMS-PDU-27).
func TestSpec_9506_2_8_6_NestingLevelEnforced(t *testing.T) {
	srv := NewServer(ServerOptions{})
	if err := srv.RegisterDomain("D"); err != nil {
		t.Fatal(err)
	}
	inner := NewStructure([]*Value{NewBoolean(true)})
	deep := NewStructure([]*Value{NewStructure([]*Value{inner})}) // 3 levels
	if err := srv.RegisterVariable(Variable{
		Name:     ObjectName{Scope: ObjectScopeDomain, Domain: "D", ItemID: "v"},
		TypeSpec: TypeSpec{Type: ValueTypeStructure},
		Write:    func(context.Context, *Value) error { return nil },
	}); err != nil {
		t.Fatal(err)
	}
	dv, err := valueToDataValue(deep)
	if err != nil {
		t.Fatal(err)
	}
	req, err := pdu.MarshalWriteRequest(5, []pdu.ObjectNameWire{{Scope: pdu.ScopeDomain, DomainID: "D", ItemID: "v"}}, []*pdu.DataValue{dv})
	if err != nil {
		t.Fatal(err)
	}
	expectPDUs(t, serverExchange(t, srv, 2, req), "a406800105810108")
	// Within the limit the write is executed.
	got := serverExchange(t, srv, 3, req)
	if len(got) != 1 || got[0][0] != 0xa1 {
		t.Fatalf("write within nesting level = %x, want a Confirmed-Response", got)
	}
}

// TestSpec_9506_2_8_4_3_ConcludeErrorReturnedByClose: Conclude-ErrorPDU
// is [13] IMPLICIT ServiceError (0xad, ISO 9506-2 clauses 7 and 8.4.3);
// Close reports it as a *ServiceError (MMS-PDU-01).
func TestSpec_9506_2_8_4_3_ConcludeErrorReturnedByClose(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()
	go srv.handleAssociation(ctx)
	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	go func() {
		srv.handleDataRequest(ctx) // conclude
		// conclude / further-communication-required (IEC 61850-8-1 Table 14)
		srv.sendDataResponse(ctx, hx(t, "ad05a003890101"))
	}()
	err = client.Close(ctx)
	var se *ServiceError
	if !errors.As(err, &se) || se.Class != ErrorClassConclude || se.Code != 1 {
		t.Fatalf("Close = %v, want ServiceError conclude/further-communication-required", err)
	}
}

// TestSpec_X690_8_1_3_ClientAcceptsBERLengths: the client accepts the
// BER length forms DER forbids (X.690 §8.1.3.5 NOTE 2, §8.1.3.6) and a
// non-minimal invokeID INTEGER (MMS-PDU-06).
func TestSpec_X690_8_1_3_ClientAcceptsBERLengths(t *testing.T) {
	// Identify-Response {vendor "v", model "m", revision "r"}.
	idResp := "a20980017681016d820172"
	cases := map[string]func(id byte) string{
		"non-minimal outer length": func(id byte) string { return "a1810e0201" + hex.EncodeToString([]byte{id}) + idResp },
		"indefinite outer length":  func(id byte) string { return "a1800201" + hex.EncodeToString([]byte{id}) + idResp + "0000" },
		"indefinite service": func(id byte) string {
			return "a1800201" + hex.EncodeToString([]byte{id}) + "a28080017681016d8201720000" + "0000"
		},
		"non-minimal invokeID": func(id byte) string { return "a10f0202" + "00" + hex.EncodeToString([]byte{id}) + idResp },
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			mt := newMockTransport()
			srv := newMockServer(t, mt)
			ctx := context.Background()
			go srv.handleAssociation(ctx)
			client, err := NewClient(ctx, mt, defaultDialOptions())
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			go func() {
				id, _, _ := srv.handleDataRequest(ctx)
				srv.sendDataResponse(ctx, hx(t, build(byte(id))))
			}()
			got, err := client.Identify(ctx)
			if err != nil || got.Vendor != "v" || got.Model != "m" || got.Revision != "r" {
				t.Fatalf("Identify = %+v, %v", got, err)
			}
			go func() {
				srv.handleDataRequest(ctx)
				srv.sendConcludeResponse(ctx)
			}()
			if err := client.Close(ctx); err != nil {
				t.Fatalf("Close: %v", err)
			}
		})
	}
}

// TestSpec_9506_2_8_6_RejectWithoutInvokeID: originalInvokeID is
// OPTIONAL in a RejectPDU (ISO 9506-2 clause 8.6); a Reject without it
// fails the only outstanding request instead of leaving it to time out
// (MMS-PDU-07).
func TestSpec_9506_2_8_6_RejectWithoutInvokeID(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()
	go srv.handleAssociation(ctx)
	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = client.Abort(ctx) }()
	go func() {
		srv.handleDataRequest(ctx)
		srv.sendDataResponse(ctx, hx(t, "a403850101")) // pdu-error invalid-pdu
	}()
	rctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err = client.Identify(rctx)
	var rj *RejectError
	if !errors.As(err, &rj) || rj.Type != RejectPDUError || rj.Reason != 1 || rj.HasInvokeID {
		t.Fatalf("Identify = %v, want RejectError pdu-error/invalid-pdu without invokeID", err)
	}
}

// TestSpec_9506_2_8_6_MalformedResponseIsRejected: a Confirmed-Response
// that does not decode but whose invokeID can be read fails that request
// with a decode error, is answered with Reject confirmed-responsePDU, and
// the association stays usable (MMS-PDU-06).
func TestSpec_9506_2_8_6_MalformedResponseIsRejected(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()
	go srv.handleAssociation(ctx)
	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = client.Abort(ctx) }()
	rejected := make(chan []byte, 1)
	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		// Outer length 0x20 overruns the PDU.
		srv.sendDataResponse(ctx, append([]byte{0xa1, 0x20, 0x02, 0x01, byte(id)}, 0xa2, 0x00))
		data, err := mt.receiveFromClient(ctx)
		if err == nil {
			m, _ := isostack.DecodeDataResponse(data)
			rejected <- m
		}
		id, _, _ = srv.handleDataRequest(ctx)
		srv.sendIdentifyResponse(ctx, id)
	}()
	if _, err := client.Identify(ctx); !errors.Is(err, ErrDecodeFailed) {
		t.Fatalf("Identify = %v, want a decode error", err)
	}
	select {
	case m := <-rejected:
		if !bytes.Equal(m, hx(t, "a406800101820100")) {
			t.Fatalf("client sent %x, want Reject confirmed-responsePDU for invokeID 1", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("client sent no Reject")
	}
	if _, err := client.Identify(ctx); err != nil {
		t.Fatalf("association unusable after a malformed response: %v", err)
	}
}

// TestSpec_9506_2_7_4_1_ServiceErrorFields: the optional ServiceError
// components (additionalCode, additionalDescription, serviceSpecificInfo)
// and modifierPosition reach the client (MMS-PDU-10), and a server handler
// can send them.
func TestSpec_9506_2_7_4_1_ServiceErrorFields(t *testing.T) {
	code := 42
	srv := NewServer(ServerOptions{})
	srv.HandleIdentify(func(context.Context, IdentifyRequest) (*ServerIdentity, error) {
		return nil, &ServiceError{Class: ErrorClassDefinition, Code: 1, AdditionalCode: &code, AdditionalDescription: "xyz",
			ServiceSpecific: &ServiceSpecificInfo{Kind: 6, HasValue: true, Value: 3}}
	})
	got := serverExchange(t, srv, 10, confReq(t, 4, 2, false, nil))
	expectPDUs(t, got, "a217800104a212a00382010181012a820378797aa303860103")

	c := unitClient(nil)
	_, content, err := pdu.DecodePdu(got[0])
	if err != nil {
		t.Fatal(err)
	}
	err = c.handleConfirmedError(4, content)
	var se *ServiceError
	if !errors.As(err, &se) || se.AdditionalCode == nil || *se.AdditionalCode != 42 || se.AdditionalDescription != "xyz" ||
		se.ServiceSpecific == nil || se.ServiceSpecific.Kind != 6 || !se.ServiceSpecific.HasValue || se.ServiceSpecific.Value != 3 {
		t.Fatalf("decoded = %+v", se)
	}
	_, content, _ = pdu.DecodePdu(hx(t, "a20d800104810100a205a003820101"))
	err = c.handleConfirmedError(4, content)
	if !errors.As(err, &se) || se.ModifierPosition == nil || *se.ModifierPosition != 0 {
		t.Fatalf("modifierPosition = %+v, %v", se, err)
	}
}

// TestSpec_9506_2_14_2_2_9_SiblingAlternateAccessUnsupported: sibling
// selections (several AlternateAccess elements) are well-formed but not
// modelled; the server answers access/object-access-unsupported instead
// of misreading them as a path (MMS-PDU-05).
func TestSpec_9506_2_14_2_2_9_SiblingAlternateAccessUnsupported(t *testing.T) {
	srv := NewServer(ServerOptions{})
	req := hx(t, "a01d020109"+"a418"+"a116"+"a014"+"3012"+"a008a1061a01441a0176"+"a506810161810162")
	expectPDUs(t, serverExchange(t, srv, 10, req), "a20a800109a205a003870101")
}

// TestSpec_9506_2_14_4_FloatWidthFromType: the Floatingpoint width is the
// declared one, not picked from the value (MMS-PDU-16).
func TestSpec_9506_2_14_4_FloatWidthFromType(t *testing.T) {
	for name, tc := range map[string]struct {
		v    *Value
		wire string
	}{
		"NewFloat(0.1)":   {NewFloat(0.1), "8705083dcccccd"},
		"NewFloat32(0.1)": {NewFloat32(0.1), "8705083dcccccd"},
		"NewFloat64(0.1)": {NewFloat64(0.1), "87090b3fb999999999999a"},
		"NewFloat64(1.5)": {NewFloat64(1.5), "87090b3ff8000000000000"},
	} {
		dv, err := valueToDataValue(tc.v)
		if err != nil {
			t.Fatal(err)
		}
		got, err := pdu.MarshalData(dv)
		if err != nil || !bytes.Equal(got, hx(t, tc.wire)) {
			t.Errorf("%s = %x, %v; want %s", name, got, err, tc.wire)
		}
	}
	// A decoded 9-octet value keeps its width when re-encoded.
	dv, _, err := pdu.UnmarshalDataElement(hx(t, "87090b3ff8000000000000"), 0)
	if err != nil {
		t.Fatal(err)
	}
	v, err := dataValueToValue(dv)
	if err != nil || v.FloatWidth() != 64 {
		t.Fatalf("decoded width = %d, %v", v.FloatWidth(), err)
	}
	back, _ := valueToDataValue(v)
	if got, _ := pdu.MarshalData(back); !bytes.Equal(got, hx(t, "87090b3ff8000000000000")) {
		t.Errorf("re-encoded = %x", got)
	}
	if NewFloat(1).FloatWidth() != 0 || NewInteger(1).FloatWidth() != 0 || NewFloat32(1).Clone().FloatWidth() != 32 {
		t.Error("FloatWidth")
	}

	// The server serves an undeclared-width value at the variable's
	// declared width, including inside a structure.
	ts := TypeSpec{Type: ValueTypeStructure, Elements: []TypeSpecElement{
		{Name: "d", Type: TypeSpec{Type: ValueTypeFloat, FormatWidth: 64, ExponentWidth: 11}},
		{Name: "s", Type: TypeSpec{Type: ValueTypeFloat, FormatWidth: 32, ExponentWidth: 8}},
		{Name: "a", Type: TypeSpec{Type: ValueTypeArray, Count: 1, Element: &TypeSpec{Type: ValueTypeFloat, FormatWidth: 64, ExponentWidth: 11}}},
	}}
	val := NewStructure([]*Value{NewFloat(0.1), NewFloat(0.1), NewArray([]*Value{NewFloat(0.5)})})
	out := withDeclaredFloatWidth(val, &ts)
	if out.elementsVal[0].FloatWidth() != 64 || out.elementsVal[1].FloatWidth() != 0 || out.elementsVal[2].elementsVal[0].FloatWidth() != 64 {
		t.Errorf("declared widths = %+v", out)
	}
	if val.elementsVal[0].FloatWidth() != 0 {
		t.Error("the handler's value was modified")
	}
	if withDeclaredFloatWidth(NewFloat64(1), &ts) == nil || withDeclaredFloatWidth(nil, &ts) != nil {
		t.Error("nil handling")
	}
	if sel := selectTypeSpec(&ts, []pdu.AccessSelectorWire{{Component: "a"}, {HasIndex: true}}); sel == nil || sel.FormatWidth != 64 {
		t.Errorf("selectTypeSpec = %+v", sel)
	}
	if selectTypeSpec(&ts, []pdu.AccessSelectorWire{{Component: "zz"}, {Component: "x"}}) != nil ||
		selectTypeSpec(&ts, []pdu.AccessSelectorWire{{IndexRange: &pdu.IndexRangeWire{}}}) != nil {
		t.Error("selectTypeSpec mismatch")
	}
}
