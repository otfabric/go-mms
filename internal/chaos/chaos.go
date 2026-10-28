// SPDX-License-Identifier: MIT

// Package chaos provides a fault-injecting wrapper for net.Conn and
// net.Listener. It is used by tests to drive the MMS client and server
// (over TPKT/COTP/TCP) under hostile network conditions: random latency,
// partial reads, fragmented writes, slow peers, black holes and abrupt
// connection drops in the middle of a PDU.
//
// A zero Config is a transparent pass-through. Typical usage:
//
//	cfg := chaos.Config{Seed: 42, ChunkReads: true, ChunkWrites: true, MaxDelay: time.Millisecond}
//	ln = chaos.WrapListener(ln, cfg)                        // server side
//	dial := chaos.Dialer(cfg)                               // client side
//	conn, err := dial(ctx, "tcp", ln.Addr().String())
//
// Operations that fail because of an injected drop return an error for
// which errors.Is(err, chaos.ErrInjected) is true. Injected stalls and
// delays honour deadlines (returning an error that satisfies
// errors.Is(err, os.ErrDeadlineExceeded) and net.Error.Timeout()) and are
// interrupted by Close.
//
// Determinism: with a non-zero Seed every fault decision is drawn from a
// seeded PCG generator. Reads and writes use independent generators, so the
// sequence of decisions in each direction is reproducible for a given
// sequence of calls. The actual byte counts returned by the underlying
// network (and therefore goroutine scheduling) can still vary between runs;
// use net.Pipe and sequential traffic when exact reproducibility matters.
// With Seed 0 a time-based seed is chosen; it can be retrieved with
// Conn.Seed so a failing run can be replayed.
package chaos

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// ErrInjected is wrapped by every error returned because of an injected
// connection drop. Detect it with errors.Is.
var ErrInjected = errors.New("chaos: injected fault")

// errDropped is the concrete error used for injected drops.
var errDropped = fmt.Errorf("%w: connection dropped", ErrInjected)

// maxChunk bounds the size of a single partial read or write chunk.
const maxChunk = 1024

// Config controls which faults are injected. The zero value is a
// transparent pass-through.
type Config struct {
	// Seed makes all random decisions deterministic. 0 selects a
	// time-based seed (see Conn.Seed).
	Seed int64

	// MaxDelay, when > 0, adds a random delay in [0, MaxDelay) before each
	// Read and Write, and between the chunks of a split Write.
	MaxDelay time.Duration

	// ChunkReads makes each Read return a small random number of bytes
	// (1..min(len(p), 1024)), exercising partial-read handling.
	ChunkReads bool

	// ChunkWrites splits each Write into several smaller writes on the
	// underlying connection (1..1024 bytes each).
	ChunkWrites bool

	// BytesPerSecond, when > 0, throttles each direction independently to
	// roughly this rate (slow peer).
	BytesPerSecond int

	// DropAfterBytes, when > 0, abruptly closes the underlying connection
	// once this many bytes (read + written) have been transferred. With
	// ResetJitter > 0 the drop point is chosen uniformly in
	// [DropAfterBytes, DropAfterBytes+ResetJitter). The point is exact for
	// sequential traffic; concurrent Read and Write may overshoot it by
	// at most one operation.
	DropAfterBytes int64

	// ResetJitter widens the DropAfterBytes drop point (see above).
	ResetJitter int64

	// DropProbability is the per-operation probability (0..1) of abruptly
	// closing the connection. A dropped Write first sends a random prefix
	// of its buffer, simulating a mid-PDU reset.
	DropProbability float64

	// StallProbability is the per-operation probability (0..1) of
	// stalling (black hole) until the deadline expires or the connection
	// is closed.
	StallProbability float64
}

// Stats reports traffic and injected faults of a Conn.
type Stats struct {
	BytesRead    int64 // Bytes returned by Read.
	BytesWritten int64 // Bytes written to the underlying connection.
	Delays       int64 // Injected random delays (MaxDelay).
	Stalls       int64 // Injected stalls (StallProbability).
	Drops        int64 // Injected drops (0 or 1).
	PartialReads int64 // Reads shortened by ChunkReads.
	SplitWrites  int64 // Writes split into more than one chunk.
	Throttles    int64 // Waits caused by BytesPerSecond.
}

