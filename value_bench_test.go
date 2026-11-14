// SPDX-License-Identifier: MIT

package mms

import "testing"

func BenchmarkNewInteger(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = NewInteger(42)
	}
}

func BenchmarkNewFloat(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = NewFloat(3.14)
	}
}

func BenchmarkNewVisibleString(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = NewVisibleString("Hello, World!")
	}
}

func BenchmarkNewBoolean(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = NewBoolean(true)
	}
}

func BenchmarkValueInt64Access(b *testing.B) {
	v := NewInteger(42)
	if i, ok := v.Int64(); !ok || i != 42 {
		b.Fatalf("Int64 = %d, %v", i, ok)
	}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = v.Int64()
	}
}

func BenchmarkValueFloat64Access(b *testing.B) {
	v := NewFloat(3.14)
	if f, ok := v.Float64(); !ok || f != 3.14 {
		b.Fatalf("Float64 = %v, %v", f, ok)
	}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = v.Float64()
	}
}
