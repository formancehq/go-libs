package httpclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

// replayableRequest builds a POST whose original body and every GetBody copy
// count their closes separately.
func replayableRequest(t *testing.T, originalClosed, copiesClosed *atomic.Int32) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://api.example/data", nil)
	require.NoError(t, err)
	req.Body = &countedReadCloser{Reader: strings.NewReader("body"), closed: originalClosed}
	req.GetBody = func() (io.ReadCloser, error) {
		return &countedReadCloser{Reader: strings.NewReader("body"), closed: copiesClosed}, nil
	}

	return req
}

// closingBase behaves like net/http's transport: it closes the body it is
// handed, then answers status.
func closingBase(status int) rtFunc {
	return func(r *http.Request) (*http.Response, error) {
		if r.Body != nil {
			_, _ = io.Copy(io.Discard, r.Body)
			_ = r.Body.Close()
		}

		return rtResponse(r, status, "ok"), nil
	}
}

type failingLimiter struct{}

func (failingLimiter) Wait(context.Context) error { return errors.New("limiter closed") }

func TestRoundTripClosesTheOriginalBody(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		cfg        RetryConfig
		wantErr    bool
		wantCopies int32 // GetBody copies Base received and closed
	}{
		// With Sign or Bearer every attempt runs on a clone carrying a GetBody
		// copy, so Base never sees the original.
		{name: "signed success", cfg: RetryConfig{Base: closingBase(http.StatusOK), Sign: func(*http.Request) error { return nil }}, wantCopies: 1},
		{name: "bearer success", cfg: RetryConfig{Base: closingBase(http.StatusOK), Bearer: &fakeBearerSource{current: "tok"}}, wantCopies: 1},
		// Nothing reaches Base at all.
		{name: "limiter failure before send", cfg: RetryConfig{Base: closingBase(http.StatusOK), Limiter: failingLimiter{}}, wantErr: true},
		{name: "bearer failure before send", cfg: RetryConfig{Base: closingBase(http.StatusOK), Bearer: &fakeBearerSource{refreshErr: errors.New("mint failed")}}, wantErr: true},
		// The first unauthenticated attempt hands the original to Base, which
		// closes it; RoundTrip must not close it a second time.
		{name: "unauthenticated first attempt", cfg: RetryConfig{Base: closingBase(http.StatusOK)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var originalClosed, copiesClosed atomic.Int32
			tc.cfg.Logger = rtDiscardLogger()
			resp, err := NewRetryTransport(tc.cfg).RoundTrip(replayableRequest(t, &originalClosed, &copiesClosed))
			if resp != nil {
				_ = resp.Body.Close()
			}
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.EqualValues(t, 1, originalClosed.Load(), "the caller's body is closed exactly once")
			require.EqualValues(t, tc.wantCopies, copiesClosed.Load(), "every GetBody copy Base received is closed by Base")
		})
	}
}

// TestSignFailureClosesASharedBodyOnce: without GetBody the attempt clone
// shares the caller's body, so a failed Sign must leave closing it to
// RoundTrip's own cleanup instead of closing it a second time.
func TestSignFailureClosesASharedBodyOnce(t *testing.T) {
	t.Parallel()

	var closed atomic.Int32
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://api.example/data", nil)
	require.NoError(t, err)
	req.Body = &countedReadCloser{Reader: strings.NewReader("body"), closed: &closed}

	resp, err := NewRetryTransport(RetryConfig{
		Base:   closingBase(http.StatusOK),
		Sign:   func(*http.Request) error { return errors.New("signer unavailable") },
		Logger: rtDiscardLogger(),
	}).RoundTrip(req)
	require.Nil(t, resp)
	require.ErrorContains(t, err, "sign request")
	require.EqualValues(t, 1, closed.Load(), "the shared body is closed exactly once")
}

