package httpclient

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
)

// instrumentationName is the OTel instrumentation scope of the retry
// transport's metrics.
const instrumentationName = "github.com/formancehq/go-libs/v5/pkg/transport/httpclient"

// Metric names recorded by the retry transport. They are deliberately
// distinct from the per-attempt semconv instruments otelhttp records (for
// example http.client.request.duration), which the default base transport
// also emits.
const (
	metricRetryRequestDuration = "http.client.retry.request.duration"
	metricRetryAttempts        = "http.client.retry.attempts"
	metricRetryCount           = "http.client.retry.count"
	metricRetryExhausted       = "http.client.retry.exhausted"
	metricLimiterWait          = "http.client.limiter.wait"
	metricBearerRefreshes      = "http.client.bearer.refreshes"
)

// Attribute keys and closed value sets, kept low-cardinality on purpose.
const (
	attrStatusClass          = "http.response.status_class"
	attrRetryReason          = "http.client.retry.reason"
	attrExhaustedCause       = "http.client.retry.exhausted.cause"
	attrBearerRefreshOutcome = "http.client.bearer.refresh.outcome"

	reasonTransport        = "transport"
	causeAttempts          = "attempts"
	causeBudget            = "budget"
	bearerRefreshSucceeded = "succeeded"
	bearerRefreshFailed    = "failed"
	bearerRefreshUnchanged = "unchanged"
)

// retryMetrics holds the instruments for one transport. A nil *retryMetrics
// records nothing, which every method's first line relies on: instrument
// construction can fail, and a client must not lose its transport over a
// telemetry problem.
type retryMetrics struct {
	duration    otelmetric.Float64Histogram
	limiterWait otelmetric.Float64Histogram
	attempts    otelmetric.Int64Counter
	retries     otelmetric.Int64Counter
	exhausted   otelmetric.Int64Counter
	bearer      otelmetric.Int64Counter
}

// newRetryMetrics builds the instruments on mp (nil = the global provider),
// returning nil if any of them fails so the transport records all of it or
// none rather than a confusing half.
func newRetryMetrics(mp otelmetric.MeterProvider) *retryMetrics {
	if mp == nil {
		mp = otel.GetMeterProvider()
	}
	meter := mp.Meter(instrumentationName)

	duration, err := meter.Float64Histogram(metricRetryRequestDuration,
		otelmetric.WithDescription("Duration of a logical HTTP request including every retry and backoff wait"),
		otelmetric.WithUnit("s"))
	if err != nil {
		return nil
	}
	limiterWait, err := meter.Float64Histogram(metricLimiterWait,
		otelmetric.WithDescription("Time an outbound request spent waiting on client-side rate limiting"),
		otelmetric.WithUnit("s"))
	if err != nil {
		return nil
	}
	attempts, err := meter.Int64Counter(metricRetryAttempts,
		otelmetric.WithDescription("Completed HTTP attempts, including retries"))
	if err != nil {
		return nil
	}
	retries, err := meter.Int64Counter(metricRetryCount,
		otelmetric.WithDescription("Attempts that were retried, by what triggered the retry"))
	if err != nil {
		return nil
	}
	exhausted, err := meter.Int64Counter(metricRetryExhausted,
		otelmetric.WithDescription("Requests that stopped retrying while still failing, by cause"))
	if err != nil {
		return nil
	}
	bearer, err := meter.Int64Counter(metricBearerRefreshes,
		otelmetric.WithDescription("Bearer-token re-mint attempts after an upstream 401, by outcome"))
	if err != nil {
		return nil
	}

	return &retryMetrics{
		duration:    duration,
		limiterWait: limiterWait,
		attempts:    attempts,
		retries:     retries,
		exhausted:   exhausted,
		bearer:      bearer,
	}
}

// route identifies the upstream a measurement belongs to: host and method
// only. A path would put an unbounded set of ids and cursors into the
// attribute space, and the per-attempt span already carries the URL.
// Process-level identity lives on the OTel Resource, not here.
func route(req *http.Request) []attribute.KeyValue {
	host := ""
	if req != nil && req.URL != nil {
		host = req.URL.Host
	}
	method := ""
	if req != nil {
		method = req.Method
	}

	return []attribute.KeyValue{
		attribute.String("server.address", host),
		attribute.String("http.request.method", method),
	}
}

func (m *retryMetrics) observeDuration(ctx context.Context, req *http.Request, d time.Duration) {
	if m == nil {
		return
	}
	m.duration.Record(ctx, d.Seconds(), otelmetric.WithAttributes(route(req)...))
}

func (m *retryMetrics) observeLimiterWait(ctx context.Context, req *http.Request, d time.Duration) {
	if m == nil {
		return
	}
	m.limiterWait.Record(ctx, d.Seconds(), otelmetric.WithAttributes(route(req)...))
}

// observeAttempt records one completed round-trip. A transport error has no
// status, so it is reported as its own outcome rather than as a status class.
func (m *retryMetrics) observeAttempt(ctx context.Context, req *http.Request, resp *http.Response, err error) {
	if m == nil {
		return
	}
	class := reasonTransport
	if err == nil && resp != nil {
		class = statusClass(resp.StatusCode)
	}
	m.attempts.Add(ctx, 1, otelmetric.WithAttributes(
		append(route(req), attribute.String(attrStatusClass, class))...))
}

// observeRetry records a retry and what triggered it. The status is recorded
// exactly, not as a class: the retryable set is a handful of codes, and
// telling 429 from 503 is the point of the measurement.
func (m *retryMetrics) observeRetry(ctx context.Context, req *http.Request, resp *http.Response, err error) {
	if m == nil {
		return
	}
	reason := reasonTransport
	if err == nil && resp != nil {
		reason = strconv.Itoa(resp.StatusCode)
	}
	m.retries.Add(ctx, 1, otelmetric.WithAttributes(
		append(route(req), attribute.String(attrRetryReason, reason))...))
}

// observeExhausted records giving up while still failing. The two causes are
// worth separating: hitting the attempt cap is an expected end state, whereas
// running out of MaxElapsed budget mid-backoff means the retry configuration
// never got to finish.
func (m *retryMetrics) observeExhausted(ctx context.Context, req *http.Request, cause string) {
	if m == nil {
		return
	}
	m.exhausted.Add(ctx, 1, otelmetric.WithAttributes(
		append(route(req), attribute.String(attrExhaustedCause, cause))...))
}

func (m *retryMetrics) observeBearerRefresh(ctx context.Context, req *http.Request, outcome string) {
	if m == nil {
		return
	}
	m.bearer.Add(ctx, 1, otelmetric.WithAttributes(
		append(route(req), attribute.String(attrBearerRefreshOutcome, outcome))...))
}

// statusClass buckets a status code, keeping the attribute space small for
// the per-attempt counter.
func statusClass(code int) string {
	switch {
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 300 && code < 400:
		return "3xx"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500:
		return "5xx"
	default:
		return "other"
	}
}
