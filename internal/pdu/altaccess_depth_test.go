// SPDX-License-Identifier: MIT

package pdu

import (
	"runtime"
	"slices"
	"testing"

	"github.com/otfabric/go-mms/internal/berutil"
)

// appendReversedHeader appends the tag and definite length of a TLV with
// n content octets to rev, a buffer built back to front.
func appendReversedHeader(rev []byte, tag byte, n int) []byte {
	var l []byte
	switch {
	case n < 0x80:
		l = []byte{byte(n)}
	case n <= 0xff:
		l = []byte{0x81, byte(n)}
	case n <= 0xffff:
		l = []byte{0x82, byte(n >> 8), byte(n)}
	default:
		l = []byte{0x83, byte(n >> 16), byte(n >> 8), byte(n)}
	}
	for i := len(l) - 1; i >= 0; i-- {
		rev = append(rev, l[i])
	}
	return append(rev, tag)
}

// nestedSelection returns AlternateAccess content of the given number of
// levels: levels-1 selectAlternateAccess components around a terminal
// selectAccess component. It is built back to front in linear time.
func nestedSelection(levels int) []byte {
	rev := []byte{'c', 0x01, tagSSComponent}
	for i := 1; i < levels; i++ {
		rev = appendReversedHeader(rev, tagNestedAltAccess, len(rev))
		rev = append(rev, 'c', 0x01, tagSAComponent)
		rev = appendReversedHeader(rev, tagSelectAltAccess, len(rev))
	}
	slices.Reverse(rev)
	return rev
}

// readRequestWithSelection is a Read-Request body naming one variable
// with an AlternateAccess of the given number of levels.
func readRequestWithSelection(levels int) []byte {
	name := berutil.EncodeTLV(0xa0, berutil.EncodeTLV(0x80, []byte("x")))
	spec := berutil.EncodeTLV(0x30, append(name, berutil.EncodeTLV(tagAltAccessWrapper, nestedSelection(levels))...))
	return berutil.EncodeTLV(tagReadVarAccessSpec, berutil.EncodeTLV(tagListOfVar, spec))
}

// An AlternateAccess chain has at most maxAlternateAccessDepth levels.
func TestAlternateAccessDepthLimit(t *testing.T) {
	for _, levels := range []int{1, 2, maxAlternateAccessDepth - 1, maxAlternateAccessDepth, maxAlternateAccessDepth + 1, 2 * maxAlternateAccessDepth} {
		chain, err := decodeAlternateAccess(nestedSelection(levels))
		if levels > maxAlternateAccessDepth {
			if err == nil {
				t.Errorf("%d levels: decoded %d selectors, want an error", levels, len(chain))
			}
			continue
		}
		if err != nil || len(chain) != levels {
			t.Errorf("%d levels: %d selectors, %v", levels, len(chain), err)
			continue
		}
		for i, sel := range chain {
			if sel.Component != "c" {
				t.Fatalf("%d levels: selector %d = %+v", levels, i, sel)
			}
		}
		// The chain encodes back to the same octets.
		if enc, err := encodeAlternateAccess(chain); err != nil || !slices.Equal(enc, nestedSelection(levels)) {
			t.Errorf("%d levels: re-encoded %x, %v", levels, enc, err)
		}
	}
}

// Decoding the deepest selection that fits the default PDU size costs a
// small multiple of its size.
func TestUnmarshalReadRequestSelectionBudget(t *testing.T) {
	const maxPDU = 65000
	levels := 1
	for len(readRequestWithSelection(levels+100)) <= maxPDU {
		levels += 100
	}
	body := readRequestWithSelection(levels)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := UnmarshalReadRequestParsed(body)
	runtime.ReadMemStats(&after)
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > uint64(4*len(body)) {
		t.Errorf("decoding %d levels in %d octets allocated %d bytes, budget %d", levels, len(body), alloc, 4*len(body))
	}
	if err == nil {
		t.Fatalf("%d levels in %d octets decoded", levels, len(body))
	}
	// At the bound the request decodes and the cost is linear too.
	body = readRequestWithSelection(maxAlternateAccessDepth)
	req, err := UnmarshalReadRequestParsed(body)
	if err != nil || len(req.Variables) != 1 || len(req.Variables[0].AlternateAccess) != maxAlternateAccessDepth {
		t.Fatalf("%d levels: %+v, %v", maxAlternateAccessDepth, req, err)
	}
}
