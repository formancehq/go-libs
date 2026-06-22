package httpserver

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

func TestOTLPMiddlewareDebugRedactsAndCapsTraceAttributes(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	previousTracerProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(tracerProvider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previousTracerProvider)
		require.NoError(t, tracerProvider.Shutdown(context.Background()))
	})

	requestBody := `{"body":"` + strings.Repeat("a", debugBodyAttributeLimit+128) + `"}`
	responseBody := `{"body":"` + strings.Repeat("b", debugBodyAttributeLimit+128) + `"}`

	handler := OTLPMiddleware("test-server", true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, requestBody, string(body))

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Set-Cookie", "session=secret")
		w.Header().Set("X-Large-Response", strings.Repeat("r", debugHeaderAttributeLimit))
		w.WriteHeader(http.StatusAccepted)
		_, err = w.Write([]byte(responseBody))
		require.NoError(t, err)
	}))

	req := httptest.NewRequest(http.MethodPost, "/test?debug=true", strings.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Cookie", "session=secret")
	req.Header.Set("X-Debug", "visible")
	req.Header.Set("X-Large", strings.Repeat("h", debugHeaderAttributeLimit))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	require.Equal(t, responseBody, rec.Body.String())

	attrs := recordedSpanAttributes(t, spanRecorder)

	requestHeaders := attrs["http.request.headers"].AsString()
	require.LessOrEqual(t, len(requestHeaders), debugHeaderAttributeLimit)
	require.Contains(t, requestHeaders, "Authorization: "+debugRedactedValue)
	require.Contains(t, requestHeaders, "Cookie: "+debugRedactedValue)
	require.Contains(t, requestHeaders, "X-Debug: visible")
	require.NotContains(t, requestHeaders, "Bearer secret")
	require.NotContains(t, requestHeaders, "session=secret")
	require.True(t, attrs["http.request.headers.truncated"].AsBool())

	responseHeaders := attrs["http.response.headers"].AsString()
	require.LessOrEqual(t, len(responseHeaders), debugHeaderAttributeLimit)
	require.Contains(t, responseHeaders, "Set-Cookie: "+debugRedactedValue)
	require.NotContains(t, responseHeaders, "session=secret")
	require.True(t, attrs["http.response.headers.truncated"].AsBool())

	require.Equal(t, requestBody[:debugBodyAttributeLimit], attrs["http.request.body"].AsString())
	require.True(t, attrs["http.request.body.truncated"].AsBool())
	require.Equal(t, responseBody[:debugBodyAttributeLimit], attrs["http.response.body"].AsString())
	require.True(t, attrs["http.response.body.truncated"].AsBool())
}

