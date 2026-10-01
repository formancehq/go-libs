package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

// fakeBearerSource hands out current until it is invalidated, then mints
// replacement (or fails with refreshErr).
type fakeBearerSource struct {
	mu          sync.Mutex
	current     string
	replacement string
	refreshErr  error
	invalidates []string
}

func (s *fakeBearerSource) Token(context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.current != "" {
		return s.current, nil
	}
	if s.refreshErr != nil {
		return "", s.refreshErr
	}
	s.current = s.replacement

	return s.current, nil
}

func (s *fakeBearerSource) Invalidate(rejected string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.invalidates = append(s.invalidates, rejected)
	if s.current == rejected {
		s.current = ""
	}
}

func (s *fakeBearerSource) invalidated() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.invalidates...)
}

// rtEventLog records the order of limiter and mint events.
type rtEventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *rtEventLog) add(event string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}

func (l *rtEventLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return append([]string(nil), l.events...)
}

type recordingLimiter struct {
	events *rtEventLog
}

func (l *recordingLimiter) Wait(context.Context) error {
	l.events.add("limiter")

	return nil
}

// pacedBearerSource mints through the same limiter the API requests use.
type pacedBearerSource struct {
	mu      sync.Mutex
	token   string
	limiter Limiter
	events  *rtEventLog
}

func (s *pacedBearerSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.token != "" {
		return s.token, nil
	}
	s.events.add("mint")
	if err := s.limiter.Wait(ctx); err != nil {
		return "", err
	}
	s.token = "token"

	return s.token, nil
}

func (s *pacedBearerSource) Invalidate(rejected string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token == rejected {
		s.token = ""
	}
}

type countingLimiter struct {
	calls atomic.Int32
}

func (l *countingLimiter) Wait(context.Context) error {
	l.calls.Add(1)

	return nil
}

// changingBearerSource rotates its token while the request waits on the
// limiter, settling after the second wait.
type changingBearerSource struct {
	calls   atomic.Int32
	limiter *countingLimiter
}

func (s *changingBearerSource) Token(context.Context) (string, error) {
	s.calls.Add(1)
	switch s.limiter.calls.Load() {
	case 0:
		return "old", nil
	case 1:
		return "replacement-1", nil
	default:
		return "replacement-2", nil
	}
}

func (*changingBearerSource) Invalidate(string) {}

// deadlineBearerSource blocks a re-mint until its context ends.
type deadlineBearerSource struct {
	mu      sync.Mutex
	current string
}

func (s *deadlineBearerSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	current := s.current
	s.mu.Unlock()
	if current != "" {
		return current, nil
	}
	<-ctx.Done()

	return "", ctx.Err()
}

func (s *deadlineBearerSource) Invalidate(rejected string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == rejected {
		s.current = ""
	}
}

type countedReadCloser struct {
	io.Reader
	closed *atomic.Int32
}

func (r *countedReadCloser) Close() error {
	r.closed.Add(1)

	return nil
}

func TestBearerReplaysAReplayableNonIdempotentRequestOnce(t *testing.T) {
	t.Parallel()

	source := &fakeBearerSource{current: "old-token", replacement: "new-token"}
	var auth, bodies []string
	base := rtFunc(func(r *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		auth = append(auth, r.Header.Get("Authorization"))
		bodies = append(bodies, string(body))
		status := http.StatusUnauthorized
		if len(auth) == 2 {
			status = http.StatusOK
		}

		return rtResponse(r, status, `{}`), nil
	})
	client := quietRetryClient(RetryConfig{Base: base, Bearer: source, MaxAttempts: 1})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://api.example/mutate", strings.NewReader(`{"value":1}`))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer caller-token")

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, []string{"Bearer old-token", "Bearer new-token"}, auth, "want rejected + replay")
	require.Equal(t, []string{`{"value":1}`, `{"value":1}`}, bodies)
	require.Equal(t, "Bearer caller-token", req.Header.Get("Authorization"), "the caller request was mutated")
	require.Equal(t, []string{"old-token"}, source.invalidated())
}

func TestBearerDoesNotTurnA5xxIntoANonIdempotentRetry(t *testing.T) {
	t.Parallel()

	source := &fakeBearerSource{current: "token", replacement: "fresh"}
	hits := 0
	client := quietRetryClient(RetryConfig{
		Base: rtFunc(func(r *http.Request) (*http.Response, error) {
			hits++

			return rtResponse(r, http.StatusServiceUnavailable, "unavailable"), nil
		}),
		Bearer: source,
	})
	resp, err := rtDoRequest(t, client, http.MethodPost, "https://api.example/mutate", strings.NewReader("body"))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	require.Equal(t, 1, hits, "a non-idempotent 5xx must not replay")
	require.Empty(t, source.invalidated(), "a 5xx must not invalidate the bearer")
}

