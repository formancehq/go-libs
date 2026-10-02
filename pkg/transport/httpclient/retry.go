package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

// Retry defaults. Zero-value RetryConfig fields fall back to these so a caller
// can pass a minimal config.
const (
	defaultMaxAttempts = 4
	defaultBaseDelay   = 500 * time.Millisecond
	defaultMaxDelay    = 30 * time.Second
	defaultMaxElapsed  = 2 * time.Minute
)

// RetryConfig configures NewRetryTransport and NewRetryClient. Every field has
// a sensible default, so a caller that does not care about retry, pacing or
// deadlines can pass the zero RetryConfig.
type RetryConfig struct {
	// TotalTimeout is the http.Client timeout NewRetryClient sets, which spans
	// ALL attempts including the backoff waits between them and the body read.
	//
	// Leave it 0 unless something is waiting on the call, and a total covering
	// MaxElapsed plus a per-attempt header deadline for every attempt is
	// derived (see RetryTransport.Timeout).
	//
	// Set it when something IS waiting, an inbound request handler say, and it
	// is honored as a hard upper bound: MaxElapsed and PerAttemptTimeout are
	// clamped to fit inside it, so the retry loop stops just short of the
	// ceiling and surfaces the last response rather than being cut
	// mid-backoff. Retries still happen, within the ceiling.
	TotalTimeout time.Duration
	// PerAttemptTimeout bounds ONE attempt's wait for response headers
	// (0 = 30s). It keeps a single hung upstream from eating the whole retry
	// budget. Ignored when Base is set: a caller supplying its own transport
	// owns its deadlines.
	PerAttemptTimeout time.Duration
	// MaxAttempts bounds the total tries including the first (0 = 4).
	MaxAttempts int
	// BaseDelay / MaxDelay bound the exponential backoff (0 = 500ms / 30s).
	BaseDelay time.Duration
	MaxDelay  time.Duration
	// MaxElapsed caps the total time spent across retries, measured from the
	// start of the request (0 = 2m). A wait that would overrun it ends the
	// retries and surfaces the last result. Discarding a retried response's
	// body and pacing a retry (its bearer token and limiter waits) count
	// against it too and are cut off when it runs out; the call then fails
	// with context.DeadlineExceeded, the discarded response being no longer
	// available to surface. The first attempt's pacing is bounded by the
	// request context alone.
	MaxElapsed time.Duration
	// RequestsPerSecond enables client-side pacing with a token bucket when
	// > 0 and Limiter is nil. Burst defaults to 1.
	RequestsPerSecond float64
	Burst             int
	// BufferBody, when > 0, snapshots up to that many bytes of each response
	// body for the policy (Attempt.Body) and re-wraps resp.Body so the caller
	// still reads the full body. Needed only for body-signalled rate limits.
	// The snapshot is read inside MaxElapsed: a body that stalls past it is
	// closed and the call fails with context.DeadlineExceeded, since the
	// response can no longer be surfaced intact. An attempt that completes
	// with the budget already spent is judged without a snapshot, since no
	// retry can follow it.
	BufferBody int
	// Policy overrides the retry decision (nil = DefaultRetryPolicy built from
	// BaseDelay and MaxDelay).
	Policy RetryPolicy
	// Limiter overrides the pacer (nil = a token bucket when
	// RequestsPerSecond > 0, otherwise no pacing). A Limiter implementing
	// Observer is fed every completed attempt.
	Limiter Limiter
	// Base is the underlying RoundTripper. Nil builds NewBaseTransport wrapped
	// in otelhttp so every attempt gets its own client span; a caller-supplied
	// Base is used verbatim, deadlines and instrumentation included.
	Base http.RoundTripper
	// Logger records each retry and bearer re-mint at Info (nil = the logger
	// carried by the request context, see logging.FromContext).
	Logger logging.Logger
	// MeterProvider records the retry metrics, and the otelhttp metrics of the
	// default Base (nil = the global provider).
	MeterProvider metric.MeterProvider
	// TracerProvider records the per-attempt client spans of the default Base
	// (nil = the global provider). Ignored when Base is set.
	TracerProvider trace.TracerProvider
	// Idempotent, when set, decides whether a request whose METHOD is not
	// idempotent is nonetheless safe to retry. It is the client-level
	// counterpart to the per-request Idempotent marker, for an upstream where
	// the answer is a property of the whole client: an API whose reads are all
	// POST-shaped, or a JSON-RPC endpoint.
	//
	// It is consulted only for non-idempotent methods, and never overrides the
	// replayable-body requirement.
	Idempotent func(*http.Request) bool
	// Sign, when set, is applied to a fresh clone of the request before every
	// attempt (after any limiter wait), so per-request credentials (a JWT
	// nonce, a timestamp-bound HMAC) are minted per try instead of replayed
	// stale into a guaranteed 401. The hook must be safe for concurrent use;
	// the caller's original request is never mutated. Use BodyForSigning to
	// read the body it signs.
	//
	// Every redirect hop must remain on the exact origin of the initial
	// request: a cross-origin redirect fails closed before its request reaches
	// the wire, since Sign would otherwise freshly sign it for a third party.
	Sign func(*http.Request) error
	// Bearer, when set, owns the Authorization header for every attempt. A 401
	// compare-and-clears the token that was actually sent, then buys exactly
	// one replay with a freshly minted token. This auth replay is gated only on
	// body replayability, not on retry idempotency: the shared assumption is
	// that a 401 was refused before any server-side mutation ran.
	//
	// Bearer is applied after Sign and owns the final Authorization header. It
	// shares Sign's redirect trust boundary. The token source must mint
	// through a separate HTTP client so token acquisition does not recurse
	// through this transport.
	Bearer BearerTokenSource
}