// TestRetryTransportIsSafeForConcurrentUse shares one transport (bearer source,
// limiter, signer, metrics) across goroutines whose tokens are rejected and
// re-minted under them. Run with -race: the per-request state (attempt count,
// carried token) must not be shared between RoundTrip calls.
func TestRetryTransportIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	source := &fakeBearerSource{current: "old", replacement: "new"}
	var unauthorized atomic.Int32
	transport := NewRetryTransport(RetryConfig{
		Base: rtFunc(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("Authorization") == "Bearer old" {
				unauthorized.Add(1)

				return rtResponse(r, http.StatusUnauthorized, "rejected"), nil
			}

			return rtResponse(r, http.StatusOK, "ok"), nil
		}),
		Bearer:  source,
		Limiter: NewTokenBucket(1e6, 1000),
		Sign: func(r *http.Request) error {
			r.Header.Set("X-Signed", "1")

			return nil
		},
		Logger: rtDiscardLogger(),
	})

	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 25 {
				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://api.example/data", nil)
				if err != nil {
					t.Error(err)

					return
				}
				resp, err := transport.RoundTrip(req)
				if err != nil {
					t.Error(err)

					return
				}
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					t.Errorf("status = %d, want 200 after the shared re-mint", resp.StatusCode)
				}
			}
		})
	}
	wg.Wait()

	require.Positive(t, unauthorized.Load(), "some requests carried the rejected token")
	require.Contains(t, source.invalidated(), "old")
}

// stallingBody blocks every Read until it is closed, like a response whose
// upstream sent headers and then went quiet.
type stallingBody struct {
	closed chan struct{}
	once   sync.Once
	closes atomic.Int32
}

func newStallingBody() *stallingBody { return &stallingBody{closed: make(chan struct{})} }

func (b *stallingBody) Read([]byte) (int, error) {
	<-b.closed

	return 0, io.ErrClosedPipe
}

func (b *stallingBody) Close() error {
	b.closes.Add(1)
	b.once.Do(func() { close(b.closed) })

	return nil
}

// slowLimiter grants the first slot at once and makes every later one wait.
type slowLimiter struct {
	calls atomic.Int32
	delay time.Duration
}

func (l *slowLimiter) Wait(context.Context) error {
	if l.calls.Add(1) > 1 {
		time.Sleep(l.delay)
	}

	return nil
}

// TestRetryBudgetCoversDiscardingAndPacing: MaxElapsed bounds the whole call,
// so a retried response whose body stalls while it is discarded, or a limiter
// wait before the retry, must end the call with context.DeadlineExceeded
// instead of sending another attempt past the budget. The bubble's fake clock
// expires the budget exactly inside the drain or the wait.
func TestRetryBudgetCoversDiscardingAndPacing(t *testing.T) {
	t.Parallel()

	const budget = 100 * time.Millisecond
	for _, tc := range []struct {
		name    string
		stall   bool
		limiter Limiter
	}{
		{name: "body stalls while discarded", stall: true},
		{name: "limiter wait overruns the budget", limiter: &slowLimiter{delay: 2 * budget}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				body := newStallingBody()
				var hits atomic.Int32
				transport := NewRetryTransport(RetryConfig{
					Base: rtFunc(func(r *http.Request) (*http.Response, error) {
						hits.Add(1)
						resp := rtResponse(r, http.StatusServiceUnavailable, "busy")
						if tc.stall {
							resp.Body = body
						}

						return resp, nil
					}),
					Limiter:    tc.limiter,
					BaseDelay:  10 * time.Millisecond,
					MaxDelay:   10 * time.Millisecond,
					MaxElapsed: budget,
					Logger:     rtDiscardLogger(),
				})
				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://api.example/data", nil)
				require.NoError(t, err)

				start := time.Now()
				resp, err := transport.RoundTrip(req)
				require.Nil(t, resp)
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.EqualValues(t, 1, hits.Load(), "no attempt is sent past the budget")
				if tc.stall {
					require.EqualValues(t, 1, body.closes.Load(), "the stalled body is closed exactly once")
					require.Equal(t, budget, time.Since(start), "the call ends when the budget does, without a backoff sleep after it")
				}
			})
		})
	}
}

