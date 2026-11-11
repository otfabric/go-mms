// SPDX-License-Identifier: MIT

package mms

import (
	"bytes"
	"context"
	"encoding/asn1"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/isostack"
	"github.com/otfabric/go-mms/internal/pdu"
	"github.com/otfabric/go-mms/internal/session"
)

// rawLog records the SPDU types a client sends and receives.
type rawLog struct {
	mu   sync.Mutex
	sent []session.SpduType
	recv []session.SpduType
	raw  [][]byte
}

func (l *rawLog) hook(dir string, raw []byte) {
	sp, err := session.Parse(raw)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if dir == RawHookSend {
		l.sent = append(l.sent, sp.Type)
	} else {
		l.recv = append(l.recv, sp.Type)
	}
	l.raw = append(l.raw, bytes.Clone(raw))
}

func (l *rawLog) snapshot() (sent, recv []session.SpduType, raw [][]byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]session.SpduType(nil), l.sent...), append([]session.SpduType(nil), l.recv...), append([][]byte(nil), l.raw...)
}

func specServer(t *testing.T, opts ServerOptions) *Server {
	t.Helper()
	opts.Logger = slog.New(slog.NewTextHandler(newTestWriter(t), &slog.HandlerOptions{Level: slog.LevelDebug}))
	return NewServer(opts)
}

// dialServer runs srv.Serve on one end of a loopback pair and dials it.
// The returned channel yields Serve's result.
func dialServer(t *testing.T, srv *Server, opts DialOptions) (*Client, <-chan error, error) {
	t.Helper()
	clientConn, serverConn := loopbackPair()
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(context.Background(), serverConn) }()
	c, err := NewClient(context.Background(), clientConn, opts)
	if err != nil {
		_ = clientConn.Close()
	}
	t.Cleanup(func() { _ = clientConn.Close(); _ = serverConn.Close() })
	return c, serveErr, err
}

func waitServe(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return")
		return nil
	}
}

// ISO 9506-2 Annex A.1.2 (MMS-STK-02): Close concludes and releases:
// DT(Conclude-Request) → DT(Conclude-Response) → FN(RLRQ) → DN(RLRE),
// and both sides end without an error.
func TestSpec_9506_A12_OrderlyRelease(t *testing.T) {
	srv := specServer(t, ServerOptions{})
	var log rawLog
	c, serveErr, err := dialServer(t, srv, DialOptions{RawHook: log.hook})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := waitServe(t, serveErr); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	sent, recv, _ := log.snapshot()
	if len(sent) != 3 || sent[0] != session.SpduConnect || sent[1] != session.SpduData || sent[2] != session.SpduFinish {
		t.Fatalf("sent %v", sent)
	}
	if len(recv) != 3 || recv[0] != session.SpduAccept || recv[1] != session.SpduData || recv[2] != session.SpduDisconnect {
		t.Fatalf("received %v", recv)
	}
}

