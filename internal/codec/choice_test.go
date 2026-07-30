// SPDX-License-Identifier: MIT

package codec

import (
	"encoding/asn1"
	"testing"

	"github.com/otfabric/go-mms/internal/berutil"
)

func TestUnmarshalImplicitSequence_Errors(t *testing.T) {
	var target struct {
		N int `asn1:"tag:0,implicit"`
	}
	err := UnmarshalImplicitSequence(asn1.RawValue{
		Class: asn1.ClassContextSpecific, Tag: 1, IsCompound: false, Bytes: nil,
	}, &target)
	if err == nil {
		t.Fatal("expected primitive error")
	}

	err = UnmarshalImplicitSequence(asn1.RawValue{
		Class: asn1.ClassContextSpecific, Tag: 1, IsCompound: true, Bytes: []byte{0xff},
	}, &target)
	if err == nil {
		t.Fatal("expected unmarshal error")
	}

}

func TestUnmarshalExplicit_Errors(t *testing.T) {
	var target struct {
		N int `asn1:"tag:0,implicit"`
	}
	err := UnmarshalExplicit(asn1.RawValue{
		Class: asn1.ClassContextSpecific, Tag: 1, IsCompound: false, Bytes: nil,
	}, &target)
	if err == nil {
		t.Fatal("expected primitive error")
	}
	err = UnmarshalExplicit(asn1.RawValue{
		Class: asn1.ClassContextSpecific, Tag: 1, IsCompound: true, Bytes: []byte{0xff},
	}, &target)
	if err == nil {
		t.Fatal("expected unmarshal error")
	}

	inner := berutil.EncodeTLV(0x80, berutil.EncodeInt(7))
	seq := berutil.EncodeTLV(0x30, inner)
	withTrail := append(append([]byte{}, seq...), 0x05, 0x00)
	err = UnmarshalExplicit(asn1.RawValue{
		Class: asn1.ClassContextSpecific, Tag: 1, IsCompound: true, Bytes: withTrail,
	}, &target)
	if err == nil {
		t.Fatal("expected trailing-bytes error")
	}
}

func TestUnmarshalFull_Errors(t *testing.T) {
	var n int
	err := UnmarshalFull(asn1.RawValue{FullBytes: []byte{0xff}}, &n)
	if err == nil {
		t.Fatal("expected unmarshal error")
	}

	ok := berutil.EncodeTLV(0x02, berutil.EncodeInt(3))
	withTrail := append(append([]byte{}, ok...), 0x05, 0x00)
	err = UnmarshalFull(asn1.RawValue{FullBytes: withTrail}, &n)
	if err == nil {
		t.Fatal("expected trailing-bytes error")
	}
}
