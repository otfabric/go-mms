// SPDX-License-Identifier: MIT

package serverconn

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/isostack"
	"github.com/otfabric/go-mms/internal/pdu"
)

// scriptTransport is a synchronous fake Transport. Receive pops the next
// scripted frame (or returns recvErr / io.EOF once the script is exhausted),
// Send records every frame and can be configured to fail after a number of
// successful sends. Because Receive never blocks, Serve can run on the test
// goroutine and no extra synchronization is needed.
type scriptTransport struct {
	mu        sync.Mutex
	in        [][]byte
	recvErr   error
	sent      [][]byte
	sendErr   error
	sendOK    int // number of sends that succeed before sendErr is returned
	closeErr  error
	closeCnt  int
	recvCalls int
}

func (t *scriptTransport) Send(_ context.Context, data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sendErr != nil && len(t.sent) >= t.sendOK {
		return t.sendErr
	}
	t.sent = append(t.sent, bytes.Clone(data))
	return nil
}

func (t *scriptTransport) Receive(_ context.Context) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.recvCalls++
	if len(t.in) == 0 {
		if t.recvErr != nil {
			return nil, t.recvErr
		}
		return nil, io.EOF
	}
	frame := t.in[0]
	t.in = t.in[1:]
	return frame, nil
}

func (t *scriptTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closeCnt++
	return t.closeErr
}

func (t *scriptTransport) sentFrames() [][]byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([][]byte(nil), t.sent...)
}

func acseParamsWithPassword(pw []byte) acse.AARQParams { return acse.AARQParams{Password: pw} }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func nopHandler(_ context.Context, _ codec.InvokeID, _ int, _ []byte) (int, bool, []byte, error) {
	return 0, false, nil, nil
}

func defaultOpts() MMSOptions {
	return MMSOptions{MaxPDUSize: 65000, MaxOutstandingCalling: 5, MaxOutstandingCalled: 5, DataStructureNestingLevel: 10}
}

func mustAssocReq(t *testing.T, mmsPayload []byte) []byte {
	t.Helper()
	b, err := isostack.EncodeAssociateRequest(isostack.Params{}, mmsPayload)
	if err != nil {
		t.Fatalf("EncodeAssociateRequest: %v", err)
	}
	return b
}

func mustInitReq(t *testing.T, maxPDU, calling, called, nesting int) []byte {
	t.Helper()
	b, err := pdu.MarshalInitiateRequest(pdu.DefaultInitiateRequest(maxPDU, calling, called, nesting))
	if err != nil {
		t.Fatalf("MarshalInitiateRequest: %v", err)
	}
	return b
}

func dataFrame(mmsPdu []byte) []byte { return isostack.EncodeDataRequest(mmsPdu) }

func mustConfirmedReq(t *testing.T, invokeID codec.InvokeID, tag int, payload []byte) []byte {
	t.Helper()
	b, err := codec.MarshalConfirmedRequest(invokeID, tag, true, payload)
	if err != nil {
		t.Fatalf("MarshalConfirmedRequest: %v", err)
	}
	return b
}

// decodeSentMMS decodes a frame sent by the server as session DATA and
// returns the MMS PDU kind and inner content.
func decodeSentMMS(t *testing.T, frame []byte) (pdu.PduKind, []byte) {
	t.Helper()
	mms, err := isostack.DecodeDataResponse(frame)
	if err != nil {
		t.Fatalf("DecodeDataResponse: %v", err)
	}
	kind, content, err := pdu.DecodePdu(mms)
	if err != nil {
		t.Fatalf("DecodePdu: %v", err)
	}
	return kind, content
}

func expectConfirmedError(t *testing.T, frame []byte, invokeID codec.InvokeID, class, code int) {
	t.Helper()
	kind, content := decodeSentMMS(t, frame)
	if kind != pdu.PduConfirmedError {
		t.Fatalf("kind = %s, want ConfirmedError", kind)
	}
	ce, err := pdu.DecodeConfirmedError(content)
	if err != nil {
		t.Fatalf("DecodeConfirmedError: %v", err)
	}
	if ce.InvokeID != invokeID || ce.ErrorClass != class || ce.ErrorCode != code {
		t.Fatalf("ConfirmedError = %+v, want invokeID=%d class=%d code=%d", *ce, invokeID, class, code)
	}
}

