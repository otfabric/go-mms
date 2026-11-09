// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/isostack"
	"github.com/otfabric/go-mms/internal/presentation"
	"github.com/otfabric/go-mms/internal/session"
)

// capture records every ISO upper-layer payload a client sends and
// receives, as a passive tap on the wire would see them.
type capture struct {
	mu    sync.Mutex
	units []capturedUnit
}

type capturedUnit struct {
	dir  string
	data []byte
}

func (c *capture) hook(dir string, raw []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.units = append(c.units, capturedUnit{dir, append([]byte(nil), raw...)})
}

func (c *capture) all() []capturedUnit {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]capturedUnit(nil), c.units...)
}

// TestDecodeTSDU_Association decodes every unit of a real association:
// associate, Identify, Read (with alternate access), Write, GetNameList,
// GetVariableAccessAttributes, named variable lists, a refused service,
// an InformationReport and the release.
func TestDecodeTSDU_Association(t *testing.T) {
	srv, _ := allowedServicesServer(t)
	if err := srv.RegisterVariable(Variable{
		Name:     ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "s"},
		TypeSpec: TypeSpec{Type: ValueTypeStructure, Elements: []TypeSpecElement{{Name: "a", Type: TypeSpec{Type: ValueTypeBoolean}}}},
		Read:     func(context.Context) (*Value, error) { return NewStructure([]*Value{NewBoolean(true)}), nil },
	}); err != nil {
		t.Fatal(err)
	}
	tap := &capture{}
	c, _ := serveLoopback(t, srv, DialOptions{RawHook: tap.hook})
	ctx := context.Background()
	v := ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "v"}
	s := ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "s"}
	list := ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: "nvl"}
	reports := make(chan struct{}, 1)
	c.OnInformationReport(func(*InformationReportIndication) { reports <- struct{}{} })

	mustNot := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := c.Identify(ctx)
	mustNot(err)
	_, err = c.ReadObject(ctx, v)
	mustNot(err)
	_, err = c.ReadComponent(ctx, s, "a")
	mustNot(err)
	_, err = c.WriteObject(ctx, v, NewInteger(9))
	mustNot(err)
	_, err = c.GetNameList(ctx, NameListRequest{ObjectClass: ObjectClassNamedVariable, Scope: ObjectScopeDomain, DomainID: "d"})
	mustNot(err)
	_, err = c.GetVariableAccessAttributes(ctx, s)
	mustNot(err)
	mustNot(c.DefineNamedVariableList(ctx, DefineNamedVariableListRequest{ListName: list, Variables: []VariableSpec{{Name: v}}}))
	_, err = c.GetNamedVariableListAttributes(ctx, list)
	mustNot(err)
	if err := c.FileDelete(ctx, "x"); err == nil {
		t.Fatal("FileDelete succeeded")
	}
	mustNot(srv.Broadcast(ctx, &InformationReportRequest{Variables: []ObjectName{v}, Values: []*Value{NewInteger(3)}}))
	select {
	case <-reports:
	case <-time.After(5 * time.Second):
		t.Fatal("no information report")
	}
	mustNot(c.Close(ctx))

	seen := map[string]*CapturedPDU{}
	var sessions, acses []string
	for _, u := range tap.all() {
		got, err := DecodeTSDU(u.data)
		if err != nil {
			t.Fatalf("%s %x: %v", u.dir, u.data, err)
		}
		sessions = append(sessions, got.Session)
		if got.ACSE != nil {
			acses = append(acses, got.ACSE.Kind)
		}
		if got.PDU != nil {
			key := got.PDU.Kind + " " + got.PDU.Service
			if got.PDU.Kind == "confirmed-request" && got.PDU.Service == "Read" && got.PDU.Variables[0].Name.ItemID == "s" {
				key += " component"
			}
			seen[key] = got.PDU
			if got.PDU.DecodeError != "" {
				t.Errorf("%s: %s", key, got.PDU.DecodeError)
			}
		}
	}
	if sessions[0] != "connect" || sessions[1] != "accept" {
		t.Errorf("sessions = %v", sessions)
	}
	if len(acses) != 2 || acses[0] != "AARQ" || acses[1] != "AARE" {
		t.Errorf("ACSE = %v", acses)
	}
	if p := seen["initiate-request "]; p == nil || p.Initiate == nil || p.Initiate.Version != 1 {
		t.Errorf("initiate request = %+v", p)
	}
	if p := seen["initiate-response "]; p == nil || p.Initiate == nil {
		t.Errorf("initiate response = %+v", p)
	}
	if p := seen["confirmed-request Read"]; p == nil || p.Variables[0].Name != v || !p.HasInvokeID {
		t.Errorf("read request = %+v", p)
	}
	if p := seen["confirmed-request Read component"]; p == nil || len(p.Variables[0].Access) != 1 || p.Variables[0].Access[0].Component != "a" {
		t.Errorf("read component request = %+v", p)
	}
	if p := seen["confirmed-response Read"]; p == nil || len(p.Values) != 1 {
		t.Errorf("read response = %+v", p)
	}
	if p := seen["confirmed-request Write"]; p == nil || len(p.Values) != 1 {
		t.Errorf("write request = %+v", p)
	} else if n, _ := p.Values[0].Int64(); n != 9 {
		t.Errorf("written value = %v", p.Values[0])
	}
	if p := seen["confirmed-response Write"]; p == nil || len(p.WriteResults) != 1 || p.WriteResults[0] != nil {
		t.Errorf("write response = %+v", p)
	}
	if p := seen["confirmed-request GetNameList"]; p == nil || p.NameListRequest == nil || p.NameListRequest.Domain != "d" {
		t.Errorf("name list request = %+v", p)
	}
	if p := seen["confirmed-response GetNameList"]; p == nil || len(p.NameList) == 0 {
		t.Errorf("name list response = %+v", p)
	}
	if p := seen["confirmed-request Identify"]; p == nil {
		t.Error("no identify request")
	}
	if p := seen["confirmed-response Identify"]; p == nil || p.Identify == nil {
		t.Errorf("identify response = %+v", p)
	}
	if p := seen["confirmed-request GetVariableAccessAttributes"]; p == nil || p.VariableName == nil || *p.VariableName != s {
		t.Errorf("GetVAA request = %+v", p)
	}
	if p := seen["confirmed-response GetVariableAccessAttributes"]; p == nil || p.TypeSpec == nil || p.TypeSpec.Type != ValueTypeStructure {
		t.Errorf("GetVAA response = %+v", p)
	}
	if p := seen["confirmed-request GetNamedVariableListAttributes"]; p == nil || p.ListName == nil || *p.ListName != list {
		t.Errorf("GetNVLAttributes request = %+v", p)
	}
	if p := seen["confirmed-response GetNamedVariableListAttributes"]; p == nil || len(p.NamedListMembers) != 1 {
		t.Errorf("GetNVLAttributes response = %+v", p)
	}
	if p := seen["confirmed-request DefineNamedVariableList"]; p == nil || len(p.Raw) == 0 {
		t.Errorf("an unmodelled service keeps its content: %+v", p)
	}
	if p := seen["confirmed-error "]; p == nil || p.Error == nil {
		t.Errorf("confirmed error = %+v", p)
	}
	if p := seen["unconfirmed InformationReport"]; p == nil || len(p.Variables) != 1 || len(p.Values) != 1 {
		t.Errorf("information report = %+v", p)
	}
	if seen["conclude-request "] == nil || seen["conclude-response "] == nil {
		t.Errorf("conclude = %v", seen)
	}
}

