// SPDX-License-Identifier: MIT

package codec

import (
	"encoding/asn1"
	"testing"
)

func TestUnwrapPdu_RejectsLongFormTag(t *testing.T) {
	// Context tag [40], constructed: must not be read as [8] InitiateRequest.
	if _, _, err := UnwrapPdu([]byte{0xbf, 0x28, 0x00}); err == nil {
		t.Fatal("UnwrapPdu accepted a long-form tag")
	}
}

func TestServiceTag_LongForm(t *testing.T) {
	raw := asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 72, IsCompound: true}
	if got := ServiceTag(raw); got != 0xbf {
		t.Fatalf("ServiceTag([72]) = 0x%02x, want 0xbf", got)
	}
	raw.Tag = 4
	if got := ServiceTag(raw); got != 0xa4 {
		t.Fatalf("ServiceTag([4]) = 0x%02x, want 0xa4", got)
	}
}
