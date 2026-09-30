package httpclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

// rtFunc adapts a function to http.RoundTripper for the retry tests.
type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// rtResponse builds a canned response to req.
func rtResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

// rtDiscardLogger keeps retry logs out of the test output.
func rtDiscardLogger() logging.Logger { return logging.Testing() }

// rtSyncBuffer is a bytes.Buffer safe for the logger to write while a test
// later reads it.
type rtSyncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *rtSyncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *rtSyncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// rtBufferLogger records Info-and-above logs as key=value text into buf.
func rtBufferLogger(buf *rtSyncBuffer) logging.Logger {
	l := logrus.New()
	l.SetOutput(buf)
	l.SetLevel(logrus.InfoLevel)
	l.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true, DisableColors: true})

	return logging.NewLogrus(l)
}

// quietRetryClient builds a retry client that logs nowhere.
func quietRetryClient(cfg RetryConfig) *http.Client {
	if cfg.Logger == nil {
		cfg.Logger = rtDiscardLogger()
	}

	return NewRetryClient(cfg)
}

// fastRetryClient builds a quiet client whose retry waits are near-instant so
// tests do not sleep for real Retry-After seconds.
func fastRetryClient(cfg RetryConfig) *http.Client {
	cfg.BaseDelay = time.Millisecond
	cfg.MaxDelay = 5 * time.Millisecond

	return quietRetryClient(cfg)
}

func rtDoRequest(t *testing.T, client *http.Client, method, url string, body io.Reader) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, body)
	require.NoError(t, err)

	return client.Do(req)
}

// rtOKServer answers every request with 200 "ok".
func rtOKServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestRetryOn429ThenSuccess(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)

			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	resp, err := rtDoRequest(t, fastRetryClient(RetryConfig{}), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.EqualValues(t, 2, calls.Load(), "want one retry")
}

func TestRetryOn503(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	resp, err := rtDoRequest(t, fastRetryClient(RetryConfig{MaxAttempts: 5}), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.EqualValues(t, 3, calls.Load(), "want 200 after 3 tries")
}

func TestRetryOnTransportError(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	client := fastRetryClient(RetryConfig{Base: rtFunc(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("connection reset by peer")
		}

		return rtResponse(r, http.StatusOK, "ok"), nil
	})})

	resp, err := rtDoRequest(t, client, http.MethodGet, "https://api.example/data", nil)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.EqualValues(t, 2, calls.Load(), "a transport error must be retried")
}

func TestNonIdempotentNotRetried(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	resp, err := rtDoRequest(t, fastRetryClient(RetryConfig{}), http.MethodPost, srv.URL, strings.NewReader("x"))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	require.EqualValues(t, 1, calls.Load(), "POST must not be retried")
}

