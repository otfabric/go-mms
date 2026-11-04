// SPDX-License-Identifier: MIT

package iso_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-cotp"
	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

// captureCR dials with opts against a raw peer that records the CR, answers
// with cc(cr) (a CC echoing nothing when cc is nil) and returns the CR and
// the dial error.
func captureCR(t *testing.T, cc func(*cotp.CR) []byte, opts ...iso.Option) (*cotp.CR, []byte, error) {
	t.Helper()
	tcpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tcpLn.Close() }()

	got := make(chan []byte, 1)
	go func() {
		conn, err := tcpLn.Accept()
		if err != nil {
			got <- nil
			return
		}
		defer func() { _ = conn.Close() }()
		frame, err := readPeerTPDU(conn)
		got <- frame
		if err != nil {
			return
		}
		d, err := cotp.Decode(frame)
		if err != nil || d.CR == nil {
			return
		}
		var resp []byte
		if cc != nil {
			resp = cc(d.CR)
		} else {
			resp, _ = (&cotp.CC{DestinationRef: d.CR.SourceRef, SourceRef: 7}).MarshalBinary()
		}
		_ = writePeerTPDUErr(conn, resp)
		_, _ = readPeerTPDU(conn) // hold the connection until the client closes
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tr, dialErr := iso.DialTCP(ctx, tcpLn.Addr().String(), opts...)
	if tr != nil {
		_ = tr.Close()
	}
	frame := <-got
	if frame == nil {
		t.Fatal("peer received no CR")
	}
	d, err := cotp.Decode(frame)
	if err != nil || d.CR == nil {
		t.Fatalf("decode CR %x: %v", frame, err)
	}
	return d.CR, frame, dialErr
}

// TestDialCRDefaults pins the CR a client sends without configuration:
// calling and called T-selector 00 01 and TPDU size 1024 (0xC0 = 0x0A),
// what IEDs such as the REB500 of go-iec61850#28 require.
func TestDialCRDefaults(t *testing.T) {
	cr, frame, err := captureCR(t, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if !bytes.Equal(cr.CallingSelector, []byte{0x00, 0x01}) || !bytes.Equal(cr.CalledSelector, []byte{0x00, 0x01}) {
		t.Fatalf("selectors calling=%x called=%x, want 0001/0001", cr.CallingSelector, cr.CalledSelector)
	}
	if cr.TPDUSize == nil || *cr.TPDUSize != 0x0A {
		t.Fatalf("TPDU size = %v, want 0x0A", cr.TPDUSize)
	}
	if cr.PreferredMaxTPDUSize != nil {
		t.Fatalf("preferred max = %v, want absent", *cr.PreferredMaxTPDUSize)
	}
	// Fixed part and variable part on the wire (SRC-REF masked).
	want := []byte{0x11, 0xE0, 0x00, 0x00, 0, 0, 0x00,
		0xC1, 0x02, 0x00, 0x01, 0xC2, 0x02, 0x00, 0x01, 0xC0, 0x01, 0x0A}
	got := append([]byte(nil), frame...)
	if len(got) >= 6 {
		got[4], got[5] = 0, 0
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("CR = % x\nwant % x", got, want)
	}
}

func TestDialCRConfigured(t *testing.T) {
	four := []byte{0x00, 0x00, 0x00, 0x01}
	cases := []struct {
		name                string
		opts                []iso.Option
		wantCalling, wantCd []byte
		wantSize            *uint8
	}{
		{name: "one octet", opts: []iso.Option{iso.WithCallingTSelector([]byte{1}), iso.WithCalledTSelector([]byte{1})},
			wantCalling: []byte{1}, wantCd: []byte{1}, wantSize: new(uint8(0x0A))},
		{name: "four octets", opts: []iso.Option{iso.WithCallingTSelector(four), iso.WithCalledTSelector(four)},
			wantCalling: four, wantCd: four, wantSize: new(uint8(0x0A))},
		{name: "omit both", opts: []iso.Option{iso.WithCallingTSelector([]byte{}), iso.WithCalledTSelector([]byte{})},
			wantSize: new(uint8(0x0A))},
		{name: "called only", opts: []iso.Option{iso.WithCallingTSelector([]byte{})},
			wantCd: []byte{0x00, 0x01}, wantSize: new(uint8(0x0A))},
		{name: "nil restores the default", opts: []iso.Option{iso.WithCalledTSelector(four), iso.WithCalledTSelector(nil)},
			wantCalling: []byte{0, 1}, wantCd: []byte{0, 1}, wantSize: new(uint8(0x0A))},
		{name: "tpdu 2048", opts: []iso.Option{iso.WithMaxTPDULength(2048)},
			wantCalling: []byte{0, 1}, wantCd: []byte{0, 1}, wantSize: new(uint8(0x0B))},
		{name: "tpdu omitted (RFC 1006)", opts: []iso.Option{iso.WithMaxTPDULength(65531)},
			wantCalling: []byte{0, 1}, wantCd: []byte{0, 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cr, _, err := captureCR(t, nil, tc.opts...)
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			if !sameSelector(cr.CallingSelector, tc.wantCalling) || !sameSelector(cr.CalledSelector, tc.wantCd) {
				t.Fatalf("selectors calling=%x called=%x, want %x/%x", cr.CallingSelector, cr.CalledSelector, tc.wantCalling, tc.wantCd)
			}
			if (cr.TPDUSize == nil) != (tc.wantSize == nil) || (cr.TPDUSize != nil && *cr.TPDUSize != *tc.wantSize) {
				t.Fatalf("TPDU size = %v, want %v", cr.TPDUSize, tc.wantSize)
			}
		})
	}
}

func sameSelector(got, want []byte) bool {
	return len(got) == len(want) && bytes.Equal(got, want)
}

// TestDialTSelectorTooLong: a client refuses a T-selector longer than
// IEC 61850-8-1 allows before it opens a connection.
func TestDialTSelectorTooLong(t *testing.T) {
	five := []byte{0, 0, 0, 0, 1}
	for name, opt := range map[string]iso.Option{
		"called":  iso.WithCalledTSelector(five),
		"calling": iso.WithCallingTSelector(five),
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			dialed := false
			_, err := iso.DialTCP(ctx, "127.0.0.1:1", opt,
				iso.WithDialer(func(context.Context, string, string) (net.Conn, error) {
					dialed = true
					return nil, errors.New("must not dial")
				}),
			)
			if err == nil || dialed || !strings.Contains(err.Error(), name+" T-selector") {
				t.Fatalf("err = %v dialed = %v, want a %s T-selector error before dialing", err, dialed, name)
			}
		})
	}
}

