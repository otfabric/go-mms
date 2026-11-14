// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

// sendReport has the server send one InformationReport to its only
// association.
func sendReport(t *testing.T, srv *Server) {
	t.Helper()
	conns := srv.Connections()
	if len(conns) != 1 {
		t.Fatalf("%d connections", len(conns))
	}
	if err := conns[0].SendInformationReport(context.Background(), &InformationReportRequest{
		Variables: []ObjectName{{Scope: ObjectScopeDomain, Domain: "LD", ItemID: "v"}},
		Values:    []*Value{NewInteger(1)},
	}); err != nil {
		t.Fatalf("SendInformationReport: %v", err)
	}
}

// A report handler may end the association (L10): Abort returns at once,
// Close when its context is done; the reader stops when the handler
// returns, and nothing is left behind.
func TestReportHandlerEndsAssociation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		end     func(*Client, context.Context) error
		wantErr error
	}{
		{"Abort", (*Client).Abort, nil},
		{"Close", (*Client).Close, context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				srv := NewServer(ServerOptions{})
				c, served := serveLoopback(t, srv, DialOptions{})
				type result struct {
					err     error
					elapsed time.Duration
				}
				done := make(chan result, 1)
				c.OnInformationReport(func(*InformationReportIndication) {
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					defer cancel()
					start := time.Now()
					err := tc.end(c, ctx)
					done <- result{err, time.Since(start)}
				})
				synctest.Wait() // the association is registered
				sendReport(t, srv)

				var r result
				select {
				case r = <-done:
				case <-time.After(time.Minute):
					t.Fatalf("%s from the report handler did not return", tc.name)
				}
				if tc.wantErr == nil && r.err != nil || tc.wantErr != nil && !errors.Is(r.err, tc.wantErr) {
					t.Errorf("%s = %v, want %v", tc.name, r.err, tc.wantErr)
				}
				if r.elapsed > time.Second {
					t.Errorf("%s took %v, its context %v", tc.name, r.elapsed, time.Second)
				}
				<-c.Done()
				_ = waitServe(t, served)
			})
		})
	}
}