func TestBearerRecoveryStopsAfterOneReplay(t *testing.T) {
	t.Parallel()

	source := &fakeBearerSource{current: "old", replacement: "new"}
	hits := 0
	client := quietRetryClient(RetryConfig{
		Base: rtFunc(func(r *http.Request) (*http.Response, error) {
			hits++

			return rtResponse(r, http.StatusUnauthorized, "revoked"), nil
		}),
		Bearer: source,
	})
	resp, err := rtDoRequest(t, client, http.MethodGet, "https://api.example/data", nil)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.Equal(t, 2, hits, "want exactly one auth replay")
}

func TestBearerRefreshFailureReturnsTheOriginal401(t *testing.T) {
	t.Parallel()

	source := &fakeBearerSource{current: "old", refreshErr: errors.New("mint failed")}
	client := quietRetryClient(RetryConfig{
		Base: rtFunc(func(r *http.Request) (*http.Response, error) {
			return rtResponse(r, http.StatusUnauthorized, "original rejection"), nil
		}),
		Bearer: source,
	})
	resp, err := rtDoRequest(t, client, http.MethodGet, "https://api.example/data", nil)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.Equal(t, "original rejection", string(body))
}

func TestBearerUnchangedRefreshReturnsTheOriginal401WithoutReplay(t *testing.T) {
	t.Parallel()

	source := &fakeBearerSource{current: "same", replacement: "same"}
	hits := 0
	client := quietRetryClient(RetryConfig{
		Base: rtFunc(func(r *http.Request) (*http.Response, error) {
			hits++

			return rtResponse(r, http.StatusUnauthorized, "original rejection"), nil
		}),
		Bearer: source,
	})
	resp, err := rtDoRequest(t, client, http.MethodGet, "https://api.example/data", nil)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.Equal(t, "original rejection", string(body))
	require.Equal(t, 1, hits, "an unchanged token must not be replayed")
}

func TestBearerDoesNotReplay403OrANonReplayableBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		body   io.Reader
	}{
		{name: "403 remains terminal", status: http.StatusForbidden},
		{name: "non-replayable body", status: http.StatusUnauthorized, body: io.NopCloser(strings.NewReader("body"))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			source := &fakeBearerSource{current: "old", replacement: "new"}
			hits := 0
			client := quietRetryClient(RetryConfig{
				Base: rtFunc(func(r *http.Request) (*http.Response, error) {
					hits++

					return rtResponse(r, tc.status, "rejected"), nil
				}),
				Bearer: source,
			})
			resp, err := rtDoRequest(t, client, http.MethodPost, "https://api.example/data", tc.body)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()
			require.Equal(t, 1, hits, "want no auth replay")
			require.Empty(t, source.invalidated())
		})
	}
}

func TestBearerReplayPreservesOrdinaryRetryAllowance(t *testing.T) {
	t.Parallel()

	source := &fakeBearerSource{current: "old", replacement: "new"}
	statuses := []int{http.StatusUnauthorized, http.StatusServiceUnavailable, http.StatusOK}
	hits := 0
	client := quietRetryClient(RetryConfig{
		Base: rtFunc(func(r *http.Request) (*http.Response, error) {
			status := statuses[hits]
			hits++

			return rtResponse(r, status, `{}`), nil
		}),
		Bearer:      source,
		MaxAttempts: 2,
		BaseDelay:   1,
		MaxDelay:    1,
	})
	resp, err := rtDoRequest(t, client, http.MethodGet, "https://api.example/data", nil)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, 3, hits, "the auth replay must not consume the ordinary retry allowance")
}

func TestBearerMintUsesASharedLimiterBeforeTheAPISlot(t *testing.T) {
	t.Parallel()

	events := &rtEventLog{}
	limiter := &recordingLimiter{events: events}
	source := &pacedBearerSource{limiter: limiter, events: events}
	client := quietRetryClient(RetryConfig{
		Base: rtFunc(func(r *http.Request) (*http.Response, error) {
			require.Equal(t, "Bearer token", r.Header.Get("Authorization"))

			return rtResponse(r, http.StatusOK, `{}`), nil
		}),
		Bearer:  source,
		Limiter: limiter,
	})
	resp, err := rtDoRequest(t, client, http.MethodGet, "https://api.example/data", nil)
	require.NoError(t, err)
	_ = resp.Body.Close()

	require.Equal(t, []string{"mint", "limiter", "limiter"}, events.snapshot(),
		"the token mint must consume the shared limiter before the API reservation")
}

