package pdu

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"github.com/otfabric/go-mms/internal/berutil"
)

const maxDataNestingDepth = 64

// MMS Data CHOICE tags (context-specific implicit).
const (
	TagDataAccessError byte = 0x80 // [0] IMPLICIT INTEGER
	TagDataArray       byte = 0xa1 // [1] IMPLICIT SEQUENCE OF Data
	TagDataStructure   byte = 0xa2 // [2] IMPLICIT SEQUENCE OF Data
	TagDataBoolean     byte = 0x83 // [3] IMPLICIT BOOLEAN
	TagDataBitString   byte = 0x84 // [4] IMPLICIT BIT STRING
	TagDataInteger     byte = 0x85 // [5] IMPLICIT INTEGER
	TagDataUnsigned    byte = 0x86 // [6] IMPLICIT unsigned INTEGER
	TagDataFloat       byte = 0x87 // [7] IMPLICIT FloatingPoint
	TagDataOctetString byte = 0x89 // [9] IMPLICIT OCTET STRING
	TagDataVisibleStr  byte = 0x8a // [10] IMPLICIT VisibleString
	TagDataBinaryTime  byte = 0x8c // [12] IMPLICIT TimeOfDay (4 or 6 bytes)
	TagDataObjId       byte = 0x88 // [8] IMPLICIT OBJECT IDENTIFIER
	TagDataMmsString   byte = 0x90 // [16] IMPLICIT MMSString (UTF-8)
	TagDataGenTime     byte = 0x8b // [11] IMPLICIT GeneralizedTime
	TagDataBCD         byte = 0x8d // [13] IMPLICIT INTEGER (BCD encoded)
	TagDataUTCTime     byte = 0x91 // [17] IMPLICIT UtcTime (8 bytes)
)

// Defensive limit for access results decoders.
const maxAccessResults = 65536

// DataValue is the internal representation of an MMS Data element.
// Each field is populated based on the Tag value.
type DataValue struct {
	Tag       byte
	Bool      bool
	Int       int64
	Uint      uint64
	Float     float64
	FloatWide bool         // true = float64 (9 bytes), false = float32 (5 bytes)
	Bytes     []byte       // OctetString, BitString data
	BitLen    int          // BitString: number of valid bits
	Str       string       // VisibleString, MmsString
	Time      time.Time    // UTCTime
	BinTimeMs int64        // BinaryTime: ms since Unix epoch (6-byte) or ms since midnight (4-byte)
	OID       []int        // ObjectIdentifier arcs
	Elements  []*DataValue // Array, Structure children
	ErrCode   int          // DataAccessError code
}

// MarshalData encodes a DataValue into BER wire format.
func MarshalData(v *DataValue) ([]byte, error) {
	switch v.Tag {
	case TagDataBoolean:
		b := byte(0x00)
		if v.Bool {
			b = 0xff
		}
		return berutil.EncodeTLV(TagDataBoolean, []byte{b}), nil

	case TagDataInteger:
		return berutil.EncodeTLV(TagDataInteger, encodeSignedInt(v.Int)), nil

	case TagDataUnsigned:
		return berutil.EncodeTLV(TagDataUnsigned, encodeUnsignedInt(v.Uint)), nil

	case TagDataFloat:
		return berutil.EncodeTLV(TagDataFloat, encodeFloat(v.Float, v.FloatWide)), nil

	case TagDataBitString:
		return berutil.EncodeTLV(TagDataBitString, encodeBitString(v.Bytes, v.BitLen)), nil

	case TagDataOctetString:
		return berutil.EncodeTLV(TagDataOctetString, v.Bytes), nil

	case TagDataVisibleStr:
		return berutil.EncodeTLV(TagDataVisibleStr, []byte(v.Str)), nil

	case TagDataMmsString:
		return berutil.EncodeTLV(TagDataMmsString, []byte(v.Str)), nil

	case TagDataUTCTime:
		return berutil.EncodeTLV(TagDataUTCTime, encodeUTCTime(v.Time)), nil

	case TagDataBinaryTime:
		return berutil.EncodeTLV(TagDataBinaryTime, encodeBinaryTime(v.BinTimeMs)), nil

	case TagDataObjId:
		content, err := berutil.EncodeObjectIdentifier(v.OID)
		if err != nil {
			return nil, fmt.Errorf("pdu: object identifier: %w", err)
		}
		return berutil.EncodeTLV(TagDataObjId, content), nil

	case TagDataGenTime:
		ts := v.Time.UTC().Format("20060102150405Z")
		return berutil.EncodeTLV(TagDataGenTime, []byte(ts)), nil

	case TagDataBCD:
		return berutil.EncodeTLV(TagDataBCD, encodeSignedInt(v.Int)), nil

	case TagDataArray, TagDataStructure:
		var inner []byte
		for _, elem := range v.Elements {
			b, err := MarshalData(elem)
			if err != nil {
				return nil, err
			}
			inner = append(inner, b...)
		}
		return berutil.EncodeTLV(v.Tag, inner), nil

	case TagDataAccessError:
		return berutil.EncodeTLV(TagDataAccessError, encodeUnsignedInt(uint64(v.ErrCode))), nil

	default:
		return nil, fmt.Errorf("pdu: unsupported Data tag 0x%02x", v.Tag)
	}
}

