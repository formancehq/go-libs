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
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// metricCounts maps metric name -> encoded attribute set -> summed value (the
// data-point count for a histogram).
type metricCounts map[string]map[string]int64

// meterFixture returns a private meter provider and a collector over the
// retry transport's instrumentation scope, so no global state is touched and
// the otelhttp instruments sharing the provider stay out of the assertions.
func meterFixture(t *testing.T) (metric.MeterProvider, func() metricCounts) {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	collect := func() metricCounts {
		var rm metricdata.ResourceMetrics
		require.NoError(t, reader.Collect(context.Background(), &rm))

		out := metricCounts{}
		for _, sm := range rm.ScopeMetrics {
			if sm.Scope.Name != instrumentationName {
				continue
			}
			for _, m := range sm.Metrics {
				byAttrs := map[string]int64{}
				switch data := m.Data.(type) {
				case metricdata.Sum[int64]:
					for _, dp := range data.DataPoints {
						byAttrs[dp.Attributes.Encoded(attribute.DefaultEncoder())] += dp.Value
					}
				case metricdata.Histogram[float64]:
					for _, dp := range data.DataPoints {
						byAttrs[dp.Attributes.Encoded(attribute.DefaultEncoder())] += int64(dp.Count)
					}
				default:
					t.Fatalf("metric %s has unexpected data type %T", m.Name, m.Data)
				}
				out[m.Name] = byAttrs
			}
		}

		return out
	}

	return mp, collect
}

// total sums every attribute set for one metric.
func (c metricCounts) total(name string) int64 {
	var sum int64
	for _, v := range c[name] {
		sum += v
	}

	return sum
}

// hasAttr reports whether any data point of the metric carries the substring,
// which asserts on an attribute value without depending on attribute order.
func (c metricCounts) hasAttr(name, substr string) bool {
	for encoded, v := range c[name] {
		if v > 0 && strings.Contains(encoded, substr) {
			return true
		}
	}

	return false
}

// TestMetricsRecordRetryAndAttempts covers the happy retry path: two attempts,
// one retry attributed to the triggering status, and one duration
// measurement for the logical request rather than one per attempt.
func TestMetricsRecordRetryAndAttempts(t *testing.T) {
	t.Parallel()

	mp, counts := meterFixture(t)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)

			return
		}
		_, _ = io.WriteString(w, "{}")
	}))
	defer srv.Close()

	client := quietRetryClient(RetryConfig{
		MeterProvider: mp,
		MaxAttempts:   2,
		BaseDelay:     time.Millisecond,
		MaxDelay:      2 * time.Millisecond,
	})
	resp, err := rtDoRequest(t, client, http.MethodGet, srv.URL+"/x", nil)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	got := counts()
	require.EqualValues(t, 2, got.total(metricRetryAttempts))
	require.EqualValues(t, 1, got.total(metricRetryCount))
	require.True(t, got.hasAttr(metricRetryCount, attrRetryReason+"=429"), "retries not attributed to 429: %v", got[metricRetryCount])
	require.EqualValues(t, 1, got.total(metricRetryRequestDuration), "want one duration per logical request")
	// The successful second attempt and the retried first must be
	// distinguishable.
	require.True(t, got.hasAttr(metricRetryAttempts, "2xx"), "attempts not split by status class: %v", got[metricRetryAttempts])
	require.True(t, got.hasAttr(metricRetryAttempts, "4xx"), "attempts not split by status class: %v", got[metricRetryAttempts])
	// A request that eventually succeeded is not exhaustion.
	require.Zero(t, got.total(metricRetryExhausted))
}

// TestMetricsRecordBudgetExhaustion is the case that is otherwise invisible:
// the retry budget runs out mid-backoff, so the configured retries never
// finish and the client returns the last failure.
func TestMetricsRecordBudgetExhaustion(t *testing.T) {
	t.Parallel()

	mp, counts := meterFixture(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Ask for a wait far beyond the budget below.
		w.Header().Set("Retry-After", "600")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := quietRetryClient(RetryConfig{
		MeterProvider: mp,
		MaxAttempts:   4,
		MaxElapsed:    10 * time.Millisecond,
	})
	resp, err := rtDoRequest(t, client, http.MethodGet, srv.URL+"/x", nil)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode, "want the 503 surfaced")

	got := counts()
	require.EqualValues(t, 1, got.total(metricRetryExhausted))
	require.True(t, got.hasAttr(metricRetryExhausted, causeBudget), "exhaustion not attributed to the budget: %v", got[metricRetryExhausted])
	require.EqualValues(t, 1, got.total(metricRetryAttempts), "the budget must stop the retries before a second attempt")
}

