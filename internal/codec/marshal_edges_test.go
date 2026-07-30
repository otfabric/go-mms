// SPDX-License-Identifier: MIT

package codec

import (
	"encoding/asn1"
	"testing"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
)

func TestMarshalMmsPdu_Edges(t *testing.T) {
	if _, err := MarshalMmsPdu(asn1util.TagConfirmedRequest, make(chan int)); err == nil {
		t.Fatal("marshal error")
	}
	// Primitive PDU tag path (ConcludeRequest is context 11, primitive).
	got, err := MarshalMmsPdu(asn1util.TagConcludeRequest, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != asn1util.TagConcludeRequest {
		t.Fatalf("tag 0x%02x", got[0])
	}
}

func TestMarshalConfirmedRequest_OK(t *testing.T) {
	// Exercise success path thoroughly (invokeID marshal error is unreachable for int).
	got, err := MarshalConfirmedRequest(7, 72, true, []byte{0x01})
	if err != nil || len(got) == 0 || got[0] != 0xa0 {
		t.Fatalf("%x %v", got, err)
	}
}

func TestMarshalSequenceContent_Edges(t *testing.T) {
	if _, err := MarshalSequenceContent(make(chan int)); err == nil {
		t.Fatal("marshal error")
	}
	// asn1.Marshal(int) yields INTEGER, not SEQUENCE.
	if _, err := MarshalSequenceContent(42); err == nil {
		t.Fatal("expected non-SEQUENCE tag")
	}
	type inner struct {
		N int
	}
	got, err := MarshalSequenceContent(inner{N: 1})
	if err != nil || len(got) == 0 {
		t.Fatalf("%x %v", got, err)
	}

	// asn1.RawValue.FullBytes is emitted as-is — truncated SEQUENCE.
	if _, err := MarshalSequenceContent(asn1.RawValue{FullBytes: []byte{0x30, 0x05}}); err == nil {
		t.Fatal("truncated SEQUENCE header")
	}
	// Complete SEQUENCE plus trailing bytes.
	seq := berutil.EncodeTLV(0x30, []byte{0x02, 0x01, 0x01})
	junk := append(append([]byte{}, seq...), 0x05, 0x00)
	if _, err := MarshalSequenceContent(asn1.RawValue{FullBytes: junk}); err == nil {
		t.Fatal("trailing bytes")
	}
}
