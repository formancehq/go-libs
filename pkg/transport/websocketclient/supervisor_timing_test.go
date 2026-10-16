package websocketclient

import (
	"testing"
	"time"
)

func TestBackoffIsBoundedAndJittered(t *testing.T) {
	const maxBackoff = 30 * time.Second

	for _, attempt := range []int{-1, 0, 1, 2, 5, 10, 29, 30, 31, 1 << 20} {
		d := Backoff(attempt)
		if d <= 0 {
			t.Errorf("Backoff(%d) = %v, want a positive delay", attempt, d)
		}
		if d > maxBackoff {
			t.Errorf("Backoff(%d) = %v, exceeds the %v cap", attempt, d, maxBackoff)
		}
		// Jitter covers the top half of the window, so the delay is at least half
		// the (capped) exponential step - never zero, which would hot-loop.
		if d < 500*time.Millisecond {
			t.Errorf("Backoff(%d) = %v, below the half-window floor", attempt, d)
		}
	}

	// A negative or overflowing attempt must saturate at the cap rather than wrap.
	for _, attempt := range []int{-1, 30, 31, 1 << 20} {
		if d := Backoff(attempt); d < maxBackoff/2 {
			t.Errorf("Backoff(%d) = %v, want the saturated window", attempt, d)
		}
	}

	// Distinct values across repeated calls: the jitter is real.
	seen := map[time.Duration]bool{}
	for range 50 {
		seen[Backoff(10)] = true
	}
	if len(seen) < 2 {
		t.Error("Backoff(10) returned one value 50 times: the jitter is not applied")
	}
}

func TestWatchdogTickNeverBusyLoops(t *testing.T) {
	for _, tc := range []struct {
		window time.Duration
		want   time.Duration
	}{
		{window: time.Minute, want: time.Second},
		{window: 5 * time.Second, want: time.Second},
		{window: time.Second, want: time.Second},
		{window: 900 * time.Millisecond, want: 900 * time.Millisecond},
		{window: 20 * time.Millisecond, want: 20 * time.Millisecond},
		{window: 5 * time.Millisecond, want: 5 * time.Millisecond},
		{window: time.Millisecond, want: 5 * time.Millisecond},
		{window: 0, want: 5 * time.Millisecond},
		{window: -time.Second, want: 5 * time.Millisecond},
	} {
		if got := watchdogTick(tc.window); got != tc.want {
			t.Errorf("watchdogTick(%v) = %v, want %v", tc.window, got, tc.want)
		}
	}
}
