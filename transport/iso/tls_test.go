// SPDX-License-Identifier: MIT

package iso_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

// tlsTestServer creates a minimal MMS server for TLS integration tests.
// Self-contained so tls_test.go does not depend on integration_test.go helpers.
func tlsTestServer(t *testing.T) *mms.Server {
	t.Helper()
	srv := mms.NewServer(mms.ServerOptions{
		Logger: slog.New(slog.NewTextHandler(tlsTestLogWriter{t}, &slog.HandlerOptions{Level: slog.LevelDebug})),
		MMS: mms.ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
	})
	srv.HandleIdentify(func(_ context.Context, _ mms.IdentifyRequest) (*mms.ServerIdentity, error) {
		return &mms.ServerIdentity{Vendor: "TLSTest", Model: "Test", Revision: "1.0"}, nil
	})
	if err := srv.RegisterDomain("testDomain"); err != nil {
		t.Fatal(err)
	}
	if err := srv.RegisterVariable(mms.Variable{
		Name:     mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: "testDomain", ItemID: "temperature"},
		TypeSpec: mms.TypeSpec{Type: mms.ValueTypeFloat, FormatWidth: 32, ExponentWidth: 8},
		Read: func(_ context.Context) (*mms.Value, error) {
			return mms.NewFloat(36.6), nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	return srv
}

type tlsTestLogWriter struct{ t *testing.T }

func (w tlsTestLogWriter) Write(p []byte) (int, error) {
	w.t.Helper()
	w.t.Log(string(p))
	return len(p), nil
}

func selfSignedCert(t *testing.T, cn string) (tls.Certificate, *x509.CertPool) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost", cn},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatal(err)
	}

	pool := x509.NewCertPool()
	pool.AddCert(cert)

	return tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
		Leaf:        cert,
	}, pool
}

func TestTLSDialAndListen(t *testing.T) {
	serverCert, serverPool := selfSignedCert(t, "localhost")

	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
	}
	clientTLS := &tls.Config{
		RootCAs:    serverPool,
		ServerName: "localhost",
	}

	ln, err := iso.Listen("127.0.0.1:0", iso.WithTLSConfig(serverTLS))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var serverConn mms.Transport
	var acceptErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		serverConn, acceptErr = ln.Accept(ctx)
	}()

	clientConn, err := iso.DialTCP(ctx, ln.Addr().String(), iso.WithTLSConfig(clientTLS))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer clientConn.Close()

	wg.Wait()
	if acceptErr != nil {
		t.Fatalf("accept: %v", acceptErr)
	}
	defer serverConn.Close()

	msg := []byte("tls integration test")
	if err := clientConn.Send(ctx, msg); err != nil {
		t.Fatalf("send: %v", err)
	}
	got, err := serverConn.Receive(ctx)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if string(got) != string(msg) {
		t.Fatalf("got %q, want %q", got, msg)
	}
}

func TestTLSConnectionState(t *testing.T) {
	serverCert, serverPool := selfSignedCert(t, "localhost")
	clientCert, clientPool := selfSignedCert(t, "client")

	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientPool,
	}

	clientTLS := &tls.Config{
		RootCAs:      serverPool,
		ServerName:   "localhost",
		Certificates: []tls.Certificate{clientCert},
	}

	ln, err := iso.Listen("127.0.0.1:0", iso.WithTLSConfig(serverTLS))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var serverConn mms.Transport
	var acceptErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		serverConn, acceptErr = ln.Accept(ctx)
	}()

	clientConn, err := iso.DialTCP(ctx, ln.Addr().String(), iso.WithTLSConfig(clientTLS))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer clientConn.Close()

	wg.Wait()
	if acceptErr != nil {
		t.Fatalf("accept: %v", acceptErr)
	}
	defer serverConn.Close()

	tt, ok := serverConn.(mms.TLSTransport)
	if !ok {
		t.Fatal("server transport does not implement TLSTransport")
	}
	state := tt.TLSConnectionState()
	if state == nil {
		t.Fatal("TLSConnectionState() returned nil")
	}
	if len(state.PeerCertificates) == 0 {
		t.Fatal("expected peer certificates from client")
	}
	if state.PeerCertificates[0].Subject.CommonName != "client" {
		t.Errorf("peer CN = %q, want %q", state.PeerCertificates[0].Subject.CommonName, "client")
	}
}

