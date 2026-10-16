package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	httphelper "github.com/formancehq/go-libs/v5/pkg/authn/oidc/http"
	"github.com/formancehq/go-libs/v5/pkg/observe/job"
)

const (
	clientCredentialsExpirySkew = 30 * time.Second
	clientCredentialsDefaultTTL = 5 * time.Minute
)

// ClientCredentialsAuth describes how the token endpoint receives the OAuth client
// credentials.
type ClientCredentialsAuth int

const (
	// ClientCredentialsForm sends client_id and client_secret in the form body.
	ClientCredentialsForm ClientCredentialsAuth = iota
	// ClientCredentialsBasic sends client_id and client_secret with HTTP Basic auth.
	ClientCredentialsBasic
)

// ClientCredentialsConfig describes one client-credentials token endpoint. HTTPClient is kept
// explicit because token minting must not recurse through the bearer-authenticated
// client that is trying to recover from a rejected token.
type ClientCredentialsConfig struct {
	HTTPClient   *http.Client
	TokenURL     string
	ClientID     string
	ClientSecret string
	Scope        string
	// Resource is the provider-selected OAuth resource indicator, when required.
	Resource   string
	ClientAuth ClientCredentialsAuth

	// ExpirySkew refreshes a token this long before its stated expiry (zero = 30s).
	ExpirySkew time.Duration
	// DefaultTTL is used when expires_in is absent or non-positive (zero = 5m).
	DefaultTTL time.Duration
	// RequiredScopes must all be present in the granted scope returned by the
	// token endpoint.
	RequiredScopes []string
	// ValidateTokenType rejects a non-empty token_type other than Bearer.
	ValidateTokenType bool
	// MaxBody caps the decoded token response (non-positive = 32 MiB).
	MaxBody int64
	// Job instruments each token-endpoint call, never a cache hit or a wait on
	// another caller's mint, as this caller-declared observe/job job. Zero leaves the
	// mint uninstrumented.
	Job job.Desc
}

// ClientCredentialsSource caches and refreshes one client-credentials access token. It is
// safe for concurrent use. One caller owns an in-flight mint while the others
// wait on its result; the network call runs outside the mutex so a waiting
// caller can still honor its own context deadline.
type ClientCredentialsSource struct {
	cfg ClientCredentialsConfig

	mu        sync.Mutex
	token     string
	expiresAt time.Time
	scopes    []string
	inflight  *clientCredentialsFlight
}

type clientCredentialsFlight struct {
	done          chan struct{}
	token         string
	err           error
	ownerCanceled bool
}

// NewClientCredentialsSource returns a client-credentials token source.
func NewClientCredentialsSource(cfg ClientCredentialsConfig) *ClientCredentialsSource {
	if cfg.HTTPClient != nil {
		// Token endpoints are final credential destinations. A 307/308 can replay
		// the form body even when net/http strips Authorization, so neither
		// Basic authentication nor form authentication may follow a redirect.
		// Copy the client: callers may share the original with other consumers.
		client := *cfg.HTTPClient
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
		cfg.HTTPClient = &client
	}
	if cfg.ExpirySkew <= 0 {
		cfg.ExpirySkew = clientCredentialsExpirySkew
	}
	if cfg.DefaultTTL <= 0 {
		cfg.DefaultTTL = clientCredentialsDefaultTTL
	}

	cfg.RequiredScopes = slices.Clone(cfg.RequiredScopes)
	return &ClientCredentialsSource{cfg: cfg}
}

// Token returns a cached token when it remains valid beyond the configured
// expiry skew, otherwise it mints and caches a replacement.
func (s *ClientCredentialsSource) Token(ctx context.Context) (string, error) {
	for {
		s.mu.Lock()
		if s.token != "" && time.Until(s.expiresAt) > s.cfg.ExpirySkew {
			token := s.token
			s.mu.Unlock()

			return token, nil
		}
		if s.inflight != nil {
			inflight := s.inflight
			s.mu.Unlock()
			select {
			case <-inflight.done:
				// A mint is allowed to inherit its owner's context, but that
				// cancellation must not poison callers whose own contexts remain
				// live. Re-enter the election and let one waiter replace the flight.
				if inflight.ownerCanceled && ctx.Err() == nil {
					continue
				}

				return inflight.token, inflight.err
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		inflight := &clientCredentialsFlight{done: make(chan struct{})}
		s.inflight = inflight
		s.mu.Unlock()

		token, expiresAt, scopes, err := s.instrumentedMint(ctx)

		s.mu.Lock()
		inflight.token = token
		inflight.err = err
		ctxErr := ctx.Err()
		inflight.ownerCanceled = ctxErr != nil && errors.Is(err, ctxErr)
		if err == nil {
			s.token = token
			s.expiresAt = expiresAt
			s.scopes = scopes
		}
		s.inflight = nil
		close(inflight.done)
		s.mu.Unlock()

		return token, err
	}
}

// Invalidate compare-and-clears a rejected token. A concurrent caller may have
// already installed a replacement; that newer token must not be discarded.
func (s *ClientCredentialsSource) Invalidate(rejected string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.token == rejected {
		s.token = ""
		s.expiresAt = time.Time{}
	}
}

// GrantedScopes returns the scopes from the most recently minted token.
func (s *ClientCredentialsSource) GrantedScopes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.scopes)
}