// RetryTransport is an http.RoundTripper that paces and retries requests. Build
// it with NewRetryTransport. It is safe for concurrent use: its fields are
// read-only after construction, and every RoundTrip keeps its per-request
// state (attempt count, the bearer token it carries) in its own roundTrip.
type RetryTransport struct {
	cfg     RetryConfig
	timeout time.Duration
	// metrics is nil when instrument construction failed, and every recording
	// method tolerates that.
	metrics *retryMetrics
}

var _ http.RoundTripper = (*RetryTransport)(nil)

// NewRetryTransport builds a transport that paces requests through the
// limiter and retries replayable requests per the policy. Pair it with an
// http.Client whose Timeout is RetryTransport.Timeout, or use NewRetryClient.
func NewRetryTransport(cfg RetryConfig) *RetryTransport {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = defaultMaxAttempts
	}
	if cfg.BaseDelay <= 0 {
		cfg.BaseDelay = defaultBaseDelay
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = defaultMaxDelay
	}
	if cfg.MaxElapsed <= 0 {
		cfg.MaxElapsed = defaultMaxElapsed
	}
	if cfg.PerAttemptTimeout <= 0 {
		cfg.PerAttemptTimeout = defaultPerAttemptTimeout
	}
	// Before Base: an explicit TotalTimeout clamps MaxElapsed and
	// PerAttemptTimeout, and the header deadline is built from the clamped
	// value.
	timeout := deriveTimeout(&cfg)
	if cfg.Base == nil {
		cfg.Base = instrumentedTransport(NewBaseTransport(cfg.PerAttemptTimeout), cfg.TracerProvider, cfg.MeterProvider)
	}
	if cfg.Policy == nil {
		cfg.Policy = DefaultRetryPolicy{Base: cfg.BaseDelay, Max: cfg.MaxDelay}
	}
	if cfg.Limiter == nil && cfg.RequestsPerSecond > 0 {
		cfg.Limiter = newTokenBucket(cfg.RequestsPerSecond, cfg.Burst)
	}

	return &RetryTransport{
		cfg:     cfg,
		timeout: timeout,
		metrics: newRetryMetrics(cfg.MeterProvider),
	}
}

// NewRetryClient builds an *http.Client around NewRetryTransport(cfg) whose
// Timeout is the transport's derived total timeout.
func NewRetryClient(cfg RetryConfig) *http.Client {
	t := NewRetryTransport(cfg)

	return &http.Client{
		Timeout:   t.Timeout(),
		Transport: t,
	}
}

