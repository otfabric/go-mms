// SPDX-License-Identifier: MIT

package pdu

import (
	"bytes"
	"encoding/asn1"
	"fmt"
	"testing"

	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
)

// reencodes is the invariant of the decoder fuzz targets: a decoded
// value encodes, that encoding decodes, and encoding the result once
// more gives the same octets.
func reencodes[T any](t *testing.T, v T, encode func(T) ([]byte, error), decode func([]byte) (T, error)) {
	t.Helper()
	first, err := encode(v)
	if err != nil {
		t.Fatalf("decoded value %+v does not encode: %v", v, err)
	}
	again, err := decode(first)
	if err != nil {
		t.Fatalf("encoding % x does not decode: %v", first, err)
	}
	second, err := encode(again)
	if err != nil {
		t.Fatalf("re-decoded value %+v does not encode: %v", again, err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("encoding changed from % x to % x", first, second)
	}
}

// pduContent returns the contents of the encoded MMS PDU b.
func pduContent(b []byte) ([]byte, error) {
	_, content, err := berutil.DecodeTLV(b)
	return content, err
}

// requestBody returns the service request contents of the encoded
// Confirmed-RequestPDU b.
func requestBody(b []byte, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	content, err := pduContent(b)
	if err != nil {
		return nil, err
	}
	_, raw, err := codec.UnmarshalConfirmedRequest(content)
	if err != nil {
		return nil, err
	}
	return raw.Bytes, nil
}

// serviceResponse is the service response body as a response decoder
// receives it.
func serviceResponse(tag int, body []byte) asn1.RawValue {
	return asn1.RawValue{Tag: tag, Class: asn1.ClassContextSpecific, IsCompound: true, Bytes: body}
}

// decodeOneElement decodes b as exactly one Data element.
func decodeOneElement(b []byte) (*DataValue, error) {
	v, n, err := UnmarshalDataElement(b, 0)
	if err == nil && n != len(b) {
		return nil, fmt.Errorf("%d of %d octets consumed", n, len(b))
	}
	return v, err
}

// marshalReadResults encodes decoded Read results as a Read response.
func marshalReadResults(vs []*DataValue) ([]byte, error) {
	results := make([]*AccessResult, len(vs))
	for i, v := range vs {
		if v.Tag == TagDataAccessError {
			results[i] = &AccessResult{IsError: true, ErrorCode: v.ErrCode}
		} else {
			results[i] = &AccessResult{Data: v}
		}
	}
	return MarshalReadResponse(results)
}

// encodableData reports whether MarshalData encodes every value in vs.
// The decoder keeps any INTEGER as a DataAccessError code; MarshalData
// encodes only a code that is not negative.
func encodableData(vs ...*DataValue) bool {
	for _, v := range vs {
		if v == nil {
			continue
		}
		if v.Tag == TagDataAccessError && v.ErrCode < 0 || !encodableData(v.Elements...) {
			return false
		}
	}
	return true
}

// noNil fails when a decoded list holds a nil value.
func noNil[T any](t *testing.T, vs []*T) {
	t.Helper()
	for i, v := range vs {
		if v == nil {
			t.Fatalf("decoded value %d of %d is nil", i, len(vs))
		}
	}
}

// page is a decoded list with its moreFollows flag.
type page[T any] struct {
	items []T
	more  bool
}