// instrumentedMint runs one token-endpoint call under the caller's job.
func (s *ClientCredentialsSource) instrumentedMint(ctx context.Context) (string, time.Time, []string, error) {
	if s.cfg.Job.Name == "" {
		return s.mint(ctx)
	}
	var (
		token     string
		expiresAt time.Time
		scopes    []string
	)
	err := job.Run(ctx, s.cfg.Job, func(ctx context.Context) error {
		var err error
		token, expiresAt, scopes, err = s.mint(ctx)

		return err
	})

	return token, expiresAt, scopes, err
}

func (s *ClientCredentialsSource) mint(ctx context.Context) (string, time.Time, []string, error) {
	if s.cfg.HTTPClient == nil {
		return "", time.Time{}, nil, fmt.Errorf("httpclient: client credentials: token HTTP client is required")
	}
	if s.cfg.TokenURL == "" {
		return "", time.Time{}, nil, fmt.Errorf("httpclient: client credentials: token URL is required")
	}

	form := url.Values{"grant_type": {"client_credentials"}}
	if s.cfg.Scope != "" {
		form.Set("scope", s.cfg.Scope)
	}
	if s.cfg.Resource != "" {
		form.Set("resource", s.cfg.Resource)
	}
	var auth httphelper.RequestAuthorization
	switch s.cfg.ClientAuth {
	case ClientCredentialsForm:
		form.Set("client_id", s.cfg.ClientID)
		form.Set("client_secret", s.cfg.ClientSecret)
	case ClientCredentialsBasic:
		auth = httphelper.AuthorizeBasic(s.cfg.ClientID, s.cfg.ClientSecret)
	default:
		return "", time.Time{}, nil, errors.New("httpclient: client credentials: unsupported client auth style")
	}
	// Keep the exact supplied scope string and resource. The string reader
	// supplies GetBody so retry clients can replay the form.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, nil, &clientCredentialsError{kind: "invalid token request", cause: err}
	}
	if auth != nil {
		auth(req)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := s.cfg.HTTPClient.Do(Idempotent(req))
	if err != nil {
		return "", time.Time{}, nil, &clientCredentialsError{kind: "token endpoint transport failed", cause: err}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", time.Time{}, nil, &TokenEndpointError{StatusCode: resp.StatusCode}
	}
	limit := s.cfg.MaxBody
	if limit <= 0 {
		limit = 32 << 20
	}
	// LimitReader's count must not wrap for a caller-supplied MaxInt64 cap.
	if limit == math.MaxInt64 {
		limit--
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return "", time.Time{}, nil, &clientCredentialsError{kind: "read token response failed", cause: err}
	}
	if int64(len(body)) > limit {
		return "", time.Time{}, nil, errors.New("httpclient: client credentials: token response exceeds limit")
	}
	var out struct {
		AccessToken string                   `json:"access_token"`
		ExpiresIn   clientCredentialsSeconds `json:"expires_in"`
		Scope       string                   `json:"scope"`
		TokenType   string                   `json:"token_type"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", time.Time{}, nil, &clientCredentialsError{kind: "invalid token response", cause: err}
	}
	if out.AccessToken == "" {
		return "", time.Time{}, nil, fmt.Errorf("httpclient: client credentials: token response omitted access_token")
	}
	if s.cfg.ValidateTokenType && out.TokenType != "" && !strings.EqualFold(out.TokenType, "bearer") {
		return "", time.Time{}, nil, errors.New("httpclient: client credentials: unsupported token_type")
	}

	granted := strings.Fields(out.Scope)
	for _, required := range s.cfg.RequiredScopes {
		if !slices.Contains(granted, required) {
			return "", time.Time{}, nil, fmt.Errorf("httpclient: client credentials: token lacks required %q scope", required)
		}
	}

	if out.ExpiresIn > clientCredentialsSeconds(math.MaxInt64/int64(time.Second)) {
		return "", time.Time{}, nil, errors.New("httpclient: client credentials: token lifetime overflows duration")
	}
	var ttl time.Duration
	if out.ExpiresIn > 0 {
		ttl = time.Duration(out.ExpiresIn) * time.Second
	}
	if ttl <= 0 {
		ttl = s.cfg.DefaultTTL
	}
	if ttl <= s.cfg.ExpirySkew {
		return "", time.Time{}, nil, fmt.Errorf(
			"httpclient: client credentials: token lifetime %s is not greater than expiry skew %s", ttl, s.cfg.ExpirySkew)
	}

	return out.AccessToken, time.Now().Add(ttl), granted, nil
}

// clientCredentialsSeconds accepts the numeric OAuth expires_in shape and the quoted numeric
// variant accepted by OAuth endpoints.
type clientCredentialsSeconds int64

func (s *clientCredentialsSeconds) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*s = 0

		return nil
	}
	raw := strings.Trim(string(data), `"`)
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return errors.New("httpclient: client credentials: invalid expires_in")
	}
	*s = clientCredentialsSeconds(value)

	return nil
}

// TokenEndpointError reports a token endpoint refusal without retaining its
// response body, URL or credentials. Status codes do not imply a permanent
// credential verdict; callers own that policy.
type TokenEndpointError struct{ StatusCode int }

func (e *TokenEndpointError) Error() string {
	return fmt.Sprintf("httpclient: token endpoint returned status %d", e.StatusCode)
}

// HTTPStatusCode exposes a response status to structured diagnostics.
func (e *TokenEndpointError) HTTPStatusCode() int { return e.StatusCode }

// Preserve cancellation/timeout identity without rendering untrusted errors.
type clientCredentialsError struct {
	kind  string
	cause error
}

func (e *clientCredentialsError) Error() string { return "httpclient: client credentials: " + e.kind }
func (e *clientCredentialsError) Unwrap() error { return e.cause }
