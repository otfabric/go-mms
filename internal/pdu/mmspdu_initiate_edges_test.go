// SPDX-License-Identifier: MIT

package pdu

import (
	"testing"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
)

func TestPduKindString_Unknown(t *testing.T) {
	if got := PduKind(999).String(); got != "PduKind(999)" {
		t.Fatalf("got %q", got)
	}
	if got := PduKind(-1).String(); got != "PduKind(-1)" {
		t.Fatalf("got %q", got)
	}
}

func TestClassifyTag_RemainingAndDecodePdu(t *testing.T) {
	cases := []struct {
		tag  byte
		want PduKind
	}{
		{asn1util.TagConcludeError, PduConcludeError},
		{asn1util.TagCancelRequest, PduCancelRequest},
		{asn1util.TagCancelResponse, PduCancelResponse},
		{asn1util.TagCancelError, PduCancelError},
	}
	for _, tc := range cases {
		got, err := classifyTag(tc.tag)
		if err != nil || got != tc.want {
			t.Fatalf("tag 0x%02x: got %v err=%v", tc.tag, got, err)
		}
		kind, content, err := DecodePdu([]byte{tc.tag, 0x00})
		if err != nil || kind != tc.want || content == nil {
			t.Fatalf("DecodePdu 0x%02x: kind=%v content=%v err=%v", tc.tag, kind, content, err)
		}
	}

	if _, _, err := DecodePdu(nil); err == nil {
		t.Fatal("empty DecodePdu")
	}
	// Valid BER with an MMS-adjacent tag that classifyTag rejects.
	if _, _, err := DecodePdu([]byte{0xa5, 0x00}); err == nil {
		t.Fatal("unknown DecodePdu tag")
	}
}

func TestUnmarshalInitiateResponse_Edges(t *testing.T) {
	if _, err := UnmarshalInitiateResponse([]byte{0xff}); err == nil {
		t.Fatal("invalid ASN.1")
	}

	resp := InitiateResponse{
		LocalDetailCalled:                  1000,
		NegotiatedMaxServOutstandingCall:   1,
		NegotiatedMaxServOutstandingCalled: 1,
		NegotiatedDataStructureNesting:     1,
		InitResponseDetail: InitResponseDetail{
			NegotiatedVersion: 1,
		},
	}
	bare, err := marshalBareSequence(resp)
	if err != nil {
		t.Fatal(err)
	}
	// Explicit 0x30 wrapper (libiec61850-style).
	wrapped := berutil.EncodeTLV(0x30, bare)
	decoded, err := UnmarshalInitiateResponse(wrapped)
	if err != nil || decoded.LocalDetailCalled != 1000 {
		t.Fatalf("wrapped: %+v err=%v", decoded, err)
	}
	// Trailing bytes after a complete SEQUENCE.
	junk := append(append([]byte{}, wrapped...), 0x05, 0x00)
	if _, err := UnmarshalInitiateResponse(junk); err == nil {
		t.Fatal("trailing bytes")
	}
}