func expectConcludeResponse(t *testing.T, frame []byte) {
	t.Helper()
	if kind, _ := decodeSentMMS(t, frame); kind != pdu.PduConcludeResponse {
		t.Fatalf("kind = %s, want ConcludeResponse", kind)
	}
}

// --- ReceiveAssociation -----------------------------------------------------

func TestReceiveAssociation_Errors(t *testing.T) {
	recvErr := errors.New("boom")
	tests := []struct {
		name    string
		tr      *scriptTransport
		wantIs  error
		wantSub string
	}{
		{
			name:   "transport receive error",
			tr:     &scriptTransport{recvErr: recvErr},
			wantIs: recvErr, wantSub: "receive association",
		},
		{
			name:    "bad associate request",
			tr:      &scriptTransport{in: [][]byte{{0x00, 0x01, 0x02}}},
			wantSub: "isostack",
		},
		{
			name:    "not an associate request (session DATA)",
			tr:      &scriptTransport{in: [][]byte{dataFrame(codec.MarshalConcludeRequest())}},
			wantSub: "expected CONNECT",
		},
		{
			name:    "undecodable MMS payload",
			tr:      &scriptTransport{in: [][]byte{mustAssocReq(t, []byte{0x1f, 0xff, 0xff, 0xff, 0xff, 0xff})}},
			wantSub: "decode MMS initiate",
		},
		{
			name:    "non-initiate MMS PDU",
			tr:      &scriptTransport{in: [][]byte{mustAssocReq(t, codec.MarshalConcludeRequest())}},
			wantSub: "expected InitiateRequest",
		},
		{
			name:    "malformed initiate request",
			tr:      &scriptTransport{in: [][]byte{mustAssocReq(t, []byte{0xa8, 0x02, 0x05, 0x00})}},
			wantSub: "serverconn:",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := New(tc.tr, discardLogger(), nopHandler, defaultOpts())
			auth, err := c.ReceiveAssociation(context.Background())
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.wantIs != nil && !errors.Is(err, tc.wantIs) {
				t.Fatalf("error %v does not wrap %v", err, tc.wantIs)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q does not contain %q", err, tc.wantSub)
			}
			if auth.Password != nil || auth.MechanismOID != nil {
				t.Fatalf("expected zero auth, got %+v", auth)
			}
			if c.pendingInitReq != nil {
				t.Fatal("pendingInitReq set after failed ReceiveAssociation")
			}
			// A failed ReceiveAssociation leaves nothing to accept.
			if err := c.AcceptAssociation(context.Background()); err == nil {
				t.Fatal("AcceptAssociation after failed receive should fail")
			}
			if n := len(tc.tr.sentFrames()); n != 0 {
				t.Fatalf("sent %d frames, want 0", n)
			}
		})
	}
}

func TestReceiveAssociation_PasswordAuth(t *testing.T) {
	init := mustInitReq(t, 65000, 5, 5, 10)
	req, err := isostack.EncodeAssociateRequest(isostack.Params{
		ACSE: acseParamsWithPassword([]byte("s3cret")),
	}, init)
	if err != nil {
		t.Fatal(err)
	}
	tr := &scriptTransport{in: [][]byte{req}}
	c := New(tr, discardLogger(), nopHandler, defaultOpts())
	auth, err := c.ReceiveAssociation(context.Background())
	if err != nil {
		t.Fatalf("ReceiveAssociation: %v", err)
	}
	if string(auth.Password) != "s3cret" {
		t.Fatalf("password = %q", auth.Password)
	}
}

// --- AcceptAssociation / negotiation / MaxPDUSize ---------------------------

