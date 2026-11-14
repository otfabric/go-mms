// SPDX-License-Identifier: MIT

package berutil

import (
	"errors"
	"fmt"
	"math"
)

// Element is one BER element with a fully parsed identifier
// (X.690 §8.1.2), including the multi-octet tag form.
type Element struct {
	Class       byte // class bits: 0x00, 0x40, 0x80 or 0xc0
	Constructed bool
	Tag         int    // tag number
	Header      []byte // identifier and length octets as received
	Content     []byte // contents octets (excluding end-of-contents for the indefinite form)
	Full        []byte // the complete encoding, end-of-contents included
	Indefinite  bool   // the length used the indefinite form (X.690 §8.1.3.6)
}

// MaxIndefiniteDepth bounds the nesting of indefinite-length encodings
// DecodeElement follows to find an end-of-contents marker, and the
// nesting Canonicalize walks.
const MaxIndefiniteDepth = 64

// maxLengthValue bounds a definite length; PDU sizes are negotiated far
// below it and an int must hold it on 32-bit targets.
const maxLengthValue = math.MaxInt32

// maxTagNumber bounds a multi-octet tag number (4 subsequent octets).
const maxTagNumber = 1<<28 - 1

// errNotCanonicalizable reports a walk Canonicalize cannot complete.
var errNotCanonicalizable = errors.New("berutil: not a TLV sequence")

// DecodeElement decodes the BER element at offset with the receive-side
// leniency of X.690 BER (as opposed to DER, X.690 §10): the long length
// form may use more octets than necessary (§8.1.3.5 NOTE 2), and a
// constructed element may use the indefinite length form terminated by
// end-of-contents octets (§8.1.3.6). Tag numbers above 30 use the
// multi-octet identifier form (§8.1.2.4). It returns the element and the
// number of octets consumed.
func DecodeElement(data []byte, offset int) (Element, int, error) {
	return decodeElement(data, offset, 0)
}

func decodeElement(data []byte, offset, depth int) (Element, int, error) {
	if offset < 0 || offset >= len(data) {
		return Element{}, 0, fmt.Errorf("offset %d beyond data length %d", offset, len(data))
	}
	lead := data[offset]
	el := Element{Class: lead & 0xc0, Constructed: lead&0x20 != 0}
	pos := offset + 1
	if lead&0x1f != 0x1f {
		el.Tag = int(lead & 0x1f)
	} else {
		// X.690 §8.1.2.4.2: base-128 subsequent octets; bits 7..1 of
		// the first subsequent octet shall not all be zero.
		n := 0
		for i := 0; ; i++ {
			if pos >= len(data) {
				return Element{}, 0, fmt.Errorf("tag 0x%02x: truncated multi-octet tag", lead)
			}
			b := data[pos]
			pos++
			if i == 0 && b == 0x80 {
				return Element{}, 0, fmt.Errorf("tag 0x%02x: non-minimal multi-octet tag", lead)
			}
			// Compared before the shift, so that the number cannot wrap
			// in a 32-bit int.
			if n > maxTagNumber>>7 {
				return Element{}, 0, fmt.Errorf("tag 0x%02x: tag number too large", lead)
			}
			n = n<<7 | int(b&0x7f)
			if b&0x80 == 0 {
				break
			}
		}
		el.Tag = n
	}
	if pos >= len(data) {
		return Element{}, 0, fmt.Errorf("tag 0x%02x length: missing length byte", lead)
	}
	lb := data[pos]
	pos++
	switch {
	case lb < 0x80:
		return finishDefinite(data, offset, pos, int(lb), lead, el)
	case lb == 0x80:
		return finishIndefinite(data, offset, pos, depth, lead, el)
	case lb == 0xff:
		// X.690 §8.1.3.5 c): the value 0xff shall not be used.
		return Element{}, 0, fmt.Errorf("tag 0x%02x length: reserved length octet 0xff", lead)
	default:
		n := int(lb & 0x7f)
		if pos+n > len(data) {
			return Element{}, 0, fmt.Errorf("tag 0x%02x length: truncated multi-byte length", lead)
		}
		l := 0
		for _, b := range data[pos : pos+n] {
			if l > maxLengthValue>>8 {
				return Element{}, 0, fmt.Errorf("tag 0x%02x length: length too large", lead)
			}
			l = l<<8 | int(b)
		}
		return finishDefinite(data, offset, pos+n, l, lead, el)
	}
}