// TestMetricsRecordAttemptCapExhaustion is the other cause: every allowed
// attempt was spent and the request is still failing.
func TestMetricsRecordAttemptCapExhaustion(t *testing.T) {
	t.Parallel()

	mp, counts := meterFixture(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	client := quietRetryClient(RetryConfig{
		MeterProvider: mp,
		MaxAttempts:   2,
		BaseDelay:     time.Millisecond,
		MaxDelay:      2 * time.Millisecond,
	})
	resp, err := rtDoRequest(t, client, http.MethodGet, srv.URL+"/x", nil)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusBadGateway, resp.StatusCode, "want the 502 surfaced")

	got := counts()
	require.EqualValues(t, 1, got.total(metricRetryExhausted))
	require.True(t, got.hasAttr(metricRetryExhausted, causeAttempts), "exhaustion not attributed to the attempt cap: %v", got[metricRetryExhausted])
}

// TestMetricsRecordLimiterWait pins that time lost to client-side pacing is
// measured, so a client paced far below its real budget is visible.
func TestMetricsRecordLimiterWait(t *testing.T) {
	t.Parallel()

	mp, counts := meterFixture(t)
	srv := rtOKServer(t)

	client := quietRetryClient(RetryConfig{MeterProvider: mp, RequestsPerSecond: 50, Burst: 1})
	for range 2 {
		resp, err := rtDoRequest(t, client, http.MethodGet, srv.URL+"/x", nil)
		require.NoError(t, err)
		_ = resp.Body.Close()
	}

	require.EqualValues(t, 2, counts().total(metricLimiterWait), "want one limiter wait per attempt")
}

// TestMetricsOmitThePath guards the cardinality decision: ids and cursors in a
// path must never reach the attribute space.
func TestMetricsOmitThePath(t *testing.T) {
	t.Parallel()

	mp, counts := meterFixture(t)
	srv := rtOKServer(t)

	client := quietRetryClient(RetryConfig{MeterProvider: mp})
	for _, id := range []string{"txn-a", "txn-b", "txn-c"} {
		resp, err := rtDoRequest(t, client, http.MethodGet, srv.URL+"/v1/transactions/"+id, nil)
		require.NoError(t, err)
		_ = resp.Body.Close()
	}

	got := counts()
	// Three distinct paths must collapse onto ONE attribute set.
	require.Len(t, got[metricRetryAttempts], 1, "the path must not be an attribute: %v", got[metricRetryAttempts])
	require.False(t, got.hasAttr(metricRetryAttempts, "txn-a"), "path leaked into the attributes")
}

// TestSpansArePerAttempt pins that the default base transport gives every
// attempt its own client span, and that a resend carries its ordinal as
// http.request.resend_count so a retry storm is readable in the trace.
func TestSpansArePerAttempt(t *testing.T) {
	t.Parallel()

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	client := fastRetryClient(RetryConfig{TracerProvider: tp})
	resp, err := rtDoRequest(t, client, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	spans := exporter.GetSpans()
	require.Len(t, spans, 2, "want one client span per attempt")

	resendCounts := map[int64]bool{}
	for _, span := range spans {
		for _, kv := range span.Attributes {
			if string(kv.Key) == resendCountAttr {
				resendCounts[kv.Value.AsInt64()] = true
			}
		}
	}
	require.Equal(t, map[int64]bool{1: true}, resendCounts, "only the resend carries http.request.resend_count=1")
}

// TestSpanURLIsRedacted: otelhttp stamps url.full from the raw URL, which
// carries a credential for an upstream embedding one in the path or query.
// The span must hold only the redacted rendering, including on retries.
func TestSpanURLIsRedacted(t *testing.T) {
	t.Parallel()

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	const pathKey, queryKey = "k9Ab7Cd3Ef1Gh5Ij2Kl", "q-secret-value"
	client := fastRetryClient(RetryConfig{TracerProvider: tp})
	resp, err := rtDoRequest(t, client, http.MethodGet, srv.URL+"/v2/"+pathKey+"/items?api_key="+queryKey+"&page=2", nil)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	spans := exporter.GetSpans()
	require.Len(t, spans, 2)
	for _, span := range spans {
		var full string
		for _, kv := range span.Attributes {
			require.NotContains(t, kv.Value.Emit(), pathKey, "attribute %s leaks the path credential", kv.Key)
			require.NotContains(t, kv.Value.Emit(), queryKey, "attribute %s leaks the query credential", kv.Key)
			if string(kv.Key) == urlFullAttr {
				full = kv.Value.AsString()
			}
		}
		require.Contains(t, full, "/items", "url.full keeps the route")
		require.Contains(t, full, "page=2", "url.full keeps non-sensitive query values")
	}
}