func TestTLSVerificationFailure(t *testing.T) {
	serverCert, _ := selfSignedCert(t, "localhost")

	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
	}

	_, otherPool := selfSignedCert(t, "other-ca")
	clientTLS := &tls.Config{
		RootCAs:    otherPool,
		ServerName: "localhost",
	}

	ln, err := iso.Listen("127.0.0.1:0", iso.WithTLSConfig(serverTLS))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		ln.Accept(ctx)
	}()

	_, err = iso.DialTCP(ctx, ln.Addr().String(), iso.WithTLSConfig(clientTLS))
	if err == nil {
		t.Fatal("expected TLS verification error")
	}
	t.Logf("got expected error: %v", err)
}

func TestTLSEndToEndMMS(t *testing.T) {
	serverCert, serverPool := selfSignedCert(t, "localhost")

	srv := tlsTestServer(t)

	ln, err := iso.Listen("127.0.0.1:0", iso.WithTLSConfig(&tls.Config{
		Certificates: []tls.Certificate{serverCert},
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go srv.ListenAndServe(ctx, ln)

	client, err := iso.Dial(ctx, ln.Addr().String(), iso.WithTLSConfig(&tls.Config{
		RootCAs:    serverPool,
		ServerName: "localhost",
	}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close(ctx)

	id, err := client.Identify(ctx)
	if err != nil {
		t.Fatalf("identify: %v", err)
	}
	if id.Vendor != "TLSTest" {
		t.Errorf("vendor = %q, want %q", id.Vendor, "TLSTest")
	}

	rr, err := client.Read(ctx, mms.ReadRequest{
		DomainID: "testDomain",
		ItemID:   "temperature",
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	f, ok := rr.Value.Float64()
	if !ok {
		t.Fatal("expected float value")
	}
	if f != 36.6 {
		t.Errorf("temperature = %v, want 36.6", f)
	}
}

func TestTLSWithAuthenticator(t *testing.T) {
	serverCert, serverPool := selfSignedCert(t, "localhost")
	clientCert, clientPool := selfSignedCert(t, "operator-1")

	var gotAuth *mms.AuthContext
	srv := mms.NewServer(mms.ServerOptions{
		Logger: slog.New(slog.NewTextHandler(tlsTestLogWriter{t}, &slog.HandlerOptions{Level: slog.LevelDebug})),
		MMS: mms.ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
		Authenticate: func(_ context.Context, auth *mms.AuthContext) (mms.AuthResult, error) {
			gotAuth = auth
			if auth.Mechanism != mms.AuthMechanismTLSCertificate {
				return mms.AuthResult{}, fmt.Errorf("expected TLS auth, got %v", auth.Mechanism)
			}
			if auth.PeerCertificate == nil {
				return mms.AuthResult{}, fmt.Errorf("no peer certificate")
			}
			if auth.PeerCertificate.Subject.CommonName != "operator-1" {
				return mms.AuthResult{}, fmt.Errorf("unexpected CN: %s", auth.PeerCertificate.Subject.CommonName)
			}
			return mms.AuthResult{Accept: true, Token: auth.PeerCertificate.Subject.CommonName}, nil
		},
	})

	srv.HandleIdentify(func(_ context.Context, _ mms.IdentifyRequest) (*mms.ServerIdentity, error) {
		return &mms.ServerIdentity{Vendor: "TLSAuth", Model: "Test", Revision: "1.0"}, nil
	})

	ln, err := iso.Listen("127.0.0.1:0", iso.WithTLSConfig(&tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientPool,
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go srv.ListenAndServe(ctx, ln)

	client, err := iso.Dial(ctx, ln.Addr().String(), iso.WithTLSConfig(&tls.Config{
		RootCAs:      serverPool,
		ServerName:   "localhost",
		Certificates: []tls.Certificate{clientCert},
	}))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close(ctx)

	id, err := client.Identify(ctx)
	if err != nil {
		t.Fatalf("identify: %v", err)
	}
	if id.Vendor != "TLSAuth" {
		t.Errorf("vendor = %q, want TLSAuth", id.Vendor)
	}

	if gotAuth.Mechanism != mms.AuthMechanismTLSCertificate {
		t.Errorf("mechanism = %v, want AuthMechanismTLSCertificate", gotAuth.Mechanism)
	}
	if gotAuth.PeerCertificate == nil {
		t.Fatal("expected peer certificate")
	}
	if gotAuth.PeerCertificate.Subject.CommonName != "operator-1" {
		t.Errorf("peer CN = %q, want operator-1", gotAuth.PeerCertificate.Subject.CommonName)
	}
	if gotAuth.RemoteAddr == nil {
		t.Error("expected RemoteAddr to be set")
	}
}

func TestTLSAuthenticatorReject(t *testing.T) {
	serverCert, serverPool := selfSignedCert(t, "localhost")

	srv := mms.NewServer(mms.ServerOptions{
		Logger: slog.New(slog.NewTextHandler(tlsTestLogWriter{t}, &slog.HandlerOptions{Level: slog.LevelDebug})),
		MMS: mms.ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
		Authenticate: func(_ context.Context, _ *mms.AuthContext) (mms.AuthResult, error) {
			return mms.AuthResult{}, fmt.Errorf("all connections rejected for testing")
		},
	})

	ln, err := iso.Listen("127.0.0.1:0", iso.WithTLSConfig(&tls.Config{
		Certificates: []tls.Certificate{serverCert},
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go srv.ListenAndServe(ctx, ln)

	_, err = iso.Dial(ctx, ln.Addr().String(), iso.WithTLSConfig(&tls.Config{
		RootCAs:    serverPool,
		ServerName: "localhost",
	}))
	if err == nil {
		t.Fatal("expected error when authenticator rejects")
	}
	t.Logf("got expected error: %v", err)
}

func TestPlaintextRemoteAddr(t *testing.T) {
	var gotAuth *mms.AuthContext
	srv := mms.NewServer(mms.ServerOptions{
		Logger: slog.New(slog.NewTextHandler(tlsTestLogWriter{t}, &slog.HandlerOptions{Level: slog.LevelDebug})),
		MMS:    mms.ServerMMSOptions{MaxPDUSize: 65000},
		Authenticate: func(_ context.Context, auth *mms.AuthContext) (mms.AuthResult, error) {
			gotAuth = auth
			return mms.AuthResult{Accept: true}, nil
		},
	})
	srv.HandleIdentify(func(_ context.Context, _ mms.IdentifyRequest) (*mms.ServerIdentity, error) {
		return &mms.ServerIdentity{Vendor: "V"}, nil
	})

	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go srv.ListenAndServe(ctx, ln)

	client, err := iso.Dial(ctx, ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close(ctx)

	if _, err := client.Identify(ctx); err != nil {
		t.Fatal(err)
	}

	if gotAuth.RemoteAddr == nil {
		t.Fatal("RemoteAddr should be non-nil for ISO transport")
	}
	t.Logf("RemoteAddr = %v", gotAuth.RemoteAddr)

	if gotAuth.PeerCertificate != nil {
		t.Error("PeerCertificate should be nil for plaintext transport")
	}
	if gotAuth.Mechanism != mms.AuthMechanismNone {
		t.Errorf("mechanism = %v, want AuthMechanismNone", gotAuth.Mechanism)
	}
}

func TestPlaintextAndTLSCoexist(t *testing.T) {
	serverCert, serverPool := selfSignedCert(t, "localhost")
	srv := tlsTestServer(t)

	plainLn, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer plainLn.Close()

	tlsLn, err := iso.Listen("127.0.0.1:0", iso.WithTLSConfig(&tls.Config{
		Certificates: []tls.Certificate{serverCert},
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer tlsLn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go srv.ListenAndServe(ctx, plainLn)
	go srv.ListenAndServe(ctx, tlsLn)

	plainClient, err := iso.Dial(ctx, plainLn.Addr().String())
	if err != nil {
		t.Fatalf("plain dial: %v", err)
	}
	defer plainClient.Close(ctx)

	tlsClient, err := iso.Dial(ctx, tlsLn.Addr().String(), iso.WithTLSConfig(&tls.Config{
		RootCAs:    serverPool,
		ServerName: "localhost",
	}))
	if err != nil {
		t.Fatalf("tls dial: %v", err)
	}
	defer tlsClient.Close(ctx)

	plainID, err := plainClient.Identify(ctx)
	if err != nil {
		t.Fatalf("plain identify: %v", err)
	}
	tlsID, err := tlsClient.Identify(ctx)
	if err != nil {
		t.Fatalf("tls identify: %v", err)
	}

	if plainID.Vendor != tlsID.Vendor {
		t.Errorf("vendor mismatch: plain=%q tls=%q", plainID.Vendor, tlsID.Vendor)
	}
}
