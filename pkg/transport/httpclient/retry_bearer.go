package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// maxBearerStabilityChecks bounds the token re-reads across limiter waits: a
// broken or adversarial token source must not turn one API request into an
// unbounded token/limiter churn loop.
const maxBearerStabilityChecks = 8

// BearerTokenSource supplies cached bearer tokens and compare-and-clears one
// that an upstream rejected. Implementations must be safe for concurrent use.
type BearerTokenSource interface {
	// Token returns the current token, minting one when none is cached.
	Token(context.Context) (string, error)
	// Invalidate drops the cached token if it is still rejected; a token
	// another request already replaced must survive.
	Invalidate(rejected string)
}

// bearerReplaySpentKey marks a request whose one auth replay is spent, so a
// redirect following it cannot buy a second one.
type bearerReplaySpentKey struct{}

// stabilizeBearer waits for an API limiter slot that the bearer token
// outlives. A token can expire or be compare-and-cleared while this request is
// queued behind the API limiter. Reserve and re-read until the token remains
// stable across a slot; if the re-read minted through the same limiter, the
// next loop reserves the API slot after that grant.
func (rt *roundTrip) stabilizeBearer(ctx, workCtx context.Context) error {
	for checks := 0; ; checks++ {
		if checks >= maxBearerStabilityChecks {
			return fmt.Errorf("httpclient: bearer token did not stabilize across limiter waits")
		}
		if err := rt.waitLimiter(ctx, workCtx); err != nil {
			return err
		}
		fresh, err := rt.t.cfg.Bearer.Token(workCtx)
		if err != nil {
			return fmt.Errorf("httpclient: revalidate bearer token: %w", err)
		}
		if fresh == "" {
			return fmt.Errorf("httpclient: revalidate bearer token: source returned an empty token")
		}
		if fresh == rt.bearer {
			return nil
		}
		rt.bearer = fresh
	}
}

// replayBudgetSpent refuses a pending auth replay whose budget expired while
// it waited for its token or limiter slot, closing the attempt's body. Caller
// cancellation wins over the budget cause.
func (rt *roundTrip) replayBudgetSpent(ctx, authBudget context.Context, r *http.Request) error {
	if !rt.authReplayPending || authBudget.Err() == nil {
		return nil
	}
	closeAttemptBody(rt.req, r)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	rt.t.metrics.observeExhausted(ctx, rt.req, causeBudget)

	return authBudget.Err()
}

// bearerRejected reports whether a completed attempt earns the one auth
// replay: a 401 to a bearer-authenticated request whose body can be replayed,
// with the replay not yet spent on this request or its redirect chain.
//
// Auth replay has a different permission from an ordinary retry: a replayable
// POST may be replayed after a 401 even though it must not be retried after a
// 5xx. The one-shot flag keeps a real credential failure from becoming an
// authentication loop.
func (rt *roundTrip) bearerRejected(resp *http.Response, err error) bool {
	return rt.t.cfg.Bearer != nil && rt.replayable && !rt.authReplayed && err == nil &&
		resp != nil && resp.StatusCode == http.StatusUnauthorized
}

// recoverBearer spends the auth replay on the 401 in resp, re-minting within
// authBudget. replay=true means the 401 was drained and the next attempt
// resends with a fresh token. Otherwise the 401 is the answer, unless the
// caller cancelled during the re-mint: then err is the caller's context error.
func (rt *roundTrip) recoverBearer(ctx, authBudget context.Context, resp *http.Response) (replay bool, err error) {
	if !time.Now().Before(rt.deadline) {
		rt.t.metrics.observeExhausted(ctx, rt.req, causeBudget)

		return false, nil
	}
	outcome, budgetExpired := rt.remintBearer(ctx, authBudget)
	if outcome != bearerRefreshSucceeded {
		if ctx.Err() != nil {
			drainResponse(resp)

			return false, ctx.Err()
		}
		if budgetExpired {
			rt.t.metrics.observeExhausted(ctx, rt.req, causeBudget)
		}
		// Keep the original 401: it describes the operation that failed better
		// than the recovery mint error. The failure outcome is observable
		// without trusting arbitrary error text with credentials.
		return false, nil
	}
	if !time.Now().Before(rt.deadline) {
		rt.t.metrics.observeExhausted(ctx, rt.req, causeBudget)

		return false, nil
	}

	rt.authReplayed = true
	rt.authReplayPending = true
	rt.maxAttempts++ // the auth replay is independent of the ordinary retry cap
	// A rejected attempt carries no transport error; see bearerRejected.
	rt.t.metrics.observeRetry(ctx, rt.req, resp, nil)
	rt.drainWithinBudget(ctx, resp)
	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	return true, nil
}

// remintBearer compare-and-clears the token the rejected attempt carried and
// mints a replacement within authBudget, recording and logging the outcome.
// budgetExpired reports that the mint ran out of request budget, as opposed to
// the caller cancelling.
func (rt *roundTrip) remintBearer(ctx, authBudget context.Context) (outcome string, budgetExpired bool) {
	rt.t.cfg.Bearer.Invalidate(rt.bearer)
	fresh, refreshErr := rt.t.cfg.Bearer.Token(authBudget)
	budgetExpired = authBudget.Err() != nil && ctx.Err() == nil
	outcome = bearerRefreshSucceeded
	if refreshErr != nil {
		outcome = bearerRefreshFailed
	} else if fresh == "" || fresh == rt.bearer {
		outcome = bearerRefreshUnchanged
	}
	rt.t.metrics.observeBearerRefresh(ctx, rt.req, outcome)

	fields := map[string]any{
		"method":  rt.req.Method,
		"host":    rt.req.URL.Host,
		"attempt": rt.attempt,
		"outcome": outcome,
	}
	if refreshErr != nil {
		for k, v := range bearerRefreshFailureFields(refreshErr) {
			fields[k] = v
		}
	}
	rt.t.logger(ctx).WithFields(fields).Infof("http bearer token rejected; re-mint attempted")

	return outcome, budgetExpired
}