func TestMaxAttemptsExhaustedReturnsLast(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	resp, err := rtDoRequest(t, fastRetryClient(RetryConfig{MaxAttempts: 3}), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	require.EqualValues(t, 3, calls.Load(), "attempts must be capped")
}

func TestContextCancelStopsWait(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	// A long Retry-After plus a generous elapsed budget: only ctx cancellation
	// should end the wait quickly.
	client := quietRetryClient(RetryConfig{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: time.Minute, MaxElapsed: time.Hour})
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	start := time.Now()
	resp, err := client.Do(req)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	require.Error(t, err, "want a context error")
	require.Less(t, time.Since(start), time.Second, "ctx cancel should abort the retry wait")
}

func TestCanceledRoundTripDoesNotEnterRetryPolicy(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	logs := &rtSyncBuffer{}
	client := quietRetryClient(RetryConfig{
		Base: rtFunc(func(req *http.Request) (*http.Response, error) {
			calls.Add(1)
			cancel()

			return nil, req.Context().Err()
		}),
		Logger:      rtBufferLogger(logs),
		MaxAttempts: 3,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.test/canceled", nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	require.ErrorIs(t, err, context.Canceled)
	require.EqualValues(t, 1, calls.Load())
	require.NotContains(t, logs.String(), "http retry after", "a canceled request entered the retry policy")
}

func TestLimiterPacesRequests(t *testing.T) {
	t.Parallel()

	srv := rtOKServer(t)

	// 20 rps, burst 1: the 1st is immediate, the next 2 cost ~50ms each.
	client := quietRetryClient(RetryConfig{RequestsPerSecond: 20, Burst: 1})
	start := time.Now()
	for range 3 {
		resp, err := rtDoRequest(t, client, http.MethodGet, srv.URL, nil)
		require.NoError(t, err)
		_ = resp.Body.Close()
	}
	require.GreaterOrEqual(t, time.Since(start), 80*time.Millisecond, "the limiter should pace ~50ms/request")
}

// TestNewTokenBucketSharedBudget proves one exported bucket paces several
// clients jointly (a single upstream key budget), instead of each client
// getting its own burst.
func TestNewTokenBucketSharedBudget(t *testing.T) {
	t.Parallel()

	srv := rtOKServer(t)

	bucket := NewTokenBucket(20, 1) // one slot, then 50ms per token
	a := quietRetryClient(RetryConfig{Limiter: bucket})
	b := quietRetryClient(RetryConfig{Limiter: bucket})

	start := time.Now()
	for _, c := range []*http.Client{a, b, a} {
		resp, err := rtDoRequest(t, c, http.MethodGet, srv.URL, nil)
		require.NoError(t, err)
		_ = resp.Body.Close()
	}
	// The second and third must each wait ~50ms. Assert half of that to stay
	// robust on slow CI.
	require.GreaterOrEqual(t, time.Since(start), 50*time.Millisecond, "the shared bucket must pace across clients")
}

// bodyPolicy retries a 429 only when the response body carries the sentinel,
// otherwise it delegates to the embedded default. It exercises BufferBody and
// the composable override.
type bodyPolicy struct {
	RetryPolicy
	sawBody chan []byte
}

func (p bodyPolicy) Retry(a Attempt) (time.Duration, bool) {
	if a.Response != nil && a.Response.StatusCode == http.StatusTooManyRequests &&
		strings.Contains(string(a.Body), `"retry_after"`) {
		select {
		case p.sawBody <- a.Body:
		default:
		}

		return time.Millisecond, true
	}

	return p.RetryPolicy.Retry(a)
}

func TestCustomPolicyBodySignalAndBufferedBody(t *testing.T) {
	t.Parallel()

	const body = `{"error":"rate_limited","retry_after":30}`
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, body)

			return
		}
		_, _ = io.WriteString(w, "final-body")
	}))
	defer srv.Close()

	seen := make(chan []byte, 1)
	client := quietRetryClient(RetryConfig{
		BufferBody: 1 << 10,
		Policy:     bodyPolicy{RetryPolicy: DefaultRetryPolicy{}, sawBody: seen},
		BaseDelay:  time.Millisecond,
		MaxDelay:   5 * time.Millisecond,
	})

	resp, err := rtDoRequest(t, client, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "final-body", string(got), "want the second response")
	require.EqualValues(t, 2, calls.Load(), "the policy must retry on the body signal")
	select {
	case b := <-seen:
		require.Equal(t, body, string(b))
	default:
		t.Fatal("policy never received the buffered body")
	}
}

func TestBufferBodyReadInFull(t *testing.T) {
	t.Parallel()

	// A body larger than BufferBody must still be fully readable by the caller.
	const body = "0123456789abcdefghij"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	resp, err := rtDoRequest(t, quietRetryClient(RetryConfig{BufferBody: 4}), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, body, string(got), "the re-wrapped reader must yield the whole body")
}

