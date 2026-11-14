// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/acse"
	"github.com/otfabric/go-mms/internal/berutil"
	"github.com/otfabric/go-mms/internal/pdu"
	"github.com/otfabric/go-mms/internal/serverconn"
)

// defineNVLBody returns the service body of a DefineNamedVariableList
// request for list with the given members.
func defineNVLBody(t *testing.T, list pdu.ObjectNameWire, members ...pdu.ObjectNameWire) []byte {
	t.Helper()
	vars := make([]pdu.VariableSpecWire, len(members))
	for i, m := range members {
		vars[i] = pdu.VariableSpecWire{Name: m}
	}
	p, err := pdu.MarshalDefineNamedVarListRequest(1, list, vars)
	if err != nil {
		t.Fatal(err)
	}
	return extractConfirmedBody(t, p)
}

func deleteNVLBody(t *testing.T, names ...pdu.ObjectNameWire) []byte {
	t.Helper()
	p, err := pdu.MarshalDeleteNamedVarListRequest(1, names)
	if err != nil {
		t.Fatal(err)
	}
	return extractConfirmedBody(t, p)
}

// deleteScopeBody returns the service body of a DeleteNamedVariableList
// request with scopeOfDelete scope (ISO 9506-2 clause 11.6.1).
func deleteScopeBody(scope int, domain string) []byte {
	b := berutil.EncodeTLV(0x80, berutil.EncodeInt(scope))
	if domain != "" {
		b = append(b, berutil.EncodeTLV(0x82, []byte(domain))...)
	}
	return b
}

func assertServiceError(t *testing.T, err error, class ErrorClass, code int) {
	t.Helper()
	se, ok := errors.AsType[*serverconn.ServiceError](err)
	if !ok || se.ErrorClass != int(class) || se.ErrorCode != code {
		t.Fatalf("got %v, want ServiceError class %d code %d", err, class, code)
	}
}

var (
	vmdMember = pdu.ObjectNameWire{Scope: pdu.ScopeVMD, ItemID: "v"}
	aaMember  = pdu.ObjectNameWire{Scope: pdu.ScopeAssociation, ItemID: "v"}
)

func TestServerLimitsDefaults(t *testing.T) {
	got := NewServer(ServerOptions{}).limits
	want := serverLimits{peerNVLs: 4096, associationNVLs: 256, nvlMembers: 1000, openFiles: 32}
	if got != want {
		t.Fatalf("limits = %+v, want %+v", got, want)
	}
	got = NewServer(ServerOptions{MaxPeerNVLs: 7, MaxAssociationNVLs: -1, MaxNVLMembers: 3, MaxOpenFiles: -2}).limits
	want = serverLimits{peerNVLs: 7, associationNVLs: -1, nvlMembers: 3, openFiles: -2}
	if got != want {
		t.Fatalf("limits = %+v, want %+v", got, want)
	}
}

func TestDefineNVLAssociationLimit(t *testing.T) {
	s := NewServer(ServerOptions{MaxAssociationNVLs: 3})
	sc := &ServerConn{}
	ctx := context.WithValue(context.Background(), serverConnCtxKey{}, sc)
	aa := func(i int) pdu.ObjectNameWire {
		return pdu.ObjectNameWire{Scope: pdu.ScopeAssociation, ItemID: fmt.Sprintf("L%d", i)}
	}
	for i := range 3 {
		if _, _, _, err := s.handleDefineNVL(ctx, defineNVLBody(t, aa(i), aaMember)); err != nil {
			t.Fatalf("define %d: %v", i, err)
		}
	}
	_, _, _, err := s.handleDefineNVL(ctx, defineNVLBody(t, aa(3), aaMember))
	assertServiceError(t, err, ErrorClassResource, svcErrCapabilityUnavailable)
	if n := len(sc.assocNVLs); n != 3 {
		t.Fatalf("association lists = %d, want 3", n)
	}
	// A duplicate at the limit is still answered as before.
	if _, _, _, err := s.handleDefineNVL(ctx, defineNVLBody(t, aa(0), aaMember)); !errors.Is(err, errObjectNonExistent) {
		t.Fatalf("duplicate: %v", err)
	}

	// Deleting one list by name frees one slot.
	if _, _, _, err := s.handleDeleteNVL(ctx, deleteNVLBody(t, aa(1))); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.handleDefineNVL(ctx, defineNVLBody(t, aa(3), aaMember)); err != nil {
		t.Fatalf("define after delete: %v", err)
	}
	if _, _, _, err := s.handleDefineNVL(ctx, defineNVLBody(t, aa(4), aaMember)); err == nil {
		t.Fatal("define over the limit after delete accepted")
	}

	// Deleting all association lists frees every slot.
	if _, _, _, err := s.handleDeleteNVL(ctx, deleteScopeBody(1, "")); err != nil {
		t.Fatal(err)
	}
	for i := 10; i < 13; i++ {
		if _, _, _, err := s.handleDefineNVL(ctx, defineNVLBody(t, aa(i), aaMember)); err != nil {
			t.Fatalf("define %d after delete all: %v", i, err)
		}
	}

	// The limit is per association.
	ctx2 := context.WithValue(context.Background(), serverConnCtxKey{}, &ServerConn{})
	if _, _, _, err := s.handleDefineNVL(ctx2, defineNVLBody(t, aa(0), aaMember)); err != nil {
		t.Fatalf("second association: %v", err)
	}
}

