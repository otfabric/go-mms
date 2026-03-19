package mms

import (
	"math"
	"testing"
	"time"
)

func TestValueBool(t *testing.T) {
	v := NewBoolean(true)
	if v.Type() != ValueTypeBoolean {
		t.Fatalf("Type() = %v, want Boolean", v.Type())
	}
	got, ok := v.Bool()
	if !ok || got != true {
		t.Errorf("Bool() = (%v, %v), want (true, true)", got, ok)
	}

	_, ok = v.Int32()
	if ok {
		t.Error("Int32() on Boolean should return ok=false")
	}
}

func TestValueInteger(t *testing.T) {
	v := NewInteger(42)
	got32, ok := v.Int32()
	if !ok || got32 != 42 {
		t.Errorf("Int32() = (%v, %v), want (42, true)", got32, ok)
	}
	got64, ok := v.Int64()
	if !ok || got64 != 42 {
		t.Errorf("Int64() = (%v, %v), want (42, true)", got64, ok)
	}

	_, ok = v.Bool()
	if ok {
		t.Error("Bool() on Integer should return ok=false")
	}
}

func TestValueIntegerOverflow(t *testing.T) {
	v := NewInteger(1 << 40)
	_, ok := v.Int32()
	if ok {
		t.Error("Int32() should return ok=false for overflow")
	}
	got, ok := v.Int64()
	if !ok || got != 1<<40 {
		t.Errorf("Int64() = (%v, %v), want (%v, true)", got, ok, int64(1<<40))
	}
}

func TestValueUnsigned(t *testing.T) {
	v := NewUnsigned(100)
	got32, ok := v.Uint32()
	if !ok || got32 != 100 {
		t.Errorf("Uint32() = (%v, %v), want (100, true)", got32, ok)
	}
	got64, ok := v.Uint64()
	if !ok || got64 != 100 {
		t.Errorf("Uint64() = (%v, %v), want (100, true)", got64, ok)
	}
}

func TestValueUnsignedOverflow(t *testing.T) {
	v := NewUnsigned(1 << 40)
	_, ok := v.Uint32()
	if ok {
		t.Error("Uint32() should return ok=false for overflow")
	}
}

func TestValueFloat(t *testing.T) {
	v := NewFloat(3.14)
	got32, ok := v.Float32()
	if !ok {
		t.Fatal("Float32() ok=false")
	}
	if got32 < 3.13 || got32 > 3.15 {
		t.Errorf("Float32() = %v, want ~3.14", got32)
	}
	got64, ok := v.Float64()
	if !ok || got64 != 3.14 {
		t.Errorf("Float64() = (%v, %v), want (3.14, true)", got64, ok)
	}
}

func TestValueStrings(t *testing.T) {
	v1 := NewVisibleString("hello")
	got, ok := v1.VisibleString()
	if !ok || got != "hello" {
		t.Errorf("VisibleString() = (%q, %v), want (hello, true)", got, ok)
	}
	_, ok = v1.MmsString()
	if ok {
		t.Error("MmsString() on VisibleString should return ok=false")
	}

	v2 := NewMmsString("world")
	got, ok = v2.MmsString()
	if !ok || got != "world" {
		t.Errorf("MmsString() = (%q, %v), want (world, true)", got, ok)
	}
}

func TestValueOctetString(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03}
	v := NewOctetString(data)
	got, ok := v.OctetString()
	if !ok {
		t.Fatal("OctetString() ok=false")
	}
	if len(got) != 3 || got[0] != 0x01 {
		t.Errorf("OctetString() = %v, want [1 2 3]", got)
	}
}

func TestValueUTCTime(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	v := NewUTCTime(now)
	got, ok := v.UTCTime()
	if !ok || !got.Equal(now) {
		t.Errorf("UTCTime() = (%v, %v), want (%v, true)", got, ok, now)
	}
}

func TestValueStructureAndArray(t *testing.T) {
	elems := []*Value{NewBoolean(true), NewInteger(42)}
	s := NewStructure(elems)
	got, ok := s.Structure()
	if !ok || len(got) != 2 {
		t.Errorf("Structure() = (%v, %v), want (2 elements, true)", got, ok)
	}

	a := NewArray(elems)
	got, ok = a.ArrayElements()
	if !ok || len(got) != 2 {
		t.Errorf("ArrayElements() = (%v, %v), want (2 elements, true)", got, ok)
	}

	_, ok = s.ArrayElements()
	if ok {
		t.Error("ArrayElements() on Structure should return ok=false")
	}
}

func TestValueDataAccessError(t *testing.T) {
	v := NewDataAccessError(DataAccessErrorObjectUndefined)
	got, ok := v.DataAccessErr()
	if !ok || got != DataAccessErrorObjectUndefined {
		t.Errorf("DataAccessErr() = (%v, %v), want (ObjectUndefined, true)", got, ok)
	}
}

func TestValueByteSliceCopyIsolation(t *testing.T) {
	orig := []byte{0x01, 0x02, 0x03}
	v := NewOctetString(orig)

	// Mutating the original should not affect the stored value.
	orig[0] = 0xff
	got, ok := v.OctetString()
	if !ok {
		t.Fatal("OctetString() ok=false")
	}
	if got[0] != 0x01 {
		t.Error("constructor should copy input: mutation of original affected stored value")
	}

	// Mutating the returned slice should not affect the stored value.
	got[1] = 0xff
	got2, _ := v.OctetString()
	if got2[1] != 0x02 {
		t.Error("accessor should copy output: mutation of returned slice affected stored value")
	}
}

