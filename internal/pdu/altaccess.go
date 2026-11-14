// SPDX-License-Identifier: MIT

package pdu

import (
	"errors"
	"fmt"
	"math"

	"github.com/otfabric/go-mms/internal/berutil"
)

// AccessSelectorWire is the internal wire representation of one level
// of alternate access selection.
type AccessSelectorWire struct {
	Component  string
	HasIndex   bool
	Index      int
	IndexRange *IndexRangeWire
}

// IndexRangeWire holds the wire representation of an array index range.
type IndexRangeWire struct {
	LowIndex         int
	NumberOfElements int
}

// VariableSpecWire is an ObjectName with optional alternate access.
type VariableSpecWire struct {
	Name            ObjectNameWire
	AlternateAccess []AccessSelectorWire
}

// ErrUnsupportedAlternateAccess reports a well-formed AlternateAccess
// this implementation does not model: several sibling selections in one
// SEQUENCE OF (which select several components into a new structure),
// the named [5] alternative, or allElements. A server answers it with
// access/object-access-unsupported.
var ErrUnsupportedAlternateAccess = errors.New("pdu: unsupported alternate access")

// BER tags for alternate access encoding (ISO 9506-2 clause 14.2.2.9).
const (
	// tagAltAccessWrapper is the [5] IMPLICIT tag of AlternateAccess
	// where VariableAccessSpecification (clause 14.2.3) and
	// DefineNamedVariableList reference it; inside an AlternateAccess
	// SEQUENCE OF the same tag is the named alternative.
	tagAltAccessWrapper byte = 0xa5

	// tagNestedAltAccess is the untagged AlternateAccess (universal
	// SEQUENCE OF) inside selectAlternateAccess.
	tagNestedAltAccess byte = 0x30

	tagSelectAltAccess byte = 0xa0 // [0] CONSTRUCTED — selectAlternateAccess

	tagSAComponent  byte = 0x80 // [0] IMPLICIT — component (inside selectAlternateAccess)
	tagSAIndex      byte = 0x81 // [1] IMPLICIT — index (inside selectAlternateAccess)
	tagSAIndexRange byte = 0xa2 // [2] CONSTRUCTED — indexRange (inside selectAlternateAccess)

	tagSAAllElements byte = 0x83 // [3] IMPLICIT NULL — allElements (inside selectAlternateAccess)

	tagSSComponent   byte = 0x81 // [1] IMPLICIT — component (selectAccess)
	tagSSIndex       byte = 0x82 // [2] IMPLICIT — index (selectAccess)
	tagSSIndexRange  byte = 0xa3 // [3] CONSTRUCTED — indexRange (selectAccess)
	tagSSAllElements byte = 0x84 // [4] IMPLICIT NULL — allElements (selectAccess)
)

// encodeAlternateAccess encodes a chain of AccessSelectorWire values
// into a BER AlternateAccess (SEQUENCE OF AlternateAccessSelection).
// The last selector uses selectAccess (terminal); preceding selectors
// use selectAlternateAccess (recursive, wrapping the rest).
func encodeAlternateAccess(selectors []AccessSelectorWire) ([]byte, error) {
	if len(selectors) == 0 {
		return nil, fmt.Errorf("empty alternate access chain")
	}

	if len(selectors) == 1 {
		return encodeTerminalSelector(selectors[0])
	}

	return encodeNestedSelector(selectors[0], selectors[1:])
}

// encodeTerminalSelector encodes a single selectAccess choice.
func encodeTerminalSelector(sel AccessSelectorWire) ([]byte, error) {
	switch {
	case sel.Component != "":
		return berutil.EncodeTLV(tagSSComponent, []byte(sel.Component)), nil
	case sel.HasIndex:
		idx, err := encodeUnsigned32(sel.Index, "index")
		if err != nil {
			return nil, err
		}
		return berutil.EncodeTLV(tagSSIndex, idx), nil
	case sel.IndexRange != nil:
		return encodeIndexRange(tagSSIndexRange, sel.IndexRange)
	default:
		return nil, fmt.Errorf("empty access selector")
	}
}

