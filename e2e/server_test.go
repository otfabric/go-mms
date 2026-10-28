// SPDX-License-Identifier: MIT

package e2e

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

// ---------------------------------------------------------------------------
// Server lifecycle
// ---------------------------------------------------------------------------

func TestServer_ListenAndServe(t *testing.T) {
	srv := mms.NewServer(mms.ServerOptions{})
	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe(ctx, ln) }()

	c := dial(t, ln.Addr().String())
	if _, err := c.Identify(timeout(t)); err == nil {
		t.Error("Identify without a handler: expected service error")
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("ListenAndServe = %v, want context.Canceled", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ListenAndServe did not return promptly after cancellation")
	}
}

// Serve runs one association on a caller-accepted transport.
func TestServer_Serve(t *testing.T) {
	m := startModel(t)
	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	ctx := timeout(t)
	served := make(chan error, 1)
	go func() {
		conn, err := ln.Accept(ctx)
		if err != nil {
			served <- err
			return
		}
		defer func() { _ = conn.Close() }()
		served <- m.srv.Serve(ctx, conn)
	}()

	c := dial(t, ln.Addr().String())
	if _, err := c.Identify(ctx); err != nil {
		t.Fatalf("Identify: %v", err)
	}
	if err := c.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-served:
		if err != nil {
			t.Errorf("Serve after conclude = %v", err)
		}
	case <-ctx.Done():
		t.Fatal("Serve did not return after the client concluded")
	}
}

// NewClient runs the MMS association over a caller-dialled transport.
func TestClient_NewClient(t *testing.T) {
	m := startModel(t)
	ctx := timeout(t)
	conn, err := iso.DialTCP(ctx, m.addr)
	if err != nil {
		t.Fatal(err)
	}
	c, err := mms.NewClient(ctx, conn, mms.DialOptions{MMS: mms.MMSOptions{MaxPDUSize: 3000}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(ctx) }()
	if c.Negotiated().MaxPDUSize != 3000 {
		t.Errorf("Negotiated = %+v", c.Negotiated())
	}
	if _, err := c.Identify(ctx); err != nil {
		t.Error(err)
	}
}

// ---------------------------------------------------------------------------
// Object model registration
// ---------------------------------------------------------------------------

func TestServer_RegisterDomain_RegisterVariable(t *testing.T) {
	srv := mms.NewServer(mms.ServerOptions{})
	if err := srv.RegisterDomain("ld"); err != nil {
		t.Fatal(err)
	}
	if err := srv.RegisterDomain("ld"); err == nil {
		t.Error("duplicate domain: expected error")
	}
	read := func(context.Context) (*mms.Value, error) { return mms.NewInteger(1), nil }
	bad := []mms.Variable{
		{Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "nope", ItemID: "x"}, TypeSpec: i32, Read: read},
		{Name: aaName("x"), TypeSpec: i32, Read: read},
		{Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "ld", ItemID: "x"}, TypeSpec: mms.TypeSpec{Type: mms.ValueTypeReal}, Read: read},
		{Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "ld"}, TypeSpec: i32, Read: read},
	}
	for _, v := range bad {
		if err := srv.RegisterVariable(v); err == nil {
			t.Errorf("RegisterVariable(%+v): expected error", v.Name)
		}
	}
	good := mms.Variable{Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "ld", ItemID: "x"}, TypeSpec: i32, Read: read}
	if err := srv.RegisterVariable(good); err != nil {
		t.Fatal(err)
	}
	if err := srv.RegisterVariable(good); err == nil {
		t.Error("duplicate variable: expected error")
	}

	c := dial(t, serve(t, srv))
	got, err := c.Read(timeout(t), mms.ReadRequest{DomainID: "ld", ItemID: "x"})
	if err != nil {
		t.Fatal(err)
	}
	mustEqual(t, "ld/x", got.Value, mms.NewInteger(1))
}

