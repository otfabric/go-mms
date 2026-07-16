// SPDX-License-Identifier: MIT

package pdu

import (
	"encoding/asn1"
	"testing"

	"github.com/otfabric/go-mms/internal/berutil"
)

// TestMalformedDataElements verifies that corrupt or truncated Data elements
// produce errors rather than panics or silent bad values.
func TestMalformedDataElements(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"single_byte", []byte{0x83}},
		{"truncated_length", []byte{0x85, 0x82}},
		{"truncated_content", []byte{0x85, 0x04, 0x01, 0x02}},
		{"boolean_too_long", berutil.EncodeTLV(0x83, []byte{0x01, 0x02})},
		{"unknown_tag", berutil.EncodeTLV(0xfe, []byte{0x01})},
		{"float_empty", berutil.EncodeTLV(0x87, nil)},
		{"float_wrong_exp_width", berutil.EncodeTLV(0x87, []byte{5, 0x00, 0x00, 0x00})},
		{"utctime_short", berutil.EncodeTLV(0x91, []byte{0x01, 0x02, 0x03})},
		{"binarytime_wrong_len", berutil.EncodeTLV(0x8c, []byte{0x01, 0x02, 0x03, 0x04, 0x05})},
		{"bitstring_empty", berutil.EncodeTLV(0x84, nil)},
		{"bitstring_bad_unused", berutil.EncodeTLV(0x84, []byte{0x09, 0xff})},
		{"integer_too_large", berutil.EncodeTLV(0x85, make([]byte, 20))},
		{"unsigned_too_large", berutil.EncodeTLV(0x86, make([]byte, 20))},
		{"nested_array_truncated", berutil.EncodeTLV(0xa1, []byte{0x83, 0x05})},
		{"struct_child_corrupt", berutil.EncodeTLV(0xa2, []byte{0xff, 0xff, 0xff})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := UnmarshalDataElement(tt.data, 0)
			if err == nil {
				t.Error("expected error for malformed data element")
			}
		})
	}
}

// TestMalformedReadResponse verifies Read response handling of corrupt inputs.
func TestMalformedReadResponse(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"wrong_outer_tag", []byte{0x31, 0x00}},
		{"truncated_list", []byte{0x30, 0x05, 0x83}},
		{"trailing_bytes", append(berutil.EncodeTLV(0x30, berutil.EncodeTLV(0x83, []byte{0xff})), 0xde, 0xad)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := asn1.RawValue{Tag: 4, Class: 2, IsCompound: true, Bytes: tt.data}
			_, err := UnmarshalReadResponse(raw)
			if err == nil {
				t.Error("expected error for malformed read response")
			}
		})
	}
}

// TestMalformedWriteResponse verifies Write response handling of corrupt inputs.
func TestMalformedWriteResponse(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"truncated", []byte{0x81}},
		{"unknown_choice", berutil.EncodeTLV(0x82, []byte{0x00})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := asn1.RawValue{Tag: 5, Class: 2, IsCompound: true, Bytes: tt.data}
			_, err := UnmarshalWriteResponse(raw)
			if err == nil {
				t.Error("expected error for malformed write response")
			}
		})
	}
}

// TestMalformedGetNameListResponse verifies GetNameList response handling.
func TestMalformedGetNameListResponse(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"missing_list", []byte{0x01, 0x00}},
		{"truncated_list", []byte{0xa0, 0x05, 0x30}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := asn1.RawValue{Tag: 1, Class: 2, IsCompound: true, Bytes: tt.data}
			_, err := UnmarshalGetNameListResponse(raw)
			if err == nil {
				t.Error("expected error for malformed getnamelist response")
			}
		})
	}
}

// TestMalformedGetVarAccessResponse verifies GetVarAccess response handling.
func TestMalformedGetVarAccessResponse(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"missing_typespec", berutil.EncodeTLV(0x80, []byte{0x00})},
		{"wrong_typespec_tag", append(berutil.EncodeTLV(0x80, []byte{0x00}), berutil.EncodeTLV(0xa3, []byte{0x00})...)},
		{"truncated_typespec", append(berutil.EncodeTLV(0x80, []byte{0x00}), 0xa2, 0x05, 0x83)},
		{
			"trailing_junk",
			append(
				append(berutil.EncodeTLV(0x80, []byte{0x00}), berutil.EncodeTLV(0xa2, berutil.EncodeTLV(0x83, []byte{0}))...),
				0xde, 0xad,
			),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := asn1.RawValue{Tag: 6, Class: 2, IsCompound: true, Bytes: tt.data}
			_, err := UnmarshalGetVarAccessResponse(raw)
			if err == nil {
				t.Error("expected error for malformed getvaraccess response")
			}
		})
	}
}