// blockingLimiter grants the first slot at once and holds every later one
// until its context is done, like a shared limiter throttled after the first
// response. onBlock, when set, runs as a wait starts blocking.
type blockingLimiter struct {
	calls   atomic.Int32
	onBlock func()
}

func (l *blockingLimiter) Wait(ctx context.Context) error {
	if l.calls.Add(1) == 1 {
		return nil
	}
	if l.onBlock != nil {
		l.onBlock()
	}
	<-ctx.Done()

	return ctx.Err()
}

// blockingBearer hands out its token once and then blocks every later Token
// call until its context is done, like a mint stalled behind its own upstream.
type blockingBearer struct {
	calls atomic.Int32
}

func (b *blockingBearer) Token(ctx context.Context) (string, error) {
	if b.calls.Add(1) == 1 {
		return "tok", nil
	}
	<-ctx.Done()

	return "", ctx.Err()
}

func (*blockingBearer) Invalidate(string) {}

// TestRetryPacingIsBoundedByTheBudget: an ordinary retry's bearer token and
// limiter waits run inside what is left of MaxElapsed, so a limiter or a mint
// that would hold the retry indefinitely ends the call at the budget with
// context.DeadlineExceeded, and a token bucket that cannot grant the slot in
// time refuses at once instead of waiting the budget out.
func TestRetryPacingIsBoundedByTheBudget(t *testing.T) {
	t.Parallel()

	const (
		budget  = 100 * time.Millisecond
		backoff = 10 * time.Millisecond
	)
	for _, tc := range []struct {
		name        string
		limiter     Limiter
		bearer      BearerTokenSource
		wantElapsed time.Duration
	}{
		{name: "limiter blocks the retry", limiter: &blockingLimiter{}, wantElapsed: budget},
		{name: "bearer mint blocks the retry", bearer: &blockingBearer{}, wantElapsed: budget},
		{name: "token bucket cannot grant in time", limiter: NewTokenBucket(1, 1), wantElapsed: backoff},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				var hits atomic.Int32
				transport := NewRetryTransport(RetryConfig{
					Base: rtFunc(func(r *http.Request) (*http.Response, error) {
						hits.Add(1)

						return rtResponse(r, http.StatusServiceUnavailable, "busy"), nil
					}),
					Limiter:    tc.limiter,
					Bearer:     tc.bearer,
					BaseDelay:  backoff,
					MaxDelay:   backoff,
					MaxElapsed: budget,
					Logger:     rtDiscardLogger(),
				})
				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://api.example/data", nil)
				require.NoError(t, err)

				start := time.Now()
				resp, err := transport.RoundTrip(req)
				require.Nil(t, resp)
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.ErrorContains(t, err, "retry budget ran out while pacing the retry")
				require.EqualValues(t, 1, hits.Load(), "no attempt is sent past the budget")
				require.LessOrEqual(t, time.Since(start), tc.wantElapsed, "the pacing wait ends with the budget")
			})
		})
	}
}

// TestRetryPacingKeepsCallerCancellation: a caller cancelling while a retry is
// held by the limiter gets its own context.Canceled, not the budget's
// deadline.
func TestRetryPacingKeepsCallerCancellation(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		transport := NewRetryTransport(RetryConfig{
			Base: rtFunc(func(r *http.Request) (*http.Response, error) {
				return rtResponse(r, http.StatusServiceUnavailable, "busy"), nil
			}),
			Limiter:    &blockingLimiter{onBlock: cancel},
			BaseDelay:  10 * time.Millisecond,
			MaxDelay:   10 * time.Millisecond,
			MaxElapsed: time.Minute,
			Logger:     rtDiscardLogger(),
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example/data", nil)
		require.NoError(t, err)

		resp, err := transport.RoundTrip(req)
		require.Nil(t, resp)
		require.ErrorIs(t, err, context.Canceled)
		require.NotErrorIs(t, err, context.DeadlineExceeded)
	})
}

