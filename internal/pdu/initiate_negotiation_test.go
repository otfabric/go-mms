// SPDX-License-Identifier: MIT

package pdu

import (
	"bytes"
	"testing"
)

// ISO 9506-2 8.2 Initiate-ErrorPDU ::= ServiceError; IEC 61850-8-1
// 8.1.3.4.2.1: access / object-access-denied for a security refusal.
func TestSpec_9506_InitiateErrorEncoding(t *testing.T) {
	b := MarshalInitiateError(ErrorClassAccess, AccessErrObjectAccessDenied)
	if want := []byte{0xaa, 0x05, 0xa0, 0x03, 0x87, 0x01, 0x03}; !bytes.Equal(b, want) {
		t.Fatalf("access error %x want %x", b, want)
	}
	b = MarshalInitiateError(ErrorClassInitiate, InitiateErrVersionIncompatible)
	if want := []byte{0xaa, 0x05, 0xa0, 0x03, 0x88, 0x01, 0x01}; !bytes.Equal(b, want) {
		t.Fatalf("initiate error %x want %x", b, want)
	}
	kind, content, err := DecodePdu(b)
	if err != nil || kind != PduInitiateError {
		t.Fatalf("kind %s err %v", kind, err)
	}
	se, err := DecodeServiceError(content)
	if err != nil || se.ErrorClass != ErrorClassInitiate || se.ErrorCode != 1 {
		t.Fatalf("decoded %+v %v", se, err)
	}
}

func TestDecodeServiceError_Edges(t *testing.T) {
	for _, tc := range []struct {
		in   []byte
		ok   bool
		c, v int
	}{
		{[]byte{0xa0, 0x03, 0x89, 0x01, 0x01, 0x81, 0x01, 0x05}, true, 9, 1}, // with additionalCode
		{[]byte{0x81, 0x01, 0x05}, false, 0, 0},                              // missing errorClass
		{[]byte{0xa0, 0x00}, false, 0, 0},                                    // empty errorClass
		{[]byte{0xa0, 0x05}, false, 0, 0},                                    // truncated
		{[]byte{0xa0, 0x04, 0x89, 0x01, 0x01, 0x00}, false, 0, 0},            // trailing in class
		{nil, false, 0, 0},
	} {
		se, err := DecodeServiceError(tc.in)
		if (err == nil) != tc.ok || (tc.ok && (se.ErrorClass != tc.c || se.ErrorCode != tc.v)) {
			t.Errorf("%x: %+v %v", tc.in, se, err)
		}
	}
}

// ISO 9506-2 8.2: localDetailCalling [0] and
// proposedDataStructureNestingLevel [3] are OPTIONAL (MMS-PDU-04).
func TestSpec_9506_InitiatePresence(t *testing.T) {
	withBoth, _ := MarshalInitiateRequest(DefaultInitiateRequest(0, 0, 0, 0))
	_, content, _ := DecodePdu(withBoth)
	if l, n := InitiatePresence(content); !l || !n {
		t.Fatalf("both present: %v %v", l, n)
	}
	// a8 13 81 01 05 82 01 05 a4 0e ... : neither [0] nor [3].
	without := []byte{0x81, 0x01, 0x05, 0x82, 0x01, 0x05, 0xa4, 0x0e, 0x80, 0x01, 0x01, 0x81, 0x03, 0x05, 0xf1, 0x00, 0x82, 0x04, 0x03, 0xee, 0x1c, 0x00}
	if l, n := InitiatePresence(without); l || n {
		t.Fatalf("absent: %v %v", l, n)
	}
	wrapped := append([]byte{0x30, byte(len(without) + 3), 0x80, 0x01, 0x10}, without...)
	if l, n := InitiatePresence(wrapped); !l || n {
		t.Fatalf("wrapped: %v %v", l, n)
	}
	if l, n := InitiatePresence([]byte{0x80, 0x01, 0x01, 0xff}); !l || n {
		t.Fatalf("truncated tail: %v %v", l, n)
	}
}

func FuzzInitiatePresence(f *testing.F) {
	f.Add([]byte{0xa0, 0x03, 0x87, 0x01, 0x03})
	f.Add([]byte{0x80, 0x01, 0x01, 0xff})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = InitiatePresence(data)
	})
}
