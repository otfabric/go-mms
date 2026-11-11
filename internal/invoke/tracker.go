// SPDX-License-Identifier: MIT

// Package invoke manages MMS invoke ID allocation and request/response
// correlation.
//
// The client uses [Tracker.NextID] for invoke ID allocation and
// [Tracker.Reserve] (or [Tracker.AllocateWithID]) and [Tracker.Complete]
// for asynchronous dispatch via the background reader loop. The tracker
// correlates inbound confirmed responses to waiting callers by invoke ID.
//
// A tracker created with a positive limit holds at most that many
// requests pending: the negotiated max-services-outstanding of the
// requesting side (ISO 9506-2 clause 8.2, Initiate). [Tracker.Reserve]
// waits for a free slot, first come first served; [Tracker.AllocateWithID]
// fails with [ErrLimit] instead.
//
// This package is internal — invoke ID management is transparent to
// users of the public mms.Client API.
package invoke

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/otfabric/go-mms/internal/codec"
)

// ErrLimit is wrapped by the error of an allocation that would exceed the
// tracker's limit.
var ErrLimit = errors.New("invoke: outstanding request limit reached")

// Tracker allocates invoke IDs and tracks outstanding confirmed requests.
// It is safe for concurrent use.
type Tracker struct {
	mu         sync.Mutex
	nextID     codec.InvokeID
	pending    map[codec.InvokeID]chan<- Response
	maxPending int
	waiters    []*waiter // FIFO of Reserve calls waiting for a slot
	closeErr   error     // non-nil after Close
}

// waiter is a Reserve call queued for a slot. ready receives nil when the
// slot is granted (the request is then pending) or the Close error.
type waiter struct {
	id    codec.InvokeID
	ch    chan Response
	ready chan error
}

// Response carries the result of an outstanding confirmed request.
type Response struct {
	InvokeID codec.InvokeID
	Kind     int    // PDU kind, opaque to this package; set by the reader
	Data     []byte // raw inner content bytes for further processing
	Err      error  // non-nil if the request failed before a response was received
}

// NewTracker creates a new invoke tracker.
// maxPending limits the number of concurrent outstanding requests
// (0 = no limit); the client passes its negotiated
// max-services-outstanding-calling.
func NewTracker(maxPending int) *Tracker {
	return &Tracker{
		pending:    make(map[codec.InvokeID]chan<- Response),
		maxPending: max(maxPending, 0),
	}
}

// Limit returns the maximum number of pending requests (0 = no limit).
func (t *Tracker) Limit() int {
	return t.maxPending
}

// NextID returns the next invoke ID without registering a pending
// request. Use this for the synchronous request/response model where
// the caller manages the send/receive lifecycle directly.
func (t *Tracker) NextID() codec.InvokeID {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.advance()
	return t.nextID
}

// Allocate reserves the next invoke ID and registers a pending request.
// The returned channel will receive exactly one Response when the
// request completes (or is cancelled). The caller must eventually call
// Complete or Cancel for this invoke ID.
func (t *Tracker) Allocate() (codec.InvokeID, <-chan Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.checkRoom(); err != nil {
		return 0, nil, err
	}

	t.advance()
	id := t.nextID

	ch := make(chan Response, 1)
	t.pending[id] = ch
	return id, ch, nil
}

// AllocateWithID registers a pending request for a specific invoke ID
// (already allocated via [NextID]) without waiting: it fails with an
// error wrapping [ErrLimit] when the limit is reached, and with the
// Close error after [Tracker.Close]. Returns the response channel.
func (t *Tracker) AllocateWithID(id codec.InvokeID) (codec.InvokeID, <-chan Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.checkRoom(); err != nil {
		return 0, nil, err
	}
	if _, exists := t.pending[id]; exists {
		return 0, nil, fmt.Errorf("invoke: ID %d already pending", id)
	}

	ch := make(chan Response, 1)
	t.pending[id] = ch
	return id, ch, nil
}

// checkRoom reports why no request can be registered now. The caller
// holds t.mu. Queued waiters go first, so a request that does not wait
// does not overtake them.
func (t *Tracker) checkRoom() error {
	if t.closeErr != nil {
		return t.closeErr
	}
	if t.full() || len(t.waiters) > 0 {
		return fmt.Errorf("%w (%d)", ErrLimit, t.maxPending)
	}
	return nil
}

// full reports whether the limit is reached. The caller holds t.mu.
func (t *Tracker) full() bool {
	return t.maxPending > 0 && len(t.pending) >= t.maxPending
}

