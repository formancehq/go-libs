package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/formancehq/go-libs/v5/pkg/observe/job"
	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

func TestTokenSourceClientAuthScopesAndCache(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		auth       ClientCredentialsAuth
		wantBasic  bool
		wantClient bool
	}{
		{name: "HTTP Basic", auth: ClientCredentialsBasic, wantBasic: true},
		{name: "form body", auth: ClientCredentialsForm, wantClient: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32
			client := &http.Client{Transport: credentialsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("read form: %v", err)
				}
				form, err := url.ParseQuery(string(body))
				if err != nil {
					t.Fatalf("parse form: %v", err)
				}
				if form.Get("grant_type") != "client_credentials" || form.Get("scope") != "ai other" {
					t.Errorf("grant form = %v", form)
				}
				user, pass, basic := r.BasicAuth()
				if basic != tc.wantBasic || (basic && (user != "client" || pass != "secret")) {
					t.Errorf("BasicAuth = (%q, %q, %v)", user, pass, basic)
				}
				if got := form.Get("client_id"); (got != "") != tc.wantClient {
					t.Errorf("client_id = %q", got)
				}

				return credentialsJSONResponse(r, http.StatusOK,
					`{"access_token":"token","expires_in":"3600","scope":"ai other","token_type":"Bearer"}`), nil
			})}
			source := NewClientCredentialsSource(ClientCredentialsConfig{
				HTTPClient:        client,
				TokenURL:          "https://auth.example/token",
				ClientID:          "client",
				ClientSecret:      "secret",
				Scope:             "ai other",
				ClientAuth:        tc.auth,
				RequiredScopes:    []string{"ai"},
				ValidateTokenType: true,
			})

			for range 2 {
				if token, err := source.Token(t.Context()); err != nil || token != "token" {
					t.Fatalf("Token = %q, %v", token, err)
				}
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("token endpoint calls = %d, want one cached mint", got)
			}
			scopes := source.GrantedScopes()
			scopes[0] = "mutated"
			if got := source.GrantedScopes(); len(got) != 2 || got[0] != "ai" {
				t.Fatalf("GrantedScopes leaked mutable state: %v", got)
			}
		})
	}
}

func TestTokenSourceValidatesResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		cfg  ClientCredentialsConfig
		want string
	}{
		{name: "missing token", body: `{"expires_in":60}`, want: "omitted access_token"},
		{name: "missing required scope", body: `{"access_token":"t","scope":"pi"}`, cfg: ClientCredentialsConfig{RequiredScopes: []string{"ai"}}, want: `lacks required "ai"`},
		{name: "unsupported token type", body: `{"access_token":"t","token_type":"MAC"}`, cfg: ClientCredentialsConfig{ValidateTokenType: true}, want: "unsupported token_type"},
		{name: "invalid expiry", body: `{"access_token":"t","expires_in":"later"}`, want: "invalid token response"},
		{name: "lifetime inside expiry skew", body: `{"access_token":"t","expires_in":1}`, cfg: ClientCredentialsConfig{ExpirySkew: time.Minute}, want: "not greater than expiry skew"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := tc.cfg
			cfg.TokenURL = "https://auth.example/token"
			cfg.HTTPClient = &http.Client{Transport: credentialsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				return credentialsJSONResponse(r, http.StatusOK, tc.body), nil
			})}
			_, err := NewClientCredentialsSource(cfg).Token(t.Context())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestTokenSourceSingleFlightsConcurrentMints(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	client := &http.Client{Transport: credentialsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond)

		return credentialsJSONResponse(r, http.StatusOK, `{"access_token":"shared","expires_in":3600}`), nil
	})}
	source := NewClientCredentialsSource(ClientCredentialsConfig{HTTPClient: client, TokenURL: "https://auth.example/token"})

	const goroutines = 24
	start := make(chan struct{})
	errs := make(chan error, goroutines)
	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			<-start
			token, err := source.Token(context.Background())
			if err == nil && token != "shared" {
				err = fmt.Errorf("token = %q", token)
			}
			errs <- err
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("concurrent token endpoint calls = %d, want one", got)
	}
}

