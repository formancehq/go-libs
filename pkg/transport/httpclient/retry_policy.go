package httpclient

import (
	"context"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"
)

// Attempt is one completed round-trip handed to a RetryPolicy for judgement.
type Attempt struct {
	Request  *http.Request
	Response *http.Response // nil on transport error
	Err      error
	Count    int    // 1-based attempt number (the try that just finished)
	Body     []byte // bounded response-body head, only when RetryConfig.BufferBody > 0
}

// RetryPolicy decides whether to retry a completed attempt and how long to
// wait before the next try. Returning retry=false ends the attempts and the
// last response/error is returned to the caller. A caller overrides the
// generic behavior by supplying its own policy; embedding DefaultRetryPolicy
// and delegating to it keeps the override additive.
//
// The policy is only consulted for requests that are safe to replay (see
// RetryConfig.Idempotent), and never overrides the attempt cap or the
// MaxElapsed budget.
type RetryPolicy interface {
	Retry(a Attempt) (wait time.Duration, retry bool)
}

// DefaultRetryPolicy retries transport errors, 408, 425, 429 and the
// transient 5xx statuses, waiting max(Retry-After, jittered exponential
// backoff) bounded by [Base, Max]. Zero fields fall back to the transport
// defaults (500ms and 30s).
type DefaultRetryPolicy struct {
	Base time.Duration
	Max  time.Duration
}

var _ RetryPolicy = DefaultRetryPolicy{}

// Retry implements RetryPolicy.
func (p DefaultRetryPolicy) Retry(a Attempt) (time.Duration, bool) {
	// A transport error (no response) is retried with plain backoff.
	if a.Err != nil || a.Response == nil {
		return p.backoff(a.Count), true
	}
	if !retryableStatus(a.Response.StatusCode) {
		return 0, false
	}
	if d, ok := parseRetryAfter(a.Response.Header.Get("Retry-After")); ok {
		return capDuration(d, p.baseDelay(), p.maxDelay()), true
	}

	return p.backoff(a.Count), true
}

func (p DefaultRetryPolicy) baseDelay() time.Duration {
	if p.Base <= 0 {
		return defaultBaseDelay
	}

	return p.Base
}

func (p DefaultRetryPolicy) maxDelay() time.Duration {
	if p.Max <= 0 {
		return defaultMaxDelay
	}

	return p.Max
}

// backoff is exponential (base << (attempt-1)) capped by Max, with up-to-half
// jitter so concurrent clients do not resynchronize their retries.
func (p DefaultRetryPolicy) backoff(attempt int) time.Duration {
	base, maxDelay := p.baseDelay(), p.maxDelay()
	d := maxDelay
	if attempt >= 1 && attempt < 31 {
		if v := base << uint(attempt-1); v > 0 && v < maxDelay {
			d = v
		}
	}
	half := d / 2

	// Jitter only; not security-sensitive.
	return half + rand.N(half+1)
}

// retryableStatus reports whether the default policy replays a status.
func retryableStatus(code int) bool {
	switch code {
	case http.StatusRequestTimeout, // upstream gave up on a slow request; a retry is the documented response
		http.StatusTooEarly, // sent too soon during TLS early data; safe to replay
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		// 501 is deliberately absent: it answers "this upstream does not
		// implement the resource", a capability answer that a retry only delays.
		return false
	}
}

// maxRetryAfter caps what a Retry-After header may ask for. An absurd or
// overflowing value (seconds near int64 max would wrap the Duration multiply
// negative) must not poison a policy that trusts the parsed result directly.
const maxRetryAfter = 24 * time.Hour

// parseRetryAfter reads a Retry-After header as integer seconds or an
// HTTP-date, capped at maxRetryAfter.
func parseRetryAfter(v string) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0, false
		}
		if secs > int(maxRetryAfter/time.Second) {
			return maxRetryAfter, true
		}

		return time.Duration(secs) * time.Second, true
	}
	if when, err := http.ParseTime(v); err == nil {
		if d := time.Until(when); d > 0 {
			return min(d, maxRetryAfter), true
		}

		return 0, true
	}

	return 0, false
}

func capDuration(d, floor, ceil time.Duration) time.Duration {
	return min(max(d, floor), ceil)
}

// idempotentKey marks a request whose caller vouches it is safe to retry
// despite a non-idempotent method.
type idempotentKey struct{}

// Idempotent marks req as safe to retry despite a non-idempotent method:
// POST-shaped reads such as JSON-RPC calls or signed listings that mutate
// nothing. The body must be replayable (GetBody set, which http.NewRequest
// does automatically for bytes and strings readers); a non-replayable body
// ignores the mark.
func Idempotent(req *http.Request) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), idempotentKey{}, true))
}

// canRetry reports whether a request is safe to replay: an idempotent method
// (or one vouched for by the Idempotent marker or RetryConfig.Idempotent) with
// a replayable (or absent) body.
func (t *RetryTransport) canRetry(req *http.Request) bool {
	replayable := bodyReplayable(req)
	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return replayable
	default:
		if marked, _ := req.Context().Value(idempotentKey{}).(bool); marked {
			return replayable
		}
		if t.cfg.Idempotent != nil && t.cfg.Idempotent(req) {
			return replayable
		}

		return false
	}
}

// bodyReplayable reports whether every attempt can send the full body: there
// is none, or GetBody can rewind it.
func bodyReplayable(req *http.Request) bool {
	return req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
}