// encodeNestedSelector encodes selectAlternateAccess [0] with the
// first selector as the accessSelection, and the remaining selectors
// recursively as the nested AlternateAccess, which is untagged there: a
// universal SEQUENCE OF, 0x30 (ISO 9506-2 clause 14.2.2.9; libiec61850's
// asn1c encoder agrees). Releases up to v1.7.2 wrapped it in [5] (0xa5),
// which a conformant peer reads as the named alternative (MMS-PDU-05).
func encodeNestedSelector(sel AccessSelectorWire, rest []AccessSelectorWire) ([]byte, error) {
	var accessSel []byte
	switch {
	case sel.Component != "":
		accessSel = berutil.EncodeTLV(tagSAComponent, []byte(sel.Component))
	case sel.HasIndex:
		idx, err := encodeUnsigned32(sel.Index, "index")
		if err != nil {
			return nil, err
		}
		accessSel = berutil.EncodeTLV(tagSAIndex, idx)
	case sel.IndexRange != nil:
		var err error
		if accessSel, err = encodeIndexRange(tagSAIndexRange, sel.IndexRange); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("empty access selector")
	}

	nested, err := encodeAlternateAccess(rest)
	if err != nil {
		return nil, fmt.Errorf("nested alternate access: %w", err)
	}
	nestedWrapped := berutil.EncodeTLV(tagNestedAltAccess, nested)

	content := make([]byte, 0, len(accessSel)+len(nestedWrapped))
	content = append(content, accessSel...)
	content = append(content, nestedWrapped...)

	return berutil.EncodeTLV(tagSelectAltAccess, content), nil
}

func encodeIndexRange(tag byte, ir *IndexRangeWire) ([]byte, error) {
	lowV, err := encodeUnsigned32(ir.LowIndex, "lowIndex")
	if err != nil {
		return nil, err
	}
	numV, err := encodeUnsigned32(ir.NumberOfElements, "numberOfElements")
	if err != nil {
		return nil, err
	}
	low := berutil.EncodeTLV(0x80, lowV)
	num := berutil.EncodeTLV(0x81, numV)
	content := make([]byte, 0, len(low)+len(num))
	content = append(content, low...)
	content = append(content, num...)
	return berutil.EncodeTLV(tag, content), nil
}

// decodeAlternateAccess decodes an AlternateAccess SEQUENCE OF from
// raw BER content (without the outer [5] wrapper tag) into a selection
// chain: each level is one AlternateAccessSelection, a
// selectAlternateAccess descending into its nested AlternateAccess.
//
// ISO 9506-2 clause 14.2.2.9: the SEQUENCE OF elements are sibling
// selections, not a path. Exactly one element is modelled; several
// siblings, the named [5] alternative and allElements yield
// ErrUnsupportedAlternateAccess (MMS-PDU-05).
func decodeAlternateAccess(data []byte) ([]AccessSelectorWire, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty alternate access")
	}
	tag, content, n, err := berutil.DecodeTLVAt(data, 0)
	if err != nil {
		return nil, fmt.Errorf("alternate access element: %w", err)
	}
	if n != len(data) {
		if _, _, _, err := berutil.DecodeTLVAt(data, n); err != nil {
			return nil, fmt.Errorf("alternate access element: %w", err)
		}
		return nil, fmt.Errorf("%w: several sibling selections", ErrUnsupportedAlternateAccess)
	}

	switch tag {
	case tagSelectAltAccess:
		return decodeSelectAlternateAccess(content)
	case tagSSComponent:
		name, err := decodeComponentName(content)
		if err != nil {
			return nil, err
		}
		return []AccessSelectorWire{{Component: name}}, nil
	case tagSSIndex:
		idx, err := berutil.DecodeUnsigned(content)
		if err != nil {
			return nil, fmt.Errorf("selectAccess index: %w", err)
		}
		i, err := indexFromUnsigned32(idx, "selectAccess index")
		if err != nil {
			return nil, err
		}
		return []AccessSelectorWire{{HasIndex: true, Index: i}}, nil
	case tagSSIndexRange:
		ir, err := decodeIndexRange(content)
		if err != nil {
			return nil, fmt.Errorf("selectAccess indexRange: %w", err)
		}
		return []AccessSelectorWire{{IndexRange: ir}}, nil
	case tagSSAllElements:
		return nil, fmt.Errorf("%w: allElements", ErrUnsupportedAlternateAccess)
	case tagAltAccessWrapper:
		return nil, fmt.Errorf("%w: named alternate access", ErrUnsupportedAlternateAccess)
	default:
		return nil, fmt.Errorf("unexpected alternate access tag 0x%02x", tag)
	}
}

func decodeComponentName(content []byte) (string, error) {
	if len(content) == 0 {
		return "", fmt.Errorf("empty component name")
	}
	if len(content) > maxIdentifierLen {
		return "", fmt.Errorf("component name length %d exceeds maximum %d", len(content), maxIdentifierLen)
	}
	return string(content), nil
}