func TestTokenSourceSingleFlightsConcurrentMintFailure(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	client := &http.Client{Transport: credentialsRoundTripFunc(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release

		return nil, errors.New("token endpoint unavailable")
	})}
	source := NewClientCredentialsSource(ClientCredentialsConfig{HTTPClient: client, TokenURL: "https://auth.example/token"})

	const goroutines = 12
	start := make(chan struct{})
	joined := make(chan struct{}, goroutines)
	errs := make(chan error, goroutines+1)
	var wg sync.WaitGroup
	wg.Go(func() { _, err := source.Token(context.Background()); errs <- err })
	<-entered
	for range goroutines {
		wg.Go(func() {
			<-start
			_, err := source.Token(&joinedFlightContext{Context: t.Context(), joined: joined})
			errs <- err
		})
	}
	close(start)
	for range goroutines {
		<-joined
	}
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err == nil || !strings.Contains(err.Error(), "token endpoint transport failed") {
			t.Fatalf("shared mint error = %v", err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("concurrent failed token endpoint calls = %d, want one", got)
	}
}

func TestTokenSourceCompareAndClearInvalidation(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	client := &http.Client{Transport: credentialsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		n := calls.Add(1)

		return credentialsJSONResponse(r, http.StatusOK, fmt.Sprintf(`{"access_token":"token-%d","expires_in":3600}`, n)), nil
	})}
	source := NewClientCredentialsSource(ClientCredentialsConfig{HTTPClient: client, TokenURL: "https://auth.example/token"})

	if token, err := source.Token(t.Context()); err != nil || token != "token-1" {
		t.Fatalf("initial Token = %q, %v", token, err)
	}
	source.Invalidate("someone-elses-token")
	if token, err := source.Token(t.Context()); err != nil || token != "token-1" {
		t.Fatalf("unrelated invalidation discarded cache: %q, %v", token, err)
	}
	source.Invalidate("token-1")
	if token, err := source.Token(t.Context()); err != nil || token != "token-2" {
		t.Fatalf("replacement Token = %q, %v", token, err)
	}
	source.Invalidate("token-1")
	if token, err := source.Token(t.Context()); err != nil || token != "token-2" {
		t.Fatalf("stale invalidation discarded replacement: %q, %v", token, err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("token endpoint calls = %d, want 2", got)
	}
}

func TestTokenSourceExpirySkewAndFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		expiresIn  string
		skew       time.Duration
		defaultTTL time.Duration
		wantCalls  int32
	}{
		{name: "missing expiry uses fallback", defaultTTL: time.Hour, wantCalls: 1},
		{name: "null expiry uses fallback", expiresIn: `,"expires_in":null`, defaultTTL: time.Hour, wantCalls: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32
			client := &http.Client{Transport: credentialsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				n := calls.Add(1)

				return credentialsJSONResponse(r, http.StatusOK, fmt.Sprintf(
					`{"access_token":"token-%d"%s}`, n, tc.expiresIn)), nil
			})}
			source := NewClientCredentialsSource(ClientCredentialsConfig{
				HTTPClient: client,
				TokenURL:   "https://auth.example/token",
				ExpirySkew: tc.skew,
				DefaultTTL: tc.defaultTTL,
			})
			for range 2 {
				if _, err := source.Token(t.Context()); err != nil {
					t.Fatalf("Token: %v", err)
				}
			}
			if got := calls.Load(); got != tc.wantCalls {
				t.Fatalf("token endpoint calls = %d, want %d", got, tc.wantCalls)
			}
		})
	}
}

func TestTokenSourceWaiterHonorsItsContext(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	entered := make(chan struct{})
	var once sync.Once
	client := &http.Client{Transport: credentialsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		once.Do(func() { close(entered) })
		<-release

		return credentialsJSONResponse(r, http.StatusOK, `{"access_token":"token","expires_in":3600}`), nil
	})}
	source := NewClientCredentialsSource(ClientCredentialsConfig{HTTPClient: client, TokenURL: "https://auth.example/token"})
	firstDone := make(chan error, 1)
	go func() {
		_, err := source.Token(context.Background())
		firstDone <- err
	}()
	<-entered

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := source.Token(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting Token error = %v, want context deadline", err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("mint owner failed: %v", err)
	}
}

