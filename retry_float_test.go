// SPDX-License-Identifier: MIT

package mms

import (
	"math"
	"testing"
	"time"
)

// A Multiplier below 1, NaN or infinite means 2; the delay never exceeds
// MaxBackoff.
func TestBackoffMultiplierValues(t *testing.T) {
	base := RetryPolicy{InitialBackoff: time.Second, MaxBackoff: time.Minute}
	for _, tc := range []struct {
		mult float64
		want []time.Duration // attempts 1, 2, 3, 10
	}{
		{0, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, time.Minute}},
		{1, []time.Duration{time.Second, time.Second, time.Second, time.Second}},
		{2, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, time.Minute}},
		{0.5, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, time.Minute}},
		{-1, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, time.Minute}},
		{math.NaN(), []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, time.Minute}},
		{math.Inf(1), []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, time.Minute}},
		{math.Inf(-1), []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, time.Minute}},
		{1e300, []time.Duration{time.Second, time.Minute, time.Minute, time.Minute}},
		{math.MaxFloat64, []time.Duration{time.Second, time.Minute, time.Minute, time.Minute}},
	} {
		p := base
		p.Multiplier = tc.mult
		for i, attempt := range []int{1, 2, 3, 10} {
			if got := p.Backoff(attempt); got != tc.want[i] {
				t.Errorf("Multiplier %v, attempt %d: %v, want %v", tc.mult, attempt, got, tc.want[i])
			}
		}
	}
	if got := DefaultRetryPolicy().Backoff(3); got != 800*time.Millisecond {
		t.Errorf("default policy, attempt 3: %v", got)
	}
}

// A Jitter that is NaN or infinite disables jitter; other values are
// kept in [0, 1], and the delay stays in [d*(1-Jitter), d].
func TestJitterValues(t *testing.T) {
	const d = time.Second
	for _, tc := range []struct {
		jitter float64
		lo     time.Duration
	}{
		{0, d},
		{-1, d},
		{math.NaN(), d},
		{math.Inf(1), d},
		{math.Inf(-1), d},
		{0.2, 800 * time.Millisecond},
		{1, 0},
		{5, 0},
	} {
		p := RetryPolicy{Jitter: tc.jitter}
		for range 100 {
			if got := p.jittered(d); got < tc.lo || got > d {
				t.Fatalf("Jitter %v: %v, want within [%v, %v]", tc.jitter, got, tc.lo, d)
			}
		}
	}
	if got := (RetryPolicy{Jitter: 0.5}).jittered(0); got != 0 {
		t.Errorf("zero delay: %v", got)
	}
}
