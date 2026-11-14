// SPDX-License-Identifier: MIT

package pdu

import (
	"encoding/asn1"
	"math"
	"testing"

	"github.com/otfabric/go-mms/internal/berutil"
)

// arrayTypeSpec is the content of an array TypeSpecification with the given
// numberOfElements octets and a boolean element type.
func arrayTypeSpec(count []byte) []byte {
	return append(berutil.EncodeTLV(0x81, count), berutil.EncodeTLV(0xa2, berutil.EncodeTLV(0x83, nil))...)
}

// wide reports whether an int holds the whole Unsigned32 range.
const wide = math.MaxInt == math.MaxInt64

// numberOfElements is Unsigned32 and is kept in an int. A negative encoding
// is rejected; a value of 2^31 or more is the value itself where an int
// holds it and is rejected where it does not. It is never a negative count.
func TestArrayTypeSpecNumberOfElementsRange(t *testing.T) {
	big := func(v uint32) int { // the expected count on a 64-bit platform
		if !wide {
			return 0
		}
		return int(int64(v))
	}
	for _, tc := range []struct {
		name  string
		count []byte
		want  int
		ok    bool
	}{
		{"zero", []byte{0x00}, 0, true},
		{"one octet", []byte{0x7f}, 127, true},
		{"padded 128", []byte{0x00, 0x80}, 128, true},
		{"max int32", []byte{0x7f, 0xff, 0xff, 0xff}, math.MaxInt32, true},
		{"negative one octet", []byte{0xff}, 0, false},
		{"negative four octets", []byte{0x80, 0x00, 0x00, 0x00}, 0, false},
		{"2^31", []byte{0x00, 0x80, 0x00, 0x00, 0x00}, big(1 << 31), wide},
		{"max uint32", []byte{0x00, 0xff, 0xff, 0xff, 0xff}, big(math.MaxUint32), wide},
		{"five octets", []byte{0x01, 0x00, 0x00, 0x00, 0x00}, 0, false},
		{"empty", nil, 0, false},
	} {
		ts, err := decodeArrayTypeSpecWithDepth(arrayTypeSpec(tc.count), 0)
		if tc.ok != (err == nil) {
			t.Errorf("%s: err = %v, want ok = %v", tc.name, err, tc.ok)
			continue
		}
		if tc.ok && (ts.Count != tc.want || ts.Count < 0) {
			t.Errorf("%s: Count = %d, want %d", tc.name, ts.Count, tc.want)
		}
	}
}

// An Unsigned32 from the wire is never a negative int: it is the value
// itself where an int holds it, and an error where it does not.
func TestIndexFromUnsigned32(t *testing.T) {
	for _, v := range []uint32{0, 1, math.MaxInt32} {
		if got, err := indexFromUnsigned32(v, "index"); err != nil || got != int(v) {
			t.Errorf("indexFromUnsigned32(%d) = %d, %v", v, got, err)
		}
	}
	for _, v := range []uint32{math.MaxInt32 + 1, math.MaxUint32} {
		got, err := indexFromUnsigned32(v, "index")
		if wide {
			if err != nil || int64(got) != int64(v) {
				t.Errorf("indexFromUnsigned32(%d) = %d, %v", v, got, err)
			}
		} else if err == nil {
			t.Errorf("indexFromUnsigned32(%d) = %d, want an error on a 32-bit platform", v, got)
		}
	}
}

// The fields of an index range from the wire are never negative.
func TestDecodeIndexRangeNeverNegative(t *testing.T) {
	big := []byte{0x00, 0x80, 0x00, 0x00, 0x00} // 2^31
	one := []byte{0x01}
	for name, data := range map[string][]byte{
		"lowIndex":         append(berutil.EncodeTLV(0x80, big), berutil.EncodeTLV(0x81, one)...),
		"numberOfElements": append(berutil.EncodeTLV(0x80, one), berutil.EncodeTLV(0x81, big)...),
	} {
		r, err := decodeIndexRange(data)
		if !wide {
			if err == nil {
				t.Errorf("%s 2^31: decoded %+v on a 32-bit platform, want an error", name, r)
			}
			continue
		}
		if err != nil || r.LowIndex < 0 || r.NumberOfElements < 0 {
			t.Errorf("%s 2^31: %+v, %v", name, r, err)
		}
	}
}

// realWithExponent is binary REAL content with base 16, an exponent in the
// long form and mantissa 1.
func realWithExponent(negative bool, exp ...byte) []byte {
	info := byte(0x80 | 0x20 | 0x03) // binary, base 16, exponent length follows
	if negative {
		info |= 0x40
	}
	return append(append([]byte{info, byte(len(exp))}, exp...), 0x01)
}

