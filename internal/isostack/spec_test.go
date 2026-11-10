// SPDX-License-Identifier: MIT

package isostack

import (
	"bytes"
	"errors"
	"testing"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/presentation"
	"github.com/otfabric/go-mms/internal/session"
)

var (
	oidACSE = []byte{0x52, 0x01, 0x00, 0x01}
	oidMMS  = []byte{0x28, 0xca, 0x22, 0x02, 0x01}
	oidBER  = []byte{0x51, 0x01}
)

func ctxDef(id int, abs []byte) []byte {
	c := berutil.EncodeTLV(0x02, berutil.EncodeInt(id))
	c = berutil.AppendTLV(c, 0x06, abs)
	c = berutil.AppendTLV(c, 0x30, berutil.EncodeTLV(0x06, oidBER))
	return berutil.EncodeTLV(0x30, c)
}

// connectWith builds a CONNECT whose CP proposes defs and carries aarq
// on context acseID.
func connectWith(tb testing.TB, sp session.ConnectParams, acseID int, aarq []byte, defs ...[]byte) []byte {
	tb.Helper()
	var list []byte
	for _, d := range defs {
		list = append(list, d...)
	}
	normal := berutil.EncodeTLV(0xa4, list)
	normal = append(normal, presentation.EncodeUserData(acseID, aarq)...)
	inner := berutil.EncodeTLV(0xa0, []byte{0x80, 0x01, 0x01})
	inner = berutil.AppendTLV(inner, 0xa2, normal)
	return mustEncode(tb)(session.EncodeConnect(sp, berutil.EncodeTLV(0x31, inner)))
}

func defaultAARQ(tb testing.TB) []byte {
	tb.Helper()
	b, err := acse.EncodeAARQ(acse.AARQParams{}, []byte{0xa8, 0x00})
	if err != nil {
		tb.Fatal(err)
	}
	return b
}

// refusal decodes a *RefusedError response down to the AARE, if any.
func refusal(t *testing.T, err error) (*session.ParsedSpdu, *presentation.ParsedPPDU, *acse.ParsedAARE) {
	t.Helper()
	var re *RefusedError
	if !errors.As(err, &re) {
		t.Fatalf("error %v is not a *RefusedError", err)
	}
	sp, perr := session.Parse(re.Response)
	if perr != nil || sp.Type != session.SpduRefuse {
		t.Fatalf("response %x: %v", re.Response, perr)
	}
	if len(sp.UserData) == 0 {
		return sp, nil, nil
	}
	pp, perr := presentation.Parse(sp.UserData)
	if perr != nil || pp.Kind != presentation.PpduCPR {
		t.Fatalf("CPR %x: %v", sp.UserData, perr)
	}
	if len(pp.UserData) == 0 {
		return sp, pp, nil
	}
	ap, perr := acse.Parse(pp.UserData)
	if perr != nil || ap.Type != acse.ApduAARE {
		t.Fatalf("AARE %x: %v", pp.UserData, perr)
	}
	return sp, pp, ap.AARE
}

