package httpclient

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limiter bounds.
const (
	// defaultBurst is the bucket size a limiter gets when none is given.
	defaultBurst = 1
	// maxAdaptiveWindow caps how far ahead a Reset header may push the throttle,
	// so a bogus or epoch-confused value cannot park the limiter for an hour. It
	// doubles as the rate floor: every derived rate is at least one request per
	// window, which keeps the limiter away from rate.Limit(0), where Wait fails
	// outright with "exceeds limiter's burst".
	maxAdaptiveWindow = 2 * time.Minute
	// epochResetThreshold separates the two Reset conventions in the wild: a
	// delta in seconds, or an absolute Unix timestamp. Nothing sane expresses a
	// rate-limit window as a billion-second delta.
	epochResetThreshold = 1_000_000_000
)

// Limiter paces outbound requests. Wait blocks until a slot is available or the
// context is done. NewTokenBucket is a fixed-rate pacer; NewAdaptiveLimiter
// additionally reads upstream rate-limit headers. A Limiter that also
// implements Observer is fed every completed attempt by the retry transport.
type Limiter interface {
	Wait(ctx context.Context) error
}

// Observer is a Limiter that learns from completed responses. The retry
// transport feeds every attempt, including the ones no retry follows, to a
// Limiter implementing it, which is how a limiter reacts to upstream
// rate-limit headers before the next request rather than after the next 429.
type Observer interface {
	Observe(*http.Response)
}

// NewTokenBucket returns a fixed-rate pacer allowing r requests per second
// with the given burst (a burst <= 0 means 1). One bucket can be shared by
// several clients that draw on a single upstream budget, for example
// per-tenant clients signing with the same API key, where per-client buckets
// would multiply the effective rate.
func NewTokenBucket(r float64, burst int) Limiter {
	return newTokenBucket(r, burst)
}

func newTokenBucket(r float64, burst int) *tokenBucket {
	if burst <= 0 {
		burst = defaultBurst
	}

	return &tokenBucket{lim: rate.NewLimiter(rate.Limit(r), burst)}
}

// tokenBucket paces requests at a fixed rate. It is x/time/rate's reservation
// model, which keeps the average rate correct across back-to-back calls: each
// Wait reserves a token and may drive the balance negative, and the wait is
// the time for that debt to be repaid.
type tokenBucket struct {
	lim *rate.Limiter
}

// Wait blocks until a token is available or ctx is done.
func (b *tokenBucket) Wait(ctx context.Context) error {
	return b.lim.Wait(ctx)
}

// AdaptiveLimiter paces at a ceiling rate and slows itself down when the
// upstream reports its remaining budget, so a client backs off BEFORE earning
// a 429 rather than discovering the limit by tripping it.
//
// It reads the IETF draft headers (RateLimit-Remaining / RateLimit-Reset) and
// the widespread X-RateLimit-* spelling, tolerating a Reset expressed either as
// a delta in seconds or as an absolute Unix timestamp. It only ever lowers the
// rate: the configured ceiling stays the upper bound, and the throttle lifts on
// its own once the reported window has turned over.
//
// Pass it as RetryConfig.Limiter; the transport feeds it every completed
// attempt.
type AdaptiveLimiter struct {
	lim     *rate.Limiter
	ceiling rate.Limit
	burst   int

	mu sync.Mutex
	// until is when the current throttle expires; zero means not throttled.
	until time.Time
	// nowFn is a test seam for the WINDOW arithmetic only. The rate.Limiter runs
	// on real time, so anything handed to it must come from time.Now, never
	// from here.
	nowFn func() time.Time
}

var (
	_ Limiter  = (*AdaptiveLimiter)(nil)
	_ Observer = (*AdaptiveLimiter)(nil)
)

// NewAdaptiveLimiter returns a limiter that paces at r requests per second
// (burst <= 0 means 1) and slows down on its own when upstream rate-limit
// headers report shrinking headroom.
func NewAdaptiveLimiter(r float64, burst int) *AdaptiveLimiter {
	if burst <= 0 {
		burst = defaultBurst
	}

	return &AdaptiveLimiter{
		lim:     rate.NewLimiter(rate.Limit(r), burst),
		ceiling: rate.Limit(r),
		burst:   burst,
		nowFn:   time.Now,
	}
}

// Wait blocks until a slot is available or ctx is done, first restoring the
// ceiling if the throttle window has passed.
func (a *AdaptiveLimiter) Wait(ctx context.Context) error {
	a.restoreIfExpired()

	return a.lim.Wait(ctx)
}