// The exponent is multiplied by the base shift. An exponent that makes the
// product overflow must not wrap into a finite value: it is beyond what a
// float64 holds, so the result is ±Inf, or zero for a negative exponent.
func TestRealExponentDoesNotWrap(t *testing.T) {
	decode := func(content []byte) float64 {
		t.Helper()
		dv, _, err := UnmarshalDataElement(berutil.EncodeTLV(TagDataReal, content), 0)
		if err != nil {
			t.Fatalf("% x: %v", content, err)
		}
		return dv.Float
	}
	// 2^62 * 4 wraps to 0 in int64, and 2^61 * 4 to the minimum int64.
	if got := decode(realWithExponent(false, 0x40, 0, 0, 0, 0, 0, 0, 0)); !math.IsInf(got, 1) {
		t.Errorf("exponent 2^62: %v, want +Inf", got)
	}
	if got := decode(realWithExponent(true, 0x40, 0, 0, 0, 0, 0, 0, 0)); !math.IsInf(got, -1) {
		t.Errorf("negative, exponent 2^62: %v, want -Inf", got)
	}
	if got := decode(realWithExponent(false, 0x20, 0, 0, 0, 0, 0, 0, 0)); !math.IsInf(got, 1) {
		t.Errorf("exponent 2^61: %v, want +Inf", got)
	}
	if got := decode(realWithExponent(false, 0xc0, 0, 0, 0, 0, 0, 0, 0)); got != 0 {
		t.Errorf("exponent -2^62: %v, want 0", got)
	}
	// In range: unchanged. 1 * 16^2 = 256, 1 * 16^-1 = 0.0625.
	if got := decode(realWithExponent(false, 0x02)); got != 256 {
		t.Errorf("exponent 2: %v, want 256", got)
	}
	if got := decode(realWithExponent(false, 0xff)); got != 0.0625 {
		t.Errorf("exponent -1: %v, want 0.0625", got)
	}
}

// A REAL is M × 2^F × B^E (X.690 §8.5): a zero mantissa is zero for
// every exponent, and a product that a float64 holds is that product, also
// when B^E alone is beyond the float64 range.
func TestRealScaling(t *testing.T) {
	fullMantissa := []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	for _, tc := range []struct {
		name    string
		content []byte
		want    float64
	}{
		{"zero, one-octet exponent 127", []byte{0x80, 0x7f, 0x00}, 0},
		{"zero, two-octet exponent 32767", []byte{0x81, 0x7f, 0xff, 0x00}, 0},
		{"zero, length-prefixed exponent 2^62", append(realWithExponent(false, 0x40, 0, 0, 0, 0, 0, 0, 0)[:10], 0x00), 0},
		{"negative zero, two-octet exponent 32767", []byte{0xc1, 0x7f, 0xff, 0x00}, math.Copysign(0, -1)},
		{"zero, exponent -32768", []byte{0x81, 0x80, 0x00, 0x00}, 0},
		// 8 octets of mantissa, base 2: (2^64-1) × 2^-1137 is about 2^-1073.
		{"full mantissa, exponent -1137", append([]byte{0x81, 0xfb, 0x8f}, fullMantissa...), math.Ldexp(float64(math.MaxUint64), -1137)},
		{"full mantissa, exponent -1100", append([]byte{0x81, 0xfb, 0xb4}, fullMantissa...), math.Ldexp(float64(math.MaxUint64), -1100)},
		// 2^64 × 2^-1086 = 2^-1022, the smallest normal float64.
		{"full mantissa, exponent -1086", append([]byte{0x81, 0xfb, 0xc2}, fullMantissa...), math.Ldexp(float64(math.MaxUint64), -1086)},
		{"full mantissa, exponent 960", append([]byte{0x81, 0x03, 0xc0}, fullMantissa...), math.Ldexp(float64(math.MaxUint64), 960)},
		{"one, exponent 0", []byte{0x80, 0x00, 0x01}, 1},
		{"-3, base 8, exponent 1, scaling 1", []byte{0xd4, 0x01, 0x03}, -48},
	} {
		dv, _, err := UnmarshalDataElement(berutil.EncodeTLV(TagDataReal, tc.content), 0)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if dv.Float != tc.want || math.Signbit(dv.Float) != math.Signbit(tc.want) || math.IsNaN(dv.Float) {
			t.Errorf("%s (% x): %v, want %v", tc.name, tc.content, dv.Float, tc.want)
		}
	}
	if math.Ldexp(float64(math.MaxUint64), -1137) == 0 {
		t.Fatal("expected value underflows; the vectors need a larger exponent")
	}
}

// numberMatched and numberDeleted of a DeleteNamedVariableList response
// are Unsigned32: the value itself where an int holds it, an error where
// it does not, never a negative count.
func TestDeleteNamedVarListResponseCounts(t *testing.T) {
	for _, v := range []uint32{0, 1, math.MaxInt32, math.MaxInt32 + 1, math.MaxUint32} {
		u := berutil.EncodeUint32(v)
		raw := asn1.RawValue{Bytes: append(berutil.EncodeTLV(0x80, u), berutil.EncodeTLV(0x81, u)...)}
		res, err := UnmarshalDeleteNamedVarListResponse(raw)
		if wide || v <= math.MaxInt32 {
			if err != nil || int64(res.NumberMatched) != int64(v) || int64(res.NumberDeleted) != int64(v) {
				t.Errorf("%d: %+v, %v", v, res, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%d: %+v, want an error on a 32-bit platform", v, res)
		}
	}
}