// lockedRand is a mutex-guarded deterministic random source.
type lockedRand struct {
	mu sync.Mutex
	r  *rand.Rand
}

func newLockedRand(seed uint64) *lockedRand {
	return &lockedRand{r: rand.New(rand.NewPCG(seed, splitmix(seed)))}
}

func (l *lockedRand) chance(p float64) bool {
	if p <= 0 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.Float64() < p
}

// intN returns a value in [0, n); n must be > 0.
func (l *lockedRand) intN(n int64) int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.Int64N(n)
}

// splitmix is the SplitMix64 finalizer, used to derive independent seeds.
func splitmix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

// deriveSeed derives the n-th child seed from base.
func deriveSeed(base int64, n uint64) int64 {
	s := int64(splitmix(uint64(base) ^ splitmix(n)))
	if s == 0 {
		s = 1
	}
	return s
}

func timeSeed() int64 {
	s := time.Now().UnixNano()
	if s == 0 {
		s = 1
	}
	return s
}

// deadline is a resettable deadline whose changes can be observed.
type deadline struct {
	mu      sync.Mutex
	t       time.Time
	changed chan struct{}
}

func newDeadline() *deadline { return &deadline{changed: make(chan struct{})} }

func (d *deadline) set(t time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.t = t
	close(d.changed)
	d.changed = make(chan struct{})
}

func (d *deadline) get() (time.Time, <-chan struct{}) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.t, d.changed
}

// direction holds per-direction state.
type direction struct {
	op   string // "read" or "write"
	mu   sync.Mutex
	rng  *lockedRand
	dl   *deadline
	next time.Time // throttle: earliest time of the next transfer
}

// Conn is a fault-injecting net.Conn. It is safe for concurrent use.
type Conn struct {
	net.Conn
	cfg    Config
	seed   int64
	dropAt int64 // 0 => no byte-count drop

	rd, wr direction

	done      chan struct{} // closed on Close or drop
	doneOnce  sync.Once
	closeOnce sync.Once
	closeErr  error
	dropped   atomic.Bool
	closed    atomic.Bool

	total                                              atomic.Int64
	bytesRead, bytesWritten                            atomic.Int64
	delays, stalls, drops, partials, splits, throttles atomic.Int64
}

// Wrap returns c wrapped with the fault injection described by cfg.
func Wrap(c net.Conn, cfg Config) *Conn {
	seed := cfg.Seed
	if seed == 0 {
		seed = timeSeed()
	}
	w := &Conn{
		Conn: c,
		cfg:  cfg,
		seed: seed,
		rd:   direction{op: "read", rng: newLockedRand(uint64(deriveSeed(seed, 1))), dl: newDeadline()},
		wr:   direction{op: "write", rng: newLockedRand(uint64(deriveSeed(seed, 2))), dl: newDeadline()},
		done: make(chan struct{}),
	}
	if cfg.DropAfterBytes > 0 {
		w.dropAt = cfg.DropAfterBytes
		if cfg.ResetJitter > 0 {
			w.dropAt += newLockedRand(uint64(deriveSeed(seed, 3))).intN(cfg.ResetJitter)
		}
	}
	return w
}

// Seed returns the effective seed (useful when Config.Seed was 0).
func (c *Conn) Seed() int64 { return c.seed }

// Stats returns a snapshot of the traffic and fault counters.
func (c *Conn) Stats() Stats {
	return Stats{
		BytesRead:    c.bytesRead.Load(),
		BytesWritten: c.bytesWritten.Load(),
		Delays:       c.delays.Load(),
		Stalls:       c.stalls.Load(),
		Drops:        c.drops.Load(),
		PartialReads: c.partials.Load(),
		SplitWrites:  c.splits.Load(),
		Throttles:    c.throttles.Load(),
	}
}

// Close closes the connection. It is idempotent and unblocks pending
// delays and stalls.
func (c *Conn) Close() error {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		c.signalDone()
		err := c.Conn.Close()
		if !c.dropped.Load() {
			c.closeErr = err
		}
	})
	return c.closeErr
}

// SetDeadline sets the read and write deadlines.
func (c *Conn) SetDeadline(t time.Time) error {
	c.rd.dl.set(t)
	c.wr.dl.set(t)
	return c.Conn.SetDeadline(t)
}

