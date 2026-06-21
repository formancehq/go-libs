package observe

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestBodiesTracingHTTPTransportDoesNotDumpSuccessfulRequestWhenDebugDisabled(t *testing.T) {
	t.Parallel()

	transport := WithBodiesTracingHTTPTransport{
		underlying: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       http.NoBody,
			}, nil
		}),
		debug: false,
	}
	req, err := http.NewRequest(http.MethodPost, "http://example.com", errorReadCloser{err: errors.New("body should not be read")})
	require.NoError(t, err)

	var rsp *http.Response
	require.NotPanics(t, func() {
		rsp, err = transport.RoundTrip(req)
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rsp.StatusCode)
}

func TestBodiesTracingHTTPTransportReturnsRequestDumpError(t *testing.T) {
	t.Parallel()

	called := false
	transport := WithBodiesTracingHTTPTransport{
		underlying: roundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return &http.Response{StatusCode: http.StatusOK}, nil
		}),
		debug: true,
	}
	req, err := http.NewRequest(http.MethodPost, "http://example.com", errorReadCloser{err: errors.New("read failed")})
	require.NoError(t, err)

	require.NotPanics(t, func() {
		_, err = transport.RoundTrip(req)
	})

	require.ErrorContains(t, err, "dump http request")
	require.False(t, called)
}

func TestBodiesTracingHTTPTransportClosesGetBodyRequestDump(t *testing.T) {
	t.Parallel()

	var closed atomic.Int32
	transport := WithBodiesTracingHTTPTransport{
		underlying: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       http.NoBody,
			}, nil
		}),
		debug: true,
	}
	req, err := http.NewRequest(http.MethodPost, "http://example.com", strings.NewReader("request body"))
	require.NoError(t, err)
	req.GetBody = func() (io.ReadCloser, error) {
		return closeCountingReadCloser{
			ReadCloser: io.NopCloser(strings.NewReader("request body")),
			closed:     &closed,
		}, nil
	}

	rsp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rsp.StatusCode)
	require.Equal(t, int32(1), closed.Load())
}

func TestBodiesTracingHTTPTransportDoesNotPanicOnResponseDumpError(t *testing.T) {
	t.Parallel()

	readErr := errors.New("read failed")
	transport := WithBodiesTracingHTTPTransport{
		underlying: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       &partialErrorReadCloser{data: []byte("partial body"), err: readErr},
			}, nil
		}),
		debug: true,
	}
	req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)

	var rsp *http.Response
	require.NotPanics(t, func() {
		rsp, err = transport.RoundTrip(req)
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusInternalServerError, rsp.StatusCode)

	body, err := io.ReadAll(rsp.Body)
	require.ErrorIs(t, err, readErr)
	require.Equal(t, "partial body", string(body))
}

func TestBodiesTracingHTTPTransportDumpsErrorResponseWhenDebugDisabled(t *testing.T) {
	t.Parallel()

	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	t.Cleanup(func() {
		require.NoError(t, tracerProvider.Shutdown(context.Background()))
	})

	ctx, span := tracerProvider.Tracer("test").Start(context.Background(), "request")
	transport := WithBodiesTracingHTTPTransport{
		underlying: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				Status:     "500 Internal Server Error",
				StatusCode: http.StatusInternalServerError,
				Header: http.Header{
					"Set-Cookie": []string{"session=secret"},
				},
				Body: io.NopCloser(strings.NewReader("failed")),
			}, nil
		}),
		debug: false,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer secret")

	rsp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	body, err := io.ReadAll(rsp.Body)
	require.NoError(t, err)
	require.Equal(t, "failed", string(body))
	span.End()

	attrs := recordedSpanAttributes(t, spanRecorder)
	require.Contains(t, attrs["raw-request"].AsString(), "GET / HTTP/1.1")
	require.Contains(t, attrs["raw-request"].AsString(), "Authorization: [REDACTED]")
	require.NotContains(t, attrs["raw-request"].AsString(), "Bearer secret")
	require.Contains(t, attrs["raw-response"].AsString(), "500 Internal Server Error")
	require.Contains(t, attrs["raw-response"].AsString(), "Set-Cookie: [REDACTED]")
	require.Contains(t, attrs["raw-response"].AsString(), "failed")
	require.NotContains(t, attrs["raw-response"].AsString(), "session=secret")
}

func recordedSpanAttributes(t *testing.T, spanRecorder *tracetest.SpanRecorder) map[string]attribute.Value {
	t.Helper()

	for _, span := range spanRecorder.Ended() {
		attrs := make(map[string]attribute.Value, len(span.Attributes()))
		for _, attr := range span.Attributes() {
			attrs[string(attr.Key)] = attr.Value
		}
		if _, ok := attrs["raw-request"]; ok {
			return attrs
		}
	}

	t.Fatalf("recorded span with HTTP debug attributes not found")
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type errorReadCloser struct {
	err error
}

func (r errorReadCloser) Read([]byte) (int, error) {
	return 0, r.err
}

func (r errorReadCloser) Close() error {
	return nil
}

type partialErrorReadCloser struct {
	data []byte
	err  error
}

func (r *partialErrorReadCloser) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func (r *partialErrorReadCloser) Close() error {
	return nil
}

type closeCountingReadCloser struct {
	io.ReadCloser
	closed *atomic.Int32
}

func (r closeCountingReadCloser) Close() error {
	r.closed.Add(1)
	return r.ReadCloser.Close()
}
