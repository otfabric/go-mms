// SPDX-License-Identifier: MIT

package mms

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/pdu"
)

// limitClient associates a client with a mock server that negotiates
// max-services-outstanding-calling = outCalling.
func limitClient(t *testing.T, outCalling int, opts DialOptions) (*Client, *mockServer, *mockTransport) {
	t.Helper()
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	srv.outCalling = outCalling
	go srv.handleAssociation(context.Background())
	c, err := NewClient(context.Background(), mt, opts)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Abort(context.Background()) })
	return c, srv, mt
}

// expectNoRequest fails when the client sends a PDU within d.
func expectNoRequest(t *testing.T, mt *mockTransport, d time.Duration) {
	t.Helper()
	select {
	case data := <-mt.toServer:
		t.Fatalf("client sent a PDU beyond the negotiated limit: %x", data)
	case <-time.After(d):
	}
}

// waitWaiting polls until n requests wait for a slot.
func waitWaiting(t *testing.T, c *Client, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for c.tracker.WaitingCount() != n {
		if time.Now().After(deadline) {
			t.Fatalf("waiting = %d, want %d", c.tracker.WaitingCount(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestSpec_9506_2_8_2_OutstandingCallingLimit: the client, as calling
// MMS-user, has at most negotiatedMaxServOutstandingCalling confirmed
// requests outstanding (ISO 9506-2 clause 8.2). Further requests wait,
// in order, for a response, and are sent when a slot frees.
func TestSpec_9506_2_8_2_OutstandingCallingLimit(t *testing.T) {
	const limit, total = 2, 7
	c, srv, mt := limitClient(t, limit, defaultDialOptions())
	if got := c.Negotiated().MaxOutCalling; got != limit {
		t.Fatalf("MaxOutCalling = %d, want %d", got, limit)
	}
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make(chan error, total)
	for i := range total {
		wg.Go(func() {
			_, err := c.ReadObject(ctx, ObjectName{Scope: ObjectScopeDomain, Domain: "D", ItemID: ItemID(fmt.Sprintf("v%d", i))})
			errs <- err
		})
	}

	var inFlight []codec.InvokeID
	answered := 0
	for answered < total {
		// Take every request the client sends until it stops.
		for len(inFlight) < total-answered {
			select {
			case data := <-mt.toServer:
				mt.toServer <- data
				id, _, _ := srv.handleDataRequest(ctx)
				inFlight = append(inFlight, id)
				continue
			case <-time.After(30 * time.Millisecond):
			}
			break
		}
		if len(inFlight) > limit {
			t.Fatalf("%d requests outstanding, negotiated %d", len(inFlight), limit)
		}
		if len(inFlight) == 0 {
			t.Fatal("no request outstanding while callers wait")
		}
		srv.sendReadResponse(ctx, inFlight[0], []*pdu.DataValue{{Tag: pdu.TagDataInteger, Int: 1}})
		inFlight = inFlight[1:]
		answered++
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("Read: %v", err)
		}
	}
	if n := c.tracker.PendingCount(); n != 0 {
		t.Errorf("pending = %d after all responses", n)
	}
}

// TestSpec_9506_2_8_2_OutstandingWaitHonoursContext: a request waiting
// for a slot ends with its context and is never sent.
func TestSpec_9506_2_8_2_OutstandingWaitHonoursContext(t *testing.T) {
	c, srv, mt := limitClient(t, 1, defaultDialOptions())
	ctx := context.Background()

	first := make(chan error, 1)
	go func() { _, err := c.Identify(ctx); first <- err }()
	id, _, _ := srv.handleDataRequest(ctx)

	wctx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	_, err := c.Identify(wctx)
	if !errors.Is(err, ErrInvokeTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting Identify = %v, want ErrInvokeTimeout", err)
	}
	cctx, ccancel := context.WithCancel(ctx)
	go func() { waitWaiting(t, c, 1); ccancel() }()
	if _, err := c.Identify(cctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Identify = %v, want context.Canceled", err)
	}
	expectNoRequest(t, mt, 20*time.Millisecond)
	if n := c.tracker.WaitingCount(); n != 0 {
		t.Errorf("waiting = %d after the waiters ended", n)
	}

	srv.sendIdentifyResponse(ctx, id)
	if err := <-first; err != nil {
		t.Fatalf("first Identify: %v", err)
	}
}

// TestSpec_9506_2_8_2_OutstandingFailFast: with FailFastOutstanding a
// request at the limit fails with ErrMaxOutstanding and is not sent.
func TestSpec_9506_2_8_2_OutstandingFailFast(t *testing.T) {
	opts := defaultDialOptions()
	opts.MMS.FailFastOutstanding = true
	c, srv, mt := limitClient(t, 1, opts)
	ctx := context.Background()

	first := make(chan error, 1)
	go func() { _, err := c.Identify(ctx); first <- err }()
	id, _, _ := srv.handleDataRequest(ctx)

	_, err := c.Identify(ctx)
	if !errors.Is(err, ErrMaxOutstanding) {
		t.Fatalf("Identify at the limit = %v, want ErrMaxOutstanding", err)
	}
	expectNoRequest(t, mt, 20*time.Millisecond)

	srv.sendIdentifyResponse(ctx, id)
	if err := <-first; err != nil {
		t.Fatalf("first Identify: %v", err)
	}
	// The slot is free again.
	go func() {
		id, _, _ := srv.handleDataRequest(ctx)
		srv.sendIdentifyResponse(ctx, id)
	}()
	if _, err := c.Identify(ctx); err != nil {
		t.Fatalf("Identify after the response: %v", err)
	}
}

// TestSpec_9506_2_8_2_AbandonedRequestStaysOutstanding: a request whose
// caller gave up is still outstanding at the server (ISO 9506-2 clause
// 6.3.1: it ends with the response). Its slot frees when the late
// response arrives, which is discarded.
func TestSpec_9506_2_8_2_AbandonedRequestStaysOutstanding(t *testing.T) {
	c, srv, mt := limitClient(t, 1, defaultDialOptions())
	ctx := context.Background()

	actx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	abandoned := make(chan error, 1)
	go func() { _, err := c.Identify(actx); abandoned <- err }()
	lateID, _, _ := srv.handleDataRequest(ctx)
	if err := <-abandoned; !errors.Is(err, ErrInvokeTimeout) {
		t.Fatalf("abandoned Identify = %v, want ErrInvokeTimeout", err)
	}

	next := make(chan error, 1)
	go func() { _, err := c.Identify(ctx); next <- err }()
	waitWaiting(t, c, 1)
	expectNoRequest(t, mt, 20*time.Millisecond)

	srv.sendIdentifyResponse(ctx, lateID) // late: frees the slot
	id, _, _ := srv.handleDataRequest(ctx)
	if id == lateID {
		t.Fatalf("invoke ID %d reused", id)
	}
	srv.sendIdentifyResponse(ctx, id)
	if err := <-next; err != nil {
		t.Fatalf("next Identify: %v", err)
	}
}

// TestSpec_9506_2_8_2_OutstandingWaitersEndOnClose: Close and a lost
// association end requests waiting for a slot with ErrClosed.
func TestSpec_9506_2_8_2_OutstandingWaitersEndOnClose(t *testing.T) {
	for _, how := range []string{"abort", "lost"} {
		t.Run(how, func(t *testing.T) {
			c, srv, mt := limitClient(t, 1, defaultDialOptions())
			ctx := context.Background()
			const waiters = 3
			errs := make(chan error, waiters+1)
			go func() { _, err := c.Identify(ctx); errs <- err }()
			srv.handleDataRequest(ctx)
			for range waiters {
				go func() { _, err := c.Identify(ctx); errs <- err }()
			}
			waitWaiting(t, c, waiters)
			if how == "abort" {
				_ = c.Abort(ctx)
			} else {
				mt.shutdown()
			}
			for range waiters + 1 {
				if err := <-errs; !errors.Is(err, ErrClosed) {
					t.Errorf("Identify = %v, want ErrClosed", err)
				}
			}
			if _, err := c.Identify(ctx); !errors.Is(err, ErrClosed) {
				t.Errorf("Identify after %s = %v, want ErrClosed", how, err)
			}
		})
	}
}

// nested returns a value of nesting depth levels: structures around an
// integer.
func nested(depth int) *Value {
	v := NewInteger(1)
	for range depth {
		v = NewStructure([]*Value{v})
	}
	return v
}

// nestedType is the TypeSpec of nested(depth).
func nestedType(depth int) TypeSpec {
	ts := TypeSpec{Type: ValueTypeInteger, Size: 32}
	for range depth {
		ts = TypeSpec{Type: ValueTypeStructure, Elements: []TypeSpecElement{{Name: "a", Type: ts}}}
	}
	return ts
}

// wireCapture records the MMS PDUs a client sends.
type wireCapture struct {
	mu   sync.Mutex
	sent []*CapturedPDU
}

func (w *wireCapture) hook(dir string, raw []byte) {
	if dir != RawHookSend {
		return
	}
	if c, err := DecodeTSDU(raw); err == nil && c.PDU != nil {
		w.mu.Lock()
		w.sent = append(w.sent, c.PDU)
		w.mu.Unlock()
	}
}

func (w *wireCapture) take() []*CapturedPDU {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := w.sent
	w.sent = nil
	return s
}

// nestingServer serves variables of nesting 2 ("ok") and 3 ("deep") and
// a client that proposes dataStructureNestingLevel 2.
func nestingServer(t *testing.T) (*Client, *Server, *wireCapture) {
	t.Helper()
	jp := newMemJournalProvider()
	jp.addJournal("D", "J")
	jp.addEntry("D", "J", JournalEntry{EntryID: []byte{1}, OccurrenceTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Variables: []JournalVariable{{Tag: "deep", Value: nested(3)}}})
	srv := NewServer(ServerOptions{JournalProvider: jp})
	if err := srv.RegisterDomain("D"); err != nil {
		t.Fatal(err)
	}
	for name, depth := range map[string]int{"ok": 2, "deep": 3} {
		if err := srv.RegisterVariable(Variable{
			Name:     ObjectName{Scope: ObjectScopeDomain, Domain: "D", ItemID: ItemID(name)},
			TypeSpec: nestedType(depth),
			Read:     func(context.Context) (*Value, error) { return nested(depth), nil },
			Write:    func(context.Context, *Value) error { return nil },
		}); err != nil {
			t.Fatal(err)
		}
	}
	w := &wireCapture{}
	opts := DialOptions{RawHook: w.hook}
	opts.MMS.DataStructureNestingLevel = 2
	c, served := serveLoopback(t, srv, opts)
	t.Cleanup(func() {
		_ = c.Close(context.Background())
		if err := <-served; err != nil {
			t.Logf("Serve: %v", err)
		}
	})
	if got := c.Negotiated().NestingLevel; got != 2 {
		t.Fatalf("negotiated nesting %d, want 2", got)
	}
	// The server registers the connection after it has sent the
	// Initiate-Response, so NewClient can return first; a Broadcast
	// before then reaches nobody.
	waitForConnections(t, srv, 1, 5*time.Second)
	w.take()
	return c, srv, w
}

func dname(item string) ObjectName {
	return ObjectName{Scope: ObjectScopeDomain, Domain: "D", ItemID: ItemID(item)}
}

// TestSpec_9506_2_8_2_NestingLevelOnSend: a request whose Data or
// AlternateAccess is nested deeper than the negotiated
// dataStructureNestingLevel fails with ErrInvalidArgument and nothing is
// sent (the server would reject it, ISO 9506-2 clause 8.6
// max-recursion-exceeded).
func TestSpec_9506_2_8_2_NestingLevelOnSend(t *testing.T) {
	c, _, w := nestingServer(t)
	ctx := context.Background()
	idx := 0
	deepAccess := []AccessSelector{SelectComponent("a"), SelectComponent("a"), {Index: &idx}}
	cases := map[string]func() error{
		"Write": func() error {
			_, err := c.Write(ctx, WriteRequest{DomainID: "D", ItemID: "deep", Value: nested(3)})
			return err
		},
		"WriteObject": func() error { _, err := c.WriteObject(ctx, dname("deep"), nested(3)); return err },
		"WriteVariables data": func() error {
			_, err := c.WriteVariables(ctx, []VariableSpec{{Name: dname("ok")}, {Name: dname("deep")}}, []*Value{nested(1), nested(3)})
			return err
		},
		"WriteVariables access": func() error {
			_, err := c.WriteVariables(ctx, []VariableSpec{{Name: dname("deep"), AlternateAccess: deepAccess}}, []*Value{NewInteger(1)})
			return err
		},
		"WriteNamedVariableList": func() error {
			_, err := c.WriteNamedVariableList(ctx, dname("L"), []*Value{nested(3)})
			return err
		},
		"ReadVariables access": func() error {
			_, err := c.ReadVariables(ctx, []VariableSpec{{Name: dname("deep"), AlternateAccess: deepAccess}})
			return err
		},
		"DefineNamedVariableList access": func() error {
			return c.DefineNamedVariableList(ctx, DefineNamedVariableListRequest{ListName: dname("L"),
				Variables: []VariableSpec{{Name: dname("deep"), AlternateAccess: deepAccess}}})
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("err = %v, want ErrInvalidArgument", err)
			}
			if sent := w.take(); len(sent) != 0 {
				t.Fatalf("sent %d PDUs, want none", len(sent))
			}
		})
	}

	// At the negotiated level the requests are sent.
	if _, err := c.WriteObject(ctx, dname("ok"), nested(2)); err != nil {
		t.Fatalf("WriteObject at the level: %v", err)
	}
	if _, err := c.ReadVariables(ctx, []VariableSpec{{Name: dname("ok"), AlternateAccess: deepAccess[:2]}}); err != nil {
		t.Fatalf("ReadVariables at the level: %v", err)
	}
}

// expectReject checks that the client sent exactly one RejectPDU with
// the given rejectReason alternative and value (ISO 9506-2 clause 8.6).
func expectReject(t *testing.T, w *wireCapture, typ, reason int, invokeID uint32, hasID bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var rejects []*CapturedPDU
	for len(rejects) == 0 && time.Now().Before(deadline) {
		for _, p := range w.take() {
			if p.Kind == "reject" {
				rejects = append(rejects, p)
			}
		}
		time.Sleep(time.Millisecond)
	}
	if len(rejects) != 1 {
		t.Fatalf("client sent %d RejectPDUs, want 1", len(rejects))
	}
	r := rejects[0]
	if r.Reject == nil || r.Reject.Type != typ || r.Reject.Reason != reason || r.HasInvokeID != hasID || (hasID && r.InvokeID != invokeID) {
		t.Fatalf("reject = %+v (invoke %d/%v), want type %d reason %d invoke %d/%v", r.Reject, r.InvokeID, r.HasInvokeID, typ, reason, invokeID, hasID)
	}
}

// TestSpec_9506_2_8_6_ResponseNestedTooDeep: a confirmed response whose
// Data or TypeSpecification is nested deeper than the negotiated
// dataStructureNestingLevel fails with ErrDecodeFailed and is rejected
// with confirmed-responsePDU max-recursion-exceeded (ISO 9506-2 clauses
// 6.4 and 8.6); the association continues.
func TestSpec_9506_2_8_6_ResponseNestedTooDeep(t *testing.T) {
	c, _, w := nestingServer(t)
	ctx := context.Background()
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	cases := map[string]func() error{
		"ReadObject": func() error { _, err := c.ReadObject(ctx, dname("deep")); return err },
		"ReadVariables": func() error {
			_, err := c.ReadVariables(ctx, []VariableSpec{{Name: dname("ok")}, {Name: dname("deep")}})
			return err
		},
		"GetVariableAccessAttributes": func() error { _, err := c.GetVariableAccessAttributes(ctx, dname("deep")); return err },
		"ReadJournal": func() error {
			_, err := c.ReadJournalTimeRange(ctx, "D", "J", start, start.AddDate(10, 0, 0))
			return err
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			err := call()
			if !errors.Is(err, ErrDecodeFailed) {
				t.Fatalf("err = %v, want ErrDecodeFailed", err)
			}
			expectRejectAny(t, w, int(RejectConfirmedResponse), rejectResponseMaxRecursion)
			if _, err := c.ReadObject(ctx, dname("ok")); err != nil {
				t.Fatalf("association lost after the reject: %v", err)
			}
			w.take()
		})
	}
	if _, err := c.GetVariableAccessAttributes(ctx, dname("ok")); err != nil {
		t.Fatalf("GetVariableAccessAttributes at the level: %v", err)
	}
}

// expectRejectAny is expectReject for a Reject that carries the invoke ID
// of the request the client sent just before it.
func expectRejectAny(t *testing.T, w *wireCapture, typ, reason int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var sent []*CapturedPDU
	for time.Now().Before(deadline) {
		w.mu.Lock()
		has := false
		for _, p := range w.sent {
			has = has || p.Kind == "reject"
		}
		w.mu.Unlock()
		if has {
			break
		}
		time.Sleep(time.Millisecond)
	}
	sent = w.take()
	var req, rej *CapturedPDU
	for _, p := range sent {
		switch p.Kind {
		case "confirmed-request":
			req = p
		case "reject":
			if rej != nil {
				t.Fatal("more than one RejectPDU")
			}
			rej = p
		}
	}
	if req == nil || rej == nil {
		t.Fatalf("sent %d PDUs without a request and a reject", len(sent))
	}
	if rej.Reject == nil || rej.Reject.Type != typ || rej.Reject.Reason != reason || !rej.HasInvokeID || rej.InvokeID != req.InvokeID {
		t.Fatalf("reject = %+v (invoke %d/%v), want type %d reason %d invoke %d", rej.Reject, rej.InvokeID, rej.HasInvokeID, typ, reason, req.InvokeID)
	}
}

// TestSpec_9506_2_8_6_InformationReportNestedTooDeep: an
// InformationReport nested deeper than the negotiated level is not
// delivered and is rejected with unconfirmedPDU max-recursion-exceeded
// (ISO 9506-2 clauses 6.4 and 8.6).
func TestSpec_9506_2_8_6_InformationReportNestedTooDeep(t *testing.T) {
	c, srv, w := nestingServer(t)
	ctx := context.Background()
	var delivered atomic.Int32
	got := make(chan struct{}, 4)
	c.OnInformationReport(func(*InformationReportIndication) { delivered.Add(1); got <- struct{}{} })

	if err := srv.Broadcast(ctx, &InformationReportRequest{Variables: []ObjectName{dname("deep")}, Values: []*Value{nested(3)}}); err != nil {
		t.Fatal(err)
	}
	expectReject(t, w, int(RejectUnconfirmed), rejectUnconfirmedMaxRecursion, 0, false)

	if err := srv.Broadcast(ctx, &InformationReportRequest{Variables: []ObjectName{dname("ok")}, Values: []*Value{nested(2)}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("report at the negotiated level not delivered")
	}
	if n := delivered.Load(); n != 1 {
		t.Fatalf("delivered %d reports, want 1", n)
	}
}

// TestNesting_Counts pins the nesting count shared by the client checks
// and the server's Write check.
func TestNesting_Counts(t *testing.T) {
	for depth := range 4 {
		dv, err := valueToDataValue(nested(depth))
		if err != nil {
			t.Fatal(err)
		}
		if got := dataNesting(dv); got != depth {
			t.Errorf("dataNesting(%d) = %d", depth, got)
		}
		ts, err := typeSpecToWire(nestedType(depth))
		if err != nil {
			t.Fatal(err)
		}
		if got := typeSpecNesting(&ts); got != depth {
			t.Errorf("typeSpecNesting(%d) = %d", depth, got)
		}
	}
	arr := pdu.TypeSpecWire{Tag: typeSpecArray, Element: &pdu.TypeSpecWire{Tag: 5}}
	if got := typeSpecNesting(&arr); got != 1 {
		t.Errorf("array nesting = %d, want 1", got)
	}
	if got := typeSpecNesting(nil); got != 0 {
		t.Errorf("nil nesting = %d", got)
	}
	if got := dataListNesting(nil); got != 0 {
		t.Errorf("empty list nesting = %d", got)
	}

	// The RejectPDUs the client sends (ISO 9506-2 clause 8.6).
	if got := codec.MarshalRejectPDU(7, int(RejectConfirmedResponse), rejectResponseMaxRecursion); !bytes.Equal(got, hx(t, "a406800107820105")) {
		t.Errorf("confirmed-responsePDU reject = %x", got)
	}
	if got := codec.MarshalRejectPDUNoInvokeID(int(RejectUnconfirmed), rejectUnconfirmedMaxRecursion); !bytes.Equal(got, hx(t, "a403840103")) {
		t.Errorf("unconfirmedPDU reject = %x", got)
	}

	// A negotiated level of 0 is not enforced.
	c := &Client{}
	if err := c.checkSendNesting("write", &pdu.DataValue{Tag: pdu.TagDataStructure}); err != nil {
		t.Errorf("level 0: %v", err)
	}
	if err := c.checkResponseNesting(context.Background(), 1, "read", 9); err != nil {
		t.Errorf("level 0: %v", err)
	}
}

// TestSpec_9506_2_8_2_ServerServesPipelinedRequestsSerially: the server
// serves confirmed requests one at a time, so it never has more than one
// indication outstanding and the negotiatedMaxServOutstandingCalling (5
// here) cannot be exceeded at the server: requests a client pipelines
// beyond it wait in the transport and are answered in order, never with
// Reject confirmed-requestPDU max-serv-outstanding-exceeded (ISO 9506-2
// clause 8.6, value 6).
func TestSpec_9506_2_8_2_ServerServesPipelinedRequestsSerially(t *testing.T) {
	srv := NewServer(ServerOptions{})
	srv.HandleIdentify(func(context.Context, IdentifyRequest) (*ServerIdentity, error) {
		return &ServerIdentity{Vendor: "v", Model: "m", Revision: "r"}, nil
	})
	const n = 8
	var reqs [][]byte
	for id := range codec.InvokeID(n) {
		reqs = append(reqs, confReq(t, id+1, 2, false, nil))
	}
	out := serverExchange(t, srv, 10, reqs...)
	if len(out) != n {
		t.Fatalf("server sent %d PDUs, want %d", len(out), n)
	}
	for i, p := range out {
		c, err := DecodePDU(p)
		if err != nil {
			t.Fatal(err)
		}
		if c.Kind != "confirmed-response" || c.InvokeID != uint32(i+1) {
			t.Errorf("PDU %d = %s invoke %d, want confirmed-response invoke %d", i, c.Kind, c.InvokeID, i+1)
		}
	}
}
