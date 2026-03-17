package pdu

import (
	"testing"
)

func TestEncodeDecodeAlternateAccess_Component(t *testing.T) {
	selectors := []AccessSelectorWire{{Component: "temperature"}}
	encoded, err := encodeAlternateAccess(selectors)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := decodeAlternateAccess(encoded)
	if err != nil {
		t.Fatal(err)
	}

	if len(decoded) != 1 {
		t.Fatalf("expected 1 selector, got %d", len(decoded))
	}
	if decoded[0].Component != "temperature" {
		t.Errorf("component = %q, want %q", decoded[0].Component, "temperature")
	}
}

func TestEncodeDecodeAlternateAccess_Index(t *testing.T) {
	selectors := []AccessSelectorWire{{HasIndex: true, Index: 42}}
	encoded, err := encodeAlternateAccess(selectors)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := decodeAlternateAccess(encoded)
	if err != nil {
		t.Fatal(err)
	}

	if len(decoded) != 1 {
		t.Fatalf("expected 1 selector, got %d", len(decoded))
	}
	if !decoded[0].HasIndex || decoded[0].Index != 42 {
		t.Errorf("index = %v/%d, want true/42", decoded[0].HasIndex, decoded[0].Index)
	}
}

func TestEncodeDecodeAlternateAccess_IndexRange(t *testing.T) {
	selectors := []AccessSelectorWire{{IndexRange: &IndexRangeWire{LowIndex: 5, NumberOfElements: 3}}}
	encoded, err := encodeAlternateAccess(selectors)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := decodeAlternateAccess(encoded)
	if err != nil {
		t.Fatal(err)
	}

	if len(decoded) != 1 {
		t.Fatalf("expected 1 selector, got %d", len(decoded))
	}
	ir := decoded[0].IndexRange
	if ir == nil {
		t.Fatal("expected IndexRange, got nil")
	}
	if ir.LowIndex != 5 || ir.NumberOfElements != 3 {
		t.Errorf("indexRange = %d/%d, want 5/3", ir.LowIndex, ir.NumberOfElements)
	}
}

func TestEncodeDecodeAlternateAccess_IndexThenComponent(t *testing.T) {
	selectors := []AccessSelectorWire{
		{HasIndex: true, Index: 3},
		{Component: "voltage"},
	}
	encoded, err := encodeAlternateAccess(selectors)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := decodeAlternateAccess(encoded)
	if err != nil {
		t.Fatal(err)
	}

	if len(decoded) != 2 {
		t.Fatalf("expected 2 selectors, got %d", len(decoded))
	}
	if !decoded[0].HasIndex || decoded[0].Index != 3 {
		t.Errorf("selector[0] = %v/%d, want index 3", decoded[0].HasIndex, decoded[0].Index)
	}
	if decoded[1].Component != "voltage" {
		t.Errorf("selector[1] = %q, want %q", decoded[1].Component, "voltage")
	}
}

func TestEncodeDecodeAlternateAccess_ComponentThenIndex(t *testing.T) {
	selectors := []AccessSelectorWire{
		{Component: "measurements"},
		{HasIndex: true, Index: 0},
	}
	encoded, err := encodeAlternateAccess(selectors)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := decodeAlternateAccess(encoded)
	if err != nil {
		t.Fatal(err)
	}

	if len(decoded) != 2 {
		t.Fatalf("expected 2 selectors, got %d", len(decoded))
	}
	if decoded[0].Component != "measurements" {
		t.Errorf("selector[0] = %q, want %q", decoded[0].Component, "measurements")
	}
	if !decoded[1].HasIndex || decoded[1].Index != 0 {
		t.Errorf("selector[1] = %v/%d, want index 0", decoded[1].HasIndex, decoded[1].Index)
	}
}

