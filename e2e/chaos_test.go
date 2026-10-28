// SPDX-License-Identifier: MIT

package e2e

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/internal/chaos"
	"github.com/otfabric/go-mms/transport/iso"
)

// Chaos tests drive client and server through internal/chaos connections.
// They are short by default; `make chaos` runs them for longer with
// MMS_CHAOS_DURATION (a time.Duration) and a time-based seed
// (MMS_CHAOS_SEED=0). A failing run prints the seed to replay it.

func chaosDuration() time.Duration {
	if d, err := time.ParseDuration(os.Getenv("MMS_CHAOS_DURATION")); err == nil && d > 0 {
		return d
	}
	return 500 * time.Millisecond
}

func chaosSeed(t *testing.T) int64 {
	seed := int64(1)
	if s := os.Getenv("MMS_CHAOS_SEED"); s != "" {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			t.Fatalf("MMS_CHAOS_SEED: %v", err)
		}
		seed = v
		if seed == 0 {
			seed = time.Now().UnixNano()
		}
	}
	t.Logf("chaos seed %d (replay with MMS_CHAOS_SEED=%d)", seed, seed)
	return seed
}

// chaosServer serves a small model through a chaos listener.
type chaosServer struct {
	srv  *mms.Server
	addr string
	mu   sync.Mutex
	vals map[string]*mms.Value
}

func startChaosServer(t *testing.T, cfg chaos.Config, opts mms.ServerOptions) *chaosServer {
	t.Helper()
	cs := &chaosServer{vals: map[string]*mms.Value{}}
	cs.srv = mms.NewServer(opts)
	cs.srv.HandleIdentify(func(context.Context, mms.IdentifyRequest) (*mms.ServerIdentity, error) {
		return &mms.ServerIdentity{Vendor: "otfabric", Model: "chaos", Revision: "1"}, nil
	})
	if err := cs.srv.RegisterDomain(dom); err != nil {
		t.Fatal(err)
	}
	for i := range 8 {
		name := fmt.Sprintf("v%d", i)
		cs.vals[name] = mms.NewInteger(int64(i))
		if err := cs.srv.RegisterVariable(mms.Variable{
			Name: domName(name), TypeSpec: i32,
			Read: func(context.Context) (*mms.Value, error) {
				cs.mu.Lock()
				defer cs.mu.Unlock()
				return cs.vals[name], nil
			},
			Write: func(_ context.Context, v *mms.Value) error {
				cs.mu.Lock()
				defer cs.mu.Unlock()
				cs.vals[name] = v
				return nil
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln := iso.NewListener(chaos.WrapListener(tcp, cfg), iso.WithHandshakeTimeout(2*time.Second))
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = cs.srv.ListenAndServe(context.Background(), ln)
	}()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = cs.srv.Shutdown(ctx)
		<-done
	})
	cs.addr = tcp.Addr().String()
	return cs
}

// expectedChaosError reports whether err is one of the documented outcomes
// of a broken or slow connection.
func expectedChaosError(err error) bool {
	return errors.Is(err, mms.ErrClosed) ||
		errors.Is(err, mms.ErrAssociationFailed) ||
		errors.Is(err, mms.ErrInvokeTimeout) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, chaos.ErrInjected) ||
		errors.Is(err, net.ErrClosed)
}

// workload runs a mix of services on c until ctx is done or the
// association breaks. It returns the number of successful requests and
// the first unexpected error.
func workload(ctx context.Context, c *mms.Client, id int) (int, error) {
	ok := 0
	for i := 0; ctx.Err() == nil; i++ {
		name := domName(fmt.Sprintf("v%d", (id+i)%8))
		reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		var err error
		switch i % 4 {
		case 0:
			_, err = c.ReadObject(reqCtx, name)
		case 1:
			_, err = c.WriteObject(reqCtx, name, mms.NewInteger(int64(i)))
		case 2:
			_, err = c.Identify(reqCtx)
		case 3:
			_, err = c.GetNameListAll(reqCtx, mms.NameListRequest{ObjectClass: mms.ObjectClassNamedVariable, Scope: mms.ObjectScopeDomain, DomainID: dom})
		}
		cancel()
		if err != nil {
			if ctx.Err() != nil || expectedChaosError(err) {
				return ok, nil
			}
			return ok, err
		}
		ok++
	}
	return ok, nil
}

// Delays, partial reads and fragmented writes on both sides must be
// invisible to the application.
func TestChaos_LossyButIntact(t *testing.T) {
	cfg := chaos.Config{Seed: chaosSeed(t), ChunkReads: true, ChunkWrites: true, MaxDelay: 200 * time.Microsecond}
	cs := startChaosServer(t, cfg, mms.ServerOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), chaosDuration())
	defer cancel()

	var wg sync.WaitGroup
	var total atomic.Int64
	for id := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := iso.Dial(timeout(t), cs.addr, iso.WithDialer(chaos.Dialer(cfg)))
			if err != nil {
				t.Errorf("client %d: Dial: %v", id, err)
				return
			}
			defer func() { _ = c.Close(timeout(t)) }()
			for ctx.Err() == nil {
				n, err := workload(ctx, c, id)
				total.Add(int64(n))
				if err != nil {
					t.Errorf("client %d: %v", id, err)
					return
				}
				if c.Err() != nil {
					t.Errorf("client %d: association lost without injected drops: %v", id, c.Err())
					return
				}
			}
		}()
	}
	wg.Wait()
	if total.Load() == 0 {
		t.Error("no request completed")
	}
	t.Logf("%d requests completed", total.Load())
}

