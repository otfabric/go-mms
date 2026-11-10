// SPDX-License-Identifier: MIT

package session

import (
	"bytes"
	"errors"
	"testing"
)

// ISO 8327-1 8.3.4: a REFUSE carries Transport Disconnect (PI 17) and
// Reason Code (PI 50). With version 2 the SS-user data follows reason 2.
func TestSpec_8327_RefuseEncoding(t *testing.T) {
	b, err := EncodeRefuse(ReasonUserData, []byte{0x30, 0x00})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x0c, 0x08, 0x11, 0x01, 0x01, 0x32, 0x03, 0x02, 0x30, 0x00}
	if !bytes.Equal(b, want) {
		t.Fatalf("REFUSE = %x, want %x", b, want)
	}
	p, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if p.Type != SpduRefuse || !p.HasReason || p.Reason != ReasonUserData || !bytes.Equal(p.UserData, []byte{0x30, 0x00}) {
		t.Fatalf("parsed %+v", p)
	}

	b, err = EncodeRefuse(ReasonVersionNotSupported, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err = Parse(b)
	if err != nil || p.Reason != ReasonVersionNotSupported || p.UserData != nil {
		t.Fatalf("parsed %+v err %v", p, err)
	}
	if _, err := EncodeRefuse(ReasonUserCongestion, []byte{1}); err == nil {
		t.Fatal("user data accepted for reason 1")
	}
	if _, err := Parse([]byte{0x0c, 0x02, 0x32, 0x00}); err == nil {
		t.Fatal("empty Reason Code accepted")
	}
	if !ReasonTransient(ReasonUserCongestion) || !ReasonTransient(ReasonSPMCongestion) || ReasonTransient(ReasonUserData) {
		t.Fatal("ReasonTransient classification")
	}
}

// ISO 8327-1 8.3.1: the Version Number and Session User Requirements of a
// CONNECT/ACCEPT are decoded so they can be negotiated.
func TestSpec_8327_VersionAndRequirementsParsed(t *testing.T) {
	b, err := EncodeAccept(ConnectParams{Version: Version1, Requirements: 0x0001}, []byte{1})
	if err != nil {
		t.Fatal(err)
	}
	p, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != Version1 || !p.HasRequirements || p.Requirements != 0x0001 {
		t.Fatalf("parsed version %d reqs %v/%04x", p.Version, p.HasRequirements, p.Requirements)
	}
	// Defaults: version 2, duplex.
	b, _ = EncodeConnect(ConnectParams{}, []byte{1})
	p, _ = Parse(b)
	if p.Version != Version2 || p.Requirements != FUDuplex {
		t.Fatalf("defaults version %d reqs %04x", p.Version, p.Requirements)
	}
	// PI 22 outside PGI 5 is read as well.
	p, err = Parse([]byte{0x0d, 0x06, 0x16, 0x01, 0x03, 0xc1, 0x01, 0xaa})
	if err != nil || p.Version != 0x03 {
		t.Fatalf("top-level PI 22: %+v %v", p, err)
	}
	if _, err := Parse([]byte{0x0d, 0x03, 0x14, 0x01, 0x02}); err == nil {
		t.Fatal("1-octet Session User Requirements accepted")
	}
	if _, err := Parse([]byte{0x0d, 0x06, 0x05, 0x04, 0x16, 0x02, 0x02, 0x02}); err == nil {
		t.Fatal("2-octet Version Number accepted")
	}
}

func TestSpec_8327_NegotiateVersion(t *testing.T) {
	for _, tc := range []struct{ in, want byte }{
		{0, Version1}, {Version1, Version1}, {Version2, Version2}, {Version1 | Version2, Version2}, {0x04, 0},
	} {
		if got := NegotiateVersion(tc.in); got != tc.want {
			t.Errorf("NegotiateVersion(%02x)=%02x want %02x", tc.in, got, tc.want)
		}
	}
}

// ISO 8327-1 8.3.1.x (MMS-STK-10): CONNECT user data above 512 octets is
// sent as Extended User Data (PGI 194), up to 10 240 octets, and PGI 194
// is read back as the user data.
func TestSpec_8327_ExtendedUserData(t *testing.T) {
	data := bytes.Repeat([]byte{0xa5}, 600)
	b, err := EncodeConnect(ConnectParams{}, data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte{0xc2, 0xff, 0x02, 0x58}) {
		t.Fatalf("no PGI 194 header in CONNECT")
	}
	p, err := Parse(b)
	if err != nil || !bytes.Equal(p.UserData, data) {
		t.Fatalf("round trip: %v", err)
	}
	if _, err := EncodeConnect(ConnectParams{}, make([]byte, 10241)); err == nil {
		t.Fatal("10241 octets of CONNECT user data encoded")
	}
	if _, err := EncodeConnect(ConnectParams{Version: Version1}, make([]byte, 513)); err == nil {
		t.Fatal("extended user data with version 1 only encoded")
	}
	// 512 octets still go in PGI 193.
	b, _ = EncodeConnect(ConnectParams{}, make([]byte, 512))
	if bytes.Contains(b[:16], []byte{0xc2}) {
		t.Fatal("512 octets sent as extended user data")
	}
	// ACCEPT never uses PGI 194.
	b, _ = EncodeAccept(ConnectParams{}, data)
	if bytes.Contains(b, []byte{0xc2, 0xff}) {
		t.Fatal("ACCEPT used PGI 194")
	}
	// Both 193 and 194 present is malformed.
	if _, err := Parse([]byte{0x0d, 0x06, 0xc1, 0x01, 0x01, 0xc2, 0x01, 0x02}); err == nil {
		t.Fatal("CONNECT with both user data PGIs accepted")
	}
}

// MMS-STK-18: session selectors are at most 16 octets.
func TestSpec_8327_SelectorLength(t *testing.T) {
	long := make([]byte, 17)
	if _, err := EncodeConnect(ConnectParams{CallingSelector: long}, nil); !errors.Is(err, ErrSelectorTooLong) {
		t.Fatalf("calling: %v", err)
	}
	if _, err := EncodeConnect(ConnectParams{CalledSelector: long}, nil); !errors.Is(err, ErrSelectorTooLong) {
		t.Fatalf("called: %v", err)
	}
	if _, err := EncodeConnect(ConnectParams{CalledSelector: long[:16]}, nil); err != nil {
		t.Fatalf("16 octets: %v", err)
	}
}

// MMS-STK-22: ABORT ACCEPT (SI 26) is a terminal SPDU that is parsed.
func TestSpec_8327_AbortAccept(t *testing.T) {
	p, err := Parse([]byte{0x1a, 0x00})
	if err != nil || p.Type != SpduAbortAccept || p.Type.String() != "ABORT-ACCEPT" {
		t.Fatalf("AA: %+v %v", p, err)
	}
}
