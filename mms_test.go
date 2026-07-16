// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"encoding/asn1"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/isostack"
	"github.com/otfabric/go-mms/internal/pdu"
	"github.com/otfabric/go-mms/internal/presentation"
	"github.com/otfabric/go-mms/internal/session"
)

// mockTransport is a bidirectional in-memory transport for testing.
type mockTransport struct {
	mu       sync.Mutex
	closed   bool
	toServer chan []byte
	toClient chan []byte
}

func newMockTransport() *mockTransport {
	return &mockTransport{
		toServer: make(chan []byte, 10),
		toClient: make(chan []byte, 10),
	}
}

func (m *mockTransport) Send(ctx context.Context, data []byte) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return fmt.Errorf("transport closed")
	}
	m.mu.Unlock()

	select {
	case m.toServer <- data:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *mockTransport) Receive(ctx context.Context) ([]byte, error) {
	select {
	case data := <-m.toClient:
		return data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *mockTransport) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockTransport) sendToClient(data []byte) {
	m.toClient <- data
}

func (m *mockTransport) receiveFromClient(ctx context.Context) ([]byte, error) {
	select {
	case data := <-m.toServer:
		return data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// mockServer simulates an MMS server for testing.
type mockServer struct {
	t         *testing.T
	transport *mockTransport
	vendor    string
	model     string
	revision  string
}

func newMockServer(t *testing.T, mt *mockTransport) *mockServer {
	return &mockServer{
		t:         t,
		transport: mt,
		vendor:    "TestVendor",
		model:     "TestModel",
		revision:  "1.0.0",
	}
}

func (s *mockServer) handleAssociation(ctx context.Context) {
	s.t.Helper()
	data, err := s.transport.receiveFromClient(ctx)
	if err != nil {
		s.t.Fatalf("server: receive association: %v", err)
	}

	// Parse session CONNECT → presentation CP → ACSE AARQ → MMS Initiate
	spdu, err := session.Parse(data)
	if err != nil {
		s.t.Fatalf("server: parse session: %v", err)
	}
	if spdu.Type != session.SpduConnect {
		s.t.Fatalf("server: expected CONNECT, got %s", spdu.Type)
	}

	ppdu, err := presentation.Parse(spdu.UserData)
	if err != nil {
		s.t.Fatalf("server: parse presentation: %v", err)
	}

	apdu, err := acse.Parse(ppdu.UserData)
	if err != nil {
		s.t.Fatalf("server: parse ACSE: %v", err)
	}
	if apdu.Type != acse.ApduAARQ {
		s.t.Fatalf("server: expected AARQ, got %s", apdu.Type)
	}

	// Build Initiate Response
	initResp := pdu.InitiateResponse{
		LocalDetailCalled:                  65000,
		NegotiatedMaxServOutstandingCall:   5,
		NegotiatedMaxServOutstandingCalled: 5,
		NegotiatedDataStructureNesting:     10,
		InitResponseDetail: pdu.InitResponseDetail{
			NegotiatedVersion:  1,
			NegotiatedParamCBB: asn1.BitString{Bytes: []byte{0xf1, 0x00}, BitLength: 11},
			ServicesSupportedCalled: asn1.BitString{
				Bytes:     []byte{0xee, 0x1c, 0x00, 0x00, 0x04, 0x08, 0x00, 0x00, 0x79, 0xef, 0x18},
				BitLength: 85,
			},
		},
	}

	initRespBytes, err := codec.MarshalMmsPdu(asn1util.TagInitiateResponse, initResp)
	if err != nil {
		s.t.Fatalf("server: marshal initiate response: %v", err)
	}

	// Build Session ACCEPT → Presentation CPA → ACSE AARE → MMS
	aare := acse.EncodeAARE(acse.ResultAccepted, initRespBytes)
	cpa := presentation.EncodeCPA(nil, aare)
	accept := session.EncodeAccept(session.ConnectParams{}, cpa)

	s.transport.sendToClient(accept)
}

func (s *mockServer) handleDataRequest(ctx context.Context) (codec.InvokeID, pdu.ServiceKind, []byte) {
	s.t.Helper()
	data, err := s.transport.receiveFromClient(ctx)
	if err != nil {
		s.t.Fatalf("server: receive data: %v", err)
	}

	mmsPayload, err := isostack.DecodeDataResponse(data)
	if err != nil {
		s.t.Fatalf("server: decode data: %v", err)
	}

	kind, content, err := pdu.DecodePdu(mmsPayload)
	if err != nil {
		s.t.Fatalf("server: decode PDU: %v", err)
	}

	if kind == pdu.PduConcludeRequest {
		return 0, 0, nil // signal conclude
	}

	if kind != pdu.PduConfirmedRequest {
		s.t.Fatalf("server: expected ConfirmedRequest, got %s", kind)
	}

	invokeID, serviceRaw, err := codec.UnmarshalConfirmedRequest(content)
	if err != nil {
		s.t.Fatalf("server: unmarshal confirmed request: %v", err)
	}

	svcKind := pdu.ClassifyServiceTag(serviceRaw.Tag)
	if svcKind == pdu.ServiceUnknown {
		s.t.Fatalf("server: unknown service tag %d", serviceRaw.Tag)
	}

	return invokeID, svcKind, content
}

func (s *mockServer) sendIdentifyResponse(ctx context.Context, invokeID codec.InvokeID) {
	s.t.Helper()

	type identifyRespASN1 struct {
		VendorName string `asn1:"tag:0,implicit,ia5"`
		ModelName  string `asn1:"tag:1,implicit,ia5"`
		Revision   string `asn1:"tag:2,implicit,ia5"`
	}

	respBody, err := asn1.Marshal(identifyRespASN1{
		VendorName: s.vendor,
		ModelName:  s.model,
		Revision:   s.revision,
	})
	if err != nil {
		s.t.Fatalf("server: marshal identify body: %v", err)
	}

	respPdu, err := codec.MarshalConfirmedResponse(invokeID, asn1util.TagNumIdentify, true, respBody)
	if err != nil {
		s.t.Fatalf("server: marshal identify response: %v", err)
	}
	s.sendDataResponse(ctx, respPdu)
}

func (s *mockServer) sendStatusResponse(ctx context.Context, invokeID codec.InvokeID) {
	s.t.Helper()

	type statusRespASN1 struct {
		VMDLogicalStatus  int `asn1:"tag:0,implicit"`
		VMDPhysicalStatus int `asn1:"tag:1,implicit"`
	}

	respBody, err := asn1.Marshal(statusRespASN1{
		VMDLogicalStatus:  0, // state-changes-allowed
		VMDPhysicalStatus: 0, // operational
	})
	if err != nil {
		s.t.Fatalf("server: marshal status body: %v", err)
	}

	respPdu, err := codec.MarshalConfirmedResponse(invokeID, asn1util.TagNumStatus, true, respBody)
	if err != nil {
		s.t.Fatalf("server: marshal status response: %v", err)
	}
	s.sendDataResponse(ctx, respPdu)
}

func (s *mockServer) sendConcludeResponse(ctx context.Context) {
	s.t.Helper()
	concludeResp := []byte{asn1util.TagConcludeResponse, 0x00}
	s.sendDataResponse(ctx, concludeResp)
}

func (s *mockServer) sendDataResponse(_ context.Context, mmsPdu []byte) {
	s.t.Helper()
	data := isostack.EncodeDataRequest(mmsPdu)
	s.transport.sendToClient(data)
}

func defaultDialOptions() DialOptions {
	return DialOptions{
		ISO: ISOOptions{
			LocalAPTitle:      APTitle{1, 1, 1, 1},
			RemoteAPTitle:     APTitle{1, 1, 1, 1},
			LocalAEQualifier:  12,
			RemoteAEQualifier: 12,
		},
		MMS: MMSOptions{
			MaxPDUSize: 65000,
		},
	}
}

func TestNewClientAndClose(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if client.maxPDUSize != 65000 {
		t.Errorf("maxPDUSize = %d, want 65000", client.maxPDUSize)
	}
	if client.maxOutCalling != 5 {
		t.Errorf("maxOutCalling = %d, want 5", client.maxOutCalling)
	}

	go func() {
		srv.handleDataRequest(ctx) // conclude
		srv.sendConcludeResponse(ctx)
	}()

	if err := client.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestIdentify(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	go func() {
		invokeID, svcKind, _ := srv.handleDataRequest(ctx)
		if svcKind != pdu.ServiceIdentify {
			t.Errorf("server: expected Identify, got %s", svcKind)
		}
		srv.sendIdentifyResponse(ctx, invokeID)
	}()

	identity, err := client.Identify(ctx)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}

	if identity.Vendor != "TestVendor" {
		t.Errorf("Vendor = %q, want %q", identity.Vendor, "TestVendor")
	}
	if identity.Model != "TestModel" {
		t.Errorf("Model = %q, want %q", identity.Model, "TestModel")
	}
	if identity.Revision != "1.0.0" {
		t.Errorf("Revision = %q, want %q", identity.Revision, "1.0.0")
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func TestStatus(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	go func() {
		invokeID, svcKind, _ := srv.handleDataRequest(ctx)
		if svcKind != pdu.ServiceStatus {
			t.Errorf("server: expected Status, got %s", svcKind)
		}
		srv.sendStatusResponse(ctx, invokeID)
	}()

	status, err := client.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if status.Logical != VMDLogicalStatusStateChangesAllowed {
		t.Errorf("Logical = %s, want StateChangesAllowed", status.Logical)
	}
	if status.Physical != VMDPhysicalStatusOperational {
		t.Errorf("Physical = %s, want Operational", status.Physical)
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func TestCloseAlreadyClosed(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()

	client.Close(ctx)

	err = client.Close(ctx)
	if err != nil {
		t.Errorf("second Close should return nil (idempotent), got %v", err)
	}
}

func TestDialContextCancellation(t *testing.T) {
	mt := newMockTransport()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Don't start server — Dial should timeout waiting for association response.
	_, err := NewClient(ctx, mt, defaultDialOptions())
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestIdentifyOnClosedClient(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)

	_, err = client.Identify(ctx)
	if !errors.Is(err, ErrClosed) {
		t.Errorf("Identify after Close: %v, want ErrClosed", err)
	}
}

func TestConfirmedError(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	go func() {
		invokeID, _, _ := srv.handleDataRequest(ctx)

		// Build and send ConfirmedErrorPDU
		invokeIDTLV := berutil.EncodeTLV(0x80, []byte{byte(invokeID)})
		errorClassChoice := berutil.EncodeTLV(0x87, []byte{0x04}) // access, code=4
		errorClass := berutil.EncodeTLV(0xa0, errorClassChoice)
		serviceError := berutil.EncodeTLV(0xa2, errorClass)

		content := make([]byte, 0, len(invokeIDTLV)+len(serviceError))
		content = append(content, invokeIDTLV...)
		content = append(content, serviceError...)

		errorPdu := berutil.EncodeTLV(asn1util.TagConfirmedError, content)
		srv.sendDataResponse(ctx, errorPdu)
	}()

	_, err = client.Identify(ctx)
	if err == nil {
		t.Fatal("expected error from ConfirmedError")
	}

	var svcErr *ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("expected ServiceError, got %T: %v", err, err)
	}
	if svcErr.Class != ErrorClassAccess {
		t.Errorf("ErrorClass = %s, want Access", svcErr.Class)
	}
	if svcErr.Code != 4 {
		t.Errorf("ErrorCode = %d, want 4", svcErr.Code)
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func (s *mockServer) sendReadResponse(ctx context.Context, invokeID codec.InvokeID, dataValues []*pdu.DataValue) {
	s.t.Helper()

	var elements []byte
	for _, dv := range dataValues {
		b, err := pdu.MarshalData(dv)
		if err != nil {
			s.t.Fatalf("server: marshal data: %v", err)
		}
		elements = append(elements, b...)
	}
	seqOf := berutil.EncodeTLV(0x30, elements)

	respPdu, err := codec.MarshalConfirmedResponse(invokeID, asn1util.TagNumRead, true, seqOf)
	if err != nil {
		s.t.Fatalf("server: marshal read response: %v", err)
	}
	s.sendDataResponse(ctx, respPdu)
}

func (s *mockServer) sendWriteResponse(ctx context.Context, invokeID codec.InvokeID, success bool, errCode int) {
	s.t.Helper()

	var content []byte
	if success {
		content = berutil.EncodeTLV(0x81, nil)
	} else {
		content = berutil.EncodeTLV(0x80, []byte{byte(errCode)})
	}

	respPdu, err := codec.MarshalConfirmedResponse(invokeID, asn1util.TagNumWrite, true, content)
	if err != nil {
		s.t.Fatalf("server: marshal write response: %v", err)
	}
	s.sendDataResponse(ctx, respPdu)
}

func TestReadSingleVariable(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	go func() {
		invokeID, svcKind, _ := srv.handleDataRequest(ctx)
		if svcKind != pdu.ServiceRead {
			t.Errorf("server: expected Read, got %s", svcKind)
		}
		srv.sendReadResponse(ctx, invokeID, []*pdu.DataValue{
			{Tag: pdu.TagDataInteger, Int: 42},
		})
	}()

	result, err := client.Read(ctx, ReadRequest{
		DomainID: "TestDomain",
		ItemID:   "TestVar",
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if result.Value.Type() != ValueTypeInteger {
		t.Errorf("type = %s, want Integer", result.Value.Type())
	}
	v, ok := result.Value.Int64()
	if !ok || v != 42 {
		t.Errorf("value = %d (ok=%v), want 42", v, ok)
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func TestReadMultipleVariables(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	go func() {
		invokeID, _, _ := srv.handleDataRequest(ctx)
		srv.sendReadResponse(ctx, invokeID, []*pdu.DataValue{
			{Tag: pdu.TagDataBoolean, Bool: true},
			{Tag: pdu.TagDataVisibleStr, Str: "hello"},
			{Tag: pdu.TagDataFloat, Float: 3.14, FloatWide: false},
		})
	}()

	results, err := client.ReadMultiple(ctx, []ObjectName{
		{Scope: ObjectScopeDomain, Domain: "D1", ItemID: "V1"},
		{Scope: ObjectScopeDomain, Domain: "D1", ItemID: "V2"},
		{Scope: ObjectScopeDomain, Domain: "D1", ItemID: "V3"},
	})
	if err != nil {
		t.Fatalf("ReadMultiple: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}

	b, ok := results[0].Value.Bool()
	if !ok || !b {
		t.Errorf("result[0] bool = %v (ok=%v), want true", b, ok)
	}
	s, ok := results[1].Value.VisibleString()
	if !ok || s != "hello" {
		t.Errorf("result[1] string = %q (ok=%v), want %q", s, ok, "hello")
	}
	f, ok := results[2].Value.Float32()
	if !ok {
		t.Errorf("result[2] float ok = false")
	}
	if f != float32(3.14) {
		t.Errorf("result[2] float = %v, want %v", f, float32(3.14))
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func TestReadDataAccessError(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	go func() {
		invokeID, _, _ := srv.handleDataRequest(ctx)
		srv.sendReadResponse(ctx, invokeID, []*pdu.DataValue{
			{Tag: pdu.TagDataAccessError, ErrCode: 5},
		})
	}()

	_, err = client.Read(ctx, ReadRequest{DomainID: "D", ItemID: "V"})
	if err == nil {
		t.Fatal("expected error from DataAccessError")
	}

	var dae *DataAccessError
	if !errors.As(err, &dae) {
		t.Fatalf("expected DataAccessError, got %T: %v", err, err)
	}
	if dae.Code != DataAccessErrorObjectUndefined {
		t.Errorf("error code = %s, want ObjectUndefined", dae.Code)
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func TestWriteSingleVariable(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	go func() {
		invokeID, svcKind, _ := srv.handleDataRequest(ctx)
		if svcKind != pdu.ServiceWrite {
			t.Errorf("server: expected Write, got %s", svcKind)
		}
		srv.sendWriteResponse(ctx, invokeID, true, 0)
	}()

	_, err = client.Write(ctx, WriteRequest{
		DomainID: "TestDomain",
		ItemID:   "TestVar",
		Value:    NewInteger(100),
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func TestWriteFailure(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	go func() {
		invokeID, _, _ := srv.handleDataRequest(ctx)
		srv.sendWriteResponse(ctx, invokeID, false, 4)
	}()

	_, err = client.Write(ctx, WriteRequest{
		DomainID: "D",
		ItemID:   "V",
		Value:    NewBoolean(true),
	})
	if err == nil {
		t.Fatal("expected error from write failure")
	}

	var dae *DataAccessError
	if !errors.As(err, &dae) {
		t.Fatalf("expected DataAccessError, got %T: %v", err, err)
	}
	if dae.Code != DataAccessErrorObjectAccessDenied {
		t.Errorf("error code = %s, want ObjectAccessDenied", dae.Code)
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func TestReadStructuredValue(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	go func() {
		invokeID, _, _ := srv.handleDataRequest(ctx)
		srv.sendReadResponse(ctx, invokeID, []*pdu.DataValue{
			{
				Tag: pdu.TagDataStructure,
				Elements: []*pdu.DataValue{
					{Tag: pdu.TagDataVisibleStr, Str: "field1"},
					{Tag: pdu.TagDataUnsigned, Uint: 999},
				},
			},
		})
	}()

	result, err := client.Read(ctx, ReadRequest{DomainID: "D", ItemID: "V"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if result.Value.Type() != ValueTypeStructure {
		t.Fatalf("type = %s, want Structure", result.Value.Type())
	}
	elems, ok := result.Value.Structure()
	if !ok || len(elems) != 2 {
		t.Fatalf("structure elements = %d, ok=%v", len(elems), ok)
	}
	str, ok := elems[0].VisibleString()
	if !ok || str != "field1" {
		t.Errorf("elem[0] = %q (ok=%v), want %q", str, ok, "field1")
	}
	u, ok := elems[1].Uint32()
	if !ok || u != 999 {
		t.Errorf("elem[1] = %d (ok=%v), want 999", u, ok)
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func TestReadValidation(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.Read(ctx, ReadRequest{DomainID: "", ItemID: "V"})
	if err == nil {
		t.Error("expected error for empty DomainID")
	}
	_, err = client.Read(ctx, ReadRequest{DomainID: "D", ItemID: ""})
	if err == nil {
		t.Error("expected error for empty ItemID")
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func TestWriteValidation(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.Write(ctx, WriteRequest{DomainID: "", ItemID: "V", Value: NewInteger(1)})
	if err == nil {
		t.Error("expected error for empty DomainID")
	}
	_, err = client.Write(ctx, WriteRequest{DomainID: "D", ItemID: "", Value: NewInteger(1)})
	if err == nil {
		t.Error("expected error for empty ItemID")
	}
	_, err = client.Write(ctx, WriteRequest{DomainID: "D", ItemID: "V", Value: nil})
	if err == nil {
		t.Error("expected error for nil Value")
	}
	_, err = client.Write(ctx, WriteRequest{DomainID: "D", ItemID: "V", Value: NewDataAccessError(5)})
	if err == nil {
		t.Error("expected error for DataAccessError value in write")
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func TestReadResultCountMismatch(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	go func() {
		invokeID, _, _ := srv.handleDataRequest(ctx)
		srv.sendReadResponse(ctx, invokeID, []*pdu.DataValue{
			{Tag: pdu.TagDataInteger, Int: 42},
		})
	}()

	_, err = client.ReadMultiple(ctx, []ObjectName{
		{Scope: ObjectScopeDomain, Domain: "D", ItemID: "V1"},
		{Scope: ObjectScopeDomain, Domain: "D", ItemID: "V2"},
	})
	if err == nil {
		t.Fatal("expected protocol error for result count mismatch")
	}
	var pe *ProtocolError
	if !errors.As(err, &pe) {
		t.Fatalf("expected ProtocolError, got %T: %v", err, err)
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func TestDataAccessErrorSentinel(t *testing.T) {
	dae := &DataAccessError{Code: DataAccessErrorObjectUndefined}
	if !errors.Is(dae, ErrDataAccess) {
		t.Error("DataAccessError should wrap ErrDataAccess")
	}
	if errors.Is(dae, ErrServiceRejected) {
		t.Error("DataAccessError should NOT wrap ErrServiceRejected")
	}
}

// --- GetNameList tests ---

func (s *mockServer) sendGetNameListResponse(ctx context.Context, invokeID codec.InvokeID, names []string, moreFollows bool) {
	s.t.Helper()

	var nameEntries []byte
	for _, n := range names {
		nameEntries = append(nameEntries, berutil.EncodeTLV(0x1a, []byte(n))...)
	}
	list := berutil.EncodeTLV(0xa0, nameEntries)

	var content []byte
	content = append(content, list...)
	if !moreFollows {
		content = append(content, berutil.EncodeTLV(0x81, []byte{0x00})...)
	}

	respPdu, err := codec.MarshalConfirmedResponse(invokeID, asn1util.TagNumGetNameList, true, content)
	if err != nil {
		s.t.Fatalf("server: marshal getnamelist response: %v", err)
	}
	s.sendDataResponse(ctx, respPdu)
}

func TestGetNameListVMD(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	go func() {
		invokeID, svcKind, _ := srv.handleDataRequest(ctx)
		if svcKind != pdu.ServiceGetNameList {
			t.Errorf("server: expected GetNameList, got %s", svcKind)
		}
		srv.sendGetNameListResponse(ctx, invokeID, []string{"Domain1", "Domain2"}, false)
	}()

	result, err := client.GetNameList(ctx, NameListRequest{
		ObjectClass: ObjectClassDomain,
		Scope:       ObjectScopeVMD,
	})
	if err != nil {
		t.Fatalf("GetNameList: %v", err)
	}
	if len(result.Names) != 2 {
		t.Fatalf("names = %d, want 2", len(result.Names))
	}
	if result.Names[0] != "Domain1" || result.Names[1] != "Domain2" {
		t.Errorf("names = %v", result.Names)
	}
	if result.MoreFollows {
		t.Error("moreFollows should be false")
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func TestGetNameListDomainSpecific(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	go func() {
		invokeID, _, _ := srv.handleDataRequest(ctx)
		srv.sendGetNameListResponse(ctx, invokeID, []string{"Var1", "Var2", "Var3"}, false)
	}()

	result, err := client.GetNameList(ctx, NameListRequest{
		ObjectClass: ObjectClassNamedVariable,
		Scope:       ObjectScopeDomain,
		DomainID:    "TestDomain",
	})
	if err != nil {
		t.Fatalf("GetNameList: %v", err)
	}
	if len(result.Names) != 3 {
		t.Fatalf("names = %d, want 3", len(result.Names))
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func TestGetNameListContinuation(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	go func() {
		// First request
		invokeID, _, _ := srv.handleDataRequest(ctx)
		srv.sendGetNameListResponse(ctx, invokeID, []string{"A", "B"}, true)

		// Second request (continuation)
		invokeID, _, _ = srv.handleDataRequest(ctx)
		srv.sendGetNameListResponse(ctx, invokeID, []string{"C", "D"}, false)
	}()

	all, err := client.GetNameListAll(ctx, NameListRequest{
		ObjectClass: ObjectClassNamedVariable,
		Scope:       ObjectScopeDomain,
		DomainID:    "D",
	})
	if err != nil {
		t.Fatalf("GetNameListAll: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("total names = %d, want 4", len(all))
	}
	want := []string{"A", "B", "C", "D"}
	for i, n := range all {
		if n != want[i] {
			t.Errorf("name[%d] = %q, want %q", i, n, want[i])
		}
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

// --- GetVariableAccessAttributes tests ---

func (s *mockServer) sendGetVarAccessResponse(ctx context.Context, invokeID codec.InvokeID, deletable bool, typeSpecContent []byte) {
	s.t.Helper()

	var del byte
	if deletable {
		del = 0xff
	}
	mmsDeletable := berutil.EncodeTLV(0x80, []byte{del})
	typeSpec := berutil.EncodeTLV(0xa2, typeSpecContent)

	content := append(mmsDeletable, typeSpec...)

	respPdu, err := codec.MarshalConfirmedResponse(invokeID, asn1util.TagNumGetVariableAccessAttributes, true, content)
	if err != nil {
		s.t.Fatalf("server: marshal getvaraccess response: %v", err)
	}
	s.sendDataResponse(ctx, respPdu)
}

func TestGetVariableAccessAttributesInteger(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	go func() {
		invokeID, svcKind, _ := srv.handleDataRequest(ctx)
		if svcKind != pdu.ServiceGetVariableAccessAttrs {
			t.Errorf("server: expected GetVariableAccessAttributes, got %s", svcKind)
		}
		// integer [5] 32 bits
		ts := berutil.EncodeTLV(0x85, []byte{32})
		srv.sendGetVarAccessResponse(ctx, invokeID, false, ts)
	}()

	attrs, err := client.GetVariableAccessAttributes(ctx, ObjectName{
		Scope:  ObjectScopeDomain,
		Domain: "TestDomain",
		ItemID: "TestVar",
	})
	if err != nil {
		t.Fatalf("GetVariableAccessAttributes: %v", err)
	}
	if attrs.TypeSpec.Type != ValueTypeInteger {
		t.Errorf("type = %s, want Integer", attrs.TypeSpec.Type)
	}
	if attrs.TypeSpec.Size != 32 {
		t.Errorf("size = %d, want 32", attrs.TypeSpec.Size)
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func TestGetVariableAccessAttributesStructure(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	go func() {
		invokeID, _, _ := srv.handleDataRequest(ctx)

		// Structure with two components: "enabled" boolean, "count" unsigned 16
		comp1Name := berutil.EncodeTLV(0x80, []byte("enabled"))
		comp1Type := berutil.EncodeTLV(0xa1, berutil.EncodeTLV(0x83, nil)) // boolean
		comp1 := berutil.EncodeTLV(0x30, append(comp1Name, comp1Type...))

		comp2Name := berutil.EncodeTLV(0x80, []byte("count"))
		comp2Type := berutil.EncodeTLV(0xa1, berutil.EncodeTLV(0x86, []byte{16})) // unsigned 16
		comp2 := berutil.EncodeTLV(0x30, append(comp2Name, comp2Type...))

		components := berutil.EncodeTLV(0xa1, append(comp1, comp2...))
		ts := berutil.EncodeTLV(0xa2, components)
		srv.sendGetVarAccessResponse(ctx, invokeID, false, ts)
	}()

	attrs, err := client.GetVariableAccessAttributes(ctx, ObjectName{Scope: ObjectScopeDomain, Domain: "D", ItemID: "V"})
	if err != nil {
		t.Fatalf("GetVariableAccessAttributes: %v", err)
	}
	if attrs.TypeSpec.Type != ValueTypeStructure {
		t.Fatalf("type = %s, want Structure", attrs.TypeSpec.Type)
	}
	if len(attrs.TypeSpec.Elements) != 2 {
		t.Fatalf("elements = %d, want 2", len(attrs.TypeSpec.Elements))
	}
	if attrs.TypeSpec.Elements[0].Name != "enabled" || attrs.TypeSpec.Elements[0].Type.Type != ValueTypeBoolean {
		t.Errorf("element[0] = %+v", attrs.TypeSpec.Elements[0])
	}
	if attrs.TypeSpec.Elements[1].Name != "count" || attrs.TypeSpec.Elements[1].Type.Type != ValueTypeUnsigned || attrs.TypeSpec.Elements[1].Type.Size != 16 {
		t.Errorf("element[1] = %+v", attrs.TypeSpec.Elements[1])
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

// --- Named Variable List tests ---

func (s *mockServer) sendDefineNamedVarListResponse(ctx context.Context, invokeID codec.InvokeID) {
	s.t.Helper()

	respPdu, err := codec.MarshalConfirmedResponse(invokeID, asn1util.TagNumDefineNamedVariableList, true, nil)
	if err != nil {
		s.t.Fatalf("server: marshal define response: %v", err)
	}
	s.sendDataResponse(ctx, respPdu)
}

func (s *mockServer) sendGetNamedVarListAttrsResponse(ctx context.Context, invokeID codec.InvokeID, deletable bool, variables []pdu.ObjectNameWire) {
	s.t.Helper()

	var del byte
	if deletable {
		del = 0xff
	}
	mmsDel := berutil.EncodeTLV(0x80, []byte{del})

	var entries []byte
	for _, v := range variables {
		objName, _ := pdu.EncodeObjectName(v)
		varSpec := asn1util.WrapConstructed(0, objName)
		entry := berutil.EncodeTLV(0x30, varSpec)
		entries = append(entries, entry...)
	}
	listOfVar := berutil.EncodeTLV(0xa1, entries)

	content := append(mmsDel, listOfVar...)

	respPdu, err := codec.MarshalConfirmedResponse(invokeID, asn1util.TagNumGetNamedVariableListAttrs, true, content)
	if err != nil {
		s.t.Fatalf("server: marshal get attrs response: %v", err)
	}
	s.sendDataResponse(ctx, respPdu)
}

func (s *mockServer) sendDeleteNamedVarListResponse(ctx context.Context, invokeID codec.InvokeID, matched, deleted int) {
	s.t.Helper()

	matchedBytes := berutil.EncodeTLV(0x02, []byte{byte(matched)})
	deletedBytes := berutil.EncodeTLV(0x02, []byte{byte(deleted)})
	content := append(matchedBytes, deletedBytes...)

	respPdu, err := codec.MarshalConfirmedResponse(invokeID, asn1util.TagNumDeleteNamedVariableList, true, content)
	if err != nil {
		s.t.Fatalf("server: marshal delete response: %v", err)
	}
	s.sendDataResponse(ctx, respPdu)
}

func TestDefineNamedVariableList(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	go func() {
		invokeID, svcKind, _ := srv.handleDataRequest(ctx)
		if svcKind != pdu.ServiceDefineNamedVariableList {
			t.Errorf("server: expected DefineNamedVariableList, got %s", svcKind)
		}
		srv.sendDefineNamedVarListResponse(ctx, invokeID)
	}()

	err = client.DefineNamedVariableList(ctx, DefineNamedVariableListRequest{
		ListName: ObjectName{Scope: ObjectScopeDomain, Domain: "D", ItemID: "MyList"},
		Variables: []VariableSpec{
			{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "D", ItemID: "V1"}},
			{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "D", ItemID: "V2"}},
		},
	})
	if err != nil {
		t.Fatalf("DefineNamedVariableList: %v", err)
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func TestGetNamedVariableListAttributes(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	go func() {
		invokeID, svcKind, _ := srv.handleDataRequest(ctx)
		if svcKind != pdu.ServiceGetNamedVariableListAttrs {
			t.Errorf("server: expected GetNamedVariableListAttributes, got %s", svcKind)
		}
		srv.sendGetNamedVarListAttrsResponse(ctx, invokeID, true, []pdu.ObjectNameWire{
			{Scope: pdu.ScopeDomain, DomainID: "D", ItemID: "V1"},
			{Scope: pdu.ScopeDomain, DomainID: "D", ItemID: "V2"},
		})
	}()

	attrs, err := client.GetNamedVariableListAttributes(ctx, ObjectName{Scope: ObjectScopeDomain, Domain: "D", ItemID: "MyList"})
	if err != nil {
		t.Fatalf("GetNamedVariableListAttributes: %v", err)
	}
	if !attrs.Deletable {
		t.Error("expected deletable = true")
	}
	if len(attrs.Variables) != 2 {
		t.Fatalf("variables = %d, want 2", len(attrs.Variables))
	}
	if attrs.Variables[0].Name.Domain != "D" || attrs.Variables[0].Name.ItemID != "V1" {
		t.Errorf("variable[0] = %+v", attrs.Variables[0])
	}
	if attrs.Variables[1].Name.Domain != "D" || attrs.Variables[1].Name.ItemID != "V2" {
		t.Errorf("variable[1] = %+v", attrs.Variables[1])
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func TestDeleteNamedVariableList(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	go func() {
		invokeID, svcKind, _ := srv.handleDataRequest(ctx)
		if svcKind != pdu.ServiceDeleteNamedVariableList {
			t.Errorf("server: expected DeleteNamedVariableList, got %s", svcKind)
		}
		srv.sendDeleteNamedVarListResponse(ctx, invokeID, 1, 1)
	}()

	result, err := client.DeleteNamedVariableList(ctx, []ObjectName{
		{Scope: ObjectScopeDomain, Domain: "D", ItemID: "MyList"},
	})
	if err != nil {
		t.Fatalf("DeleteNamedVariableList: %v", err)
	}
	if result.NumberMatched != 1 {
		t.Errorf("matched = %d, want 1", result.NumberMatched)
	}
	if result.NumberDeleted != 1 {
		t.Errorf("deleted = %d, want 1", result.NumberDeleted)
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func TestNamedVariableListLifecycle(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	go func() {
		// 1. Define
		invokeID, svcKind, _ := srv.handleDataRequest(ctx)
		if svcKind != pdu.ServiceDefineNamedVariableList {
			t.Errorf("expected Define, got %s", svcKind)
		}
		srv.sendDefineNamedVarListResponse(ctx, invokeID)

		// 2. GetAttributes
		invokeID, svcKind, _ = srv.handleDataRequest(ctx)
		if svcKind != pdu.ServiceGetNamedVariableListAttrs {
			t.Errorf("expected GetAttrs, got %s", svcKind)
		}
		srv.sendGetNamedVarListAttrsResponse(ctx, invokeID, true, []pdu.ObjectNameWire{
			{Scope: pdu.ScopeDomain, DomainID: "D", ItemID: "V1"},
		})

		// 3. Delete
		invokeID, svcKind, _ = srv.handleDataRequest(ctx)
		if svcKind != pdu.ServiceDeleteNamedVariableList {
			t.Errorf("expected Delete, got %s", svcKind)
		}
		srv.sendDeleteNamedVarListResponse(ctx, invokeID, 1, 1)

		// 4. Conclude
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()

	// Define
	err = client.DefineNamedVariableList(ctx, DefineNamedVariableListRequest{
		ListName:  ObjectName{Scope: ObjectScopeDomain, Domain: "D", ItemID: "MyList"},
		Variables: []VariableSpec{{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "D", ItemID: "V1"}}},
	})
	if err != nil {
		t.Fatalf("Define: %v", err)
	}

	// Get attributes
	attrs, err := client.GetNamedVariableListAttributes(ctx, ObjectName{Scope: ObjectScopeDomain, Domain: "D", ItemID: "MyList"})
	if err != nil {
		t.Fatalf("GetAttrs: %v", err)
	}
	if len(attrs.Variables) != 1 || !attrs.Deletable {
		t.Errorf("attrs = %+v", attrs)
	}

	// Delete
	delResult, err := client.DeleteNamedVariableList(ctx, []ObjectName{
		{Scope: ObjectScopeDomain, Domain: "D", ItemID: "MyList"},
	})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if delResult.NumberDeleted != 1 {
		t.Errorf("deleted = %d, want 1", delResult.NumberDeleted)
	}

	client.Close(ctx)
}

func TestDefineNamedVariableListValidation(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	err = client.DefineNamedVariableList(ctx, DefineNamedVariableListRequest{
		ListName:  ObjectName{Scope: ObjectScopeDomain, Domain: "D", ItemID: ""},
		Variables: []VariableSpec{{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "D", ItemID: "V1"}}},
	})
	if err == nil {
		t.Error("expected error for empty list name")
	}

	err = client.DefineNamedVariableList(ctx, DefineNamedVariableListRequest{
		ListName:  ObjectName{Scope: ObjectScopeDomain, Domain: "D", ItemID: "MyList"},
		Variables: nil,
	})
	if err == nil {
		t.Error("expected error for empty variables")
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func TestDeleteNamedVariableListValidation(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.DeleteNamedVariableList(ctx, nil)
	if err == nil {
		t.Error("expected error for empty list names")
	}

	_, err = client.DeleteNamedVariableList(ctx, []ObjectName{
		{Scope: ObjectScopeDomain, Domain: "D", ItemID: ""},
	})
	if err == nil {
		t.Error("expected error for empty ItemID in delete")
	}

	_, err = client.DeleteNamedVariableList(ctx, []ObjectName{
		{Scope: ObjectScopeDomain, Domain: "", ItemID: "List1"},
	})
	if err == nil {
		t.Error("expected error for domain scope with empty Domain")
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func TestGetNameListValidation(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.GetNameList(ctx, NameListRequest{
		ObjectClass: ObjectClassNamedVariable,
		Scope:       ObjectScopeDomain,
		DomainID:    "", // missing
	})
	if err == nil {
		t.Error("expected error for domain scope with empty DomainID")
	}

	_, err = client.GetNameList(ctx, NameListRequest{
		ObjectClass: ObjectClassNamedVariable,
		Scope:       ObjectScope(99), // invalid
	})
	if err == nil {
		t.Error("expected error for unknown scope")
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func TestDefineNamedVariableListDeepValidation(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// Variable with empty ItemID
	err = client.DefineNamedVariableList(ctx, DefineNamedVariableListRequest{
		ListName:  ObjectName{Scope: ObjectScopeDomain, Domain: "D", ItemID: "MyList"},
		Variables: []VariableSpec{{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "D", ItemID: ""}}},
	})
	if err == nil {
		t.Error("expected error for variable with empty ItemID")
	}

	// Domain-scope variable without domain
	err = client.DefineNamedVariableList(ctx, DefineNamedVariableListRequest{
		ListName:  ObjectName{Scope: ObjectScopeDomain, Domain: "D", ItemID: "MyList"},
		Variables: []VariableSpec{{Name: ObjectName{Scope: ObjectScopeDomain, Domain: "", ItemID: "V1"}}},
	})
	if err == nil {
		t.Error("expected error for domain-scope variable without Domain")
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func TestGetNameListAllStalledPagination(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	go func() {
		// First page
		invokeID, _, _ := srv.handleDataRequest(ctx)
		srv.sendGetNameListResponse(ctx, invokeID, []string{"A", "B"}, true)

		// Second page returns same last name — stalled
		invokeID, _, _ = srv.handleDataRequest(ctx)
		srv.sendGetNameListResponse(ctx, invokeID, []string{"B"}, true)
	}()

	_, err = client.GetNameListAll(ctx, NameListRequest{
		ObjectClass: ObjectClassNamedVariable,
		Scope:       ObjectScopeDomain,
		DomainID:    "D",
	})
	if err == nil {
		t.Fatal("expected error for stalled pagination")
	}
	var pe *ProtocolError
	if !errors.As(err, &pe) {
		t.Fatalf("expected ProtocolError, got %T: %v", err, err)
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func TestEncodeObjectNameInvalidScope(t *testing.T) {
	_, err := pdu.EncodeObjectName(pdu.ObjectNameWire{Scope: 99, ItemID: "test"})
	if err == nil {
		t.Error("expected error for invalid scope")
	}
}

func TestObjectScopeString(t *testing.T) {
	tests := []struct {
		s    ObjectScope
		want string
	}{
		{ObjectScopeVMD, "VMD"},
		{ObjectScopeDomain, "Domain"},
		{ObjectScopeAssociation, "Association"},
		{ObjectScope(99), "ObjectScope(99)"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("ObjectScope(%d).String() = %q, want %q", int(tt.s), got, tt.want)
		}
	}
}

func TestObjectNameScopeRoundTrip(t *testing.T) {
	names := []ObjectName{
		{Scope: ObjectScopeVMD, ItemID: "vmdVar"},
		{Scope: ObjectScopeDomain, Domain: "dom1", ItemID: "var1"},
		{Scope: ObjectScopeAssociation, ItemID: "aaVar"},
	}
	for _, n := range names {
		wire := pdu.ObjectNameWire{
			Scope:    int(n.Scope),
			DomainID: string(n.Domain),
			ItemID:   string(n.ItemID),
		}
		encoded, err := pdu.EncodeObjectName(wire)
		if err != nil {
			t.Fatalf("EncodeObjectName(%+v): %v", wire, err)
		}
		decoded, err := pdu.DecodeObjectName(encoded)
		if err != nil {
			t.Fatalf("DecodeObjectName: %v", err)
		}
		if decoded.Scope != wire.Scope || decoded.DomainID != wire.DomainID || decoded.ItemID != wire.ItemID {
			t.Errorf("round-trip mismatch: %+v != %+v", decoded, wire)
		}
	}
}

func TestReadMultipleValidation(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	tests := []struct {
		name string
		vars []ObjectName
	}{
		{"empty ItemID", []ObjectName{{Scope: ObjectScopeDomain, Domain: "d", ItemID: ""}}},
		{"domain scope no domain", []ObjectName{{Scope: ObjectScopeDomain, Domain: "", ItemID: "x"}}},
		{"unknown scope", []ObjectName{{Scope: ObjectScope(99), ItemID: "x"}}},
	}
	for _, tt := range tests {
		_, err := client.ReadMultiple(ctx, tt.vars)
		if err == nil {
			t.Errorf("%s: expected validation error", tt.name)
		}
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func TestGetVariableAccessAttributesValidation(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.GetVariableAccessAttributes(ctx, ObjectName{Scope: ObjectScopeDomain, Domain: "d", ItemID: ""})
	if err == nil {
		t.Error("expected error for empty ItemID")
	}

	_, err = client.GetVariableAccessAttributes(ctx, ObjectName{Scope: ObjectScope(42), ItemID: "x"})
	if err == nil {
		t.Error("expected error for unknown scope")
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func TestGetNameListObjectClassValidation(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.GetNameList(ctx, NameListRequest{
		ObjectClass: ObjectClass(99),
		Scope:       ObjectScopeVMD,
	})
	if err == nil {
		t.Error("expected error for invalid ObjectClass")
	}

	_, err = client.GetNameList(ctx, NameListRequest{
		ObjectClass: ObjectClass(-1),
		Scope:       ObjectScopeVMD,
	})
	if err == nil {
		t.Error("expected error for negative ObjectClass")
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func TestGetNamedVariableListAttributesValidation(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.GetNamedVariableListAttributes(ctx, ObjectName{Scope: ObjectScopeDomain, Domain: "", ItemID: "list"})
	if err == nil {
		t.Error("expected error for domain scope with empty Domain")
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}

func TestValueTypeNamedType(t *testing.T) {
	if ValueTypeNamedType.String() != "NamedType" {
		t.Errorf("ValueTypeNamedType.String() = %q, want %q", ValueTypeNamedType.String(), "NamedType")
	}

	ts := TypeSpec{Type: ValueTypeNamedType}
	if ts.Type != ValueTypeNamedType {
		t.Errorf("TypeSpec.Type = %v, want ValueTypeNamedType", ts.Type)
	}
	if ts.Type == ValueTypeBoolean {
		t.Error("ValueTypeNamedType should not equal ValueTypeBoolean")
	}
}

func TestDeleteNamedVariableListUnknownScope(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	ctx := context.Background()

	go srv.handleAssociation(ctx)

	client, err := NewClient(ctx, mt, defaultDialOptions())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.DeleteNamedVariableList(ctx, []ObjectName{
		{Scope: ObjectScope(77), ItemID: "x"},
	})
	if err == nil {
		t.Error("expected error for unknown scope")
	}

	go func() {
		srv.handleDataRequest(ctx)
		srv.sendConcludeResponse(ctx)
	}()
	client.Close(ctx)
}
