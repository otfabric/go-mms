// SPDX-License-Identifier: MIT

package pdu

import (
	"fmt"

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

// BER tags for alternate access encoding.
const (
	tagAltAccessWrapper byte = 0xa5 // [5] CONSTRUCTED — alternateAccess in ListOfVariableSeq

	tagSelectAltAccess byte = 0xa0 // [0] CONSTRUCTED — selectAlternateAccess

	tagSAComponent  byte = 0x80 // [0] IMPLICIT — component (inside selectAlternateAccess)
	tagSAIndex      byte = 0x81 // [1] IMPLICIT — index (inside selectAlternateAccess)
	tagSAIndexRange byte = 0xa2 // [2] CONSTRUCTED — indexRange (inside selectAlternateAccess)

	tagSSComponent  byte = 0x81 // [1] IMPLICIT — component (selectAccess)
	tagSSIndex      byte = 0x82 // [2] IMPLICIT — index (selectAccess)
	tagSSIndexRange byte = 0xa3 // [3] CONSTRUCTED — indexRange (selectAccess)
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
		return berutil.EncodeTLV(tagSSIndex, berutil.EncodeUint32(uint32(sel.Index))), nil
	case sel.IndexRange != nil:
		return encodeIndexRange(tagSSIndexRange, sel.IndexRange), nil
	default:
		return nil, fmt.Errorf("empty access selector")
	}
}

// encodeNestedSelector encodes selectAlternateAccess [0] with the
// first selector as the accessSelection, and the remaining selectors
// recursively as a nested AlternateAccess.
func encodeNestedSelector(sel AccessSelectorWire, rest []AccessSelectorWire) ([]byte, error) {
	var accessSel []byte
	switch {
	case sel.Component != "":
		accessSel = berutil.EncodeTLV(tagSAComponent, []byte(sel.Component))
	case sel.HasIndex:
		accessSel = berutil.EncodeTLV(tagSAIndex, berutil.EncodeUint32(uint32(sel.Index)))
	case sel.IndexRange != nil:
		accessSel = encodeIndexRange(tagSAIndexRange, sel.IndexRange)
	default:
		return nil, fmt.Errorf("empty access selector")
	}

	nested, err := encodeAlternateAccess(rest)
	if err != nil {
		return nil, fmt.Errorf("nested alternate access: %w", err)
	}
	nestedWrapped := berutil.EncodeTLV(tagAltAccessWrapper, nested)

	content := make([]byte, 0, len(accessSel)+len(nestedWrapped))
	content = append(content, accessSel...)
	content = append(content, nestedWrapped...)

	return berutil.EncodeTLV(tagSelectAltAccess, content), nil
}

func encodeIndexRange(tag byte, ir *IndexRangeWire) []byte {
	low := berutil.EncodeTLV(0x80, berutil.EncodeUint32(uint32(ir.LowIndex)))
	num := berutil.EncodeTLV(0x81, berutil.EncodeUint32(uint32(ir.NumberOfElements)))
	content := make([]byte, 0, len(low)+len(num))
	content = append(content, low...)
	content = append(content, num...)
	return berutil.EncodeTLV(tag, content)
}

// decodeAlternateAccess decodes an AlternateAccess SEQUENCE OF from
// raw BER content (without the outer [5] wrapper tag).
func decodeAlternateAccess(data []byte) ([]AccessSelectorWire, error) {
	var chain []AccessSelectorWire
	offset := 0

	for offset < len(data) {
		tag, content, n, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, fmt.Errorf("alternate access element: %w", err)
		}
		offset += n

		switch tag {
		case tagSelectAltAccess:
			sels, err := decodeSelectAlternateAccess(content)
			if err != nil {
				return nil, err
			}
			chain = append(chain, sels...)
		case tagSSComponent:
			chain = append(chain, AccessSelectorWire{Component: string(content)})
		case tagSSIndex:
			idx, err := berutil.DecodeUnsigned(content)
			if err != nil {
				return nil, fmt.Errorf("selectAccess index: %w", err)
			}
			chain = append(chain, AccessSelectorWire{HasIndex: true, Index: int(idx)})
		case tagSSIndexRange:
			ir, err := decodeIndexRange(content)
			if err != nil {
				return nil, fmt.Errorf("selectAccess indexRange: %w", err)
			}
			chain = append(chain, AccessSelectorWire{IndexRange: ir})
		default:
			return nil, fmt.Errorf("unexpected alternate access tag 0x%02x", tag)
		}
	}

	return chain, nil
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
		sel = AccessSelectorWire{Component: string(content)}
	case tagSAIndex:
		idx, err := berutil.DecodeUnsigned(content)
		if err != nil {
			return nil, fmt.Errorf("selectAlternateAccess index: %w", err)
		}
		sel = AccessSelectorWire{HasIndex: true, Index: int(idx)}
	case tagSAIndexRange:
		ir, err := decodeIndexRange(content)
		if err != nil {
			return nil, fmt.Errorf("selectAlternateAccess indexRange: %w", err)
		}
		sel = AccessSelectorWire{IndexRange: ir}
	default:
		return nil, fmt.Errorf("selectAlternateAccess: unexpected accessSelection tag 0x%02x", tag)
	}

	chain := []AccessSelectorWire{sel}

	if offset < len(data) {
		tag2, content2, n2, err := berutil.DecodeTLVAt(data, offset)
		if err != nil {
			return nil, fmt.Errorf("selectAlternateAccess nested: %w", err)
		}
		offset += n2
		if tag2 != tagAltAccessWrapper {
			return nil, fmt.Errorf("selectAlternateAccess: expected nested [5] (0xa5), got 0x%02x", tag2)
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
			low = int(v)
			gotLow = true
		case 0x81:
			v, err := berutil.DecodeUnsigned(content)
			if err != nil {
				return nil, fmt.Errorf("indexRange numberOfElements: %w", err)
			}
			num = int(v)
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
