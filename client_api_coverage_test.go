// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/invoke"
	"github.com/otfabric/go-mms/internal/isostack"
	"github.com/otfabric/go-mms/internal/pdu"
)

func unitClient(conn Transport) *Client {
	return &Client{
		logger:     slog.New(discardHandler{}),
		conn:       conn,
		tracker:    invoke.NewTracker(0),
		concludeCh: make(chan struct{}, 1),
		readerDone: make(chan struct{}),
		maxPDUSize: 64,
	}
}

func TestSendReceiveRaw_AndHooks(t *testing.T) {
	mt := newMockTransport()
	c := unitClient(mt)
	var mu sync.Mutex
	var events []string
	c.opts.RawHook = func(dir string, data []byte) {
		mu.Lock()
		events = append(events, dir)
		mu.Unlock()
	}

	ctx := context.Background()
	if err := c.sendRaw(ctx, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}

	mt.sendToClient([]byte{9, 9, 9})
	got, err := c.receiveRaw(ctx)
	if err != nil || len(got) != 3 {
		t.Fatalf("recv: %v %v", got, err)
	}

	// Oversized PDU.
	mt.sendToClient(make([]byte, 100))
	if _, err := c.receiveRaw(ctx); err == nil {
		t.Fatal("expected max PDU size error")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) < 2 {
		t.Fatalf("hooks=%v", events)
	}
}

func TestDispatchConfirmed_Edges(t *testing.T) {
	c := unitClient(newMockTransport())
	c.dispatchConfirmed(pdu.PduConfirmedResponse, []byte{0xff}) // bad invoke id

	// Unknown invoke ID while open.
	content := append(berutilInvokeID(1), 0x00)
	c.dispatchConfirmed(pdu.PduConfirmedResponse, content)

	// Unknown invoke ID while closing.
	c.closed = true
	c.dispatchConfirmed(pdu.PduConfirmedResponse, content)
}

// berutilInvokeID builds a minimal confirmed-response content with invokeID INTEGER.
func berutilInvokeID(id int) []byte {
	// ConfirmedResponse content starts with InvokeID as UNIVERSAL INTEGER (0x02).
	return []byte{0x02, 0x01, byte(id)}
}