// decodeSelectAlternateAccess decodes selectAlternateAccess [0] content.
func decodeSelectAlternateAccess(data []byte) ([]AccessSelectorWire, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("selectAlternateAccess: empty content")
	}

	offset := 0
	tag, content, n, err := berutil.DecodeTLVAt(data, offset)
	if err != nil {
		return nil, fmt.Errorf("selectAlternateAccess accessSelection: %w", err)
	}
	offset += n

	var sel AccessSelectorWire
	switch tag {
	case tagSAComponent:
		name, err := decodeComponentName(content)
		if err != nil {
			return nil, fmt.Errorf("selectAlternateAccess component: %w", err)
		}
		sel = AccessSelectorWire{Component: name}
	case tagSAIndex:
		idx, err := berutil.DecodeUnsigned(content)
		if err != nil {
			return nil, fmt.Errorf("selectAlternateAccess index: %w", err)
		}
		i, err := indexFromUnsigned32(idx, "selectAlternateAccess index")
		if err != nil {
			return nil, err
		}
		sel = AccessSelectorWire{HasIndex: true, Index: i}
	case tagSAIndexRange:
		ir, err := decodeIndexRange(content)
		if err != nil {
			return nil, fmt.Errorf("selectAlternateAccess indexRange: %w", err)
		}
		sel = AccessSelectorWire{IndexRange: ir}
	case tagSAAllElements:
		return nil, fmt.Errorf("%w: allElements", ErrUnsupportedAlternateAccess)
	default:
		return nil, fmt.Errorf("selectAlternateAccess: unexpected accessSelection tag 0x%02x", tag)
	}

	chain := []AccessSelectorWire{sel}

	// The nested alternateAccess is mandatory and untagged (0x30). A [5]
	// (0xa5) wrapper, as go-mms up to v1.7.2 sent it, cannot be any valid
	// component here and is accepted for compatibility with those peers.
	if offset >= len(data) {
		return nil, fmt.Errorf("selectAlternateAccess: missing nested alternateAccess")
	}
	{
		tag2, content2, n2, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, fmt.Errorf("selectAlternateAccess nested: %w", err)
		}
		offset += n2
		if tag2 != tagNestedAltAccess && tag2 != tagAltAccessWrapper {
			return nil, fmt.Errorf("selectAlternateAccess: expected nested SEQUENCE OF (0x30), got 0x%02x", tag2)
		}
		nested, err := decodeAlternateAccess(content2)
		if err != nil {
			return nil, fmt.Errorf("selectAlternateAccess nested: %w", err)
		}
		chain = append(chain, nested...)
	}

	if offset != len(data) {
		return nil, fmt.Errorf("selectAlternateAccess: %d trailing bytes", len(data)-offset)
	}

	return chain, nil
}

func decodeIndexRange(data []byte) (*IndexRangeWire, error) {
	offset := 0
	var low, num int
	var gotLow, gotNum bool

	for offset < len(data) {
		tag, content, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, fmt.Errorf("indexRange field: %w", err)
		}
		offset += n

		switch tag {
		case 0x80:
			v, err := berutil.DecodeUnsigned(content)
			if err != nil {
				return nil, fmt.Errorf("indexRange lowIndex: %w", err)
			}
			if low, err = indexFromUnsigned32(v, "indexRange lowIndex"); err != nil {
				return nil, err
			}
			gotLow = true
		case 0x81:
			v, err := berutil.DecodeUnsigned(content)
			if err != nil {
				return nil, fmt.Errorf("indexRange numberOfElements: %w", err)
			}
			if num, err = indexFromUnsigned32(v, "indexRange numberOfElements"); err != nil {
				return nil, err
			}
			gotNum = true
		default:
			return nil, fmt.Errorf("indexRange: unexpected tag 0x%02x", tag)
		}
	}

	if !gotLow || !gotNum {
		return nil, fmt.Errorf("indexRange: missing required field(s)")
	}

	return &IndexRangeWire{LowIndex: low, NumberOfElements: num}, nil
}

// indexFromUnsigned32 converts an Unsigned32 index or count from the wire to
// an int. The whole Unsigned32 range fits a 64-bit int; on a 32-bit platform
// a value of 2^31 or more would turn negative, so it is refused there.
func indexFromUnsigned32(v uint32, field string) (int, error) {
	if uint64(v) > uint64(math.MaxInt) {
		return 0, fmt.Errorf("%s %d does not fit an int on this platform", field, v)
	}
	return int(v), nil
}

// encodeUnsigned32 encodes v as Unsigned32 content (ISO 9506-2 Unsigned32).
// A negative or wider value is an error instead of a wrapped index.
func encodeUnsigned32(v int, field string) ([]byte, error) {
	if v < 0 || int64(v) > math.MaxUint32 {
		return nil, fmt.Errorf("alternate access %s %d out of Unsigned32 range", field, v)
	}
	return berutil.EncodeUint32(uint32(v)), nil
}