// Observe reads the rate-limit headers off a completed response and lowers the
// rate when the reported headroom calls for it. It implements Observer.
func (a *AdaptiveLimiter) Observe(resp *http.Response) {
	if resp == nil {
		return
	}

	remaining, okRemaining := headerInt(resp.Header, "Ratelimit-Remaining", "X-Ratelimit-Remaining")
	if !okRemaining {
		return
	}

	a.mu.Lock()
	now := a.nowFn()
	a.mu.Unlock()

	window, okWindow := headerReset(resp.Header, now, "Ratelimit-Reset", "X-Ratelimit-Reset")
	if !okWindow {
		return
	}

	// Spend the remaining budget evenly across the rest of the window. With
	// nothing left, pace the next request at the window boundary instead of
	// hammering a limit that is already exhausted. Because window is clamped to
	// maxAdaptiveWindow, the quotient can never collapse to zero.
	seconds := window.Seconds()
	target := 1 / seconds
	if remaining > 0 {
		target = float64(remaining) / seconds
	}

	limit := min(rate.Limit(target), a.ceiling)

	a.mu.Lock()
	defer a.mu.Unlock()
	// The rate is only ever lowered here, so one generous reply mid-window
	// cannot undo a throttle an earlier one earned. Only the window elapsing
	// restores the ceiling.
	//
	// The expiry is written in lockstep with the rate, and ONLY here: until
	// always means "when the throttle currently in force stops being
	// justified", so it is set by, and only by, the response that justified
	// that throttle, to that response's own window.
	//
	// Both ways of decoupling them are wrong. Overwriting until on every
	// response lets a looser reply with a short window bring the restore
	// forward and lift a minute-long back-off seconds in. Keeping the later of
	// the two expiries lets a long window outlive the throttle that set it, so a
	// subsequent stricter throttle with a short window inherits it and
	// over-throttles long after the upstream's window has passed.
	//
	// The residual: a renewal of equal strictness does not extend the window, so
	// the throttle lapses at its original expiry and the next response
	// re-derives it. That self-corrects within one request.
	if limit < a.lim.Limit() {
		a.lim.SetLimit(limit)
		a.drainTokens()
		a.until = now.Add(window)
	}
}

// drainTokens spends the whole tokens sitting in the bucket. It must be called
// while holding a.mu, right after lowering the limit.
//
// SetLimit does not touch the balance, so tokens that accrued under the
// previous higher ceiling survive a throttle-down: with one in hand the next
// Wait returns immediately and the new rate takes effect one request late, and
// that request is exactly the one the upstream just said not to send. With a
// burst above 1 it is that many requests.
//
// The reservation is deliberately neither cancelled nor waited on: letting it
// fall out of scope leaves the tokens spent, whereas Cancel would hand them
// back. Only whole tokens are taken, so the drain cannot push the balance
// further negative than the bucket actually held.
//
// time.Now, not nowFn: the rate.Limiter keeps its own real-time clock, and
// feeding it a fake clock would corrupt its accounting.
func (a *AdaptiveLimiter) drainTokens() {
	now := time.Now()

	whole := int(a.lim.TokensAt(now))
	if whole <= 0 {
		return
	}
	// ReserveN above the burst is a no-op, which would silently skip the drain.
	whole = min(whole, a.burst)

	_ = a.lim.ReserveN(now, whole)
}

// restoreIfExpired returns the limiter to its ceiling once the reported window
// has passed, so a throttle is self-healing and never outlives its cause.
func (a *AdaptiveLimiter) restoreIfExpired() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.until.IsZero() || a.nowFn().Before(a.until) {
		return
	}
	a.until = time.Time{}
	a.lim.SetLimit(a.ceiling)
}

// headerInt returns the first of names that parses as a non-negative integer.
func headerInt(h http.Header, names ...string) (int, bool) {
	for _, name := range names {
		v := h.Get(name)
		if v == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			continue
		}

		return n, true
	}

	return 0, false
}

// headerReset returns how long the current rate-limit window has left, reading
// the first of names that parses. The value may be a delta in seconds or an
// absolute Unix timestamp; both appear in the wild, so large values are read as
// the latter. The result is clamped to (0, maxAdaptiveWindow].
func headerReset(h http.Header, now time.Time, names ...string) (time.Duration, bool) {
	secs, ok := headerInt(h, names...)
	if !ok {
		return 0, false
	}

	var window time.Duration
	if secs >= epochResetThreshold {
		window = time.Unix(int64(secs), 0).Sub(now)
	} else {
		window = time.Duration(secs) * time.Second
	}

	if window <= 0 {
		// An already-elapsed window carries no useful pacing signal.
		return 0, false
	}

	return min(window, maxAdaptiveWindow), true
}
