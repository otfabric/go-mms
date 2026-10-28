// SPDX-License-Identifier: MIT

package codec

import (
	"bytes"
	"encoding/asn1"
	"reflect"
	"testing"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
)

// maxWrapTagNum is the largest tag number encoding/asn1 decodes.
const maxWrapTagNum = 1<<31 - 1

// envelopeSeeds returns valid confirmed request/response envelope contents
// (the bytes after the outer 0xa0/0xa1 tag) produced by this package's
// encoders, plus a few malformed ones.
func envelopeSeeds(f *testing.F, marshal func(InvokeID, int, bool, []byte) ([]byte, error)) {
	f.Helper()
	cases := []struct {
		id          InvokeID
		tag         int
		constructed bool
		payload     []byte
	}{
		{0, 0, false, nil},
		{1, 1, true, []byte{0xa0, 0x00}},
		{127, 4, true, []byte{0xa1, 0x03, 0x80, 0x01, 0x00}},
		{128, 6, true, []byte{0x30, 0x00}},
		{0xffffffff, 2, false, nil},
		{42, 72, true, []byte{0x80, 0x01, 0x41}},          // file service, long-form tag
		{42, 200, false, bytes.Repeat([]byte{0x11}, 200)}, // long-form length
	}
	for _, c := range cases {
		pduBytes, err := marshal(c.id, c.tag, c.constructed, c.payload)
		if err != nil {
			f.Fatalf("marshal: %v", err)
		}
		_, content, err := UnwrapPdu(pduBytes)
		if err != nil {
			f.Fatalf("UnwrapPdu: %v", err)
		}
		f.Add(content)
		f.Add(content[:len(content)-1])                 // truncated
		f.Add(append(bytes.Clone(content), 0x00, 0x00)) // trailing bytes
	}
	f.Add([]byte{})
	f.Add([]byte{0x02})
	f.Add([]byte{0x02, 0x01})
	f.Add([]byte{0x02, 0x01, 0x01})                   // invoke ID only
	f.Add([]byte{0x05, 0x00, 0xa4, 0x00})             // NULL instead of INTEGER
	f.Add([]byte{0x02, 0x02, 0x00, 0x01, 0xa4, 0x00}) // non-minimal INTEGER
	f.Add([]byte{0x02, 0x01, 0x01, 0xbf, 0x80, 0x00}) // bad long-form tag
	f.Add([]byte{0x02, 0x01, 0x01, 0xa4, 0x80, 0x00, 0x00})
}

// checkEnvelopeRoundTrip re-encodes a decoded envelope with marshal and
// checks that decoding it again yields the same invoke ID and service.
func checkEnvelopeRoundTrip(t *testing.T, data []byte, id InvokeID, svc asn1.RawValue,
	marshal func(InvokeID, int, bool, []byte) ([]byte, error),
	unmarshal func([]byte) (InvokeID, asn1.RawValue, error),
) {
	t.Helper()
	if svc.Tag > maxWrapTagNum {
		return
	}
	pduBytes, err := marshal(id, svc.Tag, svc.IsCompound, svc.Bytes)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	_, content, err := UnwrapPdu(pduBytes)
	if err != nil {
		t.Fatalf("UnwrapPdu(re-marshalled): %v", err)
	}
	id2, svc2, err := unmarshal(content)
	if err != nil {
		t.Fatalf("decode(encode(x)) failed: %v\noriginal %x\nre-encoded %x", err, data, content)
	}
	if id2 != id || svc2.Tag != svc.Tag || svc2.IsCompound != svc.IsCompound ||
		svc2.Class != asn1.ClassContextSpecific || !bytes.Equal(svc2.Bytes, svc.Bytes) {
		t.Fatalf("round-trip mismatch:\n got id=%d tag=%d compound=%v class=%d bytes=%x\nwant id=%d tag=%d compound=%v bytes=%x",
			id2, svc2.Tag, svc2.IsCompound, svc2.Class, svc2.Bytes, id, svc.Tag, svc.IsCompound, svc.Bytes)
	}
}

func FuzzUnmarshalConfirmedRequest(f *testing.F) {
	envelopeSeeds(f, MarshalConfirmedRequest)
	f.Fuzz(func(t *testing.T, data []byte) {
		id, svc, err := UnmarshalConfirmedRequest(data)
		if err != nil {
			return
		}
		checkEnvelopeRoundTrip(t, data, id, svc, MarshalConfirmedRequest, UnmarshalConfirmedRequest)
	})
}

func FuzzUnmarshalConfirmedResponse(f *testing.F) {
	envelopeSeeds(f, MarshalConfirmedResponse)
	f.Fuzz(func(t *testing.T, data []byte) {
		id, svc, err := UnmarshalConfirmedResponse(data)
		if err != nil {
			return
		}
		checkEnvelopeRoundTrip(t, data, id, svc, MarshalConfirmedResponse, UnmarshalConfirmedResponse)
	})
}

