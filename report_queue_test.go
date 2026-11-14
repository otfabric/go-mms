// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// reportSink records the value of each report delivered; while gate is
// open (not closed), each delivery waits for a value from it.
type reportSink struct {
	mu   sync.Mutex
	got  []int64
	gate chan struct{}
}

func newReportSink() *reportSink { return &reportSink{gate: make(chan struct{})} }

func (s *reportSink) handler(ind *InformationReportIndication) {
	v, _ := ind.Values[0].Int64()
	s.mu.Lock()
	s.got = append(s.got, v)
	s.mu.Unlock()
	<-s.gate
}

func (s *reportSink) delivered() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.got)
}

// dropLog records ReportDroppedEvents.
type dropLog struct {
	NopMetrics
	mu     sync.Mutex
	events []ReportDroppedEvent
}

func (d *dropLog) ObserveReportDropped(ev ReportDroppedEvent) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.events = append(d.events, ev)
}

func (d *dropLog) all() []ReportDroppedEvent {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.events)
}

// sendReports has the server send one report per value to its only
// association, each read by the client before the next is sent.
func sendReports(t *testing.T, srv *Server, values ...int64) {
	t.Helper()
	for _, v := range values {
		if err := srv.Connections()[0].SendInformationReport(context.Background(), &InformationReportRequest{
			Variables: []ObjectName{{Scope: ObjectScopeDomain, Domain: "LD", ItemID: "v"}},
			Values:    []*Value{NewInteger(v)},
		}); err != nil {
			t.Fatalf("SendInformationReport: %v", err)
		}
		synctest.Wait()
	}
}

// queueClient serves a server with domain LD to a client whose reports
// go to sink.
func queueClient(t *testing.T, opts DialOptions, sink *reportSink) (*Server, *Client) {
	t.Helper()
	srv := NewServer(ServerOptions{})
	if err := srv.RegisterDomain("LD"); err != nil {
		t.Fatal(err)
	}
	c, _ := serveLoopback(t, srv, opts)
	c.OnInformationReport(sink.handler)
	synctest.Wait() // the association is registered
	return srv, c
}

// getDomains starts a GetNameList of the domains and returns a channel
// that yields its error.
func getDomains(c *Client) <-chan error {
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_, err := c.GetNameList(ctx, NameListRequest{ObjectClass: ObjectClassDomain, Scope: ObjectScopeVMD})
		done <- err
	}()
	return done
}

// With a report queue, a handler that takes its time does not hold up the
// responses to confirmed requests (L11); a full queue drops the newest or
// the oldest report, counts it and reports it to Metrics, and the queued
// reports are delivered in order.
func TestReportQueueSlowHandler(t *testing.T) {
	for _, tc := range []struct {
		full ReportQueueFull
		want []int64
	}{
		{ReportDropNewest, []int64{1, 2, 3}},
		{ReportDropOldest, []int64{1, 4, 5}},
	} {
		t.Run(map[ReportQueueFull]string{ReportDropNewest: "DropNewest", ReportDropOldest: "DropOldest"}[tc.full], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				sink := newReportSink()
				drops := &dropLog{}
				srv, c := queueClient(t, DialOptions{Metrics: drops, ReportQueue: ReportQueueOptions{Length: 2, Full: tc.full}}, sink)
				defer func() { _ = c.Abort(context.Background()) }()

				sendReports(t, srv, 1) // the handler holds 1
				if err := <-getDomains(c); err != nil {
					t.Fatalf("GetNameList while the handler runs: %v", err)
				}
				sendReports(t, srv, 2, 3, 4, 5) // 2 and 3 fill the queue
				want := []ReportDroppedEvent{{Full: tc.full, Dropped: 1}, {Full: tc.full, Dropped: 2}}
				if got := drops.all(); !slices.Equal(got, want) {
					t.Fatalf("drop events %+v, want %+v", got, want)
				}
				if n := c.reportsDropped.Load(); n != 2 {
					t.Fatalf("%d reports counted as dropped, want 2", n)
				}
				close(sink.gate)
				synctest.Wait()
				if got := sink.delivered(); !slices.Equal(got, tc.want) {
					t.Fatalf("delivered %v, want %v", got, tc.want)
				}
			})
		})
	}
}

// ReportWait drops no report: while the queue is full the reader waits,
// and so do the responses.
func TestReportQueueWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sink := newReportSink()
		drops := &dropLog{}
		srv, c := queueClient(t, DialOptions{Metrics: drops, ReportQueue: ReportQueueOptions{Length: 1, Full: ReportWait}}, sink)
		defer func() { _ = c.Abort(context.Background()) }()

		sendReports(t, srv, 1, 2, 3) // the handler holds 1, 2 is queued, the reader waits with 3
		done := getDomains(c)
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("GetNameList returned (%v) while the reader waits for room", err)
		default:
		}
		close(sink.gate)
		if err := <-done; err != nil {
			t.Fatalf("GetNameList: %v", err)
		}
		synctest.Wait()
		if got := sink.delivered(); !slices.Equal(got, []int64{1, 2, 3}) {
			t.Fatalf("delivered %v, want [1 2 3]", got)
		}
		if len(drops.all()) != 0 || c.reportsDropped.Load() != 0 {
			t.Fatalf("drops %+v", drops.all())
		}
	})
}