// Timeout is the total timeout an http.Client using this transport should
// set: TotalTimeout when given, otherwise MaxElapsed plus one per-attempt
// header deadline for every attempt.
func (t *RetryTransport) Timeout() time.Duration {
	return t.timeout
}

// deriveTimeout returns the http.Client timeout for cfg, whose fields are
// already defaulted, clamping the retry budget to fit inside an explicit
// TotalTimeout.
//
// TotalTimeout spans every attempt plus the backoff waits between them, so a
// TotalTimeout smaller than MaxElapsed cannot honor the configured retries:
// the client abandons the request mid-backoff and the last response is lost
// with it. Raising the timeout to fit would silently override a deliberate
// ceiling (a handler running inside an inbound request asks for 30s for a
// reason), so the ceiling wins and the BUDGET gives way: the retry loop's own
// deadline check then stops it just short of the ceiling and surfaces the last
// response. PerAttemptTimeout is clamped too, so a single attempt's header
// deadline cannot exceed the whole request's ceiling.
func deriveTimeout(cfg *RetryConfig) time.Duration {
	if cfg.TotalTimeout <= 0 {
		return cfg.MaxElapsed + time.Duration(cfg.MaxAttempts)*cfg.PerAttemptTimeout
	}

	cfg.MaxElapsed = min(cfg.MaxElapsed, cfg.TotalTimeout)
	cfg.PerAttemptTimeout = min(cfg.PerAttemptTimeout, cfg.TotalTimeout)

	return cfg.TotalTimeout
}

// RoundTrip paces via the limiter, then retries replayable requests whose
// completed attempt the policy marks retryable, honoring the MaxElapsed
// budget and the request context. Every attempt beyond the first (and every
// attempt at all when Sign or Bearer is set) runs on a fresh clone with the
// body rewound from GetBody, so retries never replay a consumed body or a
// stale signature.
func (t *RetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	start := time.Now()
	deadline := start.Add(t.cfg.MaxElapsed)
	authBudgetCtx := ctx
	cancelAuthBudget := func() {}
	if t.cfg.Bearer != nil {
		authBudgetCtx, cancelAuthBudget = context.WithDeadline(ctx, deadline)
	}
	defer cancelAuthBudget()
	rt := &roundTrip{
		t:             t,
		req:           req,
		deadline:      deadline,
		idempotent:    t.canRetry(req),
		replayable:    bodyReplayable(req),
		authReplayed:  bearerReplaySpent(req),
		maxAttempts:   t.cfg.MaxAttempts,
		policyAttempt: 1,
	}
	// A RoundTripper must close the request body, including on errors. Base
	// closes whatever body it is handed; the caller's own body is closed here
	// whenever no attempt handed it to Base.
	defer rt.closeOriginalBody()

	// One measurement per logical request, so the histogram reflects what the
	// caller actually waited for rather than one attempt out of several.
	defer func() { t.metrics.observeDuration(ctx, req, time.Since(start)) }()

	// Both credential hooks MINT on the request the transport is about to
	// send, which is why net/http's own redirect protection does not cover
	// them: it strips sensitive headers it would have COPIED onto a new
	// origin, and these are not copied, Sign and Bearer would freshly stamp the
	// redirected request. So one refusal enforces the boundary for both, before
	// a token is minted, a limiter slot is spent, or anything reaches the
	// wire. The chain is a property of req, so it is judged once.
	if hooks := credentialHooks(t.cfg); hooks != "" && !sameOriginRedirectChain(req) {
		return nil, fmt.Errorf("httpclient: %s refuses a cross-origin redirect", hooks)
	}

	for rt.attempt = 1; ; rt.attempt++ {
		r, err := rt.prepare(ctx, authBudgetCtx)
		if err != nil {
			return nil, err
		}

		resp, err := rt.send(ctx, r)

		// Cancellation is a caller decision, not a transient transport
		// failure. Returning before the retry policy avoids a misleading
		// retry log and metric while still closing a rare response returned
		// alongside the context error.
		if err != nil && ctx.Err() != nil {
			drainResponse(resp)

			return nil, ctx.Err()
		}

		if rt.bearerRejected(resp, err) {
			replay, rerr := rt.recoverBearer(ctx, authBudgetCtx, resp)
			if rerr != nil {
				return nil, rerr
			}
			if !replay {
				return resp, err
			}

			continue
		}

		wait, retry, derr := rt.decide(ctx, r, resp, err)
		if derr != nil {
			return nil, derr
		}
		if !retry {
			return resp, err
		}
		if serr := rt.waitBackoff(ctx, resp, err, wait); serr != nil {
			return nil, serr
		}
	}
}

