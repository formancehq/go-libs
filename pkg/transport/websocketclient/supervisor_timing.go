package websocketclient

import (
	"math/rand/v2"
	"time"
)

// DefaultFrameQueueBytes bounds admitted queue payload bytes. A pending Read
// can retain one additional complete frame (up to ReadLimit), and the serial
// handler can retain one admitted frame (up to min(FrameQueueBytes, ReadLimit)).
// It is not a bound on all pending payloads or Go heap. Custom Conn implementations
// must enforce their own read limit for an overall retained-byte bound.
const DefaultFrameQueueBytes = 32 << 20
const budgetChunk = 64 << 10

// Backoff is the delay before reconnect attempt n (0-based): exponential from one
// second, capped at 30, then jittered across the top half of that window.
//
// The jitter matters when several connectors, or several sub-account connections
// within one, are knocked off by the same upstream event: without it they all
// return in lockstep and re-create the thundering herd that dropped them.
func Backoff(attempt int) time.Duration {
	const base, maxBackoff = time.Second, 30 * time.Second

	d := maxBackoff
	if attempt >= 0 && attempt < 30 {
		if v := base << uint(attempt); v > 0 && v < maxBackoff {
			d = v
		}
	}
	half := d / 2

	// Reconnect jitter only; not security-sensitive, so math/rand is fine.
	return half + time.Duration(rand.Int64N(int64(half)+1)) //nolint:gosec // backoff jitter
}

// watchdogTick is how often a staleness window is checked: at most once a second in
// production, and at the window itself when the window is shorter than that, so a
// test can drive a sub-second window without waiting a full second per check. The
// 5ms floor stops a near-zero window becoming a busy loop.
func watchdogTick(window time.Duration) time.Duration {
	if window < time.Second {
		if window < 5*time.Millisecond {
			return 5 * time.Millisecond
		}

		return window
	}

	return time.Second
}
