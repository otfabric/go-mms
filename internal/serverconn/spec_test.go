// SPDX-License-Identifier: MIT

package serverconn

import (
	"bytes"
	"context"
	"encoding/asn1"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/isostack"
	"github.com/otfabric/go-mms/internal/pdu"
	"github.com/otfabric/go-mms/internal/presentation"
	"github.com/otfabric/go-mms/internal/session"
)

// acceptInitiate associates over a script transport and returns the
// decoded Initiate-Response.
func acceptInitiate(t *testing.T, opts MMSOptions, req pdu.InitiateRequest) (*Conn, *pdu.InitiateResponse, []byte) {
	t.Helper()
	b, err := pdu.MarshalInitiateRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	tr := &scriptTransport{in: [][]byte{mustAssocReq(t, b)}}
	c := New(tr, discardLogger(), nopHandler, opts)
	if _, err := c.ReceiveAssociation(context.Background()); err != nil {
		t.Fatalf("ReceiveAssociation: %v", err)
	}
	if err := c.AcceptAssociation(context.Background()); err != nil {
		t.Fatalf("AcceptAssociation: %v", err)
	}
	res, err := isostack.DecodeAssociateResponse(tr.sentFrames()[0])
	if err != nil {
		t.Fatal(err)
	}
	_, content, err := pdu.DecodePdu(res.MmsPayload)
	if err != nil {
		t.Fatal(err)
	}
	ir, err := pdu.UnmarshalInitiateResponse(content)
	if err != nil {
		t.Fatal(err)
	}
	return c, ir, content
}

// IEC 61850-8-1 8.1.3.4.1, Tables 111/112 (MMS-STK-01, MMS-PDU-03): the
// server advertises its own services and the parameter CBB it supports,
// whatever the client offers.
func TestSpec_8_1_Table111_ServerAdvertisesOwnServices(t *testing.T) {
	own := ServiceBits(ServiceStatus, 1, ServiceIdentify, 4, 5, 6, ServiceConclude, ServiceCancel)
	opts := defaultOpts()
	opts.ServicesSupportedCalled = own
	for name, calling := range map[string]asn1.BitString{
		"all zero": {Bytes: make([]byte, 11), BitLength: 85},
		"all ones": {Bytes: append(bytes.Repeat([]byte{0xff}, 10), 0xf8), BitLength: 85},
	} {
		req := pdu.DefaultInitiateRequest(65000, 5, 5, 10)
		req.InitRequestDetail.ServicesSupportedCalling = calling
		// Propose every parameter CBB bit, cei and the c4 options included.
		req.InitRequestDetail.ProposedParamCBB = asn1.BitString{Bytes: []byte{0xff, 0xe0}, BitLength: 11}
		_, ir, _ := acceptInitiate(t, opts, req)
		got := ir.InitResponseDetail
		if got.ServicesSupportedCalled.BitLength != 85 || !bytes.Equal(got.ServicesSupportedCalled.Bytes, own.Bytes) {
			t.Errorf("%s: servicesSupportedCalled %x/%d, want %x", name, got.ServicesSupportedCalled.Bytes, got.ServicesSupportedCalled.BitLength, own.Bytes)
		}
		if got.NegotiatedParamCBB.BitLength != 11 || !bytes.Equal(got.NegotiatedParamCBB.Bytes, []byte{0xf1, 0x00}) {
			t.Errorf("%s: parameter CBB %x/%d, want f100/11", name, got.NegotiatedParamCBB.Bytes, got.NegotiatedParamCBB.BitLength)
		}
	}

	// Without configured services the Table 111 mandatory set is sent; a
	// short CBB proposal is answered over the full length.
	req := pdu.DefaultInitiateRequest(65000, 5, 5, 10)
	req.InitRequestDetail.ProposedParamCBB = asn1.BitString{Bytes: []byte{0xa0}, BitLength: 3}
	_, ir, _ := acceptInitiate(t, defaultOpts(), req)
	if !bytes.Equal(ir.InitResponseDetail.ServicesSupportedCalled.Bytes, mandatoryServices.Bytes) {
		t.Fatalf("default services %x", ir.InitResponseDetail.ServicesSupportedCalled.Bytes)
	}
	if cbb := ir.InitResponseDetail.NegotiatedParamCBB; cbb.BitLength != 11 || !bytes.Equal(cbb.Bytes, []byte{0xa0, 0x00}) {
		t.Fatalf("short CBB answered %x/%d", cbb.Bytes, cbb.BitLength)
	}
}

