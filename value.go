package mms

import (
	"bytes"
	"fmt"
	"strings"
	"time"
)

// Value represents an MMS typed data value.
//
// Create values with the typed constructors:
//
//	v := mms.NewInteger(42)
//	v := mms.NewOctetString([]byte{0x01, 0x02})
//	v := mms.NewArray([]*mms.Value{elem1, elem2})
//
// Use [Value.Type] to determine the kind, then call the corresponding
// accessor. Accessors return (T, bool) where the bool is false if the
// value's type does not match — this avoids panics for ordinary API
// misuse:
//
//	i, ok := v.Int64()
//	b, ok := v.OctetString()
//	elems, ok := v.ArrayElements()
//
// Byte slices and element slices are defensively copied by both
// constructors and accessors. Child [*Value] pointers within composite
// values (arrays, structures) are shared; use [Value.Clone] for a full
// deep copy. Use [Value.Get] with selectors for nested access.
type Value struct {
	typ ValueType

	// Exactly one of these is populated based on typ.
	boolVal     bool
	intVal      int64
	uintVal     uint64
	floatVal    float64
	bytesVal    []byte // OctetString, BitString
	bitLen      int    // BitString: number of valid bits
	stringVal   string // VisibleString, MmsString
	timeVal     time.Time
	binaryTime  int64
	elementsVal []*Value // Structure, Array
	accessErr   DataAccessErrorCode
	oidVal      []int // ObjectIdentifier arcs
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

// BitString returns a copy of the bit string as a byte slice. ok is
// false if the value is not [ValueTypeBitString].
func (v *Value) BitString() (val []byte, ok bool) {
	if v.typ != ValueTypeBitString {
		return nil, false
	}
	return copyBytes(v.bytesVal), true
}

// BitStringLength returns the number of valid bits in the bit string.
// ok is false if the value is not [ValueTypeBitString].
func (v *Value) BitStringLength() (val int, ok bool) {
	if v.typ != ValueTypeBitString {
		return 0, false
	}
	return v.bitLen, true
}

// OctetString returns a copy of the octet string. ok is false if the
// value is not [ValueTypeOctetString].
func (v *Value) OctetString() (val []byte, ok bool) {
	if v.typ != ValueTypeOctetString {
		return nil, false
	}
	return copyBytes(v.bytesVal), true
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

// GeneralizedTime returns the generalized timestamp. ok is false if
// the value is not [ValueTypeGeneralizedTime].
func (v *Value) GeneralizedTime() (val time.Time, ok bool) {
	if v.typ != ValueTypeGeneralizedTime {
		return time.Time{}, false
	}
	return v.timeVal, true
}

// BCD returns the BCD-encoded integer value. ok is false if the value
// is not [ValueTypeBCD].
func (v *Value) BCD() (val int64, ok bool) {
	if v.typ != ValueTypeBCD {
		return 0, false
	}
	return v.intVal, true
}

// ObjectIdentifier returns a copy of the OID arcs. ok is false if the
// value is not [ValueTypeObjectIdentifier].
func (v *Value) ObjectIdentifier() (val []int, ok bool) {
	if v.typ != ValueTypeObjectIdentifier {
		return nil, false
	}
	c := make([]int, len(v.oidVal))
	copy(c, v.oidVal)
	return c, true
}

// Structure returns a shallow copy of the element slice of a structure
// value. The returned slice is independent but the child [*Value]
// pointers are shared; mutating a child affects the original Value.
// ok is false if the value is not [ValueTypeStructure].
func (v *Value) Structure() (val []*Value, ok bool) {
	if v.typ != ValueTypeStructure {
		return nil, false
	}
	return copyValues(v.elementsVal), true
}

// ArrayElements returns a shallow copy of the element slice of an array
// value. The returned slice is independent but the child [*Value]
// pointers are shared; mutating a child affects the original Value.
// ok is false if the value is not [ValueTypeArray].
func (v *Value) ArrayElements() (val []*Value, ok bool) {
	if v.typ != ValueTypeArray {
		return nil, false
	}
	return copyValues(v.elementsVal), true
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
// All bits in the byte slice are considered valid (bitLen = len*8).
func NewBitString(bits []byte) *Value {
	return &Value{typ: ValueTypeBitString, bytesVal: copyBytes(bits), bitLen: len(bits) * 8}
}

// NewBitStringWithLength creates a [Value] of type [ValueTypeBitString]
// with an explicit bit length. Use this when the number of valid bits
// is not a multiple of 8.
func NewBitStringWithLength(bits []byte, bitLen int) *Value {
	return &Value{typ: ValueTypeBitString, bytesVal: copyBytes(bits), bitLen: bitLen}
}

// NewOctetString creates a [Value] of type [ValueTypeOctetString].
func NewOctetString(data []byte) *Value {
	return &Value{typ: ValueTypeOctetString, bytesVal: copyBytes(data)}
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

// NewGeneralizedTime creates a [Value] of type [ValueTypeGeneralizedTime].
func NewGeneralizedTime(t time.Time) *Value {
	return &Value{typ: ValueTypeGeneralizedTime, timeVal: t}
}

// NewBCD creates a [Value] of type [ValueTypeBCD].
func NewBCD(v int64) *Value {
	return &Value{typ: ValueTypeBCD, intVal: v}
}

// NewObjectIdentifier creates a [Value] of type [ValueTypeObjectIdentifier].
// The OID slice is defensively copied.
func NewObjectIdentifier(oid []int) *Value {
	var c []int
	if oid != nil {
		c = make([]int, len(oid))
		copy(c, oid)
	}
	return &Value{typ: ValueTypeObjectIdentifier, oidVal: c}
}

// NewArray creates a [Value] of type [ValueTypeArray].
// The element slice is shallow-copied: the slice header is independent
// but the child [*Value] pointers are shared with the caller. Mutating
// a child value after construction is visible through both the original
// slice and this Value. Use [Value.Clone] if full ownership is needed.
func NewArray(elements []*Value) *Value {
	return &Value{typ: ValueTypeArray, elementsVal: copyValues(elements)}
}

// NewStructure creates a [Value] of type [ValueTypeStructure].
// The element slice is shallow-copied: the slice header is independent
// but the child [*Value] pointers are shared with the caller. Mutating
// a child value after construction is visible through both the original
// slice and this Value. Use [Value.Clone] if full ownership is needed.
func NewStructure(elements []*Value) *Value {
	return &Value{typ: ValueTypeStructure, elementsVal: copyValues(elements)}
}

// NewDataAccessError creates a [Value] of type [ValueTypeDataAccessError].
func NewDataAccessError(code DataAccessErrorCode) *Value {
	return &Value{typ: ValueTypeDataAccessError, accessErr: code}
}

// Clone returns a deep copy of the value, including all nested
// structure and array elements.
func (v *Value) Clone() *Value {
	if v == nil {
		return nil
	}
	c := &Value{
		typ:        v.typ,
		boolVal:    v.boolVal,
		intVal:     v.intVal,
		uintVal:    v.uintVal,
		floatVal:   v.floatVal,
		bitLen:     v.bitLen,
		stringVal:  v.stringVal,
		timeVal:    v.timeVal,
		binaryTime: v.binaryTime,
		accessErr:  v.accessErr,
		bytesVal:   copyBytes(v.bytesVal),
	}
	if v.oidVal != nil {
		c.oidVal = make([]int, len(v.oidVal))
		copy(c.oidVal, v.oidVal)
	}
	if v.elementsVal != nil {
		c.elementsVal = make([]*Value, len(v.elementsVal))
		for i, e := range v.elementsVal {
			c.elementsVal[i] = e.Clone()
		}
	}
	return c
}

// Equal returns true if v and other have the same type and value,
// including deep comparison of structures and arrays. Float values
// are compared with exact bitwise equality (==), not approximate
// comparison. If epsilon-based float equality is needed, callers
// should implement that separately.
func (v *Value) Equal(other *Value) bool {
	if v == nil || other == nil {
		return v == other
	}
	if v.typ != other.typ {
		return false
	}
	switch v.typ {
	case ValueTypeBoolean:
		return v.boolVal == other.boolVal
	case ValueTypeInteger:
		return v.intVal == other.intVal
	case ValueTypeUnsigned:
		return v.uintVal == other.uintVal
	case ValueTypeFloat:
		return v.floatVal == other.floatVal
	case ValueTypeBitString:
		return v.bitLen == other.bitLen && bytes.Equal(v.bytesVal, other.bytesVal)
	case ValueTypeOctetString:
		return bytes.Equal(v.bytesVal, other.bytesVal)
	case ValueTypeVisibleString, ValueTypeMmsString:
		return v.stringVal == other.stringVal
	case ValueTypeUTCTime:
		return v.timeVal.Equal(other.timeVal)
	case ValueTypeBinaryTime:
		return v.binaryTime == other.binaryTime
	case ValueTypeGeneralizedTime:
		return v.timeVal.Equal(other.timeVal)
	case ValueTypeBCD:
		return v.intVal == other.intVal
	case ValueTypeObjectIdentifier:
		if len(v.oidVal) != len(other.oidVal) {
			return false
		}
		for i := range v.oidVal {
			if v.oidVal[i] != other.oidVal[i] {
				return false
			}
		}
		return true
	case ValueTypeStructure, ValueTypeArray:
		if len(v.elementsVal) != len(other.elementsVal) {
			return false
		}
		for i := range v.elementsVal {
			if !v.elementsVal[i].Equal(other.elementsVal[i]) {
				return false
			}
		}
		return true
	case ValueTypeDataAccessError:
		return v.accessErr == other.accessErr
	default:
		return false
	}
}

// String returns a human-readable representation of the value for
// debugging and logging.
func (v *Value) String() string {
	if v == nil {
		return "<nil>"
	}
	switch v.typ {
	case ValueTypeBoolean:
		return fmt.Sprintf("%v", v.boolVal)
	case ValueTypeInteger:
		return fmt.Sprintf("%d", v.intVal)
	case ValueTypeUnsigned:
		return fmt.Sprintf("%d", v.uintVal)
	case ValueTypeFloat:
		return fmt.Sprintf("%g", v.floatVal)
	case ValueTypeBitString:
		return fmt.Sprintf("BitString(%d bits)", v.bitLen)
	case ValueTypeOctetString:
		return fmt.Sprintf("OctetString(%d bytes)", len(v.bytesVal))
	case ValueTypeVisibleString:
		return fmt.Sprintf("%q", v.stringVal)
	case ValueTypeMmsString:
		return fmt.Sprintf("MmsString(%q)", v.stringVal)
	case ValueTypeUTCTime:
		return v.timeVal.Format(time.RFC3339)
	case ValueTypeBinaryTime:
		return fmt.Sprintf("BinaryTime(%d ms)", v.binaryTime)
	case ValueTypeStructure:
		var parts []string
		for _, e := range v.elementsVal {
			parts = append(parts, e.String())
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case ValueTypeArray:
		var parts []string
		for _, e := range v.elementsVal {
			parts = append(parts, e.String())
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case ValueTypeGeneralizedTime:
		return v.timeVal.Format("2006-01-02T15:04:05Z")
	case ValueTypeBCD:
		return fmt.Sprintf("BCD(%d)", v.intVal)
	case ValueTypeObjectIdentifier:
		return fmt.Sprintf("OID(%v)", v.oidVal)
	case ValueTypeDataAccessError:
		return fmt.Sprintf("DataAccessError(%s)", v.accessErr)
	default:
		return fmt.Sprintf("Value(type=%d)", int(v.typ))
	}
}

// Get traverses nested structure/array values using a chain of access
// selectors. Returns the selected sub-value or an error if the path
// is invalid.
//
// Note: Component selectors that use string names require the [TypeSpec]
// to resolve component positions. Without a TypeSpec, use integer index
// selectors instead (see [SelectIndex]).
func (v *Value) Get(selectors ...AccessSelector) (*Value, error) {
	cur := v
	for i, sel := range selectors {
		if cur == nil {
			return nil, fmt.Errorf("nil value at selector [%d]", i)
		}
		switch {
		case sel.Component != "":
			elems, ok := cur.Structure()
			if !ok {
				return nil, fmt.Errorf("selector [%d]: component %q on non-structure type %s", i, sel.Component, cur.typ)
			}
			_ = elems
			return nil, fmt.Errorf("selector [%d]: component access requires TypeSpec context; use TypeSpec.Resolve instead", i)
		case sel.Index != nil:
			idx := *sel.Index
			switch cur.typ {
			case ValueTypeStructure:
				elems, _ := cur.Structure()
				if idx < 0 || idx >= len(elems) {
					return nil, fmt.Errorf("selector [%d]: index %d out of range (structure has %d elements)", i, idx, len(elems))
				}
				cur = elems[idx]
			case ValueTypeArray:
				elems, _ := cur.ArrayElements()
				if idx < 0 || idx >= len(elems) {
					return nil, fmt.Errorf("selector [%d]: index %d out of range (array has %d elements)", i, idx, len(elems))
				}
				cur = elems[idx]
			default:
				return nil, fmt.Errorf("selector [%d]: index on non-composite type %s", i, cur.typ)
			}
		case sel.IndexRange != nil:
			elems, ok := cur.ArrayElements()
			if !ok {
				return nil, fmt.Errorf("selector [%d]: index range on non-array type %s", i, cur.typ)
			}
			start := sel.IndexRange.Start
			count := sel.IndexRange.Count
			if start < 0 || start+count > len(elems) {
				return nil, fmt.Errorf("selector [%d]: range [%d..%d) out of bounds (array has %d elements)", i, start, start+count, len(elems))
			}
			sub := make([]*Value, count)
			copy(sub, elems[start:start+count])
			cur = NewArray(sub)
		default:
			return nil, fmt.Errorf("selector [%d]: no field set", i)
		}
	}
	return cur, nil
}

func copyBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

func copyValues(v []*Value) []*Value {
	if v == nil {
		return nil
	}
	c := make([]*Value, len(v))
	copy(c, v)
	return c
}
