package httpclient

import (
	"context"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// limiterClock is a manual time source for the adaptive limiter's window
// arithmetic, so the tests assert the decision rather than sleeping through it.
type limiterClock struct {
	now atomic.Pointer[time.Time]
}

func newLimiterClock(t time.Time) *limiterClock {
	var c limiterClock
	c.now.Store(&t)

	return &c
}

func (c *limiterClock) Now() time.Time { return *c.now.Load() }

func (c *limiterClock) advance(d time.Duration) {
	next := c.Now().Add(d)
	c.now.Store(&next)
}

// observeHeaders feeds lim a bodiless response carrying the given headers.
func observeHeaders(lim *AdaptiveLimiter, h map[string]string) {
	header := http.Header{}
	for k, v := range h {
		header.Set(k, v)
	}
	lim.Observe(&http.Response{Header: header})
}

var limiterTestBase = time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)

// newClockedLimiter returns an adaptive limiter whose window arithmetic runs
// on a manual clock starting at limiterTestBase.
func newClockedLimiter(ceiling float64, burst int) (*AdaptiveLimiter, *limiterClock) {
	c := newLimiterClock(limiterTestBase)
	lim := NewAdaptiveLimiter(ceiling, burst)
	lim.nowFn = c.Now

	return lim, c
}

// TestAdaptiveLimiterThrottlesOnShrinkingHeadroom covers the header shapes an
// upstream actually sends, including the two Reset conventions.
func TestAdaptiveLimiterThrottlesOnShrinkingHeadroom(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		headers map[string]string
		ceiling float64
		want    rate.Limit
	}{
		{
			name:    "ietf headers spend the remainder across the window",
			headers: map[string]string{"RateLimit-Remaining": "10", "RateLimit-Reset": "20"},
			ceiling: 100,
			want:    0.5, // 10 requests / 20s
		},
		{
			name:    "vendor x-prefixed spelling is read too",
			headers: map[string]string{"X-RateLimit-Remaining": "4", "X-RateLimit-Reset": "8"},
			ceiling: 100,
			want:    0.5,
		},
		{
			name:    "reset as an absolute unix timestamp",
			headers: map[string]string{"RateLimit-Remaining": "30", "RateLimit-Reset": strconv.FormatInt(limiterTestBase.Add(60*time.Second).Unix(), 10)},
			ceiling: 100,
			want:    0.5, // 30 requests / 60s
		},
		{
			name:    "exhausted budget paces at the window boundary",
			headers: map[string]string{"RateLimit-Remaining": "0", "RateLimit-Reset": "10"},
			ceiling: 100,
			want:    0.1, // one request at the reset
		},
		{
			name:    "headroom above the ceiling never raises the rate",
			headers: map[string]string{"RateLimit-Remaining": "1000", "RateLimit-Reset": "1"},
			ceiling: 5,
			want:    5, // stays at the ceiling
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			lim, _ := newClockedLimiter(tc.ceiling, 1)
			observeHeaders(lim, tc.headers)

			require.Equal(t, tc.want, lim.lim.Limit())
		})
	}
}

// TestAdaptiveLimiterRestoresAfterWindow pins that a throttle is self-healing:
// it lifts once the reported window has turned over, without another response.
func TestAdaptiveLimiterRestoresAfterWindow(t *testing.T) {
	t.Parallel()

	lim, c := newClockedLimiter(50, 1)

	observeHeaders(lim, map[string]string{"RateLimit-Remaining": "1", "RateLimit-Reset": "10"})
	require.Equal(t, rate.Limit(0.1), lim.lim.Limit(), "throttled limit")

	// Still inside the window: the throttle holds.
	c.advance(9 * time.Second)
	lim.restoreIfExpired()
	require.Equal(t, rate.Limit(0.1), lim.lim.Limit(), "the throttle must hold mid-window")

	// Past the window: back to the ceiling.
	c.advance(2 * time.Second)
	lim.restoreIfExpired()
	require.Equal(t, rate.Limit(50), lim.lim.Limit(), "the ceiling must be restored after the window")
}

// TestAdaptiveLimiterKeepsTheLowestThrottle guards against a generous response
// mid-window undoing a throttle an earlier one earned.
func TestAdaptiveLimiterKeepsTheLowestThrottle(t *testing.T) {
	t.Parallel()

	lim, _ := newClockedLimiter(100, 1)

	observeHeaders(lim, map[string]string{"RateLimit-Remaining": "1", "RateLimit-Reset": "10"})
	observeHeaders(lim, map[string]string{"RateLimit-Remaining": "50", "RateLimit-Reset": "10"})

	require.Equal(t, rate.Limit(0.1), lim.lim.Limit(), "the stricter throttle must survive")
}

