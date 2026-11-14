// SPDX-License-Identifier: MIT

package isostack

import (
	"errors"
	"testing"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/presentation"
	"github.com/otfabric/go-mms/internal/session"
)

// proposedContextLimit is the bound of internal/presentation on the
// presentation contexts of one CP.
const proposedContextLimit = 16

// Context definitions of the CPs built below (ISO 8823-1).
var (
	acseContextDef = berutil.EncodeTLV(0x30, append(append(berutil.EncodeTLV(0x02, []byte{1}),
		berutil.EncodeTLV(0x06, []byte{0x52, 0x01, 0x00, 0x01})...),
		berutil.EncodeTLV(0x30, berutil.EncodeTLV(0x06, []byte{0x51, 0x01}))...))
	mmsContextDef = berutil.EncodeTLV(0x30, append(append(berutil.EncodeTLV(0x02, []byte{3}),
		berutil.EncodeTLV(0x06, []byte{0x28, 0xca, 0x22, 0x02, 0x01})...),
		berutil.EncodeTLV(0x30, berutil.EncodeTLV(0x06, []byte{0x51, 0x01}))...))
	// unknownContextDef is the smallest definition: identifier 5 and an
	// empty abstract syntax, which the responder rejects.
	unknownContextDef = berutil.EncodeTLV(0x30, append(berutil.EncodeTLV(0x02, []byte{5}), 0x06, 0x00))
)

// sessionPI appends one session parameter with the one- or three-octet
// length form (ISO 8327-1 8.2.5).
func sessionPI(buf []byte, code byte, v []byte) []byte {
	buf = append(buf, code)
	if len(v) < 255 {
		buf = append(buf, byte(len(v)))
	} else {
		buf = append(buf, 0xff, byte(len(v)>>8), byte(len(v)))
	}
	return append(buf, v...)
}

// connectWithContexts builds a CONNECT whose CP proposes the given
// context definitions and carries an AARQ with an MMS payload. The
// CONNECT is built by hand: its user data may exceed what
// session.EncodeConnect sends.
func connectWithContexts(tb testing.TB, defs [][]byte) []byte {
	tb.Helper()
	aarq, err := acse.EncodeAARQ(acse.AARQParams{}, []byte{0xa8, 0x00})
	if err != nil {
		tb.Fatal(err)
	}
	var list []byte
	for _, d := range defs {
		list = append(list, d...)
	}
	pdv := berutil.EncodeTLV(0x30, append(berutil.EncodeTLV(0x02, []byte{1}), berutil.EncodeTLV(0xa0, aarq)...))
	normal := append(berutil.EncodeTLV(0xa4, list), berutil.EncodeTLV(0x61, pdv)...)
	cp := berutil.EncodeTLV(0x31, append(berutil.EncodeTLV(0xa0, []byte{0x80, 0x01, 0x01}), berutil.EncodeTLV(0xa2, normal)...))

	var params []byte
	params = sessionPI(params, 0x05, []byte{0x13, 0x01, 0x00, 0x16, 0x01, session.Version2})
	params = sessionPI(params, 0x14, []byte{0x00, byte(session.FUDuplex)})
	params = sessionPI(params, 0xc2, cp)
	if len(params) > 0xffff {
		tb.Fatalf("CONNECT parameters %d octets", len(params))
	}
	return sessionPI(nil, session.SIConnect, params)
}

func repeatDef(def []byte, n int) [][]byte {
	defs := make([][]byte, n)
	for i := range defs {
		defs[i] = def
	}
	return defs
}

// A CP proposes at most proposedContextLimit presentation contexts. Up to
// the limit the refusal answers every one of them; above it the CP is
// refused without a result list. The refusal always fits a REFUSE.
func TestDecodeAssociateRequestContextCount(t *testing.T) {
	for _, tc := range []struct {
		name        string
		defs        [][]byte
		accepted    bool
		wantResults int // -1: CPR without a result list
	}{
		{"unknown, limit-1", repeatDef(unknownContextDef, proposedContextLimit-1), false, proposedContextLimit - 1},
		{"unknown, at the limit", repeatDef(unknownContextDef, proposedContextLimit), false, proposedContextLimit},
		{"unknown, limit+1", repeatDef(unknownContextDef, proposedContextLimit+1), false, -1},
		// One result per proposed context would not fit a REFUSE.
		{"unknown, 8400", repeatDef(unknownContextDef, 8400), false, -1},
		{"ACSE and MMS at the limit", append([][]byte{acseContextDef, mmsContextDef}, repeatDef(unknownContextDef, proposedContextLimit-2)...), true, 0},
		{"ACSE and MMS, limit+1", append([][]byte{acseContextDef, mmsContextDef}, repeatDef(unknownContextDef, proposedContextLimit-1)...), false, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := DecodeAssociateRequest(connectWithContexts(t, tc.defs))
			if tc.accepted {
				if err != nil || req == nil || req.Contexts.ACSE != 1 || req.Contexts.MMS != 3 {
					t.Fatalf("DecodeAssociateRequest = %+v, %v; want accepted on contexts 1 and 3", req, err)
				}
				return
			}
			re, ok := errors.AsType[*RefusedError](err)
			if !ok {
				t.Fatalf("err = %v, want *RefusedError", err)
			}
			spdu, err := session.Parse(re.Response)
			if err != nil || spdu.Type != session.SpduRefuse {
				t.Fatalf("response %x: %v", re.Response, err)
			}
			ppdu, err := presentation.Parse(spdu.UserData)
			if err != nil || ppdu.Kind != presentation.PpduCPR {
				t.Fatalf("REFUSE user data: %v", err)
			}
			if tc.wantResults < 0 {
				if ppdu.HasResults {
					t.Fatalf("CPR carries %d results, want none", len(ppdu.Results))
				}
				return
			}
			if !ppdu.HasResults || len(ppdu.Results) != tc.wantResults {
				t.Fatalf("CPR carries %d results, want %d", len(ppdu.Results), tc.wantResults)
			}
		})
	}
}

// The refusal of a presentation CP whose result list does not fit a
// REFUSE is sent without the result list.
func TestRefusePresentationWithoutRoomForResults(t *testing.T) {
	results := make([]presentation.ContextResult, 9000)
	for i := range results {
		results[i] = presentation.ContextResult{Result: presentation.ResultProviderRejection, ProviderReason: presentation.ContextReasonAbstractSyntaxNotSupported}
	}
	re, ok := errors.AsType[*RefusedError](refusePresentation(results, "test"))
	if !ok {
		t.Fatal("not a *RefusedError")
	}
	spdu, err := session.Parse(re.Response)
	if err != nil {
		t.Fatal(err)
	}
	ppdu, err := presentation.Parse(spdu.UserData)
	if err != nil || ppdu.Kind != presentation.PpduCPR || ppdu.HasResults || ppdu.ProviderReason != presentation.ProviderReasonNotSpecified {
		t.Fatalf("CPR %+v, %v", ppdu, err)
	}
}