// roundTrip is one logical request across its attempts. RoundTrip owns its
// lifetime, including the deferred duration metric and auth-budget cancel; the
// methods are the named steps of one attempt, in the order they run.
//
// The contexts stay parameters: ctx is always the caller's request context,
// and authBudget is ctx bounded by the MaxElapsed deadline when Bearer is set
// (otherwise ctx itself). The budget bounds the re-mint a 401 triggers and the
// token and limiter waits of the replay that re-mint buys; an ordinary retry's
// waits get their own bounded context from paceContext.
type roundTrip struct {
	t   *RetryTransport
	req *http.Request
	// deadline ends the MaxElapsed budget measured from the start of RoundTrip.
	deadline   time.Time
	idempotent bool
	replayable bool

	// maxAttempts is the ordinary retry cap, raised by one when the auth
	// replay is bought. policyAttempt is the 1-based count the retry policy
	// judges, which the auth replay does not advance.
	maxAttempts   int
	policyAttempt int

	// authReplayed marks the one auth replay as spent, on this request or
	// earlier in its redirect chain. authReplayPending marks the next attempt
	// as that replay, whose token and limiter waits then run inside
	// authBudget.
	authReplayed      bool
	authReplayPending bool

	// attempt is the 1-based try in flight. bearer is the token that try
	// carries, which a 401 compare-and-clears.
	attempt int
	bearer  string

	// originalBodySent records that an attempt handed the caller's own
	// req.Body to Base, which then owns closing it.
	originalBodySent bool
}

// closeOriginalBody closes the caller's request body unless an attempt handed
// it to Base. Attempts that run on a clone carry a fresh body from GetBody,
// which Base closes; the original is otherwise never closed. GetBody must
// return a reader independent of the original, as net/http's own do.
func (rt *roundTrip) closeOriginalBody() {
	body := rt.req.Body
	if rt.originalBodySent || body == nil || body == http.NoBody {
		return
	}
	_ = body.Close()
}

// prepare readies one attempt for the wire: the bearer token and limiter
// slot, then a fresh request that is signed and authorized. A pending auth
// replay whose budget ran out meanwhile is refused after signing, before the
// send.
func (rt *roundTrip) prepare(ctx, authBudget context.Context) (*http.Request, error) {
	workCtx, cancelWork := rt.paceContext(ctx, authBudget)
	err := rt.pace(ctx, workCtx)
	spent := rt.retryPacingSpentBudget(ctx, workCtx, err)
	cancelWork()
	if spent {
		return nil, rt.budgetSpent(ctx, "while pacing the retry", err)
	}
	if err != nil {
		return nil, err
	}
	r, err := rt.authorize(ctx)
	if err != nil {
		return nil, err
	}
	if err := rt.replayBudgetSpent(ctx, authBudget, r); err != nil {
		return nil, err
	}

	return r, nil
}

// paceContext bounds one attempt's token and limiter waits. An ordinary retry
// waited out its backoff inside the MaxElapsed budget, and its waits run
// inside what is left of it, so a throttled limiter or a slow mint cannot hold
// it past MaxElapsed. The auth replay's waits run inside authBudget, and the
// first attempt's inside the caller's ctx alone.
func (rt *roundTrip) paceContext(ctx, authBudget context.Context) (context.Context, context.CancelFunc) {
	switch {
	case rt.authReplayPending:
		return authBudget, func() {}
	case rt.attempt > 1:
		return context.WithDeadline(ctx, rt.deadline)
	default:
		return ctx, func() {}
	}
}