// ISO 8823-1, ISO 9506-2 Annex A.5 (MMS-STK-07): contexts {1: ACSE,
// 5: MMS} are accepted, the AARE names context 5 and data on context 5
// is served.
func TestSpec_8823_ServerUsesProposedContextIDs(t *testing.T) {
	cn := connectWith(t, session.ConnectParams{}, 1, defaultAARQ(t), ctxDef(1, oidACSE), ctxDef(5, oidMMS))
	req, err := DecodeAssociateRequest(cn)
	if err != nil {
		t.Fatal(err)
	}
	if req.Contexts != (Contexts{ACSE: 1, MMS: 5}) {
		t.Fatalf("contexts %+v", req.Contexts)
	}
	resp := mustEncode(t)(req.EncodeAssociateResponse(ResponseParams{}, []byte{0xa9, 0x00}))
	sp, _ := session.Parse(resp)
	pp, err := presentation.Parse(sp.UserData)
	if err != nil || len(pp.Results) != 2 || pp.Results[1].Result != 0 {
		t.Fatalf("CPA %+v %v", pp, err)
	}
	if !bytes.Contains(pp.UserData, []byte{0x28, 0x07, 0x02, 0x01, 0x05}) {
		t.Fatalf("AARE user-information not on context 5: %x", pp.UserData)
	}
	payload, err := DecodeData(req.Contexts, EncodeData(req.Contexts, []byte{0xa0, 0x00}))
	if err != nil || !bytes.Equal(payload, []byte{0xa0, 0x00}) {
		t.Fatalf("data on context 5: %x %v", payload, err)
	}
	if _, err := DecodeData(req.Contexts, EncodeDataRequest([]byte{0xa0, 0x00})); err == nil {
		t.Fatal("data on context 3 accepted for an association on context 5")
	}
	if err := DecodeReleaseRequestOn(req.Contexts, EncodeReleaseRequest()); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(EncodeReleaseResponseOn(req.Contexts), EncodeReleaseResponse()) {
		t.Fatal("RLRE on ACSE context 1 differs")
	}

	// ACSE on 7: the RLRE, abort and the AARE use 7.
	cn = connectWith(t, session.ConnectParams{}, 7, defaultAARQ(t), ctxDef(3, oidMMS), ctxDef(7, oidACSE))
	req, err = DecodeAssociateRequest(cn)
	if err != nil || req.Contexts != (Contexts{ACSE: 7, MMS: 3}) {
		t.Fatalf("%+v %v", req, err)
	}
	if !bytes.Contains(EncodeReleaseResponseOn(req.Contexts), []byte{0x02, 0x01, 0x07}) ||
		!bytes.Contains(EncodeAbortOn(req.Contexts), []byte{0x02, 0x01, 0x07}) {
		t.Fatal("ACSE context 7 not used")
	}
}

// ISO 8823-1: a CP without an acceptable MMS context is refused by the
// provider with the per-context result list.
func TestSpec_8823_ServerRefusesWithoutMMSContext(t *testing.T) {
	cn := connectWith(t, session.ConnectParams{}, 1, defaultAARQ(t), ctxDef(1, oidACSE), ctxDef(3, []byte{0x2a, 0x03}))
	_, err := DecodeAssociateRequest(cn)
	sp, pp, aare := refusal(t, err)
	if sp.Reason != session.ReasonUserData || pp.ProviderReason != 0 || len(pp.Results) != 2 || pp.Results[1].Result != presentation.ResultProviderRejection || aare != nil {
		t.Fatalf("%+v %+v", sp, pp)
	}
	// Undecodable CP: CPR without results.
	_, err = DecodeAssociateRequest(mustEncode(t)(session.EncodeConnect(session.ConnectParams{}, []byte{0xff})))
	if _, pp, _ := refusal(t, err); pp.HasResults || pp.ProviderReason != 0 {
		t.Fatalf("%+v", pp)
	}
	// AARQ on the MMS context.
	cn = connectWith(t, session.ConnectParams{}, 3, defaultAARQ(t), ctxDef(1, oidACSE), ctxDef(3, oidMMS))
	_, err = DecodeAssociateRequest(cn)
	if _, _, aare := refusal(t, err); aare.DiagSource != acse.DiagSourceProvider || aare.Diagnostic != acse.DiagProviderNoReasonGiven {
		t.Fatalf("%+v", aare)
	}
}

// ISO 8327-1 (MMS-STK-11): the ACCEPT selects a version the CONNECT
// offered; no common version or no duplex is refused.
func TestSpec_8327_ServerSessionNegotiation(t *testing.T) {
	cn := connectWith(t, session.ConnectParams{Version: session.Version1}, 1, defaultAARQ(t), ctxDef(1, oidACSE), ctxDef(3, oidMMS))
	req, err := DecodeAssociateRequest(cn)
	if err != nil {
		t.Fatal(err)
	}
	sp, _ := session.Parse(mustEncode(t)(req.EncodeAssociateResponse(ResponseParams{}, []byte{0xa9, 0x00})))
	if sp.Version != session.Version1 || sp.Requirements != session.FUDuplex {
		t.Fatalf("AC version %d reqs %04x", sp.Version, sp.Requirements)
	}

	cn = connectWith(t, session.ConnectParams{Version: 0x04}, 1, defaultAARQ(t), ctxDef(1, oidACSE), ctxDef(3, oidMMS))
	_, err = DecodeAssociateRequest(cn)
	if sp, _, _ := refusal(t, err); sp.Reason != session.ReasonVersionNotSupported {
		t.Fatalf("reason %d", sp.Reason)
	}
	cn = connectWith(t, session.ConnectParams{Requirements: 0x0001}, 1, defaultAARQ(t), ctxDef(1, oidACSE), ctxDef(3, oidMMS))
	_, err = DecodeAssociateRequest(cn)
	if sp, _, _ := refusal(t, err); sp.Reason != session.ReasonSPMRestriction {
		t.Fatalf("reason %d", sp.Reason)
	}
}

