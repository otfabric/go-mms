// SPDX-License-Identifier: MIT

package iso

import (
	"context"
	"errors"
	"math"
	"net"
	"testing"
	"time"

	"github.com/otfabric/go-cotp"

	mms "github.com/otfabric/go-mms"
)

func TestMaxTSDULength(t *testing.T) {
	for _, tc := range []struct{ maxPDU, want int }{
		{0, maxSessionSPDU},
		{-1, maxSessionSPDU},
		{1000, maxSessionSPDU},
		{defaultMMSPDUSize, maxSessionSPDU},
		{maxSessionSPDU - dataFraming, maxSessionSPDU},
		{maxSessionSPDU - dataFraming + 1, maxSessionSPDU + 1},
		{1_000_000, 1_000_000 + dataFraming},
		{math.MaxInt - dataFraming, math.MaxInt},
		{math.MaxInt, math.MaxInt},
	} {
		if got := maxTSDULength(tc.maxPDU); got != tc.want {
			t.Errorf("maxTSDULength(%d) = %d, want %d", tc.maxPDU, got, tc.want)
		}
	}
	if maxSessionSPDU != 65539 {
		t.Errorf("maxSessionSPDU = %d, want 65539", maxSessionSPDU)
	}
}

// pduLimit is what the mms package reads from a transport.
type pduLimit interface{ MaxMMSPDU() int }

func tsduCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// A Listener receives a TSDU of up to maxTSDULength(0) octets; go-cotp
// refuses a longer one before it holds it and ends the connection.
func TestListenerTSDUBound(t *testing.T) {
	limit := maxTSDULength(0)
	l, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	for _, size := range []int{limit - 1, limit, limit + 1} {
		ctx := tsduCtx(t)
		conn, err := net.Dial("tcp", l.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		sent := make(chan struct{})
		go func() {
			defer close(sent)
			peer, err := cotp.Connect(ctx, conn, cotp.ClientConfig{MaxTSDULength: 1 << 20})
			if err == nil {
				_ = peer.WriteTSDU(ctx, make([]byte, size))
			}
		}()
		tr, err := l.Accept(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if got := tr.(pduLimit).MaxMMSPDU(); got != limit-dataFraming {
			t.Errorf("MaxMMSPDU = %d, want %d", got, limit-dataFraming)
		}
		data, err := tr.Receive(ctx)
		if size <= limit && (err != nil || len(data) != size) {
			t.Errorf("TSDU of %d octets (limit %d): %d octets, %v", size, limit, len(data), err)
		}
		if size > limit && !errors.Is(err, cotp.ErrTSDUTooLarge) {
			t.Errorf("TSDU of %d octets (limit %d): %d octets, %v; want ErrTSDUTooLarge", size, limit, len(data), err)
		}
		_ = tr.Close()
		_ = conn.Close()
		<-sent
	}
}

// A client's TSDU bound follows the MMS PDU size it proposes: the default,
// or MMS.MaxPDUSize of WithClientDialOptions.
func TestDialTSDUBound(t *testing.T) {
	for _, tc := range []struct {
		name  string
		opts  []Option
		limit int
	}{
		{"default", nil, maxSessionSPDU},
		{"MaxPDUSize 100000", []Option{WithClientDialOptions(mms.DialOptions{MMS: mms.MMSOptions{MaxPDUSize: 100_000}})}, 100_000 + dataFraming},
	} {
		for _, size := range []int{tc.limit, tc.limit + 1} {
			ctx := tsduCtx(t)
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			served := make(chan struct{})
			go func() {
				defer close(served)
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				defer func() { _ = conn.Close() }()
				peer, err := cotp.Accept(ctx, conn, cotp.ServerConfig{MaxTSDULength: 1 << 20})
				if err == nil {
					_ = peer.WriteTSDU(ctx, make([]byte, size))
					_, _ = peer.ReadTSDU(ctx) // until the client closes
				}
			}()
			tr, err := DialTCP(ctx, ln.Addr().String(), tc.opts...)
			if err != nil {
				t.Fatal(err)
			}
			if got := tr.(pduLimit).MaxMMSPDU(); got != tc.limit-dataFraming {
				t.Errorf("%s: MaxMMSPDU = %d, want %d", tc.name, got, tc.limit-dataFraming)
			}
			data, err := tr.Receive(ctx)
			if size <= tc.limit && (err != nil || len(data) != size) {
				t.Errorf("%s: TSDU of %d octets: %d octets, %v", tc.name, size, len(data), err)
			}
			if size > tc.limit && !errors.Is(err, cotp.ErrTSDUTooLarge) {
				t.Errorf("%s: TSDU of %d octets: %d octets, %v; want ErrTSDUTooLarge", tc.name, size, len(data), err)
			}
			_ = tr.Close()
			<-served
			_ = ln.Close()
		}
	}
}

// Over a Listener, a server whose MaxPDUSize is larger than the transport
// carries supports the PDU size the transport carries, and a client of
// Dial that proposes more is answered with it.
func TestListenerPDUSizeClamp(t *testing.T) {
	ctx := tsduCtx(t)
	l, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := mms.NewServer(mms.ServerOptions{MMS: mms.ServerMMSOptions{MaxPDUSize: 1_000_000}})
	served := make(chan error, 1)
	go func() { served <- srv.ListenAndServe(ctx, l) }()
	c, err := Dial(ctx, l.Addr().String(), WithClientDialOptions(mms.DialOptions{MMS: mms.MMSOptions{MaxPDUSize: 1_000_000}}))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := c.Negotiated().MaxPDUSize, maxTSDULength(0)-dataFraming; got != want {
		t.Errorf("negotiated PDU size %d, want %d", got, want)
	}
	_ = c.Close(ctx)
	_ = srv.Shutdown(ctx)
	if err := <-served; !errors.Is(err, mms.ErrServerClosed) {
		t.Errorf("ListenAndServe = %v, want ErrServerClosed", err)
	}
}
