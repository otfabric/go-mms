// SPDX-License-Identifier: MIT

package mms

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"math"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// testLimit is the limit the boundary tests set.
const testLimit = 3

// lockedBuffer is a goroutine-safe log sink.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *lockedBuffer) logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(b, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func ctx5s(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// limitServer returns a server with domain LD, its event log and its log.
func limitServer(t *testing.T, opts ServerOptions) (*Server, *eventLog, *lockedBuffer) {
	t.Helper()
	var logs lockedBuffer
	ev := &eventLog{}
	opts.Metrics, opts.Logger = ev, logs.logger()
	srv := NewServer(opts)
	if err := srv.RegisterDomain("LD"); err != nil {
		t.Fatal(err)
	}
	return srv, ev, &logs
}

// MaxAssociations resolves as the other limits of ServerOptions: zero
// selects 100, any other value is the limit, a negative one refusing every
// association. limitOf and limitOf64, which resolve the client's *All
// maxima, read a negative value as no limit.
func TestMaxAssociationsResolve(t *testing.T) {
	for _, tc := range []struct{ v, want int }{{0, 100}, {1, 1}, {500, 500}, {-1, -1}, {math.MaxInt, math.MaxInt}} {
		if got := NewServer(ServerOptions{MaxAssociations: tc.v}).maxAssociations; got != tc.want {
			t.Errorf("MaxAssociations %d: limit %d, want %d", tc.v, got, tc.want)
		}
	}
	for _, tc := range []struct{ v, want int }{{0, 100}, {1, 1}, {-1, math.MaxInt}} {
		if got := limitOf(tc.v, 100); got != tc.want {
			t.Errorf("limitOf(%d, 100) = %d, want %d", tc.v, got, tc.want)
		}
	}
	for _, tc := range []struct{ v, want int64 }{{0, 7}, {3, 3}, {-1, math.MaxInt64}} {
		if got := limitOf64(tc.v, 7); got != tc.want {
			t.Errorf("limitOf64(%d, 7) = %d, want %d", tc.v, got, tc.want)
		}
	}
}

// MaxAssociations: the AARQ beyond the limit is answered with AARE
// rejected-transient and Initiate-Error resource capability-unavailable,
// its Serve returns an error matching ErrLimitReached and
// ErrAssociationFailed, and a slot is given back when a Serve returns. A
// negative limit answers the first AARQ so; math.MaxInt keeps the
// behaviour of v1.10.0, which had no limit.
func TestServerLimitMaxAssociations(t *testing.T) {
	for _, tc := range []struct {
		name     string
		limit, n int
	}{
		{"max-1", testLimit, testLimit - 1},
		{"max", testLimit, testLimit},
		{"max+1", testLimit, testLimit + 1},
		{"negative", -1, 1},
		{"MaxInt", math.MaxInt, testLimit + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var logs lockedBuffer
				srvLog := &eventLog{}
				srv := NewServer(ServerOptions{MaxAssociations: tc.limit, Metrics: srvLog, Logger: logs.logger()})
				var clients []*Client
				var served []<-chan error
				for i := range tc.n {
					c, serveErr, err := dialServer(t, srv, DialOptions{})
					if i < tc.limit {
						if err != nil {
							t.Fatalf("association %d of a limit of %d: %v", i+1, tc.limit, err)
						}
						clients, served = append(clients, c), append(served, serveErr)
						continue
					}
					rej, ok := errors.AsType[*RejectionError](err)
					if !ok || !rej.Transient || rej.ACSEResult != 2 || rej.InitiateError == nil ||
						rej.InitiateError.Class != ErrorClassResource || rej.InitiateError.Code != 4 {
						t.Fatalf("association %d of a limit of %d: %v (%+v), want a transient rejection with Initiate-Error resource capability-unavailable", i+1, tc.limit, err, rej)
					}
					serr := waitServe(t, serveErr)
					if !errors.Is(serr, ErrLimitReached) || !errors.Is(serr, ErrAssociationFailed) {
						t.Fatalf("Serve = %v, want ErrLimitReached and ErrAssociationFailed", serr)
					}
					if srvLog.count(ConnectionRejected) != 1 || !strings.Contains(logs.String(), "limit=MaxAssociations") {
						t.Fatalf("refusal not reported: %d rejected events, log %q", srvLog.count(ConnectionRejected), logs.String())
					}
				}
				synctest.Wait() // every association held is registered
				if got, want := len(srv.Connections()), len(clients); got != want {
					t.Fatalf("%d connections, want %d", got, want)
				}
				if len(clients) == 0 {
					return
				}
				// The slot of an association is given back when its Serve
				// returns: one more association fits after one ended.
				if err := clients[0].Abort(context.Background()); err != nil {
					t.Fatal(err)
				}
				_ = waitServe(t, served[0])
				c, _, err := dialServer(t, srv, DialOptions{})
				if err != nil {
					t.Fatalf("association after one ended: %v", err)
				}
				_ = c.Abort(context.Background())
			})
		})
	}
}
