package pdu

import (
	"math"
	"testing"
	"time"
)

func TestDataBooleanRoundTrip(t *testing.T) {
	for _, val := range []bool{true, false} {
		dv := &DataValue{Tag: TagDataBoolean, Bool: val}
		b, err := MarshalData(dv)
		if err != nil {
			t.Fatalf("marshal bool(%v): %v", val, err)
		}

		got, n, err := UnmarshalDataElement(b, 0)
		if err != nil {
			t.Fatalf("unmarshal bool(%v): %v", val, err)
		}
		if n != len(b) {
			t.Errorf("bool(%v): consumed %d, want %d", val, n, len(b))
		}
		if got.Tag != TagDataBoolean || got.Bool != val {
			t.Errorf("bool(%v): got %+v", val, got)
		}
	}
}

func TestDataIntegerRoundTrip(t *testing.T) {
	cases := []int64{0, 1, -1, 127, -128, 255, 256, -256, 32767, -32768, math.MaxInt32, math.MinInt32, math.MaxInt64, math.MinInt64}
	for _, val := range cases {
		dv := &DataValue{Tag: TagDataInteger, Int: val}
		b, err := MarshalData(dv)
		if err != nil {
			t.Fatalf("marshal int(%d): %v", val, err)
		}

		got, _, err := UnmarshalDataElement(b, 0)
		if err != nil {
			t.Fatalf("unmarshal int(%d): %v", val, err)
		}
		if got.Tag != TagDataInteger || got.Int != val {
			t.Errorf("int(%d): got %d", val, got.Int)
		}
	}
}

func TestDataUnsignedRoundTrip(t *testing.T) {
	cases := []uint64{0, 1, 127, 128, 255, 256, 65535, 65536, math.MaxUint32, math.MaxUint64}
	for _, val := range cases {
		dv := &DataValue{Tag: TagDataUnsigned, Uint: val}
		b, err := MarshalData(dv)
		if err != nil {
			t.Fatalf("marshal uint(%d): %v", val, err)
		}

		got, _, err := UnmarshalDataElement(b, 0)
		if err != nil {
			t.Fatalf("unmarshal uint(%d): %v", val, err)
		}
		if got.Tag != TagDataUnsigned || got.Uint != val {
			t.Errorf("uint(%d): got %d", val, got.Uint)
		}
	}
}

func TestDataFloat32RoundTrip(t *testing.T) {
	cases := []float64{0.0, 1.0, -1.0, 3.14, math.MaxFloat32, math.SmallestNonzeroFloat32}
	for _, val := range cases {
		f32 := float64(float32(val))
		dv := &DataValue{Tag: TagDataFloat, Float: f32, FloatWide: false}
		b, err := MarshalData(dv)
		if err != nil {
			t.Fatalf("marshal float32(%v): %v", val, err)
		}
		if len(b) != 7 { // tag(1) + length(1) + exponent(1) + ieee(4)
			t.Fatalf("float32(%v): encoded length %d, want 7", val, len(b))
		}

		got, _, err := UnmarshalDataElement(b, 0)
		if err != nil {
			t.Fatalf("unmarshal float32(%v): %v", val, err)
		}
		if got.FloatWide {
			t.Errorf("float32(%v): decoded as wide", val)
		}
		if got.Float != f32 {
			t.Errorf("float32(%v): got %v", val, got.Float)
		}
	}
}

func TestDataFloat64RoundTrip(t *testing.T) {
	val := math.Pi
	dv := &DataValue{Tag: TagDataFloat, Float: val, FloatWide: true}
	b, err := MarshalData(dv)
	if err != nil {
		t.Fatalf("marshal float64: %v", err)
	}
	if len(b) != 11 { // tag(1) + length(1) + exponent(1) + ieee(8)
		t.Fatalf("float64: encoded length %d, want 11", len(b))
	}

	got, _, err := UnmarshalDataElement(b, 0)
	if err != nil {
		t.Fatalf("unmarshal float64: %v", err)
	}
	if !got.FloatWide {
		t.Error("float64: decoded as narrow")
	}
	if got.Float != val {
		t.Errorf("float64: got %v, want %v", got.Float, val)
	}
}

func TestDataBitStringRoundTrip(t *testing.T) {
	data := []byte{0xAA, 0xBB, 0xCC}
	dv := &DataValue{Tag: TagDataBitString, Bytes: data, BitLen: 20}
	b, err := MarshalData(dv)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got, _, err := UnmarshalDataElement(b, 0)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Tag != TagDataBitString {
		t.Errorf("tag = 0x%02x, want 0x%02x", got.Tag, TagDataBitString)
	}
	if got.BitLen != 20 {
		t.Errorf("bitLen = %d, want 20", got.BitLen)
	}
	if len(got.Bytes) != 3 {
		t.Errorf("bytes len = %d, want 3", len(got.Bytes))
	}
}

