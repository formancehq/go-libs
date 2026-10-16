package httpclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	"github.com/formancehq/go-libs/v5/pkg/observe/redact"
)

type observerPolicy struct{ seen []string }

func (p *observerPolicy) Retry(a Attempt) (time.Duration, bool) {
	p.seen = append(p.seen, string(a.Body))
	return time.Nanosecond, a.Count == 1
}

func TestCompletedAttemptObserverPreservesPolicyReplayAndBody(t *testing.T) {
	policy := &observerPolicy{}
	var counts []int
	var signatures []string
	var calls int
	client := NewRetryClient(RetryConfig{
		Logger: logging.NopZap(), MaxAttempts: 2, BaseDelay: time.Nanosecond, BufferBody: 3, Policy: policy,
		Base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			signatures = append(signatures, req.Header.Get("X-Signed"))
			body, err := io.ReadAll(req.Body)
			if err != nil || string(body) != "request" {
				t.Fatalf("request replay=%q err=%v", body, err)
			}
			_ = req.Body.Close()
			return &http.Response{StatusCode: 503, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("response")), Request: req}, nil
		}),
		Sign: func(req *http.Request) error { req.Header.Set("X-Signed", time.Now().String()); return nil },
		OnAttempt: func(ctx context.Context, a Attempt) {
			if ctx != a.Request.Context() {
				t.Fatal("observer lost attempt context")
			}
			counts = append(counts, a.Count)
			head, err := io.ReadAll(io.LimitReader(a.Response.Body, 2))
			if err != nil {
				t.Fatal(err)
			}
			a.Response.Body = &prefixReadCloser{head: head, tail: a.Response.Body}
		},
	})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.test/resource", strings.NewReader("request"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != "response" {
		t.Fatalf("caller body=%q err=%v", body, err)
	}
	if calls != 2 || len(counts) != 2 || counts[0] != 1 || counts[1] != 2 || len(policy.seen) != 1 || policy.seen[0] != "res" {
		t.Fatalf("calls=%d observed=%v policy=%v", calls, counts, policy.seen)
	}
	if signatures[0] == signatures[1] || req.Header.Get("X-Signed") != "" {
		t.Fatal("per-attempt signing mutated caller or reused signature")
	}
}

func TestMetadataDiagnosticsMaskConfiguredValuesWithoutReadingBodies(t *testing.T) {
	redact.ConfigureValues([]string{"configured-diagnostic-value"})
	t.Cleanup(func() { redact.ConfigureValues(nil) })
	var logs bytes.Buffer
	logger := logging.NewZapStack(&logs, logging.ToZapLevel(logging.DebugLevel), logging.ZapStackOptions{JSONFormatting: true})
	requestBody := &diagnosticUnreadBody{t: t}
	client := NewRetryClient(RetryConfig{Logger: logger, PathIsCredential: true, Debug: &DebugConfig{MetadataOnly: true, MaxBodyBytes: -1}, Base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: requestBody, Header: http.Header{"X-Trace": {"configured-diagnostic-value"}}, Request: req}, nil
	})})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://example.test/word-key?cursor=private", requestBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Trace", "configured-diagnostic-value")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	for _, value := range []string{"configured-diagnostic-value", "word-key", "cursor=private"} {
		if strings.Contains(logs.String(), value) {
			t.Fatalf("diagnostic disclosed protected value: %s", logs.String())
		}
	}
	if !strings.Contains(logs.String(), "http request completed") || !strings.Contains(logs.String(), "https://example.test") {
		t.Fatal("metadata diagnostics missing")
	}
}

type diagnosticUnreadBody struct{ t *testing.T }

func (b *diagnosticUnreadBody) Read([]byte) (int, error) {
	b.t.Fatal("metadata diagnostics read application body")
	return 0, io.EOF
}
func (*diagnosticUnreadBody) Close() error { return nil }

type replayWaitToken struct{ calls int }

func (s *replayWaitToken) Token(ctx context.Context) (string, error) {
	s.calls++
	if s.calls == 1 {
		return "old", nil
	}
	if s.calls == 2 {
		return "new", nil
	}
	<-ctx.Done()
	return "", ctx.Err()
}
func (*replayWaitToken) Invalidate(string) {}

func TestBearerReplayWaitBudgetExhaustionIsRecorded(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(t.Context()) })
	calls := 0
	client := NewRetryClient(RetryConfig{Logger: logging.NopZap(), Bearer: &replayWaitToken{}, MaxElapsed: 10 * time.Millisecond, MeterProvider: mp, Base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("rejected")), Request: req}, nil
	})})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.test/resource", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if !errors.Is(err, context.DeadlineExceeded) || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	var result metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &result); err != nil {
		t.Fatal(err)
	}
	var exhausted int64
	for _, scope := range result.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name == metricRetryExhausted {
				for _, point := range metric.Data.(metricdata.Sum[int64]).DataPoints {
					exhausted += point.Value
				}
			}
		}
	}
	if exhausted != 1 {
		t.Fatalf("exhausted=%d want 1", exhausted)
	}
}