// retryPacingSpentBudget reports whether an ordinary retry's pacing ran into
// the end of the MaxElapsed budget: its bounded wait was cut short or refused
// as unable to finish in time, or it finished with no budget left to send in.
// Caller cancellation wins over the budget cause.
func (rt *roundTrip) retryPacingSpentBudget(ctx, workCtx context.Context, err error) bool {
	if rt.attempt == 1 || rt.authReplayPending || ctx.Err() != nil {
		return false
	}
	if err != nil {
		return workCtx.Err() != nil || errors.Is(err, context.DeadlineExceeded)
	}

	return !time.Now().Before(rt.deadline)
}

// pace acquires the bearer token, when Bearer is set, and then waits for an
// API limiter slot. workCtx bounds both waits; ctx is the caller's context.
func (rt *roundTrip) pace(ctx, workCtx context.Context) error {
	if rt.t.cfg.Bearer == nil {
		if rt.t.cfg.Limiter == nil {
			return nil
		}

		return rt.waitLimiter(ctx, workCtx)
	}

	// Acquire before the API limiter: a cache miss may mint through a
	// separate client sharing the same limiter, and reserving the API slot
	// first would let the mint and the API request leave back-to-back.
	token, err := rt.t.cfg.Bearer.Token(workCtx)
	if err != nil {
		return fmt.Errorf("httpclient: acquire bearer token: %w", err)
	}
	if token == "" {
		return fmt.Errorf("httpclient: acquire bearer token: source returned an empty token")
	}
	rt.bearer = token
	if rt.t.cfg.Limiter == nil {
		return nil
	}

	return rt.stabilizeBearer(ctx, workCtx)
}

// waitLimiter waits for one API limiter slot within workCtx. The wait is
// recorded against the caller's ctx even when the auth budget bounds it.
func (rt *roundTrip) waitLimiter(ctx, workCtx context.Context) error {
	waitStart := time.Now()
	err := rt.t.cfg.Limiter.Wait(workCtx)
	rt.t.metrics.observeLimiterWait(ctx, rt.req, time.Since(waitStart))

	return err
}

// authorize builds the attempt's request and stamps its credentials on it.
func (rt *roundTrip) authorize(ctx context.Context) (*http.Request, error) {
	sendCtx := ctx
	if rt.authReplayed {
		sendCtx = context.WithValue(ctx, bearerReplaySpentKey{}, true)
	}
	r, err := rt.t.attemptRequest(sendCtx, rt.req, rt.attempt)
	if err != nil {
		return nil, err
	}

	// Signing runs after the limiter wait so a long pacing delay cannot stale
	// a freshly minted timestamp or nonce.
	if rt.t.cfg.Sign != nil {
		if err := rt.t.cfg.Sign(r); err != nil {
			closeAttemptBody(rt.req, r)

			return nil, fmt.Errorf("httpclient: sign request: %w", err)
		}
	}
	if rt.t.cfg.Bearer != nil {
		// Bearer runs after Sign so it owns the final Authorization header.
		r.Header.Set("Authorization", "Bearer "+rt.bearer)
	}

	return r, nil
}

// send puts a prepared attempt on the wire and records what came back, before
// any decision about it: the attempt metric, then a limiter that learns from
// responses.
func (rt *roundTrip) send(ctx context.Context, r *http.Request) (*http.Response, error) {
	if r.Body != nil && r.Body == rt.req.Body {
		rt.originalBodySent = true
	}
	resp, err := rt.t.cfg.Base.RoundTrip(r)
	rt.authReplayPending = false
	rt.t.metrics.observeAttempt(ctx, rt.req, resp, err)

	// Feed the completed attempt to a limiter that learns from responses, so
	// it sees every reply including the ones no retry follows. This is what
	// lets an AdaptiveLimiter throttle before the next call rather than after
	// the next 429.
	if obs, ok := rt.t.cfg.Limiter.(Observer); ok {
		obs.Observe(resp)
	}

	return resp, err
}