func TestValueStructureCopyIsolation(t *testing.T) {
	elems := []*Value{NewBoolean(true), NewInteger(42)}
	v := NewStructure(elems)

	// Mutating the original slice should not affect the stored value.
	elems[0] = NewFloat(1.0)
	got, ok := v.Structure()
	if !ok {
		t.Fatal("Structure() ok=false")
	}
	if got[0].Type() != ValueTypeBoolean {
		t.Error("constructor should copy input: mutation of original affected stored value")
	}

	// Mutating the returned slice should not affect the stored value.
	got[1] = NewFloat(2.0)
	got2, _ := v.Structure()
	if got2[1].Type() != ValueTypeInteger {
		t.Error("accessor should copy output: mutation of returned slice affected stored value")
	}
}

func TestValueReal(t *testing.T) {
	v := NewReal(2.718281828)
	if v.Type() != ValueTypeReal {
		t.Fatalf("Type() = %v, want Real", v.Type())
	}
	got, ok := v.Real()
	if !ok || got != 2.718281828 {
		t.Errorf("Real() = (%v, %v), want (2.718281828, true)", got, ok)
	}

	_, ok = v.Float64()
	if ok {
		t.Error("Float64() on Real should return ok=false")
	}
}

func TestValueRealSpecial(t *testing.T) {
	for _, tc := range []struct {
		name string
		val  float64
	}{
		{"positive_infinity", math.Inf(1)},
		{"negative_infinity", math.Inf(-1)},
		{"negative_zero", math.Copysign(0, -1)},
		{"zero", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := NewReal(tc.val)
			got, ok := v.Real()
			if !ok {
				t.Fatal("Real() ok=false")
			}
			if math.IsInf(tc.val, 0) {
				if !math.IsInf(got, int(math.Copysign(1, tc.val))) {
					t.Errorf("Real() = %v, want %v", got, tc.val)
				}
			} else if math.Signbit(tc.val) != math.Signbit(got) || got != tc.val {
				t.Errorf("Real() = %v, want %v", got, tc.val)
			}
		})
	}
}

func TestValueRealNaN(t *testing.T) {
	v := NewReal(math.NaN())
	got, ok := v.Real()
	if !ok {
		t.Fatal("Real() ok=false")
	}
	if !math.IsNaN(got) {
		t.Errorf("Real() = %v, want NaN", got)
	}
}

func TestValueBooleanArray(t *testing.T) {
	bits := []byte{0b11001010, 0b10000000}
	v := NewBooleanArray(bits, 9)
	if v.Type() != ValueTypeBooleanArray {
		t.Fatalf("Type() = %v, want BooleanArray", v.Type())
	}
	gotBits, gotLen, ok := v.BooleanArray()
	if !ok {
		t.Fatal("BooleanArray() ok=false")
	}
	if gotLen != 9 {
		t.Errorf("bitLen = %d, want 9", gotLen)
	}
	if len(gotBits) != 2 || gotBits[0] != 0b11001010 || gotBits[1] != 0b10000000 {
		t.Errorf("BooleanArray() bits = %v, want [0xCA 0x80]", gotBits)
	}

	_, ok = v.Bool()
	if ok {
		t.Error("Bool() on BooleanArray should return ok=false")
	}
}

func TestValueBooleanArrayCopyIsolation(t *testing.T) {
	orig := []byte{0xff, 0x0f}
	v := NewBooleanArray(orig, 12)

	orig[0] = 0x00
	gotBits, _, ok := v.BooleanArray()
	if !ok {
		t.Fatal("BooleanArray() ok=false")
	}
	if gotBits[0] != 0xff {
		t.Error("constructor should copy input")
	}

	gotBits[1] = 0x00
	gotBits2, _, _ := v.BooleanArray()
	if gotBits2[1] != 0x0f {
		t.Error("accessor should copy output")
	}
}

func TestValueRealEqual(t *testing.T) {
	v1 := NewReal(3.14)
	v2 := NewReal(3.14)
	v3 := NewReal(2.71)
	if !v1.Equal(v2) {
		t.Error("Equal(same value) should be true")
	}
	if v1.Equal(v3) {
		t.Error("Equal(different value) should be false")
	}
}

func TestValueBooleanArrayEqual(t *testing.T) {
	v1 := NewBooleanArray([]byte{0xCA}, 7)
	v2 := NewBooleanArray([]byte{0xCA}, 7)
	v3 := NewBooleanArray([]byte{0xCA}, 6)
	if !v1.Equal(v2) {
		t.Error("Equal(same) should be true")
	}
	if v1.Equal(v3) {
		t.Error("Equal(different bitLen) should be false")
	}
}

func TestValueRealString(t *testing.T) {
	v := NewReal(1.5)
	s := v.String()
	if s == "" {
		t.Error("String() should not be empty")
	}
}

func TestValueBooleanArrayString(t *testing.T) {
	v := NewBooleanArray([]byte{0xff}, 8)
	s := v.String()
	if s == "" {
		t.Error("String() should not be empty")
	}
}
