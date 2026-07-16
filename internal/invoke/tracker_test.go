// SPDX-License-Identifier: MIT

package invoke

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-mms/internal/codec"
)

func TestNextIDSequential(t *testing.T) {
	tr := NewTracker(0)

	id1 := tr.NextID()
	id2 := tr.NextID()
	id3 := tr.NextID()

	if id1 == 0 {
		t.Error("invoke ID should never be 0")
	}
	if id2 != id1+1 || id3 != id2+1 {
		t.Errorf("IDs not sequential: %d, %d, %d", id1, id2, id3)
	}

	if tr.PendingCount() != 0 {
		t.Errorf("NextID should not register pending: count=%d", tr.PendingCount())
	}
}

func TestAllocateSequential(t *testing.T) {
	tr := NewTracker(0)

	id1, ch1, err := tr.Allocate()
	if err != nil {
		t.Fatalf("Allocate 1: %v", err)
	}
	id2, ch2, err := tr.Allocate()
	if err != nil {
		t.Fatalf("Allocate 2: %v", err)
	}

	if id1 == 0 {
		t.Error("invoke ID should never be 0")
	}
	if id2 != id1+1 {
		t.Errorf("id2=%d, want %d", id2, id1+1)
	}
	if ch1 == nil || ch2 == nil {
		t.Error("channels should not be nil")
	}
}

func TestAllocateLimit(t *testing.T) {
	tr := NewTracker(2)

	_, _, err := tr.Allocate()
	if err != nil {
		t.Fatalf("Allocate 1: %v", err)
	}
	_, _, err = tr.Allocate()
	if err != nil {
		t.Fatalf("Allocate 2: %v", err)
	}
	_, _, err = tr.Allocate()
	if err == nil {
		t.Error("expected error when limit reached")
	}
}

func TestComplete(t *testing.T) {
	tr := NewTracker(0)
	id, ch, _ := tr.Allocate()

	ok := tr.Complete(id, Response{Data: []byte{0x01}})
	if !ok {
		t.Fatal("Complete should return true for pending ID")
	}

	resp := <-ch
	if resp.InvokeID != id {
		t.Errorf("InvokeID = %d, want %d", resp.InvokeID, id)
	}
	if len(resp.Data) != 1 || resp.Data[0] != 0x01 {
		t.Errorf("Data = %x, want 01", resp.Data)
	}
	if resp.Err != nil {
		t.Errorf("Err = %v, want nil", resp.Err)
	}

	// Completing again should return false.
	ok = tr.Complete(id, Response{})
	if ok {
		t.Error("Complete should return false for already-completed ID")
	}
}

func TestCancel(t *testing.T) {
	tr := NewTracker(0)
	id, ch, _ := tr.Allocate()

	testErr := errors.New("cancelled")
	ok := tr.Cancel(id, testErr)
	if !ok {
		t.Fatal("Cancel should return true")
	}

	resp := <-ch
	if resp.Err != testErr {
		t.Errorf("Err = %v, want %v", resp.Err, testErr)
	}
}

func TestCancelAll(t *testing.T) {
	tr := NewTracker(0)
	_, ch1, _ := tr.Allocate()
	_, ch2, _ := tr.Allocate()
	_, ch3, _ := tr.Allocate()

	testErr := errors.New("connection closed")
	tr.CancelAll(testErr)

	for i, ch := range []<-chan Response{ch1, ch2, ch3} {
		resp := <-ch
		if resp.Err != testErr {
			t.Errorf("ch%d: Err = %v, want %v", i+1, resp.Err, testErr)
		}
	}

	if tr.PendingCount() != 0 {
		t.Errorf("PendingCount = %d, want 0", tr.PendingCount())
	}
}

func TestPendingCount(t *testing.T) {
	tr := NewTracker(0)
	if tr.PendingCount() != 0 {
		t.Fatalf("initial PendingCount = %d", tr.PendingCount())
	}

	id1, _, _ := tr.Allocate()
	tr.Allocate()
	if tr.PendingCount() != 2 {
		t.Errorf("PendingCount = %d, want 2", tr.PendingCount())
	}

	tr.Complete(id1, Response{})
	if tr.PendingCount() != 1 {
		t.Errorf("PendingCount = %d, want 1", tr.PendingCount())
	}
}

func TestCompleteUnknownID(t *testing.T) {
	tr := NewTracker(0)
	ok := tr.Complete(999, Response{})
	if ok {
		t.Error("Complete should return false for unknown ID")
	}
}

func TestAllocateWithID(t *testing.T) {
	tr := NewTracker(10)
	id := tr.NextID()

	gotID, ch, err := tr.AllocateWithID(id)
	if err != nil {
		t.Fatal(err)
	}
	if gotID != id {
		t.Fatal("id mismatch")
	}
	if ch == nil {
		t.Fatal("nil channel")
	}

	tr.Complete(id, Response{})

	id2 := tr.NextID()
	_, _, err = tr.AllocateWithID(id2)
	if err != nil {
		t.Fatal(err)
	}

	// Duplicate of already-allocated should fail
	_, _, err = tr.AllocateWithID(id2)
	if err == nil {
		t.Fatal("expected error for duplicate")
	}
}

func TestAllocateWithIDLimit(t *testing.T) {
	tr := NewTracker(1)
	id1 := tr.NextID()
	_, _, err := tr.AllocateWithID(id1)
	if err != nil {
		t.Fatal(err)
	}

	id2 := tr.NextID()
	_, _, err = tr.AllocateWithID(id2)
	if err == nil {
		t.Fatal("expected error when limit reached")
	}
}

func TestTrackerConcurrentStress(t *testing.T) {
	tr := NewTracker(10)

	var wg sync.WaitGroup
	const goroutines = 50
	const opsPerGoroutine = 100

	for i := 0; i < goroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				id, ch, err := tr.Allocate()
				if err != nil {
					continue
				}
				go tr.Complete(id, Response{})
				<-ch
			}
		}()
	}

	for i := 0; i < goroutines/4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				id, ch, err := tr.Allocate()
				if err != nil {
					continue
				}
				go tr.Cancel(id, fmt.Errorf("test cancel"))
				<-ch
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 5; j++ {
			time.Sleep(time.Millisecond)
			tr.CancelAll(fmt.Errorf("cancel all"))
		}
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("stress test timed out")
	}
}

func TestAllocateWithIDConcurrent(t *testing.T) {
	tr := NewTracker(100)
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		id := codec.InvokeID(i%10) + 1 // avoid 0; some will collide
		go func(id codec.InvokeID) {
			defer wg.Done()
			_, ch, err := tr.AllocateWithID(id)
			if err != nil {
				return
			}
			go tr.Complete(id, Response{})
			<-ch
		}(id)
	}

	wg.Wait()
}
