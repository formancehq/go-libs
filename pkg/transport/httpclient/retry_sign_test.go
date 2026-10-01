package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSignRunsPerAttempt proves the Sign hook mints fresh credentials for
// every try: a 429 then 200 sequence must see two different nonce values, and
// the caller's original request must stay unmutated.
func TestSignRunsPerAttempt(t *testing.T) {
	t.Parallel()

	var seen []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("X-Nonce"))
		n := len(seen)
		mu.Unlock()
		if n == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)

			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	var nonce atomic.Int32
	client := fastRetryClient(RetryConfig{Sign: func(r *http.Request) error {
		r.Header.Set("X-Nonce", fmt.Sprintf("n-%d", nonce.Add(1)))

		return nil
	}})

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, seen, 2)
	require.NotEmpty(t, seen[0])
	require.NotEqual(t, seen[0], seen[1], "each attempt must carry a distinct nonce")
	require.Empty(t, req.Header.Get("X-Nonce"), "signing leaked into the caller's original request")
}

// TestSignErrorSurfaces proves a failing Sign hook aborts the call instead of
// sending an unsigned request.
func TestSignErrorSurfaces(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("request must not reach the server when signing fails")
	}))
	defer srv.Close()

	client := fastRetryClient(RetryConfig{Sign: func(*http.Request) error { return errors.New("no key") }})
	resp, err := rtDoRequest(t, client, http.MethodGet, srv.URL, nil)
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.ErrorContains(t, err, "sign request")
}

// TestIdempotentPOSTRetried proves the Idempotent mark opts a POST-shaped read
// into retries with its body rewound on every attempt.
func TestIdempotentPOSTRetried(t *testing.T) {
	t.Parallel()

	var bodies []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		n := len(bodies)
		mu.Unlock()
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL, strings.NewReader(`{"method":"list"}`))
	require.NoError(t, err)
	resp, err := fastRetryClient(RetryConfig{}).Do(Idempotent(req))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode, "want 200 after retry")

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{`{"method":"list"}`, `{"method":"list"}`}, bodies, "want the full payload twice")
}

// TestGetBodyRewindOnRetry pins the body rewind: a retried GET carrying a body
// must not replay it consumed (empty) on the second attempt.
func TestGetBodyRewindOnRetry(t *testing.T) {
	t.Parallel()

	var bodies []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		n := len(bodies)
		mu.Unlock()
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, strings.NewReader("query-body"))
	require.NoError(t, err)
	resp, err := fastRetryClient(RetryConfig{}).Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"query-body", "query-body"}, bodies, "want the original payload on both attempts")
}

// TestRetryAfterHTTPDate exercises the HTTP-date branch of Retry-After.
func TestRetryAfterHTTPDate(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", time.Now().Add(50*time.Millisecond).UTC().Format(http.TimeFormat))
			w.WriteHeader(http.StatusTooManyRequests)

			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	start := time.Now()
	client := quietRetryClient(RetryConfig{BaseDelay: time.Millisecond, MaxDelay: 5 * time.Second})
	resp, err := rtDoRequest(t, client, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.EqualValues(t, 2, calls.Load(), "want 200 after one retry")
	// http.TimeFormat has second precision, so the wait rounds down to the
	// backoff floor at worst; only assert the retry happened without waiting a
	// poisoned long duration.
	require.Less(t, time.Since(start), 5*time.Second, "HTTP-date Retry-After waited unreasonably long")
}

// TestParseRetryAfter pins both header forms and the overflow clamp: absurd
// values must not wrap negative or ask a trusting policy for a year-long sleep.
func TestParseRetryAfter(t *testing.T) {
	t.Parallel()

	d, ok := parseRetryAfter("9223372036854775807")
	require.True(t, ok)
	require.Equal(t, maxRetryAfter, d, "max-int seconds must clamp")

	d, ok = parseRetryAfter(time.Now().Add(1000 * time.Hour).UTC().Format(http.TimeFormat))
	require.True(t, ok)
	require.LessOrEqual(t, d, maxRetryAfter, "a far-future date must clamp")

	d, ok = parseRetryAfter("60")
	require.True(t, ok)
	require.Equal(t, time.Minute, d, "plain seconds stay untouched")

	d, ok = parseRetryAfter(time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat))
	require.True(t, ok)
	require.Zero(t, d, "a past date means retry now")

	for _, bad := range []string{"", "-5", "soon"} {
		_, ok = parseRetryAfter(bad)
		require.False(t, ok, "%q must be ignored", bad)
	}
}

// TestRetryAfterIsBoundedByTheDelayWindow pins that the default policy honors
// Retry-After within [Base, Max] rather than trusting it outright.
func TestRetryAfterIsBoundedByTheDelayWindow(t *testing.T) {
	t.Parallel()

	p := DefaultRetryPolicy{Base: time.Second, Max: 10 * time.Second}
	resp := &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}}

	resp.Header.Set("Retry-After", "3")
	d, retry := p.Retry(Attempt{Response: resp, Count: 1})
	require.True(t, retry)
	require.Equal(t, 3*time.Second, d)

	resp.Header.Set("Retry-After", "0")
	d, _ = p.Retry(Attempt{Response: resp, Count: 1})
	require.Equal(t, time.Second, d, "a zero Retry-After is raised to Base")

	resp.Header.Set("Retry-After", "600")
	d, _ = p.Retry(Attempt{Response: resp, Count: 1})
	require.Equal(t, 10*time.Second, d, "a long Retry-After is capped at Max")
}

