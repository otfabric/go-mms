package pdu

import (
	"encoding/asn1"
	"testing"

	"github.com/otfabric/go-mms/internal/berutil"
)

func BenchmarkUnmarshalDataElementBoolean(b *testing.B) {
	data := berutil.EncodeTLV(0x83, []byte{0xff})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = UnmarshalDataElement(data, 0)
	}
}

func BenchmarkUnmarshalDataElementInteger(b *testing.B) {
	data := berutil.EncodeTLV(0x85, []byte{0x00, 0x01, 0x00, 0x00})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = UnmarshalDataElement(data, 0)
	}
}

func BenchmarkUnmarshalDataElementFloat32(b *testing.B) {
	data := berutil.EncodeTLV(0x87, []byte{8, 0x42, 0x28, 0x00, 0x00})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = UnmarshalDataElement(data, 0)
	}
}

func BenchmarkUnmarshalDataElementVisibleString(b *testing.B) {
	data := berutil.EncodeTLV(0x8a, []byte("Hello, World!"))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = UnmarshalDataElement(data, 0)
	}
}

func BenchmarkUnmarshalDataElementStructure(b *testing.B) {
	bool1 := berutil.EncodeTLV(0x83, []byte{0xff})
	int1 := berutil.EncodeTLV(0x85, []byte{42})
	str1 := berutil.EncodeTLV(0x8a, []byte("test"))
	inner := append(append(bool1, int1...), str1...)
	data := berutil.EncodeTLV(0xa2, inner)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = UnmarshalDataElement(data, 0)
	}
}

func BenchmarkUnmarshalAccessResults10(b *testing.B) {
	var elems []byte
	for i := 0; i < 10; i++ {
		elems = append(elems, berutil.EncodeTLV(0x85, []byte{byte(i)})...)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = UnmarshalAccessResults(elems)
	}
}

func BenchmarkDecodeTypeSpecInteger(b *testing.B) {
	data := berutil.EncodeTLV(0x85, []byte{32})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = DecodeTypeSpec(data)
	}
}

func BenchmarkDecodeTypeSpecStructure(b *testing.B) {
	comp1Name := berutil.EncodeTLV(0x80, []byte("enabled"))
	comp1Type := berutil.EncodeTLV(0x83, []byte{0})
	comp1 := berutil.EncodeTLV(0x30, append(comp1Name, comp1Type...))
	comp2Name := berutil.EncodeTLV(0x80, []byte("count"))
	comp2Type := berutil.EncodeTLV(0x86, []byte{16})
	comp2 := berutil.EncodeTLV(0x30, append(comp2Name, comp2Type...))
	components := berutil.EncodeTLV(0xa1, append(comp1, comp2...))
	data := berutil.EncodeTLV(0xa2, components)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = DecodeTypeSpec(data)
	}
}

func BenchmarkDecodeObjectNameDomain(b *testing.B) {
	name, _ := EncodeObjectName(ObjectNameWire{Scope: ScopeDomain, DomainID: "MyDomain", ItemID: "MyVariable"})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = DecodeObjectName(name)
	}
}

func BenchmarkEncodeObjectNameDomain(b *testing.B) {
	wire := ObjectNameWire{Scope: ScopeDomain, DomainID: "MyDomain", ItemID: "MyVariable"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = EncodeObjectName(wire)
	}
}

func BenchmarkDecodeTLV(b *testing.B) {
	data := berutil.EncodeTLV(0x30, make([]byte, 200))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = berutil.DecodeTLV(data)
	}
}

func BenchmarkMarshalData(b *testing.B) {
	dv := &DataValue{Tag: TagDataFloat, Float: 3.14, FloatWide: false}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = MarshalData(dv)
	}
}

func BenchmarkMarshalReadRequest(b *testing.B) {
	vars := []ObjectNameWire{
		{Scope: ScopeDomain, DomainID: "D1", ItemID: "V1"},
		{Scope: ScopeDomain, DomainID: "D1", ItemID: "V2"},
		{Scope: ScopeDomain, DomainID: "D1", ItemID: "V3"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = MarshalReadRequest(1, vars)
	}
}

func BenchmarkUnmarshalReadResponse(b *testing.B) {
	var list []byte
	for i := 0; i < 5; i++ {
		list = append(list, berutil.EncodeTLV(0x85, []byte{byte(i)})...)
	}
	content := berutil.EncodeTLV(0x30, list)
	raw := asn1.RawValue{Tag: 4, Class: 2, IsCompound: true, Bytes: content}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = UnmarshalReadResponse(raw)
	}
}
