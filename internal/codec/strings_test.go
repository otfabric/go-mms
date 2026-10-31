// SPDX-License-Identifier: MIT

package codec

import (
	"encoding/asn1"
	"errors"
	"reflect"
	"testing"
	"time"
)

const invalidUTF8 = "000\xa5"

// stringShapes holds every shape checkStrings descends into.
type stringShapes struct {
	Plain   string
	Pointer *string
	Any     any
	List    []string
	Nested  struct{ Name string }
	Bytes   []byte
	When    time.Time
	hidden  string
}

func TestCheckStrings(t *testing.T) {
	bad := invalidUTF8
	good := "LLN0"
	tests := []struct {
		name  string
		value any
		want  error
	}{
		{"nil", nil, nil},
		{"non-string", 5, nil},
		{"valid string", "é", nil},
		{"zero struct", stringShapes{}, nil},
		{"valid everywhere", stringShapes{Plain: good, Pointer: &good, Any: good, List: []string{good}, Bytes: []byte{0xff}}, nil},
		{"non-UTF-8 bytes are not a string", []byte(bad), nil},
		{"unexported field", stringShapes{hidden: bad}, nil},
		{"invalid string", bad, errInvalidString},
		{"invalid field", stringShapes{Plain: bad}, errInvalidString},
		{"invalid pointer", stringShapes{Pointer: &bad}, errInvalidString},
		{"invalid interface", stringShapes{Any: bad}, errInvalidString},
		{"invalid list element", stringShapes{List: []string{good, bad}}, errInvalidString},
		{"invalid nested", stringShapes{Nested: struct{ Name string }{bad}}, errInvalidString},
		{"pointer to struct", &stringShapes{Plain: bad}, errInvalidString},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := checkStrings(reflect.ValueOf(tt.value)); !errors.Is(err, tt.want) {
				t.Fatalf("checkStrings = %v, want %v", err, tt.want)
			}
		})
	}
}

// The encoders refuse what the decoders refuse: encoding/asn1 alone
// would emit this value as a UTF8String with invalid content.
func TestMarshal_RejectsInvalidUTF8(t *testing.T) {
	v := fuzzExplicit{Name: invalidUTF8, N: 1}
	if _, err := MarshalSequenceContent(v); !errors.Is(err, errInvalidString) {
		t.Errorf("MarshalSequenceContent = %v, want errInvalidString", err)
	}
	if _, err := MarshalMmsPdu(0xa8, v); !errors.Is(err, errInvalidString) {
		t.Errorf("MarshalMmsPdu = %v, want errInvalidString", err)
	}
	if _, err := MarshalMmsPduBareSequence(0xa8, v); !errors.Is(err, errInvalidString) {
		t.Errorf("MarshalMmsPduBareSequence = %v, want errInvalidString", err)
	}
}

// optionalName keeps a target's prior Name when the wire omits it, which
// puts an invalid string behind each decoder on every Go version;
// encoding/asn1 stopped producing one from the wire in Go 1.25.
type optionalName struct {
	N    int
	Name string `asn1:"optional,utf8"`
}

func TestUnmarshal_RejectsInvalidUTF8(t *testing.T) {
	inner, err := asn1.Marshal(optionalName{N: 1})
	if err != nil {
		t.Fatal(err)
	}
	content, err := MarshalSequenceContent(optionalName{N: 1})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		decode func(target any) error
	}{
		{"implicit", func(target any) error {
			return UnmarshalImplicitSequence(asn1.RawValue{IsCompound: true, Bytes: content}, target)
		}},
		{"explicit", func(target any) error {
			return UnmarshalExplicit(asn1.RawValue{IsCompound: true, Bytes: inner}, target)
		}},
		{"full", func(target any) error {
			return UnmarshalFull(asn1.RawValue{FullBytes: inner}, target)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.decode(&optionalName{Name: invalidUTF8}); !errors.Is(err, errInvalidString) {
				t.Fatalf("decode = %v, want errInvalidString", err)
			}
			var ok optionalName
			if err := tt.decode(&ok); err != nil || ok.N != 1 {
				t.Fatalf("decode = %+v, %v", ok, err)
			}
		})
	}
}

// A T61String or GeneralString in a string field either decodes to valid
// UTF-8 (Latin-1 mapping, Go 1.25 and later) or is refused (earlier Go,
// which copies the bytes); it never yields a value that cannot re-encode.
func TestUnmarshalExplicit_EightBitStrings(t *testing.T) {
	for _, tag := range []byte{asn1.TagT61String, asn1.TagGeneralString} {
		inner := []byte{0x30, 0x09, tag, 0x04, '0', '0', '0', 0xa5, 0x02, 0x01, 0x30}
		var e fuzzExplicit
		err := UnmarshalExplicit(asn1.RawValue{IsCompound: true, Bytes: inner}, &e)
		if err != nil {
			if !errors.Is(err, errInvalidString) {
				t.Errorf("tag %d: %v, want errInvalidString", tag, err)
			}
			continue
		}
		if _, err := MarshalSequenceContent(e); err != nil {
			t.Errorf("tag %d: decoded %+v does not re-encode: %v", tag, e, err)
		}
	}
}