// ISO 8650-1, ISO 9506-2 Annex A.7 (MMS-STK-12): the AARQ is validated
// and refused with the matching diagnostic.
func TestSpec_8650_ServerAARQValidation(t *testing.T) {
	aarqWith := func(fields ...[]byte) []byte {
		var c []byte
		for _, f := range fields {
			c = append(c, f...)
		}
		return berutil.EncodeTLV(0x60, c)
	}
	appCtx := func(oid []byte) []byte { return berutil.EncodeTLV(0xa1, berutil.EncodeTLV(0x06, oid)) }
	userInfo := berutil.EncodeTLV(0xbe, berutil.EncodeTLV(0x28, append(berutil.EncodeTLV(0x02, []byte{3}), berutil.EncodeTLV(0xa0, []byte{0xa8, 0x00})...)))
	for _, tc := range []struct {
		name       string
		aarq       []byte
		src, value int
		wire       []byte
	}{
		{"wrong application context", aarqWith(appCtx([]byte{0x28, 0xca, 0x22, 0x02, 0x63}), userInfo), acse.DiagSourceUser, acse.DiagUserAppContextNotSupported, []byte{0xa3, 0x05, 0xa1, 0x03, 0x02, 0x01, 0x02}},
		{"no application context", aarqWith(userInfo), acse.DiagSourceUser, acse.DiagUserAppContextNotSupported, nil},
		{"protocol version", aarqWith(berutil.EncodeTLV(0x80, []byte{0x07, 0x00}), appCtx(acse.MMSApplicationContext), userInfo), acse.DiagSourceProvider, acse.DiagProviderNoCommonACSEVersion, nil},
		{"no user information", aarqWith(appCtx(acse.MMSApplicationContext)), acse.DiagSourceUser, acse.DiagUserNoReasonGiven, nil},
		{"not an AARQ", acse.EncodeRLRQ(), acse.DiagSourceProvider, acse.DiagProviderNoReasonGiven, nil},
		{"undecodable", []byte{0x60, 0x05, 0x01}, acse.DiagSourceProvider, acse.DiagProviderNoReasonGiven, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cn := connectWith(t, session.ConnectParams{}, 1, tc.aarq, ctxDef(1, oidACSE), ctxDef(3, oidMMS))
			_, err := DecodeAssociateRequest(cn)
			_, pp, aare := refusal(t, err)
			if aare.Result != acse.ResultRejectedPerm || aare.DiagSource != tc.src || aare.Diagnostic != tc.value {
				t.Fatalf("%+v", aare)
			}
			if tc.wire != nil && !bytes.Contains(pp.UserData, tc.wire) {
				t.Fatalf("AARE %x lacks %x", pp.UserData, tc.wire)
			}
		})
	}
}