// TestAdaptiveLimiterIgnoresUnusableHeaders pins that upstream-controlled input
// cannot push the limiter somewhere absurd. Every case must leave the ceiling.
func TestAdaptiveLimiterIgnoresUnusableHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		headers map[string]string
	}{
		{"no headers at all", map[string]string{}},
		{"remaining without reset", map[string]string{"RateLimit-Remaining": "5"}},
		{"reset without remaining", map[string]string{"RateLimit-Reset": "5"}},
		{"unparseable remaining", map[string]string{"RateLimit-Remaining": "lots", "RateLimit-Reset": "5"}},
		{"negative remaining", map[string]string{"RateLimit-Remaining": "-1", "RateLimit-Reset": "5"}},
		{"zero window", map[string]string{"RateLimit-Remaining": "5", "RateLimit-Reset": "0"}},
		{"reset already in the past", map[string]string{"RateLimit-Remaining": "5", "RateLimit-Reset": strconv.FormatInt(limiterTestBase.Add(-time.Hour).Unix(), 10)}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			lim, _ := newClockedLimiter(20, 1)
			observeHeaders(lim, tc.headers)

			require.Equal(t, rate.Limit(20), lim.lim.Limit(), "the ceiling must stay untouched")
		})
	}
}

// TestAdaptiveLimiterIgnoresANilResponse pins that a transport error, which
// carries no response, is not an observation.
func TestAdaptiveLimiterIgnoresANilResponse(t *testing.T) {
	t.Parallel()

	lim, _ := newClockedLimiter(20, 1)
	lim.Observe(nil)

	require.Equal(t, rate.Limit(20), lim.lim.Limit())
}

// TestAdaptiveLimiterClampsAnAbsurdWindow pins the upper bound: a Reset far in
// the future must not park the client for longer than maxAdaptiveWindow.
func TestAdaptiveLimiterClampsAnAbsurdWindow(t *testing.T) {
	t.Parallel()

	lim, c := newClockedLimiter(100, 1)

	observeHeaders(lim, map[string]string{"RateLimit-Remaining": "0", "RateLimit-Reset": "86400"})

	// Clamped to maxAdaptiveWindow, so the pace is one per 120s, not one per day.
	require.Equal(t, rate.Limit(1/maxAdaptiveWindow.Seconds()), lim.lim.Limit())

	// And the throttle expires at the clamp, not a day out.
	c.advance(maxAdaptiveWindow + time.Second)
	lim.restoreIfExpired()
	require.Equal(t, rate.Limit(100), lim.lim.Limit(), "the ceiling must be restored at the clamp")
}

// TestAdaptiveLimiterDrainsTokensOnThrottleDown is the difference between
// backing off before a 429 and one request after it.
//
// SetLimit does not touch the token balance, so a token accrued under the
// previous higher ceiling survives the throttle-down. Without the drain the
// next Wait returns immediately, sending the one request the upstream just
// said not to send.
func TestAdaptiveLimiterDrainsTokensOnThrottleDown(t *testing.T) {
	t.Parallel()

	lim := NewAdaptiveLimiter(100, 1)
	require.GreaterOrEqual(t, lim.lim.Tokens(), 1.0, "a full burst must be available at construction")

	observeHeaders(lim, map[string]string{"RateLimit-Remaining": "0", "RateLimit-Reset": "60"})

	require.Less(t, lim.lim.Tokens(), 1.0, "the bucket must be drained after throttling to 0 remaining")

	// A Wait that would have to sit out the reported window must refuse rather
	// than pass immediately.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	require.Error(t, lim.Wait(ctx), "Wait passed immediately after the upstream reported 0 remaining")
}

// TestAdaptiveLimiterDrainRespectsBurst pins that the drain scales with the
// burst: a burst of 4 would otherwise let four requests through after a
// throttle-down.
func TestAdaptiveLimiterDrainRespectsBurst(t *testing.T) {
	t.Parallel()

	lim := NewAdaptiveLimiter(100, 4)
	require.GreaterOrEqual(t, lim.lim.Tokens(), 4.0, "the full burst of 4 must be available at construction")

	observeHeaders(lim, map[string]string{"RateLimit-Remaining": "0", "RateLimit-Reset": "60"})

	require.Less(t, lim.lim.Tokens(), 1.0, "every accrued token must be drained, not just one")
}

// TestAdaptiveLimiterDoesNotDrainWhenNotThrottling guards the other direction:
// a response that does not lower the rate must leave the bucket alone, or
// ordinary traffic would pay a drain on every reply.
func TestAdaptiveLimiterDoesNotDrainWhenNotThrottling(t *testing.T) {
	t.Parallel()

	lim := NewAdaptiveLimiter(5, 1)
	before := lim.lim.Tokens()

	// Plenty of headroom: the derived rate sits at the ceiling and nothing is
	// lowered.
	observeHeaders(lim, map[string]string{"RateLimit-Remaining": "1000", "RateLimit-Reset": "1"})

	require.GreaterOrEqual(t, lim.lim.Tokens(), before, "tokens dropped without a throttle-down")
	require.Equal(t, rate.Limit(5), lim.lim.Limit(), "the ceiling must stay untouched")
}

