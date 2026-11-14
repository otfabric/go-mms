// SPDX-License-Identifier: MIT

package e2e

import (
	"bytes"
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

func chaosSeed(t *testing.T) uint64 {
	seed := uint64(1)
	if s := os.Getenv("MMS_CHAOS_SEED"); s != "" {
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			t.Fatalf("MMS_CHAOS_SEED: %v", err)
		}
		seed = v
		if seed == 0 {
			seed = uint64(time.Now().UnixNano())
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
		// The end of the run stops the loop between requests, not a
		// request: a deadline that ends during a write ends the
		// association (TIMEOUT_AND_CLOSE.md).
		reqCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
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
				return ok, nil //nolint:nilerr // an expected outcome of chaos ends the workload
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
		wg.Go(func() {
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
		})
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
		wg.Go(func() {
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
		})
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
				wg.Go(func() {
					if _, err := workload(ctx, c, id*4+g); err != nil {
						t.Errorf("goroutine %d: %v", g, err)
					}
				})
			}
			// The server pushes reports while requests are in flight. The
			// end of the run stops the loop, not a report being written.
			reportCtx := timeout(t)
			for ctx.Err() == nil {
				_ = cs.srv.Broadcast(reportCtx, &mms.InformationReportRequest{
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

// Associations whose CONNECT and ACCEPT SPDUs need the extended LI form
// (long ACSE passwords) must survive fragmented, delayed reads and
// writes; a wrong password must be refused, never mis-framed.
func TestChaos_LargeAssociationChurn(t *testing.T) {
	seed := chaosSeed(t)
	cfg := chaos.Config{Seed: seed, ChunkReads: true, ChunkWrites: true, MaxDelay: 200 * time.Microsecond}
	password := bytes.Repeat([]byte("k"), 2000)
	cs := startChaosServer(t, cfg, mms.ServerOptions{
		Authenticate: func(_ context.Context, a *mms.AuthContext) (mms.AuthResult, error) {
			return mms.AuthResult{Accept: bytes.Equal(a.Password, password)}, nil
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), chaosDuration())
	defer cancel()

	var wg sync.WaitGroup
	var accepted, refused atomic.Int64
	for id := range 4 {
		wg.Go(func() {
			clientCfg := cfg
			clientCfg.Seed = seed + uint64(id) + 1
			for i := 0; ctx.Err() == nil; i++ {
				pw := password
				wrong := (id+i)%3 == 0
				if wrong {
					pw = append(bytes.Clone(password), 'x')
				}
				c, err := iso.Dial(timeout(t), cs.addr, iso.WithDialer(chaos.Dialer(clientCfg)),
					iso.WithClientDialOptions(mms.DialOptions{ISO: mms.ISOOptions{Password: pw}}))
				switch {
				case wrong && err == nil:
					_ = c.Abort(context.Background())
					t.Errorf("client %d: wrong password accepted", id)
					return
				case wrong:
					if !errors.Is(err, mms.ErrAssociationFailed) {
						t.Errorf("client %d: wrong password: %v, want ErrAssociationFailed", id, err)
						return
					}
					refused.Add(1)
					continue
				case err != nil:
					t.Errorf("client %d: Dial: %v", id, err)
					return
				}
				if _, err := c.Identify(timeout(t)); err != nil {
					t.Errorf("client %d: Identify: %v", id, err)
				}
				if err := c.Close(timeout(t)); err != nil {
					t.Errorf("client %d: Close: %v", id, err)
				}
				accepted.Add(1)
			}
		})
	}
	wg.Wait()
	if accepted.Load() == 0 || refused.Load() == 0 {
		t.Errorf("accepted %d, refused %d: want both", accepted.Load(), refused.Load())
	}
	t.Logf("%d associations accepted, %d refused", accepted.Load(), refused.Load())
}

// Requests the server refuses with a RejectPDU (a service it does not
// serve: ISO 9506-2 clause 8.6, IEC 61850-8-1 clause 8.1.3.4.1) or with a
// ServiceError (a GetNameList that conflicts with IEC 61850-8-1 Table 15)
// do not tear the association down, also when the PDUs are fragmented
// and delayed: each refusal reaches only the request it answers and the
// next request on the same association succeeds (MMS-PDU-02, MMS-PDU-08).
func TestChaos_RejectsKeepAssociation(t *testing.T) {
	cfg := chaos.Config{Seed: chaosSeed(t), ChunkReads: true, ChunkWrites: true, MaxDelay: 200 * time.Microsecond}
	cs := startChaosServer(t, cfg, mms.ServerOptions{}) // no JournalProvider: ReadJournal is unrecognized
	ctx, cancel := context.WithTimeout(context.Background(), chaosDuration())
	defer cancel()

	var wg sync.WaitGroup
	var rejects, refusals, oks atomic.Int64
	for id := range 4 {
		wg.Go(func() {
			c, err := iso.Dial(timeout(t), cs.addr, iso.WithDialer(chaos.Dialer(cfg)))
			if err != nil {
				t.Errorf("client %d: Dial: %v", id, err)
				return
			}
			defer func() { _ = c.Close(timeout(t)) }()
			for i := 0; ctx.Err() == nil; i++ {
				// The end of the run stops the loop, not a request (see
				// workload).
				reqCtx, cancelReq := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
				switch i % 3 {
				case 0:
					_, err = c.ReadJournalTimeRange(reqCtx, dom, "j", time.Unix(0, 0), time.Now())
					if ctx.Err() != nil {
						break // the run ended during the request
					}
					var re *mms.RejectError
					if !errors.As(err, &re) || re.Type != mms.RejectConfirmedRequest || re.Reason != 1 {
						t.Errorf("client %d: ReadJournal = %v, want Reject unrecognized-service", id, err)
					}
					rejects.Add(1)
				case 1:
					_, err = c.GetNameList(reqCtx, mms.NameListRequest{ObjectClass: mms.ObjectClassJournal, Scope: mms.ObjectScopeAssociation})
					if ctx.Err() != nil {
						break
					}
					var se *mms.ServiceError
					if !errors.As(err, &se) || se.Class != mms.ErrorClassService {
						t.Errorf("client %d: GetNameList = %v, want a service-class ServiceError", id, err)
					}
					refusals.Add(1)
				case 2:
					if _, err = c.ReadObject(reqCtx, domName(fmt.Sprintf("v%d", id))); err != nil && ctx.Err() == nil {
						t.Errorf("client %d: Read after a refusal: %v", id, err)
					}
					oks.Add(1)
				}
				cancelReq()
				if ctx.Err() != nil {
					return
				}
				if c.Err() != nil {
					t.Errorf("client %d: association lost: %v", id, c.Err())
					return
				}
				if t.Failed() {
					return
				}
			}
		})
	}
	wg.Wait()
	if rejects.Load() == 0 || refusals.Load() == 0 || oks.Load() == 0 {
		t.Errorf("rejects=%d refusals=%d reads=%d, want all > 0", rejects.Load(), refusals.Load(), oks.Load())
	}
	t.Logf("%d rejects, %d service errors, %d reads", rejects.Load(), refusals.Load(), oks.Load())
}

// outstandingMeter counts the confirmed requests a client has
// outstanding, from its own wire: +1 for each Confirmed-Request it sends
// (the hook runs before the write), -1 for each answer it receives.
type outstandingMeter struct {
	mu          sync.Mutex
	now, peak   int
	sent, recvd int
}

func (m *outstandingMeter) hook(dir string, raw []byte) {
	c, err := mms.DecodeTSDU(raw)
	if err != nil || c.PDU == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case dir == mms.RawHookSend && c.PDU.Kind == "confirmed-request":
		m.now++
		m.sent++
		m.peak = max(m.peak, m.now)
	case dir == mms.RawHookRecv && c.PDU.HasInvokeID &&
		(c.PDU.Kind == "confirmed-response" || c.PDU.Kind == "confirmed-error" || c.PDU.Kind == "reject"):
		m.now--
		m.recvd++
	}
}

// Many goroutines share one client against a server that negotiates
// max-services-outstanding-calling 2 (ISO 9506-2 clause 8.2), through
// delays and fragmented reads and writes, with some requests abandoned
// by a cancellation shortly after they start: the client never has more
// than 2 confirmed requests outstanding on the wire, every caller
// returns, the association stays up, and no goroutine is left behind
// (goleak in TestMain). Cancellation abandons a request without cutting
// its write; a deadline that ends during a write ends the association
// (TIMEOUT_AND_CLOSE.md).
func TestChaos_OutstandingLimit(t *testing.T) {
	const limit, workers = 2, 32
	cfg := chaos.Config{Seed: chaosSeed(t), ChunkReads: true, ChunkWrites: true, MaxDelay: 200 * time.Microsecond}
	cs := startChaosServer(t, cfg, mms.ServerOptions{MMS: mms.ServerMMSOptions{MaxOutstandingCalling: limit}})
	for _, failFast := range []bool{false, true} {
		t.Run(fmt.Sprintf("failFast=%v", failFast), func(t *testing.T) {
			m := &outstandingMeter{}
			c, err := iso.Dial(timeout(t), cs.addr, iso.WithDialer(chaos.Dialer(cfg)),
				iso.WithClientDialOptions(mms.DialOptions{RawHook: m.hook, MMS: mms.MMSOptions{FailFastOutstanding: failFast}}))
			if err != nil {
				t.Fatalf("Dial: %v", err)
			}
			if got := c.Negotiated().MaxOutCalling; got != limit {
				t.Fatalf("negotiated MaxOutCalling %d, want %d", got, limit)
			}
			ctx, cancel := context.WithTimeout(context.Background(), chaosDuration())
			defer cancel()
			var ok, refused, abandoned atomic.Int64
			var wg sync.WaitGroup
			for g := range workers {
				wg.Go(func() {
					for i := 0; ctx.Err() == nil; i++ {
						reqCtx, reqCancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
						if (g+i)%7 == 0 {
							// Abandoned while waiting or outstanding.
							time.AfterFunc(50*time.Microsecond, reqCancel)
						}
						_, err := c.ReadObject(reqCtx, domName(fmt.Sprintf("v%d", (g+i)%8)))
						reqCancel()
						switch {
						case err == nil:
							ok.Add(1)
						case failFast && errors.Is(err, mms.ErrMaxOutstanding):
							refused.Add(1)
							time.Sleep(50 * time.Microsecond)
						case errors.Is(err, mms.ErrInvokeTimeout) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled):
							abandoned.Add(1)
						default:
							t.Errorf("worker %d: %v", g, err)
							return
						}
					}
				})
			}
			wg.Wait()
			if err := c.Err(); err != nil {
				t.Fatalf("association lost: %v", err)
			}
			if err := c.Close(timeout(t)); err != nil {
				t.Errorf("Close: %v", err)
			}
			m.mu.Lock()
			defer m.mu.Unlock()
			t.Logf("%d ok, %d refused, %d abandoned; %d sent, %d answered, peak %d outstanding", ok.Load(), refused.Load(), abandoned.Load(), m.sent, m.recvd, m.peak)
			if m.peak > limit {
				t.Errorf("peak %d confirmed requests outstanding, negotiated %d", m.peak, limit)
			}
			if ok.Load() == 0 {
				t.Error("no request completed")
			}
			if failFast && refused.Load() == 0 {
				t.Error("FailFastOutstanding refused nothing with 32 workers and a limit of 2")
			}
		})
	}
}