func TestDefineNVLPeerLimit(t *testing.T) {
	s := NewServer(ServerOptions{MaxPeerNVLs: 3})
	if err := s.RegisterDomain("d"); err != nil {
		t.Fatal(err)
	}
	// Lists the application registers do not count, deletable or not.
	for _, del := range []bool{false, true} {
		if err := s.RegisterNamedVariableList(NamedVariableList{
			Name:      ObjectName{Scope: ObjectScopeVMD, ItemID: ItemID(fmt.Sprintf("app%v", del))},
			Deletable: del,
			Variables: []VariableSpec{{Name: ObjectName{Scope: ObjectScopeVMD, ItemID: "v"}}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	dom := func(i int) pdu.ObjectNameWire {
		return pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: fmt.Sprintf("D%d", i)}
	}
	vmd := func(i int) pdu.ObjectNameWire {
		return pdu.ObjectNameWire{Scope: pdu.ScopeVMD, ItemID: fmt.Sprintf("V%d", i)}
	}
	for _, n := range []pdu.ObjectNameWire{dom(0), dom(1), vmd(0)} {
		if _, _, _, err := s.handleDefineNVL(ctx, defineNVLBody(t, n, vmdMember)); err != nil {
			t.Fatalf("define %v: %v", n, err)
		}
	}
	for _, n := range []pdu.ObjectNameWire{dom(2), vmd(1)} {
		_, _, _, err := s.handleDefineNVL(ctx, defineNVLBody(t, n, vmdMember))
		assertServiceError(t, err, ErrorClassResource, svcErrCapabilityUnavailable)
	}
	if n := s.registry.PeerNVLCount(); n != 3 {
		t.Fatalf("peer lists = %d, want 3", n)
	}
	if _, ok := s.registry.LookupNVL(pdu.ScopeDomain, "d", "D2"); ok {
		t.Fatal("refused list stored")
	}

	// Deleting an application list frees no peer slot.
	if _, _, _, err := s.handleDeleteNVL(ctx, deleteNVLBody(t, pdu.ObjectNameWire{Scope: pdu.ScopeVMD, ItemID: "apptrue"})); err != nil {
		t.Fatal(err)
	}
	if n := s.registry.PeerNVLCount(); n != 3 {
		t.Fatalf("peer lists after app delete = %d, want 3", n)
	}

	// Delete by name.
	if _, _, _, err := s.handleDeleteNVL(ctx, deleteNVLBody(t, dom(0))); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.handleDefineNVL(ctx, defineNVLBody(t, dom(2), vmdMember)); err != nil {
		t.Fatalf("define after delete: %v", err)
	}

	// Delete all domain lists, then all VMD lists.
	if _, _, _, err := s.handleDeleteNVL(ctx, deleteScopeBody(2, "d")); err != nil {
		t.Fatal(err)
	}
	if n := s.registry.PeerNVLCount(); n != 1 {
		t.Fatalf("peer lists after domain delete = %d, want 1", n)
	}
	if _, _, _, err := s.handleDeleteNVL(ctx, deleteScopeBody(3, "")); err != nil {
		t.Fatal(err)
	}
	if n := s.registry.PeerNVLCount(); n != 0 {
		t.Fatalf("peer lists after VMD delete = %d, want 0", n)
	}
	// The non-deletable application list stays.
	if _, ok := s.registry.LookupNVL(pdu.ScopeVMD, "", "appfalse"); !ok {
		t.Fatal("non-deletable application list removed")
	}
	for i := 10; i < 13; i++ {
		if _, _, _, err := s.handleDefineNVL(ctx, defineNVLBody(t, vmd(i), vmdMember)); err != nil {
			t.Fatalf("define %d after delete all: %v", i, err)
		}
	}
}

func TestDefineNVLIdentifiersAndMembers(t *testing.T) {
	s := NewServer(ServerOptions{MaxNVLMembers: 2})
	if err := s.RegisterDomain("d"); err != nil {
		t.Fatal(err)
	}
	sc := &ServerConn{}
	ctx := context.WithValue(context.Background(), serverConnCtxKey{}, sc)
	long := strings.Repeat("x", MaxIdentifierLength+1)
	max := strings.Repeat("y", MaxIdentifierLength)

	cases := []struct {
		name    string
		list    pdu.ObjectNameWire
		members []pdu.ObjectNameWire
		want    error
	}{
		{"64-char list", pdu.ObjectNameWire{Scope: pdu.ScopeVMD, ItemID: max}, []pdu.ObjectNameWire{{Scope: pdu.ScopeVMD, ItemID: max}}, nil},
		{"65-char VMD list", pdu.ObjectNameWire{Scope: pdu.ScopeVMD, ItemID: long}, []pdu.ObjectNameWire{vmdMember}, errInvalidRequest},
		{"65-char domain list", pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: "d", ItemID: long}, []pdu.ObjectNameWire{vmdMember}, errInvalidRequest},
		{"65-char list domain", pdu.ObjectNameWire{Scope: pdu.ScopeDomain, DomainID: long, ItemID: "L"}, []pdu.ObjectNameWire{vmdMember}, errInvalidRequest},
		{"65-char association list", pdu.ObjectNameWire{Scope: pdu.ScopeAssociation, ItemID: long}, []pdu.ObjectNameWire{aaMember}, errInvalidRequest},
		{"empty list", pdu.ObjectNameWire{Scope: pdu.ScopeVMD, ItemID: "E"}, nil, errInvalidRequest},
		{"65-char member", pdu.ObjectNameWire{Scope: pdu.ScopeVMD, ItemID: "M1"}, []pdu.ObjectNameWire{{Scope: pdu.ScopeVMD, ItemID: long}}, errDefinitionObjectUndefined},
		{"empty member", pdu.ObjectNameWire{Scope: pdu.ScopeVMD, ItemID: "M2"}, []pdu.ObjectNameWire{vmdMember, {Scope: pdu.ScopeAssociation}}, errDefinitionObjectUndefined},
		{"65-char member domain", pdu.ObjectNameWire{Scope: pdu.ScopeVMD, ItemID: "M3"}, []pdu.ObjectNameWire{{Scope: pdu.ScopeDomain, DomainID: long, ItemID: "v"}}, errDefinitionObjectUndefined},
		{"too many members", pdu.ObjectNameWire{Scope: pdu.ScopeVMD, ItemID: "M4"}, []pdu.ObjectNameWire{vmdMember, vmdMember, vmdMember}, errCapabilityUnavailable},
		{"member limit", pdu.ObjectNameWire{Scope: pdu.ScopeVMD, ItemID: "M5"}, []pdu.ObjectNameWire{vmdMember, vmdMember}, nil},
		{"non-identifier characters", pdu.ObjectNameWire{Scope: pdu.ScopeVMD, ItemID: "a/b c"}, []pdu.ObjectNameWire{{Scope: pdu.ScopeVMD, ItemID: "x.y"}}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, _, err := s.handleDefineNVL(ctx, defineNVLBody(t, c.list, c.members...))
			if err != c.want { //nolint:errorlint // sentinel identity
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if c.want != nil && s.registry.PeerNVLCount()+len(sc.assocNVLs) > 3 {
				t.Fatal("refused list stored")
			}
		})
	}
	if n := s.registry.PeerNVLCount(); n != 3 {
		t.Fatalf("peer lists = %d, want 3", n)
	}
}

func TestDefineNVLDisabled(t *testing.T) {
	ctx := context.WithValue(context.Background(), serverConnCtxKey{}, &ServerConn{})
	vmdList := pdu.ObjectNameWire{Scope: pdu.ScopeVMD, ItemID: "L"}
	aaList := pdu.ObjectNameWire{Scope: pdu.ScopeAssociation, ItemID: "L"}

	s := NewServer(ServerOptions{MaxPeerNVLs: -1})
	_, _, _, err := s.handleDefineNVL(ctx, defineNVLBody(t, vmdList, vmdMember))
	assertServiceError(t, err, ErrorClassResource, svcErrCapabilityUnavailable)
	if _, _, _, err := s.handleDefineNVL(ctx, defineNVLBody(t, aaList, aaMember)); err != nil {
		t.Fatalf("association list with MaxPeerNVLs < 0: %v", err)
	}

	s = NewServer(ServerOptions{MaxAssociationNVLs: -1})
	_, _, _, err = s.handleDefineNVL(ctx, defineNVLBody(t, aaList, aaMember))
	assertServiceError(t, err, ErrorClassResource, svcErrCapabilityUnavailable)
	if _, _, _, err := s.handleDefineNVL(ctx, defineNVLBody(t, vmdList, vmdMember)); err != nil {
		t.Fatalf("VMD list with MaxAssociationNVLs < 0: %v", err)
	}

	s = NewServer(ServerOptions{MaxNVLMembers: -1})
	for _, l := range []pdu.ObjectNameWire{vmdList, aaList} {
		_, _, _, err = s.handleDefineNVL(ctx, defineNVLBody(t, l, vmdMember))
		assertServiceError(t, err, ErrorClassResource, svcErrCapabilityUnavailable)
	}
}

// countingFileProvider counts Open calls and open handles.
type countingFileProvider struct {
	*memFileProvider
	opens atomic.Int32
	open  atomic.Int32
}

func (p *countingFileProvider) Open(ctx context.Context, path string) (FileHandle, FileAttributes, error) {
	p.opens.Add(1)
	h, a, err := p.memFileProvider.Open(ctx, path)
	if err == nil {
		p.open.Add(1)
	}
	return h, a, err
}

func (p *countingFileProvider) Close(ctx context.Context, h FileHandle) error {
	p.open.Add(-1)
	return p.memFileProvider.Close(ctx, h)
}

func TestFileOpenLimit(t *testing.T) {
	fp := &countingFileProvider{memFileProvider: newMemFileProvider()}
	fp.addFile("a.txt", []byte("hi"))
	s := NewServer(ServerOptions{FileProvider: fp, MaxOpenFiles: 2})
	sc := &ServerConn{frsmTable: newFRSMTable()}
	ctx := context.WithValue(context.Background(), serverConnCtxKey{}, sc)
	openPDU, err := pdu.MarshalFileOpenRequest(1, "a.txt", 0)
	if err != nil {
		t.Fatal(err)
	}
	openBody := extractConfirmedBody(t, openPDU)

	for i := range 2 {
		if _, _, _, err := s.handleFileOpen(ctx, openBody); err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
	}
	_, _, _, err = s.handleFileOpen(ctx, openBody)
	assertServiceError(t, err, ErrorClassResource, svcErrCapabilityUnavailable)
	if n := fp.opens.Load(); n != 2 {
		t.Fatalf("provider Open calls = %d, want 2 (refused before Open)", n)
	}
	if n := sc.frsmTable.count(); n != 2 {
		t.Fatalf("open FRSMs = %d, want 2", n)
	}

	// FileClose frees a slot.
	closePDU, err := pdu.MarshalFileCloseRequest(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.handleFileClose(ctx, extractConfirmedBody(t, closePDU)); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.handleFileOpen(ctx, openBody); err != nil {
		t.Fatalf("open after close: %v", err)
	}
	// The limit is per association.
	ctx2 := context.WithValue(context.Background(), serverConnCtxKey{}, &ServerConn{frsmTable: newFRSMTable()})
	if _, _, _, err := s.handleFileOpen(ctx2, openBody); err != nil {
		t.Fatalf("second association: %v", err)
	}
	sc.frsmTable.closeAll(s.logger, fp)

	// A negative limit refuses every FileOpen without calling Open.
	fp2 := &countingFileProvider{memFileProvider: newMemFileProvider()}
	fp2.addFile("a.txt", nil)
	s = NewServer(ServerOptions{FileProvider: fp2, MaxOpenFiles: -1})
	ctx3 := context.WithValue(context.Background(), serverConnCtxKey{}, &ServerConn{frsmTable: newFRSMTable()})
	_, _, _, err = s.handleFileOpen(ctx3, openBody)
	assertServiceError(t, err, ErrorClassResource, svcErrCapabilityUnavailable)
	if fp2.opens.Load() != 0 {
		t.Fatal("Open called with MaxOpenFiles < 0")
	}
}

// --- TLS peer certificate verification ---

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
}

func newTestKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()
	key := newTestKey(t)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &testCA{cert: cert, key: key, pool: pool}
}

// issue returns a leaf certificate for cn signed by ca, or self-signed
// when ca is nil.
func issue(t *testing.T, ca *testCA, cn string, usage x509.ExtKeyUsage) tls.Certificate {
	t.Helper()
	key := newTestKey(t)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     []string{cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
	}
	parent, signer := tmpl, key
	if ca != nil {
		parent, signer = ca.cert, ca.key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, signer)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// handshakeState runs a TLS handshake over a pipe and returns the
// server's connection state.
func handshakeState(t *testing.T, serverCfg, clientCfg *tls.Config) *tls.ConnectionState {
	t.Helper()
	c1, c2 := net.Pipe()
	defer func() { _ = c1.Close() }()
	defer func() { _ = c2.Close() }()
	deadline := time.Now().Add(10 * time.Second)
	_ = c1.SetDeadline(deadline)
	_ = c2.SetDeadline(deadline)
	srv := tls.Server(c1, serverCfg)
	cli := tls.Client(c2, clientCfg)
	errc := make(chan error, 1)
	go func() { errc <- cli.Handshake() }()
	if err := srv.Handshake(); err != nil {
		t.Fatalf("server handshake: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("client handshake: %v", err)
	}
	st := srv.ConnectionState()
	return &st
}

// sendCert makes the client present cert even when it does not chain to
// a CA the server lists as acceptable.
func sendCert(cert tls.Certificate) func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
	return func(*tls.CertificateRequestInfo) (*tls.Certificate, error) { return &cert, nil }
}

type stateTLSTransport struct {
	chanTransport
	state *tls.ConnectionState
}

func (s *stateTLSTransport) TLSConnectionState() *tls.ConnectionState { return s.state }

func TestAuthContextPeerCertificateVerified(t *testing.T) {
	ca := newTestCA(t)
	serverCert := issue(t, ca, "server.test", x509.ExtKeyUsageServerAuth)
	trusted := issue(t, ca, "operator", x509.ExtKeyUsageClientAuth)
	selfSigned := issue(t, nil, "operator", x509.ExtKeyUsageClientAuth)

	cases := []struct {
		name       string
		clientAuth tls.ClientAuthType
		clientCert tls.Certificate
		verified   bool
	}{
		{"RequireAndVerify trusted", tls.RequireAndVerifyClientCert, trusted, true},
		{"VerifyIfGiven trusted", tls.VerifyClientCertIfGiven, trusted, true},
		{"RequireAny self-signed", tls.RequireAnyClientCert, selfSigned, false},
		{"Request self-signed", tls.RequestClientCert, selfSigned, false},
		{"RequireAny trusted", tls.RequireAnyClientCert, trusted, false},
	}
	s := NewServer(ServerOptions{})
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			state := handshakeState(t,
				&tls.Config{Certificates: []tls.Certificate{serverCert}, ClientAuth: c.clientAuth, ClientCAs: ca.pool, MinVersion: tls.VersionTLS12},
				&tls.Config{GetClientCertificate: sendCert(c.clientCert), RootCAs: ca.pool, ServerName: "server.test", MinVersion: tls.VersionTLS12},
			)
			ac := s.buildAuthContext(&stateTLSTransport{state: state}, acse.AuthInfo{})
			if ac.PeerCertificate == nil || ac.PeerCertificate.Subject.CommonName != "operator" {
				t.Fatalf("PeerCertificate = %v", ac.PeerCertificate)
			}
			// The mechanism does not change with verification.
			if ac.Mechanism != AuthMechanismTLSCertificate {
				t.Fatalf("Mechanism = %v", ac.Mechanism)
			}
			if ac.PeerCertificateVerified != c.verified {
				t.Fatalf("PeerCertificateVerified = %v, want %v", ac.PeerCertificateVerified, c.verified)
			}
		})
	}

	// No client certificate: nothing verified.
	state := handshakeState(t,
		&tls.Config{Certificates: []tls.Certificate{serverCert}, ClientAuth: tls.VerifyClientCertIfGiven, ClientCAs: ca.pool, MinVersion: tls.VersionTLS12},
		&tls.Config{RootCAs: ca.pool, ServerName: "server.test", MinVersion: tls.VersionTLS12},
	)
	ac := s.buildAuthContext(&stateTLSTransport{state: state}, acse.AuthInfo{})
	if ac.PeerCertificate != nil || ac.PeerCertificateVerified || ac.Mechanism != AuthMechanismNone {
		t.Fatalf("no client cert: %+v", ac)
	}
}