// decide judges a completed attempt r that answered resp/err. retry=false
// means resp/err is the answer: a request that is not safe to replay, the last
// allowed attempt, a policy refusal, or a wait the remaining budget cannot
// honor. Otherwise wait is how long to back off before the next attempt.
// fail is set when buffering the response for the policy was cut short, which
// leaves no response to surface.
func (rt *roundTrip) decide(ctx context.Context, r *http.Request, resp *http.Response, err error) (wait time.Duration, retry bool, fail error) {
	if !rt.idempotent || rt.attempt >= rt.maxAttempts {
		if rt.idempotent && failed(resp, err) {
			rt.t.metrics.observeExhausted(ctx, rt.req, causeAttempts)
		}

		return 0, false, nil
	}

	body, fail := rt.snapshot(ctx, resp)
	if fail != nil {
		return 0, false, fail
	}
	wait, retry = rt.t.cfg.Policy.Retry(Attempt{Request: r, Response: resp, Err: err, Count: rt.policyAttempt, Body: body})
	if !retry {
		return 0, false, nil
	}
	if wait <= 0 {
		wait = rt.t.cfg.BaseDelay
	}
	if remaining := time.Until(rt.deadline); wait > remaining {
		// Not enough budget left to honor the wait: stop and surface the last
		// result, and say so, since a truncated retry configuration is
		// otherwise impossible to spot from the outside.
		rt.t.metrics.observeExhausted(ctx, rt.req, causeBudget)

		return 0, false, nil
	}

	return wait, true, nil
}

// waitBackoff records the retry that decide granted, drains the discarded
// response and sleeps out wait. Only the caller's context cuts the sleep
// short.
//
// The log line carries the host, never the path or query: an upstream that
// embeds its credential in the URL would otherwise print it during ordinary
// operation. The per-attempt span carries the URL to a controlled backend.
func (rt *roundTrip) waitBackoff(ctx context.Context, resp *http.Response, err error, wait time.Duration) error {
	// Drain and close so the connection can be reused before the next try,
	// within the budget: a body that stalls must not carry the call past
	// MaxElapsed. decide checked the wait against the budget before the
	// drain, so check again: nothing after the sleep looks at it.
	rt.drainWithinBudget(resp)
	if time.Until(rt.deadline) < wait {
		return rt.budgetSpent(ctx, "while discarding the retried response", nil)
	}

	rt.t.metrics.observeRetry(ctx, rt.req, resp, err)
	rt.policyAttempt++
	rt.t.logger(ctx).WithFields(map[string]any{
		"method":  rt.req.Method,
		"host":    rt.req.URL.Host,
		"status":  statusOf(resp),
		"attempt": rt.attempt,
		"wait":    wait.String(),
	}).Infof("http retry after rate limit / server error")

	return sleepContext(ctx, wait)
}

// failed reports whether an attempt ended in a way worth counting as
// exhaustion: a transport error, or a status the default policy would have
// retried given another attempt.
func failed(resp *http.Response, err error) bool {
	if err != nil || resp == nil {
		return true
	}

	return retryableStatus(resp.StatusCode)
}

// attemptRequest prepares the request for one attempt. The first attempt with
// neither credential hook uses the caller's request untouched (the zero-copy
// fast path); any retry, and every authenticated attempt, works on a clone
// with the body rewound from GetBody so a consumed body is never replayed
// empty and credentials never mutate the caller's request.
func (t *RetryTransport) attemptRequest(ctx context.Context, req *http.Request, attempt int) (*http.Request, error) {
	if t.cfg.Sign == nil && t.cfg.Bearer == nil && attempt == 1 {
		return req, nil
	}

	r := req.Clone(withAttempt(ctx, attempt))
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, fmt.Errorf("httpclient: rewind request body: %w", err)
		}
		r.Body = body
	}

	return r, nil
}