func TestAcceptAssociation_NegotiatesAndEncodesResponse(t *testing.T) {
	// Client proposes values below the server maxima; the PDU size is
	// clamped up to the minimum.
	tr := &scriptTransport{in: [][]byte{mustAssocReq(t, mustInitReq(t, 64, 3, 2, 4))}}
	c := New(tr, discardLogger(), nopHandler, defaultOpts())
	ctx := context.Background()

	if got := c.MaxPDUSize(); got != 65000 {
		t.Fatalf("MaxPDUSize before accept = %d, want 65000", got)
	}
	if _, err := c.ReceiveAssociation(ctx); err != nil {
		t.Fatalf("ReceiveAssociation: %v", err)
	}
	if err := c.AcceptAssociation(ctx); err != nil {
		t.Fatalf("AcceptAssociation: %v", err)
	}
	if got := c.MaxPDUSize(); got != minPDUSize {
		t.Fatalf("MaxPDUSize after accept = %d, want %d", got, minPDUSize)
	}

	sent := tr.sentFrames()
	if len(sent) != 1 {
		t.Fatalf("sent %d frames, want 1", len(sent))
	}
	res, err := isostack.DecodeAssociateResponse(sent[0])
	if err != nil {
		t.Fatalf("DecodeAssociateResponse: %v", err)
	}
	if res.ACSEResult != 0 {
		t.Fatalf("ACSE result = %d, want 0 (accepted)", res.ACSEResult)
	}
	kind, content, err := pdu.DecodePdu(res.MmsPayload)
	if err != nil || kind != pdu.PduInitiateResponse {
		t.Fatalf("DecodePdu = %s, %v; want InitiateResponse", kind, err)
	}
	ir, err := pdu.UnmarshalInitiateResponse(content)
	if err != nil {
		t.Fatalf("UnmarshalInitiateResponse: %v", err)
	}
	if ir.LocalDetailCalled != minPDUSize || ir.NegotiatedMaxServOutstandingCall != 3 ||
		ir.NegotiatedMaxServOutstandingCalled != 2 || ir.NegotiatedDataStructureNesting != 4 {
		t.Fatalf("unexpected negotiated values: %+v", *ir)
	}
	if ir.InitResponseDetail.NegotiatedVersion != 1 {
		t.Fatalf("negotiated version = %d", ir.InitResponseDetail.NegotiatedVersion)
	}

	// The pending request is consumed: a second accept fails.
	if err := c.AcceptAssociation(ctx); err == nil || !strings.Contains(err.Error(), "no pending association") {
		t.Fatalf("second AcceptAssociation err = %v", err)
	}
}

func TestAcceptAssociation_SendError(t *testing.T) {
	sendErr := errors.New("link down")
	tr := &scriptTransport{in: [][]byte{mustAssocReq(t, mustInitReq(t, 1000, 5, 5, 10))}, sendErr: sendErr}
	c := New(tr, discardLogger(), nopHandler, defaultOpts())
	ctx := context.Background()
	if _, err := c.ReceiveAssociation(ctx); err != nil {
		t.Fatalf("ReceiveAssociation: %v", err)
	}
	err := c.AcceptAssociation(ctx)
	if !errors.Is(err, sendErr) || !strings.Contains(err.Error(), "send association response") {
		t.Fatalf("AcceptAssociation err = %v", err)
	}
	// Negotiated options are only committed after a successful send.
	if got := c.MaxPDUSize(); got != 65000 {
		t.Fatalf("MaxPDUSize = %d, want unchanged 65000", got)
	}
}