func TestDispatchUnconfirmed_AndInfoReport(t *testing.T) {
	c := unitClient(newMockTransport())

	// Bad unconfirmed body.
	c.dispatchUnconfirmed([]byte{0xff})

	// Valid report, no handler.
	report, err := pdu.MarshalInformationReport(&pdu.InformationReportWire{
		Variables: []pdu.ObjectNameWire{{Scope: pdu.ScopeVMD, ItemID: "v"}},
		Values:    []*pdu.DataValue{{Tag: pdu.TagDataInteger, Int: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// MarshalInformationReport returns full UnconfirmedPDU; UnmarshalInformationReport
	// expects the content inside UnconfirmedPDU. dispatchUnconfirmed receives DecodePdu content.
	kind, content, err := pdu.DecodePdu(report)
	if err != nil || kind != pdu.PduUnconfirmed {
		t.Fatalf("kind=%v err=%v", kind, err)
	}
	c.dispatchUnconfirmed(content)

	// Conversion error: bad list name scope (craft wire after unmarshal path via infoReportToIndication).
	if _, err := c.infoReportToIndication(&pdu.InformationReportWire{
		ListName: &pdu.ObjectNameWire{Scope: 99, ItemID: "L"},
	}); err == nil {
		t.Fatal("bad list name")
	}
	if _, err := c.infoReportToIndication(&pdu.InformationReportWire{
		Variables: []pdu.ObjectNameWire{{Scope: 99, ItemID: "v"}},
	}); err == nil {
		t.Fatal("bad variable name")
	}
	if _, err := c.infoReportToIndication(&pdu.InformationReportWire{
		Values: []*pdu.DataValue{{Tag: 0xFF}},
	}); err == nil {
		t.Fatal("bad value")
	}
	ind, err := c.infoReportToIndication(&pdu.InformationReportWire{
		ListName:  &pdu.ObjectNameWire{Scope: pdu.ScopeVMD, ItemID: "L"},
		Variables: []pdu.ObjectNameWire{{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: "v"}},
		Values:    []*pdu.DataValue{{Tag: pdu.TagDataBoolean, Bool: true}},
	})
	if err != nil || ind.ListName == nil || len(ind.Variables) != 1 || len(ind.Values) != 1 {
		t.Fatalf("%+v err=%v", ind, err)
	}

	// Conversion failure inside dispatch (unmarshals, then infoReportToIndication fails).
	badValReport, err := pdu.MarshalInformationReport(&pdu.InformationReportWire{
		Variables: []pdu.ObjectNameWire{{Scope: pdu.ScopeVMD, ItemID: "v"}},
		Values:    []*pdu.DataValue{{Tag: pdu.TagDataAccessError, ErrCode: 99999}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, badContent, err := pdu.DecodePdu(badValReport)
	if err != nil {
		t.Fatal(err)
	}
	c.dispatchUnconfirmed(badContent) // logs convert error, no panic

	// Handler + panic recovery.
	called := make(chan struct{}, 1)
	c.OnInformationReport(func(*InformationReportIndication) {
		called <- struct{}{}
		panic("boom")
	})
	c.dispatchUnconfirmed(content)
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("handler not called")
	}
}

func TestReaderLoop_Edges(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t.Run("receive error", func(t *testing.T) {
		mt := newMockTransport()
		c := unitClient(mt)
		done := make(chan struct{})
		go func() {
			c.readerLoop(ctx)
			close(done)
		}()
		_ = mt.Close()
		cancel() // unblock Receive
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("timeout")
		}
	})

	t.Run("decode / PDU / conclude / unexpected", func(t *testing.T) {
		mt := newMockTransport()
		c := unitClient(mt)
		c.readerDone = make(chan struct{})
		loopCtx, loopCancel := context.WithCancel(context.Background())
		defer loopCancel()
		go c.readerLoop(loopCtx)

		// Bad ISO framing → decode error stops loop.
		mt.sendToClient([]byte{0x01, 0x02})
		select {
		case <-c.readerDone:
		case <-time.After(2 * time.Second):
			t.Fatal("decode error should stop reader")
		}

		// Fresh client for remaining cases.
		mt = newMockTransport()
		c = unitClient(mt)
		c.readerDone = make(chan struct{})
		loopCtx, loopCancel = context.WithCancel(context.Background())
		defer loopCancel()
		go c.readerLoop(loopCtx)

		// Valid ISO DATA with garbage MMS → PDU decode error.
		mt.sendToClient(isostack.EncodeDataRequest([]byte{0xff}))
		select {
		case <-c.readerDone:
		case <-time.After(2 * time.Second):
			t.Fatal("PDU error should stop reader")
		}

		mt = newMockTransport()
		c = unitClient(mt)
		c.readerDone = make(chan struct{})
		loopCtx, loopCancel = context.WithCancel(context.Background())
		defer loopCancel()
		go c.readerLoop(loopCtx)

		// Unexpected PDU kind (Initiate request is not handled in switch → warn + continue).
		mt.sendToClient(isostack.EncodeDataRequest([]byte{asn1util.TagInitiateRequest, 0x00}))
		// Unconfirmed info report (no handler).
		rep, err := pdu.MarshalInformationReport(&pdu.InformationReportWire{
			Variables: []pdu.ObjectNameWire{{Scope: pdu.ScopeVMD, ItemID: "v"}},
			Values:    []*pdu.DataValue{{Tag: pdu.TagDataInteger, Int: 1}},
		})
		if err != nil {
			t.Fatal(err)
		}
		mt.sendToClient(isostack.EncodeDataRequest(rep))
		// Confirmed response / error / reject with unknown invoke id (continues).
		unknown, _ := codec.MarshalConfirmedResponse(99, asn1util.TagNumIdentify, true, []byte{0x80, 0x00})
		mt.sendToClient(isostack.EncodeDataRequest(unknown))
		mt.sendToClient(isostack.EncodeDataRequest(codec.MarshalConfirmedError(98, 1, 0)))
		mt.sendToClient(isostack.EncodeDataRequest(codec.MarshalRejectPDU(97, 1, 0)))
		// ConcludeResponse stops loop.
		mt.sendToClient(isostack.EncodeDataRequest([]byte{asn1util.TagConcludeResponse, 0x00}))
		select {
		case <-c.readerDone:
		case <-time.After(2 * time.Second):
			t.Fatal("conclude response should stop reader")
		}

		// Separate client for ConcludeError.
		mt = newMockTransport()
		c = unitClient(mt)
		c.readerDone = make(chan struct{})
		loopCtx, loopCancel = context.WithCancel(context.Background())
		defer loopCancel()
		go c.readerLoop(loopCtx)
		mt.sendToClient(isostack.EncodeDataRequest([]byte{asn1util.TagConcludeError, 0x00}))
		select {
		case <-c.readerDone:
		case <-time.After(2 * time.Second):
			t.Fatal("conclude error should stop reader")
		}
		select {
		case <-c.concludeCh:
		default:
			t.Fatal("concludeCh should be signaled")
		}
	})
}

func TestValidateAccessSelectors_Unit(t *testing.T) {
	if err := validateAccessSelectors(nil); err != nil {
		t.Fatal(err)
	}
	if err := validateAccessSelectors([]AccessSelector{{}}); err == nil {
		t.Fatal("empty selector")
	}
	idx := 0
	if err := validateAccessSelectors([]AccessSelector{{Component: "a", Index: &idx}}); err == nil {
		t.Fatal("conflicting")
	}
	neg := -1
	if err := validateAccessSelectors([]AccessSelector{{Index: &neg}}); err == nil {
		t.Fatal("neg index")
	}
	if err := validateAccessSelectors([]AccessSelector{{IndexRange: &IndexRange{Start: -1, Count: 1}}}); err == nil {
		t.Fatal("neg start")
	}
	if err := validateAccessSelectors([]AccessSelector{{IndexRange: &IndexRange{Start: 0, Count: 0}}}); err == nil {
		t.Fatal("zero count")
	}
	if err := validateAccessSelectors([]AccessSelector{
		{Component: "a"},
		{Index: &idx},
		{IndexRange: &IndexRange{Start: 0, Count: 2}},
	}); err != nil {
		t.Fatal(err)
	}
}

func associateMockClient(t *testing.T) (*Client, *mockServer, *mockTransport, context.Context) {
	t.Helper()
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()
	go srv.handleAssociation(ctx)
	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client, srv, mt, ctx
}

func closeMockClient(t *testing.T, client *Client, srv *mockServer, ctx context.Context) {
	t.Helper()
	go func() {
		_, _, _ = srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	_ = client.Close(ctx)
}

func TestClientAPIs_ProtocolAndValidation(t *testing.T) {
	client, srv, _, ctx := associateMockClient(t)
	defer closeMockClient(t, client, srv, ctx)

	// ReadMultiple validation.
	if _, err := client.ReadMultiple(ctx, []ObjectName{{Scope: ObjectScopeDomain, ItemID: "x"}}); err == nil {
		t.Fatal("read missing domain")
	}

	// Wrong service kind for ReadMultiple.
	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		srv.sendIdentifyResponse(ctx, id)
	}()
	if _, err := client.ReadMultiple(ctx, []ObjectName{
		{Scope: ObjectScopeDomain, Domain: "d", ItemID: "v"},
	}); err == nil {
		t.Fatal("wrong service read")
	}

	// Count mismatch.
	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		body, _ := pdu.MarshalReadResponse([]*pdu.AccessResult{
			{Data: &pdu.DataValue{Tag: pdu.TagDataInteger, Int: 1}},
			{Data: &pdu.DataValue{Tag: pdu.TagDataInteger, Int: 2}},
		})
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumRead, true, body)
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.ReadMultiple(ctx, []ObjectName{
		{Scope: ObjectScopeDomain, Domain: "d", ItemID: "v"},
	}); err == nil {
		t.Fatal("count mismatch")
	}

	// Bad access-error code in read result.
	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		body, _ := pdu.MarshalReadResponse([]*pdu.AccessResult{
			{IsError: true, ErrorCode: 99999},
		})
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumRead, true, body)
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.ReadMultiple(ctx, []ObjectName{
		{Scope: ObjectScopeDomain, Domain: "d", ItemID: "v"},
	}); err == nil {
		t.Fatal("bad DAE code")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumRead, true, []byte{0xff})
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.ReadMultiple(ctx, []ObjectName{
		{Scope: ObjectScopeDomain, Domain: "d", ItemID: "v"},
	}); err == nil {
		t.Fatal("read unmarshal error")
	}

	// Write validation.
	if _, err := client.Write(ctx, WriteRequest{ItemID: "v", Value: NewInteger(1)}); err == nil {
		t.Fatal("empty domain")
	}
	if _, err := client.Write(ctx, WriteRequest{DomainID: "d", Value: NewInteger(1)}); err == nil {
		t.Fatal("empty item")
	}
	if _, err := client.Write(ctx, WriteRequest{DomainID: "d", ItemID: "v"}); err == nil {
		t.Fatal("nil value")
	}
	if _, err := client.Write(ctx, WriteRequest{DomainID: "d", ItemID: "v", Value: &Value{typ: ValueTypeNamedType}}); err == nil {
		t.Fatal("bad value marshal")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		srv.sendIdentifyResponse(ctx, id)
	}()
	if _, err := client.Write(ctx, WriteRequest{DomainID: "d", ItemID: "v", Value: NewInteger(1)}); err == nil {
		t.Fatal("wrong service write")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		body, _ := pdu.MarshalWriteResponse([]pdu.WriteResult{{Success: true}, {Success: true}})
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumWrite, true, body)
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.Write(ctx, WriteRequest{DomainID: "d", ItemID: "v", Value: NewInteger(1)}); err == nil {
		t.Fatal("write count mismatch")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		body, _ := pdu.MarshalWriteResponse([]pdu.WriteResult{{Success: false, Code: 99999}})
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumWrite, true, body)
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.Write(ctx, WriteRequest{DomainID: "d", ItemID: "v", Value: NewInteger(1)}); err == nil {
		t.Fatal("write bad DAE")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		body, _ := pdu.MarshalWriteResponse([]pdu.WriteResult{{Success: false, Code: int(DataAccessErrorObjectAccessDenied)}})
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumWrite, true, body)
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.Write(ctx, WriteRequest{DomainID: "d", ItemID: "v", Value: NewInteger(1)}); err == nil {
		t.Fatal("write DAE")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumWrite, true, []byte{0xff})
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.Write(ctx, WriteRequest{DomainID: "d", ItemID: "v", Value: NewInteger(1)}); err == nil {
		t.Fatal("write unmarshal error")
	}
}

func TestClientVariablesAPIs_Edges(t *testing.T) {
	client, srv, _, ctx := associateMockClient(t)
	defer closeMockClient(t, client, srv, ctx)

	name := ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "v"}

	if _, err := client.ReadVariables(ctx, nil); err == nil {
		t.Fatal("empty vars")
	}
	if _, err := client.ReadVariables(ctx, []VariableSpec{{Name: ObjectName{Scope: ObjectScopeDomain, ItemID: "v"}}}); err == nil {
		t.Fatal("bad name")
	}
	if _, err := client.ReadVariables(ctx, []VariableSpec{{Name: name, AlternateAccess: []AccessSelector{{}}}}); err == nil {
		t.Fatal("bad selectors")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		srv.sendIdentifyResponse(ctx, id)
	}()
	if _, err := client.ReadVariables(ctx, []VariableSpec{{Name: name}}); err == nil {
		t.Fatal("wrong service")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		body, _ := pdu.MarshalReadResponse([]*pdu.AccessResult{
			{Data: &pdu.DataValue{Tag: pdu.TagDataInteger, Int: 1}},
			{Data: &pdu.DataValue{Tag: pdu.TagDataInteger, Int: 2}},
		})
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumRead, true, body)
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.ReadVariables(ctx, []VariableSpec{{Name: name}}); err == nil {
		t.Fatal("count mismatch")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		body, _ := pdu.MarshalReadResponse([]*pdu.AccessResult{
			{IsError: true, ErrorCode: int(DataAccessErrorObjectNonExistent)},
		})
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumRead, true, body)
		srv.sendDataResponse(ctx, resp)
	}()
	results, err := client.ReadVariables(ctx, []VariableSpec{{Name: name}})
	if err != nil || results[0].Value != nil {
		t.Fatalf("%v %v", results, err)
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		body, _ := pdu.MarshalReadResponse([]*pdu.AccessResult{
			{IsError: true, ErrorCode: 99999},
		})
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumRead, true, body)
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.ReadVariables(ctx, []VariableSpec{{Name: name}}); err == nil {
		t.Fatal("ReadVariables bad DAE code")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumRead, true, []byte{0xff})
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.ReadVariables(ctx, []VariableSpec{{Name: name}}); err == nil {
		t.Fatal("ReadVariables unmarshal error")
	}

	// WriteVariables validation.
	if _, err := client.WriteVariables(ctx, []VariableSpec{{Name: name}}, nil); err == nil {
		t.Fatal("count mismatch vals")
	}
	if _, err := client.WriteVariables(ctx, []VariableSpec{{Name: name}}, []*Value{nil}); err == nil {
		t.Fatal("nil value")
	}
	if _, err := client.WriteVariables(ctx, []VariableSpec{{Name: ObjectName{Scope: ObjectScopeDomain, ItemID: "v"}}}, []*Value{NewInteger(1)}); err == nil {
		t.Fatal("bad name")
	}
	if _, err := client.WriteVariables(ctx, []VariableSpec{{Name: name, AlternateAccess: []AccessSelector{{}}}}, []*Value{NewInteger(1)}); err == nil {
		t.Fatal("bad aa")
	}
	if _, err := client.WriteVariables(ctx, []VariableSpec{{Name: name}}, []*Value{{typ: ValueTypeNamedType}}); err == nil {
		t.Fatal("bad marshal")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		srv.sendIdentifyResponse(ctx, id)
	}()
	if _, err := client.WriteVariables(ctx, []VariableSpec{{Name: name}}, []*Value{NewInteger(1)}); err == nil {
		t.Fatal("wrong service write vars")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		body, _ := pdu.MarshalWriteResponse([]pdu.WriteResult{{Success: true}, {Success: true}})
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumWrite, true, body)
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.WriteVariables(ctx, []VariableSpec{{Name: name}}, []*Value{NewInteger(1)}); err == nil {
		t.Fatal("write vars count mismatch")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		body, _ := pdu.MarshalWriteResponse([]pdu.WriteResult{{Success: false, Code: 99999}})
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumWrite, true, body)
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.WriteVariables(ctx, []VariableSpec{{Name: name}}, []*Value{NewInteger(1)}); err == nil {
		t.Fatal("write vars bad DAE")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		body, _ := pdu.MarshalWriteResponse([]pdu.WriteResult{{Success: false, Code: int(DataAccessErrorObjectAccessDenied)}})
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumWrite, true, body)
		srv.sendDataResponse(ctx, resp)
	}()
	wres, err := client.WriteVariables(ctx, []VariableSpec{{Name: name}}, []*Value{NewInteger(1)})
	if err != nil || wres[0].Success {
		t.Fatalf("%v %v", wres, err)
	}
}