// A negative ReleaseTimeout keeps the conclude-only Close; a server that
// closes the transport after the Conclude-Response, or never answers
// the FINISH, still yields a successful Close.
func TestSpec_9506_A12_ReleaseTolerance(t *testing.T) {
	t.Run("skip release", func(t *testing.T) {
		srv := specServer(t, ServerOptions{ReleaseTimeout: -1})
		var log rawLog
		c, serveErr, err := dialServer(t, srv, DialOptions{RawHook: log.hook, ReleaseTimeout: -1})
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := waitServe(t, serveErr); err != nil {
			t.Fatal(err)
		}
		if sent, _, _ := log.snapshot(); len(sent) != 2 {
			t.Fatalf("sent %v", sent)
		}
	})
	t.Run("server does not wait", func(t *testing.T) {
		srv := specServer(t, ServerOptions{ReleaseTimeout: -1})
		c, serveErr, err := dialServer(t, srv, DialOptions{ReleaseTimeout: 200 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		if err := waitServeAfterClose(t, c, serveErr); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("silent peer", func(t *testing.T) {
		mt := newMockTransport()
		srv := newMockServer(t, mt)
		go srv.handleAssociation(context.Background())
		c, err := NewClient(context.Background(), mt, DialOptions{ReleaseTimeout: 50 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			srv.handleDataRequest(context.Background())
			srv.sendDataResponse(context.Background(), []byte{asn1util.TagConcludeResponse, 0x00})
		}()
		start := time.Now()
		if err := c.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
		if d := time.Since(start); d > 2*time.Second {
			t.Fatalf("Close took %v", d)
		}
	})
	t.Run("ctx ends the release", func(t *testing.T) {
		mt := newMockTransport()
		srv := newMockServer(t, mt)
		go srv.handleAssociation(context.Background())
		c, err := NewClient(context.Background(), mt, DialOptions{ReleaseTimeout: time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			srv.handleDataRequest(context.Background())
			srv.sendDataResponse(context.Background(), []byte{asn1util.TagConcludeResponse, 0x00})
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		if err := c.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Close: %v", err)
		}
	})
	t.Run("abort or junk answer", func(t *testing.T) {
		for name, answer := range map[string][]byte{"abort": isostack.EncodeAbort(), "junk": EncodeDataForTest([]byte{0xa0, 0x00})} {
			mt := newMockTransport()
			srv := newMockServer(t, mt)
			go srv.handleAssociation(context.Background())
			var log rawLog
			c, err := NewClient(context.Background(), mt, DialOptions{RawHook: log.hook})
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				srv.handleDataRequest(context.Background())
				srv.sendDataResponse(context.Background(), []byte{asn1util.TagConcludeResponse, 0x00})
				mt.sendToClient(answer)
			}()
			if err := c.Close(context.Background()); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			sent, _, _ := log.snapshot()
			wantAbort := name == "junk"
			if gotAbort := sent[len(sent)-1] == session.SpduAbort; gotAbort != wantAbort {
				t.Fatalf("%s: sent %v", name, sent)
			}
		}
	})
}

// EncodeDataForTest wraps an MMS PDU in session DATA on the default
// contexts.
func EncodeDataForTest(mmsPdu []byte) []byte { return isostack.EncodeDataRequest(mmsPdu) }

func waitServeAfterClose(t *testing.T, c *Client, serveErr <-chan error) error {
	t.Helper()
	if err := c.Close(context.Background()); err != nil {
		return err
	}
	return waitServe(t, serveErr)
}

// ISO 9506-2 24.3, IEC 61850-8-1 Table 14 (MMS-STK-13): a Conclude-Error
// makes Close abort and return the ServiceError.
func TestSpec_9506_24_3_ConcludeErrorAborts(t *testing.T) {
	mt := newMockTransport()
	srv := newMockServer(t, mt)
	go srv.handleAssociation(context.Background())
	var log rawLog
	c, err := NewClient(context.Background(), mt, DialOptions{RawHook: log.hook})
	if err != nil {
		t.Fatal(err)
	}
	// Conclude-ErrorPDU ::= ServiceError { errorClass conclude [9]
	// further-communication-required (1) }.
	concludeErr := []byte{asn1util.TagConcludeError, 0x05, 0xa0, 0x03, 0x89, 0x01, 0x01}
	go func() {
		srv.handleDataRequest(context.Background())
		srv.sendDataResponse(context.Background(), concludeErr)
	}()
	err = c.Close(context.Background())
	se, ok := errors.AsType[*ServiceError](err)
	if !ok || se.Class != ErrorClassConclude || se.Code != 1 || se.CodeName() != "further-communication-required" {
		t.Fatalf("Close: %v", err)
	}
	sent, _, _ := log.snapshot()
	if sent[len(sent)-1] != session.SpduAbort {
		t.Fatalf("sent %v, want an abort last", sent)
	}

	// An undecodable Conclude-Error is a protocol error and still aborts.
	mt = newMockTransport()
	srv = newMockServer(t, mt)
	go srv.handleAssociation(context.Background())
	c, err = NewClient(context.Background(), mt, DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		srv.handleDataRequest(context.Background())
		srv.sendDataResponse(context.Background(), []byte{asn1util.TagConcludeError, 0x00})
	}()
	if err := c.Close(context.Background()); !errors.Is(err, ErrProtocol) {
		t.Fatalf("Close: %v", err)
	}
}

// ISO 8650-1, IEC 61850-8-1 Table 3 (MMS-STK-03, -04, -09): an
// authentication refusal is REFUSE + CPR + AARE with an authentication
// diagnostic + Initiate-ErrorPDU, decoded by the client into a typed,
// non-retryable error.
func TestSpec_8650_AuthenticationRejection(t *testing.T) {
	for name, tc := range map[string]struct {
		password []byte
		diag     int
	}{
		"wrong password": {[]byte("bad"), acse.DiagUserAuthFailure},
		"no credentials": {nil, acse.DiagUserAuthRequired},
	} {
		t.Run(name, func(t *testing.T) {
			srv := specServer(t, ServerOptions{Authenticate: func(context.Context, *AuthContext) (AuthResult, error) {
				return AuthResult{Accept: false}, nil
			}})
			_, serveErr, err := dialServer(t, srv, DialOptions{ISO: ISOOptions{Password: tc.password}})
			re, ok := errors.AsType[*RejectionError](err)
			if !ok {
				t.Fatalf("NewClient: %T %v", err, err)
			}
			if re.SessionReason != 2 || re.ACSEResult != acse.ResultRejectedPerm || re.DiagnosticSource != acse.DiagSourceUser || re.Diagnostic != tc.diag || re.Transient {
				t.Fatalf("%+v", re)
			}
			if !errors.Is(err, ErrConnectionRejected) || !errors.Is(err, ErrAuthenticationFailed) || !errors.Is(err, ErrAssociationFailed) || IsRetryable(err) {
				t.Fatalf("classification of %v", err)
			}
			se, ok := errors.AsType[*ServiceError](err)
			if !ok || se.Class != ErrorClassAccess || se.Code != 3 {
				t.Fatalf("Initiate-Error %+v", se)
			}
			if re.Error() == "" {
				t.Fatal("empty message")
			}
			if serr := waitServe(t, serveErr); !errors.Is(serr, ErrAuthenticationFailed) {
				t.Fatalf("Serve: %v", serr)
			}
		})
	}
}

// ISO 8327-1 (MMS-STK-09): a REFUSE for congestion is a transient,
// retryable rejection; one for a session-provider reason is not.
func TestSpec_8327_RefuseClassification(t *testing.T) {
	for name, tc := range map[string]struct {
		reply     []byte
		retryable bool
		auth      bool
	}{
		"congestion":       {must(session.EncodeRefuse(session.ReasonUserCongestion, nil)), true, false},
		"version":          {must(session.EncodeRefuse(session.ReasonVersionNotSupported, nil)), false, false},
		"accepted+initErr": {acceptWithMMS(pdu.MarshalInitiateError(pdu.ErrorClassInitiate, pdu.InitiateErrVersionIncompatible)), false, false},
	} {
		t.Run(name, func(t *testing.T) {
			mt := newMockTransport()
			t.Cleanup(mt.shutdown)
			go func() {
				if _, err := mt.receiveFromClient(context.Background()); err == nil {
					mt.sendToClient(tc.reply)
				}
			}()
			_, err := NewClient(context.Background(), mt, DialOptions{})
			re, ok := errors.AsType[*RejectionError](err)
			if !ok || IsRetryable(err) != tc.retryable || errors.Is(err, ErrAuthenticationFailed) != tc.auth || !errors.Is(err, ErrConnectionRejected) {
				t.Fatalf("%+v %v", re, err)
			}
			if name == "accepted+initErr" && (re.InitiateError == nil || re.InitiateError.Class != ErrorClassInitiate) {
				t.Fatalf("%+v", re)
			}
		})
	}
}

func must(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}

// acceptWithMMS builds an acceptance of the default association request
// carrying payload.
func acceptWithMMS(payload []byte) []byte {
	b := must(isostack.EncodeAssociateRequest(isostack.Params{}, []byte{0xa8, 0x00}))
	req, err := isostack.DecodeAssociateRequest(b)
	if err != nil {
		panic(err)
	}
	return must(req.EncodeAssociateResponse(isostack.ResponseParams{}, payload))
}

// respondInitiate answers the client's CONNECT with resp and returns the
// client, or the NewClient error.
func respondInitiate(t *testing.T, resp pdu.InitiateResponse, opts DialOptions) (*Client, *mockTransport, error) {
	t.Helper()
	mt := newMockTransport()
	t.Cleanup(mt.shutdown)
	payload, err := codec.MarshalMmsPduBareSequence(asn1util.TagInitiateResponse, resp)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		if _, err := mt.receiveFromClient(context.Background()); err == nil {
			mt.sendToClient(acceptWithMMS(payload))
		}
	}()
	c, err := NewClient(context.Background(), mt, opts)
	if c != nil {
		t.Cleanup(func() { _ = c.Abort(context.Background()) })
	}
	return c, mt, err
}

func goodInitResp() pdu.InitiateResponse {
	return pdu.InitiateResponse{
		LocalDetailCalled:                  65000,
		NegotiatedMaxServOutstandingCall:   5,
		NegotiatedMaxServOutstandingCalled: 5,
		NegotiatedDataStructureNesting:     10,
		InitResponseDetail: pdu.InitResponseDetail{
			NegotiatedVersion:       1,
			NegotiatedParamCBB:      asn1.BitString{Bytes: []byte{0xf1, 0x00}, BitLength: 11},
			ServicesSupportedCalled: asn1.BitString{Bytes: make([]byte, 11), BitLength: 85},
		},
	}
}

// ISO 9506-2 8.2, IEC 61850-8-1 Tables 109/110 (MMS-STK-14): the client
// refuses a response that negotiates more than it proposed.
func TestSpec_9506_ClientNegotiationChecks(t *testing.T) {
	for name, mutate := range map[string]func(*pdu.InitiateResponse){
		"version 2":  func(r *pdu.InitiateResponse) { r.InitResponseDetail.NegotiatedVersion = 2 },
		"version 0":  func(r *pdu.InitiateResponse) { r.InitResponseDetail.NegotiatedVersion = 0 },
		"calling 10": func(r *pdu.InitiateResponse) { r.NegotiatedMaxServOutstandingCall = 10 },
		"called 10":  func(r *pdu.InitiateResponse) { r.NegotiatedMaxServOutstandingCalled = 10 },
		"calling 0":  func(r *pdu.InitiateResponse) { r.NegotiatedMaxServOutstandingCall = 0 },
		"nesting 11": func(r *pdu.InitiateResponse) { r.NegotiatedDataStructureNesting = 11 },
		"called -1":  func(r *pdu.InitiateResponse) { r.NegotiatedMaxServOutstandingCalled = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			resp := goodInitResp()
			mutate(&resp)
			_, _, err := respondInitiate(t, resp, DialOptions{})
			if !errors.Is(err, ErrNegotiationFailed) {
				t.Fatalf("NewClient: %v", err)
			}
		})
	}

	// Smaller values are taken; absent nesting is the proposal; the
	// server's bit strings are exposed.
	resp := goodInitResp()
	resp.NegotiatedMaxServOutstandingCall = 2
	resp.NegotiatedDataStructureNesting = 0
	resp.InitResponseDetail.ServicesSupportedCalled.Bytes[0] = 0x88
	c, _, err := respondInitiate(t, resp, DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	n := c.Negotiated()
	if n.MaxOutCalling != 2 || n.NestingLevel != 10 || n.ServicesSupportedCalled.At(0) != 1 || n.ServicesSupportedCalled.At(4) != 1 ||
		n.ParameterCBB.BitLength != 11 || n.ParameterCBB.At(7) != 1 {
		t.Fatalf("%+v", n)
	}
}

// ISO 9506-2 8.2 (MMS-STK-15): requests larger than the server's
// localDetailCalled are refused locally; responses larger than the
// client's own localDetailCalling are invalid.
func TestSpec_9506_PDUSizeLimits(t *testing.T) {
	resp := goodInitResp()
	resp.LocalDetailCalled = 1000
	var log rawLog
	c, _, err := respondInitiate(t, resp, DialOptions{RawHook: log.hook})
	if err != nil {
		t.Fatal(err)
	}
	if c.Negotiated().MaxPDUSize != 1000 {
		t.Fatalf("MaxPDUSize %d", c.Negotiated().MaxPDUSize)
	}
	_, err = c.Write(context.Background(), WriteRequest{DomainID: "d", ItemID: "x", Value: NewOctetString(make([]byte, 1500))})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("Write: %v", err)
	}
	if sent, _, _ := log.snapshot(); len(sent) != 1 {
		t.Fatalf("sent %v, want only the CONNECT", sent)
	}

	resp = goodInitResp()
	resp.LocalDetailCalled = 65000
	c, mt, err := respondInitiate(t, resp, DialOptions{MMS: MMSOptions{MaxPDUSize: 8000}})
	if err != nil {
		t.Fatal(err)
	}
	if c.Negotiated().MaxPDUSize != 8000 {
		t.Fatalf("MaxPDUSize %d", c.Negotiated().MaxPDUSize)
	}
	big, err := codec.MarshalConfirmedResponse(1, asn1util.TagNumIdentify, true, make([]byte, 9000))
	if err != nil {
		t.Fatal(err)
	}
	mt.sendToClient(isostack.EncodeDataRequest(big))
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("oversized response accepted")
	}
	if !errors.Is(c.Err(), ErrInvalidPDU) {
		t.Fatalf("Err: %v", c.Err())
	}
}

// MMS-STK-18: selectors longer than 16 octets fail before anything is
// sent.
func TestSpec_8327_SelectorLengthValidated(t *testing.T) {
	for _, iso := range []ISOOptions{
		{RemoteSSelector: make([]byte, 17)},
		{LocalPSelector: make([]byte, 17)},
	} {
		mt := newMockTransport()
		_, err := NewClient(context.Background(), mt, DialOptions{ISO: iso})
		if !errors.Is(err, ErrInvalidArgument) || IsRetryable(err) {
			t.Fatalf("NewClient: %v", err)
		}
		select {
		case b := <-mt.toServer:
			t.Fatalf("sent %x", b)
		default:
		}
	}
}

// IEC 61850-8-1 Table 111 (MMS-STK-01): the server advertises what it
// implements; file and journal services only with their providers.
func TestSpec_8_1_Table111_ServerServicesEndToEnd(t *testing.T) {
	bit := func(n NegotiatedParameters, tag int) bool { return n.ServicesSupportedCalled.At(tag) == 1 }
	srv := specServer(t, ServerOptions{})
	c, serveErr, err := dialServer(t, srv, DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	n := c.Negotiated()
	for _, tag := range []int{0, 1, 2, 4, 5, 6, 11, 12, 13, 79, 83, 84} {
		if !bit(n, tag) {
			t.Errorf("service bit %d not advertised", tag)
		}
	}
	for _, tag := range []int{asn1util.TagNumFileOpen, asn1util.TagNumObtainFile, asn1util.TagNumReadJournal, 78} {
		if bit(n, tag) {
			t.Errorf("service bit %d advertised", tag)
		}
	}
	if n.ServicesSupportedCalled.BitLength != 85 || !bytes.Equal(n.ParameterCBB.Bytes, []byte{0xf1, 0x00}) {
		t.Errorf("%+v", n)
	}
	if err := waitServeAfterClose(t, c, serveErr); err != nil {
		t.Fatal(err)
	}

	srv = specServer(t, ServerOptions{FileProvider: newMemFileProvider(), JournalProvider: newMemJournalProvider()})
	c, serveErr, err = dialServer(t, srv, DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	n = c.Negotiated()
	for _, tag := range []int{asn1util.TagNumFileOpen, asn1util.TagNumFileRead, asn1util.TagNumFileClose, asn1util.TagNumFileRename,
		asn1util.TagNumFileDelete, asn1util.TagNumFileDirectory, asn1util.TagNumObtainFile, asn1util.TagNumReadJournal} {
		if !bit(n, tag) {
			t.Errorf("service bit %d not advertised with providers", tag)
		}
	}
	if err := waitServeAfterClose(t, c, serveErr); err != nil {
		t.Fatal(err)
	}
}

// ISO 8650-1 (MMS-STK-19): the configured responding AP-title and
// AE-qualifier are sent, and responder-acse-requirements is echoed for
// an authenticated association.
func TestSpec_8650_RespondingAPTitle(t *testing.T) {
	srv := specServer(t, ServerOptions{
		RespondingAPTitle:     APTitle{1, 3, 9999, 23},
		RespondingAEQualifier: 23,
		Authenticate: func(context.Context, *AuthContext) (AuthResult, error) {
			return AuthResult{Accept: true}, nil
		},
	})
	var log rawLog
	c, serveErr, err := dialServer(t, srv, DialOptions{RawHook: log.hook, ISO: ISOOptions{Password: []byte("pw")}})
	if err != nil {
		t.Fatal(err)
	}
	_, _, raw := log.snapshot()
	accept := raw[1]
	for _, want := range [][]byte{{0xa4, 0x06, 0x06, 0x04, 0x2b, 0xce, 0x0f, 0x17}, {0xa5, 0x03, 0x02, 0x01, 0x17}, {0x88, 0x02, 0x07, 0x80}} {
		if !bytes.Contains(accept, want) {
			t.Errorf("AARE %x lacks %x", accept, want)
		}
	}
	if err := waitServeAfterClose(t, c, serveErr); err != nil {
		t.Fatal(err)
	}
}

// Close, Abort and the server's release wait race without leaking
// goroutines (goleak in TestMain) or deadlocking.
func TestChaos_ReleaseAbortRaces(t *testing.T) {
	for i := range 30 {
		srv := specServer(t, ServerOptions{ReleaseTimeout: time.Duration(i%3) * 10 * time.Millisecond})
		c, serveErr, err := dialServer(t, srv, DialOptions{ReleaseTimeout: time.Duration(i%4) * 5 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Go(func() { _ = c.Close(context.Background()) })
		if i%2 == 0 {
			wg.Go(func() { _ = c.Abort(context.Background()) })
		}
		wg.Wait()
		_ = waitServe(t, serveErr)
	}
}
