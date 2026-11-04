// SPDX-License-Identifier: MIT

package serverconn

import (
	"context"
	"testing"

	"github.com/otfabric/go-mms/internal/asn1util"
	"github.com/otfabric/go-mms/internal/codec"
	"github.com/otfabric/go-mms/internal/pdu"
)

// invokeID is Unsigned32. A negative or wider value used to be converted
// to uint32, so it could alias the ID of another request; it must be
// rejected without reaching the handler.
func TestServe_ConfirmedRequestInvokeIDOutOfRange(t *testing.T) {
	identify := []byte{0x82, 0x00} // identify [2] IMPLICIT NULL
	for name, invokeID := range map[string][]byte{
		"negative":   {0x02, 0x01, 0xff},
		"2^32":       {0x02, 0x05, 0x01, 0x00, 0x00, 0x00, 0x00},
		"2^32+7":     {0x02, 0x05, 0x01, 0x00, 0x00, 0x00, 0x07},
		"min int64":  {0x02, 0x08, 0x80, 0, 0, 0, 0, 0, 0, 0},
		"over int64": {0x02, 0x09, 0x01, 0, 0, 0, 0, 0, 0, 0, 0},
	} {
		t.Run(name, func(t *testing.T) {
			called := false
			handler := func(_ context.Context, _ codec.InvokeID, _ int, _ []byte) (int, bool, []byte, error) {
				called = true
				return 0, false, nil, nil
			}
			req := asn1util.WrapConstructed(0, append(append([]byte(nil), invokeID...), identify...))
			tr := &scriptTransport{in: [][]byte{dataFrame(req), dataFrame(codec.MarshalConcludeRequest())}}
			c := New(tr, discardLogger(), handler, defaultOpts())
			if err := c.Serve(context.Background()); err != nil {
				t.Fatalf("Serve: %v", err)
			}
			if called {
				t.Fatal("handler called for an out-of-range invoke ID")
			}
			sent := tr.sentFrames()
			if len(sent) != 2 {
				t.Fatalf("sent %d frames, want 2", len(sent))
			}
			if kind, _ := decodeSentMMS(t, sent[0]); kind != pdu.PduReject {
				t.Fatalf("kind = %s, want Reject", kind)
			}
			expectConcludeResponse(t, sent[1])
		})
	}
}

// IDs from 2^31 carry a 0x00 pad octet and are valid Unsigned32 values.
func TestServe_ConfirmedRequestInvokeIDHigh(t *testing.T) {
	var got codec.InvokeID
	handler := func(_ context.Context, id codec.InvokeID, _ int, _ []byte) (int, bool, []byte, error) {
		got = id
		return 2, false, []byte{}, nil
	}
	req := asn1util.WrapConstructed(0, []byte{0x02, 0x05, 0x00, 0xff, 0xff, 0xff, 0xff, 0x82, 0x00})
	tr := &scriptTransport{in: [][]byte{dataFrame(req), dataFrame(codec.MarshalConcludeRequest())}}
	if err := New(tr, discardLogger(), handler, defaultOpts()).Serve(context.Background()); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if got != 0xffffffff {
		t.Fatalf("handler saw invoke ID %d, want %d", got, uint32(0xffffffff))
	}
}
