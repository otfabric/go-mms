// SPDX-License-Identifier: MIT

package session

import (
	"bytes"
	"errors"
	"testing"
)

// SPDUs whose parameters exceed 254 bytes need the extended LI form
// (0xff + two octets). The encoders used to write a one-octet LI, so a
// CONNECT carrying more than ~230 bytes of user data (an AARQ with long
// authentication, for example) could not be parsed by anyone.
func TestLargeSPDURoundTrip(t *testing.T) {
	sel := []byte{0x00, 0x01}
	for _, n := range []int{0, 1, 200, 230, 231, 240, 254, 255, 300, 4096, 65000} {
		data := bytes.Repeat([]byte{0xa5}, n)
		encoders := map[string]func() ([]byte, error){
			"CONNECT": func() ([]byte, error) {
				// ISO 8327-1: CONNECT user data is bounded by 10 240 octets
				// (Extended User Data); TestConnectUserDataLimits covers it.
				d := data[:min(len(data), maxConnectExtUserData)]
				return EncodeConnect(ConnectParams{CallingSelector: sel, CalledSelector: sel}, d)
			},
			"ACCEPT":     func() ([]byte, error) { return EncodeAccept(ConnectParams{CalledSelector: sel}, data) },
			"FINISH":     func() ([]byte, error) { return EncodeFinish(data) },
			"DISCONNECT": func() ([]byte, error) { return EncodeDisconnect(data) },
			"ABORT":      func() ([]byte, error) { return EncodeAbort(data) },
		}
		for name, enc := range encoders {
			b, err := enc()
			if err != nil {
				t.Fatalf("%s n=%d: %v", name, n, err)
			}
			p, err := Parse(b)
			if err != nil {
				t.Fatalf("%s n=%d: Parse: %v", name, n, err)
			}
			if p.Type.String() != name {
				t.Fatalf("%s n=%d: type %s", name, n, p.Type)
			}
			want := data
			if name == "CONNECT" {
				want = data[:min(len(data), maxConnectExtUserData)]
			}
			if !bytes.Equal(p.UserData, want) {
				t.Fatalf("%s n=%d: user data %d bytes, want %d", name, n, len(p.UserData), n)
			}
		}
	}
}

func TestSPDUTooLarge(t *testing.T) {
	data := make([]byte, maxLI)
	if _, err := EncodeConnect(ConnectParams{}, data); err == nil {
		t.Error("CONNECT with 64 KiB user data encoded")
	}
	if _, err := EncodeAccept(ConnectParams{}, data); err == nil {
		t.Error("ACCEPT with 64 KiB user data encoded")
	}
	if _, err := EncodeFinish(data); err == nil {
		t.Error("FINISH with 64 KiB user data encoded")
	}
	if _, err := EncodeAbort(data); err == nil {
		t.Error("ABORT with 64 KiB user data encoded")
	}
}

func TestParseExtendedLITruncated(t *testing.T) {
	for _, in := range [][]byte{
		{SIConnect, 0xff},             // LI extended form cut short
		{SIConnect, 0xff, 0x01},       // one of two length octets
		{SIConnect, 0xff, 0x01, 0x00}, // LI 256, no parameters
		{SIFinish, 0xff, 0x00},
		{SIRefuse, 0xff, 0x00, 0x05},
	} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%x) accepted a truncated SPDU", in)
		}
	}
}

// FuzzSessionRoundTrip encodes arbitrary selectors and user data and
// requires Parse to return them unchanged.
func FuzzSessionRoundTrip(f *testing.F) {
	f.Add(byte(0), []byte{0, 1}, []byte{0, 1}, []byte{0x31, 0x00})
	f.Add(byte(1), []byte{}, []byte{0, 1}, bytes.Repeat([]byte{1}, 300))
	f.Add(byte(2), []byte{}, []byte{}, bytes.Repeat([]byte{2}, 255))
	f.Add(byte(4), []byte{}, []byte{}, []byte{})
	f.Fuzz(func(t *testing.T, kind byte, calling, called, data []byte) {
		var (
			b   []byte
			err error
		)
		p := ConnectParams{CallingSelector: calling, CalledSelector: called}
		switch kind % 5 {
		case 0:
			b, err = EncodeConnect(p, data)
		case 1:
			b, err = EncodeAccept(p, data)
		case 2:
			b, err = EncodeFinish(data)
			calling, called = nil, nil
		case 3:
			b, err = EncodeDisconnect(data)
			calling, called = nil, nil
		default:
			b, err = EncodeAbort(data)
			calling, called = nil, nil
		}
		if err != nil {
			// Selectors above 16 octets are refused (ISO 8327-1 PI 51/52).
			if errors.Is(err, ErrSelectorTooLong) && (len(calling) > 16 || len(called) > 16) {
				return
			}
			if len(calling)+len(called)+len(data) < maxLI-32 {
				t.Fatalf("encode failed for %d bytes: %v", len(calling)+len(called)+len(data), err)
			}
			return
		}
		got, err := Parse(b)
		if err != nil {
			t.Fatalf("Parse(encode) failed: %v", err)
		}
		if !bytes.Equal(got.UserData, data) || !bytes.Equal(got.CallingSelector, calling) || !bytes.Equal(got.CalledSelector, called) {
			t.Fatalf("round trip mismatch: %+v", got)
		}
	})
}