func TestDataOctetStringRoundTrip(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04}
	dv := &DataValue{Tag: TagDataOctetString, Bytes: data}
	b, err := MarshalData(dv)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got, _, err := UnmarshalDataElement(b, 0)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Tag != TagDataOctetString || len(got.Bytes) != 4 {
		t.Errorf("octet string mismatch: %+v", got)
	}
	for i, v := range data {
		if got.Bytes[i] != v {
			t.Errorf("byte[%d] = 0x%02x, want 0x%02x", i, got.Bytes[i], v)
		}
	}
}

func TestDataVisibleStringRoundTrip(t *testing.T) {
	str := "Hello, MMS!"
	dv := &DataValue{Tag: TagDataVisibleStr, Str: str}
	b, err := MarshalData(dv)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got, _, err := UnmarshalDataElement(b, 0)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Str != str {
		t.Errorf("visible string = %q, want %q", got.Str, str)
	}
}

func TestDataMmsStringRoundTrip(t *testing.T) {
	str := "MMS string with UTF-8: ñ"
	dv := &DataValue{Tag: TagDataMmsString, Str: str}
	b, err := MarshalData(dv)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got, _, err := UnmarshalDataElement(b, 0)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Str != str {
		t.Errorf("mms string = %q, want %q", got.Str, str)
	}
}

func TestDataUTCTimeRoundTrip(t *testing.T) {
	now := time.Date(2024, 6, 15, 12, 30, 45, 500000000, time.UTC)
	dv := &DataValue{Tag: TagDataUTCTime, Time: now}
	b, err := MarshalData(dv)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(b) != 10 { // tag(1) + length(1) + 8 bytes
		t.Fatalf("utc time encoded length = %d, want 10", len(b))
	}

	got, _, err := UnmarshalDataElement(b, 0)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	diff := got.Time.Sub(now)
	if diff < -time.Millisecond || diff > time.Millisecond {
		t.Errorf("utc time = %v, want ~%v (diff=%v)", got.Time, now, diff)
	}
}

func TestDataBinaryTime6ByteRoundTrip(t *testing.T) {
	now := time.Date(2024, 6, 15, 12, 30, 45, 0, time.UTC)
	msEpoch := now.UnixMilli()
	dv := &DataValue{Tag: TagDataBinaryTime, BinTimeMs: msEpoch}
	b, err := MarshalData(dv)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(b) != 8 { // tag(1) + length(1) + 6 bytes
		t.Fatalf("binary time encoded length = %d, want 8", len(b))
	}

	got, _, err := UnmarshalDataElement(b, 0)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Binary time has ms precision, so round-trip should be within 1 second
	diff := got.BinTimeMs - msEpoch
	if diff < -1000 || diff > 1000 {
		t.Errorf("binary time ms = %d, want ~%d (diff=%d)", got.BinTimeMs, msEpoch, diff)
	}
}

func TestDataBinaryTime4ByteDecode(t *testing.T) {
	// 4-byte binary time: ms since midnight
	data := []byte{TagDataBinaryTime, 0x04, 0x00, 0x01, 0x51, 0x80} // 86400 ms = 86.4 seconds
	got, _, err := UnmarshalDataElement(data, 0)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.BinTimeMs != 86400 {
		t.Errorf("binary time 4-byte ms = %d, want 86400", got.BinTimeMs)
	}
}

func TestDataStructureRoundTrip(t *testing.T) {
	dv := &DataValue{
		Tag: TagDataStructure,
		Elements: []*DataValue{
			{Tag: TagDataBoolean, Bool: true},
			{Tag: TagDataInteger, Int: 42},
			{Tag: TagDataVisibleStr, Str: "test"},
		},
	}
	b, err := MarshalData(dv)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got, _, err := UnmarshalDataElement(b, 0)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Tag != TagDataStructure {
		t.Fatalf("tag = 0x%02x, want 0x%02x", got.Tag, TagDataStructure)
	}
	if len(got.Elements) != 3 {
		t.Fatalf("structure elements = %d, want 3", len(got.Elements))
	}
	if !got.Elements[0].Bool {
		t.Error("element[0] bool should be true")
	}
	if got.Elements[1].Int != 42 {
		t.Errorf("element[1] int = %d, want 42", got.Elements[1].Int)
	}
	if got.Elements[2].Str != "test" {
		t.Errorf("element[2] str = %q, want %q", got.Elements[2].Str, "test")
	}
}