func FuzzUnwrapPdu(f *testing.F) {
	req, _ := MarshalConfirmedRequest(7, 4, true, []byte{0xa1, 0x00})
	resp, _ := MarshalConfirmedResponse(7, 4, true, []byte{0xa1, 0x00})
	seeds := [][]byte{
		req,
		resp,
		MarshalConfirmedError(7, 3, 0),
		MarshalRejectPDU(7, 1, 0),
		MarshalConcludeRequest(),
		MarshalConcludeResponse(),
		MarshalCancelError(7, 10, 1),
		MarshalCancelResponse(7),
		{},
		{0xa0},
		{0xa0, 0x81},
		{0xa0, 0x81, 0x01, 0x00}, // non-minimal length
		{0xa0, 0x80, 0x00, 0x00}, // indefinite length
		{0x8b, 0x00, 0x00},       // trailing byte
		{0xbf, 0x28, 0x00},       // long-form tag 40
		{0xbf, 0x80, 0x80, 0x00}, // malformed long-form tag
		{0x30, 0x03, 0x02, 0x01, 0x05},
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if tag, err := PduType(data); err == nil && tag != data[0] {
			t.Fatalf("PduType = 0x%02x, want first byte 0x%02x", tag, data[0])
		}
		tag, content, err := UnwrapPdu(data)
		if err != nil {
			return
		}
		if data[0]&0x1f == 0x1f {
			// UnwrapPdu rejects tag numbers above 30, and encoding/asn1
			// rejects non-minimal long-form encodings of smaller ones.
			t.Fatalf("UnwrapPdu accepted long-form tag %x", data)
		}
		// encoding/asn1 only accepts minimal definite lengths, so the
		// single-byte-tag re-encoding must reproduce the input exactly.
		if enc := berutil.EncodeTLV(tag, content); !bytes.Equal(enc, data) {
			t.Fatalf("re-encoding mismatch: %x != %x", enc, data)
		}
	})
}

// fuzzSeq is a representative IMPLICIT-SEQUENCE target that mirrors the
// shape of the MMS PDUs decoded through UnmarshalImplicitSequence.
type fuzzSeq struct {
	A int            `asn1:"tag:0,implicit,optional"`
	B int            `asn1:"tag:1,implicit"`
	C asn1.BitString `asn1:"tag:2,implicit,optional"`
	D []byte         `asn1:"tag:3,implicit,optional"`
}

// fuzzExplicit is a representative EXPLICIT target (an inner SEQUENCE).
type fuzzExplicit struct {
	Name string `asn1:"utf8"`
	N    int
}

func FuzzUnmarshalChoice(f *testing.F) {
	seq, _ := MarshalSequenceContent(fuzzSeq{A: 1, B: 2, C: asn1.BitString{Bytes: []byte{0xf0}, BitLength: 4}, D: []byte{9}})
	seqMin, _ := MarshalSequenceContent(fuzzSeq{B: 300})
	expl, _ := asn1.Marshal(fuzzExplicit{Name: "LLN0", N: 5})
	full, _ := asn1.Marshal(asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 3, Bytes: []byte{0x2a}})
	seeds := [][]byte{
		asn1util.WrapConstructed(4, seq),
		asn1util.WrapConstructed(4, seqMin),
		asn1util.WrapConstructed(6, expl),
		asn1util.WrapContextTag(72, true, seq),
		full,
		asn1util.WrapPrimitive(4, seq),
		asn1util.WrapConstructed(4, seq[:len(seq)-1]),
		asn1util.WrapConstructed(6, append(bytes.Clone(expl), 0x00)),
		{0xa4, 0x00},
		{0xa4, 0x02, 0x81, 0x00},
		{},
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		raw, _, err := asn1util.UnmarshalRaw(data)
		if err != nil {
			return
		}
		_ = ServiceTag(raw)

		var s fuzzSeq
		if err := UnmarshalImplicitSequence(raw, &s); err == nil {
			content, err := MarshalSequenceContent(s)
			if err != nil {
				t.Fatalf("MarshalSequenceContent(%+v): %v", s, err)
			}
			var s2 fuzzSeq
			again := asn1.RawValue{Class: raw.Class, Tag: raw.Tag, IsCompound: true, Bytes: content}
			if err := UnmarshalImplicitSequence(again, &s2); err != nil {
				t.Fatalf("decode(encode(x)) failed: %v", err)
			}
			if !reflect.DeepEqual(normSeq(s), normSeq(s2)) {
				t.Fatalf("implicit round-trip mismatch: %+v != %+v", s, s2)
			}
		}

		var e fuzzExplicit
		if err := UnmarshalExplicit(raw, &e); err == nil {
			inner, err := asn1.Marshal(e)
			if err != nil {
				t.Fatalf("asn1.Marshal(%+v): %v", e, err)
			}
			var e2 fuzzExplicit
			again := asn1.RawValue{Class: raw.Class, Tag: raw.Tag, IsCompound: true, Bytes: inner}
			if err := UnmarshalExplicit(again, &e2); err != nil || e2 != e {
				t.Fatalf("explicit round-trip mismatch: %+v != %+v (%v)", e2, e, err)
			}
		}

		var rv asn1.RawValue
		_ = UnmarshalFull(raw, &rv)
		var n int
		_ = UnmarshalFull(raw, &n)
	})
}

// normSeq maps nil and empty slices to the same value so DeepEqual
// compares content only.
func normSeq(s fuzzSeq) fuzzSeq {
	if len(s.C.Bytes) == 0 {
		s.C.Bytes = nil
	}
	if len(s.D) == 0 {
		s.D = nil
	}
	return s
}
