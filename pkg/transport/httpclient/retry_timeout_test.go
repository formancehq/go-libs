package httpclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestDeriveTimeoutHonorsAnExplicitCeiling pins how the two knobs reconcile:
// an unset TotalTimeout is derived from the retry budget, and an explicit one
// is a hard upper bound that clamps the budget instead of being raised to fit
// it. Raising the ceiling would silently override a deliberate bound, such as
// a handler running inside an inbound request.
func TestDeriveTimeoutHonorsAnExplicitCeiling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		cfg            RetryConfig
		wantTimeout    time.Duration
		wantMaxElapsed time.Duration
		wantPerAttempt time.Duration
	}{
		{
			name:           "unset is derived from the budget",
			cfg:            RetryConfig{},
			wantTimeout:    2*time.Minute + 4*30*time.Second,
			wantMaxElapsed: 2 * time.Minute,
			wantPerAttempt: 30 * time.Second,
		},
		{
			name:           "an inbound-request ceiling is honored, not raised",
			cfg:            RetryConfig{TotalTimeout: 30 * time.Second, MaxAttempts: 3},
			wantTimeout:    30 * time.Second,
			wantMaxElapsed: 30 * time.Second, // clamped down from the 2m default
			wantPerAttempt: 30 * time.Second,
		},
		{
			name:           "a ceiling below the per-attempt deadline clamps both",
			cfg:            RetryConfig{TotalTimeout: 5 * time.Second},
			wantTimeout:    5 * time.Second,
			wantMaxElapsed: 5 * time.Second,
			wantPerAttempt: 5 * time.Second,
		},
		{
			name:           "a ceiling above the budget leaves the budget alone",
			cfg:            RetryConfig{TotalTimeout: 10 * time.Minute},
			wantTimeout:    10 * time.Minute,
			wantMaxElapsed: 2 * time.Minute,
			wantPerAttempt: 30 * time.Second,
		},
		{
			name:           "an explicit budget under an explicit ceiling is untouched",
			cfg:            RetryConfig{TotalTimeout: time.Minute, MaxElapsed: 10 * time.Second},
			wantTimeout:    time.Minute,
			wantMaxElapsed: 10 * time.Second,
			wantPerAttempt: 30 * time.Second,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := quietRetryClient(tc.cfg)
			require.Equal(t, tc.wantTimeout, client.Timeout)

			transport, ok := client.Transport.(*RetryTransport)
			require.True(t, ok, "Transport is %T, want *RetryTransport", client.Transport)
			require.Equal(t, tc.wantTimeout, transport.Timeout())
			require.Equal(t, tc.wantMaxElapsed, transport.cfg.MaxElapsed)
			require.Equal(t, tc.wantPerAttempt, transport.cfg.PerAttemptTimeout)
		})
	}
}

// TestExplicitCeilingStillRetries pins that honoring the ceiling did not cost
// the retries: a short TotalTimeout still allows the attempts inside it.
func TestExplicitCeilingStillRetries(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}
		_, _ = io.WriteString(w, "{}")
	}))
	defer srv.Close()

	client := quietRetryClient(RetryConfig{
		TotalTimeout: 30 * time.Second,
		MaxAttempts:  3,
		BaseDelay:    time.Millisecond,
		MaxDelay:     2 * time.Millisecond,
	})

	resp, err := rtDoRequest(t, client, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.EqualValues(t, 2, hits.Load(), "the 503 must be retried within the ceiling")
}

// TestPerAttemptTimeoutCutsStalledHeaders proves a hung upstream is bounded
// per attempt instead of consuming the whole budget: the server never writes a
// header, so each attempt must be abandoned at PerAttemptTimeout and retried.
func TestPerAttemptTimeoutCutsStalledHeaders(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		// Stall before writing any header until the test tears the server down.
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	c := quietRetryClient(RetryConfig{
		PerAttemptTimeout: 100 * time.Millisecond,
		MaxAttempts:       2,
		BaseDelay:         time.Millisecond,
		MaxElapsed:        5 * time.Second,
	})

	start := time.Now()
	resp, err := rtDoRequest(t, c, http.MethodGet, srv.URL, nil)
	if err == nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err, "want a timeout error")

	// Two attempts of ~100ms, not one attempt that ran until the client timeout.
	require.Less(t, time.Since(start), 2*time.Second, "the per-attempt deadline must cut each try")
	require.EqualValues(t, 2, hits.Load())
}