// Reserve registers a pending request for id, waiting while the limit is
// reached. Waiters get slots in the order they called Reserve. It returns
// ctx's error when ctx ends first (nothing is registered then), and the
// Close error after [Tracker.Close].
func (t *Tracker) Reserve(ctx context.Context, id codec.InvokeID) (<-chan Response, error) {
	t.mu.Lock()
	if t.closeErr != nil {
		err := t.closeErr
		t.mu.Unlock()
		return nil, err
	}
	if _, exists := t.pending[id]; exists {
		t.mu.Unlock()
		return nil, fmt.Errorf("invoke: ID %d already pending", id)
	}
	ch := make(chan Response, 1)
	if !t.full() && len(t.waiters) == 0 {
		t.pending[id] = ch
		t.mu.Unlock()
		return ch, nil
	}
	w := &waiter{id: id, ch: ch, ready: make(chan error, 1)}
	t.waiters = append(t.waiters, w)
	t.mu.Unlock()

	select {
	case err := <-w.ready:
		if err != nil {
			return nil, err
		}
		return ch, nil
	case <-ctx.Done():
	}

	t.mu.Lock()
	for i, q := range t.waiters {
		if q == w {
			t.waiters = append(t.waiters[:i], t.waiters[i+1:]...)
			t.mu.Unlock()
			return nil, ctx.Err()
		}
	}
	// The slot was granted (or the tracker closed) while ctx ended: give
	// the slot back.
	if t.pending[id] == ch {
		delete(t.pending, id)
		t.grant()
	}
	t.mu.Unlock()
	return nil, ctx.Err()
}

// grant hands free slots to queued waiters. The caller holds t.mu.
func (t *Tracker) grant() {
	for len(t.waiters) > 0 && !t.full() {
		w := t.waiters[0]
		t.waiters[0] = nil
		t.waiters = t.waiters[1:]
		t.pending[w.id] = w.ch
		w.ready <- nil
	}
}

// Complete delivers a response for the given invoke ID and removes
// the pending entry, freeing its slot. Returns false if no pending
// request exists for the ID (e.g., already completed or cancelled).
func (t *Tracker) Complete(id codec.InvokeID, resp Response) bool {
	t.mu.Lock()
	ch, ok := t.pending[id]
	if ok {
		delete(t.pending, id)
		t.grant()
	}
	t.mu.Unlock()

	if !ok {
		return false
	}
	resp.InvokeID = id
	ch <- resp
	return true
}

// Cancel removes a pending request without delivering a response.
// The response channel receives an error. Returns false if no pending
// request exists for the ID.
func (t *Tracker) Cancel(id codec.InvokeID, err error) bool {
	return t.Complete(id, Response{Err: err})
}

// CancelAll cancels all pending requests with the given error. Queued
// waiters then get the freed slots.
func (t *Tracker) CancelAll(err error) {
	t.mu.Lock()
	pending := t.pending
	t.pending = make(map[codec.InvokeID]chan<- Response)
	t.grant()
	t.mu.Unlock()

	for id, ch := range pending {
		ch <- Response{InvokeID: id, Err: err}
	}
}

// Close cancels all pending requests and queued waiters with err; every
// later allocation fails with err. Closing again keeps the first error.
func (t *Tracker) Close(err error) {
	t.mu.Lock()
	if t.closeErr == nil {
		t.closeErr = err
	}
	pending := t.pending
	t.pending = make(map[codec.InvokeID]chan<- Response)
	waiters := t.waiters
	t.waiters = nil
	t.mu.Unlock()

	for id, ch := range pending {
		ch <- Response{InvokeID: id, Err: err}
	}
	for _, w := range waiters {
		w.ready <- err
	}
}

// advance moves nextID on, wrapping to 1 after 2^31-1: invokeID is
// Unsigned32 ::= INTEGER (0..2147483647) in ISO 9506-2 (MMS-PDU-24).
// The caller holds t.mu.
func (t *Tracker) advance() {
	t.nextID++
	if t.nextID == 0 || t.nextID > maxInvokeID {
		t.nextID = 1
	}
}

// maxInvokeID is the largest Unsigned32 value of ISO 9506-2.
const maxInvokeID = codec.MaxInvokeID

// Sole returns the invoke ID of the only pending request, if exactly one
// is pending.
func (t *Tracker) Sole() (codec.InvokeID, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.pending) != 1 {
		return 0, false
	}
	for id := range t.pending {
		return id, true
	}
	return 0, false
}

// PendingCount returns the number of currently pending requests.
func (t *Tracker) PendingCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.pending)
}

// WaitingCount returns the number of Reserve calls waiting for a slot.
func (t *Tracker) WaitingCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.waiters)
}