func TestDataArrayRoundTrip(t *testing.T) {
	dv := &DataValue{
		Tag: TagDataArray,
		Elements: []*DataValue{
			{Tag: TagDataUnsigned, Uint: 10},
			{Tag: TagDataUnsigned, Uint: 20},
			{Tag: TagDataUnsigned, Uint: 30},
		},
	}
	b, err := MarshalData(dv)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got, _, err := UnmarshalDataElement(b, 0)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Tag != TagDataArray || len(got.Elements) != 3 {
		t.Fatalf("array: tag=0x%02x elements=%d", got.Tag, len(got.Elements))
	}
	for i, expected := range []uint64{10, 20, 30} {
		if got.Elements[i].Uint != expected {
			t.Errorf("element[%d] = %d, want %d", i, got.Elements[i].Uint, expected)
		}
	}
}

func TestDataAccessErrorRoundTrip(t *testing.T) {
	dv := &DataValue{Tag: TagDataAccessError, ErrCode: 5}
	b, err := MarshalData(dv)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got, _, err := UnmarshalDataElement(b, 0)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Tag != TagDataAccessError || got.ErrCode != 5 {
		t.Errorf("access error = %+v", got)
	}
}

func TestUnmarshalAccessResultsMixed(t *testing.T) {
	var data []byte

	// Element 1: boolean true
	b1, _ := MarshalData(&DataValue{Tag: TagDataBoolean, Bool: true})
	data = append(data, b1...)

	// Element 2: data access error
	b2, _ := MarshalData(&DataValue{Tag: TagDataAccessError, ErrCode: 3})
	data = append(data, b2...)

	// Element 3: integer
	b3, _ := MarshalData(&DataValue{Tag: TagDataInteger, Int: -100})
	data = append(data, b3...)

	results, err := UnmarshalAccessResults(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}
	if !results[0].Bool {
		t.Error("result[0] should be true")
	}
	if results[1].ErrCode != 3 {
		t.Errorf("result[1] error code = %d, want 3", results[1].ErrCode)
	}
	if results[2].Int != -100 {
		t.Errorf("result[2] int = %d, want -100", results[2].Int)
	}
}

func TestNestedStructure(t *testing.T) {
	dv := &DataValue{
		Tag: TagDataStructure,
		Elements: []*DataValue{
			{Tag: TagDataVisibleStr, Str: "outer"},
			{
				Tag: TagDataStructure,
				Elements: []*DataValue{
					{Tag: TagDataInteger, Int: 99},
					{Tag: TagDataBoolean, Bool: false},
				},
			},
		},
	}
	b, err := MarshalData(dv)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got, _, err := UnmarshalDataElement(b, 0)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Elements) != 2 {
		t.Fatalf("outer elements = %d, want 2", len(got.Elements))
	}
	inner := got.Elements[1]
	if inner.Tag != TagDataStructure || len(inner.Elements) != 2 {
		t.Fatalf("inner: tag=0x%02x elements=%d", inner.Tag, len(inner.Elements))
	}
	if inner.Elements[0].Int != 99 {
		t.Errorf("inner[0] int = %d, want 99", inner.Elements[0].Int)
	}
}

func TestDecodeFloatEmptyInput(t *testing.T) {
	data := []byte{TagDataFloat, 0x00}
	_, _, err := UnmarshalDataElement(data, 0)
	if err == nil {
		t.Fatal("expected error for empty float content")
	}
}

func TestDecodeBitStringEmptyContent(t *testing.T) {
	data := []byte{TagDataBitString, 0x00}
	_, _, err := UnmarshalDataElement(data, 0)
	if err == nil {
		t.Fatal("expected error for empty bit string content")
	}
}

func TestDecodeUnsignedInt9BytesValid(t *testing.T) {
	// 9-byte encoding with leading zero: 0x00 + 8 bytes = max uint64
	dv := &DataValue{Tag: TagDataUnsigned, Uint: math.MaxUint64}
	b, err := MarshalData(dv)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, _, err := UnmarshalDataElement(b, 0)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Uint != math.MaxUint64 {
		t.Errorf("uint = %d, want %d", got.Uint, uint64(math.MaxUint64))
	}
}

func TestDecodeUnsignedInt9BytesOverflow(t *testing.T) {
	// 9-byte encoding without leading zero should fail
	content := []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	data := append([]byte{TagDataUnsigned, byte(len(content))}, content...)
	_, _, err := UnmarshalDataElement(data, 0)
	if err == nil {
		t.Fatal("expected error for 9-byte unsigned without leading zero")
	}
}

func TestDecodeUnsignedNegativeEncoding(t *testing.T) {
	// A high bit on the first byte (without leading 0x00 pad) means
	// negative in BER — must be rejected for unsigned integers.
	content := []byte{0x80}
	data := append([]byte{TagDataUnsigned, byte(len(content))}, content...)
	_, _, err := UnmarshalDataElement(data, 0)
	if err == nil {
		t.Fatal("expected error for negative unsigned encoding")
	}
}