// ISO 8327-1 / 8823-1 / 8650-1 (MMS-STK-09): the client decodes a
// REFUSE, its CPR and the rejecting AARE with its Initiate-ErrorPDU.
func TestSpec_ClientDecodesRefusal(t *testing.T) {
	initErr := []byte{0xaa, 0x05, 0xa0, 0x03, 0x87, 0x01, 0x03}
	rf := mustEncode(t)(defaultRequest(t).EncodeReject(Reject{Result: 1, DiagSource: acse.DiagSourceUser, Diagnostic: acse.DiagUserAuthFailure, MmsPayload: initErr}))
	_, err := DecodeAssociateResponse(rf)
	var rj *Rejection
	if !errors.As(err, &rj) {
		t.Fatalf("%v", err)
	}
	if rj.SessionReason != 2 || rj.ProviderReason != -1 || rj.ACSEResult != 1 || rj.DiagSource != 1 || rj.Diagnostic != 13 || !bytes.Equal(rj.MmsPayload, initErr) || rj.Transient() {
		t.Fatalf("%+v", rj)
	}
	if rj.Error() == "" {
		t.Fatal("empty message")
	}

	// Congestion is transient; a bare REFUSE has no CPR.
	_, err = DecodeAssociateResponse(mustEncode(t)(session.EncodeRefuse(session.ReasonUserCongestion, nil)))
	if !errors.As(err, &rj) || !rj.Transient() || rj.ACSEResult != -1 {
		t.Fatalf("%+v", rj)
	}
	// REFUSE with an undecodable CPR still is a refusal.
	_, err = DecodeAssociateResponse(mustEncode(t)(session.EncodeRefuse(session.ReasonUserData, []byte{0xff})))
	if !errors.As(err, &rj) || rj.SessionReason != 2 {
		t.Fatalf("%v", err)
	}
	// CPR by the provider with temporary congestion.
	cpr := presentation.EncodeCPR(nil, nil, presentation.ProviderReasonTemporaryCongestion, 0, nil)
	_, err = DecodeAssociateResponse(mustEncode(t)(session.EncodeRefuse(session.ReasonUserData, cpr)))
	if !errors.As(err, &rj) || !rj.Transient() || rj.ProviderReason != 1 {
		t.Fatalf("%+v", rj)
	}
	// ACCEPT + CPA + AARE rejected-transient (libiec61850-style form).
	ac := mustEncode(t)(session.EncodeAccept(session.ConnectParams{}, presentation.EncodeCPA(nil, acse.EncodeAARE(acse.ResultRejectedTransient, nil))))
	_, err = DecodeAssociateResponse(ac)
	if !errors.As(err, &rj) || rj.ACSEResult != 2 || !rj.Transient() || rj.SessionReason != -1 {
		t.Fatalf("%+v %v", rj, err)
	}
}

