// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"sync"
	"testing"
	"time"
)

// SetVariableRead may replace a read callback while a client reads the
// variable: every Read answers with the value of one of the callbacks.
// Run with -race.
func TestSetVariableReadWhileReading(t *testing.T) {
	srv := reentryServer(t, func(context.Context) (*Value, error) { return NewInteger(0), nil })
	c := reentryClient(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := int64(1); ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			v := NewInteger(i % 2)
			if err := srv.SetVariableRead("LD", "v", func(context.Context) (*Value, error) { return v, nil }); err != nil {
				t.Error(err)
				return
			}
		}
	})
	for range 200 {
		res, err := c.Read(ctx, ReadRequest{DomainID: "LD", ItemID: "v"})
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if i, ok := res.Value.Int64(); !ok || (i != 0 && i != 1) {
			t.Fatalf("Read = %v", res.Value)
		}
	}
	close(stop)
	wg.Wait()
}