func TestBearerRedirectsStayOnTheInitialRequestOrigin(t *testing.T) {
	t.Parallel()

	source := &fakeBearerSource{current: "secret-token"}
	var crossOriginHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		crossOriginHits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(target.Close)

	var mu sync.Mutex
	var initialAuth, sameOriginAuth string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			mu.Lock()
			initialAuth = r.Header.Get("Authorization")
			mu.Unlock()
			http.Redirect(w, r, "/same-origin", http.StatusFound)
		case "/same-origin":
			mu.Lock()
			sameOriginAuth = r.Header.Get("Authorization")
			mu.Unlock()
			http.Redirect(w, r, target.URL, http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(origin.Close)

	client := quietRetryClient(RetryConfig{Bearer: source})
	resp, err := rtDoRequest(t, client, http.MethodGet, origin.URL+"/start", nil)
	if resp != nil {
		_ = resp.Body.Close()
	}
	mu.Lock()
	require.Equal(t, "Bearer secret-token", initialAuth)
	require.Equal(t, initialAuth, sameOriginAuth, "want the bearer on both same-origin requests")
	mu.Unlock()
	require.ErrorContains(t, err, "bearer authentication refuses a cross-origin redirect")
	require.Zero(t, crossOriginHits.Load(), "the cross-origin redirect reached the wire")

	initial, err := http.NewRequestWithContext(t.Context(), http.MethodGet, origin.URL+"/start", nil)
	require.NoError(t, err)
	foreign, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target.URL, nil)
	require.NoError(t, err)
	foreign.Response = &http.Response{Request: initial}
	returned, err := http.NewRequestWithContext(t.Context(), http.MethodGet, origin.URL+"/returned", nil)
	require.NoError(t, err)
	returned.Response = &http.Response{Request: foreign}
	require.False(t, sameOriginRedirectChain(returned), "origin -> foreign -> origin regained bearer trust")
}

func TestBearerRecoveryIsOneShotAcrossSameOriginRedirects(t *testing.T) {
	t.Parallel()

	source := &fakeBearerSource{current: "old", replacement: "new"}
	var hits atomic.Int32
	client := quietRetryClient(RetryConfig{
		Base: rtFunc(func(r *http.Request) (*http.Response, error) {
			hits.Add(1)
			switch {
			case r.URL.Path == "/start" && r.Header.Get("Authorization") == "Bearer old":
				return rtResponse(r, http.StatusUnauthorized, "expired"), nil
			case r.URL.Path == "/start" && r.Header.Get("Authorization") == "Bearer new":
				resp := rtResponse(r, http.StatusFound, "")
				resp.Header.Set("Location", "/next")

				return resp, nil
			case r.URL.Path == "/next" && r.Header.Get("Authorization") == "Bearer new":
				return rtResponse(r, http.StatusUnauthorized, "still rejected"), nil
			default:
				return rtResponse(r, http.StatusForbidden, "unexpected request"), nil
			}
		}),
		Bearer: source,
	})
	resp, err := rtDoRequest(t, client, http.MethodGet, "https://api.example/start", nil)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode, "want the terminal second-hop 401")
	require.EqualValues(t, 3, hits.Load())
	require.Equal(t, []string{"old"}, source.invalidated(), "want one recovery across the redirect chain")
}

// TestBearerReplayBudgetDoesNotCancelTheResponseBody pins that the MaxElapsed
// budget bounding the re-mint is not the context of the replay's body read:
// the body keeps streaming after the budget has expired. Not parallel: the
// replay must start inside the budget.
func TestBearerReplayBudgetDoesNotCancelTheResponseBody(t *testing.T) {
	const budget = 100 * time.Millisecond
	source := &fakeBearerSource{current: "old", replacement: "new"}
	headersSent := make(chan struct{})
	releaseBody := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer old" {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("test response writer cannot flush headers")

			return
		}
		flusher.Flush()
		close(headersSent)
		<-releaseBody
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(server.Close)

	client := quietRetryClient(RetryConfig{Bearer: source, MaxElapsed: budget})
	type result struct {
		out map[string]bool
		err error
	}
	done := make(chan result, 1)
	go func() {
		out := map[string]bool{}
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
		if err != nil {
			done <- result{err: err}

			return
		}
		resp, err := client.Do(req)
		if err != nil {
			done <- result{err: err}

			return
		}
		defer func() { _ = resp.Body.Close() }()
		err = json.NewDecoder(resp.Body).Decode(&out)
		done <- result{out: out, err: err}
	}()
	<-headersSent
	select {
	case got := <-done:
		t.Fatalf("response body ended before release: %#v", got)
	case <-time.After(budget + budget/2):
	}
	close(releaseBody)
	got := <-done
	require.NoError(t, got.err, "the auth budget must not cancel the replay's body read")
	require.True(t, got.out["ok"])
}

