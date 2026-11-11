// SPDX-License-Identifier: MIT

package invoke

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/codec"
)

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestSpec_9506_2_8_2_ReserveWaitsForSlot: at the negotiated
// max-services-outstanding a Reserve waits, and waiters get freed slots
// in the order they arrived.
func TestSpec_9506_2_8_2_ReserveWaitsForSlot(t *testing.T) {
	tr := NewTracker(2)
	if tr.Limit() != 2 {
		t.Fatalf("Limit = %d", tr.Limit())
	}
	ctx := context.Background()
	for id := codec.InvokeID(1); id <= 2; id++ {
		if _, err := tr.Reserve(ctx, id); err != nil {
			t.Fatalf("Reserve %d: %v", id, err)
		}
	}
	order := make(chan codec.InvokeID, 3)
	var wg sync.WaitGroup
	for id := codec.InvokeID(3); id <= 5; id++ {
		wg.Go(func() {
			if _, err := tr.Reserve(ctx, id); err != nil {
				t.Errorf("Reserve %d: %v", id, err)
				return
			}
			order <- id
		})
		waitFor(t, "waiter", func() bool { return tr.WaitingCount() == int(id-2) })
	}
	if _, _, err := tr.AllocateWithID(9); !errors.Is(err, ErrLimit) {
		t.Fatalf("AllocateWithID with waiters = %v, want ErrLimit", err)
	}
	if _, _, err := tr.Allocate(); !errors.Is(err, ErrLimit) {
		t.Fatalf("Allocate at the limit = %v, want ErrLimit", err)
	}
	tr.Complete(1, Response{})
	if got := <-order; got != 3 {
		t.Fatalf("first granted %d, want 3", got)
	}
	tr.Cancel(2, errors.New("x"))
	if got := <-order; got != 4 {
		t.Fatalf("second granted %d, want 4", got)
	}
	tr.CancelAll(errors.New("y"))
	if got := <-order; got != 5 {
		t.Fatalf("third granted %d, want 5", got)
	}
	wg.Wait()
	if n := tr.PendingCount(); n != 1 {
		t.Fatalf("pending = %d, want 1", n)
	}
}

// A Reserve whose context ends while it waits registers nothing.
func TestReserveContextEnds(t *testing.T) {
	tr := NewTracker(1)
	ctx := context.Background()
	if _, err := tr.Reserve(ctx, 1); err != nil {
		t.Fatal(err)
	}
	wctx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	if _, err := tr.Reserve(wctx, 2); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Reserve = %v, want DeadlineExceeded", err)
	}
	if tr.WaitingCount() != 0 || tr.PendingCount() != 1 {
		t.Fatalf("waiting %d pending %d", tr.WaitingCount(), tr.PendingCount())
	}
	if _, err := tr.Reserve(ctx, 1); err == nil {
		t.Fatal("Reserve of a pending ID succeeded")
	}
}

// A slot granted while the waiter's context ends is given back to the
// next waiter.
func TestReserveGrantRacesContext(t *testing.T) {
	for range 200 {
		tr := NewTracker(1)
		ctx := context.Background()
		if _, err := tr.Reserve(ctx, 1); err != nil {
			t.Fatal(err)
		}
		wctx, cancel := context.WithCancel(ctx)
		res := make(chan error, 1)
		go func() { _, err := tr.Reserve(wctx, 2); res <- err }()
		waitFor(t, "waiter", func() bool { return tr.WaitingCount() == 1 })
		go cancel()
		tr.Complete(1, Response{})
		err := <-res
		switch {
		case err == nil:
			if tr.PendingCount() != 1 {
				t.Fatalf("granted, pending = %d", tr.PendingCount())
			}
		case errors.Is(err, context.Canceled):
			if tr.PendingCount() != 0 {
				t.Fatalf("cancelled, pending = %d", tr.PendingCount())
			}
		default:
			t.Fatalf("Reserve = %v", err)
		}
		cancel()
	}
}

// Close fails pending requests and waiters, and every later allocation.
func TestTrackerClose(t *testing.T) {
	tr := NewTracker(1)
	ctx := context.Background()
	ch, err := tr.Reserve(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	waiter := make(chan error, 1)
	go func() { _, err := tr.Reserve(ctx, 2); waiter <- err }()
	waitFor(t, "waiter", func() bool { return tr.WaitingCount() == 1 })

	closeErr := errors.New("closed")
	tr.Close(closeErr)
	tr.Close(errors.New("second")) // keeps the first error
	if r := <-ch; !errors.Is(r.Err, closeErr) {
		t.Fatalf("pending = %v", r.Err)
	}
	if err := <-waiter; !errors.Is(err, closeErr) {
		t.Fatalf("waiter = %v", err)
	}
	if _, err := tr.Reserve(ctx, 3); !errors.Is(err, closeErr) {
		t.Fatalf("Reserve after Close = %v", err)
	}
	if _, _, err := tr.AllocateWithID(3); !errors.Is(err, closeErr) {
		t.Fatalf("AllocateWithID after Close = %v", err)
	}
	if _, _, err := tr.Allocate(); !errors.Is(err, closeErr) {
		t.Fatalf("Allocate after Close = %v", err)
	}
}

// A waiter whose context ends after Close closed the tracker gets the
// Close error or its context error, never a slot.
func TestReserveCloseRacesContext(t *testing.T) {
	tr := NewTracker(1)
	ctx := context.Background()
	if _, err := tr.Reserve(ctx, 1); err != nil {
		t.Fatal(err)
	}
	wctx, cancel := context.WithCancel(ctx)
	res := make(chan error, 1)
	go func() { _, err := tr.Reserve(wctx, 2); res <- err }()
	waitFor(t, "waiter", func() bool { return tr.WaitingCount() == 1 })
	cancel()
	tr.Close(errors.New("closed"))
	if err := <-res; err == nil {
		t.Fatal("Reserve succeeded on a closed tracker")
	}
	if tr.PendingCount() != 0 {
		t.Fatalf("pending = %d", tr.PendingCount())
	}
}

// Many goroutines against a small limit never exceed it.
func TestReserveStressLimit(t *testing.T) {
	const limit, workers, rounds = 3, 32, 50
	tr := NewTracker(limit)
	ctx := context.Background()
	var mu sync.Mutex
	inFlight, peak := 0, 0
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for range rounds {
				id := tr.NextID()
				ch, err := tr.Reserve(ctx, id)
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				inFlight++
				peak = max(peak, inFlight)
				mu.Unlock()
				go func() {
					mu.Lock()
					inFlight--
					mu.Unlock()
					tr.Complete(id, Response{})
				}()
				<-ch
			}
		})
	}
	wg.Wait()
	if peak > limit {
		t.Fatalf("peak %d outstanding, limit %d", peak, limit)
	}
	if tr.PendingCount() != 0 || tr.WaitingCount() != 0 {
		t.Fatalf("pending %d waiting %d", tr.PendingCount(), tr.WaitingCount())
	}
}

func TestNewTrackerNegativeLimit(t *testing.T) {
	if NewTracker(-1).Limit() != 0 {
		t.Fatal("negative limit not treated as unlimited")
	}
}