// TestAdaptiveLimiterWindowNeverShortens is the expiry half of "a throttle is
// never weakened by a later response": a looser reply carrying a SHORT window
// must not bring the restore forward. Concurrent in-flight requests make that
// arrival order routine.
func TestAdaptiveLimiterWindowNeverShortens(t *testing.T) {
	t.Parallel()

	lim, c := newClockedLimiter(100, 1)

	// Exhausted, resets in 60s.
	observeHeaders(lim, map[string]string{"RateLimit-Remaining": "0", "RateLimit-Reset": "60"})
	throttled := lim.lim.Limit()
	require.Equal(t, rate.Limit(1.0/60.0), throttled, "want the 1-per-60s throttle")

	// A looser reply with a much shorter window must not bring the restore forward.
	observeHeaders(lim, map[string]string{"RateLimit-Remaining": "1000", "RateLimit-Reset": "1"})
	require.Equal(t, time.Minute, lim.until.Sub(limiterTestBase), "the 1m window must be kept")

	// Two seconds in, the throttle must still be in force.
	c.advance(2 * time.Second)
	lim.restoreIfExpired()
	require.Equal(t, throttled, lim.lim.Limit(), "the throttle must hold two seconds into a 60s window")

	// Past the original window, the ceiling comes back.
	c.advance(59 * time.Second)
	lim.restoreIfExpired()
	require.Equal(t, rate.Limit(100), lim.lim.Limit(), "the ceiling must be restored after the window")
}

// TestAdaptiveLimiterWindowExtends pins the conservative direction: a later
// stricter reply reporting a LONGER window pushes the expiry out.
func TestAdaptiveLimiterWindowExtends(t *testing.T) {
	t.Parallel()

	lim, _ := newClockedLimiter(100, 1)

	observeHeaders(lim, map[string]string{"RateLimit-Remaining": "0", "RateLimit-Reset": "10"})
	require.Equal(t, 10*time.Second, lim.until.Sub(limiterTestBase))

	observeHeaders(lim, map[string]string{"RateLimit-Remaining": "0", "RateLimit-Reset": "45"})
	require.Equal(t, 45*time.Second, lim.until.Sub(limiterTestBase), "the expiry must extend to 45s")
}

// TestAdaptiveLimiterExpiryBelongsToItsThrottle pins the coupling between the
// rate and the expiry: a throttle carries the window of the response that
// justified it, never one left behind by an earlier response.
//
// The first response lowers the rate too (1000/120 is under the ceiling), so
// the stale expiry arrives through the lowering branch, not around it.
func TestAdaptiveLimiterExpiryBelongsToItsThrottle(t *testing.T) {
	t.Parallel()

	lim, c := newClockedLimiter(100, 1)

	// Mild throttle on a long window.
	observeHeaders(lim, map[string]string{"RateLimit-Remaining": "1000", "RateLimit-Reset": "120"})
	require.Equal(t, 2*time.Minute, lim.until.Sub(limiterTestBase), "want the window of the response that set it")

	// A stricter throttle on a SHORT window must bring its own expiry with it.
	observeHeaders(lim, map[string]string{"RateLimit-Remaining": "0", "RateLimit-Reset": "10"})
	require.Equal(t, 10*time.Second, lim.until.Sub(limiterTestBase), "want the 10s window the upstream just reported")

	// Once that window passes the ceiling comes back, rather than the client
	// staying throttled for the rest of the stale window.
	c.advance(11 * time.Second)
	lim.restoreIfExpired()
	require.Equal(t, rate.Limit(100), lim.lim.Limit())
}

// TestAdaptiveLimiterNonThrottlingResponseLeavesExpiry pins that a response
// that does not lower the rate touches neither the rate nor the expiry.
func TestAdaptiveLimiterNonThrottlingResponseLeavesExpiry(t *testing.T) {
	t.Parallel()

	lim, _ := newClockedLimiter(100, 1)

	observeHeaders(lim, map[string]string{"RateLimit-Remaining": "100000", "RateLimit-Reset": "1"})
	require.True(t, lim.until.IsZero(), "no expiry may be recorded for a throttle that does not exist")
	require.Equal(t, rate.Limit(100), lim.lim.Limit())
}

// TestTokenBucketPaces pins the fixed-rate pacer on its own: burst 1 at 20 rps
// lets the first Wait through and makes the next two sit out ~50ms each.
func TestTokenBucketPaces(t *testing.T) {
	t.Parallel()

	bucket := NewTokenBucket(20, 0) // burst <= 0 falls back to 1
	start := time.Now()
	for range 3 {
		require.NoError(t, bucket.Wait(context.Background()))
	}
	require.GreaterOrEqual(t, time.Since(start), 80*time.Millisecond, "the bucket must pace ~50ms per token")
}

// TestRateLimitersReportADeadlineRefusal: x/time/rate refuses at once a wait
// the context deadline cannot cover; both limiters report that refusal as
// context.DeadlineExceeded.
func TestRateLimitersReportADeadlineRefusal(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		lim  Limiter
	}{
		{name: "token bucket", lim: NewTokenBucket(1, 1)},
		{name: "adaptive", lim: NewAdaptiveLimiter(1, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.NoError(t, tc.lim.Wait(context.Background()), "the burst slot is free")
			// The next token is a second away, past this deadline.
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			err := tc.lim.Wait(ctx)
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.NoError(t, ctx.Err(), "refused at once, not after waiting out the deadline")
		})
	}
}