func TestTokenSourceHealthyWaiterReplacesACancelledOwnersFlight(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	firstEntered := make(chan struct{})
	client := &http.Client{Transport: credentialsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(firstEntered)
			<-r.Context().Done()

			return nil, r.Context().Err()
		}

		return credentialsJSONResponse(r, http.StatusOK, `{"access_token":"replacement","expires_in":3600}`), nil
	})}
	source := NewClientCredentialsSource(ClientCredentialsConfig{HTTPClient: client, TokenURL: "https://auth.example/token"})
	ownerCtx, cancelOwner := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelOwner()
	ownerDone := make(chan error, 1)
	go func() {
		_, err := source.Token(ownerCtx)
		ownerDone <- err
	}()
	<-firstEntered

	waiterDone := make(chan struct {
		token string
		err   error
	}, 1)
	go func() {
		token, err := source.Token(context.Background())
		waiterDone <- struct {
			token string
			err   error
		}{token: token, err: err}
	}()

	if err := <-ownerDone; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("mint owner error = %v, want deadline exceeded", err)
	}
	waiter := <-waiterDone
	if waiter.err != nil || waiter.token != "replacement" {
		t.Fatalf("healthy waiter = %q, %v; want replacement token", waiter.token, waiter.err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("token endpoint calls = %d, want cancelled flight plus one replacement", got)
	}
}

func TestTokenSourceSharesATokenClientTimeoutWithHealthyWaiters(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	client := &http.Client{
		Timeout: 50 * time.Millisecond,
		Transport: credentialsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			if calls.Add(1) == 1 {
				close(entered)
			}
			<-r.Context().Done()
			<-release

			return nil, r.Context().Err()
		}),
	}
	source := NewClientCredentialsSource(ClientCredentialsConfig{HTTPClient: client, TokenURL: "https://auth.example/token"})
	ownerDone := make(chan error, 1)
	go func() {
		_, err := source.Token(context.Background())
		ownerDone <- err
	}()
	<-entered

	const waiters = 12
	start := make(chan struct{})
	joined := make(chan struct{}, waiters)
	errs := make(chan error, waiters)
	var wg sync.WaitGroup
	for range waiters {
		wg.Go(func() {
			<-start
			_, err := source.Token(&joinedFlightContext{Context: t.Context(), joined: joined})
			errs <- err
		})
	}
	close(start)
	for range waiters {
		<-joined
	}
	close(release)
	ownerErr := <-ownerDone
	wg.Wait()
	close(errs)
	if ownerErr == nil || !errors.Is(ownerErr, context.DeadlineExceeded) {
		t.Fatalf("mint owner error = %v, want token client deadline", ownerErr)
	}
	for err := range errs {
		if err == nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("shared token client timeout = %v", err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("token endpoint calls = %d, want one shared client-timeout failure", got)
	}
}

func TestConcurrentRejectedAPICallsShareOneReplacementMint(t *testing.T) {
	t.Parallel()

	var grants atomic.Int32
	tokenHTTP := &http.Client{Transport: credentialsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		grant := grants.Add(1)

		return credentialsJSONResponse(r, http.StatusOK, fmt.Sprintf(
			`{"access_token":"token-%d","expires_in":3600}`, grant)), nil
	})}
	source := NewClientCredentialsSource(ClientCredentialsConfig{HTTPClient: tokenHTTP, TokenURL: "https://auth.example/token"})
	if token, err := source.Token(t.Context()); err != nil || token != "token-1" {
		t.Fatalf("initial Token = %q, %v", token, err)
	}

	const callers = 24
	var rejected atomic.Int32
	allRejectedRequestsStarted := make(chan struct{})
	apiHTTP := NewRetryClient(RetryConfig{
		LogPath: true,
		Debug:   &DebugConfig{MaxBodyBytes: -1, MetadataOnly: true},

		Base: credentialsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch r.Header.Get("Authorization") {
			case "Bearer token-1":
				if rejected.Add(1) == callers {
					close(allRejectedRequestsStarted)
				}
				<-allRejectedRequestsStarted

				return credentialsJSONResponse(r, http.StatusUnauthorized, `{"error":"expired"}`), nil
			case "Bearer token-2":
				return credentialsJSONResponse(r, http.StatusOK, `{}`), nil
			default:
				return credentialsJSONResponse(r, http.StatusForbidden, `{}`), nil
			}
		}),
		Bearer:      source,
		MaxAttempts: 1,
		Logger:      logging.NopZap(),
	})

	start := make(chan struct{})
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Go(func() {
			<-start
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.example/data", nil)
			if err == nil {
				var resp *http.Response
				resp, err = apiHTTP.Do(req)
				if resp != nil {
					_ = resp.Body.Close()
					if err == nil && resp.StatusCode != http.StatusOK {
						err = fmt.Errorf("status = %d", resp.StatusCode)
					}
				}
			}
			errs <- err
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := grants.Load(); got != 2 {
		t.Fatalf("token grants = %d, want initial plus exactly one replacement", got)
	}
	if got := rejected.Load(); got != callers {
		t.Fatalf("rejected first attempts = %d, want %d", got, callers)
	}
}

func TestShortLivedTokenFailsBeforeBearerPacingCanChurn(t *testing.T) {
	t.Parallel()

	var grants atomic.Int32
	tokenHTTP := &http.Client{Transport: credentialsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		grants.Add(1)

		return credentialsJSONResponse(r, http.StatusOK, `{"access_token":"too-short","expires_in":1}`), nil
	})}
	source := NewClientCredentialsSource(ClientCredentialsConfig{
		HTTPClient: tokenHTTP,
		TokenURL:   "https://auth.example/token",
		ExpirySkew: time.Minute,
	})
	var apiHits atomic.Int32
	apiHTTP := NewRetryClient(RetryConfig{
		LogPath: true,
		Debug:   &DebugConfig{MaxBodyBytes: -1, MetadataOnly: true},

		Base: credentialsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			apiHits.Add(1)

			return credentialsJSONResponse(r, http.StatusOK, `{}`), nil
		}),
		Bearer:  source,
		Limiter: immediateLimiter{},
		Logger:  logging.NopZap(),
	})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://api.example/data", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := apiHTTP.Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "not greater than expiry skew") {
		t.Fatalf("API error = %v, want unusable token lifetime rejected", err)
	}
	if got := grants.Load(); got != 1 {
		t.Fatalf("token grants = %d, want one rejected short-lived grant", got)
	}
	if got := apiHits.Load(); got != 0 {
		t.Fatalf("API requests = %d, want none without a usable bearer", got)
	}
}

func credentialsJSONResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

type credentialsRoundTripFunc func(*http.Request) (*http.Response, error)

func (f credentialsRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type immediateLimiter struct{}

func (immediateLimiter) Wait(context.Context) error { return nil }

func TestTokenGrantRefusesRedirectsWithoutMutatingCaller(t *testing.T) {
	for _, auth := range []ClientCredentialsAuth{ClientCredentialsForm, ClientCredentialsBasic} {
		for _, status := range []int{301, 302, 303, 307, 308} {
			for _, target := range []string{"https://outside.example/token", "https://sub.auth.example/token", "https://auth.example:8443/token", "http://auth.example/token", "https://auth.example/other"} {
				t.Run(fmt.Sprintf("auth-%d/status-%d/%s", auth, status, target), func(t *testing.T) {
					var calls atomic.Int32
					client := NewRetryClient(RetryConfig{
						LogPath:     true,
						MaxAttempts: 1, Base: credentialsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
							calls.Add(1)
							response := credentialsJSONResponse(r, status, "")
							response.Header.Set("Location", target)
							return response, nil
						})})
					source := NewClientCredentialsSource(ClientCredentialsConfig{HTTPClient: client, TokenURL: "https://auth.example/token", ClientID: "audit-client", ClientSecret: "audit-secret", ClientAuth: auth})
					_, err := source.Token(t.Context())
					var apiErr *TokenEndpointError
					if !errors.As(err, &apiErr) || apiErr.StatusCode != status {
						t.Fatalf("expected original redirect response, got %v", err)
					}
					if calls.Load() != 1 {
						t.Fatalf("credential request followed redirect: %d calls", calls.Load())
					}
					if client.CheckRedirect != nil {
						t.Fatal("modified caller-owned HTTP client")
					}
				})
			}
		}
	}
}