// snapshot reads a bounded copy of the response body (when BufferBody is set)
// and re-wraps resp.Body so the caller still reads the full body. It returns
// nil when there is nothing to buffer, or when the budget is already spent: no
// retry can follow, so the policy's verdict cannot need the body, and the
// response is surfaced untouched.
//
// The read is bounded in time as well as size. A body that stalls past the
// MaxElapsed deadline, or past the caller's cancellation, is closed to unblock
// the read; the response can then no longer be surfaced intact, so the error
// says why instead: the caller's ctx error, or context.DeadlineExceeded.
func (rt *roundTrip) snapshot(ctx context.Context, resp *http.Response) ([]byte, error) {
	if rt.t.cfg.BufferBody <= 0 || resp == nil || resp.Body == nil || !time.Now().Before(rt.deadline) {
		return nil, nil
	}
	var (
		once sync.Once
		cut  bool
	)
	closeBody := func() {
		once.Do(func() {
			cut = true
			_ = resp.Body.Close()
		})
	}
	timer := time.AfterFunc(time.Until(rt.deadline), closeBody)
	stopCancel := context.AfterFunc(ctx, closeBody)
	buf, err := io.ReadAll(io.LimitReader(resp.Body, int64(rt.t.cfg.BufferBody)))
	timer.Stop()
	stopCancel()
	// Claim the once: a close that raced the end of the read has either run
	// in full, setting cut, or never will.
	once.Do(func() {})
	if cut {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		return nil, rt.budgetSpent(ctx, "while buffering the response for the retry policy", nil)
	}
	// Re-wrap: the buffered head followed by whatever remains unread.
	resp.Body = &joinReadCloser{head: buf, tail: resp.Body, pendingErr: err}

	return buf, nil
}

// logger returns the configured logger, or the one the request context
// carries.
func (t *RetryTransport) logger(ctx context.Context) logging.Logger {
	if t.cfg.Logger != nil {
		return t.cfg.Logger.WithContext(ctx)
	}

	return logging.FromContext(ctx)
}

// closeAttemptBody closes the body of a prepared attempt that will never be
// sent, unless it is the caller's own body: a clone of a request without
// GetBody shares it, and closeOriginalBody alone closes that one, once.
func closeAttemptBody(original, attempt *http.Request) {
	if attempt == nil || attempt == original || attempt.Body == nil || attempt.Body == original.Body {
		return
	}
	_ = attempt.Body.Close()
}

func statusOf(resp *http.Response) int {
	if resp == nil {
		return 0
	}

	return resp.StatusCode
}

// budgetSpent ends the retries when MaxElapsed ran out after the last
// response was already discarded, so there is no result left to surface.
// cause, when set, is the wait the budget cut short; the error wraps it and
// context.DeadlineExceeded.
func (rt *roundTrip) budgetSpent(ctx context.Context, while string, cause error) error {
	rt.t.metrics.observeExhausted(ctx, rt.req, causeBudget)
	switch {
	case cause == nil:
		cause = context.DeadlineExceeded
	case !errors.Is(cause, context.DeadlineExceeded):
		cause = fmt.Errorf("%w: %w", context.DeadlineExceeded, cause)
	}

	return fmt.Errorf("httpclient: retry budget ran out %s: %w", while, cause)
}

// drainWithinBudget drains a discarded response like drainResponse, but
// closes the body once the MaxElapsed budget runs out, which unblocks a read
// the upstream stalls. The body is closed exactly once.
func (rt *roundTrip) drainWithinBudget(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	var once sync.Once
	closeBody := func() { once.Do(func() { _ = resp.Body.Close() }) }
	timer := time.AfterFunc(max(time.Until(rt.deadline), 0), closeBody)
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	timer.Stop()
	closeBody()
}

// drainResponse reads and closes a discarded response body so the underlying
// connection can be reused on the next attempt.
func drainResponse(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
}

// sleepContext waits for d or until ctx is done.
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// joinReadCloser serves a buffered head then the remaining tail, closing the
// tail. It lets a policy inspect the body head while the caller still reads
// it whole.
type joinReadCloser struct {
	head       []byte
	off        int
	tail       io.ReadCloser
	pendingErr error
}

func (j *joinReadCloser) Read(p []byte) (int, error) {
	if j.off < len(j.head) {
		n := copy(p, j.head[j.off:])
		j.off += n

		return n, nil
	}
	if j.pendingErr != nil {
		err := j.pendingErr
		j.pendingErr = nil

		return 0, err
	}

	return j.tail.Read(p)
}

func (j *joinReadCloser) Close() error { return j.tail.Close() }