func TestUnmarshalAccessResults_TooMany(t *testing.T) {
	single, err := MarshalData(&DataValue{Tag: TagDataBoolean, Bool: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var data []byte
	for i := 0; i < maxAccessResults+1; i++ {
		data = append(data, single...)
	}
	_, err = UnmarshalAccessResults(data)
	if err == nil {
		t.Fatal("expected error for too many access results")
	}
}

func TestDataRealRoundTrip(t *testing.T) {
	cases := []float64{0.0, 1.0, -1.0, 3.14159265358979, math.MaxFloat64, math.SmallestNonzeroFloat64, -42.5}
	for _, val := range cases {
		dv := &DataValue{Tag: TagDataReal, Float: val}
		b, err := MarshalData(dv)
		if err != nil {
			t.Fatalf("marshal real(%v): %v", val, err)
		}
		got, _, err := UnmarshalDataElement(b, 0)
		if err != nil {
			t.Fatalf("unmarshal real(%v): %v", val, err)
		}
		if got.Tag != TagDataReal {
			t.Errorf("real(%v): tag = 0x%02x, want 0x%02x", val, got.Tag, TagDataReal)
		}
		if got.Float != val {
			t.Errorf("real(%v): got %v", val, got.Float)
		}
	}
}

func TestDataRealSpecialValues(t *testing.T) {
	tests := []struct {
		name string
		val  float64
		test func(float64) bool
	}{
		{"positive infinity", math.Inf(1), func(f float64) bool { return math.IsInf(f, 1) }},
		{"negative infinity", math.Inf(-1), func(f float64) bool { return math.IsInf(f, -1) }},
		{"NaN", math.NaN(), func(f float64) bool { return math.IsNaN(f) }},
		{"negative zero", math.Copysign(0, -1), func(f float64) bool { return f == 0 && math.Signbit(f) }},
		{"positive zero", 0, func(f float64) bool { return f == 0 && !math.Signbit(f) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dv := &DataValue{Tag: TagDataReal, Float: tc.val}
			b, err := MarshalData(dv)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			got, _, err := UnmarshalDataElement(b, 0)
			if err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !tc.test(got.Float) {
				t.Errorf("got %v, not %s", got.Float, tc.name)
			}
		})
	}
}

func TestDataBooleanArrayRoundTrip(t *testing.T) {
	data := []byte{0xAA, 0x55, 0xF0}
	dv := &DataValue{Tag: TagDataBooleanArray, Bytes: data, BitLen: 20}
	b, err := MarshalData(dv)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, _, err := UnmarshalDataElement(b, 0)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Tag != TagDataBooleanArray {
		t.Errorf("tag = 0x%02x, want 0x%02x", got.Tag, TagDataBooleanArray)
	}
	if got.BitLen != 20 {
		t.Errorf("bitLen = %d, want 20", got.BitLen)
	}
	if len(got.Bytes) != 3 {
		t.Errorf("bytes len = %d, want 3", len(got.Bytes))
	}
}

func TestDataBooleanArrayEmpty(t *testing.T) {
	dv := &DataValue{Tag: TagDataBooleanArray, Bytes: nil, BitLen: 0}
	b, err := MarshalData(dv)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, _, err := UnmarshalDataElement(b, 0)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Tag != TagDataBooleanArray || got.BitLen != 0 || len(got.Bytes) != 0 {
		t.Errorf("expected empty boolean array, got %+v", got)
	}
}

func TestDataObjIdTagCorrectness(t *testing.T) {
	if TagDataObjId != 0x8f {
		t.Errorf("TagDataObjId = 0x%02x, want 0x8f (context tag [15])", TagDataObjId)
	}
	if TagDataReal != 0x88 {
		t.Errorf("TagDataReal = 0x%02x, want 0x88 (context tag [8])", TagDataReal)
	}
	if TagDataBooleanArray != 0x8e {
		t.Errorf("TagDataBooleanArray = 0x%02x, want 0x8e (context tag [14])", TagDataBooleanArray)
	}
}

func TestEmptyBitStringRoundTrip(t *testing.T) {
	dv := &DataValue{Tag: TagDataBitString, Bytes: nil, BitLen: 0}
	raw, err := MarshalData(dv)
	if err != nil {
		t.Fatalf("MarshalData: %v", err)
	}
	got, _, err := UnmarshalDataElement(raw, 0)
	if err != nil {
		t.Fatalf("UnmarshalDataElement: %v", err)
	}
	if got.Tag != TagDataBitString {
		t.Errorf("tag = 0x%02x, want 0x%02x", got.Tag, TagDataBitString)
	}
	if len(got.Bytes) != 0 {
		t.Errorf("bytes = %v, want empty", got.Bytes)
	}
	if got.BitLen != 0 {
		t.Errorf("bitLen = %d, want 0", got.BitLen)
	}
}