// TestMalformedTypeSpec verifies TypeSpecification decoding of invalid inputs.
func TestMalformedTypeSpec(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"truncated_tag", []byte{0x85}},
		{"float_missing_exp", berutil.EncodeTLV(0xa7, berutil.EncodeTLV(0x02, []byte{32}))},
		{"array_missing_element", berutil.EncodeTLV(0xa1, berutil.EncodeTLV(0x80, []byte{10}))},
		{"struct_no_components", berutil.EncodeTLV(0xa2, nil)},
		{"unknown_tag", berutil.EncodeTLV(0x9f, []byte{0x01})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeTypeSpec(tt.data)
			if err == nil {
				t.Error("expected error for malformed typespec")
			}
		})
	}
}

// TestMalformedObjectName verifies ObjectName decoding of invalid inputs.
func TestMalformedObjectName(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"unknown_tag", berutil.EncodeTLV(0x84, []byte("test"))},
		{"domain_truncated", berutil.EncodeTLV(0xa1, []byte{0x1a, 0x05})},
		{"domain_missing_item", berutil.EncodeTLV(0xa1, berutil.EncodeTLV(0x1a, []byte("dom")))},
		{"domain_wrong_tag", berutil.EncodeTLV(0xa1, append(berutil.EncodeTLV(0x1a, []byte("dom")), berutil.EncodeTLV(0x02, []byte{0x01})...))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeObjectName(tt.data)
			if err == nil {
				t.Error("expected error for malformed object name")
			}
		})
	}
}

// TestMalformedConfirmedError verifies ConfirmedError decoding.
func TestMalformedConfirmedError(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"truncated_invokeID", []byte{0x80, 0x05, 0x01}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeConfirmedError(tt.data)
			if err == nil && len(tt.data) > 2 {
				t.Error("expected error for malformed confirmed error")
			}
		})
	}
}

// TestMalformedRejectPDU verifies RejectPDU decoding.
func TestMalformedRejectPDU(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"truncated_invoke", []byte{0x80, 0x05, 0x01}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeRejectPDU(tt.data)
			if err == nil && len(tt.data) > 2 {
				t.Error("expected error for malformed reject PDU")
			}
		})
	}
}

// TestMalformedDeleteNamedVarListResponse verifies the delete response decoder.
func TestMalformedDeleteNamedVarListResponse(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"single_integer", berutil.EncodeTLV(0x02, []byte{0x01})},
		{"wrong_tag", append(berutil.EncodeTLV(0x02, []byte{0x01}), berutil.EncodeTLV(0x04, []byte{0x01})...)},
		{"negative_matched", append(berutil.EncodeTLV(0x02, []byte{0xff}), berutil.EncodeTLV(0x02, []byte{0x01})...)},
		{
			"trailing_junk",
			append(append(berutil.EncodeTLV(0x02, []byte{0x01}), berutil.EncodeTLV(0x02, []byte{0x01})...), 0xde),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := asn1.RawValue{Tag: 13, Class: 2, IsCompound: true, Bytes: tt.data}
			_, err := UnmarshalDeleteNamedVarListResponse(raw)
			if err == nil {
				t.Error("expected error for malformed delete response")
			}
		})
	}
}

// TestMalformedNamedVarListAttrsResponse verifies the attrs response decoder.
func TestMalformedNamedVarListAttrsResponse(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"missing_list", berutil.EncodeTLV(0x80, []byte{0x00})},
		{"wrong_list_tag", append(berutil.EncodeTLV(0x80, []byte{0x00}), berutil.EncodeTLV(0xa2, nil)...)},
		{
			"trailing_junk",
			append(
				append(
					berutil.EncodeTLV(0x80, []byte{0x00}),
					berutil.EncodeTLV(0xa1, nil)...,
				),
				0xde,
			),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := asn1.RawValue{Tag: 12, Class: 2, IsCompound: true, Bytes: tt.data}
			_, err := UnmarshalGetNamedVarListAttrsResponse(raw)
			if err == nil {
				t.Error("expected error for malformed named var list attrs response")
			}
		})
	}
}