// TestDialAcceptsDifferingRespondingTSelector: an IED may answer the called
// 00 01 with a responding 00 00 00 01 in the CC (X.224 §13.3.4).
func TestDialAcceptsDifferingRespondingTSelector(t *testing.T) {
	_, _, err := captureCR(t, func(cr *cotp.CR) []byte {
		b, _ := (&cotp.CC{
			DestinationRef:  cr.SourceRef,
			SourceRef:       9,
			CallingSelector: cr.CallingSelector,
			CalledSelector:  []byte{0, 0, 0, 1},
			TPDUSize:        cr.TPDUSize,
		}).MarshalBinary()
		return b
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
}

// TestListenerEchoesTSelectors: a Listener returns the CR's selectors in
// the CC, as libiec61850 servers do, and accepts any called selector.
func TestListenerEchoesTSelectors(t *testing.T) {
	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		for {
			tr, err := ln.Accept(ctx)
			if err != nil {
				return
			}
			_ = tr.Close()
		}
	}()

	for _, called := range [][]byte{nil, {0x00, 0x01}, {0, 0, 0, 1}, {0x07}} {
		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		cr, _ := (&cotp.CR{SourceRef: 0x42, CallingSelector: []byte{0x00, 0x02}, CalledSelector: called, TPDUSize: new(uint8(0x0A))}).MarshalBinary()
		if err := writePeerTPDUErr(conn, cr); err != nil {
			t.Fatal(err)
		}
		frame, err := readPeerTPDU(conn)
		_ = conn.Close()
		if err != nil {
			t.Fatal(err)
		}
		d, err := cotp.Decode(frame)
		if err != nil || d.CC == nil {
			t.Fatalf("called %x: response %x is not a CC: %v", called, frame, err)
		}
		if !bytes.Equal(d.CC.CallingSelector, []byte{0x00, 0x02}) || !sameSelector(d.CC.CalledSelector, called) {
			t.Fatalf("called %x: CC calling=%x responding=%x", called, d.CC.CallingSelector, d.CC.CalledSelector)
		}
	}
}

// TestStrictListener models an IED that requires its called T-selector: a
// default client connects, a client omitting the selector or sending the
// 4-octet form is refused with reason 3 (address unknown).
func TestStrictListener(t *testing.T) {
	srv := testServer(t)
	ln, err := iso.Listen("127.0.0.1:0", iso.WithCalledTSelector([]byte{0x00, 0x01}))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() { _ = srv.ListenAndServe(ctx, ln) }()

	client, err := iso.Dial(ctx, ln.Addr().String())
	if err != nil {
		t.Fatalf("default client against strict listener: %v", err)
	}
	if _, err := client.Identify(ctx); err != nil {
		t.Fatalf("identify: %v", err)
	}
	_ = client.Close(ctx)

	for _, called := range [][]byte{{}, {0, 0, 0, 1}, {0x00, 0x02}} {
		_, err := iso.Dial(ctx, ln.Addr().String(), iso.WithCalledTSelector(called))
		rej, ok := errors.AsType[*cotp.RejectionError](err)
		if !ok || rej.Reason != cotp.ReasonAddressUnknown {
			t.Fatalf("called %x: err = %v, want RejectionError address unknown", called, err)
		}
	}
}

// TestDialTSelectorsFromDialOptions: mms.DialOptions.Transport selectors
// are used by Dial, and a non-nil empty one omits the parameter.
func TestDialTSelectorsFromDialOptions(t *testing.T) {
	srv := testServer(t)
	ln, err := iso.Listen("127.0.0.1:0", iso.WithCalledTSelector([]byte{0x00, 0x05}))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() { _ = srv.ListenAndServe(ctx, ln) }()

	client, err := iso.Dial(ctx, ln.Addr().String(), iso.WithClientDialOptions(mms.DialOptions{
		Transport: mms.TransportOptions{LocalTSelector: []byte{}, RemoteTSelector: []byte{0x00, 0x05}},
	}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = client.Close(ctx)
}
