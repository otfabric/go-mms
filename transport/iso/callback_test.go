// SPDX-License-Identifier: MIT

package iso_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-mms/transport/iso"
)

// lockedLog is a goroutine-safe log sink.
type lockedLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lockedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// A callback of the listener's TLS configuration that does not return
// normally closes that connection and is logged with its stack; the
// listener accepts the next connection.
func TestListenerTLSCallbackNotReturningNormally(t *testing.T) {
	serverCert, pool := selfSignedCert(t, "localhost")
	var calls atomic.Int32
	serverTLS := &tls.Config{GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		if calls.Add(1) == 1 {
			panic("certificate store unavailable")
		}
		return &serverCert, nil
	}}
	var logs lockedLog
	ln, err := iso.Listen("127.0.0.1:0", iso.WithTLSConfig(serverTLS),
		iso.WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	accepted := make(chan error, 1)
	go func() {
		tr, err := ln.Accept(ctx)
		if err == nil {
			_ = tr.Close()
		}
		accepted <- err
	}()

	clientTLS := &tls.Config{RootCAs: pool, ServerName: "localhost"}
	if tr, err := iso.DialTCP(ctx, ln.Addr().String(), iso.WithTLSConfig(clientTLS)); err == nil {
		_ = tr.Close()
		t.Fatal("dial succeeded through a GetCertificate that did not return normally")
	}
	tr, err := iso.DialTCP(ctx, ln.Addr().String(), iso.WithTLSConfig(clientTLS))
	if err != nil {
		t.Fatalf("next dial: %v", err)
	}
	defer func() { _ = tr.Close() }()
	if err := <-accepted; err != nil {
		t.Fatalf("Accept = %v", err)
	}
	for _, want := range []string{
		`level=ERROR msg="iso: handshake callback did not return normally, closing connection"`,
		`value="certificate store unavailable"`,
		"remote_addr=127.0.0.1:",
		"stack=",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("log lacks %s: %q", want, logs.String())
		}
	}
}
