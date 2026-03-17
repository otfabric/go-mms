// Package invoke manages MMS invoke ID allocation and request/response
// correlation.
//
// The client uses [Tracker.NextID] for invoke ID allocation and
// [Tracker.AllocateWithID]/[Tracker.Complete] for asynchronous
// dispatch via the background reader loop. The tracker correlates
// inbound confirmed responses to waiting callers by invoke ID.
//
// This package is internal — invoke ID management is transparent to
// users of the public mms.Client API.
package invoke

import (
	"fmt"
	"sync"

	"github.com/otfabric/go-mms/internal/codec"
)

// Tracker allocates invoke IDs and optionally tracks outstanding
// confirmed requests. It is safe for concurrent use.
type Tracker struct {
	mu         sync.Mutex
	nextID     codec.InvokeID
	pending    map[codec.InvokeID]chan<- Response
	maxPending int
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
// (0 = no limit, but in practice should match the negotiated value).
func NewTracker(maxPending int) *Tracker {
	return &Tracker{
		pending:    make(map[codec.InvokeID]chan<- Response),
		maxPending: maxPending,
	}
}

// NextID returns the next invoke ID without registering a pending
// request. Use this for the synchronous request/response model where
// the caller manages the send/receive lifecycle directly.
func (t *Tracker) NextID() codec.InvokeID {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.nextID++
	if t.nextID == 0 {
		t.nextID = 1
	}
	return t.nextID
}

// Allocate reserves the next invoke ID and registers a pending request.
// The returned channel will receive exactly one Response when the
// request completes (or is cancelled). The caller must eventually call
// Complete or Cancel for this invoke ID.
func (t *Tracker) Allocate() (codec.InvokeID, <-chan Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.maxPending > 0 && len(t.pending) >= t.maxPending {
		return 0, nil, fmt.Errorf("invoke: outstanding call limit reached (%d)", t.maxPending)
	}

	t.nextID++
	if t.nextID == 0 {
		t.nextID = 1
	}
	id := t.nextID

	ch := make(chan Response, 1)
	t.pending[id] = ch
	return id, ch, nil
}

// AllocateWithID registers a pending request for a specific invoke ID
// (already allocated via [NextID]). Returns the response channel.
func (t *Tracker) AllocateWithID(id codec.InvokeID) (codec.InvokeID, <-chan Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.maxPending > 0 && len(t.pending) >= t.maxPending {
		return 0, nil, fmt.Errorf("invoke: outstanding call limit reached (%d)", t.maxPending)
	}

	if _, exists := t.pending[id]; exists {
		return 0, nil, fmt.Errorf("invoke: ID %d already pending", id)
	}

	ch := make(chan Response, 1)
	t.pending[id] = ch
	return id, ch, nil
}

// Complete delivers a response for the given invoke ID and removes
// the pending entry. Returns false if no pending request exists for
// the ID (e.g., already completed or cancelled).
func (t *Tracker) Complete(id codec.InvokeID, resp Response) bool {
	t.mu.Lock()
	ch, ok := t.pending[id]
	if ok {
		delete(t.pending, id)
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

// CancelAll cancels all pending requests with the given error.
func (t *Tracker) CancelAll(err error) {
	t.mu.Lock()
	pending := t.pending
	t.pending = make(map[codec.InvokeID]chan<- Response)
	t.mu.Unlock()

	for id, ch := range pending {
		ch <- Response{InvokeID: id, Err: err}
	}
}

// PendingCount returns the number of currently pending requests.
func (t *Tracker) PendingCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.pending)
}