// bearerReplaySpent reports whether the auth replay was already spent on req
// or on an earlier request of its redirect chain.
func bearerReplaySpent(req *http.Request) bool {
	for current := req; current != nil; {
		if spent, _ := current.Context().Value(bearerReplaySpentKey{}).(bool); spent {
			return true
		}
		if current.Response == nil {
			return false
		}
		current = current.Response.Request
	}

	return false
}

// bearerRefreshFailureFields turns an untrusted token-source error into a
// small, credential-safe diagnostic vocabulary. Arbitrary error text is never
// logged: a BearerTokenSource has no contract that its Error string is safe.
// The status of an OAuth2 token-endpoint rejection is enough to tell it from
// transport, deadline, cancellation and response-validation failures.
func bearerRefreshFailureFields(err error) map[string]any {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return map[string]any{"error_kind": "deadline"}
	case errors.Is(err, context.Canceled):
		return map[string]any{"error_kind": "canceled"}
	}

	var retrieveErr *oauth2.RetrieveError
	if errors.As(err, &retrieveErr) && retrieveErr.Response != nil {
		return map[string]any{
			"error_kind":            "token_endpoint_response",
			"token_endpoint_status": retrieveErr.Response.StatusCode,
		}
	}
	var networkErr interface{ Timeout() bool }
	if errors.As(err, &networkErr) {
		return map[string]any{"error_kind": "transport"}
	}

	return map[string]any{"error_kind": "invalid_token_response"}
}

// credentialHooks names the per-attempt credential hooks a RetryConfig
// enables, for the cross-origin refusal message. It returns "" when the
// transport mints no credentials, which is the only case free to follow a
// redirect anywhere.
func credentialHooks(cfg RetryConfig) string {
	switch {
	case cfg.Sign != nil && cfg.Bearer != nil:
		return "request signing and bearer authentication"
	case cfg.Sign != nil:
		return "request signing"
	case cfg.Bearer != nil:
		return "bearer authentication"
	default:
		return ""
	}
}

// sameOriginRedirectChain keeps a minted credential, Sign's signature or
// Bearer's token, from crossing a redirect trust boundary. Request.Response
// links a redirected request to the response that caused it. Every hop must
// remain on the initial origin; once a chain leaves, returning later does not
// regain credentials.
func sameOriginRedirectChain(req *http.Request) bool {
	chain := []*http.Request{req}
	for current := req; current.Response != nil && current.Response.Request != nil; {
		current = current.Response.Request
		chain = append(chain, current)
	}
	initial := chain[len(chain)-1]
	for _, hop := range chain[:len(chain)-1] {
		if !sameOrigin(hop, initial) {
			return false
		}
	}

	return true
}

func sameOrigin(a, b *http.Request) bool {
	if a == nil || b == nil || a.URL == nil || b.URL == nil {
		return false
	}

	return strings.EqualFold(a.URL.Scheme, b.URL.Scheme) &&
		strings.EqualFold(a.URL.Hostname(), b.URL.Hostname()) &&
		originPort(a) == originPort(b)
}

func originPort(req *http.Request) string {
	if port := req.URL.Port(); port != "" {
		return port
	}
	switch strings.ToLower(req.URL.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

// maxSigningBody bounds the body BodyForSigning reads. The body is one the
// caller built, so this is a generous ceiling rather than a real constraint.
const maxSigningBody = 32 << 20

// ErrSigningBodyTooLarge is returned by BodyForSigning for a body above its
// 32 MiB ceiling.
var ErrSigningBodyTooLarge = errors.New("httpclient: request body exceeds the signing limit")

// ErrSigningBodyNotReplayable is returned by BodyForSigning for a request that
// carries a body but no GetBody: the body cannot be read without consuming
// the bytes the send needs, and signing the empty string instead would send a
// signature that does not cover the payload.
var ErrSigningBodyNotReplayable = errors.New("httpclient: request body cannot be read for signing without GetBody")

// BodyForSigning returns the request body as a string for a RetryConfig.Sign
// hook that must bind it byte-for-byte. It reads through GetBody, so req.Body
// stays unconsumed for the send that follows; an absent body signs the empty
// string, which is what a GET or an empty-form POST expects. A body without
// GetBody is ErrSigningBodyNotReplayable: build the request from a bytes,
// strings or buffer reader (http.NewRequest sets GetBody), or set GetBody.
//
// Sign runs on a fresh clone before every attempt, so this is called once per
// try. A signer covering a body should route through here rather than reach
// for req.Body, which the transport still needs.
func BodyForSigning(req *http.Request) (string, error) {
	if req.GetBody == nil {
		if req.Body == nil || req.Body == http.NoBody {
			return "", nil
		}

		return "", ErrSigningBodyNotReplayable
	}
	rc, err := req.GetBody()
	if err != nil {
		return "", fmt.Errorf("httpclient: rewind body for signing: %w", err)
	}
	defer func() { _ = rc.Close() }()

	b, err := io.ReadAll(io.LimitReader(rc, maxSigningBody+1))
	if err != nil {
		return "", fmt.Errorf("httpclient: read body for signing: %w", err)
	}
	if len(b) > maxSigningBody {
		return "", ErrSigningBodyTooLarge
	}

	return string(b), nil
}