func TestOTLPMiddlewareDebugDoesNotPanicOnResponseWriteError(t *testing.T) {
	handler := OTLPMiddleware("test-server", true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"ok":true}`))
	req.Header.Set("Content-Type", "application/json")

	require.NotPanics(t, func() {
		handler.ServeHTTP(&failingResponseWriter{header: make(http.Header)}, req)
	})
}

func TestOTLPMiddlewareDebugRecordsSwitchingProtocolsStatus(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	previousTracerProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(tracerProvider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previousTracerProvider)
		require.NoError(t, tracerProvider.Shutdown(context.Background()))
	})

	handler := OTLPMiddleware("test-server", true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusSwitchingProtocols)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/upgrade", nil))

	require.Equal(t, http.StatusSwitchingProtocols, rec.Code)
	attrs := recordedSpanAttributes(t, spanRecorder)
	require.Equal(t, int64(http.StatusSwitchingProtocols), attrs["http.response.status_code"].AsInt64())
}

func TestOTLPMiddlewareNonDebugRecordsImplicitOKStatus(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	previousTracerProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(tracerProvider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previousTracerProvider)
		require.NoError(t, tracerProvider.Shutdown(context.Background()))
	})

	handler := OTLPMiddleware("test-server", false)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/empty", nil))
	require.NoError(t, tracerProvider.ForceFlush(context.Background()))

	require.Equal(t, http.StatusOK, rec.Code)
	attrs := recordedSpanAttributesWithKey(t, spanRecorder, "http.response.status_code")
	require.Equal(t, int64(http.StatusOK), attrs["http.response.status_code"].AsInt64())
}

func TestOTLPMiddlewarePreservesResponseWriterInterfaces(t *testing.T) {
	for _, debug := range []bool{false, true} {
		t.Run(debugName(debug), func(t *testing.T) {
			base := newOptionalResponseWriter()
			handler := OTLPMiddleware("test-server", debug)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assertOptionalResponseWriterInterfaces(t, w)
			}))

			handler.ServeHTTP(base, httptest.NewRequest(http.MethodGet, "/test", nil))

			require.Equal(t, 1, base.flushes)
			require.Equal(t, 1, base.pushes)
			require.Equal(t, "/events", base.pushTarget)
			require.Equal(t, 1, base.hijacks)
			require.Equal(t, testWriteDeadline, base.writeDeadline)
		})
	}
}

func TestOTLPMiddlewareDoesNotAdvertiseUnsupportedResponseWriterInterfaces(t *testing.T) {
	for _, debug := range []bool{false, true} {
		t.Run(debugName(debug), func(t *testing.T) {
			handler := OTLPMiddleware("test-server", debug)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assertNoOptionalResponseWriterInterfaces(t, w)
			}))

			handler.ServeHTTP(newPlainResponseWriter(), httptest.NewRequest(http.MethodGet, "/test", nil))
		})
	}
}

func TestLoggerMiddlewarePreservesResponseWriterInterfaces(t *testing.T) {
	base := newOptionalResponseWriter()
	handler := LoggerMiddleware(logging.Testing())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertOptionalResponseWriterInterfaces(t, w)
	}))

	handler.ServeHTTP(base, httptest.NewRequest(http.MethodGet, "/test", nil))

	require.Equal(t, 1, base.flushes)
	require.Equal(t, 1, base.pushes)
	require.Equal(t, "/events", base.pushTarget)
	require.Equal(t, 1, base.hijacks)
	require.Equal(t, testWriteDeadline, base.writeDeadline)
}

func TestLoggingResponseWriterUnwrapsForResponseController(t *testing.T) {
	base := newOptionalResponseWriter()
	controller := http.NewResponseController(NewLoggingResponseWriter(base))

	require.NoError(t, controller.Flush())
	require.NoError(t, controller.SetWriteDeadline(testWriteDeadline))

	require.Equal(t, 1, base.flushes)
	require.Equal(t, testWriteDeadline, base.writeDeadline)
}

func TestLoggerMiddlewareRecordsImplicitOKStatus(t *testing.T) {
	buf := &safeBuffer{}
	logger := logging.NewDefaultLogger(buf, false, false, false)
	handler := LoggerMiddleware(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/empty", nil))

	require.Contains(t, buf.String(), "status=200")
	require.NotContains(t, buf.String(), "status=0")
}

func recordedSpanAttributes(t *testing.T, spanRecorder *tracetest.SpanRecorder) map[string]attribute.Value {
	t.Helper()

	return recordedSpanAttributesWithKey(t, spanRecorder, "http.request.headers")
}

func recordedSpanAttributesWithKey(t *testing.T, spanRecorder *tracetest.SpanRecorder, key string) map[string]attribute.Value {
	t.Helper()

	for _, span := range spanRecorder.Ended() {
		attrs := make(map[string]attribute.Value, len(span.Attributes()))
		for _, attr := range span.Attributes() {
			attrs[string(attr.Key)] = attr.Value
		}
		if _, ok := attrs[key]; ok {
			return attrs
		}
	}

	t.Fatalf("recorded span with %s attribute not found", key)
	return nil
}

func debugName(debug bool) string {
	if debug {
		return "debug=true"
	}
	return "debug=false"
}

func assertOptionalResponseWriterInterfaces(t *testing.T, w http.ResponseWriter) {
	t.Helper()

	flusher, ok := w.(http.Flusher)
	require.True(t, ok)
	flusher.Flush()

	pusher, ok := w.(http.Pusher)
	require.True(t, ok)
	require.NoError(t, pusher.Push("/events", nil))

	require.NoError(t, http.NewResponseController(w).SetWriteDeadline(testWriteDeadline))

	hijacker, ok := w.(http.Hijacker)
	require.True(t, ok)
	_, _, err := hijacker.Hijack()
	require.ErrorIs(t, err, errTestHijack)
}

func assertNoOptionalResponseWriterInterfaces(t *testing.T, w http.ResponseWriter) {
	t.Helper()

	_, ok := w.(http.Flusher)
	require.False(t, ok)
	_, ok = w.(http.Hijacker)
	require.False(t, ok)
	_, ok = w.(http.Pusher)
	require.False(t, ok)
}

var (
	errTestHijack     = errors.New("test hijack")
	testWriteDeadline = time.Unix(123, 0)
)

type optionalResponseWriter struct {
	header        http.Header
	flushes       int
	hijacks       int
	pushes        int
	pushTarget    string
	writeDeadline time.Time
}

func newOptionalResponseWriter() *optionalResponseWriter {
	return &optionalResponseWriter{
		header: make(http.Header),
	}
}

func (w *optionalResponseWriter) Header() http.Header {
	return w.header
}

func (w *optionalResponseWriter) Write(data []byte) (int, error) {
	return len(data), nil
}

func (w *optionalResponseWriter) WriteHeader(int) {}

func (w *optionalResponseWriter) Flush() {
	w.flushes++
}

func (w *optionalResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.hijacks++
	return nil, nil, errTestHijack
}

func (w *optionalResponseWriter) Push(target string, _ *http.PushOptions) error {
	w.pushes++
	w.pushTarget = target
	return nil
}

func (w *optionalResponseWriter) SetWriteDeadline(deadline time.Time) error {
	w.writeDeadline = deadline
	return nil
}

type plainResponseWriter struct {
	header http.Header
}

func newPlainResponseWriter() *plainResponseWriter {
	return &plainResponseWriter{
		header: make(http.Header),
	}
}

func (w *plainResponseWriter) Header() http.Header {
	return w.header
}

func (w *plainResponseWriter) Write(data []byte) (int, error) {
	return len(data), nil
}

func (w *plainResponseWriter) WriteHeader(int) {}

type failingResponseWriter struct {
	header http.Header
}

func (w *failingResponseWriter) Header() http.Header {
	return w.header
}

func (w *failingResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func (w *failingResponseWriter) WriteHeader(int) {}