func TestServer_RegisterNamedVariableList(t *testing.T) {
	m := startModel(t)
	if err := m.srv.RegisterNamedVariableList(mms.NamedVariableList{Name: domName("static"), Variables: specs(domName("NewBoolean"))}); err == nil {
		t.Error("duplicate list: expected error")
	}
	if err := m.srv.RegisterNamedVariableList(mms.NamedVariableList{Name: mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "nope", ItemID: "l"}, Variables: specs(domName("NewBoolean"))}); err == nil {
		t.Error("unknown domain: expected error")
	}
	if err := m.srv.RegisterNamedVariableList(mms.NamedVariableList{Name: vmdName("dyn"), Deletable: true, Variables: specs(vmdName("vmdvar"))}); err != nil {
		t.Fatal(err)
	}
	c := dial(t, m.addr)
	ctx := timeout(t)
	res, err := c.ReadNamedVariableList(ctx, vmdName("dyn"))
	if err != nil {
		t.Fatal(err)
	}
	mustEqual(t, "dyn", res[0].Value, mms.NewInteger(99))
	del, err := c.DeleteNamedVariableList(ctx, []mms.ObjectName{vmdName("dyn")})
	if err != nil || del.NumberDeleted != 1 {
		t.Errorf("delete deletable registered list = %+v, %v", del, err)
	}
}