func TestTokenResourceIndicatorIsExplicit(t *testing.T) {
	for _, resource := range []string{"", "https://api.example/accounts"} {
		client := &http.Client{Transport: credentialsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(r.Body)
			form, err := url.ParseQuery(string(body))
			if err != nil {
				t.Fatal(err)
			}
			if form.Get("resource") != resource {
				t.Fatalf("resource=%q", form.Get("resource"))
			}
			if _, exists := form["resource"]; exists != (resource != "") {
				t.Fatal("implicit resource parameter")
			}
			return credentialsJSONResponse(r, 200, `{"access_token":"token","expires_in":3600}`), nil
		})}
		source := NewClientCredentialsSource(ClientCredentialsConfig{HTTPClient: client, TokenURL: "https://auth.example/token", Resource: resource})
		if _, err := source.Token(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestClientCredentialsBoundsAndSafeErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		status           int
		limit            int64
	}{
		{"oversize", strings.Repeat("x", 128), "exceeds limit", 200, 32},
		{"invalid expiry", `{"access_token":"secret-token","expires_in":"secret-expiry"}`, "invalid token response", 200, 1024},
		{"invalid type", `{"access_token":"secret-token","token_type":"secret-type"}`, "unsupported token_type", 200, 1024},
		{"overflow", `{"access_token":"secret-token","expires_in":9223372036854775807}`, "overflows duration", 200, 1024},
		{"refusal", `{"client_secret":"secret-endpoint"}`, "status 403", 403, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &credentialsCountedBody{Reader: strings.NewReader(tc.body)}
			source := NewClientCredentialsSource(ClientCredentialsConfig{TokenURL: "https://auth.example/token", MaxBody: tc.limit, ValidateTokenType: true,
				HTTPClient: &http.Client{Transport: credentialsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: tc.status, Body: body, Header: make(http.Header), Request: r}, nil
				})}})
			token, err := source.Token(t.Context())
			if token != "" || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("token=%q err=%v", token, err)
			}
			if strings.Contains(err.Error(), "secret-") {
				t.Fatalf("untrusted response rendered: %v", err)
			}
			if body.read > int(tc.limit)+1 || !body.closed {
				t.Fatalf("read=%d, closed=%v", body.read, body.closed)
			}
			if tc.status == 403 {
				var response *TokenEndpointError
				if !errors.As(err, &response) || response.HTTPStatusCode() != 403 || body.read != 0 {
					t.Fatalf("refusal=%v read=%d", err, body.read)
				}
			}
		})
	}
}

type credentialsCountedBody struct {
	*strings.Reader
	read   int
	closed bool
}

func (b *credentialsCountedBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.read += n
	return n, err
}
func (b *credentialsCountedBody) Close() error { b.closed = true; return nil }

func TestClientCredentialsOwnsScopePolicyAndPreservesBasicBytes(t *testing.T) {
	required := []string{"read:one"}
	source := NewClientCredentialsSource(ClientCredentialsConfig{TokenURL: "https://auth.example/token", ClientID: "id +%", ClientSecret: "secret+% :", Scope: "read:one  read:two", Resource: "urn:example:read", ClientAuth: ClientCredentialsBasic, RequiredScopes: required,
		HTTPClient: &http.Client{Transport: credentialsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			id, secret, ok := r.BasicAuth()
			if !ok || id != "id +%" || secret != "secret+% :" {
				t.Fatal("HTTP Basic bytes changed")
			}
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("scope") != "read:one  read:two" || r.Form.Get("resource") != "urn:example:read" || r.Form.Has("client_secret") {
				t.Fatal("grant fields changed")
			}
			return credentialsJSONResponse(r, 200, `{"access_token":"token","scope":"read:one","expires_in":"3600"}`), nil
		})}})
	required[0] = "different"
	if token, err := source.Token(t.Context()); token != "token" || err != nil {
		t.Fatalf("Token=%q,%v", token, err)
	}
}

func TestClientCredentialsTransportIdentityWithoutRendering(t *testing.T) {
	cause := credentialsUnprintableError{}
	source := NewClientCredentialsSource(ClientCredentialsConfig{TokenURL: "https://auth.example/secret-token", Job: job.Desc{Name: "example.get_token", ComponentName: "example"}, HTTPClient: &http.Client{Transport: credentialsRoundTripFunc(func(*http.Request) (*http.Response, error) { return nil, cause })}})
	_, err := source.Token(t.Context())
	if !errors.Is(err, cause) || strings.Contains(err.Error(), "secret-token") {
		t.Fatal("transport identity or safe error lost")
	}
}

type credentialsUnprintableError struct{}

func (credentialsUnprintableError) Error() string { panic("untrusted transport error rendered") }

// Done is evaluated only after Token has selected the existing flight and
// released the mutex. The barrier therefore observes membership, not scheduling.
type joinedFlightContext struct {
	context.Context
	joined chan<- struct{}
	once   sync.Once
}

func (c *joinedFlightContext) Done() <-chan struct{} {
	c.once.Do(func() { c.joined <- struct{}{} })
	return c.Context.Done()
}
