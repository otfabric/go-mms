// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/codec"
)

// A Conclude-Response or Conclude-ErrorPDU answers the Conclude-Request of
// Close (ISO 9506-2 clause 8.4). Without one it ends the association: Done
// closes, Err matches ErrClosed and ErrProtocol, the transport is closed,
// and a later request is not written.
func TestConcludeWithoutRequestEndsAssociation(t *testing.T) {
	for name, concludePDU := range map[string][]byte{
		"Conclude-Response": codec.MarshalConcludeResponse(),
		"Conclude-ErrorPDU": codec.MarshalConcludeError(9, 1), // conclude, further-communication-required
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			mt := newMockTransport()
			srv := newMockServer(t, mt)
			go srv.handleAssociation(ctx)
			c, err := NewClient(ctx, mt, defaultDialOptions())
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			t.Cleanup(func() { _ = c.Abort(ctx) })

			srv.sendDataResponse(ctx, concludePDU)
			select {
			case <-c.Done():
			case <-time.After(5 * time.Second):
				t.Fatal("association still open")
			}
			if err := c.Err(); !errors.Is(err, ErrClosed) || !errors.Is(err, ErrProtocol) {
				t.Errorf("Err() = %v, want ErrClosed and ErrProtocol", err)
			}
			mt.mu.Lock()
			closed := mt.closed
			mt.mu.Unlock()
			if !closed {
				t.Error("transport still open after the association ended")
			}

			rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if _, err := c.Identify(rctx); !errors.Is(err, ErrClosed) {
				t.Errorf("Identify = %v, want ErrClosed", err)
			}
			select {
			case d := <-mt.toServer:
				t.Errorf("request written after the association ended: % x", d)
			default:
			}
		})
	}
}
