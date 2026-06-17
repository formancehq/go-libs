package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

// ignoredPathCase drives both the logging and tracing middleware tests: given a
// set of options and a request path, the request should either be observed
// (logged / traced) or skipped.
type ignoredPathCase struct {
	name     string
	opts     []Option
	path     string
	observed bool
}

func ignoredPathCases() []ignoredPathCase {
	return []ignoredPathCase{
		{"default skips /_healthcheck", nil, "/_healthcheck", false},
		{"default skips /_info", nil, "/_info", false},
		{"default observes other paths", nil, "/organizations", true},
		{"append adds a path", []Option{AppendIgnoredPaths("/ready")}, "/ready", false},
		{"append keeps the defaults", []Option{AppendIgnoredPaths("/ready")}, "/_healthcheck", false},
		{"with replaces the defaults", []Option{WithIgnoredPaths("/ready")}, "/_healthcheck", true},
		{"with skips the listed path", []Option{WithIgnoredPaths("/ready")}, "/ready", false},
		{"empty with observes everything", []Option{WithIgnoredPaths()}, "/_healthcheck", true},
	}
}

func TestLoggerMiddlewareIgnoredPaths(t *testing.T) {
	t.Parallel()

	for _, tc := range ignoredPathCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			buf := &safeBuffer{}
			logger := logging.NewDefaultLogger(buf, false, false, false)

			handler := LoggerMiddleware(logger, tc.opts...)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			handler.ServeHTTP(httptest.NewRecorder(), req)

			if tc.observed {
				require.Contains(t, buf.String(), "Request")
				require.Contains(t, buf.String(), tc.path)
			} else {
				require.NotContains(t, buf.String(), "Request")
			}
		})
	}
}

func TestOTLPMiddlewareIgnoredPaths(t *testing.T) {
	// Not parallel: the test swaps the global tracer provider.
	for _, tc := range ignoredPathCases() {
		t.Run(tc.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

			previous := otel.GetTracerProvider()
			otel.SetTracerProvider(tp)
			t.Cleanup(func() { otel.SetTracerProvider(previous) })

			handler := OTLPMiddleware("test-server", false, tc.opts...)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			handler.ServeHTTP(httptest.NewRecorder(), req)

			require.NoError(t, tp.ForceFlush(context.Background()))

			if tc.observed {
				require.Len(t, recorder.Ended(), 1)
			} else {
				require.Empty(t, recorder.Ended())
			}
		})
	}
}
