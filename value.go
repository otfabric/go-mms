package mms

import "time"

// Value represents an MMS typed data value.
//
// Use the Type method to determine the kind of value, then call the
// corresponding accessor. Accessors return (T, bool) where the bool
// is false if the value's type does not match the accessor — this
// avoids panics for ordinary API misuse.
type Value struct {
	typ ValueType

	// Exactly one of these is populated based on typ.
	boolVal      bool
	intVal       int64
	uintVal      uint64
	floatVal     float64
	bytesVal     []byte   // OctetString, BitString
	stringVal    string   // VisibleString, MmsString
	timeVal      time.Time
	binaryTime   int64
	structureVal []*Value // Structure, Array
	accessErr    DataAccessErrorCode
}

// Type returns the [ValueType] of this value.
func (v *Value) Type() ValueType { return v.typ }

// Bool returns the boolean value. ok is false if the value is not
// [ValueTypeBoolean].
func (v *Value) Bool() (val bool, ok bool) {
	if v.typ != ValueTypeBoolean {
		return false, false
	}
	return v.boolVal, true
}

// Int32 returns a signed 32-bit integer. ok is false if the value is
// not [ValueTypeInteger] or if the value overflows int32.
func (v *Value) Int32() (val int32, ok bool) {
	if v.typ != ValueTypeInteger {
		return 0, false
	}
	i := int32(v.intVal)
	if int64(i) != v.intVal {
		return 0, false
	}
	return i, true
}

// Int64 returns a signed 64-bit integer. ok is false if the value is
// not [ValueTypeInteger].
func (v *Value) Int64() (val int64, ok bool) {
	if v.typ != ValueTypeInteger {
		return 0, false
	}
	return v.intVal, true
}

// Uint32 returns an unsigned 32-bit integer. ok is false if the value
// is not [ValueTypeUnsigned] or if the value overflows uint32.
func (v *Value) Uint32() (val uint32, ok bool) {
	if v.typ != ValueTypeUnsigned {
		return 0, false
	}
	u := uint32(v.uintVal)
	if uint64(u) != v.uintVal {
		return 0, false
	}
	return u, true
}

// Uint64 returns an unsigned 64-bit integer. ok is false if the value
// is not [ValueTypeUnsigned].
func (v *Value) Uint64() (val uint64, ok bool) {
	if v.typ != ValueTypeUnsigned {
		return 0, false
	}
	return v.uintVal, true
}

// Float32 returns a 32-bit float. ok is false if the value is not
// [ValueTypeFloat].
func (v *Value) Float32() (val float32, ok bool) {
	if v.typ != ValueTypeFloat {
		return 0, false
	}
	return float32(v.floatVal), true
}

// Float64 returns a 64-bit float. ok is false if the value is not
// [ValueTypeFloat].
func (v *Value) Float64() (val float64, ok bool) {
	if v.typ != ValueTypeFloat {
		return 0, false
	}
	return v.floatVal, true
}

// BitString returns the bit string as a byte slice. ok is false if
// the value is not [ValueTypeBitString].
func (v *Value) BitString() (val []byte, ok bool) {
	if v.typ != ValueTypeBitString {
		return nil, false
	}
	return v.bytesVal, true
}

// OctetString returns the octet string. ok is false if the value is
// not [ValueTypeOctetString].
func (v *Value) OctetString() (val []byte, ok bool) {
	if v.typ != ValueTypeOctetString {
		return nil, false
	}
	return v.bytesVal, true
}

// VisibleString returns the visible string. ok is false if the value
// is not [ValueTypeVisibleString].
func (v *Value) VisibleString() (val string, ok bool) {
	if v.typ != ValueTypeVisibleString {
		return "", false
	}
	return v.stringVal, true
}

// MmsString returns the MMS string. ok is false if the value is not
// [ValueTypeMmsString].
func (v *Value) MmsString() (val string, ok bool) {
	if v.typ != ValueTypeMmsString {
		return "", false
	}
	return v.stringVal, true
}

