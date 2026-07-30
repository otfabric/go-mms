// SPDX-License-Identifier: MIT

package pdu

import (
	"math"
	"testing"
)

func TestEncodeDecodeASN1Real_Edges(t *testing.T) {
	// Multi-byte exponent forms via extreme magnitudes.
	for _, f := range []float64{
		math.SmallestNonzeroFloat64,
		-math.SmallestNonzeroFloat64,
		math.MaxFloat64,
		-math.MaxFloat64,
		math.Pow(2, -1022),
	} {
		enc := encodeASN1Real(f)
		got, err := decodeASN1Real(enc)
		if err != nil {
			t.Fatalf("f=%v encode=%x: %v", f, enc, err)
		}
		if math.IsInf(f, 0) {
			continue
		}
		if got != f && !(got == 0 && f == 0) {
			// Allow tiny denormal rounding differences only if both non-zero same sign.
			if math.Abs(got-f)/math.Max(math.Abs(f), 1) > 1e-12 {
				t.Fatalf("f=%v got=%v enc=%x", f, got, enc)
			}
		}
	}

	// Decimal / ISO 6093 (high bit clear) rejected.
	if _, err := decodeASN1Real([]byte{0x03, 0x31, 0x32, 0x33}); err == nil {
		t.Fatal("decimal REAL")
	}
	// Reserved base (bits 5–4 = 11).
	if _, err := decodeASN1Real([]byte{0xb0, 0x00, 0x01}); err == nil {
		t.Fatal("reserved base")
	}
	// Base 8 and base 16 (bits 5–4 = 01 / 10).
	if _, err := decodeASN1Real([]byte{0x90, 0x00, 0x01}); err != nil { // base 8, exp 0, mant 1
		t.Fatal(err)
	}
	if _, err := decodeASN1Real([]byte{0xa0, 0x00, 0x01}); err != nil { // base 16
		t.Fatal(err)
	}
	// 2-byte and 3-byte exponent length encodings.
	if _, err := decodeASN1Real([]byte{0x81, 0x00, 0x01, 0x01}); err != nil { // EE=01, exp 2 bytes
		t.Fatal(err)
	}
	if _, err := decodeASN1Real([]byte{0x82, 0x00, 0x00, 0x01, 0x01}); err != nil { // EE=10, exp 3 bytes
		t.Fatal(err)
	}
	// EE=11 with explicit exponent length.
	if _, err := decodeASN1Real([]byte{0x83, 0x01, 0x00, 0x01}); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeASN1Real([]byte{0x83}); err == nil {
		t.Fatal("missing exp length")
	}
	if _, err := decodeASN1Real([]byte{0x83, 0x02, 0x00}); err == nil {
		t.Fatal("exponent overflow")
	}
	if _, err := decodeASN1Real([]byte{0x80, 0x00}); err == nil {
		t.Fatal("missing mantissa")
	}
	if _, err := decodeASN1Real(append([]byte{0x80, 0x00}, make([]byte, 9)...)); err == nil {
		t.Fatal("mantissa too large")
	}
	// Negative binary REAL.
	got, err := decodeASN1Real([]byte{0xc0, 0x00, 0x01})
	if err != nil || got >= 0 {
		t.Fatalf("neg: %v %v", got, err)
	}
	// EE=11 with zero-length exponent → decodeSignedInt empty.
	if _, err := decodeASN1Real([]byte{0x83, 0x00, 0x01}); err == nil {
		t.Fatal("empty exponent")
	}
}