// ISO 8327-1, 8823-1, 9506-2 A.7 (MMS-STK-07, -11, -12): the client
// checks what the ACCEPT, CPA and AARE agree to.
func TestSpec_ClientChecksAcceptance(t *testing.T) {
	aare := acse.EncodeAARE(acse.ResultAccepted, []byte{0xa9, 0x00})
	accept := presentation.ContextResult{Result: 0, TransferSyntax: oidBER, ProviderReason: -1}
	reject := presentation.ContextResult{Result: 2, ProviderReason: 1}
	for name, tc := range map[string]struct {
		sp  session.ConnectParams
		cpa []byte
	}{
		"version 1":          {session.ConnectParams{Version: session.Version1}, presentation.EncodeCPA(nil, aare)},
		"half duplex":        {session.ConnectParams{Requirements: 0x0001}, presentation.EncodeCPA(nil, aare)},
		"MMS context reject": {session.ConnectParams{}, presentation.EncodeCPAResults(nil, []presentation.ContextResult{accept, reject}, 1, aare)},
		"one result":         {session.ConnectParams{}, presentation.EncodeCPAResults(nil, []presentation.ContextResult{accept}, 1, aare)},
		"app context": {session.ConnectParams{}, presentation.EncodeCPA(nil, berutil.EncodeTLV(0x61, append(
			berutil.EncodeTLV(0xa1, berutil.EncodeTLV(0x06, []byte{0x28, 0xca, 0x22, 0x02, 0x63})),
			berutil.EncodeTLV(0xa2, berutil.EncodeTLV(0x02, []byte{0}))...)))},
	} {
		ac := mustEncode(t)(session.EncodeAccept(tc.sp, tc.cpa))
		if _, err := DecodeAssociateResponse(ac); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	ok := mustEncode(t)(session.EncodeAccept(session.ConnectParams{}, presentation.EncodeCPA(nil, aare)))
	if r, err := DecodeAssociateResponse(ok); err != nil || r.ResponderAuth {
		t.Fatalf("%+v %v", r, err)
	}
}

// ISO 9506-2 Annex A.1.2 (MMS-STK-02, -13): release decoding requires
// FINISH+RLRQ and DISCONNECT+RLRE.
func TestSpec_9506_ReleaseDecoding(t *testing.T) {
	fnBad := mustEncode(t)(session.EncodeFinish(presentation.EncodeUserData(1, acse.EncodeRLRE())))
	if err := DecodeReleaseRequest(fnBad); !errors.Is(err, ErrNotRelease) {
		t.Fatalf("FINISH with RLRE: %v", err)
	}
	fnMMS := mustEncode(t)(session.EncodeFinish(presentation.EncodeUserData(3, acse.EncodeRLRQ())))
	if err := DecodeReleaseRequest(fnMMS); !errors.Is(err, ErrNotRelease) {
		t.Fatalf("FINISH on context 3: %v", err)
	}
	fnJunk := mustEncode(t)(session.EncodeFinish([]byte{0xff}))
	if err := DecodeReleaseRequest(fnJunk); !errors.Is(err, ErrNotRelease) {
		t.Fatalf("FINISH junk: %v", err)
	}
	fnACSEJunk := mustEncode(t)(session.EncodeFinish(presentation.EncodeUserData(1, []byte{0xff})))
	if err := DecodeReleaseRequest(fnACSEJunk); !errors.Is(err, ErrNotRelease) {
		t.Fatalf("FINISH ACSE junk: %v", err)
	}
	if typ, err := DecodeReleaseResponse(EncodeReleaseResponse()); err != nil || typ != session.SpduDisconnect {
		t.Fatalf("%s %v", typ, err)
	}
	if typ, err := DecodeReleaseResponse(EncodeAbort()); err == nil || typ != session.SpduAbort {
		t.Fatalf("abort as release response: %s %v", typ, err)
	}
	if _, err := DecodeReleaseResponse([]byte{0xff}); err == nil {
		t.Fatal("junk")
	}
	dnBad := mustEncode(t)(session.EncodeDisconnect(presentation.EncodeUserData(1, acse.EncodeRLRQ())))
	if _, err := DecodeReleaseResponse(dnBad); err == nil {
		t.Fatal("DISCONNECT with RLRQ accepted")
	}
}

// ISO 8823-1 ARU-PPDU (MMS-STK-08): the abort is AB { ARU { ABRT user } }.
func TestSpec_8823_AbortIsARU(t *testing.T) {
	sp, err := session.Parse(EncodeAbort())
	if err != nil {
		t.Fatal(err)
	}
	if sp.UserData[0] != 0xa0 {
		t.Fatalf("abort PPDU tag %02x, want ARU 0xa0", sp.UserData[0])
	}
	pp, err := presentation.ParseAbort(sp.UserData)
	if err != nil || pp.ContextID != 1 || !bytes.Equal(pp.UserData, []byte{0x64, 0x03, 0x80, 0x01, 0x00}) {
		t.Fatalf("%+v %v", pp, err)
	}
	for in, want := range map[string]string{
		string(sp.UserData):    "ABRT source acse-service-user",
		"":                     "no user data",
		"\xff":                 "undecodable abort PPDU",
		"\x30\x03\x80\x01\x01": "presentation provider abort, reason 1",
		string(presentation.EncodeARU(1, []byte{0x01, 0x00})): "user abort",
		string(presentation.EncodeARU(1, acse.EncodeABRT(1))): "ABRT source acse-service-provider",
	} {
		if got := DescribeAbort([]byte(in)); got != want {
			t.Errorf("DescribeAbort(%x) = %q, want %q", in, got, want)
		}
	}
	_, err = DecodeData(DefaultContexts, mustEncode(t)(session.EncodeAbort(nil)))
	if !errors.Is(err, ErrPeerReleased) {
		t.Fatal(err)
	}
}

// ISO 8650-1 responder-acse-requirements is echoed for an authenticated
// AARQ (MMS-STK-19).
func TestSpec_8650_ResponderAuthEcho(t *testing.T) {
	b, err := EncodeAssociateRequest(Params{ACSE: acse.AARQParams{Password: []byte("pw")}}, []byte{0xa8, 0x00})
	if err != nil {
		t.Fatal(err)
	}
	req, err := DecodeAssociateRequest(b)
	if err != nil {
		t.Fatal(err)
	}
	resp := mustEncode(t)(req.EncodeAssociateResponse(ResponseParams{RespondingAPTitle: []int{1, 3, 9999, 23}, RespondingAEQualifier: 23}, []byte{0xa9, 0x00}))
	if !bytes.Contains(resp, []byte{0x88, 0x02, 0x07, 0x80}) || !bytes.Contains(resp, []byte{0xa5, 0x03, 0x02, 0x01, 0x17}) {
		t.Fatalf("%x", resp)
	}
	r, err := DecodeAssociateResponse(resp)
	if err != nil || !r.ResponderAuth {
		t.Fatalf("%+v %v", r, err)
	}
}
