package pdu

import (
	"testing"
)

func TestInformationReportRoundTrip_ListOfVariable(t *testing.T) {
	orig := &InformationReportWire{
		Variables: []ObjectNameWire{
			{Scope: ScopeDomain, DomainID: "dom1", ItemID: "var1"},
			{Scope: ScopeVMD, ItemID: "vmdVar"},
		},
		Values: []*DataValue{
			{Tag: TagDataInteger, Int: 42},
			{Tag: TagDataBoolean, Bool: true},
		},
	}

	data, err := MarshalInformationReport(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if data[0] != 0xa3 {
		t.Fatalf("outer tag = 0x%02x, want 0xa3", data[0])
	}

	kind, content, err := DecodePdu(data)
	if err != nil {
		t.Fatalf("DecodePdu: %v", err)
	}
	if kind != PduUnconfirmed {
		t.Fatalf("kind = %s, want Unconfirmed", kind)
	}

	got, err := UnmarshalInformationReport(content)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.ListName != nil {
		t.Error("expected nil ListName")
	}
	if len(got.Variables) != 2 {
		t.Fatalf("variables = %d, want 2", len(got.Variables))
	}
	if got.Variables[0].DomainID != "dom1" || got.Variables[0].ItemID != "var1" {
		t.Errorf("var[0] = %+v", got.Variables[0])
	}
	if got.Variables[1].ItemID != "vmdVar" {
		t.Errorf("var[1] = %+v", got.Variables[1])
	}
	if len(got.Values) != 2 {
		t.Fatalf("values = %d, want 2", len(got.Values))
	}
	if got.Values[0].Tag != TagDataInteger || got.Values[0].Int != 42 {
		t.Errorf("value[0] = %+v", got.Values[0])
	}
	if got.Values[1].Tag != TagDataBoolean || !got.Values[1].Bool {
		t.Errorf("value[1] = %+v", got.Values[1])
	}
}

func TestInformationReportRoundTrip_NamedList(t *testing.T) {
	listName := ObjectNameWire{Scope: ScopeVMD, ItemID: "myReport"}
	orig := &InformationReportWire{
		ListName: &listName,
		Values: []*DataValue{
			{Tag: TagDataUnsigned, Uint: 100},
			{Tag: TagDataVisibleStr, Str: "hello"},
		},
	}

	data, err := MarshalInformationReport(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	_, content, err := DecodePdu(data)
	if err != nil {
		t.Fatalf("DecodePdu: %v", err)
	}

	got, err := UnmarshalInformationReport(content)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.ListName == nil {
		t.Fatal("expected non-nil ListName")
	}
	if got.ListName.ItemID != "myReport" {
		t.Errorf("ListName.ItemID = %q, want myReport", got.ListName.ItemID)
	}
	if len(got.Variables) != 0 {
		t.Errorf("expected 0 variables, got %d", len(got.Variables))
	}
	if len(got.Values) != 2 {
		t.Fatalf("values = %d, want 2", len(got.Values))
	}
	if got.Values[0].Uint != 100 {
		t.Errorf("value[0] = %d, want 100", got.Values[0].Uint)
	}
	if got.Values[1].Str != "hello" {
		t.Errorf("value[1] = %q, want hello", got.Values[1].Str)
	}
}

func TestInformationReportRoundTrip_DomainSpecificList(t *testing.T) {
	listName := ObjectNameWire{Scope: ScopeDomain, DomainID: "dom", ItemID: "rptList"}
	orig := &InformationReportWire{
		ListName: &listName,
		Values:   []*DataValue{{Tag: TagDataBoolean, Bool: false}},
	}

	data, err := MarshalInformationReport(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	_, content, err := DecodePdu(data)
	if err != nil {
		t.Fatalf("DecodePdu: %v", err)
	}

	got, err := UnmarshalInformationReport(content)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.ListName == nil || got.ListName.DomainID != "dom" || got.ListName.ItemID != "rptList" {
		t.Errorf("ListName = %+v, want dom/rptList", got.ListName)
	}
}

func TestUnmarshalInformationReportErrors(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"wrong tag", []byte{0xb0, 0x00}},
		{"truncated", []byte{0xa0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := UnmarshalInformationReport(tt.data)
			if err == nil {
				t.Error("expected error")
			}
		})
	}
}
