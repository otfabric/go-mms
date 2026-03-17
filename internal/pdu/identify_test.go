package pdu

import (
	"encoding/asn1"
	"encoding/hex"
	"testing"
)

func TestMarshalIdentifyRequest(t *testing.T) {
	data, err := MarshalIdentifyRequest(1)
	if err != nil {
		t.Fatalf("MarshalIdentifyRequest: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("encoded data is empty")
	}
	if data[0] != 0xa0 {
		t.Fatalf("outer tag = 0x%02x, want 0xa0 (ConfirmedRequest)", data[0])
	}

	kind, content, err := DecodePdu(data)
	if err != nil {
		t.Fatalf("DecodePdu: %v", err)
	}
	if kind != PduConfirmedRequest {
		t.Fatalf("kind = %v, want ConfirmedRequest", kind)
	}

	invokeID, serviceRaw, err := DecodeConfirmedRequestContent(content)
	if err != nil {
		t.Fatalf("decode confirmed request content: %v", err)
	}
	if invokeID != 1 {
		t.Errorf("invokeID = %d, want 1", invokeID)
	}

	// Service tag should be 0x82 (Identify, context 2, primitive, empty).
	svcTag := serviceTagFromRaw(serviceRaw)
	if svcTag != 0x82 {
		t.Errorf("service tag = 0x%02x, want 0x82 (Identify)", svcTag)
	}

	// The Identify request body should be empty.
	if len(serviceRaw.Bytes) != 0 {
		t.Errorf("Identify request body should be empty, got %d bytes", len(serviceRaw.Bytes))
	}

	t.Logf("Encoded IdentifyRequest (%d bytes): %s", len(data), hex.EncodeToString(data))
}

func TestIdentifyResponse_RoundTrip(t *testing.T) {
	// Build a synthetic Identify response body.
	respBody := identifyResponseASN1{
		VendorName: "TestVendor",
		ModelName:  "TestModel",
		Revision:   "1.0.0",
	}
	bodyBytes, err := asn1.Marshal(respBody)
	if err != nil {
		t.Fatalf("marshal response body: %v", err)
	}

	// Wrap as a ConfirmedResponsePdu.
	invokeIDBytes, err := asn1.Marshal(1)
	if err != nil {
		t.Fatalf("marshal invoke ID: %v", err)
	}

	// Wrap body in context 2 constructed tag (Identify response).
	serviceBytes := wrapConstructed(2, bodyBytes)

	innerContent := append(invokeIDBytes, serviceBytes...)

	// Wrap in outer confirmed response tag (0xa1).
	pduData := wrapConstructed(1, innerContent)

	kind, content, err := DecodePdu(pduData)
	if err != nil {
		t.Fatalf("DecodePdu: %v", err)
	}
	if kind != PduConfirmedResponse {
		t.Fatalf("kind = %v, want ConfirmedResponse", kind)
	}

	cr, err := DecodeConfirmedResponse(content)
	if err != nil {
		t.Fatalf("DecodeConfirmedResponse: %v", err)
	}
	if cr.InvokeID != 1 {
		t.Errorf("InvokeID = %d, want 1", cr.InvokeID)
	}
	if cr.ServiceKind != ServiceIdentify {
		t.Errorf("ServiceKind = %v, want Identify", cr.ServiceKind)
	}

	idResp, err := UnmarshalIdentifyResponse(cr.ServiceData)
	if err != nil {
		t.Fatalf("UnmarshalIdentifyResponse: %v", err)
	}
	if idResp.VendorName != "TestVendor" {
		t.Errorf("VendorName = %q, want TestVendor", idResp.VendorName)
	}
	if idResp.ModelName != "TestModel" {
		t.Errorf("ModelName = %q, want TestModel", idResp.ModelName)
	}
	if idResp.Revision != "1.0.0" {
		t.Errorf("Revision = %q, want 1.0.0", idResp.Revision)
	}
}

// wrapConstructed wraps content in a context-specific constructed tag.
func wrapConstructed(tagNum int, content []byte) []byte {
	tag := byte(0xa0 | (tagNum & 0x1f))
	l := len(content)
	if l < 128 {
		buf := make([]byte, 0, 2+l)
		buf = append(buf, tag, byte(l))
		return append(buf, content...)
	}
	if l < 256 {
		buf := make([]byte, 0, 3+l)
		buf = append(buf, tag, 0x81, byte(l))
		return append(buf, content...)
	}
	buf := make([]byte, 0, 4+l)
	buf = append(buf, tag, 0x82, byte(l>>8), byte(l))
	return append(buf, content...)
}

func serviceTagFromRaw(raw asn1.RawValue) byte {
	tag := byte(raw.Class<<6) | byte(raw.Tag&0x1f)
	if raw.IsCompound {
		tag |= 0x20
	}
	return tag
}

// DecodeConfirmedRequestContent is a test helper that parses the
// content of a ConfirmedRequestPdu.
func DecodeConfirmedRequestContent(content []byte) (uint32, asn1.RawValue, error) {
	var invokeInt int
	rest, err := asn1.Unmarshal(content, &invokeInt)
	if err != nil {
		return 0, asn1.RawValue{}, err
	}
	var serviceRaw asn1.RawValue
	_, err = asn1.Unmarshal(rest, &serviceRaw)
	if err != nil {
		return 0, asn1.RawValue{}, err
	}
	return uint32(invokeInt), serviceRaw, nil
}