// TestPerAttemptTimeoutAllowsSlowBody is the other half of the contract: the
// deadline bounds the wait for headers only, so a response that streams its
// body slowly past the deadline still reads in full. A per-attempt context
// deadline (the obvious but wrong implementation) would truncate this read.
func TestPerAttemptTimeoutAllowsSlowBody(t *testing.T) {
	t.Parallel()

	const perAttempt = 150 * time.Millisecond
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("ResponseWriter is not a Flusher")

			return
		}
		flusher.Flush()
		// Dribble the body out well past the per-attempt header deadline.
		for range 4 {
			_, _ = w.Write([]byte("chunk"))
			flusher.Flush()
			time.Sleep(perAttempt / 2)
		}
	}))
	defer srv.Close()

	c := quietRetryClient(RetryConfig{PerAttemptTimeout: perAttempt, MaxAttempts: 1})

	resp, err := rtDoRequest(t, c, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "chunkchunkchunkchunk", string(body), "want the full slow stream")
}

// TestRetryableStatuses pins which statuses the default policy replays: 408,
// 425, 429 and the transient 5xx are retried; 501 is not, because it is a
// capability answer, and neither are ordinary 4xx.
func TestRetryableStatuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status      int
		wantRetries bool
	}{
		{http.StatusRequestTimeout, true},
		{http.StatusTooEarly, true},
		{http.StatusTooManyRequests, true},
		{http.StatusInternalServerError, true},
		{http.StatusBadGateway, true},
		{http.StatusServiceUnavailable, true},
		{http.StatusGatewayTimeout, true},
		{http.StatusNotImplemented, false},
		{http.StatusNotFound, false},
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
	}

	for _, tc := range tests {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			t.Parallel()

			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				hits.Add(1)
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()

			c := quietRetryClient(RetryConfig{
				MaxAttempts: 2,
				BaseDelay:   time.Millisecond,
				MaxDelay:    2 * time.Millisecond,
			})

			resp, err := rtDoRequest(t, c, http.MethodGet, srv.URL, nil)
			require.NoError(t, err)
			_ = resp.Body.Close()

			want := int32(1)
			if tc.wantRetries {
				want = 2
			}
			require.Equal(t, want, hits.Load(), "status %d", tc.status)
		})
	}
}

// TestBaseTransportPoolSizing guards the pool tuning: the stdlib default of 2
// idle connections per host is the throughput ceiling this replaces.
func TestBaseTransportPoolSizing(t *testing.T) {
	t.Parallel()

	tr := NewBaseTransport(0)
	require.Equal(t, defaultMaxIdleConnsPerHost, tr.MaxIdleConnsPerHost)
	require.Equal(t, defaultPerAttemptTimeout, tr.ResponseHeaderTimeout)
	require.Equal(t, defaultIdleConnTimeout, tr.IdleConnTimeout)
	require.True(t, tr.ForceAttemptHTTP2)
	// The clone must not mutate the process-wide default.
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		require.NotEqual(t, defaultMaxIdleConnsPerHost, base.MaxIdleConnsPerHost, "NewBaseTransport mutated http.DefaultTransport")
	}

	require.Equal(t, 5*time.Second, NewBaseTransport(5*time.Second).ResponseHeaderTimeout)
}

// TestCallerBaseUsedVerbatim pins that a caller supplying Base owns the chain:
// the retry transport must not wrap it or graft its own deadlines on.
func TestCallerBaseUsedVerbatim(t *testing.T) {
	t.Parallel()

	var seen atomic.Int32
	base := rtFunc(func(r *http.Request) (*http.Response, error) {
		seen.Add(1)

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("{}")),
			Request:    r,
		}, nil
	})

	c := quietRetryClient(RetryConfig{Base: base})
	rt, ok := c.Transport.(*RetryTransport)
	require.True(t, ok, "Transport is %T, want *RetryTransport", c.Transport)
	_, isFunc := rt.cfg.Base.(rtFunc)
	require.True(t, isFunc, "Base is %T, want the caller's rtFunc unwrapped", rt.cfg.Base)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.invalid", nil)
	require.NoError(t, err)
	resp, err := c.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.EqualValues(t, 1, seen.Load())
}