func TestRejectAssociation_EncodesRejectAndClearsPending(t *testing.T) {
	tr := &scriptTransport{in: [][]byte{mustAssocReq(t, mustInitReq(t, 1000, 5, 5, 10))}}
	c := New(tr, discardLogger(), nopHandler, defaultOpts())
	ctx := context.Background()
	if _, err := c.ReceiveAssociation(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.RejectAssociation(ctx); err != nil {
		t.Fatal(err)
	}
	sent := tr.sentFrames()
	if len(sent) != 1 {
		t.Fatalf("sent %d frames", len(sent))
	}
	res, err := isostack.DecodeAssociateResponse(sent[0])
	if err != nil {
		t.Fatalf("DecodeAssociateResponse: %v", err)
	}
	if res.ACSEResult == 0 {
		t.Fatal("reject encoded as accepted")
	}
	if err := c.AcceptAssociation(ctx); err == nil {
		t.Fatal("AcceptAssociation after reject should fail")
	}
}

// --- Serve ------------------------------------------------------------------

func TestServe_ReceiveError(t *testing.T) {
	recvErr := errors.New("reset by peer")
	tr := &scriptTransport{recvErr: recvErr}
	c := New(tr, discardLogger(), nopHandler, defaultOpts())
	err := c.Serve(context.Background())
	if !errors.Is(err, recvErr) || !strings.Contains(err.Error(), "serverconn: receive") {
		t.Fatalf("Serve err = %v", err)
	}
}

func TestServe_DecodeErrorNotRelease(t *testing.T) {
	for name, frame := range map[string][]byte{
		"garbage":       {0xff, 0x00},
		"session abort": isostack.EncodeAbort(),
	} {
		t.Run(name, func(t *testing.T) {
			tr := &scriptTransport{in: [][]byte{frame}}
			c := New(tr, discardLogger(), nopHandler, defaultOpts())
			err := c.Serve(context.Background())
			if err == nil || !strings.Contains(err.Error(), "decode data") {
				t.Fatalf("Serve err = %v", err)
			}
			if n := len(tr.sentFrames()); n != 0 {
				t.Fatalf("sent %d frames, want 0", n)
			}
		})
	}
}

func TestServe_ReleaseSendsReleaseResponse(t *testing.T) {
	tr := &scriptTransport{in: [][]byte{isostack.EncodeReleaseRequest()}}
	c := New(tr, discardLogger(), nopHandler, defaultOpts())
	if err := c.Serve(context.Background()); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	sent := tr.sentFrames()
	if len(sent) != 1 || !bytes.Equal(sent[0], isostack.EncodeReleaseResponse()) {
		t.Fatalf("sent %x, want release response", sent)
	}
}

func TestServe_ReleaseSendError(t *testing.T) {
	sendErr := errors.New("closed")
	tr := &scriptTransport{in: [][]byte{isostack.EncodeReleaseRequest()}, sendErr: sendErr}
	c := New(tr, discardLogger(), nopHandler, defaultOpts())
	err := c.Serve(context.Background())
	if !errors.Is(err, sendErr) || !strings.Contains(err.Error(), "send release response") {
		t.Fatalf("Serve err = %v", err)
	}
}

func TestServe_ConcludeSendError(t *testing.T) {
	sendErr := errors.New("closed")
	tr := &scriptTransport{in: [][]byte{dataFrame(codec.MarshalConcludeRequest())}, sendErr: sendErr}
	c := New(tr, discardLogger(), nopHandler, defaultOpts())
	err := c.Serve(context.Background())
	if !errors.Is(err, sendErr) || !strings.Contains(err.Error(), "send conclude response") {
		t.Fatalf("Serve err = %v", err)
	}
}

// TestServe_SkipsBadFramesThenConcludes feeds every frame kind that Serve
// must log and skip, then a conclude. Only the conclude may be answered.
func TestServe_SkipsBadFramesThenConcludes(t *testing.T) {
	opts := defaultOpts()
	opts.MaxPDUSize = minPDUSize
	handlerCalled := false
	handler := func(_ context.Context, _ codec.InvokeID, _ int, _ []byte) (int, bool, []byte, error) {
		handlerCalled = true
		return 0, false, nil, nil
	}

	oversized := mustConfirmedReq(t, 1, 4, make([]byte, minPDUSize+1))
	tr := &scriptTransport{in: [][]byte{
		dataFrame(oversized),                                                       // larger than negotiated size
		dataFrame([]byte{0x1f, 0xff, 0xff, 0xff}),                                  // malformed MMS PDU
		dataFrame(codec.MarshalRejectPDU(0, 1, 0)),                                 // unexpected kind (Reject)
		dataFrame(codec.MarshalConcludeResponse()),                                 // unexpected kind (ConcludeResponse)
		dataFrame(asn1util.WrapPrimitive(5, []byte{0xff, 0xff, 0xff, 0xff, 0xff})), // malformed cancel
		dataFrame(codec.MarshalConcludeRequest()),
	}}
	c := New(tr, discardLogger(), handler, opts)
	if err := c.Serve(context.Background()); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if handlerCalled {
		t.Fatal("handler called for an oversized request")
	}
	sent := tr.sentFrames()
	if len(sent) != 1 {
		t.Fatalf("sent %d frames, want 1 (conclude response)", len(sent))
	}
	expectConcludeResponse(t, sent[0])
}

func TestServe_CancelRequestRepliesCancelError(t *testing.T) {
	tr := &scriptTransport{in: [][]byte{
		dataFrame(asn1util.WrapPrimitive(5, berutil.EncodeUint32(42))),
		dataFrame(codec.MarshalConcludeRequest()),
	}}
	c := New(tr, discardLogger(), nopHandler, defaultOpts())
	if err := c.Serve(context.Background()); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	sent := tr.sentFrames()
	if len(sent) != 2 {
		t.Fatalf("sent %d frames, want 2", len(sent))
	}
	mms, err := isostack.DecodeDataResponse(sent[0])
	if err != nil {
		t.Fatal(err)
	}
	if want := codec.MarshalCancelError(42, 10, 1); !bytes.Equal(mms, want) {
		t.Fatalf("cancel reply = %x, want %x", mms, want)
	}
	// Cancel-ErrorPDU [7] { originalInvokeID [0] 42, serviceError [1] {
	// errorClass [0] { cancel [10] invoke-id-unknown (1) } } }.
	kind, _, err := pdu.DecodePdu(mms)
	if err != nil || kind != pdu.PduCancelError {
		t.Fatalf("cancel reply kind = %v, %v; want CancelError", kind, err)
	}
	want := []byte{0xa7, 0x0a, 0x80, 0x01, 0x2a, 0xa1, 0x05, 0xa0, 0x03, 0x8a, 0x01, 0x01}
	if !bytes.Equal(mms, want) {
		t.Fatalf("cancel reply = %x, want %x", mms, want)
	}
	expectConcludeResponse(t, sent[1])
}

func TestServe_CancelRequestSendError(t *testing.T) {
	sendErr := errors.New("closed")
	tr := &scriptTransport{
		in:      [][]byte{dataFrame(asn1util.WrapPrimitive(5, berutil.EncodeUint32(7)))},
		sendErr: sendErr,
	}
	c := New(tr, discardLogger(), nopHandler, defaultOpts())
	if err := c.Serve(context.Background()); !errors.Is(err, sendErr) {
		t.Fatalf("Serve err = %v, want %v", err, sendErr)
	}
}

// --- handleConfirmedRequest -------------------------------------------------

func TestServe_ConfirmedRequestSuccess(t *testing.T) {
	var gotID codec.InvokeID
	var gotTag int
	var gotBody []byte
	handler := func(_ context.Context, id codec.InvokeID, tag int, body []byte) (int, bool, []byte, error) {
		gotID, gotTag, gotBody = id, tag, bytes.Clone(body)
		return tag, true, []byte{0x80, 0x01, 0x2a}, nil
	}
	tr := &scriptTransport{in: [][]byte{
		dataFrame(mustConfirmedReq(t, 77, 4, []byte{0x80, 0x01, 0xff})),
		dataFrame(codec.MarshalConcludeRequest()),
	}}
	c := New(tr, discardLogger(), handler, defaultOpts())
	if err := c.Serve(context.Background()); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if gotID != 77 || gotTag != 4 || !bytes.Equal(gotBody, []byte{0x80, 0x01, 0xff}) {
		t.Fatalf("handler got id=%d tag=%d body=%x", gotID, gotTag, gotBody)
	}
	sent := tr.sentFrames()
	if len(sent) != 2 {
		t.Fatalf("sent %d frames, want 2", len(sent))
	}
	kind, content := decodeSentMMS(t, sent[0])
	if kind != pdu.PduConfirmedResponse {
		t.Fatalf("kind = %s, want ConfirmedResponse", kind)
	}
	id, svc, err := codec.UnmarshalConfirmedResponse(content)
	if err != nil {
		t.Fatalf("UnmarshalConfirmedResponse: %v", err)
	}
	if id != 77 || svc.Tag != 4 || !svc.IsCompound || !bytes.Equal(svc.Bytes, []byte{0x80, 0x01, 0x2a}) {
		t.Fatalf("response id=%d tag=%d compound=%v bytes=%x", id, svc.Tag, svc.IsCompound, svc.Bytes)
	}
	expectConcludeResponse(t, sent[1])
}

func TestServe_ConfirmedRequestMalformedSendsReject(t *testing.T) {
	called := false
	handler := func(_ context.Context, _ codec.InvokeID, _ int, _ []byte) (int, bool, []byte, error) {
		called = true
		return 0, false, nil, nil
	}
	// ConfirmedRequest PDU whose invokeID is a NULL instead of an INTEGER.
	bad := asn1util.WrapConstructed(0, []byte{0x05, 0x00})
	tr := &scriptTransport{in: [][]byte{dataFrame(bad), dataFrame(codec.MarshalConcludeRequest())}}
	c := New(tr, discardLogger(), handler, defaultOpts())
	if err := c.Serve(context.Background()); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if called {
		t.Fatal("handler called for malformed request")
	}
	sent := tr.sentFrames()
	if len(sent) != 2 {
		t.Fatalf("sent %d frames, want 2", len(sent))
	}
	kind, content := decodeSentMMS(t, sent[0])
	if kind != pdu.PduReject {
		t.Fatalf("kind = %s, want Reject", kind)
	}
	rej, err := pdu.DecodeRejectPDU(content)
	if err != nil {
		t.Fatalf("DecodeRejectPDU: %v", err)
	}
	if rej.RejectType != 1 || rej.RejectReason != 0 || rej.InvokeID != 0 {
		t.Fatalf("reject = %+v, want type=1 reason=0 invokeID=0", *rej)
	}
	expectConcludeResponse(t, sent[1])
}

func TestServe_ConfirmedRequestHandlerErrors(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantClass int
		wantCode  int
	}{
		{"service error", &ServiceError{ErrorClass: 2, ErrorCode: 5}, 2, 5},
		{"wrapped service error", errors.Join(errors.New("ctx"), &ServiceError{ErrorClass: 6, ErrorCode: 3}), 6, 3},
		{"plain error", errors.New("nope"), 7, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := func(_ context.Context, _ codec.InvokeID, _ int, _ []byte) (int, bool, []byte, error) {
				return 0, false, nil, tc.err
			}
			tr := &scriptTransport{in: [][]byte{
				dataFrame(mustConfirmedReq(t, 9, 4, []byte{0x80, 0x00})),
				dataFrame(codec.MarshalConcludeRequest()),
			}}
			c := New(tr, discardLogger(), handler, defaultOpts())
			if err := c.Serve(context.Background()); err != nil {
				t.Fatalf("Serve: %v", err)
			}
			sent := tr.sentFrames()
			if len(sent) != 2 {
				t.Fatalf("sent %d frames, want 2", len(sent))
			}
			expectConfirmedError(t, sent[0], 9, tc.wantClass, tc.wantCode)
			expectConcludeResponse(t, sent[1])
		})
	}
}

