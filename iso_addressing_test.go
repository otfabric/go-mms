// SPDX-License-Identifier: MIT

package mms

import (
	"bytes"
	"context"
	"testing"
	"time"
)

// TestBuildISOParamsDefaults: a client sends the libiec61850 addressing
// (S-SEL 0001, P-SEL 00000001, AP-titles 1.1.1.999 / 1.1.1.999.1,
// AE-qualifier 12) unless configured otherwise; an empty value omits it.
func TestBuildISOParamsDefaults(t *testing.T) {
	c := &Client{}
	p := c.buildISOParams()
	for _, tc := range []struct {
		name      string
		got, want []byte
	}{
		{"calling S-SEL", p.CallingSessionSelector, []byte{0, 1}},
		{"called S-SEL", p.CalledSessionSelector, []byte{0, 1}},
		{"calling P-SEL", p.CallingPresentationSelector, []byte{0, 0, 0, 1}},
		{"called P-SEL", p.CalledPresentationSelector, []byte{0, 0, 0, 1}},
	} {
		if !bytes.Equal(tc.got, tc.want) {
			t.Errorf("%s = %x, want %x", tc.name, tc.got, tc.want)
		}
	}
	if !p.ACSE.CallingAPTitle.Equal(APTitle{1, 1, 1, 999}) || p.ACSE.CallingAEQualifier != 12 {
		t.Errorf("calling AP-title %v / %d, want 1.1.1.999 / 12", p.ACSE.CallingAPTitle, p.ACSE.CallingAEQualifier)
	}
	if !p.ACSE.CalledAPTitle.Equal(APTitle{1, 1, 1, 999, 1}) || p.ACSE.CalledAEQualifier != 12 {
		t.Errorf("called AP-title %v / %d, want 1.1.1.999.1 / 12", p.ACSE.CalledAPTitle, p.ACSE.CalledAEQualifier)
	}
	// The defaults are copies: changing them must not change the next call.
	p.CallingSessionSelector[1] = 9
	p.ACSE.CallingAPTitle[3] = 1
	if q := c.buildISOParams(); q.CallingSessionSelector[1] != 1 || q.ACSE.CallingAPTitle[3] != 999 {
		t.Fatal("defaults were modified through a returned slice")
	}
}

func TestBuildISOParamsConfigured(t *testing.T) {
	c := &Client{opts: DialOptions{ISO: ISOOptions{
		LocalSSelector:    []byte{},
		RemoteSSelector:   []byte{0x00, 0x02},
		LocalPSelector:    []byte{},
		RemotePSelector:   []byte{0x01},
		LocalAPTitle:      APTitle{},
		LocalAEQualifier:  5,
		RemoteAPTitle:     APTitle{1, 3, 9999, 23},
		RemoteAEQualifier: 23,
	}}}
	p := c.buildISOParams()
	if p.CallingSessionSelector != nil || p.CallingPresentationSelector != nil {
		t.Errorf("empty selectors not omitted: S=%x P=%x", p.CallingSessionSelector, p.CallingPresentationSelector)
	}
	if !bytes.Equal(p.CalledSessionSelector, []byte{0, 2}) || !bytes.Equal(p.CalledPresentationSelector, []byte{1}) {
		t.Errorf("called S=%x P=%x", p.CalledSessionSelector, p.CalledPresentationSelector)
	}
	if p.ACSE.CallingAPTitle != nil || p.ACSE.CallingAEQualifier != 0 {
		t.Errorf("empty calling AP-title not omitted: %v / %d", p.ACSE.CallingAPTitle, p.ACSE.CallingAEQualifier)
	}
	if !p.ACSE.CalledAPTitle.Equal(APTitle{1, 3, 9999, 23}) || p.ACSE.CalledAEQualifier != 23 {
		t.Errorf("called AP-title %v / %d", p.ACSE.CalledAPTitle, p.ACSE.CalledAEQualifier)
	}

	// A configured qualifier with the default AP-title is kept.
	c = &Client{opts: DialOptions{ISO: ISOOptions{LocalAEQualifier: 7}}}
	if q := c.buildISOParams(); q.ACSE.CallingAEQualifier != 7 || !q.ACSE.CallingAPTitle.Equal(APTitle{1, 1, 1, 999}) {
		t.Errorf("calling %v / %d, want 1.1.1.999 / 7", q.ACSE.CallingAPTitle, q.ACSE.CallingAEQualifier)
	}
}

// TestServerSeesDefaultCallingAPTitle: the default addressing goes on the
// wire and a go-mms server decodes it.
func TestServerSeesDefaultCallingAPTitle(t *testing.T) {
	var got *AuthContext
	srv := NewServer(ServerOptions{
		MMS: ServerMMSOptions{MaxPDUSize: 65000},
		Authenticate: func(_ context.Context, auth *AuthContext) (AuthResult, error) {
			got = auth
			return AuthResult{Accept: true}, nil
		},
	})
	client := connectClientServer(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	app := got.CallingApplication
	if app == nil || !app.APTitle.Equal(APTitle{1, 1, 1, 999}) || app.AEQualifier == nil || *app.AEQualifier != 12 {
		t.Fatalf("CallingApplication = %+v, want 1.1.1.999 / 12", app)
	}
}
