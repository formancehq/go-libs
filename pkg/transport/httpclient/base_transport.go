package httpclient

import (
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/formancehq/go-libs/v5/pkg/observe/redact"
)

// Connection-pool defaults for the transport the retry transport builds when
// a RetryConfig supplies no Base.
const (
	// defaultMaxIdleConnsPerHost sizes the pool for API-client traffic: many
	// requests to ONE upstream host. http.DefaultTransport caps this at 2,
	// which is tuned for a browser-shaped spread across many hosts; a client
	// with more than two requests in flight pays a fresh TCP+TLS handshake on
	// every further call.
	defaultMaxIdleConnsPerHost = 32
	// defaultIdleConnTimeout matches http.DefaultTransport; restated because
	// the clone below is the documented shape of the transport.
	defaultIdleConnTimeout = 90 * time.Second
	// defaultPerAttemptTimeout bounds ONE attempt's wait for response headers.
	defaultPerAttemptTimeout = 30 * time.Second
)

// NewBaseTransport returns the base transport the retry transport uses when
// RetryConfig.Base is nil: an http.DefaultTransport clone with a pool sized
// for many requests to one host and a per-attempt response-header deadline
// (perAttemptTimeout <= 0 means 30s).
//
// perAttemptTimeout is applied as ResponseHeaderTimeout rather than as a
// per-attempt context deadline, which is the only correct place for it: the
// caller reads resp.Body after RoundTrip has returned, so a context cancelled
// when the attempt "finishes" would abort a legitimate streaming read.
// ResponseHeaderTimeout bounds exactly what a hung upstream burns, the wait
// for the first response header, and leaves body streaming alone.
func NewBaseTransport(perAttemptTimeout time.Duration) *http.Transport {
	if perAttemptTimeout <= 0 {
		perAttemptTimeout = defaultPerAttemptTimeout
	}

	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		// Only reachable if something reassigned http.DefaultTransport (a test
		// harness). Build a plain transport rather than panic on the assertion.
		return &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConnsPerHost:   defaultMaxIdleConnsPerHost,
			IdleConnTimeout:       defaultIdleConnTimeout,
			ForceAttemptHTTP2:     true,
			ResponseHeaderTimeout: perAttemptTimeout,
		}
	}

	t := base.Clone()
	t.MaxIdleConnsPerHost = defaultMaxIdleConnsPerHost
	t.IdleConnTimeout = defaultIdleConnTimeout
	t.ForceAttemptHTTP2 = true
	t.ResponseHeaderTimeout = perAttemptTimeout

	return t
}

// instrumentedTransport wraps next so every attempt produces its own client
// span and otelhttp request metrics.
//
// The wrapping sits BELOW RetryTransport deliberately: each retry is its own
// round-trip and so its own span, which is what makes a retry storm visible
// in a trace instead of hiding inside one long span.
func instrumentedTransport(next http.RoundTripper, tp trace.TracerProvider, mp metric.MeterProvider) http.RoundTripper {
	var opts []otelhttp.Option
	if tp != nil {
		opts = append(opts, otelhttp.WithTracerProvider(tp))
	}
	if mp != nil {
		opts = append(opts, otelhttp.WithMeterProvider(mp))
	}

	return otelhttp.NewTransport(&attemptSpanAnnotator{next: next}, opts...)
}

// resendCountAttr is the semantic-convention attribute for the ordinal number
// of a resent request. It is written as a literal so that bumping a semconv
// package cannot silently rename it.
const resendCountAttr = "http.request.resend_count"

// attemptKey carries the 1-based attempt number of a retried request down to
// the span annotator.
type attemptKey struct{}

func withAttempt(ctx context.Context, attempt int) context.Context {
	return context.WithValue(ctx, attemptKey{}, attempt)
}

// attemptSpanAnnotator stamps http.request.resend_count on the client span of
// every attempt after the first. It sits INSIDE otelhttp, where the attempt's
// span already exists in the request context. The first attempt carries no
// resend count, as the semantic conventions require.
type attemptSpanAnnotator struct {
	next http.RoundTripper
}

func (a *attemptSpanAnnotator) RoundTrip(req *http.Request) (*http.Response, error) {
	if span := trace.SpanFromContext(req.Context()); span.IsRecording() {
		// otelhttp stamped url.full from the raw URL before calling us. That
		// value carries a credential for any upstream embedding one in the path
		// or query (an API key as a path segment, a presigned URL), so write the
		// redacted rendering over it: SetAttributes replaces an existing key.
		span.SetAttributes(attribute.String(urlFullAttr, redact.URL(req.URL)))
		if attempt, ok := req.Context().Value(attemptKey{}).(int); ok && attempt > 1 {
			span.SetAttributes(attribute.Int(resendCountAttr, attempt-1))
		}
	}

	return a.next.RoundTrip(req)
}

// urlFullAttr is the semconv attribute otelhttp stamps with the request URL,
// written as a literal so a semconv or otelhttp bump cannot silently stop the
// overwrite above from matching the key it replaces.
const urlFullAttr = "url.full"