// UTCTime returns the UTC timestamp. ok is false if the value is not
// [ValueTypeUTCTime].
func (v *Value) UTCTime() (val time.Time, ok bool) {
	if v.typ != ValueTypeUTCTime {
		return time.Time{}, false
	}
	return v.timeVal, true
}

// BinaryTime returns the binary time as milliseconds. ok is false if
// the value is not [ValueTypeBinaryTime].
func (v *Value) BinaryTime() (val int64, ok bool) {
	if v.typ != ValueTypeBinaryTime {
		return 0, false
	}
	return v.binaryTime, true
}

// Structure returns the elements of a structure value. ok is false if
// the value is not [ValueTypeStructure].
func (v *Value) Structure() (val []*Value, ok bool) {
	if v.typ != ValueTypeStructure {
		return nil, false
	}
	return v.structureVal, true
}

// ArrayElements returns the elements of an array value. ok is false
// if the value is not [ValueTypeArray].
func (v *Value) ArrayElements() (val []*Value, ok bool) {
	if v.typ != ValueTypeArray {
		return nil, false
	}
	return v.structureVal, true
}

// DataAccessErr returns the data access error code. ok is false if
// the value is not [ValueTypeDataAccessError].
func (v *Value) DataAccessErr() (val DataAccessErrorCode, ok bool) {
	if v.typ != ValueTypeDataAccessError {
		return 0, false
	}
	return v.accessErr, true
}

// Constructors for creating Value instances.

// NewBoolean creates a [Value] of type [ValueTypeBoolean].
func NewBoolean(b bool) *Value {
	return &Value{typ: ValueTypeBoolean, boolVal: b}
}

// NewInteger creates a [Value] of type [ValueTypeInteger].
func NewInteger(i int64) *Value {
	return &Value{typ: ValueTypeInteger, intVal: i}
}

// NewUnsigned creates a [Value] of type [ValueTypeUnsigned].
func NewUnsigned(u uint64) *Value {
	return &Value{typ: ValueTypeUnsigned, uintVal: u}
}

// NewFloat creates a [Value] of type [ValueTypeFloat].
func NewFloat(f float64) *Value {
	return &Value{typ: ValueTypeFloat, floatVal: f}
}

// NewBitString creates a [Value] of type [ValueTypeBitString].
func NewBitString(bits []byte) *Value {
	return &Value{typ: ValueTypeBitString, bytesVal: bits}
}

// NewOctetString creates a [Value] of type [ValueTypeOctetString].
func NewOctetString(data []byte) *Value {
	return &Value{typ: ValueTypeOctetString, bytesVal: data}
}

// NewVisibleString creates a [Value] of type [ValueTypeVisibleString].
func NewVisibleString(s string) *Value {
	return &Value{typ: ValueTypeVisibleString, stringVal: s}
}

// NewMmsString creates a [Value] of type [ValueTypeMmsString].
func NewMmsString(s string) *Value {
	return &Value{typ: ValueTypeMmsString, stringVal: s}
}

// NewUTCTime creates a [Value] of type [ValueTypeUTCTime].
func NewUTCTime(t time.Time) *Value {
	return &Value{typ: ValueTypeUTCTime, timeVal: t}
}

// NewBinaryTime creates a [Value] of type [ValueTypeBinaryTime].
// The value is milliseconds since epoch.
func NewBinaryTime(ms int64) *Value {
	return &Value{typ: ValueTypeBinaryTime, binaryTime: ms}
}

// NewArray creates a [Value] of type [ValueTypeArray].
func NewArray(elements []*Value) *Value {
	return &Value{typ: ValueTypeArray, structureVal: elements}
}

// NewStructure creates a [Value] of type [ValueTypeStructure].
func NewStructure(elements []*Value) *Value {
	return &Value{typ: ValueTypeStructure, structureVal: elements}
}

// NewDataAccessError creates a [Value] of type [ValueTypeDataAccessError].
func NewDataAccessError(code DataAccessErrorCode) *Value {
	return &Value{typ: ValueTypeDataAccessError, accessErr: code}
}