func TestClientConvenience_DataAccessErrors(t *testing.T) {
	client, srv, _, ctx := associateMockClient(t)
	defer closeMockClient(t, client, srv, ctx)
	name := ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "v"}

	respondReadDAE := func() {
		id, _, _ := srv.handleDataRequest(ctx)
		body, _ := pdu.MarshalReadResponse([]*pdu.AccessResult{
			{IsError: true, ErrorCode: int(DataAccessErrorObjectNonExistent)},
		})
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumRead, true, body)
		srv.sendDataResponse(ctx, resp)
	}
	respondWriteDAE := func() {
		id, _, _ := srv.handleDataRequest(ctx)
		body, _ := pdu.MarshalWriteResponse([]pdu.WriteResult{
			{Success: false, Code: int(DataAccessErrorObjectAccessDenied)},
		})
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumWrite, true, body)
		srv.sendDataResponse(ctx, resp)
	}

	go respondReadDAE()
	if _, err := client.ReadComponent(ctx, name, "c"); err == nil {
		t.Fatal("ReadComponent DAE")
	}
	go respondWriteDAE()
	if err := client.WriteComponent(ctx, name, "c", NewInteger(1)); err == nil {
		t.Fatal("WriteComponent DAE")
	}
	// Validation errors on convenience wrappers (err return path before network).
	badName := ObjectName{Scope: ObjectScopeDomain, ItemID: "v"}
	if _, err := client.ReadComponent(ctx, badName, "c"); err == nil {
		t.Fatal("ReadComponent bad name")
	}
	if err := client.WriteComponent(ctx, badName, "c", NewInteger(1)); err == nil {
		t.Fatal("WriteComponent bad name")
	}
	if _, err := client.ReadByIndex(ctx, badName, 0); err == nil {
		t.Fatal("ReadByIndex bad name")
	}
	if _, err := client.ReadArrayElement(ctx, badName, 0); err == nil {
		t.Fatal("ReadArrayElement bad name")
	}
	if err := client.WriteArrayElement(ctx, badName, 0, NewInteger(1)); err == nil {
		t.Fatal("WriteArrayElement bad name")
	}
	if _, err := client.ReadArrayRange(ctx, badName, 0, 1); err == nil {
		t.Fatal("ReadArrayRange bad name")
	}
	if _, err := client.ReadObject(ctx, badName); err == nil {
		t.Fatal("ReadObject bad name")
	}
	go respondReadDAE()
	if _, err := client.ReadByIndex(ctx, name, 0); err == nil {
		t.Fatal("ReadByIndex DAE")
	}
	go respondReadDAE()
	if _, err := client.ReadArrayElement(ctx, name, 0); err == nil {
		t.Fatal("ReadArrayElement DAE")
	}
	go respondWriteDAE()
	if err := client.WriteArrayElement(ctx, name, 0, NewInteger(1)); err == nil {
		t.Fatal("WriteArrayElement DAE")
	}
	go respondReadDAE()
	if _, err := client.ReadArrayRange(ctx, name, 0, 1); err == nil {
		t.Fatal("ReadArrayRange DAE")
	}
	go respondReadDAE()
	if _, err := client.ReadObject(ctx, name); err == nil {
		t.Fatal("ReadObject DAE")
	}

	// WriteObject validation + DAE + wrong service + count mismatch.
	if _, err := client.WriteObject(ctx, ObjectName{Scope: ObjectScopeDomain, ItemID: "v"}, NewInteger(1)); err == nil {
		t.Fatal("WriteObject bad name")
	}
	if _, err := client.WriteObject(ctx, name, nil); err == nil {
		t.Fatal("WriteObject nil")
	}
	if _, err := client.WriteObject(ctx, name, &Value{typ: ValueTypeNamedType}); err == nil {
		t.Fatal("WriteObject bad value")
	}
	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		srv.sendIdentifyResponse(ctx, id)
	}()
	if _, err := client.WriteObject(ctx, name, NewInteger(1)); err == nil {
		t.Fatal("WriteObject wrong service")
	}
	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		body, _ := pdu.MarshalWriteResponse([]pdu.WriteResult{{Success: true}, {Success: true}})
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumWrite, true, body)
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.WriteObject(ctx, name, NewInteger(1)); err == nil {
		t.Fatal("WriteObject count")
	}
	go respondWriteDAE()
	if _, err := client.WriteObject(ctx, name, NewInteger(1)); err == nil {
		t.Fatal("WriteObject DAE")
	}
	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		body, _ := pdu.MarshalWriteResponse([]pdu.WriteResult{{Success: false, Code: 99999}})
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumWrite, true, body)
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.WriteObject(ctx, name, NewInteger(1)); err == nil {
		t.Fatal("WriteObject bad DAE code")
	}
	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		body, _ := pdu.MarshalWriteResponse([]pdu.WriteResult{{Success: true}})
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumWrite, true, body)
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.WriteObject(ctx, name, NewInteger(1)); err != nil {
		t.Fatalf("WriteObject success: %v", err)
	}
	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumWrite, true, []byte{0xff})
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.WriteObject(ctx, name, NewInteger(1)); err == nil {
		t.Fatal("WriteObject unmarshal error")
	}
}