func TestBearerRevalidatesAfterLimiterWait(t *testing.T) {
	t.Parallel()

	limiter := &countingLimiter{}
	source := &changingBearerSource{limiter: limiter}
	client := quietRetryClient(RetryConfig{
		Base: rtFunc(func(r *http.Request) (*http.Response, error) {
			require.Equal(t, "Bearer replacement-2", r.Header.Get("Authorization"), "want the stable replacement selected after the limiter")

			return rtResponse(r, http.StatusOK, `{}`), nil
		}),
		Bearer:  source,
		Limiter: limiter,
	})
	resp, err := rtDoRequest(t, client, http.MethodGet, "https://api.example/data", nil)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.EqualValues(t, 4, source.calls.Load(), "want a token re-read after every limiter wait")
	require.EqualValues(t, 3, limiter.calls.Load(), "want limiter slots until the token remains stable")
}

// unstableBearerSource never returns the same token twice.
type unstableBearerSource struct{ n atomic.Int32 }

func (s *unstableBearerSource) Token(context.Context) (string, error) {
	return fmt.Sprintf("t-%d", s.n.Add(1)), nil
}

func (*unstableBearerSource) Invalidate(string) {}

// TestBearerStabilizationIsBounded pins the churn bound: a token source that
// never settles must fail the request rather than loop on the limiter.
func TestBearerStabilizationIsBounded(t *testing.T) {
	t.Parallel()

	limiter := &countingLimiter{}
	client := quietRetryClient(RetryConfig{
		Base: rtFunc(func(*http.Request) (*http.Response, error) {
			t.Error("an unstable token must not reach the wire")

			return nil, errors.New("unreachable")
		}),
		Bearer:  &unstableBearerSource{},
		Limiter: limiter,
	})
	resp, err := rtDoRequest(t, client, http.MethodGet, "https://api.example/data", nil)
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.ErrorContains(t, err, "bearer token did not stabilize")
	require.EqualValues(t, maxBearerStabilityChecks, limiter.calls.Load())
}

func TestBearerRefreshHonorsElapsedBudget(t *testing.T) {
	t.Parallel()

	mp, counts := meterFixture(t)
	source := &deadlineBearerSource{current: "old"}
	client := quietRetryClient(RetryConfig{
		Base: rtFunc(func(r *http.Request) (*http.Response, error) {
			return rtResponse(r, http.StatusUnauthorized, "original rejection"), nil
		}),
		Bearer:        source,
		MaxElapsed:    20 * time.Millisecond,
		MeterProvider: mp,
	})
	started := time.Now()
	resp, err := rtDoRequest(t, client, http.MethodGet, "https://api.example/data", nil)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Less(t, time.Since(started), 500*time.Millisecond, "want MaxElapsed-bound recovery")
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.Equal(t, "original rejection", string(body))
	got := counts()
	require.True(t, got.hasAttr(metricRetryExhausted, causeBudget), "budget exhaustion metric = %v", got[metricRetryExhausted])
}