// TestDecodeTSDU_ReleaseAndAbort decodes the ACSE release and abort the
// client sends.
func TestDecodeTSDU_ReleaseAndAbort(t *testing.T) {
	must := func(b []byte, err error) []byte {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	for _, tc := range []struct {
		data          []byte
		session, acse string
	}{
		{isostack.EncodeReleaseRequest(), "finish", "RLRQ"},
		{isostack.EncodeAbort(), "abort", "ABRT"},
		{must(session.EncodeDisconnect(presentation.EncodeUserData(presentation.ContextIDACSE, acse.EncodeRLRE()))), "disconnect", "RLRE"},
	} {
		got, err := DecodeTSDU(tc.data)
		if err != nil || got.Session != tc.session || got.ACSE == nil || got.ACSE.Kind != tc.acse || got.PDU != nil {
			t.Errorf("%s = %+v, %v", tc.acse, got, err)
		}
	}
	// An AARQ with a password says so and keeps the password out.
	aarq, err := acse.EncodeAARQ(acse.AARQParams{Password: []byte("secret")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	spdu := must(session.EncodeConnect(session.ConnectParams{}, presentation.EncodeCP(presentation.ConnectParams{}, aarq)))
	got, err := DecodeTSDU(spdu)
	if err != nil || got.ACSE == nil || got.ACSE.Authentication != "password" || got.Presentation != "CP" {
		t.Errorf("AARQ with password = %+v, %v", got, err)
	}
	// An AARE that rejects.
	are := must(session.EncodeAccept(session.ConnectParams{}, presentation.EncodeCPA(nil, acse.EncodeAARE(1, nil))))
	if got, err := DecodeTSDU(are); err != nil || got.ACSE == nil || got.ACSE.Result == nil || *got.ACSE.Result != 1 {
		t.Errorf("rejecting AARE = %+v, %v", got, err)
	}
}

func TestDecodeTSDU_Errors(t *testing.T) {
	if _, err := DecodeTSDU([]byte{0x03, 0x00, 0x00, 0x16}); !errors.Is(err, ErrNotMMS) {
		t.Errorf("a TPKT header = %v", err)
	}
	// A data SPDU with a presentation PPDU that does not parse.
	got, err := DecodeTSDU([]byte{0x01, 0x00, 0x01, 0x00, 0xff, 0x01, 0x00})
	if err == nil || got == nil || got.Session != "data" {
		t.Errorf("bad presentation = %+v, %v", got, err)
	}
	// An abort SPDU without user data.
	if got, err := DecodeTSDU([]byte{0x19, 0x00}); err != nil || got.Session != "abort" {
		t.Errorf("abort = %+v, %v", got, err)
	}
	if _, err := DecodePDU([]byte{0xbf, 0x00}); err == nil {
		t.Error("an unknown PDU tag decoded")
	}
}

// FuzzDecodeTSDU: a captured unit is untrusted input; decoding it never
// panics, and an MMS PDU it returns is consistent.
func FuzzDecodeTSDU(f *testing.F) {
	f.Add(isostack.EncodeReleaseRequest())
	f.Add(isostack.EncodeAbort())
	f.Add([]byte{0x01, 0x00, 0x01, 0x00, 0x61, 0x0a, 0x30, 0x08, 0x02, 0x01, 0x03, 0xa0, 0x03, 0xa3, 0x01, 0x00})
	f.Add([]byte{0x0d, 0x00})
	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := DecodeTSDU(data)
		if err != nil && got == nil && !errors.Is(err, ErrNotMMS) {
			t.Fatalf("an error without a unit that is not ErrNotMMS: %v", err)
		}
		if got != nil && got.PDU != nil && got.PDU.Kind == "" {
			t.Fatalf("a PDU without a kind: %+v", got.PDU)
		}
	})
}
