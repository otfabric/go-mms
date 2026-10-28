// SPDX-License-Identifier: MIT

package serverconn

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSendUnconfirmed_ContextEnds(t *testing.T) {
	tr := &chanTransport{send: make(chan []byte, 4), recv: make(chan []byte)}
	c := New(tr, nil, nil, MMSOptions{})

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.SendUnconfirmed(cancelled, []byte{1}); !errors.Is(err, context.Canceled) {
		t.Errorf("pre-cancelled SendUnconfirmed = %v", err)
	}

	// A write still waiting for sendMu when ctx ends is abandoned and
	// never reaches the transport.
	c.sendMu.Lock()
	ctx, cancel2 := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel2()
	if err := c.SendUnconfirmed(ctx, []byte{2}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("blocked SendUnconfirmed = %v", err)
	}
	c.sendMu.Unlock()
	time.Sleep(20 * time.Millisecond)
	select {
	case d := <-tr.send:
		t.Errorf("abandoned PDU was sent: %x", d)
	default:
	}
}
