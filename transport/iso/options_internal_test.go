// SPDX-License-Identifier: MIT

package iso

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"
)

func TestApplyOptions_WithClientDialOptions(t *testing.T) {
	opts := applyOptions([]Option{
		WithClientDialOptions(mms.DialOptions{
			MMS: mms.MMSOptions{MaxPDUSize: 32000},
		}),
	})
	if !opts.hasMmsDialOpts {
		t.Fatal("hasMmsDialOpts = false, want true")
	}
	if opts.mmsDialOpts.MMS.MaxPDUSize != 32000 {
		t.Fatalf("MaxPDUSize = %d, want 32000", opts.mmsDialOpts.MMS.MaxPDUSize)
	}
}

func TestApplyOptions_WithLogger(t *testing.T) {
	l := slog.Default()
	opts := applyOptions([]Option{WithLogger(l)})
	if opts.logger != l {
		t.Fatal("logger not applied")
	}
}

func TestRemoteAddr_NilWhenClosedAndNoRaw(t *testing.T) {
	tr := &cotpTransport{} // both cotp and raw nil
	if got := tr.RemoteAddr(); got != nil {
		t.Fatalf("RemoteAddr() = %v, want nil", got)
	}
}

func TestIsClosed(t *testing.T) {
	tr := &cotpTransport{}
	if !tr.isClosed() {
		t.Fatal("empty transport should be closed")
	}
}

func TestRemoteAddr_FallsBackToRaw(t *testing.T) {
	c1, c2 := net.Pipe()
	defer func() { _ = c1.Close() }()
	defer func() { _ = c2.Close() }()

	tr := &cotpTransport{raw: c1}
	got := tr.RemoteAddr()
	if got == nil {
		t.Fatal("RemoteAddr() nil, want raw peer address")
	}
}

func TestRemoteAddr_COTPAndAfterClose(t *testing.T) {
	ln, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		tr, err := ln.Accept(ctx)
		if err == nil {
			_ = tr.Close()
		}
	}()

	client, err := DialTCP(ctx, ln.Addr().String())
	if err != nil {
		wg.Wait()
		t.Fatalf("DialTCP: %v", err)
	}
	ra, ok := client.(mms.RemoteAddrTransport)
	if !ok {
		_ = client.Close()
		wg.Wait()
		t.Fatal("transport does not implement RemoteAddrTransport")
	}
	if ra.RemoteAddr() == nil {
		_ = client.Close()
		wg.Wait()
		t.Fatal("RemoteAddr nil with live COTP")
	}
	_ = client.Close()
	if ra.RemoteAddr() == nil {
		wg.Wait()
		t.Fatal("RemoteAddr nil after Close (raw fallback)")
	}
	wg.Wait()
}