func TestMarshalReadRequestWithAccess_RoundTrip(t *testing.T) {
	vars := []VariableSpecWire{
		{
			Name:            ObjectNameWire{Scope: ScopeDomain, DomainID: "dom", ItemID: "arr"},
			AlternateAccess: []AccessSelectorWire{{HasIndex: true, Index: 2}},
		},
		{
			Name: ObjectNameWire{Scope: ScopeDomain, DomainID: "dom", ItemID: "plain"},
		},
	}

	pduBytes, err := MarshalReadRequestWithAccess(1, vars)
	if err != nil {
		t.Fatal(err)
	}

	serviceBody := parseConfirmedServiceBody(t, pduBytes)
	specs, err := UnmarshalReadRequestFull(serviceBody)
	if err != nil {
		t.Fatal(err)
	}

	if len(specs) != 2 {
		t.Fatalf("expected 2 specs, got %d", len(specs))
	}

	if specs[0].Name.ItemID != "arr" {
		t.Errorf("specs[0].Name.ItemID = %q, want %q", specs[0].Name.ItemID, "arr")
	}
	if len(specs[0].AlternateAccess) != 1 || !specs[0].AlternateAccess[0].HasIndex || specs[0].AlternateAccess[0].Index != 2 {
		t.Errorf("specs[0].AlternateAccess = %+v, want index 2", specs[0].AlternateAccess)
	}

	if specs[1].Name.ItemID != "plain" {
		t.Errorf("specs[1].Name.ItemID = %q, want %q", specs[1].Name.ItemID, "plain")
	}
	if len(specs[1].AlternateAccess) != 0 {
		t.Errorf("specs[1] should have no alternate access, got %+v", specs[1].AlternateAccess)
	}
}

func TestMarshalWriteRequestWithAccess_RoundTrip(t *testing.T) {
	vars := []VariableSpecWire{
		{
			Name:            ObjectNameWire{Scope: ScopeDomain, DomainID: "dom", ItemID: "arr"},
			AlternateAccess: []AccessSelectorWire{{HasIndex: true, Index: 0}},
		},
	}
	values := []*DataValue{{Tag: TagDataBoolean, Bool: true}}

	pduBytes, err := MarshalWriteRequestWithAccess(1, vars, values)
	if err != nil {
		t.Fatal(err)
	}

	serviceBody := parseConfirmedServiceBody(t, pduBytes)
	specs, data, err := UnmarshalWriteRequestFull(serviceBody)
	if err != nil {
		t.Fatal(err)
	}

	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}
	if len(specs[0].AlternateAccess) != 1 || specs[0].AlternateAccess[0].Index != 0 {
		t.Errorf("specs[0].AlternateAccess = %+v, want index 0", specs[0].AlternateAccess)
	}
	if len(data) != 1 || !data[0].Bool {
		t.Errorf("data[0] = %+v, want bool=true", data[0])
	}
}

func TestDecodeAlternateAccess_EmptyReturnsError(t *testing.T) {
	_, err := encodeAlternateAccess(nil)
	if err == nil {
		t.Fatal("expected error for empty selectors")
	}
}

func TestDecodeAlternateAccess_InvalidTag(t *testing.T) {
	_, err := decodeAlternateAccess([]byte{0xff, 0x00})
	if err == nil {
		t.Fatal("expected error for invalid tag")
	}
}

// parseConfirmedServiceBody extracts the service body from a
// ConfirmedRequestPdu for testing. Skips the outer tag + invoke ID.
func parseConfirmedServiceBody(t *testing.T, pduBytes []byte) []byte {
	t.Helper()

	// Outer ConfirmedRequestPdu [0] CONSTRUCTED
	if pduBytes[0] != 0xa0 {
		t.Fatalf("expected 0xa0 outer tag, got 0x%02x", pduBytes[0])
	}

	inner := extractTLVContent(t, pduBytes)

	// Skip invoke ID (INTEGER)
	if inner[0] != 0x02 {
		t.Fatalf("expected INTEGER tag for invoke ID, got 0x%02x", inner[0])
	}
	_, _, consumed := extractTLVWithSize(t, inner)
	inner = inner[consumed:]

	// Service tag + content
	_, content, _ := extractTLVWithSize(t, inner)
	return content
}

func extractTLVContent(t *testing.T, data []byte) []byte {
	t.Helper()
	_, content, _ := extractTLVWithSize(t, data)
	return content
}

func extractTLVWithSize(t *testing.T, data []byte) (byte, []byte, int) {
	t.Helper()
	if len(data) < 2 {
		t.Fatal("TLV too short")
	}
	tag := data[0]
	lenByte := data[1]
	if lenByte < 128 {
		end := 2 + int(lenByte)
		if end > len(data) {
			t.Fatalf("TLV truncated: need %d, have %d", end, len(data))
		}
		return tag, data[2:end], end
	}
	numBytes := int(lenByte & 0x7f)
	headerLen := 2 + numBytes
	var l int
	for i := 0; i < numBytes; i++ {
		l = (l << 8) | int(data[2+i])
	}
	end := headerLen + l
	if end > len(data) {
		t.Fatalf("TLV truncated: need %d, have %d", end, len(data))
	}
	return tag, data[headerLen:end], end
}
