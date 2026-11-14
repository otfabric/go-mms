// SPDX-License-Identifier: MIT

package isostack

import (
	"bytes"
	"encoding/asn1"
	"errors"
	"testing"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/session"
)

// Sample MMS payloads used to build the seed corpora.
var fuzzMMSPayloads = [][]byte{
	nil,
	{0x8b, 0x00},                   // ConcludeRequest
	{0xa8, 0x03, 0x80, 0x01, 0x05}, // truncated InitiateRequest-like PDU
	{0xa0, 0x08, 0x02, 0x01, 0x01, 0xa4, 0x03, 0xa1, 0x01, 0x00}, // ConfirmedRequest
	bytes.Repeat([]byte{0x55}, 300),                              // long-form lengths
}

// malformedSeeds are shared truncated / garbage inputs.
var malformedSeeds = [][]byte{
	{},
	{0x00},
	{0xff},
	{0x0d, 0xff},
	{0x01, 0x00, 0x01, 0x00},
	{0x0e, 0x02, 0x00},
	{0x09, 0x00},
	{0x19, 0x00},
}

func addMalformed(f *testing.F, valid [][]byte) {
	for _, s := range malformedSeeds {
		f.Add(s)
	}
	// Truncations and a single-byte corruption of each valid encoding.
	for _, v := range valid {
		if len(v) > 1 {
			f.Add(v[:len(v)/2])
			f.Add(v[:len(v)-1])
			c := bytes.Clone(v)
			c[len(c)/2] ^= 0xff
			f.Add(c)
		}
	}
}

func FuzzDecodeAssociateRequest(f *testing.F) {
	var valid [][]byte
	for _, p := range fuzzMMSPayloads {
		for _, params := range []Params{
			{},
			{
				CallingSessionSelector:      []byte{0, 1},
				CalledSessionSelector:       []byte{0, 1},
				CallingPresentationSelector: []byte{0, 0, 0, 1},
				CalledPresentationSelector:  []byte{0, 0, 0, 1},
				ACSE: acse.AARQParams{
					CalledAPTitle:      asn1.ObjectIdentifier{1, 1, 999, 1},
					CalledAEQualifier:  12,
					CallingAPTitle:     asn1.ObjectIdentifier{1, 1, 999, 2},
					CallingAEQualifier: 13,
					Password:           []byte("secret"),
				},
			},
		} {
			b, err := EncodeAssociateRequest(params, p)
			if err != nil {
				f.Fatalf("EncodeAssociateRequest: %v", err)
			}
			valid = append(valid, b)
		}
	}
	valid = append(valid, encodeAccept(f, []byte{0xa9, 0x00}), EncodeDataRequest([]byte{0x8b, 0x00}))
	for _, v := range valid {
		f.Add(v)
	}
	addMalformed(f, valid)
	// Context lists at and above the bound of the presentation layer, and
	// one whose answer would not fit a REFUSE.
	for _, n := range []int{proposedContextLimit, proposedContextLimit + 1, 8400} {
		f.Add(connectWithContexts(f, repeatDef(unknownContextDef, n)))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		req, err := DecodeAssociateRequest(data)
		if re, ok := errors.AsType[*RefusedError](err); ok {
			// A refusal is always a REFUSE that can be sent.
			if spdu, perr := session.Parse(re.Response); perr != nil || spdu.Type != session.SpduRefuse {
				t.Fatalf("refusal %x is not a REFUSE: %v", re.Response, perr)
			}
		}
		if err != nil {
			return
		}
		// Re-encode the decoded MMS payload (and password, if any) and
		// decode it again: the payload must survive unchanged.
		var params Params
		if req.Auth.Mechanism == acse.AuthPassword && len(req.Auth.Password) > 0 {
			params.ACSE.Password = req.Auth.Password
		}
		enc, err := EncodeAssociateRequest(params, req.MmsPayload)
		if err != nil {
			t.Fatalf("re-encode: %v", err)
		}
		again, err := DecodeAssociateRequest(enc)
		if err != nil {
			t.Fatalf("decode(encode(x)) failed: %v\noriginal %x\nre-encoded %x", err, data, enc)
		}
		if !bytes.Equal(again.MmsPayload, req.MmsPayload) {
			t.Fatalf("MMS payload mismatch: %x != %x", again.MmsPayload, req.MmsPayload)
		}
		if params.ACSE.Password != nil && !bytes.Equal(again.Auth.Password, params.ACSE.Password) {
			t.Fatalf("password mismatch: %q != %q", again.Auth.Password, params.ACSE.Password)
		}
	})
}