func TestServe_ConfirmedResponseTooLarge(t *testing.T) {
	opts := defaultOpts()
	opts.MaxPDUSize = minPDUSize
	handler := func(_ context.Context, _ codec.InvokeID, tag int, _ []byte) (int, bool, []byte, error) {
		return tag, true, make([]byte, minPDUSize), nil
	}
	tr := &scriptTransport{in: [][]byte{
		dataFrame(mustConfirmedReq(t, 3, 4, []byte{0x80, 0x00})),
		dataFrame(codec.MarshalConcludeRequest()),
	}}
	c := New(tr, discardLogger(), handler, opts)
	if err := c.Serve(context.Background()); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	sent := tr.sentFrames()
	if len(sent) != 2 {
		t.Fatalf("sent %d frames, want 2", len(sent))
	}
	expectConfirmedError(t, sent[0], 3, errorClassResource, errorCodeOther)
	expectConcludeResponse(t, sent[1])
}

func TestServe_ConfirmedRequestSendErrorStopsServe(t *testing.T) {
	sendErr := errors.New("broken pipe")
	tests := []struct {
		name    string
		frame   []byte
		handler ServiceHandler
	}{
		{"response", mustConfirmedReq(t, 1, 4, nil), func(context.Context, codec.InvokeID, int, []byte) (int, bool, []byte, error) {
			return 4, true, nil, nil
		}},
		{"service error", mustConfirmedReq(t, 1, 4, nil), func(context.Context, codec.InvokeID, int, []byte) (int, bool, []byte, error) {
			return 0, false, nil, errors.New("x")
		}},
		{"reject", asn1util.WrapConstructed(0, []byte{0x05, 0x00}), nopHandler},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr := &scriptTransport{
				in:      [][]byte{dataFrame(tc.frame), dataFrame(codec.MarshalConcludeRequest())},
				sendErr: sendErr,
			}
			c := New(tr, discardLogger(), tc.handler, defaultOpts())
			if err := c.Serve(context.Background()); !errors.Is(err, sendErr) {
				t.Fatalf("Serve err = %v, want %v", err, sendErr)
			}
			// Serve must stop at the failed send and not read the conclude.
			if tr.recvCalls != 1 {
				t.Fatalf("Receive called %d times, want 1", tr.recvCalls)
			}
		})
	}
}