// MarshalDataList encodes multiple DataValue elements into a
// concatenated BER byte sequence (for use inside SEQUENCE OF).
func MarshalDataList(values []*DataValue) ([]byte, error) {
	var buf []byte
	for _, v := range values {
		b, err := MarshalData(v)
		if err != nil {
			return nil, err
		}
		buf = append(buf, b...)
	}
	return buf, nil
}

// UnmarshalDataElement decodes a single MMS Data element from data
// at the given offset. Returns the decoded value and bytes consumed.
func UnmarshalDataElement(data []byte, offset int) (*DataValue, int, error) {
	return unmarshalDataElementWithDepth(data, offset, 0)
}

// UnmarshalAccessResults decodes a sequence of AccessResult elements
// (same encoding as Data). Used for Read response parsing.
func UnmarshalAccessResults(data []byte) ([]*DataValue, error) {
	return unmarshalAccessResultsWithDepth(data, 0)
}

func unmarshalDataElementWithDepth(data []byte, offset, depth int) (*DataValue, int, error) {
	if depth > maxDataNestingDepth {
		return nil, 0, fmt.Errorf("pdu: data nesting depth %d exceeds maximum %d", depth, maxDataNestingDepth)
	}

	tag, content, consumed, err := berutil.DecodeTLVAt(data, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("pdu: data element: %w", err)
	}

	dv, err := decodeDataContentWithDepth(tag, content, depth)
	if err != nil {
		return nil, 0, err
	}
	return dv, consumed, nil
}

func unmarshalAccessResultsWithDepth(data []byte, depth int) ([]*DataValue, error) {
	var results []*DataValue
	offset := 0
	for offset < len(data) {
		dv, n, err := unmarshalDataElementWithDepth(data, offset, depth)
		if err != nil {
			return nil, fmt.Errorf("pdu: access result [%d]: %w", len(results), err)
		}
		offset += n
		results = append(results, dv)
		if len(results) > maxAccessResults {
			return nil, fmt.Errorf("pdu: access results count %d exceeds maximum %d", len(results), maxAccessResults)
		}
	}
	if offset != len(data) {
		return nil, fmt.Errorf("pdu: %d trailing bytes in access results", len(data)-offset)
	}
	return results, nil
}

