// SPDX-License-Identifier: MIT

package codec

import (
	"encoding/asn1"
	"errors"
	"reflect"
	"unicode/utf8"
)

// errInvalidString reports a string that no ASN.1 string type this
// package emits can carry.
var errInvalidString = errors.New("string is not valid UTF-8")

// marshalChecked is asn1.Marshal, refusing a string it would otherwise
// emit as a UTF8String that every decoder, this one included, rejects
// (encoding/asn1 does not validate fields tagged "utf8").
func marshalChecked(value any) ([]byte, error) {
	if err := checkStrings(reflect.ValueOf(value)); err != nil {
		return nil, err
	}
	return asn1.Marshal(value)
}

// unmarshalChecked is asn1.Unmarshal, rejecting a decoded string that is
// not valid UTF-8. Before Go 1.25 encoding/asn1 copies T61String and
// GeneralString content verbatim into a string field, whatever string
// type the field declares, so such a value decodes but cannot be encoded.
func unmarshalChecked(data []byte, target any) ([]byte, error) {
	rest, err := asn1.Unmarshal(data, target)
	if err != nil {
		return nil, err
	}
	if err := checkStrings(reflect.ValueOf(target)); err != nil {
		return nil, err
	}
	return rest, nil
}

// checkStrings walks the shapes encoding/asn1 reads and writes and
// returns errInvalidString for the first string that is not valid UTF-8.
func checkStrings(v reflect.Value) error {
	switch v.Kind() {
	case reflect.String:
		if !utf8.ValidString(v.String()) {
			return errInvalidString
		}
	case reflect.Pointer, reflect.Interface:
		return checkStrings(v.Elem())
	case reflect.Struct:
		for i := range v.NumField() {
			if !v.Type().Field(i).IsExported() {
				continue
			}
			if err := checkStrings(v.Field(i)); err != nil {
				return err
			}
		}
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return nil
		}
		for i := range v.Len() {
			if err := checkStrings(v.Index(i)); err != nil {
				return err
			}
		}
	default: // no string inside
	}
	return nil
}