// --- Full association over the script transport -----------------------------

func TestAssociationThenServe(t *testing.T) {
	handler := func(_ context.Context, _ codec.InvokeID, tag int, _ []byte) (int, bool, []byte, error) {
		return tag, true, nil, nil
	}
	tr := &scriptTransport{in: [][]byte{
		mustAssocReq(t, mustInitReq(t, 4000, 5, 5, 10)),
		dataFrame(mustConfirmedReq(t, 1, 1, nil)),
		isostack.EncodeReleaseRequest(),
	}}
	c := New(tr, discardLogger(), handler, defaultOpts())
	ctx := context.Background()
	if _, err := c.ReceiveAssociation(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.AcceptAssociation(ctx); err != nil {
		t.Fatal(err)
	}
	if c.MaxPDUSize() != 4000 {
		t.Fatalf("MaxPDUSize = %d, want 4000", c.MaxPDUSize())
	}
	if err := c.Serve(ctx); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	sent := tr.sentFrames()
	if len(sent) != 3 {
		t.Fatalf("sent %d frames, want 3", len(sent))
	}
	if kind, _ := decodeSentMMS(t, sent[1]); kind != pdu.PduConfirmedResponse {
		t.Fatalf("kind = %s, want ConfirmedResponse", kind)
	}
	if !bytes.Equal(sent[2], isostack.EncodeReleaseResponse()) {
		t.Fatal("last frame is not the release response")
	}
}

// --- Misc -------------------------------------------------------------------

func TestClose(t *testing.T) {
	closeErr := errors.New("already closed")
	tr := &scriptTransport{closeErr: closeErr}
	c := New(tr, discardLogger(), nopHandler, defaultOpts())
	if err := c.Close(); !errors.Is(err, closeErr) {
		t.Fatalf("Close err = %v", err)
	}
	if tr.closeCnt != 1 {
		t.Fatalf("transport Close called %d times", tr.closeCnt)
	}
}

func TestSendUnconfirmedAndAbort_Encoding(t *testing.T) {
	tr := &scriptTransport{}
	c := New(tr, discardLogger(), nopHandler, defaultOpts())
	ctx := context.Background()
	report := []byte{0xa3, 0x02, 0xa0, 0x00}
	if err := c.SendUnconfirmed(ctx, report); err != nil {
		t.Fatal(err)
	}
	if err := c.SendAbort(ctx); err != nil {
		t.Fatal(err)
	}
	sent := tr.sentFrames()
	if len(sent) != 2 {
		t.Fatalf("sent %d frames", len(sent))
	}
	mms, err := isostack.DecodeDataResponse(sent[0])
	if err != nil || !bytes.Equal(mms, report) {
		t.Fatalf("unconfirmed = %x, %v", mms, err)
	}
	if !bytes.Equal(sent[1], isostack.EncodeAbort()) {
		t.Fatal("abort frame mismatch")
	}
	if _, err := isostack.DecodeDataResponse(sent[1]); !errors.Is(err, isostack.ErrPeerReleased) {
		t.Fatalf("abort decode err = %v, want ErrPeerReleased", err)
	}
}

func TestSendErrorsPropagate(t *testing.T) {
	sendErr := errors.New("down")
	tr := &scriptTransport{sendErr: sendErr}
	c := New(tr, discardLogger(), nopHandler, defaultOpts())
	ctx := context.Background()
	if err := c.SendUnconfirmed(ctx, []byte{0xa3, 0x00}); !errors.Is(err, sendErr) {
		t.Fatalf("SendUnconfirmed err = %v", err)
	}
	if err := c.SendAbort(ctx); !errors.Is(err, sendErr) {
		t.Fatalf("SendAbort err = %v", err)
	}
	if err := c.RejectAssociation(ctx); !errors.Is(err, sendErr) {
		t.Fatalf("RejectAssociation err = %v", err)
	}
}

// TestSendUnconfirmedConcurrentWithServe checks that unconfirmed sends from
// another goroutine interleave safely with Serve (run with -race).
func TestSendUnconfirmedConcurrentWithServe(t *testing.T) {
	const n = 50
	in := make([][]byte, 0, n+1)
	for i := range n {
		in = append(in, dataFrame(mustConfirmedReq(t, codec.InvokeID(i), 1, nil)))
	}
	in = append(in, dataFrame(codec.MarshalConcludeRequest()))
	tr := &scriptTransport{in: in}
	handler := func(_ context.Context, _ codec.InvokeID, tag int, _ []byte) (int, bool, []byte, error) {
		return tag, true, nil, nil
	}
	c := New(tr, discardLogger(), handler, defaultOpts())
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range n {
			if err := c.SendUnconfirmed(ctx, []byte{0xa3, 0x00}); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	if err := c.Serve(ctx); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	wg.Wait()
	if got := len(tr.sentFrames()); got != 2*n+1 {
		t.Fatalf("sent %d frames, want %d", got, 2*n+1)
	}
}