func decodeDataContentWithDepth(tag byte, content []byte, depth int) (*DataValue, error) {
	switch tag {
	case TagDataBoolean:
		if len(content) != 1 {
			return nil, fmt.Errorf("pdu: boolean length %d, want 1", len(content))
		}
		return &DataValue{Tag: tag, Bool: content[0] != 0}, nil

	case TagDataInteger:
		v, err := decodeSignedInt(content)
		if err != nil {
			return nil, fmt.Errorf("pdu: integer: %w", err)
		}
		return &DataValue{Tag: tag, Int: v}, nil

	case TagDataUnsigned:
		v, err := decodeUnsignedInt(content)
		if err != nil {
			return nil, fmt.Errorf("pdu: unsigned: %w", err)
		}
		return &DataValue{Tag: tag, Uint: v}, nil

	case TagDataFloat:
		f, wide, err := decodeFloat(content)
		if err != nil {
			return nil, fmt.Errorf("pdu: float: %w", err)
		}
		return &DataValue{Tag: tag, Float: f, FloatWide: wide}, nil

	case TagDataBitString:
		data, bitLen, err := decodeBitString(content)
		if err != nil {
			return nil, fmt.Errorf("pdu: bit string: %w", err)
		}
		return &DataValue{Tag: tag, Bytes: data, BitLen: bitLen}, nil

	case TagDataOctetString:
		b := make([]byte, len(content))
		copy(b, content)
		return &DataValue{Tag: tag, Bytes: b}, nil

	case TagDataVisibleStr:
		return &DataValue{Tag: tag, Str: string(content)}, nil

	case TagDataMmsString:
		return &DataValue{Tag: tag, Str: string(content)}, nil

	case TagDataUTCTime:
		t, err := decodeUTCTime(content)
		if err != nil {
			return nil, fmt.Errorf("pdu: utc time: %w", err)
		}
		return &DataValue{Tag: tag, Time: t}, nil

	case TagDataBinaryTime:
		ms, err := decodeBinaryTime(content)
		if err != nil {
			return nil, fmt.Errorf("pdu: binary time: %w", err)
		}
		return &DataValue{Tag: tag, BinTimeMs: ms}, nil

	case TagDataObjId:
		oid, err := berutil.DecodeObjectIdentifier(content)
		if err != nil {
			return nil, fmt.Errorf("pdu: object identifier: %w", err)
		}
		return &DataValue{Tag: tag, OID: oid}, nil

	case TagDataGenTime:
		t, err := time.Parse("20060102150405Z", string(content))
		if err != nil {
			return nil, fmt.Errorf("pdu: generalized time: %w", err)
		}
		return &DataValue{Tag: tag, Time: t}, nil

	case TagDataBCD:
		v, err := decodeSignedInt(content)
		if err != nil {
			return nil, fmt.Errorf("pdu: bcd integer: %w", err)
		}
		return &DataValue{Tag: tag, Int: v}, nil

	case TagDataArray, TagDataStructure:
		elements, err := unmarshalAccessResultsWithDepth(content, depth+1)
		if err != nil {
			return nil, err
		}
		return &DataValue{Tag: tag, Elements: elements}, nil

	case TagDataAccessError:
		code, err := berutil.DecodeInteger(content)
		if err != nil {
			return nil, fmt.Errorf("pdu: data access error: %w", err)
		}
		return &DataValue{Tag: tag, ErrCode: code}, nil

	default:
		return nil, fmt.Errorf("pdu: unknown Data tag 0x%02x", tag)
	}
}

// Integer encoding: BER 2's complement, big-endian, minimum bytes.

func encodeSignedInt(v int64) []byte {
	if v == 0 {
		return []byte{0x00}
	}
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(v))
	if v > 0 {
		i := 0
		for i < 7 && buf[i] == 0 && buf[i+1]&0x80 == 0 {
			i++
		}
		return buf[i:]
	}
	i := 0
	for i < 7 && buf[i] == 0xff && buf[i+1]&0x80 != 0 {
		i++
	}
	return buf[i:]
}

func encodeUnsignedInt(v uint64) []byte {
	if v == 0 {
		return []byte{0x00}
	}
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, v)
	i := 0
	for i < 7 && buf[i] == 0 {
		i++
	}
	if buf[i]&0x80 != 0 {
		return append([]byte{0x00}, buf[i:]...)
	}
	return buf[i:]
}

func decodeSignedInt(data []byte) (int64, error) {
	if len(data) == 0 {
		return 0, fmt.Errorf("empty integer")
	}
	if len(data) > 8 {
		return 0, fmt.Errorf("integer too large (%d bytes)", len(data))
	}
	var v int64
	if data[0]&0x80 != 0 {
		v = -1
	}
	for _, b := range data {
		v = (v << 8) | int64(b)
	}
	return v, nil
}

func decodeUnsignedInt(data []byte) (uint64, error) {
	if len(data) == 0 {
		return 0, fmt.Errorf("empty unsigned integer")
	}
	// BER encodes integers as signed. A high bit on the first byte
	// without a leading 0x00 pad means negative — reject that.
	if data[0]&0x80 != 0 {
		return 0, fmt.Errorf("unsigned integer has negative encoding")
	}
	// Leading 0x00 is BER sign-padding for positive values > 0x7f.
	if data[0] == 0x00 && len(data) > 1 {
		data = data[1:]
	}
	if len(data) > 8 {
		return 0, fmt.Errorf("unsigned integer too large (%d bytes)", len(data))
	}
	var v uint64
	for _, b := range data {
		v = (v << 8) | uint64(b)
	}
	return v, nil
}

// Float encoding: [exponent_width] [IEEE 754 bytes]
// float32: exponent_width=8, 4 bytes; float64: exponent_width=11, 8 bytes

func encodeFloat(f float64, wide bool) []byte {
	if wide {
		buf := make([]byte, 9)
		buf[0] = 11
		binary.BigEndian.PutUint64(buf[1:], math.Float64bits(f))
		return buf
	}
	buf := make([]byte, 5)
	buf[0] = 8
	binary.BigEndian.PutUint32(buf[1:], math.Float32bits(float32(f)))
	return buf
}

