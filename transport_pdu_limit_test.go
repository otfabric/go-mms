// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/codec"
)

// limitedTransport carries MMS PDUs of up to limit octets, as a
// transport/iso transport says through MaxMMSPDU.
type limitedTransport struct {
	Transport
	limit int
}

func (t limitedTransport) MaxMMSPDU() int { return t.limit }

// Neither side proposes or supports a PDU size larger than its transport
// carries; a transport that says nothing, or a limit that is not positive,
// leaves the configured size.
func TestTransportPDULimit(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		clientLimit, serverLimit int // 0: the transport says nothing
		want                     int
	}{
		{"no limit", 0, 0, 65000},
		{"client transport", 8000, 0, 8000},
		{"server transport", 0, 4000, 4000},
		{"both", 3000, 4000, 3000},
		{"above the configured size", 70000, 70000, 65000},
		{"not positive", -1, -1, 65000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clientEnd, serverEnd := loopbackPair()
			if tc.clientLimit != 0 {
				clientEnd = limitedTransport{clientEnd, tc.clientLimit}
			}
			if tc.serverLimit != 0 {
				serverEnd = limitedTransport{serverEnd, tc.serverLimit}
			}
			srv := NewServer(ServerOptions{})
			served := make(chan error, 1)
			go func() { served <- srv.Serve(context.Background(), serverEnd) }()
			c, err := NewClient(context.Background(), clientEnd, DialOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = c.Abort(context.Background()); _ = waitServe(t, served) }()
			if got := c.Negotiated().MaxPDUSize; got != tc.want {
				t.Errorf("negotiated PDU size %d, want %d", got, tc.want)
			}
		})
	}
}

// A client sends no request larger than its transport carries, also when
// the server's localDetailCalled is larger: the request is refused before
// anything is sent.
func TestTransportPDULimitSend(t *testing.T) {
	mt := newMockTransport()
	t.Cleanup(mt.shutdown)
	payload, err := codec.MarshalMmsPduBareSequence(asn1util.TagInitiateResponse, goodInitResp()) // localDetailCalled 65000
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		if _, err := mt.receiveFromClient(context.Background()); err == nil {
			mt.sendToClient(acceptWithMMS(payload))
		}
	}()
	c, err := NewClient(context.Background(), limitedTransport{mt, 2000}, DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Abort(context.Background()) }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = c.Write(ctx, WriteRequest{DomainID: "d", ItemID: "x", Value: NewOctetString(make([]byte, 3000))})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("Write of 3000 octets over a transport that carries 2000: %v, want ErrInvalidArgument", err)
	}
}
