// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math"
	"time"
)

// RetryPolicy configures retries with exponential backoff and jitter. It
// is used by transport/iso.WithRetry for dialing, and by [Retry] for any
// operation such as re-establishing a lost association.
//
// The zero value performs a single attempt (no retries). Use
// [DefaultRetryPolicy] for sensible defaults.
type RetryPolicy struct {
	// MaxAttempts is the total number of attempts, including the first.
	// 0 and 1 mean a single attempt; a negative value retries until the
	// context is done.
	MaxAttempts int

	// InitialBackoff is the delay before the second attempt. Zero means
	// 100ms.
	InitialBackoff time.Duration

	// MaxBackoff caps the delay between attempts. Zero means 30s.
	MaxBackoff time.Duration

	// Multiplier is the factor applied to the delay after each attempt.
	// Values below 1, NaN and infinity mean 2.
	Multiplier float64

	// Jitter randomizes each delay by up to this fraction, downwards: a
	// delay d becomes a random value in [d*(1-Jitter), d]. It must be in
	// [0, 1]; 0 disables jitter, and so do NaN and infinite values.
	// Jitter spreads reconnect storms when many clients lose the same
	// server.
	Jitter float64

	// Retryable reports whether err is worth another attempt. Nil means
	// [IsRetryable].
	Retryable func(err error) bool

	// OnRetry, when non-nil, is called before each wait with the 1-based
	// number of the attempt that failed, its error and the delay before
	// the next attempt. Use it for logging or metrics.
	OnRetry func(attempt int, err error, delay time.Duration)
}

// DefaultRetryPolicy returns a policy of 5 attempts with delays starting
// at 200ms, doubling up to 10s, with 20% jitter.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts:    5,
		InitialBackoff: 200 * time.Millisecond,
		MaxBackoff:     10 * time.Second,
		Multiplier:     2,
		Jitter:         0.2,
	}
}

// Backoff returns the delay after the given failed attempt (1-based),
// before jitter.
func (p RetryPolicy) Backoff(attempt int) time.Duration {
	d := p.InitialBackoff
	if d <= 0 {
		d = 100 * time.Millisecond
	}
	maxD := p.MaxBackoff
	if maxD <= 0 {
		maxD = 30 * time.Second
	}
	mult := p.Multiplier
	if !(mult >= 1) || math.IsInf(mult, 1) { // also NaN
		mult = 2
	}
	for i := 1; i < attempt && d < maxD; i++ {
		// Capped in float64: a product beyond MaxBackoff is never
		// converted to a Duration.
		d = time.Duration(min(float64(d)*mult, float64(maxD)))
	}
	return min(d, maxD)
}

func (p RetryPolicy) jittered(d time.Duration) time.Duration {
	j := p.Jitter
	if math.IsNaN(j) || math.IsInf(j, 0) {
		j = 0
	}
	j = min(max(j, 0), 1)
	if j == 0 || d <= 0 {
		return d
	}
	return d - time.Duration(randFloat64()*j*float64(d))
}

// randFloat64 returns a uniform value in [0, 1) from crypto/rand, which
// cannot fail since Go 1.24. Jitter does not need a CSPRNG, but a retry
// is rare enough that the cost does not matter and no seedable source
// can make peers back off in lockstep.
func randFloat64() float64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return float64(binary.BigEndian.Uint64(b[:])>>11) / (1 << 53)
}

// IsRetryable is the default [RetryPolicy.Retryable] classification. It
// returns false for errors that another attempt cannot fix — context
// cancellation, invalid arguments, authentication failures, and
// associations rejected by the peer (a [RejectionError] is retryable
// only when Transient) — and true for everything else
// (refused or dropped connections, timeouts including a per-attempt
// context deadline, protocol failures during the handshake). [Retry]
// stops on its own when its context is done.
func IsRetryable(err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, context.Canceled):
		return false
	case isRejection(err):
		return transientRejection(err)
	case errors.Is(err, ErrInvalidArgument),
		errors.Is(err, ErrAuthenticationFailed),
		errors.Is(err, ErrConnectionRejected):
		return false
	default:
		return true
	}
}

// Retry calls fn until it succeeds, the policy gives up, or ctx is done.
// Each call gets ctx. Between attempts Retry waits according to p; a
// cancelled ctx interrupts the wait. It returns nil on success, otherwise
// the last error from fn (or ctx's error, joined with the last error,
// when ctx ended the retries).
//
// A typical reconnect loop:
//
//	client, err := ... // first dial
//	for {
//	    <-client.Done()
//	    err = mms.Retry(ctx, mms.DefaultRetryPolicy(), func(ctx context.Context) error {
//	        c, err := iso.Dial(ctx, addr)
//	        if err == nil {
//	            client = c
//	        }
//	        return err
//	    })
//	    if err != nil {
//	        return err
//	    }
//	}
func Retry(ctx context.Context, p RetryPolicy, fn func(ctx context.Context) error) error {
	retryable := p.Retryable
	if retryable == nil {
		retryable = IsRetryable
	}
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := fn(ctx)
		if err == nil {
			return nil
		}
		if p.MaxAttempts >= 0 && attempt >= max(p.MaxAttempts, 1) {
			return err
		}
		if !retryable(err) {
			return err
		}
		delay := p.jittered(p.Backoff(attempt))
		if p.OnRetry != nil {
			p.OnRetry(attempt, err, delay)
		}
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return errors.Join(ctx.Err(), err)
		case <-t.C:
		}
	}
}

func isRejection(err error) bool {
	_, ok := errors.AsType[*RejectionError](err)
	return ok
}

// transientRejection reports a [RejectionError] for a temporary refusal
// (congestion, ACSE rejected-transient), which a later attempt may
// overcome.
func transientRejection(err error) bool {
	re, _ := errors.AsType[*RejectionError](err)
	return re.Transient
}