// slowBody serves data one Read after a delay, so a read of it is in progress
// for that long.
type slowBody struct {
	io.Reader
	delay  time.Duration
	closed atomic.Int32
}

func (b *slowBody) Read(p []byte) (int, error) {
	time.Sleep(b.delay)

	return b.Reader.Read(p)
}

func (b *slowBody) Close() error {
	b.closed.Add(1)

	return nil
}

// TestBufferedPolicyReadIsBoundedByTheBudget: with BufferBody set, the policy's
// snapshot of a retryable response is read inside MaxElapsed and the caller's
// context. A body that stalls after its headers is closed when either ends,
// and the call fails with that cause, without another attempt; the response
// could no longer be surfaced intact.
func TestBufferedPolicyReadIsBoundedByTheBudget(t *testing.T) {
	t.Parallel()

	const budget = 100 * time.Millisecond
	for _, tc := range []struct {
		name        string
		cancelAfter time.Duration // 0 = never
		wantErr     error
		wantElapsed time.Duration
	}{
		{name: "budget ends during the read", wantErr: context.DeadlineExceeded, wantElapsed: budget},
		{name: "caller cancels during the read", cancelAfter: budget / 4, wantErr: context.Canceled, wantElapsed: budget / 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if tc.cancelAfter > 0 {
					time.AfterFunc(tc.cancelAfter, cancel)
				}
				body := newStallingBody()
				var hits atomic.Int32
				transport := NewRetryTransport(RetryConfig{
					Base: rtFunc(func(r *http.Request) (*http.Response, error) {
						hits.Add(1)
						resp := rtResponse(r, http.StatusServiceUnavailable, "")
						resp.Body = body

						return resp, nil
					}),
					BufferBody: 1 << 10,
					BaseDelay:  10 * time.Millisecond,
					MaxDelay:   10 * time.Millisecond,
					MaxElapsed: budget,
					Logger:     rtDiscardLogger(),
				})
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example/data", nil)
				require.NoError(t, err)

				start := time.Now()
				resp, err := transport.RoundTrip(req)
				require.Nil(t, resp)
				require.ErrorIs(t, err, tc.wantErr)
				if tc.wantErr == context.Canceled {
					require.NotErrorIs(t, err, context.DeadlineExceeded)
				}
				require.EqualValues(t, 1, hits.Load(), "no attempt is sent after the cut read")
				require.EqualValues(t, 1, body.closes.Load(), "the stalled body is closed exactly once")
				require.Equal(t, tc.wantElapsed, time.Since(start))
			})
		})
	}
}

// TestBufferedPolicyReadSkippedOnceTheBudgetIsSpent: an attempt that completes
// after MaxElapsed cannot be retried, so its response is surfaced untouched
// rather than lost to a snapshot cut at the deadline, however slow its body.
func TestBufferedPolicyReadSkippedOnceTheBudgetIsSpent(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		const budget = 100 * time.Millisecond
		body := &slowBody{Reader: strings.NewReader("late but whole"), delay: time.Millisecond}
		transport := NewRetryTransport(RetryConfig{
			Base: rtFunc(func(r *http.Request) (*http.Response, error) {
				time.Sleep(2 * budget)
				resp := rtResponse(r, http.StatusOK, "")
				resp.Body = body

				return resp, nil
			}),
			BufferBody: 1 << 10,
			MaxElapsed: budget,
			Logger:     rtDiscardLogger(),
		})
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://api.example/data", nil)
		require.NoError(t, err)

		resp, err := transport.RoundTrip(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		got, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, "late but whole", string(got))
		require.Zero(t, body.closed.Load(), "the body is the caller's to close")
	})
}