func TestClientNVL_Edges(t *testing.T) {
	client, srv, _, ctx := associateMockClient(t)
	defer closeMockClient(t, client, srv, ctx)
	list := ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "L"}

	if _, err := client.ReadNamedVariableList(ctx, ObjectName{Scope: ObjectScopeDomain, ItemID: "L"}); err == nil {
		t.Fatal("bad list name")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		srv.sendIdentifyResponse(ctx, id)
	}()
	if _, err := client.ReadNamedVariableList(ctx, list); err == nil {
		t.Fatal("wrong service")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		body, _ := pdu.MarshalReadResponse([]*pdu.AccessResult{
			{IsError: true, ErrorCode: int(DataAccessErrorObjectNonExistent)},
			{Data: &pdu.DataValue{Tag: pdu.TagDataInteger, Int: 3}},
			{IsError: true, ErrorCode: 99999},
		})
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumRead, true, body)
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.ReadNamedVariableList(ctx, list, ReadNamedVariableListOptions{SpecificationWithResult: true}); err == nil {
		t.Fatal("bad DAE in NVL read")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		body, _ := pdu.MarshalReadResponse([]*pdu.AccessResult{
			{IsError: true, ErrorCode: int(DataAccessErrorObjectNonExistent)},
			{Data: &pdu.DataValue{Tag: pdu.TagDataInteger, Int: 3}},
		})
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumRead, true, body)
		srv.sendDataResponse(ctx, resp)
	}()
	res, err := client.ReadNamedVariableList(ctx, list)
	if err != nil || len(res) != 2 || res[0].Value != nil || res[1].Value == nil {
		t.Fatalf("%v %v", res, err)
	}
	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumRead, true, []byte{0xff})
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.ReadNamedVariableList(ctx, list); err == nil {
		t.Fatal("NVL read unmarshal error")
	}

	if _, err := client.WriteNamedVariableList(ctx, ObjectName{Scope: ObjectScopeDomain, ItemID: "L"}, []*Value{NewInteger(1)}); err == nil {
		t.Fatal("bad list")
	}
	if _, err := client.WriteNamedVariableList(ctx, list, []*Value{nil}); err == nil {
		t.Fatal("nil value")
	}
	if _, err := client.WriteNamedVariableList(ctx, list, []*Value{{typ: ValueTypeNamedType}}); err == nil {
		t.Fatal("bad marshal")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		srv.sendIdentifyResponse(ctx, id)
	}()
	if _, err := client.WriteNamedVariableList(ctx, list, []*Value{NewInteger(1)}); err == nil {
		t.Fatal("wrong service")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		body, _ := pdu.MarshalWriteResponse([]pdu.WriteResult{{Success: true}, {Success: true}})
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumWrite, true, body)
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.WriteNamedVariableList(ctx, list, []*Value{NewInteger(1)}); err == nil {
		t.Fatal("count mismatch")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		body, _ := pdu.MarshalWriteResponse([]pdu.WriteResult{{Success: false, Code: 99999}})
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumWrite, true, body)
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.WriteNamedVariableList(ctx, list, []*Value{NewInteger(1)}); err == nil {
		t.Fatal("bad DAE")
	}

	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		body, _ := pdu.MarshalWriteResponse([]pdu.WriteResult{
			{Success: true},
			{Success: false, Code: int(DataAccessErrorObjectAccessDenied)},
		})
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumWrite, true, body)
		srv.sendDataResponse(ctx, resp)
	}()
	wres, err := client.WriteNamedVariableList(ctx, list, []*Value{NewInteger(1), NewInteger(2)})
	if err != nil || len(wres) != 2 || !wres[0].Success || wres[1].Success {
		t.Fatalf("%v %v", wres, err)
	}
	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		resp, _ := codec.MarshalConfirmedResponse(id, asn1util.TagNumWrite, true, []byte{0xff})
		srv.sendDataResponse(ctx, resp)
	}()
	if _, err := client.WriteNamedVariableList(ctx, list, []*Value{NewInteger(1)}); err == nil {
		t.Fatal("NVL write unmarshal error")
	}
}