func finishDefinite(data []byte, offset, start, l int, lead byte, el Element) (Element, int, error) {
	if l > len(data)-start {
		return Element{}, 0, fmt.Errorf("tag 0x%02x content truncated (need %d, have %d)", lead, l, len(data)-start)
	}
	end := start + l
	el.Header = data[offset:start]
	el.Content = data[start:end]
	el.Full = data[offset:end]
	return el, end - offset, nil
}

func finishIndefinite(data []byte, offset, start, depth int, lead byte, el Element) (Element, int, error) {
	// X.690 §8.1.3.2 a): the indefinite form is for constructed
	// encodings only.
	if !el.Constructed {
		return Element{}, 0, fmt.Errorf("tag 0x%02x length: indefinite length on a primitive encoding", lead)
	}
	if depth >= MaxIndefiniteDepth {
		return Element{}, 0, fmt.Errorf("tag 0x%02x: indefinite-length nesting exceeds %d", lead, MaxIndefiniteDepth)
	}
	pos := start
	for {
		if pos+1 < len(data) && data[pos] == 0x00 && data[pos+1] == 0x00 {
			el.Header = data[offset:start]
			el.Content = data[start:pos]
			el.Full = data[offset : pos+2]
			el.Indefinite = true
			return el, pos + 2 - offset, nil
		}
		if pos >= len(data) {
			return Element{}, 0, fmt.Errorf("tag 0x%02x: missing end-of-contents", lead)
		}
		_, n, err := decodeElement(data, pos, depth+1)
		if err != nil {
			return Element{}, 0, err
		}
		pos += n
	}
}

// Canonicalize rewrites a sequence of BER elements so that every length
// uses the minimal definite form, recursing into constructed elements.
// It is the receive-side bridge from BER to the DER-style lengths the
// definite-only decoders of this module (DecodeTLVAt and encoding/asn1)
// require. Identifier and contents octets of primitive elements are kept
// as received. When data already uses minimal definite lengths it is
// returned unchanged without copying; changed reports a rewrite.
//
// An error means data is not a well-formed sequence of BER elements;
// callers decoding a PDU then fall back to the original bytes so the
// specific decoder reports the fault.
func Canonicalize(data []byte) (out []byte, changed bool, err error) {
	if !needsRewrite(data, 0) {
		return data, false, nil
	}
	out, err = canonicalize(make([]byte, 0, len(data)), data, 0)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// needsRewrite reports whether the element sequence uses a non-minimal or
// indefinite length anywhere, or is not walkable (then canonicalize reports
// the error).
func needsRewrite(data []byte, depth int) bool {
	if depth > MaxIndefiniteDepth {
		return true
	}
	for pos := 0; pos < len(data); {
		el, n, err := DecodeElement(data, pos)
		if err != nil {
			return true
		}
		if el.Indefinite || len(el.Header)-identifierLen(el.Header) != LengthSize(len(el.Content)) {
			return true
		}
		if el.Constructed && needsRewrite(el.Content, depth+1) {
			return true
		}
		pos += n
	}
	return false
}

func canonicalize(buf, data []byte, depth int) ([]byte, error) {
	if depth > MaxIndefiniteDepth {
		return nil, fmt.Errorf("%w: nesting exceeds %d", errNotCanonicalizable, MaxIndefiniteDepth)
	}
	for pos := 0; pos < len(data); {
		el, n, err := DecodeElement(data, pos)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", errNotCanonicalizable, err)
		}
		content := el.Content
		if el.Constructed {
			content, err = canonicalize(nil, el.Content, depth+1)
			if err != nil {
				return nil, err
			}
		}
		buf = append(buf, el.Header[:identifierLen(el.Header)]...)
		buf = AppendLength(buf, len(content))
		buf = append(buf, content...)
		pos += n
	}
	return buf, nil
}

// identifierLen returns the number of identifier octets at the start of a
// header DecodeElement produced.
func identifierLen(header []byte) int {
	if header[0]&0x1f != 0x1f {
		return 1
	}
	i := 1
	for header[i]&0x80 != 0 {
		i++
	}
	return i + 1
}

// TrimInteger strips redundant leading octets from BER INTEGER contents:
// X.690 §8.3.2 requires the minimal form, but a receiver accepting BER
// from peers that pad (non-minimal 00 / ff leading octets) normalises
// before range checks. The value is unchanged.
func TrimInteger(data []byte) []byte {
	for len(data) > 1 && ((data[0] == 0x00 && data[1]&0x80 == 0) || (data[0] == 0xff && data[1]&0x80 != 0)) {
		data = data[1:]
	}
	return data
}
