// SPDX-License-Identifier: MIT

package codec

import (
	"encoding/asn1"
	"strings"
	"testing"

	"github.com/otfabric/go-mms/internal/berutil"
)

func TestMarshalConcludeRequest(t *testing.T) {
	data := MarshalConcludeRequest()
	if len(data) == 0 {
		t.Fatal("empty")
	}
	tag, err := PduType(data)
	if err != nil {
		t.Fatal(err)
	}
	// context class (0x80) + tag 11 => 0x8b
	if tag != 0x8b {
		t.Fatalf("got tag 0x%02x, want 0x8b", tag)
	}
}

func TestMarshalConcludeResponse(t *testing.T) {
	data := MarshalConcludeResponse()
	if len(data) == 0 {
		t.Fatal("empty")
	}
	tag, err := PduType(data)
	if err != nil {
		t.Fatal(err)
	}
	if tag != 0x8c {
		t.Fatalf("got tag 0x%02x, want 0x8c", tag)
	}
}

func TestConfirmedRequestRoundtrip(t *testing.T) {
	servicePayload := []byte{0x01, 0x01, 0xff} // boolean true
	pdu, err := MarshalConfirmedRequest(42, 4, true, servicePayload)
	if err != nil {
		t.Fatal(err)
	}

	tag, content, err := UnwrapPdu(pdu)
	if err != nil {
		t.Fatal(err)
	}
	// confirmed request tag = context 0, constructed = 0xa0
	if tag != 0xa0 {
		t.Fatalf("got tag 0x%02x, want 0xa0", tag)
	}

	invokeID, serviceRaw, err := UnmarshalConfirmedRequest(content)
	if err != nil {
		t.Fatal(err)
	}
	if invokeID != 42 {
		t.Fatalf("got invokeID %d, want 42", invokeID)
	}

	// ServiceTag should be context 4, constructed
	st := ServiceTag(serviceRaw)
	if st != 0xa4 {
		t.Fatalf("got service tag 0x%02x, want 0xa4", st)
	}
}

func TestConfirmedResponseRoundtrip(t *testing.T) {
	servicePayload := []byte{0x01, 0x01, 0xff}
	pdu, err := MarshalConfirmedResponse(7, 4, true, servicePayload)
	if err != nil {
		t.Fatal(err)
	}

	tag, content, err := UnwrapPdu(pdu)
	if err != nil {
		t.Fatal(err)
	}
	// confirmed response = context 1, constructed = 0xa1
	if tag != 0xa1 {
		t.Fatalf("got tag 0x%02x, want 0xa1", tag)
	}

	invokeID, serviceRaw, err := UnmarshalConfirmedResponse(content)
	if err != nil {
		t.Fatal(err)
	}
	if invokeID != 7 {
		t.Fatalf("got invokeID %d, want 7", invokeID)
	}
	_ = serviceRaw
}

func TestConfirmedError(t *testing.T) {
	data := MarshalConfirmedError(99, 1, 2)
	if len(data) == 0 {
		t.Fatal("empty")
	}
	tag, err := PduType(data)
	if err != nil {
		t.Fatal(err)
	}
	// confirmed error = context 2, constructed = 0xa2
	if tag != 0xa2 {
		t.Fatalf("got tag 0x%02x, want 0xa2", tag)
	}
}

func TestRejectPDU(t *testing.T) {
	data := MarshalRejectPDU(55, 1, 3)
	if len(data) == 0 {
		t.Fatal("empty")
	}
	tag, err := PduType(data)
	if err != nil {
		t.Fatal(err)
	}
	// reject = context 4, constructed = 0xa4
	if tag != 0xa4 {
		t.Fatalf("got tag 0x%02x, want 0xa4", tag)
	}
}

func TestMarshalMmsPdu(t *testing.T) {
	type simple struct {
		Value int
	}
	data, err := MarshalMmsPdu(0xa0, simple{Value: 42}) // constructed context 0
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("empty")
	}
}