// ISO 9506-2 8.2 (MMS-STK-05, MMS-PDU-04): an absent localDetailCalling
// or dataStructureNestingLevel is not a proposal of 0.
func TestSpec_9506_InitiateAbsentOptionalFields(t *testing.T) {
	req := pdu.DefaultInitiateRequest(65000, 5, 5, 10)
	req.LocalDetailCalling = 0
	req.ProposedDataStructureNesting = 0
	c, ir, content := acceptInitiate(t, defaultOpts(), req)
	if ir.LocalDetailCalled != 65000 || c.MaxPDUSize() != 65000 {
		t.Fatalf("localDetailCalled %d, MaxPDUSize %d; want the server's 65000", ir.LocalDetailCalled, c.MaxPDUSize())
	}
	if _, nesting := pdu.InitiatePresence(content); nesting {
		t.Fatal("nesting level answered although not proposed")
	}
	if c.mmsOpts.DataStructureNestingLevel != 10 {
		t.Fatalf("nesting %d, want the server's 10", c.mmsOpts.DataStructureNestingLevel)
	}
	// A proposal below the server value is taken as is.
	_, ir, _ = acceptInitiate(t, defaultOpts(), pdu.DefaultInitiateRequest(100, 5, 5, 3))
	if ir.LocalDetailCalled != 100 || ir.NegotiatedDataStructureNesting != 3 {
		t.Fatalf("%+v", ir)
	}
}

// ISO 9506-2 8.2, IEC 61850-8-1 Tables 109/110/13 (MMS-STK-14): the
// version is 1 and unacceptable proposals are refused with an
// Initiate-ErrorPDU of class initiate.
func TestSpec_8_1_Table13_InitiateErrors(t *testing.T) {
	req := pdu.DefaultInitiateRequest(65000, 5, 5, 10)
	req.InitRequestDetail.ProposedVersion = 2
	if _, ir, _ := acceptInitiate(t, defaultOpts(), req); ir.InitResponseDetail.NegotiatedVersion != 1 {
		t.Fatalf("version %d", ir.InitResponseDetail.NegotiatedVersion)
	}
	for name, tc := range map[string]struct {
		mutate func(*pdu.InitiateRequest)
		code   int
	}{
		"version 0":     {func(r *pdu.InitiateRequest) { r.InitRequestDetail.ProposedVersion = 0 }, pdu.InitiateErrVersionIncompatible},
		"calling 0":     {func(r *pdu.InitiateRequest) { r.ProposedMaxServOutstandingCall = 0 }, pdu.InitiateErrMaxOutstandingCallingInsuff},
		"called 0":      {func(r *pdu.InitiateRequest) { r.ProposedMaxServOutstandingCalled = 0 }, pdu.InitiateErrMaxOutstandingCalledInsuff},
		"localDetail-1": {func(r *pdu.InitiateRequest) { r.LocalDetailCalling = -1 }, pdu.InitiateErrMaxSegmentInsufficient},
	} {
		t.Run(name, func(t *testing.T) {
			r := pdu.DefaultInitiateRequest(65000, 5, 5, 10)
			tc.mutate(&r)
			b, err := pdu.MarshalInitiateRequest(r)
			if err != nil {
				t.Fatal(err)
			}
			tr := &scriptTransport{in: [][]byte{mustAssocReq(t, b)}}
			c := New(tr, discardLogger(), nopHandler, defaultOpts())
			_, err = c.ReceiveAssociation(context.Background())
			ie, ok := errors.AsType[*InitiateError](err)
			if !ok || ie.Code != tc.code || !errors.Is(err, ErrRefused) || ie.Error() == "" {
				t.Fatalf("err %v", err)
			}
			_, err = isostack.DecodeAssociateResponse(tr.sentFrames()[0])
			rj, ok := errors.AsType[*isostack.Rejection](err)
			if !ok {
				t.Fatal(err)
			}
			if !bytes.Equal(rj.MmsPayload, pdu.MarshalInitiateError(pdu.ErrorClassInitiate, tc.code)) {
				t.Fatalf("Initiate-Error %x", rj.MmsPayload)
			}
		})
	}
}