func TestServer_SetVariableRead(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)
	if err := m.srv.SetVariableRead(dom, "readonly", func(context.Context) (*mms.Value, error) {
		return mms.NewInteger(-1), nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := c.Read(ctx, mms.ReadRequest{DomainID: dom, ItemID: "readonly"})
	if err != nil {
		t.Fatal(err)
	}
	mustEqual(t, "replaced read", got.Value, mms.NewInteger(-1))

	if err := m.srv.SetVariableRead(dom, "readonly", func(context.Context) (*mms.Value, error) {
		return nil, errors.New("sensor offline")
	}); err != nil {
		t.Fatal(err)
	}
	_, err = c.Read(ctx, mms.ReadRequest{DomainID: dom, ItemID: "readonly"})
	if code := dataAccessCode(t, err); code != mms.DataAccessErrorTemporarilyUnavailable {
		t.Errorf("failing read handler code = %v, want TemporarilyUnavailable", code)
	}
	if err := m.srv.SetVariableRead(dom, "nope", nil); err == nil {
		t.Error("unknown variable: expected error")
	}
}

func TestServer_HandleIdentify_HandleStatus(t *testing.T) {
	srv := mms.NewServer(mms.ServerOptions{})
	c := dial(t, serve(t, srv))
	ctx := timeout(t)
	if _, err := c.Identify(ctx); !errors.Is(err, mms.ErrServiceRejected) {
		t.Errorf("Identify without handler = %v, want service error", err)
	}
	if _, err := c.Status(ctx); !errors.Is(err, mms.ErrServiceRejected) {
		t.Errorf("Status without handler = %v, want service error", err)
	}

	srv.HandleIdentify(func(context.Context, mms.IdentifyRequest) (*mms.ServerIdentity, error) {
		return &mms.ServerIdentity{Vendor: "v", Model: "m", Revision: "r"}, nil
	})
	srv.HandleStatus(func(context.Context, mms.StatusRequest) (*mms.ServerStatus, error) {
		return nil, errors.New("status unavailable")
	})
	if id, err := c.Identify(ctx); err != nil || id.Vendor != "v" {
		t.Errorf("Identify after HandleIdentify = %+v, %v", id, err)
	}
	if _, err := c.Status(ctx); !errors.Is(err, mms.ErrServiceRejected) {
		t.Errorf("failing Status handler = %v, want service error", err)
	}
	if _, err := c.Identify(ctx); err != nil {
		t.Errorf("association unusable after a handler error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Connections and per-association state
// ---------------------------------------------------------------------------

func TestServer_Connections(t *testing.T) {
	m := startModel(t)
	if n := len(m.srv.Connections()); n != 0 {
		t.Fatalf("%d connections before dialling", n)
	}
	a := dial(t, m.addr)
	dial(t, m.addr)
	eventually(t, "two connections", func() bool { return len(m.srv.Connections()) == 2 })
	if err := a.Close(timeout(t)); err != nil {
		t.Fatal(err)
	}
	eventually(t, "one connection", func() bool { return len(m.srv.Connections()) == 1 })
}

func TestServerConn_AuthToken(t *testing.T) {
	m := startModel(t, modelOptions{auth: func(_ context.Context, a *mms.AuthContext) (mms.AuthResult, error) {
		if a.Mechanism != mms.AuthMechanismACSEPassword || string(a.Password) != "secret" {
			return mms.AuthResult{Accept: false}, nil
		}
		return mms.AuthResult{Accept: true, Token: "operator"}, nil
	}})
	ctx := timeout(t)
	_, err := iso.Dial(ctx, m.addr, iso.WithClientDialOptions(mms.DialOptions{ISO: mms.ISOOptions{Password: []byte("wrong")}}))
	if err == nil {
		t.Fatal("wrong password: expected association failure")
	}
	dial(t, m.addr, mms.DialOptions{ISO: mms.ISOOptions{Password: []byte("secret")}})
	eventually(t, "authenticated connection", func() bool { return len(m.srv.Connections()) == 1 })
	if tok := m.srv.Connections()[0].AuthToken(); tok != "operator" {
		t.Errorf("AuthToken = %v, want operator", tok)
	}
}

func TestServerConn_RegisterVariable(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	other := dial(t, m.addr)
	ctx := timeout(t)
	eventually(t, "two connections", func() bool { return len(m.srv.Connections()) == 2 })

	// Identify the ServerConn that belongs to c through a handler context.
	var sc *mms.ServerConn
	m.srv.HandleIdentify(func(ctx context.Context, _ mms.IdentifyRequest) (*mms.ServerIdentity, error) {
		sc = mms.ServerConnFromContext(ctx)
		return &mms.ServerIdentity{}, nil
	})
	if _, err := c.Identify(ctx); err != nil || sc == nil {
		t.Fatalf("Identify: %v (conn %v)", err, sc)
	}

	val := mms.NewVisibleString("session-1")
	if err := sc.RegisterVariable(mms.Variable{
		Name:     aaName("session"),
		TypeSpec: vis32,
		Read:     func(context.Context) (*mms.Value, error) { return val, nil },
		Write:    func(_ context.Context, v *mms.Value) error { val = v; return nil },
	}); err != nil {
		t.Fatal(err)
	}
	if err := sc.RegisterVariable(mms.Variable{Name: aaName("session"), TypeSpec: vis32}); err == nil {
		t.Error("duplicate association variable: expected error")
	}

	names, err := c.GetNameListAll(ctx, mms.NameListRequest{ObjectClass: mms.ObjectClassNamedVariable, Scope: mms.ObjectScopeAssociation})
	if err != nil || len(names) != 1 || names[0] != "session" {
		t.Errorf("association variables = %v, %v", names, err)
	}
	if _, err := c.WriteObject(ctx, aaName("session"), mms.NewVisibleString("session-2")); err != nil {
		t.Fatal(err)
	}
	got, err := c.ReadObject(ctx, aaName("session"))
	if err != nil {
		t.Fatal(err)
	}
	mustEqual(t, "session", got.Value, mms.NewVisibleString("session-2"))
	if _, err := c.GetVariableAccessAttributes(ctx, aaName("session")); err != nil {
		t.Errorf("GetVariableAccessAttributes: %v", err)
	}

	if _, err := other.ReadObject(ctx, aaName("session")); err == nil {
		t.Error("association variable visible to another association")
	}
}

func TestServerConn_Abort(t *testing.T) {
	m := startModel(t)
	c := dial(t, m.addr)
	ctx := timeout(t)
	eventually(t, "server connection", func() bool { return len(m.srv.Connections()) == 1 })
	if err := m.srv.Connections()[0].Abort(ctx); err != nil {
		t.Fatal(err)
	}
	eventually(t, "client to observe the abort", func() bool {
		_, err := c.Identify(ctx)
		return errors.Is(err, mms.ErrClosed)
	})
	eventually(t, "server to drop the connection", func() bool { return len(m.srv.Connections()) == 0 })
}

// ---------------------------------------------------------------------------
// Unconfirmed services
// ---------------------------------------------------------------------------

func TestServer_Broadcast(t *testing.T) {
	m := startModel(t)
	ctx := timeout(t)
	var chans []chan *mms.InformationReportIndication
	for range 3 {
		ch := make(chan *mms.InformationReportIndication, 1)
		dial(t, m.addr).OnInformationReport(func(r *mms.InformationReportIndication) { ch <- r })
		chans = append(chans, ch)
	}
	eventually(t, "three connections", func() bool { return len(m.srv.Connections()) == 3 })
	if err := m.srv.Broadcast(ctx, &mms.InformationReportRequest{
		Variables: []mms.ObjectName{domName("NewMmsString")},
		Values:    []*mms.Value{mms.NewMmsString("to all")},
	}); err != nil {
		t.Fatal(err)
	}
	for i, ch := range chans {
		select {
		case r := <-ch:
			mustEqual(t, "report", r.Values[0], mms.NewMmsString("to all"))
		case <-ctx.Done():
			t.Fatalf("client %d received no report", i)
		}
	}
}

func TestServerConn_SendInformationReport(t *testing.T) {
	m := startModel(t)
	ctx := timeout(t)
	got := make(chan *mms.InformationReportIndication, 2)
	target := dial(t, m.addr)
	target.OnInformationReport(func(r *mms.InformationReportIndication) { got <- r })
	bystander := dial(t, m.addr)
	bystander.OnInformationReport(func(r *mms.InformationReportIndication) { got <- r })

	var sc *mms.ServerConn
	m.srv.HandleIdentify(func(ctx context.Context, _ mms.IdentifyRequest) (*mms.ServerIdentity, error) {
		sc = mms.ServerConnFromContext(ctx)
		return &mms.ServerIdentity{}, nil
	})
	if _, err := target.Identify(ctx); err != nil {
		t.Fatal(err)
	}
	vals := []*mms.Value{record(1, true, "r", 1, 2, 3), mms.NewUTCTime(t0)}
	if err := sc.SendInformationReport(ctx, &mms.InformationReportRequest{
		Variables: []mms.ObjectName{domName("NewStructure"), domName("NewUTCTime")},
		Values:    vals,
	}); err != nil {
		t.Fatal(err)
	}
	r := <-got
	for i := range vals {
		mustEqual(t, "report value", r.Values[i], vals[i])
	}
	select {
	case extra := <-got:
		t.Errorf("report delivered to another association: %+v", extra)
	case <-time.After(50 * time.Millisecond):
	}
	if err := sc.SendInformationReport(ctx, &mms.InformationReportRequest{Variables: []mms.ObjectName{domName("x")}}); err == nil {
		t.Error("mismatched variables/values: expected error")
	}
}

// The client has no UnsolicitedStatus callback; the PDU must arrive and
// leave the association usable.
func TestServerConn_SendUnsolicitedStatus(t *testing.T) {
	m := startModel(t)
	ctx := timeout(t)
	rec := &rawRecorder{}
	c := dial(t, m.addr, mms.DialOptions{RawHook: rec.hook})
	eventually(t, "server connection", func() bool { return len(m.srv.Connections()) == 1 })
	before := rec.received()
	if err := m.srv.Connections()[0].SendUnsolicitedStatus(ctx, mms.ServerStatus{Logical: mms.VMDLogicalStatusLimited, Physical: mms.VMDPhysicalStatusInoperable}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "unsolicited status frame", func() bool { return rec.received() > before })
	rec.mu.Lock()
	frame := rec.recv[before]
	rec.mu.Unlock()
	// UnconfirmedPDU [3] { unsolicitedStatus [1] { logical 2, physical 2 } }
	if !bytes.Contains(frame, []byte{0xa3, 0x08, 0xa1, 0x06, 0x80, 0x01, 0x02, 0x81, 0x01, 0x02}) {
		t.Errorf("frame % x does not carry the unsolicited status", frame)
	}
	if _, err := c.Identify(ctx); err != nil {
		t.Errorf("association unusable after UnsolicitedStatus: %v", err)
	}
}

// ---------------------------------------------------------------------------
// PDU size enforcement
// ---------------------------------------------------------------------------

// A single value larger than the negotiated PDU is answered with a
// resource ServiceError instead of an oversize PDU.
func TestServer_ResponseTooLarge(t *testing.T) {
	m := startModel(t)
	big := mms.NewOctetString(bytes.Repeat([]byte{0xaa}, 4000))
	m.variable(t, domName("big"), mms.TypeSpec{Type: mms.ValueTypeOctetString, Size: 4000}, big, false)
	c := dial(t, m.addr, mms.DialOptions{MMS: mms.MMSOptions{MaxPDUSize: 1024}})
	ctx := timeout(t)
	_, err := c.Read(ctx, mms.ReadRequest{DomainID: dom, ItemID: "big"})
	if se := serviceError(t, err); se.Class != mms.ErrorClassResource {
		t.Errorf("oversize read = %v, want resource class", se)
	}
	if _, err := c.Identify(ctx); err != nil {
		t.Errorf("association unusable after oversize response: %v", err)
	}
	full := dial(t, m.addr)
	got, err := full.Read(ctx, mms.ReadRequest{DomainID: dom, ItemID: "big"})
	if err != nil {
		t.Fatal(err)
	}
	mustEqual(t, "big", got.Value, big)
}