// Without a queue (the default) the handler runs on the reader: a response
// waits for it, as before v1.11.0.
func TestReportInlineDefault(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sink := newReportSink()
		srv, c := queueClient(t, DialOptions{}, sink)
		defer func() { _ = c.Abort(context.Background()) }()
		if c.reports != nil {
			t.Fatal("a queue without ReportQueue.Length")
		}
		sendReports(t, srv, 1)
		done := getDomains(c)
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("GetNameList returned (%v) while the handler runs on the reader", err)
		default:
		}
		close(sink.gate)
		if err := <-done; err != nil {
			t.Fatalf("GetNameList: %v", err)
		}
	})
}

// Abort with a queue ends a reader that waits for room. Called while the
// handler runs, it returns without waiting for the handler, as without a
// queue; the delivery goroutine then stops when the handler returns, and
// the reports still queued are not delivered.
func TestReportQueueAbort(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sink := newReportSink()
		srv, c := queueClient(t, DialOptions{ReportQueue: ReportQueueOptions{Length: 1, Full: ReportWait}}, sink)
		sendReports(t, srv, 1, 2, 3) // the handler holds 1, 2 is queued, the reader waits with 3
		if err := c.Abort(context.Background()); err != nil {
			t.Fatalf("Abort: %v", err)
		}
		<-c.readerDone
		close(sink.gate)
		<-c.reportsDone
		if got := sink.delivered(); !slices.Equal(got, []int64{1}) {
			t.Fatalf("delivered %v after Abort, want [1]", got)
		}
	})
}

// Close with a reader that waits for room in the queue completes the
// Conclude exchange: after Close started, the queued reports are taken
// out without being delivered, so the reader reads the Conclude-Response.
func TestReportQueueCloseWithWaitingReader(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sink := newReportSink()
		srv, c := queueClient(t, DialOptions{ReportQueue: ReportQueueOptions{Length: 1, Full: ReportWait}}, sink)
		sendReports(t, srv, 1, 2, 3, 4) // the handler holds 1, 2 is queued, the reader waits with 3, 4 waits in the transport
		closed := make(chan error, 1)
		go func() { closed <- c.Close(context.Background()) }()
		synctest.Wait()
		close(sink.gate)
		if err := <-closed; err != nil {
			t.Fatalf("Close: %v", err)
		}
		if got := sink.delivered(); !slices.Equal(got, []int64{1}) {
			t.Fatalf("delivered %v after Close, want [1]", got)
		}
	})
}

// When the peer ends the association, the reports already queued are
// delivered.
func TestReportQueueDrainsOnPeerEnd(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sink := newReportSink()
		srv, c := queueClient(t, DialOptions{ReportQueue: ReportQueueOptions{Length: 4}}, sink)
		defer func() { _ = c.Abort(context.Background()) }()
		sendReports(t, srv, 1, 2, 3)
		if err := srv.Connections()[0].Abort(context.Background()); err != nil {
			t.Fatal(err)
		}
		<-c.Done()
		close(sink.gate)
		synctest.Wait()
		if got := sink.delivered(); !slices.Equal(got, []int64{1, 2, 3}) {
			t.Fatalf("delivered %v, want [1 2 3]", got)
		}
	})
}

// A report queued while a handler was registered is not delivered after
// the handler is removed.
func TestReportQueueHandlerRemoved(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sink := newReportSink()
		srv, c := queueClient(t, DialOptions{ReportQueue: ReportQueueOptions{Length: 2}}, sink)
		defer func() { _ = c.Abort(context.Background()) }()
		sendReports(t, srv, 1, 2)
		c.OnInformationReport(nil)
		close(sink.gate)
		synctest.Wait()
		if got := sink.delivered(); !slices.Equal(got, []int64{1}) {
			t.Fatalf("delivered %v, want [1]", got)
		}
	})
}

// A handler that calls Abort with a queue does not wait for itself (L10).
func TestReportQueueHandlerAborts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv, c := queueClient(t, DialOptions{ReportQueue: ReportQueueOptions{Length: 1}}, newReportSink())
		done := make(chan time.Duration, 1)
		c.OnInformationReport(func(*InformationReportIndication) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			start := time.Now()
			_ = c.Abort(ctx)
			done <- time.Since(start)
		})
		sendReports(t, srv, 1)
		if elapsed := <-done; elapsed != 0 {
			t.Fatalf("Abort from the handler took %v", elapsed)
		}
		<-c.reportsDone
	})
}
