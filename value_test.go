package mms

import (
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
