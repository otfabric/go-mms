package mms

import "testing"

func BenchmarkNewInteger(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = NewInteger(int64(i))
	}
}

func BenchmarkNewFloat(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = NewFloat(3.14)
	}
}

func BenchmarkNewVisibleString(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = NewVisibleString("Hello, World!")
	}
}

func BenchmarkNewBoolean(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = NewBoolean(true)
	}
}

func BenchmarkValueInt64Access(b *testing.B) {
	v := NewInteger(42)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = v.Int64()
	}
}

func BenchmarkValueFloat64Access(b *testing.B) {
	v := NewFloat(3.14)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = v.Float64()
	}
}