// SetReadDeadline sets the read deadline.
func (c *Conn) SetReadDeadline(t time.Time) error {
	c.rd.dl.set(t)
	return c.Conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline.
func (c *Conn) SetWriteDeadline(t time.Time) error {
	c.wr.dl.set(t)
	return c.Conn.SetWriteDeadline(t)
}

func (c *Conn) signalDone() { c.doneOnce.Do(func() { close(c.done) }) }

// drop injects an abrupt connection loss.
func (c *Conn) drop() {
	if c.dropped.CompareAndSwap(false, true) {
		c.drops.Add(1)
		c.signalDone()
		_ = c.Conn.Close()
	}
}

func (c *Conn) opErr(op string, err error) error {
	return &net.OpError{Op: op, Net: c.LocalAddr().Network(), Source: c.LocalAddr(), Addr: c.RemoteAddr(), Err: err}
}

// doneErr is the error for an operation on a closed or dropped conn.
func (c *Conn) doneErr(op string) error {
	if c.dropped.Load() {
		return c.opErr(op, errDropped)
	}
	return c.opErr(op, net.ErrClosed)
}

// mapErr converts errors from the underlying conn after a drop.
func (c *Conn) mapErr(op string, err error) error {
	if err != nil && c.dropped.Load() {
		return c.opErr(op, errDropped)
	}
	return err
}

// wait blocks for d (or forever when stall is true) unless the
// direction's deadline expires or the conn is closed first.
func (c *Conn) wait(dir *direction, d time.Duration, stall bool) error {
	end := time.Now().Add(d)
	for {
		dl, changed := dir.dl.get()
		now := time.Now()
		if !dl.IsZero() && !now.Before(dl) {
			return c.opErr(dir.op, os.ErrDeadlineExceeded)
		}
		if !stall && !now.Before(end) {
			return nil
		}
		wake := end
		if stall || (!dl.IsZero() && dl.Before(end)) {
			wake = dl
		}
		var timerC <-chan time.Time
		var t *time.Timer
		if !wake.IsZero() {
			t = time.NewTimer(wake.Sub(now))
			timerC = t.C
		}
		select {
		case <-timerC:
		case <-changed:
		case <-c.done:
			err := c.doneErr(dir.op)
			if t != nil {
				t.Stop()
			}
			return err
		}
		if t != nil {
			t.Stop()
		}
	}
}

// preOp applies the per-operation faults common to Read and Write. It
// returns dropNow when a probabilistic drop was selected.
func (c *Conn) preOp(dir *direction) (dropNow bool, err error) {
	select {
	case <-c.done:
		return false, c.doneErr(dir.op)
	default:
	}
	if c.dropAt > 0 && c.total.Load() >= c.dropAt {
		c.drop()
		return false, c.opErr(dir.op, errDropped)
	}
	if dir.rng.chance(c.cfg.DropProbability) {
		return true, nil
	}
	if dir.rng.chance(c.cfg.StallProbability) {
		c.stalls.Add(1)
		return false, c.wait(dir, 0, true)
	}
	return false, c.delay(dir)
}

// delay applies a random MaxDelay delay.
func (c *Conn) delay(dir *direction) error {
	if c.cfg.MaxDelay <= 0 {
		return nil
	}
	d := time.Duration(dir.rng.intN(int64(c.cfg.MaxDelay)))
	c.delays.Add(1)
	return c.wait(dir, d, false)
}

// limit caps n by the throttle granularity and the drop budget, and waits
// for the throttle. It must be called with dir.mu held.
func (c *Conn) limit(dir *direction, n int) (int, error) {
	if c.cfg.BytesPerSecond > 0 {
		if g := max(1, c.cfg.BytesPerSecond/50); n > g {
			n = g
		}
		if d := time.Until(dir.next); d > 0 {
			c.throttles.Add(1)
			if err := c.wait(dir, d, false); err != nil {
				return 0, err
			}
		}
	}
	if c.dropAt > 0 {
		rem := c.dropAt - c.total.Load()
		if rem <= 0 {
			c.drop()
			return 0, c.opErr(dir.op, errDropped)
		}
		if int64(n) > rem {
			n = int(rem)
		}
	}
	return n, nil
}

// account records n transferred bytes and triggers DropAfterBytes.
func (c *Conn) account(dir *direction, n int) {
	if n <= 0 {
		return
	}
	if c.cfg.BytesPerSecond > 0 {
		start := dir.next
		if now := time.Now(); start.Before(now) {
			start = now
		}
		dir.next = start.Add(time.Duration(n) * time.Second / time.Duration(c.cfg.BytesPerSecond))
	}
	if c.total.Add(int64(n)) >= c.dropAt && c.dropAt > 0 {
		c.drop()
	}
}

// Read reads data from the connection, injecting faults.
func (c *Conn) Read(p []byte) (int, error) {
	dir := &c.rd
	dir.mu.Lock()
	defer dir.mu.Unlock()

	dropNow, err := c.preOp(dir)
	if err != nil {
		return 0, err
	}
	if dropNow {
		c.drop()
		return 0, c.opErr(dir.op, errDropped)
	}
	if len(p) == 0 {
		return c.Conn.Read(p)
	}
	n := len(p)
	if c.cfg.ChunkReads {
		n = 1 + int(dir.rng.intN(int64(min(n, maxChunk))))
	}
	if n, err = c.limit(dir, n); err != nil {
		return 0, err
	}
	if n < len(p) {
		c.partials.Add(1)
	}
	n, err = c.Conn.Read(p[:n])
	c.bytesRead.Add(int64(n))
	c.account(dir, n)
	return n, c.mapErr(dir.op, err)
}

// Write writes data to the connection, injecting faults. It returns
// len(p), nil or n < len(p) with a non-nil error.
func (c *Conn) Write(p []byte) (int, error) {
	dir := &c.wr
	dir.mu.Lock()
	defer dir.mu.Unlock()

	dropNow, err := c.preOp(dir)
	if err != nil {
		return 0, err
	}
	if dropNow {
		// Send a random prefix first to simulate a mid-PDU reset.
		var n int
		if len(p) > 0 {
			n, _ = c.Conn.Write(p[:dir.rng.intN(int64(len(p)))])
			c.bytesWritten.Add(int64(n))
		}
		c.drop()
		return n, c.opErr(dir.op, errDropped)
	}
	if len(p) == 0 {
		return c.Conn.Write(p)
	}
	written, chunks := 0, 0
	for written < len(p) {
		if c.dropped.Load() {
			return written, c.opErr(dir.op, errDropped)
		}
		n := len(p) - written
		if c.cfg.ChunkWrites {
			n = 1 + int(dir.rng.intN(int64(min(n, maxChunk))))
			if chunks > 0 {
				if err := c.delay(dir); err != nil {
					return written, err
				}
			}
		}
		if n, err = c.limit(dir, n); err != nil {
			return written, err
		}
		n, err = c.Conn.Write(p[written : written+n])
		written += n
		chunks++
		if chunks == 2 {
			c.splits.Add(1)
		}
		c.bytesWritten.Add(int64(n))
		c.account(dir, n)
		if err != nil {
			return written, c.mapErr(dir.op, err)
		}
	}
	return written, nil
}

// listener wraps every accepted connection.
type listener struct {
	net.Listener
	cfg  Config
	base int64
	n    atomic.Uint64
}

// WrapListener returns a listener whose accepted connections are wrapped
// with Wrap. Each connection gets its own seed derived from cfg.Seed (or
// a time-based seed when it is 0) and its accept index, so the i-th
// accepted connection always gets the same seed.
func WrapListener(l net.Listener, cfg Config) net.Listener {
	base := cfg.Seed
	if base == 0 {
		base = timeSeed()
	}
	return &listener{Listener: l, cfg: cfg, base: base}
}

// Accept waits for and returns the next wrapped connection.
func (l *listener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	cfg := l.cfg
	cfg.Seed = deriveSeed(l.base, l.n.Add(1))
	return Wrap(c, cfg), nil
}

// Dialer returns a dial function (compatible with net.Dialer.DialContext)
// that dials with a zero net.Dialer and wraps the result. Each dialed
// connection gets its own seed derived from cfg.Seed and the dial index.
// The returned net.Conn is a *Conn.
func Dialer(cfg Config) func(ctx context.Context, network, addr string) (net.Conn, error) {
	base := cfg.Seed
	if base == 0 {
		base = timeSeed()
	}
	var n atomic.Uint64
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		var d net.Dialer
		c, err := d.DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		cc := cfg
		cc.Seed = deriveSeed(base, n.Add(1))
		return Wrap(c, cc), nil
	}
}