// ISO 8823-1, ISO 9506-2 Annex A.5 (MMS-STK-07): an association on
// presentation contexts {1: ACSE, 5: MMS} is served on context 5 and
// released on context 1.
func TestSpec_8823_ServeOnNegotiatedContexts(t *testing.T) {
	ctxDef := func(id int, abs []byte) []byte {
		c := berutil.EncodeTLV(0x02, berutil.EncodeInt(id))
		c = berutil.AppendTLV(c, 0x06, abs)
		c = berutil.AppendTLV(c, 0x30, berutil.EncodeTLV(0x06, []byte{0x51, 0x01}))
		return berutil.EncodeTLV(0x30, c)
	}
	aarq, err := acse.EncodeAARQ(acse.AARQParams{MMSContextID: 5}, mustInitReq(t, 65000, 5, 5, 10))
	if err != nil {
		t.Fatal(err)
	}
	normal := berutil.EncodeTLV(0xa4, append(ctxDef(1, []byte{0x52, 0x01, 0x00, 0x01}), ctxDef(5, []byte{0x28, 0xca, 0x22, 0x02, 0x01})...))
	normal = append(normal, presentation.EncodeUserData(1, aarq)...)
	cp := berutil.EncodeTLV(0x31, append(berutil.EncodeTLV(0xa0, []byte{0x80, 0x01, 0x01}), berutil.EncodeTLV(0xa2, normal)...))
	cn, err := session.EncodeConnect(session.ConnectParams{}, cp)
	if err != nil {
		t.Fatal(err)
	}
	five := isostack.Contexts{ACSE: 1, MMS: 5}
	handler := func(_ context.Context, _ codec.InvokeID, tag int, _ []byte) (int, bool, []byte, error) {
		return tag, true, nil, nil
	}
	tr := &scriptTransport{in: [][]byte{
		cn,
		isostack.EncodeData(five, mustConfirmedReq(t, 1, 1, nil)),
		isostack.EncodeDataRequest(mustConfirmedReq(t, 2, 1, nil)), // wrong context: not served
	}}
	c := New(tr, discardLogger(), handler, defaultOpts())
	ctx := context.Background()
	if _, err := c.ReceiveAssociation(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.AcceptAssociation(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Serve(ctx); err == nil {
		t.Fatal("data on context 3 accepted")
	}
	sent := tr.sentFrames()
	if len(sent) != 2 {
		t.Fatalf("sent %d frames", len(sent))
	}
	if _, err := isostack.DecodeData(five, sent[1]); err != nil {
		t.Fatalf("response not on context 5: %v", err)
	}
	if err := c.SendAbort(ctx); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(tr.sentFrames()[2], isostack.EncodeAbortOn(five)) {
		t.Fatal("abort not on the ACSE context")
	}
}

// ISO 9506-2 Annex A.1.2.2 (MMS-STK-02, -13): after the Conclude-Response
// the server waits for A-RELEASE, answers DISCONNECT + RLRE and aborts
// anything else.
func TestSpec_9506_A12_ServerRelease(t *testing.T) {
	conclude := dataFrame(codec.MarshalConcludeRequest())
	for name, tc := range map[string]struct {
		in      [][]byte
		timeout time.Duration
		wantErr bool
		last    []byte // expected last frame sent (nil: the Conclude-Response)
		frames  int
	}{
		"release":         {in: [][]byte{conclude, isostack.EncodeReleaseRequest()}, last: isostack.EncodeReleaseResponse(), frames: 2},
		"peer closes":     {in: [][]byte{conclude}, frames: 1},
		"peer aborts":     {in: [][]byte{conclude, isostack.EncodeAbort()}, frames: 1},
		"no wait":         {in: [][]byte{conclude, isostack.EncodeReleaseRequest()}, timeout: -1, frames: 1},
		"data, not FINSH": {in: [][]byte{conclude, conclude}, wantErr: true, last: isostack.EncodeAbort(), frames: 2},
		"FINISH with RLRE": {in: [][]byte{conclude, finishWith(acse.EncodeRLRE())},
			wantErr: true, last: isostack.EncodeAbort(), frames: 2},
	} {
		t.Run(name, func(t *testing.T) {
			opts := defaultOpts()
			opts.ReleaseTimeout = tc.timeout
			tr := &scriptTransport{in: tc.in}
			c := New(tr, discardLogger(), nopHandler, opts)
			err := c.Serve(context.Background())
			if (err != nil) != tc.wantErr {
				t.Fatalf("Serve: %v", err)
			}
			sent := tr.sentFrames()
			if len(sent) != tc.frames {
				t.Fatalf("sent %d frames, want %d", len(sent), tc.frames)
			}
			expectConcludeResponse(t, sent[0])
			if tc.last != nil && !bytes.Equal(sent[len(sent)-1], tc.last) {
				t.Fatalf("last frame %x, want %x", sent[len(sent)-1], tc.last)
			}
		})
	}
}

// finishWith builds a FINISH whose user data carries apdu on context 1.
func finishWith(apdu []byte) []byte {
	b, err := session.EncodeFinish(presentation.EncodeUserData(1, apdu))
	if err != nil {
		panic(err)
	}
	return b
}

// The wait for A-RELEASE is bounded by ReleaseTimeout and by ctx.
func TestSpec_9506_A12_ReleaseWaitBounded(t *testing.T) {
	client, server := loopbackPair()
	defer func() { _ = client.Close() }()
	opts := defaultOpts()
	opts.ReleaseTimeout = 50 * time.Millisecond
	c := New(server, discardLogger(), nopHandler, opts)
	client.send <- dataFrame(codec.MarshalConcludeRequest())
	start := time.Now()
	if err := c.Serve(context.Background()); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("release wait took %v", d)
	}

	// Cancelling ctx while waiting is an error.
	client2, server2 := loopbackPair()
	defer func() { _ = client2.Close() }()
	opts.ReleaseTimeout = time.Minute
	c = New(server2, discardLogger(), nopHandler, opts)
	ctx, cancel := context.WithCancel(context.Background())
	client2.send <- dataFrame(codec.MarshalConcludeRequest())
	done := make(chan error, 1)
	go func() { done <- c.Serve(ctx) }()
	<-client2.recv // Conclude-Response
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after cancel")
	}
}

// The release state machine under racing closes and aborts: every Serve
// returns and no goroutine is left behind (goleak in TestMain).
func TestChaos_ReleaseRaces(t *testing.T) {
	for i := range 50 {
		client, server := loopbackPair()
		opts := defaultOpts()
		opts.ReleaseTimeout = time.Duration(i%5) * time.Millisecond
		c := New(server, discardLogger(), nopHandler, opts)
		done := make(chan error, 1)
		go func() { done <- c.Serve(context.Background()) }()
		client.send <- dataFrame(codec.MarshalConcludeRequest())
		switch i % 3 {
		case 0:
			client.send <- isostack.EncodeReleaseRequest()
		case 1:
			client.send <- isostack.EncodeAbort()
		case 2:
			close(client.send)
		}
		aborted := make(chan struct{})
		go func() { _ = c.SendAbort(context.Background()); close(aborted) }()
		select {
		case err := <-done:
			t.Logf("iteration %d: Serve: %v", i, err)
		case <-time.After(5 * time.Second):
			t.Fatalf("iteration %d: Serve did not return", i)
		}
		<-aborted
		_ = c.Close()
	}
}