func TestUnmarshalImplicitSequence(t *testing.T) {
	t.Run("short-form length", func(t *testing.T) {
		type inner struct {
			Value int
		}
		encoded, err := asn1.Marshal(inner{Value: 42})
		if err != nil {
			t.Fatal(err)
		}
		tag, bare, consumed, err2 := berutil.DecodeTLVAt(encoded, 0)
		if err2 != nil {
			t.Fatal(err2)
		}
		if tag != 0x30 {
			t.Fatalf("tag = 0x%02x, want 0x30", tag)
		}
		if consumed != len(encoded) {
			t.Fatalf("%d trailing bytes", len(encoded)-consumed)
		}
		raw := asn1.RawValue{Class: 2, Tag: 0, IsCompound: true, Bytes: bare}

		var result inner
		if err := UnmarshalImplicitSequence(raw, &result); err != nil {
			t.Fatal(err)
		}
		if result.Value != 42 {
			t.Fatalf("got %d, want 42", result.Value)
		}
	})

	t.Run("long-form length", func(t *testing.T) {
		type inner struct {
			Value string
		}
		// 200-byte string pushes the SEQUENCE length past the 127-byte short-form boundary.
		encoded, err := asn1.Marshal(inner{Value: strings.Repeat("x", 200)})
		if err != nil {
			t.Fatal(err)
		}
		tag, bare, consumed, err2 := berutil.DecodeTLVAt(encoded, 0)
		if err2 != nil {
			t.Fatal(err2)
		}
		if tag != 0x30 {
			t.Fatalf("tag = 0x%02x, want 0x30", tag)
		}
		if consumed != len(encoded) {
			t.Fatalf("%d trailing bytes", len(encoded)-consumed)
		}
		raw := asn1.RawValue{Class: 2, Tag: 0, IsCompound: true, Bytes: bare}

		var result inner
		if err := UnmarshalImplicitSequence(raw, &result); err != nil {
			t.Fatal(err)
		}
		if result.Value != strings.Repeat("x", 200) {
			t.Fatalf("got wrong value: %q", result.Value)
		}
	})
}

func TestMarshalSequenceContent(t *testing.T) {
	t.Run("short-form length", func(t *testing.T) {
		type inner struct {
			Value int
		}
		content, err := MarshalSequenceContent(inner{Value: 99})
		if err != nil {
			t.Fatal(err)
		}
		// Reconstruct SEQUENCE wrapper and decode.
		wrapped := berutil.EncodeTLV(0x30, content)
		var result inner
		if _, err := asn1.Unmarshal(wrapped, &result); err != nil {
			t.Fatal(err)
		}
		if result.Value != 99 {
			t.Fatalf("got %d, want 99", result.Value)
		}
	})

	t.Run("long-form length", func(t *testing.T) {
		type inner struct {
			Value string
		}
		want := strings.Repeat("y", 200)
		content, err := MarshalSequenceContent(inner{Value: want})
		if err != nil {
			t.Fatal(err)
		}
		wrapped := berutil.EncodeTLV(0x30, content)
		var result inner
		if _, err := asn1.Unmarshal(wrapped, &result); err != nil {
			t.Fatal(err)
		}
		if result.Value != want {
			t.Fatalf("got wrong value: %q", result.Value)
		}
	})
}

func TestUnmarshalImplicitSequencePrimitive(t *testing.T) {
	raw := asn1.RawValue{Class: 2, Tag: 0, IsCompound: false, Bytes: []byte{1}}
	var result int
	if err := UnmarshalImplicitSequence(raw, &result); err == nil {
		t.Fatal("expected error for primitive value")
	}
}

func TestUnmarshalExplicit(t *testing.T) {
	type inner struct {
		Value int
	}
	// For EXPLICIT wrapping, raw.Bytes starts with the full TLV of the inner
	// type (0x30 + length + fields).
	encoded, err := asn1.Marshal(inner{Value: 42})
	if err != nil {
		t.Fatal(err)
	}
	raw := asn1.RawValue{Class: 2, Tag: 0, IsCompound: true, Bytes: encoded}

	var result inner
	if err := UnmarshalExplicit(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Value != 42 {
		t.Fatalf("got %d, want 42", result.Value)
	}
}

func TestUnmarshalFull(t *testing.T) {
	val := 42
	encoded, err := asn1.Marshal(val)
	if err != nil {
		t.Fatal(err)
	}
	raw := asn1.RawValue{FullBytes: encoded}

	var result int
	if err2 := UnmarshalFull(raw, &result); err2 != nil {
		t.Fatal(err2)
	}
	if result != 42 {
		t.Fatalf("got %d, want 42", result)
	}
}

func TestPduTypeError(t *testing.T) {
	_, err := PduType(nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestUnwrapPduError(t *testing.T) {
	_, _, err := UnwrapPdu([]byte{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestUnwrapPduTrailingBytes(t *testing.T) {
	// Build a valid PDU then append junk
	pdu, err := MarshalConfirmedResponse(1, 4, true, []byte{0x01})
	if err != nil {
		t.Fatal(err)
	}
	pdu = append(pdu, 0xff, 0xfe) // trailing bytes
	_, _, err = UnwrapPdu(pdu)
	if err == nil {
		t.Fatal("expected error for trailing bytes")
	}
}
