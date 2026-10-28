// SPDX-License-Identifier: MIT

package mms

import (
	"context"
	"errors"
	"math/rand/v2"
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
	// Values below 1 mean 2.
	Multiplier float64

	// Jitter randomizes each delay by up to this fraction, downwards: a
	// delay d becomes a random value in [d*(1-Jitter), d]. It must be in
	// [0, 1]; 0 disables jitter. Jitter spreads reconnect storms when
	// many clients lose the same server.
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
	if mult < 1 {
		mult = 2
	}
	for i := 1; i < attempt && d < maxD; i++ {
		d = time.Duration(float64(d) * mult)
	}
	return min(d, maxD)
}

func (p RetryPolicy) jittered(d time.Duration) time.Duration {
	j := min(max(p.Jitter, 0), 1)
	if j == 0 || d <= 0 {
		return d
	}
	return d - time.Duration(rand.Float64()*j*float64(d)) //nolint:gosec // jitter does not need a CSPRNG
}

// IsRetryable is the default [RetryPolicy.Retryable] classification. It
// returns false for errors that another attempt cannot fix — context
// cancellation, invalid arguments, authentication failures, and
// associations rejected by the peer — and true for everything else
// (refused or dropped connections, timeouts including a per-attempt
// context deadline, protocol failures during the handshake). [Retry]
// stops on its own when its context is done.
func IsRetryable(err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, context.Canceled):
		return false
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