func decodeFloat(data []byte) (float64, bool, error) {
	if len(data) == 0 {
		return 0, false, fmt.Errorf("empty float encoding")
	}
	if len(data) == 5 && data[0] == 8 {
		bits := binary.BigEndian.Uint32(data[1:5])
		return float64(math.Float32frombits(bits)), false, nil
	}
	if len(data) == 9 && data[0] == 11 {
		bits := binary.BigEndian.Uint64(data[1:9])
		return math.Float64frombits(bits), true, nil
	}
	return 0, false, fmt.Errorf("unsupported float encoding (len=%d, expWidth=%d)", len(data), data[0])
}

// BitString encoding: [unused_bits] [data_bytes]

func encodeBitString(data []byte, bitLen int) []byte {
	if len(data) == 0 {
		return []byte{0}
	}
	unused := byte(0)
	if bitLen > 0 {
		remainder := bitLen % 8
		if remainder != 0 {
			unused = byte(8 - remainder)
		}
	}
	content := make([]byte, 1+len(data))
	content[0] = unused
	copy(content[1:], data)
	return content
}

func decodeBitString(data []byte) ([]byte, int, error) {
	if len(data) == 0 {
		return nil, 0, fmt.Errorf("empty bit string content (missing unused-bits octet)")
	}
	unused := int(data[0])
	if unused > 7 {
		return nil, 0, fmt.Errorf("invalid unused bits count %d", unused)
	}
	if len(data) == 1 {
		if unused != 0 {
			return nil, 0, fmt.Errorf("empty bit string with non-zero unused bits %d", unused)
		}
		return nil, 0, nil
	}
	rawBytes := make([]byte, len(data)-1)
	copy(rawBytes, data[1:])
	bitLen := len(rawBytes)*8 - unused
	return rawBytes, bitLen, nil
}

// UTCTime encoding: 4 bytes seconds + 3 bytes fraction + 1 byte quality

func encodeUTCTime(t time.Time) []byte {
	buf := make([]byte, 8)
	secs := uint32(t.Unix())
	binary.BigEndian.PutUint32(buf[0:4], secs)
	ns := t.Nanosecond()
	frac := uint32(float64(ns) / 1e9 * float64(1<<24))
	buf[4] = byte(frac >> 16)
	buf[5] = byte(frac >> 8)
	buf[6] = byte(frac)
	buf[7] = 0x00 // quality: no flags
	return buf
}

func decodeUTCTime(data []byte) (time.Time, error) {
	if len(data) != 8 {
		return time.Time{}, fmt.Errorf("UTC time must be 8 bytes, got %d", len(data))
	}
	secs := binary.BigEndian.Uint32(data[0:4])
	frac := uint32(data[4])<<16 | uint32(data[5])<<8 | uint32(data[6])
	nanos := int64(float64(frac) / float64(1<<24) * 1e9)
	return time.Unix(int64(secs), nanos).UTC(), nil
}

// BinaryTime encoding:
//
// Decoder accepts both forms:
//   - 4-byte form: ms since midnight (big-endian uint32), no date info
//   - 6-byte form: 4 bytes ms since midnight + 2 bytes days since 1984-01-01
//
// Encoder always emits the canonical 6-byte form to preserve full
// date+time information.

var epoch1984 = time.Date(1984, 1, 1, 0, 0, 0, 0, time.UTC)

func encodeBinaryTime(msEpoch int64) []byte {
	t := time.UnixMilli(msEpoch).UTC()
	days := uint16(t.Sub(epoch1984).Hours() / 24)
	midnight := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	msOfDay := uint32(t.Sub(midnight).Milliseconds())

	buf := make([]byte, 6)
	binary.BigEndian.PutUint32(buf[0:4], msOfDay)
	binary.BigEndian.PutUint16(buf[4:6], days)
	return buf
}

func decodeBinaryTime(data []byte) (int64, error) {
	switch len(data) {
	case 4:
		ms := int64(binary.BigEndian.Uint32(data))
		return ms, nil
	case 6:
		msOfDay := int64(binary.BigEndian.Uint32(data[0:4]))
		days := int64(binary.BigEndian.Uint16(data[4:6]))
		t := epoch1984.AddDate(0, 0, int(days))
		t = t.Add(time.Duration(msOfDay) * time.Millisecond)
		return t.UnixMilli(), nil
	default:
		return 0, fmt.Errorf("binary time must be 4 or 6 bytes, got %d", len(data))
	}
}
