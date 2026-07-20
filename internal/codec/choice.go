// SPDX-License-Identifier: MIT

package codec

import (
	"encoding/asn1"
	"fmt"

	"github.com/otfabric/go-mms/internal/berutil"
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

// UnmarshalImplicitSequence decodes the content of a constructed
// asn1.RawValue that carries an IMPLICIT SEQUENCE tag. The surrounding
// context-specific tag replaces the universal SEQUENCE tag (0x30), so
// raw.Bytes contains the SEQUENCE fields directly. This function
// reconstructs the 0x30 wrapper that Go's encoding/asn1 requires when
// decoding into a struct.
//
// Use this whenever the wire contract is "IMPLICIT" and the outer tag
// is a context-specific constructed tag.
func UnmarshalImplicitSequence(raw asn1.RawValue, target any) error {
	if !raw.IsCompound {
		return fmt.Errorf("codec: implicit sequence tag 0x%02x is primitive", ServiceTag(raw))
	}
	sequence := berutil.EncodeTLV(0x30, raw.Bytes)
	rest, err := asn1.Unmarshal(sequence, target)
	if err != nil {
		return fmt.Errorf("codec: unmarshal implicit sequence: %w", err)
	}
	if len(rest) != 0 {
		return fmt.Errorf("codec: %d trailing bytes after implicit sequence", len(rest))
	}
	return nil
}

// UnmarshalExplicit decodes the content of a constructed asn1.RawValue
// that carries an EXPLICIT tag. The outer context-specific tag wraps the
// inner encoding, so raw.Bytes starts with the full TLV of the inner type
// (e.g. 0x30 for a SEQUENCE struct). Go's encoding/asn1 can decode
// raw.Bytes directly.
//
// Use this whenever the wire contract is "EXPLICIT".
func UnmarshalExplicit(raw asn1.RawValue, target any) error {
	if !raw.IsCompound {
		return fmt.Errorf("codec: explicit wrapper tag 0x%02x is primitive", ServiceTag(raw))
	}
	rest, err := asn1.Unmarshal(raw.Bytes, target)
	if err != nil {
		return fmt.Errorf("codec: unmarshal explicit: %w", err)
	}
	if len(rest) != 0 {
		return fmt.Errorf("codec: %d trailing bytes after explicit wrapper", len(rest))
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