// Connections dropped mid-PDU on either side end in classified errors,
// never hangs or panics; clients reconnect with Retry and the server
// releases every association.
func TestChaos_DropsAndReconnect(t *testing.T) {
	seed := chaosSeed(t)
	serverCfg := chaos.Config{Seed: seed, ChunkWrites: true, DropAfterBytes: 3000, ResetJitter: 20000}
	clientCfg := chaos.Config{Seed: seed + 1, MaxDelay: 100 * time.Microsecond, DropProbability: 0.002}
	cs := startChaosServer(t, serverCfg, mms.ServerOptions{AssociationTimeout: time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), chaosDuration())
	defer cancel()

	policy := mms.RetryPolicy{MaxAttempts: -1, InitialBackoff: time.Millisecond, MaxBackoff: 20 * time.Millisecond, Jitter: 0.5}
	var wg sync.WaitGroup
	var total, reconnects atomic.Int64
	for id := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				dialCtx, cancelDial := context.WithTimeout(ctx, 5*time.Second)
				c, err := iso.Dial(dialCtx, cs.addr, iso.WithDialer(chaos.Dialer(clientCfg)), iso.WithRetry(policy),
					iso.WithClientDialOptions(mms.DialOptions{RequestTimeout: time.Second}))
				cancelDial()
				if err != nil {
					if ctx.Err() == nil && !expectedChaosError(err) {
						t.Errorf("client %d: Dial: %v", id, err)
					}
					return
				}
				reconnects.Add(1)
				n, err := workload(ctx, c, id)
				total.Add(int64(n))
				abortCtx, cancelAbort := context.WithTimeout(context.Background(), time.Second)
				_ = c.Abort(abortCtx)
				cancelAbort()
				select {
				case <-c.Done():
				case <-time.After(5 * time.Second):
					t.Errorf("client %d: Done not closed after Abort", id)
					return
				}
				if err != nil {
					t.Errorf("client %d: %v", id, err)
					return
				}
			}
		}()
	}
	wg.Wait()
	t.Logf("%d requests over %d associations", total.Load(), reconnects.Load())
	eventually(t, "server to release all associations", func() bool { return len(cs.srv.Connections()) == 0 })
}

// A slow peer makes requests time out without blocking Abort.
func TestChaos_SlowPeer(t *testing.T) {
	cfg := chaos.Config{Seed: chaosSeed(t), BytesPerSecond: 2000}
	cs := startChaosServer(t, chaos.Config{}, mms.ServerOptions{})
	c, err := iso.Dial(timeout(t), cs.addr, iso.WithDialer(chaos.Dialer(cfg)),
		iso.WithClientDialOptions(mms.DialOptions{RequestTimeout: 30 * time.Millisecond}))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	big := make([]*mms.Value, 0, 64)
	for range cap(big) {
		big = append(big, mms.NewVisibleString("0123456789abcdef0123456789abcdef"))
	}
	_, err = c.Write(context.Background(), mms.WriteRequest{DomainID: dom, ItemID: "v0", Value: mms.NewStructure(big)})
	if !errors.Is(err, mms.ErrInvokeTimeout) && !errors.Is(err, mms.ErrClosed) {
		t.Errorf("Write through a slow link = %v, want ErrInvokeTimeout or ErrClosed", err)
	}
	start := time.Now()
	abortCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = c.Abort(abortCtx)
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("Abort took %v on a slow link", d)
	}
}

// Many clients and goroutines share one server; run with -race.
func TestStress_Concurrent(t *testing.T) {
	cs := startChaosServer(t, chaos.Config{}, mms.ServerOptions{})
	reports := 0
	var reportsMu sync.Mutex
	for id := range 4 {
		t.Run(fmt.Sprintf("client%d", id), func(t *testing.T) {
			t.Parallel()
			c := dial(t, cs.addr)
			c.OnInformationReport(func(*mms.InformationReportIndication) {
				reportsMu.Lock()
				reports++
				reportsMu.Unlock()
			})
			ctx, cancel := context.WithTimeout(context.Background(), chaosDuration())
			defer cancel()
			var wg sync.WaitGroup
			for g := range 4 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if _, err := workload(ctx, c, id*4+g); err != nil {
						t.Errorf("goroutine %d: %v", g, err)
					}
				}()
			}
			// The server pushes reports while requests are in flight.
			for ctx.Err() == nil {
				_ = cs.srv.Broadcast(ctx, &mms.InformationReportRequest{
					Variables: []mms.ObjectName{domName("v0")},
					Values:    []*mms.Value{mms.NewInteger(1)},
				})
				time.Sleep(time.Millisecond)
			}
			wg.Wait()
			if err := c.Err(); err != nil {
				t.Errorf("association lost: %v", err)
			}
		})
	}
	t.Cleanup(func() {
		reportsMu.Lock()
		defer reportsMu.Unlock()
		if reports == 0 {
			t.Error("no InformationReport delivered under load")
		}
	})
	_ = runtime.NumGoroutine
}