// TestBodyForSigningLeavesBodySendable pins that the signer reads the body
// through GetBody so the transport can still send it. A signer that consumed
// req.Body would sign correctly and then ship an empty request.
func TestBodyForSigningLeavesBodySendable(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.test/v1/x", strings.NewReader("a=1&b=2"))
	require.NoError(t, err)

	signed, err := BodyForSigning(req)
	require.NoError(t, err)
	require.Equal(t, "a=1&b=2", signed)

	sent, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.Equal(t, "a=1&b=2", string(sent), "req.Body must stay unconsumed")
}

// TestBodyForSigningNoBody pins that a bodiless request signs the empty
// string rather than erroring.
func TestBodyForSigningNoBody(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.test/v1/x", nil)
	require.NoError(t, err)
	got, err := BodyForSigning(req)
	require.NoError(t, err)
	require.Empty(t, got)
}

// TestBodyForSigningRejectsNonReplayableBody: a body without GetBody cannot be
// read without consuming what the send needs, so signing it must fail rather
// than bind the empty string to a nonempty payload. An explicit NoBody still
// signs empty.
func TestBodyForSigningRejectsNonReplayableBody(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.test/v1/x", nil)
	require.NoError(t, err)
	req.Body = io.NopCloser(strings.NewReader("a=1"))
	_, err = BodyForSigning(req)
	require.ErrorIs(t, err, ErrSigningBodyNotReplayable)

	req.Body = http.NoBody
	got, err := BodyForSigning(req)
	require.NoError(t, err)
	require.Empty(t, got)
}

// TestSignWithNonReplayableBodyFailsBeforeSend: a Sign hook routing through
// BodyForSigning surfaces the error as a preparation failure, so the
// unsignable request never reaches the wire.
func TestSignWithNonReplayableBodyFailsBeforeSend(t *testing.T) {
	t.Parallel()

	var sent atomic.Int32
	transport := NewRetryTransport(RetryConfig{
		Base: rtFunc(func(r *http.Request) (*http.Response, error) {
			sent.Add(1)

			return rtResponse(r, http.StatusOK, "ok"), nil
		}),
		Sign: func(r *http.Request) error {
			_, err := BodyForSigning(r)

			return err
		},
		Logger: rtDiscardLogger(),
	})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://example.test/v1/x", nil)
	require.NoError(t, err)
	req.Body = io.NopCloser(strings.NewReader("a=1"))

	resp, err := transport.RoundTrip(req)
	require.Nil(t, resp)
	require.ErrorIs(t, err, ErrSigningBodyNotReplayable)
	require.Zero(t, sent.Load(), "an unsignable request must not be sent")
}

// TestSignRefusesCrossOriginRedirect proves a Sign hook cannot be tricked into
// minting a credential for a host the caller never addressed. net/http builds
// a NEW request for each redirect and calls the transport again, so the
// credential would be freshly signed onto the target, which net/http's own
// strip-on-redirect protection cannot see. A same-origin hop is still signed,
// the cross-origin one fails closed before reaching the wire, and a client
// that mints nothing keeps following redirects anywhere.
func TestSignRefusesCrossOriginRedirect(t *testing.T) {
	t.Parallel()

	var foreignHits atomic.Int32
	var foreignAuth atomic.Value
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		foreignHits.Add(1)
		foreignAuth.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer foreign.Close()

	var mu sync.Mutex
	var sameOriginAuth string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/same-origin", http.StatusFound)
		case "/same-origin":
			mu.Lock()
			sameOriginAuth = r.Header.Get("Authorization")
			mu.Unlock()
			http.Redirect(w, r, foreign.URL+"/download", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer origin.Close()

	signed := fastRetryClient(RetryConfig{Sign: func(r *http.Request) error {
		r.Header.Set("Authorization", "Signature secret-signature")

		return nil
	}})
	resp, err := rtDoRequest(t, signed, http.MethodGet, origin.URL+"/start", nil)
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.ErrorContains(t, err, "request signing refuses a cross-origin redirect")
	require.Zero(t, foreignHits.Load(), "the cross-origin redirect reached the wire carrying %v", foreignAuth.Load())
	mu.Lock()
	got := sameOriginAuth
	mu.Unlock()
	require.Equal(t, "Signature secret-signature", got, "the hop on the initial origin must be signed")

	// The refusal belongs to the credential, not to redirects: nothing is
	// minted here, so the same chain must complete.
	plain, err := rtDoRequest(t, fastRetryClient(RetryConfig{}), http.MethodGet, origin.URL+"/start", nil)
	require.NoError(t, err)
	_ = plain.Body.Close()
	require.EqualValues(t, 1, foreignHits.Load(), "with no credential the redirect must be followed once")
}