func FuzzDecodeAssociateResponse(f *testing.F) {
	var valid [][]byte
	for _, p := range fuzzMMSPayloads {
		valid = append(valid, encodeAccept(f, p))
	}
	valid = append(valid, encodeReject(f))
	if req, err := EncodeAssociateRequest(Params{}, []byte{0xa8, 0x00}); err == nil {
		valid = append(valid, req)
	}
	for _, v := range valid {
		f.Add(v)
	}
	addMalformed(f, valid)

	f.Fuzz(func(t *testing.T, data []byte) {
		res, err := DecodeAssociateResponse(data)
		if err != nil || res.ACSEResult != acse.ResultAccepted {
			return
		}
		enc := encodeAccept(t, res.MmsPayload)
		again, err := DecodeAssociateResponse(enc)
		if err != nil {
			t.Fatalf("decode(encode(x)) failed: %v\noriginal %x\nre-encoded %x", err, data, enc)
		}
		if again.ACSEResult != acse.ResultAccepted || !bytes.Equal(again.MmsPayload, res.MmsPayload) {
			t.Fatalf("round-trip mismatch: %+v != %+v", again, res)
		}
	})
}

func FuzzDecodeDataResponse(f *testing.F) {
	var valid [][]byte
	for _, p := range fuzzMMSPayloads {
		valid = append(valid, EncodeDataRequest(p))
	}
	valid = append(valid, EncodeReleaseRequest(), EncodeReleaseResponse(), EncodeAbort())
	for _, v := range valid {
		f.Add(v)
	}
	addMalformed(f, valid)

	f.Fuzz(func(t *testing.T, data []byte) {
		payload, err := DecodeDataResponse(data)
		if err != nil {
			return
		}
		enc := EncodeDataRequest(payload)
		again, err := DecodeDataResponse(enc)
		if err != nil {
			t.Fatalf("decode(encode(x)) failed: %v\noriginal %x\nre-encoded %x", err, data, enc)
		}
		if !bytes.Equal(again, payload) {
			t.Fatalf("payload mismatch: %x != %x", again, payload)
		}
	})
}

func FuzzDecodeReleaseRequest(f *testing.F) {
	valid := [][]byte{
		EncodeReleaseRequest(),
		EncodeReleaseResponse(),
		EncodeAbort(),
		EncodeDataRequest([]byte{0x8b, 0x00}),
	}
	for _, v := range valid {
		f.Add(v)
	}
	addMalformed(f, valid)

	f.Fuzz(func(t *testing.T, data []byte) {
		if err := DecodeReleaseRequest(data); err != nil {
			return
		}
		// Whatever decodes as a release request must not also be
		// accepted as session DATA.
		if _, err := DecodeDataResponse(data); err == nil {
			t.Fatalf("input accepted both as FINISH and as DATA: %x", data)
		}
	})
}

// FuzzDecodeReleaseResponse covers the client's release-response and
// abort decoding (ISO 9506-2 Annex A.1.2, ISO 8823-1 ARU/ARP).
func FuzzDecodeReleaseResponse(f *testing.F) {
	valid := [][]byte{
		EncodeReleaseResponse(),
		EncodeReleaseResponseOn(Contexts{ACSE: 7, MMS: 3}),
		EncodeAbort(),
		EncodeAbortOn(Contexts{ACSE: 7, MMS: 3}),
		EncodeReleaseRequest(),
		mustEncode(f)(session.EncodeRefuse(session.ReasonUserCongestion, nil)),
		encodeReject(f),
	}
	for _, v := range valid {
		f.Add(v)
	}
	addMalformed(f, valid)

	f.Fuzz(func(t *testing.T, data []byte) {
		typ, err := DecodeReleaseResponse(data)
		if err == nil && typ != session.SpduDisconnect {
			t.Fatalf("release response of type %s accepted", typ)
		}
		if sp, perr := session.Parse(data); perr == nil {
			_ = DescribeAbort(sp.UserData)
		}
		_, _ = DecodeAssociateResponse(data)
	})
}
