package codec

import (
	"encoding/asn1"
	"fmt"
)

// ServiceTag reconstructs the single-byte BER tag from an asn1.RawValue
// parsed from inside a confirmed request/response envelope.
func ServiceTag(raw asn1.RawValue) byte {
	tag := byte(raw.Class<<6) | byte(raw.Tag&0x1f)
	if raw.IsCompound {
		tag |= 0x20
	}
	return tag
}

// UnmarshalInner unmarshals the inner content bytes of a constructed
// asn1.RawValue into the target struct. Use this after CHOICE dispatch
// when the selected branch is a constructed (SEQUENCE) value and you
// want to decode the content within the outer tag.
//
// This is the common case for MMS service response bodies.
func UnmarshalInner(raw asn1.RawValue, target any) error {
	if !raw.IsCompound {
		return fmt.Errorf("codec: UnmarshalInner called on primitive value (tag 0x%02x)", ServiceTag(raw))
	}
	rest, err := asn1.Unmarshal(raw.Bytes, target)
	if err != nil {
		return fmt.Errorf("codec: unmarshal inner: %w", err)
	}
	if len(rest) != 0 {
		return fmt.Errorf("codec: unmarshal inner: %d trailing bytes", len(rest))
	}
	return nil
}

// UnmarshalFull unmarshals the full TLV (tag + length + value) of an
// asn1.RawValue into the target. Use this for primitive CHOICE branches
// where the tag itself carries meaning and the stdlib needs the complete
// encoding to determine the type.
func UnmarshalFull(raw asn1.RawValue, target any) error {
	rest, err := asn1.Unmarshal(raw.FullBytes, target)
	if err != nil {
		return fmt.Errorf("codec: unmarshal full: %w", err)
	}
	if len(rest) != 0 {
		return fmt.Errorf("codec: unmarshal full: %d trailing bytes", len(rest))
	}
	return nil
}