func TestDefaultPolicyNonRetryableStatus(t *testing.T) {
	t.Parallel()

	p := DefaultRetryPolicy{}
	resp := &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{}}
	_, retry := p.Retry(Attempt{Response: resp, Count: 1})
	require.False(t, retry, "400 should not be retried")
	resp.StatusCode = http.StatusOK
	_, retry = p.Retry(Attempt{Response: resp, Count: 1})
	require.False(t, retry, "200 should not be retried")
}

// TestDefaultPolicyBackoffIsBoundedAndJittered pins the backoff shape:
// exponential from Base, capped at Max, and never below half of either.
func TestDefaultPolicyBackoffIsBoundedAndJittered(t *testing.T) {
	t.Parallel()

	p := DefaultRetryPolicy{Base: 100 * time.Millisecond, Max: time.Second}
	for attempt, want := range map[int]time.Duration{1: 100 * time.Millisecond, 3: 400 * time.Millisecond, 10: time.Second, 64: time.Second} {
		for range 20 {
			d := p.backoff(attempt)
			require.GreaterOrEqual(t, d, want/2, "attempt %d", attempt)
			require.LessOrEqual(t, d, want, "attempt %d", attempt)
		}
	}
}

// TestRetryConfigIdempotentOptsInAClient pins the client-level counterpart of
// the Idempotent marker: every POST through this client is replayable.
func TestRetryConfigIdempotentOptsInAClient(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	client := fastRetryClient(RetryConfig{
		Base: rtFunc(func(r *http.Request) (*http.Response, error) {
			if calls.Add(1) == 1 {
				return rtResponse(r, http.StatusServiceUnavailable, ""), nil
			}

			return rtResponse(r, http.StatusOK, "ok"), nil
		}),
		Idempotent: func(r *http.Request) bool { return r.URL.Path == "/rpc" },
	})

	resp, err := rtDoRequest(t, client, http.MethodPost, "https://api.example/rpc", strings.NewReader(`{"method":"list"}`))
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.EqualValues(t, 2, calls.Load())

	// The hook never overrides the replayable-body requirement.
	calls.Store(0)
	resp, err = rtDoRequest(t, client, http.MethodPost, "https://api.example/rpc", io.NopCloser(strings.NewReader("x")))
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	require.EqualValues(t, 1, calls.Load(), "a non-replayable body must not be retried")
}

// TestRetryLogsThroughTheContextLogger pins the nil-Logger default: the retry
// line goes to the logger the request context carries, without the path.
func TestRetryLogsThroughTheContextLogger(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	client := NewRetryClient(RetryConfig{
		BaseDelay: time.Millisecond,
		MaxDelay:  time.Millisecond,
		Base: rtFunc(func(r *http.Request) (*http.Response, error) {
			if calls.Add(1) == 1 {
				return rtResponse(r, http.StatusTooManyRequests, ""), nil
			}

			return rtResponse(r, http.StatusOK, "ok"), nil
		}),
	})
	logs := &rtSyncBuffer{}
	ctx := logging.ContextWithLogger(context.Background(), rtBufferLogger(logs))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example/v2/secret-key/items", nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()

	text := logs.String()
	require.Contains(t, text, "http retry after rate limit / server error")
	require.Contains(t, text, "status=429")
	require.Contains(t, text, "host=api.example")
	require.NotContains(t, text, "secret-key", "the path must not reach the retry log")
}

// TestAdaptiveLimiterObservedThroughTheTransport is the integration half of
// the adaptive limiter: the transport must feed it every completed attempt,
// including a 200 that no retry follows, so the throttle lands before the
// next call.
func TestAdaptiveLimiterObservedThroughTheTransport(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Ratelimit-Remaining", "1")
		w.Header().Set("Ratelimit-Reset", "60")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	lim := NewAdaptiveLimiter(100, 1)
	resp, err := rtDoRequest(t, quietRetryClient(RetryConfig{Limiter: lim}), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	_ = resp.Body.Close()

	require.InDelta(t, 1.0/60.0, float64(lim.lim.Limit()), 1e-12, "the transport must feed the 200 to the limiter")
}