func TestBearerRefreshReturnsCallerCancellation(t *testing.T) {
	t.Parallel()

	source := &deadlineBearerSource{current: "old"}
	client := quietRetryClient(RetryConfig{
		Base: rtFunc(func(r *http.Request) (*http.Response, error) {
			return rtResponse(r, http.StatusUnauthorized, "original rejection"), nil
		}),
		Bearer: source,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example/data", nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.ErrorIs(t, err, context.DeadlineExceeded, "want the caller deadline")
}

func TestBearerReplayChecksBudgetAfterSigningBeforeSend(t *testing.T) {
	t.Parallel()

	// The bubble's fake clock only moves when every goroutine in it is
	// blocked, so the original attempt costs no time and the budget expires
	// exactly while the replay's signer runs: no wall-clock race, no real wait.
	synctest.Test(t, func(t *testing.T) {
		source := &fakeBearerSource{current: "old", replacement: "new"}
		var hits atomic.Int32
		client := quietRetryClient(RetryConfig{
			Base: rtFunc(func(r *http.Request) (*http.Response, error) {
				if hits.Add(1) > 1 {
					t.Error("the replay reached the wire after the auth budget expired")
				}

				return rtResponse(r, http.StatusUnauthorized, "original rejection"), nil
			}),
			Bearer: source,
			Sign: func(*http.Request) error {
				if hits.Load() == 1 {
					// Advances the fake clock past MaxElapsed; returns at once.
					time.Sleep(30 * time.Millisecond)
				}

				return nil
			},
			MaxElapsed: 20 * time.Millisecond,
		})
		resp, err := rtDoRequest(t, client, http.MethodGet, "https://api.example/data", nil)
		if resp != nil {
			_ = resp.Body.Close()
		}
		require.ErrorIs(t, err, context.DeadlineExceeded, "want the auth budget deadline")
		require.EqualValues(t, 1, hits.Load(), "want only the original request on the wire")
	})
}

func TestSignFailureClosesThePreparedAttemptBody(t *testing.T) {
	t.Parallel()

	var closed atomic.Int32
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://api.example/data", strings.NewReader("body"))
	require.NoError(t, err)
	req.GetBody = func() (io.ReadCloser, error) {
		return &countedReadCloser{Reader: strings.NewReader("body"), closed: &closed}, nil
	}
	client := quietRetryClient(RetryConfig{
		Base: rtFunc(func(*http.Request) (*http.Response, error) {
			t.Error("the request reached the base transport after signing failed")

			return nil, errors.New("unreachable")
		}),
		Sign: func(*http.Request) error { return errors.New("sign failed") },
	})
	resp, err := client.Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.ErrorContains(t, err, "sign failed")
	require.EqualValues(t, 1, closed.Load(), "the prepared body must be closed once")
}

// TestBearerRefreshIsObservableWithoutLeakingTokens pins that every re-mint
// outcome is counted and logged with a credential-safe error kind, and that no
// token, secret or path ever reaches the log.
func TestBearerRefreshIsObservableWithoutLeakingTokens(t *testing.T) {
	t.Parallel()

	mp, counts := meterFixture(t)
	logs := &rtSyncBuffer{}
	logger := rtBufferLogger(logs)

	run := func(source *fakeBearerSource, statuses ...int) {
		t.Helper()
		hit := 0
		client := NewRetryClient(RetryConfig{
			Base: rtFunc(func(r *http.Request) (*http.Response, error) {
				status := statuses[hit]
				hit++

				return rtResponse(r, status, "rejected"), nil
			}),
			Bearer:        source,
			Logger:        logger,
			MeterProvider: mp,
		})
		resp, err := rtDoRequest(t, client, http.MethodGet, "https://api.example/secret-path", nil)
		require.NoError(t, err)
		_ = resp.Body.Close()
	}

	run(&fakeBearerSource{current: "secret-old", replacement: "secret-new"}, http.StatusUnauthorized, http.StatusOK)
	run(&fakeBearerSource{current: "secret-failed", refreshErr: errors.New("mint failed with client_secret=error-secret")}, http.StatusUnauthorized)
	run(&fakeBearerSource{current: "secret-api", refreshErr: fmt.Errorf("oauth2cc: %w", &oauth2.RetrieveError{
		Response: &http.Response{StatusCode: http.StatusUnauthorized},
		Body:     []byte(`{"client_secret":"api-secret"}`),
	})}, http.StatusUnauthorized)

	got := counts()
	require.EqualValues(t, 3, got.total(metricBearerRefreshes), "bearer refresh metrics = %v", got[metricBearerRefreshes])
	require.True(t, got.hasAttr(metricBearerRefreshes, attrBearerRefreshOutcome+"=succeeded"))
	require.True(t, got.hasAttr(metricBearerRefreshes, attrBearerRefreshOutcome+"=failed"))
	require.EqualValues(t, 1, got.total(metricRetryCount), "401 retry metrics = %v", got[metricRetryCount])
	require.True(t, got.hasAttr(metricRetryCount, attrRetryReason+"=401"))

	text := logs.String()
	for _, want := range []string{
		"re-mint attempted", "outcome=succeeded", "outcome=failed",
		"error_kind=invalid_token_response", "error_kind=token_endpoint_response", "token_endpoint_status=401",
	} {
		require.Contains(t, text, want)
	}
	for _, secret := range []string{"secret-old", "secret-new", "secret-failed", "secret-api", "error-secret", "api-secret", "client_secret", "secret-path"} {
		require.NotContains(t, text, secret, "a credential leaked into the logs")
	}
}
